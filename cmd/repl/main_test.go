package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestNormalizeJSON(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "same object different formatting",
			a:    `{"a":1,"b":2}`,
			b:    `{ "a" : 1, "b" : 2 }`,
			want: true,
		},
		{
			name: "different values",
			a:    `{"a":1}`,
			b:    `{"a":2}`,
			want: false,
		},
		{
			name: "not json",
			a:    "hello",
			b:    "hello",
			want: true, // falls back to string comparison
		},
		{
			name: "different not json",
			a:    "hello",
			b:    "world",
			want: false,
		},
		{
			name: "empty",
			a:    "",
			b:    "",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeJSON(tt.a) == normalizeJSON(tt.b)
			if got != tt.want {
				t.Errorf("normalizeJSON match = %v, want %v (a=%q b=%q)", got, tt.want, tt.a, tt.b)
			}
		})
	}
}

func TestMatchesSession(t *testing.T) {
	tests := []struct {
		name    string
		ev      common.TraceEvent
		traceID string
		want    bool
	}{
		{
			name:    "ChkID match",
			ev:      common.TraceEvent{ChkID: "trace-1"},
			traceID: "trace-1",
			want:    true,
		},
		{
			name:    "ChkID mismatch",
			ev:      common.TraceEvent{ChkID: "trace-2"},
			traceID: "trace-1",
			want:    false,
		},
		{
			name: "data job_id match",
			ev: common.TraceEvent{
				Data: map[string]interface{}{"job_id": "trace-1"},
			},
			traceID: "trace-1",
			want:    true,
		},
		{
			name: "data trace_id match",
			ev: common.TraceEvent{
				Data: map[string]interface{}{"trace_id": "trace-1"},
			},
			traceID: "trace-1",
			want:    true,
		},
		{
			name:    "nil data no match",
			ev:      common.TraceEvent{},
			traceID: "trace-1",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSession(tt.ev, tt.traceID)
			if got != tt.want {
				t.Errorf("matchesSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPairToolCalls(t *testing.T) {
	events := []common.TraceEvent{
		{Event: "tool.call", Tool: "srch", Data: map[string]interface{}{"query": "auth"}},
		{Event: "tool.out", Tool: "srch", Data: map[string]interface{}{"results": []string{}}},
		{Event: "tool.call", Tool: "slce", Data: map[string]interface{}{"path": "main.go"}},
		{Event: "llm.in"},
		{Event: "tool.out", Tool: "slce", Data: map[string]interface{}{"text": "code"}},
	}

	entries := pairToolCalls(events)

	if len(entries) != 2 {
		t.Fatalf("pairToolCalls returned %d entries, want 2", len(entries))
	}

	if entries[0].Event.Tool != "srch" {
		t.Errorf("entry 0 tool = %q, want %q", entries[0].Event.Tool, "srch")
	}
	if entries[0].OutputRaw == "" {
		t.Error("entry 0 should have output paired")
	}

	if entries[1].Event.Tool != "slce" {
		t.Errorf("entry 1 tool = %q, want %q", entries[1].Event.Tool, "slce")
	}
}

func TestPairToolCallsNoOutput(t *testing.T) {
	events := []common.TraceEvent{
		{Event: "tool.call", Tool: "srch"},
		// No tool.out
	}

	entries := pairToolCalls(events)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].OutputRaw != "" {
		t.Error("entry without output should have empty OutputRaw")
	}
}

func TestExtractToolInput(t *testing.T) {
	tests := []struct {
		name string
		ev   common.TraceEvent
		want string
	}{
		{
			name: "with input field",
			ev: common.TraceEvent{
				Data: map[string]interface{}{
					"input": map[string]interface{}{"query": "test"},
				},
			},
			want: `{"query":"test"}`,
		},
		{
			name: "with args field",
			ev: common.TraceEvent{
				Data: map[string]interface{}{
					"args": map[string]interface{}{"path": "main.go"},
				},
			},
			want: `{"path":"main.go"}`,
		},
		{
			name: "nil data",
			ev:   common.TraceEvent{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractToolInput(tt.ev)
			if tt.want == "" {
				if got != nil {
					t.Errorf("expected nil, got %s", string(got))
				}
				return
			}
			if normalizeJSON(string(got)) != normalizeJSON(tt.want) {
				t.Errorf("extractToolInput = %s, want %s", string(got), tt.want)
			}
		})
	}
}

func TestDescribeInput(t *testing.T) {
	tests := []struct {
		name string
		ev   common.TraceEvent
		want string
	}{
		{
			name: "nil data",
			ev:   common.TraceEvent{},
			want: "<no input>",
		},
		{
			name: "with data",
			ev:   common.TraceEvent{Data: map[string]interface{}{"key": "val"}},
			want: `{"key":"val"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeInput(tt.ev)
			if tt.ev.Data == nil {
				if got != tt.want {
					t.Errorf("describeInput = %q, want %q", got, tt.want)
				}
			} else {
				if normalizeJSON(got) != normalizeJSON(tt.want) {
					t.Errorf("describeInput = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestLoadTraceEvents(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")

	events := []common.TraceEvent{
		{TS: 1000, Event: "tool.call", Tool: "srch", ChkID: "session-1"},
		{TS: 2000, Event: "tool.out", Tool: "srch", ChkID: "session-1"},
		{TS: 3000, Event: "tool.call", Tool: "slce", ChkID: "session-2"},
	}

	f, err := os.Create(tracePath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, ev := range events {
		enc.Encode(ev)
	}
	f.Close()

	// Load all events
	loaded, err := loadTraceEvents(tracePath, "")
	if err != nil {
		t.Fatalf("loadTraceEvents: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("loaded %d events, want 3", len(loaded))
	}

	// Load filtered by session
	filtered, err := loadTraceEvents(tracePath, "session-1")
	if err != nil {
		t.Fatalf("loadTraceEvents filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("filtered %d events, want 2", len(filtered))
	}
}

func TestLoadTraceEventsMissingFile(t *testing.T) {
	_, err := loadTraceEvents("/nonexistent/trace.jsonl", "")
	if err == nil {
		t.Error("expected error for missing trace file")
	}
}

func TestRunDry(t *testing.T) {
	entries := []traceEntry{
		{Idx: 0, Event: common.TraceEvent{Tool: "srch"}},
		{Idx: 1, Event: common.TraceEvent{Tool: "slce"}},
	}

	output := ReplOutput{Events: 5}
	result := runDry(entries, output)

	if result.Replayed != 0 {
		t.Errorf("dry mode should have 0 replayed, got %d", result.Replayed)
	}
	if result.Matches != 0 {
		t.Errorf("dry mode should have 0 matches, got %d", result.Matches)
	}
}

func TestTruncateRepl(t *testing.T) {
	if truncate("short", 100) != "short" {
		t.Error("short string should not be truncated")
	}
	got := truncate("hello world", 5)
	if got != "hello..." {
		t.Errorf("truncate = %q, want %q", got, "hello...")
	}
}

func TestFindToolRepl(t *testing.T) {
	got := findTool("nonexistent-repl-tool")
	if got != "nonexistent-repl-tool" {
		t.Errorf("findTool fallback = %q, want original name", got)
	}
}
