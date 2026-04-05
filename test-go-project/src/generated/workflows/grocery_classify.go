// @generated from agentic-spec/workflows/grocery-classify.yaml — do not edit
package workflows

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type GroceryClassifyInput struct {
    ItemName string `json:"item_name"`
}

func GroceryClassify(input GroceryClassifyInput) (*engine.WorkflowEnvelope, error) {
    return engine.Orchestrate("grocery-classify", input)
}
