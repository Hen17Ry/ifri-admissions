"""Deterministic synthetic BAC general transcripts. No real person, value or layout copy.

The page imitates the difficulties observed on scanned transcripts (colored vertical bands,
large translucent watermark, alternating grey rows without horizontal rulings, written + EPS +
oral tables with duplicated subjects, Total row, photo perspective and tilt). Every printed value
is synthetic and doubles as the oracle of the fixture; nothing here is a worker default.
"""

from dataclasses import dataclass, field, replace
from decimal import ROUND_DOWN, Decimal

import cv2
import numpy as np
import pymupdf

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
# Column edges relative to the table border; only the drawing uses them.
COLUMNS = (0.0, 0.535, 0.645, 0.755, 0.89, 1.0)
TABLE_LEFT, TABLE_RIGHT = 45.0, 550.0


@dataclass(frozen=True)
class Row:
    label: str
    grade: str
    coefficient: int
    smudged: bool = False
    # Printed text replacing a cell ("" = blank, e.g. faded); the oracle keeps the true values.
    grade_cell: str | None = None
    points_cell: str | None = None
    scale_cell: str | None = None

    @property
    def points(self) -> Decimal:
        return Decimal(self.grade.replace(",", ".")) * self.coefficient

    def cells(self) -> tuple[str, str, str, str]:
        return (
            self.grade if self.grade_cell is None else self.grade_cell,
            str(self.coefficient),
            printed(self.points) if self.points_cell is None else self.points_cell,
            str(20 * self.coefficient) if self.scale_cell is None else self.scale_cell,
        )


@dataclass(frozen=True)
class Transcript:
    series: str | None
    written: tuple[Row, ...]
    oral: tuple[Row, ...] = (Row("Anglais", "15,00", 2), Row("Français", "11,50", 2))
    eps: Row = Row("Éducation physique et sportive", "16,00", 1)
    vertical_rules: bool = True
    total_points_override: str | None = None
    extra_rows: tuple[Row, ...] = field(default_factory=tuple)

    def oracle(self, codes: dict[str, str]) -> dict[str, tuple[str, str]]:
        """Expected values by subject code, read from the synthetic rows themselves."""
        values = {}
        for row in self.written:
            for code, label in codes.items():
                if row.label == label:
                    values[code] = (str(Decimal(row.grade.replace(",", "."))), str(row.coefficient))
        return values


def printed(value: Decimal) -> str:
    text = f"{value:.2f}"
    return text.replace(".", ",")


BAC_D = Transcript(
    "D",
    (
        Row("Anglais", "13,50", 2),
        Row("Français", "11,25", 2),
        Row("Histoire et Géographie", "09,75", 2),
        Row("Mathématiques", "14,25", 4),
        Row("Philosophie", "08,50", 2),
        Row("Physique Chimie et Technologie", "12,00", 4),
        Row("Sciences de la Vie et de la Terre", "15,50", 5),
    ),
)
BAC_C_SHUFFLED = Transcript(
    "C",
    (
        Row("Mathématiques", "16,75", 5),
        Row("Philosophie", "10,00", 2),
        Row("Français", "12,50", 2),
        Row("Physique Chimie et Technologie", "13,25", 5),
        Row("Anglais", "09,25", 2),
        Row("Histoire et Géographie", "11,00", 2),
        Row("Sciences de la Vie et de la Terre", "12,75", 2),
    ),
)
BAC_E = Transcript(
    "E",
    (
        Row("Anglais", "10,75", 2),
        Row("Étude de fabrication", "14,50", 4),
        Row("Français", "09,50", 2),
        Row("Mathématiques", "13,00", 5),
        Row("Philosophie", "07,25", 1),
        Row("Sciences physiques", "11,75", 4),
        Row("Technologie", "12,25", 3),
    ),
)
TARGET_LABELS = {
    "MATH": "Mathématiques",
    "FRENCH": "Français",
    "ENGLISH": "Anglais",
    "MANUFACTURING_STUDY": "Étude de fabrication",
}


def _cells(transcript: Transcript, **cells: dict[str, str]) -> Transcript:
    """Override printed cells by label: _cells(BAC_D, Mathématiques={"points_cell": "59,00"})."""
    rows = tuple(replace(r, **cells.get(r.label, {})) for r in transcript.written)
    return replace(transcript, written=rows)


MATH, FRENCH, PHILOSOPHY = "Mathématiques", "Français", "Philosophie"


def variant(name: str) -> tuple[Transcript, str]:
    """Named fixtures: (transcript, declared qualification used by the oracle job)."""
    if name == "d-scan":
        return BAC_D, "BAC_D"
    if name == "d-photo":
        return BAC_D, "BAC_D"
    if name == "d-no-vertical-rules":
        return replace(BAC_D, vertical_rules=False), "BAC_D"
    if name == "c-photo-shuffled":
        return BAC_C_SHUFFLED, "BAC_C"
    if name == "e-scan":
        return BAC_E, "BAC_E"
    if name == "d-series-unprinted":
        return replace(BAC_D, series=None), "BAC_D"
    if name == "d-smudged-philosophy":
        rows = tuple(replace(r, smudged=r.label == "Philosophie") for r in BAC_D.written)
        return replace(BAC_D, written=rows), "BAC_D"
    if name == "c-declared-d":
        return BAC_C_SHUFFLED, "BAC_D"
    if name == "d-math-note-blank":
        return _cells(BAC_D, **{MATH: {"grade_cell": ""}}), "BAC_D"
    if name == "d-wrong-total":
        blank = _cells(BAC_D, **{MATH: {"grade_cell": ""}})
        return replace(blank, total_points_override="999,00"), "BAC_D"
    if name == "d-grade-points-mismatch":
        return _cells(BAC_D, **{MATH: {"points_cell": "59,00"}}), "BAC_D"
    if name == "d-compensated-points":
        # 57 -> 59 and 22,50 -> 20,50: the Points sum still equals the printed Total.
        cells = {MATH: {"points_cell": "59,00"}, FRENCH: {"points_cell": "20,50"}}
        return _cells(BAC_D, **cells), "BAC_D"
    if name == "d-faded-scale":
        return _cells(BAC_D, **{MATH: {"scale_cell": ""}}), "BAC_D"
    if name == "d-two-scales-blank":
        return _cells(BAC_D, **{MATH: {"scale_cell": ""}, FRENCH: {"scale_cell": ""}}), "BAC_D"
    if name == "d-scale-contradicted":
        # Reconstructed Math Sur = 80 - 20 = 60 because Philosophie prints 60 instead of 40.
        cells = {MATH: {"scale_cell": ""}, PHILOSOPHY: {"scale_cell": "60"}}
        return _cells(BAC_D, **cells), "BAC_D"
    if name == "d-duplicate-french":
        rows = BAC_D.written + (Row("Français", "16,00", 2),)
        return replace(BAC_D, written=rows), "BAC_D"
    if name == "d-missing-math":
        rows = tuple(r for r in BAC_D.written if r.label != "Mathématiques")
        return replace(BAC_D, written=rows), "BAC_D"
    raise ValueError(name)


# name -> (photo transform, expected status, expected failure code, cell path required)
VARIANTS = {
    "d-scan": ("scan", "SUCCEEDED", "", True),
    "d-photo": ("photo", "SUCCEEDED", "", True),
    "d-no-vertical-rules": ("scan", "SUCCEEDED", "", True),
    "c-photo-shuffled": ("photo", "SUCCEEDED", "", True),
    "e-scan": ("scan", "SUCCEEDED", "", True),
    "d-series-unprinted": ("scan", "SUCCEEDED", "", True),
    "d-smudged-philosophy": ("scan", "SUCCEEDED", "", True),
    "c-declared-d": ("scan", "FAILED", "SERIES_MISMATCH", True),
    "d-math-note-blank": ("scan", "SUCCEEDED", "", True),
    "d-wrong-total": ("scan", "LOW_CONFIDENCE", "NEEDS_REVIEW", True),
    "d-grade-points-mismatch": ("scan", "LOW_CONFIDENCE", "GRADE_POINTS_MISMATCH", True),
    "d-compensated-points": ("scan", "LOW_CONFIDENCE", "GRADE_POINTS_MISMATCH", True),
    "d-faded-scale": ("scan", "SUCCEEDED", "", True),
    "d-two-scales-blank": ("scan", "LOW_CONFIDENCE", "NEEDS_REVIEW", True),
    "d-scale-contradicted": ("scan", "LOW_CONFIDENCE", "NEEDS_REVIEW", True),
    "d-duplicate-french": ("scan", "LOW_CONFIDENCE", "AMBIGUOUS_ROW", True),
    "d-missing-math": ("scan", "LOW_CONFIDENCE", "NEEDS_REVIEW", True),
}


def _x(ratio: float) -> float:
    return TABLE_LEFT + ratio * (TABLE_RIGHT - TABLE_LEFT)


def _table(page, top, rows, *, header, total, vertical_rules, row_height=21.0):
    """Header + rows (+ optional total); grey alternating rows, no inner horizontal rules."""
    height = row_height * (len(rows) + (1 if header else 0) + (1 if total else 0))
    bottom = top + height
    y = top + (row_height if header else 0)
    for index in range(len(rows) + (1 if total else 0)):
        if index % 2 == 0:
            page.draw_rect(
                pymupdf.Rect(TABLE_LEFT, y, TABLE_RIGHT, y + row_height),
                color=None,
                fill=(0.82, 0.82, 0.80),
                fill_opacity=0.75,
            )
        y += row_height
    ink = (0.08, 0.08, 0.1)
    page.draw_line((TABLE_LEFT, top), (TABLE_RIGHT, top), color=ink, width=1.1)
    page.draw_line((TABLE_LEFT, bottom), (TABLE_RIGHT, bottom), color=ink, width=1.1)
    if header:
        page.draw_line(
            (TABLE_LEFT, top + row_height), (TABLE_RIGHT, top + row_height), color=ink, width=0.9
        )
    for index, ratio in enumerate(COLUMNS):
        if vertical_rules or index in (0, len(COLUMNS) - 1):
            page.draw_line((_x(ratio), top), (_x(ratio), bottom), color=ink, width=0.8)
    y = top
    if header:
        for ratio, text in zip(
            COLUMNS, ("Matières", "Notes", "Coeff", "Points", "Sur"), strict=False
        ):
            page.insert_text((_x(ratio) + 5, y + 15), text, fontsize=10, fontname="hebo", color=ink)
        y += row_height
    lines = [(r.label, *r.cells(), r.smudged) for r in rows]
    if total:
        lines.append((*total, False))
    for label, *numbers, smudged in lines:
        if smudged:
            # Illegible handwriting-like scribble instead of the subject label.
            for k in range(7):
                page.draw_line(
                    (_x(0) + 8 + k * 9, y + 6 + (k % 3) * 3),
                    (_x(0) + 20 + k * 9, y + 16 - (k % 2) * 5),
                    color=ink,
                    width=1.4,
                )
        else:
            page.insert_text((_x(0) + 5, y + 15), label, fontsize=10, fontname="helv", color=ink)
        for ratio, text in zip(COLUMNS[1:5], numbers, strict=True):
            page.insert_text((_x(ratio) + 6, y + 15), text, fontsize=10, fontname="helv", color=ink)
        y += row_height
    return bottom


def build_pdf(transcript: Transcript) -> pymupdf.Document:
    doc = pymupdf.open()
    page = doc.new_page(width=595, height=842)
    page.draw_rect(page.rect, color=None, fill=(0.95, 0.95, 0.88))
    for index, x in enumerate(range(0, 595, 70)):
        tint = (0.80, 0.90, 0.62) if index % 2 else (0.95, 0.88, 0.55)
        page.draw_rect(pymupdf.Rect(x, 0, x + 34, 842), color=None, fill=tint, fill_opacity=0.55)
    pivot = pymupdf.Point(80, 470)
    page.insert_text(
        pivot,
        "SPECIMEN SYNTHETIQUE",
        fontsize=64,
        fontname="hebo",
        color=(0.45, 0.62, 0.32),
        fill_opacity=0.28,
        morph=(pivot, pymupdf.Matrix(-38)),
    )
    ink = (0.08, 0.08, 0.1)
    page.insert_text((150, 52), "DOCUMENT SYNTHÉTIQUE DE TEST", fontsize=11, fontname="hebo")
    page.insert_text(
        (95, 74), "Relevé de notes du Baccalauréat de l'enseignement", fontsize=12, fontname="hebo"
    )
    page.insert_text((210, 90), "secondaire général - Session 2025", fontsize=12, fontname="hebo")
    # Series: a large isolated letter at the top left under the header, beside the identity
    # block, without any "Série" label (layout observed on the real transcript, ADR 082).
    if transcript.series:
        page.insert_text((50, 150), transcript.series, fontsize=34, fontname="hebo", color=ink)
    page.insert_text((100, 125), "Nom : CANDIDAT SYNTHETIQUE", fontsize=10, color=ink)
    page.insert_text((100, 141), "Numéro : 000000", fontsize=10, color=ink)
    page.insert_text((100, 157), "Établissement : SYNTHETIQUE", fontsize=10, color=ink)
    page.insert_text((330, 165), "Centre : SYNTHETIQUE", fontsize=10, color=ink)
    page.insert_text((45, 200), "Épreuves écrites du premier groupe", fontsize=12, fontname="hebo")
    rows = transcript.written + transcript.extra_rows
    coefficient = sum(r.coefficient for r in rows)
    points = sum((r.points for r in rows), Decimal(0))
    average = (points / coefficient).quantize(Decimal("0.01"), rounding=ROUND_DOWN)
    total_points = transcript.total_points_override or printed(points)
    total = ("TOTAL", printed(average), str(coefficient), total_points, str(20 * coefficient))
    bottom = _table(
        page, 210, rows, header=True, total=total, vertical_rules=transcript.vertical_rules
    )
    _table(page, bottom + 30, (transcript.eps,), header=False, total=None, vertical_rules=True)
    oral_title = bottom + 90
    page.insert_text(
        (45, oral_title), "Épreuves orales du second groupe", fontsize=12, fontname="hebo"
    )
    _table(
        page,
        oral_title + 10,
        transcript.oral,
        header=True,
        total=None,
        vertical_rules=transcript.vertical_rules,
    )
    page.insert_text((45, 805), "Décision du jury : document synthétique", fontsize=10)
    doc.set_metadata(METADATA)
    return doc


def rasterize(doc: pymupdf.Document, dpi: int = 300) -> np.ndarray:
    pixmap = doc[0].get_pixmap(dpi=dpi, colorspace=pymupdf.csRGB, alpha=False)
    return np.frombuffer(pixmap.samples, np.uint8).reshape(pixmap.height, pixmap.width, 3).copy()


def photograph(rgb: np.ndarray, seed: int = 7) -> np.ndarray:
    """Slight perspective, tilt, uneven light, sensor noise and blur, on a darker table."""
    h, w = rgb.shape[:2]
    margin_x, margin_y = int(w * 0.06), int(h * 0.05)
    canvas_w, canvas_h = w + 2 * margin_x, h + 2 * margin_y
    source = np.float32([[0, 0], [w - 1, 0], [w - 1, h - 1], [0, h - 1]])
    jitter = np.float32(
        [[0.012 * w, 0.004 * h], [-0.004 * w, 0.010 * h], [-0.010 * w, -0.002 * h], [0.003, 0]]
    )
    target = source + np.float32([margin_x, margin_y]) + jitter
    angle = np.deg2rad(1.2)
    center = np.float32([canvas_w / 2, canvas_h / 2])
    rotation = np.float32([[np.cos(angle), -np.sin(angle)], [np.sin(angle), np.cos(angle)]])
    target = (target - center) @ rotation.T + center
    matrix = cv2.getPerspectiveTransform(source, target.astype(np.float32))
    background = np.full((canvas_h, canvas_w, 3), (62, 54, 47), np.uint8)
    warped = cv2.warpPerspective(
        rgb,
        matrix,
        (canvas_w, canvas_h),
        flags=cv2.INTER_LINEAR,
        borderMode=cv2.BORDER_TRANSPARENT,
        dst=background,
    )
    light = np.linspace(0.78, 1.0, canvas_w, dtype=np.float32)[None, :, None]
    light = light * np.linspace(1.0, 0.9, canvas_h, dtype=np.float32)[:, None, None]
    rng = np.random.default_rng(seed)
    noise = rng.normal(0, 4.0, warped.shape).astype(np.float32)
    photo = np.clip(warped.astype(np.float32) * light + noise, 0, 255).astype(np.uint8)
    return cv2.GaussianBlur(photo, (3, 3), 0.6)


def image_pdf(rgb: np.ndarray, *, jpeg: bool) -> bytes:
    ok, encoded = cv2.imencode(
        ".jpg" if jpeg else ".png",
        cv2.cvtColor(rgb, cv2.COLOR_RGB2BGR),
        [cv2.IMWRITE_JPEG_QUALITY, 82] if jpeg else [cv2.IMWRITE_PNG_COMPRESSION, 6],
    )
    if not ok:
        raise RuntimeError("Synthetic image encoding failed")
    h, w = rgb.shape[:2]
    doc = pymupdf.open()
    page = doc.new_page(width=w * 72 / 300, height=h * 72 / 300)
    page.insert_image(page.rect, stream=encoded.tobytes())
    doc.set_metadata(METADATA)
    data = doc.tobytes(deflate=True, no_new_id=True, garbage=3)
    doc.close()
    return data


def fixture_bytes(name: str) -> bytes:
    transcript, _ = variant(name)
    transform = VARIANTS[name][0]
    doc = build_pdf(transcript)
    rgb = rasterize(doc)
    doc.close()
    if transform == "photo":
        return image_pdf(photograph(rgb), jpeg=True)
    return image_pdf(rgb, jpeg=False)


def native_bytes(name: str = "d-scan") -> bytes:
    transcript, _ = variant(name)
    doc = build_pdf(transcript)
    data = doc.tobytes(deflate=True, no_new_id=True, garbage=3)
    doc.close()
    return data
