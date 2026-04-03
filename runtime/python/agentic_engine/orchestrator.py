"""Workflow orchestration engine.

``orchestrate(workflow_name, input)`` is the main entry point.  It loads the
workflow definition, walks through serial and parallel steps (respecting
retry, fallback, and short-circuit directives), and returns a
``WorkflowEnvelope`` as a plain dict.
"""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from datetime import datetime, timezone
from typing import Any

from .llm import call_llm
from .loader import load_agent, load_router, load_workflow
from .logger import create_logger, serialize_error
from .resolver import resolve_inputs, resolve_outputs
from .runner import execute_agent
from .types import (
    AgentDefinition,
    ExecutionContext,
    ParallelGroup,
    RetryConfig,
    RouteBlock,
    RouteEntry,
    RouteOutput,
    RouterDefinition,
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
            fb_info = serialize_error(fb_exc)
            log.error(
                "Fallback agent also failed",
                step_id=step.id,
                error=fb_info["message"],
                error_name=fb_info["name"],
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

            # ---- Route entry ------------------------------------------------
            elif isinstance(entry, RouteEntry):
                if short_circuited:
                    _fill_route_skipped(entry.route, short_circuit_defaults, context, step_results)
                    continue

                route_result = await _execute_route(entry.route, context)
                context.steps[entry.route.id] = {"output": route_result.output}
                step_results.append(_step_result_to_dict(route_result))

                if route_result.error == "short_circuited":
                    short_circuited = True
                    none_target = entry.route.routes.get("_none", {})
                    sc_defaults = none_target.get("defaults", {}) if isinstance(none_target, dict) else {}
                    short_circuit_defaults = sc_defaults
                    log.info("Short-circuit triggered by route", step_id=entry.route.id)

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
        err_info = serialize_error(exc)
        log.error("Workflow failed", workflow=workflow_name, error=err_info["message"], error_name=err_info["name"])
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
# Route execution helpers
# ---------------------------------------------------------------------------

def _fill_route_skipped(
    route_block: RouteBlock,
    defaults: dict[str, Any],
    context: ExecutionContext,
    step_results: list[dict[str, Any]],
) -> None:
    """Build a skipped step result for a route block and update context."""
    default_output = defaults.get(route_block.id) if defaults else None
    log.info("Route skipped (short-circuited)", step_id=route_block.id, has_default=default_output is not None)
    context.steps[route_block.id] = {"output": default_output}
    sr = StepResult(
        step_id=route_block.id,
        agent=f"router:{route_block.router}",
        output=default_output,
        metrics=StepMetrics(),
        retries=0,
        used_fallback=False,
    )
    d = _step_result_to_dict(sr)
    d["skipped"] = True
    step_results.append(d)


async def _execute_route(
    route_block: RouteBlock,
    context: ExecutionContext,
) -> StepResult:
    """Execute a route block — 3-phase: decision (with retry/fallback), _none check, dispatch.

    Retry and fallback cover ONLY the router decision phase.  Dispatch happens
    outside the retry loop so that the target handles its own errors.

    The returned ``StepResult.error`` field is set to ``"short_circuited"``
    when the router selected ``_none`` or a ``short_circuit: true`` target,
    signalling the orchestrator to halt further steps.
    """
    max_attempts = route_block.retry.max_attempts if route_block.retry else 1
    backoff_ms = route_block.retry.backoff_ms if route_block.retry else 0

    log.info(
        "Route starting",
        step_id=route_block.id,
        router=route_block.router,
        route_keys=list(route_block.routes.keys()),
        max_attempts=max_attempts,
        has_fallback=route_block.fallback is not None,
    )

    router_def = load_router(route_block.router)
    resolved_input = resolve_inputs(route_block.input, context)
    route_keys = [k for k in route_block.routes.keys() if k != "_none"]

    # ── Phase 1: Router decision (retry + fallback cover this phase only) ──

    chosen_key: str | None = None
    router_output: dict[str, Any] = {}
    used_fallback = False
    total_attempts = 0
    decided = False
    last_error: BaseException | None = None

    for attempt in range(1, max_attempts + 1):
        total_attempts = attempt
        log.info("Route decision attempt", step_id=route_block.id, attempt=attempt, max_attempts=max_attempts)
        try:
            output = await _execute_router_decision(router_def, resolved_input, route_keys)
            key = output.get("route") if isinstance(output, dict) else None

            if not isinstance(key, str):
                raise ValueError(
                    f"Router '{route_block.router}' did not return a string 'route' key; got: {output!r}"
                )

            log.info("Route decision made", step_id=route_block.id, chosen_key=key, attempt=attempt)

            if key != "_none" and key not in route_block.routes:
                raise ValueError(
                    f"Router '{route_block.router}' returned invalid route key '{key}'. "
                    f"Valid keys: {[*route_keys, '_none']}"
                )

            chosen_key = key
            router_output = output if isinstance(output, dict) else {"route": key}
            decided = True
            break
        except Exception as exc:
            last_error = exc
            log.warn(
                "Route decision attempt failed",
                step_id=route_block.id,
                attempt=attempt,
                max_attempts=max_attempts,
                error=str(exc),
            )
            if attempt < max_attempts:
                delay = backoff_ms * attempt / 1000.0
                await asyncio.sleep(delay)

    # Fallback decision (if primary failed)
    if not decided and route_block.fallback:
        fallback_router_id = route_block.fallback.get("router", "")
        fallback_config = route_block.fallback.get("config", {})
        log.info(
            "Route trying fallback router for decision",
            step_id=route_block.id,
            fallback_router=fallback_router_id,
        )
        try:
            fallback_def = load_router(fallback_router_id)
            # Merge fallback config overrides onto the router definition
            merged_fallback = RouterDefinition(
                name=fallback_def.name,
                description=fallback_def.description,
                strategy=fallback_config.get("strategy", fallback_def.strategy),
                provider=fallback_config.get("provider", fallback_def.provider),
                model=fallback_config.get("model", fallback_def.model),
                temperature=float(fallback_config.get("temperature", fallback_def.temperature)),
                handler=fallback_config.get("handler", fallback_def.handler),
                prompt=fallback_config.get("prompt", fallback_def.prompt),
                input=fallback_def.input,
            )
            output = await _execute_router_decision(merged_fallback, resolved_input, route_keys)
            key = output.get("route") if isinstance(output, dict) else None

            if not isinstance(key, str):
                raise ValueError(
                    f"Fallback router '{fallback_router_id}' did not return a string 'route' key; got: {output!r}"
                )

            if key != "_none" and key not in route_block.routes:
                raise ValueError(f"Fallback router returned invalid key '{key}'")

            chosen_key = key
            router_output = output if isinstance(output, dict) else {"route": key}
            used_fallback = True
            total_attempts = max_attempts + 1
            decided = True
            log.info("Route fallback decision made", step_id=route_block.id, chosen_key=key)
        except Exception as exc:
            last_error = exc
            total_attempts = max_attempts + 1
            log.error(
                "Route fallback decision also failed",
                step_id=route_block.id,
                error=str(exc),
            )

    if not decided:
        error_msg = str(last_error) if last_error else "All decision attempts exhausted"
        log.error(
            "Route all decision attempts exhausted",
            step_id=route_block.id,
            total_attempts=total_attempts,
            error=error_msg,
        )
        resolved_agent = f"router:{route_block.router}"
        return StepResult(
            step_id=route_block.id,
            agent=resolved_agent,
            output=None,
            metrics=StepMetrics(),
            retries=total_attempts - 1,
            used_fallback=route_block.fallback is not None,
            error=error_msg,
        )

    resolved_agent = (
        f"router:{route_block.fallback['router']}" if used_fallback and route_block.fallback
        else f"router:{route_block.router}"
    )
    target = route_block.routes.get(chosen_key)  # type: ignore[arg-type]

    # ── Phase 2: Handle _none / short_circuit ──

    is_none = chosen_key == "_none"
    is_short_circuit = (
        not is_none
        and isinstance(target, dict)
        and target.get("short_circuit") is True
    )

    if is_none or is_short_circuit:
        route_out = RouteOutput(
            route="_none",
            router_output=router_output,
            result=None,
        )
        return StepResult(
            step_id=route_block.id,
            agent=resolved_agent,
            output={"route": route_out.route, "router_output": route_out.router_output, "result": route_out.result},
            metrics=StepMetrics(),
            retries=total_attempts - 1,
            used_fallback=used_fallback,
            error="short_circuited",
        )

    # ── Phase 3: Dispatch target (NO retry — target handles its own errors) ──

    dispatch_result = await _dispatch_route_target(target, resolved_input, route_block, context)

    route_out = RouteOutput(
        route=chosen_key,  # type: ignore[arg-type]
        router_output=router_output,
        result=dispatch_result["output"],
    )
    dispatch_metrics_raw = dispatch_result["metrics"]
    dispatch_metrics = StepMetrics(
        duration_ms=dispatch_metrics_raw.get("duration_ms", 0.0),
        prompt_tokens=dispatch_metrics_raw.get("prompt_tokens", 0),
        completion_tokens=dispatch_metrics_raw.get("completion_tokens", 0),
        total_tokens=dispatch_metrics_raw.get("total_tokens", 0),
    )

    return StepResult(
        step_id=route_block.id,
        agent=resolved_agent,
        output={"route": route_out.route, "router_output": route_out.router_output, "result": route_out.result},
        metrics=dispatch_metrics,
        retries=total_attempts - 1,
        used_fallback=used_fallback,
    )


async def _execute_router_decision(
    router_def: RouterDefinition,
    resolved_input: dict[str, Any],
    route_keys: list[str],
) -> dict[str, Any]:
    """Call the router and return its raw output dict (must contain ``route`` key)."""
    if router_def.strategy == "deterministic":
        # Build an AgentDefinition-compatible object for execute_agent
        agent_compat = AgentDefinition(
            name=router_def.name,
            description=router_def.description,
            type="deterministic",
            handler=router_def.handler,
        )
        result = await execute_agent(resolved_input, agent_compat)
        output = result.output
        return output if isinstance(output, dict) else {"route": output}

    # LLM strategy
    input_summary = "\n".join(
        f"{k}: {v if isinstance(v, str) else json.dumps(v, default=str)}"
        for k, v in resolved_input.items()
    )
    user_message = (
        f"{input_summary}\n\n"
        f"You must choose exactly one of the following routes: {', '.join(route_keys)}\n"
        f"If none of the routes apply, choose: _none\n"
        f"Respond with a JSON object: {{\"route\": \"<chosen_key>\"}}"
    )

    output, _metrics = await call_llm(
        model=router_def.model or "gpt-4.1-mini",
        system_prompt=router_def.prompt or "",
        user_content=user_message,
        temperature=router_def.temperature,
        schema_name=None,
        provider=router_def.provider,
    )
    return output if isinstance(output, dict) else {"route": output}


async def _dispatch_route_target(
    target: Any,
    pass_through: dict[str, Any],
    route_block: RouteBlock,
    context: ExecutionContext,
) -> dict[str, Any]:
    """Dispatch to the route target and return ``{"output": ..., "metrics": {...}}``.

    Supported target shapes:
    - str                     → agent ID with pass-through input
    - dict with "route"       → nested RouteBlock (recursive)
    - dict with "agent"       → agent with explicit or pass-through input
    - dict with "workflow"    → sub-workflow, use envelope result
    """
    zero_metrics = {
        "duration_ms": 0,
        "prompt_tokens": 0,
        "completion_tokens": 0,
        "total_tokens": 0,
    }

    # String target — agent ID with pass-through input
    if isinstance(target, str):
        agent_def = load_agent(target)
        result = await execute_agent(pass_through, agent_def)
        return {
            "output": result.output,
            "metrics": {
                "duration_ms": result.metrics.duration_ms,
                "prompt_tokens": result.metrics.prompt_tokens,
                "completion_tokens": result.metrics.completion_tokens,
                "total_tokens": result.metrics.total_tokens,
            },
        }

    if not isinstance(target, dict):
        raise ValueError(
            f"Unknown route target type in route '{route_block.id}': {target!r}"
        )

    # Nested route block
    if "route" in target:
        nested_raw = target["route"]
        # Parse nested route block from dict
        nested_retry = None
        if "retry" in nested_raw:
            r = nested_raw["retry"]
            nested_retry = RetryConfig(
                max_attempts=int(r.get("max_attempts", 1)),
                backoff_ms=int(r.get("backoff_ms", 0)),
            )
        nested_block = RouteBlock(
            id=nested_raw.get("id", ""),
            router=nested_raw.get("router", ""),
            input=nested_raw.get("input", {}),
            routes=nested_raw.get("routes", {}),
            retry=nested_retry,
            fallback=nested_raw.get("fallback"),
        )
        nested_result = await _execute_route(nested_block, context)
        nested_metrics = nested_result.metrics
        return {
            "output": nested_result.output,
            "metrics": {
                "duration_ms": nested_metrics.duration_ms,
                "prompt_tokens": nested_metrics.prompt_tokens,
                "completion_tokens": nested_metrics.completion_tokens,
                "total_tokens": nested_metrics.total_tokens,
            },
        }

    # Agent with explicit input mapping
    if "agent" in target:
        agent_id = target["agent"]
        agent_def = load_agent(agent_id)
        agent_input = (
            resolve_inputs(target["input"], context)
            if target.get("input")
            else pass_through
        )
        result = await execute_agent(agent_input, agent_def)
        return {
            "output": result.output,
            "metrics": {
                "duration_ms": result.metrics.duration_ms,
                "prompt_tokens": result.metrics.prompt_tokens,
                "completion_tokens": result.metrics.completion_tokens,
                "total_tokens": result.metrics.total_tokens,
            },
        }

    # Workflow target
    if "workflow" in target:
        wf_name = target["workflow"]
        wf_input = (
            resolve_inputs(target["input"], context)
            if target.get("input")
            else pass_through
        )
        envelope = await orchestrate(wf_name, wf_input)
        return {
            "output": envelope.get("output"),
            "metrics": {
                "duration_ms": envelope.get("metrics", {}).get("total_duration_ms", 0),
                "prompt_tokens": envelope.get("metrics", {}).get("total_prompt_tokens", 0),
                "completion_tokens": envelope.get("metrics", {}).get("total_completion_tokens", 0),
                "total_tokens": envelope.get("metrics", {}).get("total_tokens", 0),
            },
        }

    raise ValueError(
        f"Unknown route target shape in route '{route_block.id}': {target!r}"
    )


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
