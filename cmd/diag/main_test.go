package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestParseSinceHours(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"24h", 24},
		{"48h", 48},
		{"168h", 168},
		{"1d", 24},
		{"7d", 168},
		{"0.5h", 0.5},
		{"invalid", 24},     // default
		{"", 24},            // default
		{" 12h ", 12},       // whitespace
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSinceHours(tt.input)
			if got != tt.want {
				t.Errorf("parseSinceHours(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsErrorData(t *testing.T) {
	tests := []struct {
		name string
		data interface{}
		want bool
	}{
		{"nil", nil, false},
		{"error key", map[string]interface{}{"error": "something"}, true},
		{"err key", map[string]interface{}{"err": "something"}, true},
		{"ok false", map[string]interface{}{"ok": false}, true},
		{"ok true", map[string]interface{}{"ok": true}, false},
		{"no error keys", map[string]interface{}{"result": "fine"}, false},
		{"string error", "Error occurred", true},
		{"string fail", "operation failed", true},
		{"string ok", "everything is fine", false},
		{"string ERROR uppercase", "ERROR: bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isErrorData(tt.data)
			if got != tt.want {
				t.Errorf("isErrorData(%v) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		data interface{}
		min  int
		max  int
	}{
		{"nil", nil, 0, 0},
		{"short string", "hello", 0, 5},
		{"map", map[string]string{"key": "value"}, 1, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.data)
			if got < tt.min || got > tt.max {
				t.Errorf("estimateTokens(%v) = %d, want between %d and %d", tt.data, got, tt.min, tt.max)
			}
		})
	}
}

func TestGetOrCreate(t *testing.T) {
	m := map[string]*toolAccum{}

	// Create new entry
	acc := getOrCreate(m, "srch")
	if acc == nil {
		t.Fatal("expected non-nil accumulator")
	}
	acc.calls = 5

	// Get existing entry
	acc2 := getOrCreate(m, "srch")
	if acc2.calls != 5 {
		t.Errorf("expected calls=5, got %d", acc2.calls)
	}

	// Create another
	acc3 := getOrCreate(m, "slce")
	if acc3 == acc {
		t.Error("expected different accumulator for different tool")
	}
}

func TestReadTraceFile(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")

	now := time.Now().UnixMilli()

	events := []common.TraceEvent{
		{TS: now, Event: "tool.call", Tool: "srch", Ms: 100},
		{TS: now, Event: "tool.out", Tool: "srch", Ms: 100},
		{TS: now, Event: "llm.out", Tool: "llm", Ms: 5000, Data: "some response text here"},
	}

	f, err := os.Create(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		f.Write(b)
		f.Write([]byte("\n"))
	}
	f.Close()

	got, err := readTraceFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 events, got %d", len(got))
	}
}

func TestReadTraceFile_NotExist(t *testing.T) {
	_, err := readTraceFile("/nonexistent/trace.jsonl")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}
}

func TestReadTraceFile_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")

	content := `{"ts":1,"event":"tool.call","tool":"srch"}
not valid json
{"ts":2,"event":"llm.out","tool":"llm"}
`
	os.WriteFile(tracePath, []byte(content), 0o644)

	got, err := readTraceFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	// Should skip the malformed line
	if len(got) != 2 {
		t.Errorf("expected 2 events (skip malformed), got %d", len(got))
	}
}

func TestReadTraceFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")
	os.WriteFile(tracePath, []byte(""), 0o644)

	got, err := readTraceFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 events, got %d", len(got))
	}
}
