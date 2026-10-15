import pymupdf
import pytest

from ifri_ocr.exports import FONT, FONT_BOLD, ExportError, process_export, render_list

fonts = pytest.mark.skipif(
    not (FONT.is_file() and FONT_BOLD.is_file()), reason="DejaVu fonts not installed"
)


def document(variant: str = "PUBLIC", rows: int = 3) -> dict:
    public = variant == "PUBLIC"
    columns = (
        ["Rang", "Numéro", "Nom", "Prénoms", "Décision"]
        if public
        else ["Rang", "Numéro", "Nom", "Prénoms", "Moyenne", "Départage des mathématiques"]
    )
    body = []
    for i in range(1, rows + 1):
        name = "Ɖɔkɔ Hounkpè" if i == 1 else f"Synthétique {i}"
        row = [str(i), f"IFRI-2026-GL-{i:06d}", name, "Ifèdé Élodie", "Admis"]
        if not public:
            row = row[:4] + ["13,33", "Maths non comparées : candidat sans matière mathématique"]
        body.append(row)
    return {
        "contractVersion": 1,
        "variant": variant,
        "title": "Liste des candidats admis et en liste d’attente",
        "header": [
            ["Filière", "Génie Logiciel (GL)"],
            ["Liste", "Version 7 du 14/10/2026 18:30 (gel du 14/10/2026 18:00)"],
        ],
        "columns": columns,
        "rows": body,
        "notes": ["Rang au classement gelé ; décision à la date de cette version de la liste."],
        "landscape": not public,
    }


def text_of(pdf: bytes) -> list[str]:
    with pymupdf.open(stream=pdf, filetype="pdf") as opened:
        return [page.get_text() for page in opened]


@fonts
def test_public_list_lays_out_every_cell_and_nothing_else():
    pages = text_of(render_list(document()))
    text = "\n".join(pages)
    for expected in (
        "Liste des candidats admis",
        "Version 7 du 14/10/2026 18:30",
        "Ɖɔkɔ Hounkpè",
        "Ifèdé Élodie",
        "IFRI-2026-GL-000003",
        "Admis",
        "page 1 / 1",
    ):
        assert expected in text
    # The renderer adds no figure of its own: no average can appear in a public list.
    assert "Moyenne" not in text and "13,33" not in text


@fonts
def test_long_internal_list_repeats_the_header_row_on_every_page():
    pages = text_of(render_list(document("INTERNAL", rows=150)))
    assert len(pages) > 2
    for number, page in enumerate(pages, start=1):
        assert "Numéro" in page and f"page {number} / {len(pages)}" in page
    assert "Maths non comparées" in pages[0] and "13,33" in pages[0]


@pytest.mark.parametrize(
    "change",
    [
        {"contractVersion": 2},
        {"columns": []},
        {"rows": [["1", "2"]]},
        {"header": [["Liste"]]},
        {"title": None},
    ],
)
def test_malformed_documents_are_refused(change):
    with pytest.raises(ExportError):
        render_list({**document(), **change})


class FakeApi:
    def __init__(self, payload=None, upload_error=None):
        self._payload = payload
        self.upload_error = upload_error
        self.uploaded = None
        self.failed = None

    def payload(self, export):
        if isinstance(self._payload, Exception):
            raise self._payload
        return self._payload

    def document(self, export, pdf):
        if self.upload_error:
            raise RuntimeError(self.upload_error)
        self.uploaded = pdf

    def failure(self, export, code):
        self.failed = code


JOB = {"contractVersion": 1, "exportId": "11111111-1111-4111-8111-111111111111"}


@fonts
def test_process_uploads_the_rendered_pdf():
    api = FakeApi(document())
    assert process_export(JOB, api) == "READY"
    assert api.uploaded.startswith(b"%PDF-") and b"%%EOF" in api.uploaded[-1024:]


def test_process_acknowledges_gone_refused_and_failed_jobs_and_retries_transport_errors():
    assert process_export({"contractVersion": 9}, FakeApi()) == "IGNORED"
    assert process_export(JOB, FakeApi(RuntimeError("INTERNAL_HTTP_404"))) == "GONE"
    broken = FakeApi({**document(), "rows": [["1"]]})
    assert process_export(JOB, broken) == "RENDER_FAILED" and broken.failed == "RENDER_FAILED"
    with pytest.raises(RuntimeError):
        process_export(JOB, FakeApi(RuntimeError("INTERNAL_HTTP_503")))
    if FONT.is_file():
        assert process_export(JOB, FakeApi(document(), "INTERNAL_HTTP_409")) == "REFUSED"
        with pytest.raises(RuntimeError):
            process_export(JOB, FakeApi(document(), "INTERNAL_HTTP_500"))
