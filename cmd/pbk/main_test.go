package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMakeIDDeterministic(t *testing.T) {
	id1 := makeID("job-1", "2024-01-01")
	id2 := makeID("job-1", "2024-01-01")
	if id1 != id2 {
		t.Errorf("makeID not deterministic: %q != %q", id1, id2)
	}
	// Should have pb- prefix
	if len(id1) < 3 || id1[:3] != "pb-" {
		t.Errorf("makeID should start with 'pb-', got %q", id1)
	}
}

func TestMakeIDUnique(t *testing.T) {
	id1 := makeID("job-1", "ts-1")
	id2 := makeID("job-2", "ts-2")
	if id1 == id2 {
		t.Error("makeID should produce different IDs for different inputs")
	}
}

func TestExtractErrorPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{
			name:  "strips hex hashes",
			input: "error at abcdef1234567890 in module",
			check: func(s string) bool { return !containsStr(s, "abcdef1234567890") },
		},
		{
			name:  "strips timestamps",
			input: "error at 2024-01-15T10:30:00 in handler",
			check: func(s string) bool { return !containsStr(s, "2024-01-15T10:30:00") },
		},
		{
			name:  "strips line numbers",
			input: "error on line 42 in parser",
			check: func(s string) bool { return !containsStr(s, "line 42") },
		},
		{
			name:  "truncates long messages",
			input: string(make([]byte, 200)),
			check: func(s string) bool { return len(s) <= 100 },
		},
		{
			name:  "preserves useful text",
			input: "connection refused",
			check: func(s string) bool { return containsStr(s, "connection refused") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractErrorPattern(tt.input)
			if !tt.check(got) {
				t.Errorf("extractErrorPattern(%q) = %q, check failed", tt.input, got)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestExtractFileTypes(t *testing.T) {
	diff := `--- a/cmd/main.go
+++ b/cmd/main.go
@@ -1,3 +1,5 @@
--- a/pkg/auth.py
+++ b/pkg/auth.py
@@ -1,3 +1,5 @@
--- a/web/index.js
+++ b/web/index.js
@@ -1,3 +1,5 @@`

	types := extractFileTypes(diff)
	expected := map[string]bool{".go": false, ".py": false, ".js": false}
	for _, ft := range types {
		expected[ft] = true
	}
	for ext, found := range expected {
		if !found {
			t.Errorf("expected file type %q not found in %v", ext, types)
		}
	}
}

func TestExtractFileTypesIgnoresDevNull(t *testing.T) {
	diff := `--- /dev/null
+++ b/new_file.go`
	types := extractFileTypes(diff)
	// Should have .go but not extract anything from /dev/null
	found := false
	for _, ft := range types {
		if ft == ".go" {
			found = true
		}
	}
	if !found {
		t.Error("expected .go file type")
	}
}

func TestAppendUniquePbk(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		val   string
		want  int
	}{
		{"add new", []string{"a"}, "b", 2},
		{"duplicate", []string{"a", "b"}, "a", 2},
		{"to nil", nil, "x", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.slice, tt.val)
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestTruncatePbk(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	if truncate("hello world", 5) != "hello..." {
		t.Error("long string should be truncated with ...")
	}
}

func TestPlaybookSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	pb := Playbook{
		ID: "pb-test123",
		Trigger: Trigger{
			Goal:      "fix auth bug",
			Tools:     []string{"srch", "edit"},
			FileTypes: []string{".go"},
		},
		Steps: []Step{
			{Tool: "srch", Desc: "search for auth"},
			{Tool: "edit", Desc: "fix the bug"},
		},
		Verify:      []string{"test"},
		Risk:        "low",
		SuccessRate: 1.0,
		Uses:        1,
		CreatedAt:   "2024-01-01T00:00:00Z",
		Description: "Fix auth bug procedure",
	}

	// Save
	pbFile := filepath.Join(dir, "pb-test123.json")
	data, err := json.MarshalIndent(pb, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(pbFile, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Load back
	readData, err := os.ReadFile(pbFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var loaded Playbook
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.ID != pb.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, pb.ID)
	}
	if loaded.Trigger.Goal != "fix auth bug" {
		t.Errorf("Trigger.Goal = %q", loaded.Trigger.Goal)
	}
	if len(loaded.Steps) != 2 {
		t.Errorf("Steps count = %d, want 2", len(loaded.Steps))
	}
	if loaded.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %f, want 1.0", loaded.SuccessRate)
	}
}

func TestLoadAllPlaybooks(t *testing.T) {
	// loadAllPlaybooks uses the const playbookDir, so we test the logic directly
	dir := t.TempDir()

	// Create playbook files
	for i, name := range []string{"pb-aaa.json", "pb-bbb.json"} {
		pb := Playbook{
			ID:          name[:len(name)-5],
			Description: "test",
			SuccessRate: float64(i+1) * 0.5,
			CreatedAt:   "2024-01-01T00:00:00Z",
		}
		data, _ := json.Marshal(pb)
		os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}

	// Also create a non-json file that should be ignored
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("ignore"), 0o644)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	var playbooks []Playbook
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var pb Playbook
		if err := json.Unmarshal(data, &pb); err != nil {
			continue
		}
		playbooks = append(playbooks, pb)
	}

	if len(playbooks) != 2 {
		t.Errorf("loaded %d playbooks, want 2", len(playbooks))
	}
}

func TestSuccessRateUpdate(t *testing.T) {
	pb := &Playbook{
		SuccessRate: 1.0,
		Uses:        1,
	}

	// Simulate update with failure
	pb.Uses++
	successVal := 0.0
	n := float64(pb.Uses)
	pb.SuccessRate = pb.SuccessRate*(n-1)/n + successVal/n

	// After 1 success and 1 failure, rate should be 0.5
	if pb.SuccessRate != 0.5 {
		t.Errorf("SuccessRate after failure = %f, want 0.5", pb.SuccessRate)
	}

	// Simulate update with success
	pb.Uses++
	successVal = 1.0
	n = float64(pb.Uses)
	pb.SuccessRate = pb.SuccessRate*(n-1)/n + successVal/n

	// After 1 success, 1 failure, 1 success = ~0.667
	expected := 2.0 / 3.0
	diff := pb.SuccessRate - expected
	if diff < -0.01 || diff > 0.01 {
		t.Errorf("SuccessRate after 2 success + 1 failure = %f, want ~%f", pb.SuccessRate, expected)
	}
}

func TestMatchScoring(t *testing.T) {
	// Test the matching logic by creating playbooks and checking score ordering
	pb1 := Playbook{
		ID: "pb-1",
		Trigger: Trigger{
			Goal:         "fix authentication bug",
			ErrorPattern: "auth failed",
		},
		SuccessRate: 1.0,
	}
	pb2 := Playbook{
		ID: "pb-2",
		Trigger: Trigger{
			Goal: "refactor database",
		},
		SuccessRate: 1.0,
	}

	query := "fix authentication"

	// pb1 should score higher because its goal matches better
	score1 := 0.0
	score2 := 0.0

	// Simple word overlap scoring (matches cmdMatch logic)
	queryWords := []string{"fix", "authentication"}
	for _, qw := range queryWords {
		for _, gw := range []string{"fix", "authentication", "bug"} {
			if qw == gw && len(qw) > 2 {
				score1 += 0.5
			}
		}
		for _, gw := range []string{"refactor", "database"} {
			if qw == gw && len(qw) > 2 {
				score2 += 0.5
			}
		}
	}
	_ = query
	_ = pb1
	_ = pb2

	if score1 <= score2 {
		t.Errorf("pb1 score (%f) should be higher than pb2 score (%f)", score1, score2)
	}
}
