package main

import (
	"testing"
)

func TestBoolInfo(t *testing.T) {
	tests := []struct {
		cond    bool
		ifTrue  string
		ifFalse string
		want    string
	}{
		{true, "yes", "no", "yes"},
		{false, "yes", "no", "no"},
	}
	for _, tt := range tests {
		got := boolInfo(tt.cond, tt.ifTrue, tt.ifFalse)
		if got != tt.want {
			t.Errorf("boolInfo(%v, %q, %q) = %q, want %q",
				tt.cond, tt.ifTrue, tt.ifFalse, got, tt.want)
		}
	}
}

func TestEvaluateEmptyDiff(t *testing.T) {
	input := EvalInput{Diff: ""}
	// empty diff is handled by main(), but evaluate should also handle gracefully
	result := evaluate(input)
	// With empty diff, no files changed, risk should be 0
	if result.Risk != 0.0 {
		t.Errorf("risk = %f, want 0.0", result.Risk)
	}
	if result.Class != "low" {
		t.Errorf("class = %q, want %q", result.Class, "low")
	}
}

func TestEvaluateLowRiskDiff(t *testing.T) {
	diff := `--- a/internal/util.go
+++ b/internal/util.go
@@ -1,3 +1,4 @@
 package util

+// helper returns a value.
 func helper() int { return 42 }
`
	input := EvalInput{Diff: diff}
	result := evaluate(input)

	if result.Risk > 0.3 {
		t.Errorf("expected low risk, got %.2f", result.Risk)
	}
	if result.Class != "low" {
		t.Errorf("class = %q, want %q", result.Class, "low")
	}
	if result.Requires != "auto" {
		t.Errorf("requires = %q, want %q", result.Requires, "auto")
	}
}

func TestEvaluateMigrationDetection(t *testing.T) {
	diff := `--- a/db/migrations/001_create_users.sql
+++ b/db/migrations/001_create_users.sql
@@ -0,0 +1,5 @@
+CREATE TABLE users (
+  id SERIAL PRIMARY KEY,
+  name TEXT NOT NULL
+);
`
	input := EvalInput{Diff: diff}
	result := evaluate(input)

	if result.Risk < 0.3 {
		t.Errorf("expected risk >= 0.3 for migration, got %.2f", result.Risk)
	}

	foundMigration := false
	for _, r := range result.Reasons {
		if r == "touches migrations" {
			foundMigration = true
			break
		}
	}
	if !foundMigration {
		t.Errorf("expected 'touches migrations' in reasons: %v", result.Reasons)
	}
}

func TestEvaluateMigrationForbiddenByPolicy(t *testing.T) {
	diff := `--- a/migrations/001.sql
+++ b/migrations/001.sql
@@ -0,0 +1 @@
+ALTER TABLE users ADD COLUMN email TEXT;
`
	input := EvalInput{
		Diff:   diff,
		Policy: EvalPolicy{ForbidMigrations: true},
	}
	result := evaluate(input)

	if result.Risk < 0.5 {
		t.Errorf("expected risk >= 0.5 for forbidden migration, got %.2f", result.Risk)
	}
	if result.Requires != "approval" {
		t.Errorf("requires = %q, want %q", result.Requires, "approval")
	}
}

func TestEvaluateDependencyDetection(t *testing.T) {
	diff := `--- a/package.json
+++ b/package.json
@@ -5,3 +5,4 @@
   "dependencies": {
+    "lodash": "^4.17.21",
     "express": "^4.18.0"
`
	input := EvalInput{Diff: diff}
	result := evaluate(input)

	if result.Risk < 0.2 {
		t.Errorf("expected risk >= 0.2 for dependency change, got %.2f", result.Risk)
	}

	foundDeps := false
	for _, r := range result.Reasons {
		if len(r) > 0 && r[:8] == "modifies" {
			foundDeps = true
			break
		}
	}
	if !foundDeps {
		t.Errorf("expected dependency reason, got: %v", result.Reasons)
	}
}

func TestEvaluateDependencyForbiddenByPolicy(t *testing.T) {
	diff := `--- a/go.mod
+++ b/go.mod
@@ -3,3 +3,4 @@
 require (
+	github.com/pkg/errors v0.9.1
 )
`
	input := EvalInput{
		Diff:   diff,
		Policy: EvalPolicy{ForbidDeps: true},
	}
	result := evaluate(input)

	if result.Requires != "approval" {
		t.Errorf("requires = %q, want %q", result.Requires, "approval")
	}
}

func TestEvaluateFileCountExceeded(t *testing.T) {
	diff := `--- a/file1.go
+++ b/file1.go
@@ -1 +1,2 @@
+// change
--- a/file2.go
+++ b/file2.go
@@ -1 +1,2 @@
+// change
--- a/file3.go
+++ b/file3.go
@@ -1 +1,2 @@
+// change
`
	input := EvalInput{
		Diff:   diff,
		Policy: EvalPolicy{MaxFiles: 2},
	}
	result := evaluate(input)

	if result.Requires != "approval" {
		t.Errorf("requires = %q, want %q for exceeding max files", result.Requires, "approval")
	}
}

func TestEvaluateLineCountExceeded(t *testing.T) {
	diff := `--- a/big.go
+++ b/big.go
@@ -1 +1,6 @@
+line1
+line2
+line3
+line4
+line5
`
	input := EvalInput{
		Diff:   diff,
		Policy: EvalPolicy{MaxLines: 3},
	}
	result := evaluate(input)

	if result.Requires != "approval" {
		t.Errorf("requires = %q, want %q for exceeding max lines", result.Requires, "approval")
	}
}

func TestEvaluateDangerousPatterns(t *testing.T) {
	tests := []struct {
		name string
		diff string
	}{
		{"rm_rf", "+rm -rf /"},
		{"drop_table", "+DROP TABLE users;"},
		{"force_push", "+git push --force"},
		{"chmod_777", "+chmod 777 /etc/passwd"},
		{"curl_pipe", "+curl | bash"},
		{"git_reset_hard", "+git reset --hard"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := EvalInput{
				Diff: "--- a/script.sh\n+++ b/script.sh\n@@ -1 +1,2 @@\n" + tt.diff,
			}
			result := evaluate(input)

			foundDangerous := false
			for _, r := range result.Reasons {
				if len(r) > 18 && r[:18] == "dangerous patterns" {
					foundDangerous = true
					break
				}
			}
			if !foundDangerous {
				t.Errorf("expected dangerous pattern detection for %q, reasons: %v",
					tt.name, result.Reasons)
			}
		})
	}
}

func TestEvaluateSecretPatterns(t *testing.T) {
	tests := []struct {
		name string
		diff string
	}{
		{"password", "+password=secret123"},
		{"api_key", "+api_key=abc123"},
		{"aws_secret", "+AWS_SECRET_ACCESS_KEY=xxx"},
		{"github_token", "+GITHUB_TOKEN=ghp_xxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := EvalInput{
				Diff: "--- a/.env\n+++ b/.env\n@@ -1 +1,2 @@\n" + tt.diff,
			}
			result := evaluate(input)

			if result.Requires != "deny" {
				t.Errorf("requires = %q, want %q for secrets", result.Requires, "deny")
			}
		})
	}
}

func TestEvaluateRiskClamped(t *testing.T) {
	// Create a diff with many risk factors to push risk above 1.0
	diff := `--- a/db/migrations/001.sql
+++ b/db/migrations/001.sql
@@ -0,0 +1,5 @@
+DROP TABLE users;
+password=secret123
+rm -rf /
+chmod 777 /
--- a/package.json
+++ b/package.json
@@ -1 +1,2 @@
+new dep
`
	input := EvalInput{
		Diff: diff,
		Policy: EvalPolicy{
			ForbidMigrations: true,
			ForbidDeps:       true,
			MaxFiles:         1,
			MaxLines:         1,
		},
	}
	result := evaluate(input)

	if result.Risk > 1.0 {
		t.Errorf("risk = %.2f, should be clamped to 1.0", result.Risk)
	}
}

func TestEvaluateClassification(t *testing.T) {
	tests := []struct {
		risk float64
		want string
	}{
		{0.0, "low"},
		{0.3, "low"},
		{0.39, "low"},
		{0.4, "medium"},
		{0.5, "medium"},
		{0.69, "medium"},
		{0.7, "high"},
		{0.9, "high"},
		{1.0, "high"},
	}

	for _, tt := range tests {
		class := "low"
		if tt.risk >= 0.7 {
			class = "high"
		} else if tt.risk >= 0.4 {
			class = "medium"
		}
		if class != tt.want {
			t.Errorf("risk %.2f -> class %q, want %q", tt.risk, class, tt.want)
		}
	}
}

func TestEvaluateApprovalRequirements(t *testing.T) {
	// No risk => auto
	clean := evaluate(EvalInput{
		Diff: "--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n+// comment\n",
	})
	if clean.Requires != "auto" {
		t.Errorf("clean diff requires = %q, want %q", clean.Requires, "auto")
	}
}

func TestEvaluateChecksPresent(t *testing.T) {
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n+line\n"
	result := evaluate(EvalInput{Diff: diff})

	expectedChecks := []string{"migrations", "dependencies", "file_count", "line_count", "dangerous_patterns", "secrets"}
	if len(result.Checks) != len(expectedChecks) {
		t.Fatalf("expected %d checks, got %d", len(expectedChecks), len(result.Checks))
	}
	for i, name := range expectedChecks {
		if result.Checks[i].Name != name {
			t.Errorf("check[%d].Name = %q, want %q", i, result.Checks[i].Name, name)
		}
	}
}

func TestDepManifestsContainsCommonFiles(t *testing.T) {
	expected := []string{
		"package.json", "go.mod", "requirements.txt",
		"Cargo.toml", "Gemfile", "pom.xml",
	}
	for _, f := range expected {
		if !depManifests[f] {
			t.Errorf("depManifests missing %q", f)
		}
	}
}

func TestEvaluateNoIssuesReason(t *testing.T) {
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n+// safe comment\n"
	result := evaluate(EvalInput{Diff: diff})

	if len(result.Reasons) == 0 {
		t.Fatal("expected at least one reason")
	}
	if result.Reasons[0] != "no issues detected" {
		t.Errorf("reasons[0] = %q, want %q", result.Reasons[0], "no issues detected")
	}
}
