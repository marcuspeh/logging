"""logging-sdk: producer-side Python client for the centralised logging service.

Mirrors the Go SDK (sdk/go). Log events are serialised to JSON and
published to a Kafka topic consumed by the logging-backend service.

The correlation id is read from the module-level
:data:`log_id_var` contextvar (``LOG_ID_KEY = "log_id"``), matching the
Go SDK's constant.
"""

from .levels import Level, ParseLevel
from .client import Client, Stats, current_log_id, log_id_var, LOG_ID_KEY
from .producer import Producer, ConfluentProducer
from .handler import LoggingHandler

__all__ = [
    "Client",
    "ConfluentProducer",
    "Level",
    "LOG_ID_KEY",
    "LoggingHandler",
    "ParseLevel",
    "Producer",
    "Stats",
    "current_log_id",
    "log_id_var",
]