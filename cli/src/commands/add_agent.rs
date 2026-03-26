use std::fs;

use crate::utils::{dump_yaml, find_project_root, to_pascal_case, to_snake_case};

fn build_llm_agent_yaml(id: &str, model: &str, input_type: &str) -> serde_yaml::Value {
    let mut m = serde_yaml::Mapping::new();
    m.insert(y_str("name"), y_str(&to_pascal_case(id)));
    m.insert(y_str("description"), y_str(&format!("TODO: describe the {} agent", to_pascal_case(id))));
    m.insert(y_str("type"), y_str("llm"));
    m.insert(y_str("model"), y_str(model));
    m.insert(
        y_str("temperature"),
        serde_yaml::Value::Number(serde_yaml::Number::from(0.7_f64)),
    );
    m.insert(y_str("input_type"), y_str(input_type));
    m.insert(y_str("schema"), serde_yaml::Value::Null);
    m.insert(
        y_str("user_message"),
        y_str(&format!("Provide input for {}", to_pascal_case(id))),
    );

    let data_type = if input_type == "image" { "base64" } else { "string" };
    let mut data_field = serde_yaml::Mapping::new();
    data_field.insert(y_str("type"), y_str(data_type));
    data_field.insert(y_str("required"), serde_yaml::Value::Bool(true));
    data_field.insert(
        y_str("description"),
        y_str(&format!("Primary input for the {} agent", to_pascal_case(id))),
    );

    let mut input = serde_yaml::Mapping::new();
    input.insert(y_str("data"), serde_yaml::Value::Mapping(data_field));
    m.insert(y_str("input"), serde_yaml::Value::Mapping(input));

    serde_yaml::Value::Mapping(m)
}

fn build_deterministic_agent_yaml(id: &str) -> serde_yaml::Value {
    let mut m = serde_yaml::Mapping::new();
    m.insert(y_str("name"), y_str(&to_pascal_case(id)));
    m.insert(y_str("description"), y_str(&format!("TODO: describe the {} agent", to_pascal_case(id))));
    m.insert(y_str("type"), y_str("deterministic"));
    m.insert(y_str("handler"), y_str(&to_snake_case(id)));

    let mut data_field = serde_yaml::Mapping::new();
    data_field.insert(y_str("type"), y_str("string"));
    data_field.insert(y_str("required"), serde_yaml::Value::Bool(true));
    data_field.insert(
        y_str("description"),
        y_str(&format!("Primary input for the {} agent", to_pascal_case(id))),
    );

    let mut input = serde_yaml::Mapping::new();
    input.insert(y_str("data"), serde_yaml::Value::Mapping(data_field));
    m.insert(y_str("input"), serde_yaml::Value::Mapping(input));

    serde_yaml::Value::Mapping(m)
}

fn build_prompt_md(id: &str) -> String {
    let pascal = to_pascal_case(id);
    format!(
        "# {} — System Prompt\n\
         \n\
         You are the {} agent.\n\
         \n\
         ## Instructions\n\
         \n\
         Describe the agent's purpose and behaviour here.\n\
         \n\
         ## Constraints\n\
         \n\
         - Keep responses concise.\n\
         - Follow the output schema when one is provided.\n",
        pascal, pascal
    )
}

pub fn run(id: &str, agent_type: &str, model: &str, input_type: &str) {
    let root = find_project_root();
    let agent_dir = root.join("agents").join(id);

    if agent_dir.exists() {
        eprintln!("Error: agent \"{}\" already exists at {}", id, agent_dir.display());
        std::process::exit(1);
    }

    fs::create_dir_all(&agent_dir).expect("Failed to create agent directory");

    if agent_type == "llm" {
        let yaml = build_llm_agent_yaml(id, model, input_type);
        fs::write(agent_dir.join("agent.yaml"), dump_yaml(&yaml)).expect("Failed to write agent.yaml");
        fs::write(agent_dir.join("prompt.md"), build_prompt_md(id)).expect("Failed to write prompt.md");
        println!("  created agents/{}/agent.yaml", id);
        println!("  created agents/{}/prompt.md", id);
    } else {
        let yaml = build_deterministic_agent_yaml(id);
        fs::write(agent_dir.join("agent.yaml"), dump_yaml(&yaml)).expect("Failed to write agent.yaml");
        println!("  created agents/{}/agent.yaml", id);
    }

    println!("\nAgent \"{}\" added ({}).", id, agent_type);
}

fn y_str(s: &str) -> serde_yaml::Value {
    serde_yaml::Value::String(s.to_string())
}
