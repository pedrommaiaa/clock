package main

import (
	"testing"
)

func TestParseDiff_SimpleOneFile(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {
-    println("hi")
+    fmt.Println("hi")
`
	stats := parseDiff(diff)
	if len(stats.Files) != 1 {
		t.Errorf("files = %d, want 1", len(stats.Files))
	}
	if stats.Files[0] != "foo.go" {
		t.Errorf("file = %q, want %q", stats.Files[0], "foo.go")
	}
	if stats.LinesAdded != 2 {
		t.Errorf("added = %d, want 2", stats.LinesAdded)
	}
	if stats.LinesDeleted != 1 {
		t.Errorf("deleted = %d, want 1", stats.LinesDeleted)
	}
	if stats.HasBinary {
		t.Error("HasBinary should be false")
	}
}

func TestParseDiff_MultiFile(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,3 @@
 package main
+import "fmt"
diff --git a/bar.go b/bar.go
--- a/bar.go
+++ b/bar.go
@@ -1,2 +1,2 @@
 package main
-func bar() {}
+func bar() int { return 1 }
`
	stats := parseDiff(diff)
	if len(stats.Files) != 2 {
		t.Errorf("files = %d, want 2", len(stats.Files))
	}
	if stats.LinesAdded != 2 {
		t.Errorf("added = %d, want 2", stats.LinesAdded)
	}
	if stats.LinesDeleted != 1 {
		t.Errorf("deleted = %d, want 1", stats.LinesDeleted)
	}
}

func TestParseDiff_BinaryDetection(t *testing.T) {
	tests := []struct {
		name string
		diff string
	}{
		{
			name: "Binary files prefix",
			diff: "Binary files a/img.png and b/img.png differ\n",
		},
		{
			name: "GIT binary patch",
			diff: "diff --git a/img.png b/img.png\nGIT binary patch\n",
		},
		{
			name: "binary file in line",
			diff: "some binary file changed\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := parseDiff(tt.diff)
			if !stats.HasBinary {
				t.Error("HasBinary should be true")
			}
		})
	}
}

func TestParseDiff_ContextLineCounting(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,5 @@
 line1
 line2
-old
+new
 line4
 line5
`
	stats := parseDiff(diff)
	if len(stats.Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	// 4 context lines: line1, line2, line4, line5
	if stats.Hunks[0].ContextLines != 4 {
		t.Errorf("context lines = %d, want 4", stats.Hunks[0].ContextLines)
	}
}

func TestParseDiff_EmptyDiff(t *testing.T) {
	stats := parseDiff("")
	if len(stats.Files) != 0 {
		t.Errorf("files = %d, want 0", len(stats.Files))
	}
	if stats.LinesAdded != 0 || stats.LinesDeleted != 0 {
		t.Error("added/deleted should be 0 for empty diff")
	}
}

func TestStripDiffPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a/foo.go", "foo.go"},
		{"b/foo.go", "foo.go"},
		{"foo.go", "foo.go"},
		{"a/", ""},
		{"b/deep/path/file.go", "deep/path/file.go"},
		{"/dev/null", "/dev/null"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripDiffPrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripDiffPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsConfigFile(t *testing.T) {
	tests := []struct {
		path   string
		want   bool
	}{
		{"config.yml", true},
		{"docker-compose.yaml", true},
		{"settings.toml", true},
		{"app.ini", true},
		{"go.mod", true},
		{"go.sum", true},
		{"package.json", true},
		{"requirements.txt", true},
		{"Dockerfile", true},
		{"Makefile", true},
		{".env", true},
		{".env.local", true},
		{"main.go", false},
		{"readme.md", false},
		{"utils.py", false},
		{"index.js", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isConfigFile(tt.path)
			if got != tt.want {
				t.Errorf("isConfigFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestApplyPolicy_MaxFilesExceeded(t *testing.T) {
	stats := DiffStats{
		Files:      []string{"a.go", "b.go", "c.go"},
		LinesAdded: 5,
	}
	policy := Policy{MaxFiles: 2}
	result := applyPolicy(stats, policy)
	if result.OK {
		t.Error("expected OK=false when files exceed max_files")
	}
	if len(result.Reasons) == 0 {
		t.Error("expected reasons to be set")
	}
}

func TestApplyPolicy_MaxLinesExceeded(t *testing.T) {
	stats := DiffStats{
		Files:        []string{"a.go"},
		LinesAdded:   50,
		LinesDeleted: 60,
	}
	policy := Policy{MaxLines: 100}
	result := applyPolicy(stats, policy)
	if result.OK {
		t.Error("expected OK=false when lines exceed max_lines")
	}
}

func TestApplyPolicy_ForbiddenPaths(t *testing.T) {
	stats := DiffStats{
		Files: []string{"secrets.env", "main.go"},
	}
	policy := Policy{ForbiddenPaths: []string{"*.env"}}
	result := applyPolicy(stats, policy)
	if result.OK {
		t.Error("expected OK=false for forbidden path match")
	}
}

func TestApplyPolicy_RequireContext(t *testing.T) {
	stats := DiffStats{
		Files: []string{"a.go"},
		Hunks: []HunkInfo{
			{File: "a.go", ContextLines: 1},
		},
	}
	policy := Policy{RequireContext: 3}
	result := applyPolicy(stats, policy)
	// require_context is a warning, not blocking
	if !result.OK {
		t.Error("require_context should not block (OK should be true)")
	}
	if len(result.Needs) == 0 {
		t.Error("expected needs to be populated for insufficient context")
	}
}

func TestApplyPolicy_DenyBinary(t *testing.T) {
	stats := DiffStats{
		Files:     []string{"img.png"},
		HasBinary: true,
	}
	policy := Policy{DenyBinary: true}
	result := applyPolicy(stats, policy)
	if result.OK {
		t.Error("expected OK=false when deny_binary and has binary")
	}
}

func TestApplyPolicy_AllClear(t *testing.T) {
	stats := DiffStats{
		Files:        []string{"a.go"},
		LinesAdded:   5,
		LinesDeleted: 3,
		Hunks:        []HunkInfo{{File: "a.go", ContextLines: 5}},
	}
	policy := Policy{
		MaxFiles:       10,
		MaxLines:       100,
		RequireContext: 3,
		DenyBinary:     true,
	}
	result := applyPolicy(stats, policy)
	if !result.OK {
		t.Errorf("expected OK=true, got reasons: %v", result.Reasons)
	}
}

func TestComputeRisk_Range(t *testing.T) {
	tests := []struct {
		name  string
		stats DiffStats
	}{
		{name: "empty", stats: DiffStats{}},
		{name: "small", stats: DiffStats{Files: []string{"a.go"}, LinesAdded: 5}},
		{name: "medium", stats: DiffStats{
			Files:        []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
			LinesAdded:   200,
			LinesDeleted: 100,
		}},
		{name: "large with config", stats: DiffStats{
			Files:        []string{"a.go", "b.go", "go.mod", "go.sum", "Makefile", "config.yml"},
			LinesAdded:   500,
			LinesDeleted: 500,
			HasBinary:    true,
		}},
		{name: "huge", stats: DiffStats{
			Files:        make([]string, 100),
			LinesAdded:   10000,
			LinesDeleted: 10000,
			HasBinary:    true,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := computeRisk(tt.stats)
			if risk < 0.0 || risk > 1.0 {
				t.Errorf("risk = %f, want 0.0 <= risk <= 1.0", risk)
			}
		})
	}
}

func TestComputeRisk_Monotonic(t *testing.T) {
	small := DiffStats{Files: []string{"a.go"}, LinesAdded: 5}
	big := DiffStats{
		Files:        []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
		LinesAdded:   200,
		LinesDeleted: 100,
		HasBinary:    true,
	}

	riskSmall := computeRisk(small)
	riskBig := computeRisk(big)
	if riskSmall >= riskBig {
		t.Errorf("small risk (%f) should be less than big risk (%f)", riskSmall, riskBig)
	}
}
