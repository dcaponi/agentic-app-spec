import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { zodResponseFormat } from 'openai/helpers/zod';
import type { z } from 'zod';
import type { StepMetrics, JsonSchemaObject } from './types.js';
import { createLogger, serializeError } from './logger.js';

const log = createLogger('llm');

// ── Schema registry ──

/**
 * Schema registry — maps schema names to either Zod schemas or JSON schema objects.
 * Users register schemas via registerSchema() before invoking agents that need them.
 * JSON schemas from schemas/*.json are loaded on-demand by the loader module.
 */
const schemaRegistry = new Map<string, z.ZodType | JsonSchemaObject>();

/** Register a schema (Zod or JSON Schema object) for use by LLM agents. */
export function registerSchema(name: string, schema: z.ZodType | JsonSchemaObject): void {
	schemaRegistry.set(name, schema);
	log.info(`Schema registered: ${name}`, {
		type: isZodSchema(schema) ? 'zod' : 'json_schema',
	});
}

/** Get a registered schema by name. Returns undefined if not found. */
export function getSchema(name: string): z.ZodType | JsonSchemaObject | undefined {
	return schemaRegistry.get(name);
}

/** Check whether a value is a Zod schema (has _def property). */
function isZodSchema(schema: unknown): schema is z.ZodType {
	return (
		schema !== null &&
		typeof schema === 'object' &&
		'_def' in (schema as Record<string, unknown>)
	);
}

// ── Provider detection ──

type Provider = 'openai' | 'anthropic';

function detectProvider(model: string, explicit?: string): Provider {
	if (explicit === 'openai' || explicit === 'anthropic') return explicit;
	if (model.startsWith('claude-')) return 'anthropic';
	return 'openai';
}

// ── Clients (lazily initialised) ──

let openaiClient: OpenAI | null = null;
let anthropicClient: Anthropic | null = null;

function getOpenAIClient(): OpenAI {
	if (!openaiClient) {
		const apiKey = process.env.OPENAI_API_KEY;
		if (!apiKey) {
			log.error('OPENAI_API_KEY is not set — OpenAI calls will fail', {
				env_keys: Object.keys(process.env).filter((k) => k.startsWith('OPENAI')),
			});
		} else {
			log.info('Initializing OpenAI client', {
				key_prefix: apiKey.slice(0, 8) + '...',
			});
		}
		openaiClient = new OpenAI();
	}
	return openaiClient;
}

function getAnthropicClient(): Anthropic {
	if (!anthropicClient) {
		const apiKey = process.env.ANTHROPIC_API_KEY;
		if (!apiKey) {
			log.error('ANTHROPIC_API_KEY is not set — Anthropic calls will fail', {
				env_keys: Object.keys(process.env).filter((k) => k.startsWith('ANTHROPIC')),
			});
		} else {
			log.info('Initializing Anthropic client', {
				key_prefix: apiKey.slice(0, 8) + '...',
			});
		}
		anthropicClient = new Anthropic();
	}
	return anthropicClient;
}

// ── Public types ──

export interface LLMCallOptions {
	model: string;
	systemPrompt: string;
	userContent:
		| string
		| Array<{ type: string; text?: string; image_url?: { url: string; detail?: string } }>;
	temperature: number;
	schemaName?: string | null;
	provider?: string;
}

export interface LLMResult {
	output: unknown;
	metrics: StepMetrics;
}

// ── LLM call (provider router) ──

export async function callLLM(options: LLMCallOptions): Promise<LLMResult> {
	const provider = detectProvider(options.model, options.provider);

	log.info('API call starting', {
		provider,
		model: options.model,
		schema: options.schemaName ?? 'json_mode',
		temperature: options.temperature,
		system_prompt_length: options.systemPrompt.length,
	});

	if (provider === 'anthropic') {
		return callAnthropic(options);
	}
	return callOpenAI(options);
}

// ══════════════════════════════════════════════════════════════════════════════
// OpenAI implementation
// ══════════════════════════════════════════════════════════════════════════════

async function callOpenAI(options: LLMCallOptions): Promise<LLMResult> {
	const api = getOpenAIClient();
	const start = performance.now();

	const messages: OpenAI.ChatCompletionMessageParam[] = [
		{ role: 'system', content: options.systemPrompt },
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		{ role: 'user', content: options.userContent as any },
	];

	const schema = options.schemaName ? schemaRegistry.get(options.schemaName) : null;

	if (schema && options.schemaName) {
		if (isZodSchema(schema)) {
			return callOpenAIWithZodSchema(api, messages, options, schema, start);
		} else {
			return callOpenAIWithJsonSchema(api, messages, options, schema as JsonSchemaObject, start);
		}
	}

	// No schema — JSON mode fallback
	return callOpenAIJsonMode(api, messages, options, start);
}

async function callOpenAIWithZodSchema(
	api: OpenAI,
	messages: OpenAI.ChatCompletionMessageParam[],
	options: LLMCallOptions,
	schema: z.ZodType,
	start: number
): Promise<LLMResult> {
	try {
		const response = await api.beta.chat.completions.parse({
			model: options.model,
			messages,
			response_format: zodResponseFormat(schema, options.schemaName!),
			temperature: options.temperature,
		});

		const latency_ms = performance.now() - start;
		const parsed = response.choices[0]?.message?.parsed;
		const refusal = response.choices[0]?.message?.refusal;

		if (refusal) {
			log.error('Model refused the request', { model: options.model, schema: options.schemaName, refusal });
			throw new Error(`Model refused request: ${refusal}`);
		}

		if (!parsed) {
			log.error('Structured output parse returned null', {
				model: options.model,
				schema: options.schemaName,
				finish_reason: response.choices[0]?.finish_reason,
				raw_content: response.choices[0]?.message?.content?.slice(0, 500),
			});
			throw new Error(
				`Structured output parse failed for ${options.schemaName}. finish_reason: ${response.choices[0]?.finish_reason}`
			);
		}

		const metrics: StepMetrics = {
			latency_ms,
			input_tokens: response.usage?.prompt_tokens ?? 0,
			output_tokens: response.usage?.completion_tokens ?? 0,
		};

		log.info('API call complete (openai/structured/zod)', {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
		});

		return { output: parsed, metrics };
	} catch (err) {
		const latency_ms = performance.now() - start;
		const se = serializeError(err);
		log.error('API call failed (openai/structured/zod)', {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			error_name: se.name,
			error_message: se.message,
			error_status: se.status,
			error_code: se.code,
			error_type: se.type,
		});
		throw err;
	}
}

async function callOpenAIWithJsonSchema(
	api: OpenAI,
	messages: OpenAI.ChatCompletionMessageParam[],
	options: LLMCallOptions,
	schema: JsonSchemaObject,
	start: number
): Promise<LLMResult> {
	try {
		const response = await api.chat.completions.create({
			model: options.model,
			messages,
			response_format: {
				type: 'json_schema',
				json_schema: {
					name: options.schemaName!,
					strict: true,
					schema,
				},
			},
			temperature: options.temperature,
		});

		const latency_ms = performance.now() - start;
		const rawContent = response.choices[0]?.message?.content ?? '{}';

		let output: unknown;
		try {
			output = JSON.parse(rawContent);
		} catch (parseErr) {
			log.error('JSON parse failed on model response (openai/json_schema mode)', {
				model: options.model,
				schema: options.schemaName,
				raw_content: rawContent.slice(0, 500),
				parse_error: serializeError(parseErr).message,
			});
			throw new Error(`Failed to parse model JSON response: ${rawContent.slice(0, 200)}`);
		}

		const metrics: StepMetrics = {
			latency_ms,
			input_tokens: response.usage?.prompt_tokens ?? 0,
			output_tokens: response.usage?.completion_tokens ?? 0,
		};

		log.info('API call complete (openai/structured/json_schema)', {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
		});

		return { output, metrics };
	} catch (err) {
		const latency_ms = performance.now() - start;
		const se = serializeError(err);
		log.error('API call failed (openai/structured/json_schema)', {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			error_name: se.name,
			error_message: se.message,
			error_status: se.status,
			error_code: se.code,
			error_type: se.type,
		});
		throw err;
	}
}

async function callOpenAIJsonMode(
	api: OpenAI,
	messages: OpenAI.ChatCompletionMessageParam[],
	options: LLMCallOptions,
	start: number
): Promise<LLMResult> {
	try {
		const response = await api.chat.completions.create({
			model: options.model,
			messages,
			response_format: { type: 'json_object' },
			temperature: options.temperature,
		});

		const latency_ms = performance.now() - start;
		const rawContent = response.choices[0]?.message?.content ?? '{}';

		let output: unknown;
		try {
			output = JSON.parse(rawContent);
		} catch (parseErr) {
			log.error('JSON parse failed on model response (openai/json_mode)', {
				model: options.model,
				raw_content: rawContent.slice(0, 500),
				parse_error: serializeError(parseErr).message,
			});
			throw new Error(`Failed to parse model JSON response: ${rawContent.slice(0, 200)}`);
		}

		const metrics: StepMetrics = {
			latency_ms,
			input_tokens: response.usage?.prompt_tokens ?? 0,
			output_tokens: response.usage?.completion_tokens ?? 0,
		};

		log.info('API call complete (openai/json_mode)', {
			model: options.model,
			latency_ms: Math.round(latency_ms),
			tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
		});

		return { output, metrics };
	} catch (err) {
		const latency_ms = performance.now() - start;
		const se = serializeError(err);
		log.error('API call failed (openai/json_mode)', {
			model: options.model,
			latency_ms: Math.round(latency_ms),
			error_name: se.name,
			error_message: se.message,
			error_status: se.status,
			error_code: se.code,
			error_type: se.type,
		});
		throw err;
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// Anthropic implementation
// ══════════════════════════════════════════════════════════════════════════════

/**
 * Build the system prompt for Anthropic calls.  When a schema is provided,
 * append JSON-schema instructions so the model returns structured output.
 */
function buildAnthropicSystemPrompt(base: string, schemaName?: string | null): string {
	if (!schemaName) {
		return base + '\n\nYou must respond with a valid JSON object. Output only the JSON with no additional text, markdown, or code fences.';
	}

	const schema = schemaRegistry.get(schemaName);
	if (!schema) {
		return base + '\n\nYou must respond with a valid JSON object. Output only the JSON with no additional text, markdown, or code fences.';
	}

	let schemaJson: string;
	if (isZodSchema(schema)) {
		// Extract JSON Schema via OpenAI's zodResponseFormat helper
		const format = zodResponseFormat(schema, schemaName);
		schemaJson = JSON.stringify(format.json_schema.schema, null, 2);
	} else {
		schemaJson = JSON.stringify(schema, null, 2);
	}

	return `${base}\n\nYou must respond with a valid JSON object that conforms to the following JSON schema:\n${schemaJson}\n\nOutput only the JSON with no additional text, markdown, or code fences.`;
}

/**
 * Convert user content to Anthropic message format.
 * Anthropic uses a different content block structure than OpenAI.
 */
function toAnthropicUserContent(
	userContent: LLMCallOptions['userContent']
): string | Anthropic.ContentBlockParam[] {
	if (typeof userContent === 'string') {
		return userContent;
	}

	// Convert OpenAI-style multipart to Anthropic content blocks
	return userContent.map((part): Anthropic.ContentBlockParam => {
		if (part.type === 'text') {
			return { type: 'text', text: part.text ?? '' };
		}
		if (part.type === 'image_url' && part.image_url) {
			const url = part.image_url.url;
			// Anthropic expects base64 source for inline images
			if (url.startsWith('data:')) {
				const match = url.match(/^data:(image\/\w+);base64,(.+)$/);
				if (match) {
					return {
						type: 'image',
						source: {
							type: 'base64',
							media_type: match[1] as 'image/jpeg' | 'image/png' | 'image/gif' | 'image/webp',
							data: match[2],
						},
					};
				}
			}
			// URL-based images
			return {
				type: 'image',
				source: { type: 'url', url },
			};
		}
		return { type: 'text', text: JSON.stringify(part) };
	});
}

async function callAnthropic(options: LLMCallOptions): Promise<LLMResult> {
	const client = getAnthropicClient();
	const start = performance.now();

	const systemPrompt = buildAnthropicSystemPrompt(options.systemPrompt, options.schemaName);
	const userContent = toAnthropicUserContent(options.userContent);

	try {
		const response = await client.messages.create({
			model: options.model,
			max_tokens: 4096,
			system: systemPrompt,
			messages: [{ role: 'user', content: userContent }],
			temperature: options.temperature,
		});

		const latency_ms = performance.now() - start;

		// Extract text from content blocks
		const textBlock = response.content.find(
			(block): block is Anthropic.TextBlock => block.type === 'text'
		);
		const rawContent = textBlock?.text ?? '{}';

		let output: unknown;
		try {
			output = JSON.parse(rawContent);
		} catch (parseErr) {
			log.error('JSON parse failed on Anthropic response', {
				model: options.model,
				schema: options.schemaName,
				raw_content: rawContent.slice(0, 500),
				parse_error: serializeError(parseErr).message,
			});
			throw new Error(`Failed to parse Anthropic JSON response: ${rawContent.slice(0, 200)}`);
		}

		const metrics: StepMetrics = {
			latency_ms,
			input_tokens: response.usage?.input_tokens ?? 0,
			output_tokens: response.usage?.output_tokens ?? 0,
		};

		log.info(`API call complete (anthropic${options.schemaName ? '/structured' : '/json_mode'})`, {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
		});

		return { output, metrics };
	} catch (err) {
		const latency_ms = performance.now() - start;
		const se = serializeError(err);
		log.error('API call failed (anthropic)', {
			model: options.model,
			schema: options.schemaName,
			latency_ms: Math.round(latency_ms),
			error_name: se.name,
			error_message: se.message,
			error_status: se.status,
			error_code: se.code,
			error_type: se.type,
		});
		throw err;
	}
}
