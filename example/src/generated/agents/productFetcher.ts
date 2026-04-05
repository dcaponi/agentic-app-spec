// @generated from agentic-spec/agents/product-fetcher/agent.yaml — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ProductFetcherInput {
    productId: number;
}

export async function productFetcher(input: ProductFetcherInput): Promise<AgentResult> {
    return invokeAgent('product-fetcher', input);
}
