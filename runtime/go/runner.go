package engine

import (
	"encoding/json"
	"fmt"
)

// deterministicHandlers holds registered handler functions for deterministic agents.
var deterministicHandlers = map[string]func(map[string]interface{}) (*AgentResult, error){}

var runnerLog = NewLogger("runner")

// RegisterHandler registers a deterministic agent handler by name.
func RegisterHandler(name string, fn func(map[string]interface{}) (*AgentResult, error)) {
	deterministicHandlers[name] = fn
}

// InvokeAgent loads an agent definition and executes it.
func InvokeAgent(agentID string, input interface{}) (*AgentResult, error) {
	def, err := LoadAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent %s: %w", agentID, err)
	}

	inputMap := ensureMap(input)
	return ExecuteAgent(inputMap, def)
}

// ExecuteAgent runs a single agent (LLM or deterministic) with the given input.
func ExecuteAgent(input map[string]interface{}, agentDef *AgentDefinition) (*AgentResult, error) {
	switch agentDef.Type {
	case "deterministic":
		return executeDeterministic(input, agentDef)
	case "llm":
		return executeLLM(input, agentDef)
	default:
		return nil, fmt.Errorf("unknown agent type: %s", agentDef.Type)
	}
}

func executeDeterministic(input map[string]interface{}, agentDef *AgentDefinition) (*AgentResult, error) {
	handler, ok := deterministicHandlers[agentDef.Handler]
	if !ok {
		return nil, fmt.Errorf("no handler registered for %q", agentDef.Handler)
	}

	runnerLog.Debug("executing deterministic agent", map[string]interface{}{
		"handler": agentDef.Handler,
	})

	return handler(input)
}

func executeLLM(input map[string]interface{}, agentDef *AgentDefinition) (*AgentResult, error) {
	// Build system prompt from the agent's prompt.md content
	systemPrompt := agentDef.Prompt
	if systemPrompt == "" {
		systemPrompt = agentDef.Description
	}

	// Resolve user message template
	userMessage := ResolveTemplate(agentDef.UserMessage, input)

	// Determine schema name
	schemaName := ""
	if agentDef.Schema != nil {
		schemaName = *agentDef.Schema
	}

	// Build user content — may be multipart for image inputs
	var userContent interface{}
	if agentDef.InputType == "image" {
		userContent = buildImageContent(input, userMessage, agentDef.ImageDetail)
	} else {
		userContent = userMessage
	}

	runnerLog.Debug("executing LLM agent", map[string]interface{}{
		"model":      agentDef.Model,
		"schema":     schemaName,
		"input_type": agentDef.InputType,
	})

	return CallLLM(LLMCallOptions{
		Model:        agentDef.Model,
		SystemPrompt: systemPrompt,
		UserContent:  userContent,
		Temperature:  agentDef.Temperature,
		SchemaName:   schemaName,
		Provider:     agentDef.Provider,
	})
}

// buildImageContent creates a multipart content array with an image and text.
func buildImageContent(input map[string]interface{}, textMessage string, detail string) []interface{} {
	if detail == "" {
		detail = "auto"
	}

	parts := []interface{}{}

	// Look for image data in input
	if imageData, ok := input["image"].(string); ok {
		parts = append(parts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url":    imageData,
				"detail": detail,
			},
		})
	}
	if imageURL, ok := input["image_url"].(string); ok {
		parts = append(parts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url":    imageURL,
				"detail": detail,
			},
		})
	}

	if textMessage != "" {
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": textMessage,
		})
	}

	return parts
}

// ensureMap converts input to map[string]interface{}.
// If it is already a map, return it. Otherwise, marshal to JSON and back.
func ensureMap(input interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	if m, ok := input.(map[string]interface{}); ok {
		return m
	}
	// Try JSON round-trip for struct types
	data, err := json.Marshal(input)
	if err != nil {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]interface{}{}
	}
	return m
}
