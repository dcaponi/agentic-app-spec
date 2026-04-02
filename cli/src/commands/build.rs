use std::fs;
use std::path::{Path, PathBuf};

use crate::utils::*;

// ── Discovery ───────────────────────────────────────────────────────────────

struct FieldInfo {
    name: String,
    yaml_type: String,
}

struct AgentMeta {
    id: String,
    yaml_path: String,
    fields: Vec<FieldInfo>,
}

struct WorkflowMeta {
    name: String,
    yaml_path: String,
    fields: Vec<FieldInfo>,
}

fn extract_fields(input: Option<&serde_yaml::Value>) -> Vec<FieldInfo> {
    match input.and_then(|v| v.as_mapping()) {
        Some(map) => map
            .iter()
            .map(|(k, v)| FieldInfo {
                name: k.as_str().unwrap_or("").to_string(),
                yaml_type: v
                    .get("type")
                    .and_then(|t| t.as_str())
                    .unwrap_or("string")
                    .to_string(),
            })
            .collect(),
        None => vec![],
    }
}

fn discover_agents(root: &Path) -> Vec<AgentMeta> {
    let agents_dir = root.join("agents");
    if !agents_dir.exists() {
        return vec![];
    }

    let mut agents = Vec::new();
    if let Ok(entries) = fs::read_dir(&agents_dir) {
        for entry in entries.flatten() {
            if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                continue;
            }
            let id = entry.file_name().to_string_lossy().to_string();
            let yaml_path = entry.path().join("agent.yaml");
            if !yaml_path.exists() {
                continue;
            }
            let data = load_yaml(&yaml_path);
            let fields = extract_fields(data.get("input"));
            agents.push(AgentMeta {
                id: id.clone(),
                yaml_path: format!("agents/{}/agent.yaml", id),
                fields,
            });
        }
    }
    agents.sort_by(|a, b| a.id.cmp(&b.id));
    agents
}

fn discover_workflows(root: &Path) -> Vec<WorkflowMeta> {
    let workflows_dir = root.join("workflows");
    if !workflows_dir.exists() {
        return vec![];
    }

    let mut workflows = Vec::new();
    if let Ok(entries) = fs::read_dir(&workflows_dir) {
        for entry in entries.flatten() {
            let fname = entry.file_name().to_string_lossy().to_string();
            if !fname.ends_with(".yaml") && !fname.ends_with(".yml") {
                continue;
            }
            let data = load_yaml(&entry.path());
            let name = fname
                .trim_end_matches(".yaml")
                .trim_end_matches(".yml")
                .to_string();
            let fields = extract_fields(data.get("input"));
            workflows.push(WorkflowMeta {
                name: name.clone(),
                yaml_path: format!("workflows/{}", fname),
                fields,
            });
        }
    }
    workflows.sort_by(|a, b| a.name.cmp(&b.name));
    workflows
}

// ── TypeScript generator ────────────────────────────────────────────────────

fn gen_ts_agent(a: &AgentMeta) -> String {
    let pascal = to_pascal_case(&a.id);
    let camel = to_camel_case(&a.id);
    let field_lines: String = a
        .fields
        .iter()
        .map(|f| format!("    {}: {};", to_camel_case(&f.name), yaml_type_to_ts(&f.yaml_type)))
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "// @generated from {} — do not edit\n\
         import {{ invokeAgent }} from '../../engine/runner.js';\n\
         import type {{ AgentResult }} from '../../types.js';\n\
         \n\
         export interface {}Input {{\n\
         {}\n\
         }}\n\
         \n\
         export async function {}(input: {}Input): Promise<AgentResult> {{\n\
         \x20   return invokeAgent('{}', input);\n\
         }}\n",
        a.yaml_path, pascal, field_lines, camel, pascal, a.id
    )
}

fn gen_ts_workflow(w: &WorkflowMeta) -> String {
    let pascal = to_pascal_case(&w.name);
    let camel = to_camel_case(&w.name);
    let field_lines: String = w
        .fields
        .iter()
        .map(|f| format!("    {}: {};", to_camel_case(&f.name), yaml_type_to_ts(&f.yaml_type)))
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "// @generated from {} — do not edit\n\
         import {{ orchestrate }} from '../../engine/orchestrator.js';\n\
         import type {{ WorkflowEnvelope }} from '../../types.js';\n\
         \n\
         export interface {}Input {{\n\
         {}\n\
         }}\n\
         \n\
         export async function {}(input: {}Input): Promise<WorkflowEnvelope> {{\n\
         \x20   return orchestrate('{}', input);\n\
         }}\n",
        w.yaml_path, pascal, field_lines, camel, pascal, w.name
    )
}

// ── Python generator ────────────────────────────────────────────────────────

fn gen_py_agent(a: &AgentMeta) -> String {
    let pascal = to_pascal_case(&a.id);
    let snake = to_snake_case(&a.id);
    let field_lines = if a.fields.is_empty() {
        "    pass".to_string()
    } else {
        a.fields
            .iter()
            .map(|f| format!("    {}: {}", to_snake_case(&f.name), yaml_type_to_python(&f.yaml_type)))
            .collect::<Vec<_>>()
            .join("\n")
    };

    format!(
        "# @generated from {} — do not edit\n\
         from dataclasses import dataclass\n\
         from typing import Any\n\
         from agentic_engine.runner import invoke_agent\n\
         from agentic_engine.types import AgentResult\n\
         \n\
         \n\
         @dataclass\n\
         class {}Input:\n\
         {}\n\
         \n\
         \n\
         async def {}(input: {}Input) -> AgentResult:\n\
         \x20   return await invoke_agent('{}', vars(input))\n",
        a.yaml_path, pascal, field_lines, snake, pascal, a.id
    )
}

fn gen_py_workflow(w: &WorkflowMeta) -> String {
    let pascal = to_pascal_case(&w.name);
    let snake = to_snake_case(&w.name);
    let field_lines = if w.fields.is_empty() {
        "    pass".to_string()
    } else {
        w.fields
            .iter()
            .map(|f| format!("    {}: {}", to_snake_case(&f.name), yaml_type_to_python(&f.yaml_type)))
            .collect::<Vec<_>>()
            .join("\n")
    };

    format!(
        "# @generated from {} — do not edit\n\
         from dataclasses import dataclass\n\
         from typing import Any\n\
         from agentic_engine.orchestrator import orchestrate\n\
         from agentic_engine.types import WorkflowEnvelope\n\
         \n\
         \n\
         @dataclass\n\
         class {}Input:\n\
         {}\n\
         \n\
         \n\
         async def {}(input: {}Input) -> WorkflowEnvelope:\n\
         \x20   return await orchestrate('{}', vars(input))\n",
        w.yaml_path, pascal, field_lines, snake, pascal, w.name
    )
}

// ── Ruby generator ──────────────────────────────────────────────────────────

fn gen_rb_agent(a: &AgentMeta) -> String {
    let pascal = to_pascal_case(&a.id);
    let snake = to_snake_case(&a.id);
    let symbols = a
        .fields
        .iter()
        .map(|f| format!(":{}", to_snake_case(&f.name)))
        .collect::<Vec<_>>()
        .join(", ");
    let field_comments = a
        .fields
        .iter()
        .map(|f| format!("  # @param {} [{}]", to_snake_case(&f.name), yaml_type_to_ruby(&f.yaml_type)))
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "# @generated from {} — do not edit\n\
         # frozen_string_literal: true\n\
         \n\
         module Agents\n\
         \x20 {}Input = Struct.new({}, keyword_init: true)\n\
         \n\
         {}\n\
         \x20 def self.{}(input)\n\
         \x20   Engine::Runner.invoke_agent('{}', input.to_h)\n\
         \x20 end\n\
         end\n",
        a.yaml_path, pascal, symbols, field_comments, snake, a.id
    )
}

fn gen_rb_workflow(w: &WorkflowMeta) -> String {
    let pascal = to_pascal_case(&w.name);
    let snake = to_snake_case(&w.name);
    let symbols = w
        .fields
        .iter()
        .map(|f| format!(":{}", to_snake_case(&f.name)))
        .collect::<Vec<_>>()
        .join(", ");
    let field_comments = w
        .fields
        .iter()
        .map(|f| format!("  # @param {} [{}]", to_snake_case(&f.name), yaml_type_to_ruby(&f.yaml_type)))
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "# @generated from {} — do not edit\n\
         # frozen_string_literal: true\n\
         \n\
         module Workflows\n\
         \x20 {}Input = Struct.new({}, keyword_init: true)\n\
         \n\
         {}\n\
         \x20 def self.{}(input)\n\
         \x20   Engine::Orchestrator.orchestrate('{}', input.to_h)\n\
         \x20 end\n\
         end\n",
        w.yaml_path, pascal, symbols, field_comments, snake, w.name
    )
}

// ── Go generator ────────────────────────────────────────────────────────────

fn gen_go_agent(a: &AgentMeta) -> String {
    let pascal = to_go_public(&a.id);
    let field_lines = a
        .fields
        .iter()
        .map(|f| {
            format!(
                "    {} {} `json:\"{}\"`",
                to_go_public(&f.name),
                yaml_type_to_go(&f.yaml_type),
                to_snake_case(&f.name)
            )
        })
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "// @generated from {} — do not edit\n\
         package agents\n\
         \n\
         import engine \"github.com/dcaponi/agentic-app-spec/runtime/go\"\n\
         \n\
         type {}Input struct {{\n\
         {}\n\
         }}\n\
         \n\
         func {}(input {}Input) (*engine.AgentResult, error) {{\n\
         \x20   return engine.InvokeAgent(\"{}\", input)\n\
         }}\n",
        a.yaml_path, pascal, field_lines, pascal, pascal, a.id
    )
}

fn gen_go_workflow(w: &WorkflowMeta) -> String {
    let pascal = to_go_public(&w.name);
    let field_lines = w
        .fields
        .iter()
        .map(|f| {
            format!(
                "    {} {} `json:\"{}\"`",
                to_go_public(&f.name),
                yaml_type_to_go(&f.yaml_type),
                to_snake_case(&f.name)
            )
        })
        .collect::<Vec<_>>()
        .join("\n");

    format!(
        "// @generated from {} — do not edit\n\
         package workflows\n\
         \n\
         import engine \"github.com/dcaponi/agentic-app-spec/runtime/go\"\n\
         \n\
         type {}Input struct {{\n\
         {}\n\
         }}\n\
         \n\
         func {}(input {}Input) (*engine.WorkflowEnvelope, error) {{\n\
         \x20   return engine.Orchestrate(\"{}\", input)\n\
         }}\n",
        w.yaml_path, pascal, field_lines, pascal, pascal, w.name
    )
}

// ── Extension map ───────────────────────────────────────────────────────────

fn ext_for(lang: &str) -> &str {
    match lang {
        "typescript" => ".ts",
        "python" => ".py",
        "ruby" => ".rb",
        "go" => ".go",
        _ => unreachable!(),
    }
}

// ── Main build runner ───────────────────────────────────────────────────────

pub fn run(lang_opt: Option<&str>, outdir_opt: Option<&str>) {
    let root = find_project_root();

    // Load config for defaults
    let mut config_lang = "typescript".to_string();
    let mut config_outdir = "src/generated".to_string();

    let config_path = root.join("agentic.config.yaml");
    if config_path.exists() {
        let cfg = load_yaml(&config_path);
        if let Some(l) = cfg.get("lang").and_then(|v| v.as_str()) {
            config_lang = l.to_string();
        }
        if let Some(o) = cfg.get("outdir").and_then(|v| v.as_str()) {
            config_outdir = o.to_string();
        }
    }

    let lang = lang_opt.unwrap_or(&config_lang);
    let outdir = outdir_opt.unwrap_or(&config_outdir);

    let valid = ["typescript", "python", "ruby", "go"];
    if !valid.contains(&lang) {
        eprintln!(
            "Error: unsupported language \"{}\". Choose from: {}",
            lang,
            valid.join(", ")
        );
        std::process::exit(1);
    }

    let agents = discover_agents(&root);
    let workflows = discover_workflows(&root);

    if agents.is_empty() && workflows.is_empty() {
        println!("Nothing to build — no agents or workflows found.");
        return;
    }

    let out_root = root.join(outdir);
    let agents_out = out_root.join("agents");
    let workflows_out = out_root.join("workflows");

    fs::create_dir_all(&agents_out).expect("Failed to create agents output dir");
    fs::create_dir_all(&workflows_out).expect("Failed to create workflows output dir");

    let ext = ext_for(lang);

    for agent in &agents {
        let code = match lang {
            "typescript" => gen_ts_agent(agent),
            "python" => gen_py_agent(agent),
            "ruby" => gen_rb_agent(agent),
            "go" => gen_go_agent(agent),
            _ => unreachable!(),
        };
        let file_name = if lang == "go" {
            to_snake_case(&agent.id)
        } else {
            to_camel_case(&agent.id)
        };
        let out_path = agents_out.join(format!("{}{}", file_name, ext));
        fs::write(&out_path, code).expect("Failed to write generated file");
        println!("  generated {}", relative_to(&root, &out_path));
    }

    for wf in &workflows {
        let code = match lang {
            "typescript" => gen_ts_workflow(wf),
            "python" => gen_py_workflow(wf),
            "ruby" => gen_rb_workflow(wf),
            "go" => gen_go_workflow(wf),
            _ => unreachable!(),
        };
        let file_name = if lang == "go" {
            to_snake_case(&wf.name)
        } else {
            to_camel_case(&wf.name)
        };
        let out_path = workflows_out.join(format!("{}{}", file_name, ext));
        fs::write(&out_path, code).expect("Failed to write generated file");
        println!("  generated {}", relative_to(&root, &out_path));
    }

    println!(
        "\nBuild complete: {} agent(s), {} workflow(s) -> {} in {}/",
        agents.len(),
        workflows.len(),
        lang,
        outdir
    );
}

fn relative_to(base: &Path, full: &PathBuf) -> String {
    full.strip_prefix(base)
        .map(|p| p.display().to_string())
        .unwrap_or_else(|_| full.display().to_string())
}
