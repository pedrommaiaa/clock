// Command llm is an LLM API client that reads a request JSON from stdin,
// calls the specified provider (anthropic, openai, ollama), parses the response,
// and outputs an ActionEnvelope JSON to stdout.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// LLMInput is the input schema for the llm tool.
type LLMInput struct {
	Provider  string          `json:"provider"`
	Model     string          `json:"model"`
	Bundle    common.PackBundle `json:"bundle"`
	APIKeyEnv string          `json:"api_key_env,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

// anthropicRequest is the Anthropic API request body.
type anthropicRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []common.Message `json:"messages"`
	Tools     []common.ToolDef `json:"tools,omitempty"`
}

// anthropicResponse is the Anthropic API response body.
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// openaiRequest is the OpenAI API request body.
type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []openaiTool    `json:"tools,omitempty"`
}

// openaiMessage is an OpenAI chat message.
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiTool is an OpenAI function tool definition.
type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

// openaiFunction is an OpenAI function definition.
type openaiFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// openaiResponse is the OpenAI API response body.
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// ollamaRequest is the Ollama API request body.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

// ollamaResponse is the Ollama API response body.
type ollamaResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// retryableCodes are HTTP status codes that trigger a retry.
var retryableCodes = map[int]bool{
	429: true,
	500: true,
	502: true,
	503: true,
}

const maxRetries = 3

func main() {
	var input LLMInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Provider == "" {
		input.Provider = "anthropic"
	}
	if input.Model == "" {
		jsonutil.Fatal("model is required")
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = 4096
	}

	var envelope common.ActionEnvelope
	var err error

	switch input.Provider {
	case "anthropic":
		envelope, err = callAnthropic(input)
	case "openai":
		envelope, err = callOpenAI(input)
	case "ollama":
		envelope, err = callOllama(input)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown provider: %q", input.Provider))
	}

	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("api call: %v", err))
	}

	if err := jsonutil.WriteOutput(envelope); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// getAPIKey reads the API key from the environment.
func getAPIKey(input LLMInput, defaultEnv string) (string, error) {
	envVar := input.APIKeyEnv
	if envVar == "" {
		envVar = defaultEnv
	}
	key := os.Getenv(envVar)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set", envVar)
	}
	return key, nil
}

// doRequestWithRetry performs an HTTP request with retry logic.
func doRequestWithRetry(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 120 * time.Second}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			time.Sleep(backoff)
		}

		// Clone request body for retry
		var bodyBytes []byte
		if req.Body != nil {
			bodyBytes, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			// Reset body for next retry
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
			continue
		}

		if retryableCodes[resp.StatusCode] {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("exhausted %d retries: %w", maxRetries, lastErr)
}

// callAnthropic calls the Anthropic Messages API.
func callAnthropic(input LLMInput) (common.ActionEnvelope, error) {
	apiKey, err := getAPIKey(input, "ANTHROPIC_API_KEY")
	if err != nil {
		return common.ActionEnvelope{}, err
	}

	reqBody := anthropicRequest{
		Model:     input.Model,
		MaxTokens: input.MaxTokens,
		System:    input.Bundle.System,
		Messages:  input.Bundle.Messages,
		Tools:     input.Bundle.Tools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	respBody, err := doRequestWithRetry(req)
	if err != nil {
		return common.ActionEnvelope{}, err
	}

	var resp anthropicResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("parse response: %w", err)
	}

	return parseAnthropicResponse(resp)
}

// parseAnthropicResponse converts an Anthropic response to an ActionEnvelope.
func parseAnthropicResponse(resp anthropicResponse) (common.ActionEnvelope, error) {
	// Check for tool use first
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			var args interface{}
			if len(block.Input) > 0 {
				if err := json.Unmarshal(block.Input, &args); err != nil {
					args = json.RawMessage(block.Input)
				}
			}
			return common.ActionEnvelope{
				Kind: "tool",
				Name: block.Name,
				Args: args,
			}, nil
		}
	}

	// Collect text blocks
	var texts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}

	text := strings.Join(texts, "\n")
	if text == "" {
		return common.ActionEnvelope{
			Kind:   "done",
			Answer: "",
			Why:    "empty response",
		}, nil
	}

	return parseTextResponse(text)
}

// callOpenAI calls the OpenAI Chat Completions API.
func callOpenAI(input LLMInput) (common.ActionEnvelope, error) {
	apiKey, err := getAPIKey(input, "OPENAI_API_KEY")
	if err != nil {
		return common.ActionEnvelope{}, err
	}

	// Convert messages: prepend system message
	var messages []openaiMessage
	if input.Bundle.System != "" {
		messages = append(messages, openaiMessage{
			Role:    "system",
			Content: input.Bundle.System,
		})
	}
	for _, m := range input.Bundle.Messages {
		messages = append(messages, openaiMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Convert tools
	var tools []openaiTool
	for _, t := range input.Bundle.Tools {
		tools = append(tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}

	reqBody := openaiRequest{
		Model:    input.Model,
		Messages: messages,
		Tools:    tools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	respBody, err := doRequestWithRetry(req)
	if err != nil {
		return common.ActionEnvelope{}, err
	}

	var resp openaiResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("parse response: %w", err)
	}

	return parseOpenAIResponse(resp)
}

// parseOpenAIResponse converts an OpenAI response to an ActionEnvelope.
func parseOpenAIResponse(resp openaiResponse) (common.ActionEnvelope, error) {
	if len(resp.Choices) == 0 {
		return common.ActionEnvelope{
			Kind:   "done",
			Answer: "",
			Why:    "no choices in response",
		}, nil
	}

	choice := resp.Choices[0]

	// Check for tool calls
	if len(choice.Message.ToolCalls) > 0 {
		tc := choice.Message.ToolCalls[0]
		var args interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = tc.Function.Arguments
			}
		}
		return common.ActionEnvelope{
			Kind: "tool",
			Name: tc.Function.Name,
			Args: args,
		}, nil
	}

	text := choice.Message.Content
	if text == "" {
		return common.ActionEnvelope{
			Kind:   "done",
			Answer: "",
			Why:    "empty response",
		}, nil
	}

	return parseTextResponse(text)
}

// callOllama calls the Ollama chat API.
func callOllama(input LLMInput) (common.ActionEnvelope, error) {
	// Convert messages
	var messages []openaiMessage
	if input.Bundle.System != "" {
		messages = append(messages, openaiMessage{
			Role:    "system",
			Content: input.Bundle.System,
		})
	}
	for _, m := range input.Bundle.Messages {
		messages = append(messages, openaiMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	reqBody := ollamaRequest{
		Model:    input.Model,
		Messages: messages,
		Stream:   false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "http://localhost:11434/api/chat", bytes.NewReader(body))
	if err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	respBody, err := doRequestWithRetry(req)
	if err != nil {
		return common.ActionEnvelope{}, err
	}

	var resp ollamaResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return common.ActionEnvelope{}, fmt.Errorf("parse response: %w", err)
	}

	text := resp.Message.Content
	if text == "" {
		return common.ActionEnvelope{
			Kind:   "done",
			Answer: "",
			Why:    "empty response",
		}, nil
	}

	return parseTextResponse(text)
}

// parseTextResponse tries to parse a text response as a structured ActionEnvelope.
// If parsing fails, it wraps the text as a plain "done" response.
func parseTextResponse(text string) (common.ActionEnvelope, error) {
	// Try to extract JSON from the text (it might be wrapped in markdown code fences)
	jsonStr := extractJSON(text)

	if jsonStr != "" {
		var envelope common.ActionEnvelope
		if err := json.Unmarshal([]byte(jsonStr), &envelope); err == nil {
			if envelope.Kind != "" {
				return envelope, nil
			}
		}
	}

	// Plain text response - wrap as done
	return common.ActionEnvelope{
		Kind:   "done",
		Answer: text,
		Why:    "direct response",
	}, nil
}

// extractJSON tries to find a JSON object in text, including inside code fences.
func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)

	// Direct JSON object
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}

	// Look for JSON in code fences
	fenceStarts := []string{"```json\n", "```json\r\n", "```\n", "```\r\n"}
	for _, fence := range fenceStarts {
		if idx := strings.Index(trimmed, fence); idx >= 0 {
			start := idx + len(fence)
			end := strings.Index(trimmed[start:], "```")
			if end >= 0 {
				candidate := strings.TrimSpace(trimmed[start : start+end])
				if strings.HasPrefix(candidate, "{") {
					return candidate
				}
			}
		}
	}

	return ""
}
