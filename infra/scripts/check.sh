#!/usr/bin/env bash
# Local CI until a remote exists: runs every validation, continues after a failure, prints a
# PASS/FAIL table with durations and exits non-zero if anything failed.
# Usage: infra/scripts/check.sh [static|stack|browser|all]   (default: all)
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCOPE="${1:-all}"
LOGS="$(mktemp -d "${TMPDIR:-/tmp}/ifri-check-XXXXXX")"
PY=(uv run --project "$ROOT/services/ocr" --locked --no-sync)
RUFF=("${PY[@]}" ruff)
results=()
failed=0

step() {
  local name="$1" dir="$2"
  shift 2
  local log="$LOGS/$name.log" started ended status
  started=$(date +%s)
  if (cd "$dir" && "$@") >"$log" 2>&1; then status=PASS; else status=FAIL; failed=1; fi
  ended=$(date +%s)
  results+=("$(printf '%-24s %-4s %5ss' "$name" "$status" "$((ended - started))")")
  printf '%-24s %s\n' "$name" "$status"
  if [ "$status" = FAIL ]; then tail -n 25 "$log" | sed 's/^/    /'; fi
}

static() {
  local web="$ROOT/apps/web" api="$ROOT/services/api" ocr="$ROOT/services/ocr"
  step web-build "$web" npm run build
  step web-typecheck "$web" npm run typecheck
  step web-lint "$web" npm run lint
  step web-vitest "$web" npm run test
  step web-format "$web" npm run format:check
  step go-gofmt "$api" sh -c 'out=$(gofmt -l .); echo "$out"; test -z "$out"'
  step go-vet "$api" sh -c 'go vet ./... && go vet -tags=integration ./...'
  step go-test "$api" go test -count=1 ./...
  step go-build "$api" sh -c 'for c in api db ocrvalidate; do go build -buildvcs=false -o /dev/null ./cmd/$c || exit 1; done'
  step py-pytest "$ocr" "${PY[@]}" pytest -q -o cache_dir="$LOGS/pytest-cache"
  step py-ruff "$ocr" sh -c "${RUFF[*]} check . --no-cache && ${RUFF[*]} format --check . --no-cache"
  step scripts-ruff "$ROOT" sh -c "${RUFF[*]} check --config services/ocr/pyproject.toml --no-cache infra/scripts && ${RUFF[*]} format --check --config services/ocr/pyproject.toml --no-cache infra/scripts"
  step compose-config "$ROOT" sh -c 'docker compose config --quiet && docker compose --profile test config --quiet'
  step git-diff-check "$ROOT" git diff --check
}

stack() {
  step compose-up "$ROOT" docker compose up -d --build --wait --wait-timeout 300
  step db-migrate "$ROOT" docker compose exec -T api db up
  step db-seed "$ROOT" sh -c 'docker compose exec -T api db seed && docker compose exec -T api db seed'
  step go-integration "$ROOT" docker compose --profile test run --build --rm references-tests
  for smoke in smoke references_smoke drafts_smoke documents_smoke; do
    step "$smoke" "$ROOT" "${PY[@]}" python "infra/scripts/$smoke.py"
  done
  # pytest with real Tesseract, byte-identical fixture regeneration, realistic BAC benchmark.
  fixtures_digest > "$LOGS/fixtures.before"
  step ocr-tests "$ROOT" docker compose --profile test run --build --rm ocr-tests
  fixtures_digest > "$LOGS/fixtures.after"
  step fixtures-untouched "$ROOT" cmp "$LOGS/fixtures.before" "$LOGS/fixtures.after"
  step ocr_smoke "$ROOT" "${PY[@]}" python infra/scripts/ocr_smoke.py
}

fixtures_digest() {
  (cd "$ROOT/services/ocr/tests/fixtures" && find . -type f -print0 | sort -z | xargs -0 sha256sum)
}

browser() {
  step playwright "$ROOT/apps/web" npm run test:browser
}

case "$SCOPE" in
  static) static ;;
  stack) stack ;;
  browser) browser ;;
  all) static; stack; browser ;;
  *) echo "usage: $0 [static|stack|browser|all]" >&2; exit 2 ;;
esac

echo
echo "Summary ($SCOPE) — logs: $LOGS"
printf '%s\n' "${results[@]}"
if [ "$failed" -ne 0 ]; then
  echo "CHECK FAILED"
  exit 1
fi
echo "CHECK PASSED"
