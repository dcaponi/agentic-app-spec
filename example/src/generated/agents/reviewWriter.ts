// @generated from agentic-spec/agents/review-writer/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ReviewWriterInput {
    productName: string;
    brand: string;
    price: number;
    reviewAnalysis: Record<string, unknown>;
    comparisonReport: Record<string, unknown>;
}

export async function reviewWriter(input: ReviewWriterInput): Promise<AgentResult> {
    return invokeAgent('review-writer', input);
}
