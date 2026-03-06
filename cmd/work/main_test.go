package main

import (
	"encoding/json"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestExtractPathsFromArray(t *testing.T) {
	hits := []common.SearchHit{
		{Path: "src/main.go", Line: 1, Text: "package main"},
		{Path: "src/util.go", Line: 5, Text: "func helper()"},
		{Path: "src/main.go", Line: 10, Text: "func main()"}, // duplicate
	}
	data, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}

	paths := extractPaths(data)
	if len(paths) != 2 {
		t.Fatalf("expected 2 unique paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "src/main.go" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "src/main.go")
	}
	if paths[1] != "src/util.go" {
		t.Errorf("paths[1] = %q, want %q", paths[1], "src/util.go")
	}
}

func TestExtractPathsFromJSONL(t *testing.T) {
	// JSONL fallback does not deduplicate (unlike the array parser)
	jsonl := `{"path":"a.go","line":1,"text":"x"}
{"path":"b.go","line":2,"text":"y"}
{"path":"a.go","line":3,"text":"z"}`

	paths := extractPaths([]byte(jsonl))
	// The JSONL path does not dedup, so we get 3 paths
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths from JSONL (no dedup), got %d: %v", len(paths), paths)
	}
	if paths[0] != "a.go" || paths[1] != "b.go" || paths[2] != "a.go" {
		t.Errorf("unexpected paths order: %v", paths)
	}
}

func TestExtractPathsFromJSONLUnique(t *testing.T) {
	jsonl := `{"path":"a.go","line":1,"text":"x"}
{"path":"b.go","line":2,"text":"y"}`

	paths := extractPaths([]byte(jsonl))
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
}

func TestExtractPathsEmpty(t *testing.T) {
	paths := extractPaths([]byte(""))
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestExtractPathsInvalidJSON(t *testing.T) {
	paths := extractPaths([]byte("not json at all"))
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for invalid JSON, got %d", len(paths))
	}
}

func TestExtractPathsEmptyPaths(t *testing.T) {
	hits := []common.SearchHit{
		{Path: "", Line: 1, Text: "no path"},
	}
	data, _ := json.Marshal(hits)
	paths := extractPaths(data)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for empty path hits, got %d", len(paths))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"over", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"zero_max", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestFindToolFallback(t *testing.T) {
	// For a tool name that won't exist anywhere, findTool should return the name itself
	name := "nonexistent_tool_xyz_12345"
	got := findTool(name)
	if got != name {
		t.Errorf("findTool(%q) = %q, want %q (fallback)", name, got, name)
	}
}

func TestRunAgentLoopExhausted(t *testing.T) {
	// runAgentLoop with a job that won't find real tools will exhaust iterations
	// This test verifies the result structure when all iterations fail
	job := common.JobSpec{
		ID:   "test-exhaust",
		Goal: "this should exhaust iterations",
	}

	result := runAgentLoop(job)

	if result.OK {
		t.Error("expected OK=false for exhausted loop")
	}
	if result.Err == "" {
		t.Error("expected non-empty error for exhausted loop")
	}
	if result.ID != "test-exhaust" {
		t.Errorf("result.ID = %q, want %q", result.ID, "test-exhaust")
	}
}

func TestHandlePatchGuardFail(t *testing.T) {
	// handlePatch should fail when guard tool is not available
	payload := json.RawMessage(`{"diff": "--- a/test\n+++ b/test\n+new line"}`)
	ok, diff := handlePatch(payload, 1)
	if ok {
		t.Error("expected handlePatch to fail when guard tool unavailable")
	}
	if diff != "" {
		t.Errorf("expected empty diff on failure, got %q", diff)
	}
}

func TestRunUndoNoTool(t *testing.T) {
	// Should not panic when undo tool is not available
	runUndo("fake-chk-id")
}

func TestGenerateReportNoTool(t *testing.T) {
	// Should not panic when rpt tool is not available
	result := &common.JobResult{
		ID:     "test-report",
		OK:     true,
		Goal:   "test",
		Report: "original report",
	}
	generateReport(result)
	// Report should stay as-is when rpt tool fails
	if result.Report != "original report" {
		t.Errorf("report changed unexpectedly to %q", result.Report)
	}
}

func TestLogWarnDoesNotPanic(t *testing.T) {
	// logWarn should not panic
	logWarn("test warning: %s", "message")
}
