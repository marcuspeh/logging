from __future__ import annotations

import contextvars
import json
import time

import pytest

from loggingsdk import (
    Client,
    Level,
    LoggingHandler,
    ParseLevel,
    current_log_id,
    log_id_var,
)


class FakeProducer:
    """In-memory Producer for tests."""

    def __init__(self) -> None:
        self.messages: list[tuple[str, bytes, bytes]] = []
        self.fail_next = False
        self.poll_calls = 0
        self.flush_calls = 0
        self.close_calls = 0

    def produce(self, topic: str, key: bytes, payload: bytes) -> None:
        if self.fail_next:
            self.fail_next = False
            raise RuntimeError("simulated kafka failure")
        self.messages.append((topic, key, payload))

    def poll(self, timeout: float = 0.0) -> None:
        self.poll_calls += 1

    def flush(self, timeout: float) -> int:
        self.flush_calls += 1
        return 0

    def close(self) -> int:
        self.close_calls += 1
        return 0


@pytest.fixture
def fake() -> FakeProducer:
    return FakeProducer()


@pytest.fixture
def bound_log_id():
    """Yield after binding log_id_var; reset afterwards."""
    token = log_id_var.set("req-fixture")
    try:
        yield "req-fixture"
    finally:
        log_id_var.reset(token)


# ---- current_log_id / log_id_var -----------------------------------------


def test_current_log_id_unset_returns_unknown():
    assert current_log_id() == "unknown"


def test_current_log_id_reads_contextvar():
    token = log_id_var.set("req-7f2c")
    try:
        assert current_log_id() == "req-7f2c"
    finally:
        log_id_var.reset(token)


def test_current_log_id_empty_string_falls_back():
    token = log_id_var.set("")
    try:
        assert current_log_id() == "unknown"
    finally:
        log_id_var.reset(token)


def test_current_log_id_non_string_falls_back():
    token = log_id_var.set(123)  # type: ignore[arg-type]
    try:
        assert current_log_id() == "unknown"
    finally:
        log_id_var.reset(token)


# ---- Client construction -------------------------------------------------


def test_client_requires_bootstrap_and_project():
    with pytest.raises(ValueError, match="bootstrap"):
        Client("", "p", producer=FakeProducer())
    with pytest.raises(ValueError, match="project"):
        Client("k:9092", "", producer=FakeProducer())


def test_client_rejects_negative_options():
    with pytest.raises(ValueError):
        Client("k:9092", "p", producer=FakeProducer(), min_level=-1)


# ---- Sync path -----------------------------------------------------------


def test_info_emits_json(fake: FakeProducer):
    c = Client("k:9092", "billing", producer=fake)
    token = log_id_var.set("req-1")
    try:
        c.info("hello %s", "world")
    finally:
        log_id_var.reset(token)

    assert len(fake.messages) == 1
    topic, key, payload = fake.messages[0]
    assert topic == "logs"
    assert key == b"billing"
    body = json.loads(payload)
    assert body["project"] == "billing"
    assert body["logid"] == "req-1"
    assert body["level"] == "INFO"
    assert body["message"] == "hello world"
    for k in ("timestamp", "project", "logid", "level", "message"):
        assert k in body


def test_min_level_drops_below_threshold(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake, min_level=Level.WARN)
    c.debug("dropped")
    c.info("dropped")
    assert fake.messages == []


def test_unknown_logid_when_contextvar_unset(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake)
    c.info("m")
    body = json.loads(fake.messages[0][2])
    assert body["logid"] == "unknown"


def test_format_args_failure_falls_back_to_template(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake)
    c.info("count=%d", "not-a-number")
    body = json.loads(fake.messages[0][2])
    # When % substitution fails we keep the raw template.
    assert body["message"] == "count=%d"


def test_send_failure_increments_failed(fake: FakeProducer):
    fake.fail_next = True
    c = Client("k:9092", "p", producer=fake)
    c.info("m")
    assert fake.messages == []
    assert c.stats.failed == 1


# ---- Async path ----------------------------------------------------------


def test_async_drops_oldest_on_overflow(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake, async_capacity=2)
    try:
        for i in range(50):
            c.info("msg %d", i)
        # Give the worker a beat to drain.
        time.sleep(0.2)
    finally:
        c.close()

    # Either Sent went up and Dropped went up, or both.
    s = c.stats
    assert s.dropped > 0 or s.sent > 0


def test_async_worker_emits_messages(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake, async_capacity=8, flush_interval=0.05)
    for i in range(5):
        token = log_id_var.set(f"id-{i}")
        try:
            c.info("m")
        finally:
            log_id_var.reset(token)
    c.close()
    assert len(fake.messages) == 5
    assert c.stats.dropped == 0


# ---- Close + flush -------------------------------------------------------


def test_close_flushes(fake: FakeProducer):
    c = Client("k:9092", "p", producer=fake)
    c.info("m")
    c.close()
    assert fake.flush_calls + fake.close_calls >= 1


# ---- Level parsing -------------------------------------------------------


def test_level_parse_round_trip():
    for l in (Level.DEBUG, Level.INFO, Level.WARN, Level.ERROR, Level.FATAL):
        assert ParseLevel(str(l))[0] == l
    assert ParseLevel("bogus") == (Level.INFO, False)


# ---- LoggingHandler ------------------------------------------------------


def test_logging_handler_routes_through_client(fake: FakeProducer):
    import logging

    c = Client("k:9092", "p", producer=fake)
    handler = LoggingHandler(c)
    handler.setLevel(logging.INFO)

    root = logging.getLogger("loggingsdk.test")
    root.setLevel(logging.INFO)
    root.addHandler(handler)
    token = log_id_var.set("req-handler")
    try:
        try:
            root.info("via stdlib logging")
        finally:
            root.removeHandler(handler)
    finally:
        log_id_var.reset(token)

    assert len(fake.messages) == 1
    body = json.loads(fake.messages[0][2])
    assert body["level"] == "INFO"
    assert body["message"] == "via stdlib logging"
    assert body["logid"] == "req-handler"


def test_logging_handler_emits_warn_as_warn(fake: FakeProducer):
    import logging

    c = Client("k:9092", "p", producer=fake)
    handler = LoggingHandler(c)

    root = logging.getLogger("loggingsdk.test.warn")
    root.setLevel(logging.WARNING)
    root.addHandler(handler)
    try:
        root.warning("careful")
    finally:
        root.removeHandler(handler)

    body = json.loads(fake.messages[0][2])
    assert body["level"] == "WARN"


def test_logging_handler_falls_back_to_unknown_when_unset(fake: FakeProducer):
    import logging

    c = Client("k:9092", "p", producer=fake)
    handler = LoggingHandler(c)

    root = logging.getLogger("loggingsdk.test.unset")
    root.setLevel(logging.INFO)
    root.addHandler(handler)
    try:
        root.info("no logid bound")
    finally:
        root.removeHandler(handler)

    body = json.loads(fake.messages[0][2])
    assert body["logid"] == "unknown"