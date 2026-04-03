import { readFileSync, readdirSync, existsSync } from 'fs';
import { join } from 'path';
import YAML from 'yaml';
import type { AgentDefinition, WorkflowDefinition, WorkflowSummary, JsonSchemaObject, RoutingAgentDefinition } from './types.js';
import { createLogger, serializeError } from './logger.js';

const log = createLogger('loader');

// ── Configurable root directory ──

let ROOT = process.cwd();

/** Set the project root directory for loading agents, workflows, and schemas. */
export function setProjectRoot(dir: string): void {
	ROOT = dir;
	log.info('Project root updated', { root: ROOT });
	// Clear caches when root changes
	agentCache.clear();
	workflowCache.clear();
	jsonSchemaCache.clear();
	routingAgentCache.clear();
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

function routingAgentsDir(): string {
	return join(ROOT, 'agentic-spec', 'routing-agents');
}

// ── Caches (lazy-loaded, process-scoped) ──

const agentCache = new Map<string, AgentDefinition>();
const workflowCache = new Map<string, WorkflowDefinition>();
const jsonSchemaCache = new Map<string, JsonSchemaObject>();
const routingAgentCache = new Map<string, RoutingAgentDefinition>();

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
		log.error(`agent.yaml not found for agent: ${agentId}`, { expected_path: configPath });
		throw new Error(`agent.yaml not found at ${configPath}`);
	}

	let config: AgentDefinition;
	try {
		config = YAML.parse(readFileSync(configPath, 'utf-8'));
	} catch (err) {
		log.error(`Failed to parse agent.yaml for: ${agentId}`, {
			path: configPath,
			error: serializeError(err).message,
		});
		throw new Error(`Failed to parse ${configPath}: ${serializeError(err).message}`);
	}

	const promptPath = join(dir, 'prompt.md');
	if (existsSync(promptPath)) {
		config.prompt = readFileSync(promptPath, 'utf-8').trim();
		log.debug(`Loaded prompt.md for ${agentId}`, { prompt_length: config.prompt.length });
	} else if (config.type === 'llm') {
		log.warn(`LLM agent ${agentId} has no prompt.md — system prompt will be empty`, { path: promptPath });
	}

	log.info(`Agent loaded: ${agentId}`, {
		type: config.type,
		model: config.model,
		schema: config.schema,
		input_type: config.input_type,
		has_prompt: !!config.prompt,
	});

	agentCache.set(agentId, config);
	return config;
}

export function loadAllAgents(): Map<string, AgentDefinition> {
	const ad = agentsDir();
	if (!existsSync(ad)) {
		log.error('Agents directory does not exist', {
			expected: ad,
			cwd: ROOT,
		});
		return new Map();
	}

	const dirs = readdirSync(ad, { withFileTypes: true }).filter((d) => d.isDirectory());
	log.info(`Loading all agents`, { count: dirs.length, agents: dirs.map((d) => d.name) });

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

// ── Routing agent loading ──

export function loadRoutingAgent(routingAgentId: string): RoutingAgentDefinition {
	const cached = routingAgentCache.get(routingAgentId);
	if (cached) return cached;

	const dir = join(routingAgentsDir(), routingAgentId);
	log.info(`Loading routing-agent: ${routingAgentId}`, { path: dir });

	if (!existsSync(dir)) {
		const rd = routingAgentsDir();
		log.error(`Routing-agent directory not found: ${routingAgentId}`, {
			expected_path: dir,
			routing_agents_dir_exists: existsSync(rd),
			available_routing_agents: existsSync(rd)
				? readdirSync(rd, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name)
				: [],
		});
		throw new Error(`Routing-agent not found: ${routingAgentId}. Directory does not exist: ${dir}`);
	}

	const configPath = join(dir, 'routing-agent.yaml');
	if (!existsSync(configPath)) {
		log.error(`routing-agent.yaml not found for routing-agent: ${routingAgentId}`, { expected_path: configPath });
		throw new Error(`routing-agent.yaml not found at ${configPath}`);
	}

	let config: RoutingAgentDefinition;
	try {
		config = YAML.parse(readFileSync(configPath, 'utf-8'));
	} catch (err) {
		log.error(`Failed to parse routing-agent.yaml for: ${routingAgentId}`, {
			path: configPath,
			error: serializeError(err).message,
		});
		throw new Error(`Failed to parse ${configPath}: ${serializeError(err).message}`);
	}

	const promptPath = join(dir, 'prompt.md');
	if (existsSync(promptPath)) {
		config.prompt = readFileSync(promptPath, 'utf-8').trim();
		log.debug(`Loaded prompt.md for routing-agent ${routingAgentId}`, { prompt_length: config.prompt.length });
	} else if (config.strategy === 'llm') {
		log.warn(`LLM routing-agent ${routingAgentId} has no prompt.md — system prompt will be empty`, { path: promptPath });
	}

	log.info(`Routing-agent loaded: ${routingAgentId}`, {
		strategy: config.strategy,
		model: config.model,
		has_prompt: !!config.prompt,
	});

	routingAgentCache.set(routingAgentId, config);
	return config;
}

// ── Workflow loading ──

export function loadWorkflow(name: string): WorkflowDefinition {
	const cached = workflowCache.get(name);
	if (cached) return cached;

	const wd = workflowsDir();
	const filePath = join(wd, `${name}.yaml`);
	log.info(`Loading workflow: ${name}`, { path: filePath });

	if (!existsSync(filePath)) {
		log.error(`Workflow file not found: ${name}`, {
			expected_path: filePath,
			workflows_dir_exists: existsSync(wd),
			available_workflows: existsSync(wd)
				? readdirSync(wd).filter((f) => f.endsWith('.yaml'))
				: [],
		});
		throw new Error(`Workflow not found: ${name}. File does not exist: ${filePath}`);
	}

	let wf: WorkflowDefinition;
	try {
		wf = YAML.parse(readFileSync(filePath, 'utf-8'));
	} catch (err) {
		log.error(`Failed to parse workflow YAML: ${name}`, {
			path: filePath,
			error: serializeError(err).message,
		});
		throw new Error(`Failed to parse ${filePath}: ${serializeError(err).message}`);
	}

	log.info(`Workflow loaded: ${name}`, {
		version: wf.version,
		steps: wf.steps?.length ?? 0,
		input_keys: wf.input ? Object.keys(wf.input) : [],
	});

	workflowCache.set(name, wf);
	return wf;
}

export function listWorkflows(): WorkflowSummary[] {
	const wd = workflowsDir();
	if (!existsSync(wd)) {
		log.warn('Workflows directory does not exist', { expected: wd });
		return [];
	}
	return readdirSync(wd)
		.filter((f) => f.endsWith('.yaml'))
		.map((f) => {
			const wf = YAML.parse(readFileSync(join(wd, f), 'utf-8'));
			return { name: wf.name, description: wf.description, version: wf.version, input: wf.input };
		});
}

// ── JSON Schema loading (for non-Zod runtimes) ──

export function loadJsonSchema(name: string): JsonSchemaObject {
	const cached = jsonSchemaCache.get(name);
	if (cached) return cached;

	const sd = schemasDir();
	const filePath = join(sd, `${name}.json`);
	log.info(`Loading JSON schema: ${name}`, { path: filePath });

	if (!existsSync(filePath)) {
		log.error(`JSON schema file not found: ${name}`, {
			expected_path: filePath,
			schemas_dir_exists: existsSync(sd),
			available_schemas: existsSync(sd)
				? readdirSync(sd).filter((f) => f.endsWith('.json'))
				: [],
		});
		throw new Error(`JSON schema not found: ${name}. File does not exist: ${filePath}`);
	}

	let schema: JsonSchemaObject;
	try {
		schema = JSON.parse(readFileSync(filePath, 'utf-8'));
	} catch (err) {
		log.error(`Failed to parse JSON schema: ${name}`, {
			path: filePath,
			error: serializeError(err).message,
		});
		throw new Error(`Failed to parse ${filePath}: ${serializeError(err).message}`);
	}

	log.info(`JSON schema loaded: ${name}`, {
		type: schema.type,
		properties: schema.properties ? Object.keys(schema.properties) : [],
	});

	jsonSchemaCache.set(name, schema);
	return schema;
}

/** Clear all caches (useful for testing or hot-reload scenarios). */
export function clearCaches(): void {
	agentCache.clear();
	workflowCache.clear();
	jsonSchemaCache.clear();
	routingAgentCache.clear();
	log.info('All caches cleared');
}
