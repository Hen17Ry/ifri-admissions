import json
import logging
from unittest.mock import Mock

import pytest

from ifri_ocr import worker
from ifri_ocr.config import Config
from ifri_ocr.logging import JSONFormatter


def environment():
    return {
        "APP_ENV": "test",
        "REDIS_HOST": "localhost",
        "REDIS_PORT": "6379",
        "REDIS_PASSWORD": "test-password",
        "REDIS_TLS": "false",
    }


def test_bootstrap_checks_redis():
    config = Config.load(environment())
    client = Mock()
    client.ping.return_value = True
    worker.bootstrap(config, client)
    client.ping.assert_called_once()


@pytest.mark.parametrize(
    "key,value",
    [
        ("REDIS_PASSWORD", ""),
        ("REDIS_PORT", "0"),
        ("REDIS_PORT", "invalid"),
        ("APP_ENV", "prod"),
        ("APP_ENV", "production"),
        ("REDIS_TLS", "yes"),
    ],
)
def test_invalid_config_is_rejected(key, value):
    values = environment()
    values[key] = value
    with pytest.raises(ValueError):
        Config.load(values)


def test_failed_ping_never_reports_ready():
    client = Mock()
    client.ping.return_value = False
    with pytest.raises(RuntimeError):
        worker.bootstrap(Config.load(environment()), client)


def test_heartbeat_expires(tmp_path, monkeypatch):
    heartbeat = tmp_path / "heartbeat"
    monkeypatch.setattr(worker, "HEARTBEAT", heartbeat)
    monkeypatch.setattr(worker, "time", lambda: 100)
    assert worker.healthcheck() == 1
    heartbeat.write_text("95")
    assert worker.healthcheck() == 0
    heartbeat.write_text("70")
    assert worker.healthcheck() == 1


def test_structured_logs_have_context():
    record = logging.LogRecord("test", logging.INFO, "", 0, "ready", (), None)
    value = json.loads(JSONFormatter("test").format(record))
    assert value["service"] == "ocr-worker"
    assert value["environment"] == "test"
    assert value["level"] == "INFO"
    assert value["timestamp"]
