import { callLLM } from './llm.js';
import { resolveTemplate } from './resolver.js';
import { loadAgent } from './loader.js';
import { createLogger, serializeError } from './logger.js';
const log = createLogger('runner');
// ── Handler registry (user-facing) ──
const handlerRegistry = new Map();
/**
 * Register a deterministic handler function.
 * Called by generated stubs or user code:
 *   registerHandler('product_fetch', (input) => { ... })
 */
export function registerHandler(name, fn) {
    handlerRegistry.set(name, fn);
    log.info(`Handler registered: ${name}`);
}
/** Get all registered handler names (for diagnostics). */
export function getRegisteredHandlers() {
    return [...handlerRegistry.keys()];
}
// ── Public API (called by generated stubs) ──
/** Invoke a single agent by ID with the given input. */
export async function invokeAgent(agentId, input) {
    log.info(`invokeAgent: ${agentId}`, { input_keys: Object.keys(input) });
    const agentDef = loadAgent(agentId);
    return executeAgent(input, agentDef);
}
// ── Core executor ──
/** Execute an agent (used by orchestrator and invokeAgent). */
export async function executeAgent(input, agentDef) {
    log.info(`Executing agent: ${agentDef.name}`, {
        type: agentDef.type,
        model: agentDef.model,
        input_keys: Object.keys(input),
        input_value_types: Object.fromEntries(Object.entries(input).map(([k, v]) => [
            k,
            v === null
                ? 'null'
                : v === undefined
                    ? 'undefined'
                    : typeof v === 'string'
                        ? `string(${v.length})`
                        : typeof v,
        ])),
    });
    if (agentDef.type === 'deterministic') {
        const handlerName = agentDef.handler ?? '';
        const handler = handlerRegistry.get(handlerName);
        if (!handler) {
            log.error(`No deterministic handler registered: ${handlerName}`, {
                available_handlers: [...handlerRegistry.keys()],
            });
            throw new Error(`No deterministic handler: ${handlerName}. Available: ${[...handlerRegistry.keys()].join(', ')}`);
        }
        try {
            const result = handler(input);
            log.info(`Deterministic agent complete: ${agentDef.name}`, {
                latency_ms: Math.round(result.metrics.latency_ms),
            });
            return result;
        }
        catch (err) {
            log.error(`Deterministic agent failed: ${agentDef.name}`, {
                handler: agentDef.handler,
                error: serializeError(err).message,
                input_keys: Object.keys(input),
            });
            throw err;
        }
    }
    return executeLLMAgent(input, agentDef);
}
// ── LLM Agent Executor ──
async function executeLLMAgent(input, agentDef) {
    const systemPrompt = agentDef.prompt ?? '';
    const model = agentDef.model ?? 'gpt-4.1';
    const temperature = agentDef.temperature ?? 0.1;
    if (!systemPrompt) {
        log.warn(`LLM agent ${agentDef.name} has empty system prompt`);
    }
    let userContent;
    if (agentDef.input_type === 'image') {
        const imageBase64 = input.image;
        if (!imageBase64) {
            log.error(`Image agent ${agentDef.name} received no image data`, {
                input_keys: Object.keys(input),
                image_value: imageBase64 === undefined
                    ? 'undefined'
                    : imageBase64 === null
                        ? 'null'
                        : 'empty string',
            });
            throw new Error(`Image agent "${agentDef.name}" requires input.image but received ${imageBase64 === undefined ? 'undefined' : imageBase64 === null ? 'null' : 'empty string'}`);
        }
        log.debug(`Building image message for ${agentDef.name}`, {
            image_length: imageBase64.length,
            detail: agentDef.image_detail,
            user_message: agentDef.user_message?.slice(0, 100),
        });
        userContent = [
            {
                type: 'image_url',
                image_url: {
                    url: `data:image/jpeg;base64,${imageBase64}`,
                    detail: agentDef.image_detail ?? 'auto',
                },
            },
            {
                type: 'text',
                text: agentDef.user_message ?? 'Analyze this image.',
            },
        ];
    }
    else {
        let message = agentDef.user_message ?? JSON.stringify(input);
        message = resolveTemplate(message, input);
        log.debug(`Built text message for ${agentDef.name}`, {
            message_length: message.length,
            message_preview: message.slice(0, 200),
        });
        userContent = message;
    }
    try {
        const result = await callLLM({
            model,
            systemPrompt,
            userContent,
            temperature,
            schemaName: agentDef.schema,
        });
        log.info(`LLM agent complete: ${agentDef.name}`, {
            latency_ms: Math.round(result.metrics.latency_ms),
            tokens: { input: result.metrics.input_tokens, output: result.metrics.output_tokens },
        });
        return result;
    }
    catch (err) {
        log.error(`LLM agent failed: ${agentDef.name}`, {
            model,
            schema: agentDef.schema,
            error: serializeError(err).message,
            error_name: serializeError(err).name,
            error_status: serializeError(err).status,
        });
        throw err;
    }
}
//# sourceMappingURL=runner.js.map