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
    ParallelGroup,
    RetryConfig,
    RouteBlock,
    RouteEntry,
    RouterDefinition,
    ShortCircuit,
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
_router_cache: dict[str, RouterDefinition] = {}
_project_root: str | None = None


# ---------------------------------------------------------------------------
# Project root discovery
# ---------------------------------------------------------------------------

def _find_project_root() -> str:
    """Walk up from cwd looking for agentic.config.yaml or agents/ dir."""
    global _project_root
    if _project_root is not None:
        return _project_root

    current = Path.cwd()
    while True:
        if (current / "agentic.config.yaml").exists():
            _project_root = str(current)
            return _project_root
        if (current / "agents").is_dir():
            _project_root = str(current)
            return _project_root
        parent = current.parent
        if parent == current:
            # Reached filesystem root without finding a marker.
            raise FileNotFoundError(
                "Could not locate project root (no agentic.config.yaml or agents/ directory found)."
            )
        current = parent


def get_project_root() -> str:
    """Return the cached project root path, discovering it if necessary."""
    return _find_project_root()


# ---------------------------------------------------------------------------
# Agent loading
# ---------------------------------------------------------------------------

def load_agent(agent_id: str) -> AgentDefinition:
    """Load an agent definition from ``agents/<agent_id>/agent.yaml``.

    If a ``prompt.md`` file exists alongside the YAML, its contents are stored
    in the ``system_prompt`` field.
    """
    if agent_id in _agent_cache:
        return _agent_cache[agent_id]

    root = get_project_root()
    agent_dir = os.path.join(root, "agents", agent_id)
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
        provider=raw.get("provider", ""),
        model=raw.get("model", ""),
        temperature=float(raw.get("temperature", 0.0)),
        input_type=raw.get("input_type", "text"),
        image_detail=raw.get("image_detail", "auto"),
        schema=raw.get("schema"),
        user_message=raw.get("user_message", ""),
        handler=raw.get("handler", ""),
        input=raw.get("input", {}),
        system_prompt=system_prompt,
    )

    _agent_cache[agent_id] = agent_def
    log.debug("Loaded agent", agent_id=agent_id)
    return agent_def


# ---------------------------------------------------------------------------
# Router loading
# ---------------------------------------------------------------------------

def load_router(router_id: str) -> RouterDefinition:
    """Load a router definition from ``routers/<router_id>/router.yaml``.

    If a ``prompt.md`` file exists alongside the YAML, its contents are stored
    in the ``prompt`` field.
    """
    if router_id in _router_cache:
        return _router_cache[router_id]

    root = get_project_root()
    router_dir = os.path.join(root, "routers", router_id)
    yaml_path = os.path.join(router_dir, "router.yaml")

    if not os.path.isfile(yaml_path):
        raise FileNotFoundError(f"Router definition not found: {yaml_path}")

    with open(yaml_path, "r", encoding="utf-8") as fh:
        raw: dict[str, Any] = yaml.safe_load(fh)

    prompt_path = os.path.join(router_dir, "prompt.md")
    prompt = ""
    if os.path.isfile(prompt_path):
        with open(prompt_path, "r", encoding="utf-8") as fh:
            prompt = fh.read().strip()
    elif raw.get("strategy", "llm") == "llm":
        log.warn("LLM router has no prompt.md — system prompt will be empty", router_id=router_id)

    router_def = RouterDefinition(
        name=raw.get("name", ""),
        description=raw.get("description", ""),
        strategy=raw.get("strategy", "llm"),
        provider=raw.get("provider", ""),
        model=raw.get("model", ""),
        temperature=float(raw.get("temperature", 0.0)),
        handler=raw.get("handler", ""),
        prompt=prompt,
        input=raw.get("input"),
    )

    _router_cache[router_id] = router_def
    log.debug("Loaded router", router_id=router_id)
    return router_def


# ---------------------------------------------------------------------------
# Workflow loading
# ---------------------------------------------------------------------------

def _parse_step(raw: dict[str, Any]) -> WorkflowStep:
    """Parse a raw step dict into a WorkflowStep dataclass."""
    retry: RetryConfig | None = None
    if "retry" in raw:
        r = raw["retry"]
        retry = RetryConfig(
            max_attempts=int(r.get("max_attempts", 1)),
            backoff_ms=int(r.get("backoff_ms", 0)),
        )

    fallback: FallbackConfig | None = None
    if "fallback" in raw:
        f = raw["fallback"]
        fallback = FallbackConfig(
            agent=f.get("agent", ""),
            config=f.get("config", {}),
        )

    short_circuit: ShortCircuit | None = None
    if "short_circuit" in raw:
        sc = raw["short_circuit"]
        short_circuit = ShortCircuit(
            condition=sc.get("condition", ""),
            defaults=sc.get("defaults", {}),
        )

    return WorkflowStep(
        id=raw.get("id", ""),
        agent=raw.get("agent", ""),
        input=raw.get("input", {}),
        config=raw.get("config", {}),
        retry=retry,
        fallback=fallback,
        short_circuit=short_circuit,
    )


def _parse_route_entry(raw: dict[str, Any]) -> RouteEntry:
    """Parse a raw ``route`` block dict into a ``RouteEntry`` dataclass."""
    retry: RetryConfig | None = None
    if "retry" in raw:
        r = raw["retry"]
        retry = RetryConfig(
            max_attempts=int(r.get("max_attempts", 1)),
            backoff_ms=int(r.get("backoff_ms", 0)),
        )

    fallback: dict[str, Any] | None = raw.get("fallback")

    route_block = RouteBlock(
        id=raw.get("id", ""),
        router=raw.get("router", ""),
        input=raw.get("input", {}),
        routes=raw.get("routes", {}),
        retry=retry,
        fallback=fallback,
    )
    return RouteEntry(route=route_block)


def load_workflow(workflow_name: str) -> WorkflowDefinition:
    """Load a workflow definition from ``workflows/<workflow_name>.yaml``."""
    if workflow_name in _workflow_cache:
        return _workflow_cache[workflow_name]

    root = get_project_root()
    yaml_path = os.path.join(root, "workflows", f"{workflow_name}.yaml")

    if not os.path.isfile(yaml_path):
        raise FileNotFoundError(f"Workflow definition not found: {yaml_path}")

    with open(yaml_path, "r", encoding="utf-8") as fh:
        raw: dict[str, Any] = yaml.safe_load(fh)

    steps: list[WorkflowStep | ParallelGroup | RouteEntry] = []
    for entry in raw.get("steps", []):
        if "parallel" in entry:
            group = ParallelGroup(
                parallel=[_parse_step(s) for s in entry["parallel"]]
            )
            steps.append(group)
        elif "route" in entry:
            steps.append(_parse_route_entry(entry["route"]))
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
    """Scan ``schemas/`` and load every JSON file into the cache."""
    root = get_project_root()
    schemas_dir = os.path.join(root, "schemas")
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
    """Return a JSON schema dict by name.

    On first call this eagerly loads all schemas from the ``schemas/``
    directory.  Schemas registered via ``register_schema`` take priority.
    """
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
    _router_cache.clear()
    _project_root = None
    _schemas_loaded = False
