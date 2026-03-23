import fs from 'node:fs';
import path from 'node:path';
import {
  findProjectRoot,
  loadYaml,
  toCamelCase,
  toPascalCase,
  toSnakeCase,
  toGoPublic,
  yamlTypeToTS,
  yamlTypeToPython,
  yamlTypeToRuby,
  yamlTypeToGo,
} from '../utils.js';

export interface BuildOptions {
  lang?: string;
  outdir?: string;
}

type Lang = 'typescript' | 'python' | 'ruby' | 'go';

// ── Helpers ─────────────────────────────────────────────────────────────────

interface FieldInfo {
  name: string;
  yamlType: string;
}

function extractFields(input: Record<string, any> | undefined): FieldInfo[] {
  if (!input || typeof input !== 'object') return [];
  return Object.entries(input).map(([name, spec]) => ({
    name,
    yamlType: (spec as any)?.type ?? 'string',
  }));
}

function ensureDir(dir: string): void {
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
}

// ── Discover ────────────────────────────────────────────────────────────────

interface AgentMeta {
  id: string;
  yamlPath: string;
  data: Record<string, any>;
  fields: FieldInfo[];
}

interface WorkflowMeta {
  name: string;
  yamlPath: string;
  data: Record<string, any>;
  fields: FieldInfo[];
}

function discoverAgents(root: string): AgentMeta[] {
  const agentsDir = path.join(root, 'agents');
  if (!fs.existsSync(agentsDir)) return [];

  return fs
    .readdirSync(agentsDir, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => {
      const yamlPath = path.join(agentsDir, d.name, 'agent.yaml');
      if (!fs.existsSync(yamlPath)) return null;
      const data = loadYaml(yamlPath);
      return {
        id: d.name,
        yamlPath: `agents/${d.name}/agent.yaml`,
        data,
        fields: extractFields(data.input),
      } as AgentMeta;
    })
    .filter(Boolean) as AgentMeta[];
}

function discoverWorkflows(root: string): WorkflowMeta[] {
  const workflowsDir = path.join(root, 'workflows');
  if (!fs.existsSync(workflowsDir)) return [];

  return fs
    .readdirSync(workflowsDir)
    .filter((f) => f.endsWith('.yaml') || f.endsWith('.yml'))
    .map((f) => {
      const yamlPath = path.join(workflowsDir, f);
      const data = loadYaml(yamlPath);
      const name = f.replace(/\.ya?ml$/, '');

      return {
        name,
        yamlPath: `workflows/${f}`,
        data,
        fields: extractFields(data.input),
      } as WorkflowMeta;
    });
}

// ── TypeScript generator ────────────────────────────────────────────────────

function genTypeScriptAgent(agent: AgentMeta): string {
  const pascal = toPascalCase(agent.id);
  const camel = toCamelCase(agent.id);
  const fieldLines = agent.fields
    .map((f) => `    ${toCamelCase(f.name)}: ${yamlTypeToTS(f.yamlType)};`)
    .join('\n');

  return `// @generated from ${agent.yamlPath} — do not edit
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ${pascal}Input {
${fieldLines}
}

export async function ${camel}(input: ${pascal}Input): Promise<AgentResult> {
    return invokeAgent('${agent.id}', input);
}
`;
}

function genTypeScriptWorkflow(wf: WorkflowMeta): string {
  const pascal = toPascalCase(wf.name);
  const camel = toCamelCase(wf.name);
  const fieldLines = wf.fields
    .map((f) => `    ${toCamelCase(f.name)}: ${yamlTypeToTS(f.yamlType)};`)
    .join('\n');

  return `// @generated from ${wf.yamlPath} — do not edit
import { orchestrate } from '../../engine/orchestrator.js';
import type { WorkflowEnvelope } from '../../types.js';

export interface ${pascal}Input {
${fieldLines}
}

export async function ${camel}(input: ${pascal}Input): Promise<WorkflowEnvelope> {
    return orchestrate('${wf.name}', input);
}
`;
}

// ── Python generator ────────────────────────────────────────────────────────

function genPythonAgent(agent: AgentMeta): string {
  const pascal = toPascalCase(agent.id);
  const snake = toSnakeCase(agent.id);
  const fieldLines = agent.fields
    .map((f) => `    ${toSnakeCase(f.name)}: ${yamlTypeToPython(f.yamlType)}`)
    .join('\n');

  return `# @generated from ${agent.yamlPath} — do not edit
from dataclasses import dataclass
from typing import Any
from engine.runner import invoke_agent
from engine.types import AgentResult


@dataclass
class ${pascal}Input:
${fieldLines || '    pass'}


async def ${snake}(input: ${pascal}Input) -> AgentResult:
    return await invoke_agent('${agent.id}', vars(input))
`;
}

function genPythonWorkflow(wf: WorkflowMeta): string {
  const pascal = toPascalCase(wf.name);
  const snake = toSnakeCase(wf.name);
  const fieldLines = wf.fields
    .map((f) => `    ${toSnakeCase(f.name)}: ${yamlTypeToPython(f.yamlType)}`)
    .join('\n');

  return `# @generated from ${wf.yamlPath} — do not edit
from dataclasses import dataclass
from typing import Any
from engine.orchestrator import orchestrate
from engine.types import WorkflowEnvelope


@dataclass
class ${pascal}Input:
${fieldLines || '    pass'}


async def ${snake}(input: ${pascal}Input) -> WorkflowEnvelope:
    return await orchestrate('${wf.name}', vars(input))
`;
}

// ── Ruby generator ──────────────────────────────────────────────────────────

function genRubyAgent(agent: AgentMeta): string {
  const pascal = toPascalCase(agent.id);
  const snake = toSnakeCase(agent.id);
  const symbols = agent.fields.map((f) => `:${toSnakeCase(f.name)}`).join(', ');
  const fieldComments = agent.fields
    .map((f) => `  # @param ${toSnakeCase(f.name)} [${yamlTypeToRuby(f.yamlType)}]`)
    .join('\n');

  return `# @generated from ${agent.yamlPath} — do not edit
# frozen_string_literal: true

module Agents
  ${pascal}Input = Struct.new(${symbols}, keyword_init: true)

${fieldComments}
  def self.${snake}(input)
    Engine::Runner.invoke_agent('${agent.id}', input.to_h)
  end
end
`;
}

function genRubyWorkflow(wf: WorkflowMeta): string {
  const pascal = toPascalCase(wf.name);
  const snake = toSnakeCase(wf.name);
  const symbols = wf.fields.map((f) => `:${toSnakeCase(f.name)}`).join(', ');
  const fieldComments = wf.fields
    .map((f) => `  # @param ${toSnakeCase(f.name)} [${yamlTypeToRuby(f.yamlType)}]`)
    .join('\n');

  return `# @generated from ${wf.yamlPath} — do not edit
# frozen_string_literal: true

module Workflows
  ${pascal}Input = Struct.new(${symbols}, keyword_init: true)

${fieldComments}
  def self.${snake}(input)
    Engine::Orchestrator.orchestrate('${wf.name}', input.to_h)
  end
end
`;
}

// ── Go generator ────────────────────────────────────────────────────────────

function genGoAgent(agent: AgentMeta): string {
  const pascal = toGoPublic(agent.id);
  const fieldLines = agent.fields
    .map(
      (f) =>
        `    ${toGoPublic(f.name)} ${yamlTypeToGo(f.yamlType)} \`json:"${toSnakeCase(f.name)}"\``,
    )
    .join('\n');

  return `// @generated from ${agent.yamlPath} — do not edit
package agents

import "agentic/engine"

type ${pascal}Input struct {
${fieldLines}
}

func ${pascal}(input ${pascal}Input) (*engine.AgentResult, error) {
    return engine.InvokeAgent("${agent.id}", input)
}
`;
}

function genGoWorkflow(wf: WorkflowMeta): string {
  const pascal = toGoPublic(wf.name);
  const fieldLines = wf.fields
    .map(
      (f) =>
        `    ${toGoPublic(f.name)} ${yamlTypeToGo(f.yamlType)} \`json:"${toSnakeCase(f.name)}"\``,
    )
    .join('\n');

  return `// @generated from ${wf.yamlPath} — do not edit
package workflows

import "agentic/engine"

type ${pascal}Input struct {
${fieldLines}
}

func ${pascal}(input ${pascal}Input) (*engine.WorkflowEnvelope, error) {
    return engine.Orchestrate("${wf.name}", input)
}
`;
}

// ── File extension / subdirectory per language ──────────────────────────────

const EXT: Record<Lang, string> = {
  typescript: '.ts',
  python: '.py',
  ruby: '.rb',
  go: '.go',
};

// ── Main build runner ───────────────────────────────────────────────────────

export function runBuild(opts: BuildOptions): void {
  const root = findProjectRoot();

  // Load config for defaults
  let configLang: Lang = 'typescript';
  let configOutdir = 'src/generated';

  const configPath = path.join(root, 'agentic.config.yaml');
  if (fs.existsSync(configPath)) {
    const cfg = loadYaml(configPath);
    if (cfg?.lang) configLang = cfg.lang as Lang;
    if (cfg?.outdir) configOutdir = cfg.outdir;
  }

  const lang: Lang = (opts.lang as Lang) ?? configLang;
  const outdir = opts.outdir ?? configOutdir;

  const validLangs: Lang[] = ['typescript', 'python', 'ruby', 'go'];
  if (!validLangs.includes(lang)) {
    console.error(`Error: unsupported language "${lang}". Choose from: ${validLangs.join(', ')}`);
    process.exit(1);
  }

  const agents = discoverAgents(root);
  const workflows = discoverWorkflows(root);

  if (agents.length === 0 && workflows.length === 0) {
    console.log('Nothing to build — no agents or workflows found.');
    return;
  }

  const outRoot = path.join(root, outdir);
  const agentsOutDir = path.join(outRoot, 'agents');
  const workflowsOutDir = path.join(outRoot, 'workflows');

  ensureDir(agentsOutDir);
  ensureDir(workflowsOutDir);

  const ext = EXT[lang];

  // Generate agent handles
  for (const agent of agents) {
    let code: string;
    switch (lang) {
      case 'typescript':
        code = genTypeScriptAgent(agent);
        break;
      case 'python':
        code = genPythonAgent(agent);
        break;
      case 'ruby':
        code = genRubyAgent(agent);
        break;
      case 'go':
        code = genGoAgent(agent);
        break;
    }
    const fileName = lang === 'go' ? toSnakeCase(agent.id) : toCamelCase(agent.id);
    const outPath = path.join(agentsOutDir, `${fileName}${ext}`);
    fs.writeFileSync(outPath, code, 'utf-8');
    console.log(`  generated ${path.relative(root, outPath)}`);
  }

  // Generate workflow handles
  for (const wf of workflows) {
    let code: string;
    switch (lang) {
      case 'typescript':
        code = genTypeScriptWorkflow(wf);
        break;
      case 'python':
        code = genPythonWorkflow(wf);
        break;
      case 'ruby':
        code = genRubyWorkflow(wf);
        break;
      case 'go':
        code = genGoWorkflow(wf);
        break;
    }
    const fileName = lang === 'go' ? toSnakeCase(wf.name) : toCamelCase(wf.name);
    const outPath = path.join(workflowsOutDir, `${fileName}${ext}`);
    fs.writeFileSync(outPath, code, 'utf-8');
    console.log(`  generated ${path.relative(root, outPath)}`);
  }

  console.log(
    `\nBuild complete: ${agents.length} agent(s), ${workflows.length} workflow(s) -> ${lang} in ${outdir}/`,
  );
}
