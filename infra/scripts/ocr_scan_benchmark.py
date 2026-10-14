"""Public synthetic scan/photo benchmark: subjects always come from the real reference API."""

import json
import os
from decimal import Decimal
from pathlib import Path
from time import perf_counter
from urllib.request import urlopen

import cv2
import numpy as np
import pymupdf
from ifri_ocr.extraction import Tesseract, analyze, locate
from ifri_ocr.preprocessing import prepare, variant

OUT = Path(os.environ.get("OCR_FIXTURE_OUTPUT", "services/ocr/tests/fixtures"))
REFERENCE = os.environ.get("OCR_REFERENCE_URL", "http://127.0.0.1:18080")


def synthetic_bac(expected):
    doc = pymupdf.open()
    page = doc.new_page()
    page.draw_rect(page.rect, color=None, fill=(0.73, 0.85, 0.47))
    for x in (140, 320, 490):
        page.draw_rect(
            pymupdf.Rect(x, 0, x + 60, 842), color=None, fill=(0.88, 0.83, 0.40)
        )
    page.insert_text((90, 180), "SYNTHETIQUE", fontsize=46, color=(0.52, 0.67, 0.35))
    page.insert_text(
        (45, 220), "Baccalauréat de l'enseignement secondaire général", fontsize=12
    )
    page.insert_text((45, 260), "Épreuves écrites du premier groupe", fontsize=12)
    for x, text in [
        (45, "Matière"),
        (330, "Note"),
        (380, "Coeff"),
        (450, "Points"),
        (515, "Sur"),
    ]:
        page.insert_text((x, 290), text, fontsize=11)
    # These are the explicitly requested synthetic duplication oracle, never runtime defaults.
    values = {"MATH": ("6", "4"), "FRENCH": ("9", "2"), "ENGLISH": ("11", "2")}
    for index, subject in enumerate(expected):
        grade, coef = values[subject["id"]]
        y = 325 + index * 32
        for x, text in [
            (45, subject["name"]),
            (330, grade),
            (380, coef),
            (450, str(int(grade) * int(coef))),
            (515, str(20 * int(coef))),
        ]:
            page.insert_text((x, y), text, fontsize=11)
    page.insert_text((45, 430), "Total des épreuves écrites", fontsize=11)
    page.insert_text((45, 500), "Épreuves orales du deuxième groupe", fontsize=12)
    for x, text in [(45, "Matière"), (330, "Note"), (380, "Coeff")]:
        page.insert_text((x, 530), text, fontsize=11)
    for y, name, grade in [(565, "Anglais", "17"), (597, "Français", "14")]:
        for x, text in [(45, name), (330, grade), (380, "2")]:
            page.insert_text((x, y), text, fontsize=11)
    for y in [300, 335, 367, 400, 445]:
        page.draw_line((40, y), (555, y), color=(0.2, 0.3, 0.1), width=0.5)
    return doc, values


def main():
    with urlopen(
        REFERENCE
        + "/api/v1/expected-subjects?campaign=DEV_LOCAL&program=GL&qualification=BAC_D",
        timeout=10,
    ) as response:
        reference = json.load(response)
    expected = [
        {"id": s["code"], "name": s["name"], "position": s["position"]}
        for s in reference["expectedSubjects"]
    ]
    OUT.mkdir(parents=True, exist_ok=True)
    doc, oracle = synthetic_bac(expected)
    native = OUT / "BAC-general-sections-text.pdf"
    doc.save(native)
    pixmap = doc[0].get_pixmap(dpi=300, colorspace=pymupdf.csRGB, alpha=False)
    rgb = (
        np.frombuffer(pixmap.samples, np.uint8)
        .reshape(pixmap.height, pixmap.width, 3)
        .copy()
    )
    doc.close()
    files = [("A_NATIVE", native)]
    for name, kind in [
        ("png", "B_SCAN"),
        ("jpeg-photo", "C_PHOTO"),
        ("skew", "C_PHOTO"),
    ]:
        transformed = rgb.copy()
        if name == "jpeg-photo":
            shadow = np.linspace(0.7, 1.0, transformed.shape[1])[None, :, None]
            transformed = np.clip(transformed * shadow, 0, 255).astype(np.uint8)
        if name == "skew":
            h, w = transformed.shape[:2]
            transformed = cv2.warpAffine(
                transformed,
                cv2.getRotationMatrix2D((w / 2, h / 2), 2, 1),
                (w, h),
                borderValue=(255, 255, 255),
            )
        ok, encoded = cv2.imencode(
            ".jpg" if name == "jpeg-photo" else ".png",
            cv2.cvtColor(transformed, cv2.COLOR_RGB2BGR),
        )
        assert ok
        scan = pymupdf.open()
        page = scan.new_page()
        page.insert_image(page.rect, stream=encoded.tobytes())
        path = OUT / f"BAC-general-security-{name}.pdf"
        scan.save(path, deflate=True)
        scan.close()
        files.append((kind, path))
    minimum = float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85"))
    measurements = []
    for category, path in files:
        diagnostics = []
        start = perf_counter()
        result = analyze(
            path, expected, Tesseract(), minimum=minimum, diagnostics=diagnostics
        )
        exact = result["status"] == "SUCCEEDED" and all(
            (Decimal(s["gradeValue"]), Decimal(s["coefficientValue"]))
            == tuple(map(Decimal, oracle[s["subjectId"]]))
            for s in result["subjects"]
        )
        measurements.append(
            {
                "category": category,
                "fixture": path.name,
                "status": result["status"],
                "failureCode": result["failureCode"],
                "seconds": round(perf_counter() - start, 3),
                "exactValues": exact,
                "passes": diagnostics,
            }
        )
    # Local evaluation only: variants/modes are not all executed by the production worker.
    evaluation = []
    scan = prepare(rgb)
    from tempfile import TemporaryDirectory

    with TemporaryDirectory() as directory:
        for preprocessing in [
            "grayscale",
            "clahe",
            "adaptive",
            "otsu",
            "color-channel",
        ]:
            image = Path(directory) / "synthetic.png"
            cv2.imwrite(str(image), variant(scan, preprocessing))
            for psm in (6, 11):
                start = perf_counter()
                tokens = Tesseract(psm).extract(image, rgb.shape[1], rgb.shape[0], 30)
                subjects, issue = locate(tokens, expected, 1, minimum)
                exact = not issue and all(
                    (Decimal(s["gradeValue"]), Decimal(s["coefficientValue"]))
                    == tuple(map(Decimal, oracle[s["subjectId"]]))
                    for s in subjects
                )
                evaluation.append(
                    {
                        "preprocessing": preprocessing,
                        "psm": psm,
                        "exactValues": exact,
                        "failureCode": issue,
                        "seconds": round(perf_counter() - start, 3),
                    }
                )
    resolution_study = []
    with pymupdf.open(native) as source, TemporaryDirectory() as directory:
        for dpi in (200, 300, 350, 400):
            started = perf_counter()
            rendered = source[0].get_pixmap(
                dpi=dpi, colorspace=pymupdf.csRGB, alpha=False
            )
            array = np.frombuffer(rendered.samples, np.uint8).reshape(
                rendered.height, rendered.width, 3
            )
            prepared = prepare(array)
            image = Path(directory) / "synthetic.png"
            cv2.imwrite(str(image), variant(prepared, "clahe"))
            tokens = Tesseract(11).extract(image, rendered.width, rendered.height, 30)
            subjects, issue = locate(tokens, expected, 1, minimum)
            exact = not issue and all(
                (Decimal(s["gradeValue"]), Decimal(s["coefficientValue"]))
                == tuple(map(Decimal, oracle[s["subjectId"]]))
                for s in subjects
            )
            resolution_study.append(
                {
                    "dpi": dpi,
                    "psm": 11,
                    "preprocessing": "clahe",
                    "exactValues": exact,
                    "failureCode": issue,
                    "minimumConfidence": min(
                        (s["overallConfidence"] for s in subjects), default=None
                    ),
                    "seconds": round(perf_counter() - started, 3),
                }
            )
    report = {
        "measurements": measurements,
        "evaluation300Dpi": evaluation,
        "resolutionStudy": resolution_study,
    }
    (OUT / "scan-benchmark.json").write_text(json.dumps(report, indent=2))
    print(json.dumps(report, indent=2))
    if any(not m["exactValues"] for m in measurements):
        raise SystemExit("Synthetic security/sections/photo benchmark failed safely")


if __name__ == "__main__":
    main()
