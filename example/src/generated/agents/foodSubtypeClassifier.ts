// @generated from agentic-spec/agents/food-subtype-classifier/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface FoodSubtypeClassifierInput {
    itemName: string;
}

export async function foodSubtypeClassifier(input: FoodSubtypeClassifierInput): Promise<AgentResult> {
    return invokeAgent('food-subtype-classifier', input);
}
