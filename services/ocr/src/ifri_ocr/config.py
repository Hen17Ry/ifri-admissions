from collections.abc import Mapping
from dataclasses import dataclass, field
from os import environ


@dataclass(frozen=True)
class Config:
    environment: str
    redis_host: str
    redis_port: int
    redis_password: str = field(repr=False)
    redis_tls: bool

    @classmethod
    def load(cls, env: Mapping[str, str] | None = None) -> Config:
        values = environ if env is None else env
        for name in ("APP_ENV", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_TLS"):
            if not values.get(name, "").strip():
                raise ValueError(f"{name} is required")
        environment = values["APP_ENV"]
        if environment not in {"development", "test", "production"}:
            raise ValueError("APP_ENV must be development, test or production")
        try:
            port = int(values["REDIS_PORT"])
        except ValueError:
            raise ValueError("REDIS_PORT must be a valid TCP port") from None
        if not 1 <= port <= 65535:
            raise ValueError("REDIS_PORT must be a valid TCP port")
        if values["REDIS_TLS"] not in {"true", "false"}:
            raise ValueError("REDIS_TLS must be true or false")
        tls = values["REDIS_TLS"] == "true"
        if environment == "production" and (
            not tls or values["REDIS_PASSWORD"].startswith("replace-dev-")
        ):
            raise ValueError("production requires Redis TLS and non-development credentials")
        return cls(environment, values["REDIS_HOST"], port, values["REDIS_PASSWORD"], tls)
