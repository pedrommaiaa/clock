// Command diag provides system diagnostics by analyzing trace files.
// It reads a DiagInput JSON from stdin, parses the trace file, and outputs
// per-tool stats, LLM stats, slowest calls, and detected issues.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// DiagInput is the input schema for the diag tool.
type DiagInput struct {
	Source string `json:"source"` // path to trace JSONL file
	Since  string `json:"since"`  // duration filter, e.g. "24h"
}

// DiagOutput is the output schema for the diag tool.
type DiagOutput struct {
	Period      string       `json:"period"`
	TotalEvents int          `json:"total_events"`
	ToolStats   []ToolStat   `json:"tool_stats"`
	LLMStats    LLMStat      `json:"llm_stats"`
	Slowest     []SlowEntry  `json:"slowest"`
	Issues      []common.DiagIssue `json:"issues"`
}

// ToolStat holds per-tool aggregate statistics.
type ToolStat struct {
	Tool      string  `json:"tool"`
	Calls     int     `json:"calls"`
	AvgMs     int64   `json:"avg_ms"`
	Errors    int     `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
}

// LLMStat holds aggregate LLM statistics.
type LLMStat struct {
	Calls     int   `json:"calls"`
	EstTokens int   `json:"est_tokens"`
	AvgMs     int64 `json:"avg_ms"`
}

// SlowEntry is a single slow tool call.
type SlowEntry struct {
	Tool     string `json:"tool"`
	Ms       int64  `json:"ms"`
	EventIdx int    `json:"event_idx"`
}

// toolAccum accumulates stats for a single tool.
type toolAccum struct {
	calls    int
	totalMs  int64
	errors   int
}

func main() {
	var input DiagInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Source == "" {
		input.Source = ".clock/trce.jsonl"
	}
	if input.Since == "" {
		input.Since = "24h"
	}

	// Parse "since" as hours
	sinceHours := parseSinceHours(input.Since)
	cutoff := time.Now().Add(-time.Duration(sinceHours) * time.Hour)
	cutoffMs := cutoff.UnixMilli()

	// Read and parse trace file
	events, err := readTraceFile(input.Source)
	if err != nil {
		// If file doesn't exist, output empty diagnostics
		if os.IsNotExist(err) {
			emptyOutput(input.Since)
			return
		}
		jsonutil.Fatal(fmt.Sprintf("read trace file: %v", err))
	}

	// Filter by time
	var filtered []indexedEvent
	for i, ev := range events {
		if ev.TS >= cutoffMs {
			filtered = append(filtered, indexedEvent{idx: i, event: ev})
		}
	}

	// Compute stats
	tools := map[string]*toolAccum{}
	var llmCalls int
	var llmTotalMs int64
	var llmEstTokens int
	var retryCount int
	var allToolCalls []SlowEntry

	for _, ie := range filtered {
		ev := ie.event
		switch ev.Event {
		case "tool.call":
			name := ev.Tool
			if name == "" {
				continue
			}
			acc := getOrCreate(tools, name)
			acc.calls++
			if ev.Ms > 0 {
				acc.totalMs += ev.Ms
				allToolCalls = append(allToolCalls, SlowEntry{
					Tool:     name,
					Ms:       ev.Ms,
					EventIdx: ie.idx,
				})
			}
		case "tool.out":
			name := ev.Tool
			if name == "" {
				continue
			}
			acc := getOrCreate(tools, name)
			// Check for error in data
			if isErrorData(ev.Data) {
				acc.errors++
			}
			if ev.Ms > 0 {
				// If tool.call didn't have ms, use tool.out ms
				if acc.totalMs == 0 || acc.calls == 0 {
					acc.totalMs += ev.Ms
				}
				allToolCalls = append(allToolCalls, SlowEntry{
					Tool:     name,
					Ms:       ev.Ms,
					EventIdx: ie.idx,
				})
			}
		case "llm.in", "llm.out":
			if ev.Event == "llm.out" {
				llmCalls++
				if ev.Ms > 0 {
					llmTotalMs += ev.Ms
				}
				// Estimate tokens from data content length (rough: 4 chars per token)
				llmEstTokens += estimateTokens(ev.Data)
			}
		case "retry":
			retryCount++
		}
	}

	// Build tool stats
	var toolStats []ToolStat
	for name, acc := range tools {
		var avgMs int64
		if acc.calls > 0 {
			avgMs = acc.totalMs / int64(acc.calls)
		}
		var errorRate float64
		if acc.calls > 0 {
			errorRate = math.Round(float64(acc.errors)/float64(acc.calls)*100) / 100
		}
		toolStats = append(toolStats, ToolStat{
			Tool:      name,
			Calls:     acc.calls,
			AvgMs:     avgMs,
			Errors:    acc.errors,
			ErrorRate: errorRate,
		})
	}
	// Sort by calls descending
	sort.Slice(toolStats, func(i, j int) bool {
		return toolStats[i].Calls > toolStats[j].Calls
	})

	// LLM stats
	var llmAvgMs int64
	if llmCalls > 0 {
		llmAvgMs = llmTotalMs / int64(llmCalls)
	}
	llmStats := LLMStat{
		Calls:     llmCalls,
		EstTokens: llmEstTokens,
		AvgMs:     llmAvgMs,
	}

	// Top 5 slowest tool calls
	sort.Slice(allToolCalls, func(i, j int) bool {
		return allToolCalls[i].Ms > allToolCalls[j].Ms
	})
	slowest := allToolCalls
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}

	// Detect issues
	var issues []common.DiagIssue
	for _, ts := range toolStats {
		if ts.AvgMs > 5000 {
			issues = append(issues, common.DiagIssue{
				Tool:    ts.Tool,
				Problem: fmt.Sprintf("high avg latency: %dms", ts.AvgMs),
				Impact:  float64(ts.AvgMs) / 1000.0,
				Count:   ts.Calls,
			})
		}
		if ts.ErrorRate > 0.3 {
			issues = append(issues, common.DiagIssue{
				Tool:    ts.Tool,
				Problem: fmt.Sprintf("high error rate: %.0f%%", ts.ErrorRate*100),
				Impact:  ts.ErrorRate,
				Count:   ts.Errors,
			})
		}
	}
	if retryCount > 0 {
		issues = append(issues, common.DiagIssue{
			Tool:    "system",
			Problem: fmt.Sprintf("retry events detected: %d", retryCount),
			Impact:  float64(retryCount) / math.Max(float64(len(filtered)), 1.0),
			Count:   retryCount,
		})
	}

	if issues == nil {
		issues = []common.DiagIssue{}
	}
	if toolStats == nil {
		toolStats = []ToolStat{}
	}
	if slowest == nil {
		slowest = []SlowEntry{}
	}

	output := DiagOutput{
		Period:      input.Since,
		TotalEvents: len(filtered),
		ToolStats:   toolStats,
		LLMStats:    llmStats,
		Slowest:     slowest,
		Issues:      issues,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// indexedEvent pairs a trace event with its original index.
type indexedEvent struct {
	idx   int
	event common.TraceEvent
}

// parseSinceHours parses a duration string like "24h", "48h", "168h" into hours.
func parseSinceHours(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "h") {
		val := strings.TrimSuffix(s, "h")
		if h, err := strconv.ParseFloat(val, 64); err == nil {
			return h
		}
	}
	if strings.HasSuffix(s, "d") {
		val := strings.TrimSuffix(s, "d")
		if d, err := strconv.ParseFloat(val, 64); err == nil {
			return d * 24
		}
	}
	// Default: 24 hours
	return 24
}

// readTraceFile reads all trace events from a JSONL file.
func readTraceFile(path string) ([]common.TraceEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
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
			// Skip malformed lines
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return events, err
	}
	return events, nil
}

// getOrCreate returns the accumulator for a tool, creating if necessary.
func getOrCreate(m map[string]*toolAccum, name string) *toolAccum {
	if acc, ok := m[name]; ok {
		return acc
	}
	acc := &toolAccum{}
	m[name] = acc
	return acc
}

// isErrorData checks if the event data indicates an error.
func isErrorData(data interface{}) bool {
	if data == nil {
		return false
	}
	switch v := data.(type) {
	case map[string]interface{}:
		if _, ok := v["error"]; ok {
			return true
		}
		if _, ok := v["err"]; ok {
			return true
		}
		if ok, exists := v["ok"]; exists {
			if b, isBool := ok.(bool); isBool && !b {
				return true
			}
		}
	case string:
		lower := strings.ToLower(v)
		return strings.Contains(lower, "error") || strings.Contains(lower, "fail")
	}
	return false
}

// estimateTokens estimates token count from event data (roughly 4 chars per token).
func estimateTokens(data interface{}) int {
	if data == nil {
		return 0
	}
	b, err := json.Marshal(data)
	if err != nil {
		return 0
	}
	return len(b) / 4
}

// emptyOutput writes an empty diagnostics result.
func emptyOutput(since string) {
	output := DiagOutput{
		Period:      since,
		TotalEvents: 0,
		ToolStats:   []ToolStat{},
		LLMStats:    LLMStat{},
		Slowest:     []SlowEntry{},
		Issues:      []common.DiagIssue{},
	}
	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
