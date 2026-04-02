# Routing Agents: Conditional Branching for Multi-Agent Workflows

Workflows in Agentic App Spec have always been linear sequences with a twist -- you can run steps in parallel and short-circuit early when things go wrong. But real-world pipelines are rarely straight lines. Sometimes the next step depends on what you learned in the previous one. A classification task might need one specialist for food items and a completely different one for cleaning supplies. Until now, there was no clean way to express that.

Version 2.0 introduces the **router primitive** -- a new workflow-level construct that lets you define conditional branching directly in your workflow YAML. This post walks through the design, shows a complete grocery classification example in Python, and explains why routers are not agents.

---

## The Problem

Consider a grocery store that wants to classify incoming items. At the top level, you need to decide: is this item food or non-food? If it is food, you need a second decision: is it meat, a vegetable, or canned goods? Each leaf category has its own specialist classifier that knows the domain-specific details.

Before routers, you had two bad options:

1. **One giant agent.** A single LLM call that tries to handle all classification levels at once. This works for simple cases but the prompt gets bloated, the context is unfocused, and accuracy drops as you add categories.

2. **Hard-coded orchestration.** Write custom application code that calls agents in sequence, inspects outputs, and dispatches to the next agent. This works but violates the core principle of the spec -- all orchestration should be visible in the workflow definition, not hidden in code.

Routers give you a third option: declare the branching tree in YAML, and let each routing decision be a focused LLM call (or a deterministic function) that only needs to choose between a small number of options.

---

## What Is a Router?

A router is a workflow primitive -- not an agent. This distinction matters.

**Agents do work.** They classify meat cuts, analyze reviews, write articles. They produce useful output.

**Routers make flow-control decisions.** They look at the input, consider the available routes, and pick one. Their only output is a route key. They exist to direct traffic, not to produce content.

This separation is enforced by design. Routers live in their own `routers/` directory (not `agents/`). They have their own schema (`router.yaml` instead of `agent.yaml`). And the workflow syntax makes it clear when you are routing versus when you are doing work.

Routers can use an LLM to make their decision (`strategy: llm`) or a registered handler function (`strategy: deterministic`). Either way, they return `{ "route": "<key>" }` and the runtime dispatches to the declared target.

---

## The Grocery Classifier

Here is the complete project structure:

```
routers/
  food-router/
    router.yaml
    prompt.md
  food-subtype-router/
    router.yaml
    prompt.md

agents/
  classify-food/
    agent.yaml
    prompt.md
  classify-non-food/
    agent.yaml
    prompt.md
  classify-meat/
    agent.yaml
    prompt.md
  classify-vegetable/
    agent.yaml
    prompt.md
  classify-canned/
    agent.yaml
    prompt.md

workflows/
  grocery-classify.yaml
```

### The Routers

The top-level router decides food vs. non-food:

```yaml
# routers/food-router/router.yaml
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

Its prompt gives the LLM context for making the decision:

```markdown
<!-- routers/food-router/prompt.md -->
You are a grocery store classification system. Your job is to determine
whether a given item is a food product or a non-food product.

Food items include: fresh produce, meat, seafood, dairy, baked goods,
canned food, frozen food, beverages, snacks, condiments, spices, grains,
pasta, cereal, and any other consumable item intended for eating or drinking.

Non-food items include: cleaning supplies, paper products, personal care
items, pet supplies, household goods, kitchenware, and any other
non-consumable product.
```

The food-subtype router narrows further:

```yaml
# routers/food-subtype-router/router.yaml
name: Food Subtype Router
description: Classifies a food item into meat, vegetable, or canned
strategy: llm
model: gpt-4.1-mini
temperature: 0

input:
  item_name:
    type: string
    required: true
```

Notice that routers do not need to know agent IDs. They just pick a route key (`food`, `non_food`, `meat`, `vegetable`, `canned`). The workflow YAML maps those keys to targets.

### The Workflow

This is where the branching tree lives:

```yaml
# workflows/grocery-classify.yaml
name: grocery-classify
version: "1.0"
description: Classifies grocery items through a hierarchical routing tree

input:
  item_name:
    type: string
    required: true

steps:
  - route:
      id: classify
      router: food-router
      input:
        item_name: $.input.item_name
      routes:
        food:
          route:
            id: food_sub
            router: food-subtype-router
            input:
              item_name: $.input.item_name
            routes:
              meat: classify-meat
              vegetable: classify-vegetable
              canned: classify-canned
              _none:
                short_circuit: true
                defaults: {}
        non_food: classify-non-food
        _none:
          short_circuit: true
          defaults: {}

output:
  classification: $.steps.classify.output
```

A few things to notice:

**Routes nest.** The `food` route does not point to an agent -- it points to another `route` block. The food-subtype-router then picks between `meat`, `vegetable`, and `canned`. You can nest to any depth.

**`_none` is the bail-out.** If the router decides none of the options fit, it returns `_none` and the workflow short-circuits with defaults. This prevents the system from forcing a bad classification.

**Route targets are flexible.** A route value can be a string (agent ID with pass-through input), an object with explicit input bindings, a nested route block, or a full workflow reference.

### Running It in Python

Here is the Python application code that runs the workflow:

```python
import asyncio
import json
from agentic_engine import orchestrate

async def main():
    result = await orchestrate("grocery-classify", {
        "item_name": "chicken breast"
    })

    print(f"Status: {result['status']}")
    print(json.dumps(result["result"], indent=2))

asyncio.run(main())
```

For the input `"chicken breast"`, the output looks like this:

```json
{
  "classification": {
    "route": "food",
    "router_output": { "route": "food" },
    "result": {
      "route": "meat",
      "router_output": { "route": "meat" },
      "result": {
        "protein_type": "poultry",
        "cut": "breast",
        "is_frozen": false,
        "is_processed": false,
        "cooking_methods": ["grilling", "baking", "pan-searing", "poaching"]
      }
    }
  }
}
```

The output is **wrapped for auditability**. Every routing decision is preserved: you can see the full path (`food` -> `meat`) and the router's raw output at each level. The leaf agent's actual classification lives at `result.result.result` for a two-level tree. Verbose, but unambiguous -- and the routing metadata is invaluable for debugging and logging.

For `"paper towels"`:

```json
{
  "classification": {
    "route": "non_food",
    "router_output": { "route": "non_food" },
    "result": {
      "category": "paper products",
      "subcategory": "kitchen towels",
      "typical_aisle": "paper goods"
    }
  }
}
```

This item hit the `non_food` route directly -- no sub-routing needed. The classify-non-food agent handled it in one step.

---

## Why Routers Are Not Agents

This was a deliberate design decision. We considered making routers a type of agent (`type: router`) but rejected it because of a core principle:

**Agents cannot compose other agents in code.** All agent composition must happen through workflow definitions. This prevents hidden side effects and keeps orchestration logic visible in the spec.

If a router were an agent, it would need to invoke other agents -- violating this principle. By making the router a workflow construct, the branching tree is declared in YAML where anyone (including AI assistants helping you develop) can see it. The router makes a decision. The workflow dispatches to the target. Clean separation.

This also means routers do not appear in your handler registry or generated stubs. They are invisible to your application code. The runtime handles them internally during workflow execution.

---

## Retry, Fallback, and Error Handling

Routers support the same retry and fallback mechanisms as regular steps:

```yaml
- route:
    id: classify
    router: food-router
    input:
      item_name: $.input.item_name
    retry:
      max_attempts: 2
      backoff_ms: 500
    fallback:
      router: food-router-simple
    routes:
      food: classify-food
      non_food: classify-non-food
      _none:
        short_circuit: true
        defaults: {}
```

An important detail: **retry and fallback apply to the routing decision, not to the dispatched agent.** If the food-router's LLM call fails, it retries. If the classify-food agent fails after being dispatched, that error propagates up -- the router does not retry its decision. The dispatched agent handles its own errors (using its own retry/fallback config if it has one).

---

## What's Next

The router primitive opens up several patterns we plan to explore:

- **Deterministic routers for rule-based branching** -- use a registered handler function instead of an LLM for fast, predictable routing based on input fields.
- **Multi-level classification pipelines** -- chain routers to build taxonomies: department -> category -> subcategory -> item type.
- **A/B testing** -- route a percentage of traffic to different agents to compare output quality.

Routers are available in all four runtimes: TypeScript, Python, Go, and Ruby. Check the `example/` directory in the repo for the complete grocery classifier example.

---

## Upgrading

Version 2.0 is a breaking change for runtime consumers. The `WorkflowEntry` union type now includes `RouteEntry` alongside `WorkflowStep` and `ParallelGroup`. If your code exhaustively pattern-matches on workflow entry types, you will need to add a case for route entries.

Existing workflows without route blocks continue to work with no changes. The schema change is additive -- `routeBlock` is a new option in the `steps.items.oneOf` array.

Update your runtime dependency to the latest version for your language:

```bash
# TypeScript
npm install agentic-app-engine@latest

# Python
pip install agentic-engine --upgrade

# Go
go get github.com/dcaponi/agentic-app-spec/runtime/go@latest

# Ruby
bundle update agentic_engine
```
