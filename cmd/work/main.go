// Command work is the V1 agent loop executor.
// It reads a JobSpec JSON from stdin, runs the agent loop (srch/slce/pack/llm/act
// cycle up to 10 iterations), and outputs a JobResult JSON.
// Each sub-tool is invoked via os/exec, piping JSON through stdin/stdout.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const maxIterations = 10

func main() {
	var job common.JobSpec
	if err := jsonutil.ReadInput(&job); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if job.Goal == "" {
		jsonutil.Fatal("job goal is required")
	}

	result := runAgentLoop(job)

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// runAgentLoop executes the full agent workflow for a job.
func runAgentLoop(job common.JobSpec) common.JobResult {
	result := common.JobResult{
		ID:   job.ID,
		Goal: job.Goal,
	}

	// Step 1: Log start to trace
	traceEvent("work.start", "work", map[string]interface{}{
		"job_id": job.ID,
		"goal":   job.Goal,
	})

	// Step 2: Run rfrsh to ensure dossier is current
	if _, err := runTool("rfrsh", nil); err != nil {
		logWarn("rfrsh failed (continuing): %v", err)
	}

	// Step 3: Run scope to get workset
	scopeOut, err := runTool("scope", nil)
	if err != nil {
		logWarn("scope failed (continuing): %v", err)
	}
	_ = scopeOut // scope output used implicitly by other tools

	// Step 4: Enter agent loop
	query := job.Goal
	var lastErr string
	var accumulatedDiff string

	for i := 0; i < maxIterations; i++ {
		traceEvent("work.iteration", "work", map[string]interface{}{
			"iteration": i + 1,
			"query":     query,
		})

		// 4a: Run srch with the current query
		srchInput := map[string]string{"query": query}
		srchOut, err := runTool("srch", srchInput)
		if err != nil {
			logWarn("srch failed (iteration %d): %v", i+1, err)
			continue
		}

		// 4b: Run slce on top results (extract paths from search results)
		slcePaths := extractPaths(srchOut)
		if len(slcePaths) > 0 {
			slceInput := map[string]interface{}{
				"path": slcePaths[0], // top result
			}
			if _, err := runTool("slce", slceInput); err != nil {
				logWarn("slce failed (iteration %d): %v", i+1, err)
			}
		}

		// 4c: Run pack to build prompt bundle
		packInput := map[string]interface{}{
			"goal":    job.Goal,
			"context": srchOut,
		}
		if lastErr != "" {
			packInput["error"] = lastErr
		}
		packOut, err := runTool("pack", packInput)
		if err != nil {
			logWarn("pack failed (iteration %d): %v", i+1, err)
			continue
		}

		// 4d: Run llm to get action
		llmOut, err := runToolRaw("llm", packOut)
		if err != nil {
			logWarn("llm failed (iteration %d): %v", i+1, err)
			continue
		}

		// 4e: Run act to validate the action
		actOut, err := runToolRaw("act", llmOut)
		if err != nil {
			logWarn("act failed (iteration %d): %v", i+1, err)
			continue
		}

		// Parse the action output
		var action struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(actOut, &action); err != nil {
			logWarn("parse act output (iteration %d): %v", i+1, err)
			continue
		}

		traceEvent("work.action", "work", map[string]interface{}{
			"iteration": i + 1,
			"kind":      action.Kind,
		})

		// 4f-4h: Handle the action
		switch action.Kind {
		case "srch", "slce":
			// Continue loop with new query from payload
			var payload map[string]interface{}
			if err := json.Unmarshal(action.Payload, &payload); err == nil {
				if q, ok := payload["query"].(string); ok && q != "" {
					query = q
				}
			}
			continue

		case "patch":
			// Run guard -> aply -> vrfy pipeline
			ok, diff := handlePatch(action.Payload, i+1)
			if ok {
				accumulatedDiff += diff + "\n"
				// Patch succeeded, but continue loop in case more work needed
				query = job.Goal + " (patch applied, verify next steps)"
			} else {
				// vrfy failed, run undo and feed failure into next iteration
				lastErr = "patch application or verification failed"
				query = job.Goal + " (previous patch failed: " + lastErr + ")"
			}
			continue

		case "done":
			// Parse the answer
			var payload map[string]string
			if err := json.Unmarshal(action.Payload, &payload); err == nil {
				result.OK = true
				result.Report = payload["answer"]
				if accumulatedDiff != "" {
					result.Diff = accumulatedDiff
				}
			} else {
				result.OK = true
				result.Report = string(action.Payload)
			}

			traceEvent("work.done", "work", map[string]interface{}{
				"job_id":     job.ID,
				"iterations": i + 1,
				"ok":         true,
			})

			// Generate report via rpt
			generateReport(&result)
			return result

		default:
			logWarn("unknown action kind %q (iteration %d)", action.Kind, i+1)
			continue
		}
	}

	// Exhausted max iterations
	result.OK = false
	result.Err = fmt.Sprintf("exhausted %d iterations without reaching done", maxIterations)
	if accumulatedDiff != "" {
		result.Diff = accumulatedDiff
	}

	traceEvent("work.exhausted", "work", map[string]interface{}{
		"job_id":     job.ID,
		"iterations": maxIterations,
	})

	generateReport(&result)
	return result
}

// handlePatch runs the guard -> aply -> vrfy pipeline for a patch action.
// Returns (success, diff_text).
func handlePatch(payload json.RawMessage, iteration int) (bool, string) {
	// Run guard
	guardOut, err := runToolRaw("guard", payload)
	if err != nil {
		logWarn("guard failed (iteration %d): %v", iteration, err)
		return false, ""
	}

	var guardResult common.GuardResult
	if err := json.Unmarshal(guardOut, &guardResult); err != nil {
		logWarn("parse guard output (iteration %d): %v", iteration, err)
		return false, ""
	}

	if !guardResult.OK {
		logWarn("guard rejected patch (iteration %d): risk=%.2f reasons=%v",
			iteration, guardResult.Risk, guardResult.Reasons)
		return false, ""
	}

	// Run aply
	aplyOut, err := runToolRaw("aply", payload)
	if err != nil {
		logWarn("aply failed (iteration %d): %v", iteration, err)
		return false, ""
	}

	var aplyResult common.ApplyResult
	if err := json.Unmarshal(aplyOut, &aplyResult); err != nil {
		logWarn("parse aply output (iteration %d): %v", iteration, err)
		return false, ""
	}

	if !aplyResult.OK {
		logWarn("aply failed (iteration %d): %s", iteration, aplyResult.Err)
		return false, ""
	}

	// Extract diff from payload
	var patchPayload map[string]string
	diffText := ""
	if err := json.Unmarshal(payload, &patchPayload); err == nil {
		diffText = patchPayload["diff"]
	}

	// Run vrfy
	vrfyOut, err := runTool("vrfy", nil)
	if err != nil {
		logWarn("vrfy failed (iteration %d): %v", iteration, err)
		// Run undo to revert
		runUndo(aplyResult.ChkID)
		return false, ""
	}

	var vrfyResult common.VerifyResult
	if err := json.Unmarshal(vrfyOut, &vrfyResult); err == nil {
		if !vrfyResult.OK {
			logWarn("vrfy checks failed (iteration %d): %s", iteration, vrfyResult.Logs)
			// Run undo to revert
			runUndo(aplyResult.ChkID)
			return false, ""
		}
	}

	traceEvent("work.patch.ok", "work", map[string]interface{}{
		"iteration": iteration,
		"files":     aplyResult.Files,
		"lines_add": aplyResult.Lines.Add,
		"lines_del": aplyResult.Lines.Del,
	})

	return true, diffText
}

// runUndo reverts a patch by checkpoint ID.
func runUndo(chkID string) {
	input := map[string]string{"chk": chkID}
	if _, err := runTool("undo", input); err != nil {
		logWarn("undo failed for chk %s: %v", chkID, err)
	}
}

// generateReport runs the rpt tool to generate a final report.
func generateReport(result *common.JobResult) {
	rptInput := map[string]interface{}{
		"id":   result.ID,
		"ok":   result.OK,
		"goal": result.Goal,
		"diff": result.Diff,
		"err":  result.Err,
	}
	rptOut, err := runTool("rpt", rptInput)
	if err != nil {
		logWarn("rpt failed: %v", err)
		return
	}
	// If rpt returns a report string, use it
	var rpt map[string]string
	if err := json.Unmarshal(rptOut, &rpt); err == nil {
		if r, ok := rpt["report"]; ok && r != "" {
			result.Report = r
		}
	}
}

// extractPaths extracts file paths from search results output.
func extractPaths(data []byte) []string {
	// Try to parse as array of SearchHit
	var hits []common.SearchHit
	if err := json.Unmarshal(data, &hits); err == nil {
		seen := make(map[string]bool)
		var paths []string
		for _, h := range hits {
			if h.Path != "" && !seen[h.Path] {
				seen[h.Path] = true
				paths = append(paths, h.Path)
			}
		}
		return paths
	}

	// Try JSONL format (line by line)
	var paths []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var hit common.SearchHit
		if err := json.Unmarshal(line, &hit); err == nil && hit.Path != "" {
			paths = append(paths, hit.Path)
		}
	}
	return paths
}

// runTool runs a Clock tool by name, marshaling input as JSON to stdin
// and returning the raw stdout bytes.
func runTool(name string, input interface{}) ([]byte, error) {
	var stdinData []byte
	if input != nil {
		var err error
		stdinData, err = json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("marshal input for %s: %w", name, err)
		}
	}
	return runToolRaw(name, stdinData)
}

// runToolRaw runs a Clock tool by name with raw bytes as stdin.
func runToolRaw(name string, stdinData []byte) ([]byte, error) {
	start := time.Now()

	// Look for the tool in PATH, then relative locations
	toolPath := findTool(name)

	cmd := exec.Command(toolPath)
	if stdinData != nil {
		cmd.Stdin = bytes.NewReader(stdinData)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	traceEvent("tool.call", name, map[string]interface{}{
		"ms":     elapsed,
		"ok":     err == nil,
		"stderr": truncate(stderr.String(), 500),
	})

	if err != nil {
		return stdout.Bytes(), fmt.Errorf("run %s: %w (stderr: %s)", name, err, truncate(stderr.String(), 500))
	}

	return stdout.Bytes(), nil
}

// findTool locates a tool binary by name.
func findTool(name string) string {
	// Check if it's in PATH
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// Check bin/ directory relative to working dir
	binPath := fmt.Sprintf("bin/%s", name)
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	// Check clock- prefixed in PATH
	if p, err := exec.LookPath("clock-" + name); err == nil {
		return p
	}
	// Fallback: just use the name and let exec handle it
	return name
}

// traceEvent sends a trace event to the trce tool (best-effort).
func traceEvent(event, tool string, data interface{}) {
	ev := common.TraceEvent{
		TS:    time.Now().UnixMilli(),
		Event: event,
		Tool:  tool,
		Data:  data,
	}
	input, err := json.Marshal(ev)
	if err != nil {
		return
	}
	toolPath := findTool("trce")
	cmd := exec.Command(toolPath)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Fire and forget - don't block on trace
	_ = cmd.Run()
}

// logWarn writes a warning to stderr.
func logWarn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[work] WARN: %s\n", msg)
}

// truncate limits a string to maxLen bytes.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
