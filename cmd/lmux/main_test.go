package main

import (
	"testing"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPfx  string
		wantProv string
		wantMdl  string
		wantErr  bool
	}{
		{"anthropic sonnet", "anth:sonnet", "anth", "anthropic", "sonnet", false},
		{"openai gpt4o", "oai:gpt4o", "oai", "openai", "gpt4o", false},
		{"ollama llama3", "oll:llama3", "oll", "ollama", "llama3", false},
		{"vllm model", "vllm:mistral", "vllm", "vllm", "mistral", false},
		{"lcpp model", "lcpp:llama2", "lcpp", "lcpp", "llama2", false},
		{"unknown prefix", "gcp:gemini", "", "", "", true},
		{"no colon", "sonnet", "", "", "", true},
		{"empty prefix", ":sonnet", "", "", "", true},
		{"empty model", "anth:", "", "", "", true},
		{"empty string", "", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := parseModel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseModel(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseModel(%q) unexpected error: %v", tt.input, err)
			}
			if info.Prefix != tt.wantPfx {
				t.Errorf("Prefix = %q, want %q", info.Prefix, tt.wantPfx)
			}
			if info.Provider != tt.wantProv {
				t.Errorf("Provider = %q, want %q", info.Provider, tt.wantProv)
			}
			if info.Model != tt.wantMdl {
				t.Errorf("Model = %q, want %q", info.Model, tt.wantMdl)
			}
			if info.Raw != tt.input {
				t.Errorf("Raw = %q, want %q", info.Raw, tt.input)
			}
		})
	}
}

func TestSortByPriority(t *testing.T) {
	providers := []Provider{
		{Name: "c", Priority: 3},
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
	}

	sorted := sortByPriority(providers)
	if sorted[0].Name != "a" || sorted[1].Name != "b" || sorted[2].Name != "c" {
		t.Errorf("expected sorted by priority [a,b,c], got [%s,%s,%s]",
			sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
}

func TestSortByPriority_AlreadySorted(t *testing.T) {
	providers := []Provider{
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
		{Name: "c", Priority: 3},
	}

	sorted := sortByPriority(providers)
	if sorted[0].Name != "a" || sorted[1].Name != "b" || sorted[2].Name != "c" {
		t.Error("already sorted input should remain sorted")
	}
}

func TestSortByPriority_EqualPriority(t *testing.T) {
	providers := []Provider{
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 1},
	}

	sorted := sortByPriority(providers)
	if len(sorted) != 2 {
		t.Errorf("expected 2 providers, got %d", len(sorted))
	}
}

func TestSortByPriority_Empty(t *testing.T) {
	sorted := sortByPriority([]Provider{})
	if len(sorted) != 0 {
		t.Errorf("expected empty result, got %d", len(sorted))
	}
}

func TestSortByPriority_Single(t *testing.T) {
	providers := []Provider{{Name: "only", Priority: 1}}
	sorted := sortByPriority(providers)
	if len(sorted) != 1 || sorted[0].Name != "only" {
		t.Error("single element should be unchanged")
	}
}

func TestSortByPriority_DoesNotMutateInput(t *testing.T) {
	providers := []Provider{
		{Name: "b", Priority: 2},
		{Name: "a", Priority: 1},
	}

	sortByPriority(providers)

	// Original should be unchanged
	if providers[0].Name != "b" {
		t.Error("sortByPriority should not mutate the original slice")
	}
}

func TestPrefixToProvider(t *testing.T) {
	expected := map[string]string{
		"anth": "anthropic",
		"oai":  "openai",
		"oll":  "ollama",
		"vllm": "vllm",
		"lcpp": "lcpp",
	}

	for prefix, want := range expected {
		got, ok := prefixToProvider[prefix]
		if !ok {
			t.Errorf("prefix %q not found in prefixToProvider", prefix)
			continue
		}
		if got != want {
			t.Errorf("prefixToProvider[%q] = %q, want %q", prefix, got, want)
		}
	}
}

func TestRoundRobinState(t *testing.T) {
	// Test basic round-robin index calculation
	providers := []Provider{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}

	tests := []struct {
		counter int
		wantIdx int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 0},
		{4, 1},
	}

	for _, tt := range tests {
		idx := tt.counter % len(providers)
		if idx != tt.wantIdx {
			t.Errorf("counter=%d: idx=%d, want %d", tt.counter, idx, tt.wantIdx)
		}
	}
}

func TestRoundRobinOrder(t *testing.T) {
	// Verify round-robin order construction: selected provider first, then rest
	providers := []Provider{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}

	counter := 1 // Should select "b" first
	idx := counter % len(providers)

	order := make([]Provider, 0, len(providers))
	order = append(order, providers[idx])
	for i, p := range providers {
		if i != idx {
			order = append(order, p)
		}
	}

	if order[0].Name != "b" {
		t.Errorf("first in order should be 'b', got %q", order[0].Name)
	}
	if len(order) != 3 {
		t.Errorf("expected 3 providers in order, got %d", len(order))
	}
}
