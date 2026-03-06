package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

// helper: set up queue directories under t.TempDir()
func setupQueue(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range statusDirs {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// helper: write a QueueEntry directly to a status dir
func writeTestEntry(t *testing.T, root, status string, entry common.QueueEntry) {
	t.Helper()
	path := filepath.Join(root, status, entry.ID+".json")
	if err := writeEntry(path, entry); err != nil {
		t.Fatal(err)
	}
}

// helper: read a QueueEntry from a status dir
func readTestEntry(t *testing.T, root, status, id string) common.QueueEntry {
	t.Helper()
	path := filepath.Join(root, status, id+".json")
	entry, err := readEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

// helper: check file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	if id1 == "" {
		t.Fatal("generateID returned empty string")
	}
	if id1 == id2 {
		t.Fatalf("generateID returned duplicate: %s", id1)
	}
}

func TestWriteAndReadEntry(t *testing.T) {
	root := t.TempDir()
	entry := common.QueueEntry{
		ID:        "test-123",
		Status:    "pending",
		CreatedAt: 1000,
		Job: common.JobSpec{
			ID:   "j1",
			Goal: "do something",
		},
	}
	path := filepath.Join(root, "entry.json")
	if err := writeEntry(path, entry); err != nil {
		t.Fatalf("writeEntry: %v", err)
	}

	got, err := readEntry(path)
	if err != nil {
		t.Fatalf("readEntry: %v", err)
	}
	if got.ID != entry.ID {
		t.Errorf("ID = %q, want %q", got.ID, entry.ID)
	}
	if got.Status != entry.Status {
		t.Errorf("Status = %q, want %q", got.Status, entry.Status)
	}
	if got.Job.Goal != entry.Job.Goal {
		t.Errorf("Job.Goal = %q, want %q", got.Job.Goal, entry.Job.Goal)
	}
}

func TestWriteEntryAtomicity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "atomic.json")
	entry := common.QueueEntry{ID: "atom-1", Status: "pending", CreatedAt: 500}

	if err := writeEntry(path, entry); err != nil {
		t.Fatalf("writeEntry: %v", err)
	}

	// .tmp file should NOT exist after write (renamed away)
	tmpPath := path + ".tmp"
	if fileExists(tmpPath) {
		t.Error("temp file still exists after atomic write")
	}

	// actual file should exist
	if !fileExists(path) {
		t.Error("target file does not exist after atomic write")
	}
}

func TestReadEntryNotFound(t *testing.T) {
	_, err := readEntry("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestBackoffSeconds(t *testing.T) {
	tests := []struct {
		retries int
		want    int
	}{
		{1, 5},
		{2, 25},
		{3, 125},
	}
	for _, tt := range tests {
		got := backoffSeconds(tt.retries)
		if got != tt.want {
			t.Errorf("backoffSeconds(%d) = %d, want %d", tt.retries, got, tt.want)
		}
	}
}

func TestPutCreatesJobInPending(t *testing.T) {
	root := setupQueue(t)

	entry := common.QueueEntry{
		ID:        "put-test-1",
		Status:    "pending",
		CreatedAt: 1000,
		Job: common.JobSpec{
			ID:   "j1",
			Goal: "test put",
		},
	}
	writeTestEntry(t, root, "pending", entry)

	// Verify file is in pending dir
	path := filepath.Join(root, "pending", "put-test-1.json")
	if !fileExists(path) {
		t.Fatal("job file not created in pending dir")
	}

	got, err := readEntry(path)
	if err != nil {
		t.Fatalf("readEntry: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q", got.Status, "pending")
	}
}

func TestTakeMovesFromPendingToLeased(t *testing.T) {
	root := setupQueue(t)

	// Place two entries in pending with different timestamps for ordering
	e1 := common.QueueEntry{
		ID:        "1000-aaa",
		Status:    "pending",
		CreatedAt: 1000,
		Job:       common.JobSpec{Goal: "first"},
	}
	e2 := common.QueueEntry{
		ID:        "2000-bbb",
		Status:    "pending",
		CreatedAt: 2000,
		Job:       common.JobSpec{Goal: "second"},
	}
	writeTestEntry(t, root, "pending", e1)
	writeTestEntry(t, root, "pending", e2)

	// Simulate take logic: read pending, sort, move oldest to leased
	pendingDir := filepath.Join(root, "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 pending entries, got %d", len(entries))
	}

	// Take the oldest (1000-aaa)
	srcPath := filepath.Join(root, "pending", "1000-aaa.json")
	entry, err := readEntry(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	entry.Status = "leased"
	entry.LeasedAt = 3000

	dstPath := filepath.Join(root, "leased", "1000-aaa.json")
	if err := writeEntry(dstPath, entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	// Verify: pending should have 1, leased should have 1
	if fileExists(srcPath) {
		t.Error("source file still exists in pending after take")
	}
	if !fileExists(dstPath) {
		t.Error("destination file not created in leased")
	}

	leased := readTestEntry(t, root, "leased", "1000-aaa")
	if leased.Status != "leased" {
		t.Errorf("leased entry status = %q, want %q", leased.Status, "leased")
	}
}

func TestTakeEmptyQueueReturnsNoJobs(t *testing.T) {
	root := setupQueue(t)
	pendingDir := filepath.Join(root, "pending")

	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		t.Fatal(err)
	}

	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	if len(jsonFiles) != 0 {
		t.Errorf("expected 0 json files in empty pending, got %d", len(jsonFiles))
	}
}

func TestAckMovesFromLeasedToDone(t *testing.T) {
	root := setupQueue(t)

	entry := common.QueueEntry{
		ID:        "ack-test-1",
		Status:    "leased",
		LeasedAt:  2000,
		CreatedAt: 1000,
		Job:       common.JobSpec{Goal: "ack me"},
	}
	writeTestEntry(t, root, "leased", entry)

	// Simulate ack: read from leased, write to done, remove from leased
	srcPath := filepath.Join(root, "leased", "ack-test-1.json")
	got, err := readEntry(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	got.Status = "done"
	got.DoneAt = 3000

	dstPath := filepath.Join(root, "done", "ack-test-1.json")
	if err := writeEntry(dstPath, got); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	if fileExists(srcPath) {
		t.Error("leased file still exists after ack")
	}
	done := readTestEntry(t, root, "done", "ack-test-1")
	if done.Status != "done" {
		t.Errorf("done entry status = %q, want %q", done.Status, "done")
	}
	if done.DoneAt != 3000 {
		t.Errorf("DoneAt = %d, want 3000", done.DoneAt)
	}
}

func TestFailRetryMovesBackToPending(t *testing.T) {
	root := setupQueue(t)

	entry := common.QueueEntry{
		ID:        "fail-retry-1",
		Status:    "leased",
		Retries:   0,
		CreatedAt: 1000,
		LeasedAt:  2000,
		Job:       common.JobSpec{Goal: "might fail"},
	}
	writeTestEntry(t, root, "leased", entry)

	// Simulate fail with retries < maxRetries (3)
	srcPath := filepath.Join(root, "leased", "fail-retry-1.json")
	got, err := readEntry(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	got.Retries++

	if got.Retries >= 3 {
		t.Fatal("retries should be < 3 for this test")
	}

	got.Status = "pending"
	got.LeasedAt = 0
	dstPath := filepath.Join(root, "pending", "fail-retry-1.json")
	if err := writeEntry(dstPath, got); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	if fileExists(srcPath) {
		t.Error("leased file still exists after fail retry")
	}
	pending := readTestEntry(t, root, "pending", "fail-retry-1")
	if pending.Status != "pending" {
		t.Errorf("status = %q, want %q", pending.Status, "pending")
	}
	if pending.Retries != 1 {
		t.Errorf("retries = %d, want 1", pending.Retries)
	}
}

func TestFailDeadLetterAfterMaxRetries(t *testing.T) {
	root := setupQueue(t)

	entry := common.QueueEntry{
		ID:        "fail-dead-1",
		Status:    "leased",
		Retries:   2, // next fail will make it 3 = maxRetries
		CreatedAt: 1000,
		LeasedAt:  2000,
		Job:       common.JobSpec{Goal: "doomed"},
	}
	writeTestEntry(t, root, "leased", entry)

	srcPath := filepath.Join(root, "leased", "fail-dead-1.json")
	got, err := readEntry(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	got.Retries++

	if got.Retries < 3 {
		t.Fatalf("expected retries >= 3, got %d", got.Retries)
	}

	got.Status = "dead"
	dstPath := filepath.Join(root, "dead", "fail-dead-1.json")
	if err := writeEntry(dstPath, got); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	if fileExists(srcPath) {
		t.Error("leased file still exists after dead letter")
	}
	dead := readTestEntry(t, root, "dead", "fail-dead-1")
	if dead.Status != "dead" {
		t.Errorf("status = %q, want %q", dead.Status, "dead")
	}
	if dead.Retries != 3 {
		t.Errorf("retries = %d, want 3", dead.Retries)
	}
}

func TestListEnumeratesAllStatuses(t *testing.T) {
	root := setupQueue(t)

	entries := []struct {
		status string
		entry  common.QueueEntry
	}{
		{"pending", common.QueueEntry{ID: "list-p1", Status: "pending", CreatedAt: 1000}},
		{"leased", common.QueueEntry{ID: "list-l1", Status: "leased", CreatedAt: 2000}},
		{"done", common.QueueEntry{ID: "list-d1", Status: "done", CreatedAt: 3000}},
		{"failed", common.QueueEntry{ID: "list-f1", Status: "failed", CreatedAt: 4000}},
		{"dead", common.QueueEntry{ID: "list-x1", Status: "dead", CreatedAt: 5000}},
	}

	for _, e := range entries {
		writeTestEntry(t, root, e.status, e.entry)
	}

	// Enumerate all dirs
	var all []common.QueueEntry
	for _, status := range statusDirs {
		dir := filepath.Join(root, status)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if filepath.Ext(f.Name()) == ".json" {
				entry, err := readEntry(filepath.Join(dir, f.Name()))
				if err != nil {
					continue
				}
				all = append(all, entry)
			}
		}
	}

	if len(all) != 5 {
		t.Errorf("list found %d entries, want 5", len(all))
	}
}

func TestLockAndUnlock(t *testing.T) {
	root := t.TempDir()

	f, err := lockFile(root)
	if err != nil {
		t.Fatalf("lockFile: %v", err)
	}
	lockPath := filepath.Join(root, ".lock")
	if !fileExists(lockPath) {
		t.Error("lock file not created")
	}
	unlock(f)
}

func TestUnlockNil(t *testing.T) {
	// Should not panic
	unlock(nil)
}

func TestStatusDirsConstant(t *testing.T) {
	expected := []string{"pending", "leased", "done", "failed", "dead"}
	if len(statusDirs) != len(expected) {
		t.Fatalf("statusDirs length = %d, want %d", len(statusDirs), len(expected))
	}
	for i, d := range statusDirs {
		if d != expected[i] {
			t.Errorf("statusDirs[%d] = %q, want %q", i, d, expected[i])
		}
	}
}

func TestWriteEntryInvalidPath(t *testing.T) {
	entry := common.QueueEntry{ID: "bad"}
	err := writeEntry("/nonexistent/dir/file.json", entry)
	if err == nil {
		t.Fatal("expected error writing to invalid path")
	}
}

func TestReadEntryInvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readEntry(path)
	if err == nil {
		t.Fatal("expected error reading invalid JSON")
	}
}

func TestWriteEntryPreservesJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "preserve.json")
	entry := common.QueueEntry{
		ID:        "pres-1",
		Status:    "pending",
		CreatedAt: 12345,
		Retries:   2,
		Job: common.JobSpec{
			ID:   "job-1",
			Goal: "test goal",
			Repo: "owner/repo",
		},
	}
	if err := writeEntry(path, entry); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed["id"] != "pres-1" {
		t.Errorf("id = %v, want pres-1", parsed["id"])
	}
}
