import type { ExecutionContext } from './types.js';
/** Resolve a `$.path.to.value` reference against execution context. */
export declare function resolveRef(ref: unknown, context: ExecutionContext): unknown;
/** Resolve all input bindings for a step. */
export declare function resolveInputs(bindings: Record<string, string>, context: ExecutionContext): Record<string, unknown>;
/** Resolve output bindings to produce final workflow result. */
export declare function resolveOutputs(bindings: Record<string, string>, context: ExecutionContext): Record<string, unknown>;
/** Replace {{key}} and {{key.sub}} template placeholders with values from input. */
export declare function resolveTemplate(template: string, input: Record<string, unknown>): string;
//# sourceMappingURL=resolver.d.ts.map