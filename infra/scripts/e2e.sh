#!/usr/bin/env bash
# Playwright on an isolated, disposable stack: Compose project "ifri-e2e" with its own
# PostgreSQL, Redis, MinIO and OCR worker, on other host ports. It reuses the images already
# built for the development stack (never rebuilds) and is destroyed afterwards with its own
# volumes only: nothing is left in the development database.
# Usage: infra/scripts/e2e.sh [playwright arguments]
#   E2E_MEMORY_REPORT=/path/file  samples `docker stats` of every container while tests run.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROJECT=ifri-e2e
export API_PORT="${E2E_API_PORT:-18180}"
export MINIO_API_PORT="${E2E_MINIO_API_PORT:-19100}"
export MINIO_CONSOLE_PORT="${E2E_MINIO_CONSOLE_PORT:-19101}"
compose() { docker compose --project-directory "$ROOT" -p "$PROJECT" "$@"; }

sampler=""
cleanup() {
  if [ -n "$sampler" ]; then kill "$sampler" 2>/dev/null || true; fi
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

for image in ifri-admissions-api ifri-admissions-ocr-worker ifri-admissions-minio ifri-admissions-minio-init; do
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "Image $image missing: build it first (make check-fast or docker compose build)." >&2
    exit 1
  fi
done
cleanup # leftovers of an interrupted run
compose up -d --no-build --wait --wait-timeout 300 postgres redis minio minio-init api ocr-worker
compose exec -T api db up
compose exec -T api db seed

if [ -n "${E2E_MEMORY_REPORT:-}" ]; then
  : >"$E2E_MEMORY_REPORT"
  (while true; do
    docker stats --no-stream --format '{{.Name}}\t{{.MemUsage}}' >>"$E2E_MEMORY_REPORT" 2>/dev/null || true
    sleep 2
  done) &
  sampler=$!
fi

cd "$ROOT/apps/web"
E2E_PROJECT="$PROJECT" E2E_ROOT="$ROOT" npx playwright test "$@"
