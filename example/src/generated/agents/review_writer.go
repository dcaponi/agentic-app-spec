// @generated from agents/review-writer/agent.yaml — do not edit
package agents

import engine "github.com/dominickcaponi/agentic-app-spec/runtime/go"

type ReviewWriterInput struct {
    ProductName string `json:"product_name"`
    Brand string `json:"brand"`
    Price float64 `json:"price"`
    ReviewAnalysis map[string]interface{} `json:"review_analysis"`
    ComparisonReport map[string]interface{} `json:"comparison_report"`
}

func ReviewWriter(input ReviewWriterInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("review-writer", input)
}
