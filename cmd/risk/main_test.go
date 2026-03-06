package main

import (
	"testing"
)

func TestAnalyzeDiff_EmptyDiff(t *testing.T) {
	result := analyzeDiff("", "")
	if result.Risk != 0 {
		t.Errorf("risk = %f, want 0", result.Risk)
	}
	if result.Class != "low" {
		t.Errorf("class = %q, want low", result.Class)
	}
}

func TestAnalyzeDiff_SingleFileChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"

 func main() {
`
	result := analyzeDiff(diff, "")
	if result.Risk <= 0 {
		t.Errorf("risk should be > 0 for a real change, got %f", result.Risk)
	}
	if result.Class != "low" && result.Class != "med" {
		t.Errorf("single file small change should be low or med, got %q", result.Class)
	}
}

func TestAnalyzeDiff_MultiFileChange(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
+line1
diff --git a/b.go b/b.go
+++ b/b.go
@@ -1,1 +1,2 @@
+line2
diff --git a/c.go b/c.go
+++ b/c.go
@@ -1,1 +1,2 @@
+line3
diff --git a/d.go b/d.go
+++ b/d.go
@@ -1,1 +1,2 @@
+line4
diff --git a/e.go b/e.go
+++ b/e.go
@@ -1,1 +1,2 @@
+line5
`
	result := analyzeDiff(diff, "")
	if result.Risk <= 0.1 {
		t.Errorf("5 files changed should have notable risk, got %f", result.Risk)
	}
}

func TestAnalyzeDiff_ConfigFiles(t *testing.T) {
	diff := `diff --git a/config.yaml b/config.yaml
+++ b/config.yaml
@@ -1,1 +1,2 @@
+setting: true
diff --git a/settings.toml b/settings.toml
+++ b/settings.toml
@@ -1,1 +1,2 @@
+key = "val"
`
	result := analyzeDiff(diff, "")
	// Config files should add risk
	found := false
	for _, w := range result.Why {
		if contains(w, "config") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected config file mention in why, got %v", result.Why)
	}
}

func TestAnalyzeDiff_TestFilesOnly(t *testing.T) {
	diff := `diff --git a/foo_test.go b/foo_test.go
+++ b/foo_test.go
@@ -1,1 +1,2 @@
+func TestFoo(t *testing.T) {}
`
	result := analyzeDiff(diff, "")
	// Test-only changes should have lower risk
	if result.Class == "high" {
		t.Errorf("test-only change should not be high risk, got class=%q risk=%f", result.Class, result.Risk)
	}
	found := false
	for _, w := range result.Why {
		if contains(w, "test") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected test file mention in why, got %v", result.Why)
	}
}

func TestAnalyzeDiff_HighRiskPatterns(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"Dockerfile", "Dockerfile"},
		{"migration", "migrations/001_init.sql"},
		{"github workflow", ".github/workflows/ci.yml"},
		{"go.mod", "go.mod"},
		{"package.json", "package.json"},
		{"Makefile", "Makefile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := `diff --git a/` + tt.filename + ` b/` + tt.filename + `
+++ b/` + tt.filename + `
@@ -1,1 +1,2 @@
+change
`
			result := analyzeDiff(diff, "")
			found := false
			for _, w := range result.Why {
				if contains(w, "high-risk") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected high-risk mention for %s, got why=%v", tt.filename, result.Why)
			}
		})
	}
}

func TestAnalyzeDiff_DossierHits(t *testing.T) {
	diff := `diff --git a/auth/login.go b/auth/login.go
+++ b/auth/login.go
@@ -1,1 +1,2 @@
+// changed
`
	dossier := "The auth module is a critical and fragile component. Handle with caution."
	result := analyzeDiff(diff, dossier)

	found := false
	for _, w := range result.Why {
		if contains(w, "risky zone") || contains(w, "dossier") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dossier hit mention in why, got %v", result.Why)
	}
}

func TestAnalyzeDiff_LargeChange(t *testing.T) {
	// Generate a diff with many added lines
	lines := "+++ b/big.go\n@@ -1,1 +1,600 @@\n"
	for i := 0; i < 550; i++ {
		lines += "+line\n"
	}
	diff := "diff --git a/big.go b/big.go\n" + lines

	result := analyzeDiff(diff, "")
	found := false
	for _, w := range result.Why {
		if contains(w, "large") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'large' mention for big change, got %v", result.Why)
	}
}

func TestAnalyzeDiff_Classification(t *testing.T) {
	tests := []struct {
		name      string
		risk      float64
		wantClass string
	}{
		{"zero", 0.0, "low"},
		{"low boundary", 0.29, "low"},
		{"med boundary", 0.3, "med"},
		{"mid med", 0.45, "med"},
		{"high boundary", 0.6, "high"},
		{"very high", 0.9, "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := "low"
			if tt.risk >= 0.6 {
				class = "high"
			} else if tt.risk >= 0.3 {
				class = "med"
			}
			if class != tt.wantClass {
				t.Errorf("risk %f -> class %q, want %q", tt.risk, class, tt.wantClass)
			}
		})
	}
}

func TestAnalyzeDiff_NoDossier(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,2 @@
+x
`
	result := analyzeDiff(diff, "")
	for _, w := range result.Why {
		if contains(w, "dossier") {
			t.Errorf("should not have dossier mention without dossier, got %q", w)
		}
	}
}

func TestAnalyzeDiff_ManyFiles(t *testing.T) {
	// 10+ files should trigger "large number of files"
	diff := ""
	for i := 0; i < 12; i++ {
		name := string(rune('a'+i)) + ".go"
		diff += "diff --git a/" + name + " b/" + name + "\n"
		diff += "+++ b/" + name + "\n"
		diff += "@@ -1,1 +1,2 @@\n"
		diff += "+line\n"
	}
	result := analyzeDiff(diff, "")
	found := false
	for _, w := range result.Why {
		if contains(w, "large number") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'large number' mention for 12 files, got %v", result.Why)
	}
}

func TestAnalyzeDiff_RiskClamped(t *testing.T) {
	// Construct a scenario that would push risk very high
	diff := ""
	for i := 0; i < 15; i++ {
		name := "Dockerfile" + string(rune('a'+i))
		diff += "diff --git a/" + name + " b/" + name + "\n"
		diff += "+++ b/" + name + "\n"
		diff += "@@ -1,1 +1,2 @@\n"
		for j := 0; j < 50; j++ {
			diff += "+line\n"
		}
	}
	result := analyzeDiff(diff, "this is a critical dangerous risky zone")
	if result.Risk > 1.0 {
		t.Errorf("risk should be clamped to 1.0, got %f", result.Risk)
	}
}

// contains checks if s contains substr (case-insensitive-ish via simple substring).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
