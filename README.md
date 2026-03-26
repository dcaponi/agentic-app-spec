# Agentic App Spec

A file-tree based specification for defining AI agent pipelines and multi-agent workflows. Agents and workflows are declared in YAML, system prompts live in Markdown, and a build step generates typed function handles in your target language. Think of it as **protobuf for AI agent orchestration**.

---

## Quick Start

### Install the CLI

Download the prebuilt binary for your platform from the [releases](https://github.com/dominickcaponi/agentic-app-spec/releases) page, or build from source:

```bash
cd cli && cargo build --release
# binary at cli/target/release/agentic
```

| Platform       | Binary                         |
|----------------|--------------------------------|
| macOS arm64    | `agentic-darwin-arm64`         |
| macOS x86_64   | `agentic-darwin-x86_64`        |
| Linux x86_64   | `agentic-linux-x86_64`         |
| Linux arm64    | `agentic-linux-arm64`          |
| Windows x86_64 | `agentic-windows-x86_64.exe`   |

### Install the runtime for your language

```bash
# TypeScript
npm install agentic-engine

# Python
pip install "agentic-engine @ git+https://github.com/dominickcaponi/agentic-app-spec.git#subdirectory=runtime/python"

# Go
go get github.com/dominickcaponi/agentic-app-spec/runtime/go@latest
```

Ruby — in your Gemfile:

```ruby
gem "agentic_engine", git: "https://github.com/dominickcaponi/agentic-app-spec.git", glob: "runtime/ruby/*.gemspec"
```

### Scaffold and build

```bash
agentic init
agentic add agent my-agent --type llm
agentic add workflow my-pipeline --agents my-agent
agentic build --lang typescript   # or python, ruby, go
```

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
  - [Binding Syntax](#binding-syntax)
  - [Execution Model](#execution-model)
  - [Response Envelope](#response-envelope)
- [Schemas](#schemas)
- [Code Generation (Build Step)](#code-generation-build-step)
- [CLI](#cli)
- [Language Support](#language-support)
  - [TypeScript](#typescript)
  - [Python](#python)
  - [Ruby](#ruby)
  - [Go](#go)
- [Example Project: Product Review Pipeline](#example-project-product-review-pipeline)
- [Architecture Decisions](#architecture-decisions)
- [Best Practices](#best-practices)
- [Glossary](#glossary)

---

## Project Structure

```
project-root/
├── agents/
│   └── <agent-id>/
│       ├── agent.yaml        # Agent configuration
│       └── prompt.md          # System prompt (LLM agents only)
├── workflows/
│   └── <workflow-name>.yaml   # Workflow orchestration definition
├── schemas/                   # (optional) Zod/JSON schemas for structured output
└── agentic.config.yaml        # (optional) Project-level config
```

### Directory Conventions

- `**agents/**` -- Each subdirectory is an agent. The directory name is the **agent ID** and must be unique across the project. Use kebab-case (e.g., `review-analyzer`, `product-fetcher`).
- `**workflows/`** -- Each YAML file is a workflow. The filename (minus extension) is the **workflow name**.
- `**schemas/`** -- Optional directory for Zod or JSON Schema definitions. These are registered by name and referenced from agent configurations.
- `**agentic.config.yaml**` -- Optional project-level configuration (default model, default temperature, schema registry path, etc.).

---

## Agent Definition

An agent is a single unit of work. It is either an **LLM agent** (calls a language model) or a **deterministic agent** (runs a handler function with no LLM involved).

Each agent lives in its own directory under `agents/`:

```
agents/
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
agents/
└── review-analyzer/
    ├── agent.yaml
    └── prompt.md    <-- system prompt
```

The system prompt is plain Markdown. It can be as long or as short as needed. The runtime reads this file at build time (or at agent load time, depending on the implementation) and sends it as the `system` role message in the LLM call.

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

A workflow chains agents together into a pipeline. It declares the execution order, data flow between steps, parallel groups, retry/fallback logic, short-circuit conditions, and the final output shape.

Each workflow is a single YAML file in the `workflows/` directory:

```
workflows/
└── product-review.yaml
```

### workflow.yaml Reference

```yaml
# ---------- Metadata ----------

name: string
# Human-readable workflow name.
# Example: "Product Review Pipeline"

description: string
# Plain-language description of the workflow's purpose.
# Example: "Fetches a product, analyzes its reviews, and generates a comprehensive review"

version: string
# Semantic version string for this workflow definition.
# Example: "1.0"

# ---------- Inputs ----------

input:
  <param_name>:
    type: 'base64' | 'string' | 'number' | 'boolean' | 'object'
    # Data type (same type system as agent inputs).
    required: boolean
    # Whether the caller must provide this parameter. Default: true.

# ---------- Steps ----------

steps:
  # Steps are an ordered list. By default, they execute serially (top to bottom).
  # Each step is either a serial step or a parallel group.

  # --- Serial Step ---
  - id: string
    # Unique step identifier. Used in binding paths (e.g., $.steps.<id>.output).
    # Must be unique within the workflow.

    agent: string
    # Agent ID — must match a directory name under agents/.
    # Example: "review-analyzer"

    input:
      <param>: <binding>
      # Input bindings. Each key is an agent input parameter name.
      # Each value is a binding expression (see "Binding Syntax" below)
      # or a literal value.

    config:
      # (optional) Override agent configuration at the step level.
      # Any field from agent.yaml can be overridden here.
      model: string
      # Example: use a different model for this step
      temperature: number
      # Example: use a different temperature

    retry:
      # (optional) Retry configuration for transient failures.
      max_attempts: number
      # Total number of attempts including the first try.
      # Example: 3 means 1 initial attempt + 2 retries.
      backoff_ms: number
      # Base delay between retries in milliseconds.
      # The actual delay is backoff_ms * attempt_number.
      # Example: backoff_ms: 500 -> delays of 500ms, 1000ms, 1500ms, ...

    fallback:
      # (optional) Fallback agent to use if all retry attempts fail.
      agent: string
      # Agent ID for the fallback. Can be the same agent (with different
      # config) or a completely different agent.
      config:
        # (optional) Config overrides for the fallback execution.
        model: string
        # Common pattern: fall back to a cheaper/faster model.
        # Example: "gpt-4.1-mini"

    short_circuit:
      # (optional) Early exit condition evaluated after this step completes.
      condition: string
      # A JavaScript expression evaluated with the step's `output` in scope.
      # If the expression evaluates to a truthy value, ALL remaining steps
      # are skipped.
      # Examples:
      #   "!output.is_food"
      #   "output.score < 0.5"
      #   "output.status === 'not_found'"

      defaults:
        <step_id>: <value>
        # Default output values for each subsequent step that would be
        # skipped. Keyed by step ID. These values populate the skipped
        # steps' output fields in the response envelope.
        # Example:
        #   review-analyzer: { sentiment: "N/A", error: "Product not found" }
        #   review-writer: { review: "No review generated" }

  # --- Parallel Group ---
  - parallel:
    # An array of steps that run concurrently. The orchestrator launches
    # all steps in the group simultaneously (Promise.all in JS, asyncio.gather
    # in Python, goroutines in Go) and waits for all to complete before
    # proceeding to the next step.
    #
    # Each step inside a parallel group supports the same fields as a
    # serial step (id, agent, input, config, retry, fallback).
    # Short-circuit is NOT supported inside parallel groups.

    - id: string
      agent: string
      input:
        <param>: <binding>
      config:
        model: string
        temperature: number
      retry:
        max_attempts: number
        backoff_ms: number
      fallback:
        agent: string
        config:
          model: string

    - id: string
      agent: string
      input:
        <param>: <binding>

# ---------- Output ----------

output:
  <key>: <binding>
  # Workflow output bindings. Maps named output keys to binding expressions
  # that reference step outputs or workflow inputs.
  # These are resolved after all steps complete (or after a short-circuit)
  # and become the `result` field in the response envelope.
```

#### Complete Workflow Example

```yaml
name: Product Review Pipeline
description: Fetches a product, analyzes reviews, and generates a comprehensive review
version: "1.0"

input:
  product_id:
    type: number
    required: true

steps:
  - id: product-fetcher
    agent: product-fetcher
    input:
      product_id: $.input.product_id
    short_circuit:
      condition: "!output.found"
      defaults:
        review-analyzer: { sentiment: null, pros: [], cons: [], themes: [] }
        comparison-researcher: { comparisons: [] }
        review-writer: { review: "Product not found." }
        quality-scorer: { score: 0, breakdown: {} }

  - parallel:
    - id: review-analyzer
      agent: review-analyzer
      input:
        product_name: $.steps.product-fetcher.output.title
        category: $.steps.product-fetcher.output.category
        description: $.steps.product-fetcher.output.description
        reviews: $.steps.product-fetcher.output.reviews
      retry:
        max_attempts: 3
        backoff_ms: 500
      fallback:
        agent: review-analyzer
        config:
          model: gpt-4.1-mini

    - id: comparison-researcher
      agent: comparison-researcher
      input:
        product_name: $.steps.product-fetcher.output.title
        category: $.steps.product-fetcher.output.category
        price: $.steps.product-fetcher.output.price
      retry:
        max_attempts: 3
        backoff_ms: 500
      fallback:
        agent: comparison-researcher
        config:
          model: gpt-4.1-mini

  - id: review-writer
    agent: review-writer
    input:
      product_name: $.steps.product-fetcher.output.title
      analysis: $.steps.review-analyzer.output
      comparisons: $.steps.comparison-researcher.output
    retry:
      max_attempts: 4
      backoff_ms: 1000
    fallback:
      agent: review-writer
      config:
        model: gpt-4.1-mini

  - id: quality-scorer
    agent: quality-scorer
    input:
      review: $.steps.review-writer.output.review

output:
  product: $.steps.product-fetcher.output
  analysis: $.steps.review-analyzer.output
  comparisons: $.steps.comparison-researcher.output
  review: $.steps.review-writer.output.review
  quality_score: $.steps.quality-scorer.output
```

### Binding Syntax

Bindings are path expressions that tell the orchestrator where to find data at runtime. They use a `$.` prefix followed by a dot-separated path.

#### Binding Types


| Pattern                           | Description                                                    | Example                                |
| --------------------------------- | -------------------------------------------------------------- | -------------------------------------- |
| `$.input.<key>`                   | Reference a workflow input parameter                           | `$.input.product_id`                   |
| `$.steps.<step_id>.output`        | Reference the full output object of a completed step           | `$.steps.product-fetcher.output`       |
| `$.steps.<step_id>.output.<path>` | Reference a nested field within a step's output                | `$.steps.product-fetcher.output.title` |
| Literal value                     | Strings, numbers, booleans, and objects pass through unchanged | `"default-category"`, `42`, `true`     |


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

### Execution Model

The orchestrator processes a workflow as follows:

1. **Input validation.** The orchestrator checks that all required workflow inputs are present and correctly typed. If validation fails, the workflow returns immediately with status `error`.
2. **Sequential execution.** Steps execute in the order they appear in the `steps` array, from top to bottom. Each step must complete before the next one begins (unless grouped in a parallel block).
3. **Context accumulation.** The orchestrator maintains an execution context object. After each step completes, its output is stored in the context at `$.steps.<step_id>.output`. This makes the output available to all subsequent steps via bindings.
4. **Parallel groups.** When the orchestrator encounters a `parallel` block, it launches all contained steps simultaneously. In JavaScript, this is `Promise.all`; in Python, `asyncio.gather`; in Go, goroutines with a `WaitGroup`. All steps in the group must complete before the orchestrator moves to the next item in the `steps` array. Steps within a parallel group can reference outputs from earlier steps (above the parallel block) but **cannot** reference outputs from sibling steps within the same parallel group.
5. **Retry.** If a step fails and has a `retry` configuration:
  - The orchestrator retries the step up to `max_attempts` total times (including the initial attempt).
  - Between each retry, it waits `backoff_ms * attempt_number` milliseconds. For example, with `backoff_ms: 500`: first retry waits 500ms, second retry waits 1000ms, third retry waits 1500ms.
  - Each retry re-resolves input bindings (in case upstream data changed, though this is uncommon).
6. **Fallback.** If all retry attempts are exhausted and the step still fails, and the step has a `fallback` configuration:
  - The orchestrator loads the fallback agent (specified by `fallback.agent`).
  - If `fallback.config` is provided, those values override the fallback agent's defaults (e.g., using a cheaper model).
  - The fallback agent is executed **once** with the same resolved inputs.
  - If the fallback succeeds, the step is marked as successful with `used_fallback: true`.
  - If the fallback also fails, the step is marked as `error`.
7. **Short-circuit.** If a step has a `short_circuit` configuration and the step completes successfully:
  - The orchestrator evaluates the `condition` expression with the step's `output` object in scope.
  - If the condition evaluates to a truthy value, **all remaining steps are skipped**.
  - Each skipped step receives its default output from `short_circuit.defaults` (keyed by step ID).
  - Skipped steps have status `skipped` in the response envelope.
  - The workflow status is set to `short_circuited`.
8. **Failure.** If a step fails after all retries and fallback (or if it has no retry/fallback configured), the workflow halts immediately. The workflow status is set to `error`, and the `error` field contains the failure message.
9. **Output resolution.** After all steps complete (or after a short-circuit), the orchestrator resolves the `output` bindings. These resolved values become the `result` field in the response envelope.

### Response Envelope

Every workflow execution returns a **WorkflowEnvelope** -- a standardized response structure that provides the result, per-step details, and observability metrics.

#### WorkflowEnvelope

```typescript
{
  workflow: string;
  // The workflow name (from the YAML `name` field).

  version: string;
  // The workflow version (from the YAML `version` field).

  request_id: string;
  // A UUID v4 generated for this execution. Use for logging, tracing,
  // and correlation.

  status: 'success' | 'error' | 'short_circuited';
  // Overall execution status.
  //   success         — all steps completed without error
  //   error           — at least one step failed after retries/fallback
  //   short_circuited — a step triggered an early exit via short_circuit

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
    // Number of steps that actually ran (including retries and fallback).

    steps_skipped: number;
    // Number of steps skipped due to short-circuit.
  };

  steps: StepResult[];
  // Ordered array of per-step results (see StepResult below).

  result: Record<string, unknown>;
  // The resolved output bindings. This is the "business payload" of
  // the workflow — the data the caller cares about.

  error?: string;
  // Present only when status is 'error'. Contains the error message
  // from the failed step.
}
```

#### StepResult

```typescript
{
  id: string;
  // The step ID (from the YAML `id` field).

  agent: string;
  // The agent ID that executed this step.

  status: 'success' | 'skipped' | 'error';
  // Per-step status.
  //   success — the step completed (possibly after retries or with fallback)
  //   skipped — the step was skipped due to short-circuit
  //   error   — the step failed after all retries and fallback

  output: unknown;
  // The step's output. For LLM agents, this is the parsed response
  // (object if schema was used, string otherwise). For deterministic
  // agents, this is the handler's return value. For skipped steps,
  // this is the default value from short_circuit.defaults.

  metrics: {
    latency_ms: number;
    // Wall-clock time for this step (including retries).

    input_tokens: number;
    // Input tokens used (0 for deterministic agents).

    output_tokens: number;
    // Output tokens used (0 for deterministic agents).
  };

  attempts?: number;
  // Total number of attempts (present if retry was configured).
  // 1 = succeeded on first try, 2 = succeeded on first retry, etc.

  used_fallback?: boolean;
  // true if the fallback agent was used. Only present if fallback
  // was configured and used.

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
4. Generated files are placed alongside the YAML definitions (or in a configured output directory).

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

#### TypeScript -- Agent Handle

```typescript
// @generated from agents/review-analyzer/agent.yaml
import { invokeAgent } from '../../engine/runner.js';
import type { AgentResult } from '../../types.js';

export interface ReviewAnalyzerInput {
  product_data: Record<string, unknown>;
}

export async function reviewAnalyzer(input: ReviewAnalyzerInput): Promise<AgentResult> {
  return invokeAgent('review-analyzer', input);
}
```

#### TypeScript -- Workflow Handle

```typescript
// @generated from workflows/product-review.yaml
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

## CLI

The `agentic` CLI provides commands for initializing projects, scaffolding agents and workflows, generating code, and inspecting the project.

### Commands

#### `agentic init`

Initialize a new agentic project in the current directory. Creates the directory structure and optional config file.

```bash
agentic init
```

Creates:

```
.
├── agents/
├── workflows/
├── schemas/
└── agentic.config.yaml
```

#### `agentic add agent`

Scaffold a new agent with the required files.

```bash
agentic add agent <agent-id> [options]
```

**Options:**


| Flag      | Description                          | Default   |
| --------- | ------------------------------------ | --------- |
| `--type`  | Agent type: `llm` or `deterministic` | `llm`     |
| `--model` | Model identifier (LLM agents only)   | `gpt-4.1` |


**Examples:**

```bash
# Add an LLM agent with default model
agentic add agent review-analyzer --type llm

# Add an LLM agent with a specific model
agentic add agent review-analyzer --type llm --model gpt-4.1-mini

# Add a deterministic agent
agentic add agent quality-scorer --type deterministic
```

Creates:

```
agents/
└── review-analyzer/
    ├── agent.yaml    # Pre-filled with type, model, and stub fields
    └── prompt.md      # Empty system prompt (LLM only)
```

#### `agentic add workflow`

Scaffold a new workflow with references to existing agents.

```bash
agentic add workflow <name> [options]
```

**Options:**


| Flag       | Description                                           | Default  |
| ---------- | ----------------------------------------------------- | -------- |
| `--agents` | Comma-separated list of agent IDs to include as steps | *(none)* |


**Examples:**

```bash
# Create a workflow with three agents
agentic add workflow product-review --agents product-fetcher,review-analyzer,review-writer

# Create an empty workflow
agentic add workflow product-review
```

Creates:

```
workflows/
└── product-review.yaml   # Pre-filled with steps referencing the specified agents
```

#### `agentic build`

Generate typed integration handles from YAML definitions.

```bash
agentic build [options]
```

**Options:**


| Flag     | Description                                           | Default      |
| -------- | ----------------------------------------------------- | ------------ |
| `--lang` | Target language: `typescript`, `python`, `ruby`, `go` | `typescript` |


**Examples:**

```bash
# Generate TypeScript handles
agentic build --lang typescript

# Generate Python handles
agentic build --lang python

# Generate Ruby handles
agentic build --lang ruby

# Generate Go handles
agentic build --lang go
```

#### `agentic list`

List all agents and workflows in the project.

```bash
agentic list
```

**Example output:**

```
Agents:
  product-fetcher    deterministic   handler: product-fetcher
  review-analyzer    llm             model: gpt-4.1
  comparison-researcher  llm         model: gpt-4.1
  review-writer      llm             model: gpt-4.1
  quality-scorer     deterministic   handler: scoring

Workflows:
  product-review     v1.0   5 steps (3 serial, 2 parallel)
```

---

## Language Support

The build step generates idiomatic code for each supported language. Below are complete examples of generated agent handles.

### TypeScript

```typescript
// @generated from agents/review-analyzer/agent.yaml
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
# @generated from agents/review-analyzer/agent.yaml
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
# @generated from agents/review-analyzer/agent.yaml
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
// @generated from agents/review-analyzer/agent.yaml
package agents

import engine "github.com/dominickcaponi/agentic-app-spec/runtime/go"

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

## Example Project: Product Review Pipeline

This is a complete, working example using [DummyJSON](https://dummyjson.com) (free, no authentication required). The pipeline fetches a product, analyzes its reviews, researches comparisons, writes a comprehensive review, and scores the quality.

### Pipeline Overview

```
product-fetcher (serial, short-circuit if product not found)
  |
  v
parallel:
  |- review-analyzer (retry 2x, backoff 500ms, fallback to gpt-4.1-mini)
  |- comparison-researcher (retry 2x, backoff 500ms, fallback to gpt-4.1-mini)
  |
  v
review-writer (retry 3x, backoff 1000ms, fallback to gpt-4.1-mini)
  |
  v
quality-scorer (deterministic)
```

### Agent Definitions

#### 1. product-fetcher (deterministic)

Calls `https://dummyjson.com/products/{id}` and extracts product data including reviews.

`**agents/product-fetcher/agent.yaml**`

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

The handler implementation (registered in your runtime):

```typescript
// handlers/product-fetcher.ts
export async function productFetcherHandler(input: { product_id: number }) {
  const res = await fetch(`https://dummyjson.com/products/${input.product_id}`);

  if (!res.ok) {
    return { found: false, error: `Product ${input.product_id} not found` };
  }

  const product = await res.json();

  return {
    found: true,
    id: product.id,
    title: product.title,
    description: product.description,
    category: product.category,
    price: product.price,
    rating: product.rating,
    brand: product.brand,
    reviews: JSON.stringify(product.reviews, null, 2),
  };
}
```

#### 2. review-analyzer (LLM)

Analyzes reviews for sentiment, pros/cons, and common themes.

`**agents/review-analyzer/agent.yaml**`

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

`**agents/review-analyzer/prompt.md**`

```markdown
You are an expert product review analyst. Your task is to analyze customer reviews
and extract structured insights.

## Guidelines

- Determine the overall sentiment: positive, negative, mixed, or neutral
- Identify specific pros and cons mentioned across reviews
- Find common themes that recur across multiple reviews
- Assign a score from 0-10 based on overall review sentiment
- Be objective — report what reviewers said, do not add your own opinion

## Handling Edge Cases

- If there are no reviews, return neutral sentiment with score 5
- If reviews are contradictory, mark sentiment as "mixed"
- If reviews are in different languages, analyze all of them
```

#### 3. comparison-researcher (LLM)

Generates comparisons with similar products based on the product category and LLM knowledge.

`**agents/comparison-researcher/agent.yaml**`

```yaml
name: Comparison Researcher
description: Researches and generates comparisons with similar products
type: llm
model: gpt-4.1
temperature: 0.5
schema: ComparisonResearch
user_message: |
  Research alternatives and comparisons for the following product:

  Product: "{{product_name}}"
  Category: {{category}}
  Price: ${{price}}

  Identify 2-3 comparable products and compare them on features,
  price, and value proposition.
input:
  product_name:
    type: string
    required: true
  category:
    type: string
    required: true
  price:
    type: number
    required: true
```

`**agents/comparison-researcher/prompt.md**`

```markdown
You are a product comparison expert. Your job is to identify competing products
and provide objective comparisons.

## Guidelines

- Identify 2-3 real, comparable products in the same category and price range
- Compare on: features, build quality, price, value for money
- Be specific — mention actual product names and concrete differences
- Include a recommendation on when to choose each alternative
- If you are unsure about specific details, say so rather than fabricating
```

#### 4. review-writer (LLM)

Takes the analysis and comparison data and writes a comprehensive review.

`**agents/review-writer/agent.yaml**`

```yaml
name: Review Writer
description: Writes a comprehensive product review from analysis and comparison data
type: llm
model: gpt-4.1
temperature: 0.7
schema: null
user_message: |
  Write a comprehensive, engaging product review for "{{product_name}}" using
  the following analysis and comparison data.

  Review Analysis:
  {{analysis}}

  Product Comparisons:
  {{comparisons}}

  Write a review that is informative, balanced, and helpful for potential buyers.
  Include sections for overview, pros/cons, how it compares to alternatives,
  and a final verdict.
input:
  product_name:
    type: string
    required: true
  analysis:
    type: object
    required: true
  comparisons:
    type: object
    required: true
```

`**agents/review-writer/prompt.md**`

```markdown
You are a professional product reviewer. Write engaging, informative, and balanced
product reviews.

## Style Guide

- Use a conversational but authoritative tone
- Structure the review with clear sections: Overview, Pros, Cons, Comparisons, Verdict
- Support claims with specific details from the analysis data
- Be honest about weaknesses — readers trust balanced reviews
- End with a clear verdict: who should buy this product and who should not
- Target length: 500-800 words
```

#### 5. quality-scorer (deterministic)

Scores the generated review on structure, completeness, and tone.

`**agents/quality-scorer/agent.yaml**`

```yaml
name: Quality Scorer
description: Scores the generated review on structure, completeness, and tone
type: deterministic
handler: scoring
input:
  review:
    type: string
    required: true
```

The handler implementation:

```typescript
// handlers/scoring.ts
export function scoringHandler(input: { review: string }) {
  const review = input.review;

  // Structure: check for expected sections
  const sections = ['overview', 'pros', 'cons', 'comparison', 'verdict'];
  const foundSections = sections.filter(s =>
    review.toLowerCase().includes(s)
  );
  const structureScore = Math.round((foundSections.length / sections.length) * 100);

  // Completeness: check minimum length and content density
  const wordCount = review.split(/\s+/).length;
  const completenessScore = Math.min(100, Math.round((wordCount / 600) * 100));

  // Tone: simple heuristic — check for balanced language
  const positiveWords = (review.match(/great|excellent|good|impressive|solid/gi) || []).length;
  const negativeWords = (review.match(/poor|bad|weak|lacking|disappointing/gi) || []).length;
  const hasBalance = positiveWords > 0 && negativeWords > 0;
  const toneScore = hasBalance ? 85 : 60;

  const overall = Math.round((structureScore + completenessScore + toneScore) / 3);

  return {
    overall,
    breakdown: {
      structure: structureScore,
      completeness: completenessScore,
      tone: toneScore,
    },
    suggestions: [
      ...(structureScore < 80 ? [`Missing sections: ${sections.filter(s => !review.toLowerCase().includes(s)).join(', ')}`] : []),
      ...(completenessScore < 70 ? ['Review is too short — aim for 500-800 words'] : []),
      ...(!hasBalance ? ['Review lacks balance — include both pros and cons'] : []),
    ],
  };
}
```

### Workflow Definition

`**workflows/product-review.yaml**`

```yaml
name: Product Review Pipeline
description: Fetches a product from DummyJSON, analyzes reviews, researches comparisons, writes a comprehensive review, and scores quality
version: "1.0"

input:
  product_id:
    type: number
    required: true

steps:
  - id: product-fetcher
    agent: product-fetcher
    input:
      product_id: $.input.product_id
    short_circuit:
      condition: "!output.found"
      defaults:
        review-analyzer:
          sentiment: null
          score: 0
          pros: []
          cons: []
          themes: []
          summary: "Product not found."
        comparison-researcher:
          comparisons: []
        review-writer:
          review: "Unable to generate review — product not found."
        quality-scorer:
          overall: 0
          breakdown:
            structure: 0
            completeness: 0
            tone: 0
          suggestions: []

  - parallel:
    - id: review-analyzer
      agent: review-analyzer
      input:
        product_name: $.steps.product-fetcher.output.title
        category: $.steps.product-fetcher.output.category
        description: $.steps.product-fetcher.output.description
        reviews: $.steps.product-fetcher.output.reviews
      retry:
        max_attempts: 3
        backoff_ms: 500
      fallback:
        agent: review-analyzer
        config:
          model: gpt-4.1-mini

    - id: comparison-researcher
      agent: comparison-researcher
      input:
        product_name: $.steps.product-fetcher.output.title
        category: $.steps.product-fetcher.output.category
        price: $.steps.product-fetcher.output.price
      retry:
        max_attempts: 3
        backoff_ms: 500
      fallback:
        agent: comparison-researcher
        config:
          model: gpt-4.1-mini

  - id: review-writer
    agent: review-writer
    input:
      product_name: $.steps.product-fetcher.output.title
      analysis: $.steps.review-analyzer.output
      comparisons: $.steps.comparison-researcher.output
    retry:
      max_attempts: 4
      backoff_ms: 1000
    fallback:
      agent: review-writer
      config:
        model: gpt-4.1-mini

  - id: quality-scorer
    agent: quality-scorer
    input:
      review: $.steps.review-writer.output.review

output:
  product: $.steps.product-fetcher.output
  analysis: $.steps.review-analyzer.output
  comparisons: $.steps.comparison-researcher.output
  review: $.steps.review-writer.output.review
  quality: $.steps.quality-scorer.output
```

### Running the Example

```bash
# Initialize the project
agentic init

# Add agents
agentic add agent product-fetcher --type deterministic
agentic add agent review-analyzer --type llm --model gpt-4.1
agentic add agent comparison-researcher --type llm --model gpt-4.1
agentic add agent review-writer --type llm --model gpt-4.1
agentic add agent quality-scorer --type deterministic

# Add workflow
agentic add workflow product-review --agents product-fetcher,review-analyzer,comparison-researcher,review-writer,quality-scorer

# Generate TypeScript handles
agentic build --lang typescript

# Call the workflow (from your app code)
# import { productReview } from './workflows/product-review/index.js';
# const result = await productReview({ product_id: 1 });
```

### Expected Response Envelope

```json
{
  "workflow": "Product Review Pipeline",
  "version": "1.0",
  "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": "success",
  "timestamps": {
    "started_at": "2026-03-22T10:00:00.000Z",
    "completed_at": "2026-03-22T10:00:12.345Z"
  },
  "metrics": {
    "total_latency_ms": 12345,
    "total_input_tokens": 2150,
    "total_output_tokens": 1830,
    "steps_executed": 5,
    "steps_skipped": 0
  },
  "steps": [
    {
      "id": "product-fetcher",
      "agent": "product-fetcher",
      "status": "success",
      "output": { "found": true, "id": 1, "title": "Essence Mascara Lash Princess", "..." : "..." },
      "metrics": { "latency_ms": 230, "input_tokens": 0, "output_tokens": 0 }
    },
    {
      "id": "review-analyzer",
      "agent": "review-analyzer",
      "status": "success",
      "output": { "sentiment": "positive", "score": 7.5, "pros": ["..."], "cons": ["..."], "themes": ["..."] },
      "metrics": { "latency_ms": 4200, "input_tokens": 850, "output_tokens": 620 },
      "attempts": 1
    },
    {
      "id": "comparison-researcher",
      "agent": "comparison-researcher",
      "status": "success",
      "output": { "comparisons": ["..."] },
      "metrics": { "latency_ms": 3800, "input_tokens": 400, "output_tokens": 510 },
      "attempts": 1
    },
    {
      "id": "review-writer",
      "agent": "review-writer",
      "status": "success",
      "output": { "review": "# Essence Mascara Lash Princess Review\n\n..." },
      "metrics": { "latency_ms": 5100, "input_tokens": 900, "output_tokens": 700 },
      "attempts": 1
    },
    {
      "id": "quality-scorer",
      "agent": "quality-scorer",
      "status": "success",
      "output": { "overall": 82, "breakdown": { "structure": 80, "completeness": 83, "tone": 85 }, "suggestions": [] },
      "metrics": { "latency_ms": 5, "input_tokens": 0, "output_tokens": 0 }
    }
  ],
  "result": {
    "product": { "found": true, "id": 1, "title": "Essence Mascara Lash Princess", "..." : "..." },
    "analysis": { "sentiment": "positive", "score": 7.5, "..." : "..." },
    "comparisons": { "comparisons": ["..."] },
    "review": "# Essence Mascara Lash Princess Review\n\n...",
    "quality": { "overall": 82, "breakdown": { "..." : "..." }, "suggestions": [] }
  }
}
```

---

## Architecture Decisions

### Why file-tree based?

The file-tree structure makes the spec **portable**, **version-controllable**, and **AI-readable**.

- **Portable.** The entire agent/workflow configuration is plain files. Copy the `agents/` and `workflows/` directories to a new project and they work. No database, no registry service, no configuration server.
- **Version-controllable.** Every change to an agent or workflow is a Git diff. You can review prompt changes in PRs, bisect regressions, and roll back to any point in history.
- **AI-readable.** AI coding assistants can read the directory structure, understand the relationships, and scaffold new agents or workflows without special tooling. The YAML format is the most common structured format in LLM training data.

### Why YAML?

- **Human-readable.** YAML is easier to read and write than JSON for configuration files, especially with multi-line strings (prompts).
- **Widely supported.** Every language has a YAML parser. Every developer has seen YAML.
- **Not code.** YAML separates configuration from implementation. You cannot accidentally put business logic in a YAML file. This forces clean separation of concerns.
- **Comment support.** Unlike JSON, YAML supports comments -- critical for documenting configuration choices inline.

### Why code generation?

The protoc-inspired code generation pattern provides:

- **Type safety.** Generated interfaces and structs catch type mismatches at compile time. If you rename an input parameter in the YAML but forget to update the caller, the compiler tells you.
- **IDE support.** Autocomplete, go-to-definition, hover documentation, and refactoring tools all work with generated code.
- **No runtime reflection.** Generated code imports the engine directly with concrete types. There is no dynamic YAML parsing, no string-based lookups, and no runtime type coercion at call sites.
- **Cross-language.** The same YAML definitions generate idiomatic code in TypeScript, Python, Ruby, and Go. Teams using multiple languages share a single source of truth.

### Why the envelope pattern?

The `WorkflowEnvelope` provides a **consistent contract** between the orchestrator and the caller:

- **Observable.** Every execution includes timing, token counts, and per-step status. You can build dashboards, set alerts, and track costs without modifying agent code.
- **Debuggable.** When something goes wrong, the envelope tells you which step failed, how many retries were attempted, whether the fallback was used, and what error occurred.
- **Composable.** Because every workflow returns the same envelope shape, workflows can be nested or chained without special handling.

### Why declarative retry/fallback/short-circuit?

Resilience logic (retry, fallback, short-circuit) is declared in the workflow YAML, not in agent code:

- **Separation of concerns.** Agents are pure functions -- they take input and produce output. They do not know about retries, fallbacks, or early exits. The orchestrator handles all resilience.
- **Centralized control.** Changing retry behavior is a YAML edit, not a code change. No redeployment of agent code required.
- **Testability.** Agents can be tested in isolation without mocking retry logic. The orchestrator can be tested with mock agents.

---

## Best Practices

### Agent Design

- **Keep agents small and focused.** Each agent should do one thing well (single responsibility principle). If an agent's prompt is longer than 500 words, consider splitting it into multiple agents.
- **Use descriptive IDs.** Use kebab-case names that describe the agent's purpose: `review-analyzer`, `sentiment-classifier`, `product-fetcher`. Avoid generic names like `agent1`, `step-2`, or `processor`.
- **Put complex prompts in prompt.md.** The `user_message` field in `agent.yaml` should contain the template with interpolation placeholders. Long, detailed instructions belong in `prompt.md` (the system prompt).
- **Use schemas for structured output.** Whenever you need to extract structured data from an LLM, define a schema. This eliminates parsing failures and guarantees the shape of the output.

### Workflow Design

- **Use short-circuit for early exit.** If a validation step can determine that the rest of the pipeline is pointless (e.g., "this is not a food image"), short-circuit immediately rather than wasting tokens on downstream steps.
- **Use fallback for degraded quality over total failure.** A common pattern is to fall back from a more capable model (e.g., `gpt-4.1`) to a cheaper/faster model (e.g., `gpt-4.1-mini`). The output may be lower quality, but the workflow succeeds.
- **Use retry for transient failures.** Rate limits, network timeouts, and temporary API errors are common. Retry with backoff handles these gracefully.
- **Set backoff_ms appropriately.** Use 1000ms or more for rate limit errors (the provider needs time to reset). Use 500ms for network timeouts. Use 200ms for parse errors that might succeed on retry.

### Resilience Strategy Guide


| Scenario                 | Strategy                    | Configuration                                                         |
| ------------------------ | --------------------------- | --------------------------------------------------------------------- |
| Rate limit (429)         | Retry with high backoff     | `retry: { max_attempts: 3, backoff_ms: 2000 }`                        |
| Network timeout          | Retry with moderate backoff | `retry: { max_attempts: 3, backoff_ms: 500 }`                         |
| Model quality issue      | Fallback to different model | `fallback: { agent: same, config: { model: "gpt-4.1-mini" } }`        |
| Validation failure       | Short-circuit               | `short_circuit: { condition: "!output.is_valid", defaults: { ... } }` |
| Intermittent parse error | Retry with low backoff      | `retry: { max_attempts: 2, backoff_ms: 200 }`                         |
| Critical failure         | No retry, fail fast         | *(omit retry and fallback)*                                           |


### General

- **Version your workflows.** Bump the `version` field when you change the workflow structure. This makes it easy to correlate response envelopes with specific workflow definitions.
- **Use consistent naming.** If an agent is called `review-analyzer`, its schema should be `ReviewAnalysis` (not `SentimentResult`), and its handler (if deterministic) should be `review-analyzer`.
- **Test agents in isolation.** Each agent should be testable with mock inputs, independent of the workflow. Generate handles and write unit tests against them.
- **Monitor via the envelope.** Use the `metrics` fields in the envelope to track latency, token usage, and failure rates. Set alerts on `steps_skipped` (unexpected short-circuits) and `used_fallback` (quality degradation).

---

## Glossary


| Term                       | Definition                                                                                                                                                                                                                     |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Agent**                  | A single unit of work in a pipeline. Either an LLM agent (calls a language model) or a deterministic agent (runs a handler function). Defined by an `agent.yaml` file and optionally a `prompt.md` file.                       |
| **Agent ID**               | The unique identifier for an agent, derived from its directory name under `agents/`. Always kebab-case. Example: `review-analyzer`.                                                                                            |
| **Binding**                | A path expression (e.g., `$.steps.review-analyzer.output.sentiment`) that references data in the execution context. Resolved at runtime just before a step executes.                                                           |
| **Deterministic Handler**  | A registered function that executes without calling an LLM. Used for API calls, calculations, data transformations, and other logic that does not require a language model. Referenced by the `handler` field in `agent.yaml`. |
| **Envelope**               | Short for **WorkflowEnvelope**. The standardized response structure returned by every workflow execution. Contains the result, per-step details, metrics, and status.                                                          |
| **Execution Context**      | The runtime state maintained by the orchestrator during workflow execution. Accumulates step outputs as they complete, making them available to subsequent steps via bindings.                                                 |
| **Fallback**               | A backup agent (or the same agent with different configuration) that runs if a step fails after all retry attempts. Configured via the `fallback` field on a workflow step. The fallback is attempted exactly once.            |
| **Parallel Group**         | A set of steps declared under a `parallel` key in the workflow. All steps in the group execute concurrently, and the orchestrator waits for all to complete before proceeding.                                                 |
| **Retry**                  | Automatic re-execution of a failed step. Configured with `max_attempts` (total tries including the first) and `backoff_ms` (base delay between attempts, multiplied by attempt number).                                        |
| **Schema**                 | A Zod, Pydantic, or JSON Schema definition registered by name. When referenced by an agent, it forces the LLM to produce structured output conforming to the schema.                                                           |
| **Schema Registry**        | A name-to-schema map that allows agents to reference schemas by string name rather than importing them directly.                                                                                                               |
| **Short-circuit**          | An early exit mechanism. When a step's `short_circuit.condition` evaluates to true, all remaining steps are skipped and filled with default values. The workflow status becomes `short_circuited`.                             |
| **Step**                   | A single node in a workflow. Each step invokes one agent with specific input bindings. Steps can be serial (one at a time) or grouped in parallel blocks.                                                                      |
| **Step Result**            | The per-step record in the response envelope. Contains the step's ID, agent, status, output, metrics, and optional fields like `attempts`, `used_fallback`, and `error`.                                                       |
| **Template Interpolation** | The `{{key}}` syntax used in `user_message` to inject runtime values into the user prompt. Supports nested paths (`{{key.sub.deep}}`).                                                                                         |
| **Workflow**               | An orchestrated pipeline of steps (agents) with defined data flow, execution order, and resilience configuration. Defined by a YAML file in the `workflows/` directory.                                                        |
| **Workflow Input**         | Parameters declared in the workflow's `input` section. Provided by the caller when invoking the workflow. Available to steps via `$.input.<key>` bindings.                                                                     |
| **Workflow Output**        | The `output` section of a workflow definition. Maps named keys to binding expressions. Resolved after all steps complete and returned as the `result` field in the envelope.                                                   |


---

## License

This specification is open for adoption. Use it to build your own agent orchestration layer in any language.
