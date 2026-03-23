// @generated from workflows/product-review.yaml — do not edit
package workflows

import "agentic/engine"

type ProductReviewInput struct {
    ProductId float64 `json:"product_id"`
}

func ProductReview(input ProductReviewInput) (*engine.WorkflowEnvelope, error) {
    return engine.Orchestrate("product-review", input)
}
