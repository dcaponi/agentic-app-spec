"""Structured logging utilities for the Agentic Engine runtime."""

from __future__ import annotations

import json
import sys
import traceback
from datetime import datetime, timezone
from typing import Any


class StructuredLogger:
    """Emits structured JSON log lines to stderr."""

    def __init__(self, component: str) -> None:
        self._component = component

    def _emit(self, level: str, message: str, **extra: Any) -> None:
        entry: dict[str, Any] = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "level": level,
            "component": self._component,
            "message": message,
        }
        if extra:
            entry["data"] = extra
        line = json.dumps(entry, default=str)
        sys.stderr.write(line + "\n")
        sys.stderr.flush()

    def debug(self, message: str, **extra: Any) -> None:
        self._emit("debug", message, **extra)

    def info(self, message: str, **extra: Any) -> None:
        self._emit("info", message, **extra)

    def warn(self, message: str, **extra: Any) -> None:
        self._emit("warn", message, **extra)

    def error(self, message: str, **extra: Any) -> None:
        self._emit("error", message, **extra)


def create_logger(component: str) -> StructuredLogger:
    """Create a structured logger for the given component name."""
    return StructuredLogger(component)


def serialize_error(err: BaseException) -> dict[str, Any]:
    """Extract a serializable dict from an exception."""
    return {
        "name": type(err).__name__,
        "message": str(err),
        "stack": "".join(traceback.format_exception(type(err), err, err.__traceback__)),
    }
