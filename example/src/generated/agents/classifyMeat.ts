// @generated from agentic-spec/agents/classify-meat/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ClassifyMeatInput {
    itemName: string;
}

export async function classifyMeat(input: ClassifyMeatInput): Promise<AgentResult> {
    return invokeAgent('classify-meat', input);
}
