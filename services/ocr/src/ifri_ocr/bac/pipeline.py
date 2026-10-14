"""BAC general page: anchors -> grid -> parallel cell reads -> attribution -> arithmetic.

Numbers are read on every row because the totals need them. Labels only locate the expected
subjects, and only expected subjects can yield accepted values (arithmetic.evaluate). The printed
series, a large isolated letter at the top left, is compared with the declared qualification.
"""

import os
from pathlib import Path
from tempfile import TemporaryDirectory
from threading import Thread
from time import monotonic

import cv2
import numpy as np
from rapidfuzz.fuzz import ratio

from ifri_ocr.bac.arithmetic import (
    ADDITIVE,
    RECONSTRUCTED,
    coefficient_validated,
    evaluate,
    parse_numbers,
)
from ifri_ocr.bac.assignment import (
    TOTAL_SCORE,
    assign,
    clean,
    total_score,
    vocabulary_word,
    word_scores,
)
from ifri_ocr.bac.geometry import binary_v, grid_cells
from ifri_ocr.bac.reading import SERIES_WHITELIST as SERIES_LETTERS
from ifri_ocr.bac.reading import (
    CellJob,
    cell_workers,
    label_variants,
    numeric_variant,
    read_cells,
    tesseract_line,
    text_line,
    trim_to_ink,
)
from ifri_ocr.extraction import Tesseract
from ifri_ocr.layouts import select_section
from ifri_ocr.preprocessing import MAX_PIXELS, prepare

DPI = 300
LABEL_VARIANTS = ("LABEL_V_OTSU", "LABEL_V_ADAPTIVE", "LABEL_V_FLAT_OTSU")
# A row whose best label score stays below this is not recognisable as a subject at all.
HEADER_LABEL_SCORE = 0.6
ANCHOR_LANGUAGES = "fra"
# Column titles of the label column; "Matières" otherwise scores ~0.67 against MATHEMATIQUES.
HEADER_WORDS = ("matieres", "matiere", "disciplines", "discipline")
# Series zone, in table widths W from the table's left border and from the bottom of the written
# section title: x in [left - 0.12 W, left + 0.30 W], y in [title - 0.45 W, title - 0.03 W].
SERIES_ZONE = (-0.12, 0.30, 0.45, 0.03)
# Letter-shaped component: height in median text heights, width/height, ink/box ratios.
SERIES_HEIGHT = (2.0, 8.0)
SERIES_ASPECT = (0.45, 1.3)
SERIES_FILL = (0.15, 0.85)
SERIES_READS = ("SERIES_OTSU", "SERIES_ADAPTIVE", "SERIES_FREE")


def declared_series(qualification):
    """BAC_C -> 'C'. Other diplomas have no BAC general series."""
    if qualification and len(qualification) == 5 and qualification.startswith("BAC_"):
        return qualification[-1]
    return None


def series_mismatch(series, qualification):
    if not series or qualification is None:
        return False
    return series != declared_series(qualification)


def series_zone(border, title_bottom, shape):
    """Fixed top-left zone under the institutional header, relative to the written table."""
    left, right = border
    width = right - left
    h, w = shape
    x_left, x_right, y_top, y_bottom = SERIES_ZONE
    return (
        max(0, int(left + x_left * width)),
        max(0, int(title_bottom - y_top * width)),
        min(w, int(left + x_right * width)),
        max(0, int(title_bottom - y_bottom * width)),
    )


def series_candidates(gray_zone, text_height):
    """Large, isolated, letter-shaped ink components of the zone (geometry only, no OCR)."""
    if gray_zone.size == 0 or min(gray_zone.shape) < 16:
        return []
    ink = cv2.adaptiveThreshold(
        gray_zone, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY_INV, 51, 15
    )
    count, _, stats, _ = cv2.connectedComponentsWithStats(ink, connectivity=8)
    zh, zw = ink.shape
    boxes = [tuple(int(v) for v in stats[i]) for i in range(1, count) if stats[i][4] >= 8]
    found = []
    for index, (x, y, cw, ch, area) in enumerate(boxes):
        if not (
            SERIES_HEIGHT[0] * text_height <= ch <= SERIES_HEIGHT[1] * text_height
            and SERIES_ASPECT[0] <= cw / ch <= SERIES_ASPECT[1]
            and SERIES_FILL[0] <= area / (cw * ch) <= SERIES_FILL[1]
        ):
            continue
        if x == 0 or y == 0 or x + cw >= zw or y + ch >= zh:
            continue  # Cut by the zone: not a whole letter.
        # Isolated: no comparable glyph beside it (a word or a title is not a series letter).
        # Page edges, frames and rules (sparse or taller than any letter) are not glyphs.
        crowded = any(
            other != index
            and 0.4 * ch <= oh <= SERIES_HEIGHT[1] * text_height
            and oarea >= 0.1 * ow * oh
            and oy < y + ch
            and y < oy + oh
            and max(ox - (x + cw), x - (ox + ow)) < 0.6 * ch
            for other, (ox, oy, ow, oh, oarea) in enumerate(boxes)
        )
        if not crowded:
            found.append((x, y, cw, ch))
    return found


def _series_jobs(gray, zone, text_height):
    """One candidate -> two A-G whitelisted reads and one free read; else nothing is read."""
    x0, y0, x1, y1 = zone
    candidates = series_candidates(gray[y0:y1, x0:x1], text_height)
    probe = {"count": len(candidates)}
    if len(candidates) != 1:
        return [], "NOT_FOUND" if not candidates else "AMBIGUOUS", probe
    x, y, cw, ch = candidates[0]
    h, w = gray.shape
    probe.update(
        box=[round((x0 + x) / w, 4), round((y0 + y) / h, 4)]
        + [round((x0 + x + cw) / w, 4), round((y0 + y + ch) / h, 4)],
        heightOverText=round(float(ch / text_height), 2),
    )
    pad = max(6, int(ch * 0.2))
    crop = gray[max(0, y0 + y - pad) : y0 + y + ch + pad, max(0, x0 + x - pad) : x0 + x + cw + pad]
    otsu = cv2.threshold(crop, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    adaptive = cv2.adaptiveThreshold(
        crop, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY, 51, 15
    )
    images = {
        "SERIES_OTSU": (trim_to_ink(otsu, 10), "series"),
        "SERIES_ADAPTIVE": (trim_to_ink(adaptive, 10), "series"),
        "SERIES_FREE": (trim_to_ink(otsu, 10), "series_free"),
    }
    return (
        [CellJob(("series", name), image, kind, name) for name, (image, kind) in images.items()],
        "",
        probe,
    )


def _read_series(results, status):
    """READ only when both whitelisted reads and the free read give the same letter A-G."""
    if status:
        return None, status
    letters = {results[("series", name)]["raw"].strip().upper() for name in SERIES_READS}
    letter = next(iter(letters))
    if len(letters) == 1 and len(letter) == 1 and letter in SERIES_LETTERS:
        return letter, "READ"
    return None, "UNREADABLE"


def _is_header_label(texts):
    words = [clean(text).split()[0] for text in texts if clean(text)]
    return any(ratio(word, header) >= 80 for word in words for header in HEADER_WORDS)


def _looks_like_data(row):
    if coefficient_validated(row) or row["totalScore"] >= TOTAL_SCORE:
        return True
    return max(row["wordScores"].values()) >= HEADER_LABEL_SCORE and not _is_header_label(
        row.get("labelTexts", [])
    )


def _debug_dir(diagnostics):
    root = os.environ.get("OCR_PRIVATE_DEBUG_DIR") if diagnostics is not None else None
    if not root:
        return None
    path = Path(root)
    path.mkdir(parents=True, exist_ok=True)
    return path


def extract_bac_page(
    page,
    expected,
    timeout,
    page_number,
    diagnostics=None,
    qualification=None,
    reader=None,
    prepare=None,
):
    """Returns None when the page is not a BAC general written table; never uses oral rows.

    `prepare` loads the cell reader's model in the background during the anchoring pass.
    """
    warm = None
    if prepare is not None:
        warm = Thread(target=_quietly, args=(prepare,), daemon=True)
        warm.start()
    try:
        return _extract_bac_page(
            page, expected, timeout, page_number, diagnostics, qualification, reader
        )
    finally:
        if warm is not None:
            warm.join(timeout=max(0, timeout))


def _quietly(function):
    try:
        function()
    except Exception:  # noqa: BLE001 -- the first real read reports the engine failure
        pass


def _extract_bac_page(page, expected, timeout, page_number, diagnostics, qualification, reader):
    import pymupdf

    started = monotonic()
    deadline = started + timeout
    if page.rect.width * page.rect.height * (DPI / 72) ** 2 > MAX_PIXELS:
        return [], "NEEDS_REVIEW"
    pixmap = page.get_pixmap(dpi=DPI, colorspace=pymupdf.csRGB, alpha=False)
    scan = prepare(np.frombuffer(pixmap.samples, np.uint8).reshape(pixmap.height, pixmap.width, 3))
    if scan.perspective:
        return [], "NEEDS_REVIEW"
    binary = binary_v(scan.image)
    with TemporaryDirectory(prefix="ifri-cells-") as directory:
        path = Path(directory) / "page.png"
        cv2.imwrite(str(path), binary)
        # Section titles and the series label are French: one model instead of fra+eng.
        anchors = Tesseract(11, languages=ANCHOR_LANGUAGES).extract(
            path, binary.shape[1], binary.shape[0], max(1, int(deadline - monotonic()))
        )
    anchors_seconds = round(monotonic() - started, 3)
    profile, _, issue = select_section(anchors)
    if profile.name != "BENIN_BAC_GENERAL":
        return None
    if not issue and profile.bounds is None:
        return [], "SUBJECT_NOT_FOUND"  # Oral-only continuation contributes no written rows.
    if issue or profile.bounds is None:
        return [], "NEEDS_REVIEW"
    h, w = binary.shape
    start, end = profile.bounds
    margin = np.median([t.box[3] - t.box[1] for t in anchors]) if anchors else 0.01
    y0 = max(0, int((start - margin * 0.5) * h))
    y1 = min(h, int((end + margin * 3) * h))
    region = binary[y0:y1]
    value = scan.image.max(axis=2)[y0:y1]
    gray = cv2.cvtColor(scan.image, cv2.COLOR_RGB2GRAY)[y0:y1]
    red = scan.image[y0:y1, :, 0]
    geometry = {}
    cells, issue = grid_cells(region, value, geometry, content_top=int(start * h) - y0)
    debug = _debug_dir(diagnostics)
    if debug:
        _write_overlay(debug, page_number, region, geometry, cells)
    report = {
        "strategy": "BAC_CELL_GRID",
        "page": page_number,
        "profile": profile.name,
        "section": profile.section,
        "dpi": DPI,
        "psm": 7,
        "segmentation": geometry,
        "workers": cell_workers(),
        "anchorsSeconds": anchors_seconds,
    }
    if issue:
        if diagnostics is not None:
            diagnostics.append({**report, "failureCode": issue})
        return [], issue
    # Phase 1: labels of every band (they locate the Total row) and the series letter.
    label_jobs = []
    for row_index, row_cells in enumerate(cells[1:], start=1):
        x0, ry0, x1, ry1 = row_cells[0]
        for name, variant in label_variants(value[ry0:ry1, x0:x1], gray[ry0:ry1, x0:x1]):
            label_jobs.append(CellJob((row_index, name), trim_to_ink(variant), "label", name))
    page_gray = cv2.cvtColor(scan.image, cv2.COLOR_RGB2GRAY)
    zone = series_zone(geometry["outerBorder"], int(start * h), page_gray.shape)
    series_jobs, series_status, series_probe = _series_jobs(page_gray, zone, margin * h)
    cells_started = monotonic()
    results = read_cells(label_jobs + series_jobs, deadline, reader=reader or tesseract_line)
    labels_by_row = {
        row_index: [results[(row_index, name)] for name in LABEL_VARIANTS]
        for row_index in range(1, len(cells))
    }
    # Phase 2: numbers only up to the first Total label (all bands when none is readable):
    # the EPS/oral tables below never contribute and are not read.
    last = next(
        (
            row_index
            for row_index, labels in labels_by_row.items()
            if total_score([cell["raw"] for cell in labels]) >= TOTAL_SCORE
        ),
        len(cells) - 1,
    )
    numeric_jobs = []
    for row_index in range(1, last + 1):
        for col in range(1, 5):
            x0, ry0, x1, ry1 = cells[row_index][col]
            if debug:
                raw = cv2.cvtColor(scan.image[y0 + ry0 : y0 + ry1, x0:x1], cv2.COLOR_RGB2BGR)
                cv2.imwrite(str(debug / f"page-{page_number}-raw-{row_index}-{col}.png"), raw)
            crop, gray_crop, name = numeric_variant(value[ry0:ry1, x0:x1], red[ry0:ry1, x0:x1], col)
            line = text_line(crop, gray=gray_crop)
            numeric_jobs.append(CellJob((row_index, col), line, "numeric", name))
    results.update(read_cells(numeric_jobs, deadline, reader=reader or tesseract_line))
    cells_seconds = round(monotonic() - cells_started, 3)
    if debug:
        for job in label_jobs + series_jobs + numeric_jobs:
            if job.image is not None:
                name = "-".join(str(part) for part in job.key)
                cv2.imwrite(str(debug / f"page-{page_number}-{name}.png"), job.image)
    series, series_status = _read_series(results, series_status)
    rows = []
    for row_index in range(1, last + 1):
        labels = labels_by_row[row_index]
        numbers = [results[(row_index, col)] for col in range(1, 5)]
        row = parse_numbers([cell["raw"] for cell in numbers])
        row["labelTexts"] = [cell["raw"] for cell in labels]
        row["labelConfidences"] = [cell["confidence"] for cell in labels]
        row["ocrConfidences"] = [cell["confidence"] for cell in numbers]
        row["wordScores"] = word_scores(row["labelTexts"])
        row["totalScore"] = total_score(row["labelTexts"])
        row["cellBoxes"] = cells[row_index]
        row["cropDiagnostics"] = [
            {k: v for k, v in cell.items() if k not in {"raw", "confidence", "wordConfidences"}}
            for cell in labels + numbers
        ]
        rows.append(row)
    # Leading rows that are neither a subject label nor a coefficient/scale pair are header
    # remnants (two-line or watermarked headers). Dropping a genuine row could only break the
    # sums against the Total cells, so this can never create an accepted value.
    leading = 0
    while leading < len(rows) - 2 and not _looks_like_data(rows[leading]):
        leading += 1
    rows = rows[leading:]
    # The written table ends at its Total row: the first label reading "total", else the last row.
    total_index = next(
        (i for i, row in enumerate(rows) if row["totalScore"] >= TOTAL_SCORE), len(rows) - 1
    )
    total_source = "LABEL" if rows[total_index]["totalScore"] >= TOTAL_SCORE else "LAST_ROW"
    subjects, total = rows[:total_index], rows[total_index]
    assignment, assignment_issue, assignment_report = assign(
        expected, [row["wordScores"] for row in subjects]
    )
    # Only rows of expected subjects can yield accepted values; the others serve the totals.
    total_checks, rows_outcome = evaluate(subjects, total, sorted(assignment.values()))
    mismatch = series_mismatch(series, qualification)
    if mismatch:
        outcome = "SERIES_MISMATCH"
    elif not subjects:
        outcome = "NEEDS_REVIEW"
    else:
        outcome = assignment_issue or rows_outcome
    if diagnostics is not None:
        diagnostics.append(
            {
                **report,
                "failureCode": outcome,
                "series": series,
                "seriesStatus": series_status,
                "seriesCandidate": series_probe,
                "seriesZone": [round(zone[0] / w, 4), round(zone[1] / h, 4)]
                + [round(zone[2] / w, 4), round(zone[3] / h, 4)],
                "sectionStart": round(start, 3),
                "seriesMatchesDeclaration": None if series is None else not mismatch,
                "totalRowSource": total_source,
                "leadingNonDataRows": leading,
                "subjectRowCount": len(subjects),
                "expectedRows": sorted(assignment.values()),
                "rows": [{k: v for k, v in r.items() if k != "cellBoxes"} for r in rows],
                "totalChecks": total_checks,
                "assignment": assignment_report,
                "cellsSeconds": cells_seconds,
                "seconds": round(monotonic() - started, 3),
            }
        )
    if outcome:
        return [], outcome
    # Direct reads only; a reconstructed cell is named so that Go re-derives it from the Total.
    evidence_rows = []
    for r in subjects:
        cells_row = {
            name: r["parsed"][column] or ""
            for column, name in enumerate(("grade", "coefficient", "points", "scale"))
        }
        rebuilt = [n for c, n in ADDITIVE.items() if r["cellSources"][c] == RECONSTRUCTED]
        if rebuilt:
            cells_row["reconstructed"] = rebuilt
        evidence_rows.append(cells_row)
    # Values as interpreted by the worker (an impossible average is no reading), "" if none.
    total_cells = [cell or "" for cell in total["parsed"]]
    result = []
    for target in expected:
        row_index = assignment[target["id"]]
        row = subjects[row_index]
        word = vocabulary_word(target)
        word_index = max(
            range(len(LABEL_VARIANTS)),
            key=lambda i: word_scores([row["labelTexts"][i]])[word],
        )
        boxes = [
            scan.original_box(
                {"x0": x0 / w, "y0": (y0 + ry0) / h, "x1": x1 / w, "y1": (y0 + ry1) / h}
            )
            for x0, ry0, x1, ry1 in row["cellBoxes"]
        ]
        confidences = [row["labelConfidences"][word_index], *row["ocrConfidences"]]
        result.append(
            {
                "subjectId": target["id"],
                "position": target["position"],
                "detectedSubjectText": row["labelTexts"][word_index][:300],
                "gradeRaw": row["rawCells"][0][:32],
                "gradeValue": row["gradeValue"],
                "coefficientRaw": row["rawCells"][1][:32],
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
                    "dpi": DPI,
                    "psm": 7,
                    "deskewDegrees": scan.angle,
                    "validation": "CELL_ARITHMETIC",
                    "gradeSource": row["gradeSource"],
                    "points": boxes[3],
                    "scale": boxes[4],
                    "arithmeticRows": evidence_rows,
                    "arithmeticRowIndex": row_index,
                    "totalCells": total_cells,
                    "series": series or "",
                },
            }
        )
    return result, ""


def _write_overlay(debug, page_number, region, geometry, cells):
    cv2.imwrite(str(debug / f"page-{page_number}-section.png"), region)
    overlay = cv2.cvtColor(region, cv2.COLOR_GRAY2BGR)
    for x in geometry.get("columnEdges", []):
        cv2.line(overlay, (x, 0), (x, region.shape[0] - 1), (255, 0, 0), 2)
    for row_cells in cells:
        for x0, ry0, x1, ry1 in row_cells:
            cv2.rectangle(overlay, (x0, ry0), (x1, ry1), (0, 0, 255), 2)
    cv2.imwrite(str(debug / f"page-{page_number}-grid.png"), overlay)
