// @generated from agentic-spec/agents/classify-canned/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type ClassifyCannedInput struct {
    ItemName string `json:"item_name"`
}

func ClassifyCanned(input ClassifyCannedInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("classify-canned", input)
}
