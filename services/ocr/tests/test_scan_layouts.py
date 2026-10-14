from decimal import Decimal

import cv2
import numpy as np
import pymupdf
import pytest

from ifri_ocr.extraction import Token, analyze, locate
from ifri_ocr.layouts import select_section
from ifri_ocr.preprocessing import prepare, variant
from ifri_ocr.scans import ScanSettings, choose_pass


def expected():
    # Parser fixture, not an eligibility rule: these IDs are injected exactly as by a job.
    return [
        {"id": code, "name": name, "position": i + 1}
        for i, (code, name) in enumerate(
            [("MATH", "Mathématiques"), ("FRENCH", "Français"), ("ENGLISH", "Anglais")]
        )
    ]


def bac_tokens():
    tokens = []

    def row(y, cells):
        for x, text in cells:
            tokens.append(Token(text, (x, y, x + 0.04, y + 0.015)))

    row(
        0.1, [(0.1, "Épreuves"), (0.2, "écrites"), (0.3, "du"), (0.35, "premier"), (0.45, "groupe")]
    )
    row(0.15, [(0.1, "Matière"), (0.6, "Note"), (0.72, "Coeff"), (0.83, "Points"), (0.94, "Sur")])
    for y, subject, g, c, points, scale in [
        (0.2, "ANGLAIS", "11", "2", "22", "40"),
        (0.25, "FRANCAIS", "9", "2", "18", "40"),
        (0.3, "MATHEMATI0UES", "6", "4", "24", "80"),
    ]:
        row(y, [(0.1, subject), (0.6, g), (0.72, c), (0.83, points), (0.94, scale)])
    row(0.35, [(0.1, "Total des épreuves écrites"), (0.6, "10"), (0.72, "8")])
    row(
        0.4, [(0.1, "Épreuves"), (0.2, "orales"), (0.3, "du"), (0.35, "deuxième"), (0.45, "groupe")]
    )
    row(0.45, [(0.1, "Matière"), (0.6, "Note"), (0.72, "Coeff")])
    row(0.5, [(0.1, "ANGLAIS"), (0.6, "17"), (0.72, "2")])
    row(0.55, [(0.1, "FRANCAIS"), (0.6, "14"), (0.72, "2")])
    return tokens


def test_written_section_wins_over_duplicated_oral_subjects_and_totals():
    result, issue = locate(bac_tokens(), expected(), 1, 0.85)
    assert not issue
    assert [
        (s["subjectId"], Decimal(s["gradeValue"]), Decimal(s["coefficientValue"])) for s in result
    ] == [
        ("MATH", Decimal(6), Decimal(4)),
        ("FRENCH", Decimal(9), Decimal(2)),
        ("ENGLISH", Decimal(11), Decimal(2)),
    ]
    assert all(s["evidence"]["section"] == "FIRST_GROUP_WRITTEN" for s in result)


def test_points_and_scale_are_secondary_checks_never_sources():
    tokens = bac_tokens()
    altered = [Token("23" if t.text == "22" else t.text, t.box) for t in tokens]
    assert locate(altered, expected(), 1, 0.85)[1] == "LOW_CONFIDENCE"
    missing = [t for t in tokens if not (t.text == "2" and t.box[1] == 0.2 and t.box[0] == 0.72)]
    assert locate(missing, expected(), 1, 0.85)[1] == "COEFFICIENT_NOT_FOUND"


def test_known_profile_without_written_section_does_not_use_other_table():
    tokens = [Token("Baccalauréat général", (0.1, 0.05, 0.5, 0.08))]
    assert select_section(tokens)[2] == "LOW_CONFIDENCE"
    # An oral-only continuation page contributes no written grades.
    assert select_section([t for t in bac_tokens() if t.box[1] >= 0.4])[1] == []


@pytest.mark.parametrize(
    "name", ["grayscale", "clahe", "adaptive", "otsu", "color-channel", "background-normalized"]
)
def test_security_background_variants_preserve_image_geometry(name):
    rgb = np.zeros((400, 500, 3), np.uint8)
    rgb[:] = [180, 220, 90]
    cv2.putText(rgb, "SYNTHETIC", (30, 160), cv2.FONT_HERSHEY_SIMPLEX, 1, (20, 20, 20), 2)
    scan = prepare(rgb)
    output = variant(scan, name)
    assert output.shape == (400, 500)
    assert output.dtype == np.uint8
    assert output.max() > output.min()
    assert scan.original_box({"x0": 0.1, "y0": 0.2, "x1": 0.3, "y1": 0.4}) == pytest.approx(
        {"x0": 0.1, "y0": 0.2, "x1": 0.3, "y1": 0.4}
    )


def test_credible_deskew_preserves_original_coordinates():
    rgb = np.full((600, 800, 3), 255, np.uint8)
    for y in range(100, 500, 50):
        cv2.line(rgb, (80, y), (700, y), (0, 0, 0), 2)
    rotation = cv2.getRotationMatrix2D((400, 300), 2, 1)
    skewed = cv2.warpAffine(rgb, rotation, (800, 600), borderValue=(255, 255, 255))
    scan = prepare(skewed)
    assert 1.5 < abs(scan.angle) < 2.5
    bbox = scan.original_box({"x0": 0.2, "y0": 0.2, "x1": 0.5, "y1": 0.3})
    assert 0 <= bbox["x0"] < bbox["x1"] <= 1
    assert bbox["x0"] != pytest.approx(0.2)


def test_perspective_is_flagged_instead_of_fabricating_values():
    rgb = np.full((600, 800, 3), 255, np.uint8)
    for index, y in enumerate(range(100, 500, 50)):
        cv2.line(rgb, (80, y), (700, y + index * 8), (0, 0, 0), 2)
    assert prepare(rgb).perspective


def test_conflicting_ocr_passes_are_not_arbitrarily_selected():
    subjects, issue = locate(bac_tokens(), expected(), 1, 0.85)
    other = [{**s, "gradeValue": "12"} for s in subjects]
    assert choose_pass([(subjects, issue), (other, "")], 0.85) == ([], "AMBIGUOUS_ROW")


def test_scan_settings_are_bounded(monkeypatch):
    monkeypatch.setenv("OCR_SCAN_DPI", "500")
    with pytest.raises(ValueError):
        ScanSettings.load()


def test_native_multpage_written_section_and_oral_are_isolated(tmp_path):
    doc = pymupdf.open()
    doc.new_page()  # empty cover: real scan path is mocked, no identities
    page = doc.new_page()
    for token in bac_tokens():
        page.insert_text((token.box[0] * 595, token.box[1] * 842), token.text, fontsize=7)
    path = tmp_path / "multipage.pdf"
    doc.save(path)
    doc.close()

    class CoverEngine:
        name = "synthetic"
        version = "1"

        def extract(self, *args):
            return []

    # Empty unrelated cover must not override a complete reliable native table.
    result = analyze(path, expected(), CoverEngine())
    assert result["status"] == "SUCCEEDED"
    assert all(s["pageNumber"] == 2 for s in result["subjects"])


@pytest.mark.parametrize("extension", [".png", ".jpg"])
def test_image_converted_to_pdf_uses_scan_pipeline(extension, tmp_path):
    rgb = np.full((1000, 700, 3), 230, np.uint8)
    cv2.putText(rgb, "SYNTHETIC", (30, 200), cv2.FONT_HERSHEY_SIMPLEX, 1, (0, 0, 0), 2)
    ok, encoded = cv2.imencode(extension, rgb)
    assert ok
    doc = pymupdf.open()
    page = doc.new_page()
    page.insert_image(page.rect, stream=encoded.tobytes())
    assert page.get_text("words") == []
    pdf = tmp_path / "converted.pdf"
    doc.save(pdf)
    doc.close()

    class Engine:
        name = "synthetic-word-engine"
        version = "1"
        calls = 0

        def extract(self, *args):
            self.calls += 1
            return bac_tokens()

    engine = Engine()
    diagnostics = []
    result = analyze(pdf, expected(), engine, diagnostics=diagnostics)
    assert result["status"] == "SUCCEEDED"
    assert engine.calls == 2
    assert all(p["imageOnly"] for p in diagnostics)
    assert all(s["evidence"]["dpi"] == 300 for s in result["subjects"])


def test_section_crop_evidence_is_mapped_back_to_original_page(tmp_path):
    from ifri_ocr.layouts import BAC_GENERAL

    tokens = bac_tokens()
    _, selected, _ = select_section(tokens)

    class Engine:
        name = "synthetic"
        version = "1"
        calls = 0

        def extract(self, *args):
            self.calls += 1
            if self.calls == 1:
                return [Token(t.text, t.box, 0.9) for t in tokens]
            # The actual second pass OCR sees only the cropped written table.
            return [
                Token(
                    t.text,
                    (
                        (t.box[0] - 0.075) / 0.925,
                        (t.box[1] - 0.085) / 0.265,
                        (t.box[2] - 0.075) / 0.925,
                        (t.box[3] - 0.085) / 0.265,
                    ),
                    0.99,
                )
                for t in selected
            ]

    doc = pymupdf.open()
    doc.new_page()
    path = tmp_path / "crop.pdf"
    doc.save(path)
    doc.close()
    diagnostics = []
    result = analyze(path, expected(), Engine(), diagnostics=diagnostics)
    assert result["status"] == "SUCCEEDED"
    assert diagnostics[1]["sectionCrop"]
    assert result["subjects"][0]["evidence"]["grade"]["x0"] == pytest.approx(0.6, abs=0.001)
    assert result["subjects"][0]["evidence"]["grade"]["y0"] == pytest.approx(0.3, abs=0.001)
    assert result["subjects"][0]["evidence"]["profile"] == BAC_GENERAL.name


def test_multiline_section_anchor_and_column_headers():
    tokens = bac_tokens()
    adapted = []
    for token in tokens:
        x0, y0, x1, y1 = token.box
        if y0 == 0.1 and token.text in {"du", "premier", "groupe"}:
            y0 += 0.02
            y1 += 0.02
        if y0 == 0.15 and token.text in {"Note", "Coeff"}:
            y0 += 0.017
            y1 += 0.017
        adapted.append(Token(token.text, (x0, y0, x1, y1), token.confidence))
    subjects, issue = locate(adapted, expected(), 1, 0.85)
    assert not issue
    assert [s["gradeValue"] for s in subjects] == ["6", "9", "11"]


def test_security_glyph_does_not_change_written_anchor_or_table_rows():
    tokens = bac_tokens() + [Token("SECURITY", (0.12, 0.02, 0.55, 0.195))]
    profile, section, issue = select_section(tokens)
    assert not issue
    assert profile.bounds[0] == pytest.approx(0.115)
    assert all(t.text != "SECURITY" for t in section)
    subjects, issue = locate(tokens, expected(), 1, 0.85)
    assert not issue
    assert [s["gradeValue"] for s in subjects] == ["6", "9", "11"]


def test_written_total_ends_table_before_unlabelled_sports_table():
    tokens = [t for t in bac_tokens() if t.box[1] < 0.4]
    tokens.extend(
        [Token("Matière", (0.1, 0.42, 0.2, 0.44)), Token("ANGLAIS", (0.1, 0.46, 0.2, 0.48))]
    )
    profile, selected, issue = select_section(tokens)
    assert not issue
    assert profile.bounds == pytest.approx((0.115, 0.35))
    assert all(t.box[3] <= 0.35 for t in selected)


def test_column_edges_require_long_separated_vertical_grid_lines():
    from ifri_ocr.preprocessing import table_column_edges

    image = np.full((400, 600), 255, np.uint8)
    assert table_column_edges(image) == []
    for x in (20, 300, 380, 450, 580):
        cv2.line(image, (x, 20), (x, 380), 0, 2)
    cv2.putText(image, "SYNTHETIC", (35, 150), cv2.FONT_HERSHEY_SIMPLEX, 1, 0, 2)
    assert table_column_edges(image) == [20, 300, 380, 450, 580]
