"""On-demand OCR proof images for the administration.

One table row cropped from the exact analysed page (the document version and the coordinates
stored with the analysis), read cells highlighted. Proof jobs have their own stream and loop:
they never wait behind candidates' analyses.
"""

import hashlib
import json
import logging
from threading import Event
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import cv2
import numpy as np

PROOF_STREAM = "ifri:ocr:proofs:v1"
PROOF_GROUP = "ifri-ocr-proofs-v1"
DPI = 150
MAX_PIXELS = 20_000_000
HIGHLIGHT = (250, 204, 21)  # amber fill, RGB
OUTLINE = (30, 58, 95)  # institutional navy, RGB
CELLS = ("grade", "coefficient", "points", "scale")


class ProofError(Exception):
    """A definitive failure, reported to the API with a closed code."""


def _box(evidence: dict, name: str) -> tuple[float, float, float, float] | None:
    box = evidence.get(name)
    if not isinstance(box, dict):
        return None
    try:
        x0, y0, x1, y1 = (float(box[k]) for k in ("x0", "y0", "x1", "y1"))
    except KeyError, TypeError, ValueError:
        return None
    if not (0 <= x0 < x1 <= 1 and 0 <= y0 < y1 <= 1):
        return None
    return x0, y0, x1, y1


def render_proof(pdf: bytes, page_number: int, evidence: dict) -> bytes:
    """PNG of the row band around the stored boxes, cells outlined and highlighted."""
    import pymupdf

    boxes = {name: _box(evidence, name) for name in ("subject", *CELLS)}
    if boxes["grade"] is None or boxes["coefficient"] is None:
        raise ProofError("RENDER_FAILED")
    try:
        document = pymupdf.open(stream=pdf, filetype="pdf")
    except RuntimeError, ValueError:
        raise ProofError("DOCUMENT_UNAVAILABLE") from None
    with document:
        if not 1 <= page_number <= document.page_count:
            raise ProofError("PAGE_MISSING")
        page = document[page_number - 1]
        dpi = DPI
        while page.rect.width * page.rect.height * (dpi / 72) ** 2 > MAX_PIXELS and dpi > 50:
            dpi -= 25
        pixmap = page.get_pixmap(dpi=dpi, colorspace=pymupdf.csRGB, alpha=False)
    image = np.frombuffer(pixmap.samples, np.uint8).reshape(pixmap.height, pixmap.width, 3).copy()
    height, width = image.shape[:2]
    present = {name: box for name, box in boxes.items() if box is not None}
    pixels = {
        name: (int(x0 * width), int(y0 * height), int(x1 * width), int(y1 * height))
        for name, (x0, y0, x1, y1) in present.items()
    }
    row = max(y1 - y0 for _, y0, _, y1 in pixels.values())
    margin_y, margin_x = max(12, row), max(16, width // 50)
    left = max(0, min(b[0] for b in pixels.values()) - margin_x)
    right = min(width, max(b[2] for b in pixels.values()) + margin_x)
    top = max(0, min(b[1] for b in pixels.values()) - margin_y)
    bottom = min(height, max(b[3] for b in pixels.values()) + margin_y)
    if right - left < 8 or bottom - top < 8:
        raise ProofError("RENDER_FAILED")
    overlay = image.copy()
    for name in CELLS:
        if name in pixels:
            x0, y0, x1, y1 = pixels[name]
            cv2.rectangle(overlay, (x0, y0), (x1, y1), HIGHLIGHT, thickness=-1)
    image = cv2.addWeighted(overlay, 0.35, image, 0.65, 0)
    for name, (x0, y0, x1, y1) in pixels.items():
        thickness = 1 if name == "subject" else 2
        cv2.rectangle(image, (x0, y0), (x1, y1), OUTLINE, thickness=thickness)
    crop = cv2.cvtColor(image[top:bottom, left:right], cv2.COLOR_RGB2BGR)
    ok, encoded = cv2.imencode(".png", crop)
    if not ok:
        raise ProofError("RENDER_FAILED")
    return encoded.tobytes()


class ProofClient:
    """Internal API routes of the proof loop (bearer secret, private network)."""

    def __init__(self, base: str, secret: str):
        self.base = base
        self.secret = secret

    def _call(self, path: str, data: bytes | None, content_type: str) -> bytes:
        request = Request(
            f"{self.base}/internal/proofs/{path}",
            data=data,
            headers={"Authorization": f"Bearer {self.secret}", "Content-Type": content_type},
        )
        try:
            with urlopen(request, timeout=30) as response:
                return response.read(50 * 1024 * 1024 + 1)
        except HTTPError as error:
            raise RuntimeError(f"INTERNAL_HTTP_{error.code}") from None

    def document(self, proof: str) -> bytes:
        return self._call(f"{proof}/document", None, "application/json")

    def image(self, proof: str, png: bytes) -> None:
        self._call(f"{proof}/image", png, "image/png")

    def failure(self, proof: str, code: str) -> None:
        self._call(
            f"{proof}/failure", json.dumps({"failureCode": code}).encode(), "application/json"
        )


def process_proof(job: dict, api: ProofClient) -> str:
    """Render and upload one proof; returns the outcome for logging. Transport errors raise."""
    proof = str(job.get("proofId", ""))
    if job.get("contractVersion") != 1 or len(proof) != 36:
        return "IGNORED"
    try:
        try:
            pdf = api.document(proof)
        except RuntimeError as error:
            if str(error) == "INTERNAL_HTTP_404":
                raise ProofError("DOCUMENT_UNAVAILABLE") from None
            raise
        # The exact analysed version: any other content is refused.
        if hashlib.sha256(pdf).hexdigest() != job.get("documentSha256"):
            raise ProofError("DOCUMENT_UNAVAILABLE")
        png = render_proof(pdf, int(job.get("pageNumber", 0)), job.get("evidence") or {})
    except ProofError as error:
        api.failure(proof, str(error))
        return str(error)
    try:
        api.image(proof, png)
    except RuntimeError as error:
        # 409/422: the proof is no longer pending or the image was refused: acknowledge.
        if str(error) in {"INTERNAL_HTTP_400", "INTERNAL_HTTP_409", "INTERNAL_HTTP_422"}:
            return "REFUSED"
        raise
    return "READY"


def serve_proofs(client, api: ProofClient, consumer: str, stop: Event) -> None:
    """Dedicated loop on the proof stream (own Redis connection, own consumer group)."""
    logger = logging.getLogger("ifri_ocr")
    try:
        client.xgroup_create(PROOF_STREAM, PROOF_GROUP, id="0", mkstream=True)
    except Exception as error:  # noqa: BLE001 - redis raises ResponseError for BUSYGROUP
        if "BUSYGROUP" not in str(error):
            raise
    while not stop.is_set():
        try:
            reclaimed = client.xautoclaim(
                PROOF_STREAM, PROOF_GROUP, consumer, min_idle_time=120000, start_id="0-0", count=1
            )
            messages = reclaimed[1]
            if not messages:
                batches = client.xreadgroup(
                    PROOF_GROUP, consumer, {PROOF_STREAM: ">"}, count=1, block=1000
                )
                messages = batches[0][1] if batches else []
            for message_id, fields in messages:
                job = json.loads(fields["job"])
                outcome = process_proof(job, api)
                client.xack(PROOF_STREAM, PROOF_GROUP, message_id)
                logger.info(
                    "Proof job handled",
                    extra={"proof_id": job.get("proofId"), "outcome": outcome},
                )
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            logger.error(
                "Proof transport failed; job retained", extra={"failure_code": str(error)[:40]}
            )
            stop.wait(2)
        except Exception:  # noqa: BLE001 - keep the analysis loop alive whatever happens here
            logger.error("Proof loop error; retrying")
            stop.wait(2)
