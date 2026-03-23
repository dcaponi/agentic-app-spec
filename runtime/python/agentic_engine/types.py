"""Core data types for the Agentic Engine runtime."""

from __future__ import annotations

from dataclasses import dataclass, field, asdict
from typing import Any


@dataclass
class RetryConfig:
    """Configuration for step retries."""

    max_attempts: int = 1
    backoff_ms: int = 0


@dataclass
class FallbackConfig:
    """Configuration for step fallback agent."""

    agent: str = ""
    config: dict[str, Any] = field(default_factory=dict)


@dataclass
class ShortCircuit:
    """Early-exit condition that skips remaining steps when true."""

    condition: str = ""
    defaults: dict[str, Any] = field(default_factory=dict)


@dataclass
class WorkflowStep:
    """A single step within a workflow."""

    id: str = ""
    agent: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    retry: RetryConfig | None = None
    fallback: FallbackConfig | None = None
    short_circuit: ShortCircuit | None = None


@dataclass
class ParallelGroup:
    """A group of steps that execute concurrently."""

    parallel: list[WorkflowStep] = field(default_factory=list)


@dataclass
class WorkflowDefinition:
    """Parsed workflow YAML definition."""

    name: str = ""
    description: str = ""
    version: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    steps: list[WorkflowStep | ParallelGroup] = field(default_factory=list)
    output: dict[str, str] = field(default_factory=dict)


@dataclass
class AgentDefinition:
    """Parsed agent YAML definition."""

    name: str = ""
    description: str = ""
    type: str = ""  # "llm" or "deterministic"
    model: str = ""
    temperature: float = 0.0
    input_type: str = "text"
    image_detail: str = "auto"
    schema: str | None = None
    user_message: str = ""
    handler: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    system_prompt: str = ""


@dataclass
class StepMetrics:
    """Timing and token metrics for a single step or LLM call."""

    duration_ms: float = 0.0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


@dataclass
class StepResult:
    """Result of executing a single workflow step."""

    step_id: str = ""
    agent: str = ""
    output: Any = None
    metrics: StepMetrics = field(default_factory=StepMetrics)
    retries: int = 0
    used_fallback: bool = False
    error: str | None = None


@dataclass
class AgentResult:
    """Result returned from a single agent invocation."""

    output: Any = None
    metrics: StepMetrics = field(default_factory=StepMetrics)


@dataclass
class ExecutionContext:
    """Runtime context threaded through workflow execution."""

    input: dict[str, Any] = field(default_factory=dict)
    steps: dict[str, dict[str, Any]] = field(default_factory=dict)


@dataclass
class WorkflowEnvelope:
    """Top-level envelope wrapping a complete workflow execution result."""

    request_id: str = ""
    workflow: str = ""
    version: str = ""
    status: str = "success"  # "success" or "error"
    output: dict[str, Any] = field(default_factory=dict)
    steps: list[dict[str, Any]] = field(default_factory=list)
    metrics: dict[str, Any] = field(default_factory=dict)
    started_at: str = ""
    completed_at: str = ""
    error: str | None = None


def to_dict(obj: Any) -> Any:
    """Recursively convert a dataclass instance (or primitive) to a plain dict."""
    if hasattr(obj, "__dataclass_fields__"):
        return asdict(obj)
    return obj
