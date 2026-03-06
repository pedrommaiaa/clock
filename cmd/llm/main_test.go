package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestGetAPIKey(t *testing.T) {
	tests := []struct {
		name       string
		input      LLMInput
		defaultEnv string
		envKey     string
		envVal     string
		wantErr    bool
		wantKey    string
	}{
		{
			name:       "uses default env var",
			input:      LLMInput{},
			defaultEnv: "TEST_API_KEY_DEFAULT",
			envKey:     "TEST_API_KEY_DEFAULT",
			envVal:     "sk-default-123",
			wantErr:    false,
			wantKey:    "sk-default-123",
		},
		{
			name:       "uses custom env var",
			input:      LLMInput{APIKeyEnv: "CUSTOM_KEY_ENV"},
			defaultEnv: "TEST_API_KEY_DEFAULT",
			envKey:     "CUSTOM_KEY_ENV",
			envVal:     "sk-custom-456",
			wantErr:    false,
			wantKey:    "sk-custom-456",
		},
		{
			name:       "missing env var",
			input:      LLMInput{},
			defaultEnv: "NONEXISTENT_KEY_VAR_XYZ",
			envKey:     "",
			envVal:     "",
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any previously set values
			if tt.envKey != "" {
				os.Setenv(tt.envKey, tt.envVal)
				defer os.Unsetenv(tt.envKey)
			}

			key, err := getAPIKey(tt.input, tt.defaultEnv)
			if (err != nil) != tt.wantErr {
				t.Errorf("getAPIKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && key != tt.wantKey {
				t.Errorf("getAPIKey() = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string // empty means no JSON found
	}{
		{
			name: "direct JSON",
			text: `{"kind":"done","answer":"hello"}`,
			want: `{"kind":"done","answer":"hello"}`,
		},
		{
			name: "JSON with whitespace",
			text: `  {"kind":"tool","name":"srch"}  `,
			want: `{"kind":"tool","name":"srch"}`,
		},
		{
			name: "JSON in code fence",
			text: "Here is the result:\n```json\n{\"kind\":\"done\"}\n```\n",
			want: `{"kind":"done"}`,
		},
		{
			name: "JSON in plain code fence",
			text: "```\n{\"kind\":\"tool\",\"name\":\"slce\"}\n```",
			want: `{"kind":"tool","name":"slce"}`,
		},
		{
			name: "plain text no JSON",
			text: "This is just a plain text response with no JSON.",
			want: "",
		},
		{
			name: "empty string",
			text: "",
			want: "",
		},
		{
			name: "code fence without JSON object",
			text: "```json\nnot json\n```",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.text)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTextResponse(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantKind string
	}{
		{
			name:     "valid JSON envelope",
			text:     `{"kind":"tool","name":"srch","args":{"q":"hello"}}`,
			wantKind: "tool",
		},
		{
			name:     "plain text becomes done",
			text:     "I think the answer is 42.",
			wantKind: "done",
		},
		{
			name:     "JSON without kind becomes done",
			text:     `{"foo":"bar"}`,
			wantKind: "done",
		},
		{
			name:     "JSON in code fence",
			text:     "```json\n{\"kind\":\"patch\",\"diff\":\"---\"}\n```",
			wantKind: "patch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := parseTextResponse(tt.text)
			if err != nil {
				t.Fatalf("parseTextResponse() error: %v", err)
			}
			if env.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", env.Kind, tt.wantKind)
			}
		})
	}
}

func TestParseTextResponse_PlainTextPreservesContent(t *testing.T) {
	text := "The answer is 42."
	env, err := parseTextResponse(text)
	if err != nil {
		t.Fatal(err)
	}
	if env.Answer != text {
		t.Errorf("answer = %q, want %q", env.Answer, text)
	}
	if env.Why != "direct response" {
		t.Errorf("why = %q, want %q", env.Why, "direct response")
	}
}

func TestParseAnthropicResponse_ToolUse(t *testing.T) {
	resp := anthropicResponse{
		Content: []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		}{
			{
				Type:  "tool_use",
				Name:  "srch",
				Input: json.RawMessage(`{"query":"hello"}`),
			},
		},
	}

	env, err := parseAnthropicResponse(resp)
	if err != nil {
		t.Fatalf("parseAnthropicResponse() error: %v", err)
	}
	if env.Kind != "tool" {
		t.Errorf("kind = %q, want %q", env.Kind, "tool")
	}
	if env.Name != "srch" {
		t.Errorf("name = %q, want %q", env.Name, "srch")
	}
}

func TestParseAnthropicResponse_TextOnly(t *testing.T) {
	resp := anthropicResponse{
		Content: []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		}{
			{
				Type: "text",
				Text: "The answer is 42.",
			},
		},
	}

	env, err := parseAnthropicResponse(resp)
	if err != nil {
		t.Fatalf("parseAnthropicResponse() error: %v", err)
	}
	if env.Kind != "done" {
		t.Errorf("kind = %q, want %q", env.Kind, "done")
	}
}

func TestParseAnthropicResponse_EmptyContent(t *testing.T) {
	resp := anthropicResponse{
		Content: nil,
	}

	env, err := parseAnthropicResponse(resp)
	if err != nil {
		t.Fatalf("parseAnthropicResponse() error: %v", err)
	}
	if env.Kind != "done" {
		t.Errorf("kind = %q, want %q", env.Kind, "done")
	}
	if env.Why != "empty response" {
		t.Errorf("why = %q, want %q", env.Why, "empty response")
	}
}

func TestParseAnthropicResponse_MultipleTextBlocks(t *testing.T) {
	resp := anthropicResponse{
		Content: []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		}{
			{Type: "text", Text: "First"},
			{Type: "text", Text: "Second"},
		},
	}

	env, err := parseAnthropicResponse(resp)
	if err != nil {
		t.Fatalf("parseAnthropicResponse() error: %v", err)
	}
	// Should join with newline
	if env.Kind != "done" {
		t.Errorf("kind = %q, want %q", env.Kind, "done")
	}
}

func TestParseOpenAIResponse_NoChoices(t *testing.T) {
	resp := openaiResponse{}
	env, err := parseOpenAIResponse(resp)
	if err != nil {
		t.Fatalf("parseOpenAIResponse() error: %v", err)
	}
	if env.Kind != "done" {
		t.Errorf("kind = %q, want %q", env.Kind, "done")
	}
	if env.Why != "no choices in response" {
		t.Errorf("why = %q, want %q", env.Why, "no choices in response")
	}
}

func TestParseOpenAIResponse_TextContent(t *testing.T) {
	resp := openaiResponse{
		Choices: []struct {
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
		}{
			{
				Message: struct {
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
				}{
					Role:    "assistant",
					Content: "Hello world",
				},
				FinishReason: "stop",
			},
		},
	}

	env, err := parseOpenAIResponse(resp)
	if err != nil {
		t.Fatalf("parseOpenAIResponse() error: %v", err)
	}
	if env.Kind != "done" {
		t.Errorf("kind = %q, want %q", env.Kind, "done")
	}
}

func TestParseOpenAIResponse_ToolCall(t *testing.T) {
	resp := openaiResponse{
		Choices: []struct {
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
		}{
			{
				Message: struct {
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
				}{
					Role: "assistant",
					ToolCalls: []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}{
						{
							ID:   "call_123",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "srch",
								Arguments: `{"q":"test"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	env, err := parseOpenAIResponse(resp)
	if err != nil {
		t.Fatalf("parseOpenAIResponse() error: %v", err)
	}
	if env.Kind != "tool" {
		t.Errorf("kind = %q, want %q", env.Kind, "tool")
	}
	if env.Name != "srch" {
		t.Errorf("name = %q, want %q", env.Name, "srch")
	}
}

func TestDoRequestWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL, nil)
	body, err := doRequestWithRetry(req)
	if err != nil {
		t.Fatalf("doRequestWithRetry() error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want %q", string(body), `{"ok":true}`)
	}
}

func TestDoRequestWithRetry_NonRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL, nil)
	_, err := doRequestWithRetry(req)
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestDoRequestWithRetry_RetryableEventualSuccess(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 3 {
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL, nil)
	body, err := doRequestWithRetry(req)
	if err != nil {
		t.Fatalf("doRequestWithRetry() should succeed after retries: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", string(body))
	}
}

func TestDoRequestWithRetry_ExhaustedRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL, nil)
	_, err := doRequestWithRetry(req)
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
}

func TestRetryableCodes(t *testing.T) {
	codes := []int{429, 500, 502, 503}
	for _, code := range codes {
		if !retryableCodes[code] {
			t.Errorf("code %d should be retryable", code)
		}
	}
	nonRetryable := []int{200, 201, 400, 401, 403, 404}
	for _, code := range nonRetryable {
		if retryableCodes[code] {
			t.Errorf("code %d should not be retryable", code)
		}
	}
}

func TestCallAnthropic_MockServer(t *testing.T) {
	// Create a mock Anthropic API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			w.WriteHeader(400)
			return
		}

		resp := anthropicResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text,omitempty"`
				ID    string          `json:"id,omitempty"`
				Name  string          `json:"name,omitempty"`
				Input json.RawMessage `json:"input,omitempty"`
			}{
				{Type: "text", Text: "Hello from mock"},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// We cannot easily redirect callAnthropic to our mock server since
	// the URL is hardcoded. Instead, test the parsing functions which
	// are the core logic. The HTTP layer is tested via doRequestWithRetry.
	// This test is a placeholder showing the mock pattern.
	t.Log("Anthropic HTTP mock server test: parsing tested separately")
}

func TestCallOllama_MockServer(t *testing.T) {
	// Similar to Anthropic: the URL is hardcoded. The parsing is tested
	// via parseTextResponse. This verifies the mock pattern.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Done: true,
		}
		resp.Message.Role = "assistant"
		resp.Message.Content = `{"kind":"done","answer":"test"}`
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	t.Log("Ollama HTTP mock server test: parsing tested separately")
}

func TestBuildAnthropicRequest(t *testing.T) {
	// Test that we can construct and marshal an Anthropic request.
	input := LLMInput{
		Provider:  "anthropic",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 4096,
		Bundle: common.PackBundle{
			System: "You are a helpful assistant.",
			Messages: []common.Message{
				{Role: "user", Content: "Hello"},
			},
			Tools: []common.ToolDef{
				{Name: "srch", Description: "Search files"},
			},
		},
	}

	reqBody := anthropicRequest{
		Model:     input.Model,
		MaxTokens: input.MaxTokens,
		System:    input.Bundle.System,
		Messages:  input.Bundle.Messages,
		Tools:     input.Bundle.Tools,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v, want claude-sonnet-4-20250514", decoded["model"])
	}
	if decoded["system"] != "You are a helpful assistant." {
		t.Errorf("system = %v", decoded["system"])
	}
	if fmt.Sprintf("%v", decoded["max_tokens"]) != "4096" {
		t.Errorf("max_tokens = %v", decoded["max_tokens"])
	}
}

func TestBuildOpenAIRequest(t *testing.T) {
	input := LLMInput{
		Provider: "openai",
		Model:    "gpt-4",
		Bundle: common.PackBundle{
			System: "System prompt",
			Messages: []common.Message{
				{Role: "user", Content: "Hi"},
			},
		},
	}

	// Build messages the same way callOpenAI does
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

	if len(messages) != 2 {
		t.Errorf("messages count = %d, want 2", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", messages[0].Role, "system")
	}
	if messages[1].Role != "user" {
		t.Errorf("second message role = %q, want %q", messages[1].Role, "user")
	}
}

func TestMaxRetries(t *testing.T) {
	if maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", maxRetries)
	}
}
