/** Structured server-side logger. Truncates large values (base64 images) automatically. */

type LogData = Record<string, unknown>;

function truncate(value: unknown, maxLen = 200): unknown {
	if (typeof value === 'string' && value.length > maxLen) {
		return `${value.slice(0, maxLen)}... (${value.length} chars)`;
	}
	if (Array.isArray(value)) {
		return value.length > 5
			? [...value.slice(0, 3).map((v) => truncate(v, maxLen)), `... +${value.length - 3} more`]
			: value.map((v) => truncate(v, maxLen));
	}
	if (value && typeof value === 'object') {
		const out: Record<string, unknown> = {};
		for (const [k, v] of Object.entries(value)) {
			out[k] = truncate(v, maxLen);
		}
		return out;
	}
	return value;
}

function fmt(component: string, level: string, message: string, data?: LogData): string {
	const ts = new Date().toISOString();
	const prefix = `[${ts}] [${level}] [${component}]`;
	if (!data || Object.keys(data).length === 0) return `${prefix} ${message}`;
	return `${prefix} ${message} ${JSON.stringify(truncate(data), null, 2)}`;
}

export interface Logger {
	debug(message: string, data?: LogData): void;
	info(message: string, data?: LogData): void;
	warn(message: string, data?: LogData): void;
	error(message: string, data?: LogData): void;
}

export function createLogger(component: string): Logger {
	return {
		debug(message: string, data?: LogData) {
			console.debug(fmt(component, 'DEBUG', message, data));
		},
		info(message: string, data?: LogData) {
			console.log(fmt(component, 'INFO', message, data));
		},
		warn(message: string, data?: LogData) {
			console.warn(fmt(component, 'WARN', message, data));
		},
		error(message: string, data?: LogData) {
			console.error(fmt(component, 'ERROR', message, data));
		},
	};
}

/** Extract a serializable error summary for logging/analysis. */
export function serializeError(err: unknown): {
	message: string;
	name: string;
	stack?: string;
	status?: number;
	code?: string;
	type?: string;
	raw: string;
} {
	if (err instanceof Error) {
		const e = err as Error & { status?: number; code?: string; type?: string };
		return {
			message: e.message,
			name: e.name,
			stack: e.stack,
			status: e.status,
			code: e.code,
			type: e.type,
			raw: String(err),
		};
	}
	return { message: String(err), name: 'Unknown', raw: String(err) };
}
