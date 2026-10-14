import hashlib

import cv2
import numpy as np
import pymupdf
import pytest

from ifri_ocr.proofs import ProofError, process_proof, render_proof

EVIDENCE = {
    "system": "NORMALIZED_TOP_LEFT",
    "subject": {"x0": 0.08, "y0": 0.30, "x1": 0.45, "y1": 0.33},
    "grade": {"x0": 0.50, "y0": 0.30, "x1": 0.60, "y1": 0.33},
    "coefficient": {"x0": 0.62, "y0": 0.30, "x1": 0.70, "y1": 0.33},
}
PROOF = "00000000-0000-4000-8000-0000000000aa"


def transcript() -> bytes:
    document = pymupdf.open()
    page = document.new_page()
    page.insert_text((60, 260), "Mathematiques      15,50      3")
    content = document.tobytes()
    document.close()
    return content


def test_render_crops_one_row_with_highlighted_cells():
    png = render_proof(transcript(), 1, EVIDENCE)
    assert png.startswith(b"\x89PNG\r\n\x1a\n")
    image = cv2.imdecode(np.frombuffer(png, np.uint8), cv2.IMREAD_COLOR)
    page_height = round(842 * 150 / 72)
    height, width = image.shape[:2]
    # A band around the row, not the whole page; wider than the cells themselves.
    assert 20 < height < page_height / 5
    assert width >= int((0.70 - 0.08) * round(595 * 150 / 72))
    # The highlighted cells are tinted (blue channel lowered by the amber fill).
    assert (image[:, :, 0] < 200).any()


def test_render_refuses_missing_cells_and_pages():
    with pytest.raises(ProofError, match="RENDER_FAILED"):
        render_proof(transcript(), 1, {"subject": EVIDENCE["subject"]})
    with pytest.raises(ProofError, match="PAGE_MISSING"):
        render_proof(transcript(), 2, EVIDENCE)
    with pytest.raises(ProofError, match="RENDER_FAILED"):
        render_proof(
            transcript(), 1, {**EVIDENCE, "grade": {"x0": 0.6, "y0": 0.3, "x1": 0.5, "y1": 2}}
        )
    with pytest.raises(ProofError, match="DOCUMENT_UNAVAILABLE"):
        render_proof(b"not a pdf", 1, EVIDENCE)


class API:
    def __init__(self, pdf: bytes | None, image_error: str | None = None):
        self.pdf = pdf
        self.image_error = image_error
        self.images: list[bytes] = []
        self.failures: list[str] = []

    def document(self, proof):
        if self.pdf is None:
            raise RuntimeError("INTERNAL_HTTP_404")
        return self.pdf

    def image(self, proof, png):
        if self.image_error:
            raise RuntimeError(self.image_error)
        self.images.append(png)

    def failure(self, proof, code):
        self.failures.append(code)


def job(pdf: bytes, **extra) -> dict:
    return {
        "contractVersion": 1,
        "proofId": PROOF,
        "documentSha256": hashlib.sha256(pdf).hexdigest(),
        "pageNumber": 1,
        "evidence": EVIDENCE,
        **extra,
    }


def test_process_uploads_the_exact_version_only():
    pdf = transcript()
    api = API(pdf)
    assert process_proof(job(pdf), api) == "READY"
    assert len(api.images) == 1 and not api.failures
    other = API(pdf)
    assert process_proof(job(pdf, documentSha256="0" * 64), other) == "DOCUMENT_UNAVAILABLE"
    assert other.failures == ["DOCUMENT_UNAVAILABLE"] and not other.images
    missing = API(None)
    assert process_proof(job(pdf), missing) == "DOCUMENT_UNAVAILABLE"
    assert process_proof({"contractVersion": 2, "proofId": PROOF}, API(pdf)) == "IGNORED"


def test_process_acknowledges_refusals_and_retries_transport_errors():
    pdf = transcript()
    assert process_proof(job(pdf), API(pdf, image_error="INTERNAL_HTTP_409")) == "REFUSED"
    with pytest.raises(RuntimeError):
        process_proof(job(pdf), API(pdf, image_error="INTERNAL_HTTP_503"))
