package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	openAIEndpoint    = "https://api.openai.com/v1/chat/completions"
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
)

// LLMCallOptions configures a single LLM invocation.
type LLMCallOptions struct {
	Model        string
	SystemPrompt string
	UserContent  interface{} // string or []interface{} for multipart content
	Temperature  float64
	SchemaName   string // non-empty -> structured output; empty -> json_object/json mode
	Provider     string // "openai" or "anthropic"; auto-detected from Model if empty
}

var llmLog = NewLogger("llm")

// detectProvider returns "openai" or "anthropic" based on model name or explicit override.
func detectProvider(model, explicit string) string {
	if explicit == "openai" || explicit == "anthropic" {
		return explicit
	}
	if strings.HasPrefix(model, "claude-") {
		return "anthropic"
	}
	return "openai"
}

// CallLLM invokes the appropriate provider's API based on the provider setting.
func CallLLM(opts LLMCallOptions) (*AgentResult, error) {
	provider := detectProvider(opts.Model, opts.Provider)

	llmLog.Debug("calling LLM", map[string]interface{}{
		"provider": provider,
		"model":    opts.Model,
		"schema":   opts.SchemaName,
	})

	if provider == "anthropic" {
		return callAnthropic(opts)
	}
	return callOpenAI(opts)
}

// ════════════════════════════════════════════════════════════════════════════
// OpenAI implementation
// ════════════════════════════════════════════════════════════════════════════

func callOpenAI(opts LLMCallOptions) (*AgentResult, error) {
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

// ════════════════════════════════════════════════════════════════════════════
// Anthropic implementation
// ════════════════════════════════════════════════════════════════════════════

// buildAnthropicSystemPrompt augments the base system prompt with JSON output
// instructions. When a schema is provided, it is embedded so the model returns
// structured output.
func buildAnthropicSystemPrompt(base, schemaName string) string {
	if schemaName != "" {
		schemaObj, err := LoadSchema(schemaName)
		if err == nil {
			// Extract the schema body (the "schema" key if present, otherwise the whole object)
			schemaBody := schemaObj
			if inner, ok := schemaObj["schema"]; ok {
				if innerMap, ok := inner.(map[string]interface{}); ok {
					schemaBody = innerMap
				}
			}
			schemaJSON, err := json.MarshalIndent(schemaBody, "", "  ")
			if err == nil {
				return base + "\n\nYou must respond with a valid JSON object that conforms to the following JSON schema:\n" +
					string(schemaJSON) +
					"\n\nOutput only the JSON with no additional text, markdown, or code fences."
			}
		}
	}

	return base + "\n\nYou must respond with a valid JSON object. Output only the JSON with no additional text, markdown, or code fences."
}

// buildAnthropicMessages converts user content to Anthropic message format.
func buildAnthropicMessages(userContent interface{}) []map[string]interface{} {
	switch c := userContent.(type) {
	case string:
		return []map[string]interface{}{
			{"role": "user", "content": c},
		}
	case []interface{}:
		// Convert OpenAI-style multipart to Anthropic content blocks
		blocks := make([]interface{}, 0, len(c))
		for _, part := range c {
			if partMap, ok := part.(map[string]interface{}); ok {
				partType, _ := partMap["type"].(string)
				if partType == "text" {
					text, _ := partMap["text"].(string)
					blocks = append(blocks, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				} else if partType == "image_url" {
					if imgURL, ok := partMap["image_url"].(map[string]interface{}); ok {
						url, _ := imgURL["url"].(string)
						if strings.HasPrefix(url, "data:") {
							// Parse data URI
							mediaType, data := parseDataURI(url)
							blocks = append(blocks, map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type":       "base64",
									"media_type": mediaType,
									"data":       data,
								},
							})
						} else {
							blocks = append(blocks, map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type": "url",
									"url":  url,
								},
							})
						}
					}
				}
			}
		}
		return []map[string]interface{}{
			{"role": "user", "content": blocks},
		}
	case []map[string]interface{}:
		// Same as above but with typed slice
		asInterface := make([]interface{}, len(c))
		for i, v := range c {
			asInterface[i] = v
		}
		return buildAnthropicMessages(asInterface)
	default:
		return []map[string]interface{}{
			{"role": "user", "content": fmt.Sprintf("%v", c)},
		}
	}
}

// parseDataURI extracts the media type and base64 data from a data URI.
func parseDataURI(uri string) (mediaType, data string) {
	// Format: data:<mediaType>;base64,<data>
	after := strings.TrimPrefix(uri, "data:")
	parts := strings.SplitN(after, ";base64,", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "image/png", after
}

func callAnthropic(opts LLMCallOptions) (*AgentResult, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	systemPrompt := buildAnthropicSystemPrompt(opts.SystemPrompt, opts.SchemaName)
	messages := buildAnthropicMessages(opts.UserContent)

	body := map[string]interface{}{
		"model":       opts.Model,
		"max_tokens":  4096,
		"system":      systemPrompt,
		"messages":    messages,
		"temperature": opts.Temperature,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Anthropic request body: %w", err)
	}

	start := time.Now()

	req, err := http.NewRequest("POST", anthropicEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create Anthropic HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Anthropic HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Anthropic response body: %w", err)
	}

	latencyMs := float64(time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Anthropic API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	// Extract text content
	contentStr := ""
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			contentStr = block.Text
			break
		}
	}

	if contentStr == "" {
		return nil, fmt.Errorf("Anthropic returned no text content")
	}

	var output interface{}
	if err := json.Unmarshal([]byte(contentStr), &output); err != nil {
		return nil, fmt.Errorf("failed to parse Anthropic output as JSON: %w (content: %s)", err, contentStr)
	}

	return &AgentResult{
		Output: output,
		Metrics: StepMetrics{
			LatencyMs:    latencyMs,
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
		},
	}, nil
}
