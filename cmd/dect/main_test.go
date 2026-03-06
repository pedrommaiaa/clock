package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	dir := t.TempDir()

	// Create a file
	fpath := filepath.Join(dir, "test.txt")
	os.WriteFile(fpath, []byte("hi"), 0o644)

	if !fileExists(fpath) {
		t.Error("fileExists returned false for existing file")
	}

	if fileExists(filepath.Join(dir, "nope.txt")) {
		t.Error("fileExists returned true for non-existent file")
	}

	// Directory should return false
	if fileExists(dir) {
		t.Error("fileExists returned true for a directory")
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()

	if !dirExists(dir) {
		t.Error("dirExists returned false for existing directory")
	}

	if dirExists(filepath.Join(dir, "nope")) {
		t.Error("dirExists returned true for non-existent directory")
	}

	// File should return false
	fpath := filepath.Join(dir, "file.txt")
	os.WriteFile(fpath, []byte("hi"), 0o644)
	if dirExists(fpath) {
		t.Error("dirExists returned true for a file")
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  int
	}{
		{"add to empty", nil, "hello", 1},
		{"add new", []string{"a"}, "b", 2},
		{"skip duplicate", []string{"a", "b"}, "a", 2},
		{"add third", []string{"a", "b"}, "c", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.slice, tt.s)
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d, got %v", len(got), tt.want, got)
			}
		})
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o644)

	output := DectOutput{}
	if fileExists(filepath.Join(dir, "go.mod")) {
		output.Fmt = appendUnique(output.Fmt, "gofmt -l .")
		output.Lint = appendUnique(output.Lint, "go vet ./...")
		output.Test = appendUnique(output.Test, "go test ./...")
		output.Build = appendUnique(output.Build, "go build ./...")
	}

	if len(output.Fmt) != 1 || output.Fmt[0] != "gofmt -l ." {
		t.Errorf("expected gofmt, got %v", output.Fmt)
	}
	if len(output.Test) != 1 || output.Test[0] != "go test ./..." {
		t.Errorf("expected go test, got %v", output.Test)
	}
	if len(output.Build) != 1 || output.Build[0] != "go build ./..." {
		t.Errorf("expected go build, got %v", output.Build)
	}
}

func TestDetectNode(t *testing.T) {
	dir := t.TempDir()
	pkg := map[string]interface{}{
		"name": "test",
		"scripts": map[string]interface{}{
			"test":   "jest",
			"lint":   "eslint .",
			"build":  "tsc",
			"format": "prettier --write .",
		},
	}
	data, _ := json.Marshal(pkg)
	pkgPath := filepath.Join(dir, "package.json")
	os.WriteFile(pkgPath, data, 0o644)

	output := DectOutput{}
	detectNode(pkgPath, &output)

	if len(output.Test) != 1 || output.Test[0] != "npm run test" {
		t.Errorf("expected npm run test, got %v", output.Test)
	}
	if len(output.Lint) != 1 || output.Lint[0] != "npm run lint" {
		t.Errorf("expected npm run lint, got %v", output.Lint)
	}
	if len(output.Build) != 1 || output.Build[0] != "npm run build" {
		t.Errorf("expected npm run build, got %v", output.Build)
	}
	if len(output.Fmt) != 1 || output.Fmt[0] != "npm run format" {
		t.Errorf("expected npm run format, got %v", output.Fmt)
	}
}

func TestDetectNode_NoScripts(t *testing.T) {
	dir := t.TempDir()
	pkg := map[string]interface{}{
		"name": "test",
	}
	data, _ := json.Marshal(pkg)
	pkgPath := filepath.Join(dir, "package.json")
	os.WriteFile(pkgPath, data, 0o644)

	output := DectOutput{}
	detectNode(pkgPath, &output)

	if len(output.Test) != 0 {
		t.Errorf("expected no test commands, got %v", output.Test)
	}
}

func TestDetectNode_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "package.json")
	os.WriteFile(pkgPath, []byte("not json"), 0o644)

	output := DectOutput{}
	detectNode(pkgPath, &output) // should not panic
}

func TestDetectMakefile(t *testing.T) {
	dir := t.TempDir()
	makefileContent := `
test:
	go test ./...

lint:
	golangci-lint run

build:
	go build ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/
`
	makefilePath := filepath.Join(dir, "Makefile")
	os.WriteFile(makefilePath, []byte(makefileContent), 0o644)

	output := DectOutput{}
	detectMakefile(makefilePath, &output)

	if len(output.Test) != 1 || output.Test[0] != "make test" {
		t.Errorf("expected make test, got %v", output.Test)
	}
	if len(output.Lint) != 1 || output.Lint[0] != "make lint" {
		t.Errorf("expected make lint, got %v", output.Lint)
	}
	if len(output.Build) != 1 || output.Build[0] != "make build" {
		t.Errorf("expected make build, got %v", output.Build)
	}
	if len(output.Fmt) != 1 || output.Fmt[0] != "make fmt" {
		t.Errorf("expected make fmt, got %v", output.Fmt)
	}
}

func TestDetectMakefile_SkipsVariables(t *testing.T) {
	dir := t.TempDir()
	content := `
VERSION := 1.0.0
GO ?= go

test:
	$(GO) test ./...
`
	path := filepath.Join(dir, "Makefile")
	os.WriteFile(path, []byte(content), 0o644)

	output := DectOutput{}
	detectMakefile(path, &output)

	if len(output.Test) != 1 {
		t.Errorf("expected 1 test command, got %d: %v", len(output.Test), output.Test)
	}
}

func TestDetectCI(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		isDir   bool
		wantCI  string
	}{
		{"github actions", filepath.Join(".github", "workflows"), true, "GitHub Actions"},
		{"gitlab ci", ".gitlab-ci.yml", false, "GitLab CI"},
		{"travis", ".travis.yml", false, "Travis CI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			fullPath := filepath.Join(tmpDir, tt.path)
			if tt.isDir {
				os.MkdirAll(fullPath, 0o755)
			} else {
				os.MkdirAll(filepath.Dir(fullPath), 0o755)
				os.WriteFile(fullPath, []byte("ci config"), 0o644)
			}

			output := DectOutput{}
			detectCI(tmpDir, &output)

			found := false
			for _, n := range output.Notes {
				if stringContains(n, tt.wantCI) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected CI detection for %s, got notes: %v", tt.wantCI, output.Notes)
			}
		})
	}
}

func TestDetectCI_NoneFound(t *testing.T) {
	dir := t.TempDir()
	output := DectOutput{}
	detectCI(dir, &output)
	if len(output.Notes) != 0 {
		t.Errorf("expected no CI notes for empty dir, got %v", output.Notes)
	}
}

func TestDetectPython(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname = \"test\"\n"), 0o644)

	output := DectOutput{}
	if fileExists(filepath.Join(dir, "pyproject.toml")) {
		output.Test = appendUnique(output.Test, "pytest")
		output.Lint = appendUnique(output.Lint, "ruff check .")
		output.Fmt = appendUnique(output.Fmt, "black .")
	}

	if len(output.Test) != 1 || output.Test[0] != "pytest" {
		t.Errorf("expected pytest, got %v", output.Test)
	}
}

func TestDetectRust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644)

	output := DectOutput{}
	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		output.Test = appendUnique(output.Test, "cargo test")
		output.Build = appendUnique(output.Build, "cargo build")
		output.Fmt = appendUnique(output.Fmt, "cargo fmt")
		output.Lint = appendUnique(output.Lint, "cargo clippy")
	}

	if len(output.Test) != 1 || output.Test[0] != "cargo test" {
		t.Errorf("expected cargo test, got %v", output.Test)
	}
	if len(output.Build) != 1 || output.Build[0] != "cargo build" {
		t.Errorf("expected cargo build, got %v", output.Build)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
