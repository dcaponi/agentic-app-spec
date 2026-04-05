# @generated from agentic-spec/agents/classify-non-food/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class ClassifyNonFoodInput:
    item_name: str


async def classify_non_food(input: ClassifyNonFoodInput) -> AgentResult:
    return await invoke_agent('classify-non-food', vars(input))
