// Command self is the self-analysis entry point.
// It shells out to diag to get diagnostics, analyzes them for improvement
// opportunities, and outputs proposals for tool, playbook, policy, or workflow changes.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// SelfInput is the input schema for the self tool.
type SelfInput struct {
	Goal      string `json:"goal"`
	TraceFile string `json:"trace_file"`
	MemDir    string `json:"mem_dir"`
}

// SelfOutput is the output schema for the self tool.
type SelfOutput struct {
	Proposals          []common.Proposal `json:"proposals"`
	DiagnosticsSummary string            `json:"diagnostics_summary"`
}

// DiagInput mirrors the diag tool's input.
type DiagInput struct {
	Source string `json:"source"`
	Since  string `json:"since"`
}

// DiagOutput mirrors the diag tool's output for unmarshalling.
type DiagOutput struct {
	Period      string      `json:"period"`
	TotalEvents int         `json:"total_events"`
	ToolStats   []ToolStat  `json:"tool_stats"`
	LLMStats    LLMStat     `json:"llm_stats"`
	Slowest     []SlowEntry `json:"slowest"`
	Issues      []DiagIssue `json:"issues"`
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

// DiagIssue mirrors common.DiagIssue for unmarshalling.
type DiagIssue struct {
	Tool    string  `json:"tool"`
	Problem string  `json:"problem"`
	Impact  float64 `json:"impact,omitempty"`
	Count   int     `json:"count,omitempty"`
}

func main() {
	var input SelfInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.TraceFile == "" {
		input.TraceFile = ".clock/trce.jsonl"
	}
	if input.MemDir == "" {
		input.MemDir = ".clock/mem/"
	}

	// Shell out to diag
	diag, diagErr := runDiag(input.TraceFile)

	var proposals []common.Proposal
	var summaryParts []string

	if diagErr != nil {
		summaryParts = append(summaryParts, fmt.Sprintf("diag error: %v", diagErr))
		// Still produce proposals based on the goal alone
		proposals = append(proposals, goalBasedProposals(input.Goal)...)
	} else {
		summaryParts = append(summaryParts, fmt.Sprintf("period=%s events=%d tools=%d llm_calls=%d",
			diag.Period, diag.TotalEvents, len(diag.ToolStats), diag.LLMStats.Calls))

		if len(diag.Issues) > 0 {
			var issueDescs []string
			for _, iss := range diag.Issues {
				issueDescs = append(issueDescs, fmt.Sprintf("%s: %s", iss.Tool, iss.Problem))
			}
			summaryParts = append(summaryParts, fmt.Sprintf("issues: %s", strings.Join(issueDescs, "; ")))
		}

		// Analyze diagnostics for proposals
		proposals = append(proposals, analyzeDiagnostics(diag)...)
		proposals = append(proposals, goalBasedProposals(input.Goal)...)
	}

	// Deduplicate proposals by name
	proposals = deduplicateProposals(proposals)

	if proposals == nil {
		proposals = []common.Proposal{}
	}

	output := SelfOutput{
		Proposals:          proposals,
		DiagnosticsSummary: strings.Join(summaryParts, "; "),
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// runDiag shells out to the diag binary and parses its output.
func runDiag(traceFile string) (*DiagOutput, error) {
	diagInput := DiagInput{
		Source: traceFile,
		Since:  "24h",
	}
	inputBytes, err := json.Marshal(diagInput)
	if err != nil {
		return nil, fmt.Errorf("marshal diag input: %w", err)
	}

	cmd := exec.Command("diag")
	cmd.Stdin = bytes.NewReader(inputBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Try running as go run if binary not found
		cmd2 := exec.Command("go", "run", "./cmd/diag")
		cmd2.Stdin = bytes.NewReader(inputBytes)
		var stdout2, stderr2 bytes.Buffer
		cmd2.Stdout = &stdout2
		cmd2.Stderr = &stderr2
		if err2 := cmd2.Run(); err2 != nil {
			return nil, fmt.Errorf("diag failed: %v (stderr: %s)", err, stderr.String())
		}
		stdout = stdout2
	}

	var result DiagOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parse diag output: %w (raw: %s)", err, stdout.String())
	}
	return &result, nil
}

// analyzeDiagnostics generates proposals from diagnostic results.
func analyzeDiagnostics(diag *DiagOutput) []common.Proposal {
	var proposals []common.Proposal

	for _, ts := range diag.ToolStats {
		// High latency: avg > 5000ms
		if ts.AvgMs > 5000 {
			proposals = append(proposals, common.Proposal{
				Type:   "tool",
				Name:   fmt.Sprintf("optimize-%s", ts.Tool),
				Reason: fmt.Sprintf("Tool %q has avg latency %dms (threshold: 5000ms). Consider caching, parallelization, or reducing scope.", ts.Tool, ts.AvgMs),
				Symptoms: []string{
					fmt.Sprintf("avg_latency=%dms", ts.AvgMs),
					fmt.Sprintf("calls=%d", ts.Calls),
				},
				Criteria: []string{
					fmt.Sprintf("reduce %s avg latency below 5000ms", ts.Tool),
					"no regression in output quality",
				},
			})
		}

		// High error rate: > 30%
		if ts.ErrorRate > 0.3 {
			proposals = append(proposals, common.Proposal{
				Type:   "tool",
				Name:   fmt.Sprintf("harden-%s", ts.Tool),
				Reason: fmt.Sprintf("Tool %q has error rate %.0f%% (%d/%d calls). Needs better error handling, input validation, or fallback.", ts.Tool, ts.ErrorRate*100, ts.Errors, ts.Calls),
				Symptoms: []string{
					fmt.Sprintf("error_rate=%.2f", ts.ErrorRate),
					fmt.Sprintf("errors=%d", ts.Errors),
				},
				Criteria: []string{
					fmt.Sprintf("reduce %s error rate below 30%%", ts.Tool),
					"add retry or fallback logic",
				},
			})
		}

		// Frequent tool: if called > 20 times, might need a playbook
		if ts.Calls > 20 {
			proposals = append(proposals, common.Proposal{
				Type:   "playbook",
				Name:   fmt.Sprintf("playbook-%s", ts.Tool),
				Reason: fmt.Sprintf("Tool %q called %d times in %s. Repeated usage pattern may benefit from a playbook.", ts.Tool, ts.Calls, diag.Period),
				Symptoms: []string{
					fmt.Sprintf("high_frequency=%d", ts.Calls),
				},
				Criteria: []string{
					"identify common call patterns",
					"create reusable playbook template",
				},
			})
		}
	}

	// Check for retry issues
	for _, issue := range diag.Issues {
		if strings.Contains(issue.Problem, "retry") {
			proposals = append(proposals, common.Proposal{
				Type:   "policy",
				Name:   "retry-policy",
				Reason: fmt.Sprintf("System has %d retry events. Consider smarter retry backoff or circuit-breaking.", issue.Count),
				Symptoms: []string{
					fmt.Sprintf("retry_count=%d", issue.Count),
				},
				Criteria: []string{
					"implement exponential backoff",
					"add circuit-breaker for repeated failures",
				},
			})
		}
	}

	// Check for slow LLM calls
	if diag.LLMStats.AvgMs > 10000 {
		proposals = append(proposals, common.Proposal{
			Type:   "workflow",
			Name:   "llm-optimization",
			Reason: fmt.Sprintf("LLM avg latency is %dms. Consider prompt compression, caching, or batching.", diag.LLMStats.AvgMs),
			Symptoms: []string{
				fmt.Sprintf("llm_avg_ms=%d", diag.LLMStats.AvgMs),
				fmt.Sprintf("llm_calls=%d", diag.LLMStats.Calls),
				fmt.Sprintf("est_tokens=%d", diag.LLMStats.EstTokens),
			},
			Criteria: []string{
				"reduce avg LLM latency below 10000ms",
				"maintain output quality",
			},
		})
	}

	// High token usage
	if diag.LLMStats.EstTokens > 100000 {
		proposals = append(proposals, common.Proposal{
			Type:   "policy",
			Name:   "token-budget",
			Reason: fmt.Sprintf("Estimated %d tokens used in %s. Consider tighter context management.", diag.LLMStats.EstTokens, diag.Period),
			Symptoms: []string{
				fmt.Sprintf("est_tokens=%d", diag.LLMStats.EstTokens),
			},
			Criteria: []string{
				"reduce token usage by 20%",
				"no loss in task completion rate",
			},
		})
	}

	return proposals
}

// goalBasedProposals generates proposals based on the stated goal.
func goalBasedProposals(goal string) []common.Proposal {
	if goal == "" {
		return nil
	}

	var proposals []common.Proposal
	goalLower := strings.ToLower(goal)

	if strings.Contains(goalLower, "retrieval") || strings.Contains(goalLower, "search") {
		proposals = append(proposals, common.Proposal{
			Type:   "tool",
			Name:   "improve-retrieval",
			Reason: fmt.Sprintf("Goal mentions retrieval/search: %q. Consider adding semantic indexing, result ranking, or caching.", goal),
			Symptoms: []string{
				"goal-driven: retrieval improvement requested",
			},
			Criteria: []string{
				"improve search precision",
				"reduce search latency",
			},
		})
	}

	if strings.Contains(goalLower, "accuracy") {
		proposals = append(proposals, common.Proposal{
			Type:   "workflow",
			Name:   "accuracy-feedback-loop",
			Reason: fmt.Sprintf("Goal mentions accuracy: %q. Consider adding verification steps or confidence scoring.", goal),
			Symptoms: []string{
				"goal-driven: accuracy improvement requested",
			},
			Criteria: []string{
				"add verification step to workflow",
				"track accuracy metrics over time",
			},
		})
	}

	if strings.Contains(goalLower, "speed") || strings.Contains(goalLower, "fast") || strings.Contains(goalLower, "performance") {
		proposals = append(proposals, common.Proposal{
			Type:   "tool",
			Name:   "performance-optimization",
			Reason: fmt.Sprintf("Goal mentions performance: %q. Consider parallelization, caching, or reducing I/O.", goal),
			Symptoms: []string{
				"goal-driven: performance improvement requested",
			},
			Criteria: []string{
				"reduce end-to-end latency",
				"benchmark before/after",
			},
		})
	}

	if strings.Contains(goalLower, "reliability") || strings.Contains(goalLower, "robust") {
		proposals = append(proposals, common.Proposal{
			Type:   "policy",
			Name:   "reliability-policy",
			Reason: fmt.Sprintf("Goal mentions reliability: %q. Consider adding retries, health checks, or graceful degradation.", goal),
			Symptoms: []string{
				"goal-driven: reliability improvement requested",
			},
			Criteria: []string{
				"reduce error rate below 5%",
				"add health check endpoint",
			},
		})
	}

	return proposals
}

// deduplicateProposals removes proposals with duplicate names, keeping the first.
func deduplicateProposals(proposals []common.Proposal) []common.Proposal {
	seen := map[string]bool{}
	var result []common.Proposal
	for _, p := range proposals {
		if !seen[p.Name] {
			seen[p.Name] = true
			result = append(result, p)
		}
	}
	return result
}
