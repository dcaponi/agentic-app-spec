// @generated from agentic-spec/agents/classify-vegetable/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ClassifyVegetableInput struct {
    ItemName string `json:"item_name"`
}

func ClassifyVegetable(input ClassifyVegetableInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("classify-vegetable", input)
}
