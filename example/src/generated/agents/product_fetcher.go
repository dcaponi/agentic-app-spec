// @generated from agents/product-fetcher/agent.yaml — do not edit
package agents

import engine "github.com/dominickcaponi/agentic-app-spec/runtime/go"

type ProductFetcherInput struct {
    ProductId float64 `json:"product_id"`
}

func ProductFetcher(input ProductFetcherInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("product-fetcher", input)
}
