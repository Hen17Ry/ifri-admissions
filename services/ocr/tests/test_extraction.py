from decimal import Decimal

import pytest

from ifri_ocr.extraction import Token, lines, locate, normalize, number


def test_normalization_and_decimal():
    assert normalize(" MATHÉMATIQUES ") == normalize("mathematiques")
    assert number("15,50") == Decimal("15.50")
    assert number("09.75") == Decimal("9.75")
    assert number("NaN") is None
    assert number("1e4") is None


def table(extra=False, missing=False):
    tokens = [
        Token("Matière", (0.1, 0.1, 0.2, 0.12)),
        Token("Note", (0.6, 0.1, 0.65, 0.12)),
        Token("Coef", (0.8, 0.1, 0.85, 0.12)),
        Token("Mathématiques", (0.1, 0.2, 0.3, 0.22)),
        Token("15,5", (0.6, 0.2, 0.65, 0.22)),
    ]
    if not missing:
        tokens.append(Token("2.5", (0.8, 0.2, 0.85, 0.22)))
    if extra:
        tokens.append(Token("14", (0.7, 0.2, 0.75, 0.22)))
    return tokens


def test_spatial_table_provenance():
    expected = [{"id": "provided-by-go", "name": "Mathématiques", "position": 1}]
    result, failure = locate(table(), expected, 2, 0.85)
    assert not failure
    assert len(lines(table())) == 2
    assert result[0]["gradeValue"] == "15.5"
    assert result[0]["coefficientValue"] == "2.5"
    assert result[0]["pageNumber"] == 2
    assert result[0]["evidence"]["system"] == "NORMALIZED_TOP_LEFT"


@pytest.mark.parametrize(
    ("extra", "missing", "failure"),
    [(True, False, "AMBIGUOUS_ROW"), (False, True, "COEFFICIENT_NOT_FOUND")],
)
def test_refuses_missing_or_ambiguous_numbers(extra, missing, failure):
    result, code = locate(
        table(extra, missing), [{"id": "expected", "name": "Mathématiques", "position": 1}], 1, 0.85
    )
    assert result == []
    assert code == failure


def test_no_coefficient_default_without_header():
    result, failure = locate(
        table()[3:], [{"id": "expected", "name": "Mathématiques", "position": 1}], 1, 0.85
    )
    assert result == []
    assert failure == "SUBJECT_NOT_FOUND"


def test_native_pdf_uses_decimal_values_from_file(tmp_path):
    import pymupdf

    from ifri_ocr.extraction import analyze

    class NoOcr:
        def extract(self, *args):
            raise AssertionError("A readable native table must not be rasterized")

    doc = pymupdf.open()
    page = doc.new_page()
    expected = [
        {"id": f"test-{i}", "name": name, "position": i + 1}
        for i, name in enumerate(["Alpha", "Beta", "Gamma"])
    ]
    for x, text in [(40, "Matière"), (350, "Note"), (460, "Coef")]:
        page.insert_text((x, 100), text)
    for i, subject in enumerate(expected):
        for x, text in [(40, subject["name"]), (350, "15,50"), (460, "2.5")]:
            page.insert_text((x, 140 + i * 40), text)
    pdf = tmp_path / "native.pdf"
    doc.save(pdf)
    doc.close()
    result = analyze(pdf, expected, NoOcr())
    assert result["status"] == "SUCCEEDED"
    assert result["engine"] == "pymupdf"
    assert all(
        s["gradeValue"] == "15.50" and s["coefficientValue"] == "2.5" for s in result["subjects"]
    )


def test_coefficients_are_not_confused_with_points():
    tokens = table()
    tokens.extend([Token("Points", (0.9, 0.1, 0.95, 0.12)), Token("38.75", (0.9, 0.2, 0.95, 0.22))])
    result, failure = locate(
        tokens, [{"id": "expected", "name": "Mathématiques", "position": 1}], 1, 0.85
    )
    assert not failure
    assert result[0]["coefficientValue"] == "2.5"


def test_scan_fallback_keeps_normalized_evidence(tmp_path):
    import pymupdf

    from ifri_ocr.extraction import analyze

    class Engine:
        name = "synthetic-test-engine"
        version = "1"

        def __init__(self):
            self.calls = 0

        def extract(self, image, width, height, timeout):
            self.calls += 1
            assert image.exists() and width > 0 and height > 0 and 0 < timeout <= 30
            return table()

    doc = pymupdf.open()
    doc.new_page()
    pdf = tmp_path / "image-only.pdf"
    doc.save(pdf)
    doc.close()
    engine = Engine()
    result = analyze(pdf, [{"id": "from-backend", "name": "Mathématiques", "position": 1}], engine)
    assert engine.calls == 2
    assert result["status"] == "SUCCEEDED"
    assert result["subjects"][0]["evidence"]["system"] == "NORMALIZED_TOP_LEFT"


def test_source_line_groups_preserve_skewed_rows():
    tokens = [
        Token("Matière", (0.1, 0.1, 0.2, 0.12), 1, (1, 1, 1)),
        Token("Note", (0.6, 0.14, 0.65, 0.16), 1, (1, 1, 1)),
        Token("Coef", (0.8, 0.16, 0.85, 0.18), 1, (1, 1, 1)),
    ]
    assert len(lines(tokens)) == 1


def test_ocr_subject_noise_does_not_compete_with_below_threshold_match():
    expected = [
        {"id": "french", "name": "Français", "position": 1},
        {"id": "english", "name": "Anglais", "position": 2},
    ]
    tokens = table()
    tokens[3] = Token("Frangais", tokens[3].box, 0.91666771)
    tokens.extend(
        [
            Token("Anglais", (0.1, 0.3, 0.3, 0.32), 0.96),
            Token("14", (0.6, 0.3, 0.65, 0.32), 0.96),
            Token("3", (0.8, 0.3, 0.85, 0.32), 0.96),
        ]
    )
    result, failure = locate(tokens, expected, 1, 0.85)
    assert not failure
    assert [subject["subjectId"] for subject in result] == ["french", "english"]
    assert result[0]["subjectConfidence"] == 0.875
    assert result[0]["gradeValue"] == "15.5"
    assert result[0]["coefficientValue"] == "2.5"


def test_two_viable_similar_subjects_remain_ambiguous():
    result, failure = locate(
        table(),
        [
            {"id": "a", "name": "Mathématiques", "position": 1},
            {"id": "b", "name": "Mathématique", "position": 2},
        ],
        1,
        0.85,
    )
    assert result == []
    assert failure == "AMBIGUOUS_ROW"
