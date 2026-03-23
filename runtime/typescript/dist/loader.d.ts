import type { AgentDefinition, WorkflowDefinition, WorkflowSummary, JsonSchemaObject } from './types.js';
/** Set the project root directory for loading agents, workflows, and schemas. */
export declare function setProjectRoot(dir: string): void;
/** Get the current project root directory. */
export declare function getProjectRoot(): string;
export declare function loadAgent(agentId: string): AgentDefinition;
export declare function loadAllAgents(): Map<string, AgentDefinition>;
export declare function loadWorkflow(name: string): WorkflowDefinition;
export declare function listWorkflows(): WorkflowSummary[];
export declare function loadJsonSchema(name: string): JsonSchemaObject;
/** Clear all caches (useful for testing or hot-reload scenarios). */
export declare function clearCaches(): void;
//# sourceMappingURL=loader.d.ts.map