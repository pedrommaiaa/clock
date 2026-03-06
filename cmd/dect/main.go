// Command dect performs repo capability detection.
// It reads a JSON input with a root path from stdin, scans for project
// markers (go.mod, package.json, Cargo.toml, etc.), and outputs detected
// commands for formatting, linting, testing, and building.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// DectInput is the input schema for the dect tool.
type DectInput struct {
	Root string `json:"root"`
}

// DectOutput is the output of the dect tool.
type DectOutput struct {
	Fmt   []string `json:"fmt,omitempty"`
	Lint  []string `json:"lint,omitempty"`
	Test  []string `json:"test,omitempty"`
	Build []string `json:"build,omitempty"`
	Notes []string `json:"notes,omitempty"`
}

func main() {
	var input DectInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	root := input.Root
	if root == "" {
		root = "."
	}

	// Resolve to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("resolve root: %v", err))
	}

	output := DectOutput{}

	// Detect Go
	if fileExists(filepath.Join(absRoot, "go.mod")) {
		output.Fmt = appendUnique(output.Fmt, "gofmt -l .")
		output.Lint = appendUnique(output.Lint, "go vet ./...")
		output.Test = appendUnique(output.Test, "go test ./...")
		output.Build = appendUnique(output.Build, "go build ./...")
		output.Notes = append(output.Notes, "detected Go project via go.mod")
	}

	// Detect Node.js
	pkgPath := filepath.Join(absRoot, "package.json")
	if fileExists(pkgPath) {
		detectNode(pkgPath, &output)
	}

	// Detect Python
	if fileExists(filepath.Join(absRoot, "pyproject.toml")) {
		output.Test = appendUnique(output.Test, "pytest")
		output.Lint = appendUnique(output.Lint, "ruff check .")
		output.Fmt = appendUnique(output.Fmt, "black .")
		output.Notes = append(output.Notes, "detected Python project via pyproject.toml")
	} else if fileExists(filepath.Join(absRoot, "setup.py")) {
		output.Test = appendUnique(output.Test, "pytest")
		output.Lint = appendUnique(output.Lint, "ruff check .")
		output.Fmt = appendUnique(output.Fmt, "black .")
		output.Notes = append(output.Notes, "detected Python project via setup.py")
	}

	// Detect Rust
	if fileExists(filepath.Join(absRoot, "Cargo.toml")) {
		output.Test = appendUnique(output.Test, "cargo test")
		output.Build = appendUnique(output.Build, "cargo build")
		output.Fmt = appendUnique(output.Fmt, "cargo fmt")
		output.Lint = appendUnique(output.Lint, "cargo clippy")
		output.Notes = append(output.Notes, "detected Rust project via Cargo.toml")
	}

	// Detect Makefile targets
	makefilePath := filepath.Join(absRoot, "Makefile")
	if fileExists(makefilePath) {
		detectMakefile(makefilePath, &output)
	}

	// Detect CI
	detectCI(absRoot, &output)

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// appendUnique appends s to slice only if it's not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// detectNode parses package.json to find npm scripts.
func detectNode(pkgPath string, output *DectOutput) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	runner := "npm run"
	// Use a simpler detection — just check scripts
	output.Notes = append(output.Notes, "detected Node project via package.json")

	if _, ok := pkg.Scripts["test"]; ok {
		output.Test = appendUnique(output.Test, runner+" test")
	}
	if _, ok := pkg.Scripts["lint"]; ok {
		output.Lint = appendUnique(output.Lint, runner+" lint")
	}
	if _, ok := pkg.Scripts["build"]; ok {
		output.Build = appendUnique(output.Build, runner+" build")
	}
	if _, ok := pkg.Scripts["format"]; ok {
		output.Fmt = appendUnique(output.Fmt, runner+" format")
	}
	if _, ok := pkg.Scripts["fmt"]; ok {
		output.Fmt = appendUnique(output.Fmt, runner+" fmt")
	}
	if _, ok := pkg.Scripts["prettier"]; ok {
		output.Fmt = appendUnique(output.Fmt, runner+" prettier")
	}
}

// detectMakefile parses a Makefile and extracts target names.
func detectMakefile(path string, output *DectOutput) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	var targets []string
	for _, line := range lines {
		// A Makefile target line looks like: target: [deps]
		if len(line) == 0 || line[0] == '\t' || line[0] == '#' || line[0] == ' ' || line[0] == '.' {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx <= 0 {
			continue
		}
		// Skip variable assignments (NAME := value)
		beforeColon := line[:colonIdx]
		if strings.ContainsAny(beforeColon, "?+=") {
			continue
		}
		target := strings.TrimSpace(beforeColon)
		if target == "" || strings.ContainsAny(target, " \t$%") {
			continue
		}
		targets = append(targets, target)
	}

	if len(targets) > 0 {
		output.Notes = append(output.Notes, fmt.Sprintf("detected Makefile with targets: %s", strings.Join(targets, ", ")))

		// Map well-known targets to commands
		for _, t := range targets {
			switch t {
			case "test", "tests", "check":
				output.Test = appendUnique(output.Test, "make "+t)
			case "lint":
				output.Lint = appendUnique(output.Lint, "make "+t)
			case "fmt", "format":
				output.Fmt = appendUnique(output.Fmt, "make "+t)
			case "build", "all":
				output.Build = appendUnique(output.Build, "make "+t)
			}
		}
	}
}

// detectCI detects CI configuration files.
func detectCI(root string, output *DectOutput) {
	ciChecks := []struct {
		path string
		name string
	}{
		{filepath.Join(root, ".github", "workflows"), "GitHub Actions"},
		{filepath.Join(root, ".gitlab-ci.yml"), "GitLab CI"},
		{filepath.Join(root, "Jenkinsfile"), "Jenkins"},
		{filepath.Join(root, ".circleci"), "CircleCI"},
		{filepath.Join(root, ".travis.yml"), "Travis CI"},
		{filepath.Join(root, "azure-pipelines.yml"), "Azure Pipelines"},
		{filepath.Join(root, "bitbucket-pipelines.yml"), "Bitbucket Pipelines"},
		{filepath.Join(root, ".drone.yml"), "Drone CI"},
	}

	for _, ci := range ciChecks {
		if fileExists(ci.path) || dirExists(ci.path) {
			output.Notes = append(output.Notes, fmt.Sprintf("detected CI: %s", ci.name))
		}
	}
}
