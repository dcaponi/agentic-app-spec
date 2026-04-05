// @generated from agentic-spec/agents/quality-scorer/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface QualityScorerInput {
    reviewArticle: Record<string, unknown>;
    reviewAnalysis: Record<string, unknown>;
    comparisonReport: Record<string, unknown>;
}

export async function qualityScorer(input: QualityScorerInput): Promise<AgentResult> {
    return invokeAgent('quality-scorer', input);
}
