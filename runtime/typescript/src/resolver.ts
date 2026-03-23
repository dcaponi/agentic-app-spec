import type { ExecutionContext } from './types.js';
import { createLogger } from './logger.js';

const log = createLogger('resolver');

/** Traverse a nested object by dot-separated path segments. */
function getNestedValue(obj: unknown, parts: string[]): unknown {
	let current = obj;
	for (const part of parts) {
		if (current == null || typeof current !== 'object') return undefined;
		current = (current as Record<string, unknown>)[part];
	}
	return current;
}

/** Resolve a `$.path.to.value` reference against execution context. */
export function resolveRef(ref: unknown, context: ExecutionContext): unknown {
	if (typeof ref !== 'string' || !ref.startsWith('$.')) return ref;

	const path = ref.slice(2);
	const value = getNestedValue(context, path.split('.'));

	if (value === undefined) {
		// Build a useful debug picture of what IS available
		const available: Record<string, string> = {};
		available['input_keys'] = context.input && typeof context.input === 'object'
			? Object.keys(context.input as Record<string, unknown>).join(', ')
			: String(typeof context.input);
		for (const [stepId, stepData] of Object.entries(context.steps)) {
			const out = stepData.output;
			available[`steps.${stepId}.output`] = out && typeof out === 'object'
				? `{${Object.keys(out as Record<string, unknown>).join(', ')}}`
				: String(typeof out);
		}
		log.warn(`Binding resolved to undefined: ${ref}`, {
			path,
			available_bindings: available,
		});
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

	// Log a summary of what resolved vs what's missing
	const resolvedKeys = Object.entries(resolved)
		.map(([k, v]) => `${k}: ${v === undefined ? 'UNDEFINED' : typeof v}`)
		.join(', ');
	log.debug('Resolved step inputs', { bindings: resolvedKeys });

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
		const value = getNestedValue(input, path.split('.'));
		if (value === undefined) {
			log.warn(`Template placeholder unresolved: {{${path}}}`, {
				available_keys: Object.keys(input),
			});
			return `{{${path}}}`;
		}
		return typeof value === 'string' ? value : JSON.stringify(value);
	});
}
