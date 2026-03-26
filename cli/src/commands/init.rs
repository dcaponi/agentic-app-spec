use std::env;
use std::fs;

use crate::utils::dump_yaml;

pub fn run() {
    let root = env::current_dir().expect("Failed to get current directory");

    let dirs = ["agents", "workflows", "schemas"];

    for dir in &dirs {
        let full = root.join(dir);
        if !full.exists() {
            fs::create_dir_all(&full).expect("Failed to create directory");
            println!("  created {}/", dir);
        } else {
            println!("  exists  {}/", dir);
        }
    }

    let config_path = root.join("agentic.config.yaml");
    if !config_path.exists() {
        let config = serde_yaml::Value::Mapping({
            let mut m = serde_yaml::Mapping::new();
            m.insert(
                serde_yaml::Value::String("lang".into()),
                serde_yaml::Value::String("typescript".into()),
            );
            m.insert(
                serde_yaml::Value::String("outdir".into()),
                serde_yaml::Value::String("src/generated".into()),
            );
            m
        });
        fs::write(&config_path, dump_yaml(&config)).expect("Failed to write config");
        println!("  created agentic.config.yaml");
    } else {
        println!("  exists  agentic.config.yaml");
    }

    println!("\nProject initialised. Run `agentic add agent <id>` to add your first agent.");
}
