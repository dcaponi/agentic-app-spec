/** Structured server-side logger. Truncates large values (base64 images) automatically. */
type LogData = Record<string, unknown>;
export interface Logger {
    debug(message: string, data?: LogData): void;
    info(message: string, data?: LogData): void;
    warn(message: string, data?: LogData): void;
    error(message: string, data?: LogData): void;
}
export declare function createLogger(component: string): Logger;
/** Extract a serializable error summary for logging/analysis. */
export declare function serializeError(err: unknown): {
    message: string;
    name: string;
    stack?: string;
    status?: number;
    code?: string;
    type?: string;
    raw: string;
};
export {};
//# sourceMappingURL=logger.d.ts.map