package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupMsetTest(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	os.MkdirAll(filepath.Join(dir, ".clock"), 0o755)
	return func() { os.Chdir(origDir) }
}

func TestParseModelString(t *testing.T) {
	raw := json.RawMessage(`"anth:sonnet"`)
	result, err := parseModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "anth:sonnet" {
		t.Errorf("expected 'anth:sonnet', got %q", s)
	}
}

func TestParseModelArray(t *testing.T) {
	raw := json.RawMessage(`["anth:sonnet","oai:gpt4o"]`)
	result, err := parseModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
	if arr[0].(string) != "anth:sonnet" {
		t.Errorf("first element = %v, want anth:sonnet", arr[0])
	}
}

func TestParseModelEmptyArray(t *testing.T) {
	raw := json.RawMessage(`[]`)
	_, err := parseModel(raw)
	if err == nil {
		t.Error("expected error for empty array")
	}
}

func TestParseModelInvalid(t *testing.T) {
	raw := json.RawMessage(`123`)
	_, err := parseModel(raw)
	if err == nil {
		t.Error("expected error for numeric input")
	}
}

func TestValidatePrefix(t *testing.T) {
	tests := []struct {
		model   string
		wantErr bool
	}{
		{"anth:sonnet", false},
		{"oai:gpt4o", false},
		{"oll:llama3", false},
		{"vllm:mistral", false},
		{"lcpp:llama2", false},
		{"gcp:gemini", true},
		{"no-prefix", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			err := validatePrefix(tt.model)
			if tt.wantErr && err == nil {
				t.Errorf("validatePrefix(%q) expected error", tt.model)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validatePrefix(%q) unexpected error: %v", tt.model, err)
			}
		})
	}
}

func TestValidateModels_String(t *testing.T) {
	err := validateModels("anth:sonnet")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateModels_Array(t *testing.T) {
	arr := []interface{}{"anth:sonnet", "oai:gpt4o"}
	err := validateModels(arr)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateModels_ArrayInvalid(t *testing.T) {
	arr := []interface{}{"anth:sonnet", "gcp:gemini"}
	err := validateModels(arr)
	if err == nil {
		t.Error("expected error for invalid prefix in array")
	}
}

func TestValidateModels_ArrayNonString(t *testing.T) {
	arr := []interface{}{"anth:sonnet", 123}
	err := validateModels(arr)
	if err == nil {
		t.Error("expected error for non-string in array")
	}
}

func TestLoadSavePresets(t *testing.T) {
	cleanup := setupMsetTest(t)
	defer cleanup()

	// Load from empty state
	presets := loadPresets()
	if len(presets) != 0 {
		t.Errorf("expected empty presets, got %d", len(presets))
	}

	// Save
	presets["chat"] = "anth:sonnet"
	presets["code"] = []interface{}{"oai:gpt4o", "anth:sonnet"}
	savePresets(presets)

	// Reload
	loaded := loadPresets()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 presets, got %d", len(loaded))
	}
	if loaded["chat"] != "anth:sonnet" {
		t.Errorf("chat preset = %v, want anth:sonnet", loaded["chat"])
	}
}

func TestLoadPresets_InvalidJSON(t *testing.T) {
	cleanup := setupMsetTest(t)
	defer cleanup()

	os.WriteFile(filepath.Join(".clock", "models.json"), []byte("not json"), 0o644)
	presets := loadPresets()
	if len(presets) != 0 {
		t.Errorf("expected empty presets for invalid JSON, got %d", len(presets))
	}
}

func TestPresetSetGetDelete(t *testing.T) {
	cleanup := setupMsetTest(t)
	defer cleanup()

	// Set
	presets := loadPresets()
	presets["chat"] = "anth:sonnet"
	savePresets(presets)

	// Get
	presets = loadPresets()
	val, ok := presets["chat"]
	if !ok {
		t.Fatal("expected 'chat' key")
	}
	if val != "anth:sonnet" {
		t.Errorf("chat = %v, want anth:sonnet", val)
	}

	// Delete
	delete(presets, "chat")
	savePresets(presets)

	presets = loadPresets()
	if _, ok := presets["chat"]; ok {
		t.Error("expected 'chat' key to be deleted")
	}
}

func TestPresetListAll(t *testing.T) {
	cleanup := setupMsetTest(t)
	defer cleanup()

	presets := map[string]interface{}{
		"chat":    "anth:sonnet",
		"code":    "oai:gpt4o",
		"default": []interface{}{"anth:sonnet", "oai:gpt4o"},
	}
	savePresets(presets)

	loaded := loadPresets()
	if len(loaded) != 3 {
		t.Errorf("expected 3 presets, got %d", len(loaded))
	}
}

func TestPresetFallbackArray(t *testing.T) {
	cleanup := setupMsetTest(t)
	defer cleanup()

	presets := map[string]interface{}{
		"default": []interface{}{"anth:sonnet", "oai:gpt4o", "oll:llama3"},
	}
	savePresets(presets)

	loaded := loadPresets()
	val := loaded["default"]
	arr, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected array, got %T", val)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 elements in fallback array, got %d", len(arr))
	}
}
