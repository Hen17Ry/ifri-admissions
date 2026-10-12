"""Verify Compose health, public technical endpoints and private local bucket."""

import json
import subprocess
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from dev import ROOT, environment


def main():
    env = environment()
    result = subprocess.run(
        ["docker", "compose", "ps", "--all", "--format", "json"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    text = result.stdout.strip()
    services = (
        json.loads(text)
        if text.startswith("[")
        else [json.loads(line) for line in text.splitlines()]
    )
    names = {service["Service"]: service for service in services}
    for name in ("postgres", "redis", "minio", "api", "ocr-worker"):
        if names.get(name, {}).get("Health") != "healthy":
            raise RuntimeError(f"{name}: Docker health is not healthy")
    if (
        names.get("minio-init", {}).get("State") != "exited"
        or names.get("minio-init", {}).get("ExitCode") != 0
    ):
        raise RuntimeError("minio-init failed")

    ping = subprocess.run(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "redis",
            "sh",
            "-ec",
            'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping',
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    if ping.stdout.strip() != "PONG":
        raise RuntimeError("Authenticated Redis PING failed")
    print("Redis PING: PONG")

    identity = subprocess.run(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "postgres",
            "sh",
            "-ec",
            'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 '
            '-U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1 '
            "-c \"SELECT json_build_object('database', current_database(), "
            "'user', current_user)\"",
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    actual = json.loads(identity.stdout)
    expected = {"database": env["POSTGRES_DB"], "user": env["POSTGRES_USER"]}
    if actual != expected:
        raise RuntimeError("PostgreSQL database/user does not match configuration")
    print("PostgreSQL connection: configured database and user verified")

    api = f"http://127.0.0.1:{env['API_PORT']}"
    for path in ("health", "ready"):
        with urlopen(f"{api}/{path}", timeout=5) as response:
            payload = json.load(response)
            if payload.get("status") != "ok":
                raise RuntimeError(f"API /{path} not ready")
            if path == "ready" and payload.get("checks") != {
                "postgres": "ok",
                "redis": "ok",
                "minio": "ok",
            }:
                raise RuntimeError("API readiness dependencies are not all available")
            print(f"GET /{path}: HTTP {response.status}, status=ok")

    allowed = Request(f"{api}/health", headers={"Origin": env["FRONTEND_ORIGIN"]})
    with urlopen(allowed, timeout=5) as response:
        if response.headers.get("Access-Control-Allow-Origin") != env["FRONTEND_ORIGIN"]:
            raise RuntimeError("Configured frontend origin is not allowed")
    denied = Request(f"{api}/health", headers={"Origin": "https://bootstrap-invalid.example"})
    try:
        with urlopen(denied, timeout=5):
            raise RuntimeError("Unconfigured frontend origin unexpectedly allowed")
    except HTTPError as error:
        if error.code != 403 or error.headers.get("Access-Control-Allow-Origin"):
            raise RuntimeError("CORS rejection is invalid") from None
    print("CORS: configured origin allowed, other origin rejected")
    try:
        with urlopen(f"{env['MINIO_ENDPOINT']}/{env['MINIO_BUCKET']}", timeout=5):
            raise RuntimeError("Anonymous bucket listing unexpectedly allowed")
    except HTTPError as error:
        if error.code != 403:
            raise RuntimeError("Expected private bucket to deny anonymous access") from None
    print("All five services healthy; API ready; bucket denies anonymous access.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Smoke check failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None
