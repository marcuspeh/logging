from __future__ import annotations

from enum import IntEnum


class Level(IntEnum):
    DEBUG = 0
    INFO = 1
    WARN = 2
    ERROR = 3
    FATAL = 4

    def __str__(self) -> str:
        return _NAMES[self]


_NAMES = {
    Level.DEBUG: "DEBUG",
    Level.INFO: "INFO",
    Level.WARN: "WARN",
    Level.ERROR: "ERROR",
    Level.FATAL: "FATAL",
}

_PARSE = {v: k for k, v in _NAMES.items()}


def ParseLevel(name: str) -> tuple[Level, bool]:
    """Parse a canonical level name (case-sensitive). Returns (Level.INFO, False) on miss."""
    return _PARSE.get(name, Level.INFO), name in _PARSE