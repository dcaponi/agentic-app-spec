# Building a Go API with Agentic App Spec: Three Endpoints, Zero Orchestration Code

Most AI-powered backends follow the same pattern. You start with one LLM call, wrap it in an HTTP handler, and ship it. Then someone asks for a second model in the pipeline. Then a third. Before long you're writing retry loops, threading data between steps, bolting on fallback logic, and parallelizing calls with goroutines and WaitGroups — all tangled into your application code. Every new workflow means more Go code, more tests, more things that can quietly break.

What if the orchestration wasn't in your code at all?

This post walks through a real Go project that exposes three HTTP endpoints — a full product review pipeline, a streamlined version without comparison research, and a standalone comparison agent — all backed by the same set of agents. The punchline: adding the second workflow required zero new Go code. Just a new YAML file.

## The project at a glance

The project is a product review API. You give it a DummyJSON product ID, and it returns a structured review — fetched product data, sentiment analysis, competitor comparison, a written article, and a quality score. The interesting part is how little application code it takes.

The directory looks like this:

```
test-go-project/
  agents/
    product-fetcher/agent.yaml
    review-analyzer/agent.yaml + prompt.md
    comparison-researcher/agent.yaml + prompt.md
    review-writer/agent.yaml + prompt.md
    quality-scorer/agent.yaml
  workflows/
    product-review.yaml
    quick-review.yaml
  schemas/
    review_analysis.json
    comparison_report.json
  main.go
  go.mod
```

Five agents. Two workflows. Two JSON schemas. One Go file.

## What the Go code actually does

Here's the entire `main.go` in terms of what it's responsible for:

1. **Register two deterministic handlers** — `product_fetch` (calls the DummyJSON API) and `quality_scoring` (scores the generated article with a heuristic).
2. **Mount three HTTP endpoints** that delegate to the engine.
3. That's it.

The three endpoints look like this:

```go
func main() {
    engine.RegisterHandler("product_fetch", productFetchHandler)
    engine.RegisterHandler("quality_scoring", qualityScoringHandler)

    http.HandleFunc("/review", handleReview)
    http.HandleFunc("/quick-review", handleQuickReview)
    http.HandleFunc("/comparison", handleComparison)

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

The `/review` and `/quick-review` handlers are nearly identical — the only difference is the workflow name passed to `engine.Orchestrate`:

```go
// Full pipeline: fetch -> [review + comparison in parallel] -> article -> quality
envelope, err := engine.Orchestrate("product-review", map[string]interface{}{
    "product_id": productID,
})
```

```go
// Streamlined: fetch -> review -> article -> quality (no comparison)
envelope, err := engine.Orchestrate("quick-review", map[string]interface{}{
    "product_id": productID,
})
```

The `/comparison` endpoint skips the workflow entirely and invokes a single agent:

```go
result, err := engine.InvokeAgent("comparison-researcher", map[string]interface{}{
    "product_name": product["title"],
    "category":     product["category"],
    "price":        product["price"],
    "brand":        product["brand"],
    "description":  product["description"],
})
```

Three different behaviors. The Go code doesn't know or care which agents run, in what order, whether they run in parallel, or what happens when one fails. That's all in the YAML.

## The full workflow

`workflows/product-review.yaml` defines the complete pipeline:

```yaml
name: product-review
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
    short_circuit:
      condition: "!output.found"
      defaults:
        review_analysis: { sentiment: "unknown", pros: [], cons: [], themes: [], quality_rating: 0 }
        comparison: { alternatives: [], recommendation: "N/A" }
        article: { title: "Product Not Found", overview: "...", pros_cons: "", comparison: "", verdict: "N/A", rating: 0 }
        quality: { structure_score: 0, completeness_score: 0, tone_score: 0, overall: 0 }

  - parallel:
    - id: review_analysis
      agent: review-analyzer
      input:
        product_name: $.steps.fetch.output.title
        category: $.steps.fetch.output.category
        price: $.steps.fetch.output.price
        rating: $.steps.fetch.output.rating
        reviews_text: $.steps.fetch.output.reviews_text
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
        brand: $.steps.fetch.output.brand
        description: $.steps.fetch.output.description
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
      brand: $.steps.fetch.output.brand
      price: $.steps.fetch.output.price
      review_analysis: $.steps.review_analysis.output
      comparison_report: $.steps.comparison.output

  - id: quality
    agent: quality-scorer
    input:
      review_article: $.steps.article.output
      review_analysis: $.steps.review_analysis.output
      comparison_report: $.steps.comparison.output

output:
  product: $.steps.fetch.output
  review_analysis: $.steps.review_analysis.output
  comparison: $.steps.comparison.output
  article: $.steps.article.output
  quality_scores: $.steps.quality.output
```

A lot is happening here and none of it is in Go:

- **Data binding**: `$.steps.fetch.output.title` threads the product title from step 1 into steps 2, 3, and 4. No intermediate variables, no struct passing.
- **Parallel execution**: The `review_analysis` and `comparison` steps run concurrently in goroutines. The engine handles the WaitGroup.
- **Retry with backoff**: Both LLM steps retry up to 2 times with 500ms backoff before falling back.
- **Fallback**: If retries are exhausted, the same agent is re-invoked with a cheaper model (`gpt-4.1-mini`).
- **Short-circuit**: If the product isn't found, the entire pipeline skips the remaining steps and returns default values. No `if err != nil` chain in your handler.
- **Output mapping**: The `output` section assembles the final response from across all steps.

## Adding a workflow without touching Go

The `quick-review` workflow strips out the comparison step. Here's what that looks like:

```yaml
name: quick-review
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
    short_circuit:
      condition: "!output.found"
      defaults:
        review_analysis: { sentiment: "unknown", pros: [], cons: [], themes: [], quality_rating: 0 }
        article: { title: "Product Not Found", overview: "...", pros_cons: "", comparison: "", verdict: "N/A", rating: 0 }
        quality: { structure_score: 0, completeness_score: 0, tone_score: 0, overall: 0 }

  - id: review_analysis
    agent: review-analyzer
    input:
      product_name: $.steps.fetch.output.title
      category: $.steps.fetch.output.category
      price: $.steps.fetch.output.price
      rating: $.steps.fetch.output.rating
      reviews_text: $.steps.fetch.output.reviews_text

  - id: article
    agent: review-writer
    input:
      product_name: $.steps.fetch.output.title
      brand: $.steps.fetch.output.brand
      price: $.steps.fetch.output.price
      review_analysis: $.steps.review_analysis.output
      comparison_report: {}

  - id: quality
    agent: quality-scorer
    input:
      review_article: $.steps.article.output
      review_analysis: $.steps.review_analysis.output
      comparison_report: {}

output:
  product: $.steps.fetch.output
  review_analysis: $.steps.review_analysis.output
  article: $.steps.article.output
  quality_scores: $.steps.quality.output
```

Key differences from the full workflow:

- No `parallel:` block — `review_analysis` and `article` run in sequence.
- `comparison_report` is hardcoded to `{}` instead of a binding.
- The `comparison` key is gone from `output`.
- No comparison-related short-circuit defaults needed.

The Go code to expose this? One line:

```go
http.HandleFunc("/quick-review", handleQuickReview)
```

And the handler is a copy-paste of `handleReview` with `"quick-review"` instead of `"product-review"`. The orchestration, data flow, retry logic, error handling — all inherited from the YAML and the engine. You could even factor the handlers into a single generic function that takes the workflow name as a parameter.

## Invoking a single agent

Not everything needs a workflow. The `/comparison` endpoint calls one agent directly:

```go
result, err := engine.InvokeAgent("comparison-researcher", input)
```

`InvokeAgent` loads `agents/comparison-researcher/agent.yaml`, reads its `prompt.md`, and makes the LLM call with the structured output schema from `schemas/comparison_report.json`. One function call, fully configured by the agent definition on disk.

This is the same agent that participates in both workflows. The agent definition is written once and reused everywhere — in a workflow, standalone, or in a completely different workflow you haven't written yet.

## Running it

```bash
export OPENAI_API_KEY=sk-...
go run .
```

Then in another terminal:

```bash
# Full pipeline — fetch, parallel analysis + comparison, article, quality scoring
curl -s "http://localhost:8080/review?product_id=1" | jq

# No comparison — faster, cheaper, still gets you a review
curl -s "http://localhost:8080/quick-review?product_id=1" | jq

# Just the comparison agent
curl -s "http://localhost:8080/comparison?product_id=42" | jq
```

Product IDs 1 through 194 are valid on the DummyJSON API. Try a few. The full pipeline takes around 10-15 seconds (two parallel LLM calls + one serial). The quick review is a few seconds faster since it skips comparison.

## What you get back

Every workflow returns a `WorkflowEnvelope` — a standardized response with the workflow name, version, request ID, timestamps, per-step results with latency and token metrics, and the final assembled output. It looks like this (abbreviated):

```json
{
  "workflow": "product-review",
  "version": "1.0",
  "request_id": "wf_dc905347-2667-4168-8fbe-285e36e39330",
  "status": "success",
  "timestamps": {
    "started_at": "2026-03-22T18:19:37Z",
    "completed_at": "2026-03-22T18:19:49Z"
  },
  "metrics": {
    "total_latency_ms": 12737,
    "total_input_tokens": 1977,
    "total_output_tokens": 880
  },
  "steps": [
    { "id": "fetch", "status": "success", "output": { "title": "Essence Mascara Lash Princess", ... } },
    { "id": "review_analysis", "status": "success", "output": { "sentiment": "mixed", ... } },
    { "id": "comparison", "status": "success", "output": { "alternatives": [...], ... } },
    { "id": "article", "status": "success", "output": { "title": "Big Drama on a Budget", ... } },
    { "id": "quality", "status": "success", "output": { "overall": 96.67 } }
  ],
  "result": {
    "product": { ... },
    "review_analysis": { ... },
    "comparison": { ... },
    "article": { ... },
    "quality_scores": { ... }
  }
}
```

Every step includes its own latency and token counts. The `result` field is the clean output — what you'd actually use downstream.

## The point

The Go code in this project does exactly two things: it implements the business logic that can't be expressed declaratively (fetching from an external API, scoring with a heuristic), and it mounts HTTP handlers that delegate to the engine.

Everything else — which agents to call, in what order, in parallel or serial, with what retry and fallback behavior, how data flows between steps, what to do when a product isn't found, and how to assemble the final response — is in YAML files that you can read, diff, review, and modify without recompiling.

Want to add a sixth agent that checks product availability? Write the agent YAML and prompt, add a step to the workflow, and rebuild. Want a third workflow that only does fetch + quality scoring? Copy-paste the YAML and delete what you don't need. Want to swap gpt-4.1 for a different model? Change one line in the agent definition.

The ratio of YAML-to-Go in this project tells the story. The two workflow files and five agent definitions contain more orchestration logic than the entire Go application. That's the design working as intended — your code handles your domain, the spec handles the wiring.
