// Command repl is a replay engine for deterministic re-execution.
// It reads a replay config from stdin, loads trace events, and replays
// tool calls in dry, full, or compare mode.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// ReplInput is the input to the repl tool.
type ReplInput struct {
	TraceID   string `json:"trace_id"`
	TraceFile string `json:"trace_file"`
	Mode      string `json:"mode"` // full, dry, compare
}

// ReplOutput is the output of the repl tool.
type ReplOutput struct {
	Events   int        `json:"events"`
	Replayed int        `json:"replayed"`
	Matches  int        `json:"matches"`
	Diffs    []ReplDiff `json:"diffs"`
}

// ReplDiff records a mismatch between expected and actual output.
type ReplDiff struct {
	EventIdx int    `json:"event_idx"`
	Tool     string `json:"tool"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

// traceEntry is an extended trace event that pairs tool.call with tool.out.
type traceEntry struct {
	Idx       int
	Event     common.TraceEvent
	OutputRaw string // stored output from the subsequent tool.out event
}

func main() {
	var input ReplInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.TraceFile == "" {
		input.TraceFile = ".clock/trce.jsonl"
	}
	if input.Mode == "" {
		input.Mode = "dry"
	}

	validModes := map[string]bool{"dry": true, "full": true, "compare": true}
	if !validModes[input.Mode] {
		jsonutil.Fatal(fmt.Sprintf("invalid mode %q: must be dry, full, or compare", input.Mode))
	}

	// Load trace events
	events, err := loadTraceEvents(input.TraceFile, input.TraceID)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load trace: %v", err))
	}

	// Extract tool.call events and pair with tool.out
	entries := pairToolCalls(events)

	output := ReplOutput{
		Events: len(events),
	}

	switch input.Mode {
	case "dry":
		output = runDry(entries, output)
	case "full":
		output = runFull(entries, output)
	case "compare":
		output = runCompare(entries, output)
	}

	if output.Diffs == nil {
		output.Diffs = []ReplDiff{}
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func loadTraceEvents(path string, traceID string) ([]common.TraceEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var events []common.TraceEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev common.TraceEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip unparseable lines
		}

		// Filter by trace_id if provided
		if traceID != "" {
			if !matchesSession(ev, traceID) {
				continue
			}
		}

		events = append(events, ev)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	return events, nil
}

// matchesSession checks if a trace event belongs to the given session/trace ID.
// It checks the ChkID field and also the Data field for job_id matches.
func matchesSession(ev common.TraceEvent, traceID string) bool {
	if ev.ChkID == traceID {
		return true
	}

	// Check if Data contains a matching job_id or trace_id
	if ev.Data == nil {
		return false
	}
	dataBytes, err := json.Marshal(ev.Data)
	if err != nil {
		return false
	}
	var dataMap map[string]interface{}
	if err := json.Unmarshal(dataBytes, &dataMap); err != nil {
		return false
	}

	if id, ok := dataMap["job_id"].(string); ok && id == traceID {
		return true
	}
	if id, ok := dataMap["trace_id"].(string); ok && id == traceID {
		return true
	}

	return false
}

// pairToolCalls pairs each tool.call event with its subsequent tool.out event.
func pairToolCalls(events []common.TraceEvent) []traceEntry {
	var entries []traceEntry

	for i := 0; i < len(events); i++ {
		ev := events[i]
		if ev.Event != "tool.call" {
			continue
		}

		entry := traceEntry{
			Idx:   i,
			Event: ev,
		}

		// Look for the next tool.out event for the same tool
		for j := i + 1; j < len(events); j++ {
			if events[j].Event == "tool.out" && events[j].Tool == ev.Tool {
				// Extract output
				if events[j].Data != nil {
					outBytes, err := json.Marshal(events[j].Data)
					if err == nil {
						entry.OutputRaw = string(outBytes)
					}
				}
				break
			}
		}

		entries = append(entries, entry)
	}

	return entries
}

func runDry(entries []traceEntry, output ReplOutput) ReplOutput {
	// Just print what would be replayed
	for _, e := range entries {
		inputDesc := describeInput(e.Event)
		fmt.Fprintf(os.Stderr, "[dry] event_idx=%d tool=%s input=%s\n",
			e.Idx, e.Event.Tool, truncate(inputDesc, 100))
	}
	output.Replayed = 0
	output.Matches = 0
	return output
}

func runFull(entries []traceEntry, output ReplOutput) ReplOutput {
	replayed := 0

	for _, e := range entries {
		toolInput := extractToolInput(e.Event)
		_, err := executeTool(e.Event.Tool, toolInput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[full] event_idx=%d tool=%s error=%v\n",
				e.Idx, e.Event.Tool, err)
		} else {
			replayed++
		}
	}

	output.Replayed = replayed
	output.Matches = replayed // In full mode, matches = successful replays
	return output
}

func runCompare(entries []traceEntry, output ReplOutput) ReplOutput {
	replayed := 0
	matches := 0
	var diffs []ReplDiff

	for _, e := range entries {
		toolInput := extractToolInput(e.Event)
		actual, err := executeTool(e.Event.Tool, toolInput)
		replayed++

		if err != nil {
			diffs = append(diffs, ReplDiff{
				EventIdx: e.Idx,
				Tool:     e.Event.Tool,
				Expected: truncate(e.OutputRaw, 500),
				Got:      fmt.Sprintf("error: %v", err),
			})
			continue
		}

		// Compare outputs
		actualStr := strings.TrimSpace(string(actual))
		expectedStr := strings.TrimSpace(e.OutputRaw)

		if normalizeJSON(actualStr) == normalizeJSON(expectedStr) {
			matches++
		} else {
			diffs = append(diffs, ReplDiff{
				EventIdx: e.Idx,
				Tool:     e.Event.Tool,
				Expected: truncate(expectedStr, 500),
				Got:      truncate(actualStr, 500),
			})
		}
	}

	output.Replayed = replayed
	output.Matches = matches
	output.Diffs = diffs
	return output
}

// normalizeJSON re-marshals JSON to normalize formatting for comparison.
func normalizeJSON(s string) string {
	var obj interface{}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return s // not valid JSON, compare as-is
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return s
	}
	return string(b)
}

func extractToolInput(ev common.TraceEvent) []byte {
	if ev.Data == nil {
		return nil
	}
	dataBytes, err := json.Marshal(ev.Data)
	if err != nil {
		return nil
	}

	// Try to extract an "input" or "args" field from data
	var dataMap map[string]interface{}
	if err := json.Unmarshal(dataBytes, &dataMap); err != nil {
		return dataBytes
	}

	if input, ok := dataMap["input"]; ok {
		b, err := json.Marshal(input)
		if err == nil {
			return b
		}
	}
	if args, ok := dataMap["args"]; ok {
		b, err := json.Marshal(args)
		if err == nil {
			return b
		}
	}

	return dataBytes
}

func executeTool(name string, input []byte) ([]byte, error) {
	toolPath := findTool(name)

	cmd := exec.Command(toolPath)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("run %s: %w (stderr: %s)", name, err, truncate(stderr.String(), 500))
	}

	return stdout.Bytes(), nil
}

func describeInput(ev common.TraceEvent) string {
	if ev.Data == nil {
		return "<no input>"
	}
	b, err := json.Marshal(ev.Data)
	if err != nil {
		return "<marshal error>"
	}
	return string(b)
}

func findTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	binPath := fmt.Sprintf("bin/%s", name)
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	if p, err := exec.LookPath("clock-" + name); err == nil {
		return p
	}
	return name
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
