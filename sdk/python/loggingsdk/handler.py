from __future__ import annotations

import logging

from .client import Client, current_log_id
from .levels import Level


_LEVELS: dict[int, Level] = {
    logging.DEBUG: Level.DEBUG,
    logging.INFO: Level.INFO,
    logging.WARNING: Level.WARN,
    logging.ERROR: Level.ERROR,
    logging.CRITICAL: Level.FATAL,
}


class LoggingHandler(logging.Handler):
    """Routes stdlib ``logging`` records through a :class:`Client`.

    The correlation id is read from the active :data:`log_id_var`
    contextvar, so callers should bind one before logging::

        token = log_id_var.set("req-7f2c")
        try:
            logging.info("user authenticated")
        finally:
            log_id_var.reset(token)

    Usage::

        client = loggingsdk.Client("kafka:9092", "billing-service")
        handler = loggingsdk.LoggingHandler(client)
        logging.getLogger().addHandler(handler)
        logging.getLogger().setLevel(logging.INFO)
    """

    def __init__(self, client: Client, level: int = logging.NOTSET) -> None:
        super().__init__(level=level)
        self._client = client

    def emit(self, record: logging.LogRecord) -> None:
        level = _LEVELS.get(record.levelno, Level.INFO)
        try:
            self._client.log(level, record.getMessage())
        except Exception:  # pragma: no cover - never raise from logging
            self.handleError(record)

    @staticmethod
    def current_log_id() -> str:
        """Expose the active correlation id (handy in formatters)."""
        return current_log_id()