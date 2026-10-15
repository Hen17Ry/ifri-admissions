"""One consumer loop per administration stream (OCR proofs, list exports).

Each loop has its own Redis connection and consumer group, apart from the analysis loop: an
administration job never waits behind candidates' analyses, and the reverse.
"""

import json
import logging
from collections.abc import Callable
from threading import Event


def serve_stream(
    client,
    stream: str,
    group: str,
    consumer: str,
    stop: Event,
    handle: Callable[[dict], str],
    label: str,
    id_key: str,
) -> None:
    """Read, handle and acknowledge jobs; transport errors keep the job for a later retry."""
    logger = logging.getLogger("ifri_ocr")
    try:
        client.xgroup_create(stream, group, id="0", mkstream=True)
    except Exception as error:  # noqa: BLE001 - redis raises ResponseError for BUSYGROUP
        if "BUSYGROUP" not in str(error):
            raise
    while not stop.is_set():
        try:
            reclaimed = client.xautoclaim(
                stream, group, consumer, min_idle_time=120000, start_id="0-0", count=1
            )
            messages = reclaimed[1]
            if not messages:
                batches = client.xreadgroup(group, consumer, {stream: ">"}, count=1, block=1000)
                messages = batches[0][1] if batches else []
            for message_id, fields in messages:
                job = json.loads(fields["job"])
                outcome = handle(job)
                client.xack(stream, group, message_id)
                logger.info(
                    f"{label} job handled",
                    extra={id_key: job.get(_camel(id_key)), "outcome": outcome},
                )
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            logger.error(
                f"{label} transport failed; job retained", extra={"failure_code": str(error)[:40]}
            )
            stop.wait(2)
        except Exception:  # noqa: BLE001 - keep the loop alive whatever happens here
            logger.error(f"{label} loop error; retrying")
            stop.wait(2)


def _camel(snake: str) -> str:
    head, *rest = snake.split("_")
    return head + "".join(part.title() for part in rest)
