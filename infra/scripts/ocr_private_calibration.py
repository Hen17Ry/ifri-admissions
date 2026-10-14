"""Opt-in local private calibration. No PDF, identity, full text or hash is emitted."""

import json
import os
from decimal import Decimal
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

import pymupdf
from ifri_ocr.extraction import Tesseract, analyze


def main():
    pymupdf.TOOLS.mupdf_display_errors(False)
    pymupdf.TOOLS.mupdf_display_warnings(False)
    root = Path(
        os.environ.get(
            "OCR_PRIVATE_FIXTURE_ROOT", "services/ocr/tests/private-fixtures"
        )
    )
    pdf = root / "Relever.pdf"
    if not pdf.is_file():
        print("Private real scanned BAC transcript: NOT TESTED (fixture absent).")
        return
    oracle_path = root / "oracle.json"
    if not oracle_path.is_file():
        raise SystemExit(
            "Private oracle.json required; keep it in the ignored private-fixtures directory."
        )
    oracle = json.loads(oracle_path.read_text())
    reference = os.environ.get("OCR_REFERENCE_URL", "http://127.0.0.1:18080")
    with urlopen(
        reference
        + "/api/v1/expected-subjects?campaign=DEV_LOCAL&program=GL&qualification=BAC_D",
        timeout=10,
    ) as response:
        data = json.load(response)
    expected = [
        {"id": s["code"], "name": s["name"], "position": s["position"]}
        for s in data["expectedSubjects"]
    ]
    diagnostics = []
    started = perf_counter()
    result = analyze(
        pdf,
        expected,
        Tesseract(),
        minimum=float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85")),
        diagnostics=diagnostics,
        page_timeout=int(os.environ.get("OCR_PAGE_TIMEOUT_SECONDS", "30")),
    )
    selected = [
        {
            "subject": s["subjectId"],
            "grade": s["gradeValue"],
            "coefficient": s["coefficientValue"],
            "confidence": s["overallConfidence"],
            "page": s["pageNumber"],
        }
        for s in result["subjects"]
    ]
    exact = (
        result["status"] == "SUCCEEDED"
        and len(selected) == len(expected)
        and all(
            (Decimal(s["grade"]), Decimal(s["coefficient"]))
            == tuple(map(Decimal, oracle[s["subject"]]))
            for s in selected
        )
    )
    # Anonymous output only; raw tokens and any identifying header are never printed.
    print(
        json.dumps(
            {
                "category": "D_PRIVATE_LOCAL",
                "document": "real scanned BAC D transcript",
                "status": result["status"],
                "failureCode": result["failureCode"],
                "seconds": round(perf_counter() - started, 3),
                "exactValues": exact,
                "subjects": selected,
                "passes": diagnostics,
            },
            indent=2,
        )
    )
    if not exact:
        raise SystemExit("Private transcript calibration failed safely.")


if __name__ == "__main__":
    try:
        main()
    except Exception:  # noqa: BLE001 -- do not reveal private parser exception text
        # Keep parser/storage exceptions out of terminal output for a private document.
        raise SystemExit(
            "Private calibration unavailable: check runtime and reference API."
        ) from None
