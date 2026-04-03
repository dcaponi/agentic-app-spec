"""LLM interaction layer supporting OpenAI and Anthropic providers.

Supports both structured-output mode (JSON Schema in system prompt for Anthropic,
``json_schema`` response format for OpenAI) and plain JSON-object mode, as well
as text and base64-image inputs.
"""

from __future__ import annotations

import json
import os
import time
from typing import Any

from anthropic import AsyncAnthropic
from openai import AsyncOpenAI

from .loader import load_schema
from .logger import create_logger, serialize_error
from .types import StepMetrics

log = create_logger("llm")

# Module-level clients — lazily initialised so the import itself never fails
# when API keys are not yet set.
_openai_client: AsyncOpenAI | None = None
_anthropic_client: AsyncAnthropic | None = None


def _get_openai_client() -> AsyncOpenAI:
    global _openai_client
    if _openai_client is None:
        _openai_client = AsyncOpenAI()
    return _openai_client


def _get_anthropic_client() -> AsyncAnthropic:
    global _anthropic_client
    if _anthropic_client is None:
        _anthropic_client = AsyncAnthropic()
    return _anthropic_client


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
    base_url: str = "",
    api_key_env: str = "",
) -> tuple[dict[str, Any], StepMetrics]:
    """Call an LLM and return ``(parsed_output, metrics)``.

    Parameters
    ----------
    model:
        Model identifier (e.g. ``gpt-4.1`` or ``claude-sonnet-4-5-20241022``).
    system_prompt:
        System-level instructions.
    user_content:
        The user message body.  For text input this is a plain string.  For
        image input this should be a base64-encoded data URI string.
    temperature:
        Sampling temperature.
    schema_name:
        If set, the response is requested as structured output conforming to
        the named JSON schema.  If ``None``, plain JSON mode is used.
    input_type:
        ``"text"`` (default) or ``"image"``.
    image_detail:
        Vision detail level (``"low"``, ``"high"``, ``"auto"``).  Only used
        when *input_type* is ``"image"``.
    base_url:
        Optional base URL for an OpenAI-compatible API endpoint.
    api_key_env:
        Environment variable name to read the API key from when *base_url* is
        set.  If empty (and base_url is set), ``"not-needed"`` is used.
    """
    if base_url:
        api_key = os.environ.get(api_key_env, "not-needed") if api_key_env else "not-needed"
        client = AsyncOpenAI(base_url=base_url, api_key=api_key)
        log.info(
            "Calling LLM",
            provider="openai-compatible",
            base_url=base_url,
            model=model,
            schema_name=schema_name,
            temperature=temperature,
            input_type=input_type,
        )
        return await _call_openai(
            model=model,
            system_prompt=system_prompt,
            user_content=user_content,
            temperature=temperature,
            schema_name=schema_name,
            input_type=input_type,
            image_detail=image_detail,
            client=client,
        )
    elif model.startswith("claude-"):
        log.info(
            "Calling LLM",
            provider="anthropic",
            model=model,
            schema_name=schema_name,
            temperature=temperature,
            input_type=input_type,
        )
        return await _call_anthropic(
            model=model,
            system_prompt=system_prompt,
            user_content=user_content,
            temperature=temperature,
            schema_name=schema_name,
            input_type=input_type,
            image_detail=image_detail,
        )
    else:
        log.info(
            "Calling LLM",
            provider="openai",
            model=model,
            schema_name=schema_name,
            temperature=temperature,
            input_type=input_type,
        )
        return await _call_openai(
            model=model,
            system_prompt=system_prompt,
            user_content=user_content,
            temperature=temperature,
            schema_name=schema_name,
            input_type=input_type,
            image_detail=image_detail,
        )


# ---------------------------------------------------------------------------
# OpenAI implementation
# ---------------------------------------------------------------------------

async def _call_openai(
    model: str,
    system_prompt: str,
    user_content: str | dict[str, Any],
    temperature: float,
    schema_name: str | None,
    input_type: str,
    image_detail: str,
    client: AsyncOpenAI | None = None,
) -> tuple[dict[str, Any], StepMetrics]:
    client = client or _get_openai_client()

    # -- Build messages -------------------------------------------------------
    messages: list[dict[str, Any]] = []

    if system_prompt:
        messages.append({"role": "system", "content": system_prompt})

    if input_type == "image" and isinstance(user_content, str):
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
        text = user_content if isinstance(user_content, str) else json.dumps(user_content, default=str)
        messages.append({"role": "user", "content": text})

    # -- Build response_format ------------------------------------------------
    response_format: dict[str, Any]
    if schema_name is not None:
        schema_obj = load_schema(schema_name)
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
    start = time.monotonic()
    try:
        response = await client.chat.completions.create(
            model=model,
            messages=messages,
            temperature=temperature,
            response_format=response_format,
        )
    except Exception as exc:
        ei = serialize_error(exc)
        log.error("OpenAI call failed", error=ei["message"], error_name=ei["name"])
        raise

    elapsed_ms = (time.monotonic() - start) * 1000

    # -- Parse response -------------------------------------------------------
    choice = response.choices[0]
    raw_text = choice.message.content or "{}"

    try:
        output = json.loads(raw_text)
    except json.JSONDecodeError:
        log.warn("OpenAI returned non-JSON content, wrapping as text", raw=raw_text[:200])
        output = {"raw": raw_text}

    usage = response.usage
    metrics = StepMetrics(
        duration_ms=round(elapsed_ms, 2),
        prompt_tokens=usage.prompt_tokens if usage else 0,
        completion_tokens=usage.completion_tokens if usage else 0,
        total_tokens=usage.total_tokens if usage else 0,
    )

    log.info(
        "OpenAI call completed",
        duration_ms=metrics.duration_ms,
        total_tokens=metrics.total_tokens,
    )

    return output, metrics


# ---------------------------------------------------------------------------
# Anthropic implementation
# ---------------------------------------------------------------------------

def _build_anthropic_system_prompt(
    base: str, schema_name: str | None
) -> str:
    """Augment the system prompt with JSON output instructions.

    When *schema_name* is provided the corresponding JSON schema is embedded
    so the model returns structured output.
    """
    if schema_name is not None:
        try:
            schema_obj = load_schema(schema_name)
            schema_body = schema_obj.get("schema", schema_obj)
            schema_json = json.dumps(schema_body, indent=2)
            return (
                f"{base}\n\nYou must respond with a valid JSON object that "
                f"conforms to the following JSON schema:\n{schema_json}\n\n"
                "Output only the JSON with no additional text, markdown, or code fences."
            )
        except Exception:
            pass  # Fall through to basic JSON instruction

    return (
        f"{base}\n\nYou must respond with a valid JSON object. "
        "Output only the JSON with no additional text, markdown, or code fences."
    )


def _build_anthropic_messages(
    user_content: str | dict[str, Any],
    input_type: str,
    image_detail: str,
) -> list[dict[str, Any]]:
    """Build Anthropic-format messages from user content."""
    if input_type == "image" and isinstance(user_content, str):
        image_data = user_content
        media_type = "image/png"
        # Parse data URI if present
        if image_data.startswith("data:"):
            import re
            match = re.match(r"^data:(image/\w+);base64,(.+)$", image_data)
            if match:
                media_type = match.group(1)
                image_data = match.group(2)
        return [{
            "role": "user",
            "content": [
                {
                    "type": "image",
                    "source": {
                        "type": "base64",
                        "media_type": media_type,
                        "data": image_data,
                    },
                }
            ],
        }]

    text = user_content if isinstance(user_content, str) else json.dumps(user_content, default=str)
    return [{"role": "user", "content": text}]


async def _call_anthropic(
    model: str,
    system_prompt: str,
    user_content: str | dict[str, Any],
    temperature: float,
    schema_name: str | None,
    input_type: str,
    image_detail: str,
) -> tuple[dict[str, Any], StepMetrics]:
    client = _get_anthropic_client()

    system = _build_anthropic_system_prompt(system_prompt, schema_name)
    messages = _build_anthropic_messages(user_content, input_type, image_detail)

    start = time.monotonic()
    try:
        response = await client.messages.create(
            model=model,
            max_tokens=4096,
            system=system,
            messages=messages,
            temperature=temperature,
        )
    except Exception as exc:
        ei = serialize_error(exc)
        log.error("Anthropic call failed", error=ei["message"], error_name=ei["name"])
        raise

    elapsed_ms = (time.monotonic() - start) * 1000

    # Extract text from content blocks
    raw_text = ""
    for block in response.content:
        if block.type == "text":
            raw_text = block.text
            break

    if not raw_text:
        raw_text = "{}"

    # Strip markdown code fences if present (Claude often wraps JSON in ```json ... ```)
    stripped = raw_text.strip()
    if stripped.startswith("```"):
        lines = stripped.split("\n")
        # Remove first line (```json or ```) and last line (```)
        lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        stripped = "\n".join(lines).strip()

    try:
        output = json.loads(stripped)
    except json.JSONDecodeError:
        log.warn("Anthropic returned non-JSON content, wrapping as text", raw=raw_text[:200])
        output = {"raw": raw_text}

    usage = response.usage
    metrics = StepMetrics(
        duration_ms=round(elapsed_ms, 2),
        prompt_tokens=usage.input_tokens if usage else 0,
        completion_tokens=usage.output_tokens if usage else 0,
        total_tokens=(usage.input_tokens + usage.output_tokens) if usage else 0,
    )

    log.info(
        "Anthropic call completed",
        duration_ms=metrics.duration_ms,
        total_tokens=metrics.total_tokens,
    )

    return output, metrics
