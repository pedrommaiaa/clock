// Command guard is a diff validator that parses a unified diff, applies
// policy checks, computes a risk score, and outputs a GuardResult JSON.
package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// GuardInput is the input schema for the guard tool.
type GuardInput struct {
	Diff   string `json:"diff"`
	Policy Policy `json:"policy"`
}

// Policy defines the constraints for diff validation.
type Policy struct {
	MaxFiles       int      `json:"max_files"`
	MaxLines       int      `json:"max_lines"`
	ForbiddenPaths []string `json:"forbidden_paths"`
	RequireContext int      `json:"require_context"`
	DenyBinary     bool     `json:"deny_binary"`
}

// DiffStats holds parsed statistics from a unified diff.
type DiffStats struct {
	Files        []string
	LinesAdded   int
	LinesDeleted int
	Hunks        []HunkInfo
	HasBinary    bool
}

// HunkInfo holds context information about a single hunk.
type HunkInfo struct {
	File         string
	ContextLines int
}

// configPatterns are file path patterns that indicate config/migration files.
var configPatterns = []string{
	"*.yml", "*.yaml", "*.toml", "*.ini", "*.cfg", "*.conf",
	"*.json", "*.env", "*.env.*",
	"Makefile", "Dockerfile", "docker-compose*",
	"*migration*", "*migrate*",
	".github/*", ".gitlab-ci*", "Jenkinsfile",
	"go.mod", "go.sum", "package.json", "package-lock.json",
	"Cargo.toml", "Cargo.lock", "requirements.txt", "Pipfile*",
}

func main() {
	var input GuardInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Diff == "" {
		result := common.GuardResult{
			OK:      true,
			Risk:    0.0,
			Reasons: []string{"empty diff"},
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	stats := parseDiff(input.Diff)
	result := applyPolicy(stats, input.Policy)

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// parseDiff extracts statistics from a unified diff string.
func parseDiff(diff string) DiffStats {
	var stats DiffStats
	fileSet := make(map[string]bool)
	lines := strings.Split(diff, "\n")

	var currentFile string
	inHunk := false
	hunkContextLines := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Detect binary files
		if strings.HasPrefix(line, "Binary files") ||
			strings.HasPrefix(line, "GIT binary patch") ||
			strings.Contains(line, "binary file") {
			stats.HasBinary = true
			continue
		}

		// Parse file headers
		if strings.HasPrefix(line, "--- ") {
			// The --- line indicates the old file; the +++ line follows
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			filePath := strings.TrimPrefix(line, "+++ ")
			// Strip common prefixes like b/
			filePath = stripDiffPrefix(filePath)
			if filePath != "/dev/null" && filePath != "" {
				currentFile = filePath
				if !fileSet[currentFile] {
					fileSet[currentFile] = true
					stats.Files = append(stats.Files, currentFile)
				}
			}
			continue
		}

		// Parse hunk header
		if strings.HasPrefix(line, "@@") {
			// Save previous hunk info if we were in one
			if inHunk && currentFile != "" {
				stats.Hunks = append(stats.Hunks, HunkInfo{
					File:         currentFile,
					ContextLines: hunkContextLines,
				})
			}
			inHunk = true
			hunkContextLines = 0
			continue
		}

		// Parse diff content lines
		if inHunk {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				stats.LinesAdded++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				stats.LinesDeleted++
			} else if strings.HasPrefix(line, " ") {
				// Context line (starts with space)
				hunkContextLines++
			}
			// Lines starting with \ (e.g., "\ No newline at end of file") are ignored
		}

		// Detect diff/file boundary (new diff section)
		if strings.HasPrefix(line, "diff ") {
			// Save previous hunk if any
			if inHunk && currentFile != "" {
				stats.Hunks = append(stats.Hunks, HunkInfo{
					File:         currentFile,
					ContextLines: hunkContextLines,
				})
			}
			inHunk = false
			hunkContextLines = 0
		}
	}

	// Save the last hunk
	if inHunk && currentFile != "" {
		stats.Hunks = append(stats.Hunks, HunkInfo{
			File:         currentFile,
			ContextLines: hunkContextLines,
		})
	}

	return stats
}

// stripDiffPrefix removes common diff prefixes like a/ or b/.
func stripDiffPrefix(path string) string {
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}

// applyPolicy checks the diff stats against the policy and computes a risk score.
func applyPolicy(stats DiffStats, policy Policy) common.GuardResult {
	result := common.GuardResult{
		OK:   true,
		Risk: 0.0,
	}

	totalLines := stats.LinesAdded + stats.LinesDeleted
	numFiles := len(stats.Files)

	// Check max_files
	if policy.MaxFiles > 0 && numFiles > policy.MaxFiles {
		result.OK = false
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("files changed (%d) exceeds max_files (%d)", numFiles, policy.MaxFiles))
	}

	// Check max_lines
	if policy.MaxLines > 0 && totalLines > policy.MaxLines {
		result.OK = false
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("lines changed (%d) exceeds max_lines (%d)", totalLines, policy.MaxLines))
	}

	// Check forbidden_paths
	for _, file := range stats.Files {
		for _, pattern := range policy.ForbiddenPaths {
			matched, err := filepath.Match(pattern, filepath.Base(file))
			if err != nil {
				continue
			}
			if matched {
				result.OK = false
				result.Reasons = append(result.Reasons,
					fmt.Sprintf("file %q matches forbidden path pattern %q", file, pattern))
			}
			// Also try matching against the full path
			if !matched {
				matched, err = filepath.Match(pattern, file)
				if err == nil && matched {
					result.OK = false
					result.Reasons = append(result.Reasons,
						fmt.Sprintf("file %q matches forbidden path pattern %q", file, pattern))
				}
			}
		}
	}

	// Check require_context (warning, not blocking)
	if policy.RequireContext > 0 {
		for _, hunk := range stats.Hunks {
			if hunk.ContextLines < policy.RequireContext {
				result.Needs = append(result.Needs,
					fmt.Sprintf("hunk in %s has %d context lines (want >= %d)",
						hunk.File, hunk.ContextLines, policy.RequireContext))
			}
		}
	}

	// Check deny_binary
	if policy.DenyBinary && stats.HasBinary {
		result.OK = false
		result.Reasons = append(result.Reasons, "diff contains binary file changes")
	}

	// Compute risk score
	result.Risk = computeRisk(stats)

	return result
}

// computeRisk calculates a risk score from 0.0 to 1.0.
func computeRisk(stats DiffStats) float64 {
	risk := 0.0

	numFiles := len(stats.Files)
	totalLines := stats.LinesAdded + stats.LinesDeleted

	// File count risk: more files = riskier
	// 1 file = 0.05, 5 files = 0.15, 10+ files = 0.25
	fileRisk := math.Min(float64(numFiles)*0.025, 0.25)
	risk += fileRisk

	// Lines changed risk: more lines = riskier
	// 10 lines = 0.02, 100 lines = 0.15, 500+ lines = 0.35
	lineRisk := math.Min(float64(totalLines)*0.0007, 0.35)
	risk += lineRisk

	// Config/migration file risk
	configCount := 0
	for _, file := range stats.Files {
		if isConfigFile(file) {
			configCount++
		}
	}
	if configCount > 0 {
		configRisk := math.Min(float64(configCount)*0.1, 0.25)
		risk += configRisk
	}

	// Binary file risk
	if stats.HasBinary {
		risk += 0.15
	}

	// Cap at 1.0
	risk = math.Min(risk, 1.0)

	// Round to 2 decimal places
	risk = math.Round(risk*100) / 100

	return risk
}

// isConfigFile checks if a file path matches known config/migration patterns.
func isConfigFile(path string) bool {
	base := filepath.Base(path)
	for _, pattern := range configPatterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}
