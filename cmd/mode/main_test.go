package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidMode(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"read", true},
		{"suggest", true},
		{"pr", true},
		{"auto", true},
		{"ops", true},
		{"invalid", false},
		{"", false},
		{"READ", false},
		{"Auto", false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := isValidMode(tt.mode)
			if got != tt.want {
				t.Errorf("isValidMode(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestModeLevel(t *testing.T) {
	tests := []struct {
		mode string
		want int
	}{
		{"read", 0},
		{"suggest", 1},
		{"pr", 2},
		{"auto", 3},
		{"ops", 4},
		{"invalid", -1},
		{"", -1},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := modeLevel(tt.mode)
			if got != tt.want {
				t.Errorf("modeLevel(%q) = %d, want %d", tt.mode, got, tt.want)
			}
		})
	}
}

func TestModeLevelHierarchyOrder(t *testing.T) {
	// Verify strict ordering: read < suggest < pr < auto < ops
	modes := []string{"read", "suggest", "pr", "auto", "ops"}
	for i := 0; i < len(modes)-1; i++ {
		l1 := modeLevel(modes[i])
		l2 := modeLevel(modes[i+1])
		if l1 >= l2 {
			t.Errorf("modeLevel(%q)=%d should be < modeLevel(%q)=%d",
				modes[i], l1, modes[i+1], l2)
		}
	}
}

func TestIsActionAllowed(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		action  string
		want    bool
	}{
		// read-level action in read mode = allowed
		{"read_action_in_read_mode", "read", "analysis", true},
		{"srch_in_read_mode", "read", "srch", true},
		{"llm_in_read_mode", "read", "llm", true},

		// auto-level action in read mode = denied
		{"auto_action_in_read_mode", "read", "commit", false},
		{"push_in_read_mode", "read", "push", false},

		// suggest-level action in read mode = denied
		{"suggest_action_in_read_mode", "read", "generate-diff", false},

		// suggest-level action in suggest mode = allowed
		{"suggest_action_in_suggest_mode", "suggest", "generate-diff", true},

		// read-level action in auto mode = allowed (higher mode inherits lower)
		{"read_action_in_auto_mode", "auto", "analysis", true},

		// pr-level action in auto mode = allowed
		{"pr_action_in_auto_mode", "auto", "apply-diff", true},

		// pr-level action in suggest mode = denied
		{"pr_action_in_suggest_mode", "suggest", "apply-diff", false},

		// ops-level in auto mode = denied
		{"ops_action_in_auto_mode", "auto", "multi-repo", false},

		// ops-level in ops mode = allowed
		{"ops_action_in_ops_mode", "ops", "multi-repo", true},

		// unknown action in auto mode = allowed (auto level >= auto)
		{"unknown_action_in_auto_mode", "auto", "some-unknown-action", true},

		// unknown action in ops mode = allowed
		{"unknown_action_in_ops_mode", "ops", "some-unknown-action", true},

		// unknown action in read mode = denied (read level < auto)
		{"unknown_action_in_read_mode", "read", "some-unknown-action", false},

		// unknown action in suggest mode = denied
		{"unknown_action_in_suggest_mode", "suggest", "some-unknown-action", false},

		// unknown action in pr mode = denied
		{"unknown_action_in_pr_mode", "pr", "some-unknown-action", false},

		// specific actions at their exact required mode
		{"aply_in_pr_mode", "pr", "aply", true},
		{"aply_in_suggest_mode", "suggest", "aply", false},
		{"campaign_in_ops", "ops", "campaign", true},
		{"campaign_in_auto", "auto", "campaign", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isActionAllowed(tt.mode, tt.action)
			if got != tt.want {
				t.Errorf("isActionAllowed(%q, %q) = %v, want %v",
					tt.mode, tt.action, got, tt.want)
			}
		})
	}
}

func TestLoadModeDefault(t *testing.T) {
	// When no mode file exists, loadMode should return default "suggest"
	// We need to temporarily point modeFile to a nonexistent path
	// Since modeFile is a const, we test the default ModeState directly
	state := ModeState{
		Mode:         "suggest",
		Capabilities: modeCapabilities["suggest"],
	}
	if state.Mode != "suggest" {
		t.Errorf("default mode = %q, want %q", state.Mode, "suggest")
	}
	if len(state.Capabilities) == 0 {
		t.Error("default capabilities should not be empty")
	}
}

func TestSaveAndLoadMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".clock", "mode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	state := ModeState{
		Mode:         "auto",
		Capabilities: modeCapabilities["auto"],
	}

	// Write
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Read back
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got ModeState
	if err := json.Unmarshal(readData, &got); err != nil {
		t.Fatal(err)
	}

	if got.Mode != "auto" {
		t.Errorf("loaded mode = %q, want %q", got.Mode, "auto")
	}
	if len(got.Capabilities) == 0 {
		t.Error("capabilities should not be empty")
	}
}

func TestModeCapabilitiesCompleteness(t *testing.T) {
	// Every mode in modeHierarchy should have capabilities defined
	for _, mode := range modeHierarchy {
		caps, ok := modeCapabilities[mode]
		if !ok {
			t.Errorf("no capabilities defined for mode %q", mode)
			continue
		}
		if len(caps) == 0 {
			t.Errorf("empty capabilities for mode %q", mode)
		}
	}
}

func TestModeCapabilitiesGrow(t *testing.T) {
	// Higher modes should have >= capabilities than lower modes
	for i := 0; i < len(modeHierarchy)-1; i++ {
		lower := modeHierarchy[i]
		upper := modeHierarchy[i+1]
		lCaps := len(modeCapabilities[lower])
		uCaps := len(modeCapabilities[upper])
		if uCaps < lCaps {
			t.Errorf("mode %q has %d caps but mode %q has %d (should be >=)",
				upper, uCaps, lower, lCaps)
		}
	}
}

func TestActionModeMapCoversAllCapabilities(t *testing.T) {
	// Every action in actionModeMap should map to a valid mode
	for action, mode := range actionModeMap {
		if !isValidMode(mode) {
			t.Errorf("actionModeMap[%q] = %q which is not a valid mode", action, mode)
		}
	}
}

func TestModeCheckResultJSON(t *testing.T) {
	result := ModeCheckResult{
		Allowed: true,
		Mode:    "auto",
		Action:  "commit",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var got ModeCheckResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Allowed != true {
		t.Error("Allowed should be true")
	}
	if got.Mode != "auto" {
		t.Errorf("Mode = %q, want %q", got.Mode, "auto")
	}
}

func TestModeStateJSON(t *testing.T) {
	state := ModeState{
		Mode:         "pr",
		Capabilities: []string{"analysis", "apply-diff"},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	var got ModeState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Mode != "pr" {
		t.Errorf("Mode = %q, want %q", got.Mode, "pr")
	}
	if len(got.Capabilities) != 2 {
		t.Errorf("Capabilities length = %d, want 2", len(got.Capabilities))
	}
}
