"""Run the optional real document calibration without including it in an image."""

import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PRIVATE = ROOT / "services/ocr/tests/private-fixtures"


def main():
    if not (PRIVATE / "Relever.pdf").is_file():
        print("Private real scanned BAC transcript: NOT TESTED (fixture absent).")
        return
    if not (PRIVATE / "oracle.json").is_file():
        raise SystemExit(
            "Create the local ignored oracle.json before private calibration."
        )
    debug = Path(tempfile.mkdtemp(prefix="ifri-ocr-private-debug-", dir="/tmp"))
    print(f"Private debug images (local only): {debug}", flush=True)
    command = [
        "docker",
        "compose",
        "--profile",
        "test",
        "run",
        "--build",
        "--no-deps",
        "--rm",
        "--entrypoint",
        "python",
        "--volume",
        f"{(PRIVATE / 'Relever.pdf').resolve()}:/private/Relever.pdf:ro",
        "--volume",
        f"{(PRIVATE / 'oracle.json').resolve()}:/private/oracle.json:ro",
        "--env",
        "OCR_PRIVATE_FIXTURE_ROOT=/private",
        "--env",
        "OCR_PAGE_TIMEOUT_SECONDS=120",
        "--volume",
        f"{debug}:/private-debug:rw",
        "--env",
        "OCR_PRIVATE_DEBUG_DIR=/private-debug",
        "ocr-tests",
        "/app/ocr_private_calibration.py",
    ]
    if "--evaluate" in sys.argv[1:]:
        command.append("--evaluate")
    raise SystemExit(subprocess.run(command, cwd=ROOT, check=False).returncode)


if __name__ == "__main__":
    main()
