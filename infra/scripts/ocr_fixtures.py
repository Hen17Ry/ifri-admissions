"""Regenerate the committed synthetic PDFs deterministically, verify them, then benchmark.

Default mode "verify" writes into a temporary directory and fails if any regenerated file differs
byte for byte from the committed fixture: a test run never rewrites tracked files. Mode "write"
(make fixtures) deliberately refreshes the committed copies.
"""

import json
import os
import platform
import tempfile
from decimal import Decimal
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

import pymupdf
import rapidfuzz
from ifri_ocr.extraction import Tesseract, analyze

HERE = Path(__file__).resolve()
COMMITTED = Path(
    os.environ.get(
        "OCR_COMMITTED_FIXTURES",
        "/app/tests/fixtures"
        if Path("/app/tests/fixtures").is_dir()
        else str(HERE.parents[2] / "services/ocr/tests/fixtures"),
    )
)
MODE = os.environ.get("OCR_FIXTURE_MODE", "verify")
OUT = (
    COMMITTED
    if MODE == "write"
    else Path(os.environ.get("OCR_FIXTURE_OUTPUT", tempfile.mkdtemp(prefix="ifri-fixtures-")))
)
REFERENCE = os.environ.get("OCR_REFERENCE_URL", "http://localhost:18080")
# Fixed metadata and trailer ID make the bytes a pure function of the drawn content.
METADATA = {
    "producer": "ifri-synthetic",
    "creator": "ifri-synthetic",
    "creationDate": "D:20260101000000Z",
    "modDate": "D:20260101000000Z",
    "title": "",
    "author": "",
    "subject": "",
    "keywords": "",
}


def document(subjects, *, missing=False, coefficient=False, ambiguous=False, accents=True):
    doc = pymupdf.open()
    page = doc.new_page(width=595, height=842)
    for x, text in [(40, "Matière"), (350, "Note"), (460, "Coef")]:
        page.insert_text((x, 100), text, fontsize=12)
    for i, subject in enumerate(subjects):
        if missing and i == 2:
            continue
        name = subject["name"]
        if not accents:
            import unicodedata

            name = "".join(
                c for c in unicodedata.normalize("NFKD", name) if not unicodedata.combining(c)
            )
        y = 135 + i * 40
        page.insert_text((40, y), name, fontsize=10)
        # Synthetic extracted values belong to the PDF fixture, never to admission rules.
        page.insert_text((350, y), ["15,50", "13.75", "14"][i], fontsize=12)
        if ambiguous and i == 0:
            page.insert_text((405, y), "12", fontsize=12)
        if not (coefficient and i == 0):
            page.insert_text((460, y), ["2,5", "2", "3"][i], fontsize=12)
    return doc


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    measurements = []
    drift = []
    for program, qualification in [
        ("GL", "BAC_C"),
        ("GL", "BAC_E"),
        ("GL", "DT_DWM"),
        ("SI", "DT_IMI"),
        ("IM", "DT_PM"),
        ("SEIOT", "DT_EAP"),
    ]:
        with urlopen(
            f"{REFERENCE}/api/v1/expected-subjects?campaign=DEV_LOCAL"
            f"&program={program}&qualification={qualification}",
            timeout=5,
        ) as response:
            subjects = json.load(response)["expectedSubjects"]
        # Worker IDs come from job snapshot. Fixtures require only canonical names/positions;
        # temporary test IDs are not parallel eligibility logic.
        expected = [{**s, "id": s["code"]} for s in subjects]
        variants = (
            ("text", "scan")
            if (program, qualification) != ("GL", "DT_DWM")
            else (
                "text",
                "scan",
                "missing",
                "coefficient",
                "ambiguous",
                "no-accents",
                "degraded",
                "skew",
                "wrong-subjects",
                "alternate-layout",
            )
        )
        for variant in variants:
            fixture_subjects = (
                [{**subject, "name": "Discipline inconnue"} for subject in subjects]
                if variant == "wrong-subjects"
                else subjects
            )
            doc = document(
                fixture_subjects,
                missing=variant == "missing",
                coefficient=variant == "coefficient",
                ambiguous=variant == "ambiguous",
                accents=variant != "no-accents",
            )
            if variant in {"scan", "degraded", "skew"}:
                image = (
                    doc[0].get_pixmap(matrix=pymupdf.Matrix(200 / 72, 200 / 72).prerotate(2))
                    if variant == "skew"
                    else doc[0].get_pixmap(dpi=20 if variant == "degraded" else 200)
                )
                scan = pymupdf.open()
                page = scan.new_page()
                page.insert_image(page.rect, stream=image.tobytes("png"))
                doc.close()
                doc = scan
            if variant == "alternate-layout":
                # A different page format/column placement keeps coordinates meaningful.
                shifted = pymupdf.open()
                shifted_page = shifted.new_page(width=842, height=595)
                shifted_page.show_pdf_page(pymupdf.Rect(80, 40, 650, 570), doc, 0)
                doc.close()
                doc = shifted
            filename = OUT / f"{program}-{qualification}-{variant}.pdf"
            doc.set_metadata(METADATA)
            doc.save(filename, deflate=True, no_new_id=True, garbage=3)
            doc.close()
            committed = COMMITTED / filename.name
            if MODE != "write" and (
                not committed.is_file() or committed.read_bytes() != filename.read_bytes()
            ):
                drift.append(filename.name)
            start = perf_counter()
            result = analyze(
                filename,
                expected,
                Tesseract(),
                minimum=float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85")),
            )
            exact = (
                result["status"] == "SUCCEEDED"
                and [Decimal(item["gradeValue"]) for item in result["subjects"]]
                == [Decimal("15.50"), Decimal("13.75"), Decimal(14)]
                and [Decimal(item["coefficientValue"]) for item in result["subjects"]]
                == [Decimal("2.5"), Decimal(2), Decimal(3)]
                and [item["subjectId"] for item in result["subjects"]]
                == [item["id"] for item in expected]
            )
            measurements.append(
                {
                    "fixture": filename.name,
                    "status": result["status"],
                    "failureCode": result["failureCode"],
                    "seconds": round(perf_counter() - start, 3),
                    "exactValues": exact,
                }
            )
    runtime = {
        "python": platform.python_version(),
        "pymupdf": pymupdf.VersionBind,
        "rapidfuzz": rapidfuzz.__version__,
        "tesseract": Tesseract().version,
    }
    print(json.dumps(runtime))
    for m in measurements:
        print(
            f"{m['fixture']}: {m['status']} {m['failureCode']} {m['seconds']}s "
            f"exactValues={m['exactValues']}"
        )
    if drift:
        raise AssertionError(
            "Committed fixtures differ from the deterministic generator "
            f"(run `make fixtures` deliberately): {', '.join(sorted(drift))}"
        )
    print(f"Fixture bytes: {'written' if MODE == 'write' else 'identical to committed copies'}")
    for measurement in measurements:
        name = measurement["fixture"]
        if name.endswith(
            ("-text.pdf", "-scan.pdf", "-no-accents.pdf", "-alternate-layout.pdf")
        ) and (measurement["status"] != "SUCCEEDED" or not measurement["exactValues"]):
            raise AssertionError(
                f"Synthetic positive fixture failed: {name}: {measurement['failureCode']}"
            )
        if (
            name.endswith(
                (
                    "-missing.pdf",
                    "-coefficient.pdf",
                    "-ambiguous.pdf",
                    "-wrong-subjects.pdf",
                )
            )
            and measurement["status"] == "SUCCEEDED"
        ):
            raise AssertionError(f"Unsafe acceptance of negative fixture: {name}")


if __name__ == "__main__":
    main()
