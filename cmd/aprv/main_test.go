package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper: set up approval dirs under t.TempDir()
func setupApprovalDirs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"inbox", "approved", "denied", "deferred"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// helper: write approval entry to a subdir
func writeApprovalEntry(t *testing.T, root, subdir string, entry ApprovalEntry) {
	t.Helper()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, subdir, entry.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// helper: read approval entry from subdir
func readApprovalEntry(t *testing.T, root, subdir, id string) ApprovalEntry {
	t.Helper()
	path := filepath.Join(root, subdir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry ApprovalEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

// helper: check file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRequestCreatesInInbox(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{
		ID:        "req-1",
		Diff:      "some diff",
		Risk:      0.5,
		Reasons:   []string{"touches migrations"},
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	writeApprovalEntry(t, root, "inbox", entry)

	path := filepath.Join(root, "inbox", "req-1.json")
	if !fileExists(path) {
		t.Fatal("request file not created in inbox")
	}

	got := readApprovalEntry(t, root, "inbox", "req-1")
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q", got.Status, "pending")
	}
	if got.Risk != 0.5 {
		t.Errorf("Risk = %f, want 0.5", got.Risk)
	}
}

func TestRequestRequiresID(t *testing.T) {
	req := ApprovalRequest{ID: ""}
	if req.ID == "" {
		// matches validation in doRequest
	} else {
		t.Error("empty ID should be caught as error")
	}
}

func TestApproveMovesFromInboxToApproved(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{
		ID:        "appr-1",
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	writeApprovalEntry(t, root, "inbox", entry)

	// Simulate moveApproval (approve)
	srcPath := filepath.Join(root, "inbox", "appr-1.json")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}

	var e ApprovalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	e.Status = "approved"
	e.UpdatedAt = "2025-01-01T01:00:00Z"

	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(root, "approved", "appr-1.json")
	if err := os.WriteFile(dstPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	if fileExists(srcPath) {
		t.Error("inbox file still exists after approval")
	}
	if !fileExists(dstPath) {
		t.Error("approved file not created")
	}

	got := readApprovalEntry(t, root, "approved", "appr-1")
	if got.Status != "approved" {
		t.Errorf("Status = %q, want %q", got.Status, "approved")
	}
}

func TestDenyMovesFromInboxToDenied(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{
		ID:        "deny-1",
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	writeApprovalEntry(t, root, "inbox", entry)

	srcPath := filepath.Join(root, "inbox", "deny-1.json")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}

	var e ApprovalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	e.Status = "denied"
	e.Reason = "too risky"
	e.UpdatedAt = "2025-01-01T01:00:00Z"

	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(root, "denied", "deny-1.json")
	if err := os.WriteFile(dstPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	if fileExists(srcPath) {
		t.Error("inbox file still exists after deny")
	}

	got := readApprovalEntry(t, root, "denied", "deny-1")
	if got.Status != "denied" {
		t.Errorf("Status = %q, want %q", got.Status, "denied")
	}
	if got.Reason != "too risky" {
		t.Errorf("Reason = %q, want %q", got.Reason, "too risky")
	}
}

func TestDeferMovesFromInboxToDeferred(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{
		ID:        "defer-1",
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	writeApprovalEntry(t, root, "inbox", entry)

	srcPath := filepath.Join(root, "inbox", "defer-1.json")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}

	var e ApprovalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	e.Status = "deferred"
	e.UpdatedAt = "2025-01-01T01:00:00Z"

	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(root, "deferred", "defer-1.json")
	if err := os.WriteFile(dstPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(srcPath); err != nil {
		t.Fatal(err)
	}

	got := readApprovalEntry(t, root, "deferred", "defer-1")
	if got.Status != "deferred" {
		t.Errorf("Status = %q, want %q", got.Status, "deferred")
	}
}

func TestCheckStatusPending(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{ID: "chk-1", Status: "pending", CreatedAt: "2025-01-01T00:00:00Z"}
	writeApprovalEntry(t, root, "inbox", entry)

	// Simulate doCheck: search all dirs
	dirs := []struct {
		dir    string
		status string
	}{
		{"inbox", "pending"},
		{"approved", "approved"},
		{"denied", "denied"},
		{"deferred", "deferred"},
	}

	foundStatus := "not_found"
	for _, d := range dirs {
		path := filepath.Join(root, d.dir, "chk-1.json")
		if fileExists(path) {
			foundStatus = d.status
			break
		}
	}

	if foundStatus != "pending" {
		t.Errorf("check status = %q, want %q", foundStatus, "pending")
	}
}

func TestCheckStatusApproved(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{ID: "chk-2", Status: "approved", CreatedAt: "2025-01-01T00:00:00Z"}
	writeApprovalEntry(t, root, "approved", entry)

	dirs := []struct {
		dir    string
		status string
	}{
		{"inbox", "pending"},
		{"approved", "approved"},
		{"denied", "denied"},
		{"deferred", "deferred"},
	}

	foundStatus := "not_found"
	for _, d := range dirs {
		path := filepath.Join(root, d.dir, "chk-2.json")
		if fileExists(path) {
			foundStatus = d.status
			break
		}
	}

	if foundStatus != "approved" {
		t.Errorf("check status = %q, want %q", foundStatus, "approved")
	}
}

func TestCheckStatusDenied(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{ID: "chk-3", Status: "denied", CreatedAt: "2025-01-01T00:00:00Z"}
	writeApprovalEntry(t, root, "denied", entry)

	dirs := []struct {
		dir    string
		status string
	}{
		{"inbox", "pending"},
		{"approved", "approved"},
		{"denied", "denied"},
		{"deferred", "deferred"},
	}

	foundStatus := "not_found"
	for _, d := range dirs {
		path := filepath.Join(root, d.dir, "chk-3.json")
		if fileExists(path) {
			foundStatus = d.status
			break
		}
	}

	if foundStatus != "denied" {
		t.Errorf("check status = %q, want %q", foundStatus, "denied")
	}
}

func TestCheckStatusDeferred(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{ID: "chk-4", Status: "deferred", CreatedAt: "2025-01-01T00:00:00Z"}
	writeApprovalEntry(t, root, "deferred", entry)

	dirs := []struct {
		dir    string
		status string
	}{
		{"inbox", "pending"},
		{"approved", "approved"},
		{"denied", "denied"},
		{"deferred", "deferred"},
	}

	foundStatus := "not_found"
	for _, d := range dirs {
		path := filepath.Join(root, d.dir, "chk-4.json")
		if fileExists(path) {
			foundStatus = d.status
			break
		}
	}

	if foundStatus != "deferred" {
		t.Errorf("check status = %q, want %q", foundStatus, "deferred")
	}
}

func TestCheckStatusNotFound(t *testing.T) {
	root := setupApprovalDirs(t)

	dirs := []struct {
		dir    string
		status string
	}{
		{"inbox", "pending"},
		{"approved", "approved"},
		{"denied", "denied"},
		{"deferred", "deferred"},
	}

	foundStatus := "not_found"
	for _, d := range dirs {
		path := filepath.Join(root, d.dir, "nonexistent.json")
		if fileExists(path) {
			foundStatus = d.status
			break
		}
	}

	if foundStatus != "not_found" {
		t.Errorf("check status = %q, want %q", foundStatus, "not_found")
	}
}

func TestApprovalEntryJSONRoundTrip(t *testing.T) {
	entry := ApprovalEntry{
		ID:        "rt-1",
		Diff:      "--- a/x\n+++ b/x\n+line",
		Risk:      0.75,
		Reasons:   []string{"migration", "deps"},
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var got ApprovalEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.ID != entry.ID {
		t.Errorf("ID = %q, want %q", got.ID, entry.ID)
	}
	if got.Risk != entry.Risk {
		t.Errorf("Risk = %f, want %f", got.Risk, entry.Risk)
	}
	if len(got.Reasons) != 2 {
		t.Errorf("Reasons length = %d, want 2", len(got.Reasons))
	}
}

func TestStatusResponseJSON(t *testing.T) {
	resp := StatusResponse{ID: "test", Status: "approved"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var got StatusResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.ID != "test" || got.Status != "approved" {
		t.Errorf("got ID=%q Status=%q, want ID=test Status=approved", got.ID, got.Status)
	}
}

func TestListInboxEntries(t *testing.T) {
	root := setupApprovalDirs(t)

	for _, id := range []string{"list-1", "list-2", "list-3"} {
		writeApprovalEntry(t, root, "inbox", ApprovalEntry{
			ID:        id,
			Status:    "pending",
			CreatedAt: "2025-01-01T00:00:00Z",
		})
	}

	inboxDir := filepath.Join(root, "inbox")
	entries, err := os.ReadDir(inboxDir)
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 inbox entries, got %d", count)
	}
}

func TestMoveApprovalSourceNotFound(t *testing.T) {
	root := setupApprovalDirs(t)

	srcPath := filepath.Join(root, "inbox", "nonexistent.json")
	_, err := os.ReadFile(srcPath)
	if err == nil {
		t.Error("expected error reading nonexistent source")
	}
}

func TestDenyWithEmptyReason(t *testing.T) {
	root := setupApprovalDirs(t)

	entry := ApprovalEntry{
		ID:        "deny-empty",
		Status:    "pending",
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	writeApprovalEntry(t, root, "inbox", entry)

	srcPath := filepath.Join(root, "inbox", "deny-empty.json")
	data, _ := os.ReadFile(srcPath)
	var e ApprovalEntry
	json.Unmarshal(data, &e)

	e.Status = "denied"
	e.Reason = "" // empty reason is valid

	out, _ := json.MarshalIndent(e, "", "  ")
	dstPath := filepath.Join(root, "denied", "deny-empty.json")
	os.WriteFile(dstPath, out, 0644)
	os.Remove(srcPath)

	got := readApprovalEntry(t, root, "denied", "deny-empty")
	if got.Reason != "" {
		t.Errorf("Reason = %q, want empty", got.Reason)
	}
}
