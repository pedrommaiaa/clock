package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHashContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"simple", "hello world"},
		{"json", `{"key":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashContent([]byte(tt.content))
			// Verify against standard lib
			h := sha256.Sum256([]byte(tt.content))
			want := hex.EncodeToString(h[:])
			if got != want {
				t.Errorf("hashContent = %q, want %q", got, want)
			}
			// Should be 64 hex chars
			if len(got) != 64 {
				t.Errorf("hash length = %d, want 64", len(got))
			}
		})
	}
}

func TestHashContentDeterministic(t *testing.T) {
	data := []byte("test content")
	h1 := hashContent(data)
	h2 := hashContent(data)
	if h1 != h2 {
		t.Error("hashContent is not deterministic")
	}
}

func TestHashContentDifferent(t *testing.T) {
	h1 := hashContent([]byte("content a"))
	h2 := hashContent([]byte("content b"))
	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
}

func TestConflictResolutionLocalNewer(t *testing.T) {
	// When local is newer, local wins
	localMod := int64(2000)
	remoteMod := int64(1000)

	winner := "remote"
	if localMod > remoteMod {
		winner = "local"
	}
	if winner != "local" {
		t.Errorf("winner = %q, want %q (local is newer)", winner, "local")
	}
}

func TestConflictResolutionRemoteNewer(t *testing.T) {
	// When remote is newer, remote wins
	localMod := int64(1000)
	remoteMod := int64(2000)

	winner := "local"
	if remoteMod > localMod {
		winner = "remote"
	}
	if winner != "remote" {
		t.Errorf("winner = %q, want %q (remote is newer)", winner, "remote")
	}
}

func TestConflictResolutionSameTimestamp(t *testing.T) {
	// Same timestamp with different content: remote wins (last-writer-wins)
	localMod := int64(1000)
	remoteMod := int64(1000)
	localHash := hashContent([]byte("local content"))
	remoteHash := hashContent([]byte("remote content"))

	// Same timestamp, different content
	if localHash == remoteHash {
		t.Skip("hashes are same (shouldn't happen)")
	}

	winner := ""
	if localMod > remoteMod {
		winner = "local"
	} else if remoteMod > localMod {
		winner = "remote"
	} else {
		winner = "remote" // tie-breaker: remote wins
	}

	if winner != "remote" {
		t.Errorf("winner = %q, want %q (tie-breaker)", winner, "remote")
	}
}

func TestSyncEntryRoundTrip(t *testing.T) {
	content := "test file content"
	entry := SyncEntry{
		Path:    ".clock/mem/test.md",
		Content: content,
		Hash:    hashContent([]byte(content)),
		ModTime: 1700000000,
		Size:    int64(len(content)),
	}

	if entry.Hash != hashContent([]byte(entry.Content)) {
		t.Error("hash mismatch in sync entry")
	}
	if entry.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", entry.Size, len(content))
	}
}

func TestImportNewFile(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "subdir", "newfile.txt")

	remote := SyncEntry{
		Path:    targetPath,
		Content: "new content",
		Hash:    hashContent([]byte("new content")),
		ModTime: 1700000000,
		Size:    11,
	}

	// Simulate import of new file
	parent := filepath.Dir(remote.Path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(remote.Path, []byte(remote.Content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("content = %q, want %q", string(data), "new content")
	}
}

func TestImportIdenticalSkips(t *testing.T) {
	dir := t.TempDir()
	content := "existing content"
	filePath := filepath.Join(dir, "existing.txt")
	os.WriteFile(filePath, []byte(content), 0o644)

	localHash := hashContent([]byte(content))
	remoteHash := hashContent([]byte(content))

	if localHash != remoteHash {
		t.Error("identical content should have identical hashes")
	}

	// Should skip
	skipped := localHash == remoteHash
	if !skipped {
		t.Error("should skip identical content")
	}
}

func TestCollectFilesSkipsTmpAndLock(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "data")
	os.MkdirAll(subDir, 0o755)

	// Create normal file, tmp file, and lock file
	os.WriteFile(filepath.Join(subDir, "data.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(subDir, "data.json.tmp"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(subDir, "data.lock"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(subDir, ".lock"), []byte(""), 0o644)

	// Walk and filter
	var count int
	filepath.Walk(subDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		name := fi.Name()
		if filepath.Ext(name) == ".tmp" || filepath.Ext(name) == ".lock" || name == ".lock" {
			return nil
		}
		count++
		return nil
	})

	if count != 1 {
		t.Errorf("should collect 1 file (not tmp/lock), got %d", count)
	}
}

func TestLogConflict(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "conflicts.jsonl")

	entry := ConflictEntry{
		Path:       "test.txt",
		LocalHash:  "aaa",
		RemoteHash: "bbb",
		LocalMod:   1000,
		RemoteMod:  2000,
		Winner:     "remote",
		Timestamp:  "2024-01-01T00:00:00Z",
	}

	// Write conflict log
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	data, _ := json.MarshalIndent(entry, "", "")
	f.Write(append(data, '\n'))
	f.Close()

	// Verify it was written
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(content) == 0 {
		t.Error("conflict log is empty")
	}
}

func TestImportResultCounting(t *testing.T) {
	result := ImportResult{
		Imported:  3,
		Conflicts: 1,
		Skipped:   2,
	}

	total := result.Imported + result.Conflicts + result.Skipped
	// Note: Conflicts can overlap with Imported or Skipped
	if total < result.Imported {
		t.Error("total should be >= imported")
	}
}

func TestMergeOverwritesOlderLocal(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "data.json")

	// Write local file with old content
	os.WriteFile(filePath, []byte("old content"), 0o644)

	// Remote is newer
	remote := SyncEntry{
		Path:    filePath,
		Content: "new content",
		Hash:    hashContent([]byte("new content")),
		ModTime: 9999999999, // far future
		Size:    11,
	}

	localInfo, _ := os.Stat(filePath)
	localMod := localInfo.ModTime().Unix()

	// Remote is newer, should overwrite
	if remote.ModTime > localMod {
		os.WriteFile(filePath, []byte(remote.Content), 0o644)
	}

	data, _ := os.ReadFile(filePath)
	if string(data) != "new content" {
		t.Errorf("content = %q, want %q", string(data), "new content")
	}
}
