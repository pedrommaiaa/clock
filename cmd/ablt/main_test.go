package main

import (
	"math"
	"testing"
)

func TestCompareVariantsEmpty(t *testing.T) {
	winner, reason, confidence := compareVariants(nil)
	if winner != "" {
		t.Errorf("winner = %q, want empty", winner)
	}
	if reason != "no results" {
		t.Errorf("reason = %q, want %q", reason, "no results")
	}
	if confidence != 0.0 {
		t.Errorf("confidence = %f, want 0.0", confidence)
	}
}

func TestCompareVariantsOnePasses(t *testing.T) {
	results := []VariantResult{
		{Name: "a", Passed: true, LatencyMs: 5000, DiffLines: 10},
		{Name: "b", Passed: false, LatencyMs: 3000, DiffLines: 5},
	}

	winner, reason, _ := compareVariants(results)
	if winner != "a" {
		t.Errorf("winner = %q, want %q (only one passed)", winner, "a")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestCompareVariantsBothPass(t *testing.T) {
	results := []VariantResult{
		{Name: "fast", Passed: true, LatencyMs: 1000, DiffLines: 20},
		{Name: "slow", Passed: true, LatencyMs: 10000, DiffLines: 20},
	}

	winner, _, _ := compareVariants(results)
	if winner != "fast" {
		t.Errorf("winner = %q, want %q (faster)", winner, "fast")
	}
}

func TestCompareVariantsBothPassSmallerDiff(t *testing.T) {
	results := []VariantResult{
		{Name: "big", Passed: true, LatencyMs: 5000, DiffLines: 100},
		{Name: "small", Passed: true, LatencyMs: 5000, DiffLines: 5},
	}

	winner, _, _ := compareVariants(results)
	if winner != "small" {
		t.Errorf("winner = %q, want %q (smaller diff)", winner, "small")
	}
}

func TestCompareVariantsNonePasses(t *testing.T) {
	results := []VariantResult{
		{Name: "a", Passed: false, LatencyMs: 5000, DiffLines: 10},
		{Name: "b", Passed: false, LatencyMs: 3000, DiffLines: 5},
	}

	_, reason, confidence := compareVariants(results)
	// Should indicate no variant passed
	if reason == "" {
		t.Error("reason should not be empty")
	}
	// Confidence should be low when nothing passes
	if confidence > 0.3 {
		t.Errorf("confidence = %f, want <= 0.3 (no variant passed)", confidence)
	}
}

func TestCompareVariantsConfidence(t *testing.T) {
	// Large gap between variants should give high confidence
	results := []VariantResult{
		{Name: "good", Passed: true, LatencyMs: 1000, DiffLines: 5},
		{Name: "bad", Passed: false, LatencyMs: 60000, DiffLines: 500},
	}

	_, _, confidence := compareVariants(results)
	if confidence < 0.5 {
		t.Errorf("confidence = %f, expected > 0.5 for clear winner", confidence)
	}
}

func TestCompareVariantsThreeWay(t *testing.T) {
	results := []VariantResult{
		{Name: "a", Passed: true, LatencyMs: 10000, DiffLines: 50},
		{Name: "b", Passed: true, LatencyMs: 5000, DiffLines: 20},
		{Name: "c", Passed: false, LatencyMs: 3000, DiffLines: 5},
	}

	winner, _, _ := compareVariants(results)
	// b should win: passes, faster than a, and smaller diff
	if winner != "b" {
		t.Errorf("winner = %q, want %q", winner, "b")
	}
}

func TestFindToolFallback(t *testing.T) {
	got := findTool("nonexistent-tool-xyz")
	if got != "nonexistent-tool-xyz" {
		t.Errorf("findTool fallback = %q, want %q", got, "nonexistent-tool-xyz")
	}
}

func TestConfidenceBounds(t *testing.T) {
	results := []VariantResult{
		{Name: "a", Passed: true, LatencyMs: 1000, DiffLines: 5},
		{Name: "b", Passed: true, LatencyMs: 1001, DiffLines: 6},
	}

	_, _, confidence := compareVariants(results)
	if confidence < 0 || confidence > 1.0 {
		t.Errorf("confidence = %f, should be in [0, 1]", confidence)
	}
	if math.IsNaN(confidence) {
		t.Error("confidence should not be NaN")
	}
}
