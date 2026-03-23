package engine

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

var orchLog = NewLogger("orchestrator")

// Orchestrate loads and executes a named workflow end-to-end.
func Orchestrate(workflowName string, input interface{}) (*WorkflowEnvelope, error) {
	startTime := time.Now().UTC()

	wfDef, err := LoadWorkflow(workflowName)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow: %w", err)
	}

	// Pre-load all agents
	if _, err := LoadAllAgents(); err != nil {
		orchLog.Warn("could not pre-load all agents", map[string]interface{}{"error": err.Error()})
	}

	ctx := &ExecutionContext{
		Input: input,
		Steps: make(map[string]map[string]interface{}),
	}

	envelope := &WorkflowEnvelope{
		Workflow:  wfDef.Name,
		Version:  wfDef.Version,
		RequestID: generateRequestID(),
		Timestamps: map[string]string{
			"started_at": startTime.Format(time.RFC3339),
		},
		Metrics: map[string]interface{}{},
		Steps:   []StepResult{},
		Status:  "success",
	}

	shortCircuited := false
	var shortCircuitDefaults map[string]interface{}

	for _, rawStep := range wfDef.Steps {
		if shortCircuited {
			// Fill remaining steps with skipped status
			switch s := rawStep.(type) {
			case *WorkflowStep:
				envelope.Steps = append(envelope.Steps, buildSkippedResult(s.ID, s.Agent, shortCircuitDefaults))
				populateCtxFromDefaults(ctx, s.ID, shortCircuitDefaults)
			case *ParallelGroup:
				for _, ps := range s.Parallel {
					envelope.Steps = append(envelope.Steps, buildSkippedResult(ps.ID, ps.Agent, shortCircuitDefaults))
					populateCtxFromDefaults(ctx, ps.ID, shortCircuitDefaults)
				}
			}
			continue
		}

		switch s := rawStep.(type) {
		case *WorkflowStep:
			result := executeStepWithRetry(s, ctx)
			envelope.Steps = append(envelope.Steps, *result)
			if result.Status == "success" {
				ctx.Steps[s.ID] = map[string]interface{}{"output": result.Output}
			}
			// Check short-circuit
			if s.ShortCircuit != nil && result.Status == "success" {
				if evaluateCondition(s.ShortCircuit.Condition, result.Output) {
					shortCircuited = true
					shortCircuitDefaults = s.ShortCircuit.Defaults
					envelope.Status = "short_circuited"
				}
			}

		case *ParallelGroup:
			results := executeParallelSteps(s, ctx)
			for i, result := range results {
				envelope.Steps = append(envelope.Steps, *result)
				stepID := s.Parallel[i].ID
				if result.Status == "success" {
					ctx.Steps[stepID] = map[string]interface{}{"output": result.Output}
				}
			}
		}
	}

	// Resolve output bindings
	if wfDef.Output != nil {
		envelope.Result = ResolveOutputs(wfDef.Output, ctx)
	}

	endTime := time.Now().UTC()
	envelope.Timestamps["completed_at"] = endTime.Format(time.RFC3339)
	envelope.Metrics["total_latency_ms"] = float64(endTime.Sub(startTime).Milliseconds())

	// Calculate totals
	var totalInput, totalOutput int
	for _, sr := range envelope.Steps {
		totalInput += sr.Metrics.InputTokens
		totalOutput += sr.Metrics.OutputTokens
	}
	envelope.Metrics["total_input_tokens"] = totalInput
	envelope.Metrics["total_output_tokens"] = totalOutput

	// Check for any step errors
	if envelope.Status == "success" {
		for _, sr := range envelope.Steps {
			if sr.Status == "error" {
				envelope.Status = "error"
				envelope.Error = fmt.Sprintf("step %s failed: %s", sr.ID, sr.Error)
				break
			}
		}
	}

	orchLog.Info("workflow completed", map[string]interface{}{
		"workflow":  wfDef.Name,
		"status":   envelope.Status,
		"latency":  envelope.Metrics["total_latency_ms"],
	})

	return envelope, nil
}

func executeStepWithRetry(step *WorkflowStep, ctx *ExecutionContext) *StepResult {
	resolvedInput := ResolveInputs(step.Input, ctx)

	// Merge step config overrides
	if step.Config != nil {
		for k, v := range step.Config {
			resolvedInput["__config_"+k] = v
		}
	}

	maxAttempts := 1
	backoffMs := 0
	if step.Retry != nil {
		maxAttempts = step.Retry.MaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		backoffMs = step.Retry.BackoffMs
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		orchLog.Debug("executing step", map[string]interface{}{
			"step":    step.ID,
			"agent":   step.Agent,
			"attempt": attempt,
		})

		start := time.Now()
		agentResult, err := InvokeAgent(step.Agent, resolvedInput)
		latency := float64(time.Since(start).Milliseconds())

		if err == nil {
			return &StepResult{
				ID:       step.ID,
				Agent:    step.Agent,
				Status:   "success",
				Output:   agentResult.Output,
				Metrics: StepMetrics{
					LatencyMs:    latency,
					InputTokens:  agentResult.Metrics.InputTokens,
					OutputTokens: agentResult.Metrics.OutputTokens,
				},
				Attempts: attempt,
			}
		}

		lastErr = err
		orchLog.Warn("step attempt failed", map[string]interface{}{
			"step":    step.ID,
			"attempt": attempt,
			"error":   err.Error(),
		})

		if attempt < maxAttempts && backoffMs > 0 {
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
		}
	}

	// All retries exhausted — try fallback
	if step.Fallback != nil {
		orchLog.Info("trying fallback agent", map[string]interface{}{
			"step":     step.ID,
			"fallback": step.Fallback.Agent,
		})

		// Merge fallback config
		fallbackInput := make(map[string]interface{})
		for k, v := range resolvedInput {
			fallbackInput[k] = v
		}
		for k, v := range step.Fallback.Config {
			fallbackInput[k] = v
		}

		start := time.Now()
		agentResult, err := InvokeAgent(step.Fallback.Agent, fallbackInput)
		latency := float64(time.Since(start).Milliseconds())

		if err == nil {
			return &StepResult{
				ID:           step.ID,
				Agent:        step.Fallback.Agent,
				Status:       "success",
				Output:       agentResult.Output,
				Metrics: StepMetrics{
					LatencyMs:    latency,
					InputTokens:  agentResult.Metrics.InputTokens,
					OutputTokens: agentResult.Metrics.OutputTokens,
				},
				Attempts:     maxAttempts,
				UsedFallback: true,
			}
		}
		lastErr = err
	}

	return &StepResult{
		ID:       step.ID,
		Agent:    step.Agent,
		Status:   "error",
		Metrics:  StepMetrics{},
		Attempts: maxAttempts,
		Error:    lastErr.Error(),
	}
}

func executeParallelSteps(group *ParallelGroup, ctx *ExecutionContext) []*StepResult {
	results := make([]*StepResult, len(group.Parallel))
	var wg sync.WaitGroup

	for i, step := range group.Parallel {
		wg.Add(1)
		go func(idx int, s WorkflowStep) {
			defer wg.Done()
			results[idx] = executeStepWithRetry(&s, ctx)
		}(i, step)
	}

	wg.Wait()
	return results
}

// evaluateCondition handles simple short-circuit conditions.
// Supported patterns:
//   - "!output.field"       -> true when output[field] is falsy
//   - "output.field"        -> true when output[field] is truthy
//   - "output.field == val" -> equality check
func evaluateCondition(condition string, output interface{}) bool {
	outputMap := toMap(output)
	if outputMap == nil {
		return false
	}

	condition = strings.TrimSpace(condition)

	// Handle negation: !output.field
	if strings.HasPrefix(condition, "!") {
		inner := strings.TrimSpace(condition[1:])
		return !evaluatePositiveCondition(inner, outputMap)
	}

	// Handle equality: output.field == value
	if idx := strings.Index(condition, "=="); idx != -1 {
		left := strings.TrimSpace(condition[:idx])
		right := strings.TrimSpace(condition[idx+2:])
		right = strings.Trim(right, "\"'")
		val := resolveFieldPath(left, outputMap)
		return fmt.Sprintf("%v", val) == right
	}

	return evaluatePositiveCondition(condition, outputMap)
}

func evaluatePositiveCondition(expr string, outputMap map[string]interface{}) bool {
	val := resolveFieldPath(expr, outputMap)
	return isTruthy(val)
}

func resolveFieldPath(path string, outputMap map[string]interface{}) interface{} {
	// Strip "output." prefix if present
	path = strings.TrimPrefix(path, "output.")
	parts := strings.Split(path, ".")
	return traverseMap(outputMap, parts)
}

func isTruthy(val interface{}) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return true
	}
}

func buildSkippedResult(stepID, agent string, defaults map[string]interface{}) StepResult {
	var output interface{}
	if defaults != nil {
		if val, ok := defaults[stepID]; ok {
			output = val
		}
	}
	return StepResult{
		ID:     stepID,
		Agent:  agent,
		Status: "skipped",
		Output: output,
	}
}

func populateCtxFromDefaults(ctx *ExecutionContext, stepID string, defaults map[string]interface{}) {
	if defaults == nil {
		return
	}
	if val, ok := defaults[stepID]; ok {
		ctx.Steps[stepID] = map[string]interface{}{"output": val}
	}
}

func generateRequestID() string {
	return fmt.Sprintf("wf_%s", newUUID())
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// Set version 4 bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
