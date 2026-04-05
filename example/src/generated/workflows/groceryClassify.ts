// @generated from agentic-spec/workflows/grocery-classify.yaml — do not edit
import { orchestrate } from '../../engine/orchestrator.js';
import type { WorkflowEnvelope } from '../../types.js';

export interface GroceryClassifyInput {
    itemName: string;
}

export async function groceryClassify(input: GroceryClassifyInput): Promise<WorkflowEnvelope> {
    return orchestrate('grocery-classify', input);
}
