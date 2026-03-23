"""LLM interaction layer using the OpenAI Python SDK.

Supports both structured-output mode (``json_schema``) and plain JSON-object
mode, as well as text and base64-image inputs.
"""

from __future__ import annotations

import json
import time
from typing import Any

from openai import AsyncOpenAI

from .loader import load_schema
from .logger import create_logger, serialize_error
from .types import StepMetrics

log = create_logger("llm")

# Module-level client — lazily initialised so the import itself never fails
# when OPENAI_API_KEY is not yet set.
_client: AsyncOpenAI | None = None


def _get_client() -> AsyncOpenAI:
    global _client
    if _client is None:
        _client = AsyncOpenAI()
    return _client


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

async def call_llm(
    model: str,
    system_prompt: str,
    user_content: str | dict[str, Any],
    temperature: float = 0.0,
    schema_name: str | None = None,
    input_type: str = "text",
    image_detail: str = "auto",
) -> tuple[dict[str, Any], StepMetrics]:
    """Call an LLM and return ``(parsed_output, metrics)``.

    Parameters
    ----------
    model:
        Model identifier (e.g. ``gpt-4.1``).
    system_prompt:
        System-level instructions.
    user_content:
        The user message body.  For text input this is a plain string.  For
        image input this should be a base64-encoded data URI string.
    temperature:
        Sampling temperature.
    schema_name:
        If set, the response is requested as structured output conforming to
        the named JSON schema.  If ``None``, plain ``json_object`` mode is
        used.
    input_type:
        ``"text"`` (default) or ``"image"``.
    image_detail:
        Vision detail level (``"low"``, ``"high"``, ``"auto"``).  Only used
        when *input_type* is ``"image"``.
    """
    client = _get_client()

    # -- Build messages -------------------------------------------------------
    messages: list[dict[str, Any]] = []

    if system_prompt:
        messages.append({"role": "system", "content": system_prompt})

    if input_type == "image" and isinstance(user_content, str):
        # Build a multimodal user message with a base64 image URL.
        image_url = user_content
        if not user_content.startswith("data:"):
            image_url = f"data:image/png;base64,{user_content}"
        messages.append({
            "role": "user",
            "content": [
                {
                    "type": "image_url",
                    "image_url": {"url": image_url, "detail": image_detail},
                }
            ],
        })
    else:
        # Plain text user message.
        text = user_content if isinstance(user_content, str) else json.dumps(user_content, default=str)
        messages.append({"role": "user", "content": text})

    # -- Build response_format ------------------------------------------------
    response_format: dict[str, Any]
    if schema_name is not None:
        schema_obj = load_schema(schema_name)
        # The file on disk has the OpenAI wrapper structure:
        #   { "name": "...", "strict": true, "schema": { ... } }
        # Build the response_format accordingly.
        response_format = {
            "type": "json_schema",
            "json_schema": {
                "name": schema_obj.get("name", schema_name),
                "schema": schema_obj.get("schema", schema_obj),
                "strict": schema_obj.get("strict", True),
            },
        }
    else:
        response_format = {"type": "json_object"}

    # -- Call the API ---------------------------------------------------------
    log.info(
        "Calling LLM",
        model=model,
        schema_name=schema_name,
        temperature=temperature,
        input_type=input_type,
    )

    start = time.monotonic()
    try:
        response = await client.chat.completions.create(
            model=model,
            messages=messages,
            temperature=temperature,
            response_format=response_format,
        )
    except Exception as exc:
        log.error("LLM call failed", **serialize_error(exc))
        raise

    elapsed_ms = (time.monotonic() - start) * 1000

    # -- Parse response -------------------------------------------------------
    choice = response.choices[0]
    raw_text = choice.message.content or "{}"

    try:
        output = json.loads(raw_text)
    except json.JSONDecodeError:
        log.warn("LLM returned non-JSON content, wrapping as text", raw=raw_text[:200])
        output = {"raw": raw_text}

    usage = response.usage
    metrics = StepMetrics(
        duration_ms=round(elapsed_ms, 2),
        prompt_tokens=usage.prompt_tokens if usage else 0,
        completion_tokens=usage.completion_tokens if usage else 0,
        total_tokens=usage.total_tokens if usage else 0,
    )

    log.info(
        "LLM call completed",
        duration_ms=metrics.duration_ms,
        total_tokens=metrics.total_tokens,
    )

    return output, metrics
