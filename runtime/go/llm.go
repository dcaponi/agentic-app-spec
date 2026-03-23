package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

// LLMCallOptions configures a single LLM invocation.
type LLMCallOptions struct {
	Model        string
	SystemPrompt string
	UserContent  interface{} // string or []interface{} for multipart content
	Temperature  float64
	SchemaName   string // non-empty -> structured output via json_schema; empty -> json_object
}

var llmLog = NewLogger("llm")

// CallLLM invokes the OpenAI chat completions API via raw HTTP.
func CallLLM(opts LLMCallOptions) (*AgentResult, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	// Build messages
	messages := []map[string]interface{}{
		{"role": "system", "content": opts.SystemPrompt},
	}

	// User message: can be a plain string or multipart content array
	switch c := opts.UserContent.(type) {
	case string:
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": c,
		})
	case []interface{}:
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": c,
		})
	case []map[string]interface{}:
		// Convert to []interface{} for JSON marshaling
		parts := make([]interface{}, len(c))
		for i, v := range c {
			parts[i] = v
		}
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": parts,
		})
	default:
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("%v", c),
		})
	}

	// Build request body
	body := map[string]interface{}{
		"model":       opts.Model,
		"messages":    messages,
		"temperature": opts.Temperature,
	}

	// Response format
	if opts.SchemaName != "" {
		schemaObj, err := LoadSchema(opts.SchemaName)
		if err != nil {
			return nil, fmt.Errorf("failed to load schema %s: %w", opts.SchemaName, err)
		}
		// Schema files are already in OpenAI json_schema format with name, strict, schema fields
		body["response_format"] = map[string]interface{}{
			"type":        "json_schema",
			"json_schema": schemaObj,
		}
	} else {
		body["response_format"] = map[string]interface{}{
			"type": "json_object",
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	llmLog.Debug("calling OpenAI", map[string]interface{}{
		"model":  opts.Model,
		"schema": opts.SchemaName,
	})

	start := time.Now()

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	latencyMs := float64(time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI returned no choices")
	}

	// Parse the content as JSON
	contentStr := apiResp.Choices[0].Message.Content
	var output interface{}
	if err := json.Unmarshal([]byte(contentStr), &output); err != nil {
		return nil, fmt.Errorf("failed to parse LLM output as JSON: %w (content: %s)", err, contentStr)
	}

	return &AgentResult{
		Output: output,
		Metrics: StepMetrics{
			LatencyMs:    latencyMs,
			InputTokens:  apiResp.Usage.PromptTokens,
			OutputTokens: apiResp.Usage.CompletionTokens,
		},
	}, nil
}
