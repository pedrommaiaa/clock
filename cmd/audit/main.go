// Command audit performs security audits on tool source directories,
// checking permissions, dangerous patterns, hash integrity, and file structure.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// AuditCheck is a single audit check result.
type AuditCheck struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Details string `json:"details,omitempty"`
}

// AuditResult is the output of the audit tool.
type AuditResult struct {
	Risk     string       `json:"risk"`
	Checks   []AuditCheck `json:"checks"`
	Warnings []string     `json:"warnings,omitempty"`
	Hash     string       `json:"hash"`
}

// AuditInput is the stdin input schema (for artifact ref mode).
type AuditInput struct {
	Artifact string `json:"artifact,omitempty"`
	Path     string `json:"path,omitempty"`
}

// Capability patterns map filesystem/network/exec usage to required capabilities.
var capabilityPatterns = map[string][]string{
	"write": {
		`os\.Remove`, `os\.RemoveAll`, `os\.Create`, `os\.WriteFile`,
		`os\.MkdirAll`, `os\.Mkdir`, `os\.Rename`, `os\.Truncate`,
		`os\.OpenFile`,
	},
	"net": {
		`net/http`, `net\.Dial`, `net\.Listen`, `http\.Get`, `http\.Post`,
		`http\.NewRequest`, `http\.Client`,
	},
	"run": {
		`os/exec`, `exec\.Command`, `syscall\.Exec`, `syscall\.ForkExec`,
	},
}

// Dangerous patterns to scan for.
var dangerousPatterns = []struct {
	Name    string
	Pattern string
	Risk    string
}{
	{"setenv", `os\.Setenv`, "med"},
	{"unsetenv", `os\.Unsetenv`, "med"},
	{"unsafe_package", `"unsafe"`, "high"},
	{"cgo_usage", `import\s+"C"`, "high"},
	{"cgo_comment", `#cgo`, "high"},
	{"hardcoded_password", `(?i)(password|passwd|pwd)\s*[:=]\s*"[^"]+"|'[^']+'`, "high"},
	{"hardcoded_secret", `(?i)(secret|api_key|apikey|token|auth)\s*[:=]\s*"[^"]+"|'[^']+'`, "high"},
	{"shell_injection", `exec\.Command\(\s*"(?:sh|bash|zsh|cmd)"`, "high"},
}

func main() {
	pathFlag := flag.String("path", "", "path to tool source directory")
	flag.Parse()

	dir := *pathFlag

	// If no -path flag, try reading from stdin
	if dir == "" {
		var input AuditInput
		if err := jsonutil.ReadInput(&input); err == nil && input.Path != "" {
			dir = input.Path
		}
	}

	if dir == "" {
		jsonutil.Fatal("path to tool source directory is required (-path <dir> or via stdin)")
	}

	// Verify directory exists
	info, err := os.Stat(dir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("cannot access %q: %v", dir, err))
	}
	if !info.IsDir() {
		jsonutil.Fatal(fmt.Sprintf("%q is not a directory", dir))
	}

	// Collect all source files
	sourceFiles, err := collectSourceFiles(dir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("collect source files: %v", err))
	}

	// Read all source content
	sourceContent := make(map[string]string)
	for _, f := range sourceFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			jsonutil.Fatal(fmt.Sprintf("read file %q: %v", f, err))
		}
		rel, _ := filepath.Rel(dir, f)
		sourceContent[rel] = string(data)
	}

	// Read manifest if it exists
	var manifest *common.ToolManifest
	manifestPath := filepath.Join(dir, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var m common.ToolManifest
		if err := json.Unmarshal(data, &m); err == nil {
			manifest = &m
		}
	}

	result := AuditResult{
		Risk: "low",
	}

	// Check 1: Permissions — verify declared capabilities match actual code patterns
	permCheck := checkPermissions(sourceContent, manifest)
	result.Checks = append(result.Checks, permCheck)
	if !permCheck.Pass {
		result.Risk = elevateRisk(result.Risk, "med")
	}

	// Check 2: Dangerous patterns
	dangerCheck, warnings := checkDangerousPatterns(sourceContent)
	result.Checks = append(result.Checks, dangerCheck)
	result.Warnings = append(result.Warnings, warnings...)
	if !dangerCheck.Pass {
		result.Risk = elevateRisk(result.Risk, "high")
	}

	// Check 3: Hash integrity
	hashVal, hashCheck := checkHashIntegrity(dir, sourceFiles, manifest)
	result.Checks = append(result.Checks, hashCheck)
	result.Hash = hashVal
	if !hashCheck.Pass {
		result.Risk = elevateRisk(result.Risk, "med")
	}

	// Check 4: File structure
	structCheck := checkFileStructure(dir, sourceContent)
	result.Checks = append(result.Checks, structCheck)
	if !structCheck.Pass {
		result.Risk = elevateRisk(result.Risk, "med")
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// collectSourceFiles walks a directory and returns all .go files.
func collectSourceFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden directories and vendor
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && path != dir {
				return filepath.SkipDir
			}
			if base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// checkPermissions verifies declared capabilities match actual code usage.
func checkPermissions(sourceContent map[string]string, manifest *common.ToolManifest) AuditCheck {
	// Detect actual capabilities used in code
	detectedCaps := make(map[string]bool)
	for _, content := range sourceContent {
		for cap, patterns := range capabilityPatterns {
			for _, pattern := range patterns {
				re, err := regexp.Compile(pattern)
				if err != nil {
					continue
				}
				if re.MatchString(content) {
					detectedCaps[cap] = true
					break
				}
			}
		}
	}

	if manifest == nil {
		if len(detectedCaps) == 0 {
			return AuditCheck{
				Name:    "permissions",
				Pass:    true,
				Details: "no manifest found; no risky capabilities detected",
			}
		}
		caps := mapKeys(detectedCaps)
		return AuditCheck{
			Name:    "permissions",
			Pass:    false,
			Details: fmt.Sprintf("no manifest found; detected capabilities: %s", strings.Join(caps, ", ")),
		}
	}

	// Compare declared vs detected
	declaredCaps := make(map[string]bool)
	for _, c := range manifest.Capabilities {
		declaredCaps[c] = true
	}

	var undeclared []string
	for cap := range detectedCaps {
		if !declaredCaps[cap] {
			undeclared = append(undeclared, cap)
		}
	}
	sort.Strings(undeclared)

	if len(undeclared) > 0 {
		return AuditCheck{
			Name:    "permissions",
			Pass:    false,
			Details: fmt.Sprintf("undeclared capabilities: %s", strings.Join(undeclared, ", ")),
		}
	}

	return AuditCheck{
		Name:    "permissions",
		Pass:    true,
		Details: "declared capabilities match code usage",
	}
}

// checkDangerousPatterns scans for dangerous code patterns.
func checkDangerousPatterns(sourceContent map[string]string) (AuditCheck, []string) {
	var warnings []string
	highRiskFound := false

	for _, dp := range dangerousPatterns {
		re, err := regexp.Compile(dp.Pattern)
		if err != nil {
			continue
		}
		for file, content := range sourceContent {
			matches := re.FindAllString(content, -1)
			if len(matches) > 0 {
				warning := fmt.Sprintf("[%s] %s: found %d match(es) in %s", dp.Risk, dp.Name, len(matches), file)
				warnings = append(warnings, warning)
				if dp.Risk == "high" {
					highRiskFound = true
				}
			}
		}
	}

	sort.Strings(warnings)

	if highRiskFound {
		return AuditCheck{
			Name:    "dangerous_patterns",
			Pass:    false,
			Details: fmt.Sprintf("found %d warning(s), including high-risk patterns", len(warnings)),
		}, warnings
	}

	if len(warnings) > 0 {
		return AuditCheck{
			Name:    "dangerous_patterns",
			Pass:    true,
			Details: fmt.Sprintf("found %d warning(s), none high-risk", len(warnings)),
		}, warnings
	}

	return AuditCheck{
		Name:    "dangerous_patterns",
		Pass:    true,
		Details: "no dangerous patterns detected",
	}, nil
}

// checkHashIntegrity computes SHA256 of all source files and compares with manifest.
func checkHashIntegrity(dir string, files []string, manifest *common.ToolManifest) (string, AuditCheck) {
	h := sha256.New()

	// Sort files for deterministic hashing
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)

	for _, f := range sorted {
		fh, err := os.Open(f)
		if err != nil {
			return "", AuditCheck{
				Name:    "hash_integrity",
				Pass:    false,
				Details: fmt.Sprintf("cannot open %q: %v", f, err),
			}
		}
		rel, _ := filepath.Rel(dir, f)
		// Include the relative path in the hash for file-order sensitivity
		h.Write([]byte(rel))
		if _, err := io.Copy(h, fh); err != nil {
			fh.Close()
			return "", AuditCheck{
				Name:    "hash_integrity",
				Pass:    false,
				Details: fmt.Sprintf("cannot read %q: %v", f, err),
			}
		}
		fh.Close()
	}

	hashStr := "sha256:" + hex.EncodeToString(h.Sum(nil))

	if manifest == nil {
		return hashStr, AuditCheck{
			Name:    "hash_integrity",
			Pass:    true,
			Details: fmt.Sprintf("computed hash: %s (no manifest to compare)", hashStr),
		}
	}

	if manifest.SHA256 == "" {
		return hashStr, AuditCheck{
			Name:    "hash_integrity",
			Pass:    true,
			Details: fmt.Sprintf("computed hash: %s (manifest has no hash)", hashStr),
		}
	}

	if manifest.SHA256 == hashStr {
		return hashStr, AuditCheck{
			Name:    "hash_integrity",
			Pass:    true,
			Details: "hash matches manifest",
		}
	}

	return hashStr, AuditCheck{
		Name:    "hash_integrity",
		Pass:    false,
		Details: fmt.Sprintf("hash mismatch: computed %s, manifest %s", hashStr, manifest.SHA256),
	}
}

// checkFileStructure verifies expected files exist.
func checkFileStructure(dir string, sourceContent map[string]string) AuditCheck {
	var missing []string

	// Check for main.go
	hasMain := false
	for f := range sourceContent {
		if f == "main.go" || strings.HasSuffix(f, "/main.go") {
			hasMain = true
			break
		}
	}
	if !hasMain {
		missing = append(missing, "main.go")
	}

	// Check for test file
	hasTest := false
	for f := range sourceContent {
		if strings.HasSuffix(f, "_test.go") {
			hasTest = true
			break
		}
	}
	if !hasTest {
		missing = append(missing, "*_test.go")
	}

	if len(missing) > 0 {
		return AuditCheck{
			Name:    "file_structure",
			Pass:    false,
			Details: fmt.Sprintf("missing expected files: %s", strings.Join(missing, ", ")),
		}
	}

	return AuditCheck{
		Name:    "file_structure",
		Pass:    true,
		Details: "expected files present (main.go, test file)",
	}
}

// elevateRisk returns the higher of current and new risk levels.
func elevateRisk(current, new string) string {
	levels := map[string]int{"low": 0, "med": 1, "high": 2}
	if levels[new] > levels[current] {
		return new
	}
	return current
}

// mapKeys returns sorted keys of a map.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
