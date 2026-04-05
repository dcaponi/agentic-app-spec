// @generated from agentic-spec/agents/classify-food/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ClassifyFoodInput struct {
    ItemName string `json:"item_name"`
}

func ClassifyFood(input ClassifyFoodInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("classify-food", input)
}
