// @generated from agentic-spec/agents/food-subtype-classifier/agent.yaml — do not edit
package agents

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type FoodSubtypeClassifierInput struct {
    ItemName string `json:"item_name"`
}

func FoodSubtypeClassifier(input FoodSubtypeClassifierInput) (*engine.AgentResult, error) {
    return engine.InvokeAgent("food-subtype-classifier", input)
}
