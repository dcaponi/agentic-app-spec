# Agentic App Spec: A File-Tree Standard for Multi-Agent AI Pipelines

If you have built anything non-trivial with LLMs, you know the feeling. You start with one prompt in one file. Then you add a second agent. Then a retry loop. Then a fallback model. Then a third agent that depends on the first two. Before long your "AI feature" is a tangle of prompt strings buried in application code, retry logic threaded through business logic, and a growing sense that adding a sixth agent will require touching every file in the project.

Agentic App Spec is an attempt to fix that. It is a file-tree based specification for defining AI agent pipelines and multi-agent workflows. You describe your agents in YAML and Markdown. You describe your orchestration -- the order, parallelism, retries, fallbacks, short-circuits -- in a separate YAML workflow file. Then a CLI reads those definitions and generates typed function handles you can call from your application code, the same way `protoc` generates stubs from `.proto` files.

This post explains why, how, and what comes next.

---

## The Problem

Most multi-agent AI applications today are structured in one of two ways, and both have serious drawbacks.

**Approach 1: Everything in code.** Prompts are template literals in your source files. Retry logic wraps your API calls. Fallback logic wraps your retry logic. Adding a new agent means creating a new function, updating the orchestration function that calls it, updating the types, and hoping you remembered to handle the error case. There is no single place to look to understand what agents exist or how they compose.

**Approach 2: Framework magic.** You adopt a framework that provides decorators, class hierarchies, or a DSL to define agents. This helps with structure, but now your agent definitions are coupled to one language and one framework. Your prompts are still embedded in code (or passed as constructor arguments). And the orchestration -- which agent runs when, what happens on failure -- is usually imperative, buried in method calls or callback chains.

The common thread is that agent identity (what is this agent?), agent behavior (what does it do?), and orchestration policy (when does it run, what if it fails?) are all mixed together. Change one, and you risk breaking the others. Worse, a new team member -- or an AI assistant helping you develop -- has no straightforward way to understand the system's structure by reading the codebase.

---

## The Inspiration

The main inspirationfor this idea came from gRPC and Protocol Buffers. In the gRPC world, you write a `.proto` file that declares your service contract -- the methods, the request types, the response types. Then `protoc` reads that declaration and generates typed client and server stubs in your target language. Your application code never parses `.proto` files at runtime. It just calls the generated functions, with full type safety and IDE support.

Agentic App Spec combines these: file-tree agent definitions (the "what") plus declarative workflow orchestration (the "how") plus code generation (the "bridge to your app").

---

## What Agentic App Spec Is

At its core, the spec defines three things:

**1. Agents as folders.** Each agent lives at `agents/<id>/agent.yaml` with an optional `prompt.md` alongside it. The YAML describes the agent's metadata: its model, temperature, response schema, input parameters, and whether it is an LLM agent or a deterministic handler.

```
agents/
  review-analyzer/
    agent.yaml
    prompt.md
  product-fetcher/
    agent.yaml
  quality-scorer/
    agent.yaml
```

A typical `agent.yaml` for an LLM agent looks like this:

```yaml
id: review-analyzer
name: Review Analyzer
type: llm
model: gpt-4.1
temperature: 0.3
input:
  product_name: string
  reviews: "array<object>"
output_schema: ./schemas/review-analysis.json
system_message: prompt.md
user_message: |
  Analyze the reviews for "{{product_name}}".
  Reviews: {{reviews}}
```

A deterministic agent (one that runs code, not an LLM) omits the model and prompt fields and declares a handler:

```yaml
id: product-fetcher
name: Product Fetcher
type: deterministic
handler: product_fetch
input:
  product_id: number
```

**2. Workflows as YAML orchestration.** A workflow file at `workflows/<name>.yaml` composes agents into a pipeline. It declares the input the workflow accepts, the steps to execute (in order, in parallel, or both), and the output to return. Critically, it also declares the resilience policy for each step: retries, fallbacks, and short-circuit conditions.

```yaml
name: product-review
input:
  product_id: number

steps:
  - id: fetch
    agent: product-fetcher
    input:
      product_id: "$.input.product_id"
    short_circuit:
      condition: "$.steps.fetch.output == null"
      message: "Product not found"

  - parallel:
      - id: analyze
        agent: review-analyzer
        input:
          product_name: "$.steps.fetch.output.title"
          reviews: "$.steps.fetch.output.reviews"
        retry:
          max_attempts: 2
      - id: research
        agent: comparison-researcher
        input:
          product_name: "$.steps.fetch.output.title"
          category: "$.steps.fetch.output.category"

  - id: write
    agent: review-writer
    input:
      product_name: "$.steps.fetch.output.title"
      analysis: "$.steps.analyze.output"
      alternatives: "$.steps.research.output"
    retry:
      max_attempts: 2
      fallback:
        agent: review-writer
        model_override: gpt-4.1-mini

  - id: score
    agent: quality-scorer
    input:
      article: "$.steps.write.output"

output:
  article: "$.steps.write.output"
  scores: "$.steps.score.output"
  analysis: "$.steps.analyze.output"
```

**3. Code generation.** A CLI tool reads the agent and workflow definitions and generates typed function handles for your target language. In TypeScript, you get something like:

```typescript
import { productReview } from "./generated/workflows";

const result = await productReview({ product_id: 42 });
console.log(result.output.article);
console.log(result.metrics.total_duration_ms);
```

Your application code never touches YAML at runtime. It calls a typed function, gets a typed response.

---

## How It Works

When a generated workflow function is called, the execution follows a predictable path:

1. **Input resolution.** The workflow receives its declared input (e.g., `{ product_id: 42 }`).
2. **Step execution.** Each step resolves its own inputs by evaluating binding expressions against the workflow context. The expression `$.steps.fetch.output.title` means "look up the output of the step with id `fetch` and read its `title` field."
3. **Parallelism.** Steps declared inside a `parallel` block execute concurrently. The orchestrator waits for all parallel steps to complete before moving to the next sequential step.
4. **Resilience.** If a step fails and has a `retry` policy, the orchestrator retries it (up to `max_attempts`). If retries are exhausted and a `fallback` is declared, the orchestrator executes the fallback (which can be a different agent, a different model, or both). If the step has a `short_circuit` condition and that condition evaluates to true after execution, the workflow exits early with the short-circuit message.
5. **Envelope construction.** After all steps complete, the orchestrator assembles the response envelope and returns it.

The key insight is that none of this resilience logic lives in the agent. The `review-analyzer` agent has no idea it might be retried. The `review-writer` agent has no idea there is a fallback to a cheaper model. Each agent is a pure unit of work. The orchestrator handles everything else.

---

## Why Declarative Orchestration

It is tempting to put retry logic in your application code. A simple try-catch with a loop is only a few lines. But this approach scales poorly for three reasons.

**Visibility.** When orchestration is in code, understanding the resilience policy of a pipeline requires reading the code. When it is in YAML, you can open one file and see, at a glance, which steps retry, which have fallbacks, and which can short-circuit. A product manager can read it. An on-call engineer can read it. An AI assistant can read it.

**Changeability.** Want to add a retry to a step? Edit the YAML. Want to swap the fallback model from `gpt-4.1-mini` to `claude-sonnet-4-20250514`? Edit the YAML. No code changes, no redeployment of business logic, no risk of introducing a bug in your handler.

**Separation of concerns.** The agent's job is to do its task well. The orchestrator's job is to make the pipeline resilient. Mixing these concerns means your agent code gets cluttered with infrastructure logic, and your infrastructure logic gets tangled with prompt engineering. Keeping them separate makes both easier to reason about and test.

---

## Why Code Generation

If you have used gRPC, the code generation pattern will feel familiar. If you have not, here is the reasoning.

The YAML files are the **contract**. They declare what agents exist, what inputs they accept, what outputs they produce, and how they compose. The generated code is the **bridge** between that contract and your application.

This matters for several reasons:

**Type safety.** The generated function signatures reflect the declared inputs and outputs. If an agent expects `product_id: number`, the generated function will enforce that at compile time. Your IDE provides autocompletion. Typos are caught before runtime.

**Language agnosticism.** The same YAML definitions can generate TypeScript, Python, Ruby, or Go code. Your agent definitions are not locked to a language or framework. A team that writes their backend in Go and their scripts in Python can share the same agent and workflow definitions.

**No runtime YAML parsing.** Your application does not need a YAML parser. It does not need to resolve binding expressions at runtime (the generated code handles that). The YAML is a build-time artifact, not a runtime dependency.

**AI-friendliness.** An AI coding assistant can read the YAML, understand the pipeline structure, and generate or modify agents and workflows. The declarative format is far easier for an LLM to work with than imperative orchestration code scattered across multiple files.

---

## The Response Envelope

Every workflow execution returns the same response shape, regardless of which agents ran or how many steps completed:

```json
{
  "workflow": "product-review",
  "status": "success",
  "output": {
    "article": "...",
    "scores": { "overall": 82, "depth": 78 },
    "analysis": { "sentiment": "positive", "pros": [...] }
  },
  "metrics": {
    "total_duration_ms": 4523,
    "steps": {
      "fetch": { "duration_ms": 210, "retries": 0 },
      "analyze": { "duration_ms": 1802, "retries": 0 },
      "research": { "duration_ms": 2105, "retries": 0 },
      "write": { "duration_ms": 1980, "retries": 1, "used_fallback": false },
      "score": { "duration_ms": 45, "retries": 0 }
    }
  }
}
```

This consistency pays dividends in three areas:

**Observability.** You can build dashboards, alerts, and logs that work across all workflows because the shape is always the same. Track retry rates, fallback usage, and step durations without custom instrumentation per workflow.

**Debugging.** When something goes wrong, the envelope tells you which step failed, how many times it was retried, whether a fallback was invoked, and how long each step took. You do not need to add logging to each agent.

**Client predictability.** Consumers of the workflow -- whether a frontend, another service, or a CLI -- always know what shape to expect. Parsing the response does not require workflow-specific logic.

---

## What's Next

Agentic App Spec is still early. Here is what is on the roadmap:

**The CLI** (`npx agentic`) is the primary interface. `agentic init` scaffolds a project. `agentic build --lang typescript` generates code. Future commands will include `agentic validate` (check YAML correctness without building) and `agentic run` (execute a workflow from the terminal for testing).

**Multi-language support.** TypeScript is the first target. Python, Ruby, and Go generators are planned. The spec itself is language-agnostic; only the code generation layer changes.

**AI-readable definitions.** One of the more exciting possibilities is that the spec's declarative, file-tree structure makes it straightforward for AI assistants to scaffold new agents, modify workflows, and reason about pipeline structure. The vision is a development loop where you describe what you want, an AI writes the YAML and prompts, the CLI generates the code, and you run it. Editing the pipeline means editing the YAML, not refactoring application code.

**Community schemas.** As patterns emerge (e.g., "fetch-analyze-generate" pipelines, "fan-out-fan-in" workflows), we want to collect and share reusable workflow templates and agent definitions.

If you want to see it in action, the companion tutorial walks through building a complete product review pipeline from scratch -- five agents, parallel execution, retry with fallback, and short-circuit logic, all defined in YAML and generated into typed TypeScript.

The specification, CLI, and example project are all open source. Contributions, feedback, and questions are welcome.