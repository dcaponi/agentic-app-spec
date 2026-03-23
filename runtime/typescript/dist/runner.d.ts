import type { AgentDefinition, AgentResult, DeterministicHandler } from './types.js';
/**
 * Register a deterministic handler function.
 * Called by generated stubs or user code:
 *   registerHandler('product_fetch', (input) => { ... })
 */
export declare function registerHandler(name: string, fn: DeterministicHandler): void;
/** Get all registered handler names (for diagnostics). */
export declare function getRegisteredHandlers(): string[];
/** Invoke a single agent by ID with the given input. */
export declare function invokeAgent(agentId: string, input: Record<string, unknown>): Promise<AgentResult>;
/** Execute an agent (used by orchestrator and invokeAgent). */
export declare function executeAgent(input: Record<string, unknown>, agentDef: AgentDefinition): Promise<AgentResult>;
//# sourceMappingURL=runner.d.ts.map