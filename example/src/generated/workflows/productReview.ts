// @generated from agentic-spec/workflows/product-review.yaml — do not edit
import { orchestrate } from '../../engine/orchestrator.js';
import type { WorkflowEnvelope } from '../../types.js';

export interface ProductReviewInput {
    productId: number;
}

export async function productReview(input: ProductReviewInput): Promise<WorkflowEnvelope> {
    return orchestrate('product-review', input);
}
