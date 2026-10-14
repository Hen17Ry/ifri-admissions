"""Cell crops and single-line Tesseract reads, executed in parallel within the page budget.

Numeric cells and labels are prepared differently: numbers keep a strict whitelist and are read
on every row (totals need them); labels are cropped to their ink and read with a few variants,
because they only serve to locate expected subjects.
"""

import csv
import io
import os
import subprocess
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from time import monotonic

import cv2
import numpy as np

NUMERIC_WHITELIST = "0123456789,."
SERIES_WHITELIST = "ABCDEFG"


def cell_workers() -> int:
    configured = os.environ.get("OCR_CELL_WORKERS")
    workers = int(configured) if configured else min(4, os.cpu_count() or 1)
    if not 1 <= workers <= 16:
        raise ValueError("OCR_CELL_WORKERS out of range")
    return workers


def remove_rules(binary):
    """Inpaint table rulings only; character strokes stay intact.

    A vertical ruling spans >= 75% of the cell height (crops are ~1.7 glyph heights, so a digit
    reaches ~60%); a 3-px horizontal dilation tolerates the residual tilt of photographed rules.
    """
    h, w = binary.shape
    ink = 255 - binary
    horizontal = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (max(20, int(w * 0.6)), 1))
    )
    tolerant = cv2.dilate(ink, np.ones((1, 3), np.uint8))
    vertical = cv2.morphologyEx(
        tolerant,
        cv2.MORPH_OPEN,
        cv2.getStructuringElement(cv2.MORPH_RECT, (1, max(20, int(h * 0.75)))),
    )
    vertical = cv2.bitwise_and(cv2.dilate(vertical, np.ones((1, 3), np.uint8)), ink)
    return cv2.inpaint(binary, cv2.bitwise_or(horizontal, vertical), 2, cv2.INPAINT_TELEA)


def trim_to_ink(binary, margin=6):
    """Crop to glyph ink. Border-touching rule fragments and specks never extend the crop.

    Returns None for an empty cell. The kept components are unchanged: no glyph is redrawn.
    """
    h, w = binary.shape
    ink = (binary < 128).astype(np.uint8)
    count, labels, stats, _ = cv2.connectedComponentsWithStats(ink, connectivity=8)
    keep = np.zeros_like(ink)
    for component in range(1, count):
        x, y, cw, ch, area = stats[component]
        touches = x == 0 or y == 0 or x + cw >= w or y + ch >= h
        line_like = cw <= 3 or ch <= 3 or cw > 6 * ch or ch > 6 * cw
        if area < max(6, h * w * 0.0004) or (touches and line_like):
            continue
        keep[labels == component] = 1
    points = cv2.findNonZero(keep)
    if points is None:
        return None
    x, y, cw, ch = cv2.boundingRect(points)
    cleaned = np.where(keep == 1, 0, 255).astype(np.uint8)
    cropped = cleaned[y : y + ch, x : x + cw]
    return cv2.copyMakeBorder(
        cropped, margin, margin, margin, margin, cv2.BORDER_CONSTANT, value=255
    )


def flatten_background(channel):
    """Divide by a closing-estimated background: colored bands and alternating row tones become
    uniform white while dark strokes (narrower than the kernel) keep their contrast."""
    kernel = cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (17, 17))
    background = cv2.morphologyEx(channel, cv2.MORPH_CLOSE, kernel)
    return cv2.divide(channel, np.maximum(background, 1), scale=255)


def numeric_variant(value_crop, red_crop, column):
    """Background-flattened V channel enlarged x2 before Otsu (faded thin digits keep open
    counters); a still too-dark cell falls back to red CLAHE. Returns (binary mask, grayscale).

    The switch depends on the crop's ink density only, never on expected values.
    """
    gray = cv2.resize(
        flatten_background(value_crop), None, fx=2, fy=2, interpolation=cv2.INTER_CUBIC
    )
    crop = cv2.threshold(gray, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    name = "HSV_V_FLAT_X2_OTSU"
    if float(np.mean(crop < 128)) > 0.45:
        red = cv2.createCLAHE(clipLimit=2, tileGridSize=(4, 4)).apply(red_crop)
        gray = cv2.resize(red, None, fx=2, fy=2, interpolation=cv2.INTER_CUBIC)
        crop = cv2.threshold(gray, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
        name = "RED_CELL_CLAHE_X2_OTSU"
    return remove_rules(crop), gray, name + "_RULES_REMOVED"


def text_line(binary, margin=8, gray=None):
    """Keep only glyphs on the cell's dominant text line: drops rule fragments, specks and
    background blobs that a plain bounding box would include. Returns None if no text line.

    With `gray` (same geometry), the kept glyphs are returned in grayscale (anti-aliased edges
    and open counters preserved) on white, so Tesseract binarises the cleaned line itself.
    """
    ink = (binary < 128).astype(np.uint8)
    count, labels, stats, _ = cv2.connectedComponentsWithStats(ink, connectivity=8)
    h, w = binary.shape
    candidates = []
    for component in range(1, count):
        x, y, cw, ch, area = stats[component]
        # Glyph-like: not a thin rule, not a wide blob, not a speck.
        if ch < max(6, h * 0.18) or ch > h * 0.95 or cw > 1.6 * ch + 4 or area < 0.12 * cw * ch:
            continue
        # A thin stroke hugging the left/right crop edge is a column-ruling fragment.
        if cw <= max(3, 0.3 * ch) and (x <= 6 or x + cw >= w - 6):
            continue
        candidates.append((x, y, cw, ch, component))
    if not candidates:
        return None
    heights = sorted(c[3] for c in candidates)
    glyph = heights[len(heights) // 2]
    # Dominant line: the vertical position supported by the most glyph-sized components.
    centers = [c[1] + c[3] / 2 for c in candidates if 0.6 * glyph <= c[3] <= 1.5 * glyph]
    if not centers:
        return None
    line = max(centers, key=lambda y: sum(abs(y - other) <= glyph * 0.35 for other in centers))
    top, bottom = line - glyph * 0.75, line + glyph * 0.75
    keep = np.zeros_like(ink)
    glyphs, marks = [], []
    for component in range(1, count):
        x, y, cw, ch, area = stats[component]
        middle = y + ch / 2
        glyph_like = 0.6 * glyph <= ch <= 1.5 * glyph and cw <= 1.6 * ch + 4
        edge_rule = cw <= max(3, 0.3 * ch) and (x <= 6 or x + cw >= w - 6)
        if glyph_like and top <= middle <= bottom and not edge_rule:
            glyphs.append(component)
        elif ch < 0.6 * glyph and cw < glyph and area >= 4 and middle >= line:
            marks.append(component)
    if not glyphs:
        return None
    left = min(stats[c][0] for c in glyphs)
    right = max(stats[c][0] + stats[c][2] for c in glyphs)
    # A decimal separator sits between digits, on the lower half of the line; other specks go.
    for component in glyphs + [
        m for m in marks if left < stats[m][0] and stats[m][0] + stats[m][2] < right
    ]:
        keep[labels == component] = 1
    points = cv2.findNonZero(keep)
    if points is None:
        return None
    x, y, cw, ch = cv2.boundingRect(points)
    if gray is None:
        cleaned = np.where(keep == 1, 0, 255).astype(np.uint8)
    else:
        halo = cv2.dilate(keep, np.ones((3, 3), np.uint8))
        cleaned = np.where(halo == 1, gray, 255).astype(np.uint8)
    cleaned = cleaned[y : y + ch, x : x + cw]
    return cv2.copyMakeBorder(
        cleaned, margin, margin, margin, margin, cv2.BORDER_CONSTANT, value=255
    )


def label_variants(value_crop, gray_crop):
    """Two to three binarisations of a wide label cell; the best vocabulary score wins later."""
    otsu = cv2.threshold(value_crop, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    adaptive = cv2.adaptiveThreshold(
        cv2.GaussianBlur(value_crop, (3, 3), 0),
        255,
        cv2.ADAPTIVE_THRESH_GAUSSIAN_C,
        cv2.THRESH_BINARY,
        31,
        15,
    )
    flat = flatten_background(value_crop)
    flat = cv2.threshold(flat, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    return [
        ("LABEL_V_OTSU", remove_rules(otsu)),
        ("LABEL_V_ADAPTIVE", remove_rules(adaptive)),
        ("LABEL_V_FLAT_OTSU", remove_rules(flat)),
    ]


@dataclass(frozen=True)
class CellJob:
    key: tuple
    image: np.ndarray | None
    kind: str  # "numeric", "label", "series" (A-G whitelist) or "series_free" (cross-check)
    preprocessing: str


def _command(kind, psm=None):
    if kind == "numeric":
        whitelist = "tessedit_char_whitelist=" + NUMERIC_WHITELIST
        return ["-l", "eng", "--psm", psm or "7", "-c", whitelist]
    if kind == "series":
        return ["-l", "eng", "--psm", "10", "-c", "tessedit_char_whitelist=" + SERIES_WHITELIST]
    if kind == "series_free":
        return ["-l", "eng", "--psm", "10"]
    return ["-l", "fra", "--psm", "7"]


# Tesseract's LSTM reads best around this glyph height; a fixed x3 enlargement of 300 DPI
# cells gave ~90 px glyphs and confused 5/9/'.' on clean synthetic digits (measured).
TARGET_INK_HEIGHT = 36


def _scale(image):
    rows = np.flatnonzero(np.count_nonzero(image < 128, axis=1))
    height = rows[-1] - rows[0] + 1 if rows.size else image.shape[0]
    return float(np.clip(TARGET_INK_HEIGHT / max(1, height), 0.5, 4.0))


def tesseract_line(image, kind, timeout):
    """Numeric cells: PSM 7 first; only an empty/unparseable answer falls back to PSM 8 then 13.

    The order is fixed and blind to expected values; arithmetic then checks whatever is read.
    """
    result = _tesseract(image, kind, timeout, None)
    if kind != "numeric" or result.get("error") or _parses(result["raw"]):
        return result
    for psm in ("8", "13"):
        retry = _tesseract(image, kind, timeout, psm)
        if _parses(retry["raw"]):
            retry["psmFallback"] = psm
            retry["seconds"] = round(retry["seconds"] + result["seconds"], 4)
            return retry
    return result


def _parses(raw):
    from ifri_ocr.extraction import number

    return number(raw) is not None


def _tesseract(image, kind, timeout, psm):
    """One Tesseract process per crop, image passed through stdin, TSV words level 5 only."""
    started = monotonic()
    if image is None:
        return {"raw": "", "confidence": 0, "error": "EMPTY_CELL", "seconds": 0}
    factor = _scale(image)
    interpolation = cv2.INTER_CUBIC if factor > 1 else cv2.INTER_AREA
    image = cv2.resize(image, None, fx=factor, fy=factor, interpolation=interpolation)
    image = cv2.copyMakeBorder(image, 15, 15, 15, 15, cv2.BORDER_CONSTANT, value=255)
    ok, encoded = cv2.imencode(".png", image)
    if not ok:
        return {"raw": "", "confidence": 0, "error": "CELL_UNREADABLE", "seconds": 0}
    try:
        result = subprocess.run(
            ["tesseract", "stdin", "stdout", *_command(kind, psm), "tsv"],
            input=encoded.tobytes(),
            capture_output=True,
            check=True,
            timeout=max(1, timeout),
            env={**os.environ, "OMP_THREAD_LIMIT": "1"},
        )
    except subprocess.TimeoutExpired, subprocess.CalledProcessError:
        return {"raw": "", "confidence": 0, "error": "CELL_UNREADABLE", "seconds": 0}
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


def read_cells(jobs, deadline, workers=None, reader=tesseract_line):
    """Run every crop through `reader` in parallel; exhausted budget never invents text."""

    def run(job):
        remaining = deadline - monotonic()
        if remaining <= 0:
            return {"raw": "", "confidence": 0, "error": "PAGE_BUDGET_EXHAUSTED", "seconds": 0}
        result = reader(job.image, job.kind, int(remaining))
        result["preprocessing"] = job.preprocessing
        return result

    with ThreadPoolExecutor(max_workers=workers or cell_workers()) as pool:
        return dict(zip((job.key for job in jobs), pool.map(run, jobs), strict=True))
