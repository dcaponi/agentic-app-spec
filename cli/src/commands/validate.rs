use std::collections::{HashMap, HashSet, VecDeque};
use std::path::Path;

use crate::utils::{find_project_root, load_yaml};

// ── Diagnostics ────────────────────────────────────────────────────────────

pub enum DiagLevel {
    Error,
    Warning,
}

impl DiagLevel {
    pub fn label(&self) -> &str {
        match self {
            DiagLevel::Error => "error",
            DiagLevel::Warning => "warning",
        }
    }
}

pub struct Diagnostic {
    pub level: DiagLevel,
    pub message: String,
}

const CONFIG_WHITELIST: [&str; 3] = ["model", "temperature", "image_detail"];

// ── Public entry points ────────────────────────────────────────────────────

/// Validate all workflows in the project. Returns (errors, warnings).
pub fn validate_all(root: &Path) -> (usize, usize) {
    let workflows_dir = root.join("agentic-spec").join("workflows");
    if !workflows_dir.exists() {
        return (0, 0);
    }

    let mut total_errors = 0;
    let mut total_warnings = 0;

    if let Ok(entries) = std::fs::read_dir(&workflows_dir) {
        let mut files: Vec<_> = entries.flatten().collect();
        files.sort_by_key(|e| e.file_name());

        for entry in files {
            let fname = entry.file_name().to_string_lossy().to_string();
            if !fname.ends_with(".yaml") && !fname.ends_with(".yml") {
                continue;
            }
            let data = load_yaml(&entry.path());
            let wf_name = fname
                .trim_end_matches(".yaml")
                .trim_end_matches(".yml");

            let diags = validate_workflow(&data, root);
            if diags.is_empty() {
                println!("  {} ... ok", wf_name);
                continue;
            }

            for d in &diags {
                match d.level {
                    DiagLevel::Error => {
                        total_errors += 1;
                        eprintln!("  {} [{}]: {}", wf_name, d.level.label(), d.message);
                    }
                    DiagLevel::Warning => {
                        total_warnings += 1;
                        eprintln!("  {} [{}]: {}", wf_name, d.level.label(), d.message);
                    }
                }
            }
        }
    }

    (total_errors, total_warnings)
}

/// Standalone `agentic validate` command.
pub fn run() {
    let root = find_project_root();
    println!("Validating workflows...\n");

    let (errors, warnings) = validate_all(&root);

    println!();
    if errors > 0 {
        eprintln!(
            "Validation failed: {} error(s), {} warning(s)",
            errors, warnings
        );
        std::process::exit(1);
    } else if warnings > 0 {
        println!("Validation passed with {} warning(s)", warnings);
    } else {
        println!("All workflows valid.");
    }
}

// ── Core validation ────────────────────────────────────────────────────────

pub fn validate_workflow(data: &serde_yaml::Value, root: &Path) -> Vec<Diagnostic> {
    let mut diags = Vec::new();

    // Required top-level fields
    if data.get("name").and_then(|v| v.as_str()).is_none() {
        diags.push(err("Missing required field 'name'"));
    }
    if data.get("version").and_then(|v| v.as_str()).is_none() {
        diags.push(err("Missing required field 'version'"));
    }
    if data.get("output").is_none() {
        diags.push(err("Missing required field 'output'"));
    }

    let steps = match data.get("steps").and_then(|v| v.as_sequence()) {
        Some(s) => s,
        None => {
            diags.push(err("Missing or invalid 'steps' array"));
            return diags;
        }
    };
    if steps.is_empty() {
        diags.push(err("Workflow must have at least one step"));
        return diags;
    }

    // ── Phase 1: extract step graph ──
    let mut ordered_ids: Vec<String> = Vec::new();
    let mut all_ids: HashSet<String> = HashSet::new();
    let mut edges: HashMap<String, Vec<String>> = HashMap::new();

    for entry in steps {
        extract_entry(
            entry,
            &mut ordered_ids,
            &mut all_ids,
            &mut edges,
            &mut diags,
            root,
        );
    }

    // ── Phase 2: validate next targets exist ──
    for (from, targets) in &edges {
        for target in targets {
            if target != "_end" && !all_ids.contains(target.as_str()) {
                diags.push(err(&format!(
                    "Step '{}': next target '{}' does not exist",
                    from, target
                )));
            }
        }
    }

    // ── Phase 3: reachability from first step ──
    if !ordered_ids.is_empty() {
        let reachable = compute_reachable(&ordered_ids, &edges);
        for id in &ordered_ids {
            if !reachable.contains(id.as_str()) {
                diags.push(warn(&format!("Step '{}' is unreachable", id)));
            }
        }
    }

    // ── Phase 4: validate output bindings ──
    if let Some(output) = data.get("output").and_then(|v| v.as_mapping()) {
        for (_, val) in output {
            if let Some(s) = val.as_str() {
                check_binding(s, "output", &mut diags);
            }
        }
    }

    diags
}

// ── Step extraction ────────────────────────────────────────────────────────

fn extract_entry(
    entry: &serde_yaml::Value,
    ordered_ids: &mut Vec<String>,
    all_ids: &mut HashSet<String>,
    edges: &mut HashMap<String, Vec<String>>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    if let Some(parallel) = entry.get("parallel") {
        extract_parallel(parallel, ordered_ids, all_ids, edges, diags, root);
    } else if let Some(loop_block) = entry.get("loop") {
        extract_loop(loop_block, ordered_ids, all_ids, edges, diags, root);
    } else if entry.get("for_each").is_some() {
        extract_for_each(entry, ordered_ids, all_ids, edges, diags, root);
    } else {
        extract_step(entry, ordered_ids, all_ids, edges, diags, root);
    }
}

fn extract_step(
    entry: &serde_yaml::Value,
    ordered_ids: &mut Vec<String>,
    all_ids: &mut HashSet<String>,
    edges: &mut HashMap<String, Vec<String>>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    let id = match entry.get("id").and_then(|v| v.as_str()) {
        Some(s) => s.to_string(),
        None => {
            diags.push(err("Step missing required 'id' field"));
            return;
        }
    };

    if !all_ids.insert(id.clone()) {
        diags.push(err(&format!("Duplicate step ID '{}'", id)));
    }
    ordered_ids.push(id.clone());

    // Next targets
    if let Some(next) = entry.get("next") {
        edges.insert(id.clone(), extract_next_targets(next));
    }

    // Agent / sub-workflow existence
    if let Some(agent) = entry.get("agent").and_then(|v| v.as_str()) {
        check_agent_exists(agent, &id, root, diags);
    }
    if let Some(wf) = entry.get("workflow").and_then(|v| v.as_str()) {
        check_workflow_exists(wf, &id, root, diags);
    }

    // Config whitelist
    validate_config(entry.get("config"), &id, diags);
    if let Some(fb) = entry.get("fallback") {
        validate_config(fb.get("config"), &format!("{}/fallback", id), diags);
    }

    // Input bindings
    validate_bindings(entry.get("input"), &id, diags);
}

fn extract_parallel(
    parallel: &serde_yaml::Value,
    ordered_ids: &mut Vec<String>,
    all_ids: &mut HashSet<String>,
    edges: &mut HashMap<String, Vec<String>>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    let id = match parallel.get("id").and_then(|v| v.as_str()) {
        Some(s) => s.to_string(),
        None => {
            diags.push(err("Parallel block missing required 'id' field"));
            return;
        }
    };

    if !all_ids.insert(id.clone()) {
        diags.push(err(&format!("Duplicate step ID '{}'", id)));
    }
    ordered_ids.push(id.clone());

    // Join strategy
    if let Some(join) = parallel.get("join").and_then(|v| v.as_str()) {
        if !["all", "any", "all_settled"].contains(&join) {
            diags.push(err(&format!(
                "Parallel '{}': invalid join strategy '{}' (must be all, any, or all_settled)",
                id, join
            )));
        }
    }

    // Branches
    match parallel.get("branches").and_then(|v| v.as_sequence()) {
        Some(branches) => {
            if branches.len() < 2 {
                diags.push(err(&format!(
                    "Parallel '{}': must have at least 2 branches, got {}",
                    id,
                    branches.len()
                )));
            }
            for branch in branches {
                extract_branch(branch, all_ids, diags, root);
            }
        }
        None => {
            diags.push(err(&format!(
                "Parallel '{}': missing 'branches' field",
                id
            )));
        }
    }

    // Next targets
    if let Some(next) = parallel.get("next") {
        edges.insert(id.clone(), extract_next_targets(next));
    }
}

fn extract_branch(
    branch: &serde_yaml::Value,
    all_ids: &mut HashSet<String>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    // Bare agent step within a parallel branch
    if let Some(bid) = branch.get("id").and_then(|v| v.as_str()) {
        if !all_ids.insert(bid.to_string()) {
            diags.push(err(&format!("Duplicate step ID '{}'", bid)));
        }
        if let Some(agent) = branch.get("agent").and_then(|v| v.as_str()) {
            check_agent_exists(agent, bid, root, diags);
        }
        if let Some(wf) = branch.get("workflow").and_then(|v| v.as_str()) {
            check_workflow_exists(wf, bid, root, diags);
        }
        validate_config(branch.get("config"), bid, diags);
        if let Some(fb) = branch.get("fallback") {
            validate_config(fb.get("config"), &format!("{}/fallback", bid), diags);
        }
        validate_bindings(branch.get("input"), bid, diags);
    }
    // Multi-step branch: { steps: [...] }
    if let Some(inner_steps) = branch.get("steps").and_then(|v| v.as_sequence()) {
        for step in inner_steps {
            if let Some(sid) = step.get("id").and_then(|v| v.as_str()) {
                if !all_ids.insert(sid.to_string()) {
                    diags.push(err(&format!("Duplicate step ID '{}'", sid)));
                }
                if let Some(agent) = step.get("agent").and_then(|v| v.as_str()) {
                    check_agent_exists(agent, sid, root, diags);
                }
                validate_config(step.get("config"), sid, diags);
                validate_bindings(step.get("input"), sid, diags);
            }
        }
    }
}

fn extract_loop(
    loop_block: &serde_yaml::Value,
    ordered_ids: &mut Vec<String>,
    all_ids: &mut HashSet<String>,
    edges: &mut HashMap<String, Vec<String>>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    let id = match loop_block.get("id").and_then(|v| v.as_str()) {
        Some(s) => s.to_string(),
        None => {
            diags.push(err("Loop block missing required 'id' field"));
            return;
        }
    };

    if !all_ids.insert(id.clone()) {
        diags.push(err(&format!("Duplicate step ID '{}'", id)));
    }
    ordered_ids.push(id.clone());

    // Required: max_iterations
    if loop_block
        .get("max_iterations")
        .and_then(|v| v.as_u64())
        .is_none()
    {
        diags.push(err(&format!(
            "Loop '{}': missing or invalid 'max_iterations' (required integer >= 1)",
            id
        )));
    }

    // Required: until
    if loop_block
        .get("until")
        .and_then(|v| v.as_str())
        .is_none()
    {
        diags.push(err(&format!(
            "Loop '{}': missing 'until' condition",
            id
        )));
    }

    // Inner steps
    match loop_block.get("steps").and_then(|v| v.as_sequence()) {
        Some(inner_steps) => {
            if inner_steps.is_empty() {
                diags.push(err(&format!(
                    "Loop '{}': steps array must not be empty",
                    id
                )));
            }
            for step in inner_steps {
                if let Some(sid) = step.get("id").and_then(|v| v.as_str()) {
                    all_ids.insert(sid.to_string());
                    if let Some(agent) = step.get("agent").and_then(|v| v.as_str()) {
                        check_agent_exists(agent, sid, root, diags);
                    }
                    validate_config(step.get("config"), sid, diags);
                    validate_bindings(step.get("input"), sid, diags);
                }
            }
        }
        None => {
            diags.push(err(&format!("Loop '{}': missing 'steps' array", id)));
        }
    }

    if let Some(next) = loop_block.get("next") {
        edges.insert(id.clone(), extract_next_targets(next));
    }
}

fn extract_for_each(
    entry: &serde_yaml::Value,
    ordered_ids: &mut Vec<String>,
    all_ids: &mut HashSet<String>,
    edges: &mut HashMap<String, Vec<String>>,
    diags: &mut Vec<Diagnostic>,
    root: &Path,
) {
    // Detect format: wrapped {for_each: {id, collection, ...}} vs flat {id, for_each: "$.ref", ...}
    let fe_val = entry.get("for_each").unwrap();
    let (id, inner) = if fe_val.is_mapping() {
        // Wrapped format
        match fe_val.get("id").and_then(|v| v.as_str()) {
            Some(s) => (s.to_string(), fe_val),
            None => {
                diags.push(err("for_each block missing 'id' field"));
                return;
            }
        }
    } else {
        // Flat format
        match entry.get("id").and_then(|v| v.as_str()) {
            Some(s) => (s.to_string(), entry),
            None => {
                diags.push(err("for_each step missing 'id' field"));
                return;
            }
        }
    };

    if !all_ids.insert(id.clone()) {
        diags.push(err(&format!("Duplicate step ID '{}'", id)));
    }
    ordered_ids.push(id.clone());

    if let Some(agent) = inner.get("agent").and_then(|v| v.as_str()) {
        check_agent_exists(agent, &id, root, diags);
    }
    if let Some(wf) = inner.get("workflow").and_then(|v| v.as_str()) {
        check_workflow_exists(wf, &id, root, diags);
    }
    validate_config(inner.get("config"), &id, diags);
    if let Some(fb) = inner.get("fallback") {
        validate_config(fb.get("config"), &format!("{}/fallback", id), diags);
    }
    validate_bindings(inner.get("input"), &id, diags);

    if let Some(next) = inner.get("next") {
        edges.insert(id.clone(), extract_next_targets(next));
    }
}

// ── Next-target extraction ─────────────────────────────────────────────────

fn extract_next_targets(next: &serde_yaml::Value) -> Vec<String> {
    let mut targets = Vec::new();

    if let Some(s) = next.as_str() {
        // Simple goto
        targets.push(s.to_string());
    } else if let Some(map) = next.as_mapping() {
        // switch: { switch, cases, default }
        if let Some(cases) = map.get(&y_key("cases")).and_then(|v| v.as_mapping()) {
            for (_, target) in cases {
                if let Some(t) = target.as_str() {
                    targets.push(t.to_string());
                }
            }
        }
        if let Some(default) = map.get(&y_key("default")).and_then(|v| v.as_str()) {
            targets.push(default.to_string());
        }
        // if: { if, then, else }
        if let Some(then_t) = map.get(&y_key("then")).and_then(|v| v.as_str()) {
            targets.push(then_t.to_string());
        }
        if let Some(else_t) = map.get(&y_key("else")).and_then(|v| v.as_str()) {
            targets.push(else_t.to_string());
        }
    }

    targets
}

fn y_key(s: &str) -> serde_yaml::Value {
    serde_yaml::Value::String(s.to_string())
}

// ── Reachability (BFS with fall-through) ───────────────────────────────────

fn compute_reachable(
    ordered_ids: &[String],
    edges: &HashMap<String, Vec<String>>,
) -> HashSet<String> {
    let pos: HashMap<&str, usize> = ordered_ids
        .iter()
        .enumerate()
        .map(|(i, id)| (id.as_str(), i))
        .collect();

    let mut reachable = HashSet::new();
    let mut queue = VecDeque::new();

    if let Some(first) = ordered_ids.first() {
        reachable.insert(first.clone());
        queue.push_back(first.clone());
    }

    while let Some(current) = queue.pop_front() {
        let successors: Vec<String> = if let Some(targets) = edges.get(&current) {
            // Explicit next: targets
            targets
                .iter()
                .filter(|t| t.as_str() != "_end")
                .cloned()
                .collect()
        } else {
            // Fall-through to next step in array
            match pos.get(current.as_str()) {
                Some(&idx) if idx + 1 < ordered_ids.len() => {
                    vec![ordered_ids[idx + 1].clone()]
                }
                _ => vec![],
            }
        };

        for succ in successors {
            if reachable.insert(succ.clone()) {
                queue.push_back(succ);
            }
        }
    }

    reachable
}

// ── Binding validation ─────────────────────────────────────────────────────

fn check_binding(binding: &str, context: &str, diags: &mut Vec<Diagnostic>) {
    if binding.starts_with("$.trail") {
        diags.push(err(&format!(
            "{}: binding '{}' references $.trail which is not accessible to agents",
            context, binding
        )));
    }
}

fn validate_bindings(
    input: Option<&serde_yaml::Value>,
    step_id: &str,
    diags: &mut Vec<Diagnostic>,
) {
    if let Some(map) = input.and_then(|v| v.as_mapping()) {
        for (_, val) in map {
            collect_binding_refs(val, step_id, diags);
        }
    }
}

fn collect_binding_refs(
    val: &serde_yaml::Value,
    step_id: &str,
    diags: &mut Vec<Diagnostic>,
) {
    match val {
        serde_yaml::Value::String(s) if s.starts_with("$.") => {
            check_binding(s, &format!("Step '{}'", step_id), diags);
        }
        serde_yaml::Value::Mapping(m) => {
            for (_, v) in m {
                collect_binding_refs(v, step_id, diags);
            }
        }
        serde_yaml::Value::Sequence(seq) => {
            for v in seq {
                collect_binding_refs(v, step_id, diags);
            }
        }
        _ => {}
    }
}

// ── Config whitelist ───────────────────────────────────────────────────────

fn validate_config(
    config: Option<&serde_yaml::Value>,
    step_id: &str,
    diags: &mut Vec<Diagnostic>,
) {
    if let Some(map) = config.and_then(|v| v.as_mapping()) {
        for (key, _) in map {
            if let Some(k) = key.as_str() {
                if !CONFIG_WHITELIST.contains(&k) {
                    diags.push(err(&format!(
                        "Step '{}': config key '{}' is not allowed (permitted: {})",
                        step_id,
                        k,
                        CONFIG_WHITELIST.join(", ")
                    )));
                }
            }
        }
    }
}

// ── Agent / workflow existence ──────────────────────────────────────────────

fn check_agent_exists(
    agent_id: &str,
    step_id: &str,
    root: &Path,
    diags: &mut Vec<Diagnostic>,
) {
    let agent_yaml = root
        .join("agentic-spec")
        .join("agents")
        .join(agent_id)
        .join("agent.yaml");
    if !agent_yaml.exists() {
        diags.push(warn(&format!(
            "Step '{}': agent '{}' not found at {}",
            step_id,
            agent_id,
            agent_yaml.display()
        )));
    }
}

fn check_workflow_exists(
    wf_name: &str,
    step_id: &str,
    root: &Path,
    diags: &mut Vec<Diagnostic>,
) {
    let wf_yaml = root
        .join("agentic-spec")
        .join("workflows")
        .join(format!("{}.yaml", wf_name));
    let wf_yml = root
        .join("agentic-spec")
        .join("workflows")
        .join(format!("{}.yml", wf_name));
    if !wf_yaml.exists() && !wf_yml.exists() {
        diags.push(warn(&format!(
            "Step '{}': sub-workflow '{}' not found",
            step_id, wf_name
        )));
    }
}

// ── Diagnostic constructors ────────────────────────────────────────────────

fn err(msg: &str) -> Diagnostic {
    Diagnostic {
        level: DiagLevel::Error,
        message: msg.to_string(),
    }
}

fn warn(msg: &str) -> Diagnostic {
    Diagnostic {
        level: DiagLevel::Warning,
        message: msg.to_string(),
    }
}
