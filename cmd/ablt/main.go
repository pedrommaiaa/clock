// Command ablt is an A/B and ablation runner.
// It reads a task and variant configs from stdin, runs each variant through
// the work tool, and compares results to determine a winner.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// AbltInput is the input to the ablt tool.
type AbltInput struct {
	Task     AbltTask     `json:"task"`
	Variants []AbltVariant `json:"variants"`
}

// AbltTask describes the task to run.
type AbltTask struct {
	Goal string `json:"goal"`
	Repo string `json:"repo"`
}

// AbltVariant is a named configuration variant.
type AbltVariant struct {
	Name   string       `json:"name"`
	Config VariantConfig `json:"config"`
}

// VariantConfig holds provider/model configuration for a variant.
type VariantConfig struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
}

// VariantResult holds the outcome for a single variant run.
type VariantResult struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	LatencyMs int64  `json:"latency_ms"`
	Tokens    int    `json:"tokens"`
	DiffLines int    `json:"diff_lines"`
}

// AbltOutput is the full output of the ablt tool.
type AbltOutput struct {
	Task       string          `json:"task"`
	Variants   []VariantResult `json:"variants"`
	Winner     string          `json:"winner"`
	Reason     string          `json:"reason"`
	Confidence float64         `json:"confidence"`
}

func main() {
	var input AbltInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Task.Goal == "" {
		jsonutil.Fatal("task goal is required")
	}
	if len(input.Variants) < 2 {
		jsonutil.Fatal("at least 2 variants required")
	}

	// Run each variant
	var results []VariantResult
	for _, variant := range input.Variants {
		result := runVariant(input.Task, variant)
		results = append(results, result)
	}

	// Determine winner
	winner, reason, confidence := compareVariants(results)

	output := AbltOutput{
		Task:       input.Task.Goal,
		Variants:   results,
		Winner:     winner,
		Reason:     reason,
		Confidence: confidence,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func runVariant(task AbltTask, variant AbltVariant) VariantResult {
	result := VariantResult{
		Name: variant.Name,
	}

	// Build job spec
	job := common.JobSpec{
		ID:   fmt.Sprintf("ablt-%s-%d", variant.Name, time.Now().UnixMilli()),
		Goal: task.Goal,
		Repo: task.Repo,
	}
	jobData, err := json.Marshal(job)
	if err != nil {
		result.Passed = false
		return result
	}

	// Set up environment for variant config
	env := os.Environ()
	if variant.Config.Provider != "" {
		env = append(env, fmt.Sprintf("CLOCK_PROVIDER=%s", variant.Config.Provider))
	}
	if variant.Config.Model != "" {
		env = append(env, fmt.Sprintf("CLOCK_MODEL=%s", variant.Config.Model))
	}

	// Run work tool
	start := time.Now()
	workPath := findTool("work")
	cmd := exec.Command(workPath)
	cmd.Stdin = bytes.NewReader(jobData)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Passed = false
		return result
	}

	// Parse work output
	var jobResult common.JobResult
	if err := json.Unmarshal(stdout.Bytes(), &jobResult); err != nil {
		result.Passed = false
		return result
	}

	result.Passed = jobResult.OK
	if jobResult.Verify != nil {
		result.Passed = result.Passed && jobResult.Verify.OK
	}

	// Count diff lines
	if jobResult.Diff != "" {
		result.DiffLines = strings.Count(jobResult.Diff, "\n")
	}

	// Estimate tokens from trace
	result.Tokens = estimateTokens(job.ID)

	return result
}

func estimateTokens(jobID string) int {
	// Read trace and sum up token-related data
	tracePath := ".clock/trce.jsonl"
	data, err := os.ReadFile(tracePath)
	if err != nil {
		return 0
	}

	tokens := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev common.TraceEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Event == "llm.out" {
			// Try to extract token count from data
			if dataMap, ok := ev.Data.(map[string]interface{}); ok {
				if t, ok := dataMap["tokens"].(float64); ok {
					tokens += int(t)
				} else {
					// Estimate from response size: rough ~4 chars per token
					if out, ok := dataMap["output"].(string); ok {
						tokens += len(out) / 4
					} else {
						// Default estimate per call
						tokens += 1000
					}
				}
			} else {
				tokens += 1000 // default estimate per LLM call
			}
		}
	}
	return tokens
}

func compareVariants(results []VariantResult) (winner, reason string, confidence float64) {
	if len(results) == 0 {
		return "", "no results", 0.0
	}

	// Score each variant
	type scored struct {
		name  string
		score float64
		idx   int
	}

	var scoredResults []scored
	for i, r := range results {
		s := 0.0

		// Passing is most important (0.5 weight)
		if r.Passed {
			s += 0.5
		}

		// Speed matters (0.25 weight) - normalize against fastest
		// Will be adjusted after finding min latency
		scoredResults = append(scoredResults, scored{name: r.Name, score: s, idx: i})
	}

	// Normalize latency scores
	minLatency := int64(math.MaxInt64)
	for _, r := range results {
		if r.LatencyMs > 0 && r.LatencyMs < minLatency {
			minLatency = r.LatencyMs
		}
	}
	if minLatency == 0 {
		minLatency = 1
	}

	for i, r := range results {
		if r.LatencyMs > 0 {
			speedRatio := float64(minLatency) / float64(r.LatencyMs)
			scoredResults[i].score += speedRatio * 0.25
		}
	}

	// Normalize diff size scores (smaller is better, 0.25 weight)
	minDiff := math.MaxInt64
	for _, r := range results {
		if r.DiffLines > 0 && r.DiffLines < minDiff {
			minDiff = r.DiffLines
		}
	}
	if minDiff == 0 || minDiff == math.MaxInt64 {
		minDiff = 1
	}

	for i, r := range results {
		if r.DiffLines > 0 {
			diffRatio := float64(minDiff) / float64(r.DiffLines)
			scoredResults[i].score += diffRatio * 0.25
		} else if r.Passed {
			// No diff but passed: perfect minimality
			scoredResults[i].score += 0.25
		}
	}

	// Find winner
	bestIdx := 0
	for i := 1; i < len(scoredResults); i++ {
		if scoredResults[i].score > scoredResults[bestIdx].score {
			bestIdx = i
		}
	}

	winner = scoredResults[bestIdx].name

	// Determine reason
	w := results[bestIdx]
	var reasons []string

	// Compare winner against each other variant
	allPassed := true
	winnerOnlyPassed := false
	for i, r := range results {
		if i == bestIdx {
			continue
		}
		if !r.Passed && w.Passed {
			winnerOnlyPassed = true
		}
		if !r.Passed {
			allPassed = false
		}
	}

	if winnerOnlyPassed {
		reasons = append(reasons, "only variant that passed")
	} else if allPassed || w.Passed {
		// Check speed
		fasterThanAll := true
		for i, r := range results {
			if i == bestIdx {
				continue
			}
			if r.LatencyMs <= w.LatencyMs {
				fasterThanAll = false
			}
		}
		if fasterThanAll {
			reasons = append(reasons, "faster")
		}

		// Check diff size
		smallerThanAll := true
		for i, r := range results {
			if i == bestIdx {
				continue
			}
			if r.DiffLines <= w.DiffLines && r.DiffLines > 0 {
				smallerThanAll = false
			}
		}
		if smallerThanAll && w.DiffLines > 0 {
			reasons = append(reasons, "smaller diff")
		}

		if len(reasons) == 0 {
			reasons = append(reasons, "best overall score")
		}
	}

	if !w.Passed {
		reasons = append(reasons, "no variant passed")
	}

	reason = strings.Join(reasons, " with ")
	if reason == "" {
		reason = "marginal difference"
	}

	// Confidence: how far apart are scores?
	if len(scoredResults) >= 2 {
		// Sort scores descending
		bestScore := scoredResults[bestIdx].score
		secondBest := 0.0
		for i, s := range scoredResults {
			if i != bestIdx && s.score > secondBest {
				secondBest = s.score
			}
		}
		gap := bestScore - secondBest
		// Confidence scales with gap
		confidence = math.Min(gap*2.0+0.5, 1.0)
		if !w.Passed {
			confidence = math.Min(confidence, 0.3)
		}
	} else {
		confidence = 0.5
	}

	confidence = math.Round(confidence*100) / 100
	return winner, reason, confidence
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
