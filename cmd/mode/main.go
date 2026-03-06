// Command mode is an autonomy mode controller for Clock.
// It manages the current operating mode (read, suggest, pr, auto, ops)
// and validates whether actions are permitted in the current mode.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const modeFile = ".clock/mode.json"

// ModeState is the persisted mode state.
type ModeState struct {
	Mode         string   `json:"mode"`
	Capabilities []string `json:"capabilities"`
}

// ModeCheckResult is the output of `mode check`.
type ModeCheckResult struct {
	Allowed bool   `json:"allowed"`
	Mode    string `json:"mode"`
	Action  string `json:"action"`
}

// Known modes and their allowed action sets (cumulative).
// Each higher mode includes all capabilities of lower modes.
var modeHierarchy = []string{"read", "suggest", "pr", "auto", "ops"}

// modeCapabilities maps each mode to its cumulative capabilities.
var modeCapabilities = map[string][]string{
	"read":    {"analysis", "srch", "slce", "map", "ctrt", "flow", "doss", "pack", "llm-question"},
	"suggest": {"analysis", "srch", "slce", "map", "ctrt", "flow", "doss", "pack", "llm-question", "generate-diff"},
	"pr":      {"analysis", "srch", "slce", "map", "ctrt", "flow", "doss", "pack", "llm-question", "generate-diff", "apply-diff", "create-pr"},
	"auto":    {"analysis", "srch", "slce", "map", "ctrt", "flow", "doss", "pack", "llm-question", "generate-diff", "apply-diff", "create-pr", "apply-branch"},
	"ops":     {"analysis", "srch", "slce", "map", "ctrt", "flow", "doss", "pack", "llm-question", "generate-diff", "apply-diff", "create-pr", "apply-branch", "multi-repo"},
}

// actionModeMap maps actions to the minimum mode required.
var actionModeMap = map[string]string{
	// read-level actions
	"analysis":     "read",
	"srch":         "read",
	"slce":         "read",
	"map":          "read",
	"ctrt":         "read",
	"flow":         "read",
	"doss":         "read",
	"pack":         "read",
	"llm-question": "read",
	"llm":          "read",
	"scan":         "read",
	"scope":        "read",
	"read":         "read",
	// suggest-level
	"generate-diff": "suggest",
	"suggest":       "suggest",
	"diff":          "suggest",
	// pr-level
	"apply-diff": "pr",
	"create-pr":  "pr",
	"apply":      "pr",
	"aply":       "pr",
	"pr":         "pr",
	// auto-level
	"apply-branch": "auto",
	"auto":         "auto",
	"commit":       "auto",
	"push":         "auto",
	// ops-level
	"multi-repo": "ops",
	"campaign":   "ops",
	"ops":        "ops",
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		jsonutil.Fatal("usage: mode <get|set|check> [args]")
	}

	switch args[0] {
	case "get":
		doGet()
	case "set":
		if len(args) < 2 {
			jsonutil.Fatal("usage: mode set <mode>")
		}
		doSet(args[1])
	case "check":
		if len(args) < 2 {
			jsonutil.Fatal("usage: mode check <action>")
		}
		doCheck(args[1])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", args[0]))
	}
}

func doGet() {
	state := loadMode()
	if err := jsonutil.WriteOutput(state); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doSet(mode string) {
	if !isValidMode(mode) {
		jsonutil.Fatal(fmt.Sprintf("unknown mode: %s (valid: read, suggest, pr, auto, ops)", mode))
	}

	state := ModeState{
		Mode:         mode,
		Capabilities: modeCapabilities[mode],
	}
	saveMode(state)

	if err := jsonutil.WriteOutput(state); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doCheck(action string) {
	state := loadMode()

	allowed := isActionAllowed(state.Mode, action)

	result := ModeCheckResult{
		Allowed: allowed,
		Mode:    state.Mode,
		Action:  action,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func isValidMode(mode string) bool {
	for _, m := range modeHierarchy {
		if m == mode {
			return true
		}
	}
	return false
}

func modeLevel(mode string) int {
	for i, m := range modeHierarchy {
		if m == mode {
			return i
		}
	}
	return -1
}

func isActionAllowed(currentMode, action string) bool {
	requiredMode, known := actionModeMap[action]
	if !known {
		// Unknown actions are allowed only in auto or ops mode
		currentLevel := modeLevel(currentMode)
		return currentLevel >= modeLevel("auto")
	}

	currentLevel := modeLevel(currentMode)
	requiredLevel := modeLevel(requiredMode)
	return currentLevel >= requiredLevel
}

func loadMode() ModeState {
	state := ModeState{
		Mode:         "suggest",
		Capabilities: modeCapabilities["suggest"],
	}

	data, err := os.ReadFile(modeFile)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)

	// Ensure capabilities are populated
	if caps, ok := modeCapabilities[state.Mode]; ok {
		state.Capabilities = caps
	}
	return state
}

func saveMode(state ModeState) {
	dir := filepath.Dir(modeFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("create dir: %v", err))
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal state: %v", err))
	}
	if err := os.WriteFile(modeFile, data, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write state: %v", err))
	}
}
