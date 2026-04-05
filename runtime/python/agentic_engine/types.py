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
    """Configuration for step fallback agent or workflow."""

    agent: str = ""
    workflow: str = ""
    config: dict[str, Any] = field(default_factory=dict)


@dataclass
class SwitchNext:
    """Value-based branching: dispatch on an expression."""

    expression: str = ""
    cases: dict[str, str] = field(default_factory=dict)
    default: str = ""


@dataclass
class IfNext:
    """Binary branching: evaluate a condition."""

    condition: str = ""
    then: str = ""
    else_: str = ""


@dataclass
class NextField:
    """Control flow field — exactly one of target, switch, or if_ is set."""

    target: str = ""
    switch: SwitchNext | None = None
    if_: IfNext | None = None


@dataclass
class WorkflowStep:
    """A single step within a workflow (agent or sub-workflow invocation)."""

    id: str = ""
    agent: str = ""
    workflow: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    retry: RetryConfig | None = None
    fallback: FallbackConfig | None = None
    requires: list[str] = field(default_factory=list)
    next: NextField | None = None


@dataclass
class ParallelBranch:
    """A single branch within a parallel block."""

    id: str = ""
    agent: str = ""
    workflow: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    retry: RetryConfig | None = None
    fallback: FallbackConfig | None = None


@dataclass
class ParallelBlock:
    """A set of branches that run concurrently."""

    id: str = ""
    join: str = "all"  # "all", "any", "all_settled"
    branches: list[ParallelBranch] = field(default_factory=list)
    next: NextField | None = None


@dataclass
class LoopBlock:
    """Bounded iteration step."""

    id: str = ""
    agent: str = ""
    workflow: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    until: str = ""
    max_iterations: int = 1
    retry: RetryConfig | None = None
    fallback: FallbackConfig | None = None
    next: NextField | None = None


@dataclass
class ForEachBlock:
    """Dynamic fan-out over a runtime array."""

    id: str = ""
    agent: str = ""
    workflow: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    collection: str = ""
    max_concurrency: int = 0
    retry: RetryConfig | None = None
    fallback: FallbackConfig | None = None
    next: NextField | None = None


@dataclass
class TrailEntry:
    """A single event in the workflow execution trail."""

    step_id: str = ""
    event: str = ""
    timestamp: str = ""
    data: Any = None


@dataclass
class WorkflowDefinition:
    """Parsed workflow YAML definition."""

    name: str = ""
    description: str = ""
    version: str = ""
    input: dict[str, Any] = field(default_factory=dict)
    steps: list[WorkflowStep | ParallelBlock | LoopBlock | ForEachBlock] = field(default_factory=list)
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
    base_url: str = ""
    api_key_env: str = ""


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
    workflow: str = ""
    status: str = "success"  # "success", "error", "not_executed", "partial_failure"
    output: Any = None
    metrics: StepMetrics = field(default_factory=StepMetrics)
    attempts: int = 0
    used_fallback: bool = False
    fallback_reason: str = ""
    sub_envelope: dict[str, Any] | None = None
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


class WorkflowError(Exception):
    """Wraps a workflow failure with the partial envelope (including trail)."""

    def __init__(self, message: str, envelope: dict[str, Any] | None = None):
        super().__init__(message)
        self.envelope = envelope


# Type alias for the envelope dict returned by orchestrate().
WorkflowEnvelope = dict[str, Any]


def to_dict(obj: Any) -> Any:
    """Recursively convert a dataclass instance (or primitive) to a plain dict."""
    if hasattr(obj, "__dataclass_fields__"):
        return asdict(obj)
    return obj
