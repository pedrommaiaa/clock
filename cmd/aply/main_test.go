package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseDiffStats(t *testing.T) {
	tests := []struct {
		name        string
		diff        string
		wantFiles   []string
		wantAdded   int
		wantDeleted int
	}{
		{
			name: "single file add",
			diff: `--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,3 @@
 package main
+import "fmt"
`,
			wantFiles:   []string{"foo.go"},
			wantAdded:   1,
			wantDeleted: 0,
		},
		{
			name: "single file modify",
			diff: `--- a/bar.go
+++ b/bar.go
@@ -1,3 +1,3 @@
 package main
-func old() {}
+func new() int { return 1 }
`,
			wantFiles:   []string{"bar.go"},
			wantAdded:   1,
			wantDeleted: 1,
		},
		{
			name: "multi file",
			diff: `--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,3 @@
 package main
+import "fmt"
--- a/bar.go
+++ b/bar.go
@@ -1,2 +1,2 @@
 package main
-func old() {}
+func new() {}
`,
			wantFiles:   []string{"foo.go", "bar.go"},
			wantAdded:   2,
			wantDeleted: 1,
		},
		{
			name: "new file",
			diff: `--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+
+func hello() {}
`,
			wantFiles:   []string{"new.go"},
			wantAdded:   3,
			wantDeleted: 0,
		},
		{
			name: "deleted file",
			diff: `--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package main
-
-func gone() {}
`,
			wantFiles:   nil,
			wantAdded:   0,
			wantDeleted: 3,
		},
		{
			name:        "empty diff",
			diff:        "",
			wantFiles:   nil,
			wantAdded:   0,
			wantDeleted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, added, deleted := parseDiffStats(tt.diff)
			if len(files) != len(tt.wantFiles) {
				t.Errorf("files count = %d, want %d; files = %v", len(files), len(tt.wantFiles), files)
			} else {
				for i, f := range files {
					if f != tt.wantFiles[i] {
						t.Errorf("files[%d] = %q, want %q", i, f, tt.wantFiles[i])
					}
				}
			}
			if added != tt.wantAdded {
				t.Errorf("added = %d, want %d", added, tt.wantAdded)
			}
			if deleted != tt.wantDeleted {
				t.Errorf("deleted = %d, want %d", deleted, tt.wantDeleted)
			}
		})
	}
}

func TestParseDiffStats_DuplicateFiles(t *testing.T) {
	diff := `--- a/foo.go
+++ b/foo.go
@@ -1 +1 @@
-a
+b
--- a/foo.go
+++ b/foo.go
@@ -5 +5 @@
-c
+d
`
	files, _, _ := parseDiffStats(diff)
	if len(files) != 1 {
		t.Errorf("duplicate file should be counted once, got %d", len(files))
	}
}

func TestIsDirty_NoGit(t *testing.T) {
	// In a temp dir with no git repo, isDirty should return false
	// (because git status fails).
	origDir, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	if isDirty() {
		t.Error("isDirty() should return false when git is not initialized")
	}
}

func TestCreateCheckpoint_UnknownMode(t *testing.T) {
	err := createCheckpoint("bogus", "abc123", "test")
	if err == nil {
		t.Error("expected error for unknown checkpoint mode")
	}
}

func TestCreateCheckpoint_CommitCleanTree(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	runGit(t, tmp, "init")
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")

	os.WriteFile(filepath.Join(tmp, "README"), []byte("hello"), 0o644)
	runGit(t, tmp, "add", ".")
	runGit(t, tmp, "commit", "-m", "init")

	// commit mode on a clean tree should be a no-op (no error)
	err := createCheckpoint("commit", "test123", "clean tree")
	if err != nil {
		t.Errorf("createCheckpoint(commit) on clean tree: %v", err)
	}
}

func TestCreateCheckpoint_AutoCleanTree(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	runGit(t, tmp, "init")
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")

	os.WriteFile(filepath.Join(tmp, "README"), []byte("hello"), 0o644)
	runGit(t, tmp, "add", ".")
	runGit(t, tmp, "commit", "-m", "init")

	// auto mode on a clean tree should be a no-op
	err := createCheckpoint("auto", "test123", "msg")
	if err != nil {
		t.Errorf("createCheckpoint(auto) on clean tree: %v", err)
	}
}

func TestApplyDiff_InvalidPatch(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	runGit(t, tmp, "init")

	// Write an invalid patch file
	patchFile := filepath.Join(tmp, "bad.patch")
	os.WriteFile(patchFile, []byte("this is not a valid patch"), 0o644)

	err := applyDiff(patchFile)
	if err == nil {
		t.Error("applyDiff with invalid patch should fail")
	}
}

func TestApplyDiff_ValidPatch(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	runGit(t, tmp, "init")
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")

	// Create initial file and commit
	initial := filepath.Join(tmp, "hello.txt")
	os.WriteFile(initial, []byte("hello\n"), 0o644)
	runGit(t, tmp, "add", ".")
	runGit(t, tmp, "commit", "-m", "init")

	// Create a valid patch
	patch := `--- a/hello.txt
+++ b/hello.txt
@@ -1 +1 @@
-hello
+world
`
	patchFile := filepath.Join(tmp, "fix.patch")
	os.WriteFile(patchFile, []byte(patch), 0o644)

	err := applyDiff(patchFile)
	if err != nil {
		t.Fatalf("applyDiff with valid patch: %v", err)
	}

	// Verify the file was modified
	content, _ := os.ReadFile(initial)
	if string(content) != "world\n" {
		t.Errorf("file content = %q, want %q", string(content), "world\n")
	}
}

// runGit is a test helper that runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
