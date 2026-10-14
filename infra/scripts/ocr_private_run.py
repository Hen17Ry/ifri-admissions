"""Evaluate the ignored private corpus with the production pipeline and the Go validator.

Every services/ocr/tests/private-fixtures/<name>.pdf with a <name>.expected.json oracle is
mounted read-only under an anonymous name (doc-N). Results containing values live only in a
private temporary directory that is always deleted; debug images are written only with
--keep-debug. The printed report contains per-field outcomes and pipeline facts, never names,
raw OCR text, grades, coefficients, hashes or file names. Exit code 1 if a false value was
accepted (the release blocker), 0 otherwise; NOT TESTED when the corpus is absent.
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[2]
PRIVATE = ROOT / "services/ocr/tests/private-fixtures"


def corpus():
    pairs = []
    for pdf in sorted(PRIVATE.glob("*.pdf")):
        oracle = pdf.with_suffix(".expected.json")
        if oracle.is_file():
            pairs.append((pdf.resolve(), oracle.resolve()))
    return pairs


def compose(*arguments, capture=True):
    command = ["docker", "compose", "--profile", "test", *arguments]
    return subprocess.run(command, cwd=ROOT, check=False, capture_output=capture, text=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--keep-debug", action="store_true", help="keep debug crops in /tmp")
    parser.add_argument("--page-timeout", type=int, help="override the production page budget")
    parser.add_argument("--report", type=Path, help="also write the anonymous report there")
    parser.add_argument(
        "--numeric-engine",
        choices=("rapidocr", "tesseract"),
        default="rapidocr",
        help="digit reader of the BAC cells (production default: rapidocr)",
    )
    args = parser.parse_args()
    pairs = corpus()
    if not pairs:
        print("Private real transcripts: NOT TESTED (no <name>.pdf + <name>.expected.json).")
        return 0
    port = os.environ.get("API_PORT", "18080")
    try:
        with urlopen(f"http://127.0.0.1:{port}/ready", timeout=5) as response:
            if response.status != 200:
                raise OSError
    except OSError:
        print("Reference API not ready: run `docker compose up -d --wait` first.")
        return 2
    work = Path(tempfile.mkdtemp(prefix="ifri-ocr-private-", dir="/tmp"))
    work.chmod(0o700)
    out, debug = work / "out", work / "debug"
    out.mkdir()
    debug.mkdir()
    try:
        volumes = []
        for index, (pdf, oracle) in enumerate(pairs, start=1):
            volumes += ["--volume", f"{pdf}:/private/doc-{index}.pdf:ro"]
            volumes += ["--volume", f"{oracle}:/private/doc-{index}.expected.json:ro"]
        environment = ["--env", "OCR_PRIVATE_FIXTURE_ROOT=/private"]
        environment += ["--env", "OCR_PRIVATE_OUT=/private-out"]
        rapidocr = "true" if args.numeric_engine == "rapidocr" else "false"
        environment += ["--env", f"OCR_RAPIDOCR_ENABLED={rapidocr}"]
        if args.page_timeout:
            environment += ["--env", f"OCR_PAGE_TIMEOUT_SECONDS={args.page_timeout}"]
        if args.keep_debug:
            environment += ["--env", "OCR_PRIVATE_DEBUG_DIR=/private-debug"]
        run = compose(
            "run",
            "--build",
            "--no-deps",
            "--rm",
            "--entrypoint",
            "python",
            *volumes,
            "--volume",
            f"{out}:/private-out:rw",
            "--volume",
            f"{debug}:/private-debug:rw",
            *environment,
            "ocr-tests",
            "/app/ocr_private_eval.py",
        )
        if run.returncode:
            print("Private evaluation unavailable: check the OCR image and reference API.")
            return 2
        # Compose may print build progress on stdout before the harness report.
        report = json.loads(run.stdout[run.stdout.index('{\n  "pageTimeoutSeconds"') :])
        verdicts = compose(
            "run",
            "--build",
            "--no-deps",
            "--rm",
            "-T",
            "--volume",
            f"{out}:/private-out:ro",
            "references-tests",
            "go",
            "run",
            "./cmd/ocrvalidate",
            "/private-out",
        )
        if verdicts.returncode:
            print("Go validation unavailable: check the API test image.")
            return 2
        by_document = {
            v["document"]: v for v in json.loads(verdicts.stdout.strip().splitlines()[-1])
        }
        false_accepted = 0
        for document in report["documents"]:
            verdict = by_document.get(document["document"], {})
            document["goAccepted"] = verdict.get("accepted", False)
            document["goReason"] = verdict.get("reason", "")
            document["accepted"] = document.get("workerAccepted", False) and document["goAccepted"]
            document["falseValuesAccepted"] = (
                document.pop("falseValuesAcceptedByWorker", 0) if document["accepted"] else 0
            )
            false_accepted += document["falseValuesAccepted"]
        outcomes = [o for d in report["documents"] for o in d.get("fields", {}).values()]
        report["summary"] = {
            "documents": len(report["documents"]),
            "accepted": sum(d.get("accepted", False) for d in report["documents"]),
            "fields": {k: outcomes.count(k) for k in ("EXACT", "FALSE", "MISSING", "NO_ORACLE")},
            "falseValuesAccepted": false_accepted,
            # Rows validated on their own while the document failed: must stay 0 as well.
            "falseRowDecisions": sum(d.get("falseRowDecisions", 0) for d in report["documents"]),
        }
        text = json.dumps(report, indent=2)
        print(text)
        if args.report:
            args.report.write_text(text + "\n")
        return 1 if false_accepted or report["summary"]["falseRowDecisions"] else 0
    finally:
        shutil.rmtree(out, ignore_errors=True)
        if args.keep_debug and any(debug.iterdir()):
            print(f"Private debug images kept (delete after use): {debug}", file=sys.stderr)
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
