package main

import (
	"strings"
	"testing"
)

func TestMakeID(t *testing.T) {
	// Same inputs should produce same ID
	id1 := makeID("file", "main.go")
	id2 := makeID("file", "main.go")
	if id1 != id2 {
		t.Errorf("makeID determinism: %q != %q", id1, id2)
	}

	// Different inputs should produce different IDs
	id3 := makeID("file", "other.go")
	if id1 == id3 {
		t.Error("makeID should produce different IDs for different inputs")
	}

	// ID should be hex string
	if len(id1) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("makeID length = %d, want 16", len(id1))
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		val   string
		want  int
	}{
		{"add new", []string{"a", "b"}, "c", 3},
		{"duplicate", []string{"a", "b"}, "a", 2},
		{"empty slice", nil, "x", 1},
		{"add to single", []string{"a"}, "b", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.slice, tt.val)
			if len(got) != tt.want {
				t.Errorf("appendUnique len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"zero length", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestDiffFilePattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"--- a/cmd/main.go", "cmd/main.go"},
		{"+++ b/pkg/auth/login.go", "pkg/auth/login.go"},
		{"--- a/README.md", "README.md"},
		{"+++ b/go.mod", "go.mod"},
		{"not a diff line", ""},
	}
	for _, tt := range tests {
		m := diffFilePattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("diffFilePattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestDiffHunkPattern(t *testing.T) {
	tests := []struct {
		line    string
		wantCtx string
	}{
		{"@@ -10,5 +10,7 @@ func Login(user string)", "func Login(user string)"},
		{"@@ -1 +1 @@ package main", "package main"},
		{"@@ -10,5 +10,7 @@", ""},
		{"not a hunk", ""},
	}
	for _, tt := range tests {
		m := diffHunkPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = strings.TrimSpace(m[1])
		}
		if got != tt.wantCtx {
			t.Errorf("diffHunkPattern(%q) ctx = %q, want %q", tt.line, got, tt.wantCtx)
		}
	}
}

func TestFuncGoPattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"+func Login(user string) error {", "Login"},
		{"-func (s *Server) HandleRequest() {", "HandleRequest"},
		{"+func main() {", "main"},
		{" func NotChanged() {", ""}, // no + or - prefix
	}
	for _, tt := range tests {
		m := funcGoPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("funcGoPattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFuncPyPattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"+def login(user):", "login"},
		{"-def handle_request(self):", "handle_request"},
		{" def unchanged():", ""}, // no + or -
	}
	for _, tt := range tests {
		m := funcPyPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("funcPyPattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestLogErrorPattern(t *testing.T) {
	tests := []struct {
		line  string
		match bool
	}{
		{"ERROR: connection refused to database", true},
		{"FATAL: out of memory", true},
		{"panic: runtime error: index out of range", true},
		{"Exception: invalid argument", true},
		{"INFO: server started", false},
		{"DEBUG: processing request", false},
	}
	for _, tt := range tests {
		m := logErrorPattern.FindStringSubmatch(tt.line)
		got := m != nil
		if got != tt.match {
			t.Errorf("logErrorPattern(%q) match = %v, want %v", tt.line, got, tt.match)
		}
	}
}

func TestLogTestPattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"PASS TestLogin", "TestLogin"},
		{"FAIL TestAuth", "TestAuth"},
		{"SKIP TestHelper", "TestHelper"},
		{"--- TestSkipped", "TestSkipped"},
		{"regular log line", ""},
		{"--- PASS: TestLogin (0.01s)", ""}, // colon blocks \s+ match
	}
	for _, tt := range tests {
		m := logTestPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("logTestPattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestImportGoPattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{`import "fmt"`, "fmt"},
		{`import "net/http"`, "net/http"},
		{`var x = 1`, ""},
	}
	for _, tt := range tests {
		m := importGoPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("importGoPattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestProcessDiffExtraction(t *testing.T) {
	diff := `--- a/cmd/auth/login.go
+++ b/cmd/auth/login.go
@@ -10,5 +10,7 @@ func Login(user string)
+func ValidateToken(token string) error {
+	return nil
+}
--- a/pkg/db/connect.go
+++ b/pkg/db/connect.go
@@ -1,3 +1,5 @@
-func OldConnect() {
+func NewConnect() {`

	artifact := Artifact{
		Type:    "diff",
		Content: diff,
		Source:  "test",
	}

	output := &LinkOutput{Facts: []string{}}
	addedNodes := make(map[string]bool)
	addedEdges := make(map[string]bool)

	processDiff(artifact, "trace-1", output, addedNodes, addedEdges)

	// Should extract file and function facts
	if len(output.Facts) == 0 {
		t.Error("processDiff produced no facts")
	}

	// Check for file facts
	hasFileFact := false
	for _, fact := range output.Facts {
		if strings.Contains(fact, "file modified") {
			hasFileFact = true
			break
		}
	}
	if !hasFileFact {
		t.Error("expected at least one 'file modified' fact")
	}

	// Check for function facts
	hasFuncFact := false
	for _, fact := range output.Facts {
		if strings.Contains(fact, "function modified") {
			hasFuncFact = true
			break
		}
	}
	if !hasFuncFact {
		t.Error("expected at least one 'function modified' fact")
	}
}

func TestFindErrorNodes(t *testing.T) {
	content := "ERROR: connection refused\nFATAL: disk full"
	addedNodes := make(map[string]bool)

	// First add the error nodes
	for _, m := range logErrorPattern.FindAllStringSubmatch(content, -1) {
		errMsg := strings.TrimSpace(m[1])
		nodeID := makeID("fact", "error", errMsg)
		addedNodes[nodeID] = true
	}

	ids := findErrorNodes(content, addedNodes)
	if len(ids) == 0 {
		t.Error("findErrorNodes returned empty")
	}
}

func TestModuleRefPattern(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"package main", "main"},
		{"module github.com/user/repo", "github.com/user/repo"},
		{"var x = 1", ""},
	}
	for _, tt := range tests {
		m := moduleRefPattern.FindStringSubmatch(tt.line)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("moduleRefPattern(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
