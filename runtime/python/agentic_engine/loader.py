"""Loads agent definitions, workflow definitions, and JSON schemas from disk.

The project root is located by walking up from the current working directory
until a directory containing ``agentic.config.yaml`` or an ``agents/``
subdirectory is found.  Results are cached so repeated loads are cheap.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

import yaml

from .logger import create_logger
from .types import (
    AgentDefinition,
    FallbackConfig,
    ForEachBlock,
    IfNext,
    LoopBlock,
    NextField,
    ParallelBlock,
    ParallelBranch,
    RetryConfig,
    SwitchNext,
    WorkflowDefinition,
    WorkflowStep,
)

log = create_logger("loader")

# ---------------------------------------------------------------------------
# Caches
# ---------------------------------------------------------------------------
_agent_cache: dict[str, AgentDefinition] = {}
_workflow_cache: dict[str, WorkflowDefinition] = {}
_schema_cache: dict[str, dict[str, Any]] = {}
_project_root: str | None = None


# ---------------------------------------------------------------------------
# Project root discovery
# ---------------------------------------------------------------------------

def _find_project_root() -> str:
    """Walk up from cwd looking for agentic.config.yaml, agentic-spec/ dir, or agents/ dir."""
    global _project_root
    if _project_root is not None:
        return _project_root

    current = Path.cwd()
    while True:
        if (current / "agentic.config.yaml").exists():
            _project_root = str(current)
            return _project_root
        if (current / "agentic-spec").is_dir():
            _project_root = str(current)
            return _project_root
        if (current / "agents").is_dir():
            _project_root = str(current)
            return _project_root
        parent = current.parent
        if parent == current:
            raise FileNotFoundError(
                "Could not locate project root (no agentic.config.yaml, agentic-spec/, or agents/ directory found)."
            )
        current = parent


def get_project_root() -> str:
    """Return the cached project root path, discovering it if necessary."""
    return _find_project_root()


# ---------------------------------------------------------------------------
# Agent loading
# ---------------------------------------------------------------------------

def load_agent(agent_id: str) -> AgentDefinition:
    """Load an agent definition from ``agentic-spec/agents/<agent_id>/agent.yaml``."""
    if agent_id in _agent_cache:
        return _agent_cache[agent_id]

    root = get_project_root()
    agent_dir = os.path.join(root, "agentic-spec", "agents", agent_id)
    yaml_path = os.path.join(agent_dir, "agent.yaml")

    if not os.path.isfile(yaml_path):
        raise FileNotFoundError(f"Agent definition not found: {yaml_path}")

    with open(yaml_path, "r", encoding="utf-8") as fh:
        raw: dict[str, Any] = yaml.safe_load(fh)

    prompt_path = os.path.join(agent_dir, "prompt.md")
    system_prompt = ""
    if os.path.isfile(prompt_path):
        with open(prompt_path, "r", encoding="utf-8") as fh:
            system_prompt = fh.read()

    agent_def = AgentDefinition(
        name=raw.get("name", ""),
        description=raw.get("description", ""),
        type=raw.get("type", ""),
        model=raw.get("model", ""),
        temperature=float(raw.get("temperature", 0.0)),
        input_type=raw.get("input_type", "text"),
        image_detail=raw.get("image_detail", "auto"),
        schema=raw.get("schema"),
        user_message=raw.get("user_message", ""),
        handler=raw.get("handler", ""),
        input=raw.get("input", {}),
        system_prompt=system_prompt,
        base_url=raw.get("base_url", ""),
        api_key_env=raw.get("api_key_env", ""),
    )

    _agent_cache[agent_id] = agent_def
    log.debug("Loaded agent", agent_id=agent_id)
    return agent_def


# ---------------------------------------------------------------------------
# Workflow loading
# ---------------------------------------------------------------------------

def _parse_retry(raw: dict[str, Any] | None) -> RetryConfig | None:
    if not raw:
        return None
    return RetryConfig(
        max_attempts=int(raw.get("max_attempts", 1)),
        backoff_ms=int(raw.get("backoff_ms", 0)),
    )


def _parse_fallback(raw: dict[str, Any] | None) -> FallbackConfig | None:
    if not raw:
        return None
    return FallbackConfig(
        agent=raw.get("agent", ""),
        workflow=raw.get("workflow", ""),
        config=raw.get("config", {}),
    )


def _parse_next(raw: Any) -> NextField | None:
    """Parse a next: value from YAML into a NextField."""
    if raw is None:
        return None

    # Simple string target
    if isinstance(raw, str):
        return NextField(target=raw)

    if isinstance(raw, dict):
        # switch: { switch, cases, default }
        if "switch" in raw:
            return NextField(switch=SwitchNext(
                expression=raw["switch"],
                cases=raw.get("cases", {}),
                default=raw.get("default", ""),
            ))

        # if: { if, then, else }
        if "if" in raw:
            return NextField(if_=IfNext(
                condition=raw["if"],
                then=raw.get("then", ""),
                else_=raw.get("else", ""),
            ))

        raise ValueError(f"next: mapping must contain 'switch' or 'if' key, got: {list(raw.keys())}")

    raise ValueError(f"next: must be a string or mapping, got: {type(raw).__name__}")


def _parse_step(raw: dict[str, Any]) -> WorkflowStep:
    """Parse a raw step dict into a WorkflowStep."""
    return WorkflowStep(
        id=raw.get("id", ""),
        agent=raw.get("agent", ""),
        workflow=raw.get("workflow", ""),
        input=raw.get("input", {}),
        config=raw.get("config", {}),
        retry=_parse_retry(raw.get("retry")),
        fallback=_parse_fallback(raw.get("fallback")),
        requires=raw.get("requires", []),
        next=_parse_next(raw.get("next")),
    )


def _parse_parallel_branch(raw: dict[str, Any]) -> ParallelBranch:
    return ParallelBranch(
        id=raw.get("id", ""),
        agent=raw.get("agent", ""),
        workflow=raw.get("workflow", ""),
        input=raw.get("input", {}),
        config=raw.get("config", {}),
        retry=_parse_retry(raw.get("retry")),
        fallback=_parse_fallback(raw.get("fallback")),
    )


def load_workflow(workflow_name: str) -> WorkflowDefinition:
    """Load a workflow definition from ``agentic-spec/workflows/<workflow_name>.yaml``."""
    if workflow_name in _workflow_cache:
        return _workflow_cache[workflow_name]

    root = get_project_root()
    yaml_path = os.path.join(root, "agentic-spec", "workflows", f"{workflow_name}.yaml")

    if not os.path.isfile(yaml_path):
        raise FileNotFoundError(f"Workflow definition not found: {yaml_path}")

    with open(yaml_path, "r", encoding="utf-8") as fh:
        raw: dict[str, Any] = yaml.safe_load(fh)

    steps: list[WorkflowStep | ParallelBlock | LoopBlock | ForEachBlock] = []
    for entry in raw.get("steps", []):
        if "parallel" in entry:
            p = entry["parallel"]
            block = ParallelBlock(
                id=p.get("id", ""),
                join=p.get("join", "all"),
                branches=[_parse_parallel_branch(b) for b in p.get("branches", [])],
                next=_parse_next(p.get("next")),
            )
            steps.append(block)

        elif "loop" in entry:
            l = entry["loop"]
            block = LoopBlock(
                id=l.get("id", ""),
                agent=l.get("agent", ""),
                workflow=l.get("workflow", ""),
                input=l.get("input", {}),
                config=l.get("config", {}),
                until=l.get("until", ""),
                max_iterations=int(l.get("max_iterations", 1)),
                retry=_parse_retry(l.get("retry")),
                fallback=_parse_fallback(l.get("fallback")),
                next=_parse_next(l.get("next")),
            )
            steps.append(block)

        elif "for_each" in entry:
            fe = entry["for_each"]
            block = ForEachBlock(
                id=fe.get("id", ""),
                agent=fe.get("agent", ""),
                workflow=fe.get("workflow", ""),
                input=fe.get("input", {}),
                config=fe.get("config", {}),
                collection=fe.get("collection", ""),
                max_concurrency=int(fe.get("max_concurrency", 0)),
                retry=_parse_retry(fe.get("retry")),
                fallback=_parse_fallback(fe.get("fallback")),
                next=_parse_next(fe.get("next")),
            )
            steps.append(block)

        else:
            steps.append(_parse_step(entry))

    wf = WorkflowDefinition(
        name=raw.get("name", ""),
        description=raw.get("description", ""),
        version=raw.get("version", ""),
        input=raw.get("input", {}),
        steps=steps,
        output=raw.get("output", {}),
    )

    _workflow_cache[workflow_name] = wf
    log.debug("Loaded workflow", workflow_name=workflow_name)
    return wf


# ---------------------------------------------------------------------------
# Schema loading
# ---------------------------------------------------------------------------

def _load_all_schemas() -> None:
    """Scan ``agentic-spec/schemas/`` and load every JSON file into the cache."""
    root = get_project_root()
    schemas_dir = os.path.join(root, "agentic-spec", "schemas")
    if not os.path.isdir(schemas_dir):
        return
    for filename in os.listdir(schemas_dir):
        if not filename.endswith(".json"):
            continue
        schema_name = filename.removesuffix(".json")
        if schema_name in _schema_cache:
            continue
        filepath = os.path.join(schemas_dir, filename)
        with open(filepath, "r", encoding="utf-8") as fh:
            schema_obj: dict[str, Any] = json.load(fh)
        _schema_cache[schema_name] = schema_obj
        log.debug("Loaded schema", schema_name=schema_name)


_schemas_loaded = False


def load_schema(schema_name: str) -> dict[str, Any]:
    """Return a JSON schema dict by name."""
    global _schemas_loaded
    if not _schemas_loaded:
        _load_all_schemas()
        _schemas_loaded = True

    if schema_name not in _schema_cache:
        raise KeyError(f"Schema not found: {schema_name}")
    return _schema_cache[schema_name]


def register_schema(name: str, schema_dict: dict[str, Any]) -> None:
    """Manually register a JSON schema (takes priority over file-loaded ones)."""
    _schema_cache[name] = schema_dict


# ---------------------------------------------------------------------------
# Cache management (useful in tests)
# ---------------------------------------------------------------------------

def clear_caches() -> None:
    """Clear all internal caches and reset project root discovery."""
    global _project_root, _schemas_loaded
    _agent_cache.clear()
    _workflow_cache.clear()
    _schema_cache.clear()
    _project_root = None
    _schemas_loaded = False
