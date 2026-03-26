// @generated from agents/quality-scorer/agent.yaml — do not edit
package agents

import engine "github.com/dominickcaponi/agentic-app-spec/runtime/go"

type QualityScorerInput struct {
    ReviewArticle map[string]interface{} `json:"review_article"`
    ReviewAnalysis map[string]interface{} `json:"review_analysis"`
    ComparisonReport map[string]interface{} `json:"comparison_report"`
}

func QualityScorer(input QualityScorerInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("quality-scorer", input)
}
