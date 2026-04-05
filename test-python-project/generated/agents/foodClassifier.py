# @generated from agentic-spec/agents/food-classifier/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class FoodClassifierInput:
    item_name: str


async def food_classifier(input: FoodClassifierInput) -> AgentResult:
    return await invoke_agent('food-classifier', vars(input))
