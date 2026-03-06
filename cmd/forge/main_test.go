package main

import (
	"strings"
	"testing"
)

func TestCapitalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "Hello"},
		{"", ""},
		{"A", "A"},
		{"abc", "Abc"},
		{"helloWorld", "HelloWorld"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := capitalize(tt.input)
			if got != tt.want {
				t.Errorf("capitalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestJsonTypeToGo(t *testing.T) {
	tests := []struct {
		name string
		prop map[string]interface{}
		want string
	}{
		{"string", map[string]interface{}{"type": "string"}, "string"},
		{"number", map[string]interface{}{"type": "number"}, "float64"},
		{"integer", map[string]interface{}{"type": "integer"}, "int"},
		{"boolean", map[string]interface{}{"type": "boolean"}, "bool"},
		{"object", map[string]interface{}{"type": "object"}, "map[string]interface{}"},
		{"array of strings", map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "string"},
		}, "[]string"},
		{"array no items", map[string]interface{}{"type": "array"}, "[]interface{}"},
		{"unknown", map[string]interface{}{"type": "foobar"}, "interface{}"},
		{"no type", map[string]interface{}{}, "interface{}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonTypeToGo(tt.prop)
			if got != tt.want {
				t.Errorf("jsonTypeToGo(%v) = %q, want %q", tt.prop, got, tt.want)
			}
		})
	}
}

func TestSchemaToStructFields(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "search query",
			},
		},
	}

	result := schemaToStructFields(schema, "Input")
	if !strings.Contains(result, "Query") {
		t.Error("expected capitalized field name 'Query'")
	}
	if !strings.Contains(result, "string") {
		t.Error("expected Go type 'string'")
	}
	if !strings.Contains(result, `json:"query"`) {
		t.Error("expected json tag")
	}
	if !strings.Contains(result, "search query") {
		t.Error("expected description comment")
	}
}

func TestSchemaToStructFields_NoProperties(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
	}

	result := schemaToStructFields(schema, "Input")
	if !strings.Contains(result, "No properties") {
		t.Errorf("expected 'No properties' message, got %q", result)
	}
}

func TestSchemaToStructFields_EmptyProperties(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	result := schemaToStructFields(schema, "Input")
	if !strings.Contains(result, "No properties") {
		t.Errorf("expected 'No properties' for empty properties, got %q", result)
	}
}

func TestGenerateManifest(t *testing.T) {
	spec := ToolSpec{
		Tool:        "srch",
		Version:     "0.1.0",
		Goal:        "search files",
		Lang:        "go",
		Permissions: []string{"read:repo"},
		Entrypoint:  "cmd/srch/main.go",
		Inputs:      map[string]interface{}{"type": "object"},
		Outputs:     map[string]interface{}{"type": "object"},
	}

	manifest := generateManifest(spec)
	if manifest["name"] != "srch" {
		t.Errorf("expected name=srch, got %v", manifest["name"])
	}
	if manifest["version"] != "0.1.0" {
		t.Errorf("expected version=0.1.0, got %v", manifest["version"])
	}
	if manifest["risk_class"] != "low" {
		t.Errorf("expected risk_class=low for read-only, got %v", manifest["risk_class"])
	}
}

func TestGenerateManifest_MediumRisk(t *testing.T) {
	spec := ToolSpec{
		Tool:        "aply",
		Version:     "0.1.0",
		Permissions: []string{"read:repo", "write:repo"},
	}

	manifest := generateManifest(spec)
	if manifest["risk_class"] != "medium" {
		t.Errorf("expected risk_class=medium for write permissions, got %v", manifest["risk_class"])
	}
}

func TestGenerateManifest_HighRisk(t *testing.T) {
	spec := ToolSpec{
		Tool:        "llm",
		Version:     "0.1.0",
		Permissions: []string{"net:outbound"},
	}

	manifest := generateManifest(spec)
	if manifest["risk_class"] != "high" {
		t.Errorf("expected risk_class=high for net permissions, got %v", manifest["risk_class"])
	}
}

func TestGenerateMainGo(t *testing.T) {
	spec := ToolSpec{
		Tool:    "srch",
		Goal:    "search files",
		Lang:    "go",
		Inputs:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}},
		Outputs: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"results": map[string]interface{}{"type": "array"}}},
	}

	result := generateMainGo(spec)
	if !strings.Contains(result, "SrchInput") {
		t.Error("expected SrchInput type in generated code")
	}
	if !strings.Contains(result, "SrchOutput") {
		t.Error("expected SrchOutput type in generated code")
	}
	if !strings.Contains(result, "package main") {
		t.Error("expected package main in generated code")
	}
}

func TestGenerateTestGo(t *testing.T) {
	spec := ToolSpec{
		Tool: "srch",
		TestPlan: TestPlan{
			UnitTests:   []string{"test basic search"},
			Conformance: []string{"valid JSON output"},
		},
		Examples: []Example{
			{
				Input:  map[string]interface{}{"query": "hello"},
				Output: map[string]interface{}{"results": []interface{}{}},
			},
		},
	}

	result := generateTestGo(spec)
	if !strings.Contains(result, "package main") {
		t.Error("expected package main")
	}
	if !strings.Contains(result, "testing") {
		t.Error("expected testing import")
	}
	if !strings.Contains(result, "Test_srch_1") {
		t.Error("expected Test_srch_1 function")
	}
	if !strings.Contains(result, "TestConformance_1") {
		t.Error("expected TestConformance_1 function")
	}
	if !strings.Contains(result, "TestExample_1") {
		t.Error("expected TestExample_1 function")
	}
}
