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
		Version:   wfDef.Version,
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
			case *RouteBlock:
				envelope.Steps = append(envelope.Steps, buildSkippedResult(s.ID, "routing-agent:"+s.RoutingAgent, shortCircuitDefaults))
				populateCtxFromDefaults(ctx, s.ID, shortCircuitDefaults)
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

		case *RouteBlock:
			result := executeRoute(s, ctx)
			envelope.Steps = append(envelope.Steps, *result)
			if result.Status == "success" || result.Status == "short_circuited" {
				ctx.Steps[s.ID] = map[string]interface{}{"output": result.Output}
			}
			if result.Status == "short_circuited" {
				shortCircuited = true
				// Extract defaults from the _none target if present
				if noneTarget, ok := s.Routes["_none"]; ok {
					if noneMap, ok := noneTarget.(map[string]interface{}); ok {
						if defaults, ok := noneMap["defaults"].(map[string]interface{}); ok {
							shortCircuitDefaults = defaults
						}
					}
				}
				envelope.Status = "short_circuited"
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
		"workflow": wfDef.Name,
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
				ID:     step.ID,
				Agent:  step.Agent,
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
				ID:    step.ID,
				Agent: step.Fallback.Agent,
				Status: "success",
				Output: agentResult.Output,
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

// ── Route execution engine ──

// executeRoute executes a RouteBlock in three phases:
// Phase 1: routing-agent decision with retry + fallback
// Phase 2: handle _none (short-circuit)
// Phase 3: dispatch target (no retry)
func executeRoute(rb *RouteBlock, ctx *ExecutionContext) *StepResult {
	maxAttempts := 1
	backoffMs := 0
	if rb.Retry != nil {
		maxAttempts = rb.Retry.MaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		backoffMs = rb.Retry.BackoffMs
	}

	orchLog.Info("route starting", map[string]interface{}{
		"id":             rb.ID,
		"routing_agent":  rb.RoutingAgent,
		"route_keys":     routeKeys(rb),
		"max_attempts":   maxAttempts,
		"has_fallback":   rb.Fallback != nil,
	})

	routingAgentDef, err := LoadRoutingAgent(rb.RoutingAgent)
	if err != nil {
		return &StepResult{
			ID:     rb.ID,
			Agent:  "routing-agent:" + rb.RoutingAgent,
			Status: "error",
			Error:  err.Error(),
		}
	}

	resolvedInput := ResolveInputs(rb.Input, ctx)
	keys := routeKeys(rb)

	// ── Phase 1: Decision with retry + fallback ──

	var chosenKey string
	var routingAgentOutput map[string]interface{}
	usedFallback := false
	totalAttempts := 0
	decided := false
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		totalAttempts = attempt
		orchLog.Debug("route decision attempt", map[string]interface{}{
			"id":      rb.ID,
			"attempt": attempt,
		})

		output, err := executeRoutingAgentDecision(routingAgentDef, resolvedInput, keys)
		if err == nil {
			key, _ := output["route"].(string)
			if key != "_none" {
				if _, exists := rb.Routes[key]; !exists {
					err = fmt.Errorf("routing-agent %q returned invalid route key %q; valid: %v", rb.RoutingAgent, key, append(keys, "_none"))
				}
			}
			if err == nil {
				chosenKey = key
				routingAgentOutput = output
				decided = true
				orchLog.Info("route decision made", map[string]interface{}{
					"id":    rb.ID,
					"route": chosenKey,
				})
				break
			}
		}

		lastErr = err
		orchLog.Warn("route decision attempt failed", map[string]interface{}{
			"id":      rb.ID,
			"attempt": attempt,
			"error":   err.Error(),
		})

		if attempt < maxAttempts && backoffMs > 0 {
			time.Sleep(time.Duration(backoffMs*attempt) * time.Millisecond)
		}
	}

	// Fallback decision
	if !decided && rb.Fallback != nil {
		orchLog.Info("trying fallback routing-agent", map[string]interface{}{
			"id":                      rb.ID,
			"fallback_routing_agent":  rb.Fallback.RoutingAgent,
		})

		fallbackDef, ferr := LoadRoutingAgent(rb.Fallback.RoutingAgent)
		if ferr == nil {
			// Merge fallback config overrides into a copy
			mergedDef := *fallbackDef
			if rb.Fallback.Config != nil {
				for k, v := range rb.Fallback.Config {
					switch k {
					case "model":
						if s, ok := v.(string); ok {
							mergedDef.Model = s
						}
					case "temperature":
						if f, ok := v.(float64); ok {
							mergedDef.Temperature = f
						}
					}
				}
			}

			output, ferr2 := executeRoutingAgentDecision(&mergedDef, resolvedInput, keys)
			if ferr2 == nil {
				key, _ := output["route"].(string)
				if key != "_none" {
					if _, exists := rb.Routes[key]; !exists {
						ferr2 = fmt.Errorf("fallback routing-agent returned invalid key %q", key)
					}
				}
				if ferr2 == nil {
					chosenKey = key
					routingAgentOutput = output
					usedFallback = true
					totalAttempts = maxAttempts + 1
					decided = true
				} else {
					lastErr = ferr2
				}
			} else {
				lastErr = ferr2
			}
		} else {
			lastErr = ferr
		}

		if !decided {
			totalAttempts = maxAttempts + 1
		}
	}

	if !decided {
		errMsg := "all decision attempts exhausted"
		if lastErr != nil {
			errMsg = lastErr.Error()
		}
		orchLog.Warn("route failed", map[string]interface{}{"id": rb.ID, "error": errMsg})
		return &StepResult{
			ID:           rb.ID,
			Agent:        "routing-agent:" + rb.RoutingAgent,
			Status:       "error",
			Metrics:      StepMetrics{},
			Attempts:     totalAttempts,
			UsedFallback: rb.Fallback != nil,
			Error:        errMsg,
		}
	}

	resolvedAgent := "routing-agent:" + rb.RoutingAgent
	if usedFallback {
		resolvedAgent = "routing-agent:" + rb.Fallback.RoutingAgent
	}

	// ── Phase 2: Handle _none → short_circuited ──

	target := rb.Routes[chosenKey]
	isNone := chosenKey == "_none"
	if !isNone {
		if targetMap, ok := target.(map[string]interface{}); ok {
			if sc, _ := targetMap["short_circuit"].(bool); sc {
				isNone = true
			}
		}
	}

	if isNone {
		output := &RouteOutput{
			Route:        "_none",
			RouterOutput: routingAgentOutput,
			Result:       nil,
		}
		return &StepResult{
			ID:           rb.ID,
			Agent:        resolvedAgent,
			Status:       "short_circuited",
			Output:       output,
			Metrics:      StepMetrics{},
			Attempts:     totalAttempts,
			UsedFallback: usedFallback,
		}
	}

	// ── Phase 3: Dispatch target (no retry) ──

	agentResult, err := dispatchRouteTarget(target, resolvedInput, rb, ctx)
	if err != nil {
		return &StepResult{
			ID:           rb.ID,
			Agent:        resolvedAgent,
			Status:       "error",
			Metrics:      StepMetrics{},
			Attempts:     totalAttempts,
			UsedFallback: usedFallback,
			Error:        err.Error(),
		}
	}

	output := &RouteOutput{
		Route:        chosenKey,
		RouterOutput: routingAgentOutput,
		Result:       agentResult.Output,
	}

	return &StepResult{
		ID:           rb.ID,
		Agent:        resolvedAgent,
		Status:       "success",
		Output:       output,
		Metrics:      agentResult.Metrics,
		Attempts:     totalAttempts,
		UsedFallback: usedFallback,
	}
}

// executeRoutingAgentDecision invokes the routing agent (deterministic or LLM) and returns
// a map containing at least {"route": "<key>"}.
func executeRoutingAgentDecision(routingAgentDef *RoutingAgentDefinition, input map[string]interface{}, keys []string) (map[string]interface{}, error) {
	if routingAgentDef.Strategy == "deterministic" {
		agentCompat := &AgentDefinition{
			Name:        routingAgentDef.Name,
			Description: routingAgentDef.Description,
			Type:        "deterministic",
			Handler:     routingAgentDef.Handler,
		}
		result, err := ExecuteAgent(input, agentCompat)
		if err != nil {
			return nil, err
		}
		if m, ok := result.Output.(map[string]interface{}); ok {
			return m, nil
		}
		return nil, fmt.Errorf("deterministic routing-agent returned non-map output")
	}

	// LLM strategy
	inputParts := make([]string, 0, len(input))
	for k, v := range input {
		if s, ok := v.(string); ok {
			inputParts = append(inputParts, k+": "+s)
		} else {
			inputParts = append(inputParts, fmt.Sprintf("%s: %v", k, v))
		}
	}
	inputSummary := strings.Join(inputParts, "\n")

	userMessage := inputSummary + "\n\n" +
		"You must choose exactly one of the following routes: " + strings.Join(keys, ", ") + "\n" +
		"If none of the routes apply, choose: _none\n" +
		`Respond with a JSON object: { "route": "<chosen_key>" }`

	result, err := CallLLM(LLMCallOptions{
		Model:        routingAgentDef.Model,
		SystemPrompt: routingAgentDef.Prompt,
		UserContent:  userMessage,
		Temperature:  routingAgentDef.Temperature,
		SchemaName:   "",
		BaseURL:      routingAgentDef.BaseURL,
		APIKeyEnv:    routingAgentDef.APIKeyEnv,
	})
	if err != nil {
		return nil, err
	}

	if m, ok := result.Output.(map[string]interface{}); ok {
		return m, nil
	}
	return nil, fmt.Errorf("LLM routing-agent returned non-map output")
}

// dispatchRouteTarget dispatches to the chosen route target:
//   - string        → agent ID with pass-through input
//   - map["route"]  → nested RouteBlock (recursive)
//   - map["agent"]  → agent with explicit or pass-through input
//   - map["workflow"] → call Orchestrate
func dispatchRouteTarget(target interface{}, passThrough map[string]interface{}, rb *RouteBlock, ctx *ExecutionContext) (*AgentResult, error) {
	// String → agent ID
	if agentID, ok := target.(string); ok {
		return InvokeAgent(agentID, passThrough)
	}

	targetMap, ok := target.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unknown route target type in route %s: %T", rb.ID, target)
	}

	// Nested route block
	if nestedRaw, ok := targetMap["route"]; ok {
		// Re-marshal and decode into RouteBlock
		nestedRB, err := decodeRouteBlock(nestedRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to decode nested route block: %w", err)
		}
		nestedResult := executeRoute(nestedRB, ctx)
		return &AgentResult{
			Output:  nestedResult.Output,
			Metrics: nestedResult.Metrics,
		}, nil
	}

	// Agent with explicit input
	if agentID, ok := targetMap["agent"].(string); ok {
		var agentInput map[string]interface{}
		if inputRaw, hasInput := targetMap["input"]; hasInput {
			if inputMap, ok := inputRaw.(map[string]interface{}); ok {
				agentInput = ResolveInputs(inputMap, ctx)
			}
		}
		if agentInput == nil {
			agentInput = passThrough
		}
		return InvokeAgent(agentID, agentInput)
	}

	// Workflow target
	if wfName, ok := targetMap["workflow"].(string); ok {
		var wfInput map[string]interface{}
		if inputRaw, hasInput := targetMap["input"]; hasInput {
			if inputMap, ok := inputRaw.(map[string]interface{}); ok {
				wfInput = ResolveInputs(inputMap, ctx)
			}
		}
		if wfInput == nil {
			wfInput = passThrough
		}
		envelope, err := Orchestrate(wfName, wfInput)
		if err != nil {
			return nil, err
		}
		latency, _ := envelope.Metrics["total_latency_ms"].(float64)
		return &AgentResult{
			Output: envelope.Result,
			Metrics: StepMetrics{
				LatencyMs:    latency,
				InputTokens:  toInt(envelope.Metrics["total_input_tokens"]),
				OutputTokens: toInt(envelope.Metrics["total_output_tokens"]),
			},
		}, nil
	}

	return nil, fmt.Errorf("unknown route target type in route %s: %v", rb.ID, targetMap)
}

// routeKeys returns the valid (non-_none) keys from a RouteBlock's routes map.
func routeKeys(rb *RouteBlock) []string {
	keys := make([]string, 0, len(rb.Routes))
	for k := range rb.Routes {
		if k != "_none" {
			keys = append(keys, k)
		}
	}
	return keys
}

// decodeRouteBlock converts a raw interface{} (from YAML map) into a *RouteBlock.
func decodeRouteBlock(raw interface{}) (*RouteBlock, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("route block is not a map")
	}
	rb := &RouteBlock{}
	if id, ok := m["id"].(string); ok {
		rb.ID = id
	}
	if ra, ok := m["routing_agent"].(string); ok {
		rb.RoutingAgent = ra
	}
	if input, ok := m["input"].(map[string]interface{}); ok {
		rb.Input = input
	}
	if routes, ok := m["routes"].(map[string]interface{}); ok {
		rb.Routes = routes
	}
	if retryRaw, ok := m["retry"].(map[string]interface{}); ok {
		rb.Retry = &RetryConfig{
			MaxAttempts: toInt(retryRaw["max_attempts"]),
			BackoffMs:   toInt(retryRaw["backoff_ms"]),
		}
	}
	if fallbackRaw, ok := m["fallback"].(map[string]interface{}); ok {
		rb.Fallback = &RoutingAgentFallbackConfig{}
		if ra, ok := fallbackRaw["routing_agent"].(string); ok {
			rb.Fallback.RoutingAgent = ra
		}
		if cfg, ok := fallbackRaw["config"].(map[string]interface{}); ok {
			rb.Fallback.Config = cfg
		}
	}
	return rb, nil
}

// toInt safely converts interface{} to int.
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
	// Set version 4 bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
