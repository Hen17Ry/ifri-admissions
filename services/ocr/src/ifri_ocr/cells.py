"""BAC general first-group grid: cell OCR, immutable values, arithmetic acceptance."""

import csv
import io
import os
import subprocess
from decimal import Decimal, localcontext
from fractions import Fraction
from itertools import pairwise
from pathlib import Path
from tempfile import TemporaryDirectory
from time import monotonic

import cv2
import numpy as np
from rapidfuzz.fuzz import ratio

from ifri_ocr.extraction import normalize, number
from ifri_ocr.layouts import BAC_COLUMN_RATIOS, select_section
from ifri_ocr.preprocessing import MAX_PIXELS, prepare

# Closed document vocabulary, independent of admissions/expected-subject rules.
BAC_SUBJECTS = (
    "ANGLAIS",
    "FRANCAIS",
    "MATHEMATIQUES",
    "HISTOIRE ET GEOGRAPHIE",
    "PHILOSOPHIE",
    "PHYSIQUE CHIMIE ET TECHNOLOGIE",
    "SCIENCES DE LA VIE ET DE LA TERRE",
    "ESPAGNOL",
    "ALLEMAND",
)


def binary_v(rgb):
    value = cv2.cvtColor(rgb, cv2.COLOR_RGB2HSV)[:, :, 2]
    # Neutral black strokes remain dark; olive security ink is brighter on V.
    return cv2.threshold(value, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]


def axis_centers(mask, axis, minimum):
    coordinates = np.flatnonzero(np.count_nonzero(mask, axis=axis) >= minimum)
    groups = []
    for coordinate in coordinates:
        if not groups or coordinate - groups[-1][-1] > 3:
            groups.append([int(coordinate)])
        else:
            groups[-1].append(int(coordinate))
    return [int(np.median(group)) for group in groups]


def first_table_bands(bands):
    """Retain header + first table; a large whitespace gap starts another table."""
    if len(bands) < 3:
        return bands, None, None
    gaps = [following[0] - previous[-1] for previous, following in pairwise(bands)]
    median_gap = float(np.median(gaps))
    if median_gap <= 0:
        return bands, None, median_gap
    # Header spacing may be larger than body spacing: never split header from first row.
    for index, gap in enumerate(gaps):
        if index > 0 and gap > median_gap * 2.5:
            return bands[: index + 1], index + 1, median_gap
    return bands, None, median_gap


def grid_cells(binary, value=None, geometry=None):
    """Five columns only. No OCR header coordinates or numerical guesses."""
    h, w = binary.shape
    ink = 255 - binary
    vertical = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (1, max(30, h // 3)))
    )
    horizontal = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (max(40, w // 3), 1))
    )
    xs = axis_centers(vertical, 0, h * 0.35)
    ys = axis_centers(horizontal, 1, w * 0.3)
    # Some grids are faint/broken: join only short gaps before detecting long rulings.
    if len(ys) < 3:
        joined = cv2.dilate(ink, np.ones((9, 1), np.uint8))
        joined = cv2.morphologyEx(joined, cv2.MORPH_CLOSE, np.ones((1, 9), np.uint8))
        horizontal = cv2.morphologyEx(
            joined, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (max(40, w // 3), 1))
        )
        ys = axis_centers(horizontal, 1, w * 0.3)
    contours, _ = cv2.findContours(horizontal, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
    borders = sorted(
        (cv2.boundingRect(c) for c in contours if cv2.boundingRect(c)[2] >= w * 0.5),
        key=lambda rect: rect[1],
    )
    if not borders:
        return [], "COLUMN_SEGMENTATION_FAILED"
    left, _, span, _ = borders[0]
    right = left + span - 1
    desired = [left + int(r * (right - left)) for r in BAC_COLUMN_RATIOS]
    inside = [x for x in xs if left - 5 <= x <= right + 5]
    matched = [min(inside, key=lambda x: abs(x - wanted)) for wanted in desired] if inside else []
    detected = len(set(matched)) == 6 and all(
        abs(found - wanted) <= span * 0.025 for found, wanted in zip(matched, desired, strict=True)
    )
    xs = matched if detected else desired
    widths = np.diff(xs) / float(right - left)
    if not (
        widths[0] > 3 * max(widths[1:]) and 0.075 <= min(widths[1:]) and max(widths[1:]) <= 0.17
    ):
        return [], "COLUMN_SEGMENTATION_FAILED"
    if geometry is not None:
        geometry.update(
            columnEdges=list(xs),
            outerBorder=[left, right],
            columnStrategy="DETECTED" if detected else "PROFILE_RATIOS",
            relativeWidths=widths.tolist(),
        )
    # Alternating row backgrounds are not horizontal borders. Use label-column ink only.
    label_left, label_right = xs[0] + 8, xs[1] - 8
    label = (
        (255 - binary[:, label_left:label_right])
        if value is None
        else cv2.adaptiveThreshold(
            value[:, label_left:label_right],
            255,
            cv2.ADAPTIVE_THRESH_GAUSSIAN_C,
            cv2.THRESH_BINARY_INV,
            41,
            12,
        )
    )
    count, labels, stats, _ = cv2.connectedComponentsWithStats(label)
    filtered = np.zeros_like(label)
    character_heights = []
    for component in range(1, count):
        _, _, cw, ch, area = stats[component]
        if (
            max(5, h * 0.012) <= ch <= h * 0.12
            and 2 <= cw <= (label_right - label_left) * 0.25
            and area >= 8
        ):
            filtered[labels == component] = 255
            character_heights.append(int(ch))
    projection = np.count_nonzero(filtered, axis=1)
    occupied = np.flatnonzero(projection >= max(3, float(projection.max()) * 0.12))
    bands = []
    for y in occupied:
        if not bands or y - bands[-1][-1] > 4:
            bands.append([int(y)])
        else:
            bands[-1].append(int(y))
    minimum_band_height = (
        max(5, np.percentile(character_heights, 75) * 0.5) if character_heights else 5
    )
    bands = [band for band in bands if band[-1] - band[0] + 1 >= minimum_band_height]
    all_bands = bands
    bands, split_index, median_gap = first_table_bands(bands)
    if geometry is not None:
        geometry["allRowBands"] = [(band[0], band[-1]) for band in all_bands]
        geometry["tableSplitAfterBand"] = split_index
        geometry["medianInterBandGap"] = median_gap
        geometry["rowBands"] = [(band[0], band[-1]) for band in bands]
        geometry["dataRowCount"] = max(0, len(bands) - 1)
    # Exclude the actual header band before row segmentation; no OCR dependence.
    top_line = borders[0][1]
    header_lines = [line for line in ys if len(bands) > 1 and bands[0][-1] < line < bands[1][0]]
    header_bottom = (
        min(header_lines)
        if header_lines
        else ((bands[0][-1] + bands[1][0]) // 2 if len(bands) > 1 else None)
    )
    if header_bottom is not None:
        bands = [band for band in bands if band[0] > header_bottom + 3]
        if not 5 <= len(bands) <= 12:
            return [], "ROW_SEGMENTATION_FAILED"
        if geometry is not None:
            geometry["headerExcludedThrough"] = header_bottom
            geometry["dataRowCount"] = len(bands)
        row_edges = (
            [header_bottom]
            + [(a[-1] + b[0]) // 2 for a, b in pairwise(bands)]
            + [min(h - 1, bands[-1][-1] + 6)]
        )
        ys = [max(0, top_line)] + row_edges
        padding = max(4, int(np.median([band[-1] - band[0] + 1 for band in bands]) * 0.35))
        cells = [
            [
                (xs[col] + 5, max(0, top_line) + 4, xs[col + 1] - 5, header_bottom - 4)
                for col in range(5)
            ]
        ]
        cells += [
            [
                (
                    xs[col] + 5,
                    max(0, band[0] - padding),
                    xs[col + 1] - 5,
                    min(h - 1, band[-1] + padding),
                )
                for col in range(5)
            ]
            for band in bands
        ]
        return cells, ""
    # Without a visible header separator, first label band is the profile header.
    if not 5 <= len(bands) - 1 <= 12:
        return [], "ROW_SEGMENTATION_FAILED"
    ys = (
        [max(0, bands[0][0] - 5)]
        + [(a[-1] + b[0]) // 2 for a, b in pairwise(bands)]
        + [min(h - 1, bands[-1][-1] + 6)]
    )
    padding = max(4, int(np.median([band[-1] - band[0] + 1 for band in bands]) * 0.35))
    cells = [
        [
            (
                xs[col] + 5,
                max(0, band[0] - padding),
                xs[col + 1] - 5,
                min(h - 1, band[-1] + padding),
            )
            for col in range(5)
        ]
        for band in bands
    ]
    return cells, ""


def read_cell(image, numeric, path, timeout, whitelist=None):
    started = monotonic()
    # x3 and a white margin are applied to each cell, not the entire page.
    image = cv2.resize(image, None, fx=3, fy=3, interpolation=cv2.INTER_CUBIC)
    image = cv2.copyMakeBorder(image, 15, 15, 15, 15, cv2.BORDER_CONSTANT, value=255)
    cv2.imwrite(str(path), image)
    command = ["tesseract", str(path), "stdout", "-l", "fra+eng", "--psm", "7"]
    if numeric or whitelist:
        command += ["-c", "tessedit_char_whitelist=" + ("0123456789,." if numeric else whitelist)]
    try:
        result = subprocess.run(command + ["tsv"], capture_output=True, check=True, timeout=timeout)
    except subprocess.TimeoutExpired, subprocess.CalledProcessError:
        return {"raw": "", "confidence": 0, "error": "CELL_UNREADABLE"}
    words = [
        r
        for r in csv.DictReader(io.StringIO(result.stdout.decode()), delimiter="\t")
        if r["level"] == "5" and r["text"].strip()
    ]
    word_confidences = [float(r["conf"]) / 100 for r in words if float(r["conf"]) >= 0]
    return {
        "raw": " ".join(r["text"] for r in words).strip(),
        "confidence": sum(word_confidences) / len(word_confidences) if word_confidences else 0,
        "wordConfidences": word_confidences,
        "confidenceSource": "SAME_TSV_LEVEL_5",
        "ignoredNegativeConfidenceWords": sum(float(r["conf"]) < 0 for r in words),
        "seconds": round(monotonic() - started, 4),
    }


def parse_row(cells):
    raw = [c["raw"] for c in cells]
    parsed = [number(value) for value in raw[1:]]
    grade, coef, points, scale = parsed
    checks = {
        "gradeRange": grade is not None and 0 <= grade <= 20,
        "positiveCoefficient": coef is not None
        and coef == coef.to_integral_value()
        and 1 <= coef <= 6,
        "pointsEqualGradeTimesCoefficient": False,
        "scaleEqual20TimesCoefficient": False,
    }
    with localcontext() as context:
        context.prec = 200
        if grade is not None and coef is not None:
            checks["pointsEqualGradeTimesCoefficient"] = (
                None if points is None else points == grade * coef
            )
        checks["scaleEqual20TimesCoefficient"] = (
            coef is not None and scale is not None and scale == 20 * coef
        )
    text = normalize(raw[0]).replace("mathemati0ues", "mathematiques").replace("anglals", "anglais")
    matches = sorted(
        ((ratio(text, normalize(name)) / 100, name) for name in BAC_SUBJECTS), reverse=True
    )
    match, name = matches[0]
    label_valid = match >= 0.8 and (match - matches[1][0] >= 0.08)
    checks["closedVocabularyMatch"] = label_valid
    return {
        "subject": name if label_valid else None,
        "rawCells": raw,
        "parsed": [None if v is None else str(v) for v in parsed],
        "checks": checks,
        "ocrConfidences": [c["confidence"] for c in cells],
        "labelMatchScore": match,
        "coefficientStatus": "VALIDATED"
        if checks["positiveCoefficient"] and checks["scaleEqual20TimesCoefficient"]
        else "NEEDS_REVIEW",
        "gradeStatus": "VALIDATED_BY_ROW"
        if checks["gradeRange"] and checks["pointsEqualGradeTimesCoefficient"] is True
        else "NEEDS_REVIEW",
        "state": "ACCEPTED"
        if (
            checks["gradeRange"]
            and checks["positiveCoefficient"]
            and checks["closedVocabularyMatch"]
            and checks["scaleEqual20TimesCoefficient"]
            and checks["pointsEqualGradeTimesCoefficient"] is True
        )
        else "NEEDS_REVIEW",
    }


def check_totals(rows, total):
    grade, coef, points, scale = [number(raw) for raw in total["rawCells"][1:]]
    checks = {
        "coefficientSum": False,
        "pointsSum": False,
        "scaleSum": False,
        "displayedTotalNote": None,
    }
    if rows and all(row["coefficientStatus"] == "VALIDATED" for row in rows):
        with localcontext() as context:
            context.prec = 200
            checks["coefficientSum"] = coef is not None and coef == sum(
                Decimal(r["parsed"][1]) for r in rows
            )
            checks["scaleSum"] = scale is not None and scale == sum(
                Decimal(r["parsed"][3]) for r in rows
            )
            checks["pointsSum"] = (
                points is not None
                and all(r["parsed"][2] is not None for r in rows)
                and points == sum(Decimal(r["parsed"][2]) for r in rows)
            )
            checks["displayedTotalNote"] = (
                None
                if grade is None or coef is None or points is None
                else abs(grade * coef - points) <= coef * Decimal(".01")
            )
    return checks


def validate_document_rows(rows, total):
    """Only complete, matching document sums authorize replacing a direct note."""
    checks = check_totals(rows, total)
    valid = all(checks.get(name) is True for name in ("coefficientSum", "pointsSum", "scaleSum"))
    for row in rows:
        row["directGradeValue"] = row["parsed"][0]
        row["gradeSource"] = "DIRECT_CELL"
        if valid:
            with localcontext() as context:
                context.prec = 200
                coef, points = Decimal(row["parsed"][1]), Decimal(row["parsed"][2])
                derived = points / coef
                denominator = Fraction(points / 1) / Fraction(coef)
                remainder = denominator.denominator
                for factor in (2, 5):
                    while remainder % factor == 0:
                        remainder //= factor
                exact = (
                    remainder == 1
                    and derived.as_tuple().exponent >= -32
                    and derived * coef == points
                    and 0 <= derived <= 20
                )
            if exact and row["checks"]["closedVocabularyMatch"]:
                row["parsed"][0] = format(derived, "f")
                row["gradeSource"] = "POINTS_TOTALS"
                row["gradeDivergence"] = (
                    row["directGradeValue"] is None or Decimal(row["directGradeValue"]) != derived
                )
                row["gradeStatus"] = "VALIDATED_BY_TOTALS"
                row["state"] = "ACCEPTED"
                continue
        row["gradeStatus"] = "NEEDS_REVIEW"
        row["state"] = "NEEDS_REVIEW"
    return checks, valid


def remove_cell_rules(binary):
    h, w = binary.shape
    ink = 255 - binary
    horizontal = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (max(20, int(w * 0.6)), 1))
    )
    vertical = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (1, max(20, int(h * 0.9))))
    )
    return cv2.inpaint(binary, cv2.bitwise_or(horizontal, vertical), 2, cv2.INPAINT_TELEA)


def extract_bac_cells(page, expected, engine, timeout, page_number, diagnostics=None):
    """Returns None when no BAC written anchor; never substitutes oral values."""
    import pymupdf

    deadline = monotonic() + timeout
    dpi = 300
    if page.rect.width * page.rect.height * (dpi / 72) ** 2 > MAX_PIXELS:
        return [], "NEEDS_REVIEW"
    pixmap = page.get_pixmap(dpi=dpi, colorspace=pymupdf.csRGB, alpha=False)
    scan = prepare(np.frombuffer(pixmap.samples, np.uint8).reshape(pixmap.height, pixmap.width, 3))
    if scan.perspective:
        return [], "NEEDS_REVIEW"
    binary = binary_v(scan.image)
    with TemporaryDirectory(prefix="ifri-cells-") as directory:
        path = Path(directory) / "cell.png"
        cv2.imwrite(str(path), binary)
        anchors = engine.extract(
            path, binary.shape[1], binary.shape[0], max(1, int(deadline - monotonic()))
        )
        profile, _, issue = select_section(anchors)
        if profile.name != "BENIN_BAC_GENERAL":
            return None
        if not issue and profile.bounds is None:
            return [], "SUBJECT_NOT_FOUND"  # Oral-only continuation contributes no written rows.
        if issue or profile.bounds is None:
            return [], "NEEDS_REVIEW"
        h, w = binary.shape
        start, end = profile.bounds
        # Include the Total row for checks and the header top ruling for grid reconstruction.
        margin = np.median([t.box[3] - t.box[1] for t in anchors]) if anchors else 0.01
        y0 = max(0, int((start - margin * 0.5) * h))
        y1 = min(h, int((end + margin * 3) * h))
        region = binary[y0:y1]
        debug_root = os.environ.get("OCR_PRIVATE_DEBUG_DIR") if diagnostics is not None else None
        debug = Path(debug_root) if debug_root else None
        if debug:
            debug.mkdir(parents=True, exist_ok=True)
            cv2.imwrite(str(debug / f"page-{page_number}-section.png"), region)
        value = scan.image.max(axis=2)[y0:y1]
        geometry = {}
        cells, issue = grid_cells(region, value, geometry)
        if debug:
            overlay = cv2.cvtColor(region, cv2.COLOR_GRAY2BGR)
            for x in geometry.get("columnEdges", []):
                cv2.line(overlay, (x, 0), (x, region.shape[0] - 1), (255, 0, 0), 2)
            for lo, hi in geometry.get("rowBands", []):
                cv2.line(overlay, (0, lo), (region.shape[1] - 1, lo), (0, 255, 0), 1)
                cv2.line(overlay, (0, hi), (region.shape[1] - 1, hi), (0, 255, 0), 1)
            for row_cells in cells:
                for x0, ry0, x1, ry1 in row_cells:
                    cv2.rectangle(overlay, (x0, ry0), (x1, ry1), (0, 0, 255), 2)
            cv2.imwrite(str(debug / f"page-{page_number}-grid.png"), overlay)
        if issue:
            if diagnostics is not None:
                diagnostics.append(
                    {
                        "strategy": "BAC_CELL_GRID",
                        "page": page_number,
                        "failureCode": issue,
                        "sectionBounds": profile.bounds,
                        "segmentation": geometry,
                    }
                )
            return [], issue
        rows = []
        for row_index, row_cells in enumerate(cells):
            if row_index == 0:  # Fixed five-column profile: header is never a subject.
                continue
            cell_results = []
            for col, (x0, ry0, x1, ry1) in enumerate(row_cells):
                remaining = deadline - monotonic()
                cell_value = value[ry0:ry1, x0:x1]
                crop = cv2.threshold(cell_value, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
                effective_preprocessing = "HSV_V_CELL_OTSU"
                if col == 3 and float(np.mean(crop < 128)) > 0.45:
                    red = scan.image[y0 + ry0 : y0 + ry1, x0:x1, 0]
                    red = cv2.createCLAHE(clipLimit=2, tileGridSize=(4, 4)).apply(red)
                    crop = cv2.threshold(red, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
                    effective_preprocessing = "RED_CELL_CLAHE_OTSU"
                crop = remove_cell_rules(crop)
                effective_preprocessing += "_RULES_REMOVED"
                metrics = {
                    "widthPixels": int(crop.shape[1]),
                    "heightPixels": int(crop.shape[0]),
                    "blackPixelRatio": round(float(np.mean(crop < 128)), 4),
                    "polarity": "BLACK_ON_WHITE",
                    "preprocessing": effective_preprocessing,
                }
                if debug:
                    cv2.imwrite(
                        str(debug / f"page-{page_number}-row-{row_index}-col-{col}.png"), crop
                    )
                if remaining <= 0:
                    cell_results.append(
                        {"raw": "", "confidence": 0, "error": "PAGE_BUDGET_EXHAUSTED", **metrics}
                    )
                    continue
                if min(crop.shape) < 5 or not 0.001 <= metrics["blackPixelRatio"] <= 0.8:
                    cell_results.append(
                        {"raw": "", "confidence": 0, "error": "EMPTY_OR_INVALID_CROP", **metrics}
                    )
                    continue
                cell_results.append(
                    read_cell(
                        crop,
                        col > 0,
                        path,
                        max(1, int(remaining)),
                    )
                )
                cell_results[-1].update(metrics)
                if debug and path.is_file():
                    actual_input = cv2.imread(str(path), cv2.IMREAD_GRAYSCALE)
                    if actual_input is not None:
                        cv2.imwrite(
                            str(
                                debug
                                / f"page-{page_number}-row-{row_index}-col-{col}-tesseract.png"
                            ),
                            actual_input,
                        )
            row = parse_row(cell_results)
            row["cropDiagnostics"] = [
                {k: v for k, v in cell.items() if k not in {"raw", "confidence"}}
                for cell in cell_results
            ]
            row["cellBoxes"] = row_cells
            rows.append(row)
            label = normalize(row["rawCells"][0])
            if label.startswith("total") or ratio(label.split()[0] if label else "", "total") >= 80:
                break
        totals = [
            r
            for r in rows
            if normalize(r["rawCells"][0]).startswith("total")
            or ratio(
                normalize(r["rawCells"][0]).split()[0] if normalize(r["rawCells"][0]) else "",
                "total",
            )
            >= 80
        ]
        subjects = [r for r in rows if r not in totals]
        total_checks, total_valid = (
            validate_document_rows(subjects, totals[0]) if len(totals) == 1 else ({}, False)
        )
        series_tokens = sorted(
            [t for t in anchors if t.box[3] < start and t.box[0] < 0.5],
            key=lambda t: (t.box[1], t.box[0]),
        )
        series = None
        for token in series_tokens:
            if normalize(token.text) == "serie":
                right = min(0.5, token.box[2] + 0.10)
                sx0, sy0 = int(token.box[2] * w), max(0, int((token.box[1] - 0.004) * h))
                sx1, sy1 = int(right * w), min(h, int((token.box[3] + 0.004) * h))
                series_crop = binary[sy0:sy1, sx0:sx1]
                if series_crop.size and deadline - monotonic() > 0:
                    isolated = read_cell(
                        series_crop,
                        False,
                        path,
                        max(1, int(deadline - monotonic())),
                        whitelist="ABCDE",
                    )
                    candidate = normalize(isolated["raw"]).upper()
                    if candidate in {"A", "B", "C", "D", "E"}:
                        series = candidate
        if diagnostics is not None:
            diagnostics.append(
                {
                    "strategy": "BAC_CELL_GRID",
                    "profile": profile.name,
                    "section": profile.section,
                    "dpi": dpi,
                    "psm": 7,
                    "preprocessing": "CELL_OTSU_HSV_V_WITH_POINTS_RED_FALLBACK",
                    "preprocessingScope": "CELL",
                    "segmentation": geometry,
                    "series": series,
                    "rows": [{k: v for k, v in row.items() if k != "cellBoxes"} for row in rows],
                    "totalChecks": total_checks,
                    "seconds": round(timeout - (deadline - monotonic()), 3),
                }
            )
        if not total_valid or not series or any(r["state"] != "ACCEPTED" for r in subjects):
            return [], "NEEDS_REVIEW"
        evidence_rows = [
            {
                "grade": r["parsed"][0],
                "coefficient": r["parsed"][1],
                "points": r["parsed"][2] or "",
                "scale": r["parsed"][3],
            }
            for r in subjects
        ]
        result = []
        for target in expected:
            matched = [
                (i, r)
                for i, r in enumerate(subjects)
                if ratio(normalize(target["name"]), normalize(r["subject"])) >= 85
            ]
            if len(matched) != 1:
                return [], "NEEDS_REVIEW"
            row_index, row = matched[0]
            boxes = []
            for x0, ry0, x1, ry1 in row["cellBoxes"]:
                boxes.append(
                    scan.original_box(
                        {"x0": x0 / w, "y0": (y0 + ry0) / h, "x1": x1 / w, "y1": (y0 + ry1) / h}
                    )
                )
            confidences = row["ocrConfidences"]
            result.append(
                {
                    "subjectId": target["id"],
                    "position": target["position"],
                    "detectedSubjectText": row["rawCells"][0],
                    "gradeRaw": row["rawCells"][1],
                    "gradeValue": row["parsed"][0],
                    "coefficientRaw": row["rawCells"][2],
                    "coefficientValue": row["parsed"][1],
                    "pageNumber": page_number,
                    "subjectConfidence": confidences[0],
                    "gradeConfidence": confidences[1],
                    "coefficientConfidence": confidences[2],
                    "overallConfidence": min(confidences[:3]),
                    "evidence": {
                        "system": "NORMALIZED_TOP_LEFT",
                        "subject": boxes[0],
                        "grade": boxes[1],
                        "coefficient": boxes[2],
                        "profile": profile.name,
                        "section": profile.section,
                        "preprocessing": "hsv-v",
                        "dpi": dpi,
                        "psm": 7,
                        "deskewDegrees": scan.angle,
                        "validation": "CELL_ARITHMETIC",
                        "gradeSource": row.get("gradeSource", "DIRECT_CELL"),
                        "points": boxes[3],
                        "scale": boxes[4],
                        "arithmeticRows": evidence_rows,
                        "arithmeticRowIndex": row_index,
                        "totalCells": [
                            raw if number(raw) is not None else ""
                            for raw in totals[0]["rawCells"][1:]
                        ],
                        "series": series,
                    },
                }
            )
        return result, ""
