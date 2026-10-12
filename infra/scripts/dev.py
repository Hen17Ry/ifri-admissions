"""Run native API/worker with the root .env, on PowerShell or Unix shells."""

import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def environment():
    values = os.environ.copy()
    env_file = ROOT / ".env"
    if not env_file.exists():
        raise ValueError("Copy .env.example to .env before starting a native service")
    for line in env_file.read_text(encoding="utf-8-sig").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError("Invalid .env line: expected KEY=value")
        key, value = line.split("=", 1)
        values.setdefault(key.strip(), value.strip().strip("\"'"))
    return values


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in {"api", "worker"}:
        print("Usage: python infra/scripts/dev.py api|worker", file=sys.stderr)
        return 1
    try:
        env = environment()
        if sys.argv[1] == "api":
            command = ["go", "run", "-buildvcs=false", "./cmd/api"]
            cwd = ROOT / "services/api"
        else:
            command = ["uv", "run", "--no-sync", "ifri-ocr"]
            cwd = ROOT / "services/ocr"
        return subprocess.call(command, cwd=cwd, env=env)
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
