// @generated from agentic-spec/agents/food-classifier/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface FoodClassifierInput {
    itemName: string;
}

export async function foodClassifier(input: FoodClassifierInput): Promise<AgentResult> {
    return invokeAgent('food-classifier', input);
}
