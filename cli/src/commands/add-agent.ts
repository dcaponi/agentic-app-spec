import fs from 'node:fs';
import path from 'node:path';
import { dumpYaml, findProjectRoot, toPascalCase } from '../utils.js';

export interface AddAgentOptions {
  type: 'llm' | 'deterministic';
  model: string;
  inputType: 'image' | 'text';
}

function buildLlmAgentYaml(id: string, opts: AddAgentOptions): Record<string, unknown> {
  return {
    name: toPascalCase(id),
    type: 'llm',
    model: opts.model,
    temperature: 0.7,
    input_type: opts.inputType,
    schema: null,
    user_message: `Provide input for ${toPascalCase(id)}`,
    input: {
      data: {
        type: opts.inputType === 'image' ? 'base64' : 'string',
        required: true,
        description: `Primary input for the ${toPascalCase(id)} agent`,
      },
    },
  };
}

function buildDeterministicAgentYaml(id: string): Record<string, unknown> {
  return {
    name: toPascalCase(id),
    type: 'deterministic',
    handler: `handlers/${id}.ts`,
    input: {
      data: {
        type: 'string',
        required: true,
        description: `Primary input for the ${toPascalCase(id)} agent`,
      },
    },
  };
}

function buildPromptMd(id: string): string {
  return `# ${toPascalCase(id)} — System Prompt

You are the ${toPascalCase(id)} agent.

## Instructions

Describe the agent's purpose and behaviour here.

## Constraints

- Keep responses concise.
- Follow the output schema when one is provided.
`;
}

export function runAddAgent(id: string, opts: AddAgentOptions): void {
  const root = findProjectRoot();
  const agentDir = path.join(root, 'agents', id);

  if (fs.existsSync(agentDir)) {
    console.error(`Error: agent "${id}" already exists at ${agentDir}`);
    process.exit(1);
  }

  fs.mkdirSync(agentDir, { recursive: true });

  if (opts.type === 'llm') {
    const yaml = buildLlmAgentYaml(id, opts);
    fs.writeFileSync(path.join(agentDir, 'agent.yaml'), dumpYaml(yaml), 'utf-8');
    fs.writeFileSync(path.join(agentDir, 'prompt.md'), buildPromptMd(id), 'utf-8');
    console.log(`  created agents/${id}/agent.yaml`);
    console.log(`  created agents/${id}/prompt.md`);
  } else {
    const yaml = buildDeterministicAgentYaml(id);
    fs.writeFileSync(path.join(agentDir, 'agent.yaml'), dumpYaml(yaml), 'utf-8');
    console.log(`  created agents/${id}/agent.yaml`);
  }

  console.log(`\nAgent "${id}" added (${opts.type}).`);
}
