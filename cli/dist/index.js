#!/usr/bin/env node
import { Command } from 'commander';
import { runInit } from './commands/init.js';
import { runAddAgent } from './commands/add-agent.js';
import { runAddWorkflow } from './commands/add-workflow.js';
import { runBuild } from './commands/build.js';
import { runList } from './commands/list.js';
const program = new Command();
program
    .name('agentic')
    .description('CLI for Agentic App Spec — scaffold agents, workflows, and generate typed handles')
    .version('1.0.0');
// ── init ────────────────────────────────────────────────────────────────────
program
    .command('init')
    .description('Initialise a new agentic project (creates agents/, workflows/, schemas/, and config)')
    .action(() => {
    runInit();
});
// ── add ─────────────────────────────────────────────────────────────────────
const add = program
    .command('add')
    .description('Add a new agent or workflow');
add
    .command('agent <id>')
    .description('Scaffold a new agent')
    .option('--type <type>', 'Agent type: llm or deterministic', 'llm')
    .option('--model <model>', 'Model to use (LLM agents only)', 'gpt-4.1')
    .option('--input-type <inputType>', 'Input modality: text or image', 'text')
    .action((id, opts) => {
    if (opts.type !== 'llm' && opts.type !== 'deterministic') {
        console.error(`Error: --type must be "llm" or "deterministic", got "${opts.type}"`);
        process.exit(1);
    }
    if (opts.inputType !== 'text' && opts.inputType !== 'image') {
        console.error(`Error: --input-type must be "text" or "image", got "${opts.inputType}"`);
        process.exit(1);
    }
    runAddAgent(id, {
        type: opts.type,
        model: opts.model,
        inputType: opts.inputType,
    });
});
add
    .command('workflow <name>')
    .description('Scaffold a new workflow')
    .option('--agents <agents>', 'Comma-separated agent IDs to include as steps')
    .action((name, opts) => {
    runAddWorkflow(name, { agents: opts.agents });
});
// ── build ───────────────────────────────────────────────────────────────────
program
    .command('build')
    .description('Generate typed integration handles from agents and workflows')
    .option('--lang <lang>', 'Target language: typescript, python, ruby, or go')
    .option('--outdir <outdir>', 'Output directory for generated code')
    .action((opts) => {
    runBuild(opts);
});
// ── list ────────────────────────────────────────────────────────────────────
program
    .command('list')
    .description('List all discovered agents and workflows')
    .action(() => {
    runList();
});
// ── parse ───────────────────────────────────────────────────────────────────
program.parse();
//# sourceMappingURL=index.js.map