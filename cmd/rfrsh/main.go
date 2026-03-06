// Command rfrsh is a dossier refresh trigger.
// It reads a RefreshInput JSON from stdin, checks if the project dossier
// is stale (missing, too old, or too many changes), and if so, runs the
// scan | map, ctrt, flow | doss pipeline. Outputs a RefreshResult JSON.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// RefreshInput is the input schema for the rfrsh tool.
type RefreshInput struct {
	Root      string `json:"root"`
	DiffLimit int    `json:"diff_limit"`
	AgeMin    int    `json:"age_min"` // minutes
	Force     bool   `json:"force"`
}

// RefreshResult is the output schema for the rfrsh tool.
type RefreshResult struct {
	Stale  bool     `json:"stale"`
	Reason string   `json:"reason,omitempty"`
	Did    []string `json:"did,omitempty"`
	Err    string   `json:"err,omitempty"`
}

func main() {
	var input RefreshInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	// Defaults
	if input.Root == "" {
		input.Root = "."
	}
	if input.DiffLimit <= 0 {
		input.DiffLimit = 30
	}
	if input.AgeMin <= 0 {
		input.AgeMin = 1440
	}

	dossPath := filepath.Join(input.Root, ".clock", "doss.md")
	result := RefreshResult{}

	stale, reason := checkStaleness(dossPath, input)

	if input.Force {
		stale = true
		reason = "forced refresh"
	}

	result.Stale = stale
	result.Reason = reason

	if !stale {
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	// Run the pipeline: scan | map, ctrt, flow | doss
	did, err := runPipeline(input.Root)
	if err != nil {
		result.Err = err.Error()
	}
	result.Did = did

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// checkStaleness determines if the dossier needs rebuilding.
func checkStaleness(dossPath string, input RefreshInput) (bool, string) {
	// Check if file exists
	info, err := os.Stat(dossPath)
	if os.IsNotExist(err) {
		return true, "dossier not found"
	}
	if err != nil {
		return true, fmt.Sprintf("stat error: %v", err)
	}

	// Check age
	age := time.Since(info.ModTime())
	ageMinDuration := time.Duration(input.AgeMin) * time.Minute
	if age > ageMinDuration {
		return true, fmt.Sprintf("dossier age %s exceeds %d minutes", age.Round(time.Minute), input.AgeMin)
	}

	// Check git diff --stat count
	diffCount, err := gitDiffCount(input.Root)
	if err != nil {
		// If git diff fails, consider it stale
		return true, fmt.Sprintf("git diff failed: %v", err)
	}
	if diffCount > input.DiffLimit {
		return true, fmt.Sprintf("diff count %d exceeds limit %d", diffCount, input.DiffLimit)
	}

	return false, ""
}

// gitDiffCount returns the number of files changed according to git diff --stat.
func gitDiffCount(root string) (int, error) {
	cmd := exec.Command("git", "diff", "--stat")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return 0, err
	}

	// The last line of git diff --stat contains "N files changed"
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 {
		return 0, nil
	}

	lastLine := lines[len(lines)-1]
	// Parse "N file(s) changed" from the summary line
	parts := strings.Fields(lastLine)
	if len(parts) >= 1 {
		n, err := strconv.Atoi(parts[0])
		if err == nil {
			return n, nil
		}
	}

	return 0, nil
}

// runPipeline runs the dossier rebuild pipeline.
// Pipeline: scan, map, ctrt, flow, doss
func runPipeline(root string) ([]string, error) {
	var did []string
	var lastErr error

	// Ensure .clock directory exists
	clockDir := filepath.Join(root, ".clock")
	if err := os.MkdirAll(clockDir, 0o755); err != nil {
		return did, fmt.Errorf("mkdir .clock: %w", err)
	}

	// Step 1: scan
	scanOut, err := runTool(root, "scan", fmt.Sprintf(`{"root": %q}`, root))
	if err != nil {
		lastErr = fmt.Errorf("scan: %w", err)
	} else {
		did = append(did, "scan")
	}

	// Step 2: map (uses scan output)
	mapInput := scanOut
	if mapInput == "" {
		mapInput = fmt.Sprintf(`{"root": %q}`, root)
	}
	_, err = runTool(root, "map", mapInput)
	if err != nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("map: %w", err)
		}
	} else {
		did = append(did, "map")
	}

	// Step 3: ctrt
	_, err = runTool(root, "ctrt", fmt.Sprintf(`{"root": %q}`, root))
	if err != nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("ctrt: %w", err)
		}
	} else {
		did = append(did, "ctrt")
	}

	// Step 4: flow
	_, err = runTool(root, "flow", fmt.Sprintf(`{"root": %q}`, root))
	if err != nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("flow: %w", err)
		}
	} else {
		did = append(did, "flow")
	}

	// Step 5: doss
	_, err = runTool(root, "doss", fmt.Sprintf(`{"root": %q}`, root))
	if err != nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("doss: %w", err)
		}
	} else {
		did = append(did, "doss")
	}

	return did, lastErr
}

// runTool runs a clock tool by name, passing input via stdin, and returns stdout.
func runTool(root, toolName, input string) (string, error) {
	// Try to find the tool binary relative to the project
	// First try the Go binary in the project
	binName := toolName

	// Look for the tool as a Go cmd in the project
	cmdPath := filepath.Join(root, "cmd", toolName, "main.go")
	if _, err := os.Stat(cmdPath); err == nil {
		// Run with go run
		cmd := exec.Command("go", "run", cmdPath)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%s: %v: %s", toolName, err, stderr.String())
		}
		return stdout.String(), nil
	}

	// Try as a shell script
	scriptPath := filepath.Join(root, "scripts", toolName+".sh")
	if _, err := os.Stat(scriptPath); err == nil {
		cmd := exec.Command("bash", scriptPath)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%s: %v: %s", toolName, err, stderr.String())
		}
		return stdout.String(), nil
	}

	// Try as a binary on PATH
	path, err := exec.LookPath(binName)
	if err != nil {
		return "", fmt.Errorf("tool %q not found", toolName)
	}

	cmd := exec.Command(path)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %v: %s", toolName, err, stderr.String())
	}
	return stdout.String(), nil
}
