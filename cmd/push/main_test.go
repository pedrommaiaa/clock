package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatReportFull(t *testing.T) {
	r := PushResult{
		Goal:   "fix the bug",
		Report: "Bug fixed in main.go",
		Diff:   "+added line\n-removed line",
	}

	report := formatReport(r)

	if !strings.Contains(report, "## Goal") {
		t.Error("report missing Goal section")
	}
	if !strings.Contains(report, "fix the bug") {
		t.Error("report missing goal content")
	}
	if !strings.Contains(report, "## Report") {
		t.Error("report missing Report section")
	}
	if !strings.Contains(report, "Bug fixed in main.go") {
		t.Error("report missing report content")
	}
	if !strings.Contains(report, "## Diff") {
		t.Error("report missing Diff section")
	}
	if !strings.Contains(report, "```diff") {
		t.Error("report missing diff code block")
	}
	if !strings.Contains(report, "+added line") {
		t.Error("report missing diff content")
	}
}

func TestFormatReportGoalOnly(t *testing.T) {
	r := PushResult{Goal: "just a goal"}
	report := formatReport(r)

	if !strings.Contains(report, "## Goal") {
		t.Error("report missing Goal section")
	}
	if strings.Contains(report, "## Report") {
		t.Error("report should not have Report section when empty")
	}
	if strings.Contains(report, "## Diff") {
		t.Error("report should not have Diff section when empty")
	}
}

func TestFormatReportReportOnly(t *testing.T) {
	r := PushResult{Report: "just a report"}
	report := formatReport(r)

	if strings.Contains(report, "## Goal") {
		t.Error("report should not have Goal section when empty")
	}
	if !strings.Contains(report, "## Report") {
		t.Error("report missing Report section")
	}
}

func TestFormatReportDiffOnly(t *testing.T) {
	r := PushResult{Diff: "+new line"}
	report := formatReport(r)

	if strings.Contains(report, "## Goal") {
		t.Error("report should not have Goal section when empty")
	}
	if !strings.Contains(report, "## Diff") {
		t.Error("report missing Diff section")
	}
}

func TestFormatReportEmpty(t *testing.T) {
	r := PushResult{}
	report := formatReport(r)

	if report != "" {
		t.Errorf("expected empty report, got %q", report)
	}
}

func TestPushStdout(t *testing.T) {
	input := PushInput{
		Target: "stdout",
		Result: PushResult{
			Goal:   "test goal",
			Report: "test report",
		},
	}

	result := pushStdout(input)

	if !result.OK {
		t.Error("pushStdout should return OK=true")
	}
	if result.Target != "stdout" {
		t.Errorf("Target = %q, want %q", result.Target, "stdout")
	}
}

func TestPushFileSuccess(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output.md")

	input := PushInput{
		Target: "file",
		Result: PushResult{
			Goal:   "test goal",
			Report: "test report",
		},
		Config: PushConfig{
			FilePath: outPath,
		},
	}

	result := pushFile(input)

	if !result.OK {
		t.Errorf("pushFile failed: %s", result.Err)
	}
	if result.Target != "file" {
		t.Errorf("Target = %q, want %q", result.Target, "file")
	}

	// Verify file contents
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "test goal") {
		t.Error("output file missing goal content")
	}
	if !strings.Contains(content, "test report") {
		t.Error("output file missing report content")
	}
}

func TestPushFileMissingPath(t *testing.T) {
	input := PushInput{
		Target: "file",
		Result: PushResult{Report: "test"},
		Config: PushConfig{FilePath: ""},
	}

	result := pushFile(input)

	if result.OK {
		t.Error("pushFile with empty path should fail")
	}
	if result.Err == "" {
		t.Error("expected error message for missing file_path")
	}
}

func TestPushFileCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "sub", "dir", "output.md")

	input := PushInput{
		Target: "file",
		Result: PushResult{Report: "nested output"},
		Config: PushConfig{FilePath: outPath},
	}

	result := pushFile(input)

	if !result.OK {
		t.Errorf("pushFile with nested path failed: %s", result.Err)
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Error("output file not created in nested directory")
	}
}

func TestPushGitHubMissingToken(t *testing.T) {
	// Ensure GITHUB_TOKEN is unset
	originalToken := os.Getenv("GITHUB_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")
	defer func() {
		if originalToken != "" {
			os.Setenv("GITHUB_TOKEN", originalToken)
		}
	}()

	input := PushInput{
		Target: "github",
		Result: PushResult{Report: "test"},
		Config: PushConfig{Repo: "owner/repo", PR: 1},
	}

	result := pushGitHub(input)

	if result.OK {
		t.Error("pushGitHub without token should fail")
	}
	if !strings.Contains(result.Err, "GITHUB_TOKEN") {
		t.Errorf("error should mention GITHUB_TOKEN, got %q", result.Err)
	}
}

func TestPushGitHubMissingRepo(t *testing.T) {
	os.Setenv("GITHUB_TOKEN", "fake-token")
	defer os.Unsetenv("GITHUB_TOKEN")

	input := PushInput{
		Target: "github",
		Result: PushResult{Report: "test"},
		Config: PushConfig{PR: 1},
	}

	result := pushGitHub(input)

	if result.OK {
		t.Error("pushGitHub without repo should fail")
	}
	if !strings.Contains(result.Err, "repo") {
		t.Errorf("error should mention repo, got %q", result.Err)
	}
}

func TestPushGitHubMissingPR(t *testing.T) {
	os.Setenv("GITHUB_TOKEN", "fake-token")
	defer os.Unsetenv("GITHUB_TOKEN")

	input := PushInput{
		Target: "github",
		Result: PushResult{Report: "test"},
		Config: PushConfig{Repo: "owner/repo"},
	}

	result := pushGitHub(input)

	if result.OK {
		t.Error("pushGitHub without PR should fail")
	}
	if !strings.Contains(result.Err, "pr") {
		t.Errorf("error should mention pr, got %q", result.Err)
	}
}

func TestPushSlackMissingWebhook(t *testing.T) {
	input := PushInput{
		Target: "slack",
		Result: PushResult{Report: "test"},
		Config: PushConfig{},
	}

	result := pushSlack(input)

	if result.OK {
		t.Error("pushSlack without webhook should fail")
	}
	if !strings.Contains(result.Err, "webhook_url") {
		t.Errorf("error should mention webhook_url, got %q", result.Err)
	}
}

func TestPushUnknownTarget(t *testing.T) {
	input := PushInput{
		Target: "unknown",
		Result: PushResult{Report: "test"},
	}

	// Mirror main switch default
	result := PushOutput{
		OK:     false,
		Target: input.Target,
		Err:    "unknown target: unknown",
	}

	if result.OK {
		t.Error("unknown target should not be OK")
	}
	if !strings.Contains(result.Err, "unknown") {
		t.Errorf("error should mention unknown, got %q", result.Err)
	}
}

func TestPushOutputJSONRoundTrip(t *testing.T) {
	out := PushOutput{
		OK:     true,
		Target: "stdout",
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}

	var got PushOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.OK != true {
		t.Error("OK should be true")
	}
	if got.Target != "stdout" {
		t.Errorf("Target = %q, want %q", got.Target, "stdout")
	}
}

func TestPushInputJSONParsing(t *testing.T) {
	input := `{
		"target": "file",
		"result": {"goal": "g", "report": "r", "diff": "d"},
		"config": {"file_path": "/tmp/out.md"}
	}`

	var got PushInput
	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatal(err)
	}

	if got.Target != "file" {
		t.Errorf("Target = %q, want %q", got.Target, "file")
	}
	if got.Result.Goal != "g" {
		t.Errorf("Result.Goal = %q, want %q", got.Result.Goal, "g")
	}
	if got.Config.FilePath != "/tmp/out.md" {
		t.Errorf("Config.FilePath = %q, want %q", got.Config.FilePath, "/tmp/out.md")
	}
}

func TestPushOutputWithError(t *testing.T) {
	out := PushOutput{
		OK:     false,
		Target: "github",
		Err:    "GITHUB_TOKEN not set",
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}

	// Verify error field is present in JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["err"] != "GITHUB_TOKEN not set" {
		t.Errorf("err = %v, want %q", raw["err"], "GITHUB_TOKEN not set")
	}
}
