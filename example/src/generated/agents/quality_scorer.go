// @generated from agents/quality-scorer/agent.yaml — do not edit
package agents

import "agentic/engine"

type QualityScorerInput struct {
    ReviewArticle map[string]interface{} `json:"review_article"`
    ReviewAnalysis map[string]interface{} `json:"review_analysis"`
    ComparisonReport map[string]interface{} `json:"comparison_report"`
}

func QualityScorer(input QualityScorerInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("quality-scorer", input)
}
