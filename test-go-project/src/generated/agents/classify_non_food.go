// @generated from agentic-spec/agents/classify-non-food/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ClassifyNonFoodInput struct {
    ItemName string `json:"item_name"`
}

func ClassifyNonFood(input ClassifyNonFoodInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("classify-non-food", input)
}
