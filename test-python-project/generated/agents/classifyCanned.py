# @generated from agents/classify-canned/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class ClassifyCannedInput:
    item_name: str


async def classify_canned(input: ClassifyCannedInput) -> AgentResult:
    return await invoke_agent('classify-canned', vars(input))
