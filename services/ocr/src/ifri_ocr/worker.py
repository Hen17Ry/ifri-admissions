import signal
import sys
from pathlib import Path
from tempfile import gettempdir
from threading import Event
from time import time

from redis import Redis
from redis.exceptions import RedisError

from ifri_ocr.config import Config
from ifri_ocr.logging import configure

# Internal heartbeat only: no public HTTP API, no queue consumer.
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
    configure(config.environment).info("Worker ready; document processing is not enabled")


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
        while not stop.is_set():
            if not client.ping():
                raise RuntimeError("Redis readiness check failed")
            HEARTBEAT.write_text(str(time()))
            stop.wait(5)
    except RedisError, RuntimeError, OSError:
        # Redis exceptions can include connection details; never log credentials.
        logger.error("Worker unavailable: Redis connection or heartbeat failed")
        return 1
    finally:
        HEARTBEAT.unlink(missing_ok=True)
        client.close()
    logger.info("Worker stopped")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
