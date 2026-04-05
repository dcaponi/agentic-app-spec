"""Workflow orchestration engine.

``orchestrate(workflow_name, input)`` is the main entry point.  It loads the
workflow definition, traverses the step graph following ``next:`` edges
(with support for switch/if branching, parallel, loop, and for_each), and
returns a ``WorkflowEnvelope`` as a plain dict.
"""

from __future__ import annotations

import asyncio
import re
import time
import uuid
from datetime import datetime, timezone
from typing import Any

from .loader import load_agent, load_workflow
from .logger import create_logger, serialize_error
from .resolver import resolve_inputs, resolve_outputs, resolve_ref
from .runner import execute_agent
from .types import (
    ExecutionContext,
    FallbackConfig,
    ForEachBlock,
    LoopBlock,
    NextField,
    ParallelBlock,
    ParallelBranch,
    StepMetrics,
    StepResult,
    WorkflowError,
    WorkflowStep,
)

log = create_logger("orchestrator")


# ---------------------------------------------------------------------------
# Orchestrate
# ---------------------------------------------------------------------------

async def orchestrate(
    workflow_name: str,
    input_data: dict[str, Any],
) -> dict[str, Any]:
    """Execute a complete workflow and return a ``WorkflowEnvelope`` dict."""
    request_id = str(uuid.uuid4())
    started_at = datetime.now(timezone.utc).isoformat()
    overall_start = time.monotonic()

    log.info("Starting workflow", workflow=workflow_name, request_id=request_id)

    wf = load_workflow(workflow_name)
    context = ExecutionContext(input=input_data, steps={})
    step_results: list[dict[str, Any]] = []
    trail: list[dict[str, Any]] = []
    status = "success"
    error_msg: str | None = None

    # Build step index for graph traversal
    step_index: dict[str, int] = {}
    for i, entry in enumerate(wf.steps):
        sid = _get_step_id(entry)
        if sid:
            step_index[sid] = i
        # Also index parallel branch IDs
        if isinstance(entry, ParallelBlock):
            for branch in entry.branches:
                if branch.id:
                    step_index[branch.id] = i

    executed_steps: set[str] = set()

    try:
        cursor = 0
        while cursor < len(wf.steps):
            entry = wf.steps[cursor]
            sid = _get_step_id(entry)
            executed_steps.add(sid)

            next_target: str = ""

            if isinstance(entry, WorkflowStep):
                next_target = await _execute_workflow_step(entry, context, step_results, trail)
            elif isinstance(entry, ParallelBlock):
                next_target = await _execute_parallel_block(entry, context, step_results, trail)
            elif isinstance(entry, LoopBlock):
                next_target = await _execute_loop_block(entry, context, step_results, trail)
            elif isinstance(entry, ForEachBlock):
                next_target = await _execute_for_each_block(entry, context, step_results, trail)

            # Resolve next step
            if next_target == "_end":
                break
            if next_target:
                if next_target not in step_index:
                    raise RuntimeError(f"next target '{next_target}' not found")
                cursor = step_index[next_target]
            else:
                cursor += 1

    except Exception as exc:
        err_info = serialize_error(exc)
        log.error("Workflow failed", workflow=workflow_name, error=err_info["message"])
        status = "error"
        error_msg = str(exc)

    # Mark non-executed steps
    for entry in wf.steps:
        sid = _get_step_id(entry)
        if sid not in executed_steps:
            _mark_not_executed(entry, step_results)

    # Resolve outputs
    output: dict[str, Any] = {}
    try:
        output = resolve_outputs(wf.output, context)
    except Exception:
        pass

    completed_at = datetime.now(timezone.utc).isoformat()
    total_ms = round((time.monotonic() - overall_start) * 1000, 2)

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
        "trail": trail,
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

    log.info("Workflow completed", workflow=workflow_name, status=status, duration_ms=total_ms)

    if status == "error":
        raise WorkflowError(error_msg or "unknown error", envelope=envelope)

    return envelope


# ---------------------------------------------------------------------------
# Step execution
# ---------------------------------------------------------------------------

async def _execute_workflow_step(
    step: WorkflowStep,
    ctx: ExecutionContext,
    results: list[dict[str, Any]],
    trail: list[dict[str, Any]],
) -> str:
    """Execute a single agent or sub-workflow step. Returns the next target."""
    _emit_trail(trail, step.id, "step_start")

    if step.workflow:
        sr = await _execute_sub_workflow(step, ctx)
    else:
        sr = await _execute_agent_step(step.id, step.agent, step.input, step.config, step.retry, step.fallback, ctx)

    results.append(_step_result_to_dict(sr))

    if sr.status == "success":
        ctx.steps[step.id] = {"output": sr.output}
        _emit_trail(trail, step.id, "step_success")
    else:
        _emit_trail(trail, step.id, "step_error", {"error": sr.error})
        raise RuntimeError(f"Step '{step.id}' failed: {sr.error}")

    return _resolve_next_target(step.next, sr.output)


async def _execute_sub_workflow(step: WorkflowStep, ctx: ExecutionContext) -> StepResult:
    """Invoke a sub-workflow via orchestrate."""
    resolved_input = resolve_inputs(step.input, ctx)
    start = time.monotonic()

    try:
        sub_envelope = await orchestrate(step.workflow, resolved_input)
        elapsed_ms = (time.monotonic() - start) * 1000
        return StepResult(
            step_id=step.id,
            workflow=step.workflow,
            status="success",
            output=sub_envelope.get("output"),
            sub_envelope=sub_envelope,
            metrics=StepMetrics(
                duration_ms=round(elapsed_ms, 2),
                prompt_tokens=sub_envelope.get("metrics", {}).get("total_prompt_tokens", 0),
                completion_tokens=sub_envelope.get("metrics", {}).get("total_completion_tokens", 0),
                total_tokens=sub_envelope.get("metrics", {}).get("total_tokens", 0),
            ),
        )
    except WorkflowError as exc:
        elapsed_ms = (time.monotonic() - start) * 1000
        return StepResult(
            step_id=step.id,
            workflow=step.workflow,
            status="error",
            sub_envelope=exc.envelope,
            metrics=StepMetrics(duration_ms=round(elapsed_ms, 2)),
            error=str(exc),
        )


async def _execute_agent_step(
    step_id: str,
    agent_id: str,
    input_bindings: dict[str, Any],
    config: dict[str, Any] | None,
    retry: Any | None,
    fallback: Any | None,
    ctx: ExecutionContext,
) -> StepResult:
    """Execute an agent step with retry + fallback."""
    resolved_input = resolve_inputs(input_bindings, ctx)
    config_overrides = dict(config) if config else {}

    max_attempts = retry.max_attempts if retry else 1
    backoff_ms = retry.backoff_ms if retry else 0
    last_error: Exception | None = None

    for attempt in range(1, max_attempts + 1):
        try:
            result = await execute_agent(resolved_input, load_agent(agent_id), config_overrides)
            return StepResult(
                step_id=step_id,
                agent=agent_id,
                status="success",
                output=result.output,
                metrics=result.metrics,
                attempts=attempt,
            )
        except Exception as exc:
            last_error = exc
            log.warn("Step attempt failed", step_id=step_id, attempt=attempt, error=str(exc))
            if attempt < max_attempts:
                await asyncio.sleep(backoff_ms * attempt / 1000.0)

    # Fallback
    if fallback and fallback.agent:
        log.info("Using fallback agent", step_id=step_id, fallback_agent=fallback.agent)
        try:
            fb_overrides = {**config_overrides, **fallback.config}
            result = await execute_agent(resolved_input, load_agent(fallback.agent), fb_overrides)
            return StepResult(
                step_id=step_id,
                agent=fallback.agent,
                status="success",
                output=result.output,
                metrics=result.metrics,
                attempts=max_attempts,
                used_fallback=True,
                fallback_reason=str(last_error) if last_error else "",
            )
        except Exception as fb_exc:
            last_error = fb_exc

    return StepResult(
        step_id=step_id,
        agent=agent_id,
        status="error",
        metrics=StepMetrics(),
        attempts=max_attempts,
        error=str(last_error) if last_error else "Unknown error",
    )


# ---------------------------------------------------------------------------
# Parallel execution
# ---------------------------------------------------------------------------

async def _execute_parallel_block(
    pb: ParallelBlock,
    ctx: ExecutionContext,
    results: list[dict[str, Any]],
    trail: list[dict[str, Any]],
) -> str:
    _emit_trail(trail, pb.id, "parallel_start", {"join": pb.join, "branches": len(pb.branches)})

    async def _run_branch(branch: ParallelBranch) -> StepResult:
        if branch.workflow:
            ws = WorkflowStep(id=branch.id, workflow=branch.workflow, input=branch.input, config=branch.config)
            return await _execute_sub_workflow(ws, ctx)
        return await _execute_agent_step(branch.id, branch.agent, branch.input, branch.config, branch.retry, branch.fallback, ctx)

    tasks = [_run_branch(b) for b in pb.branches]
    branch_results = await asyncio.gather(*tasks, return_exceptions=True)

    has_error = False
    for i, res in enumerate(branch_results):
        branch = pb.branches[i]
        if isinstance(res, BaseException):
            sr = StepResult(step_id=branch.id, agent=branch.agent, status="error", error=str(res))
            has_error = True
        else:
            sr = res
            if sr.status != "success":
                has_error = True

        results.append(_step_result_to_dict(sr))
        if sr.status == "success":
            ctx.steps[sr.step_id] = {"output": sr.output}

    _emit_trail(trail, pb.id, "parallel_end")

    if pb.join == "all" and has_error:
        raise RuntimeError(f"Parallel block '{pb.id}': one or more branches failed (join=all)")

    if pb.join == "any":
        any_success = any(
            not isinstance(r, BaseException) and r.status == "success"
            for r in branch_results
        )
        if not any_success:
            raise RuntimeError(f"Parallel block '{pb.id}': all branches failed (join=any)")

    # all_settled: always continue

    return _resolve_next_target(pb.next, None)


# ---------------------------------------------------------------------------
# Loop execution
# ---------------------------------------------------------------------------

async def _execute_loop_block(
    lb: LoopBlock,
    ctx: ExecutionContext,
    results: list[dict[str, Any]],
    trail: list[dict[str, Any]],
) -> str:
    last_result: StepResult | None = None

    for iteration in range(1, lb.max_iterations + 1):
        _emit_trail(trail, lb.id, "loop_iteration", {"iteration": iteration})

        if lb.workflow:
            ws = WorkflowStep(id=lb.id, workflow=lb.workflow, input=lb.input, config=lb.config)
            sr = await _execute_sub_workflow(ws, ctx)
        else:
            sr = await _execute_agent_step(lb.id, lb.agent, lb.input, lb.config, lb.retry, lb.fallback, ctx)

        if sr.status == "success":
            ctx.steps[lb.id] = {"output": sr.output}
        else:
            results.append(_step_result_to_dict(sr))
            raise RuntimeError(f"Loop '{lb.id}' iteration {iteration} failed: {sr.error}")

        last_result = sr

        # Check exit condition
        if lb.until and _evaluate_condition(lb.until, sr.output):
            break

    if last_result:
        results.append(_step_result_to_dict(last_result))

    return _resolve_next_target(lb.next, None)


# ---------------------------------------------------------------------------
# ForEach execution
# ---------------------------------------------------------------------------

async def _execute_for_each_block(
    feb: ForEachBlock,
    ctx: ExecutionContext,
    results: list[dict[str, Any]],
    trail: list[dict[str, Any]],
) -> str:
    collection_raw = resolve_ref(feb.collection, ctx)
    if not isinstance(collection_raw, list):
        raise RuntimeError(f"for_each '{feb.id}': collection '{feb.collection}' did not resolve to a list")

    if not collection_raw:
        ctx.steps[feb.id] = {"output": []}
        results.append(_step_result_to_dict(StepResult(
            step_id=feb.id, agent=feb.agent, status="success", output=[],
        )))
        return _resolve_next_target(feb.next, None)

    concurrency = feb.max_concurrency if feb.max_concurrency > 0 else len(collection_raw)
    semaphore = asyncio.Semaphore(concurrency)
    iteration_results: list[Any] = [None] * len(collection_raw)
    iteration_errors: list[str | None] = [None] * len(collection_raw)

    async def _run_iteration(idx: int, item: Any) -> None:
        async with semaphore:
            _emit_trail(trail, feb.id, "for_each_iteration", {"index": idx})

            # Build per-iteration context with $.current
            iter_ctx = ExecutionContext(input=ctx.input, steps={**ctx.steps, "__current": {"output": item}})

            sr = await _execute_agent_step(
                f"{feb.id}[{idx}]", feb.agent, feb.input, feb.config,
                feb.retry, feb.fallback, iter_ctx,
            )
            if sr.status == "success":
                iteration_results[idx] = sr.output
            else:
                iteration_errors[idx] = sr.error or "unknown"

    tasks = [_run_iteration(i, item) for i, item in enumerate(collection_raw)]
    await asyncio.gather(*tasks)

    has_error = any(e is not None for e in iteration_errors)
    has_success = any(e is None for e in iteration_errors)

    if has_error and has_success:
        fe_status = "partial_failure"
    elif has_error:
        fe_status = "error"
    else:
        fe_status = "success"

    results.append(_step_result_to_dict(StepResult(
        step_id=feb.id, agent=feb.agent, status=fe_status, output=iteration_results,
    )))
    ctx.steps[feb.id] = {"output": iteration_results}

    if fe_status == "error":
        first_err = next(e for e in iteration_errors if e)
        raise RuntimeError(f"for_each '{feb.id}': {first_err}")

    return _resolve_next_target(feb.next, None)


# ---------------------------------------------------------------------------
# Next resolution
# ---------------------------------------------------------------------------

def _resolve_next_target(next_field: NextField | None, output: Any) -> str:
    """Evaluate a NextField and return the target step ID ('' for fall-through, '_end' to stop)."""
    if next_field is None:
        return ""

    if next_field.target:
        return next_field.target

    if next_field.switch:
        sn = next_field.switch
        output_map = output if isinstance(output, dict) else {}
        val = _resolve_field_path(sn.expression, output_map)
        val_str = str(val) if val is not None else ""
        return sn.cases.get(val_str, sn.default)

    if next_field.if_:
        ifn = next_field.if_
        if _evaluate_condition(ifn.condition, output):
            return ifn.then
        return ifn.else_

    return ""


# ---------------------------------------------------------------------------
# Condition evaluation
# ---------------------------------------------------------------------------

def _evaluate_condition(condition: str, output: Any) -> bool:
    """Evaluate a condition expression against step output."""
    output_map = output if isinstance(output, dict) else {}
    condition = condition.strip()

    if condition.startswith("!"):
        inner = condition[1:].strip()
        return not _evaluate_positive(inner, output_map)

    if "==" in condition:
        left, right = condition.split("==", 1)
        left_val = _resolve_field_path(left.strip(), output_map)
        right_val = right.strip().strip("\"'")
        return str(left_val) == right_val

    if ">=" in condition:
        left, right = condition.split(">=", 1)
        left_val = _resolve_field_path(left.strip(), output_map)
        return _to_float(left_val) >= _to_float(right.strip())

    if ">" in condition:
        left, right = condition.split(">", 1)
        left_val = _resolve_field_path(left.strip(), output_map)
        return _to_float(left_val) > _to_float(right.strip())

    return _evaluate_positive(condition, output_map)


def _evaluate_positive(expr: str, output_map: dict[str, Any]) -> bool:
    val = _resolve_field_path(expr, output_map)
    return _is_truthy(val)


def _resolve_field_path(path: str, output_map: dict[str, Any]) -> Any:
    path = path.removeprefix("output.")
    parts = path.split(".")
    current: Any = output_map
    for part in parts:
        if isinstance(current, dict):
            current = current.get(part)
        else:
            return None
    return current


def _is_truthy(val: Any) -> bool:
    if val is None:
        return False
    if isinstance(val, bool):
        return val
    if isinstance(val, str):
        return val != ""
    if isinstance(val, (int, float)):
        return val != 0
    return True


def _to_float(v: Any) -> float:
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _get_step_id(entry: Any) -> str:
    if hasattr(entry, "id"):
        return entry.id
    return ""


def _emit_trail(trail: list[dict[str, Any]], step_id: str, event: str, data: Any = None) -> None:
    trail.append({
        "step_id": step_id,
        "event": event,
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "data": data,
    })


def _mark_not_executed(entry: Any, results: list[dict[str, Any]]) -> None:
    if isinstance(entry, ParallelBlock):
        for b in entry.branches:
            results.append({"step_id": b.id, "agent": b.agent, "status": "not_executed", "output": None, "metrics": {}})
    elif hasattr(entry, "id"):
        agent = getattr(entry, "agent", "")
        results.append({"step_id": entry.id, "agent": agent, "status": "not_executed", "output": None, "metrics": {}})


def _step_result_to_dict(sr: StepResult) -> dict[str, Any]:
    d: dict[str, Any] = {
        "step_id": sr.step_id,
        "agent": sr.agent,
        "status": sr.status,
        "output": sr.output,
        "metrics": {
            "duration_ms": sr.metrics.duration_ms,
            "prompt_tokens": sr.metrics.prompt_tokens,
            "completion_tokens": sr.metrics.completion_tokens,
            "total_tokens": sr.metrics.total_tokens,
        },
        "attempts": sr.attempts,
        "used_fallback": sr.used_fallback,
    }
    if sr.workflow:
        d["workflow"] = sr.workflow
    if sr.fallback_reason:
        d["fallback_reason"] = sr.fallback_reason
    if sr.sub_envelope:
        d["sub_envelope"] = sr.sub_envelope
    if sr.error:
        d["error"] = sr.error
    return d
