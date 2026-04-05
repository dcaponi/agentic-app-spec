# @generated from agentic-spec/agents/food-subtype-classifier/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class FoodSubtypeClassifierInput:
    item_name: str


async def food_subtype_classifier(input: FoodSubtypeClassifierInput) -> AgentResult:
    return await invoke_agent('food-subtype-classifier', vars(input))
