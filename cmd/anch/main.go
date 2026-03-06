// Command anch performs resilient patch anchoring.
// It reads a JSON input with a unified diff and anchoring policy from stdin,
// attempts to find drifted context lines in the target files, rebases hunks
// if possible, and outputs the result.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// AnchInput is the input schema for the anch tool.
type AnchInput struct {
	Diff   string     `json:"diff"`
	Policy AnchPolicy `json:"policy"`
}

// AnchPolicy controls anchoring behavior.
type AnchPolicy struct {
	MaxDrift      int  `json:"max_drift"`
	RequireUnique bool `json:"require_unique"`
}

// AnchOutput is the output of the anch tool.
type AnchOutput struct {
	OK      bool   `json:"ok"`
	Rebased bool   `json:"rebased"`
	Diff2   string `json:"diff2,omitempty"`
	Why     string `json:"why"`
}

// Hunk represents a parsed unified diff hunk.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string // raw lines including +/-/space prefix
	Header   string   // @@ line
}

// FileDiff represents all hunks for a single file.
type FileDiff struct {
	OldPath    string
	NewPath    string
	HeaderLines []string // lines before first hunk (diff --git, ---, +++)
	Hunks      []Hunk
}

func main() {
	var input AnchInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Diff == "" {
		jsonutil.WriteOutput(AnchOutput{OK: true, Rebased: false, Why: "empty diff"})
		return
	}

	if input.Policy.MaxDrift == 0 {
		input.Policy.MaxDrift = 10
	}

	fileDiffs, err := parseDiff(input.Diff)
	if err != nil {
		jsonutil.WriteOutput(AnchOutput{OK: false, Rebased: false, Why: fmt.Sprintf("parse diff: %v", err)})
		return
	}

	anyRebased := false
	allOK := true
	var reasons []string

	for i, fd := range fileDiffs {
		// Read the target file
		targetPath := fd.NewPath
		if targetPath == "/dev/null" {
			// File deletion — no anchoring needed
			continue
		}

		fileContent, err := os.ReadFile(targetPath)
		if err != nil {
			// Try without leading path components
			allOK = false
			reasons = append(reasons, fmt.Sprintf("cannot read %s: %v", targetPath, err))
			continue
		}

		fileLines := strings.Split(string(fileContent), "\n")

		for j, hunk := range fd.Hunks {
			// Extract context lines (lines without +/- prefix) from the hunk
			contextLines := extractContextLines(hunk)
			if len(contextLines) == 0 {
				continue
			}

			// Check if the context lines match at the expected position
			expectedStart := hunk.OldStart - 1 // 0-indexed
			if matchesAt(fileLines, contextLines, expectedStart) {
				// No drift — hunk is already correct
				continue
			}

			// Search for the context lines within max_drift range
			drift, found, ambiguous := findDrift(fileLines, contextLines, expectedStart, input.Policy.MaxDrift, input.Policy.RequireUnique)
			if !found {
				allOK = false
				reasons = append(reasons, fmt.Sprintf("hunk %d in %s: context not found within drift %d", j+1, targetPath, input.Policy.MaxDrift))
				continue
			}
			if ambiguous {
				allOK = false
				reasons = append(reasons, fmt.Sprintf("hunk %d in %s: ambiguous match (multiple locations)", j+1, targetPath))
				continue
			}

			// Rebase the hunk
			fileDiffs[i].Hunks[j].OldStart += drift
			fileDiffs[i].Hunks[j].NewStart += drift
			anyRebased = true
			reasons = append(reasons, fmt.Sprintf("hunk %d in %s: rebased by %+d lines", j+1, targetPath, drift))
		}
	}

	output := AnchOutput{
		OK:      allOK,
		Rebased: anyRebased,
		Why:     strings.Join(reasons, "; "),
	}

	if anyRebased {
		output.Diff2 = reconstructDiff(fileDiffs)
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// parseDiff parses a unified diff into FileDiff structures.
func parseDiff(diff string) ([]FileDiff, error) {
	lines := strings.Split(diff, "\n")
	var fileDiffs []FileDiff
	var current *FileDiff

	i := 0
	for i < len(lines) {
		line := lines[i]

		if strings.HasPrefix(line, "diff --git") {
			if current != nil {
				fileDiffs = append(fileDiffs, *current)
			}
			current = &FileDiff{
				HeaderLines: []string{line},
			}
			i++
			continue
		}

		if current == nil {
			// Lines before the first diff header — skip or start a new diff
			if strings.HasPrefix(line, "--- ") {
				current = &FileDiff{HeaderLines: []string{}}
			} else {
				i++
				continue
			}
		}

		if strings.HasPrefix(line, "--- ") {
			path := strings.TrimPrefix(line, "--- ")
			if strings.HasPrefix(path, "a/") {
				path = path[2:]
			}
			current.OldPath = path
			current.HeaderLines = append(current.HeaderLines, line)
			i++
			continue
		}

		if strings.HasPrefix(line, "+++ ") {
			path := strings.TrimPrefix(line, "+++ ")
			if strings.HasPrefix(path, "b/") {
				path = path[2:]
			}
			current.NewPath = path
			current.HeaderLines = append(current.HeaderLines, line)
			i++
			continue
		}

		if strings.HasPrefix(line, "@@") {
			hunk, consumed := parseHunk(lines, i)
			current.Hunks = append(current.Hunks, hunk)
			i += consumed
			continue
		}

		// Other header lines (index, mode, etc.)
		if current != nil && len(current.Hunks) == 0 {
			current.HeaderLines = append(current.HeaderLines, line)
		}
		i++
	}

	if current != nil {
		fileDiffs = append(fileDiffs, *current)
	}

	return fileDiffs, nil
}

// parseHunk parses a single hunk starting at the @@ line.
func parseHunk(lines []string, start int) (Hunk, int) {
	header := lines[start]
	hunk := Hunk{Header: header}

	// Parse @@ -oldStart,oldCount +newStart,newCount @@
	parseHunkHeader(header, &hunk)

	consumed := 1
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff --git") {
			break
		}
		hunk.Lines = append(hunk.Lines, line)
		consumed++
	}

	return hunk, consumed
}

// parseHunkHeader extracts line numbers from the @@ header.
func parseHunkHeader(header string, hunk *Hunk) {
	// @@ -oldStart,oldCount +newStart,newCount @@ optional context
	parts := strings.SplitN(header, "@@", 3)
	if len(parts) < 2 {
		return
	}
	rangePart := strings.TrimSpace(parts[1])
	fields := strings.Fields(rangePart)
	if len(fields) < 2 {
		return
	}

	// Parse -oldStart,oldCount
	old := strings.TrimPrefix(fields[0], "-")
	oldParts := strings.SplitN(old, ",", 2)
	hunk.OldStart, _ = strconv.Atoi(oldParts[0])
	if len(oldParts) > 1 {
		hunk.OldCount, _ = strconv.Atoi(oldParts[1])
	} else {
		hunk.OldCount = 1
	}

	// Parse +newStart,newCount
	new_ := strings.TrimPrefix(fields[1], "+")
	newParts := strings.SplitN(new_, ",", 2)
	hunk.NewStart, _ = strconv.Atoi(newParts[0])
	if len(newParts) > 1 {
		hunk.NewCount, _ = strconv.Atoi(newParts[1])
	} else {
		hunk.NewCount = 1
	}
}

// extractContextLines returns the context lines (unchanged lines) from a hunk.
// These are lines that start with a space (or have no prefix in some diffs).
func extractContextLines(hunk Hunk) []string {
	var ctx []string
	for _, line := range hunk.Lines {
		if len(line) > 0 && line[0] == ' ' {
			ctx = append(ctx, line[1:])
		} else if len(line) == 0 {
			// Empty context line
			ctx = append(ctx, "")
		}
	}
	return ctx
}

// matchesAt checks if context lines match the file at the given 0-indexed position.
func matchesAt(fileLines, contextLines []string, start int) bool {
	if start < 0 || start+len(contextLines) > len(fileLines) {
		return false
	}
	ctxIdx := 0
	// We need to find all context lines in order starting near the given position
	for lineIdx := start; lineIdx < len(fileLines) && ctxIdx < len(contextLines); lineIdx++ {
		if fileLines[lineIdx] == contextLines[ctxIdx] {
			ctxIdx++
		}
	}
	return ctxIdx == len(contextLines)
}

// matchesAtExact checks if all context lines match consecutively.
func matchesAtExact(fileLines, contextLines []string, start int) bool {
	if start < 0 {
		return false
	}
	// Find first context line at or after start
	ci := 0
	fi := start
	for fi < len(fileLines) && ci < len(contextLines) {
		if fileLines[fi] == contextLines[ci] {
			ci++
		}
		fi++
	}
	return ci == len(contextLines)
}

// findDrift searches for context lines around expectedStart and returns the drift.
func findDrift(fileLines, contextLines []string, expectedStart, maxDrift int, requireUnique bool) (drift int, found bool, ambiguous bool) {
	var matches []int

	for d := -maxDrift; d <= maxDrift; d++ {
		pos := expectedStart + d
		if pos < 0 {
			continue
		}
		if matchesAtExact(fileLines, contextLines, pos) {
			matches = append(matches, d)
		}
	}

	if len(matches) == 0 {
		return 0, false, false
	}

	if requireUnique && len(matches) > 1 {
		// Check if there are matches beyond the expected position
		// If only one match exists, it's unambiguous
		return 0, true, true
	}

	// Pick the match with minimum absolute drift
	bestDrift := matches[0]
	for _, d := range matches[1:] {
		if abs(d) < abs(bestDrift) {
			bestDrift = d
		}
	}

	return bestDrift, true, false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// reconstructDiff rebuilds the unified diff from FileDiff structures.
func reconstructDiff(fileDiffs []FileDiff) string {
	var sb strings.Builder

	for _, fd := range fileDiffs {
		for _, h := range fd.HeaderLines {
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
		for _, hunk := range fd.Hunks {
			// Rebuild @@ header
			sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@",
				hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount))
			// Preserve any trailing context from the original header
			parts := strings.SplitN(hunk.Header, "@@", 3)
			if len(parts) >= 3 && parts[2] != "" {
				sb.WriteString(" @@")
				sb.WriteString(parts[2])
			}
			sb.WriteByte('\n')
			for _, line := range hunk.Lines {
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
		}
	}

	return sb.String()
}
