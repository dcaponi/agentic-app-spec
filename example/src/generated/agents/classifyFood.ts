// @generated from agentic-spec/agents/classify-food/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ClassifyFoodInput {
    itemName: string;
}

export async function classifyFood(input: ClassifyFoodInput): Promise<AgentResult> {
    return invokeAgent('classify-food', input);
}
