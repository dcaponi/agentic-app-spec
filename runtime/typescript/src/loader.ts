import { readFileSync, readdirSync, existsSync } from 'fs';
import { join } from 'path';
import YAML from 'yaml';
import type { AgentDefinition, WorkflowDefinition, WorkflowSummary, JsonSchemaObject } from './types.js';
import { createLogger, serializeError } from './logger.js';

const log = createLogger('loader');

// ── Configurable root directory ──

let ROOT = process.cwd();

/** Set the project root directory for loading agents, workflows, and schemas. */
export function setProjectRoot(dir: string): void {
	ROOT = dir;
	log.info('Project root updated', { root: ROOT });
	agentCache.clear();
	workflowCache.clear();
	jsonSchemaCache.clear();
}

/** Get the current project root directory. */
export function getProjectRoot(): string {
	return ROOT;
}

function agentsDir(): string {
	return join(ROOT, 'agentic-spec', 'agents');
}

function workflowsDir(): string {
	return join(ROOT, 'agentic-spec', 'workflows');
}

function schemasDir(): string {
	return join(ROOT, 'agentic-spec', 'schemas');
}

// ── Caches (lazy-loaded, process-scoped) ──

const agentCache = new Map<string, AgentDefinition>();
const workflowCache = new Map<string, WorkflowDefinition>();
const jsonSchemaCache = new Map<string, JsonSchemaObject>();

// ── Agent loading ──

export function loadAgent(agentId: string): AgentDefinition {
	const cached = agentCache.get(agentId);
	if (cached) return cached;

	const dir = join(agentsDir(), agentId);
	log.info(`Loading agent: ${agentId}`, { path: dir });

	if (!existsSync(dir)) {
		const ad = agentsDir();
		log.error(`Agent directory not found: ${agentId}`, {
			expected_path: dir,
			agents_dir_exists: existsSync(ad),
			available_agents: existsSync(ad)
				? readdirSync(ad, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name)
				: [],
		});
		throw new Error(`Agent not found: ${agentId}. Directory does not exist: ${dir}`);
	}

	const configPath = join(dir, 'agent.yaml');
	if (!existsSync(configPath)) {
		throw new Error(`agent.yaml not found at ${configPath}`);
	}

	let config: AgentDefinition;
	try {
		config = YAML.parse(readFileSync(configPath, 'utf-8'));
	} catch (err) {
		throw new Error(`Failed to parse ${configPath}: ${serializeError(err).message}`);
	}

	const promptPath = join(dir, 'prompt.md');
	if (existsSync(promptPath)) {
		config.prompt = readFileSync(promptPath, 'utf-8').trim();
	}

	agentCache.set(agentId, config);
	return config;
}

export function loadAllAgents(): Map<string, AgentDefinition> {
	const ad = agentsDir();
	if (!existsSync(ad)) {
		return new Map();
	}

	const dirs = readdirSync(ad, { withFileTypes: true }).filter((d) => d.isDirectory());
	for (const entry of dirs) {
		if (!agentCache.has(entry.name)) {
			try {
				loadAgent(entry.name);
			} catch (err) {
				log.error(`Failed to load agent: ${entry.name}`, { error: serializeError(err).message });
			}
		}
	}
	return new Map(agentCache);
}

// ── Workflow loading ──

export function loadWorkflow(name: string): WorkflowDefinition {
	const cached = workflowCache.get(name);
	if (cached) return cached;

	const filePath = join(workflowsDir(), `${name}.yaml`);
	if (!existsSync(filePath)) {
		throw new Error(`Workflow not found: ${name}. File does not exist: ${filePath}`);
	}

	let wf: WorkflowDefinition;
	try {
		wf = YAML.parse(readFileSync(filePath, 'utf-8'));
	} catch (err) {
		throw new Error(`Failed to parse ${filePath}: ${serializeError(err).message}`);
	}

	log.info(`Workflow loaded: ${name}`, {
		version: wf.version,
		steps: wf.steps?.length ?? 0,
	});

	workflowCache.set(name, wf);
	return wf;
}

export function listWorkflows(): WorkflowSummary[] {
	const wd = workflowsDir();
	if (!existsSync(wd)) return [];
	return readdirSync(wd)
		.filter((f) => f.endsWith('.yaml'))
		.map((f) => {
			const wf = YAML.parse(readFileSync(join(wd, f), 'utf-8'));
			return { name: wf.name, description: wf.description, version: wf.version, input: wf.input };
		});
}

// ── JSON Schema loading ──

export function loadJsonSchema(name: string): JsonSchemaObject {
	const cached = jsonSchemaCache.get(name);
	if (cached) return cached;

	const filePath = join(schemasDir(), `${name}.json`);
	if (!existsSync(filePath)) {
		throw new Error(`JSON schema not found: ${name}. File does not exist: ${filePath}`);
	}

	let schema: JsonSchemaObject;
	try {
		schema = JSON.parse(readFileSync(filePath, 'utf-8'));
	} catch (err) {
		throw new Error(`Failed to parse ${filePath}: ${serializeError(err).message}`);
	}

	jsonSchemaCache.set(name, schema);
	return schema;
}

/** Clear all caches (useful for testing or hot-reload scenarios). */
export function clearCaches(): void {
	agentCache.clear();
	workflowCache.clear();
	jsonSchemaCache.clear();
	log.info('All caches cleared');
}
