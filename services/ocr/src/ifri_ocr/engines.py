"""Cell readers behind one port (ADR 078).

BAC numeric cells are read by RapidOCR (recognition model only) when OCR_RAPIDOCR_ENABLED is true,
the default; anchors, labels, the series letter and the generic scan path stay on Tesseract. The
recognition model ships in the image, pinned by SHA-256 in services/ocr/rapidocr-models.sha256,
and is never downloaded at runtime (the worker has no outbound network).
"""

import hashlib
import os
import re
import threading
from collections.abc import Mapping
from importlib.metadata import version
from pathlib import Path
from time import monotonic
from typing import Protocol

import numpy as np

from ifri_ocr.bac.reading import tesseract_line

RAPIDOCR_MODEL_DIR = Path("/opt/rapidocr-models")
RAPIDOCR_MANIFEST = RAPIDOCR_MODEL_DIR / "SHA256SUMS"
RAPIDOCR_PARAMS = {
    "Global.log_level": "critical",
    "Global.model_root_dir": str(RAPIDOCR_MODEL_DIR),
}


class CellReader(Protocol):
    name: str

    @property
    def version(self) -> str: ...

    def read(self, image, kind: str, timeout: int) -> dict: ...

    def prepare(self) -> None: ...

    def verify(self) -> None: ...


class TesseractCells:
    """Every cell kind through single-line Tesseract (labels fra, digits eng + whitelist)."""

    name = "tesseract"
    version = ""

    def read(self, image, kind, timeout):
        return tesseract_line(image, kind, timeout)

    def prepare(self):
        return None

    def verify(self):
        return None


class RapidOcrDigits:
    """Numeric cells through RapidOCR recognition; every other kind stays on Tesseract.

    The output is filtered exactly like the Tesseract whitelist (digits and separators); nothing
    is corrected towards a value. The engine is created once per process, thread-safely.
    """

    name = "rapidocr"

    def __init__(self, model_dir: Path = RAPIDOCR_MODEL_DIR):
        self.model_dir = model_dir
        self._engine = None
        self._lock = threading.Lock()

    @property
    def version(self):
        return f"rapidocr {version('rapidocr')}; onnxruntime {version('onnxruntime')}"

    def engine(self):
        if self._engine is None:
            with self._lock:
                if self._engine is None:
                    # No telemetry upload from document processing (also set in the image).
                    os.environ.setdefault("ORT_DISABLE_TELEMETRY", "1")
                    from rapidocr import RapidOCR

                    params = {**RAPIDOCR_PARAMS, "Global.model_root_dir": str(self.model_dir)}
                    self._engine = RapidOCR(params=params)
        return self._engine

    def read(self, image, kind, timeout):
        if kind != "numeric" or image is None:
            return tesseract_line(image, kind, timeout)
        started = monotonic()
        rgb = np.dstack([image] * 3) if image.ndim == 2 else image
        output = self.engine()(rgb, use_det=False, use_cls=False, use_rec=True)
        texts = list(getattr(output, "txts", None) or [])
        scores = [float(s) for s in (getattr(output, "scores", None) or [])]
        return {
            "raw": re.sub(r"[^0-9,.]", "", " ".join(texts)),
            "confidence": scores[0] if scores else 0.0,
            "wordConfidences": scores,
            "confidenceSource": "RAPIDOCR_REC",
            "ignoredNegativeConfidenceWords": 0,
            "seconds": round(monotonic() - started, 4),
        }

    def prepare(self):
        """Load the model before the first numeric cell (~3 s, overlapped with anchoring)."""
        self.read(np.full((48, 160), 255, np.uint8), "numeric", 5)

    def verify(self):
        """Readiness: pinned model files intact, then one recognition on a blank cell."""
        manifest = self.model_dir / "SHA256SUMS"
        try:
            entries = [line.split() for line in manifest.read_text().splitlines() if line.strip()]
        except OSError:
            raise RuntimeError("RapidOCR model manifest missing") from None
        if not entries or any(len(entry) != 2 for entry in entries):
            raise RuntimeError("RapidOCR model manifest invalid")
        for digest, name in entries:
            path = self.model_dir / name.lstrip("*")
            try:
                actual = hashlib.sha256(path.read_bytes()).hexdigest()
            except OSError:
                raise RuntimeError("RapidOCR model missing") from None
            if actual != digest:
                raise RuntimeError("RapidOCR model checksum mismatch")
        try:
            self.prepare()
        except Exception:  # noqa: BLE001 -- engine errors are reported as one static reason
            raise RuntimeError("RapidOCR inference unavailable") from None


def rapidocr_enabled(env: Mapping[str, str] | None = None) -> bool:
    value = (os.environ if env is None else env).get("OCR_RAPIDOCR_ENABLED", "true")
    if value not in {"true", "false"}:
        raise ValueError("OCR_RAPIDOCR_ENABLED must be true or false")
    return value == "true"


def cell_reader(env: Mapping[str, str] | None = None) -> CellReader:
    return RapidOcrDigits() if rapidocr_enabled(env) else TesseractCells()


def production_engine(env: Mapping[str, str] | None = None):
    """The configured engine: Tesseract pages and labels, plus the configured cell reader."""
    from ifri_ocr.extraction import Tesseract

    return Tesseract(cells=cell_reader(env))
