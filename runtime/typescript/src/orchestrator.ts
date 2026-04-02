import type {
	WorkflowDefinition,
	WorkflowStep,
	WorkflowEntry,
	ParallelGroup,
	RouteEntry,
	RouteBlock,
	RouteTarget,
	RouteOutput,
	AgentDefinition,
	RouterDefinition,
	ExecutionContext,
	WorkflowEnvelope,
	StepResult,
	StepMetrics,
} from './types.js';
import { loadWorkflow, loadAllAgents, loadAgent, loadRouter } from './loader.js';
import { executeAgent } from './runner.js';
import { resolveInputs, resolveOutputs } from './resolver.js';
import { createLogger, serializeError } from './logger.js';

const log = createLogger('orchestrator');

const ZERO_METRICS: StepMetrics = { latency_ms: 0, input_tokens: 0, output_tokens: 0 };

// ── Public API (called by generated workflow stubs) ──

export async function orchestrate(
	workflowName: string,
	input: Record<string, unknown>
): Promise<WorkflowEnvelope> {
	log.info(`orchestrate("${workflowName}") called`, {
		input_keys: Object.keys(input),
		input_value_types: Object.fromEntries(
			Object.entries(input).map(([k, v]) => [
				k,
				v === null
					? 'null'
					: v === undefined
						? 'undefined'
						: typeof v === 'string'
							? `string(${v.length})`
							: typeof v,
			])
		),
	});

	let workflow: WorkflowDefinition;
	try {
		workflow = loadWorkflow(workflowName);
	} catch (err) {
		log.error(`Failed to load workflow: ${workflowName}`, { error: serializeError(err).message });
		throw err;
	}

	let agents: Map<string, AgentDefinition>;
	try {
		agents = loadAllAgents();
	} catch (err) {
		log.error('Failed to load agents', { error: serializeError(err).message });
		throw err;
	}

	log.info(`Starting workflow: ${workflow.name} v${workflow.version}`, {
		steps: workflow.steps.length,
		agents_loaded: [...agents.keys()],
	});

	return executeWorkflow(workflow, input, agents);
}

// ── Core orchestration engine ──

async function executeWorkflow(
	workflow: WorkflowDefinition,
	input: Record<string, unknown>,
	agents: Map<string, AgentDefinition>
): Promise<WorkflowEnvelope> {
	const requestId = crypto.randomUUID();
	const startedAt = new Date().toISOString();
	const stepResults: StepResult[] = [];
	const context: ExecutionContext = { input, steps: {} };

	let shortCircuited = false;
	let shortCircuitDefaults: Record<string, unknown> | undefined;

	try {
		for (const entry of workflow.steps) {
			if (isParallelGroup(entry)) {
				if (shortCircuited) {
					for (const step of entry.parallel) {
						fillSkipped(step, shortCircuitDefaults, context, stepResults);
					}
					continue;
				}

				log.info('Executing parallel group', {
					steps: entry.parallel.map((s) => `${s.id}(${s.agent})`),
				});

				const results = await Promise.all(
					entry.parallel.map((step) => executeStepWithRetry(step, context, agents))
				);

				for (let i = 0; i < entry.parallel.length; i++) {
					const step = entry.parallel[i];
					context.steps[step.id] = { output: results[i].output };
					stepResults.push(results[i]);

					if (
						step.short_circuit &&
						evaluateCondition(step.short_circuit.condition, results[i].output)
					) {
						shortCircuited = true;
						shortCircuitDefaults = step.short_circuit.defaults;
						log.info(`Short-circuit triggered by parallel step: ${step.id}`, {
							condition: step.short_circuit.condition,
						});
					}
				}
			} else if (isRouteEntry(entry)) {
				if (shortCircuited) {
					fillRouteSkipped(entry.route, shortCircuitDefaults, context, stepResults);
					continue;
				}

				const routeResult = await executeRoute(entry.route, context, agents);
				context.steps[entry.route.id] = { output: routeResult.output };
				stepResults.push(routeResult);

				if (routeResult.status === 'short_circuited') {
					shortCircuited = true;
					const noneTarget = entry.route.routes['_none'];
					shortCircuitDefaults = (noneTarget && typeof noneTarget === 'object' && 'defaults' in noneTarget)
						? (noneTarget as { defaults: Record<string, unknown> }).defaults
						: {};
					log.info(`Short-circuit triggered by route: ${entry.route.id}`);
				}
			} else {
				if (shortCircuited) {
					fillSkipped(entry, shortCircuitDefaults, context, stepResults);
					continue;
				}

				const result = await executeStepWithRetry(entry, context, agents);
				context.steps[entry.id] = { output: result.output };
				stepResults.push(result);

				if (
					entry.short_circuit &&
					evaluateCondition(entry.short_circuit.condition, result.output)
				) {
					shortCircuited = true;
					shortCircuitDefaults = entry.short_circuit.defaults;
					log.info(`Short-circuit triggered by step: ${entry.id}`, {
						condition: entry.short_circuit.condition,
					});
				}
			}
		}

		const result = resolveOutputs(workflow.output, context);
		const envelope = buildEnvelope(
			workflow,
			requestId,
			startedAt,
			stepResults,
			result,
			shortCircuited ? 'short_circuited' : 'success'
		);

		log.info(`Workflow complete: ${workflow.name}`, {
			status: envelope.status,
			total_latency_ms: envelope.metrics.total_latency_ms,
			steps_executed: envelope.metrics.steps_executed,
			steps_skipped: envelope.metrics.steps_skipped,
			total_tokens: envelope.metrics.total_input_tokens + envelope.metrics.total_output_tokens,
		});

		return envelope;
	} catch (err) {
		const se = serializeError(err);
		log.error(`Workflow failed: ${workflow.name}`, {
			error: se.message,
			error_name: se.name,
			steps_completed: stepResults.length,
			steps_completed_ids: stepResults.map((s) => `${s.id}:${s.status}`),
		});
		return buildEnvelope(workflow, requestId, startedAt, stepResults, null, 'error', se.message);
	}
}

// ── Step execution with retry + fallback ──

async function executeStepWithRetry(
	step: WorkflowStep,
	context: ExecutionContext,
	agents: Map<string, AgentDefinition>
): Promise<StepResult> {
	const maxAttempts = step.retry?.max_attempts ?? 1;
	const backoffMs = step.retry?.backoff_ms ?? 0;

	log.info(`Step ${step.id}: starting`, {
		agent: step.agent,
		max_attempts: maxAttempts,
		has_fallback: !!step.fallback,
		input_bindings: step.input,
	});

	const agentDef = agents.get(step.agent);
	if (!agentDef) {
		log.error(`Step ${step.id}: agent not found: ${step.agent}`, {
			available_agents: [...agents.keys()],
		});
		throw new Error(
			`Agent not found: ${step.agent}. Available: ${[...agents.keys()].join(', ')}`
		);
	}

	const mergedAgent = step.config ? { ...agentDef, ...step.config } : agentDef;
	const resolvedInput = resolveInputs(step.input, context);

	// Check for undefined inputs that will likely cause downstream failures
	const undefinedInputs = Object.entries(resolvedInput)
		.filter(([, v]) => v === undefined)
		.map(([k]) => k);
	if (undefinedInputs.length > 0) {
		log.warn(`Step ${step.id}: resolved inputs contain undefined values`, {
			undefined_keys: undefinedInputs,
			all_input_keys: Object.keys(resolvedInput),
			bindings: step.input,
		});
	}

	let lastError: unknown;

	// Primary attempts
	for (let attempt = 1; attempt <= maxAttempts; attempt++) {
		log.info(`Step ${step.id}: attempt ${attempt}/${maxAttempts}`, {
			agent: step.agent,
			model: mergedAgent.model,
		});

		try {
			const result = await executeAgent(resolvedInput, mergedAgent);
			log.info(`Step ${step.id}: success (attempt ${attempt})`, {
				latency_ms: Math.round(result.metrics.latency_ms),
				output_type:
					result.output && typeof result.output === 'object'
						? `{${Object.keys(result.output as Record<string, unknown>).join(', ')}}`
						: typeof result.output,
			});
			return {
				id: step.id,
				agent: step.agent,
				status: 'success',
				output: result.output,
				metrics: result.metrics,
				attempts: attempt,
				used_fallback: false,
			};
		} catch (err) {
			lastError = err;
			const se = serializeError(err);
			log.error(`Step ${step.id}: attempt ${attempt}/${maxAttempts} failed`, {
				agent: step.agent,
				error: se.message,
				error_name: se.name,
				error_status: se.status,
				error_code: se.code,
			});
			if (attempt < maxAttempts) {
				const waitMs = backoffMs * attempt;
				log.info(`Step ${step.id}: waiting ${waitMs}ms before retry`);
				await sleep(waitMs);
			}
		}
	}

	// Fallback
	if (step.fallback) {
		log.info(`Step ${step.id}: all ${maxAttempts} attempts failed, trying fallback`, {
			fallback_agent: step.fallback.agent,
			fallback_config: step.fallback.config,
		});

		let fallbackAgentDef: AgentDefinition;
		try {
			fallbackAgentDef = loadAgent(step.fallback.agent);
		} catch (err) {
			log.error(`Step ${step.id}: failed to load fallback agent: ${step.fallback.agent}`, {
				error: serializeError(err).message,
			});
			return makeErrorResult(step, lastError, maxAttempts + 1, true);
		}

		const fallbackMerged = step.fallback.config
			? { ...fallbackAgentDef, ...step.fallback.config }
			: fallbackAgentDef;

		try {
			const result = await executeAgent(resolvedInput, fallbackMerged);
			log.info(`Step ${step.id}: fallback succeeded`, {
				fallback_agent: step.fallback.agent,
				latency_ms: Math.round(result.metrics.latency_ms),
			});
			return {
				id: step.id,
				agent: step.fallback.agent,
				status: 'success',
				output: result.output,
				metrics: result.metrics,
				attempts: maxAttempts + 1,
				used_fallback: true,
			};
		} catch (err) {
			lastError = err;
			log.error(`Step ${step.id}: fallback also failed`, {
				fallback_agent: step.fallback.agent,
				error: serializeError(err).message,
			});
		}
	}

	// All attempts exhausted
	log.error(`Step ${step.id}: all attempts exhausted`, {
		total_attempts: maxAttempts + (step.fallback ? 1 : 0),
		final_error: serializeError(lastError).message,
	});
	return makeErrorResult(step, lastError, maxAttempts + (step.fallback ? 1 : 0), !!step.fallback);
}

function makeErrorResult(
	step: WorkflowStep,
	err: unknown,
	attempts: number,
	usedFallback: boolean
): StepResult {
	return {
		id: step.id,
		agent: step.agent,
		status: 'error',
		output: null,
		metrics: ZERO_METRICS,
		attempts,
		used_fallback: usedFallback,
		error: serializeError(err).message,
	};
}

// ── Helpers ──

function isParallelGroup(entry: WorkflowEntry): entry is ParallelGroup {
	return 'parallel' in entry;
}

function isRouteEntry(entry: WorkflowEntry): entry is RouteEntry {
	return 'route' in entry;
}

function evaluateCondition(condition: string, output: unknown): boolean {
	try {
		const fn = new Function('output', `"use strict"; return (${condition});`);
		return !!fn(output);
	} catch (err) {
		log.warn(`Short-circuit condition evaluation failed: "${condition}"`, {
			error: serializeError(err).message,
			output_type: typeof output,
		});
		return false;
	}
}

function fillSkipped(
	step: WorkflowStep,
	defaults: Record<string, unknown> | undefined,
	context: ExecutionContext,
	results: StepResult[]
): void {
	const defaultOutput = defaults?.[step.id] ?? null;
	log.info(`Step ${step.id}: skipped (short-circuited)`, {
		has_default: defaultOutput !== null,
	});
	context.steps[step.id] = { output: defaultOutput };
	results.push({
		id: step.id,
		agent: step.agent,
		status: 'skipped',
		output: defaultOutput,
		metrics: ZERO_METRICS,
	});
}

function fillRouteSkipped(
	routeBlock: RouteBlock,
	defaults: Record<string, unknown> | undefined,
	context: ExecutionContext,
	results: StepResult[]
): void {
	const defaultOutput = defaults?.[routeBlock.id] ?? null;
	log.info(`Route ${routeBlock.id}: skipped (short-circuited)`, {
		has_default: defaultOutput !== null,
	});
	context.steps[routeBlock.id] = { output: defaultOutput };
	results.push({
		id: routeBlock.id,
		agent: `router:${routeBlock.router}`,
		status: 'skipped',
		output: defaultOutput,
		metrics: ZERO_METRICS,
	});
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

function buildEnvelope(
	workflow: WorkflowDefinition,
	requestId: string,
	startedAt: string,
	steps: StepResult[],
	result: unknown,
	status: 'success' | 'error' | 'short_circuited',
	error?: string
): WorkflowEnvelope {
	let totalLatency = 0;
	let totalInput = 0;
	let totalOutput = 0;
	let executed = 0;
	let skipped = 0;
	for (const s of steps) {
		totalLatency += s.metrics.latency_ms;
		totalInput += s.metrics.input_tokens;
		totalOutput += s.metrics.output_tokens;
		if (s.status === 'success') executed++;
		if (s.status === 'skipped') skipped++;
	}

	return {
		workflow: workflow.name,
		version: workflow.version,
		request_id: requestId,
		status,
		timestamps: { started_at: startedAt, completed_at: new Date().toISOString() },
		metrics: {
			total_latency_ms: Math.round(totalLatency),
			total_input_tokens: totalInput,
			total_output_tokens: totalOutput,
			steps_executed: executed,
			steps_skipped: skipped,
		},
		steps,
		result,
		error,
	};
}

// ── Route execution engine ──

async function executeRoute(
	routeBlock: RouteBlock,
	context: ExecutionContext,
	agents: Map<string, AgentDefinition>
): Promise<StepResult> {
	const maxAttempts = routeBlock.retry?.max_attempts ?? 1;
	const backoffMs = routeBlock.retry?.backoff_ms ?? 0;

	log.info(`Route ${routeBlock.id}: starting`, {
		router: routeBlock.router,
		route_keys: Object.keys(routeBlock.routes),
		max_attempts: maxAttempts,
		has_fallback: !!routeBlock.fallback,
	});

	const routerDef = loadRouter(routeBlock.router);
	const resolvedInput = resolveInputs(routeBlock.input, context);
	const routeKeys = Object.keys(routeBlock.routes).filter((k) => k !== '_none');

	let lastError: unknown;

	for (let attempt = 1; attempt <= maxAttempts; attempt++) {
		log.info(`Route ${routeBlock.id}: decision attempt ${attempt}/${maxAttempts}`);

		try {
			const routerOutput = await executeRouterDecision(routerDef, resolvedInput, routeKeys);
			const chosenKey = (routerOutput as Record<string, unknown>).route as string;

			log.info(`Route ${routeBlock.id}: chose "${chosenKey}"`, { attempt });

			if (chosenKey !== '_none' && !routeBlock.routes[chosenKey]) {
				throw new Error(
					`Router "${routeBlock.router}" returned invalid route key "${chosenKey}". ` +
					`Valid keys: ${[...routeKeys, '_none'].join(', ')}`
				);
			}

			const target = routeBlock.routes[chosenKey] ?? routeBlock.routes['_none'];

			// Handle _none / short_circuit
			if (chosenKey === '_none' || (target && typeof target === 'object' && 'short_circuit' in target && target.short_circuit)) {
				const output: RouteOutput = {
					route: '_none',
					router_output: routerOutput as Record<string, unknown>,
					result: null,
				};
				return {
					id: routeBlock.id,
					agent: `router:${routeBlock.router}`,
					status: 'short_circuited',
					output,
					metrics: ZERO_METRICS,
					attempts: attempt,
				};
			}

			// Dispatch to target
			const dispatchResult = await dispatchRouteTarget(
				target, resolvedInput, routeBlock, context, agents
			);

			const output: RouteOutput = {
				route: chosenKey,
				router_output: routerOutput as Record<string, unknown>,
				result: dispatchResult.output,
			};

			return {
				id: routeBlock.id,
				agent: `router:${routeBlock.router}`,
				status: 'success',
				output,
				metrics: dispatchResult.metrics,
				attempts: attempt,
			};
		} catch (err) {
			lastError = err;
			log.error(`Route ${routeBlock.id}: attempt ${attempt}/${maxAttempts} failed`, {
				error: serializeError(err).message,
			});
			if (attempt < maxAttempts) {
				const waitMs = backoffMs * attempt;
				log.info(`Route ${routeBlock.id}: waiting ${waitMs}ms before retry`);
				await sleep(waitMs);
			}
		}
	}

	// Fallback router
	if (routeBlock.fallback) {
		log.info(`Route ${routeBlock.id}: trying fallback router`, {
			fallback_router: routeBlock.fallback.router,
		});

		try {
			const fallbackDef = loadRouter(routeBlock.fallback.router);
			const mergedFallback = routeBlock.fallback.config
				? { ...fallbackDef, ...routeBlock.fallback.config }
				: fallbackDef;
			const routerOutput = await executeRouterDecision(
				mergedFallback as RouterDefinition, resolvedInput, routeKeys
			);
			const chosenKey = (routerOutput as Record<string, unknown>).route as string;

			if (chosenKey !== '_none' && !routeBlock.routes[chosenKey]) {
				throw new Error(`Fallback router returned invalid key "${chosenKey}"`);
			}

			const target = routeBlock.routes[chosenKey];
			if (chosenKey === '_none' || (target && typeof target === 'object' && 'short_circuit' in target)) {
				const output: RouteOutput = {
					route: '_none',
					router_output: routerOutput as Record<string, unknown>,
					result: null,
				};
				return {
					id: routeBlock.id,
					agent: `router:${routeBlock.fallback.router}`,
					status: 'short_circuited',
					output,
					metrics: ZERO_METRICS,
					attempts: maxAttempts + 1,
					used_fallback: true,
				};
			}

			const dispatchResult = await dispatchRouteTarget(
				target, resolvedInput, routeBlock, context, agents
			);
			const output: RouteOutput = {
				route: chosenKey,
				router_output: routerOutput as Record<string, unknown>,
				result: dispatchResult.output,
			};
			return {
				id: routeBlock.id,
				agent: `router:${routeBlock.fallback.router}`,
				status: 'success',
				output,
				metrics: dispatchResult.metrics,
				attempts: maxAttempts + 1,
				used_fallback: true,
			};
		} catch (err) {
			lastError = err;
			log.error(`Route ${routeBlock.id}: fallback also failed`, {
				error: serializeError(err).message,
			});
		}
	}

	return {
		id: routeBlock.id,
		agent: `router:${routeBlock.router}`,
		status: 'error',
		output: null,
		metrics: ZERO_METRICS,
		attempts: maxAttempts + (routeBlock.fallback ? 1 : 0),
		used_fallback: !!routeBlock.fallback,
		error: serializeError(lastError).message,
	};
}

async function executeRouterDecision(
	routerDef: RouterDefinition,
	resolvedInput: Record<string, unknown>,
	routeKeys: string[]
): Promise<unknown> {
	if (routerDef.strategy === 'deterministic') {
		const agentCompat: AgentDefinition = {
			name: routerDef.name,
			description: routerDef.description,
			type: 'deterministic',
			handler: routerDef.handler,
		};
		const result = await executeAgent(resolvedInput, agentCompat);
		return result.output;
	}

	// LLM strategy
	const { callLLM } = await import('./llm.js');
	const inputSummary = Object.entries(resolvedInput)
		.map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
		.join('\n');

	const userMessage =
		`${inputSummary}\n\n` +
		`You must choose exactly one of the following routes: ${routeKeys.join(', ')}\n` +
		`If none of the routes apply, choose: _none\n` +
		`Respond with a JSON object: { "route": "<chosen_key>" }`;

	const result = await callLLM({
		model: routerDef.model ?? 'gpt-4.1-mini',
		systemPrompt: routerDef.prompt ?? '',
		userContent: userMessage,
		temperature: routerDef.temperature ?? 0,
		schemaName: null,
		provider: routerDef.provider,
	});

	return result.output;
}

async function dispatchRouteTarget(
	target: RouteTarget,
	passThrough: Record<string, unknown>,
	routeBlock: RouteBlock,
	context: ExecutionContext,
	agents: Map<string, AgentDefinition>
): Promise<{ output: unknown; metrics: StepMetrics }> {
	// String target — agent ID with pass-through input
	if (typeof target === 'string') {
		const agentDef = agents.get(target) ?? loadAgent(target);
		const result = await executeAgent(passThrough, agentDef);
		return result;
	}

	// Nested route block
	if ('route' in target) {
		const nestedResult = await executeRoute(
			(target as { route: RouteBlock }).route, context, agents
		);
		return { output: nestedResult.output, metrics: nestedResult.metrics };
	}

	// Agent with explicit input
	if ('agent' in target) {
		const agentTarget = target as { agent: string; input?: Record<string, string> };
		const agentDef = agents.get(agentTarget.agent) ?? loadAgent(agentTarget.agent);
		const agentInput = agentTarget.input
			? resolveInputs(agentTarget.input, context)
			: passThrough;
		const result = await executeAgent(agentInput, agentDef);
		return result;
	}

	// Workflow target
	if ('workflow' in target) {
		const wfTarget = target as { workflow: string; input?: Record<string, string> };
		const wfInput = wfTarget.input
			? resolveInputs(wfTarget.input, context)
			: passThrough;
		const envelope = await orchestrate(wfTarget.workflow, wfInput);
		return {
			output: envelope.result,
			metrics: {
				latency_ms: envelope.metrics.total_latency_ms,
				input_tokens: envelope.metrics.total_input_tokens,
				output_tokens: envelope.metrics.total_output_tokens,
			},
		};
	}

	throw new Error(`Unknown route target type in route ${routeBlock.id}: ${JSON.stringify(target)}`);
}
