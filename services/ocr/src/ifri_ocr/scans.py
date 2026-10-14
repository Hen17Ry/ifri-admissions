"""Scan-first path: at most three passes, positional extraction and disagreement checks."""

import os
from dataclasses import dataclass
from decimal import Decimal
from pathlib import Path
from tempfile import TemporaryDirectory
from time import monotonic

import cv2
import numpy as np

from ifri_ocr.preprocessing import MAX_PIXELS, prepare, variant


@dataclass(frozen=True)
class ScanSettings:
    dpi: int = 300
    retry_dpi: int = 350
    max_passes: int = 3

    @classmethod
    def load(cls):
        settings = cls(
            int(os.environ.get("OCR_SCAN_DPI", "300")),
            int(os.environ.get("OCR_SCAN_RETRY_DPI", "350")),
            int(os.environ.get("OCR_SCAN_MAX_PASSES", "3")),
        )
        if (
            not 250 <= settings.dpi <= settings.retry_dpi <= 400
            or not 2 <= settings.max_passes <= 3
        ):
            raise ValueError("Scan limits invalid")
        return settings


def conflicting(passes):
    values = {}
    for subjects, _ in passes:
        for subject in subjects:
            # Do not compare unreadable numerical cells as competing reliable evidence.
            if min(subject["gradeConfidence"], subject["coefficientConfidence"]) < 0.5:
                continue
            pair = (Decimal(subject["gradeValue"]), Decimal(subject["coefficientValue"]))
            previous = values.setdefault(subject["subjectId"], pair)
            if previous != pair:
                return True
    return False


def choose_pass(passes, minimum):
    if conflicting(passes):
        return [], "AMBIGUOUS_ROW"
    viable = [(subjects, issue) for subjects, issue in passes if subjects]
    if not viable:
        issues = [issue for _, issue in passes if issue]
        if issues and all(issue == "SUBJECT_NOT_FOUND" for issue in issues):
            return [], "SUBJECT_NOT_FOUND"
        return [], "AMBIGUOUS_ROW" if "AMBIGUOUS_ROW" in issues else "LOW_CONFIDENCE"
    subjects, issue = max(
        viable, key=lambda pair: (len(pair[0]), min(s["overallConfidence"] for s in pair[0]))
    )
    if not issue and any(s["overallConfidence"] < minimum for s in subjects):
        return [], "LOW_CONFIDENCE"
    return subjects, issue


def safe_diagnostics(tokens, expected):
    from rapidfuzz.fuzz import ratio

    from ifri_ocr.extraction import lines, normalize, number
    from ifri_ocr.layouts import select_section

    _, selected, _ = select_section(tokens)
    header_candidates = {}
    for name, aliases in {
        "subject": ("matiere", "matieres"),
        "grade": ("note", "notes"),
        "coefficient": ("coef", "coeff", "coefficient"),
    }.items():
        possibilities = [
            (max(ratio(normalize(t.text), alias) / 100 for alias in aliases), t) for t in selected
        ]
        if possibilities:
            score, token = max(possibilities, key=lambda item: item[0])
            if score >= 0.6:
                header_candidates[name] = {
                    "matchScore": round(score, 3),
                    "ocrConfidence": round(token.confidence, 3),
                    "box": list(token.box),
                }
    label_scores = {}
    for subject in expected:
        label_scores[subject["id"]] = round(
            max(
                (
                    ratio(
                        normalize(" ".join(t.text for t in row if number(t.text) is None)),
                        normalize(subject["name"]),
                    )
                    / 100
                    for row in lines(selected)
                ),
                default=0,
            ),
            3,
        )
    # Only canonical identifiers, scores and geometry; never raw OCR/document text.
    word_scores = {}
    for subject in expected:
        if selected:
            score, token = max(
                ((ratio(normalize(t.text), normalize(subject["name"])) / 100, t) for t in selected),
                key=lambda pair: pair[0],
            )
            word_scores[subject["id"]] = {
                "matchScore": round(score, 3),
                "ocrConfidence": round(token.confidence, 3),
                "box": list(token.box),
            }
    return {
        "headerCandidates": header_candidates,
        "expectedLabelScores": label_scores,
        "expectedWordCandidates": word_scores,
    }


def extract_scan(
    page,
    expected,
    engine,
    minimum,
    page_timeout,
    page_number,
    diagnostics=None,
    qualification=None,
):
    import pymupdf

    from ifri_ocr.extraction import Tesseract, locate
    from ifri_ocr.layouts import select_section

    if isinstance(engine, Tesseract):
        from ifri_ocr.bac.pipeline import extract_bac_page

        cell_result = extract_bac_page(
            page,
            expected,
            page_timeout,
            page_number,
            diagnostics,
            qualification,
            reader=engine.cells.read,
            prepare=engine.cells.prepare,
        )
        if cell_result is not None:
            return cell_result
    settings = ScanSettings.load()
    # The page budget covers all OCR passes, not each subprocess separately.
    deadline = monotonic() + page_timeout
    passes = []
    plan = [
        (settings.dpi, "clahe", 11),
        (settings.dpi, "adaptive", 6),
        (settings.retry_dpi, "color-channel", 11),
    ][: settings.max_passes]
    with TemporaryDirectory(prefix="ifri-scan-") as directory:
        scans = {}
        region = None
        region_profile = None
        for index, (dpi, preprocessing, psm) in enumerate(plan):
            pass_started = monotonic()
            if index == 2:
                found, issue = choose_pass(passes, minimum)
                if not issue:
                    break
            if page.rect.width * page.rect.height * (dpi / 72) ** 2 > MAX_PIXELS:
                return [], "LOW_CONFIDENCE"
            if dpi not in scans:
                pixmap = page.get_pixmap(dpi=dpi, colorspace=pymupdf.csRGB, alpha=False)
                rgb = np.frombuffer(pixmap.samples, dtype=np.uint8).reshape(
                    pixmap.height, pixmap.width, 3
                )
                scans[dpi] = prepare(rgb)
            scan = scans[dpi]
            if scan.perspective:
                return [], "LOW_CONFIDENCE"
            image = Path(directory) / f"pass-{index}.png"
            prepared_image = variant(scan, preprocessing)
            full_h, full_w = prepared_image.shape
            x0, y0, x1, y1 = 0, 0, full_w, full_h
            if region is not None:
                x0, y0 = int(region[0] * full_w), int(region[1] * full_h)
                x1, y1 = int(region[2] * full_w), int(region[3] * full_h)
                prepared_image = prepared_image[y0:y1, x0:x1]
                psm = 6
            cv2.imwrite(str(image), prepared_image)
            remaining = deadline - monotonic()
            if remaining <= 0:
                return [], "LOW_CONFIDENCE"
            pass_engine = Tesseract(psm) if isinstance(engine, Tesseract) else engine
            tokens = pass_engine.extract(
                image, prepared_image.shape[1], prepared_image.shape[0], max(1, int(remaining))
            )
            profile, section_tokens, _ = select_section(tokens)
            subjects, issue = locate(
                tokens,
                expected,
                page_number,
                minimum,
                profile_hint=region_profile if region is not None else None,
            )
            for subject in subjects:
                evidence = subject["evidence"]
                for field in ("subject", "grade", "coefficient"):
                    bbox = evidence[field]
                    page_box = {
                        "x0": (x0 + bbox["x0"] * (x1 - x0)) / full_w,
                        "y0": (y0 + bbox["y0"] * (y1 - y0)) / full_h,
                        "x1": (x0 + bbox["x1"] * (x1 - x0)) / full_w,
                        "y1": (y0 + bbox["y1"] * (y1 - y0)) / full_h,
                    }
                    evidence[field] = scan.original_box(page_box)
                evidence.update(
                    {
                        "preprocessing": preprocessing,
                        "dpi": dpi,
                        "psm": psm,
                        "deskewDegrees": scan.angle,
                    }
                )
            passes.append((subjects, issue))
            if (
                region is None
                and profile.name == "BENIN_BAC_GENERAL"
                and profile.bounds is not None
                and len(section_tokens) >= 6
            ):
                section_start, section_end = profile.bounds
                region = (
                    max(0, min(t.box[0] for t in section_tokens) - 0.025),
                    max(0, section_start - 0.03),
                    min(1, max(t.box[2] for t in section_tokens) + 0.025),
                    min(1, section_end),
                )
                region_profile = profile
            if diagnostics is not None:
                diagnostics.append(
                    {
                        "page": page_number,
                        "imageOnly": not bool(page.get_text("words")),
                        "profile": region_profile.name if region_profile else profile.name,
                        "section": region_profile.section if region_profile else profile.section,
                        "sectionCrop": x0 != 0 or y0 != 0,
                        "preprocessing": preprocessing,
                        "dpi": dpi,
                        "psm": psm,
                        "deskewDegrees": scan.angle,
                        "subjectsFound": len(subjects),
                        "sectionTokenCount": len(section_tokens),
                        "sectionBounds": profile.bounds,
                        "cropBounds": list(region) if region is not None else None,
                        "failureCode": issue,
                        "seconds": round(monotonic() - pass_started, 3),
                        **safe_diagnostics(tokens, expected),
                    }
                )
    return choose_pass(passes, minimum)
