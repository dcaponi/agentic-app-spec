// @generated from agentic-spec/agents/review-analyzer/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ReviewAnalyzerInput {
    productName: string;
    category: string;
    price: number;
    rating: number;
    reviewsText: string;
}

export async function reviewAnalyzer(input: ReviewAnalyzerInput): Promise<AgentResult> {
    return invokeAgent('review-analyzer', input);
}
