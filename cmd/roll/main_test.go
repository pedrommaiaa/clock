package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func setupRollTest(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	os.MkdirAll(filepath.Join(dir, ".clock"), 0o755)
	return func() { os.Chdir(origDir) }
}

func TestFindToolRoll(t *testing.T) {
	registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
		{Name: "slce", Version: "1.0"},
	}

	idx := findTool(registry, "srch")
	if idx != 0 {
		t.Errorf("findTool(srch) = %d, want 0", idx)
	}

	idx = findTool(registry, "missing")
	if idx != -1 {
		t.Errorf("findTool(missing) = %d, want -1", idx)
	}
}

func TestReadWriteRegistryRoll(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	manifests := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
	}

	err := writeRegistry(manifests)
	if err != nil {
		t.Fatal(err)
	}

	got, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "srch" {
		t.Errorf("unexpected registry contents: %+v", got)
	}
}

func TestReadRegistry_NotExistRoll(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	got, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty registry for non-existent file, got %d", len(got))
	}
}

func TestAppendAndReadHistory(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	entry1 := HistoryEntry{
		Timestamp: 1000,
		Action:    "snapshot",
		Registry: []common.ToolManifest{
			{Name: "srch", Version: "1.0"},
		},
	}
	entry2 := HistoryEntry{
		Timestamp: 2000,
		Action:    "install",
		Tool:      "srch",
		Version:   "2.0",
		Registry: []common.ToolManifest{
			{Name: "srch", Version: "2.0"},
		},
	}

	err := appendHistory(entry1)
	if err != nil {
		t.Fatal(err)
	}
	err = appendHistory(entry2)
	if err != nil {
		t.Fatal(err)
	}

	history, err := readHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].Timestamp != 1000 {
		t.Errorf("first entry timestamp = %d, want 1000", history[0].Timestamp)
	}
	if history[1].Action != "install" {
		t.Errorf("second entry action = %q, want %q", history[1].Action, "install")
	}
}

func TestReadHistory_NotExist(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	history, err := readHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty history for non-existent file, got %d", len(history))
	}
}

func TestReadHistory_EmptyFile(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	os.WriteFile(historyPath(), []byte(""), 0o644)

	history, err := readHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty history for empty file, got %d", len(history))
	}
}

func TestByteReader(t *testing.T) {
	data := []byte(`{"timestamp":1}
{"timestamp":2}
`)
	r := jsonReader(data)

	dec := json.NewDecoder(r)
	var entries []HistoryEntry
	for dec.More() {
		var entry HistoryEntry
		if err := dec.Decode(&entry); err != nil {
			break
		}
		entries = append(entries, entry)
	}

	if len(entries) != 2 {
		t.Errorf("expected 2 entries from reader, got %d", len(entries))
	}
}

func TestRollbackLogic(t *testing.T) {
	cleanup := setupRollTest(t)
	defer cleanup()

	// Set up: write registry with v2, then write history with v1 snapshot
	v1Registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
	}
	v2Registry := []common.ToolManifest{
		{Name: "srch", Version: "2.0"},
	}

	// Write current registry as v2
	writeRegistry(v2Registry)

	// Write history: first snapshot has v1, second has v2
	appendHistory(HistoryEntry{
		Timestamp: 1000,
		Action:    "snapshot",
		Registry:  v1Registry,
	})
	appendHistory(HistoryEntry{
		Timestamp: 2000,
		Action:    "install",
		Tool:      "srch",
		Version:   "2.0",
		Registry:  v2Registry,
	})

	// Read current registry
	registry, _ := readRegistry()
	history, _ := readHistory()

	// Find current version
	currentIdx := findTool(registry, "srch")
	currentVersion := registry[currentIdx].Version

	if currentVersion != "2.0" {
		t.Fatalf("expected current version 2.0, got %s", currentVersion)
	}

	// Search backwards for previous version
	var previousManifest *common.ToolManifest
	for i := len(history) - 1; i >= 0; i-- {
		idx := findTool(history[i].Registry, "srch")
		if idx >= 0 && history[i].Registry[idx].Version != currentVersion {
			m := history[i].Registry[idx]
			previousManifest = &m
			break
		}
	}

	if previousManifest == nil {
		t.Fatal("expected to find previous version in history")
	}
	if previousManifest.Version != "1.0" {
		t.Errorf("expected previous version 1.0, got %s", previousManifest.Version)
	}
}
