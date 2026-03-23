import type { z } from 'zod';
import type { StepMetrics, JsonSchemaObject } from './types.js';
/** Register a schema (Zod or JSON Schema object) for use by LLM agents. */
export declare function registerSchema(name: string, schema: z.ZodType | JsonSchemaObject): void;
/** Get a registered schema by name. Returns undefined if not found. */
export declare function getSchema(name: string): z.ZodType | JsonSchemaObject | undefined;
export interface LLMCallOptions {
    model: string;
    systemPrompt: string;
    userContent: string | Array<{
        type: string;
        text?: string;
        image_url?: {
            url: string;
            detail?: string;
        };
    }>;
    temperature: number;
    schemaName?: string | null;
}
export interface LLMResult {
    output: unknown;
    metrics: StepMetrics;
}
export declare function callLLM(options: LLMCallOptions): Promise<LLMResult>;
//# sourceMappingURL=llm.d.ts.map