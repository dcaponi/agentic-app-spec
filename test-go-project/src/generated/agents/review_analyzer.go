// @generated from agentic-spec/agents/review-analyzer/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ReviewAnalyzerInput struct {
    ProductName string `json:"product_name"`
    Category string `json:"category"`
    Price float64 `json:"price"`
    Rating float64 `json:"rating"`
    ReviewsText string `json:"reviews_text"`
}

func ReviewAnalyzer(input ReviewAnalyzerInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("review-analyzer", input)
}
