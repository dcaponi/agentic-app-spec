# @generated from agentic-spec/agents/classify-meat/agent.yaml — do not edit
from dataclasses import dataclass
from typing import Any
from agentic_engine.runner import invoke_agent
from agentic_engine.types import AgentResult


@dataclass
class ClassifyMeatInput:
    item_name: str


async def classify_meat(input: ClassifyMeatInput) -> AgentResult:
    return await invoke_agent('classify-meat', vars(input))
