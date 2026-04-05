import type { ExecutionContext } from './types.js';
import { createLogger } from './logger.js';

const log = createLogger('resolver');

/**
 * Tokenize a dotted path, splitting array indices.
 * "steps.fetch.output[0].name" -> ["steps", "fetch", "output", "[0]", "name"]
 */
function tokenizePath(path: string): string[] {
	const tokens: string[] = [];
	for (const part of path.split('.')) {
		if (!part) continue;
		const idx = part.indexOf('[');
		if (idx !== -1) {
			const field = part.slice(0, idx);
			if (field) tokens.push(field);
			tokens.push(part.slice(idx)); // e.g. "[0]"
		} else {
			tokens.push(part);
		}
	}
	return tokens;
}

/** Traverse a nested object/array by tokenized path segments. */
function traversePath(obj: unknown, tokens: string[]): unknown {
	let current = obj;
	for (const token of tokens) {
		if (current == null || typeof current !== 'object') return undefined;

		if (token.startsWith('[') && token.endsWith(']')) {
			// Array index
			const idx = parseInt(token.slice(1, -1), 10);
			if (!Array.isArray(current) || idx < 0 || idx >= current.length) return undefined;
			current = current[idx];
		} else {
			current = (current as Record<string, unknown>)[token];
		}
	}
	return current;
}

/** Resolve a `$.path.to.value` reference against execution context. */
export function resolveRef(ref: unknown, context: ExecutionContext): unknown {
	if (typeof ref !== 'string' || !ref.startsWith('$.')) return ref;

	const tokens = tokenizePath(ref.slice(2)); // strip "$."
	if (tokens.length === 0) return ref;

	const root = tokens[0];
	const remaining = tokens.slice(1);

	let value: unknown;

	if (root === 'input') {
		value = traversePath(context.input, remaining);
	} else if (root === 'current') {
		// $.current is sugar for $.steps.__current.output
		const currentData = context.steps['__current'];
		if (!currentData) return undefined;
		value = remaining.length > 0
			? traversePath(currentData.output, remaining)
			: currentData.output;
	} else if (root === 'steps') {
		value = traversePath(context.steps, remaining);
	} else {
		return ref;
	}

	if (value === undefined) {
		const available: Record<string, string> = {};
		available['input_keys'] = context.input && typeof context.input === 'object'
			? Object.keys(context.input as Record<string, unknown>).join(', ')
			: String(typeof context.input);
		for (const [stepId, stepData] of Object.entries(context.steps)) {
			if (stepId === '__current') continue;
			const out = stepData.output;
			available[`steps.${stepId}.output`] = out && typeof out === 'object'
				? `{${Object.keys(out as Record<string, unknown>).join(', ')}}`
				: String(typeof out);
		}
		log.warn(`Binding resolved to undefined: ${ref}`, { available_bindings: available });
	}

	return value;
}

/** Resolve all input bindings for a step. */
export function resolveInputs(
	bindings: Record<string, string>,
	context: ExecutionContext
): Record<string, unknown> {
	const resolved: Record<string, unknown> = {};
	for (const [key, ref] of Object.entries(bindings)) {
		resolved[key] = resolveRef(ref, context);
	}
	return resolved;
}

/** Resolve output bindings to produce final workflow result. */
export function resolveOutputs(
	bindings: Record<string, string>,
	context: ExecutionContext
): Record<string, unknown> {
	const resolved: Record<string, unknown> = {};
	for (const [key, ref] of Object.entries(bindings)) {
		resolved[key] = resolveRef(ref, context);
	}
	return resolved;
}

/** Replace {{key}} and {{key.sub}} template placeholders with values from input. */
export function resolveTemplate(template: string, input: Record<string, unknown>): string {
	return template.replace(/\{\{(\S+?)\}\}/g, (_, path: string) => {
		const value = traversePath(input, path.split('.'));
		if (value === undefined) {
			log.warn(`Template placeholder unresolved: {{${path}}}`, {
				available_keys: Object.keys(input),
			});
			return `{{${path}}}`;
		}
		return typeof value === 'string' ? value : JSON.stringify(value);
	});
}
