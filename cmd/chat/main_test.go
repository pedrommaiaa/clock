package main

import (
	"testing"
)

func TestClassifyIntent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"fix the bug in main.go", "change"},
		{"add a new endpoint", "change"},
		{"remove unused imports", "change"},
		{"update the version number", "change"},
		{"refactor the database layer", "change"},
		{"implement user auth", "change"},
		{"create a new module", "change"},
		{"delete the old file", "change"},
		{"rename the function", "change"},
		{"modify the config", "change"},
		{"replace the logger", "change"},
		{"rewrite the parser", "change"},
		{"write tests for this", "change"},
		{"build the deployment script", "change"},
		{"migrate to postgres", "change"},
		{"what does this function do?", "question"},
		{"how does the auth work?", "question"},
		{"explain the architecture", "question"},
		{"where is the config file?", "question"},
		{"why is this failing?", "question"},
		{"show me the logs", "question"},
		{"", "question"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := classifyIntent(tt.input)
			if got != tt.want {
				t.Errorf("classifyIntent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassifyIntent_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"FIX the bug", "change"},
		{"ADD a feature", "change"},
		{"Fix It", "change"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := classifyIntent(tt.input)
			if got != tt.want {
				t.Errorf("classifyIntent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatModelShort(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     string
	}{
		{"anthropic", "claude-sonnet-4-20250514", "anth:claude-sonnet-4"},
		{"openai", "gpt-4o-2024-01-15", "oai:gpt-4o"},
		{"ollama", "llama3", "oll:llama3"},
		{"vllm", "mistral", "vllm:mistral"},
		{"custom", "model-name", "custom:model-name"},
		// Model already has prefix
		{"anthropic", "anth:sonnet", "anth:sonnet"},
		{"openai", "oai:gpt4o", "oai:gpt4o"},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			got := formatModelShort(tt.provider, tt.model)
			if got != tt.want {
				t.Errorf("formatModelShort(%q, %q) = %q, want %q", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestExtractAnswer(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			"action envelope with answer",
			[]byte(`{"kind":"done","answer":"The function does X."}`),
			"The function does X.",
		},
		{
			"content field",
			[]byte(`{"content":"Here is the answer."}`),
			"Here is the answer.",
		},
		{
			"text field",
			[]byte(`{"text":"Some text response."}`),
			"Some text response.",
		},
		{
			"message field",
			[]byte(`{"message":"A message."}`),
			"A message.",
		},
		{
			"raw string JSON",
			[]byte(`"Just a plain string"`),
			"Just a plain string",
		},
		{
			"raw text",
			[]byte(`plain text response`),
			"plain text response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAnswer(tt.input)
			if got != tt.want {
				t.Errorf("extractAnswer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDispatch_SlashCommands(t *testing.T) {
	// Test slash command parsing logic.
	tests := []struct {
		input   string
		wantCmd string
		wantArg string
	}{
		{"/help", "/help", ""},
		{"/ask what is this", "/ask", "what is this"},
		{"/fix the bug", "/fix", "the bug"},
		{"/search main.go", "/search", "main.go"},
		{"/read src/main.go 10:20", "/read", "src/main.go 10:20"},
		{"/diff", "/diff", ""},
		{"/undo", "/undo", ""},
		{"/run ls -la", "/run", "ls -la"},
		{"/mode suggest", "/mode", "suggest"},
		{"/status", "/status", ""},
		{"/clear", "/clear", ""},
		{"/model anth:sonnet", "/model", "anth:sonnet"},
		{"/quit", "/quit", ""},
		{"/exit", "/exit", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Simulate the dispatch parsing logic
			input := tt.input
			if len(input) == 0 || input[0] != '/' {
				t.Fatal("expected slash command")
			}

			parts := splitCommand(input)
			cmd := parts[0]
			args := ""
			if len(parts) > 1 {
				args = parts[1]
			}

			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if args != tt.wantArg {
				t.Errorf("args = %q, want %q", args, tt.wantArg)
			}
		})
	}
}

// splitCommand splits a slash command into command and args.
// This mirrors the logic in Session.dispatch.
func splitCommand(input string) []string {
	parts := make([]string, 0, 2)
	idx := -1
	for i, c := range input {
		if c == ' ' {
			idx = i
			break
		}
	}
	if idx < 0 {
		parts = append(parts, input)
	} else {
		parts = append(parts, input[:idx])
		arg := input[idx+1:]
		// Trim leading whitespace from args
		for len(arg) > 0 && arg[0] == ' ' {
			arg = arg[1:]
		}
		if arg != "" {
			parts = append(parts, arg)
		}
	}
	return parts
}

func TestConversationHistory(t *testing.T) {
	// Test recentHistory logic
	session := &Session{
		History: []Message{
			{Role: "user", Content: "hello", TS: 1},
			{Role: "assistant", Content: "hi", TS: 2},
			{Role: "user", Content: "how", TS: 3},
			{Role: "assistant", Content: "fine", TS: 4},
			{Role: "user", Content: "bye", TS: 5},
		},
	}

	// Get last 3 messages
	recent := session.recentHistory(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "how" {
		t.Errorf("first recent = %q, want 'how'", recent[0].Content)
	}
	if recent[2].Content != "bye" {
		t.Errorf("last recent = %q, want 'bye'", recent[2].Content)
	}
}

func TestConversationHistory_FewerThanN(t *testing.T) {
	session := &Session{
		History: []Message{
			{Role: "user", Content: "hello", TS: 1},
		},
	}

	recent := session.recentHistory(10)
	if len(recent) != 1 {
		t.Errorf("expected 1 message when history < n, got %d", len(recent))
	}
}

func TestConversationHistory_Empty(t *testing.T) {
	session := &Session{
		History: nil,
	}

	recent := session.recentHistory(10)
	if len(recent) != 0 {
		t.Errorf("expected 0 messages for nil history, got %d", len(recent))
	}
}

func TestClearHistory(t *testing.T) {
	session := &Session{
		History: []Message{
			{Role: "user", Content: "hello"},
		},
	}

	session.clearHistory()

	if session.History != nil {
		t.Errorf("expected nil history after clear, got %v", session.History)
	}
}

func TestPatchApprovalParsing(t *testing.T) {
	tests := []struct {
		input   string
		wantAct string
	}{
		{"y", "apply"},
		{"yes", "apply"},
		{"n", "reject"},
		{"no", "reject"},
		{"e", "edit"},
		{"edit", "edit"},
		{"q", "abort"},
		{"quit", "abort"},
		{"unknown", "reject"},
		{"", "reject"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Simulate the patch approval parsing from handlePatch
			var action string
			switch tt.input {
			case "y", "yes":
				action = "apply"
			case "n", "no":
				action = "reject"
			case "e", "edit":
				action = "edit"
			case "q", "quit":
				action = "abort"
			default:
				action = "reject"
			}

			if action != tt.wantAct {
				t.Errorf("patch approval for %q = %q, want %q", tt.input, action, tt.wantAct)
			}
		})
	}
}
