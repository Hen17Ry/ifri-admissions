"""Private corpus evaluation, run inside the OCR image. Anonymous structural report only.

For every <name>.pdf mounted under OCR_PRIVATE_FIXTURE_ROOT with a <name>.expected.json oracle,
the production pipeline runs with the declared qualification. The raw worker result is written to
OCR_PRIVATE_OUT for the independent Go validation; stdout never carries names, raw OCR text,
grades, coefficients, hashes or file names, only per-field outcomes and pipeline diagnostics.
"""

import json
import os
from decimal import Decimal, InvalidOperation
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

import pymupdf
from ifri_ocr.bac.assignment import MIN_SCORE
from ifri_ocr.engines import production_engine
from ifri_ocr.extraction import analyze


def expected_subjects(reference, program, qualification):
    url = (
        f"{reference}/api/v1/expected-subjects?campaign=DEV_LOCAL"
        f"&program={program}&qualification={qualification}"
    )
    with urlopen(url, timeout=10) as response:
        data = json.load(response)
    return [
        {"id": s["code"], "code": s["code"], "name": s["name"], "position": s["position"]}
        for s in data["expectedSubjects"]
    ]


def same(value, oracle):
    try:
        return Decimal(str(value)) == Decimal(str(oracle).replace(",", "."))
    except InvalidOperation:
        return False


def structure(diagnostics):
    """Pipeline facts without document content."""
    cell = next((d for d in diagnostics if d.get("strategy") == "BAC_CELL_GRID"), None)
    if cell is None:
        return {"strategy": "GENERIC_SCAN_OR_NATIVE", "passes": len(diagnostics)}
    rows = cell.get("rows", [])
    subjects = rows[: cell.get("subjectRowCount", len(rows))]
    segmentation = cell.get("segmentation", {})
    return {
        "strategy": "BAC_CELL_GRID",
        "failureCode": cell.get("failureCode"),
        "columnStrategy": segmentation.get("columnStrategy"),
        # Geometry only (pixel bands at 300 DPI), no content.
        "geometry": {
            k: segmentation.get(k)
            for k in (
                "horizontalRules",
                "allRowBands",
                "rowBands",
                "tableSplitAfterBand",
                "medianInterBandGap",
                "headerExcludedThrough",
                "outerBorder",
                "relativeWidths",
            )
        },
        "dataRowCount": segmentation.get("dataRowCount"),
        "subjectRowCount": cell.get("subjectRowCount"),
        "totalRowSource": cell.get("totalRowSource"),
        "leadingNonDataRows": cell.get("leadingNonDataRows"),
        "seriesStatus": cell.get("seriesStatus"),
        "seriesCandidate": cell.get("seriesCandidate"),
        "seriesZone": cell.get("seriesZone"),
        "sectionStart": cell.get("sectionStart"),
        "labelsRecognized": sum(max(r["wordScores"].values()) >= MIN_SCORE for r in subjects),
        "coefficientsValidated": sum(r.get("coefficientStatus") == "VALIDATED" for r in subjects),
        "cellsReadable": {
            name: sum(r["cellSources"][column] == "READ" for r in subjects)
            for column, name in enumerate(("note", "coefficient", "points", "scale"))
        },
        "cellsReconstructed": sum(
            source == "RECONSTRUCTED_FROM_TOTAL" for r in subjects for source in r["cellSources"]
        ),
        "rowProductsVerified": sum(r.get("rowProduct") is True for r in subjects),
        "rowProductsContradicted": sum(r.get("rowProduct") is False for r in subjects),
        # Decisions of the expected rows only (statuses, never values).
        "expectedRowDecisions": [
            subjects[i].get("gradeStatus") for i in cell.get("expectedRows", [])
        ],
        "totalChecks": cell.get("totalChecks"),
        "assignment": [
            {k: v for k, v in item.items() if k != "row"}
            for item in cell.get("assignment", {}).get("subjects", [])
        ],
        "workers": cell.get("workers"),
        "anchorsSeconds": cell.get("anchorsSeconds"),
        "cellsSeconds": cell.get("cellsSeconds"),
    }


def row_outcomes(cell, oracle):
    """Exactness of each expected row's decision and direct reads, accepted or not.

    Outcomes only (EXACT/FALSE/NONE): a row validated while the document fails elsewhere must
    still be exact, otherwise per-row acceptance would hide a false value.
    """

    def outcome(value, truth):
        if value is None or truth is None:
            return "NONE"
        return "EXACT" if same(value, truth) else "FALSE"

    rows, outcomes = cell.get("rows", []), {}
    for item in cell.get("assignment", {}).get("subjects", []):
        row, truth = rows[item["row"]], oracle["subjects"].get(item["subject"], {})
        outcomes[item["subject"]] = {
            "decision": row.get("gradeStatus"),
            "decidedGrade": outcome(row.get("gradeValue"), truth.get("grade")),
            "noteRead": outcome(row["parsed"][0], truth.get("grade")),
            "coefficientRead": outcome(row["parsed"][1], truth.get("coefficient")),
        }
    return outcomes


def evaluate(index, pdf, oracle, reference, page_timeout, out, engine):
    qualification = oracle["qualification"]
    expected = expected_subjects(reference, oracle.get("program", "GL"), qualification)
    diagnostics = []
    started = perf_counter()
    result = analyze(
        pdf,
        expected,
        engine,
        minimum=float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85")),
        diagnostics=diagnostics,
        page_timeout=page_timeout,
        qualification=qualification,
    )
    seconds = round(perf_counter() - started, 3)
    document = f"doc-{index}"
    (out / f"{document}.json").write_text(
        json.dumps({"result": result, "expected": expected, "qualification": qualification})
    )
    accepted = result["status"] == "SUCCEEDED"
    values = {s["subjectId"]: s for s in result["subjects"]}
    fields = {}
    for subject in expected:
        truth = oracle["subjects"].get(subject["code"])
        extracted = values.get(subject["id"])
        for field, key in (("grade", "gradeValue"), ("coefficient", "coefficientValue")):
            name = f"{subject['code']}.{field}"
            if truth is None:
                fields[name] = "NO_ORACLE"
            elif extracted is None:
                fields[name] = "MISSING"
            else:
                fields[name] = "EXACT" if same(extracted[key], truth[field]) else "FALSE"
    cell = next((d for d in diagnostics if d.get("strategy") == "BAC_CELL_GRID"), {})
    expected_rows = row_outcomes(cell, oracle)
    series = cell.get("series")
    if oracle.get("series") is None:
        fields["series"] = "NO_ORACLE"
    elif series is None:
        fields["series"] = "MISSING"
    else:
        fields["series"] = "EXACT" if series == oracle["series"] else "FALSE"
    return {
        "document": document,
        "category": f"real scanned {qualification.replace('_', ' ')} transcript",
        "status": result["status"],
        "failureCode": result["failureCode"],
        "engine": result["engine"],
        "seconds": seconds,
        "workerAccepted": accepted,
        "fields": fields,
        "falseValuesAcceptedByWorker": sum(
            outcome == "FALSE" for name, outcome in fields.items() if name != "series"
        )
        if accepted
        else 0,
        "expectedRows": expected_rows,
        "falseRowDecisions": sum(r["decidedGrade"] == "FALSE" for r in expected_rows.values()),
        "pipeline": structure(diagnostics),
    }


def main():
    # OCR_RAPIDOCR_ENABLED=false measures the Tesseract digit reader in the same image.
    engine = production_engine()
    pymupdf.TOOLS.mupdf_display_errors(False)
    pymupdf.TOOLS.mupdf_display_warnings(False)
    root = Path(os.environ.get("OCR_PRIVATE_FIXTURE_ROOT", "/private"))
    out = Path(os.environ.get("OCR_PRIVATE_OUT", "/private-out"))
    reference = os.environ.get("OCR_REFERENCE_URL", "http://api:8080")
    page_timeout = int(os.environ.get("OCR_PAGE_TIMEOUT_SECONDS", "30"))
    pdfs = sorted(p for p in root.glob("*.pdf") if p.with_suffix(".expected.json").is_file())
    report = []
    for index, pdf in enumerate(pdfs, start=1):
        try:
            oracle = json.loads(pdf.with_suffix(".expected.json").read_text())
            if not isinstance(oracle.get("subjects"), dict) or not oracle.get("qualification"):
                raise ValueError
        except ValueError, AttributeError:
            report.append({"document": f"doc-{index}", "status": "ORACLE_INVALID"})
            continue
        try:
            report.append(evaluate(index, pdf, oracle, reference, page_timeout, out, engine))
        except Exception:  # noqa: BLE001 -- never echo private parser/engine messages
            report.append({"document": f"doc-{index}", "status": "HARNESS_ERROR"})
    print(
        json.dumps(
            {
                "pageTimeoutSeconds": page_timeout,
                "numericReader": engine.cells.name,
                "documents": report,
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
