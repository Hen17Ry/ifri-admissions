import shutil
from decimal import Decimal
from pathlib import Path

import cv2
import numpy as np
import pytest

from ifri_ocr.bac.arithmetic import (
    RECONSTRUCTED,
    coefficient_validated,
    evaluate,
    parse_numbers,
)
from ifri_ocr.bac.assignment import (
    assign,
    hungarian,
    total_score,
    vocabulary_word,
    word_scores,
)
from ifri_ocr.bac.geometry import binary_v, first_table_bands, grid_cells
from ifri_ocr.bac.pipeline import (
    _looks_like_data,
    _read_series,
    series_candidates,
    series_mismatch,
    series_zone,
)
from ifri_ocr.bac.reading import (
    CellJob,
    cell_workers,
    read_cells,
    remove_rules,
    tesseract_line,
    text_line,
    trim_to_ink,
)
from ifri_ocr.extraction import failed, number

EXPECTED = [
    {"id": "MATH", "code": "MATH", "name": "Mathématiques", "position": 1},
    {"id": "FRENCH", "code": "FRENCH", "name": "Français", "position": 2},
    {"id": "ENGLISH", "code": "ENGLISH", "name": "Anglais", "position": 3},
]


def row(label, *numbers):
    parsed = parse_numbers(list(numbers))
    parsed["wordScores"] = word_scores([label])
    parsed["totalScore"] = total_score([label])
    return parsed


# --- geometry -------------------------------------------------------------------------------


def table_image(labels, *, header_rule=True, height=420):
    image = np.full((height, 900), 255, np.uint8)
    for x in (20, 500, 590, 680, 770, 880):
        cv2.line(image, (x, 10), (x, height - 20), 0, 2)
    rules = (10, 80, height - 20) if header_rule else (10, height - 20)
    for y in rules:
        cv2.line(image, (20, y), (880, y), 0, 2)
    cv2.putText(image, "MATIERE", (40, 50), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    for index, label in enumerate(labels):
        y = 120 + index * 40
        cv2.putText(image, label, (40, y), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    return image


def test_black_text_on_saturated_background_uses_value_not_gray():
    rgb = np.full((100, 100, 3), (30, 210, 30), np.uint8)
    rgb[40:60, 40:60] = (10, 10, 10)
    result = binary_v(rgb)
    assert result[0, 0] == 255
    assert result[50, 50] == 0


def test_grid_is_reconstructed_without_header_ocr():
    rows, issue = grid_cells(table_image(["MATIERE"] * 6))
    assert not issue
    assert len(rows) == 7 and all(len(r) == 5 for r in rows)
    assert rows[1][0][1] > 80  # Actual header separator excluded from first data cell.
    assert rows[1][0][3] - rows[1][0][1] == rows[2][0][3] - rows[2][0][1]
    assert grid_cells(np.full((400, 900), 255, np.uint8))[1] == "COLUMN_SEGMENTATION_FAILED"


def test_header_plus_one_subject_plus_total_is_enough():
    rows, issue = grid_cells(table_image(["ANGLAIS", "TOTAL"]))
    assert not issue and len(rows) == 3


def test_header_and_a_single_band_cannot_hold_subject_and_total():
    assert grid_cells(table_image(["TOTAL"]))[1] == "ROW_SEGMENTATION_FAILED"


def test_short_labels_next_to_a_long_label_are_rows():
    labels = ["Anglais", "SCIENCES DE LA VIE ET DE LA TERRE ET TECHNO", "Fr", "TOTAL"]
    rows, issue = grid_cells(table_image(labels))
    assert not issue and len(rows) == 1 + len(labels)


def test_title_ink_above_content_top_is_not_a_row():
    image = table_image(["ANGLAIS", "FRANCAIS", "TOTAL"], header_rule=False)
    cv2.putText(image, "EPREUVES", (40, 4), cv2.FONT_HERSHEY_SIMPLEX, 0.6, 0, 1)
    geometry = {}
    rows, issue = grid_cells(image, geometry=geometry, content_top=6)
    assert not issue
    assert geometry["rowBands"][0][0] > 6


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
    assert geometry["columnEdges"][-1] < 890
    assert geometry["relativeWidths"][0] == pytest.approx(0.535, abs=0.003)


def test_first_table_gap_excludes_following_eps_table_without_fixed_subject_count():
    bounds = [(51, 68), (147, 174), (203, 228), (261, 290), (321, 349), (380, 407), (438, 467)]
    bounds += [(498, 523), (556, 581), (692, 713), (784, 813), (848, 871)]
    first, split, median = first_table_bands([list(range(a, b + 1)) for a, b in bounds])
    assert len(first) == 9 and first[-1][-1] == 581
    assert split == 9 and median == 33


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
    assert max(cell[3] for r in rows for cell in r) < 450


# --- cell images and reads -------------------------------------------------------------------


def digits(text="18", width=220, height=70, rule_x=None):
    image = np.full((height, width), 255, np.uint8)
    cv2.putText(image, text, (90, 48), cv2.FONT_HERSHEY_SIMPLEX, 1, 0, 2)
    if rule_x is not None:
        cv2.line(image, (rule_x, 0), (rule_x, height - 1), 0, 2)
    return image


def test_rules_are_removed_but_digit_strokes_kept():
    binary = digits(rule_x=3)
    cv2.line(binary, (0, 2), (219, 2), 0, 1)
    clean = remove_rules(binary)
    assert np.mean(clean[:, 3]) > 240
    assert np.count_nonzero(clean[20:55, 85:140] < 128) > 40


def test_text_line_drops_edge_rule_fragment_and_stray_specks():
    image = digits("9,61", rule_x=None)
    cv2.line(image, (2, 15), (2, 55), 0, 2)  # Ruling fragment hugging the left edge.
    cv2.circle(image, (200, 60), 2, 0, -1)  # Speck outside the number.
    line = text_line(image)
    assert line is not None
    ink_columns = np.flatnonzero(np.count_nonzero(line < 128, axis=0))
    assert ink_columns[0] >= 6 and line.shape[1] < 140  # Fragment and speck removed.
    assert text_line(np.full((60, 200), 255, np.uint8)) is None
    assert trim_to_ink(np.full((60, 200), 255, np.uint8)) is None


def test_cell_read_uses_stdin_psm7_whitelist_and_normalized_glyph_height(monkeypatch):
    calls = []

    class Result:
        stdout = b"level\ttext\tconf\n5\t8,5\t32\n"

    def run(command, **kwargs):
        calls.append(command)
        assert command[1] == "stdin" and command[command.index("--psm") + 1] == "7"
        assert "tessedit_char_whitelist=0123456789,." in command
        image = cv2.imdecode(np.frombuffer(kwargs["input"], np.uint8), cv2.IMREAD_GRAYSCALE)
        rows = np.flatnonzero(np.count_nonzero(image < 128, axis=1))
        assert 30 <= rows[-1] - rows[0] + 1 <= 42  # ~36 px glyphs, not a fixed x3.
        return Result()

    monkeypatch.setattr("ifri_ocr.bac.reading.subprocess.run", run)
    result = tesseract_line(text_line(digits()), "numeric", 5)
    assert result["raw"] == "8,5" and result["confidence"] == 0.32 and len(calls) == 1


def test_unparseable_numeric_read_retries_psm8_then_13_only(monkeypatch):
    answers = iter(
        [b"level\ttext\tconf\n", b"level\ttext\tconf\n5\t.\t50\n", b"level\ttext\tconf\n5\t5\t70\n"]
    )
    modes = []

    def run(command, **kwargs):
        modes.append(command[command.index("--psm") + 1])

        class Result:
            stdout = next(answers)

        return Result()

    monkeypatch.setattr("ifri_ocr.bac.reading.subprocess.run", run)
    result = tesseract_line(text_line(digits("5")), "numeric", 5)
    assert modes == ["7", "8", "13"] and result["raw"] == "5" and result["psmFallback"] == "13"


def test_negative_word_confidence_is_not_fabricated_as_zero(monkeypatch):
    class Result:
        stdout = b"level\ttext\tconf\n4\theader\t-1\n5\t8\t-1\n5\t5\t60\n"

    monkeypatch.setattr("ifri_ocr.bac.reading.subprocess.run", lambda *a, **k: Result())
    result = tesseract_line(text_line(digits()), "label", 5)
    assert result["raw"] == "8 5" and result["confidence"] == 0.6
    assert result["ignoredNegativeConfidenceWords"] == 1


def test_exhausted_budget_never_invents_text():
    jobs = [CellJob((1, 1), digits(), "numeric", "X")]
    results = read_cells(jobs, deadline=0, workers=1, reader=lambda *a: pytest.fail("read"))
    assert results[(1, 1)]["error"] == "PAGE_BUDGET_EXHAUSTED" and results[(1, 1)]["raw"] == ""


def test_cell_workers_are_bounded(monkeypatch):
    monkeypatch.setenv("OCR_CELL_WORKERS", "3")
    assert cell_workers() == 3
    monkeypatch.setenv("OCR_CELL_WORKERS", "64")
    with pytest.raises(ValueError):
        cell_workers()


@pytest.mark.parametrize(
    "raw,expected", [("18.", "18"), ("80 .", "80"), ("9,61", "9.61"), (".", None), ("1 8", None)]
)
def test_numeric_cell_terminal_separator(raw, expected):
    result = number(raw)
    assert (str(result) if result is not None else None) == expected


# --- arithmetic -----------------------------------------------------------------------------

# Four rows (Note, Coeff, Points, Sur) whose Total is 21 / 202 / 420: average 202/21 = 9.619...,
# printed truncated as 9,61 like the Office du Bac transcript.
ROWS = (("10", "5", "50", "100"), ("9", "6", "54", "120"), ("8", "4", "32", "80"))
ROWS += (("11", "6", "66", "120"),)
TOTAL = ("9,61", "21", "202", "420")


def table(changes=None, total=None):
    """Fresh parsed rows; changes = {row: {column: printed text}}, "" for an unreadable cell."""
    cells = [list(r) for r in ROWS]
    for index, columns in (changes or {}).items():
        for column, text in columns.items():
            cells[index][column] = text
    return [parse_numbers(c) for c in cells], parse_numbers(list(total or TOTAL))


def decided(rows, index):
    return rows[index]["gradeStatus"], rows[index]["gradeValue"], rows[index]["gradeSource"]


def test_impossible_note_is_no_reading():
    assert parse_numbers(["1350", "2", "27", "40"])["parsed"][0] is None
    assert parse_numbers(["13,50", "2", "27", "40"])["parsed"][0] == "13.50"


def test_coefficient_validation_survives_missing_note():
    assert coefficient_validated(parse_numbers(["", "4", "", "80"]))


@pytest.mark.parametrize("coefficient,scale", [("7", "140"), ("2.5", "50"), ("2", "41")])
def test_coefficient_must_be_integer_1_to_6_with_matching_scale(coefficient, scale):
    assert not coefficient_validated(parse_numbers(["10", coefficient, "", scale]))
    assert failed("NEEDS_REVIEW")["status"] == "LOW_CONFIDENCE"
    assert failed("GRADE_POINTS_MISMATCH")["status"] == "LOW_CONFIDENCE"


def test_note_verified_on_its_row_is_accepted_without_the_totals():
    rows, total = table(total=("9,61", "21", "999", "420"))
    checks, outcome = evaluate(rows, total, [0])
    assert outcome == "" and not checks["tableVerified"]
    assert decided(rows, 0) == ("VALIDATED_BY_ROW", "10", "DIRECT_CELL")


def test_unreadable_note_is_derived_only_from_verified_totals():
    rows, total = table({1: {0: ""}})
    checks, outcome = evaluate(rows, total, [1])
    assert outcome == "" and checks["tableVerified"] and checks["displayedTotalNote"]
    assert decided(rows, 1) == ("VALIDATED_BY_TOTALS", "9", "POINTS_TOTALS")
    rows, total = table({1: {0: ""}}, total=("9,61", "21", "201", "420"))
    _, outcome = evaluate(rows, total, [1])
    assert outcome == "NEEDS_REVIEW" and rows[1]["gradeValue"] is None


def test_readable_note_contradicting_points_is_a_grade_points_mismatch():
    rows, total = table({0: {0: "12"}})
    _, outcome = evaluate(rows, total, [0])
    assert outcome == "GRADE_POINTS_MISMATCH" and rows[0]["gradeValue"] is None


def test_compensated_points_errors_are_never_accepted():
    # 50 -> 52 and 54 -> 52: the Points sum still equals the Total.
    rows, total = table({0: {2: "52"}, 1: {2: "52"}})
    checks, outcome = evaluate(rows, total, [0, 1])
    assert checks["pointsSum"] and outcome == "GRADE_POINTS_MISMATCH"
    assert all(row["gradeValue"] is None for row in rows)


def test_compensated_error_behind_an_unreadable_note_blocks_the_totals():
    # The expected row's note is unreadable and its Points read 52 instead of 50; another row's
    # Points read 30 instead of 32. Sums close and 52/5 = 10.4 is plausible: only the readable
    # note of the other row (8 x 4 != 30) reveals the compensation.
    rows, total = table({0: {0: "", 2: "52"}, 2: {2: "30"}})
    checks, outcome = evaluate(rows, total, [0])
    assert checks["pointsSum"] and checks["plausibleQuotients"]
    assert not checks["readableNotesAgree"] and outcome == "NEEDS_REVIEW"
    assert rows[0]["gradeValue"] is None


def test_compensation_between_two_unreadable_notes_is_not_accepted():
    # Two rows without a readable note: 50 -> 52 and 32 -> 30. Sums close, 52/5 = 10.4 and
    # 30/4 = 7.5 are plausible, no readable note disagrees: the totals cannot tell which
    # Points is right, so no note is derived from them.
    rows, total = table({0: {0: "", 2: "52"}, 2: {0: "", 2: "30"}})
    checks, outcome = evaluate(rows, total, [0])
    assert checks["tableVerified"] and outcome == "NEEDS_REVIEW"
    assert rows[0]["gradeValue"] is None


def test_rows_of_other_subjects_only_serve_the_totals():
    rows, total = table({3: {1: "7"}})
    checks, outcome = evaluate(rows, total, [0])
    assert not checks["tableVerified"] and outcome == ""
    assert decided(rows, 0) == ("VALIDATED_BY_ROW", "10", "DIRECT_CELL")


def test_single_unreadable_scale_is_reconstructed_then_checked_by_its_row():
    rows, total = table({2: {3: ""}})
    checks, outcome = evaluate(rows, total, [2])
    assert checks["reconstruction"]["scale"] == {"unreadable": 1, "reconstructed": True}
    assert rows[2]["parsed"][3] == "80" and rows[2]["cellSources"][3] == RECONSTRUCTED
    assert outcome == "" and decided(rows, 2) == ("VALIDATED_BY_ROW", "8", "DIRECT_CELL")


def test_reconstruction_contradicted_by_another_misread_cell():
    # Row 0 Sur misread 120 (true 100): the reconstructed Sur of row 2 is 60, not 20 x 4.
    rows, total = table({2: {3: ""}, 0: {3: "120"}})
    _, outcome = evaluate(rows, total, [2])
    assert rows[2]["parsed"][3] == "60" and not coefficient_validated(rows[2])
    assert outcome == "NEEDS_REVIEW" and rows[2]["gradeValue"] is None


def test_two_unreadable_cells_in_a_column_are_not_reconstructed():
    rows, total = table({2: {3: ""}, 3: {3: ""}})
    checks, outcome = evaluate(rows, total, [2])
    assert checks["reconstruction"]["scale"] == {"unreadable": 2, "reconstructed": False}
    assert rows[2]["parsed"][3] is None and outcome == "NEEDS_REVIEW"


def test_reconstructed_points_are_never_the_only_source_of_a_note():
    rows, total = table({1: {2: ""}})
    _, outcome = evaluate(rows, total, [1])
    assert rows[1]["cellSources"][2] == RECONSTRUCTED and outcome == ""
    assert decided(rows, 1) == ("VALIDATED_BY_ROW", "9", "DIRECT_CELL")  # Read note confirms.
    rows, total = table({1: {0: "", 2: ""}})
    _, outcome = evaluate(rows, total, [1])
    assert outcome == "NEEDS_REVIEW" and rows[1]["gradeValue"] is None


def test_reconstructed_coefficient_serves_the_totals_but_is_never_returned():
    rows, total = table({3: {1: ""}, 1: {0: ""}})
    checks, outcome = evaluate(rows, total, [1, 3])
    assert rows[3]["parsed"][1] == "6" and checks["tableVerified"]
    assert rows[1]["gradeValue"] == "9" and rows[3]["gradeStatus"] == "NEEDS_REVIEW"
    assert outcome == "NEEDS_REVIEW"


@pytest.mark.parametrize(
    "displayed,accepted", [("9,61", True), ("9,62", True), ("9,60", False), ("9,6", False)]
)
def test_displayed_average_is_its_truncation_or_rounding_only(displayed, accepted):
    rows, total = table({1: {0: ""}}, total=(displayed, "21", "202", "420"))
    checks, outcome = evaluate(rows, total, [1])
    assert checks["displayedTotalNote"] is accepted and (outcome == "") is accepted


def test_implausible_quotient_anywhere_blocks_the_totals():
    # Sums close, but 70/1 > 20 on one row reveals compensating misreads.
    rows = [parse_numbers(["", "1", "70", "20"]), parse_numbers(["", "1", "1", "20"])]
    checks, outcome = evaluate(rows, parse_numbers(["", "2", "71", "40"]), [1])
    assert not checks["plausibleQuotients"] and outcome == "NEEDS_REVIEW"


def test_non_terminating_quotient_is_never_rounded():
    rows = [parse_numbers(["", "3", "40", "60"])]
    _, outcome = evaluate(rows, parse_numbers(["", "3", "40", "60"]), [0])
    assert outcome == "NEEDS_REVIEW" and rows[0]["gradeValue"] is None


# --- attribution ----------------------------------------------------------------------------


def test_hungarian_finds_the_optimal_unique_assignment():
    cost = [[0.1, 0.2, 0.9], [0.2, 0.9, 0.9]]  # Greedy (0,0) would force (1,1) = 0.9.
    assert hungarian(cost) == [(0, 1), (1, 0)]


def test_unreadable_rows_do_not_block_attribution():
    scores = [word_scores([text]) for text in ("Anglais", "~~~", "Mathematiques", "Francais")]
    assignment, issue, _ = assign(EXPECTED, scores)
    assert not issue and assignment == {"ENGLISH": 0, "MATH": 2, "FRENCH": 3}


def test_duplicate_subject_row_is_ambiguous():
    scores = [word_scores([text]) for text in ("Anglais", "Francais", "Mathematiques", "Français")]
    assert assign(EXPECTED, scores)[1] == "AMBIGUOUS_ROW"


def test_missing_subject_needs_review_without_values():
    scores = [word_scores([text]) for text in ("Anglais", "Francais", "Philosophie")]
    assignment, issue, _ = assign(EXPECTED, scores)
    assert issue == "NEEDS_REVIEW" and assignment == {}


def test_best_of_label_variants_and_digit_confusables():
    assert word_scores(["", "ANGLA1S", "garbage"])["ANGLAIS"] == pytest.approx(1.0)
    assert word_scores(["MATHEMATI0UES"])["MATHEMATIQUES"] >= 0.85


def test_vocabulary_word_by_code_then_unique_name():
    assert vocabulary_word({"code": "MANUFACTURING_STUDY", "name": "x"}) == "ETUDE DE FABRICATION"
    assert vocabulary_word({"id": "X", "name": "Étude de Fabrication"}) == "ETUDE DE FABRICATION"
    assert vocabulary_word({"id": "X", "name": "Sites et Applications Web"}) is None


# --- series and header rows -----------------------------------------------------------------


def test_series_compared_with_declared_qualification():
    assert series_mismatch("C", "BAC_D") and not series_mismatch("D", "BAC_D")
    assert series_mismatch("D", "DT_IMI")  # A BAC general transcript for a DT declaration.
    assert not series_mismatch(None, "BAC_D") and not series_mismatch("C", None)


def test_series_needs_three_agreeing_reads():
    names = ("SERIES_OTSU", "SERIES_ADAPTIVE", "SERIES_FREE")
    results = {("series", name): {"raw": "D"} for name in names}
    assert _read_series(results, "") == ("D", "READ")
    results[("series", "SERIES_FREE")]["raw"] = "O"  # Whitelist forced D on something else.
    assert _read_series(results, "") == (None, "UNREADABLE")
    assert _read_series({}, "NOT_FOUND") == (None, "NOT_FOUND")
    assert _read_series({}, "AMBIGUOUS") == (None, "AMBIGUOUS")


def letter_zone(*, big="D", word=None, frame=False):
    image = np.full((420, 900), 235, np.uint8)
    if frame:  # Photographed page edge crossing the whole zone.
        cv2.rectangle(image, (5, 5), (894, 414), 40, 3)
    for y in (60, 100, 140):  # Identity lines at body text height.
        cv2.putText(image, "Nom : SYNTHETIQUE 000", (330, y), cv2.FONT_HERSHEY_SIMPLEX, 0.8, 30, 2)
    if big:
        cv2.putText(image, big, (120, 200), cv2.FONT_HERSHEY_SIMPLEX, 4, 20, 9)
    if word:
        cv2.putText(image, word, (100, 360), cv2.FONT_HERSHEY_SIMPLEX, 3, 20, 7)
    return image


def test_series_candidate_is_one_large_isolated_letter():
    assert len(series_candidates(letter_zone(), 22)) == 1
    assert series_candidates(letter_zone(big=None), 22) == []
    # A large word is not a series letter; it does not hide the letter either.
    assert len(series_candidates(letter_zone(big=None, word="BAC"), 22)) == 0
    assert len(series_candidates(letter_zone(word="BAC"), 22)) == 1
    assert len(series_candidates(letter_zone(frame=True), 22)) == 1


def test_series_zone_is_relative_to_the_table_and_section_title():
    # Real-transcript geometry at 300 DPI: table 368-2142 px, section title bottom at 1050 px.
    x0, y0, x1, y1 = series_zone((368, 2142), 1050, (3509, 2480))
    assert (x0, x1) == (155, 900) and (y0, y1) == (251, 996)
    assert x0 < 263 and 371 < x1 and y0 < 482 and 598 < y1  # Observed letter box inside.


def test_header_remnants_are_not_data_rows():
    assert not _looks_like_data(row("~~ ~", "20", "", "", ""))
    header = row("Matières", "", "", "", "")
    header["labelTexts"] = ["Matières"]
    assert not _looks_like_data(header)
    assert _looks_like_data(row("", "", "2", "", "40"))
    assert _looks_like_data(row("Anglais", "", "", "", ""))


# --- cell readers ---------------------------------------------------------------------------


def test_rapidocr_is_enabled_by_default_and_strictly_configured():
    from ifri_ocr.engines import RapidOcrDigits, TesseractCells, cell_reader, rapidocr_enabled

    assert rapidocr_enabled({}) and not rapidocr_enabled({"OCR_RAPIDOCR_ENABLED": "false"})
    with pytest.raises(ValueError):
        rapidocr_enabled({"OCR_RAPIDOCR_ENABLED": "yes"})
    assert isinstance(cell_reader({}), RapidOcrDigits)
    assert isinstance(cell_reader({"OCR_RAPIDOCR_ENABLED": "false"}), TesseractCells)


def test_rapidocr_reads_digits_only_and_leaves_other_cells_to_tesseract(monkeypatch):
    from ifri_ocr import engines

    class Output:
        txts, scores = ("1 3,5O",), (0.91,)

    reader = engines.RapidOcrDigits()
    reader._engine = lambda *a, **k: Output()
    result = reader.read(digits("13,5"), "numeric", 5)
    assert result["raw"] == "13,5" and result["confidence"] == 0.91  # Never corrected.
    monkeypatch.setattr(engines, "tesseract_line", lambda image, kind, timeout: {"raw": kind})
    assert reader.read(digits(), "label", 5)["raw"] == "label"
    assert reader.read(None, "numeric", 5)["raw"] == "numeric"  # Empty cell: no inference.


def test_rapidocr_readiness_requires_the_pinned_model(tmp_path, monkeypatch):
    import hashlib

    from ifri_ocr import engines

    reader = engines.RapidOcrDigits(model_dir=tmp_path)
    with pytest.raises(RuntimeError, match="manifest missing"):
        reader.verify()
    (tmp_path / "model.onnx").write_bytes(b"model")
    (tmp_path / "SHA256SUMS").write_text("0" * 64 + "  model.onnx\n")
    with pytest.raises(RuntimeError, match="checksum mismatch"):
        reader.verify()
    digest = hashlib.sha256(b"model").hexdigest()
    (tmp_path / "SHA256SUMS").write_text(f"{digest}  model.onnx\n")
    monkeypatch.setattr(reader, "prepare", lambda: None)
    reader.verify()
    monkeypatch.setattr(reader, "prepare", lambda: 1 / 0)
    with pytest.raises(RuntimeError, match="inference unavailable"):
        reader.verify()


def test_manifest_pins_exactly_the_recognition_model():
    # Repository copy on the host, installed copy in the OCR image.
    candidates = (
        Path(__file__).resolve().parents[1] / "rapidocr-models.sha256",
        Path("/opt/rapidocr-models/SHA256SUMS"),
    )
    lines = next(p for p in candidates if p.is_file()).read_text().splitlines()
    assert len(lines) == 1 and lines[0].endswith("  PP-OCRv6_rec_small.onnx")
    assert len(lines[0].split()[0]) == 64


# --- end to end on the realistic synthetic generator (real engines only) ----------------------

needs_engines = pytest.mark.skipif(
    shutil.which("tesseract") is None or not Path("/opt/rapidocr-models/SHA256SUMS").is_file(),
    reason="Tesseract or the RapidOCR model absent (run in the OCR image)",
)


@needs_engines
@pytest.mark.parametrize(
    "name,status,failure",
    [
        ("d-photo", "SUCCEEDED", ""),
        ("c-declared-d", "FAILED", "SERIES_MISMATCH"),
        ("d-faded-scale", "SUCCEEDED", ""),
        ("d-compensated-points", "LOW_CONFIDENCE", "GRADE_POINTS_MISMATCH"),
    ],
)
def test_cell_path_end_to_end_on_realistic_synthetic(name, status, failure, tmp_path):
    from support.synthetic_bac import TARGET_LABELS, fixture_bytes, variant

    from ifri_ocr.engines import production_engine
    from ifri_ocr.extraction import analyze

    transcript, qualification = variant(name)
    path = tmp_path / f"{name}.pdf"
    path.write_bytes(fixture_bytes(name))
    diagnostics = []
    result = analyze(
        path, EXPECTED, production_engine(), diagnostics=diagnostics, qualification=qualification
    )
    assert (result["status"], result["failureCode"]) == (status, failure)
    cell = next(d for d in diagnostics if d.get("strategy") == "BAC_CELL_GRID")
    assert cell["seriesStatus"] == "READ"
    oracle = transcript.oracle(TARGET_LABELS)
    for subject in result["subjects"]:
        assert (Decimal(subject["gradeValue"]), Decimal(subject["coefficientValue"])) == tuple(
            map(Decimal, oracle[subject["subjectId"]])
        )
        assert subject["evidence"]["validation"] == "CELL_ARITHMETIC"
    if status == "SUCCEEDED":
        assert result["engine"] == "tesseract+rapidocr"
