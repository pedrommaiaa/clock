// Command aply is an atomic patch application tool.
// It reads an ApplyInput JSON from stdin, creates a checkpoint (stash or commit),
// applies a unified diff via git apply --3way (with fallbacks), and outputs
// an ApplyResult JSON with file/line counts.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// ApplyInput is the input schema for the aply tool.
type ApplyInput struct {
	Diff       string `json:"diff"`
	Checkpoint string `json:"checkpoint"` // auto, stash, commit
	Msg        string `json:"msg"`
}

func main() {
	var input ApplyInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Diff == "" {
		jsonutil.Fatal("diff is required")
	}
	if input.Checkpoint == "" {
		input.Checkpoint = "auto"
	}

	// Generate checkpoint ID as timestamp hex
	chkID := fmt.Sprintf("%x", time.Now().UnixMilli())

	result := common.ApplyResult{
		ChkID: chkID,
	}

	// Create checkpoint
	if err := createCheckpoint(input.Checkpoint, chkID, input.Msg); err != nil {
		result.OK = false
		result.Err = fmt.Sprintf("checkpoint failed: %v", err)
		jsonutil.WriteOutput(result)
		return
	}

	// Write diff to temp file
	tmpFile, err := os.CreateTemp("", "clock-diff-*.patch")
	if err != nil {
		result.OK = false
		result.Err = fmt.Sprintf("create temp file: %v", err)
		jsonutil.WriteOutput(result)
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(input.Diff); err != nil {
		tmpFile.Close()
		result.OK = false
		result.Err = fmt.Sprintf("write diff: %v", err)
		jsonutil.WriteOutput(result)
		return
	}
	tmpFile.Close()

	// Try applying the diff with fallback chain
	applyErr := applyDiff(tmpFile.Name())
	if applyErr != nil {
		result.OK = false
		result.Err = fmt.Sprintf("apply failed: %v", applyErr)
		jsonutil.WriteOutput(result)
		return
	}

	// Count files changed and lines added/deleted from the diff
	files, added, deleted := parseDiffStats(input.Diff)

	result.OK = true
	result.Files = files
	result.Lines.Add = added
	result.Lines.Del = deleted

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// isDirty returns true if the git working tree has uncommitted changes.
func isDirty() bool {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(out)) > 0
}

// createCheckpoint creates a checkpoint before applying the diff.
func createCheckpoint(mode, chkID, msg string) error {
	label := "clock-chk-" + chkID
	if msg != "" {
		label += ": " + msg
	}

	switch mode {
	case "stash":
		// Always stash (include untracked)
		cmd := exec.Command("git", "stash", "push", "-m", label)
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "commit":
		if !isDirty() {
			// Nothing to commit, skip
			return nil
		}
		cmd := exec.Command("git", "commit", "-am", label)
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "auto":
		if isDirty() {
			cmd := exec.Command("git", "stash", "push", "-m", label)
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		// Clean tree, no checkpoint needed
		return nil

	default:
		return fmt.Errorf("unknown checkpoint mode: %s", mode)
	}
}

// applyDiff tries to apply a patch file using a fallback chain:
// 1. git apply --3way
// 2. git apply
// 3. patch -p1
func applyDiff(patchPath string) error {
	// Try git apply --3way
	cmd := exec.Command("git", "apply", "--3way", patchPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try plain git apply
	cmd = exec.Command("git", "apply", patchPath)
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try patch -p1
	cmd = exec.Command("patch", "-p1", "-i", patchPath)
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("all apply methods failed; last error: %s", stderr.String())
	}

	return nil
}

// parseDiffStats extracts file names, lines added, and lines deleted from a unified diff.
func parseDiffStats(diff string) (files []string, added, deleted int) {
	seen := make(map[string]bool)
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		// Detect file paths from +++ lines
		if strings.HasPrefix(line, "+++ ") {
			path := strings.TrimPrefix(line, "+++ ")
			// Strip a/ or b/ prefix
			if strings.HasPrefix(path, "b/") {
				path = path[2:]
			}
			if path != "/dev/null" && path != "" && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			continue
		}
		// Count added/deleted lines (ignore --- and +++ header lines)
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			deleted++
		}
	}
	return files, added, deleted
}

