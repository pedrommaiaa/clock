// Command judge is an automated evaluation harness.
// It reads an evaluation config from stdin, runs tasks through the work tool,
// checks results against expected outcomes, and outputs scored results.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// JudgeInput is the input to the judge tool.
type JudgeInput struct {
	Suite  string      `json:"suite"`
	Config JudgeConfig `json:"config"`
}

// JudgeConfig holds provider/model settings.
type JudgeConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// BenchSuite is the benchmark suite file format.
type BenchSuite struct {
	Tasks []BenchTask `json:"tasks"`
}

// BenchTask is a single benchmark task.
type BenchTask struct {
	ID       string       `json:"id"`
	Repo     string       `json:"repo"`
	Goal     string       `json:"goal"`
	Expected TaskExpected `json:"expected"`
}

// TaskExpected defines what a successful task should produce.
type TaskExpected struct {
	Files     []string `json:"files"`
	TestsPass bool     `json:"tests_pass"`
}

// TaskResult is the scored result for a single task.
type TaskResult struct {
	ID        string  `json:"id"`
	Score     float64 `json:"score"`
	Passed    bool    `json:"passed"`
	LatencyMs int64   `json:"latency_ms"`
	LLMCalls  int     `json:"llm_calls"`
	Details   string  `json:"details"`
}

// JudgeOutput is the full judge output.
type JudgeOutput struct {
	Suite     string        `json:"suite"`
	Results   []TaskResult  `json:"results"`
	Aggregate AggregateInfo `json:"aggregate"`
}

// AggregateInfo holds summary statistics.
type AggregateInfo struct {
	MeanScore float64 `json:"mean_score"`
	PassRate  float64 `json:"pass_rate"`
}

func main() {
	var input JudgeInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Suite == "" {
		jsonutil.Fatal("suite path is required")
	}

	// Load suite file
	suiteData, err := os.ReadFile(input.Suite)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read suite %s: %v", input.Suite, err))
	}

	var suite BenchSuite
	if err := json.Unmarshal(suiteData, &suite); err != nil {
		jsonutil.Fatal(fmt.Sprintf("parse suite: %v", err))
	}

	if len(suite.Tasks) == 0 {
		jsonutil.Fatal("suite has no tasks")
	}

	// Run each task
	var results []TaskResult
	for _, task := range suite.Tasks {
		result := runTask(task, input.Config)
		results = append(results, result)
	}

	// Compute aggregates
	var totalScore float64
	var passCount int
	for _, r := range results {
		totalScore += r.Score
		if r.Passed {
			passCount++
		}
	}
	meanScore := 0.0
	if len(results) > 0 {
		meanScore = totalScore / float64(len(results))
	}
	meanScore = roundTo(meanScore, 2)
	passRate := 0.0
	if len(results) > 0 {
		passRate = float64(passCount) / float64(len(results))
	}
	passRate = roundTo(passRate, 2)

	output := JudgeOutput{
		Suite:   input.Suite,
		Results: results,
		Aggregate: AggregateInfo{
			MeanScore: meanScore,
			PassRate:  passRate,
		},
	}

	// Output JSON to stdout
	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}

	// Output Markdown report to stderr
	writeMarkdownReport(output)
}

func runTask(task BenchTask, config JudgeConfig) TaskResult {
	result := TaskResult{
		ID: task.ID,
	}

	// Build job spec
	job := common.JobSpec{
		ID:   task.ID,
		Goal: task.Goal,
		Repo: task.Repo,
	}
	jobData, err := json.Marshal(job)
	if err != nil {
		result.Details = fmt.Sprintf("marshal job: %v", err)
		return result
	}

	// Set up environment for provider/model
	env := os.Environ()
	if config.Provider != "" {
		env = append(env, fmt.Sprintf("CLOCK_PROVIDER=%s", config.Provider))
	}
	if config.Model != "" {
		env = append(env, fmt.Sprintf("CLOCK_MODEL=%s", config.Model))
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
		result.Details = fmt.Sprintf("work failed: %v (stderr: %s)", err, truncate(stderr.String(), 500))
		result.Score = 0.0
		result.Passed = false
		return result
	}

	// Parse work output
	var jobResult common.JobResult
	if err := json.Unmarshal(stdout.Bytes(), &jobResult); err != nil {
		result.Details = fmt.Sprintf("parse work output: %v", err)
		result.Score = 0.0
		result.Passed = false
		return result
	}

	// Count LLM calls from trace file
	result.LLMCalls = countLLMCalls(task.ID)

	// Score the result
	score := scoreTask(task, jobResult, result.LatencyMs)
	result.Score = score
	result.Passed = jobResult.OK && checkTestsPass(task, jobResult) && checkFilesModified(task, jobResult)

	// Build details
	var details []string
	details = append(details, fmt.Sprintf("ok=%v", jobResult.OK))
	if jobResult.Verify != nil {
		details = append(details, fmt.Sprintf("tests_ok=%v", jobResult.Verify.OK))
	}
	if jobResult.Diff != "" {
		diffLines := strings.Count(jobResult.Diff, "\n")
		details = append(details, fmt.Sprintf("diff_lines=%d", diffLines))
	}
	if jobResult.Err != "" {
		details = append(details, fmt.Sprintf("err=%s", truncate(jobResult.Err, 200)))
	}
	result.Details = strings.Join(details, "; ")

	return result
}

func scoreTask(task BenchTask, jobResult common.JobResult, latencyMs int64) float64 {
	score := 0.0

	// Correctness (0.5 weight): did tests pass and expected files modified?
	correctness := 0.0
	if jobResult.OK {
		correctness += 0.5
	}
	if checkTestsPass(task, jobResult) {
		correctness += 0.5
	}
	if checkFilesModified(task, jobResult) {
		correctness += 0.0 // bonus already included in pass check
	} else if len(task.Expected.Files) > 0 {
		correctness -= 0.2
	}
	if correctness > 1.0 {
		correctness = 1.0
	}
	if correctness < 0.0 {
		correctness = 0.0
	}
	score += correctness * 0.5

	// Minimality (0.3 weight): smaller diff is better
	diffLines := 0
	if jobResult.Diff != "" {
		diffLines = strings.Count(jobResult.Diff, "\n")
	}
	minimality := 1.0
	if diffLines > 100 {
		minimality = 100.0 / float64(diffLines)
	}
	score += minimality * 0.3

	// Speed (0.2 weight): faster is better (baseline 60s)
	speed := 1.0
	if latencyMs > 60000 {
		speed = 60000.0 / float64(latencyMs)
	}
	score += speed * 0.2

	return roundTo(score, 2)
}

func checkTestsPass(task BenchTask, jobResult common.JobResult) bool {
	if !task.Expected.TestsPass {
		return true // not required
	}
	if jobResult.Verify == nil {
		return false
	}
	return jobResult.Verify.OK
}

func checkFilesModified(task BenchTask, jobResult common.JobResult) bool {
	if len(task.Expected.Files) == 0 {
		return true // no expected files
	}
	if jobResult.Diff == "" {
		return false
	}
	for _, expected := range task.Expected.Files {
		if !strings.Contains(jobResult.Diff, expected) {
			return false
		}
	}
	return true
}

func countLLMCalls(taskID string) int {
	// Try to read trace file and count LLM calls for this task
	tracePath := ".clock/trce.jsonl"
	data, err := os.ReadFile(tracePath)
	if err != nil {
		return 0
	}

	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev common.TraceEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Event == "llm.out" || ev.Event == "llm.in" {
			// Count LLM interactions - each llm.out is one call
			if ev.Event == "llm.out" {
				count++
			}
		}
	}
	return count
}

func writeMarkdownReport(output JudgeOutput) {
	fmt.Fprintf(os.Stderr, "\n# Judge Report: %s\n\n", output.Suite)
	fmt.Fprintf(os.Stderr, "| Task | Score | Passed | Latency | LLM Calls | Details |\n")
	fmt.Fprintf(os.Stderr, "|------|-------|--------|---------|-----------|--------|\n")

	for _, r := range output.Results {
		passedStr := "no"
		if r.Passed {
			passedStr = "yes"
		}
		fmt.Fprintf(os.Stderr, "| %s | %.2f | %s | %dms | %d | %s |\n",
			r.ID, r.Score, passedStr, r.LatencyMs, r.LLMCalls, truncate(r.Details, 60))
	}

	fmt.Fprintf(os.Stderr, "\n**Aggregate:** mean_score=%.2f pass_rate=%.2f\n\n",
		output.Aggregate.MeanScore, output.Aggregate.PassRate)
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

func roundTo(v float64, decimals int) float64 {
	pow := 1.0
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int(v*pow+0.5)) / pow
}
