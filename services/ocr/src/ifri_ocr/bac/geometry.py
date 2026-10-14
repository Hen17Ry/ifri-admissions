"""Table geometry: five fixed columns and row bands from label-column ink, without OCR."""

from itertools import pairwise

import cv2
import numpy as np

from ifri_ocr.layouts import BAC_COLUMN_RATIOS

# Contract bound shared with Go (arithmeticRows <= 30), not a plausibility rule on subjects.
MAX_TABLE_ROWS = 30


def binary_v(rgb):
    value = cv2.cvtColor(rgb, cv2.COLOR_RGB2HSV)[:, :, 2]
    # Neutral black strokes remain dark; colored security ink is brighter on V.
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


def _columns(binary, ink, geometry):
    h, w = binary.shape
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
        return None, ys, None
    left, _, span, _ = borders[0]
    right = left + span - 1
    desired = [left + int(r * (right - left)) for r in BAC_COLUMN_RATIOS]
    inside = [x for x in xs if left - 5 <= x <= right + 5]
    matched = [min(inside, key=lambda x: abs(x - wanted)) for wanted in desired] if inside else []
    detected = len(set(matched)) == 6 and all(
        abs(found - wanted) <= span * 0.025 for found, wanted in zip(matched, desired, strict=True)
    )
    edges = matched if detected else desired
    widths = np.diff(edges) / float(right - left)
    if not (
        widths[0] > 3 * max(widths[1:]) and 0.075 <= min(widths[1:]) and max(widths[1:]) <= 0.17
    ):
        return None, ys, borders[0]
    if geometry is not None:
        geometry.update(
            columnEdges=list(edges),
            outerBorder=[left, right],
            columnStrategy="DETECTED" if detected else "PROFILE_RATIOS",
            relativeWidths=widths.tolist(),
        )
    return list(edges), ys, borders[0]


def _label_bands(binary, value, xs, h):
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
    count, _, stats, _ = cv2.connectedComponentsWithStats(label)
    # Characters crossing each scanline: independent of label length (short "Anglais" or
    # "TOTAL" next to a long subject name), unlike a threshold relative to the densest row.
    crossing = np.zeros(h, np.int32)
    character_heights = []
    for component in range(1, count):
        _, cy, cw, ch, area = stats[component]
        if (
            max(5, h * 0.012) <= ch <= h * 0.12
            and 2 <= cw <= (label_right - label_left) * 0.25
            and area >= 8
        ):
            crossing[cy : cy + ch] += 1
            character_heights.append(int(ch))
    occupied = np.flatnonzero(crossing >= 2)
    bands = []
    for y in occupied:
        if not bands or y - bands[-1][-1] > 4:
            bands.append([int(y)])
        else:
            bands[-1].append(int(y))
    minimum_band_height = (
        max(5, np.percentile(character_heights, 75) * 0.5) if character_heights else 5
    )
    return [band for band in bands if band[-1] - band[0] + 1 >= minimum_band_height]


def grid_cells(binary, value=None, geometry=None, content_top=0):
    """Header row + N >= 1 subject rows + Total row, five cells each. No OCR coordinates.

    content_top: y of the section title bottom inside `binary`; ink ending above it belongs to
    the title, never to the table (real tables may have no detectable horizontal ruling).
    """
    h, _ = binary.shape
    ink = 255 - binary
    xs, ys, border = _columns(binary, ink, geometry)
    if xs is None:
        return [], "COLUMN_SEGMENTATION_FAILED"
    top_line = max(0, border[1])
    all_bands = [band for band in _label_bands(binary, value, xs, h) if band[-1] > content_top + 2]
    bands, split_index, median_gap = first_table_bands(all_bands)
    if geometry is not None:
        geometry["horizontalRules"] = list(ys)
        geometry["allRowBands"] = [(band[0], band[-1]) for band in all_bands]
        geometry["tableSplitAfterBand"] = split_index
        geometry["medianInterBandGap"] = median_gap
        geometry["rowBands"] = [(band[0], band[-1]) for band in bands]
    # Exclude the actual header band before row segmentation; no OCR dependence.
    header_lines = [line for line in ys if len(bands) > 1 and bands[0][-1] < line < bands[1][0]]
    if header_lines:
        header_bottom = min(header_lines)
        data = [band for band in bands if band[0] > header_bottom + 3]
        header = (top_line + 4, header_bottom - 4)
    elif len(bands) > 1:
        # Without a visible header separator, the first label band is the header.
        header_bottom = (bands[0][-1] + bands[1][0]) // 2
        data = bands[1:]
        header = (max(0, bands[0][0] - 5), header_bottom)
    else:
        data, header, header_bottom = [], None, None
    if geometry is not None:
        geometry["headerExcludedThrough"] = header_bottom
        geometry["dataRowCount"] = len(data)
    # At least one subject row followed by the Total row; the bound only limits resources.
    if not 2 <= len(data) <= MAX_TABLE_ROWS:
        return [], "ROW_SEGMENTATION_FAILED"
    # Symmetric padding around each band's own ink, including the first data row.
    padding = max(4, int(np.median([band[-1] - band[0] + 1 for band in data]) * 0.35))
    cells = [[(xs[col] + 5, header[0], xs[col + 1] - 5, header[1]) for col in range(5)]]
    for band in data:
        top = max(0, band[0] - padding)
        cells.append(
            [
                (xs[col] + 5, top, xs[col + 1] - 5, min(h - 1, band[-1] + padding))
                for col in range(5)
            ]
        )
    return cells, ""
