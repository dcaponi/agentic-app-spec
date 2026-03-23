import OpenAI from 'openai';
import { zodResponseFormat } from 'openai/helpers/zod';
import { createLogger, serializeError } from './logger.js';
const log = createLogger('llm');
// ── Schema registry ──
/**
 * Schema registry — maps schema names to either Zod schemas or JSON schema objects.
 * Users register schemas via registerSchema() before invoking agents that need them.
 * JSON schemas from schemas/*.json are loaded on-demand by the loader module.
 */
const schemaRegistry = new Map();
/** Register a schema (Zod or JSON Schema object) for use by LLM agents. */
export function registerSchema(name, schema) {
    schemaRegistry.set(name, schema);
    log.info(`Schema registered: ${name}`, {
        type: isZodSchema(schema) ? 'zod' : 'json_schema',
    });
}
/** Get a registered schema by name. Returns undefined if not found. */
export function getSchema(name) {
    return schemaRegistry.get(name);
}
/** Check whether a value is a Zod schema (has _def property). */
function isZodSchema(schema) {
    return (schema !== null &&
        typeof schema === 'object' &&
        '_def' in schema);
}
// ── OpenAI client ──
let client = null;
function getClient() {
    if (!client) {
        const apiKey = process.env.OPENAI_API_KEY;
        if (!apiKey) {
            log.error('OPENAI_API_KEY is not set — all LLM calls will fail', {
                env_keys: Object.keys(process.env).filter((k) => k.startsWith('OPENAI')),
            });
        }
        else {
            log.info('Initializing OpenAI client', {
                key_prefix: apiKey.slice(0, 8) + '...',
            });
        }
        client = new OpenAI();
    }
    return client;
}
// ── LLM call ──
export async function callLLM(options) {
    const api = getClient();
    const start = performance.now();
    const userContentSummary = typeof options.userContent === 'string'
        ? { type: 'text', length: options.userContent.length }
        : { type: 'multipart', parts: options.userContent.map((p) => p.type) };
    log.info('API call starting', {
        model: options.model,
        schema: options.schemaName ?? 'json_mode',
        temperature: options.temperature,
        system_prompt_length: options.systemPrompt.length,
        user_content: userContentSummary,
    });
    const messages = [
        { role: 'system', content: options.systemPrompt },
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        { role: 'user', content: options.userContent },
    ];
    const schema = options.schemaName ? schemaRegistry.get(options.schemaName) : null;
    if (schema && options.schemaName) {
        if (isZodSchema(schema)) {
            return callWithZodSchema(api, messages, options, schema, start);
        }
        else {
            return callWithJsonSchema(api, messages, options, schema, start);
        }
    }
    // No schema — JSON mode fallback
    return callJsonMode(api, messages, options, start);
}
// ── Zod structured output via beta.chat.completions.parse() ──
async function callWithZodSchema(api, messages, options, schema, start) {
    try {
        const response = await api.beta.chat.completions.parse({
            model: options.model,
            messages,
            response_format: zodResponseFormat(schema, options.schemaName),
            temperature: options.temperature,
        });
        const latency_ms = performance.now() - start;
        const parsed = response.choices[0]?.message?.parsed;
        const refusal = response.choices[0]?.message?.refusal;
        if (refusal) {
            log.error('Model refused the request', {
                model: options.model,
                schema: options.schemaName,
                refusal,
            });
            throw new Error(`Model refused request: ${refusal}`);
        }
        if (!parsed) {
            log.error('Structured output parse returned null', {
                model: options.model,
                schema: options.schemaName,
                finish_reason: response.choices[0]?.finish_reason,
                raw_content: response.choices[0]?.message?.content?.slice(0, 500),
            });
            throw new Error(`Structured output parse failed for ${options.schemaName}. finish_reason: ${response.choices[0]?.finish_reason}`);
        }
        const metrics = {
            latency_ms,
            input_tokens: response.usage?.prompt_tokens ?? 0,
            output_tokens: response.usage?.completion_tokens ?? 0,
        };
        log.info('API call complete (structured/zod)', {
            model: options.model,
            schema: options.schemaName,
            latency_ms: Math.round(latency_ms),
            tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
            output_keys: parsed && typeof parsed === 'object' ? Object.keys(parsed) : [],
        });
        return { output: parsed, metrics };
    }
    catch (err) {
        const latency_ms = performance.now() - start;
        const se = serializeError(err);
        log.error('API call failed (structured/zod)', {
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
// ── JSON Schema structured output via response_format.json_schema ──
async function callWithJsonSchema(api, messages, options, schema, start) {
    try {
        const response = await api.chat.completions.create({
            model: options.model,
            messages,
            response_format: {
                type: 'json_schema',
                json_schema: {
                    name: options.schemaName,
                    strict: true,
                    schema,
                },
            },
            temperature: options.temperature,
        });
        const latency_ms = performance.now() - start;
        const rawContent = response.choices[0]?.message?.content ?? '{}';
        let output;
        try {
            output = JSON.parse(rawContent);
        }
        catch (parseErr) {
            log.error('JSON parse failed on model response (json_schema mode)', {
                model: options.model,
                schema: options.schemaName,
                raw_content: rawContent.slice(0, 500),
                parse_error: serializeError(parseErr).message,
            });
            throw new Error(`Failed to parse model JSON response: ${rawContent.slice(0, 200)}`);
        }
        const metrics = {
            latency_ms,
            input_tokens: response.usage?.prompt_tokens ?? 0,
            output_tokens: response.usage?.completion_tokens ?? 0,
        };
        log.info('API call complete (structured/json_schema)', {
            model: options.model,
            schema: options.schemaName,
            latency_ms: Math.round(latency_ms),
            tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
        });
        return { output, metrics };
    }
    catch (err) {
        const latency_ms = performance.now() - start;
        const se = serializeError(err);
        log.error('API call failed (structured/json_schema)', {
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
// ── Plain JSON mode (no schema) ──
async function callJsonMode(api, messages, options, start) {
    try {
        const response = await api.chat.completions.create({
            model: options.model,
            messages,
            response_format: { type: 'json_object' },
            temperature: options.temperature,
        });
        const latency_ms = performance.now() - start;
        const rawContent = response.choices[0]?.message?.content ?? '{}';
        let output;
        try {
            output = JSON.parse(rawContent);
        }
        catch (parseErr) {
            log.error('JSON parse failed on model response', {
                model: options.model,
                raw_content: rawContent.slice(0, 500),
                parse_error: serializeError(parseErr).message,
            });
            throw new Error(`Failed to parse model JSON response: ${rawContent.slice(0, 200)}`);
        }
        const metrics = {
            latency_ms,
            input_tokens: response.usage?.prompt_tokens ?? 0,
            output_tokens: response.usage?.completion_tokens ?? 0,
        };
        log.info('API call complete (json_mode)', {
            model: options.model,
            latency_ms: Math.round(latency_ms),
            tokens: { input: metrics.input_tokens, output: metrics.output_tokens },
        });
        return { output, metrics };
    }
    catch (err) {
        const latency_ms = performance.now() - start;
        const se = serializeError(err);
        log.error('API call failed (json_mode)', {
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
//# sourceMappingURL=llm.js.map