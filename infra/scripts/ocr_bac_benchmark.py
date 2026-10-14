"""Realistic synthetic BAC general benchmark: cell path end to end, generated in a temp dir.

Fixtures come from services/ocr/tests/support/synthetic_bac.py (deterministic, no real data) and
are never written into the repository. Each variant must take the BAC_CELL_GRID path, reach its
expected status, and accept no false value. Expected subjects come from the reference API.
"""

import json
import os
import sys
import tempfile
from decimal import Decimal
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

HERE = Path(__file__).resolve()
for candidate in (Path("/app/tests"), *(parent / "services/ocr/tests" for parent in HERE.parents)):
    if (candidate / "support").is_dir():
        sys.path.insert(0, str(candidate))
        break

from ifri_ocr.engines import production_engine  # noqa: E402
from ifri_ocr.extraction import analyze  # noqa: E402
from support.synthetic_bac import (  # noqa: E402
    TARGET_LABELS,
    VARIANTS,
    fixture_bytes,
    native_bytes,
    variant,
)

REFERENCE = os.environ.get("OCR_REFERENCE_URL", "http://127.0.0.1:18080")


def expected_subjects(qualification):
    url = (
        f"{REFERENCE}/api/v1/expected-subjects?campaign=DEV_LOCAL"
        f"&program=GL&qualification={qualification}"
    )
    with urlopen(url, timeout=10) as response:
        subjects = json.load(response)["expectedSubjects"]
    return [{**s, "id": s["code"]} for s in subjects]


def run(name, data, qualification, minimum, page_timeout, engine):
    transcript, _ = variant(name)
    expected = expected_subjects(qualification)
    oracle = {
        code: (Decimal(grade), Decimal(coefficient))
        for code, (grade, coefficient) in transcript.oracle(TARGET_LABELS).items()
    }
    with tempfile.TemporaryDirectory(prefix="ifri-bac-") as directory:
        path = Path(directory) / f"{name}.pdf"
        path.write_bytes(data)
        diagnostics = []
        started = perf_counter()
        result = analyze(
            path,
            expected,
            engine,
            minimum=minimum,
            diagnostics=diagnostics,
            page_timeout=page_timeout,
            qualification=qualification,
        )
        seconds = round(perf_counter() - started, 3)
    values = {
        s["subjectId"]: (Decimal(s["gradeValue"]), Decimal(s["coefficientValue"]))
        for s in result["subjects"]
    }
    false_values = sum(values[code] != oracle.get(code) for code in values)
    cell = next((d for d in diagnostics if d.get("strategy") == "BAC_CELL_GRID"), {})
    return {
        "fixture": name,
        "status": result["status"],
        "failureCode": result["failureCode"],
        "seconds": seconds,
        "cellPath": bool(cell),
        "exactValues": result["status"] == "SUCCEEDED"
        and set(values) == {s["id"] for s in expected}
        and false_values == 0,
        "falseValuesAccepted": false_values if result["status"] == "SUCCEEDED" else 0,
        "series": cell.get("seriesStatus"),
        "engine": result.get("engine"),
        "anchorsSeconds": cell.get("anchorsSeconds"),
        "cellsSeconds": cell.get("cellsSeconds"),
    }


def main():
    # OCR_RAPIDOCR_ENABLED=false measures the Tesseract digit reader in the same image.
    engine = production_engine()
    print(f"Numeric cell reader: {engine.cells.name}")
    minimum = float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85"))
    page_timeout = int(os.environ.get("OCR_PAGE_TIMEOUT_SECONDS", "30"))
    measurements, failures = [], []
    for name, (_, status, failure, cell_path) in VARIANTS.items():
        data = fixture_bytes(name)
        if data != fixture_bytes(name):
            failures.append(f"{name}: generator not deterministic")
        _, qualification = variant(name)
        m = run(name, data, qualification, minimum, page_timeout, engine)
        measurements.append(m)
        if m["falseValuesAccepted"]:
            failures.append(f"{name}: FALSE VALUE ACCEPTED")
        if (m["status"], m["failureCode"]) != (status, failure):
            failures.append(f"{name}: {m['status']}/{m['failureCode']} != {status}/{failure}")
        if cell_path and not m["cellPath"]:
            failures.append(f"{name}: cell path not used")
        if status == "SUCCEEDED" and not m["exactValues"]:
            failures.append(f"{name}: values not exact")
    native = run("d-scan", native_bytes(), "BAC_D", minimum, page_timeout, engine)
    native["fixture"] = "d-native-text"
    measurements.append(native)
    if not native["exactValues"] or native["cellPath"]:
        failures.append("d-native-text: native path must give exact values without OCR")
    for m in measurements:
        print(
            f"{m['fixture']}: {m['status']} {m['failureCode']} {m['seconds']}s "
            f"cellPath={m['cellPath']} exactValues={m['exactValues']} "
            f"falseAccepted={m['falseValuesAccepted']} series={m['series']} "
            f"anchors={m['anchorsSeconds']}s cells={m['cellsSeconds']}s"
        )
    if failures:
        raise SystemExit("Synthetic BAC benchmark failed: " + "; ".join(failures))
    print(f"Synthetic BAC benchmark PASS: {len(measurements)} fixtures, 0 false value accepted.")


if __name__ == "__main__":
    main()
