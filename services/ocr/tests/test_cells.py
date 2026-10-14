import cv2
import numpy as np
import pytest

from ifri_ocr.cells import binary_v, check_totals, grid_cells, parse_row, read_cell
from ifri_ocr.extraction import failed


def cells(*texts):
    return [{"raw": text, "confidence": 0.1} for text in texts]


def test_black_text_on_saturated_background_uses_value_not_gray():
    rgb = np.full((100, 100, 3), (30, 210, 30), np.uint8)
    rgb[40:60, 40:60] = (10, 10, 10)
    result = binary_v(rgb)
    assert result[0, 0] == 255
    assert result[50, 50] == 0


def test_low_tesseract_confidence_does_not_replace_cell_arithmetic():
    row = parse_row(cells("MATHEMATI0UES", "7,5", "3", "22,5", "60"))
    assert row["state"] == "ACCEPTED"
    assert row["subject"] == "MATHEMATIQUES"
    assert row["ocrConfidences"] == [0.1] * 5


@pytest.mark.parametrize(
    "values",
    [
        ("7", "3", "22", "60"),
        ("7", "3", "21", "80"),
        ("", "3", "21", "60"),
        ("21", "3", "63", "60"),
    ],
)
def test_inconsistent_or_unreadable_cells_need_review(values):
    assert parse_row(cells("ANGLAIS", *values))["state"] == "NEEDS_REVIEW"
    assert failed("NEEDS_REVIEW")["status"] == "LOW_CONFIDENCE"


def test_totals_are_verified_against_all_written_rows():
    rows = [
        parse_row(cells("ANGLAIS", "12", "2", "24", "40")),
        parse_row(cells("FRANCAIS", "15", "3", "45", "60")),
    ]
    total = parse_row(cells("Total des épreuves écrites", "13,80", "5", "69", "100"))
    assert all(check_totals(rows, total).values())
    total["rawCells"][3] = "70"
    assert not check_totals(rows, total)["pointsSum"]


def test_grid_is_reconstructed_without_header_ocr():
    image = np.full((400, 900), 255, np.uint8)
    for x in (20, 500, 590, 680, 770, 880):
        cv2.line(image, (x, 10), (x, 380), 0, 2)
    for y in (10, 80, 380):
        cv2.line(image, (20, y), (880, y), 0, 2)
    for y in (50, 120, 160, 200, 240, 280, 330):
        cv2.putText(image, "MATIERE", (40, y), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    rows, issue = grid_cells(image)
    assert not issue
    assert len(rows) == 7 and all(len(row) == 5 for row in rows)
    assert rows[1][0][1] > 80  # Actual header separator excluded from first data cell.
    assert rows[1][0][3] - rows[1][0][1] == rows[2][0][3] - rows[2][0][1]
    assert grid_cells(np.full((400, 900), 255, np.uint8))[1] == "COLUMN_SEGMENTATION_FAILED"


def test_cell_ocr_enlargement_psm7_and_numeric_whitelist(tmp_path, monkeypatch):
    class Result:
        stdout = b"level\ttext\tconf\n5\t8,5\t32\n"

    def run(command, **kwargs):
        assert command[command.index("--psm") + 1] == "7"
        assert "tessedit_char_whitelist=0123456789,." in command
        image = cv2.imread(command[1], cv2.IMREAD_GRAYSCALE)
        assert image.shape == (90, 120)
        return Result()

    monkeypatch.setattr("ifri_ocr.cells.subprocess.run", run)
    result = read_cell(np.full((20, 30), 255, np.uint8), True, tmp_path / "cell.png", 5)
    assert result["raw"] == "8,5" and result["confidence"] == 0.32
    assert result["seconds"] >= 0


def test_implausible_row_count_stops_before_cell_ocr():
    image = np.full((400, 900), 255, np.uint8)
    for x in (20, 500, 590, 680, 770, 880):
        cv2.line(image, (x, 10), (x, 380), 0, 2)
    for y in (10, 380):
        cv2.line(image, (20, y), (880, y), 0, 2)
    for y in (40, 100, 160):
        cv2.putText(image, "MATIERE", (40, y), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    assert grid_cells(image)[1] == "ROW_SEGMENTATION_FAILED"


def test_unreadable_points_are_optional_but_readable_conflict_blocks():
    row = parse_row(cells("ANGLAIS", "12", "2", "", "40"))
    assert row["state"] == "NEEDS_REVIEW"
    assert row["coefficientStatus"] == "VALIDATED"
    assert row["gradeStatus"] == "NEEDS_REVIEW"
    assert row["checks"]["pointsEqualGradeTimesCoefficient"] is None
    assert parse_row(cells("ANGLAIS", "12", "2", "25", "40"))["state"] == "NEEDS_REVIEW"
    assert parse_row(cells("ANGLAIS", "12", "2", "24", ""))["state"] == "NEEDS_REVIEW"
    total = parse_row(cells("Total", "12", "2", "24", "40"))
    assert check_totals([row], total)["pointsSum"] is False


def test_missing_label_note_separator_uses_outer_border_ratios():
    image = np.full((400, 900), 255, np.uint8)
    for x in (20, 590, 680, 770, 880):  # Missing label/note ruling.
        cv2.line(image, (x, 10), (x, 380), 0, 2)
    cv2.line(image, (20, 10), (880, 10), 0, 2)
    cv2.line(image, (20, 380), (880, 380), 0, 2)
    cv2.line(image, (895, 10), (895, 380), 0, 2)  # Outside-table distractor must be excluded.
    for y in (50, 120, 160, 200, 240, 280, 330):
        cv2.putText(image, "MATIERE", (40, y), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    geometry = {}
    rows, issue = grid_cells(image, geometry=geometry)
    assert not issue
    assert geometry["columnStrategy"] == "PROFILE_RATIOS"
    assert len(rows[0]) == 5
    assert geometry["columnEdges"][-1] < 890
    assert geometry["relativeWidths"][0] == pytest.approx(0.535, abs=0.003)


def test_wrong_note_and_correct_scale_never_accepts_note():
    row = parse_row(cells("ANGLAIS", "18", "2", "", "40"))
    assert row["coefficientStatus"] == "VALIDATED"
    assert row["gradeStatus"] == "NEEDS_REVIEW"
    assert row["state"] == "NEEDS_REVIEW"
    total = parse_row(cells("Total", "12", "2", "24", "40"))
    assert check_totals([row], total)["coefficientSum"] is True
    assert check_totals([row], total)["pointsSum"] is False


@pytest.mark.parametrize("coefficient,scale", [("7", "140"), ("2.5", "50"), ("2", "41")])
def test_coefficient_must_be_integer_1_to_6_with_matching_scale(coefficient, scale):
    row = parse_row(cells("ANGLAIS", "10", coefficient, "", scale))
    assert row["coefficientStatus"] == "NEEDS_REVIEW"


def test_negative_word_confidence_is_not_fabricated_as_zero(tmp_path, monkeypatch):
    class Result:
        stdout = b"level\ttext\tconf\n4\theader\t-1\n5\t8\t-1\n5\t5\t60\n"

    monkeypatch.setattr("ifri_ocr.cells.subprocess.run", lambda *args, **kwargs: Result())
    result = read_cell(np.full((20, 30), 255, np.uint8), True, tmp_path / "cell.png", 5)
    assert result["raw"] == "8 5"
    assert result["confidence"] == 0.6
    assert result["ignoredNegativeConfidenceWords"] == 1
    assert result["confidenceSource"] == "SAME_TSV_LEVEL_5"


def test_first_table_gap_excludes_following_eps_table_without_fixed_subject_count():
    from ifri_ocr.cells import first_table_bands

    bounds = [
        (51, 68),
        (147, 174),
        (203, 228),
        (261, 290),
        (321, 349),
        (380, 407),
        (438, 467),
        (498, 523),
        (556, 581),
        (692, 713),
        (784, 813),
        (848, 871),
    ]
    bands = [list(range(a, b + 1)) for a, b in bounds]
    first, split, median = first_table_bands(bands)
    assert len(first) == 9  # Header + variable data, through written Total only.
    assert first[-1][-1] == 581
    assert split == 9 and median == 33
    shorter = [
        list(range(a, b + 1))
        for a, b in [(10, 25), (60, 80), (105, 125), (150, 170), (195, 215), (300, 320), (350, 370)]
    ]
    assert len(first_table_bands(shorter)[0]) == 5


def test_two_tables_segmentation_never_crops_following_table():
    image = np.full((800, 900), 255, np.uint8)
    for x in (20, 500, 590, 680, 770, 880):
        cv2.line(image, (x, 10), (x, 780), 0, 2)
    for y in (10, 80, 400, 520, 580, 780):
        cv2.line(image, (20, y), (880, y), 0, 2)
    for y in (50, 120, 160, 200, 240, 280, 330, 380, 550, 630, 710):
        cv2.putText(image, "MATIERE", (40, y), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    geometry = {}
    rows, issue = grid_cells(image, geometry=geometry)
    assert not issue
    assert geometry["dataRowCount"] == 7
    assert max(cell[3] for row in rows for cell in row) < 450


@pytest.mark.parametrize(
    "raw,expected", [("18.", "18"), ("80 .", "80"), ("9,61", "9.61"), (".", None), ("1 8", None)]
)
def test_numeric_cell_terminal_separator(raw, expected):
    from ifri_ocr.extraction import number

    result = number(raw)
    assert (str(result) if result is not None else None) == expected


def test_coefficient_validation_survives_missing_note():
    row = parse_row(cells("MATHEMATIQUES", "", "4", "", "80"))
    assert row["checks"]["scaleEqual20TimesCoefficient"] is True
    assert row["coefficientStatus"] == "VALIDATED"
    assert row["gradeStatus"] == "NEEDS_REVIEW"


def test_document_totals_validate_derived_notes_and_keep_direct_divergence():
    from ifri_ocr.cells import validate_document_rows

    rows = [
        parse_row(cells("ANGLAIS", "18", "2", "24", "40")),
        parse_row(cells("FRANCAIS", "", "3", "45", "60")),
    ]
    total = parse_row(cells("Total", "13,80", "5", "69", "100"))
    checks, valid = validate_document_rows(rows, total)
    assert valid and all(checks.values())
    assert [r["parsed"][0] for r in rows] == ["12", "15"]
    assert all(r["gradeStatus"] == "VALIDATED_BY_TOTALS" for r in rows)
    assert rows[0]["directGradeValue"] == "18" and rows[0]["gradeDivergence"]
    assert rows[0]["rawCells"][1] == "18"
    rows = [
        parse_row(cells("ANGLAIS", "18", "2", "25", "40")),
        parse_row(cells("FRANCAIS", "15", "3", "45", "60")),
    ]
    _, valid = validate_document_rows(rows, total)
    assert not valid
    assert all(r["state"] == "NEEDS_REVIEW" for r in rows)


def test_remove_cell_rules_preserves_short_number_strokes():
    from ifri_ocr.cells import remove_cell_rules

    binary = np.full((100, 200), 255, np.uint8)
    cv2.line(binary, (2, 0), (2, 99), 0, 1)
    cv2.line(binary, (0, 2), (199, 2), 0, 1)
    cv2.putText(binary, "18", (60, 65), cv2.FONT_HERSHEY_SIMPLEX, 1, 0, 2)
    clean = remove_cell_rules(binary)
    assert np.mean(clean[:, 2]) > 240
    assert np.count_nonzero(clean[35:70, 60:105] < 128) > 40
