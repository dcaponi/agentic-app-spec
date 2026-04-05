# Agentic App Spec

A file-tree based specification for defining AI agent pipelines and multi-agent workflows. Agents and workflows are declared in YAML, system prompts live in Markdown, and a build step generates typed function handles in your target language. Think of it as **protobuf for AI agent orchestration**.

---

## Quick Start

### Install the CLI

```bash
curl -fsSL https://raw.githubusercontent.com/dcaponi/agentic-app-spec/main/scripts/install.sh | bash
```

Or build from source:

```bash
cd cli && cargo build --release
cp target/release/agentic /usr/local/bin/
```

### Install the runtime for your language

```bash
# TypeScript
npm install agentic-engine

# Python
pip install "agentic-engine @ git+https://github.com/dcaponi/agentic-app-spec.git#subdirectory=runtime/python"

# Go
go get github.com/dcaponi/agentic-app-spec/runtime/go@latest
```

Ruby — in your Gemfile:

```ruby
gem "agentic_engine", git: "https://github.com/dcaponi/agentic-app-spec.git", glob: "runtime/ruby/*.gemspec"
```

### Scaffold and build

```bash
agentic init
agentic add agent my-agent --type llm
agentic add workflow my-pipeline --agents my-agent
agentic build --lang typescript   # or python, ruby, go
```

For a complete walkthrough building a real project from scratch, see [Building a Go API with Agentic App Spec](blog/building-a-go-api-with-agentic-app-spec.md). The same pattern applies to all supported languages.

---

## Table of Contents

- [Quick Start](#quick-start)
- [Project Structure](#project-structure)
- [Agent Definition](#agent-definition)
  - [agent.yaml Reference](#agentyaml-reference)
  - [System Prompt (prompt.md)](#system-prompt-promptmd)
  - [Template Interpolation](#template-interpolation)
- [Workflow Definition](#workflow-definition)
  - [workflow.yaml Reference](#workflowyaml-reference)
  - [Step Types](#step-types)
  - [Control Flow](#control-flow)
  - [Binding Syntax](#binding-syntax)
  - [requires: Dependency Validation](#requires-dependency-validation)
  - [Execution Model](#execution-model)
- [Trail Contract](#trail-contract)
- [Response Envelope](#response-envelope)
- [Schemas](#schemas)
- [Code Generation (Build Step)](#code-generation-build-step)
- [Build-Time Validation](#build-time-validation)
- [CLI](#cli)
- [Language Support](#language-support)
  - [TypeScript](#typescript)
  - [Python](#python)
  - [Ruby](#ruby)
  - [Go](#go)
- [Example: Product Review Pipeline](#example-product-review-pipeline)
- [Example: Grocery Classification (switch)](#example-grocery-classification-switch)
- [Example: Iterative Refinement (loop)](#example-iterative-refinement-loop)
- [Example: Batch Analysis (for_each)](#example-batch-analysis-for_each)
- [Future: Agent-Level Tool Use](#future-agent-level-tool-use)
- [Architecture Decisions](#architecture-decisions)
- [Best Practices](#best-practices)
- [Glossary](#glossary)

---

## Project Structure

```
project-root/
├── agentic-spec/
│   ├── agents/
│   │   └── <agent-id>/
│   │       ├── agent.yaml        # Agent configuration
│   │       └── prompt.md          # System prompt (LLM agents only)
│   ├── workflows/
│   │   └── <workflow-name>.yaml   # Workflow orchestration definition
│   └── schemas/                   # (optional) Zod/JSON schemas for structured output
├── src/
│   └── generated/                 # Output from agentic build
│       ├── agents/
│       └── workflows/
├── agentic.config.yaml            # (optional) Project-level config
└── package.json
```

### Directory Conventions

- **`agentic-spec/agents/`** — Each subdirectory is an agent. The directory name is the **agent ID** and must be unique across the project. Use kebab-case (e.g., `review-analyzer`, `product-fetcher`).
- **`agentic-spec/workflows/`** — Each YAML file is a workflow. The filename (minus extension) is the **workflow name**.
- **`agentic-spec/schemas/`** — Optional directory for Zod or JSON Schema definitions. These are registered by name and referenced from agent configurations.
- **`agentic.config.yaml`** — Optional project-level configuration (default model, default temperature, schema registry path, etc.).

---

## Agent Definition

An agent is a single unit of work. It is either an **LLM agent** (calls a language model) or a **deterministic agent** (runs a handler function with no LLM involved).

Each agent lives in its own directory under `agentic-spec/agents/`:

```
agentic-spec/agents/
└── review-analyzer/
    ├── agent.yaml
    └── prompt.md
```

### agent.yaml Reference

Below is the complete field reference for `agent.yaml`. Fields marked **(LLM only)** or **(deterministic only)** apply only to that agent type. All other fields are shared.

```yaml
# ---------- Required fields ----------

name: string
# Human-readable display name.
# Example: "Review Analyzer"

description: string
# Plain-language description of what this agent does.
# Example: "Analyzes product reviews for sentiment, pros/cons, and common themes"

type: 'llm' | 'deterministic'
# Execution type.
#   llm           — sends a prompt to a language model
#   deterministic — runs a registered handler function (no LLM call)

# ---------- LLM-specific fields (type: llm) ----------

model: string
# Model identifier passed to the provider.
# Examples: "gpt-4.1", "gpt-4.1-mini", "claude-sonnet-4-5-20241022"

temperature: number
# Sampling temperature, range 0 to 2.
# Lower values (0 - 0.3) produce more deterministic output — use for structured
# tasks like extraction, classification, and scoring.
# Higher values (0.7 - 1.2) produce more creative output — use for writing,
# brainstorming, and open-ended generation.
# Default recommendation: 0.1 for structured tasks, 0.7 for creative tasks.

input_type: 'image' | 'text'
# The modality of the primary input.
# Default: 'text'
# When set to 'image', the runtime sends the input as a vision message
# (base64-encoded image or image URL).

image_detail: 'low' | 'high' | 'auto'
# Vision detail level. Only applicable when input_type is 'image'.
#   low  — faster, cheaper, lower fidelity (good for simple images)
#   high — slower, more expensive, higher fidelity (good for detailed images)
#   auto — let the model decide
# Default: 'auto'

schema: string | null
# Reference to a registered schema name for structured output.
# When set to a string, the runtime uses structured output mode
# (e.g., OpenAI's zodResponseFormat) to force the model to conform
# to the schema.
# When set to null, the runtime uses JSON mode — the model is asked
# to return valid JSON but is not constrained to a specific shape.
# When omitted entirely, the model returns plain text.

user_message: string
# User prompt template. Supports {{key}} interpolation (see "Template
# Interpolation" below). This is sent as the user message in the LLM
# call, alongside the system prompt from prompt.md.
# Example: "Analyze the following reviews for {{product_name}}:\n\n{{reviews}}"

# ---------- Deterministic-specific fields (type: deterministic) ----------

handler: string
# The name of a registered handler function.
# At runtime, the engine looks up this name in a handler registry and
# calls the function with the resolved input.
# Example: "scoring", "product-fetcher", "calculate-nutrition"

# ---------- Shared fields ----------

base_url: string
# API endpoint URL. When set, uses an OpenAI-compatible client pointed at this URL.

api_key_env: string
# Name of the environment variable holding the API key (e.g., DEEPSEEK_API_KEY).

input:
  <param_name>:
    type: 'base64' | 'string' | 'number' | 'boolean' | 'object'
    # The data type of this input parameter.
    #   base64   — base64-encoded binary data (maps to string in code)
    #   string   — UTF-8 text
    #   number   — numeric value (integer or float)
    #   boolean  — true or false
    #   object   — arbitrary key-value data (maps to Record<string, unknown>
    #              in TypeScript, dict[str, Any] in Python, etc.)

    required: boolean
    # Whether this parameter must be provided. Default: true.
    # If a required parameter is missing at runtime, the step fails
    # immediately without executing the agent.
```

#### Complete LLM Agent Example

```yaml
name: Review Analyzer
description: Analyzes product reviews for sentiment, pros/cons, and common themes
type: llm
model: gpt-4.1
temperature: 0.2
schema: ReviewAnalysis
user_message: |
  Analyze the following product reviews for "{{product_name}}" (category: {{category}}).

  Product description: {{description}}

  Reviews:
  {{reviews}}

  Provide a thorough analysis including overall sentiment, key pros and cons,
  and common themes across reviews.
input:
  product_name:
    type: string
    required: true
  category:
    type: string
    required: true
  description:
    type: string
    required: true
  reviews:
    type: string
    required: true
```

#### Complete Deterministic Agent Example

```yaml
name: Product Fetcher
description: Fetches product data and reviews from the DummyJSON API
type: deterministic
handler: product-fetcher
input:
  product_id:
    type: number
    required: true
```

### System Prompt (prompt.md)

For **LLM agents**, the system prompt lives in a file called `prompt.md` in the same directory as `agent.yaml`. This file contains the system message that is sent to the model before the user message.

```
agentic-spec/agents/
└── review-analyzer/
    ├── agent.yaml
    └── prompt.md    <-- system prompt
```

The system prompt is plain Markdown. It can be as long or as short as needed. The runtime reads this file at agent load time and sends it as the `system` role message in the LLM call.

**Deterministic agents do not need a `prompt.md` file.** If one is present, it is ignored.

#### Example prompt.md

```markdown
You are an expert product review analyst. Your job is to analyze customer reviews
and extract structured insights.

## Guidelines

- Identify the overall sentiment (positive, negative, mixed, neutral)
- Extract specific pros and cons mentioned by reviewers
- Identify common themes that appear across multiple reviews
- Note any outlier opinions that differ from the consensus
- Be objective — report what reviewers said, do not inject your own opinion

## Output Format

Return a structured analysis with clear sections for sentiment, pros, cons,
themes, and a summary.
```

### Template Interpolation

The `user_message` field in `agent.yaml` supports **template interpolation** using double-curly-brace syntax: `{{key}}`.

#### How It Works

1. At runtime, just before the agent executes, the engine resolves the agent's input bindings.
2. Each `{{key}}` placeholder in `user_message` is replaced with the corresponding value from the resolved inputs.
3. The fully interpolated string is sent as the user message in the LLM call.

#### Syntax


| Pattern            | Description                                                      |
| ------------------ | ---------------------------------------------------------------- |
| `{{key}}`          | Replaced with the value of input parameter `key`                 |
| `{{key.sub}}`      | Replaced with the nested property `sub` of input parameter `key` |
| `{{key.sub.deep}}` | Supports arbitrarily deep nested paths                           |


#### Behavior

- **Resolved values** are converted to strings. Objects are JSON-stringified.
- **Unresolved placeholders** are left as-is. If `{{missing_key}}` cannot be resolved, the literal text `{{missing_key}}` remains in the message. This allows for optional interpolation without causing errors.
- **Nested paths** use dot notation. Given an input `product_data` that is an object `{ name: "Widget", price: 9.99 }`, the placeholder `{{product_data.name}}` resolves to `"Widget"`.

#### Example

Given this `user_message`:

```yaml
user_message: |
  Compare "{{predicted.meal_title}}" with "{{reference.meal_title}}".

  Predicted nutrients: {{predicted.nutrients}}
  Reference nutrients: {{reference.nutrients}}
```

And these resolved inputs:

```json
{
  "predicted": { "meal_title": "Grilled Chicken Salad", "nutrients": { "calories": 350 } },
  "reference": { "meal_title": "Caesar Salad", "nutrients": { "calories": 400 } }
}
```

The interpolated user message becomes:

```
Compare "Grilled Chicken Salad" with "Caesar Salad".

Predicted nutrients: {"calories":350}
Reference nutrients: {"calories":400}
```

---

## Workflow Definition

A workflow chains agents together into a directed graph. It declares the execution order, data flow between steps, control flow (branching, loops, fan-out), retry/fallback logic, and the final output shape.

Each workflow is a single YAML file in the `agentic-spec/workflows/` directory:

```
agentic-spec/workflows/
└── product-review.yaml
```

### Design Principle

**Agents compute, workflows orchestrate.** Anything that decides "what happens next" belongs in the workflow. Anything that decides "how to produce an answer" belongs in the agent.

### workflow.yaml Reference

```yaml
# ---------- Metadata ----------

name: string
# Human-readable workflow name.

description: string
# Plain-language description of the workflow's purpose.

version: string
# Semantic version string for this workflow definition.

# ---------- Inputs ----------

input:
  <param_name>:
    type: 'base64' | 'string' | 'number' | 'boolean' | 'object'
    required: boolean

# ---------- Steps ----------

steps:
  # Steps form a directed graph. By default, they execute top-to-bottom
  # (each step falls through to the next in the array). Use next: to
  # override control flow — branching, jumping, or terminating early.
  #
  # Step types: agent step, workflow step, parallel block, loop block, for_each step.
  # See "Step Types" below.

# ---------- Output ----------

output:
  <key>: <binding>
  # Workflow output bindings. Maps named output keys to binding expressions
  # that reference step outputs or workflow inputs.
  # These are resolved after execution completes and become the `result`
  # field in the response envelope.
```

### Step Types

#### Agent Step

Invokes a single agent.

```yaml
- id: string
  agent: string               # Agent ID (folder name in agentic-spec/agents/)
  input:
    <param>: <binding>        # Input bindings (see "Binding Syntax")

  # --- Optional fields ---

  config:                     # Override agent config for this step
    model: string             #   Whitelisted: model, temperature, image_detail
    temperature: number
    image_detail: string

  retry:                      # Retry on transient failure
    max_attempts: number      #   Total attempts including first try
    backoff_ms: number        #   Base delay (multiplied by attempt number)

  fallback:                   # Fallback agent if all retries fail
    agent: string
    config:
      model: string

  requires:                   # Explicit binding dependencies (see "requires:")
    - $.steps.<id>.output.<path>

  next: <target>              # Control flow (see "Control Flow")
```

#### Workflow Step

Invokes a sub-workflow. The sub-workflow's envelope embeds in the parent step result under `sub_envelope`. Sub-workflow cycles are detected at build time.

```yaml
- id: string
  workflow: string            # Workflow name (filename in agentic-spec/workflows/)
  input:
    <param>: <binding>
  retry: { ... }
  fallback: { ... }
  requires: [ ... ]
  next: <target>
```

The step's `output` is the sub-workflow's resolved `result` (the `output` bindings from the child workflow). The full child envelope (with its own trail, steps, and metrics) is available as `sub_envelope` on the step result for debugging.

#### Parallel Block

Runs independent branches concurrently with a join strategy.

```yaml
- parallel:
    id: string                # Unique ID (used as next: target)
    join: all | any | all_settled   # Default: all
    branches:
      - steps:                # Branch 1: multi-step sequence
          - id: analyze
            agent: review-analyzer
            input: { ... }
          - id: deep-analyze
            agent: deep-analyzer
            input:
              initial: $.steps.analyze.output

      - steps:                # Branch 2
          - id: compare
            agent: comparison-researcher
            input: { ... }

      - id: quick-check       # Sugar: bare step = single-step branch
        agent: quick-checker
        input: { ... }

    next: <target>            # Optional; falls through by default
```

**Rules:**
- Each branch is a `steps` array (a mini-workflow). A bare agent step is sugar for a single-step branch.
- Branches execute concurrently. The join strategy determines when the block completes.
- Within a branch, steps are sequential and can reference earlier steps in the same branch.
- Cross-branch references are prohibited (enforced at build time).
- `join: all` — wait for every branch to complete (default).
- `join: any` — first branch to complete wins; remaining branches are cancelled.
- `join: all_settled` — wait for all branches, tolerate individual branch failures.

#### Loop Block

Bounded iteration: repeats a set of steps until a condition is met or the iteration cap is reached.

```yaml
- loop:
    id: string                # Unique ID
    max_iterations: number    # Hard cap (required, prevents runaway loops)
    until: <binding>          # Exit condition, evaluated after each iteration
    steps:
      - id: draft
        agent: writer
        input:
          previous: $.steps.draft.output      # null on first iteration
          feedback: $.steps.review.output.feedback
      - id: review
        agent: reviewer
        input:
          draft: $.steps.draft.output
    next: <target>            # Optional
```

**Rules:**
- `until` is a binding expression evaluated after each complete iteration. Loop exits when the value is truthy.
- Steps within the loop can reference their own prior outputs (overwritten each iteration). On the first iteration, prior-output references resolve to `null`.
- `max_iterations` is required. There is no unbounded loop.
- The loop's `output` is the final iteration's step outputs.
- The trail emits one entry per iteration (see [Trail Contract](#trail-contract)).

#### for_each Step

Dynamic fan-out: invokes an agent once per element in a runtime-determined array.

```yaml
- id: string
  for_each: <binding>         # Binding to an array
  as: string                  # Loop variable name
  agent: string               # Agent to invoke per element
  input:
    data: "{{item}}"          # Loop variable available via interpolation
    context: $.steps.prior.output   # Other bindings still work
  max_concurrency: number     # Optional; omit = all in parallel
  config: { ... }
  retry: { ... }              # Per iteration, not the step as a whole
  fallback: { ... }           # Per iteration
  requires: [ ... ]
  next: <target>
```

**Rules:**
- The orchestrator resolves the `for_each` binding to an array, then invokes the agent once per element.
- If `max_concurrency` is set, runs that many concurrently with a rolling window. If omitted, all run in parallel.
- Empty array produces `output: []` with status `success`.
- The step's `output` is an array of per-iteration results in input-array order.
- Downstream bindings use array indexing: `$.steps.<id>.output[0].field` or `$.steps.<id>.output` for the whole array.
- Retry and fallback apply per iteration, not to the step as a whole.
- If some iterations fail after retries, those slots contain error sentinels (`{ error: "..." }`) and the step's status is `partial_failure`.
- The trail emits exactly two entries: `for_each_started` and `for_each_completed` (aggregate-only; see [Trail Contract](#trail-contract)).

### Control Flow

Every step has an optional `next:` field that determines which step executes afterward. When omitted, the default is **fall-through** to the next step in the `steps` array. This means simple linear workflows need no `next:` at all.

#### Goto

Jump to a specific step by ID:

```yaml
- id: validate
  agent: validator
  input: { ... }
  next: summarize             # Jump to the step with id: summarize
```

#### switch (value-based branching)

Branch on a field in the step's output:

```yaml
- id: classify
  agent: food-classifier
  input:
    item_name: $.input.item_name
  next:
    switch: output.category
    cases:
      food: handle-food
      non_food: handle-non-food
    default: reject           # Required — prevents non-exhaustive branches
```

`switch` evaluates a dot-path expression against the step's output and jumps to the step ID matching the value. The `default` case is **required** — `agentic build` rejects workflows with non-exhaustive switch blocks.

#### if (binary branching)

Branch on a boolean condition:

```yaml
- id: fetch
  agent: product-fetcher
  input:
    product_id: $.input.product_id
  next:
    if: output.found
    then: analyze
    else: not-found
```

Both `then` and `else` are **required** — every branch must be accounted for.

#### Backward Edges

`next:` can reference a step that appears earlier in the `steps` array, creating a cycle. This is the escape hatch for loop patterns that don't fit the `loop:` block. Backward edges must be detectable and bounded — `agentic build` performs cycle analysis and warns on unbounded cycles (those without a conditional exit).

### Binding Syntax

Bindings are path expressions that tell the orchestrator where to find data at runtime. They use a `$.` prefix followed by a dot-separated path.

#### Binding Types


| Pattern                               | Description                                             | Example                                  |
| ------------------------------------- | ------------------------------------------------------- | ---------------------------------------- |
| `$.input.<key>`                       | Reference a workflow input parameter                    | `$.input.product_id`                     |
| `$.steps.<step_id>.output`            | Full output object of a completed step                  | `$.steps.fetch.output`                   |
| `$.steps.<step_id>.output.<path>`     | Nested field within a step's output                     | `$.steps.fetch.output.title`             |
| `$.steps.<step_id>.output[<n>]`       | Array index (for `for_each` outputs)                    | `$.steps.analyze-each.output[0].score`   |
| `$.steps.<step_id>.output[<n>].<path>`| Nested field within an indexed array element            | `$.steps.analyze-each.output[2].summary` |
| Literal value                         | Strings, numbers, booleans, objects pass through as-is  | `"default-category"`, `42`, `true`       |


#### Deep Nesting

Binding paths support arbitrarily deep nesting with dot notation:

```yaml
input:
  meal_title: $.steps.predictor.output.predicted.meal_title
  calories: $.steps.predictor.output.predicted.nutrients.calories
```

#### Resolution Behavior

- Bindings are resolved **at runtime**, immediately before the step executes.
- The orchestrator maintains an **execution context** that accumulates outputs as steps complete.
- If a binding resolves to `undefined` (e.g., the referenced step has not run yet, or the path does not exist in the output), a **warning is logged** but execution continues. The value is passed as `undefined` to the agent.
- Literal values (strings, numbers, booleans, objects, arrays) are **not** interpreted as binding paths. Only strings starting with `$.` are treated as bindings.

#### Trail Isolation

Bindings starting with `$.trail` are **rejected at build time**. The trail is a rich event log intended for envelope consumers (debuggers, visualization tools, audit systems). It is not part of the execution context that feeds agent inputs.

Allowing `$.trail` bindings would cause two problems:
1. **Context bloat.** The entire execution history would be injected into agent prompts, drowning the signal the agent needs in noise.
2. **Abstraction violation.** Agents would become entangled with workflow topology and execution metadata, breaking their reusability.

Build error: `"$.trail bindings are not permitted. The trail is for envelope consumers only, not agent inputs."`

### requires: Dependency Validation

The optional `requires:` field on a step lists the binding paths it depends on:

```yaml
- id: summarize
  agent: summarizer
  requires:
    - $.steps.classify.output.type
    - $.steps.fetch.output.content
  input:
    category: $.steps.classify.output.type
    body: $.steps.fetch.output.content
```

**Purpose:** In a flat graph with `next:` routing, a step can be reached by multiple paths. Some paths might not have executed the upstream steps the current step needs. `requires:` enables build-time validation that every path into a step satisfies its dependencies.

**Behavior:**
- `agentic build` walks the workflow graph from entry to every reachable step. For each path into a step, it verifies every `requires:` entry corresponds to an upstream step that will have executed on that path.
- Build fails with a clear error if any path violates this: `"path classify-skip → summarize does not satisfy $.steps.classify.output.type."`
- **If omitted:** `requires:` is inferred from the step's `input:` bindings — every `$.steps.X.output.*` becomes an inferred requirement. Users only need to write `requires:` explicitly when they want to document dependencies not visible in `input:` (rare) or override inference.

### Execution Model

The orchestrator processes a workflow as a directed graph:

1. **Input validation.** Check that all required workflow inputs are present and correctly typed. If validation fails, return immediately with status `error`.

2. **Graph traversal.** Start at the first step in the `steps` array. After each step completes, determine the next step:
   - If the step has `next:`, evaluate it (goto, switch, or if) to determine the target.
   - If no `next:`, fall through to the next step in the array.
   - If there is no next step (end of array or explicit termination), proceed to output resolution.

3. **Context accumulation.** After each step completes, its output is stored at `$.steps.<step_id>.output`. This makes it available to subsequent steps via bindings.

4. **Parallel blocks.** When the orchestrator encounters a `parallel` block, it launches all branches simultaneously. The join strategy determines when the block completes:
   - `all` — wait for every branch (default). If any branch fails, the block fails.
   - `any` — first branch to complete wins. Remaining branches are cancelled.
   - `all_settled` — wait for all branches, tolerate failures. Failed branches are recorded but don't halt the workflow.
   - Steps within a branch can reference earlier steps in the same branch but **cannot** reference steps in sibling branches.

5. **Loop blocks.** Execute the contained steps sequentially, then evaluate the `until` condition. If truthy, exit the loop. If falsy and `max_iterations` not reached, repeat. Step outputs are overwritten each iteration (prior values available via the same binding paths).

6. **for_each steps.** Resolve the `for_each` binding to an array, then invoke the agent once per element. Respect `max_concurrency` if set. Collect results in input-array order.

7. **Retry.** If a step fails and has a `retry` configuration:
   - Retry up to `max_attempts` total times (including the initial attempt).
   - Wait `backoff_ms * attempt_number` milliseconds between attempts.
   - Each retry re-resolves input bindings.

8. **Fallback.** If all retry attempts are exhausted and the step has a `fallback` configuration:
   - Load the fallback agent with optional config overrides.
   - Execute **once** with the same resolved inputs.
   - If the fallback succeeds, mark with `used_fallback: true` and `fallback_reason` (the error from the last retry attempt).
   - If the fallback also fails, the step is marked `error`.

9. **Failure.** If a step fails after all retries and fallback (or has none configured), the workflow halts immediately. Status is set to `error`. The partial trail up to the failure point is preserved in the envelope (see [Partial Trail on Crash](#partial-trail-on-crash)).

10. **Output resolution.** After execution completes, resolve the `output` bindings. These become the `result` field in the response envelope. Steps that were not reached (due to branching) have status `not_executed` and no output.

---

## Trail Contract

The trail is an ordered event log that records every significant execution event during a workflow run. It exists for **envelope consumers** — debuggers, visualization tools, audit systems, cost trackers — not for agents. Agents receive their inputs via explicit bindings; the trail is invisible to them (see [Trail Isolation](#trail-isolation)).

### Trail Entry Structure

Each trail entry is an object with:

```typescript
{
  type: string;                   // Event type (see below)
  step_id?: string;               // Step that emitted this event (if applicable)
  timestamp: string;              // ISO 8601 timestamp
  details: Record<string, unknown>; // Type-specific payload
}
```

### Trail Event Types

| Event Type            | Emitted When                               | Details                                                                |
| --------------------- | ------------------------------------------ | ---------------------------------------------------------------------- |
| `step_started`        | Agent or workflow step begins              | `{ agent: string }` or `{ workflow: string }`                          |
| `step_completed`      | Agent or workflow step finishes            | `{ status, latency_ms, attempts?, used_fallback?, fallback_reason? }`  |
| `branch_evaluated`    | A `switch` or `if` condition is evaluated  | `{ expression: string, result: unknown, target: string }`              |
| `parallel_started`    | Parallel block begins                      | `{ branch_count: number, join: string }`                               |
| `parallel_completed`  | Parallel block finishes                    | `{ completed_count: number, join: string }`                            |
| `loop_iteration`      | One iteration of a loop block completes    | `{ iteration: number, until_value: unknown }`                          |
| `loop_completed`      | Loop block exits                           | `{ iterations: number, exit_reason: 'until_met' \| 'max_iterations' }`|
| `for_each_started`    | for_each step begins                       | `{ input_count: number, max_concurrency: number \| null }`            |
| `for_each_completed`  | for_each step finishes                     | `{ success_count, error_count, error_summary: string \| null, latency_ms }` |
| `sub_workflow_entered` | Sub-workflow invocation begins            | `{ workflow: string }`                                                 |
| `sub_workflow_exited`  | Sub-workflow invocation finishes          | `{ workflow: string, status: string }`                                 |
| `workflow_ended`      | Workflow completes normally                | `{ status: string }`                                                   |

### Aggregate-Only Trail Entries

`for_each` steps emit exactly **two** trail entries, not one per iteration:
- `for_each_started`: records input count and concurrency settings.
- `for_each_completed`: records aggregate outcome.

Where `error_summary` is a human-readable roll-up like `"3 of 10 iterations failed: 2x timeout, 1x parse error"` or `null` if all succeeded.

Rationale: `for_each` is typically "apply this to a batch" — the aggregate outcome is what matters for debugging. Per-iteration detail is already in the step's `output` array and `StepResult` metrics. Workflow `loop:` blocks are different — each iteration IS a distinct execution cycle, so they get per-iteration trail entries.

### Partial Trail on Crash

If a workflow crashes mid-execution, the caller receives whatever trail was captured up to the crash point, alongside the error. The trail is never lost.

Entries emit synchronously at event boundaries, so the trail is always consistent up to the last completed event. A crash is inferred from the **absence of `workflow_ended`** plus the presence of an error — do not attempt to emit a `workflow_crashed` entry from a panic handler.

**Per-language idiom:**
- **TypeScript / Python:** Throw a custom `WorkflowError` exception that carries the partial envelope as a property.
- **Ruby:** Raise a custom exception class with the partial envelope attached.
- **Go:** Return `(*WorkflowEnvelope, error)` — envelope is non-nil with partial trail, error is non-nil with failure reason.

---

## Response Envelope

Every workflow execution returns a **WorkflowEnvelope** — a standardized response structure that provides the result, per-step details, trail, and observability metrics.

### WorkflowEnvelope

```typescript
{
  workflow: string;
  // The workflow name (from the YAML name field).

  version: string;
  // The workflow version (from the YAML version field).

  request_id: string;
  // A UUID v4 generated for this execution. Use for logging, tracing,
  // and correlation.

  status: 'success' | 'error';
  // Overall execution status.
  //   success — all executed steps completed without error
  //   error   — at least one step failed after retries/fallback

  timestamps: {
    started_at: string;   // ISO 8601 timestamp when execution began
    completed_at: string; // ISO 8601 timestamp when execution finished
  };

  metrics: {
    total_latency_ms: number;
    // Wall-clock time for the entire workflow execution.

    total_input_tokens: number;
    // Sum of input tokens across all LLM steps.

    total_output_tokens: number;
    // Sum of output tokens across all LLM steps.

    steps_executed: number;
    // Number of steps that actually ran.

    steps_not_executed: number;
    // Number of steps not reached due to branching.
  };

  steps: StepResult[];
  // Ordered array of per-step results (see StepResult below).
  // Includes entries for all declared steps, including those not reached.

  trail: TrailEntry[];
  // Ordered event log for debugging and audit.
  // See "Trail Contract" above.

  result: Record<string, unknown>;
  // The resolved output bindings. This is the "business payload" of
  // the workflow — the data the caller cares about.

  error?: string;
  // Present only when status is 'error'. Contains the error message
  // from the failed step.
}
```

### StepResult

```typescript
{
  id: string;
  // The step ID (from the YAML id field).

  agent: string;
  // The agent ID that executed this step. For workflow steps, this is
  // the workflow name prefixed with "workflow:".

  status: 'success' | 'error' | 'not_executed' | 'partial_failure';
  // Per-step status.
  //   success         — the step completed (possibly after retries or with fallback)
  //   error           — the step failed after all retries and fallback
  //   not_executed    — the step was not reached due to branching
  //   partial_failure — for_each step where some iterations failed

  output: unknown;
  // The step's output. For LLM agents: parsed response (object if schema,
  // string otherwise). For deterministic agents: handler return value.
  // For not_executed steps: null. For for_each steps: array of per-iteration
  // results (error slots contain { error: "..." }).

  sub_envelope?: WorkflowEnvelope;
  // Present only for workflow steps. Contains the full child workflow
  // envelope (trail, steps, metrics) for debugging.

  metrics: {
    latency_ms: number;
    // Wall-clock time for this step (including retries).

    input_tokens: number;
    // Input tokens used (0 for deterministic agents, summed for for_each).

    output_tokens: number;
    // Output tokens used (0 for deterministic agents, summed for for_each).
  };

  attempts?: number;
  // Total number of attempts (present if retry was configured).
  // 1 = succeeded on first try, 2 = succeeded on first retry, etc.

  used_fallback?: boolean;
  // true if the fallback agent was used. Only present when true.

  fallback_reason?: string;
  // The error message from the final retry attempt that triggered the
  // fallback. Only present when used_fallback is true.

  error?: string;
  // Error message if the step failed.
}
```

---

## Schemas

Schemas enforce structured output from LLM agents. Instead of hoping the model returns valid JSON in the right shape, schemas **guarantee** it.

### How Schemas Work

1. **Define** a schema using Zod (TypeScript), Pydantic (Python), or JSON Schema.
2. **Register** the schema by name in a schema registry.
3. **Reference** the schema name in `agent.yaml` via the `schema` field.
4. At runtime, the engine passes the schema to the LLM provider's structured output API (e.g., OpenAI's `zodResponseFormat`).

### Schema Registry Pattern

The schema registry is a simple name-to-schema map. In TypeScript:

```typescript
// schemas/registry.ts
import { z } from 'zod';

export const ReviewAnalysis = z.object({
  sentiment: z.enum(['positive', 'negative', 'mixed', 'neutral']),
  score: z.number().min(0).max(10),
  pros: z.array(z.string()),
  cons: z.array(z.string()),
  themes: z.array(z.object({
    name: z.string(),
    frequency: z.number(),
    examples: z.array(z.string()),
  })),
  summary: z.string(),
});

export const QualityScore = z.object({
  overall: z.number().min(0).max(100),
  breakdown: z.object({
    structure: z.number().min(0).max(100),
    completeness: z.number().min(0).max(100),
    tone: z.number().min(0).max(100),
    accuracy: z.number().min(0).max(100),
  }),
  suggestions: z.array(z.string()),
});

// Registry map
export const schemas: Record<string, z.ZodType> = {
  ReviewAnalysis,
  QualityScore,
};
```

### Schema vs. null vs. Omitted


| `schema` value     | Behavior                                                                                                                               |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `"ReviewAnalysis"` | Structured output mode. Model is forced to conform to the schema. Response is parsed and validated.                                    |
| `null`             | JSON mode. Model is instructed to return valid JSON, but the shape is not constrained. Useful when you want JSON but the shape varies. |
| *(omitted)*        | Plain text mode. Model returns a free-form text response.                                                                              |


---

## Code Generation (Build Step)

Agentic App Spec follows a **protoc-inspired code generation pattern**. The YAML definitions are the contract (like `.proto` files), and a build step generates typed function handles in your target language.

### Why Code Generation?

- **Type safety.** Generated interfaces/structs catch mismatches at compile time.
- **IDE support.** Autocomplete, go-to-definition, and inline documentation work out of the box.
- **No runtime reflection.** The generated code imports the runtime engine directly -- no dynamic YAML parsing at call time.

### How It Works

1. The `agentic build` command reads all `agent.yaml` and `workflow.yaml` files.
2. For each agent, it generates a typed function and an input type/interface.
3. For each workflow, it generates a typed function and an input type/interface.
4. Generated files are placed in the configured output directory (default: `src/generated/`).

### Type Mapping


| YAML Type | TypeScript                | Python           | Ruby                     | Go                       |
| --------- | ------------------------- | ---------------- | ------------------------ | ------------------------ |
| `base64`  | `string`                  | `str`            | `String`                 | `string`                 |
| `string`  | `string`                  | `str`            | `String`                 | `string`                 |
| `number`  | `number`                  | `float`          | `Numeric`                | `float64`                |
| `boolean` | `boolean`                 | `bool`           | `TrueClass | FalseClass` | `bool`                   |
| `object`  | `Record<string, unknown>` | `dict[str, Any]` | `Hash`                   | `map[string]interface{}` |


### Naming Conventions


| Source                | Convention                                                   | Example                                               |
| --------------------- | ------------------------------------------------------------ | ----------------------------------------------------- |
| Agent ID (directory)  | kebab-case                                                   | `review-analyzer`                                     |
| Generated function    | camelCase (TS/JS), snake_case (Python/Ruby), PascalCase (Go) | `reviewAnalyzer`, `review_analyzer`, `ReviewAnalyzer` |
| Generated type/struct | PascalCase                                                   | `ReviewAnalyzerInput`                                 |


### Generated Output Examples

#### TypeScript — Agent Handle

```typescript
// @generated from agentic-spec/agents/review-analyzer/agent.yaml
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ReviewAnalyzerInput {
  product_name: string;
  category: string;
  description: string;
  reviews: string;
}

export async function reviewAnalyzer(input: ReviewAnalyzerInput): Promise<AgentResult> {
  return invokeAgent('review-analyzer', input);
}
```

#### TypeScript — Workflow Handle

```typescript
// @generated from agentic-spec/workflows/product-review.yaml
import { orchestrate } from '../../engine/orchestrator.js';
import type { WorkflowEnvelope } from '../../types.js';

export interface ProductReviewInput {
  product_id: number;
}

export async function productReview(input: ProductReviewInput): Promise<WorkflowEnvelope> {
  return orchestrate('product-review', input);
}
```

---

## Build-Time Validation

`agentic build` performs comprehensive graph analysis on every workflow before generating code. All checks are **build-time failures** — the build aborts with a clear error message. No runtime surprises.

### Validation Checks

| Check                          | Description                                                                                                   |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| **Reachability**               | Every declared step must be reachable from the entry point. Unreachable steps are build errors, not warnings.  |
| **Target existence**           | Every `next:` target (goto, switch case, if/then/else) must reference a step ID that exists in the workflow.   |
| **Cycle detection**            | Backward edges are detected. Cycles without a conditional exit (switch/if that breaks the cycle) are rejected. |
| **Non-exhaustive conditions**  | `switch` blocks require a `default` case. `if` blocks require both `then` and `else`.                         |
| **Parallel branch isolation**  | Steps in one parallel branch cannot reference outputs from sibling branches.                                   |
| **requires: satisfaction**     | For every path into a step, all `requires:` entries (explicit or inferred) are satisfied by upstream steps.    |
| **Trail isolation**            | Any binding starting with `$.trail` is rejected.                                                              |
| **Sub-workflow cycles**        | A workflow cannot invoke itself (directly or transitively) via `workflow:` steps.                              |
| **Config whitelist**           | `config:` overrides only allow `model`, `temperature`, `image_detail`. Other fields are rejected.             |

---

## CLI

The `agentic` CLI provides commands for initializing projects, scaffolding agents and workflows, generating code, and inspecting the project.

### Commands

#### `agentic init`

Initialize a new agentic project in the current directory.

```bash
agentic init
```

Creates:

```
.
├── agentic-spec/
│   ├── agents/
│   ├── workflows/
│   └── schemas/
└── agentic.config.yaml
```

#### `agentic add agent`

Scaffold a new agent with the required files.

```bash
agentic add agent <agent-id> [options]
```

| Flag      | Description                          | Default   |
| --------- | ------------------------------------ | --------- |
| `--type`  | Agent type: `llm` or `deterministic` | `llm`     |
| `--model` | Model identifier (LLM agents only)   | `gpt-4.1` |

#### `agentic add workflow`

Scaffold a new workflow with references to existing agents.

```bash
agentic add workflow <name> [options]
```

| Flag       | Description                                           | Default  |
| ---------- | ----------------------------------------------------- | -------- |
| `--agents` | Comma-separated list of agent IDs to include as steps | *(none)* |

#### `agentic build`

Validate workflow graphs and generate typed integration handles from YAML definitions.

```bash
agentic build [options]
```

| Flag     | Description                                           | Default      |
| -------- | ----------------------------------------------------- | ------------ |
| `--lang` | Target language: `typescript`, `python`, `ruby`, `go` | `typescript` |

The build step performs all [build-time validation checks](#build-time-validation) before generating code. If any check fails, the build aborts with a descriptive error and no files are written.

#### `agentic list`

List all agents and workflows in the project.

```bash
agentic list
```

---

## Language Support

The build step generates idiomatic code for each supported language. Below are complete examples of generated agent handles.

### TypeScript

```typescript
// @generated from agentic-spec/agents/review-analyzer/agent.yaml
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ReviewAnalyzerInput {
  product_name: string;
  category: string;
  description: string;
  reviews: string;
}

export async function reviewAnalyzer(input: ReviewAnalyzerInput): Promise<AgentResult> {
  return invokeAgent('review-analyzer', input);
}
```

### Python

```python
# @generated from agentic-spec/agents/review-analyzer/agent.yaml
from dataclasses import dataclass
from typing import Any
from engine.runner import invoke_agent
from engine.types import AgentResult


@dataclass
class ReviewAnalyzerInput:
    product_name: str
    category: str
    description: str
    reviews: str


async def review_analyzer(input: ReviewAnalyzerInput) -> AgentResult:
    return await invoke_agent('review-analyzer', input.__dict__)
```

### Ruby

```ruby
# @generated from agentic-spec/agents/review-analyzer/agent.yaml
module Agents
  ReviewAnalyzerInput = Struct.new(
    :product_name,
    :category,
    :description,
    :reviews,
    keyword_init: true
  )

  def self.review_analyzer(input)
    Engine::Runner.invoke_agent('review-analyzer', input.to_h)
  end
end
```

### Go

```go
// @generated from agentic-spec/agents/review-analyzer/agent.yaml
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ReviewAnalyzerInput struct {
    ProductName string `json:"product_name"`
    Category    string `json:"category"`
    Description string `json:"description"`
    Reviews     string `json:"reviews"`
}

func ReviewAnalyzer(input ReviewAnalyzerInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("review-analyzer", input)
}
```

---

## Example: Product Review Pipeline

A complete pipeline that fetches a product, analyzes its reviews, researches comparisons, writes a comprehensive review, and scores quality. Demonstrates `parallel`, `if` branching, `retry`, and `fallback`.

### Pipeline Overview

```
fetch (deterministic)
  |
  if: output.found
  |        \
  v         v
parallel   not-found (terminates)
  |- review-analyzer (retry 2x, fallback gpt-4.1-mini)
  |- comparison-researcher (retry 2x, fallback gpt-4.1-mini)
  |
  v
review-writer (retry 3x, fallback gpt-4.1-mini)
  |
  v
quality-scorer (deterministic)
```

### workflows/product-review.yaml

```yaml
name: product-review
description: Fetches a product, analyzes reviews, and generates a comprehensive review
version: "1.0"

input:
  product_id:
    type: number
    required: true

steps:
  - id: fetch
    agent: product-fetcher
    input:
      product_id: $.input.product_id
    next:
      if: output.found
      then: research
      else: not-found

  - id: not-found
    agent: not-found-responder
    input:
      product_id: $.input.product_id

  - parallel:
      id: research
      join: all
      branches:
        - id: review-analysis
          agent: review-analyzer
          input:
            product_name: $.steps.fetch.output.title
            category: $.steps.fetch.output.category
            description: $.steps.fetch.output.description
            reviews: $.steps.fetch.output.reviews
          retry:
            max_attempts: 2
            backoff_ms: 500
          fallback:
            agent: review-analyzer
            config:
              model: gpt-4.1-mini

        - id: comparison
          agent: comparison-researcher
          input:
            product_name: $.steps.fetch.output.title
            category: $.steps.fetch.output.category
            price: $.steps.fetch.output.price
          retry:
            max_attempts: 2
            backoff_ms: 500
          fallback:
            agent: comparison-researcher
            config:
              model: gpt-4.1-mini

  - id: article
    agent: review-writer
    input:
      product_name: $.steps.fetch.output.title
      analysis: $.steps.review-analysis.output
      comparisons: $.steps.comparison.output
    retry:
      max_attempts: 3
      backoff_ms: 1000
    fallback:
      agent: review-writer
      config:
        model: gpt-4.1-mini

  - id: quality
    agent: quality-scorer
    input:
      review: $.steps.article.output

output:
  product: $.steps.fetch.output
  review_analysis: $.steps.review-analysis.output
  comparison: $.steps.comparison.output
  article: $.steps.article.output
  quality_scores: $.steps.quality.output
```

---

## Example: Grocery Classification (switch)

Demonstrates hierarchical classification using `switch` branching — no routing agents needed. Each classifier is a regular agent with a schema constraining its output to an enum.

### workflows/grocery-classify.yaml

```yaml
name: grocery-classify
description: Classifies grocery items through hierarchical branching
version: "1.0"

input:
  item_name:
    type: string
    required: true

steps:
  - id: classify
    agent: food-classifier
    input:
      item_name: $.input.item_name
    next:
      switch: output.category
      cases:
        food: classify-subtype
        non_food: handle-non-food
      default: unknown-item

  - id: classify-subtype
    agent: food-subtype-classifier
    input:
      item_name: $.input.item_name
    next:
      switch: output.subtype
      cases:
        meat: handle-meat
        vegetable: handle-vegetable
        canned: handle-canned
      default: handle-generic-food

  - id: handle-meat
    agent: classify-meat
    input:
      item_name: $.input.item_name

  - id: handle-vegetable
    agent: classify-vegetable
    input:
      item_name: $.input.item_name

  - id: handle-canned
    agent: classify-canned
    input:
      item_name: $.input.item_name

  - id: handle-generic-food
    agent: classify-food
    input:
      item_name: $.input.item_name

  - id: handle-non-food
    agent: classify-non-food
    input:
      item_name: $.input.item_name

  - id: unknown-item
    agent: unknown-classifier
    input:
      item_name: $.input.item_name

output:
  classification: $.steps.classify.output
```

The `food-classifier` agent returns `{ category: "food" | "non_food" | "unknown" }` via a schema. The `food-subtype-classifier` returns `{ subtype: "meat" | "vegetable" | "canned" | "other" }`. The switch picks the right specialist. No routing agent, no special agent type — just regular agents whose structured output drives workflow-level control flow.

---

## Example: Iterative Refinement (loop)

Demonstrates a write-review-revise cycle with bounded iteration.

### workflows/draft-review.yaml

```yaml
name: draft-review
description: Iteratively refines a review article until quality threshold is met
version: "1.0"

input:
  product_data:
    type: object
    required: true
  analysis:
    type: object
    required: true

steps:
  - loop:
      id: refinement
      max_iterations: 3
      until: $.steps.review.output.is_satisfactory
      steps:
        - id: draft
          agent: review-writer
          input:
            product: $.input.product_data
            analysis: $.input.analysis
            previous_draft: $.steps.draft.output
            feedback: $.steps.review.output.feedback
        - id: review
          agent: quality-reviewer
          input:
            draft: $.steps.draft.output

output:
  final_draft: $.steps.draft.output
  review_verdict: $.steps.review.output
```

On the first iteration, `$.steps.draft.output` and `$.steps.review.output.feedback` resolve to `null`. Each subsequent iteration sees the prior output. The loop exits when the reviewer returns `is_satisfactory: true` or after 3 iterations.

---

## Example: Batch Analysis (for_each)

Demonstrates dynamic fan-out over a runtime-determined list.

### workflows/batch-analyze.yaml

```yaml
name: batch-analyze
description: Extracts items from a document then analyzes each one
version: "1.0"

input:
  document:
    type: string
    required: true

steps:
  - id: extract
    agent: item-extractor
    input:
      document: $.input.document

  - id: analyze-each
    for_each: $.steps.extract.output.items
    as: item
    agent: item-analyzer
    input:
      data: "{{item}}"
      context: $.steps.extract.output.context
    max_concurrency: 5
    retry:
      max_attempts: 2
      backoff_ms: 500

  - id: summarize
    agent: batch-summarizer
    input:
      results: $.steps.analyze-each.output

output:
  items: $.steps.extract.output.items
  analyses: $.steps.analyze-each.output
  summary: $.steps.summarize.output
```

The `item-extractor` returns `{ items: [...], context: "..." }`. The `for_each` step runs `item-analyzer` once per item (up to 5 concurrently). Failed iterations produce `{ error: "..." }` in their output slot. The `batch-summarizer` receives the full array of results.

---

## Future: Agent-Level Tool Use

A planned extension that allows agents to invoke other agents as tools during their reasoning loop. This is distinct from workflow-level composition — it gives individual agents the ability to delegate sub-tasks to specialists as part of producing a single answer.

### Planned agent.yaml Surface

```yaml
name: Research Coordinator
type: llm
model: gpt-4.1
temperature: 0.3
max_tool_iterations: 5
tools:
  - name: web-search
    description: Search the web for current information
    agent: web-searcher
  - name: fact-check
    description: Verify a claim against known sources
    agent: fact-checker
  - name: summarize
    description: Summarize a long document
    agent: summarizer
```

Each tool entry declares:
- **name** — tool name presented to the LLM
- **description** — what the tool does (sent to the LLM for tool selection)
- **agent** — the agent ID to invoke when the LLM calls this tool

The agent's `input` schema becomes the tool's input schema. The agent's output becomes the tool's return value.

### Key Design Considerations

This feature is intentionally deferred from the current release because it requires focused design work across several dimensions:

1. **Trail contract expansion.** New event types needed: `tool_called`, `tool_returned`, `tool_failed`, `llm_turn`. A `tool_calls` field on `StepResult` for at-a-glance debugging.
2. **Input and output schemas.** Both must be declared — the LLM needs to reason about return shapes, not just input shapes.
3. **Iteration bounds.** Every tool-using agent needs `max_tool_iterations` to cap the internal reasoning loop.
4. **Error semantics.** Tool failure within the loop: the LLM sees the error and can recover. Per-tool retry cap so one broken tool can't eat the iteration budget.
5. **Workflow interaction.** If a tool-using agent exhausts its tool budget and fails, the workflow's fallback should still fire.
6. **Observability.** `StepResult` should get a `tool_usage` summary (`{ total_calls, by_tool, iterations }`) so users can debug "why did this step take 40 seconds" without drilling into the trail.

**Do not build patterns that depend on agent-level tool use.** The surface described above is directional but not finalized. Use workflow-level composition (sub-workflows, `for_each`, parallel branches) for multi-agent patterns until this feature ships.

---

## Architecture Decisions

### Why file-tree based?

The file-tree structure makes the spec **portable**, **version-controllable**, and **AI-readable**.

- **Portable.** The entire agent/workflow configuration is plain files. Copy the `agentic-spec/` directory to a new project and it works. No database, no registry service, no configuration server.
- **Version-controllable.** Every change to an agent or workflow is a Git diff. You can review prompt changes in PRs, bisect regressions, and roll back to any point in history.
- **AI-readable.** AI coding assistants can read the directory structure, understand the relationships, and scaffold new agents or workflows without special tooling. The YAML format is the most common structured format in LLM training data.

### Why YAML?

- **Human-readable.** YAML is easier to read and write than JSON for configuration files, especially with multi-line strings (prompts).
- **Widely supported.** Every language has a YAML parser. Every developer has seen YAML.
- **Not code.** YAML separates configuration from implementation. You cannot accidentally put business logic in a YAML file. This forces clean separation of concerns.
- **Comment support.** Unlike JSON, YAML supports comments — critical for documenting configuration choices inline.

### Why code generation?

The protoc-inspired code generation pattern provides:

- **Type safety.** Generated interfaces and structs catch type mismatches at compile time. If you rename an input parameter in the YAML but forget to update the caller, the compiler tells you.
- **IDE support.** Autocomplete, go-to-definition, hover documentation, and refactoring tools all work with generated code.
- **No runtime reflection.** Generated code imports the engine directly with concrete types. There is no dynamic YAML parsing, no string-based lookups, and no runtime type coercion at call sites.
- **Cross-language.** The same YAML definitions generate idiomatic code in TypeScript, Python, Ruby, and Go. Teams using multiple languages share a single source of truth.

### Why the envelope pattern?

The `WorkflowEnvelope` provides a **consistent contract** between the orchestrator and the caller:

- **Observable.** Every execution includes timing, token counts, and per-step status. You can build dashboards, set alerts, and track costs without modifying agent code.
- **Debuggable.** When something goes wrong, the envelope tells you which step failed, how many retries were attempted, whether the fallback was used, and what error occurred. The trail provides full event-level detail.
- **Composable.** Because every workflow returns the same envelope shape, workflows can be nested (via `workflow:` steps) without special handling.

### Why "agents compute, workflows orchestrate"?

The strict separation exists to keep agents **reusable** and **testable**:

- **Agents** are pure I/O units. They take input, produce output, and know nothing about workflow topology, branching logic, or other agents' existence. An agent can be tested in isolation with mock inputs.
- **Workflows** own all control flow: sequencing, branching, parallelism, loops, fan-out. Changing how the pipeline works is a YAML edit, not a code change across multiple agents.
- **No leaking.** The trail isolation rule (rejecting `$.trail` bindings) enforces this boundary at build time. An agent cannot implicitly access execution history, routing decisions, or other agents' metadata.

### Why declarative retry/fallback?

Resilience logic is declared in the workflow YAML, not in agent code:

- **Separation of concerns.** Agents don't know about retries or fallbacks. The orchestrator handles all resilience.
- **Centralized control.** Changing retry behavior is a YAML edit, not a code change.
- **Testability.** Agents can be tested in isolation. The orchestrator can be tested with mock agents.

---

## Best Practices

### Agent Design

- **Keep agents small and focused.** Each agent should do one thing well. If an agent's prompt is longer than 500 words, consider splitting it.
- **Use descriptive IDs.** Use kebab-case names that describe the agent's purpose: `review-analyzer`, `sentiment-classifier`, `product-fetcher`. Avoid `agent1`, `step-2`, `processor`.
- **Put complex prompts in prompt.md.** The `user_message` field should contain the template with placeholders. Long instructions belong in `prompt.md` (the system prompt).
- **Use schemas for structured output.** Whenever you need structured data from an LLM, define a schema. This eliminates parsing failures and guarantees the output shape.
- **Use schemas to drive control flow.** For `switch` branching, constrain the agent's output to an enum via schema. The workflow `switch` dispatches on that value. This replaces the old routing agent pattern with a cleaner separation: the agent classifies, the workflow branches.

### Workflow Design

- **Use `if` for early exit.** If a validation step can determine that the rest of the pipeline is pointless, branch to an exit step rather than proceeding.
- **Use `switch` for classification.** When an agent classifies input into categories, use `switch` to dispatch to the appropriate handler. Each case is a step ID — no nesting required.
- **Use `loop` for refinement.** Write-review-revise cycles, iterative improvement, and convergence loops all fit the `loop:` block. Always set a reasonable `max_iterations`.
- **Use `for_each` for batches.** When a prior step produces a list, `for_each` maps an agent over it. Set `max_concurrency` to avoid rate-limiting.
- **Use fallback for degraded quality over total failure.** A common pattern is to fall back from a capable model to a cheaper model. The output may be lower quality, but the workflow succeeds.
- **Use retry for transient failures.** Rate limits, network timeouts, and temporary API errors are common. Retry with backoff handles these gracefully.

### Resilience Strategy Guide


| Scenario                 | Strategy                    | Configuration                                                  |
| ------------------------ | --------------------------- | -------------------------------------------------------------- |
| Rate limit (429)         | Retry with high backoff     | `retry: { max_attempts: 3, backoff_ms: 2000 }`                |
| Network timeout          | Retry with moderate backoff | `retry: { max_attempts: 3, backoff_ms: 500 }`                 |
| Model quality issue      | Fallback to different model | `fallback: { agent: same, config: { model: "gpt-4.1-mini" }}` |
| Validation failure       | Branch with `if`            | `next: { if: output.is_valid, then: continue, else: reject }` |
| Intermittent parse error | Retry with low backoff      | `retry: { max_attempts: 2, backoff_ms: 200 }`                 |
| Critical failure         | No retry, fail fast         | *(omit retry and fallback)*                                    |


### General

- **Version your workflows.** Bump the `version` field when you change the workflow structure. This makes it easy to correlate response envelopes with specific workflow definitions.
- **Use consistent naming.** If an agent is called `review-analyzer`, its schema should be `ReviewAnalysis`, and its handler (if deterministic) should be `review-analyzer`.
- **Test agents in isolation.** Each agent should be testable with mock inputs, independent of the workflow. Generate handles and write unit tests against them.
- **Monitor via the envelope.** Use the `metrics` fields to track latency, token usage, and failure rates. Set alerts on `steps_not_executed` (unexpected branching), `used_fallback` (quality degradation), and `partial_failure` (for_each issues).
- **Use `requires:` for complex graphs.** In workflows with multiple `next:` branches, explicit `requires:` makes dependencies clear and catches missing-data bugs at build time rather than runtime.

---

## Glossary


| Term                       | Definition                                                                                                                                                                                                          |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Agent**                  | A single unit of work. Either an LLM agent (calls a language model) or a deterministic agent (runs a handler function). Defined by `agent.yaml` and optionally `prompt.md`.                                         |
| **Agent ID**               | Unique identifier derived from the directory name under `agentic-spec/agents/`. Always kebab-case.                                                                                                                 |
| **Binding**                | A path expression (e.g., `$.steps.fetch.output.title`) that references data in the execution context. Resolved at runtime before a step executes.                                                                   |
| **Branch**                 | One arm of a `parallel` block. Contains a sequential `steps` array. Branches execute concurrently.                                                                                                                  |
| **Deterministic Handler**  | A registered function that executes without calling an LLM. Used for API calls, calculations, and data transformations.                                                                                             |
| **Envelope**               | Short for **WorkflowEnvelope**. The standardized response structure returned by every workflow execution. Contains the result, step details, trail, metrics, and status.                                             |
| **Execution Context**      | Runtime state maintained by the orchestrator. Accumulates step outputs, making them available to subsequent steps via bindings.                                                                                     |
| **Fallback**               | A backup agent that runs if a step fails after all retries. Attempted exactly once. `fallback_reason` records why the fallback was triggered.                                                                       |
| **for_each**               | Dynamic fan-out step that invokes an agent once per element in a runtime-determined array. Output is an array of per-iteration results.                                                                              |
| **Loop**                   | A `loop:` block that repeats a set of steps until an `until` condition is met or `max_iterations` is reached.                                                                                                       |
| **next:**                  | Control flow field on steps. Determines which step executes afterward. Supports goto (string), `switch` (value-based branching), and `if` (binary branching).                                                       |
| **Parallel Block**         | A set of branches declared under `parallel:`. All branches execute concurrently with a configurable `join` strategy.                                                                                                |
| **requires:**              | Optional field listing binding paths a step depends on. Enables build-time validation that all paths into the step satisfy its data dependencies.                                                                    |
| **Retry**                  | Automatic re-execution of a failed step. Configured with `max_attempts` and `backoff_ms`.                                                                                                                           |
| **Schema**                 | A Zod, Pydantic, or JSON Schema definition registered by name. Forces the LLM to produce structured output conforming to the schema.                                                                                |
| **Schema Registry**        | A name-to-schema map that allows agents to reference schemas by string name.                                                                                                                                        |
| **Step**                   | A single node in the workflow graph. Can be an agent step, workflow step, parallel block, loop block, or for_each step.                                                                                              |
| **Step Result**            | Per-step record in the envelope. Contains status, output, metrics, and optional fields (attempts, fallback_reason, sub_envelope).                                                                                   |
| **Sub-workflow**           | A workflow invoked by another workflow via a `workflow:` step. Its envelope embeds in the parent step result as `sub_envelope`.                                                                                     |
| **switch:**                | Value-based branching sugar on `next:`. Evaluates an output field and jumps to the matching case.                                                                                                                    |
| **Template Interpolation** | The `{{key}}` syntax used in `user_message` to inject runtime values into the user prompt.                                                                                                                           |
| **Trail**                  | Ordered event log in the envelope. Records every significant execution event for debugging and audit. Not accessible to agents (trail isolation).                                                                    |
| **Trail Isolation**        | Build-time rule rejecting `$.trail` bindings. The trail is for envelope consumers only, not agent inputs.                                                                                                            |
| **Workflow**               | A directed graph of steps with defined data flow, control flow, and resilience configuration. Defined by a YAML file in `agentic-spec/workflows/`.                                                                   |
| **Workflow Input**         | Parameters declared in the workflow's `input` section. Available to steps via `$.input.<key>` bindings.                                                                                                              |
| **Workflow Output**        | The `output` section mapping keys to binding expressions. Resolved after execution and returned as the `result` field in the envelope.                                                                               |


---

## License

This specification is open for adoption. Use it to build your own agent orchestration layer in any language.
