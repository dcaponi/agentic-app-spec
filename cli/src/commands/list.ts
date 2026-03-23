import fs from 'node:fs';
import path from 'node:path';
import { findProjectRoot, loadYaml } from '../utils.js';

export function runList(): void {
  const root = findProjectRoot();

  // ── Agents ──────────────────────────────────────────────────────────────
  const agentsDir = path.join(root, 'agents');
  const agents: Array<{
    id: string;
    type: string;
    model: string | null;
    hasPrompt: boolean;
  }> = [];

  if (fs.existsSync(agentsDir)) {
    for (const entry of fs.readdirSync(agentsDir, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const yamlPath = path.join(agentsDir, entry.name, 'agent.yaml');
      if (!fs.existsSync(yamlPath)) continue;

      const data = loadYaml(yamlPath);
      agents.push({
        id: entry.name,
        type: data.type ?? 'unknown',
        model: data.model ?? null,
        hasPrompt: fs.existsSync(path.join(agentsDir, entry.name, 'prompt.md')),
      });
    }
  }

  // ── Workflows ───────────────────────────────────────────────────────────
  const workflowsDir = path.join(root, 'workflows');
  const workflows: Array<{
    name: string;
    version: string;
    stepCount: number;
    stepIds: string[];
  }> = [];

  if (fs.existsSync(workflowsDir)) {
    for (const f of fs.readdirSync(workflowsDir)) {
      if (!f.endsWith('.yaml') && !f.endsWith('.yml')) continue;
      const data = loadYaml(path.join(workflowsDir, f));
      const name = f.replace(/\.ya?ml$/, '');
      const steps: any[] = Array.isArray(data.steps) ? data.steps : [];
      workflows.push({
        name,
        version: data.version ?? '-',
        stepCount: steps.length,
        stepIds: steps.map((s: any) => s.id ?? '?'),
      });
    }
  }

  // ── Print ───────────────────────────────────────────────────────────────
  if (agents.length === 0 && workflows.length === 0) {
    console.log('No agents or workflows found. Run `agentic init` to get started.');
    return;
  }

  if (agents.length > 0) {
    console.log('Agents');
    console.log('------');
    for (const a of agents) {
      const modelStr = a.model ? ` model=${a.model}` : '';
      const promptStr = a.hasPrompt ? ' prompt.md=yes' : '';
      console.log(`  ${a.id}  type=${a.type}${modelStr}${promptStr}`);
    }
    console.log();
  }

  if (workflows.length > 0) {
    console.log('Workflows');
    console.log('---------');
    for (const w of workflows) {
      console.log(
        `  ${w.name}  version=${w.version}  steps=${w.stepCount} [${w.stepIds.join(', ')}]`,
      );
    }
    console.log();
  }
}
