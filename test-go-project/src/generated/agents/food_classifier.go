// @generated from agentic-spec/agents/food-classifier/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type FoodClassifierInput struct {
    ItemName string `json:"item_name"`
}

func FoodClassifier(input FoodClassifierInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("food-classifier", input)
}
