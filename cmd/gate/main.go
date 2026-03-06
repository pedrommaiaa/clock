// Command gate is a promotion gatekeeper that validates whether a tool
// artifact meets all policy requirements before promotion.
package main

import (
	"fmt"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// GateInput is the input schema for the gate tool.
type GateInput struct {
	Artifact    string          `json:"artifact"`
	Spec        string          `json:"spec"`
	TestResult  *TestResult     `json:"test_result,omitempty"`
	BenchResult *BenchResult    `json:"bench_result,omitempty"`
	AuditResult *AuditResult    `json:"audit_result,omitempty"`
	Policy      GatePolicy      `json:"policy"`
}

// TestResult holds test execution results.
type TestResult struct {
	Passed bool `json:"passed"`
	Tests  int  `json:"tests"`
}

// BenchResult holds benchmark comparison results.
type BenchResult struct {
	Speedup float64 `json:"speedup"`
	Winner  string  `json:"winner"`
}

// AuditResult holds security audit results.
type AuditResult struct {
	Risk string `json:"risk"`
}

// GatePolicy defines the promotion requirements.
type GatePolicy struct {
	RequireTests    bool    `json:"require_tests"`
	RequireBench    bool    `json:"require_bench"`
	RequireAudit    bool    `json:"require_audit"`
	MaxRisk         string  `json:"max_risk"`
	MinSpeedup      float64 `json:"min_speedup"`
	RequireApproval bool    `json:"require_approval"`
}

// Check is a single gate check result.
type Check struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Details string `json:"details,omitempty"`
}

// GateResult is the output of the gate tool.
type GateResult struct {
	Approved bool     `json:"approved"`
	Requires string   `json:"requires,omitempty"`
	Checks   []Check  `json:"checks"`
	Blockers []string `json:"blockers,omitempty"`
}

// riskLevel returns a numeric level for risk comparison.
// low=0, med=1, high=2
func riskLevel(risk string) int {
	switch risk {
	case "low":
		return 0
	case "med":
		return 1
	case "high":
		return 2
	default:
		return 3 // unknown risks are treated as highest
	}
}

func main() {
	var input GateInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	result := GateResult{
		Approved: true,
	}

	// Check 1: Tests
	if input.Policy.RequireTests {
		if input.TestResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{
				Name: "tests", Pass: false, Details: "test_result missing",
			})
			result.Blockers = append(result.Blockers, "test_result is required but missing")
		} else if !input.TestResult.Passed {
			result.Approved = false
			result.Checks = append(result.Checks, Check{
				Name: "tests", Pass: false,
				Details: fmt.Sprintf("tests failed (%d tests)", input.TestResult.Tests),
			})
			result.Blockers = append(result.Blockers,
				fmt.Sprintf("tests failed (%d tests)", input.TestResult.Tests))
		} else {
			result.Checks = append(result.Checks, Check{
				Name: "tests", Pass: true,
				Details: fmt.Sprintf("%d tests passed", input.TestResult.Tests),
			})
		}
	}

	// Check 2: Benchmark
	if input.Policy.RequireBench {
		if input.BenchResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{
				Name: "benchmark", Pass: false, Details: "bench_result missing",
			})
			result.Blockers = append(result.Blockers, "bench_result is required but missing")
		} else {
			minSpeedup := input.Policy.MinSpeedup
			if minSpeedup <= 0 {
				minSpeedup = 0.9
			}
			if input.BenchResult.Speedup < minSpeedup {
				result.Approved = false
				result.Checks = append(result.Checks, Check{
					Name: "benchmark", Pass: false,
					Details: fmt.Sprintf("speedup %.2fx < %.2fx minimum", input.BenchResult.Speedup, minSpeedup),
				})
				result.Blockers = append(result.Blockers,
					fmt.Sprintf("benchmark regression: %.1fx < %.1fx minimum", input.BenchResult.Speedup, minSpeedup))
			} else {
				result.Checks = append(result.Checks, Check{
					Name: "benchmark", Pass: true,
					Details: fmt.Sprintf("speedup %.2fx >= %.2fx minimum", input.BenchResult.Speedup, minSpeedup),
				})
			}
		}
	}

	// Check 3: Audit risk
	if input.Policy.RequireAudit {
		if input.AuditResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{
				Name: "audit", Pass: false, Details: "audit_result missing",
			})
			result.Blockers = append(result.Blockers, "audit_result is required but missing")
		} else {
			maxRisk := input.Policy.MaxRisk
			if maxRisk == "" {
				maxRisk = "med"
			}
			actualLevel := riskLevel(input.AuditResult.Risk)
			maxLevel := riskLevel(maxRisk)
			if actualLevel > maxLevel {
				result.Approved = false
				result.Checks = append(result.Checks, Check{
					Name: "audit", Pass: false,
					Details: fmt.Sprintf("risk %q exceeds max %q", input.AuditResult.Risk, maxRisk),
				})
				result.Blockers = append(result.Blockers,
					fmt.Sprintf("audit risk %q exceeds maximum allowed %q", input.AuditResult.Risk, maxRisk))
			} else {
				result.Checks = append(result.Checks, Check{
					Name: "audit", Pass: true,
					Details: fmt.Sprintf("risk %q within max %q", input.AuditResult.Risk, maxRisk),
				})
			}
		}
	}

	// Check 4: Required fields
	if input.Artifact == "" {
		result.Approved = false
		result.Checks = append(result.Checks, Check{
			Name: "artifact", Pass: false, Details: "artifact hash missing",
		})
		result.Blockers = append(result.Blockers, "artifact hash is required")
	} else {
		result.Checks = append(result.Checks, Check{
			Name: "artifact", Pass: true, Details: input.Artifact,
		})
	}

	// If policy requires human approval, override
	if input.Policy.RequireApproval {
		result.Approved = false
		result.Requires = "human_approval"
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
