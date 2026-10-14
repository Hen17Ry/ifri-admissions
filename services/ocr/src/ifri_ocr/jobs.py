"""Internal callback client and at-least-once stream consumer, without PostgreSQL access."""

import hashlib
import json
import logging
import os
import signal
import subprocess
import sys
from pathlib import Path
from tempfile import TemporaryDirectory
from threading import Event
from time import monotonic
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from ifri_ocr.extraction import failed

STREAM = "ifri:ocr:jobs:v1"
GROUP = "ifri-ocr-v1"
# Go answered definitively: result persisted, refused (then marked FAILED by Go) or stale.
TERMINAL_HTTP = {"INTERNAL_HTTP_400", "INTERNAL_HTTP_409", "INTERNAL_HTTP_422"}


class InternalClient:
    def __init__(self, base: str, secret: str):
        self.base = base
        self.secret = secret

    def ready(self) -> bool:
        request = Request(
            f"{self.base}/internal/ocr/ready", headers={"Authorization": f"Bearer {self.secret}"}
        )
        try:
            with urlopen(request, timeout=2) as response:
                return response.status == 200
        except OSError, ValueError:
            return False

    def request(self, analysis: str, action: str, data: dict | None = None) -> bytes:
        request = Request(
            f"{self.base}/internal/ocr/{analysis}/{action}",
            data=None if data is None else json.dumps(data).encode(),
            headers={"Authorization": f"Bearer {self.secret}", "Content-Type": "application/json"},
        )
        try:
            with urlopen(request, timeout=30) as response:
                return response.read(50 * 1024 * 1024 + 1)
        except HTTPError as error:
            # Don't retain response bodies, URLs, secrets or raw upstream errors in logs.
            raise RuntimeError(f"INTERNAL_HTTP_{error.code}") from None


def deliver(api: InternalClient, identity: str, payload: dict) -> str:
    """POST the result; a definitive refusal is acknowledged instead of replayed for minutes."""
    try:
        api.request(identity, "result", payload)
    except RuntimeError as error:
        if str(error) not in TERMINAL_HTTP:
            raise
        logging.getLogger("ifri_ocr").warning(
            "Analysis result refused by API",
            extra={"analysis_id": identity, "status": str(error)},
        )
        return "REFUSED"
    return "ACCEPTED"


def process(
    job: dict, api: InternalClient, timeout: int, minimum: float, stop: Event | None = None
) -> bool:
    if job.get("contractVersion") != 1:
        raise ValueError("Unsupported OCR contract")
    identity = job["analysisId"]
    claim = json.loads(api.request(identity, "claim", {}))
    if claim["status"] == "SKIP":
        return True
    with TemporaryDirectory(prefix="ifri-analysis-") as directory:
        pdf = Path(directory) / "transcript.pdf"
        content = api.request(identity, "document")
        if hashlib.sha256(content).hexdigest() != job["documentSha256"]:
            deliver(
                api,
                identity,
                {**failed("PDF_UNREADABLE"), "attemptNumber": claim["attemptNumber"]},
            )
            return True
        pdf.write_bytes(content)
        expected = Path(directory) / "expected.json"
        # Declared qualification lets the document series be compared; optional in contract v1.
        expected.write_text(
            json.dumps(
                {
                    "expectedSubjects": job["expectedSubjects"],
                    "qualificationCode": job.get("qualificationCode"),
                }
            )
        )
        try:
            with subprocess.Popen(
                [sys.executable, "-m", "ifri_ocr.runner", str(pdf), str(expected), str(minimum)],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                start_new_session=os.name == "posix",
                env={**os.environ, "OMP_THREAD_LIMIT": "1"},
            ) as child:
                deadline = monotonic() + timeout
                try:
                    while True:
                        if stop is not None and stop.is_set():
                            if os.name == "posix":
                                os.killpg(child.pid, signal.SIGTERM)
                            else:
                                child.terminate()
                            child.wait(timeout=3)
                            return False
                        try:
                            stdout, _ = child.communicate(
                                timeout=min(1, max(0.01, deadline - monotonic()))
                            )
                            break
                        except subprocess.TimeoutExpired:
                            if monotonic() >= deadline:
                                raise
                    if child.returncode:
                        raise subprocess.CalledProcessError(child.returncode, "ocr-runner")
                except BaseException:
                    if child.poll() is None:
                        if os.name == "posix":
                            os.killpg(child.pid, signal.SIGKILL)
                        else:
                            child.kill()
                        child.wait()
                    raise
            result = json.loads(stdout)
        except subprocess.TimeoutExpired:
            result = failed("ANALYSIS_TIMEOUT")
        except subprocess.CalledProcessError, ValueError:
            result = failed("OCR_ENGINE_ERROR")
        deliver(api, identity, {**result, "attemptNumber": claim["attemptNumber"]})
    return True
