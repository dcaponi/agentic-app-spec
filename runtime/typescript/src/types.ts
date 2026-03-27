// ── Public types (importable by consumers) ──

export interface StepMetrics {
	latency_ms: number;
	input_tokens: number;
	output_tokens: number;
}

export interface StepResult {
	id: string;
	agent: string;
	status: 'success' | 'skipped' | 'error';
	output: unknown;
	metrics: StepMetrics;
	attempts?: number;
	used_fallback?: boolean;
	error?: string;
}

export interface WorkflowEnvelope<T = unknown> {
	workflow: string;
	version: string;
	request_id: string;
	status: 'success' | 'error' | 'short_circuited';
	timestamps: {
		started_at: string;
		completed_at: string;
	};
	metrics: {
		total_latency_ms: number;
		total_input_tokens: number;
		total_output_tokens: number;
		steps_executed: number;
		steps_skipped: number;
	};
	steps: StepResult[];
	result: T;
	error?: string;
}

export interface AgentResult<T = unknown> {
	output: T;
	metrics: StepMetrics;
}

export interface WorkflowSummary {
	name: string;
	description: string;
	version: string;
	input: Record<string, { type: string; required?: boolean }>;
}

// ── Engine-internal types ──

export interface AgentDefinition {
	name: string;
	description: string;
	type: 'llm' | 'deterministic';
	provider?: 'openai' | 'anthropic';
	model?: string;
	temperature?: number;
	input_type?: 'image' | 'text';
	image_detail?: 'low' | 'high';
	schema?: string | null;
	user_message?: string;
	handler?: string;
	prompt?: string;
	input?: Record<string, { type: string; required?: boolean }>;
}

export interface RetryConfig {
	max_attempts: number;
	backoff_ms: number;
}

export interface FallbackConfig {
	agent: string;
	config?: Record<string, unknown>;
}

export interface ShortCircuit {
	condition: string;
	defaults: Record<string, unknown>;
}

export interface WorkflowStep {
	id: string;
	agent: string;
	input: Record<string, string>;
	config?: Record<string, unknown>;
	retry?: RetryConfig;
	fallback?: FallbackConfig;
	short_circuit?: ShortCircuit;
}

export interface ParallelGroup {
	parallel: WorkflowStep[];
}

export type WorkflowEntry = WorkflowStep | ParallelGroup;

export interface WorkflowDefinition {
	name: string;
	description: string;
	version: string;
	input: Record<string, { type: string; required?: boolean }>;
	steps: WorkflowEntry[];
	output: Record<string, string>;
}

export interface ExecutionContext {
	input: unknown;
	steps: Record<string, { output: unknown }>;
}

// ── Handler & schema registry types ──

/** A deterministic handler function registered by the user. */
export type DeterministicHandler = (input: Record<string, unknown>) => AgentResult | Promise<AgentResult>;

/** A JSON Schema object (for non-Zod runtimes). */
export interface JsonSchemaObject {
	type: string;
	properties?: Record<string, unknown>;
	required?: string[];
	[key: string]: unknown;
}
