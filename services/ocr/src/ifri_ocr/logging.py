import json
import logging
import sys
from datetime import UTC, datetime


class JSONFormatter(logging.Formatter):
    def __init__(self, environment: str) -> None:
        super().__init__()
        self.environment = environment

    def format(self, record: logging.LogRecord) -> str:
        return json.dumps(
            {
                "timestamp": datetime.now(UTC).isoformat(),
                "level": record.levelname,
                "service": "ocr-worker",
                "environment": self.environment,
                "message": record.getMessage(),
            }
        )


def configure(environment: str) -> logging.Logger:
    logger = logging.getLogger("ifri_ocr")
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter(environment))
    logger.handlers = [handler]
    logger.setLevel(logging.INFO)
    logger.propagate = False
    return logger
