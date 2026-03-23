"""Agent execution — deterministic handlers and LLM agents.

Deterministic handlers are registered at module level via
``register_handler(name, fn)``.  LLM agents build a user message from the
template defined in their agent YAML then call the LLM.
"""

from __future__ import annotations

import asyncio
import inspect
from typing import Any, Awaitable, Callable

from .llm import call_llm
from .loader import load_agent
from .logger import create_logger, serialize_error
from .resolver import resolve_template
from .types import AgentDefinition, AgentResult, StepMetrics

log = create_logger("runner")

# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HandlerFn = Callable[..., Any]
_handler_registry: dict[str, HandlerFn] = {}


def register_handler(name: str, fn: HandlerFn) -> None:
    """Register a deterministic handler function by name.

    The function may be sync or async.  It will receive a single ``input``
    dict and must return a dict.
    """
    _handler_registry[name] = fn
    log.debug("Registered handler", name=name)


# ---------------------------------------------------------------------------
# Agent execution
# ---------------------------------------------------------------------------

async def execute_agent(
    input_data: dict[str, Any],
    agent_def: AgentDefinition,
    config_overrides: dict[str, Any] | None = None,
) -> AgentResult:
    """Execute a single agent given its definition and resolved input.

    For *deterministic* agents the registered handler is called directly.
    For *llm* agents the user message template is rendered and the LLM is
    invoked.

    Parameters
    ----------
    input_data:
        Resolved input dict for the agent.
    agent_def:
        The agent definition (from loader).
    config_overrides:
        Optional dict with keys like ``model`` or ``temperature`` that
        override the agent definition values for this execution.
    """
    overrides = config_overrides or {}
    model = overrides.get("model", agent_def.model)
    temperature = float(overrides.get("temperature", agent_def.temperature))

    if agent_def.type == "deterministic":
        return await _run_deterministic(agent_def.handler, input_data)
    elif agent_def.type == "llm":
        return await _run_llm(
            agent_def=agent_def,
            input_data=input_data,
            model=model,
            temperature=temperature,
        )
    else:
        raise ValueError(f"Unknown agent type: {agent_def.type}")


async def _run_deterministic(
    handler_name: str, input_data: dict[str, Any]
) -> AgentResult:
    """Invoke a registered deterministic handler."""
    if handler_name not in _handler_registry:
        raise KeyError(
            f"No handler registered for '{handler_name}'. "
            f"Call register_handler('{handler_name}', fn) before invoking."
        )

    handler = _handler_registry[handler_name]
    log.info("Running deterministic handler", handler=handler_name)

    import time

    start = time.monotonic()
    try:
        result = handler(input_data)
        # Support async handlers transparently.
        if inspect.isawaitable(result):
            result = await result
    except Exception as exc:
        log.error("Handler failed", handler=handler_name, **serialize_error(exc))
        raise

    elapsed_ms = (time.monotonic() - start) * 1000
    metrics = StepMetrics(duration_ms=round(elapsed_ms, 2))

    return AgentResult(output=result, metrics=metrics)


async def _run_llm(
    agent_def: AgentDefinition,
    input_data: dict[str, Any],
    model: str,
    temperature: float,
) -> AgentResult:
    """Build the user message from the template and call the LLM."""
    user_message = resolve_template(agent_def.user_message, input_data)

    output, metrics = await call_llm(
        model=model,
        system_prompt=agent_def.system_prompt,
        user_content=user_message,
        temperature=temperature,
        schema_name=agent_def.schema,
        input_type=agent_def.input_type,
        image_detail=agent_def.image_detail,
    )

    return AgentResult(output=output, metrics=metrics)


# ---------------------------------------------------------------------------
# Convenience: invoke by agent ID
# ---------------------------------------------------------------------------

async def invoke_agent(
    agent_id: str,
    input_data: dict[str, Any],
    config_overrides: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Load an agent by ID, execute it, and return the result as a dict.

    This is the primary public entry point for one-shot agent invocations.
    """
    agent_def = load_agent(agent_id)
    result = await execute_agent(input_data, agent_def, config_overrides)
    return {
        "output": result.output,
        "metrics": {
            "duration_ms": result.metrics.duration_ms,
            "prompt_tokens": result.metrics.prompt_tokens,
            "completion_tokens": result.metrics.completion_tokens,
            "total_tokens": result.metrics.total_tokens,
        },
    }
