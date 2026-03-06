package main

import (
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestAnalyzeDiagnostics_HighLatency(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		ToolStats: []ToolStat{
			{Tool: "srch", Calls: 10, AvgMs: 8000, Errors: 0, ErrorRate: 0},
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "optimize-srch" && p.Type == "tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected optimize-srch proposal for high latency tool")
	}
}

func TestAnalyzeDiagnostics_HighErrorRate(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		ToolStats: []ToolStat{
			{Tool: "slce", Calls: 10, AvgMs: 100, Errors: 5, ErrorRate: 0.5},
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "harden-slce" && p.Type == "tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected harden-slce proposal for high error rate tool")
	}
}

func TestAnalyzeDiagnostics_HighFrequency(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		ToolStats: []ToolStat{
			{Tool: "srch", Calls: 25, AvgMs: 100, Errors: 0, ErrorRate: 0},
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "playbook-srch" && p.Type == "playbook" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected playbook-srch proposal for frequently called tool")
	}
}

func TestAnalyzeDiagnostics_RetryIssues(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		Issues: []DiagIssue{
			{Tool: "system", Problem: "retry events detected: 5", Count: 5},
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "retry-policy" && p.Type == "policy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected retry-policy proposal for retry issues")
	}
}

func TestAnalyzeDiagnostics_SlowLLM(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		LLMStats: LLMStat{
			Calls:     10,
			EstTokens: 5000,
			AvgMs:     15000,
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "llm-optimization" && p.Type == "workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected llm-optimization proposal for slow LLM")
	}
}

func TestAnalyzeDiagnostics_HighTokenUsage(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		LLMStats: LLMStat{
			Calls:     10,
			EstTokens: 200000,
			AvgMs:     1000,
		},
	}

	proposals := analyzeDiagnostics(diag)
	found := false
	for _, p := range proposals {
		if p.Name == "token-budget" && p.Type == "policy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected token-budget proposal for high token usage")
	}
}

func TestAnalyzeDiagnostics_NoIssues(t *testing.T) {
	diag := &DiagOutput{
		Period: "24h",
		ToolStats: []ToolStat{
			{Tool: "srch", Calls: 5, AvgMs: 100, Errors: 0, ErrorRate: 0},
		},
		LLMStats: LLMStat{Calls: 2, EstTokens: 100, AvgMs: 500},
	}

	proposals := analyzeDiagnostics(diag)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for healthy system, got %d", len(proposals))
	}
}

func TestGoalBasedProposals(t *testing.T) {
	tests := []struct {
		goal     string
		wantName string
		wantType string
	}{
		{"improve retrieval quality", "improve-retrieval", "tool"},
		{"search indexing", "improve-retrieval", "tool"},
		{"improve accuracy", "accuracy-feedback-loop", "workflow"},
		{"make it faster", "performance-optimization", "tool"},
		{"speed up processing", "performance-optimization", "tool"},
		{"improve performance", "performance-optimization", "tool"},
		{"increase reliability", "reliability-policy", "policy"},
		{"make it robust", "reliability-policy", "policy"},
	}

	for _, tt := range tests {
		t.Run(tt.goal, func(t *testing.T) {
			proposals := goalBasedProposals(tt.goal)
			found := false
			for _, p := range proposals {
				if p.Name == tt.wantName && p.Type == tt.wantType {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("goalBasedProposals(%q): expected proposal %q (type %q)", tt.goal, tt.wantName, tt.wantType)
			}
		})
	}
}

func TestGoalBasedProposals_EmptyGoal(t *testing.T) {
	proposals := goalBasedProposals("")
	if proposals != nil {
		t.Errorf("expected nil proposals for empty goal, got %v", proposals)
	}
}

func TestGoalBasedProposals_UnrelatedGoal(t *testing.T) {
	proposals := goalBasedProposals("celebrate a birthday")
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for unrelated goal, got %d", len(proposals))
	}
}

func TestDeduplicateProposals(t *testing.T) {
	proposals := []common.Proposal{
		{Name: "optimize-srch", Type: "tool", Reason: "first"},
		{Name: "optimize-srch", Type: "tool", Reason: "second"},
		{Name: "harden-slce", Type: "tool", Reason: "first"},
	}

	result := deduplicateProposals(proposals)
	if len(result) != 2 {
		t.Errorf("expected 2 deduplicated proposals, got %d", len(result))
	}
	if result[0].Reason != "first" {
		t.Error("expected first occurrence to be kept")
	}
}

func TestDeduplicateProposals_Empty(t *testing.T) {
	result := deduplicateProposals(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestDeduplicateProposals_NoDuplicates(t *testing.T) {
	proposals := []common.Proposal{
		{Name: "a", Type: "tool"},
		{Name: "b", Type: "policy"},
	}
	result := deduplicateProposals(proposals)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}
