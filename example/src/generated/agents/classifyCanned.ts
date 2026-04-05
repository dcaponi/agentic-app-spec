// @generated from agentic-spec/agents/classify-canned/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ClassifyCannedInput {
    itemName: string;
}

export async function classifyCanned(input: ClassifyCannedInput): Promise<AgentResult> {
    return invokeAgent('classify-canned', input);
}
