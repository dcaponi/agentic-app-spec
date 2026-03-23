package engine

// StepMetrics contains timing and token usage info for a single step.
type StepMetrics struct {
	LatencyMs    float64 `json:"latency_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

// StepResult is the result of executing a single workflow step.
type StepResult struct {
	ID           string      `json:"id"`
	Agent        string      `json:"agent"`
	Status       string      `json:"status"` // "success", "skipped", "error"
	Output       interface{} `json:"output"`
	Metrics      StepMetrics `json:"metrics"`
	Attempts     int         `json:"attempts,omitempty"`
	UsedFallback bool        `json:"used_fallback,omitempty"`
	Error        string      `json:"error,omitempty"`
}

// WorkflowEnvelope is the top-level response envelope for a workflow execution.
type WorkflowEnvelope struct {
	Workflow   string                 `json:"workflow"`
	Version    string                 `json:"version"`
	RequestID  string                 `json:"request_id"`
	Status     string                 `json:"status"` // "success", "error", "short_circuited"
	Timestamps map[string]string      `json:"timestamps"`
	Metrics    map[string]interface{} `json:"metrics"`
	Steps      []StepResult           `json:"steps"`
	Result     interface{}            `json:"result"`
	Error      string                 `json:"error,omitempty"`
}

// AgentResult is the return type from executing a single agent.
type AgentResult struct {
	Output  interface{} `json:"output"`
	Metrics StepMetrics `json:"metrics"`
}

// AgentDefinition is parsed from agents/<id>/agent.yaml.
type AgentDefinition struct {
	Name        string                   `yaml:"name" json:"name"`
	Description string                   `yaml:"description" json:"description"`
	Type        string                   `yaml:"type" json:"type"` // "llm" or "deterministic"
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

// FallbackConfig identifies an alternate agent to use when retries are exhausted.
type FallbackConfig struct {
	Agent  string                 `yaml:"agent" json:"agent"`
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// ShortCircuit defines an early-exit condition after a step.
type ShortCircuit struct {
	Condition string                 `yaml:"condition" json:"condition"`
	Defaults  map[string]interface{} `yaml:"defaults" json:"defaults"`
}

// WorkflowStep is a single (non-parallel) step in a workflow.
type WorkflowStep struct {
	ID           string                 `yaml:"id" json:"id"`
	Agent        string                 `yaml:"agent" json:"agent"`
	Input        map[string]interface{} `yaml:"input" json:"input"`
	Config       map[string]interface{} `yaml:"config" json:"config"`
	Retry        *RetryConfig           `yaml:"retry" json:"retry"`
	Fallback     *FallbackConfig        `yaml:"fallback" json:"fallback"`
	ShortCircuit *ShortCircuit          `yaml:"short_circuit" json:"short_circuit"`
}

// ParallelGroup wraps a set of steps that should run concurrently.
type ParallelGroup struct {
	Parallel []WorkflowStep `yaml:"parallel" json:"parallel"`
}

// WorkflowDefinition is parsed from workflows/<name>.yaml.
type WorkflowDefinition struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description"`
	Version     string                   `yaml:"version"`
	Input       map[string]InputParamDef `yaml:"input"`
	Steps       []interface{}            `yaml:"-"` // each element is *WorkflowStep or *ParallelGroup
	Output      map[string]string        `yaml:"output"`
}

// ExecutionContext tracks state during workflow execution.
type ExecutionContext struct {
	Input interface{}
	Steps map[string]map[string]interface{} // step_id -> {"output": value}
}
