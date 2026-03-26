use std::env;
use std::fs;
use std::path::{Path, PathBuf};

// ── Case conversions ────────────────────────────────────────────────────────

/// kebab-case or snake_case -> camelCase
pub fn to_camel_case(s: &str) -> String {
    let mut result = String::new();
    let mut upper_next = false;
    for (i, c) in s.chars().enumerate() {
        if c == '-' || c == '_' {
            upper_next = true;
        } else if upper_next {
            result.push(c.to_ascii_uppercase());
            upper_next = false;
        } else if i == 0 {
            result.push(c.to_ascii_lowercase());
        } else {
            result.push(c);
        }
    }
    result
}

/// kebab-case or snake_case -> PascalCase
pub fn to_pascal_case(s: &str) -> String {
    let camel = to_camel_case(s);
    let mut chars = camel.chars();
    match chars.next() {
        None => String::new(),
        Some(c) => c.to_ascii_uppercase().to_string() + chars.as_str(),
    }
}

/// kebab-case -> snake_case
pub fn to_snake_case(s: &str) -> String {
    s.replace('-', "_")
}

/// kebab-case or snake_case -> PascalCase (Go public identifier)
pub fn to_go_public(s: &str) -> String {
    to_pascal_case(s)
}

// ── Type mapping ────────────────────────────────────────────────────────────

pub fn yaml_type_to_ts(t: &str) -> &str {
    match t {
        "base64" | "string" => "string",
        "number" => "number",
        "boolean" => "boolean",
        "object" => "Record<string, unknown>",
        _ => "unknown",
    }
}

pub fn yaml_type_to_python(t: &str) -> &str {
    match t {
        "base64" | "string" => "str",
        "number" => "float",
        "boolean" => "bool",
        "object" => "dict[str, Any]",
        _ => "Any",
    }
}

pub fn yaml_type_to_ruby(t: &str) -> &str {
    match t {
        "base64" | "string" => "String",
        "number" => "Numeric",
        "boolean" => "Boolean (TrueClass)",
        "object" => "Hash",
        _ => "Object",
    }
}

pub fn yaml_type_to_go(t: &str) -> &str {
    match t {
        "base64" | "string" => "string",
        "number" => "float64",
        "boolean" => "bool",
        "object" => "map[string]interface{}",
        _ => "interface{}",
    }
}

// ── YAML helpers ────────────────────────────────────────────────────────────

pub fn load_yaml(path: &Path) -> serde_yaml::Value {
    let raw = fs::read_to_string(path)
        .unwrap_or_else(|e| panic!("Failed to read {}: {}", path.display(), e));
    serde_yaml::from_str(&raw)
        .unwrap_or_else(|e| panic!("Failed to parse YAML {}: {}", path.display(), e))
}

pub fn dump_yaml(value: &serde_yaml::Value) -> String {
    serde_yaml::to_string(value).expect("Failed to serialize YAML")
}

// ── Project root discovery ──────────────────────────────────────────────────

/// Walk up from `cwd` looking for `agentic.config.yaml` or an `agents/`
/// directory. Returns the first matching ancestor, or falls back to `cwd`.
pub fn find_project_root() -> PathBuf {
    let cwd = env::current_dir().expect("Failed to get current directory");
    let mut dir = cwd.clone();

    loop {
        if dir.join("agentic.config.yaml").exists() || dir.join("agents").exists() {
            return dir;
        }
        match dir.parent() {
            Some(parent) => dir = parent.to_path_buf(),
            None => return cwd,
        }
    }
}
