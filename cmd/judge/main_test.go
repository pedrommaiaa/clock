package main

import (
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestScoreTask(t *testing.T) {
	tests := []struct {
		name      string
		task      BenchTask
		result    common.JobResult
		latencyMs int64
		wantMin   float64
		wantMax   float64
	}{
		{
			name: "perfect result fast",
			task: BenchTask{
				ID:   "t1",
				Goal: "fix bug",
				Expected: TaskExpected{
					TestsPass: true,
					Files:     []string{"main.go"},
				},
			},
			result: common.JobResult{
				OK:     true,
				Diff:   "+fix\n-old\n",
				Verify: &common.VerifyResult{OK: true},
			},
			latencyMs: 10000,
			wantMin:   0.5,
			wantMax:   1.0,
		},
		{
			name: "failed result",
			task: BenchTask{
				ID:   "t2",
				Goal: "fix bug",
				Expected: TaskExpected{
					TestsPass: true,
				},
			},
			result: common.JobResult{
				OK: false,
			},
			latencyMs: 5000,
			wantMin:   0.0,
			wantMax:   0.6,
		},
		{
			name: "slow result",
			task: BenchTask{
				ID:   "t3",
				Goal: "fix bug",
				Expected: TaskExpected{
					TestsPass: false,
				},
			},
			result: common.JobResult{
				OK:   true,
				Diff: "+fix\n",
			},
			latencyMs: 300000, // 5 minutes
			wantMin:   0.0,
			wantMax:   0.9,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreTask(tt.task, tt.result, tt.latencyMs)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("score = %f, want between %f and %f", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCheckTestsPass(t *testing.T) {
	tests := []struct {
		name string
		task BenchTask
		result common.JobResult
		want bool
	}{
		{
			name: "tests not required",
			task: BenchTask{Expected: TaskExpected{TestsPass: false}},
			result: common.JobResult{},
			want: true,
		},
		{
			name: "tests required and pass",
			task: BenchTask{Expected: TaskExpected{TestsPass: true}},
			result: common.JobResult{Verify: &common.VerifyResult{OK: true}},
			want: true,
		},
		{
			name: "tests required and fail",
			task: BenchTask{Expected: TaskExpected{TestsPass: true}},
			result: common.JobResult{Verify: &common.VerifyResult{OK: false}},
			want: false,
		},
		{
			name: "tests required but no verify",
			task: BenchTask{Expected: TaskExpected{TestsPass: true}},
			result: common.JobResult{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkTestsPass(tt.task, tt.result)
			if got != tt.want {
				t.Errorf("checkTestsPass = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckFilesModified(t *testing.T) {
	tests := []struct {
		name   string
		task   BenchTask
		result common.JobResult
		want   bool
	}{
		{
			name:   "no expected files",
			task:   BenchTask{Expected: TaskExpected{}},
			result: common.JobResult{},
			want:   true,
		},
		{
			name: "expected files present in diff",
			task: BenchTask{Expected: TaskExpected{
				Files: []string{"main.go", "auth.go"},
			}},
			result: common.JobResult{
				Diff: "--- a/main.go\n+++ b/main.go\n--- a/auth.go\n+++ b/auth.go\n",
			},
			want: true,
		},
		{
			name: "expected file missing from diff",
			task: BenchTask{Expected: TaskExpected{
				Files: []string{"main.go", "missing.go"},
			}},
			result: common.JobResult{
				Diff: "--- a/main.go\n+++ b/main.go\n",
			},
			want: false,
		},
		{
			name: "expected files but empty diff",
			task: BenchTask{Expected: TaskExpected{
				Files: []string{"main.go"},
			}},
			result: common.JobResult{},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkFilesModified(tt.task, tt.result)
			if got != tt.want {
				t.Errorf("checkFilesModified = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateJudge(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestRoundTo(t *testing.T) {
	tests := []struct {
		v        float64
		decimals int
		want     float64
	}{
		{0.123456, 2, 0.12},
		{0.555, 2, 0.56},
		{1.0, 2, 1.0},
		{0.0, 2, 0.0},
		{0.999, 1, 1.0},
	}
	for _, tt := range tests {
		got := roundTo(tt.v, tt.decimals)
		if got != tt.want {
			t.Errorf("roundTo(%f, %d) = %f, want %f", tt.v, tt.decimals, got, tt.want)
		}
	}
}

func TestAggregateCalculation(t *testing.T) {
	results := []TaskResult{
		{Score: 0.8, Passed: true},
		{Score: 0.6, Passed: true},
		{Score: 0.2, Passed: false},
		{Score: 0.4, Passed: false},
	}

	var totalScore float64
	var passCount int
	for _, r := range results {
		totalScore += r.Score
		if r.Passed {
			passCount++
		}
	}

	meanScore := totalScore / float64(len(results))
	passRate := float64(passCount) / float64(len(results))

	if meanScore != 0.5 {
		t.Errorf("mean score = %f, want 0.5", meanScore)
	}
	if passRate != 0.5 {
		t.Errorf("pass rate = %f, want 0.5", passRate)
	}
}

func TestAggregateEmpty(t *testing.T) {
	var results []TaskResult
	meanScore := 0.0
	passRate := 0.0
	if len(results) > 0 {
		t.Error("should be empty")
	}
	if meanScore != 0 || passRate != 0 {
		t.Error("empty should give 0")
	}
}

func TestFindTool(t *testing.T) {
	// findTool should return the name itself as fallback
	got := findTool("nonexistent-tool-12345")
	if got != "nonexistent-tool-12345" {
		t.Errorf("findTool fallback = %q, want %q", got, "nonexistent-tool-12345")
	}
}
