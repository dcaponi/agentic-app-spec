# Agentic App Spec v3: Cleaner Structure, Routing Agents, and Any LLM

Three breaking changes ship in v3. All three are small in scope, straightforward to migrate, and fix things that were slightly wrong from the start. This post covers each one, explains the reasoning, and ends with the exact steps to upgrade from v2.

---

## 1. Everything moves under `agentic-spec/`

In v2, the spec directories -- `agents/`, `routers/`, `workflows/`, `schemas/` -- all lived at your project root. That made the initial `agentic init` experience feel tidy, but it created a naming collision the moment your project grew. Any application code that happened to use an `agents/` or `schemas/` directory conflicted with the spec. Monorepos with existing structure had to work around it.

In v3, all spec definitions live under `agentic-spec/`:

```
agentic-spec/
  agents/
    review-analyzer/
      agent.yaml
      prompt.md
  routing-agents/
    food-router/
      routing-agent.yaml
      prompt.md
  workflows/
    product-review.yaml
  schemas/
    review_analysis.json
```

The `agentic-spec/` directory is the spec's home. Your application code lives wherever it has always lived. There is no ambiguity about what belongs to the spec and what belongs to the project.

The practical effect is that `agentic init` creates one directory instead of four. The CLI, code generation, and all runtimes look for spec definitions under `agentic-spec/` by default. Nothing changes about the internal structure of those directories -- only the root location.

---

## 2. Routers are now routing-agents

In v2, routers lived in `routers/` and their definition files were named `router.yaml`. Version 2 also introduced the argument that routers are "not agents" -- they're workflow constructs, they make flow-control decisions, and keeping them separate from agents prevents confusion.

That argument held up right until we looked at how the runtime actually executes them. A routing-agent receives input, runs an LLM call (or a deterministic handler), and produces output that the workflow uses to dispatch to the next step. That is exactly what an agent does. The only difference is that a routing-agent's output is interpreted by the workflow runtime to select a branch rather than passed directly to the next step as data.

So in v3, routers are renamed to **routing-agents**:

- `routers/<id>/router.yaml` becomes `agentic-spec/routing-agents/<id>/routing-agent.yaml`
- The `routers:` field in workflow YAML becomes `routing_agent:`
- The `router:` key on a route step becomes `routing_agent:`

The definition file itself changes minimally -- just the filename and the reference field:

```yaml
# agentic-spec/routing-agents/food-router/routing-agent.yaml
name: Food Router
description: Decides if a grocery item is food or non-food
strategy: llm
model: gpt-4.1-mini
temperature: 0

input:
  item_name:
    type: string
    required: true
```

And in your workflow:

```yaml
steps:
  - route:
      id: classify
      routing_agent: food-router   # was: router: food-router
      input:
        item_name: $.input.item_name
      routes:
        food: classify-food
        non_food: classify-non-food
```

**Why keep them separate from regular agents at all?** Because routing-agents can invoke other agents indirectly -- through the workflow runtime's dispatch mechanism -- and that is a capability regular agents should not have. Application code should never instantiate a routing-agent directly and call it like a function. Keeping them in a separate directory enforces that boundary structurally, the same way the spec enforces that orchestration logic lives in workflow YAML rather than in agent code.

The rename makes the identity clear ("this is an agent") while the directory separation preserves the boundary ("but not one your application code should hold a direct reference to").

---

## 3. The `provider` field is gone -- use `base_url` instead

In v2, agent YAML accepted a `provider` enum with values like `openai`, `anthropic`, `google`. Adding a new provider meant waiting for a spec update.

That design made sense two years ago. It does not make sense now. Almost every major LLM provider speaks the OpenAI protocol -- the same base URL convention, the same `/v1/chat/completions` endpoint, the same request and response shape. Building a custom integration per provider is unnecessary.

In v3, the `provider` field is removed. Instead, set `base_url` and `api_key_env` directly in your agent YAML:

```yaml
# Local Ollama
name: Local Classifier
type: llm
model: llama3.2
base_url: http://localhost:11434/v1
api_key_env: OLLAMA_API_KEY   # can be any non-empty string for Ollama
```

```yaml
# DeepSeek remote
name: DeepSeek Reasoner
type: llm
model: deepseek-reasoner
base_url: https://api.deepseek.com/v1
api_key_env: DEEPSEEK_API_KEY
```

```yaml
# HuggingFace Inference API
name: Mistral Agent
type: llm
model: mistralai/Mistral-7B-Instruct-v0.3
base_url: https://api-inference.huggingface.co/v1
api_key_env: HF_TOKEN
```

This covers roughly 90% of providers without any spec changes. For the remaining cases, check whether your provider documents an OpenAI-compatible endpoint -- most do, even if it is not their primary API.

**Anthropic is the exception.** The Anthropic API uses a different protocol than OpenAI. Rather than require you to set a custom base URL that the runtime would need special handling for anyway, the runtime auto-detects Anthropic from the model prefix: any model name starting with `claude-` routes through the Anthropic client automatically, using `ANTHROPIC_API_KEY` from the environment. You do not need to set `base_url` or `api_key_env` for Anthropic models.

```yaml
# Anthropic -- no base_url needed
name: Review Writer
type: llm
model: claude-sonnet-4-20250514
temperature: 0.7
```

For OpenAI models, `base_url` also defaults to `https://api.openai.com/v1` if omitted, so existing agents that only set `model: gpt-4.1` continue to work after updating the runtime -- as long as you remove the `provider: openai` line.

---

## Migrating from v2

**1. Move your spec directories.**

```bash
mkdir agentic-spec
mv agents agentic-spec/
mv routers agentic-spec/routing-agents
mv workflows agentic-spec/
mv schemas agentic-spec/
```

**2. Rename `router.yaml` files to `routing-agent.yaml`.**

```bash
for f in $(find agentic-spec/routing-agents -name "router.yaml"); do
  mv "$f" "$(dirname $f)/routing-agent.yaml"
done
```

**3. Update workflow YAML.** Replace `router:` with `routing_agent:` in every `route` step.

**4. Remove `provider:` fields from agent YAML.** If the agent used `provider: openai`, drop the field -- `base_url` defaults to OpenAI. If it used `provider: anthropic`, drop the field -- Anthropic is auto-detected from the `claude-` prefix. For any other provider, add `base_url` and `api_key_env` pointing at the OpenAI-compatible endpoint.

**5. Rebuild and update runtimes.**

```bash
agentic build --lang go   # or typescript, python, ruby

# TypeScript
npm install agentic-app-engine@latest

# Python
pip install agentic-engine --upgrade

# Go
go get github.com/dcaponi/agentic-app-spec/runtime/go@latest

# Ruby
bundle update agentic_engine
```

The generated code and runtime will look for spec definitions under `agentic-spec/` automatically after the update.

---

These three changes clean up the rough edges that accumulated in v1 and v2. The project root is less cluttered. The routing-agent name tells you what you're looking at. And you can point an agent at any OpenAI-compatible endpoint without waiting for a spec update. If you hit any migration issues, open an issue on GitHub.
