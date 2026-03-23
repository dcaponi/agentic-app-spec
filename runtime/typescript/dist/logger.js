/** Structured server-side logger. Truncates large values (base64 images) automatically. */
function truncate(value, maxLen = 200) {
    if (typeof value === 'string' && value.length > maxLen) {
        return `${value.slice(0, maxLen)}... (${value.length} chars)`;
    }
    if (Array.isArray(value)) {
        return value.length > 5
            ? [...value.slice(0, 3).map((v) => truncate(v, maxLen)), `... +${value.length - 3} more`]
            : value.map((v) => truncate(v, maxLen));
    }
    if (value && typeof value === 'object') {
        const out = {};
        for (const [k, v] of Object.entries(value)) {
            out[k] = truncate(v, maxLen);
        }
        return out;
    }
    return value;
}
function fmt(component, level, message, data) {
    const ts = new Date().toISOString();
    const prefix = `[${ts}] [${level}] [${component}]`;
    if (!data || Object.keys(data).length === 0)
        return `${prefix} ${message}`;
    return `${prefix} ${message} ${JSON.stringify(truncate(data), null, 2)}`;
}
export function createLogger(component) {
    return {
        debug(message, data) {
            console.debug(fmt(component, 'DEBUG', message, data));
        },
        info(message, data) {
            console.log(fmt(component, 'INFO', message, data));
        },
        warn(message, data) {
            console.warn(fmt(component, 'WARN', message, data));
        },
        error(message, data) {
            console.error(fmt(component, 'ERROR', message, data));
        },
    };
}
/** Extract a serializable error summary for logging/analysis. */
export function serializeError(err) {
    if (err instanceof Error) {
        const e = err;
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
//# sourceMappingURL=logger.js.map