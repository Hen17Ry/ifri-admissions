"""Spatial extraction; expected subjects are supplied by the Go reference snapshot."""

import csv
import io
import re
import subprocess
import unicodedata
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation, localcontext
from pathlib import Path
from statistics import median
from typing import Protocol

from rapidfuzz.fuzz import ratio


@dataclass(frozen=True)
class Token:
    text: str
    box: tuple[float, float, float, float]
    confidence: float = 1.0
    line_id: tuple[int, int, int] | None = None


def normalize(text: str) -> str:
    text = unicodedata.normalize("NFKD", text.casefold())
    return " ".join(
        re.sub(r"[^a-z0-9]+", " ", "".join(c for c in text if not unicodedata.combining(c))).split()
    )


def number(text: str) -> Decimal | None:
    text = text.strip()
    text = re.sub(r"\s+([.,])", r"\1", text)
    if re.fullmatch(r"[0-9]{1,64}(?:[.,][0-9]{1,32})?[.,]", text):
        text = text[:-1]
    if not re.fullmatch(r"[0-9]{1,64}(?:[.,][0-9]{1,32})?", text):
        return None
    try:
        return Decimal(text.replace(",", "."))
    except InvalidOperation:
        return None


class OcrEngine(Protocol):
    name: str
    version: str

    def extract(self, image: Path, width: int, height: int, timeout: int) -> list[Token]: ...


class Tesseract:
    name = "tesseract"

    def __init__(self, psm: int = 6):
        if psm not in {6, 11}:
            raise ValueError("Unsupported page segmentation mode")
        self.psm = psm

    @property
    def version(self) -> str:
        result = subprocess.run(
            ["tesseract", "--version"], capture_output=True, check=True, timeout=5
        )
        return result.stdout.decode().splitlines()[0][:100]

    def extract(self, image: Path, width: int, height: int, timeout: int) -> list[Token]:
        result = subprocess.run(
            ["tesseract", str(image), "stdout", "-l", "fra+eng", "--psm", str(self.psm), "tsv"],
            capture_output=True,
            timeout=timeout,
            check=True,
        )
        tokens = []
        for row in csv.DictReader(io.StringIO(result.stdout.decode("utf-8")), delimiter="\t"):
            if row["level"] != "5" or not row["text"].strip():
                continue
            x, y, w, h = (int(row[k]) for k in ("left", "top", "width", "height"))
            tokens.append(
                Token(
                    row["text"],
                    (x / width, y / height, (x + w) / width, (y + h) / height),
                    max(0, float(row["conf"]) / 100),
                    tuple(int(row[key]) for key in ("block_num", "par_num", "line_num"))
                    if self.psm == 6
                    else None,
                )
            )
        return tokens


def text_scale_tokens(tokens: list[Token]) -> list[Token]:
    heights = [t.box[3] - t.box[1] for t in tokens]
    if len(heights) < 8:
        return tokens
    typical_height = median(heights)
    return [t for t in tokens if t.box[3] - t.box[1] <= typical_height * 2.5]


def lines(tokens: list[Token]) -> list[list[Token]]:
    if tokens and all(token.line_id is not None for token in tokens):
        groups: dict[tuple[int, int, int], list[Token]] = {}
        for token in tokens:
            groups.setdefault(token.line_id, []).append(token)
        return [
            sorted(row, key=lambda token: token.box[0])
            for row in sorted(groups.values(), key=lambda row: min(token.box[1] for token in row))
        ]
    rows: list[list[Token]] = []
    for token in sorted(tokens, key=lambda t: ((t.box[1] + t.box[3]) / 2, t.box[0])):
        middle = (token.box[1] + token.box[3]) / 2
        row = next(
            (
                r
                for r in rows
                if abs(middle - (r[0].box[1] + r[0].box[3]) / 2)
                <= min(token.box[3] - token.box[1], r[0].box[3] - r[0].box[1]) * 0.55
            ),
            None,
        )
        if row is None:
            rows.append([token])
        else:
            row.append(token)
    return [sorted(row, key=lambda t: t.box[0]) for row in rows]


def box(tokens: list[Token]) -> dict[str, float]:
    return dict(
        zip(
            ("x0", "y0", "x1", "y1"),
            (
                min(t.box[0] for t in tokens),
                min(t.box[1] for t in tokens),
                max(t.box[2] for t in tokens),
                max(t.box[3] for t in tokens),
            ),
            strict=True,
        )
    )


def locate(
    tokens: list[Token], expected: list[dict], page: int, minimum: float, profile_hint=None
) -> tuple[list[dict], str]:
    from ifri_ocr.layouts import select_section

    profile, tokens, section_issue = select_section(tokens)
    if section_issue:
        return [], section_issue
    if profile_hint is not None and profile.name == "UNKNOWN_GENERIC":
        profile = profile_hint
    # Large security glyphs are not table words. Keep normal labels/numbers unchanged.
    tokens = text_scale_tokens(tokens)
    rows = lines(tokens)
    candidates: dict[str, list[dict]] = {s["id"]: [] for s in expected}
    columns = None
    arithmetic_columns = None
    for index, row in enumerate(rows):
        header_row = row
        names = {normalize(t.text) for t in row}
        complete_header = (
            names & {"matiere", "matieres", "discipline"}
            and names & {"note", "notes", "moyenne", "moy"}
            and names & {"coef", "coeff", "coefficient"}
        )
        if not complete_header and index + 1 < len(rows):
            following = rows[index + 1]
            gap = min(t.box[1] for t in following) - max(t.box[3] for t in row)
            height = max(t.box[3] - t.box[1] for t in row)
            if 0 <= gap <= height * 1.5:
                header_row = row + following
        headers = {normalize(t.text): t for t in header_row}
        grade = next(
            (headers[k] for k in ("note", "notes", "moyenne", "moy") if k in headers), None
        )
        coef = next((headers[k] for k in ("coef", "coeff", "coefficient") if k in headers), None)
        subject = next(
            (headers[k] for k in ("matiere", "matieres", "discipline") if k in headers), None
        )
        if grade and coef and subject and subject.box[0] < grade.box[0] < coef.box[0]:

            def center(t):
                return (t.box[0] + t.box[2]) / 2

            gx, cx = center(grade), center(coef)
            right_headers = sorted(
                [t for t in header_row if t.box[0] > coef.box[2]], key=lambda t: t.box[0]
            )
            right = center(right_headers[0]) if right_headers else None
            columns = (gx - (cx - gx) / 2, (gx + cx) / 2, (cx + right) / 2 if right else 1.0)
            points = next((t for t in right_headers if normalize(t.text) == "points"), None)
            scale = next((t for t in right_headers if normalize(t.text) == "sur"), None)
            arithmetic_columns = (
                ((center(points) + center(scale)) / 2, center(scale)) if points and scale else None
            )
            continue
        if columns is None:
            continue
        left, split, end = columns
        # Cells are determined by header centers, not by values or a document template.
        subject_tokens = [t for t in row if (t.box[0] + t.box[2]) / 2 < left]
        grade_tokens = [t for t in row if left <= (t.box[0] + t.box[2]) / 2 < split]
        coef_tokens = [t for t in row if split <= (t.box[0] + t.box[2]) / 2 < end]
        text = " ".join(t.text for t in subject_tokens)
        if not text or normalize(text).startswith("total"):
            continue
        scores = sorted(
            (
                (
                    ratio(
                        normalize(text)
                        .replace("mathemati0ues", "mathematiques")
                        .replace("anglals", "anglais"),
                        normalize(s["name"]),
                    )
                    / 100,
                    s,
                )
                for s in expected
            ),
            key=lambda p: p[0],
            reverse=True,
        )
        match, s = scores[0]
        if match < minimum:
            continue
        # A below-threshold subject is not a viable competing match.
        if len(scores) > 1 and scores[1][0] >= minimum and scores[0][0] - scores[1][0] < 0.08:
            return [], "AMBIGUOUS_ROW"
        # Exactly one token per numeric cell; multiple values are not guessed.
        if len(grade_tokens) != 1:
            return [], "GRADE_NOT_FOUND" if not grade_tokens else "AMBIGUOUS_ROW"
        if len(coef_tokens) != 1:
            return [], "COEFFICIENT_NOT_FOUND" if not coef_tokens else "AMBIGUOUS_ROW"
        g, c = number(grade_tokens[0].text), number(coef_tokens[0].text)
        if g is None or not 0 <= g <= 20:
            return [], "GRADE_NOT_FOUND"
        if c is None or c <= 0:
            return [], "COEFFICIENT_NOT_FOUND"
        if arithmetic_columns:
            boundary, _ = arithmetic_columns
            point_tokens = [t for t in row if end <= (t.box[0] + t.box[2]) / 2 < boundary]
            scale_tokens = [t for t in row if (t.box[0] + t.box[2]) / 2 >= boundary]
            if len(point_tokens) == len(scale_tokens) == 1:
                points, scale = number(point_tokens[0].text), number(scale_tokens[0].text)
                with localcontext() as context:
                    context.prec = 200
                    if (
                        points is not None
                        and scale is not None
                        and (
                            abs(g * c - points) > Decimal("0.05")
                            or abs(20 * c - scale) > Decimal("0.05")
                        )
                    ):
                        return [], "LOW_CONFIDENCE"
        sc = min(match, *(t.confidence for t in subject_tokens))
        gc, cc = grade_tokens[0].confidence, coef_tokens[0].confidence
        overall = min(sc, gc, cc)
        candidates[s["id"]].append(
            {
                "subjectId": s["id"],
                "position": s["position"],
                "detectedSubjectText": text,
                "gradeRaw": grade_tokens[0].text,
                "gradeValue": format(g, "f"),
                "coefficientRaw": coef_tokens[0].text,
                "coefficientValue": format(c, "f"),
                "pageNumber": page,
                "subjectConfidence": sc,
                "gradeConfidence": gc,
                "coefficientConfidence": cc,
                "overallConfidence": overall,
                "evidence": {
                    "subject": box(subject_tokens),
                    "grade": box(grade_tokens),
                    "coefficient": box(coef_tokens),
                    "system": "NORMALIZED_TOP_LEFT",
                    "profile": profile.name,
                    "section": profile.section or "GENERIC_TABLE",
                },
            }
        )
    if any(len(v) > 1 for v in candidates.values()):
        return [], "AMBIGUOUS_ROW"
    found = [v[0] for v in candidates.values() if v]
    return found, "" if len(found) == len(expected) else "SUBJECT_NOT_FOUND"


def analyze(
    path: Path,
    expected: list[dict],
    engine: OcrEngine,
    minimum: float = 0.85,
    page_timeout: int = 30,
    max_pages: int = 10,
    diagnostics: list | None = None,
) -> dict:
    import pymupdf

    all_subjects = []
    used_ocr = False
    failure = ""
    with pymupdf.open(path) as doc:
        page_count = len(doc)
        if doc.is_encrypted or not 1 <= len(doc) <= max_pages:
            return failed("PDF_UNREADABLE")
        for index, page in enumerate(doc):
            width, height = page.rect.width, page.rect.height
            tokens = []
            for word in page.get_text("words"):
                rect = pymupdf.Rect(word[:4]) * page.rotation_matrix
                tokens.append(
                    Token(
                        word[4],
                        (rect.x0 / width, rect.y0 / height, rect.x1 / width, rect.y1 / height),
                    )
                )
            names = {normalize(token.text) for token in tokens}
            usable_headers = bool(names & {"note", "notes", "moy", "moyenne"}) and bool(
                names & {"coef", "coeff", "coefficient"}
            )
            native_subjects, native_issue = locate(tokens, expected, index + 1, minimum)
            if (
                len(tokens) < 10
                or not usable_headers
                or (not native_subjects and native_issue == "SUBJECT_NOT_FOUND")
            ):
                from ifri_ocr.scans import extract_scan

                subjects, issue = extract_scan(
                    page, expected, engine, minimum, page_timeout, index + 1, diagnostics
                )
                used_ocr = True
            else:
                subjects, issue = native_subjects, native_issue
            all_subjects.extend(subjects)
            if issue and issue != "SUBJECT_NOT_FOUND":
                failure = issue
    ids = [s["subjectId"] for s in all_subjects]
    if len(ids) != len(set(ids)):
        failure = "AMBIGUOUS_ROW"
    if failure:
        return failed(failure)
    if set(ids) != {s["id"] for s in expected}:
        return failed("LOW_CONFIDENCE" if used_ocr else "SUBJECT_NOT_FOUND")
    if any(
        s["overallConfidence"] < minimum and s["evidence"].get("validation") != "CELL_ARITHMETIC"
        for s in all_subjects
    ):
        return failed("LOW_CONFIDENCE")
    return {
        "contractVersion": 1,
        "pageCount": page_count,
        "status": "SUCCEEDED",
        "engine": engine.name if used_ocr else "pymupdf",
        "engineVersion": (engine.version if used_ocr else "pymupdf " + pymupdf.VersionBind),
        "failureCode": "",
        "subjects": sorted(all_subjects, key=lambda s: s["position"]),
    }


def failed(code: str) -> dict:
    return {
        "contractVersion": 1,
        "status": "LOW_CONFIDENCE"
        if code
        in {
            "AMBIGUOUS_ROW",
            "LOW_CONFIDENCE",
            "NEEDS_REVIEW",
            "ROW_SEGMENTATION_FAILED",
            "COLUMN_SEGMENTATION_FAILED",
        }
        else "FAILED",
        "engine": "hybrid",
        "engineVersion": "1",
        "failureCode": code,
        "subjects": [],
    }
