"""Generate synthetic PDFs using subjects read from the real reference API; benchmark locally."""

import json
import os
import platform
from decimal import Decimal
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

import pymupdf
import rapidfuzz
from ifri_ocr.extraction import Tesseract, analyze

ROOT = (
    Path(__file__).resolve().parents[2]
    if len(Path(__file__).resolve().parents) > 2
    else Path("/app")
)
OUT = Path(
    os.environ.get("OCR_FIXTURE_OUTPUT", str(ROOT / "services/ocr/tests/fixtures"))
)
REFERENCE = os.environ.get("OCR_REFERENCE_URL", "http://localhost:18080")


def document(
    subjects, *, missing=False, coefficient=False, ambiguous=False, accents=True
):
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
                c
                for c in unicodedata.normalize("NFKD", name)
                if not unicodedata.combining(c)
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
    for program, qualification in [
        ("GL", "BAC_C"),
        ("GL", "BAC_E"),
        ("GL", "DT_DWM"),
        ("SI", "DT_IMI"),
        ("IM", "DT_PM"),
        ("SEIOT", "DT_EAP"),
    ]:
        with urlopen(
            f"{REFERENCE}/api/v1/expected-subjects?campaign=DEV_LOCAL&program={program}&qualification={qualification}",
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
                    doc[0].get_pixmap(
                        matrix=pymupdf.Matrix(200 / 72, 200 / 72).prerotate(2)
                    )
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
            doc.save(filename, deflate=True)
            doc.close()
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
    (OUT / "benchmark.json").write_text(
        json.dumps({"runtime": runtime, "measurements": measurements}, indent=2)
    )
    for temporary in OUT.glob("page-*.png"):
        temporary.unlink()
    print(json.dumps(runtime))
    for measurement in measurements:
        print(
            f"{measurement['fixture']}: {measurement['status']} {measurement['failureCode']} {measurement['seconds']}s exactValues={measurement['exactValues']}"
        )
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
