// @generated from agentic-spec/agents/classify-vegetable/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ClassifyVegetableInput {
    itemName: string;
}

export async function classifyVegetable(input: ClassifyVegetableInput): Promise<AgentResult> {
    return invokeAgent('classify-vegetable', input);
}
