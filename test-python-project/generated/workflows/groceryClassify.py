# @generated from agentic-spec/workflows/grocery-classify.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.orchestrator import orchestrate
from agentic_engine.types import WorkflowEnvelope


@dataclass
class GroceryClassifyInput:
    item_name: str


async def grocery_classify(input: GroceryClassifyInput) -> WorkflowEnvelope:
    return await orchestrate('grocery-classify', vars(input))
