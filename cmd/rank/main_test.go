package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRerank(t *testing.T) {
	candidates := []Candidate{
		{Path: "a.go", Score: 1.0},
		{Path: "b.go", Score: 0.5},
		{Path: "c.go", Score: 0.8},
	}

	history := History{
		Files: map[string]FileHistory{
			"b.go": {Helped: 5, Total: 5}, // always helped
		},
	}

	boosts := BoostConfig{
		Patterns: []BoostPattern{
			{Glob: "*.go", Boost: 0.1},
		},
	}

	rerank(candidates, history, boosts)

	// After rerank, b.go should be boosted significantly due to history (helped/total=1.0)
	// All scores should be normalized to 0-1 range
	for _, c := range candidates {
		if c.Score < 0 || c.Score > 1.0 {
			t.Errorf("score for %s out of range: %f", c.Path, c.Score)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name       string
		candidates []Candidate
		wantMin    float64
		wantMax    float64
	}{
		{
			name: "varied scores",
			candidates: []Candidate{
				{Path: "a", Score: 10.0},
				{Path: "b", Score: 5.0},
				{Path: "c", Score: 0.0},
			},
			wantMin: 0.0,
			wantMax: 1.0,
		},
		{
			name: "all same score",
			candidates: []Candidate{
				{Path: "a", Score: 5.0},
				{Path: "b", Score: 5.0},
			},
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			name:       "empty",
			candidates: []Candidate{},
		},
		{
			name: "single",
			candidates: []Candidate{
				{Path: "a", Score: 42.0},
			},
			wantMin: 1.0,
			wantMax: 1.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalize(tt.candidates)
			if len(tt.candidates) == 0 {
				return
			}
			minScore := math.Inf(1)
			maxScore := math.Inf(-1)
			for _, c := range tt.candidates {
				if c.Score < minScore {
					minScore = c.Score
				}
				if c.Score > maxScore {
					maxScore = c.Score
				}
			}
			if math.Abs(minScore-tt.wantMin) > 0.001 {
				t.Errorf("min score = %f, want %f", minScore, tt.wantMin)
			}
			if math.Abs(maxScore-tt.wantMax) > 0.001 {
				t.Errorf("max score = %f, want %f", maxScore, tt.wantMax)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"*.go", "dir/main.go", false}, // no ** so doesn't match nested
		{"cmd/*", "cmd/main", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.path)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchGlobDoublestar(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*.go", "cmd/main.go", true},
		{"**/*.go", "main.go", true},
		{"src/**/*.js", "src/app/index.js", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.path)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestLoadHistoryMissing(t *testing.T) {
	h := loadHistory("/nonexistent/path/history.json")
	if h.Files == nil {
		t.Error("loadHistory should return initialized Files map")
	}
	if len(h.Files) != 0 {
		t.Error("loadHistory from missing file should have empty Files")
	}
}

func TestLoadHistoryValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	history := History{
		Files: map[string]FileHistory{
			"main.go": {Helped: 3, Total: 5},
			"auth.go": {Helped: 1, Total: 10},
		},
	}
	data, _ := json.MarshalIndent(history, "", "  ")
	os.WriteFile(path, data, 0o644)

	loaded := loadHistory(path)
	if len(loaded.Files) != 2 {
		t.Fatalf("loaded %d files, want 2", len(loaded.Files))
	}
	if loaded.Files["main.go"].Helped != 3 {
		t.Errorf("main.go helped = %d, want 3", loaded.Files["main.go"].Helped)
	}
}

func TestLoadBoostsMissing(t *testing.T) {
	b := loadBoosts("/nonexistent/path/boosts.json")
	if b.Patterns != nil && len(b.Patterns) > 0 {
		t.Error("loadBoosts from missing file should have empty patterns")
	}
}

func TestLoadBoostsValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boosts.json")

	boosts := BoostConfig{
		Patterns: []BoostPattern{
			{Glob: "*.go", Boost: 0.5},
			{Glob: "cmd/**", Boost: 0.3},
		},
	}
	data, _ := json.MarshalIndent(boosts, "", "  ")
	os.WriteFile(path, data, 0o644)

	loaded := loadBoosts(path)
	if len(loaded.Patterns) != 2 {
		t.Fatalf("loaded %d patterns, want 2", len(loaded.Patterns))
	}
	if loaded.Patterns[0].Boost != 0.5 {
		t.Errorf("pattern[0] boost = %f, want 0.5", loaded.Patterns[0].Boost)
	}
}

func TestRerankWithHistoryBoost(t *testing.T) {
	candidates := []Candidate{
		{Path: "low.go", Score: 1.0},
		{Path: "high.go", Score: 1.0},
	}

	history := History{
		Files: map[string]FileHistory{
			"high.go": {Helped: 10, Total: 10}, // 100% helped
		},
	}

	boosts := BoostConfig{}
	rerank(candidates, history, boosts)

	// high.go should have a higher score than low.go after normalization
	var highScore, lowScore float64
	for _, c := range candidates {
		if c.Path == "high.go" {
			highScore = c.Score
		}
		if c.Path == "low.go" {
			lowScore = c.Score
		}
	}

	if highScore <= lowScore {
		t.Errorf("high.go score (%f) should be > low.go score (%f)", highScore, lowScore)
	}
}

func TestRerankSorting(t *testing.T) {
	candidates := []Candidate{
		{Path: "c.go", Score: 0.1},
		{Path: "a.go", Score: 0.9},
		{Path: "b.go", Score: 0.5},
	}

	// No history or boosts
	rerank(candidates, History{Files: make(map[string]FileHistory)}, BoostConfig{})

	// Verify scores are normalized
	for _, c := range candidates {
		if c.Score < 0 || c.Score > 1.0 {
			t.Errorf("score for %s out of range: %f", c.Path, c.Score)
		}
	}
}

func TestExtractSegment(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    string
	}{
		{"cmd/main.go", "cmd", "cmd"},
		{"a/b/c/d.go", "a/b", "a/b"},
	}
	for _, tt := range tests {
		got := extractSegment(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("extractSegment(%q, %q) = %q, want %q", tt.path, tt.pattern, got, tt.want)
		}
	}
}
