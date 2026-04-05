package engine

// StepMetrics contains timing and token usage info for a single step.
type StepMetrics struct {
	LatencyMs    float64 `json:"latency_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

// TrailEntry is a single event in the workflow execution trail.
type TrailEntry struct {
	StepID    string      `json:"step_id"`
	Event     string      `json:"event"` // "step_start", "step_success", "step_error", "step_fallback", "step_skip", "loop_iteration", "for_each_iteration", "parallel_start", "parallel_end"
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// StepResult is the result of executing a single workflow step.
type StepResult struct {
	ID             string            `json:"id"`
	Agent          string            `json:"agent,omitempty"`
	Workflow       string            `json:"workflow,omitempty"`
	Status         string            `json:"status"` // "success", "error", "not_executed", "partial_failure"
	Output         interface{}       `json:"output"`
	Metrics        StepMetrics       `json:"metrics"`
	Attempts       int               `json:"attempts,omitempty"`
	UsedFallback   bool              `json:"used_fallback,omitempty"`
	FallbackReason string            `json:"fallback_reason,omitempty"`
	SubEnvelope    *WorkflowEnvelope `json:"sub_envelope,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// WorkflowEnvelope is the top-level response envelope for a workflow execution.
type WorkflowEnvelope struct {
	Workflow   string                 `json:"workflow"`
	Version    string                 `json:"version"`
	RequestID  string                 `json:"request_id"`
	Status     string                 `json:"status"` // "success", "error", "partial_failure"
	Timestamps map[string]string      `json:"timestamps"`
	Metrics    map[string]interface{} `json:"metrics"`
	Steps      []StepResult           `json:"steps"`
	Trail      []TrailEntry           `json:"trail"`
	Result     interface{}            `json:"result"`
	Error      string                 `json:"error,omitempty"`
}

// WorkflowError wraps a workflow failure with the partial envelope (including trail).
type WorkflowError struct {
	Err      error
	Envelope *WorkflowEnvelope
}

func (e *WorkflowError) Error() string { return e.Err.Error() }
func (e *WorkflowError) Unwrap() error { return e.Err }

// AgentResult is the return type from executing a single agent.
type AgentResult struct {
	Output  interface{} `json:"output"`
	Metrics StepMetrics `json:"metrics"`
}

// AgentDefinition is parsed from agentic-spec/agents/<id>/agent.yaml.
type AgentDefinition struct {
	Name        string                   `yaml:"name" json:"name"`
	Description string                   `yaml:"description" json:"description"`
	Type        string                   `yaml:"type" json:"type"` // "llm" or "deterministic"
	BaseURL     string                   `yaml:"base_url" json:"base_url"`
	APIKeyEnv   string                   `yaml:"api_key_env" json:"api_key_env"`
	Model       string                   `yaml:"model" json:"model"`
	Temperature float64                  `yaml:"temperature" json:"temperature"`
	InputType   string                   `yaml:"input_type" json:"input_type"`
	ImageDetail string                   `yaml:"image_detail" json:"image_detail"`
	Schema      *string                  `yaml:"schema" json:"schema"`
	UserMessage string                   `yaml:"user_message" json:"user_message"`
	Handler     string                   `yaml:"handler" json:"handler"`
	Prompt      string                   `yaml:"-" json:"-"`
	Input       map[string]InputParamDef `yaml:"input" json:"input"`
}

// InputParamDef describes one input parameter for an agent or workflow.
type InputParamDef struct {
	Type     string `yaml:"type" json:"type"`
	Required *bool  `yaml:"required" json:"required"`
}

// RetryConfig specifies retry behaviour for a workflow step.
type RetryConfig struct {
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	BackoffMs   int `yaml:"backoff_ms" json:"backoff_ms"`
}

// FallbackConfig identifies an alternate agent (or workflow) to use when retries are exhausted.
type FallbackConfig struct {
	Agent    string                 `yaml:"agent" json:"agent,omitempty"`
	Workflow string                 `yaml:"workflow" json:"workflow,omitempty"`
	Config   map[string]interface{} `yaml:"config" json:"config,omitempty"`
}

// NextField represents the next: control flow field on a step.
// Exactly one of Target, Switch, or If is populated.
type NextField struct {
	Target string      // simple goto: "step-id" or "_end"
	Switch *SwitchNext // switch: value-based branching
	If     *IfNext     // if: binary branching
}

// SwitchNext dispatches on the value of an expression.
type SwitchNext struct {
	Expression string            // e.g., "output.category"
	Cases      map[string]string // value -> step-id
	Default    string            // default step-id (may be "_end" or empty)
}

// IfNext evaluates a condition and branches to Then or Else.
type IfNext struct {
	Condition string // e.g., "output.found"
	Then      string // step-id if true
	Else      string // step-id if false
}

// WorkflowStep is a single step in a workflow (agent or sub-workflow invocation).
type WorkflowStep struct {
	ID       string                 `yaml:"id" json:"id"`
	Agent    string                 `yaml:"agent" json:"agent,omitempty"`
	Workflow string                 `yaml:"workflow" json:"workflow,omitempty"`
	Input    map[string]interface{} `yaml:"input" json:"input"`
	Config   map[string]interface{} `yaml:"config" json:"config,omitempty"`
	Retry    *RetryConfig           `yaml:"retry" json:"retry,omitempty"`
	Fallback *FallbackConfig        `yaml:"fallback" json:"fallback,omitempty"`
	Requires []string               `yaml:"requires" json:"requires,omitempty"`
	Next     *NextField             `yaml:"-" json:"-"` // custom-parsed
}

// ParallelBranch is a single branch within a parallel block.
type ParallelBranch struct {
	ID       string                 `yaml:"id" json:"id"`
	Agent    string                 `yaml:"agent" json:"agent,omitempty"`
	Workflow string                 `yaml:"workflow" json:"workflow,omitempty"`
	Input    map[string]interface{} `yaml:"input" json:"input"`
	Config   map[string]interface{} `yaml:"config" json:"config,omitempty"`
	Retry    *RetryConfig           `yaml:"retry" json:"retry,omitempty"`
	Fallback *FallbackConfig        `yaml:"fallback" json:"fallback,omitempty"`
}

// ParallelBlock represents a set of branches that run concurrently.
type ParallelBlock struct {
	ID       string           `json:"id"`
	Join     string           `json:"join"` // "all", "any", "all_settled"
	Branches []ParallelBranch `json:"branches"`
	Next     *NextField       `json:"-"` // custom-parsed
}

// LoopBlock represents a bounded iteration step.
type LoopBlock struct {
	ID            string                 `json:"id"`
	Agent         string                 `json:"agent,omitempty"`
	Workflow      string                 `json:"workflow,omitempty"`
	Input         map[string]interface{} `json:"input"`
	Config        map[string]interface{} `json:"config,omitempty"`
	Until         string                 `json:"until"`          // condition expression for exit
	MaxIterations int                    `json:"max_iterations"` // required upper bound
	Retry         *RetryConfig           `json:"retry,omitempty"`
	Fallback      *FallbackConfig        `json:"fallback,omitempty"`
	Next          *NextField             `json:"-"`
}

// ForEachBlock represents a dynamic fan-out over a runtime array.
type ForEachBlock struct {
	ID             string                 `json:"id"`
	Agent          string                 `json:"agent,omitempty"`
	Workflow       string                 `json:"workflow,omitempty"`
	Input          map[string]interface{} `json:"input"`
	Config         map[string]interface{} `json:"config,omitempty"`
	Collection     string                 `json:"collection"` // binding expression for the array (e.g., "$.steps.fetch.output.items")
	MaxConcurrency int                    `json:"max_concurrency,omitempty"`
	Retry          *RetryConfig           `json:"retry,omitempty"`
	Fallback       *FallbackConfig        `json:"fallback,omitempty"`
	Next           *NextField             `json:"-"`
}

// WorkflowDefinition is parsed from agentic-spec/workflows/<name>.yaml.
type WorkflowDefinition struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description"`
	Version     string                   `yaml:"version"`
	Input       map[string]InputParamDef `yaml:"input"`
	Steps       []interface{}            `yaml:"-"` // *WorkflowStep, *ParallelBlock, *LoopBlock, or *ForEachBlock
	Output      map[string]string        `yaml:"output"`
}

// ExecutionContext tracks state during workflow execution.
type ExecutionContext struct {
	Input interface{}
	Steps map[string]map[string]interface{} // step_id -> {"output": value}
}
