// @generated from agentic-spec/agents/comparison-researcher/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ComparisonResearcherInput {
    productName: string;
    category: string;
    price: number;
    brand: string;
    description: string;
}

export async function comparisonResearcher(input: ComparisonResearcherInput): Promise<AgentResult> {
    return invokeAgent('comparison-researcher', input);
}
