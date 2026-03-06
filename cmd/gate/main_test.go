package main

import (
	"testing"
)

func TestRiskLevel(t *testing.T) {
	tests := []struct {
		risk string
		want int
	}{
		{"low", 0},
		{"med", 1},
		{"high", 2},
		{"unknown", 3},
		{"", 3},
	}

	for _, tt := range tests {
		t.Run(tt.risk, func(t *testing.T) {
			got := riskLevel(tt.risk)
			if got != tt.want {
				t.Errorf("riskLevel(%q) = %d, want %d", tt.risk, got, tt.want)
			}
		})
	}
}

func TestGate_AllPassing(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		Spec:     "srch",
		TestResult: &TestResult{
			Passed: true,
			Tests:  10,
		},
		BenchResult: &BenchResult{
			Speedup: 1.5,
			Winner:  "candidate",
		},
		AuditResult: &AuditResult{
			Risk: "low",
		},
		Policy: GatePolicy{
			RequireTests: true,
			RequireBench: true,
			RequireAudit: true,
			MaxRisk:      "med",
			MinSpeedup:   0.9,
		},
	}

	result := evaluateGate(input)
	if !result.Approved {
		t.Errorf("expected approved=true, got false. Blockers: %v", result.Blockers)
	}
}

func TestGate_TestsFailed(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		TestResult: &TestResult{
			Passed: false,
			Tests:  10,
		},
		Policy: GatePolicy{
			RequireTests: true,
		},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when tests failed")
	}
	if len(result.Blockers) == 0 {
		t.Error("expected at least one blocker")
	}
}

func TestGate_TestsMissing(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		Policy: GatePolicy{
			RequireTests: true,
		},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when test_result is missing")
	}
}

func TestGate_BenchBelowMinSpeedup(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		BenchResult: &BenchResult{
			Speedup: 0.5,
			Winner:  "baseline",
		},
		Policy: GatePolicy{
			RequireBench: true,
			MinSpeedup:   0.9,
		},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when speedup below minimum")
	}
}

func TestGate_BenchDefaultMinSpeedup(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		BenchResult: &BenchResult{
			Speedup: 0.95,
			Winner:  "candidate",
		},
		Policy: GatePolicy{
			RequireBench: true,
			// MinSpeedup=0 -> default 0.9
		},
	}

	result := evaluateGate(input)
	if !result.Approved {
		t.Errorf("expected approved=true (0.95 >= default 0.9), blockers: %v", result.Blockers)
	}
}

func TestGate_AuditExceedsMaxRisk(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		AuditResult: &AuditResult{
			Risk: "high",
		},
		Policy: GatePolicy{
			RequireAudit: true,
			MaxRisk:      "med",
		},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when risk exceeds max")
	}
}

func TestGate_AuditWithinMaxRisk(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		AuditResult: &AuditResult{
			Risk: "low",
		},
		Policy: GatePolicy{
			RequireAudit: true,
			MaxRisk:      "med",
		},
	}

	result := evaluateGate(input)
	if !result.Approved {
		t.Errorf("expected approved=true (low within med), blockers: %v", result.Blockers)
	}
}

func TestGate_MissingArtifact(t *testing.T) {
	input := GateInput{
		Artifact: "",
		Policy:   GatePolicy{},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when artifact is empty")
	}
}

func TestGate_RequireApproval(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		Policy: GatePolicy{
			RequireApproval: true,
		},
	}

	result := evaluateGate(input)
	if result.Approved {
		t.Error("expected approved=false when human approval required")
	}
	if result.Requires != "human_approval" {
		t.Errorf("expected requires=human_approval, got %q", result.Requires)
	}
}

func TestGate_NoPolicyRequirements(t *testing.T) {
	input := GateInput{
		Artifact: "sha256:abc123",
		Policy:   GatePolicy{},
	}

	result := evaluateGate(input)
	if !result.Approved {
		t.Errorf("expected approved=true with no policy requirements, blockers: %v", result.Blockers)
	}
}

// evaluateGate is a helper that replicates the main() gate logic
// for testing without stdin/stdout I/O.
func evaluateGate(input GateInput) GateResult {
	result := GateResult{
		Approved: true,
	}

	if input.Policy.RequireTests {
		if input.TestResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{Name: "tests", Pass: false, Details: "test_result missing"})
			result.Blockers = append(result.Blockers, "test_result is required but missing")
		} else if !input.TestResult.Passed {
			result.Approved = false
			result.Checks = append(result.Checks, Check{Name: "tests", Pass: false})
			result.Blockers = append(result.Blockers, "tests failed")
		} else {
			result.Checks = append(result.Checks, Check{Name: "tests", Pass: true})
		}
	}

	if input.Policy.RequireBench {
		if input.BenchResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{Name: "benchmark", Pass: false})
			result.Blockers = append(result.Blockers, "bench_result is required but missing")
		} else {
			minSpeedup := input.Policy.MinSpeedup
			if minSpeedup <= 0 {
				minSpeedup = 0.9
			}
			if input.BenchResult.Speedup < minSpeedup {
				result.Approved = false
				result.Checks = append(result.Checks, Check{Name: "benchmark", Pass: false})
				result.Blockers = append(result.Blockers, "benchmark regression")
			} else {
				result.Checks = append(result.Checks, Check{Name: "benchmark", Pass: true})
			}
		}
	}

	if input.Policy.RequireAudit {
		if input.AuditResult == nil {
			result.Approved = false
			result.Checks = append(result.Checks, Check{Name: "audit", Pass: false})
			result.Blockers = append(result.Blockers, "audit_result is required but missing")
		} else {
			maxRisk := input.Policy.MaxRisk
			if maxRisk == "" {
				maxRisk = "med"
			}
			if riskLevel(input.AuditResult.Risk) > riskLevel(maxRisk) {
				result.Approved = false
				result.Checks = append(result.Checks, Check{Name: "audit", Pass: false})
				result.Blockers = append(result.Blockers, "audit risk exceeds maximum")
			} else {
				result.Checks = append(result.Checks, Check{Name: "audit", Pass: true})
			}
		}
	}

	if input.Artifact == "" {
		result.Approved = false
		result.Checks = append(result.Checks, Check{Name: "artifact", Pass: false})
		result.Blockers = append(result.Blockers, "artifact hash is required")
	} else {
		result.Checks = append(result.Checks, Check{Name: "artifact", Pass: true})
	}

	if input.Policy.RequireApproval {
		result.Approved = false
		result.Requires = "human_approval"
	}

	return result
}
