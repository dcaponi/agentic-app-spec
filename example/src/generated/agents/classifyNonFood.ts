// @generated from agentic-spec/agents/classify-non-food/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ClassifyNonFoodInput {
    itemName: string;
}

export async function classifyNonFood(input: ClassifyNonFoodInput): Promise<AgentResult> {
    return invokeAgent('classify-non-food', input);
}
