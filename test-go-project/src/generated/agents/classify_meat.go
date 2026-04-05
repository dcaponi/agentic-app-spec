// @generated from agentic-spec/agents/classify-meat/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ClassifyMeatInput struct {
    ItemName string `json:"item_name"`
}

func ClassifyMeat(input ClassifyMeatInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("classify-meat", input)
}
