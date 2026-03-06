// Command bench is a performance benchmark tool that compares a candidate
// binary against a baseline, measuring latency and output size.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// BenchInput is the input schema for the bench tool.
type BenchInput struct {
	Candidate string          `json:"candidate"`
	Baseline  string          `json:"baseline"`
	TestInput json.RawMessage `json:"test_input"`
	Runs      int             `json:"runs"`
}

// BenchStats holds timing and output stats for one tool.
type BenchStats struct {
	Name        string  `json:"name"`
	MeanMs      float64 `json:"mean_ms"`
	MinMs       float64 `json:"min_ms"`
	MaxMs       float64 `json:"max_ms"`
	P50Ms       float64 `json:"p50_ms"`
	OutputBytes int     `json:"output_bytes"`
}

// BenchResult is the output of the bench tool.
type BenchResult struct {
	Baseline  BenchStats `json:"baseline"`
	Candidate BenchStats `json:"candidate"`
	Speedup   float64    `json:"speedup"`
	Winner    string     `json:"winner"`
	Runs      int        `json:"runs"`
}

func main() {
	var input BenchInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Runs <= 0 {
		input.Runs = 5
	}
	if input.Candidate == "" {
		jsonutil.Fatal("candidate binary path is required")
	}
	if input.Baseline == "" {
		input.Baseline = "srch"
	}

	inputData, err := json.Marshal(input.TestInput)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal test_input: %v", err))
	}

	baselineStats, err := runBenchmark(input.Baseline, inputData, input.Runs)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("baseline benchmark failed: %v", err))
	}

	candidateStats, err := runBenchmark(input.Candidate, inputData, input.Runs)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("candidate benchmark failed: %v", err))
	}

	// Calculate speedup
	var speedup float64
	if candidateStats.MeanMs > 0 {
		speedup = baselineStats.MeanMs / candidateStats.MeanMs
	} else if baselineStats.MeanMs > 0 {
		speedup = math.Inf(1)
	} else {
		speedup = 1.0
	}
	speedup = math.Round(speedup*100) / 100

	winner := "tie"
	if speedup > 1.05 {
		winner = "candidate"
	} else if speedup < 0.95 {
		winner = "baseline"
	}

	result := BenchResult{
		Baseline:  *baselineStats,
		Candidate: *candidateStats,
		Speedup:   speedup,
		Winner:    winner,
		Runs:      input.Runs,
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// runBenchmark executes a binary `runs` times with the given input and collects timing stats.
func runBenchmark(binary string, inputData []byte, runs int) (*BenchStats, error) {
	durations := make([]float64, 0, runs)
	var lastOutputSize int

	for i := 0; i < runs; i++ {
		cmd := exec.Command(binary)
		cmd.Stdin = bytes.NewReader(inputData)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		start := time.Now()
		err := cmd.Run()
		elapsed := time.Since(start)

		if err != nil {
			// If the binary doesn't exist or can't run, try with PATH lookup
			if pathErr, ok := err.(*exec.Error); ok {
				return nil, fmt.Errorf("cannot execute %q: %v", binary, pathErr)
			}
			// Non-zero exit is acceptable for benchmarking, we still measure time
			fmt.Fprintf(os.Stderr, "warning: %s run %d exited with error: %v\n", binary, i+1, err)
		}

		ms := float64(elapsed.Nanoseconds()) / 1e6
		durations = append(durations, ms)
		lastOutputSize = stdout.Len()
	}

	stats := computeStats(durations)
	stats.Name = binary
	stats.OutputBytes = lastOutputSize

	return stats, nil
}

// computeStats calculates mean, min, max, and p50 from a slice of durations.
func computeStats(durations []float64) *BenchStats {
	if len(durations) == 0 {
		return &BenchStats{}
	}

	sorted := make([]float64, len(durations))
	copy(sorted, durations)
	sort.Float64s(sorted)

	var sum float64
	for _, d := range sorted {
		sum += d
	}

	mean := sum / float64(len(sorted))
	minVal := sorted[0]
	maxVal := sorted[len(sorted)-1]

	// P50 (median)
	var p50 float64
	n := len(sorted)
	if n%2 == 0 {
		p50 = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		p50 = sorted[n/2]
	}

	return &BenchStats{
		MeanMs: math.Round(mean*100) / 100,
		MinMs:  math.Round(minVal*100) / 100,
		MaxMs:  math.Round(maxVal*100) / 100,
		P50Ms:  math.Round(p50*100) / 100,
	}
}
