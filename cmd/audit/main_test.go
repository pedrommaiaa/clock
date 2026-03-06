package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestElevateRisk(t *testing.T) {
	tests := []struct {
		current string
		new     string
		want    string
	}{
		{"low", "med", "med"},
		{"low", "high", "high"},
		{"med", "high", "high"},
		{"high", "low", "high"},
		{"med", "low", "med"},
		{"low", "low", "low"},
	}

	for _, tt := range tests {
		t.Run(tt.current+"->"+tt.new, func(t *testing.T) {
			got := elevateRisk(tt.current, tt.new)
			if got != tt.want {
				t.Errorf("elevateRisk(%q, %q) = %q, want %q", tt.current, tt.new, got, tt.want)
			}
		})
	}
}

func TestMapKeys(t *testing.T) {
	m := map[string]bool{
		"c": true,
		"a": true,
		"b": true,
	}

	keys := mapKeys(m)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	// Should be sorted
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("expected sorted keys [a b c], got %v", keys)
	}
}

func TestMapKeys_Empty(t *testing.T) {
	keys := mapKeys(map[string]bool{})
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestCheckDangerousPatterns_Clean(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello")
}`,
	}

	check, warnings := checkDangerousPatterns(source)
	if !check.Pass {
		t.Errorf("expected pass for clean code, got fail: %s", check.Details)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestCheckDangerousPatterns_UnsafePackage(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "unsafe"

func main() {
	_ = unsafe.Pointer(nil)
}`,
	}

	check, warnings := checkDangerousPatterns(source)
	if check.Pass {
		t.Error("expected fail for unsafe import")
	}
	if len(warnings) == 0 {
		t.Error("expected warnings for unsafe import")
	}
}

func TestCheckDangerousPatterns_ShellInjection(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "os/exec"

func main() {
	exec.Command("bash", "-c", "rm -rf /")
}`,
	}

	check, warnings := checkDangerousPatterns(source)
	if check.Pass {
		t.Error("expected fail for shell injection pattern")
	}
	if len(warnings) == 0 {
		t.Error("expected warnings for shell injection")
	}
}

func TestCheckDangerousPatterns_HardcodedSecret(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

var api_key = "sk-12345abcdef"

func main() {}`,
	}

	check, warnings := checkDangerousPatterns(source)
	// Should detect hardcoded secret
	if len(warnings) == 0 {
		t.Error("expected warnings for hardcoded secret")
	}
	_ = check
}

func TestCheckPermissions_NoManifest_NoCaps(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello")
}`,
	}

	check := checkPermissions(source, nil)
	if !check.Pass {
		t.Errorf("expected pass for no manifest and no risky caps, got: %s", check.Details)
	}
}

func TestCheckPermissions_NoManifest_WithCaps(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "os"

func main() {
	os.WriteFile("test.txt", nil, 0644)
}`,
	}

	check := checkPermissions(source, nil)
	if check.Pass {
		t.Error("expected fail when capabilities detected but no manifest")
	}
}

func TestCheckPermissions_DeclaredMatchesActual(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "os"

func main() {
	os.WriteFile("test.txt", nil, 0644)
}`,
	}

	manifest := &common.ToolManifest{
		Capabilities: []string{"write"},
	}

	check := checkPermissions(source, manifest)
	if !check.Pass {
		t.Errorf("expected pass when declared caps match, got: %s", check.Details)
	}
}

func TestCheckPermissions_Undeclared(t *testing.T) {
	source := map[string]string{
		"main.go": `package main

import "os"
import "os/exec"

func main() {
	os.WriteFile("test.txt", nil, 0644)
	exec.Command("ls")
}`,
	}

	manifest := &common.ToolManifest{
		Capabilities: []string{"write"},
		// Missing "run" capability
	}

	check := checkPermissions(source, manifest)
	if check.Pass {
		t.Error("expected fail when capabilities are undeclared")
	}
}

func TestCheckFileStructure_Complete(t *testing.T) {
	source := map[string]string{
		"main.go":      "package main",
		"main_test.go": "package main",
	}

	check := checkFileStructure(".", source)
	if !check.Pass {
		t.Errorf("expected pass for complete structure, got: %s", check.Details)
	}
}

func TestCheckFileStructure_MissingMain(t *testing.T) {
	source := map[string]string{
		"helper.go":    "package main",
		"main_test.go": "package main",
	}

	check := checkFileStructure(".", source)
	if check.Pass {
		t.Error("expected fail when main.go is missing")
	}
}

func TestCheckFileStructure_MissingTest(t *testing.T) {
	source := map[string]string{
		"main.go": "package main",
	}

	check := checkFileStructure(".", source)
	if check.Pass {
		t.Error("expected fail when test file is missing")
	}
}

func TestCollectSourceFiles(t *testing.T) {
	dir := t.TempDir()

	// Create Go files
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# README"), 0o644)

	// Create hidden dir (should be skipped)
	hiddenDir := filepath.Join(dir, ".hidden")
	os.MkdirAll(hiddenDir, 0o755)
	os.WriteFile(filepath.Join(hiddenDir, "skip.go"), []byte("package hidden"), 0o644)

	// Create vendor dir (should be skipped)
	vendorDir := filepath.Join(dir, "vendor")
	os.MkdirAll(vendorDir, 0o755)
	os.WriteFile(filepath.Join(vendorDir, "dep.go"), []byte("package dep"), 0o644)

	files, err := collectSourceFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should only have 2 Go files (main.go and helper.go)
	if len(files) != 2 {
		t.Errorf("expected 2 Go files, got %d: %v", len(files), files)
	}
}

func TestCheckHashIntegrity_NoManifest(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main"), 0o644)

	hash, check := checkHashIntegrity(dir, []string{goFile}, nil)
	if !check.Pass {
		t.Errorf("expected pass with no manifest, got: %s", check.Details)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestCheckHashIntegrity_MatchingHash(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main"), 0o644)

	// Compute hash first
	hash1, _ := checkHashIntegrity(dir, []string{goFile}, nil)

	// Now verify with matching manifest
	manifest := &common.ToolManifest{SHA256: hash1}
	hash2, check := checkHashIntegrity(dir, []string{goFile}, manifest)

	if !check.Pass {
		t.Errorf("expected pass for matching hash, got: %s", check.Details)
	}
	if hash1 != hash2 {
		t.Error("hash should be consistent")
	}
}

func TestCheckHashIntegrity_MismatchHash(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main"), 0o644)

	manifest := &common.ToolManifest{SHA256: "sha256:wrong"}
	_, check := checkHashIntegrity(dir, []string{goFile}, manifest)

	if check.Pass {
		t.Error("expected fail for mismatched hash")
	}
}
