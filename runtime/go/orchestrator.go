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

	if _, err := LoadAllAgents(); err != nil {
		orchLog.Warn("could not pre-load all agents", map[string]interface{}{"error": err.Error()})
	}

	ctx := &ExecutionContext{
		Input: input,
		Steps: make(map[string]map[string]interface{}),
	}

	envelope := &WorkflowEnvelope{
		Workflow:  wfDef.Name,
		Version:   wfDef.Version,
		RequestID: generateRequestID(),
		Timestamps: map[string]string{
			"started_at": startTime.Format(time.RFC3339),
		},
		Metrics: map[string]interface{}{},
		Steps:   []StepResult{},
		Trail:   []TrailEntry{},
		Status:  "success",
	}

	// Build step index for graph traversal
	stepIndex := buildStepIndex(wfDef.Steps)
	executedSteps := map[string]bool{}

	// Graph traversal: start at step 0, follow next: edges
	cursor := 0
	for cursor < len(wfDef.Steps) {
		rawStep := wfDef.Steps[cursor]
		stepID := getStepID(rawStep)
		executedSteps[stepID] = true

		var nextTarget string
		var stepErr error

		switch s := rawStep.(type) {
		case *WorkflowStep:
			nextTarget, stepErr = executeWorkflowStep(s, ctx, envelope)
		case *ParallelBlock:
			nextTarget, stepErr = executeParallelBlock(s, ctx, envelope)
		case *LoopBlock:
			nextTarget, stepErr = executeLoopBlock(s, ctx, envelope)
		case *ForEachBlock:
			nextTarget, stepErr = executeForEachBlock(s, ctx, envelope)
		}

		if stepErr != nil {
			envelope.Status = "error"
			envelope.Error = stepErr.Error()
			break
		}

		// Resolve next step
		if nextTarget == "_end" {
			break
		}
		if nextTarget != "" {
			idx, ok := stepIndex[nextTarget]
			if !ok {
				envelope.Status = "error"
				envelope.Error = fmt.Sprintf("next target %q not found", nextTarget)
				break
			}
			cursor = idx
		} else {
			// Fall through to next step in array
			cursor++
		}
	}

	// Mark non-executed steps
	for _, rawStep := range wfDef.Steps {
		stepID := getStepID(rawStep)
		if !executedSteps[stepID] {
			markNotExecuted(rawStep, envelope)
		}
	}

	// Resolve output bindings
	if wfDef.Output != nil {
		envelope.Result = ResolveOutputs(wfDef.Output, ctx)
	}

	finalizeEnvelope(envelope, startTime)

	orchLog.Info("workflow completed", map[string]interface{}{
		"workflow": wfDef.Name,
		"status":   envelope.Status,
		"latency":  envelope.Metrics["total_latency_ms"],
	})

	if envelope.Status == "error" {
		return envelope, &WorkflowError{
			Err:      fmt.Errorf("%s", envelope.Error),
			Envelope: envelope,
		}
	}

	return envelope, nil
}

// buildStepIndex creates a map from step ID to array index for O(1) lookups.
func buildStepIndex(steps []interface{}) map[string]int {
	index := make(map[string]int, len(steps))
	for i, s := range steps {
		id := getStepID(s)
		if id != "" {
			index[id] = i
		}
		// Also index parallel branch IDs to their parent parallel block index
		if pb, ok := s.(*ParallelBlock); ok {
			for _, branch := range pb.Branches {
				if branch.ID != "" {
					index[branch.ID] = i
				}
			}
		}
	}
	return index
}

// getStepID returns the ID of any step type.
func getStepID(s interface{}) string {
	switch v := s.(type) {
	case *WorkflowStep:
		return v.ID
	case *ParallelBlock:
		return v.ID
	case *LoopBlock:
		return v.ID
	case *ForEachBlock:
		return v.ID
	}
	return ""
}

// getNextField returns the NextField of any step type.
func getNextField(s interface{}) *NextField {
	switch v := s.(type) {
	case *WorkflowStep:
		return v.Next
	case *ParallelBlock:
		return v.Next
	case *LoopBlock:
		return v.Next
	case *ForEachBlock:
		return v.Next
	}
	return nil
}

// ── Step execution ──────────────────────────────────────────────────────────

// executeWorkflowStep executes a single agent or sub-workflow step.
// Returns the resolved next target ("", "_end", or step-id) and any fatal error.
func executeWorkflowStep(step *WorkflowStep, ctx *ExecutionContext, env *WorkflowEnvelope) (string, error) {
	emitTrail(env, step.ID, "step_start", nil)

	var result *StepResult

	if step.Workflow != "" {
		result = executeSubWorkflow(step, ctx)
	} else {
		result = executeAgentStepWithRetry(step.ID, step.Agent, step.Input, step.Config, step.Retry, step.Fallback, ctx)
	}

	env.Steps = append(env.Steps, *result)

	if result.Status == "success" {
		ctx.Steps[step.ID] = map[string]interface{}{"output": result.Output}
		emitTrail(env, step.ID, "step_success", nil)
	} else {
		emitTrail(env, step.ID, "step_error", map[string]interface{}{"error": result.Error})
		return "", fmt.Errorf("step %s failed: %s", step.ID, result.Error)
	}

	return resolveNextTarget(step.Next, result.Output), nil
}

// executeSubWorkflow invokes a sub-workflow via Orchestrate.
func executeSubWorkflow(step *WorkflowStep, ctx *ExecutionContext) *StepResult {
	resolvedInput := ResolveInputs(step.Input, ctx)

	start := time.Now()
	subEnvelope, err := Orchestrate(step.Workflow, resolvedInput)
	latency := float64(time.Since(start).Milliseconds())

	if err != nil {
		sr := &StepResult{
			ID:       step.ID,
			Workflow: step.Workflow,
			Status:   "error",
			Metrics:  StepMetrics{LatencyMs: latency},
			Error:    err.Error(),
		}
		// Attach partial envelope if available
		if wfErr, ok := err.(*WorkflowError); ok && wfErr.Envelope != nil {
			sr.SubEnvelope = wfErr.Envelope
		}
		return sr
	}

	return &StepResult{
		ID:          step.ID,
		Workflow:    step.Workflow,
		Status:      "success",
		Output:      subEnvelope.Result,
		SubEnvelope: subEnvelope,
		Metrics: StepMetrics{
			LatencyMs:    latency,
			InputTokens:  toInt(subEnvelope.Metrics["total_input_tokens"]),
			OutputTokens: toInt(subEnvelope.Metrics["total_output_tokens"]),
		},
	}
}

// executeAgentStepWithRetry runs an agent with retry + fallback logic.
func executeAgentStepWithRetry(stepID, agentID string, inputBindings, config map[string]interface{}, retry *RetryConfig, fallback *FallbackConfig, ctx *ExecutionContext) *StepResult {
	resolvedInput := ResolveInputs(inputBindings, ctx)

	// Apply config overrides
	if config != nil {
		for k, v := range config {
			resolvedInput["__config_"+k] = v
		}
	}

	maxAttempts := 1
	backoffMs := 0
	if retry != nil {
		maxAttempts = retry.MaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		backoffMs = retry.BackoffMs
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		orchLog.Debug("executing step", map[string]interface{}{
			"step":    stepID,
			"agent":   agentID,
			"attempt": attempt,
		})

		start := time.Now()
		agentResult, err := InvokeAgent(agentID, resolvedInput)
		latency := float64(time.Since(start).Milliseconds())

		if err == nil {
			return &StepResult{
				ID:     stepID,
				Agent:  agentID,
				Status: "success",
				Output: agentResult.Output,
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
			"step":    stepID,
			"attempt": attempt,
			"error":   err.Error(),
		})

		if attempt < maxAttempts && backoffMs > 0 {
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
		}
	}

	// All retries exhausted — try fallback
	if fallback != nil && fallback.Agent != "" {
		orchLog.Info("trying fallback agent", map[string]interface{}{
			"step":     stepID,
			"fallback": fallback.Agent,
		})

		fallbackInput := make(map[string]interface{})
		for k, v := range resolvedInput {
			fallbackInput[k] = v
		}
		if fallback.Config != nil {
			for k, v := range fallback.Config {
				fallbackInput["__config_"+k] = v
			}
		}

		start := time.Now()
		agentResult, err := InvokeAgent(fallback.Agent, fallbackInput)
		latency := float64(time.Since(start).Milliseconds())

		if err == nil {
			return &StepResult{
				ID:     stepID,
				Agent:  fallback.Agent,
				Status: "success",
				Output: agentResult.Output,
				Metrics: StepMetrics{
					LatencyMs:    latency,
					InputTokens:  agentResult.Metrics.InputTokens,
					OutputTokens: agentResult.Metrics.OutputTokens,
				},
				Attempts:       maxAttempts,
				UsedFallback:   true,
				FallbackReason: lastErr.Error(),
			}
		}
		lastErr = err
	}

	return &StepResult{
		ID:       stepID,
		Agent:    agentID,
		Status:   "error",
		Metrics:  StepMetrics{},
		Attempts: maxAttempts,
		Error:    lastErr.Error(),
	}
}

// ── Parallel execution ──────────────────────────────────────────────────────

func executeParallelBlock(pb *ParallelBlock, ctx *ExecutionContext, env *WorkflowEnvelope) (string, error) {
	emitTrail(env, pb.ID, "parallel_start", map[string]interface{}{"join": pb.Join, "branches": len(pb.Branches)})

	results := make([]*StepResult, len(pb.Branches))
	var wg sync.WaitGroup

	for i, branch := range pb.Branches {
		wg.Add(1)
		go func(idx int, b ParallelBranch) {
			defer wg.Done()
			if b.Workflow != "" {
				ws := &WorkflowStep{
					ID:       b.ID,
					Workflow: b.Workflow,
					Input:    b.Input,
					Config:   b.Config,
				}
				results[idx] = executeSubWorkflow(ws, ctx)
			} else {
				results[idx] = executeAgentStepWithRetry(b.ID, b.Agent, b.Input, b.Config, b.Retry, b.Fallback, ctx)
			}
		}(i, branch)
	}

	wg.Wait()

	// Process results based on join strategy
	hasError := false
	for i, result := range results {
		env.Steps = append(env.Steps, *result)
		branchID := pb.Branches[i].ID
		if result.Status == "success" {
			ctx.Steps[branchID] = map[string]interface{}{"output": result.Output}
		} else {
			hasError = true
		}
	}

	emitTrail(env, pb.ID, "parallel_end", nil)

	switch pb.Join {
	case "all":
		if hasError {
			return "", fmt.Errorf("parallel block %s: one or more branches failed (join=all)", pb.ID)
		}
	case "any":
		// At least one must succeed
		anySuccess := false
		for _, r := range results {
			if r.Status == "success" {
				anySuccess = true
				break
			}
		}
		if !anySuccess {
			return "", fmt.Errorf("parallel block %s: all branches failed (join=any)", pb.ID)
		}
	case "all_settled":
		// Always continue, but mark partial_failure on envelope if any errors
		if hasError {
			if env.Status == "success" {
				env.Status = "partial_failure"
			}
		}
	}

	return resolveNextTarget(pb.Next, nil), nil
}

// ── Loop execution ──────────────────────────────────────────────────────────

func executeLoopBlock(lb *LoopBlock, ctx *ExecutionContext, env *WorkflowEnvelope) (string, error) {
	for iteration := 1; iteration <= lb.MaxIterations; iteration++ {
		emitTrail(env, lb.ID, "loop_iteration", map[string]interface{}{"iteration": iteration})

		var result *StepResult
		if lb.Workflow != "" {
			ws := &WorkflowStep{
				ID:       lb.ID,
				Workflow: lb.Workflow,
				Input:    lb.Input,
				Config:   lb.Config,
			}
			result = executeSubWorkflow(ws, ctx)
		} else {
			result = executeAgentStepWithRetry(lb.ID, lb.Agent, lb.Input, lb.Config, lb.Retry, lb.Fallback, ctx)
		}

		// Update context with latest iteration output
		if result.Status == "success" {
			ctx.Steps[lb.ID] = map[string]interface{}{"output": result.Output}
		} else {
			env.Steps = append(env.Steps, *result)
			return "", fmt.Errorf("loop %s iteration %d failed: %s", lb.ID, iteration, result.Error)
		}

		// Only add the final iteration to steps (or all if you want full trail)
		// We'll add the last one after the loop; for now, overwrite
		if iteration == lb.MaxIterations {
			env.Steps = append(env.Steps, *result)
		}

		// Check exit condition
		if lb.Until != "" && evaluateCondition(lb.Until, result.Output) {
			env.Steps = append(env.Steps, *result)
			break
		}
	}

	return resolveNextTarget(lb.Next, nil), nil
}

// ── ForEach execution ───────────────────────────────────────────────────────

func executeForEachBlock(feb *ForEachBlock, ctx *ExecutionContext, env *WorkflowEnvelope) (string, error) {
	// Resolve the collection binding to get the array
	collectionRaw := ResolveRef(feb.Collection, ctx)
	items, ok := toSlice(collectionRaw)
	if !ok {
		return "", fmt.Errorf("for_each %s: collection %q did not resolve to an array", feb.ID, feb.Collection)
	}

	if len(items) == 0 {
		ctx.Steps[feb.ID] = map[string]interface{}{"output": []interface{}{}}
		env.Steps = append(env.Steps, StepResult{
			ID:     feb.ID,
			Agent:  feb.Agent,
			Status: "success",
			Output: []interface{}{},
		})
		return resolveNextTarget(feb.Next, nil), nil
	}

	concurrency := feb.MaxConcurrency
	if concurrency <= 0 {
		concurrency = len(items)
	}

	results := make([]interface{}, len(items))
	errors := make([]error, len(items))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, currentItem interface{}) {
			defer wg.Done()
			defer func() { <-sem }()

			emitTrail(env, feb.ID, "for_each_iteration", map[string]interface{}{"index": idx})

			// Build per-iteration input: resolve bindings, inject $.current
			iterCtx := &ExecutionContext{
				Input: ctx.Input,
				Steps: make(map[string]map[string]interface{}),
			}
			// Copy existing step outputs
			for k, v := range ctx.Steps {
				iterCtx.Steps[k] = v
			}
			// Inject $.current as a pseudo-step
			iterCtx.Steps["__current"] = map[string]interface{}{"output": currentItem}

			iterInput := ResolveInputs(feb.Input, iterCtx)

			// Replace any $.current references in resolved input
			for k, v := range iterInput {
				if s, ok := v.(string); ok && s == "$.current" {
					iterInput[k] = currentItem
				}
			}

			stepResult := executeAgentStepWithRetry(
				fmt.Sprintf("%s[%d]", feb.ID, idx),
				feb.Agent, feb.Input, feb.Config,
				feb.Retry, feb.Fallback, iterCtx,
			)

			if stepResult.Status == "success" {
				results[idx] = stepResult.Output
			} else {
				errors[idx] = fmt.Errorf("iteration %d: %s", idx, stepResult.Error)
			}
		}(i, item)
	}

	wg.Wait()

	// Check for errors
	var firstErr error
	for _, e := range errors {
		if e != nil {
			firstErr = e
			break
		}
	}

	// Aggregate-only trail entry
	env.Steps = append(env.Steps, StepResult{
		ID:     feb.ID,
		Agent:  feb.Agent,
		Status: statusForForEach(errors),
		Output: results,
	})

	ctx.Steps[feb.ID] = map[string]interface{}{"output": results}

	if firstErr != nil && statusForForEach(errors) == "error" {
		return "", fmt.Errorf("for_each %s: %w", feb.ID, firstErr)
	}

	return resolveNextTarget(feb.Next, nil), nil
}

func statusForForEach(errors []error) string {
	hasError := false
	hasSuccess := false
	for _, e := range errors {
		if e != nil {
			hasError = true
		} else {
			hasSuccess = true
		}
	}
	if hasError && hasSuccess {
		return "partial_failure"
	}
	if hasError {
		return "error"
	}
	return "success"
}

// ── Next resolution ─────────────────────────────────────────────────────────

// resolveNextTarget evaluates the NextField and returns the target step ID.
// Returns "" for fall-through, "_end" to stop, or a step ID to jump to.
func resolveNextTarget(next *NextField, output interface{}) string {
	if next == nil {
		return "" // fall-through
	}

	if next.Target != "" {
		return next.Target
	}

	if next.Switch != nil {
		return resolveSwitchTarget(next.Switch, output)
	}

	if next.If != nil {
		return resolveIfTarget(next.If, output)
	}

	return ""
}

func resolveSwitchTarget(sn *SwitchNext, output interface{}) string {
	outputMap := toMap(output)
	if outputMap == nil {
		return sn.Default
	}

	val := resolveFieldPath(sn.Expression, outputMap)
	if val == nil {
		return sn.Default
	}

	valStr := fmt.Sprintf("%v", val)
	if target, ok := sn.Cases[valStr]; ok {
		return target
	}

	return sn.Default
}

func resolveIfTarget(in *IfNext, output interface{}) string {
	outputMap := toMap(output)
	if outputMap == nil {
		return in.Else
	}

	if evaluateCondition(in.Condition, output) {
		return in.Then
	}
	return in.Else
}

// ── Condition evaluation ────────────────────────────────────────────────────

// evaluateCondition handles conditions used in if:, until:, and switch:.
// Supported patterns:
//   - "output.field"        -> truthy check
//   - "!output.field"       -> falsy check
//   - "output.field == val" -> equality
//   - "output.field > val"  -> numeric comparison
func evaluateCondition(condition string, output interface{}) bool {
	outputMap := toMap(output)
	if outputMap == nil {
		return false
	}

	condition = strings.TrimSpace(condition)

	if strings.HasPrefix(condition, "!") {
		inner := strings.TrimSpace(condition[1:])
		return !evaluatePositiveCondition(inner, outputMap)
	}

	if idx := strings.Index(condition, "=="); idx != -1 {
		left := strings.TrimSpace(condition[:idx])
		right := strings.TrimSpace(condition[idx+2:])
		right = strings.Trim(right, "\"'")
		val := resolveFieldPath(left, outputMap)
		return fmt.Sprintf("%v", val) == right
	}

	if idx := strings.Index(condition, ">"); idx != -1 && !strings.Contains(condition, ">=") {
		left := strings.TrimSpace(condition[:idx])
		right := strings.TrimSpace(condition[idx+1:])
		leftVal := resolveFieldPath(left, outputMap)
		return toFloat(leftVal) > toFloat(right)
	}

	if idx := strings.Index(condition, ">="); idx != -1 {
		left := strings.TrimSpace(condition[:idx])
		right := strings.TrimSpace(condition[idx+2:])
		leftVal := resolveFieldPath(left, outputMap)
		return toFloat(leftVal) >= toFloat(right)
	}

	return evaluatePositiveCondition(condition, outputMap)
}

func evaluatePositiveCondition(expr string, outputMap map[string]interface{}) bool {
	val := resolveFieldPath(expr, outputMap)
	return isTruthy(val)
}

func resolveFieldPath(path string, outputMap map[string]interface{}) interface{} {
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

// ── Trail & helpers ─────────────────────────────────────────────────────────

func emitTrail(env *WorkflowEnvelope, stepID, event string, data interface{}) {
	env.Trail = append(env.Trail, TrailEntry{
		StepID:    stepID,
		Event:     event,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	})
}

func markNotExecuted(rawStep interface{}, env *WorkflowEnvelope) {
	switch s := rawStep.(type) {
	case *WorkflowStep:
		env.Steps = append(env.Steps, StepResult{
			ID:     s.ID,
			Agent:  s.Agent,
			Status: "not_executed",
		})
	case *ParallelBlock:
		for _, b := range s.Branches {
			env.Steps = append(env.Steps, StepResult{
				ID:     b.ID,
				Agent:  b.Agent,
				Status: "not_executed",
			})
		}
	case *LoopBlock:
		env.Steps = append(env.Steps, StepResult{
			ID:     s.ID,
			Agent:  s.Agent,
			Status: "not_executed",
		})
	case *ForEachBlock:
		env.Steps = append(env.Steps, StepResult{
			ID:     s.ID,
			Agent:  s.Agent,
			Status: "not_executed",
		})
	}
}

func finalizeEnvelope(env *WorkflowEnvelope, startTime time.Time) {
	endTime := time.Now().UTC()
	env.Timestamps["completed_at"] = endTime.Format(time.RFC3339)
	env.Metrics["total_latency_ms"] = float64(endTime.Sub(startTime).Milliseconds())

	var totalInput, totalOutput int
	for _, sr := range env.Steps {
		totalInput += sr.Metrics.InputTokens
		totalOutput += sr.Metrics.OutputTokens
	}
	env.Metrics["total_input_tokens"] = totalInput
	env.Metrics["total_output_tokens"] = totalOutput

	// Check for step errors if status is still success
	if env.Status == "success" {
		for _, sr := range env.Steps {
			if sr.Status == "error" {
				env.Status = "error"
				if env.Error == "" {
					env.Error = fmt.Sprintf("step %s failed: %s", sr.ID, sr.Error)
				}
				break
			}
		}
	}
}

func toSlice(v interface{}) ([]interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.([]interface{}); ok {
		return s, true
	}
	return nil, false
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		var f float64
		fmt.Sscanf(n, "%f", &f)
		return f
	}
	return 0
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

func generateRequestID() string {
	return fmt.Sprintf("wf_%s", newUUID())
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
