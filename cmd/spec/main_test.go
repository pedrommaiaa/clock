package main

import (
	"testing"
)

func TestNameRegex(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"srch", true},
		{"slce", true},
		{"act", true},
		{"abcde", true},
		{"ab", false},       // too short
		{"abcdef", false},   // too long
		{"SRCH", false},     // uppercase
		{"sr1h", false},     // contains digit
		{"sr-h", false},     // contains hyphen
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nameRegex.MatchString(tt.name)
			if got != tt.valid {
				t.Errorf("nameRegex.MatchString(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s    string
		subs []string
		want bool
	}{
		{"search files", []string{"search", "find"}, true},
		{"list directory", []string{"search", "find"}, false},
		{"validate input", []string{"validate", "check"}, true},
		{"", []string{"something"}, false},
		{"anything", []string{}, false},
	}

	for _, tt := range tests {
		got := containsAny(tt.s, tt.subs...)
		if got != tt.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.subs, got, tt.want)
		}
	}
}

func TestExpandPermissions(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"empty", nil, []string{"read:repo"}},
		{"read", []string{"read"}, []string{"read:repo"}},
		{"write", []string{"write"}, []string{"write:repo"}},
		{"run", []string{"run"}, []string{"run:local"}},
		{"net", []string{"net"}, []string{"net:outbound"}},
		{"multiple", []string{"read", "write"}, []string{"read:repo", "write:repo"}},
		{"already qualified", []string{"read:repo"}, []string{"read:repo"}},
		{"dedup", []string{"read", "read"}, []string{"read:repo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPermissions(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("expandPermissions(%v) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expandPermissions(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGenerateSchemas_FileGoal(t *testing.T) {
	inputs, outputs := generateSchemas("scan source files")
	inProps := inputs["properties"].(map[string]interface{})
	if _, ok := inProps["paths"]; !ok {
		t.Error("expected 'paths' input property for file-related goal")
	}
	_ = outputs
}

func TestGenerateSchemas_SearchGoal(t *testing.T) {
	inputs, outputs := generateSchemas("search codebase for patterns")
	inProps := inputs["properties"].(map[string]interface{})
	outProps := outputs["properties"].(map[string]interface{})
	if _, ok := inProps["query"]; !ok {
		t.Error("expected 'query' input property for search goal")
	}
	if _, ok := outProps["results"]; !ok {
		t.Error("expected 'results' output property for search goal")
	}
}

func TestGenerateSchemas_TransformGoal(t *testing.T) {
	inputs, outputs := generateSchemas("transform data format")
	inProps := inputs["properties"].(map[string]interface{})
	outProps := outputs["properties"].(map[string]interface{})
	if _, ok := inProps["input"]; !ok {
		t.Error("expected 'input' property for transform goal")
	}
	if _, ok := outProps["output"]; !ok {
		t.Error("expected 'output' property for transform goal")
	}
}

func TestGenerateSchemas_ValidationGoal(t *testing.T) {
	_, outputs := generateSchemas("validate configuration")
	outProps := outputs["properties"].(map[string]interface{})
	if _, ok := outProps["ok"]; !ok {
		t.Error("expected 'ok' output property for validation goal")
	}
	if _, ok := outProps["issues"]; !ok {
		t.Error("expected 'issues' output property for validation goal")
	}
}

func TestGenerateSchemas_GenericGoal(t *testing.T) {
	inputs, outputs := generateSchemas("do something completely novel")
	inProps := inputs["properties"].(map[string]interface{})
	outProps := outputs["properties"].(map[string]interface{})
	if _, ok := inProps["input"]; !ok {
		t.Error("expected generic 'input' property for unrecognized goal")
	}
	if _, ok := outProps["result"]; !ok {
		t.Error("expected generic 'result' property for unrecognized goal")
	}
}

func TestGenerateTestPlan(t *testing.T) {
	tests := []struct {
		goal          string
		lang          string
		wantUnit      bool
		wantConform   bool
		minUnitTests  int
	}{
		{"scan source files", "go", true, true, 3},
		{"search for patterns", "go", true, true, 3},
		{"validate config", "go", true, true, 3},
		{"something generic", "go", true, true, 3},
	}

	for _, tt := range tests {
		t.Run(tt.goal, func(t *testing.T) {
			plan := generateTestPlan(tt.goal, tt.lang)
			if len(plan.UnitTests) < tt.minUnitTests {
				t.Errorf("expected at least %d unit tests, got %d", tt.minUnitTests, len(plan.UnitTests))
			}
			if len(plan.Conformance) < 3 {
				t.Errorf("expected at least 3 conformance tests, got %d", len(plan.Conformance))
			}
		})
	}
}

func TestGenerateExamples(t *testing.T) {
	inputs := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "search query",
			},
		},
	}
	outputs := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"results": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "object"},
				"description": "search results",
			},
		},
	}

	examples := generateExamples("search for code", inputs, outputs)
	if len(examples) == 0 {
		t.Fatal("expected at least one example")
	}
	ex := examples[0]
	inMap, ok := ex.Input.(map[string]interface{})
	if !ok {
		t.Fatal("expected example input to be a map")
	}
	if _, ok := inMap["query"]; !ok {
		t.Error("expected 'query' key in example input")
	}
}
