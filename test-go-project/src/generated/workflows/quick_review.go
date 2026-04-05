// @generated from agentic-spec/workflows/quick-review.yaml — do not edit
package workflows

import engine "github.com/dcaponi/agentic-app-spec/runtime/go"

type QuickReviewInput struct {
    ProductId float64 `json:"product_id"`
}

func QuickReview(input QuickReviewInput) (*engine.WorkflowEnvelope, error) {
    return engine.Orchestrate("quick-review", input)
}
