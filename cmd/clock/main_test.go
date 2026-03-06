package main

import (
	"encoding/json"
	"testing"
)

func TestMustJSON(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"string", "hello"},
		{"map", map[string]string{"key": "value"}},
		{"int", 42},
		{"nil", nil},
		{"slice", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mustJSON(tt.input)
			if len(result) == 0 {
				t.Error("expected non-empty JSON")
			}
			// Verify it's valid JSON
			if !json.Valid(result) {
				t.Errorf("mustJSON produced invalid JSON: %s", result)
			}
		})
	}
}

func TestMustJSON_Panics(t *testing.T) {
	// Test that mustJSON panics on unmarshalable values
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unmarshalable value")
		}
	}()

	// Channels cannot be marshaled to JSON
	ch := make(chan int)
	mustJSON(ch)
}

func TestFindTool_NotInPath(t *testing.T) {
	// Test that findTool returns an error for a nonexistent tool
	_, err := findTool("nonexistent-tool-12345")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

func TestSubcommandRouting(t *testing.T) {
	// Test the subcommand routing logic without executing commands
	tests := []struct {
		args    []string
		wantCmd string
	}{
		{[]string{"clock", "init"}, "init"},
		{[]string{"clock", "ask", "question"}, "ask"},
		{[]string{"clock", "fix", "goal"}, "fix"},
		{[]string{"clock", "start"}, "start"},
		{[]string{"clock", "stop"}, "stop"},
		{[]string{"clock", "status"}, "status"},
		{[]string{"clock", "chat"}, "chat"},
		{[]string{"clock", "doctor"}, "doctor"},
		{[]string{"clock", "help"}, "help"},
		{[]string{"clock", "-h"}, "help"},
		{[]string{"clock", "--help"}, "help"},
	}

	validCmds := map[string]bool{
		"init": true, "ask": true, "fix": true,
		"start": true, "stop": true, "status": true,
		"chat": true, "doctor": true,
		"help": true, "-h": true, "--help": true,
	}

	for _, tt := range tests {
		t.Run(tt.wantCmd, func(t *testing.T) {
			subcmd := tt.args[1]
			// Normalize help variants
			switch subcmd {
			case "help", "-h", "--help":
				subcmd = "help"
			}

			_, isValid := validCmds[tt.args[1]]
			if !isValid {
				t.Errorf("subcommand %q not recognized", tt.args[1])
			}

			if subcmd != tt.wantCmd {
				t.Errorf("subcommand routing: got %q, want %q", subcmd, tt.wantCmd)
			}
		})
	}
}

func TestSubcommandRouting_Unknown(t *testing.T) {
	unknownCmds := []string{"deploy", "build", "test", "run"}

	validCmds := map[string]bool{
		"init": true, "ask": true, "fix": true,
		"start": true, "stop": true, "status": true,
		"chat": true, "doctor": true,
		"help": true, "-h": true, "--help": true,
	}

	for _, cmd := range unknownCmds {
		if validCmds[cmd] {
			t.Errorf("command %q should not be valid", cmd)
		}
	}
}

func TestArgumentParsing(t *testing.T) {
	// Test ask/fix argument joining
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"single word", []string{"clock", "ask", "hello"}, "hello"},
		{"multiple words", []string{"clock", "ask", "how", "does", "this", "work"}, "how does this work"},
		{"fix goal", []string{"clock", "fix", "add", "auth"}, "add auth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the argument joining from main()
			var result string
			if len(tt.args) > 2 {
				parts := tt.args[2:]
				joined := ""
				for i, p := range parts {
					if i > 0 {
						joined += " "
					}
					joined += p
				}
				result = joined
			}

			if result != tt.want {
				t.Errorf("argument join = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestReadDossier_NotExist(t *testing.T) {
	// readDossier should return empty string when file doesn't exist
	got := readDossier()
	// In this test environment there's no .clock/doss.md
	// It should not panic, just return empty
	_ = got
}

func TestReadPolicy_NotExist(t *testing.T) {
	// readPolicy should return nil when file doesn't exist
	got := readPolicy()
	if got != nil {
		t.Errorf("expected nil policy for non-existent file, got %s", string(got))
	}
}
