// ── Public types (importable by consumers) ──

export interface StepMetrics {
	latency_ms: number;
	input_tokens: number;
	output_tokens: number;
}

export interface TrailEntry {
	step_id: string;
	event: string;
	timestamp: string;
	data?: unknown;
}

export interface StepResult {
	id: string;
	agent?: string;
	workflow?: string;
	status: 'success' | 'error' | 'not_executed' | 'partial_failure';
	output: unknown;
	metrics: StepMetrics;
	attempts?: number;
	used_fallback?: boolean;
	fallback_reason?: string;
	sub_envelope?: WorkflowEnvelope;
	error?: string;
}

export interface WorkflowEnvelope<T = unknown> {
	workflow: string;
	version: string;
	request_id: string;
	status: 'success' | 'error' | 'partial_failure';
	timestamps: {
		started_at: string;
		completed_at: string;
	};
	metrics: {
		total_latency_ms: number;
		total_input_tokens: number;
		total_output_tokens: number;
	};
	steps: StepResult[];
	trail: TrailEntry[];
	result: T;
	error?: string;
}

export class WorkflowError extends Error {
	envelope?: WorkflowEnvelope;
	constructor(message: string, envelope?: WorkflowEnvelope) {
		super(message);
		this.name = 'WorkflowError';
		this.envelope = envelope;
	}
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
	base_url?: string;
	api_key_env?: string;
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
	agent?: string;
	workflow?: string;
	config?: Record<string, unknown>;
}

export interface SwitchNext {
	switch: string;
	cases: Record<string, string>;
	default?: string;
}

export interface IfNext {
	if: string;
	then: string;
	else: string;
}

export type NextField = string | SwitchNext | IfNext;

export interface WorkflowStep {
	id: string;
	agent?: string;
	workflow?: string;
	input: Record<string, string>;
	config?: Record<string, unknown>;
	retry?: RetryConfig;
	fallback?: FallbackConfig;
	requires?: string[];
	next?: NextField;
}

export interface ParallelBranch {
	id: string;
	agent?: string;
	workflow?: string;
	input: Record<string, string>;
	config?: Record<string, unknown>;
	retry?: RetryConfig;
	fallback?: FallbackConfig;
}

export interface ParallelBlock {
	parallel: {
		id: string;
		join?: 'all' | 'any' | 'all_settled';
		branches: ParallelBranch[];
		next?: NextField;
	};
}

export interface LoopBlock {
	loop: {
		id: string;
		agent?: string;
		workflow?: string;
		input: Record<string, string>;
		config?: Record<string, unknown>;
		until: string;
		max_iterations: number;
		retry?: RetryConfig;
		fallback?: FallbackConfig;
		next?: NextField;
	};
}

export interface ForEachBlock {
	for_each: {
		id: string;
		agent?: string;
		workflow?: string;
		input: Record<string, string>;
		config?: Record<string, unknown>;
		collection: string;
		max_concurrency?: number;
		retry?: RetryConfig;
		fallback?: FallbackConfig;
		next?: NextField;
	};
}

export type WorkflowEntry = WorkflowStep | ParallelBlock | LoopBlock | ForEachBlock;

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
