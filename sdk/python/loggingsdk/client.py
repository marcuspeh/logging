from __future__ import annotations

import contextvars
import json
import queue
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Optional

from .levels import Level
from .producer import Producer, ConfluentProducer, drain

LOG_ID_KEY = "log_id"
"""Context key name for the correlation id. Matches the Go SDK constant."""

log_id_var: contextvars.ContextVar = contextvars.ContextVar(
    LOG_ID_KEY, default=None
)
"""Module-level contextvar carrying the active correlation id.

Bind it at the top of a request:

    token = log_id_var.set("req-7f2c")
    try:
        client.info("charge succeeded")
    finally:
        log_id_var.reset(token)
"""


def current_log_id() -> str:
    """Return the active correlation id, or ``"unknown"`` when unset/empty."""
    v = log_id_var.get()
    if isinstance(v, str) and v:
        return v
    return "unknown"


@dataclass
class Stats:
    sent: int = 0
    failed: int = 0
    dropped: int = 0


class Client:
    """Publishes log events to Kafka.

    The correlation id is pulled from :data:`log_id_var` at call time.
    Bind it before logging::

        token = log_id_var.set("req-7f2c")
        try:
            client.info("charge succeeded")
        finally:
            log_id_var.reset(token)

    By default ``log()`` (and ``info()`` / ``debug()`` / ...) synchronously
    hand the event to the underlying producer. Pass ``async_capacity>0``
    to buffer events on a background worker thread (drop-oldest on
    overflow).
    """

    def __init__(
        self,
        bootstrap: str,
        project: str,
        *,
        topic: str = "logs",
        producer: Optional[Producer] = None,
        min_level: Level = Level.DEBUG,
        async_capacity: int = 0,
        flush_interval: float = 1.0,
    ) -> None:
        if not bootstrap:
            raise ValueError("loggingsdk: bootstrap is required")
        if not project:
            raise ValueError("loggingsdk: project is required")
        if min(flush_interval, async_capacity, min_level) < 0:
            raise ValueError("loggingsdk: numeric options must be >= 0")

        self._project = project
        self._topic = topic
        self._producer = producer or ConfluentProducer(bootstrap)
        self._min_level = min_level
        self._key = project.encode("utf-8")
        self._stats_lock = threading.Lock()
        self._stats = Stats()

        self._async_capacity = async_capacity
        self._flush_interval = flush_interval
        self._queue: Optional[queue.Queue[tuple[bytes, bytes]]] = None
        self._worker: Optional[threading.Thread] = None
        self._stop = threading.Event()

        if async_capacity > 0:
            self._queue = queue.Queue(maxsize=async_capacity)
            self._worker = threading.Thread(
                target=self._run_worker,
                name="loggingsdk-worker",
                daemon=True,
            )
            self._worker.start()

    @property
    def project(self) -> str:
        return self._project

    @property
    def stats(self) -> Stats:
        with self._stats_lock:
            return Stats(self._stats.sent, self._stats.failed, self._stats.dropped)

    def log(self, level: Level, message: str, *args: Any) -> None:
        if level < self._min_level:
            return
        payload = self._encode(level, current_log_id(), message, args)
        self._dispatch(payload)

    def debug(self, message: str, *args: Any) -> None:
        self.log(Level.DEBUG, message, *args)

    def info(self, message: str, *args: Any) -> None:
        self.log(Level.INFO, message, *args)

    def warn(self, message: str, *args: Any) -> None:
        self.log(Level.WARN, message, *args)

    def error(self, message: str, *args: Any) -> None:
        self.log(Level.ERROR, message, *args)

    def fatal(self, message: str, *args: Any) -> None:
        self.log(Level.FATAL, message, *args)

    def close(self, timeout: float = 5.0) -> None:
        if self._async_capacity > 0 and self._worker is not None:
            self._stop.set()
            if self._queue is not None:
                # Poison pill to wake the worker.
                try:
                    self._queue.put_nowait((b"", b""))
                except queue.Full:
                    pass
            self._worker.join(timeout=timeout)
            self._worker = None
        drain(self._producer, timeout)

    def _encode(self, level: Level, logid: str, message: str, args: tuple[Any, ...]) -> bytes:
        if args:
            try:
                message = message % args
            except Exception:  # pragma: no cover - malformed format
                pass
        ev = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "project": self._project,
            "logid": logid,
            "level": str(level),
            "message": message,
        }
        return json.dumps(ev, separators=(",", ":")).encode("utf-8")

    def _dispatch(self, payload: bytes) -> None:
        if self._async_capacity > 0 and self._queue is not None:
            try:
                self._queue.put_nowait((self._key, payload))
                return
            except queue.Full:
                with self._stats_lock:
                    self._stats.dropped += 1
                # Drop-oldest: try to evict then enqueue.
                try:
                    self._queue.get_nowait()
                except queue.Empty:
                    pass
                with self._stats_lock:
                    self._stats.dropped += 1
                try:
                    self._queue.put_nowait((self._key, payload))
                    return
                except queue.Full:
                    return  # give up silently — caller can read stats
        self._send_sync(payload)

    def _send_sync(self, payload: bytes) -> None:
        try:
            self._producer.produce(self._topic, self._key, payload)
            self._producer.poll(0)
            with self._stats_lock:
                self._stats.sent += 1
        except Exception:  # pragma: no cover - defensive
            with self._stats_lock:
                self._stats.failed += 1

    def _run_worker(self) -> None:
        assert self._queue is not None
        next_flush = time.monotonic() + self._flush_interval
        while True:
            try:
                key, payload = self._queue.get(timeout=0.1)
                if self._stop.is_set() and not key and not payload:
                    return
                self._send_sync(payload)
            except queue.Empty:
                pass
            if time.monotonic() >= next_flush:
                self._producer.poll(0)
                next_flush = time.monotonic() + self._flush_interval