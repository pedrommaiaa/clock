package main

import (
	"encoding/json"
	"testing"
)

func TestMatchValue_String(t *testing.T) {
	tests := []struct {
		val    interface{}
		target string
		want   bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"", "", true},
		{"abc", "ABC", false},
	}
	for _, tt := range tests {
		got := matchValue(tt.val, tt.target)
		if got != tt.want {
			t.Errorf("matchValue(%v, %q) = %v, want %v", tt.val, tt.target, got, tt.want)
		}
	}
}

func TestMatchValue_Float64(t *testing.T) {
	tests := []struct {
		val    interface{}
		target string
		want   bool
	}{
		{float64(42), "42", true},
		{float64(3.14), "3.14", true},
		{float64(0), "0", true},
		{float64(42), "43", false},
		{float64(42), "not-a-number", false},
		{float64(-1.5), "-1.5", true},
	}
	for _, tt := range tests {
		got := matchValue(tt.val, tt.target)
		if got != tt.want {
			t.Errorf("matchValue(%v, %q) = %v, want %v", tt.val, tt.target, got, tt.want)
		}
	}
}

func TestMatchValue_Bool(t *testing.T) {
	tests := []struct {
		val    interface{}
		target string
		want   bool
	}{
		{true, "true", true},
		{false, "false", true},
		{true, "false", false},
		{false, "true", false},
		{true, "1", true},
		{false, "0", true},
		{true, "not-bool", false},
	}
	for _, tt := range tests {
		got := matchValue(tt.val, tt.target)
		if got != tt.want {
			t.Errorf("matchValue(%v, %q) = %v, want %v", tt.val, tt.target, got, tt.want)
		}
	}
}

func TestMatchValue_Nil(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"null", true},
		{"", true},
		{"something", false},
	}
	for _, tt := range tests {
		got := matchValue(nil, tt.target)
		if got != tt.want {
			t.Errorf("matchValue(nil, %q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

func TestMatchValue_Complex(t *testing.T) {
	// For complex types, matchValue marshals to JSON and compares
	arr := []interface{}{"a", "b"}
	arrJSON, _ := json.Marshal(arr)

	if !matchValue(arr, string(arrJSON)) {
		t.Errorf("expected array to match its JSON serialization")
	}
	if matchValue(arr, "something else") {
		t.Errorf("expected array not to match arbitrary string")
	}

	obj := map[string]interface{}{"key": "val"}
	objJSON, _ := json.Marshal(obj)
	if !matchValue(obj, string(objJSON)) {
		t.Errorf("expected object to match its JSON serialization")
	}
}
