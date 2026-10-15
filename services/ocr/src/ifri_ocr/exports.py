"""PDF of a ranking list (public or internal), laid out from a document decided by the API.

The API builds every cell (labels, decisions, figures): this module only lays them out. The
job on Redis carries the export identifier alone; the document, which holds names, is fetched
over the private network and never stored by the worker.
"""

import json
from pathlib import Path
from threading import Event
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from ifri_ocr.streams import serve_stream

EXPORT_STREAM = "ifri:exports:v1"
EXPORT_GROUP = "ifri-exports-v1"
# DejaVu Sans covers French and the Latin letters of Beninese names (ɔ, ɛ, Ɖ…).
FONT = Path("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
FONT_BOLD = Path("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf")
NAVY = (30 / 255, 58 / 255, 95 / 255)
MUTED = (71 / 255, 85 / 255, 105 / 255)
STRIPE = (233 / 255, 238 / 255, 245 / 255)
DIVIDER = (203 / 255, 213 / 255, 225 / 255)
MARGIN = 36
MAX_ROWS = 20000


class ExportError(Exception):
    """A definitive failure, reported to the API with a closed code."""


def _strings(value) -> list[str]:
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise ExportError("RENDER_FAILED") from None
    return value


def _check(document: dict) -> tuple[list[str], list[list[str]], list[list[str]], list[str]]:
    if document.get("contractVersion") != 1 or not isinstance(document.get("title"), str):
        raise ExportError("RENDER_FAILED")
    columns = _strings(document.get("columns"))
    rows = document.get("rows") or []
    header = document.get("header") or []
    if not columns or not isinstance(rows, list) or len(rows) > MAX_ROWS:
        raise ExportError("RENDER_FAILED")
    for row in rows:
        if len(_strings(row)) != len(columns):
            raise ExportError("RENDER_FAILED")
    for line in header:
        if len(_strings(line)) != 2:
            raise ExportError("RENDER_FAILED")
    return columns, rows, header, _strings(document.get("notes") or [])


def _wrap(font, text: str, size: float, width: float) -> list[str]:
    """Greedy word wrap; a word longer than the width is cut on characters."""
    lines: list[str] = []
    for paragraph in text.split("\n"):
        line = ""
        for word in paragraph.split(" "):
            candidate = f"{line} {word}" if line else word
            if font.text_length(candidate, fontsize=size) <= width:
                line = candidate
                continue
            if line:
                lines.append(line)
            line = ""
            for char in word:
                if font.text_length(line + char, fontsize=size) > width and line:
                    lines.append(line)
                    line = ""
                line += char
        lines.append(line)
    return lines or [""]


def render_list(document: dict) -> bytes:
    """A4 PDF: title, versioned header, table (header row repeated on each page), notes, footer."""
    import pymupdf

    columns, rows, header, notes = _check(document)
    if not FONT.is_file() or not FONT_BOLD.is_file():
        raise ExportError("RENDER_FAILED")
    regular = pymupdf.Font(fontfile=str(FONT))
    bold = pymupdf.Font(fontfile=str(FONT_BOLD))
    landscape = bool(document.get("landscape"))
    width, height = (842, 595) if landscape else (595, 842)
    size = 7 if landscape else 9
    line_height = size * 1.3
    pad = 3
    available = width - 2 * MARGIN
    # Column widths follow their content, within the page width.
    natural = []
    for index, title in enumerate(columns):
        cells = [bold.text_length(word, fontsize=size) for word in title.split(" ")]
        cells += [regular.text_length(row[index], fontsize=size) for row in rows[:500]]
        natural.append(min(max(cells + [12]), available / 2) + 2 * pad)
    widths = [w * available / sum(natural) for w in natural]

    pdf = pymupdf.open()

    def new_page():
        # A Page object is only valid until the next page is added: always draw on the last one.
        page = pdf.new_page(width=width, height=height)
        page.insert_font(fontname="dv", fontfile=str(FONT))
        page.insert_font(fontname="dvb", fontfile=str(FONT_BOLD))
        return page

    def cell_lines(cells: list[str], font) -> list[list[str]]:
        return [_wrap(font, text, size, widths[i] - 2 * pad) for i, text in enumerate(cells)]

    def draw_row(page, y: float, lines: list[list[str]], fill, color, fontname: str) -> float:
        row_height = max(len(cell) for cell in lines) * line_height + 2 * pad
        x = MARGIN
        page.draw_rect(
            pymupdf.Rect(MARGIN, y, width - MARGIN, y + row_height),
            color=DIVIDER,
            fill=fill,
            width=0.5,
        )
        for i, cell in enumerate(lines):
            for n, text in enumerate(cell):
                baseline = y + pad + size + n * line_height
                page.insert_text(
                    (x + pad, baseline), text, fontname=fontname, fontsize=size, color=color
                )
            x += widths[i]
        return y + row_height

    page = new_page()
    y = MARGIN + 16
    page.insert_text((MARGIN, y), document["title"], fontname="dvb", fontsize=14, color=NAVY)
    y += 14
    for label, value in header:
        for text in _wrap(regular, f"{label} : {value}", 9, available):
            y += 12
            page.insert_text((MARGIN, y), text, fontname="dv", fontsize=9, color=MUTED)
    y += 12
    head = cell_lines(columns, bold)
    y = draw_row(page, y, head, NAVY, (1, 1, 1), "dvb")
    for index, row in enumerate(rows):
        lines = cell_lines(row, regular)
        needed = max(len(cell) for cell in lines) * line_height + 2 * pad
        if y + needed > height - MARGIN - 18:
            page = new_page()
            y = draw_row(page, MARGIN, head, NAVY, (1, 1, 1), "dvb")
        y = draw_row(page, y, lines, STRIPE if index % 2 else None, (0, 0, 0), "dv")
    for note in notes:
        for text in _wrap(regular, note, 8, available):
            if y + 20 > height - MARGIN - 18:
                page = new_page()
                y = MARGIN
            y += 11
            page.insert_text((MARGIN, y + 6), text, fontname="dv", fontsize=8, color=MUTED)
    version = next((value for label, value in header if label == "Liste"), "")
    total = pdf.page_count
    for number in range(1, total + 1):
        page = pdf[number - 1]
        page.insert_font(fontname="dv", fontfile=str(FONT))
        footer = f"{document['title']} — {version} — page {number} / {total}"
        for text in _wrap(regular, footer, 7, available)[:1]:
            page.insert_text(
                (MARGIN, height - MARGIN + 6), text, fontname="dv", fontsize=7, color=MUTED
            )
    pdf.set_metadata(
        {"title": document["title"], "creator": "IFRI Admissions", "producer": "IFRI Admissions"}
    )
    try:
        pdf.subset_fonts()  # embed only the glyphs used: a few kilobytes instead of 800
        return pdf.tobytes(garbage=3, deflate=True)
    finally:
        pdf.close()


class ExportClient:
    """Internal API routes of the export loop (bearer secret, private network)."""

    def __init__(self, base: str, secret: str):
        self.base = base
        self.secret = secret

    def _call(self, path: str, data: bytes | None, content_type: str) -> bytes:
        request = Request(
            f"{self.base}/internal/exports/{path}",
            data=data,
            headers={"Authorization": f"Bearer {self.secret}", "Content-Type": content_type},
        )
        try:
            with urlopen(request, timeout=30) as response:
                return response.read(20 * 1024 * 1024 + 1)
        except HTTPError as error:
            raise RuntimeError(f"INTERNAL_HTTP_{error.code}") from None

    def payload(self, export: str) -> dict:
        return json.loads(self._call(f"{export}/payload", None, "application/json"))

    def document(self, export: str, pdf: bytes) -> None:
        self._call(f"{export}/document", pdf, "application/pdf")

    def failure(self, export: str, code: str) -> None:
        self._call(
            f"{export}/failure", json.dumps({"failureCode": code}).encode(), "application/json"
        )


def process_export(job: dict, api: ExportClient) -> str:
    """Render and upload one export; returns the outcome for logging. Transport errors raise."""
    export = str(job.get("exportId", ""))
    if job.get("contractVersion") != 1 or len(export) != 36:
        return "IGNORED"
    try:
        document = api.payload(export)
    except RuntimeError as error:
        # 404: no longer pending (already rendered, failed or expired): acknowledge.
        if str(error) == "INTERNAL_HTTP_404":
            return "GONE"
        raise
    try:
        pdf = render_list(document)
    except ExportError as error:
        api.failure(export, str(error))
        return str(error)
    except RuntimeError, ValueError, KeyError, TypeError:
        api.failure(export, "RENDER_FAILED")
        return "RENDER_FAILED"
    try:
        api.document(export, pdf)
    except RuntimeError as error:
        if str(error) in {"INTERNAL_HTTP_400", "INTERNAL_HTTP_409", "INTERNAL_HTTP_422"}:
            return "REFUSED"
        raise
    return "READY"


def serve_exports(client, api: ExportClient, consumer: str, stop: Event) -> None:
    """Dedicated loop on the export stream (own Redis connection, own consumer group)."""
    serve_stream(
        client,
        EXPORT_STREAM,
        EXPORT_GROUP,
        consumer,
        stop,
        lambda job: process_export(job, api),
        "Export",
        "export_id",
    )
