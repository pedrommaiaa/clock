package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestTraceAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")

	ev := common.TraceEvent{
		TS:    1000,
		Event: "tool.call",
		Tool:  "exec",
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var got common.TraceEvent
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.Event != "tool.call" {
		t.Errorf("event = %q, want %q", got.Event, "tool.call")
	}
	if got.Tool != "exec" {
		t.Errorf("tool = %q, want %q", got.Tool, "exec")
	}
	if got.TS != 1000 {
		t.Errorf("ts = %d, want 1000", got.TS)
	}
}

func TestTraceMultipleAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")

	events := []common.TraceEvent{
		{TS: 1, Event: "a"},
		{TS: 2, Event: "b"},
		{TS: 3, Event: "c"},
	}

	for _, ev := range events {
		line, _ := json.Marshal(ev)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(append(line, '\n'))
		f.Close()
	}

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, l := range lines {
		var got common.TraceEvent
		if err := json.Unmarshal([]byte(l), &got); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if got.Event != events[i].Event {
			t.Errorf("line %d: event = %q, want %q", i, got.Event, events[i].Event)
		}
	}
}

func TestTraceTimestampFill(t *testing.T) {
	ev := common.TraceEvent{Event: "test"}
	if ev.TS != 0 {
		t.Fatal("expected zero TS")
	}

	// Simulate the main logic: fill timestamp if missing
	before := time.Now().UnixMilli()
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	after := time.Now().UnixMilli()

	if ev.TS < before || ev.TS > after {
		t.Errorf("timestamp %d not in range [%d, %d]", ev.TS, before, after)
	}
}

func TestTraceTimestampPreserved(t *testing.T) {
	ev := common.TraceEvent{TS: 42, Event: "test"}
	// The main logic should NOT overwrite a non-zero TS
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	if ev.TS != 42 {
		t.Errorf("timestamp should have been preserved as 42, got %d", ev.TS)
	}
}

func TestTraceDirectoryCreation(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested")
	path := filepath.Join(nested, "trace.jsonl")

	// Simulate the mkdir logic
	dirPath := filepath.Dir(path)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(nested); os.IsNotExist(err) {
		t.Fatal("directory was not created")
	}
}

func TestTraceJSONRoundTrip(t *testing.T) {
	ev := common.TraceEvent{
		TS:    12345,
		Event: "llm.out",
		Tool:  "risk",
		Data:  map[string]interface{}{"score": 0.5},
		Ms:    100,
		ChkID: "abc123",
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var got common.TraceEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	if got.TS != 12345 {
		t.Errorf("TS = %d, want 12345", got.TS)
	}
	if got.Event != "llm.out" {
		t.Errorf("Event = %q, want %q", got.Event, "llm.out")
	}
	if got.Tool != "risk" {
		t.Errorf("Tool = %q, want %q", got.Tool, "risk")
	}
	if got.Ms != 100 {
		t.Errorf("Ms = %d, want 100", got.Ms)
	}
	if got.ChkID != "abc123" {
		t.Errorf("ChkID = %q, want %q", got.ChkID, "abc123")
	}
}
