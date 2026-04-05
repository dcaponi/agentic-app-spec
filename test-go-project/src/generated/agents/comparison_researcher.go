// @generated from agentic-spec/agents/comparison-researcher/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ComparisonResearcherInput struct {
    ProductName string `json:"product_name"`
    Category string `json:"category"`
    Price float64 `json:"price"`
    Brand string `json:"brand"`
    Description string `json:"description"`
}

func ComparisonResearcher(input ComparisonResearcherInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("comparison-researcher", input)
}
