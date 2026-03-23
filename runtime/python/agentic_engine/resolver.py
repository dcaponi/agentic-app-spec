"""Reference resolution utilities.

Handles ``$.path`` bindings used in workflow step inputs/outputs and
``{{key}}`` template interpolation used in agent user messages.
"""

from __future__ import annotations

import json
import re
from typing import Any

from .logger import create_logger
from .types import ExecutionContext

log = create_logger("resolver")


# ---------------------------------------------------------------------------
# $.path reference resolution
# ---------------------------------------------------------------------------

def resolve_ref(ref: str, context: ExecutionContext) -> Any:
    """Resolve a ``$.``-prefixed reference against the execution context.

    Supported forms:
      - ``$.input.key``         -> context.input[key]
      - ``$.steps.sid.output``  -> context.steps[sid]["output"]
      - ``$.steps.sid.output.f`` -> context.steps[sid]["output"][f]

    If *ref* does not start with ``$.``, it is returned as a literal value.
    """
    if not isinstance(ref, str) or not ref.startswith("$."):
        return ref

    parts = ref[2:].split(".")  # strip leading "$."

    # Navigate into either context.input or context.steps
    current: Any
    root = parts[0]
    if root == "input":
        current = context.input
        parts = parts[1:]
    elif root == "steps":
        current = context.steps
        parts = parts[1:]
    else:
        raise ValueError(f"Unknown reference root '{root}' in '{ref}'")

    for part in parts:
        if isinstance(current, dict):
            if part not in current:
                raise KeyError(f"Reference '{ref}': key '{part}' not found in dict")
            current = current[part]
        elif isinstance(current, list):
            current = current[int(part)]
        else:
            current = getattr(current, part)

    return current


# ---------------------------------------------------------------------------
# Input / output binding resolution
# ---------------------------------------------------------------------------

def resolve_inputs(
    bindings: dict[str, Any], context: ExecutionContext
) -> dict[str, Any]:
    """Resolve a dict of input bindings.

    Values that are ``$.``-prefixed strings are resolved as refs; everything
    else is passed through as a literal.
    """
    resolved: dict[str, Any] = {}
    for key, value in bindings.items():
        if isinstance(value, str) and value.startswith("$."):
            resolved[key] = resolve_ref(value, context)
        else:
            resolved[key] = value
    return resolved


def resolve_outputs(
    bindings: dict[str, str], context: ExecutionContext
) -> dict[str, Any]:
    """Resolve workflow-level output bindings."""
    resolved: dict[str, Any] = {}
    for key, ref in bindings.items():
        resolved[key] = resolve_ref(ref, context)
    return resolved


# ---------------------------------------------------------------------------
# Template interpolation
# ---------------------------------------------------------------------------

_TEMPLATE_RE = re.compile(r"\{\{(.+?)\}\}")


def _deep_get(obj: Any, dotted_key: str) -> Any:
    """Navigate into *obj* using a dotted key path like ``key.sub``."""
    parts = dotted_key.strip().split(".")
    current = obj
    for part in parts:
        if isinstance(current, dict):
            current = current[part]
        else:
            current = getattr(current, part)
    return current


def _stringify(value: Any) -> str:
    """Convert a value to a string suitable for template insertion."""
    if isinstance(value, str):
        return value
    if isinstance(value, (dict, list)):
        return json.dumps(value, indent=2, default=str)
    return str(value)


def resolve_template(template: str, input_data: dict[str, Any]) -> str:
    """Replace ``{{key}}`` and ``{{key.sub}}`` placeholders with values.

    Values are sourced from *input_data*.  Dicts and lists are JSON-serialized
    for readability.
    """

    def _replacer(match: re.Match[str]) -> str:
        dotted_key = match.group(1)
        try:
            value = _deep_get(input_data, dotted_key)
            return _stringify(value)
        except (KeyError, AttributeError, TypeError) as exc:
            log.warn(
                "Template placeholder could not be resolved",
                placeholder=dotted_key,
                error=str(exc),
            )
            return match.group(0)  # leave placeholder intact

    return _TEMPLATE_RE.sub(_replacer, template)
