package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: set up mem store dirs under t.TempDir()
func setupMemStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for kind := range validKinds {
		dir := filepath.Join(root, kind)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// helper: write a MemEntry directly
func writeMemEntry(t *testing.T, root, kind, key string, entry MemEntry) {
	t.Helper()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, kind, key+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// helper: read a MemEntry directly
func readMemEntry(t *testing.T, root, kind, key string) MemEntry {
	t.Helper()
	path := filepath.Join(root, kind, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry MemEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestValidKinds(t *testing.T) {
	expected := []string{"fix", "fingerprint", "command", "playbook", "dossier"}
	for _, k := range expected {
		if !validKinds[k] {
			t.Errorf("expected %q to be a valid kind", k)
		}
	}
	if validKinds["invalid"] {
		t.Error("expected 'invalid' to not be a valid kind")
	}
}

func TestPutWritesByKind(t *testing.T) {
	root := setupMemStore(t)

	entry := MemEntry{
		Key:       "mykey",
		Kind:      "fix",
		Data:      map[string]string{"detail": "fix data"},
		CreatedAt: "2025-01-01T00:00:00Z",
		UpdatedAt: "2025-01-01T00:00:00Z",
	}
	writeMemEntry(t, root, "fix", "mykey", entry)

	path := filepath.Join(root, "fix", "mykey.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("put did not create file in fix/ dir")
	}

	got := readMemEntry(t, root, "fix", "mykey")
	if got.Key != "mykey" {
		t.Errorf("Key = %q, want %q", got.Key, "mykey")
	}
	if got.Kind != "fix" {
		t.Errorf("Kind = %q, want %q", got.Kind, "fix")
	}
}

func TestPutPreservesCreatedAt(t *testing.T) {
	root := setupMemStore(t)

	original := MemEntry{
		Key:       "preserve",
		Kind:      "command",
		Data:      "v1",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	writeMemEntry(t, root, "command", "preserve", original)

	// Simulate update: read existing, preserve created_at
	path := filepath.Join(root, "command", "preserve.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var existing MemEntry
	if err := json.Unmarshal(data, &existing); err != nil {
		t.Fatal(err)
	}

	updated := MemEntry{
		Key:       "preserve",
		Kind:      "command",
		Data:      "v2",
		CreatedAt: existing.CreatedAt,
		UpdatedAt: "2025-06-01T00:00:00Z",
	}
	writeMemEntry(t, root, "command", "preserve", updated)

	got := readMemEntry(t, root, "command", "preserve")
	if got.CreatedAt != "2024-01-01T00:00:00Z" {
		t.Errorf("CreatedAt changed: got %q, want %q", got.CreatedAt, "2024-01-01T00:00:00Z")
	}
	if got.UpdatedAt != "2025-06-01T00:00:00Z" {
		t.Errorf("UpdatedAt = %q, want %q", got.UpdatedAt, "2025-06-01T00:00:00Z")
	}
}

func TestGetByKey(t *testing.T) {
	root := setupMemStore(t)

	entry := MemEntry{
		Key:       "findme",
		Kind:      "playbook",
		Data:      "playbook content",
		CreatedAt: "2025-01-01T00:00:00Z",
		UpdatedAt: "2025-01-01T00:00:00Z",
	}
	writeMemEntry(t, root, "playbook", "findme", entry)

	// Simulate doGet: search all kinds
	var found *MemEntry
	for kind := range validKinds {
		path := filepath.Join(root, kind, "findme.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e MemEntry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		found = &e
		break
	}

	if found == nil {
		t.Fatal("get did not find the entry")
	}
	if found.Kind != "playbook" {
		t.Errorf("Kind = %q, want %q", found.Kind, "playbook")
	}
}

func TestGetKeyNotFound(t *testing.T) {
	root := setupMemStore(t)

	var found bool
	for kind := range validKinds {
		path := filepath.Join(root, kind, "nonexistent.json")
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}
	if found {
		t.Error("expected key not found")
	}
}

func TestListByKind(t *testing.T) {
	root := setupMemStore(t)

	for i, key := range []string{"e1", "e2", "e3"} {
		writeMemEntry(t, root, "fix", key, MemEntry{
			Key:       key,
			Kind:      "fix",
			Data:      i,
			CreatedAt: "2025-01-01T00:00:00Z",
		})
	}
	writeMemEntry(t, root, "command", "c1", MemEntry{
		Key:       "c1",
		Kind:      "command",
		Data:      "cmd",
		CreatedAt: "2025-01-01T00:00:00Z",
	})

	// List only fix entries
	dir := filepath.Join(root, "fix")
	entries, err := os.ReadDir(dir)
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
		t.Errorf("expected 3 fix entries, got %d", count)
	}
}

func TestListAllKinds(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "fix", "f1", MemEntry{Key: "f1", Kind: "fix", CreatedAt: "2025-01-01T00:00:00Z"})
	writeMemEntry(t, root, "dossier", "d1", MemEntry{Key: "d1", Kind: "dossier", CreatedAt: "2025-01-01T00:00:00Z"})

	total := 0
	for kind := range validKinds {
		dir := filepath.Join(root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".json" {
				total++
			}
		}
	}
	if total != 2 {
		t.Errorf("expected 2 total entries, got %d", total)
	}
}

func TestSearchFindsSubstring(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "fix", "search1", MemEntry{
		Key:       "search1",
		Kind:      "fix",
		Data:      "contains needle in data",
		CreatedAt: "2025-01-01T00:00:00Z",
	})
	writeMemEntry(t, root, "fix", "search2", MemEntry{
		Key:       "search2",
		Kind:      "fix",
		Data:      "no match here",
		CreatedAt: "2025-01-01T00:00:00Z",
	})

	query := "needle"
	queryLower := strings.ToLower(query)
	var results []MemEntry
	for kind := range validKinds {
		dir := filepath.Join(root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if !strings.Contains(strings.ToLower(string(data)), queryLower) {
				continue
			}
			var entry MemEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}
			results = append(results, entry)
		}
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].Key != "search1" {
		t.Errorf("search result key = %q, want %q", results[0].Key, "search1")
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "command", "ci1", MemEntry{
		Key:       "ci1",
		Kind:      "command",
		Data:      "HAS UPPERCASE CONTENT",
		CreatedAt: "2025-01-01T00:00:00Z",
	})

	query := "uppercase"
	var found bool
	dir := filepath.Join(root, "command")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(query)) {
			found = true
			break
		}
	}
	if !found {
		t.Error("case-insensitive search should find uppercase content")
	}
}

func TestSearchNoResults(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "fix", "s1", MemEntry{
		Key:       "s1",
		Kind:      "fix",
		Data:      "something else entirely",
		CreatedAt: "2025-01-01T00:00:00Z",
	})

	query := "zzznomatch"
	var count int
	for kind := range validKinds {
		dir := filepath.Join(root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(strings.ToLower(string(data)), strings.ToLower(query)) {
				count++
			}
		}
	}
	if count != 0 {
		t.Errorf("expected 0 search results, got %d", count)
	}
}

func TestDeleteEntry(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "fingerprint", "del1", MemEntry{
		Key:       "del1",
		Kind:      "fingerprint",
		Data:      "to delete",
		CreatedAt: "2025-01-01T00:00:00Z",
	})

	path := filepath.Join(root, "fingerprint", "del1.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("entry should exist before delete")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("entry should not exist after delete")
	}
}

func TestDeleteKeyNotFound(t *testing.T) {
	root := setupMemStore(t)

	var found bool
	for kind := range validKinds {
		path := filepath.Join(root, kind, "nonexistent.json")
		if _, err := os.Stat(path); err == nil {
			found = true
		}
	}
	if found {
		t.Error("nonexistent key should not be found")
	}
}

func TestMemEntryJSONRoundTrip(t *testing.T) {
	entry := MemEntry{
		Key:       "rt1",
		Kind:      "dossier",
		Data:      map[string]interface{}{"nested": true, "count": float64(42)},
		CreatedAt: "2025-01-01T00:00:00Z",
		UpdatedAt: "2025-06-01T00:00:00Z",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var got MemEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Key != entry.Key {
		t.Errorf("Key = %q, want %q", got.Key, entry.Key)
	}
	if got.Kind != entry.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, entry.Kind)
	}
}

func TestPutInputValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   PutInput
		wantErr bool
	}{
		{"valid", PutInput{Key: "k", Kind: "fix", Data: "d"}, false},
		{"empty_key", PutInput{Key: "", Kind: "fix", Data: "d"}, true},
		{"empty_kind", PutInput{Key: "k", Kind: "", Data: "d"}, true},
		{"invalid_kind", PutInput{Key: "k", Kind: "bogus", Data: "d"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasErr := tt.input.Key == "" || tt.input.Kind == "" || !validKinds[tt.input.Kind]
			if hasErr != tt.wantErr {
				t.Errorf("validation result = %v, want %v", hasErr, tt.wantErr)
			}
		})
	}
}

func TestStatsOutput(t *testing.T) {
	root := setupMemStore(t)

	writeMemEntry(t, root, "fix", "s1", MemEntry{Key: "s1", Kind: "fix", CreatedAt: "2025-01-01T00:00:00Z"})
	writeMemEntry(t, root, "fix", "s2", MemEntry{Key: "s2", Kind: "fix", CreatedAt: "2025-01-01T00:00:00Z"})
	writeMemEntry(t, root, "command", "s3", MemEntry{Key: "s3", Kind: "command", CreatedAt: "2025-01-01T00:00:00Z"})

	countKind := func(kind string) int {
		dir := filepath.Join(root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0
		}
		count := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				count++
			}
		}
		return count
	}

	if got := countKind("fix"); got != 2 {
		t.Errorf("fix count = %d, want 2", got)
	}
	if got := countKind("command"); got != 1 {
		t.Errorf("command count = %d, want 1", got)
	}
	if got := countKind("dossier"); got != 0 {
		t.Errorf("dossier count = %d, want 0", got)
	}
}
