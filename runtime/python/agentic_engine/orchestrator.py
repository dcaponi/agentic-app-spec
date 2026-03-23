"""Workflow orchestration engine.

``orchestrate(workflow_name, input)`` is the main entry point.  It loads the
workflow definition, walks through serial and parallel steps (respecting
retry, fallback, and short-circuit directives), and returns a
``WorkflowEnvelope`` as a plain dict.
"""

from __future__ import annotations

import asyncio
import time
import uuid
from datetime import datetime, timezone
from typing import Any

from .loader import load_agent, load_workflow
from .logger import create_logger, serialize_error
from .resolver import resolve_inputs, resolve_outputs
from .runner import execute_agent
from .types import (
    ExecutionContext,
    ParallelGroup,
    StepMetrics,
    StepResult,
    WorkflowStep,
)

log = create_logger("orchestrator")


# ---------------------------------------------------------------------------
# Short-circuit evaluation
# ---------------------------------------------------------------------------

def _evaluate_condition(condition: str, output: Any) -> bool:
    """Evaluate a short-circuit condition string.

    The condition is a simple expression with ``output`` in scope (mirrors
    the JS convention from the spec, translated for Python).  Common patterns:

      - ``not output.get('is_food')``   (Pythonic)
      - ``not output['found']``         (Pythonic)
      - ``!output.found``               (JS-style — we translate)

    To support JS-style conditions coming straight from the YAML (e.g.
    ``!output.is_food``), we do a lightweight translation before ``eval``.
    """
    # Translate JS-isms to Python:
    py_condition = condition
    # Replace leading/inline "!" with "not " (but not "!=")
    py_condition = _js_not_to_python(py_condition)
    # Replace "." member access on output with dict-style access.
    # e.g. output.found -> output["found"]
    py_condition = _dot_to_bracket(py_condition)

    try:
        return bool(eval(py_condition, {"__builtins__": {}}, {"output": output}))  # noqa: S307
    except Exception as exc:
        log.warn(
            "Short-circuit condition evaluation failed, treating as False",
            condition=condition,
            translated=py_condition,
            error=str(exc),
        )
        return False


def _js_not_to_python(expr: str) -> str:
    """Replace JS ``!`` (not !=) with Python ``not ``."""
    import re

    # Replace "!" that is NOT followed by "=" (i.e. not "!=").
    return re.sub(r"!(?!=)", "not ", expr)


def _dot_to_bracket(expr: str) -> str:
    """Replace ``output.key`` with ``output["key"]`` for safe eval."""
    import re

    # Match output.someKey (but not output["..."]).
    def _replacer(m: re.Match[str]) -> str:
        key = m.group(1)
        return f'output["{key}"]'

    return re.sub(r'\boutput\.(\w+)', _replacer, expr)


# ---------------------------------------------------------------------------
# Single step execution (with retry + fallback)
# ---------------------------------------------------------------------------

async def _execute_step(
    step: WorkflowStep,
    context: ExecutionContext,
) -> StepResult:
    """Execute a single workflow step, honouring retry and fallback config."""
    agent_def = load_agent(step.agent)
    resolved_input = resolve_inputs(step.input, context)
    config_overrides = step.config or {}

    max_attempts = step.retry.max_attempts if step.retry else 1
    backoff_ms = step.retry.backoff_ms if step.retry else 0
    retries_used = 0
    last_error: BaseException | None = None

    # -- Retry loop -----------------------------------------------------------
    for attempt in range(1, max_attempts + 1):
        try:
            result = await execute_agent(resolved_input, agent_def, config_overrides)
            return StepResult(
                step_id=step.id,
                agent=step.agent,
                output=result.output,
                metrics=result.metrics,
                retries=retries_used,
                used_fallback=False,
            )
        except Exception as exc:
            last_error = exc
            retries_used = attempt
            log.warn(
                "Step attempt failed",
                step_id=step.id,
                attempt=attempt,
                max_attempts=max_attempts,
                error=str(exc),
            )
            if attempt < max_attempts:
                delay = backoff_ms * attempt / 1000.0
                await asyncio.sleep(delay)

    # -- Fallback -------------------------------------------------------------
    if step.fallback:
        log.info("Using fallback agent", step_id=step.id, fallback_agent=step.fallback.agent)
        try:
            fallback_def = load_agent(step.fallback.agent)
            fb_overrides = {**config_overrides, **step.fallback.config}
            result = await execute_agent(resolved_input, fallback_def, fb_overrides)
            return StepResult(
                step_id=step.id,
                agent=step.fallback.agent,
                output=result.output,
                metrics=result.metrics,
                retries=retries_used,
                used_fallback=True,
            )
        except Exception as fb_exc:
            log.error(
                "Fallback agent also failed",
                step_id=step.id,
                **serialize_error(fb_exc),
            )
            last_error = fb_exc

    # -- All attempts exhausted -----------------------------------------------
    error_msg = str(last_error) if last_error else "Unknown error"
    return StepResult(
        step_id=step.id,
        agent=step.agent,
        output=None,
        metrics=StepMetrics(),
        retries=retries_used,
        used_fallback=step.fallback is not None,
        error=error_msg,
    )


# ---------------------------------------------------------------------------
# Orchestrate
# ---------------------------------------------------------------------------

async def orchestrate(
    workflow_name: str,
    input_data: dict[str, Any],
) -> dict[str, Any]:
    """Execute a complete workflow and return a ``WorkflowEnvelope`` dict.

    Parameters
    ----------
    workflow_name:
        Name of the workflow (matches the YAML filename without extension).
    input_data:
        Workflow-level input dict.
    """
    request_id = str(uuid.uuid4())
    started_at = datetime.now(timezone.utc).isoformat()
    overall_start = time.monotonic()

    log.info("Starting workflow", workflow=workflow_name, request_id=request_id)

    wf = load_workflow(workflow_name)
    context = ExecutionContext(input=input_data, steps={})
    step_results: list[dict[str, Any]] = []
    short_circuited = False
    short_circuit_defaults: dict[str, Any] = {}

    try:
        for entry in wf.steps:
            # ---- Parallel group ---------------------------------------------
            if isinstance(entry, ParallelGroup):
                # Check if any parallel step should be skipped due to a prior
                # short-circuit.
                if short_circuited:
                    for ps in entry.parallel:
                        default_output = short_circuit_defaults.get(ps.id, {})
                        context.steps[ps.id] = {"output": default_output}
                        step_results.append(_default_step_result(ps.id, ps.agent, default_output))
                    continue

                tasks = [_execute_step(ps, context) for ps in entry.parallel]
                results = await asyncio.gather(*tasks, return_exceptions=True)

                for i, res in enumerate(results):
                    ps = entry.parallel[i]
                    if isinstance(res, BaseException):
                        sr = StepResult(
                            step_id=ps.id,
                            agent=ps.agent,
                            output=None,
                            error=str(res),
                        )
                    else:
                        sr = res

                    context.steps[sr.step_id] = {"output": sr.output}
                    step_results.append(_step_result_to_dict(sr))

                    # Evaluate short-circuit for each parallel step
                    if not short_circuited and ps.short_circuit and sr.output is not None:
                        if _evaluate_condition(ps.short_circuit.condition, sr.output):
                            log.info("Short-circuit triggered", step_id=ps.id)
                            short_circuited = True
                            short_circuit_defaults = ps.short_circuit.defaults

            # ---- Serial step ------------------------------------------------
            elif isinstance(entry, WorkflowStep):
                if short_circuited:
                    default_output = short_circuit_defaults.get(entry.id, {})
                    context.steps[entry.id] = {"output": default_output}
                    step_results.append(_default_step_result(entry.id, entry.agent, default_output))
                    continue

                sr = await _execute_step(entry, context)
                context.steps[sr.step_id] = {"output": sr.output}
                step_results.append(_step_result_to_dict(sr))

                if sr.error:
                    raise RuntimeError(
                        f"Step '{sr.step_id}' failed after retries: {sr.error}"
                    )

                # Evaluate short-circuit
                if entry.short_circuit and sr.output is not None:
                    if _evaluate_condition(entry.short_circuit.condition, sr.output):
                        log.info("Short-circuit triggered", step_id=entry.id)
                        short_circuited = True
                        short_circuit_defaults = entry.short_circuit.defaults

        # ---- Resolve outputs ------------------------------------------------
        output = resolve_outputs(wf.output, context)
        status = "success"
        error_msg = None

    except Exception as exc:
        log.error("Workflow failed", workflow=workflow_name, **serialize_error(exc))
        output = {}
        status = "error"
        error_msg = str(exc)

    completed_at = datetime.now(timezone.utc).isoformat()
    total_ms = round((time.monotonic() - overall_start) * 1000, 2)

    # Aggregate token metrics
    total_prompt = sum(s.get("metrics", {}).get("prompt_tokens", 0) for s in step_results)
    total_completion = sum(s.get("metrics", {}).get("completion_tokens", 0) for s in step_results)
    total_tokens = sum(s.get("metrics", {}).get("total_tokens", 0) for s in step_results)

    envelope: dict[str, Any] = {
        "request_id": request_id,
        "workflow": wf.name,
        "version": wf.version,
        "status": status,
        "output": output,
        "steps": step_results,
        "metrics": {
            "total_duration_ms": total_ms,
            "total_prompt_tokens": total_prompt,
            "total_completion_tokens": total_completion,
            "total_tokens": total_tokens,
        },
        "started_at": started_at,
        "completed_at": completed_at,
    }
    if error_msg:
        envelope["error"] = error_msg

    log.info(
        "Workflow completed",
        workflow=workflow_name,
        status=status,
        duration_ms=total_ms,
    )
    return envelope


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _step_result_to_dict(sr: StepResult) -> dict[str, Any]:
    """Convert a StepResult dataclass to a plain dict."""
    d: dict[str, Any] = {
        "step_id": sr.step_id,
        "agent": sr.agent,
        "output": sr.output,
        "metrics": {
            "duration_ms": sr.metrics.duration_ms,
            "prompt_tokens": sr.metrics.prompt_tokens,
            "completion_tokens": sr.metrics.completion_tokens,
            "total_tokens": sr.metrics.total_tokens,
        },
        "retries": sr.retries,
        "used_fallback": sr.used_fallback,
    }
    if sr.error:
        d["error"] = sr.error
    return d


def _default_step_result(step_id: str, agent: str, default_output: Any) -> dict[str, Any]:
    """Build a step result dict for a short-circuited (skipped) step."""
    return {
        "step_id": step_id,
        "agent": agent,
        "output": default_output,
        "metrics": {
            "duration_ms": 0,
            "prompt_tokens": 0,
            "completion_tokens": 0,
            "total_tokens": 0,
        },
        "retries": 0,
        "used_fallback": False,
        "skipped": True,
    }
