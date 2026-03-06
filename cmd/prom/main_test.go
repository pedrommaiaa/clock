package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

// setupTestRegistry creates a temporary registry for testing.
func setupTestRegistry(t *testing.T, manifests []common.ToolManifest) (cleanup func()) {
	t.Helper()
	dir := t.TempDir()

	// Override registryPath and toolsDir by changing directory
	origDir, _ := os.Getwd()
	os.Chdir(dir)

	// Create .clock directory
	os.MkdirAll(filepath.Join(dir, ".clock"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".clock", "tools"), 0o755)

	if manifests != nil {
		data, _ := json.MarshalIndent(manifests, "", "  ")
		os.WriteFile(filepath.Join(dir, ".clock", "registry.json"), data, 0o644)
	}

	return func() {
		os.Chdir(origDir)
	}
}

func TestFindTool(t *testing.T) {
	registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
		{Name: "slce", Version: "1.0"},
	}

	idx := findTool(registry, "srch")
	if idx != 0 {
		t.Errorf("findTool(srch) = %d, want 0", idx)
	}

	idx = findTool(registry, "slce")
	if idx != 1 {
		t.Errorf("findTool(slce) = %d, want 1", idx)
	}

	idx = findTool(registry, "nonexistent")
	if idx != -1 {
		t.Errorf("findTool(nonexistent) = %d, want -1", idx)
	}
}

func TestFindTool_EmptyRegistry(t *testing.T) {
	idx := findTool([]common.ToolManifest{}, "srch")
	if idx != -1 {
		t.Errorf("findTool on empty registry = %d, want -1", idx)
	}
}

func TestReadRegistry_NotExist(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	// Remove the registry file if it exists
	os.Remove(registryPath())

	reg, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(reg))
	}
}

func TestReadRegistry_EmptyFile(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	os.WriteFile(registryPath(), []byte(""), 0o644)

	reg, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 0 {
		t.Errorf("expected empty registry for empty file, got %d entries", len(reg))
	}
}

func TestReadWriteRegistry(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	manifests := []common.ToolManifest{
		{Name: "srch", Version: "1.0", Owner: "core"},
		{Name: "slce", Version: "2.0", Owner: "userland"},
	}

	err := writeRegistry(manifests)
	if err != nil {
		t.Fatal(err)
	}

	got, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Name != "srch" || got[0].Version != "1.0" {
		t.Errorf("first entry = %+v, want srch/1.0", got[0])
	}
	if got[1].Name != "slce" || got[1].Version != "2.0" {
		t.Errorf("second entry = %+v, want slce/2.0", got[1])
	}
}

func TestWriteRegistry_AtomicOverwrite(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	// Write initial
	initial := []common.ToolManifest{{Name: "srch", Version: "1.0"}}
	writeRegistry(initial)

	// Overwrite
	updated := []common.ToolManifest{
		{Name: "srch", Version: "2.0"},
		{Name: "slce", Version: "1.0"},
	}
	err := writeRegistry(updated)
	if err != nil {
		t.Fatal(err)
	}

	got, err := readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries after overwrite, got %d", len(got))
	}
	if got[0].Version != "2.0" {
		t.Errorf("expected version 2.0 after overwrite, got %s", got[0].Version)
	}
}

func TestRegistryUpdateExisting(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
	}

	// Simulate updating an existing tool
	manifest := common.ToolManifest{Name: "srch", Version: "2.0"}
	idx := findTool(registry, manifest.Name)
	if idx >= 0 {
		registry[idx] = manifest
	}

	if registry[0].Version != "2.0" {
		t.Errorf("expected version 2.0 after update, got %s", registry[0].Version)
	}
}

func TestRegistryAddNew(t *testing.T) {
	cleanup := setupTestRegistry(t, nil)
	defer cleanup()

	registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
	}

	// Simulate adding a new tool
	manifest := common.ToolManifest{Name: "slce", Version: "1.0"}
	idx := findTool(registry, manifest.Name)
	if idx < 0 {
		registry = append(registry, manifest)
	}

	if len(registry) != 2 {
		t.Errorf("expected 2 tools after add, got %d", len(registry))
	}
}

func TestRegistryRemove(t *testing.T) {
	registry := []common.ToolManifest{
		{Name: "srch", Version: "1.0"},
		{Name: "slce", Version: "1.0"},
		{Name: "pack", Version: "1.0"},
	}

	name := "slce"
	idx := findTool(registry, name)
	if idx < 0 {
		t.Fatal("expected to find slce")
	}

	registry = append(registry[:idx], registry[idx+1:]...)
	if len(registry) != 2 {
		t.Errorf("expected 2 entries after remove, got %d", len(registry))
	}
	// Verify slce was removed
	if findTool(registry, "slce") != -1 {
		t.Error("slce should have been removed")
	}
	// Verify others remain
	if findTool(registry, "srch") == -1 {
		t.Error("srch should still exist")
	}
	if findTool(registry, "pack") == -1 {
		t.Error("pack should still exist")
	}
}
