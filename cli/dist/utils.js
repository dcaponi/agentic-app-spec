import fs from 'node:fs';
import path from 'node:path';
import YAML from 'yaml';
// ── Case conversions ────────────────────────────────────────────────────────
/** kebab-case or snake_case -> camelCase */
export function toCamelCase(s) {
    return s.replace(/[-_]([a-z0-9])/g, (_, c) => c.toUpperCase());
}
/** kebab-case or snake_case -> PascalCase */
export function toPascalCase(s) {
    const camel = toCamelCase(s);
    return camel.charAt(0).toUpperCase() + camel.slice(1);
}
/** kebab-case -> snake_case */
export function toSnakeCase(s) {
    return s.replace(/-/g, '_');
}
/** kebab-case or snake_case -> PascalCase (Go public identifier) */
export function toGoPublic(s) {
    return toPascalCase(s);
}
// ── Type mapping ────────────────────────────────────────────────────────────
const tsTypeMap = {
    base64: 'string',
    string: 'string',
    number: 'number',
    boolean: 'boolean',
    object: 'Record<string, unknown>',
};
const pyTypeMap = {
    base64: 'str',
    string: 'str',
    number: 'float',
    boolean: 'bool',
    object: 'dict[str, Any]',
};
const rubyTypeMap = {
    base64: 'String',
    string: 'String',
    number: 'Numeric',
    boolean: 'Boolean (TrueClass)',
    object: 'Hash',
};
const goTypeMap = {
    base64: 'string',
    string: 'string',
    number: 'float64',
    boolean: 'bool',
    object: 'map[string]interface{}',
};
export function yamlTypeToTS(type) {
    return tsTypeMap[type] ?? 'unknown';
}
export function yamlTypeToPython(type) {
    return pyTypeMap[type] ?? 'Any';
}
export function yamlTypeToRuby(type) {
    return rubyTypeMap[type] ?? 'Object';
}
export function yamlTypeToGo(type) {
    return goTypeMap[type] ?? 'interface{}';
}
// ── YAML helpers ────────────────────────────────────────────────────────────
export function loadYaml(filePath) {
    const raw = fs.readFileSync(filePath, 'utf-8');
    return YAML.parse(raw);
}
export function dumpYaml(data) {
    return YAML.stringify(data, { indent: 2, lineWidth: 120 });
}
// ── Project root discovery ──────────────────────────────────────────────────
/**
 * Walk up from `cwd` looking for `agentic.config.yaml` or an `agents/`
 * directory. Returns the first matching ancestor, or falls back to `cwd`.
 */
export function findProjectRoot(startDir) {
    let dir = startDir ?? process.cwd();
    const root = path.parse(dir).root;
    while (true) {
        if (fs.existsSync(path.join(dir, 'agentic.config.yaml')) ||
            fs.existsSync(path.join(dir, 'agents'))) {
            return dir;
        }
        const parent = path.dirname(dir);
        if (parent === dir || parent === root) {
            // Reached filesystem root without finding markers — use cwd
            return startDir ?? process.cwd();
        }
        dir = parent;
    }
}
//# sourceMappingURL=utils.js.map