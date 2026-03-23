import fs from 'node:fs';
import path from 'node:path';
import { dumpYaml, findProjectRoot, toSnakeCase } from '../utils.js';

export interface AddWorkflowOptions {
  agents?: string; // comma-separated agent IDs
}

function buildStep(agentId: string): Record<string, unknown> {
  return {
    id: `${toSnakeCase(agentId)}_step`,
    agent: agentId,
    input: {
      data: '$.input.data',
    },
    retry: {
      max_attempts: 2,
      backoff_ms: 500,
    },
  };
}

export function runAddWorkflow(name: string, opts: AddWorkflowOptions): void {
  const root = findProjectRoot();
  const workflowsDir = path.join(root, 'workflows');

  if (!fs.existsSync(workflowsDir)) {
    fs.mkdirSync(workflowsDir, { recursive: true });
  }

  const filePath = path.join(workflowsDir, `${name}.yaml`);

  if (fs.existsSync(filePath)) {
    console.error(`Error: workflow "${name}" already exists at ${filePath}`);
    process.exit(1);
  }

  const agentIds = opts.agents
    ? opts.agents.split(',').map((a) => a.trim()).filter(Boolean)
    : [];

  const steps =
    agentIds.length > 0
      ? agentIds.map((id) => buildStep(id))
      : [buildStep('placeholder-agent')];

  // Build output mapping from steps
  const output: Record<string, string> = {};
  for (const step of steps) {
    const stepId = step.id as string;
    output[stepId] = `$.steps.${stepId}.output`;
  }

  const workflow: Record<string, unknown> = {
    name,
    version: '1.0.0',
    steps,
    output,
  };

  fs.writeFileSync(filePath, dumpYaml(workflow), 'utf-8');
  console.log(`  created workflows/${name}.yaml`);
  console.log(`\nWorkflow "${name}" added with ${steps.length} step(s).`);
}
