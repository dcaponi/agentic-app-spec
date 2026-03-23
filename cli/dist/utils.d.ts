/** kebab-case or snake_case -> camelCase */
export declare function toCamelCase(s: string): string;
/** kebab-case or snake_case -> PascalCase */
export declare function toPascalCase(s: string): string;
/** kebab-case -> snake_case */
export declare function toSnakeCase(s: string): string;
/** kebab-case or snake_case -> PascalCase (Go public identifier) */
export declare function toGoPublic(s: string): string;
export declare function yamlTypeToTS(type: string): string;
export declare function yamlTypeToPython(type: string): string;
export declare function yamlTypeToRuby(type: string): string;
export declare function yamlTypeToGo(type: string): string;
export declare function loadYaml(filePath: string): any;
export declare function dumpYaml(data: unknown): string;
/**
 * Walk up from `cwd` looking for `agentic.config.yaml` or an `agents/`
 * directory. Returns the first matching ancestor, or falls back to `cwd`.
 */
export declare function findProjectRoot(startDir?: string): string;
//# sourceMappingURL=utils.d.ts.map