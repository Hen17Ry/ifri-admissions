"""Bounded scan variants and reversible geometry; no document text is retained."""

from dataclasses import dataclass
from math import atan2, degrees

import cv2
import numpy as np

MAX_PIXELS = 25_000_000


@dataclass
class PreparedScan:
    image: np.ndarray
    inverse: np.ndarray
    original_width: int
    original_height: int
    angle: float
    perspective: bool

    def original_box(self, box):
        h, w = self.image.shape[:2]
        corners = np.array(
            [
                [
                    [box["x0"] * w, box["y0"] * h],
                    [box["x1"] * w, box["y0"] * h],
                    [box["x1"] * w, box["y1"] * h],
                    [box["x0"] * w, box["y1"] * h],
                ]
            ],
            dtype=np.float64,
        )
        mapped = cv2.perspectiveTransform(corners, self.inverse)[0]
        return {
            "x0": float(np.clip(mapped[:, 0].min() / self.original_width, 0, 1)),
            "y0": float(np.clip(mapped[:, 1].min() / self.original_height, 0, 1)),
            "x1": float(np.clip(mapped[:, 0].max() / self.original_width, 0, 1)),
            "y1": float(np.clip(mapped[:, 1].max() / self.original_height, 0, 1)),
        }


def prepare(rgb: np.ndarray) -> PreparedScan:
    cv2.setNumThreads(1)
    h, w = rgb.shape[:2]
    if h * w > MAX_PIXELS:
        raise ValueError("Pixel limit exceeded")
    original_h, original_w = h, w
    perspective_matrix = np.eye(3)
    page_angle = None
    # Correct only a large, convex four-corner document boundary; no portrait/table warp.
    edge_image = cv2.Canny(cv2.cvtColor(rgb, cv2.COLOR_RGB2GRAY), 60, 160)
    contours, _ = cv2.findContours(edge_image, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
    for contour in sorted(contours, key=cv2.contourArea, reverse=True)[:5]:
        if cv2.contourArea(contour) < h * w * 0.65:
            continue
        quad = cv2.approxPolyDP(contour, cv2.arcLength(contour, True) * 0.02, True)
        if len(quad) != 4 or not cv2.isContourConvex(quad):
            continue
        points = quad[:, 0].astype(np.float32)
        ordered = np.array(
            [
                points[np.argmin(points.sum(axis=1))],
                points[np.argmax(points[:, 0] - points[:, 1])],
                points[np.argmax(points.sum(axis=1))],
                points[np.argmin(points[:, 0] - points[:, 1])],
            ],
            np.float32,
        )
        if len(np.unique(ordered, axis=0)) != 4:
            continue
        upper, lower = (
            np.linalg.norm(ordered[1] - ordered[0]),
            np.linalg.norm(ordered[2] - ordered[3]),
        )
        left, right = (
            np.linalg.norm(ordered[3] - ordered[0]),
            np.linalg.norm(ordered[2] - ordered[1]),
        )
        if (
            max(upper, lower) / min(upper, lower) <= 1.03
            and max(left, right) / min(left, right) <= 1.03
        ):
            # Nearly rectangular page photographed at an angle: its own border gives the tilt,
            # more reliably than content lines bent by a slight perspective.
            top = degrees(atan2(ordered[1][1] - ordered[0][1], ordered[1][0] - ordered[0][0]))
            bottom = degrees(atan2(ordered[2][1] - ordered[3][1], ordered[2][0] - ordered[3][0]))
            if abs(top - bottom) <= 1.0:
                page_angle = (top + bottom) / 2
            break
        perspective_matrix = cv2.getPerspectiveTransform(
            ordered, np.array([[0, 0], [w - 1, 0], [w - 1, h - 1], [0, h - 1]], np.float32)
        )
        rgb = cv2.warpPerspective(rgb, perspective_matrix, (w, h), borderValue=(255, 255, 255))
        break
    gray = cv2.cvtColor(rgb, cv2.COLOR_RGB2GRAY)
    # Long horizontal table lines give a much more credible angle than a portrait/background.
    scale = min(1.0, 1400 / max(h, w))
    small = cv2.resize(gray, None, fx=scale, fy=scale)
    edges = cv2.Canny(small, 60, 160)
    segments = cv2.HoughLinesP(
        edges, 1, np.pi / 1800, threshold=60, minLineLength=int(w * scale * 0.18), maxLineGap=12
    )
    angles = []
    if segments is not None:
        for x0, y0, x1, y1 in segments[:, 0]:
            angle = degrees(atan2(y1 - y0, x1 - x0))
            if abs(angle) < 12:
                angles.append(angle)
    angle = float(np.median(angles)) if len(angles) >= 4 else 0.0
    dispersion = (
        float(np.percentile(angles, 90) - np.percentile(angles, 10)) if len(angles) >= 4 else 0
    )
    # Perspective or nonparallel lines: refuse rather than silently flattening a complex photograph.
    perspective = len(angles) >= 4 and dispersion > 4
    if page_angle is not None and not perspective:
        angle, dispersion = page_angle, 0.0
    matrix = np.eye(3)
    if not perspective and 0.3 <= abs(angle) <= 5 and dispersion < 1.5:
        matrix[:2] = cv2.getRotationMatrix2D((w / 2, h / 2), angle, 1)
        rgb = cv2.warpAffine(
            rgb, matrix[:2], (w, h), flags=cv2.INTER_CUBIC, borderValue=(255, 255, 255)
        )
    else:
        angle = 0.0
    return PreparedScan(
        rgb, np.linalg.inv(matrix @ perspective_matrix), original_w, original_h, angle, perspective
    )


def variant(scan: PreparedScan, name: str) -> np.ndarray:
    gray = cv2.cvtColor(scan.image, cv2.COLOR_RGB2GRAY)
    if name == "grayscale":
        return gray
    if name == "clahe":
        return cv2.createCLAHE(clipLimit=2.0, tileGridSize=(8, 8)).apply(gray)
    if name == "adaptive":
        smooth = cv2.GaussianBlur(gray, (3, 3), 0)
        return cv2.adaptiveThreshold(
            smooth, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY, 41, 12
        )
    if name == "security-table":
        # Lightest channel suppresses colored ink while preserving neutral dark glyphs.
        light = scan.image.max(axis=2)
        background = cv2.GaussianBlur(light, (0, 0), 25)
        normalized = cv2.divide(light, np.maximum(background, 1), scale=255)
        binary = cv2.adaptiveThreshold(
            normalized, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY, 51, 12
        )
        ink = 255 - binary
        h, w = ink.shape
        horizontal = cv2.morphologyEx(
            ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (max(80, w // 5), 1))
        )
        vertical = cv2.morphologyEx(
            ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (1, max(80, h // 8)))
        )
        grid = cv2.bitwise_or(horizontal, vertical)
        # Fill only detected long grid strokes; short character strokes stay intact.
        return cv2.inpaint(binary, grid, 2, cv2.INPAINT_TELEA)
    if name == "background-normalized":
        # Estimate slow illumination/security motifs at a scale larger than text strokes.
        background = cv2.GaussianBlur(gray, (0, 0), 15)
        normalized = cv2.divide(gray, np.maximum(background, 1), scale=255)
        return cv2.adaptiveThreshold(
            normalized, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY, 41, 10
        )
    if name == "otsu":
        return cv2.threshold(gray, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    if name == "color-channel":
        # Pick greatest local contrast, not a fixed green/red preference.
        contrasts = [float(cv2.Laplacian(scan.image[:, :, i], cv2.CV_64F).var()) for i in range(3)]
        channel = scan.image[:, :, int(np.argmax(contrasts))]
        return cv2.createCLAHE(clipLimit=2.0, tileGridSize=(8, 8)).apply(channel)
    raise ValueError("Unknown preprocessing variant")


def table_column_edges(image: np.ndarray) -> list[int]:
    """Long vertical rulings only; refuse absent or excessively fragmented grids."""
    h, w = image.shape
    ink = cv2.adaptiveThreshold(
        image, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY_INV, 41, 12
    )
    vertical = cv2.morphologyEx(
        ink, cv2.MORPH_OPEN, cv2.getStructuringElement(cv2.MORPH_RECT, (1, max(40, h // 3)))
    )
    positions = np.flatnonzero(np.count_nonzero(vertical, axis=0) >= h * 0.3)
    groups = []
    for x in positions:
        if not groups or x - groups[-1][-1] > max(3, w // 300):
            groups.append([int(x)])
        else:
            groups[-1].append(int(x))
    edges = [int(np.median(group)) for group in groups]
    return edges if 4 <= len(edges) <= 8 else []
