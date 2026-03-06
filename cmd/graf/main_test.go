package main

import (
	"testing"
)

func TestLangFromExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".js", "js"},
		{".jsx", "js"},
		{".mjs", "js"},
		{".ts", "ts"},
		{".tsx", "ts"},
		{".py", "python"},
		{".rs", "rust"},
		{".txt", ""},
		{".md", ""},
		{".java", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := langFromExt(tt.ext)
		if got != tt.want {
			t.Errorf("langFromExt(%q) = %q, want %q", tt.ext, got, tt.want)
		}
	}
}

func TestExtractImports_Go(t *testing.T) {
	// Note: the parser takes the first field from each import line.
	// For aliased imports like `myalias "path"`, it extracts `myalias`.
	content := `package main

import (
	"fmt"
	"os"
)

import "strings"
`
	imports := extractImports(content, "go")

	want := map[string]bool{
		"fmt":     true,
		"os":      true,
		"strings": true,
	}

	for _, imp := range imports {
		if !want[imp] {
			t.Errorf("unexpected import: %q", imp)
		}
		delete(want, imp)
	}
	for missing := range want {
		t.Errorf("missing import: %q", missing)
	}
}

func TestExtractImports_Go_Aliased(t *testing.T) {
	// Aliased imports: the parser takes the alias name as the first field
	content := `package main

import (
	myalias "github.com/foo/bar"
)
`
	imports := extractImports(content, "go")

	// The parser extracts the alias (first field) rather than the path
	if len(imports) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imports), imports)
	}
	if imports[0] != "myalias" {
		t.Errorf("expected alias 'myalias', got %q", imports[0])
	}
}

func TestExtractImports_Go_SingleImport(t *testing.T) {
	content := `package main
import "net/http"
`
	imports := extractImports(content, "go")
	if len(imports) != 1 || imports[0] != "net/http" {
		t.Errorf("expected [net/http], got %v", imports)
	}
}

func TestExtractImports_JS(t *testing.T) {
	content := `import React from 'react';
import { useState } from 'react';
import './styles.css';
const fs = require('fs');
const path = require("path");
`
	imports := extractImports(content, "js")

	want := map[string]bool{
		"react":        true,
		"./styles.css": true,
		"fs":           true,
		"path":         true,
	}

	for _, imp := range imports {
		if !want[imp] {
			t.Errorf("unexpected JS import: %q", imp)
		}
		delete(want, imp)
	}
	for missing := range want {
		t.Errorf("missing JS import: %q", missing)
	}
}

func TestExtractImports_Python(t *testing.T) {
	content := `import os
import sys
from pathlib import Path
from collections.abc import Mapping
`
	imports := extractImports(content, "python")

	want := map[string]bool{
		"os":              true,
		"sys":             true,
		"pathlib":         true,
		"collections.abc": true,
	}

	for _, imp := range imports {
		if !want[imp] {
			t.Errorf("unexpected Python import: %q", imp)
		}
		delete(want, imp)
	}
	for missing := range want {
		t.Errorf("missing Python import: %q", missing)
	}
}

func TestExtractImports_Rust(t *testing.T) {
	content := `use std::io;
use crate::config;
use serde::{Deserialize, Serialize};
`
	imports := extractImports(content, "rust")

	want := map[string]bool{
		"std":   true,
		"crate": true,
		"serde": true,
	}

	for _, imp := range imports {
		if !want[imp] {
			t.Errorf("unexpected Rust import: %q", imp)
		}
		delete(want, imp)
	}
	for missing := range want {
		t.Errorf("missing Rust import: %q", missing)
	}
}

func TestExtractImports_Empty(t *testing.T) {
	imports := extractImports("", "go")
	if len(imports) != 0 {
		t.Errorf("expected 0 imports for empty content, got %v", imports)
	}
}

func TestExtractImports_UnknownLang(t *testing.T) {
	imports := extractImports("import foo", "java")
	if len(imports) != 0 {
		t.Errorf("expected 0 imports for unknown lang, got %v", imports)
	}
}

func TestExtractImports_Dedup(t *testing.T) {
	content := `import "fmt"
import "fmt"
`
	imports := extractImports(content, "go")
	if len(imports) != 1 {
		t.Errorf("expected deduplication, got %d imports: %v", len(imports), imports)
	}
}

func TestExtractSymbols_Go(t *testing.T) {
	content := `package main

func main() {}

func helper(x int) string {
	return ""
}

type Config struct {
	Name string
}

func (c *Config) Method() {}

type Handler interface{}
`
	symbols := extractSymbols(content, "go")

	nameSet := map[string]bool{}
	for _, s := range symbols {
		nameSet[s.name] = true
	}

	for _, want := range []string{"main", "helper", "Config", "Method", "Handler"} {
		if !nameSet[want] {
			t.Errorf("missing symbol: %q", want)
		}
	}
}

func TestExtractSymbols_JS(t *testing.T) {
	content := `function greet() {}
export function hello() {}
export async function fetchData() {}
class MyClass {}
export class Service {}
`
	symbols := extractSymbols(content, "js")

	nameSet := map[string]bool{}
	for _, s := range symbols {
		nameSet[s.name] = true
	}

	for _, want := range []string{"greet", "hello", "fetchData", "MyClass", "Service"} {
		if !nameSet[want] {
			t.Errorf("missing JS symbol: %q", want)
		}
	}
}

func TestExtractSymbols_Python(t *testing.T) {
	// Note: pyFunc regex uses ^def, so indented methods won't match
	content := `def foo():
    pass

class Bar:
    def method(self):
        pass

def baz(x, y):
    return x + y
`
	symbols := extractSymbols(content, "python")

	nameSet := map[string]bool{}
	for _, s := range symbols {
		nameSet[s.name] = true
	}

	// Only top-level defs and classes are detected (regex uses ^)
	for _, want := range []string{"foo", "Bar", "baz"} {
		if !nameSet[want] {
			t.Errorf("missing Python symbol: %q", want)
		}
	}
	// Indented methods are NOT detected by the current regex
	if nameSet["method"] {
		t.Error("indented method should not be detected by ^def regex")
	}
}

func TestExtractSymbols_Rust(t *testing.T) {
	content := `pub fn main() {}

fn helper() {}

pub struct Config {
    name: String,
}

struct Private {}
`
	symbols := extractSymbols(content, "rust")

	nameSet := map[string]bool{}
	for _, s := range symbols {
		nameSet[s.name] = true
	}

	for _, want := range []string{"main", "helper", "Config", "Private"} {
		if !nameSet[want] {
			t.Errorf("missing Rust symbol: %q", want)
		}
	}
}

func TestExtractSymbols_Empty(t *testing.T) {
	symbols := extractSymbols("", "go")
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for empty content, got %d", len(symbols))
	}
}

func TestExtractSymbols_LineNumbers(t *testing.T) {
	content := `package main

func first() {}

func second() {}
`
	symbols := extractSymbols(content, "go")

	for _, s := range symbols {
		if s.name == "first" && s.line != 3 {
			t.Errorf("first function at line %d, want 3", s.line)
		}
		if s.name == "second" && s.line != 5 {
			t.Errorf("second function at line %d, want 5", s.line)
		}
	}
}

func TestExtractSymbols_Kind(t *testing.T) {
	content := `package main

func myFunc() {}

type MyType struct{}
`
	symbols := extractSymbols(content, "go")

	for _, s := range symbols {
		if s.name == "myFunc" && s.kind != "function" {
			t.Errorf("myFunc kind = %q, want function", s.kind)
		}
		if s.name == "MyType" && s.kind != "type" {
			t.Errorf("MyType kind = %q, want type", s.kind)
		}
	}
}
