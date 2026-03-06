// Command spec generates machine-readable tool specifications.
// It reads a SpecInput JSON from stdin and outputs a ToolSpec JSON,
// also writing the spec to .clock/specs/<name>.json.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// SpecInput is the input schema for the spec tool.
type SpecInput struct {
	Name        string   `json:"name"`
	Goal        string   `json:"goal"`
	IO          string   `json:"io"`          // "json" (default)
	Lang        string   `json:"lang"`        // "go" (default)
	Permissions []string `json:"permissions"` // e.g. ["read", "write", "run", "net"]
}

// ToolSpec is the generated tool specification.
type ToolSpec struct {
	Tool        string                 `json:"tool"`
	Version     string                 `json:"version"`
	Goal        string                 `json:"goal"`
	Lang        string                 `json:"lang"`
	Inputs      map[string]interface{} `json:"inputs"`
	Outputs     map[string]interface{} `json:"outputs"`
	Permissions []string               `json:"permissions"`
	TestPlan    TestPlan               `json:"test_plan"`
	Examples    []Example              `json:"examples"`
	Entrypoint  string                 `json:"entrypoint"`
}

// TestPlan describes the test skeleton.
type TestPlan struct {
	UnitTests   []string `json:"unit_tests"`
	Conformance []string `json:"conformance"`
}

// Example is a sample input/output pair.
type Example struct {
	Input  interface{} `json:"input"`
	Output interface{} `json:"output"`
}

// nameRegex validates tool names: 3-5 lowercase letters.
var nameRegex = regexp.MustCompile(`^[a-z]{3,5}$`)

func main() {
	var input SpecInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	// Validate name
	if !nameRegex.MatchString(input.Name) {
		jsonutil.Fatal(fmt.Sprintf("invalid name %q: must be 3-5 lowercase letters", input.Name))
	}

	if input.Goal == "" {
		jsonutil.Fatal("goal is required")
	}
	if input.IO == "" {
		input.IO = "json"
	}
	if input.Lang == "" {
		input.Lang = "go"
	}

	// Generate schemas based on goal keywords
	inputs, outputs := generateSchemas(input.Goal)

	// Expand permissions
	permissions := expandPermissions(input.Permissions)

	// Generate test plan
	testPlan := generateTestPlan(input.Goal, input.Lang)

	// Generate examples
	examples := generateExamples(input.Goal, inputs, outputs)

	spec := ToolSpec{
		Tool:        input.Name,
		Version:     "0.1.0",
		Goal:        input.Goal,
		Lang:        input.Lang,
		Inputs:      inputs,
		Outputs:     outputs,
		Permissions: permissions,
		TestPlan:    testPlan,
		Examples:    examples,
		Entrypoint:  fmt.Sprintf("cmd/%s/main.go", input.Name),
	}

	// Write spec to .clock/specs/<name>.json
	specsDir := filepath.Join(".clock", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", specsDir, err))
	}

	specPath := filepath.Join(specsDir, input.Name+".json")
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal spec: %v", err))
	}
	if err := os.WriteFile(specPath, append(specJSON, '\n'), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write spec file: %v", err))
	}

	// Output to stdout
	if err := jsonutil.WriteOutput(spec); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// generateSchemas generates input/output JSON schemas based on goal keywords.
func generateSchemas(goal string) (map[string]interface{}, map[string]interface{}) {
	goalLower := strings.ToLower(goal)

	inputs := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	outputs := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	inProps := inputs["properties"].(map[string]interface{})
	outProps := outputs["properties"].(map[string]interface{})

	// Detect common patterns from goal keywords

	// File/path operations
	if containsAny(goalLower, "file", "path", "source", "code", "extract", "scan", "read") {
		inProps["paths"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "file paths to process",
		}
	}

	// Directory operations
	if containsAny(goalLower, "directory", "folder", "tree", "walk") {
		inProps["root"] = map[string]interface{}{
			"type":        "string",
			"description": "root directory path",
		}
	}

	// Search/query operations
	if containsAny(goalLower, "search", "find", "query", "filter", "match") {
		inProps["query"] = map[string]interface{}{
			"type":        "string",
			"description": "search query or pattern",
		}
		outProps["results"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "object"},
			"description": "search results",
		}
	}

	// Transform operations
	if containsAny(goalLower, "transform", "convert", "format", "generate", "build") {
		inProps["input"] = map[string]interface{}{
			"type":        "string",
			"description": "input data to transform",
		}
		outProps["output"] = map[string]interface{}{
			"type":        "string",
			"description": "transformed output",
		}
	}

	// Variable/environment operations
	if containsAny(goalLower, "variable", "environment", "env", "config") {
		outProps["vars"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "object"},
			"description": "extracted variables",
		}
	}

	// Analysis operations
	if containsAny(goalLower, "analyze", "analysis", "metric", "stat", "count", "measure") {
		outProps["metrics"] = map[string]interface{}{
			"type":        "object",
			"description": "analysis metrics",
		}
	}

	// Validation operations
	if containsAny(goalLower, "validate", "check", "verify", "lint", "test") {
		outProps["ok"] = map[string]interface{}{
			"type":        "boolean",
			"description": "whether validation passed",
		}
		outProps["issues"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "object"},
			"description": "validation issues found",
		}
	}

	// If no specific patterns matched, add generic properties
	if len(inProps) == 0 {
		inProps["input"] = map[string]interface{}{
			"type":        "string",
			"description": "input data",
		}
	}
	if len(outProps) == 0 {
		outProps["result"] = map[string]interface{}{
			"type":        "object",
			"description": "processing result",
		}
	}

	return inputs, outputs
}

// expandPermissions maps short permission names to qualified permission strings.
func expandPermissions(perms []string) []string {
	if len(perms) == 0 {
		return []string{"read:repo"}
	}

	mapping := map[string]string{
		"read":  "read:repo",
		"write": "write:repo",
		"run":   "run:local",
		"net":   "net:outbound",
	}

	var expanded []string
	seen := map[string]bool{}
	for _, p := range perms {
		resolved := p
		if mapped, ok := mapping[p]; ok {
			resolved = mapped
		}
		if !seen[resolved] {
			seen[resolved] = true
			expanded = append(expanded, resolved)
		}
	}
	return expanded
}

// generateTestPlan creates a test plan skeleton.
func generateTestPlan(goal, lang string) TestPlan {
	goalLower := strings.ToLower(goal)

	var unitTests []string
	var conformance []string

	// Standard conformance tests
	conformance = append(conformance,
		"valid JSON output",
		"handles empty input",
		"error output on invalid input",
	)

	// Goal-based unit tests
	if containsAny(goalLower, "file", "source", "code") {
		if lang == "go" {
			unitTests = append(unitTests, "test with Go files")
		}
		unitTests = append(unitTests,
			"test with JS files",
			"test with Python files",
			"test with empty file",
			"test with missing file",
		)
	}

	if containsAny(goalLower, "search", "find", "query") {
		unitTests = append(unitTests,
			"test with matching query",
			"test with no matches",
			"test with regex pattern",
		)
	}

	if containsAny(goalLower, "extract", "parse") {
		unitTests = append(unitTests,
			"test extraction from simple input",
			"test extraction from complex input",
			"test with malformed input",
		)
	}

	if containsAny(goalLower, "validate", "check", "verify") {
		unitTests = append(unitTests,
			"test with valid input",
			"test with invalid input",
			"test boundary conditions",
		)
	}

	// Ensure at least some unit tests
	if len(unitTests) == 0 {
		unitTests = append(unitTests,
			"test basic functionality",
			"test edge cases",
			"test error handling",
		)
	}

	return TestPlan{
		UnitTests:   unitTests,
		Conformance: conformance,
	}
}

// generateExamples creates example input/output pairs.
func generateExamples(goal string, inputSchema, outputSchema map[string]interface{}) []Example {
	goalLower := strings.ToLower(goal)

	var examples []Example

	// Build example input based on schema properties
	exInput := map[string]interface{}{}
	if props, ok := inputSchema["properties"].(map[string]interface{}); ok {
		for key, propI := range props {
			prop, ok := propI.(map[string]interface{})
			if !ok {
				continue
			}
			switch prop["type"] {
			case "string":
				exInput[key] = "example_value"
			case "array":
				if containsAny(goalLower, "file", "path", "source") && key == "paths" {
					exInput[key] = []string{"src/main.go", "src/util.go"}
				} else {
					exInput[key] = []interface{}{"item1", "item2"}
				}
			case "object":
				exInput[key] = map[string]interface{}{}
			case "boolean":
				exInput[key] = true
			case "number", "integer":
				exInput[key] = 0
			}
		}
	}

	// Build example output
	exOutput := map[string]interface{}{}
	if props, ok := outputSchema["properties"].(map[string]interface{}); ok {
		for key, propI := range props {
			prop, ok := propI.(map[string]interface{})
			if !ok {
				continue
			}
			switch prop["type"] {
			case "string":
				exOutput[key] = "example_result"
			case "array":
				exOutput[key] = []interface{}{}
			case "object":
				exOutput[key] = map[string]interface{}{}
			case "boolean":
				exOutput[key] = true
			case "number", "integer":
				exOutput[key] = 0
			}
		}
	}

	examples = append(examples, Example{
		Input:  exInput,
		Output: exOutput,
	})

	return examples
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
