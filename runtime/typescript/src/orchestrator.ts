import type {
	WorkflowDefinition,
	WorkflowStep,
	WorkflowEntry,
	ParallelBlock,
	ParallelBranch,
	LoopBlock,
	ForEachBlock,
	NextField,
	SwitchNext,
	IfNext,
	ExecutionContext,
	WorkflowEnvelope,
	StepResult,
	StepMetrics,
	TrailEntry,
	FallbackConfig,
	RetryConfig,
} from './types.js';
import { WorkflowError } from './types.js';
import { loadWorkflow, loadAllAgents, loadAgent } from './loader.js';
import { executeAgent } from './runner.js';
import { resolveInputs, resolveOutputs, resolveRef } from './resolver.js';
import { createLogger, serializeError } from './logger.js';

const log = createLogger('orchestrator');

const ZERO_METRICS: StepMetrics = { latency_ms: 0, input_tokens: 0, output_tokens: 0 };

// ── Public API ──

export async function orchestrate(
	workflowName: string,
	input: Record<string, unknown>
): Promise<WorkflowEnvelope> {
	const workflow = loadWorkflow(workflowName);
	loadAllAgents();

	const requestId = crypto.randomUUID();
	const startedAt = new Date().toISOString();
	const startMs = performance.now();

	const context: ExecutionContext = { input, steps: {} };
	const stepResults: StepResult[] = [];
	const trail: TrailEntry[] = [];
	let status: 'success' | 'error' | 'partial_failure' = 'success';
	let errorMsg: string | undefined;

	// Build step index for graph traversal
	const stepIndex = new Map<string, number>();
	for (let i = 0; i < workflow.steps.length; i++) {
		const id = getStepId(workflow.steps[i]);
		if (id) stepIndex.set(id, i);
		// Also index parallel branch IDs
		if (isParallelBlock(workflow.steps[i])) {
			const pb = (workflow.steps[i] as ParallelBlock).parallel;
			for (const branch of pb.branches) {
				if (branch.id) stepIndex.set(branch.id, i);
			}
		}
	}

	const executedSteps = new Set<string>();

	try {
		let cursor = 0;
		while (cursor < workflow.steps.length) {
			const entry = workflow.steps[cursor];
			const sid = getStepId(entry);
			executedSteps.add(sid);

			let nextTarget = '';

			if (isParallelBlock(entry)) {
				nextTarget = await executeParallelBlock(entry as ParallelBlock, context, stepResults, trail);
			} else if (isLoopBlock(entry)) {
				nextTarget = await executeLoopBlock(entry as LoopBlock, context, stepResults, trail);
			} else if (isForEachBlock(entry)) {
				nextTarget = await executeForEachBlock(entry as ForEachBlock, context, stepResults, trail);
			} else {
				nextTarget = await executeWorkflowStep(entry as WorkflowStep, context, stepResults, trail);
			}

			if (nextTarget === '_end') break;
			if (nextTarget) {
				const idx = stepIndex.get(nextTarget);
				if (idx === undefined) throw new Error(`next target "${nextTarget}" not found`);
				cursor = idx;
			} else {
				cursor++;
			}
		}
	} catch (err) {
		status = 'error';
		errorMsg = serializeError(err).message;
		log.error(`Workflow failed: ${workflowName}`, { error: errorMsg });
	}

	// Mark non-executed steps
	for (const entry of workflow.steps) {
		const sid = getStepId(entry);
		if (!executedSteps.has(sid)) {
			markNotExecuted(entry, stepResults);
		}
	}

	// Resolve outputs
	let result: unknown = null;
	try {
		result = resolveOutputs(workflow.output, context);
	} catch { /* swallow */ }

	const envelope = buildEnvelope(workflow, requestId, startedAt, startMs, stepResults, trail, result, status, errorMsg);

	log.info(`Workflow complete: ${workflow.name}`, {
		status: envelope.status,
		total_latency_ms: envelope.metrics.total_latency_ms,
	});

	if (status === 'error') {
		throw new WorkflowError(errorMsg ?? 'unknown error', envelope);
	}

	return envelope;
}

// ── Step execution ──

async function executeWorkflowStep(
	step: WorkflowStep,
	ctx: ExecutionContext,
	results: StepResult[],
	trail: TrailEntry[],
): Promise<string> {
	emitTrail(trail, step.id, 'step_start');

	let sr: StepResult;
	if (step.workflow) {
		sr = await executeSubWorkflow(step, ctx);
	} else {
		sr = await executeAgentStep(step.id, step.agent!, step.input, step.config, step.retry, step.fallback, ctx);
	}

	results.push(sr);

	if (sr.status === 'success') {
		ctx.steps[step.id] = { output: sr.output };
		emitTrail(trail, step.id, 'step_success');
	} else {
		emitTrail(trail, step.id, 'step_error', { error: sr.error });
		throw new Error(`Step '${step.id}' failed: ${sr.error}`);
	}

	return resolveNextTarget(step.next, sr.output);
}

async function executeSubWorkflow(step: WorkflowStep, ctx: ExecutionContext): Promise<StepResult> {
	const resolvedInput = resolveInputs(step.input, ctx);
	const start = performance.now();

	try {
		const subEnvelope = await orchestrate(step.workflow!, resolvedInput);
		return {
			id: step.id,
			workflow: step.workflow,
			status: 'success',
			output: subEnvelope.result,
			sub_envelope: subEnvelope,
			metrics: {
				latency_ms: performance.now() - start,
				input_tokens: subEnvelope.metrics.total_input_tokens,
				output_tokens: subEnvelope.metrics.total_output_tokens,
			},
		};
	} catch (err) {
		const wfErr = err instanceof WorkflowError ? err : undefined;
		return {
			id: step.id,
			workflow: step.workflow,
			status: 'error',
			output: null,
			sub_envelope: wfErr?.envelope,
			metrics: { latency_ms: performance.now() - start, input_tokens: 0, output_tokens: 0 },
			error: serializeError(err).message,
		};
	}
}

async function executeAgentStep(
	stepId: string,
	agentId: string,
	inputBindings: Record<string, string>,
	config: Record<string, unknown> | undefined,
	retry: RetryConfig | undefined,
	fallback: FallbackConfig | undefined,
	ctx: ExecutionContext,
): Promise<StepResult> {
	const agentDef = loadAgent(agentId);
	const mergedAgent = config ? { ...agentDef, ...config } : agentDef;
	const resolvedInput = resolveInputs(inputBindings, ctx);

	const maxAttempts = retry?.max_attempts ?? 1;
	const backoffMs = retry?.backoff_ms ?? 0;
	let lastError: unknown;

	for (let attempt = 1; attempt <= maxAttempts; attempt++) {
		try {
			const result = await executeAgent(resolvedInput, mergedAgent);
			return {
				id: stepId,
				agent: agentId,
				status: 'success',
				output: result.output,
				metrics: result.metrics,
				attempts: attempt,
			};
		} catch (err) {
			lastError = err;
			log.warn(`Step ${stepId}: attempt ${attempt}/${maxAttempts} failed`, {
				error: serializeError(err).message,
			});
			if (attempt < maxAttempts) await sleep(backoffMs * attempt);
		}
	}

	// Fallback
	if (fallback?.agent) {
		log.info(`Step ${stepId}: trying fallback agent`, { fallback_agent: fallback.agent });
		try {
			const fallbackDef = loadAgent(fallback.agent);
			const fbMerged = fallback.config ? { ...fallbackDef, ...fallback.config } : fallbackDef;
			const result = await executeAgent(resolvedInput, fbMerged);
			return {
				id: stepId,
				agent: fallback.agent,
				status: 'success',
				output: result.output,
				metrics: result.metrics,
				attempts: maxAttempts,
				used_fallback: true,
				fallback_reason: serializeError(lastError).message,
			};
		} catch (err) {
			lastError = err;
		}
	}

	return {
		id: stepId,
		agent: agentId,
		status: 'error',
		output: null,
		metrics: ZERO_METRICS,
		attempts: maxAttempts,
		error: serializeError(lastError).message,
	};
}

// ── Parallel execution ──

async function executeParallelBlock(
	entry: ParallelBlock,
	ctx: ExecutionContext,
	results: StepResult[],
	trail: TrailEntry[],
): Promise<string> {
	const pb = entry.parallel;
	emitTrail(trail, pb.id, 'parallel_start', { join: pb.join, branches: pb.branches.length });

	const branchResults = await Promise.all(
		pb.branches.map((branch) => {
			if (branch.workflow) {
				const ws: WorkflowStep = { id: branch.id, workflow: branch.workflow, input: branch.input, config: branch.config };
				return executeSubWorkflow(ws, ctx);
			}
			return executeAgentStep(branch.id, branch.agent!, branch.input, branch.config, branch.retry, branch.fallback, ctx);
		})
	);

	let hasError = false;
	for (const sr of branchResults) {
		results.push(sr);
		if (sr.status === 'success') {
			ctx.steps[sr.id] = { output: sr.output };
		} else {
			hasError = true;
		}
	}

	emitTrail(trail, pb.id, 'parallel_end');

	const join = pb.join ?? 'all';
	if (join === 'all' && hasError) {
		throw new Error(`Parallel block '${pb.id}': one or more branches failed (join=all)`);
	}
	if (join === 'any' && !branchResults.some((r) => r.status === 'success')) {
		throw new Error(`Parallel block '${pb.id}': all branches failed (join=any)`);
	}

	return resolveNextTarget(pb.next, null);
}

// ── Loop execution ──

async function executeLoopBlock(
	entry: LoopBlock,
	ctx: ExecutionContext,
	results: StepResult[],
	trail: TrailEntry[],
): Promise<string> {
	const lb = entry.loop;
	let lastResult: StepResult | undefined;

	for (let i = 1; i <= lb.max_iterations; i++) {
		emitTrail(trail, lb.id, 'loop_iteration', { iteration: i });

		let sr: StepResult;
		if (lb.workflow) {
			const ws: WorkflowStep = { id: lb.id, workflow: lb.workflow, input: lb.input, config: lb.config };
			sr = await executeSubWorkflow(ws, ctx);
		} else {
			sr = await executeAgentStep(lb.id, lb.agent!, lb.input, lb.config, lb.retry, lb.fallback, ctx);
		}

		if (sr.status === 'success') {
			ctx.steps[lb.id] = { output: sr.output };
		} else {
			results.push(sr);
			throw new Error(`Loop '${lb.id}' iteration ${i} failed: ${sr.error}`);
		}

		lastResult = sr;

		if (lb.until && evaluateCondition(lb.until, sr.output)) break;
	}

	if (lastResult) results.push(lastResult);
	return resolveNextTarget(lb.next, null);
}

// ── ForEach execution ──

async function executeForEachBlock(
	entry: ForEachBlock,
	ctx: ExecutionContext,
	results: StepResult[],
	trail: TrailEntry[],
): Promise<string> {
	const feb = entry.for_each;
	const rawCollection = resolveRef(feb.collection, ctx);
	if (!Array.isArray(rawCollection)) {
		throw new Error(`for_each '${feb.id}': collection '${feb.collection}' did not resolve to an array`);
	}
	const collection: unknown[] = rawCollection;

	if (collection.length === 0) {
		ctx.steps[feb.id] = { output: [] };
		results.push({ id: feb.id, agent: feb.agent, status: 'success', output: [], metrics: ZERO_METRICS });
		return resolveNextTarget(feb.next, null);
	}

	const concurrency = feb.max_concurrency && feb.max_concurrency > 0 ? feb.max_concurrency : collection.length;
	const iterResults: unknown[] = new Array(collection.length).fill(null);
	const iterErrors: (string | null)[] = new Array(collection.length).fill(null);

	// Simple concurrency limiter
	let running = 0;
	let nextIdx = 0;
	await new Promise<void>((resolve, reject) => {
		let completed = 0;
		const total = collection.length;

		function startNext() {
			while (running < concurrency && nextIdx < total) {
				const idx = nextIdx++;
				running++;
				(async () => {
					emitTrail(trail, feb.id, 'for_each_iteration', { index: idx });
					const iterCtx: ExecutionContext = {
						input: ctx.input,
						steps: { ...ctx.steps, __current: { output: collection[idx] } },
					};
					const sr = await executeAgentStep(
						`${feb.id}[${idx}]`, feb.agent!, feb.input, feb.config,
						feb.retry, feb.fallback, iterCtx,
					);
					if (sr.status === 'success') {
						iterResults[idx] = sr.output;
					} else {
						iterErrors[idx] = sr.error ?? 'unknown';
					}
					running--;
					completed++;
					if (completed === total) resolve();
					else startNext();
				})().catch(reject);
			}
		}
		if (total === 0) resolve();
		else startNext();
	});

	const hasError = iterErrors.some((e) => e !== null);
	const hasSuccess = iterErrors.some((e) => e === null);
	const feStatus = hasError && hasSuccess ? 'partial_failure' : hasError ? 'error' : 'success';

	results.push({ id: feb.id, agent: feb.agent, status: feStatus, output: iterResults, metrics: ZERO_METRICS });
	ctx.steps[feb.id] = { output: iterResults };

	if (feStatus === 'error') {
		const firstErr = iterErrors.find((e) => e !== null);
		throw new Error(`for_each '${feb.id}': ${firstErr}`);
	}

	return resolveNextTarget(feb.next, null);
}

// ── Next resolution ──

function resolveNextTarget(next: NextField | undefined, output: unknown): string {
	if (next === undefined || next === null) return '';

	// Simple string target
	if (typeof next === 'string') return next;

	// Switch
	if ('switch' in next) {
		const sn = next as SwitchNext;
		const outputMap = (output && typeof output === 'object') ? output as Record<string, unknown> : {};
		const val = resolveFieldPath(sn.switch, outputMap);
		const valStr = val != null ? String(val) : '';
		return sn.cases[valStr] ?? sn.default ?? '';
	}

	// If
	if ('if' in next) {
		const ifn = next as IfNext;
		return evaluateCondition(ifn.if, output) ? ifn.then : ifn.else;
	}

	return '';
}

// ── Condition evaluation ──

function evaluateCondition(condition: string, output: unknown): boolean {
	const outputMap = (output && typeof output === 'object') ? output as Record<string, unknown> : {};
	condition = condition.trim();

	if (condition.startsWith('!')) {
		return !evaluatePositive(condition.slice(1).trim(), outputMap);
	}

	if (condition.includes('==')) {
		const [left, right] = condition.split('==').map((s) => s.trim());
		const leftVal = resolveFieldPath(left, outputMap);
		const rightVal = right.replace(/^["']|["']$/g, '');
		return String(leftVal) === rightVal;
	}

	if (condition.includes('>=')) {
		const [left, right] = condition.split('>=').map((s) => s.trim());
		return toFloat(resolveFieldPath(left, outputMap)) >= toFloat(right);
	}

	if (condition.includes('>')) {
		const [left, right] = condition.split('>').map((s) => s.trim());
		return toFloat(resolveFieldPath(left, outputMap)) > toFloat(right);
	}

	return evaluatePositive(condition, outputMap);
}

function evaluatePositive(expr: string, outputMap: Record<string, unknown>): boolean {
	return isTruthy(resolveFieldPath(expr, outputMap));
}

function resolveFieldPath(path: string, outputMap: Record<string, unknown>): unknown {
	path = path.replace(/^output\./, '');
	let current: unknown = outputMap;
	for (const part of path.split('.')) {
		if (current == null || typeof current !== 'object') return undefined;
		current = (current as Record<string, unknown>)[part];
	}
	return current;
}

function isTruthy(val: unknown): boolean {
	if (val == null) return false;
	if (typeof val === 'boolean') return val;
	if (typeof val === 'string') return val !== '';
	if (typeof val === 'number') return val !== 0;
	return true;
}

function toFloat(v: unknown): number {
	const n = Number(v);
	return isNaN(n) ? 0 : n;
}

// ── Helpers ──

function getStepId(entry: WorkflowEntry): string {
	if ('parallel' in entry) return (entry as ParallelBlock).parallel.id;
	if ('loop' in entry) return (entry as LoopBlock).loop.id;
	if ('for_each' in entry) return (entry as ForEachBlock).for_each.id;
	return (entry as WorkflowStep).id;
}

function isParallelBlock(entry: WorkflowEntry): entry is ParallelBlock {
	return 'parallel' in entry;
}

function isLoopBlock(entry: WorkflowEntry): entry is LoopBlock {
	return 'loop' in entry;
}

function isForEachBlock(entry: WorkflowEntry): entry is ForEachBlock {
	return 'for_each' in entry;
}

function emitTrail(trail: TrailEntry[], stepId: string, event: string, data?: unknown): void {
	trail.push({ step_id: stepId, event, timestamp: new Date().toISOString(), data });
}

function markNotExecuted(entry: WorkflowEntry, results: StepResult[]): void {
	if (isParallelBlock(entry)) {
		for (const b of (entry as ParallelBlock).parallel.branches) {
			results.push({ id: b.id, agent: b.agent, status: 'not_executed', output: null, metrics: ZERO_METRICS });
		}
	} else {
		const id = getStepId(entry);
		const agent = 'agent' in entry ? (entry as WorkflowStep).agent : undefined;
		results.push({ id, agent, status: 'not_executed', output: null, metrics: ZERO_METRICS });
	}
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

function buildEnvelope(
	workflow: WorkflowDefinition,
	requestId: string,
	startedAt: string,
	startMs: number,
	steps: StepResult[],
	trail: TrailEntry[],
	result: unknown,
	status: 'success' | 'error' | 'partial_failure',
	error?: string,
): WorkflowEnvelope {
	let totalInput = 0;
	let totalOutput = 0;
	for (const s of steps) {
		totalInput += s.metrics.input_tokens;
		totalOutput += s.metrics.output_tokens;
	}

	return {
		workflow: workflow.name,
		version: workflow.version,
		request_id: requestId,
		status,
		timestamps: { started_at: startedAt, completed_at: new Date().toISOString() },
		metrics: {
			total_latency_ms: Math.round(performance.now() - startMs),
			total_input_tokens: totalInput,
			total_output_tokens: totalOutput,
		},
		steps,
		trail,
		result,
		error,
	};
}
