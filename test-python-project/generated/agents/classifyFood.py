# @generated from agentic-spec/agents/classify-food/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class ClassifyFoodInput:
    item_name: str


async def classify_food(input: ClassifyFoodInput) -> AgentResult:
    return await invoke_agent('classify-food', vars(input))
