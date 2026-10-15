import json
import os
import shutil
import signal
import socket
import subprocess
import sys
from pathlib import Path
from tempfile import gettempdir
from threading import Event, Thread
from time import time
from urllib.parse import urlparse

from redis import Redis
from redis.exceptions import RedisError

from ifri_ocr.config import Config
from ifri_ocr.jobs import GROUP, STREAM, InternalClient, process
from ifri_ocr.logging import configure

# Internal heartbeat: separate thread keeps Redis readiness fresh during bounded analysis.
HEARTBEAT = Path(gettempdir()) / "ifri-ocr-heartbeat"


def connect(config: Config) -> Redis:
    return Redis(
        host=config.redis_host,
        port=config.redis_port,
        password=config.redis_password,
        ssl=config.redis_tls,
        socket_connect_timeout=2,
        socket_timeout=2,
        decode_responses=True,
    )


def bootstrap(config: Config, client: Redis) -> None:
    if not client.ping():
        raise RuntimeError("Redis readiness check failed")
    configure(config.environment).info("Worker Redis connection ready")


def healthcheck() -> int:
    try:
        age = time() - float(HEARTBEAT.read_text())
        return 0 if 0 <= age < 20 else 1
    except OSError, ValueError:
        return 1


def main() -> int:
    if sys.argv[1:] == ["healthcheck"]:
        return healthcheck()
    logger = configure("unknown")
    try:
        config = Config.load()
        logger = configure(config.environment)
    except ValueError as error:
        logger.error(str(error))
        return 1
    stop = Event()
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, lambda *_: stop.set())
    HEARTBEAT.unlink(missing_ok=True)
    client = connect(config)
    try:
        bootstrap(config, client)
        base = os.environ.get("OCR_INTERNAL_URL", "")
        secret = os.environ.get("OCR_INTERNAL_SECRET", "")
        if (
            len(secret) < 32
            or urlparse(base).scheme not in {"http", "https"}
            or not urlparse(base).hostname
        ):
            raise RuntimeError("OCR internal configuration invalid")
        if config.environment == "production" and (
            secret.startswith("replace-dev-") or urlparse(base).scheme != "https"
        ):
            raise RuntimeError("Production OCR internal configuration invalid")
        from ifri_ocr.bac.reading import cell_workers
        from ifri_ocr.scans import ScanSettings

        ScanSettings.load()
        cell_workers()
        timeout = int(os.environ.get("OCR_ANALYSIS_TIMEOUT_SECONDS", "120"))
        minimum = float(os.environ.get("OCR_MIN_CONFIDENCE", "0.85"))
        if not 10 <= timeout <= 240 or not 0.5 <= minimum <= 1:
            raise RuntimeError("OCR limits invalid")
        page_timeout = int(os.environ.get("OCR_PAGE_TIMEOUT_SECONDS", "30"))
        max_pages = int(os.environ.get("OCR_MAX_PAGES", "10"))
        if not 5 <= page_timeout <= 120 or not 1 <= max_pages <= 100:
            raise RuntimeError("OCR page limits invalid")
        if not shutil.which("tesseract"):
            raise RuntimeError("Tesseract required")
        languages = subprocess.run(
            ["tesseract", "--list-langs"], capture_output=True, check=True, timeout=5
        ).stdout.decode()
        if not all(language in languages.split() for language in ("fra", "eng")):
            raise RuntimeError("Tesseract fra/eng required")
        from ifri_ocr.engines import cell_reader

        # Numeric cell reader (RapidOCR by default): pinned model intact and one inference run.
        reader = cell_reader()
        reader.verify()
        api = InternalClient(base, secret)
        if not api.ready():
            raise RuntimeError("Internal API readiness failed")
        try:
            client.xgroup_create(STREAM, GROUP, id="0", mkstream=True)
        except RedisError as error:
            if "BUSYGROUP" not in str(error):
                raise

        def heartbeat():
            while not stop.is_set():
                try:
                    if client.ping() and api.ready():
                        HEARTBEAT.write_text(str(time()))
                except RedisError, OSError:
                    HEARTBEAT.unlink(missing_ok=True)
                stop.wait(5)

        pulse = Thread(target=heartbeat, daemon=True)
        pulse.start()
        consumer = socket.gethostname()
        # OCR proofs: own Redis connection and loop, never queued behind analyses.
        from ifri_ocr.proofs import ProofClient, serve_proofs

        proofs = Thread(
            target=serve_proofs,
            args=(connect(config), ProofClient(base, secret), consumer, stop),
            daemon=True,
        )
        proofs.start()
        # Ranking list exports (PDF): their own stream, connection and loop as well.
        from ifri_ocr.exports import FONT, FONT_BOLD, ExportClient, serve_exports

        if not (FONT.is_file() and FONT_BOLD.is_file()):
            raise RuntimeError("DejaVu fonts missing: list exports cannot be rendered")
        exports = Thread(
            target=serve_exports,
            args=(connect(config), ExportClient(base, secret), consumer, stop),
            daemon=True,
        )
        exports.start()
        logger.info("Worker ready; hybrid extraction enabled", extra={"engine": reader.name})
        while not stop.is_set():
            reclaimed = client.xautoclaim(
                STREAM, GROUP, consumer, min_idle_time=300000, start_id="0-0", count=1
            )
            messages = reclaimed[1]
            if not messages:
                batches = client.xreadgroup(GROUP, consumer, {STREAM: ">"}, count=1, block=1000)
                messages = batches[0][1] if batches else []
            for message_id, fields in messages:
                try:
                    job = json.loads(fields["job"])
                    started = time()
                    if process(job, api, timeout, minimum, stop):
                        client.xack(STREAM, GROUP, message_id)
                    logger.info(
                        "Analysis callback acknowledged",
                        extra={
                            "analysis_id": job["analysisId"],
                            "document_version_id": job["documentVersionId"],
                            "duration_seconds": round(time() - started, 3),
                        },
                    )
                except RuntimeError, OSError, ValueError, KeyError:
                    logger.error("Analysis transport failed; pending job retained")
    except (RuntimeError, ValueError) as error:
        # Static readiness reasons only (configuration, Tesseract, RapidOCR model); no secrets.
        logger.error("Worker unavailable", extra={"failure_code": str(error)[:100]})
        return 1
    except RedisError, OSError, subprocess.SubprocessError:
        # Redis exceptions can include connection details; never log credentials.
        logger.error("Worker unavailable: Redis connection or heartbeat failed")
        return 1
    finally:
        stop.set()
        if "pulse" in locals():
            pulse.join(timeout=6)
        HEARTBEAT.unlink(missing_ok=True)
        client.close()
    logger.info("Worker stopped")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
