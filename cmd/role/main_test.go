package main

import (
	"testing"
)

func TestBuiltinRolesExist(t *testing.T) {
	expected := []string{"plan", "edit", "revw", "test", "sec", "perf"}
	for _, role := range expected {
		if _, ok := builtinRoles[role]; !ok {
			t.Errorf("missing built-in role: %s", role)
		}
	}
}

func TestBuiltinRolesHaveRequiredFields(t *testing.T) {
	for name, def := range builtinRoles {
		t.Run(name, func(t *testing.T) {
			if def.System == "" {
				t.Error("System prompt is empty")
			}
			if len(def.Tools) == 0 {
				t.Error("Tools list is empty")
			}
			if def.Rubric.Focus == "" {
				t.Error("Rubric focus is empty")
			}
			if len(def.Rubric.Criteria) == 0 {
				t.Error("Rubric criteria is empty")
			}
		})
	}
}

func TestRolePolicies(t *testing.T) {
	tests := []struct {
		role     string
		canWrite bool
		canRun   bool
		canNet   bool
	}{
		{"plan", false, false, false},
		{"edit", true, false, false},
		{"revw", false, false, false},
		{"test", false, true, false},
		{"sec", false, false, false},
		{"perf", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			def := builtinRoles[tt.role]
			if def.Policy.CanWrite != tt.canWrite {
				t.Errorf("CanWrite = %v, want %v", def.Policy.CanWrite, tt.canWrite)
			}
			if def.Policy.CanRun != tt.canRun {
				t.Errorf("CanRun = %v, want %v", def.Policy.CanRun, tt.canRun)
			}
			if def.Policy.CanNet != tt.canNet {
				t.Errorf("CanNet = %v, want %v", def.Policy.CanNet, tt.canNet)
			}
		})
	}
}

func TestRoleToolLists(t *testing.T) {
	tests := []struct {
		role          string
		expectContain []string
	}{
		{"plan", []string{"srch", "slce", "map"}},
		{"edit", []string{"srch", "slce", "pack", "llm", "aply"}},
		{"revw", []string{"srch", "slce", "guard"}},
		{"test", []string{"vrfy", "exec"}},
		{"sec", []string{"srch", "guard", "risk"}},
		{"perf", []string{"srch", "slce", "exec"}},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			def := builtinRoles[tt.role]
			toolSet := make(map[string]bool)
			for _, tool := range def.Tools {
				toolSet[tool] = true
			}
			for _, expected := range tt.expectContain {
				if !toolSet[expected] {
					t.Errorf("role %q tools %v missing %q", tt.role, def.Tools, expected)
				}
			}
		})
	}
}

func TestUnknownRoleNotInMap(t *testing.T) {
	unknown := []string{"admin", "root", "deploy", ""}
	for _, role := range unknown {
		if _, ok := builtinRoles[role]; ok {
			t.Errorf("unexpected role found in builtinRoles: %q", role)
		}
	}
}
