package main

import (
	"testing"
)

func TestIndexByte(t *testing.T) {
	tests := []struct {
		s    string
		c    byte
		want int
	}{
		{"hello\nworld", '\n', 5},
		{"no newline", '\n', -1},
		{"", '\n', -1},
		{"\n", '\n', 0},
		{"abc\ndef\nghi", '\n', 3},
	}
	for _, tt := range tests {
		got := indexByte(tt.s, tt.c)
		if got != tt.want {
			t.Errorf("indexByte(%q, %q) = %d, want %d", tt.s, tt.c, got, tt.want)
		}
	}
}

func TestTrimLeadingLines(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"zero lines", "hello\nworld", 0, "hello\nworld"},
		{"one line", "hello\nworld", 1, "world"},
		{"two lines", "a\nb\nc", 2, "c"},
		{"all lines", "a\nb", 2, ""},
		{"more than available", "a\nb", 5, ""},
		{"negative", "hello", -1, "hello"},
		{"empty string", "", 1, ""},
		{"single line no newline", "hello", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimLeadingLines(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("trimLeadingLines(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestMergeOverlapping_NoOverlap(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 5, Text: "line1\nline2\nline3\nline4\nline5", Score: 1.0},
		{Path: "a.go", Start: 10, End: 15, Text: "line10\nline11\nline12\nline13\nline14\nline15", Score: 0.5},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 2 {
		t.Fatalf("expected 2 snippets, got %d", len(result))
	}
}

func TestMergeOverlapping_WithOverlap(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 5, Text: "l1\nl2\nl3\nl4\nl5", Score: 1.0},
		{Path: "a.go", Start: 3, End: 8, Text: "l3\nl4\nl5\nl6\nl7\nl8", Score: 2.0},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged snippet, got %d", len(result))
	}
	if result[0].Start != 1 {
		t.Errorf("start = %d, want 1", result[0].Start)
	}
	if result[0].End != 8 {
		t.Errorf("end = %d, want 8", result[0].End)
	}
	if result[0].Score != 2.0 {
		t.Errorf("score = %f, want 2.0", result[0].Score)
	}
}

func TestMergeOverlapping_DifferentFiles(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 5, Text: "text-a", Score: 1.0},
		{Path: "b.go", Start: 1, End: 5, Text: "text-b", Score: 2.0},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 2 {
		t.Fatalf("expected 2 snippets (different files), got %d", len(result))
	}
}

func TestMergeOverlapping_Adjacent(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 5, Text: "l1\nl2\nl3\nl4\nl5", Score: 1.0},
		{Path: "a.go", Start: 6, End: 10, Text: "l6\nl7\nl8\nl9\nl10", Score: 0.5},
	}
	result := mergeOverlapping(snippets)
	// Adjacent ranges (end+1 == start) should be merged
	if len(result) != 1 {
		t.Fatalf("expected 1 merged snippet for adjacent ranges, got %d", len(result))
	}
	if result[0].End != 10 {
		t.Errorf("end = %d, want 10", result[0].End)
	}
}

func TestMergeOverlapping_SingleSnippet(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 10, Text: "hello", Score: 5.0},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(result))
	}
	if result[0].Score != 5.0 {
		t.Errorf("score = %f, want 5.0", result[0].Score)
	}
}

func TestMergeOverlapping_Empty(t *testing.T) {
	result := mergeOverlapping(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 snippets for nil input, got %d", len(result))
	}
}

func TestMergeOverlapping_MaxScore(t *testing.T) {
	snippets := []Snippet{
		{Path: "a.go", Start: 1, End: 10, Text: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10", Score: 3.0},
		{Path: "a.go", Start: 5, End: 15, Text: "l5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15", Score: 7.0},
		{Path: "a.go", Start: 12, End: 20, Text: "l12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nl20", Score: 1.0},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(result))
	}
	if result[0].Score != 7.0 {
		t.Errorf("score = %f, want 7.0 (max of 3.0, 7.0, 1.0)", result[0].Score)
	}
}

func TestBudgetPacking(t *testing.T) {
	// Simulate the budget packing logic from main
	candidates := []Snippet{
		{Path: "a.go", Start: 1, End: 10, Text: "short", Score: 10},
		{Path: "b.go", Start: 1, End: 10, Text: "medium text here", Score: 5},
		{Path: "c.go", Start: 1, End: 10, Text: "this is a much longer text that takes more bytes", Score: 1},
	}

	maxBytes := 25

	// Sort by score descending (already sorted)
	var packed []Snippet
	usedBytes := 0
	for _, s := range candidates {
		size := len(s.Text)
		if size == 0 {
			continue
		}
		if usedBytes+size > maxBytes {
			continue
		}
		packed = append(packed, s)
		usedBytes += size
	}

	// Should include "short" (5 bytes) and "medium text here" (16 bytes) = 21 bytes
	// But not "this is a much longer..." (49 bytes, would exceed 25)
	if len(packed) != 2 {
		t.Fatalf("expected 2 packed snippets, got %d", len(packed))
	}
	if packed[0].Path != "a.go" {
		t.Errorf("first packed = %q, want a.go", packed[0].Path)
	}
	if packed[1].Path != "b.go" {
		t.Errorf("second packed = %q, want b.go", packed[1].Path)
	}
	if usedBytes != 5+16 {
		t.Errorf("used bytes = %d, want %d", usedBytes, 5+16)
	}
}

func TestBudgetPackingSkipsEmptyText(t *testing.T) {
	candidates := []Snippet{
		{Path: "a.go", Start: 1, End: 10, Text: "", Score: 10},
		{Path: "b.go", Start: 1, End: 10, Text: "hello", Score: 5},
	}
	maxBytes := 100
	var packed []Snippet
	usedBytes := 0
	for _, s := range candidates {
		size := len(s.Text)
		if size == 0 {
			continue
		}
		if usedBytes+size > maxBytes {
			continue
		}
		packed = append(packed, s)
		usedBytes += size
	}

	if len(packed) != 1 {
		t.Fatalf("expected 1 packed snippet (empty text skipped), got %d", len(packed))
	}
	if packed[0].Path != "b.go" {
		t.Errorf("packed path = %q, want b.go", packed[0].Path)
	}
}

func TestMergeOverlapping_OrderPreserved(t *testing.T) {
	snippets := []Snippet{
		{Path: "b.go", Start: 1, End: 5, Text: "b-text", Score: 1.0},
		{Path: "a.go", Start: 1, End: 5, Text: "a-text", Score: 2.0},
	}
	result := mergeOverlapping(snippets)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	// Order should be preserved: b.go first, then a.go
	if result[0].Path != "b.go" {
		t.Errorf("first result path = %q, want b.go", result[0].Path)
	}
	if result[1].Path != "a.go" {
		t.Errorf("second result path = %q, want a.go", result[1].Path)
	}
}
