use std::fs;

use crate::utils::{dump_yaml, find_project_root, load_yaml, to_snake_case};

fn is_deterministic_agent(root: &std::path::Path, agent_id: &str) -> bool {
    let yaml_path = root.join("agents").join(agent_id).join("agent.yaml");
    if !yaml_path.exists() {
        return false;
    }
    let data = load_yaml(&yaml_path);
    data.get("type")
        .and_then(|v| v.as_str())
        .map(|t| t == "deterministic")
        .unwrap_or(false)
}

fn build_step(agent_id: &str, deterministic: bool) -> serde_yaml::Value {
    let mut m = serde_yaml::Mapping::new();
    m.insert(y_str("id"), y_str(&to_snake_case(agent_id)));
    m.insert(y_str("agent"), y_str(agent_id));

    let mut input = serde_yaml::Mapping::new();
    input.insert(y_str("data"), y_str("$.input.data"));
    m.insert(y_str("input"), serde_yaml::Value::Mapping(input));

    if !deterministic {
        let mut retry = serde_yaml::Mapping::new();
        retry.insert(
            y_str("max_attempts"),
            serde_yaml::Value::Number(serde_yaml::Number::from(2_u64)),
        );
        retry.insert(
            y_str("backoff_ms"),
            serde_yaml::Value::Number(serde_yaml::Number::from(500_u64)),
        );
        m.insert(y_str("retry"), serde_yaml::Value::Mapping(retry));
    }

    serde_yaml::Value::Mapping(m)
}

pub fn run(name: &str, agents: Option<&str>) {
    let root = find_project_root();
    let workflows_dir = root.join("workflows");

    if !workflows_dir.exists() {
        fs::create_dir_all(&workflows_dir).expect("Failed to create workflows directory");
    }

    let file_path = workflows_dir.join(format!("{}.yaml", name));

    if file_path.exists() {
        eprintln!(
            "Error: workflow \"{}\" already exists at {}",
            name,
            file_path.display()
        );
        std::process::exit(1);
    }

    let agent_ids: Vec<&str> = match agents {
        Some(a) => a.split(',').map(|s| s.trim()).filter(|s| !s.is_empty()).collect(),
        None => vec![],
    };

    let steps: Vec<serde_yaml::Value> = if agent_ids.is_empty() {
        vec![build_step("placeholder-agent", false)]
    } else {
        agent_ids
            .iter()
            .map(|id| {
                let det = is_deterministic_agent(&root, id);
                build_step(id, det)
            })
            .collect()
    };

    // Build output mapping from steps
    let mut output = serde_yaml::Mapping::new();
    for step in &steps {
        if let Some(step_id) = step.get("id").and_then(|v| v.as_str()) {
            output.insert(
                y_str(step_id),
                y_str(&format!("$.steps.{}.output", step_id)),
            );
        }
    }

    let mut workflow = serde_yaml::Mapping::new();
    workflow.insert(y_str("name"), y_str(name));
    workflow.insert(y_str("description"), y_str(&format!("TODO: describe the {} workflow", name)));
    workflow.insert(y_str("version"), y_str("1.0"));

    // Add a placeholder input block
    let mut input_field = serde_yaml::Mapping::new();
    let mut data_field = serde_yaml::Mapping::new();
    data_field.insert(y_str("type"), y_str("string"));
    data_field.insert(y_str("required"), serde_yaml::Value::Bool(true));
    input_field.insert(y_str("data"), serde_yaml::Value::Mapping(data_field));
    workflow.insert(y_str("input"), serde_yaml::Value::Mapping(input_field));

    workflow.insert(
        y_str("steps"),
        serde_yaml::Value::Sequence(steps),
    );
    workflow.insert(y_str("output"), serde_yaml::Value::Mapping(output));

    let yaml_val = serde_yaml::Value::Mapping(workflow);
    fs::write(&file_path, dump_yaml(&yaml_val)).expect("Failed to write workflow YAML");
    println!("  created workflows/{}.yaml", name);

    let step_count = if agent_ids.is_empty() { 1 } else { agent_ids.len() };
    println!("\nWorkflow \"{}\" added with {} step(s).", name, step_count);
}

fn y_str(s: &str) -> serde_yaml::Value {
    serde_yaml::Value::String(s.to_string())
}
