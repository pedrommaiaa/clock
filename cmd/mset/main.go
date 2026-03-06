// Command mset is a model preset manager for lmux.
// It manages role-to-model mappings stored in .clock/models.json,
// supporting both single models and fallback arrays.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const modelsFile = ".clock/models.json"

// validPrefixes lists recognized LLM provider prefixes.
var validPrefixes = []string{"anth:", "oai:", "oll:", "vllm:", "lcpp:"}

// setInput is the stdin payload for the "set" subcommand.
type setInput struct {
	Action string          `json:"action"`
	Role   string          `json:"role"`
	Model  json.RawMessage `json:"model"`
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		jsonutil.Fatal("usage: mset <set|remove|get|list|resolve> [args]")
	}

	switch args[0] {
	case "set":
		doSet()
	case "remove":
		if len(args) < 2 {
			jsonutil.Fatal("usage: mset remove <role>")
		}
		doRemove(args[1])
	case "get":
		if len(args) < 2 {
			jsonutil.Fatal("usage: mset get <role>")
		}
		doGet(args[1])
	case "list":
		doList()
	case "resolve":
		if len(args) < 2 {
			jsonutil.Fatal("usage: mset resolve <role>")
		}
		doResolve(args[1])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", args[0]))
	}
}

// doSet reads a set command from stdin and updates the models file.
func doSet() {
	var input setInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}
	if input.Role == "" {
		jsonutil.Fatal("role is required")
	}
	if len(input.Model) == 0 {
		jsonutil.Fatal("model is required")
	}

	// Parse model: either a string or an array of strings.
	model, err := parseModel(input.Model)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	// Validate all model names.
	if err := validateModels(model); err != nil {
		jsonutil.Fatal(err.Error())
	}

	presets := loadPresets()
	presets[input.Role] = model
	savePresets(presets)

	out := map[string]interface{}{
		"ok":    true,
		"role":  input.Role,
		"model": model,
	}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doRemove removes a role mapping from the models file.
func doRemove(role string) {
	presets := loadPresets()
	if _, ok := presets[role]; !ok {
		jsonutil.Fatal(fmt.Sprintf("role %q not found", role))
	}
	delete(presets, role)
	savePresets(presets)

	out := map[string]interface{}{
		"ok":     true,
		"role":   role,
		"status": "removed",
	}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doGet retrieves the model mapping for a specific role.
func doGet(role string) {
	presets := loadPresets()
	model, ok := presets[role]
	if !ok {
		jsonutil.Fatal(fmt.Sprintf("role %q not found", role))
	}

	out := map[string]interface{}{
		"ok":    true,
		"role":  role,
		"model": model,
	}

	// If it's an array, include "primary" field (first element).
	if arr, isArr := model.([]interface{}); isArr && len(arr) > 0 {
		out["primary"] = arr[0]
	}

	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doList outputs all role-to-model mappings.
func doList() {
	presets := loadPresets()
	out := map[string]interface{}{
		"ok":    true,
		"roles": presets,
	}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doResolve outputs the primary model for a role as raw text (no JSON).
func doResolve(role string) {
	presets := loadPresets()
	model, ok := presets[role]
	if !ok {
		jsonutil.Fatal(fmt.Sprintf("role %q not found", role))
	}

	switch v := model.(type) {
	case string:
		fmt.Println(v)
	case []interface{}:
		if len(v) == 0 {
			jsonutil.Fatal(fmt.Sprintf("role %q has empty model array", role))
		}
		fmt.Println(v[0])
	default:
		jsonutil.Fatal(fmt.Sprintf("role %q has invalid model type", role))
	}
}

// parseModel parses a JSON value that is either a string or []string.
// Returns the value as-is (string or []interface{}) for storage.
func parseModel(raw json.RawMessage) (interface{}, error) {
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	// Try array of strings.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return nil, fmt.Errorf("model array must not be empty")
		}
		// Convert to []interface{} for consistent JSON handling.
		result := make([]interface{}, len(arr))
		for i, v := range arr {
			result[i] = v
		}
		return result, nil
	}

	return nil, fmt.Errorf("model must be a string or array of strings")
}

// validateModels checks that all model names have valid provider prefixes.
func validateModels(model interface{}) error {
	switch v := model.(type) {
	case string:
		return validatePrefix(v)
	case []interface{}:
		for _, m := range v {
			s, ok := m.(string)
			if !ok {
				return fmt.Errorf("model array must contain only strings")
			}
			if err := validatePrefix(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePrefix checks that a model name starts with a valid provider prefix.
func validatePrefix(model string) error {
	for _, p := range validPrefixes {
		if strings.HasPrefix(model, p) {
			return nil
		}
	}
	return fmt.Errorf("invalid model %q: must start with one of %v", model, validPrefixes)
}

// loadPresets reads the models.json file. Returns empty map if file doesn't exist.
func loadPresets() map[string]interface{} {
	presets := make(map[string]interface{})
	data, err := os.ReadFile(modelsFile)
	if err != nil {
		return presets
	}
	_ = json.Unmarshal(data, &presets)
	return presets
}

// savePresets writes the models.json file atomically (write tmp, rename).
func savePresets(presets map[string]interface{}) {
	dir := filepath.Dir(modelsFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("create dir: %v", err))
	}

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal presets: %v", err))
	}
	data = append(data, '\n')

	// Atomic write: write to temp file, then rename.
	tmp := modelsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write temp file: %v", err))
	}
	if err := os.Rename(tmp, modelsFile); err != nil {
		os.Remove(tmp)
		jsonutil.Fatal(fmt.Sprintf("rename temp file: %v", err))
	}
}
