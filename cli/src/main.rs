mod commands;
mod utils;

use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "agentic", version, about = "CLI for Agentic App Spec — scaffold agents, workflows, and generate typed handles")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Initialise a new agentic project (creates agents/, workflows/, schemas/, and config)
    Init,
    /// Add a new agent or workflow
    Add {
        #[command(subcommand)]
        target: AddTarget,
    },
    /// Generate typed integration handles from agents and workflows
    Build {
        /// Target language: typescript, python, ruby, or go
        #[arg(long)]
        lang: Option<String>,
        /// Output directory for generated code
        #[arg(long)]
        outdir: Option<String>,
    },
    /// List all discovered agents and workflows
    List,
    /// Validate workflow YAML files (graph structure, targets, config whitelist)
    Validate,
}

#[derive(Subcommand)]
enum AddTarget {
    /// Scaffold a new agent
    Agent {
        /// Agent identifier (kebab-case)
        id: String,
        /// Agent type: llm or deterministic
        #[arg(long, default_value = "llm")]
        r#type: String,
        /// Model to use (LLM agents only)
        #[arg(long, default_value = "gpt-4.1")]
        model: String,
        /// Input modality: text or image
        #[arg(long, default_value = "text")]
        input_type: String,
    },
    /// Scaffold a new workflow
    Workflow {
        /// Workflow name
        name: String,
        /// Comma-separated agent IDs to include as steps
        #[arg(long)]
        agents: Option<String>,
    },
}

fn main() {
    let cli = Cli::parse();

    match cli.command {
        Commands::Init => commands::init::run(),
        Commands::Add { target } => match target {
            AddTarget::Agent {
                id,
                r#type,
                model,
                input_type,
            } => {
                if r#type != "llm" && r#type != "deterministic" {
                    eprintln!("Error: --type must be \"llm\" or \"deterministic\", got \"{}\"", r#type);
                    std::process::exit(1);
                }
                if input_type != "text" && input_type != "image" {
                    eprintln!("Error: --input-type must be \"text\" or \"image\", got \"{}\"", input_type);
                    std::process::exit(1);
                }
                commands::add_agent::run(&id, &r#type, &model, &input_type);
            }
            AddTarget::Workflow { name, agents } => {
                commands::add_workflow::run(&name, agents.as_deref());
            }
        },
        Commands::Build { lang, outdir } => {
            commands::build::run(lang.as_deref(), outdir.as_deref());
        }
        Commands::List => commands::list::run(),
        Commands::Validate => commands::validate::run(),
    }
}
