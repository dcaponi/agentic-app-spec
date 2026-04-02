# @generated from agents/classify-vegetable/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class ClassifyVegetableInput:
    item_name: str


async def classify_vegetable(input: ClassifyVegetableInput) -> AgentResult:
    return await invoke_agent('classify-vegetable', vars(input))
