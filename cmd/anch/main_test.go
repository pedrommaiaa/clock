package main

import (
	"strings"
	"testing"
)

func TestParseDiff_SingleFile(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"

 func main() {
`
	fds, err := parseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(fds) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fds))
	}
	if fds[0].OldPath != "foo.go" {
		t.Errorf("OldPath = %q, want foo.go", fds[0].OldPath)
	}
	if fds[0].NewPath != "foo.go" {
		t.Errorf("NewPath = %q, want foo.go", fds[0].NewPath)
	}
	if len(fds[0].Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(fds[0].Hunks))
	}
}

func TestParseDiff_MultiFile(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package a
+var x = 1

diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -5,3 +5,4 @@
 func foo() {
+    return
 }
`
	fds, err := parseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(fds) != 2 {
		t.Fatalf("expected 2 file diffs, got %d", len(fds))
	}
	if fds[0].NewPath != "a.go" {
		t.Errorf("first file = %q, want a.go", fds[0].NewPath)
	}
	if fds[1].NewPath != "b.go" {
		t.Errorf("second file = %q, want b.go", fds[1].NewPath)
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		header   string
		oldStart int
		oldCount int
		newStart int
		newCount int
	}{
		{"@@ -1,3 +1,4 @@", 1, 3, 1, 4},
		{"@@ -10,5 +12,7 @@ func main()", 10, 5, 12, 7},
		{"@@ -1 +1 @@", 1, 1, 1, 1},
		{"@@ -100,0 +101,3 @@", 100, 0, 101, 3},
	}
	for _, tt := range tests {
		var hunk Hunk
		parseHunkHeader(tt.header, &hunk)
		if hunk.OldStart != tt.oldStart {
			t.Errorf("%q: OldStart = %d, want %d", tt.header, hunk.OldStart, tt.oldStart)
		}
		if hunk.OldCount != tt.oldCount {
			t.Errorf("%q: OldCount = %d, want %d", tt.header, hunk.OldCount, tt.oldCount)
		}
		if hunk.NewStart != tt.newStart {
			t.Errorf("%q: NewStart = %d, want %d", tt.header, hunk.NewStart, tt.newStart)
		}
		if hunk.NewCount != tt.newCount {
			t.Errorf("%q: NewCount = %d, want %d", tt.header, hunk.NewCount, tt.newCount)
		}
	}
}

func TestExtractContextLines(t *testing.T) {
	hunk := Hunk{
		Lines: []string{
			" context line 1",
			"+added line",
			"-removed line",
			" context line 2",
			"",
		},
	}
	ctx := extractContextLines(hunk)
	if len(ctx) != 3 {
		t.Fatalf("expected 3 context lines, got %d: %v", len(ctx), ctx)
	}
	if ctx[0] != "context line 1" {
		t.Errorf("ctx[0] = %q, want %q", ctx[0], "context line 1")
	}
	if ctx[1] != "context line 2" {
		t.Errorf("ctx[1] = %q, want %q", ctx[1], "context line 2")
	}
	if ctx[2] != "" {
		t.Errorf("ctx[2] = %q, want empty", ctx[2])
	}
}

func TestExtractContextLines_NoContext(t *testing.T) {
	hunk := Hunk{
		Lines: []string{
			"+added",
			"-removed",
		},
	}
	ctx := extractContextLines(hunk)
	if len(ctx) != 0 {
		t.Fatalf("expected 0 context lines, got %d", len(ctx))
	}
}

func TestMatchesAt(t *testing.T) {
	fileLines := []string{"a", "b", "c", "d", "e"}

	tests := []struct {
		name         string
		contextLines []string
		start        int
		want         bool
	}{
		{"match at start", []string{"a", "b"}, 0, true},
		{"match in middle", []string{"c", "d"}, 2, true},
		{"no match", []string{"x", "y"}, 0, false},
		{"partial match", []string{"a", "x"}, 0, false},
		{"out of bounds start", []string{"a"}, -1, false},
		{"past end", []string{"e", "f"}, 4, false},
		{"empty context", []string{}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAt(fileLines, tt.contextLines, tt.start)
			if got != tt.want {
				t.Errorf("matchesAt(fileLines, %v, %d) = %v, want %v", tt.contextLines, tt.start, got, tt.want)
			}
		})
	}
}

func TestMatchesAtExact(t *testing.T) {
	fileLines := []string{"a", "b", "c", "d", "e"}

	tests := []struct {
		name         string
		contextLines []string
		start        int
		want         bool
	}{
		{"exact match", []string{"a", "b"}, 0, true},
		{"match with gap", []string{"a", "c"}, 0, true}, // finds them in order
		{"no match", []string{"x"}, 0, false},
		{"negative start", []string{"a"}, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAtExact(fileLines, tt.contextLines, tt.start)
			if got != tt.want {
				t.Errorf("matchesAtExact(fileLines, %v, %d) = %v, want %v", tt.contextLines, tt.start, got, tt.want)
			}
		})
	}
}

func TestFindDrift(t *testing.T) {
	// matchesAtExact scans forward from the start position looking for context lines
	// in order. So we need a file where context lines ONLY appear at a specific spot
	// and nowhere else within the drift range when scanning from other positions.
	fileLines := []string{
		"aaa", "bbb", "ccc", "ddd", "eee",
		"UNIQUE_X", "UNIQUE_Y",
		"fff", "ggg", "hhh",
	}

	tests := []struct {
		name          string
		contextLines  []string
		expectedStart int
		maxDrift      int
		requireUnique bool
		wantDrift     int
		wantFound     bool
		wantAmbiguous bool
	}{
		{
			name:          "no drift needed",
			contextLines:  []string{"UNIQUE_X", "UNIQUE_Y"},
			expectedStart: 5,
			maxDrift:      5,
			wantDrift:     0,
			wantFound:     true,
		},
		{
			name:          "drift by -2",
			contextLines:  []string{"UNIQUE_X", "UNIQUE_Y"},
			expectedStart: 7,
			maxDrift:      5,
			wantDrift:     -2,
			wantFound:     true,
		},
		{
			name:          "not found at all",
			contextLines:  []string{"nonexistent"},
			expectedStart: 0,
			maxDrift:      10,
			wantFound:     false,
		},
		{
			name:          "found at edge of drift",
			contextLines:  []string{"UNIQUE_X", "UNIQUE_Y"},
			expectedStart: 8,
			maxDrift:      3,
			wantDrift:     -3,
			wantFound:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drift, found, ambiguous := findDrift(fileLines, tt.contextLines, tt.expectedStart, tt.maxDrift, tt.requireUnique)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if found && drift != tt.wantDrift {
				t.Errorf("drift = %d, want %d", drift, tt.wantDrift)
			}
			if ambiguous != tt.wantAmbiguous {
				t.Errorf("ambiguous = %v, want %v", ambiguous, tt.wantAmbiguous)
			}
		})
	}
}

func TestFindDrift_Ambiguous(t *testing.T) {
	// Repeated pattern
	fileLines := []string{
		"a", "b", "c", "a", "b", "c",
	}
	_, found, ambiguous := findDrift(fileLines, []string{"a", "b"}, 0, 5, true)
	if !found {
		t.Fatal("expected to find match")
	}
	if !ambiguous {
		t.Fatal("expected ambiguous match with requireUnique=true")
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 0},
		{5, 5},
		{-5, 5},
		{-1, 1},
		{1, 1},
	}
	for _, tt := range tests {
		got := abs(tt.input)
		if got != tt.want {
			t.Errorf("abs(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestReconstructDiff(t *testing.T) {
	fileDiffs := []FileDiff{
		{
			HeaderLines: []string{
				"diff --git a/foo.go b/foo.go",
				"--- a/foo.go",
				"+++ b/foo.go",
			},
			Hunks: []Hunk{
				{
					OldStart: 1,
					OldCount: 3,
					NewStart: 1,
					NewCount: 4,
					Header:   "@@ -1,3 +1,4 @@",
					Lines: []string{
						" package main",
						"+import \"fmt\"",
						" ",
					},
				},
			},
		},
	}

	result := reconstructDiff(fileDiffs)
	if !strings.Contains(result, "diff --git") {
		t.Error("reconstructed diff missing header")
	}
	if !strings.Contains(result, "@@ -1,3 +1,4 @@") {
		t.Error("reconstructed diff missing hunk header")
	}
	if !strings.Contains(result, "+import \"fmt\"") {
		t.Error("reconstructed diff missing added line")
	}
}

func TestParseDiff_EmptyDiff(t *testing.T) {
	fds, err := parseDiff("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fds) != 0 {
		t.Errorf("expected 0 file diffs for empty input, got %d", len(fds))
	}
}

func TestParseDiff_StripABPrefix(t *testing.T) {
	diff := `--- a/src/foo.go
+++ b/src/foo.go
@@ -1,2 +1,3 @@
 package foo
+var x = 1
`
	fds, err := parseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(fds) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fds))
	}
	if fds[0].OldPath != "src/foo.go" {
		t.Errorf("OldPath = %q, want src/foo.go", fds[0].OldPath)
	}
	if fds[0].NewPath != "src/foo.go" {
		t.Errorf("NewPath = %q, want src/foo.go", fds[0].NewPath)
	}
}

func TestReconstructDiff_PreservesTrailingContext(t *testing.T) {
	fileDiffs := []FileDiff{
		{
			HeaderLines: []string{"diff --git a/x.go b/x.go", "--- a/x.go", "+++ b/x.go"},
			Hunks: []Hunk{
				{
					OldStart: 10,
					OldCount: 5,
					NewStart: 12,
					NewCount: 7,
					Header:   "@@ -10,5 +12,7 @@ func foo()",
					Lines:    []string{" line1", "+line2"},
				},
			},
		},
	}
	result := reconstructDiff(fileDiffs)
	if !strings.Contains(result, "@@ -10,5 +12,7 @@") {
		t.Error("missing reconstructed hunk header")
	}
}
