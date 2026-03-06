package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckStaleness_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	dossPath := filepath.Join(tmp, ".clock", "doss.md")
	input := RefreshInput{Root: tmp, DiffLimit: 30, AgeMin: 1440}

	stale, reason := checkStaleness(dossPath, input)
	if !stale {
		t.Error("expected stale=true when dossier is missing")
	}
	if reason != "dossier not found" {
		t.Errorf("reason = %q, want %q", reason, "dossier not found")
	}
}

func TestCheckStaleness_OldFile(t *testing.T) {
	tmp := t.TempDir()
	clockDir := filepath.Join(tmp, ".clock")
	os.MkdirAll(clockDir, 0o755)
	dossPath := filepath.Join(clockDir, "doss.md")
	os.WriteFile(dossPath, []byte("old dossier"), 0o644)

	// Set mod time to 2 days ago
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(dossPath, oldTime, oldTime)

	input := RefreshInput{Root: tmp, DiffLimit: 30, AgeMin: 1440} // 24 hours
	stale, reason := checkStaleness(dossPath, input)
	if !stale {
		t.Error("expected stale=true when dossier is old")
	}
	if reason == "" {
		t.Error("expected a reason for staleness")
	}
}

func TestCheckStaleness_FreshFile(t *testing.T) {
	tmp := t.TempDir()
	clockDir := filepath.Join(tmp, ".clock")
	os.MkdirAll(clockDir, 0o755)
	dossPath := filepath.Join(clockDir, "doss.md")
	os.WriteFile(dossPath, []byte("fresh dossier"), 0o644)

	// Note: this test will try to run git diff, which will fail in a non-git dir.
	// checkStaleness treats git diff failure as stale, so we initialize a git repo.
	initGitRepo(t, tmp)

	input := RefreshInput{Root: tmp, DiffLimit: 30, AgeMin: 1440}
	stale, _ := checkStaleness(dossPath, input)
	if stale {
		// A fresh file with no git changes should not be stale.
		// However, it depends on git diff working. If git diff returns 0 files, it's fresh.
		t.Error("expected stale=false for fresh dossier with no git changes")
	}
}

func TestCheckStaleness_DiffLimitExceeded(t *testing.T) {
	// We can't easily mock git diff --stat, but we can test the function
	// indirectly by creating a git repo with many changed files.
	tmp := t.TempDir()
	clockDir := filepath.Join(tmp, ".clock")
	os.MkdirAll(clockDir, 0o755)
	dossPath := filepath.Join(clockDir, "doss.md")
	os.WriteFile(dossPath, []byte("dossier"), 0o644)

	initGitRepo(t, tmp)

	// Create and commit some files, then modify them
	for i := 0; i < 5; i++ {
		f := filepath.Join(tmp, "file"+string(rune('a'+i))+".txt")
		os.WriteFile(f, []byte("content"), 0o644)
	}
	gitCmd(t, tmp, "add", ".")
	gitCmd(t, tmp, "commit", "-m", "add files")

	// Modify files to create diffs
	for i := 0; i < 5; i++ {
		f := filepath.Join(tmp, "file"+string(rune('a'+i))+".txt")
		os.WriteFile(f, []byte("modified"), 0o644)
	}

	// With diff_limit=2, 5 changed files should trigger staleness
	input := RefreshInput{Root: tmp, DiffLimit: 2, AgeMin: 1440}
	stale, reason := checkStaleness(dossPath, input)
	if !stale {
		t.Error("expected stale=true when diff count exceeds limit")
	}
	if reason == "" {
		t.Error("expected reason to be set")
	}
}

func TestGitDiffCount_NoGitRepo(t *testing.T) {
	tmp := t.TempDir()
	_, err := gitDiffCount(tmp)
	if err == nil {
		t.Error("expected error when running git diff in non-git directory")
	}
}

func TestGitDiffCount_CleanRepo(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("hi"), 0o644)
	gitCmd(t, tmp, "add", ".")
	gitCmd(t, tmp, "commit", "-m", "init")

	count, err := gitDiffCount(tmp)
	if err != nil {
		t.Fatalf("gitDiffCount: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for clean repo", count)
	}
}

func TestGitDiffCount_WithChanges(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	f := filepath.Join(tmp, "f.txt")
	os.WriteFile(f, []byte("hello"), 0o644)
	gitCmd(t, tmp, "add", ".")
	gitCmd(t, tmp, "commit", "-m", "init")

	// Modify the file
	os.WriteFile(f, []byte("world"), 0o644)

	count, err := gitDiffCount(tmp)
	if err != nil {
		t.Fatalf("gitDiffCount: %v", err)
	}
	if count < 1 {
		t.Errorf("count = %d, want >= 1 for modified file", count)
	}
}

// initGitRepo initializes a git repository in the given directory.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "config", "user.name", "Test")
}

// gitCmd runs a git command in the given directory.
func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
