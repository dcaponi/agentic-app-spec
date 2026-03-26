use std::fs;

use crate::utils::{find_project_root, load_yaml};

pub fn run() {
    let root = find_project_root();

    // ── Agents ──────────────────────────────────────────────────────────────
    let agents_dir = root.join("agents");
    let mut agents: Vec<(String, String, Option<String>, bool)> = Vec::new();

    if agents_dir.exists() {
        if let Ok(entries) = fs::read_dir(&agents_dir) {
            for entry in entries.flatten() {
                if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                    continue;
                }
                let name = entry.file_name().to_string_lossy().to_string();
                let yaml_path = entry.path().join("agent.yaml");
                if !yaml_path.exists() {
                    continue;
                }
                let data = load_yaml(&yaml_path);
                let agent_type = data
                    .get("type")
                    .and_then(|v| v.as_str())
                    .unwrap_or("unknown")
                    .to_string();
                let model = data
                    .get("model")
                    .and_then(|v| v.as_str())
                    .map(|s| s.to_string());
                let has_prompt = entry.path().join("prompt.md").exists();
                agents.push((name, agent_type, model, has_prompt));
            }
        }
    }
    agents.sort_by(|a, b| a.0.cmp(&b.0));

    // ── Workflows ───────────────────────────────────────────────────────────
    let workflows_dir = root.join("workflows");
    let mut workflows: Vec<(String, String, usize, Vec<String>)> = Vec::new();

    if workflows_dir.exists() {
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
                let version = data
                    .get("version")
                    .and_then(|v| v.as_str())
                    .unwrap_or("-")
                    .to_string();
                let steps = data
                    .get("steps")
                    .and_then(|v| v.as_sequence())
                    .map(|seq| {
                        seq.iter()
                            .map(|s| {
                                s.get("id")
                                    .and_then(|v| v.as_str())
                                    .unwrap_or("?")
                                    .to_string()
                            })
                            .collect::<Vec<_>>()
                    })
                    .unwrap_or_default();
                let step_count = steps.len();
                workflows.push((name, version, step_count, steps));
            }
        }
    }
    workflows.sort_by(|a, b| a.0.cmp(&b.0));

    // ── Print ───────────────────────────────────────────────────────────────
    if agents.is_empty() && workflows.is_empty() {
        println!("No agents or workflows found. Run `agentic init` to get started.");
        return;
    }

    if !agents.is_empty() {
        println!("Agents");
        println!("------");
        for (id, agent_type, model, has_prompt) in &agents {
            let model_str = model
                .as_ref()
                .map(|m| format!(" model={}", m))
                .unwrap_or_default();
            let prompt_str = if *has_prompt { " prompt.md=yes" } else { "" };
            println!("  {}  type={}{}{}", id, agent_type, model_str, prompt_str);
        }
        println!();
    }

    if !workflows.is_empty() {
        println!("Workflows");
        println!("---------");
        for (name, version, step_count, step_ids) in &workflows {
            println!(
                "  {}  version={}  steps={} [{}]",
                name,
                version,
                step_count,
                step_ids.join(", ")
            );
        }
        println!();
    }
}
