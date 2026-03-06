package jsonutil

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// withStdin replaces os.Stdin with a pipe containing the given data, runs fn, then restores it.
func withStdin(t *testing.T, data string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte(data))
		w.Close()
	}()
	defer func() { os.Stdin = old }()
	fn()
}

// captureStdout replaces os.Stdout with a pipe, runs fn, and returns captured output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// ---------- ReadInput ----------

func TestReadInput_ValidJSON(t *testing.T) {
	type input struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var got input
	withStdin(t, `{"name":"alice","age":30}`, func() {
		if err := ReadInput(&got); err != nil {
			t.Fatal(err)
		}
	})
	if got.Name != "alice" || got.Age != 30 {
		t.Errorf("ReadInput mismatch: %+v", got)
	}
}

func TestReadInput_EmptyInput(t *testing.T) {
	var got map[string]interface{}
	withStdin(t, "", func() {
		err := ReadInput(&got)
		if err != nil {
			t.Errorf("ReadInput with empty input should return nil, got %v", err)
		}
	})
	if got != nil {
		t.Errorf("expected nil map for empty input, got %+v", got)
	}
}

func TestReadInput_InvalidJSON(t *testing.T) {
	var got map[string]interface{}
	withStdin(t, `{not valid json`, func() {
		err := ReadInput(&got)
		if err == nil {
			t.Error("ReadInput with invalid JSON should return error")
		}
	})
}

func TestReadInput_Array(t *testing.T) {
	var got []int
	withStdin(t, `[1,2,3]`, func() {
		if err := ReadInput(&got); err != nil {
			t.Fatal(err)
		}
	})
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("ReadInput array mismatch: %+v", got)
	}
}

func TestReadInput_NestedJSON(t *testing.T) {
	type nested struct {
		Inner struct {
			Value string `json:"value"`
		} `json:"inner"`
	}
	var got nested
	withStdin(t, `{"inner":{"value":"deep"}}`, func() {
		if err := ReadInput(&got); err != nil {
			t.Fatal(err)
		}
	})
	if got.Inner.Value != "deep" {
		t.Errorf("ReadInput nested mismatch: %+v", got)
	}
}

// ---------- WriteOutput ----------

func TestWriteOutput_ProducesIndentedJSON(t *testing.T) {
	data := map[string]interface{}{"key": "value", "num": float64(42)}
	out := captureStdout(t, func() {
		if err := WriteOutput(data); err != nil {
			t.Fatal(err)
		}
	})
	// Should be indented (contain newlines within the object)
	if !strings.Contains(out, "\n") {
		t.Errorf("WriteOutput should produce indented JSON, got %q", out)
	}
	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("WriteOutput produced invalid JSON: %v", err)
	}
	if parsed["key"] != "value" || parsed["num"] != float64(42) {
		t.Errorf("WriteOutput content mismatch: %+v", parsed)
	}
}

func TestWriteOutput_Struct(t *testing.T) {
	type s struct {
		Name string `json:"name"`
	}
	out := captureStdout(t, func() {
		if err := WriteOutput(s{Name: "test"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"name": "test"`) {
		t.Errorf("WriteOutput struct mismatch: %q", out)
	}
}

// ---------- WriteCompact ----------

func TestWriteCompact_ProducesCompactJSON(t *testing.T) {
	data := map[string]string{"a": "1", "b": "2"}
	out := captureStdout(t, func() {
		if err := WriteCompact(data); err != nil {
			t.Fatal(err)
		}
	})
	// Compact JSON should not have indentation spaces between key and value
	trimmed := strings.TrimSpace(out)
	if strings.Contains(trimmed, "  ") {
		t.Errorf("WriteCompact should not produce indented JSON, got %q", out)
	}
	// Should be valid JSON
	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("WriteCompact produced invalid JSON: %v", err)
	}
}

func TestWriteCompact_TrailingNewline(t *testing.T) {
	out := captureStdout(t, func() {
		if err := WriteCompact("hello"); err != nil {
			t.Fatal(err)
		}
	})
	// json.Encoder.Encode appends a newline
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("WriteCompact should end with newline, got %q", out)
	}
}

// ---------- WriteJSONL ----------

func TestWriteJSONL_SingleLine(t *testing.T) {
	out := captureStdout(t, func() {
		if err := WriteJSONL(map[string]int{"x": 1}); err != nil {
			t.Fatal(err)
		}
	})
	trimmed := strings.TrimSpace(out)
	// Should be a single line of valid JSON
	if strings.Count(trimmed, "\n") > 0 {
		t.Errorf("WriteJSONL should produce single line, got %q", out)
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		t.Errorf("WriteJSONL produced invalid JSON: %v", err)
	}
}

// ---------- ReadJSONL ----------

func TestReadJSONL_MultipleLines(t *testing.T) {
	input := `{"a":1}
{"a":2}
{"a":3}
`
	var results []json.RawMessage
	withStdin(t, input, func() {
		err := ReadJSONL(func(msg json.RawMessage) error {
			results = append(results, append(json.RawMessage{}, msg...))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(results) != 3 {
		t.Errorf("ReadJSONL expected 3 lines, got %d", len(results))
	}
	// Verify each line is valid JSON
	for i, r := range results {
		var m map[string]int
		if err := json.Unmarshal(r, &m); err != nil {
			t.Errorf("ReadJSONL line %d invalid JSON: %v", i, err)
		}
	}
}

func TestReadJSONL_EmptyLines(t *testing.T) {
	input := `{"a":1}

{"a":2}

`
	var count int
	withStdin(t, input, func() {
		err := ReadJSONL(func(msg json.RawMessage) error {
			count++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	// Empty lines should be skipped
	if count != 2 {
		t.Errorf("ReadJSONL should skip empty lines, expected 2, got %d", count)
	}
}

func TestReadJSONL_LargeLines(t *testing.T) {
	// Build a line larger than default bufio.Scanner buffer (64KB)
	bigValue := strings.Repeat("x", 100_000)
	input := `{"big":"` + bigValue + `"}` + "\n"
	var got json.RawMessage
	withStdin(t, input, func() {
		err := ReadJSONL(func(msg json.RawMessage) error {
			got = append(json.RawMessage{}, msg...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if got == nil {
		t.Fatal("ReadJSONL should handle large lines")
	}
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed["big"]) != 100_000 {
		t.Errorf("ReadJSONL large line value length mismatch: %d", len(parsed["big"]))
	}
}

func TestReadJSONL_EmptyInput(t *testing.T) {
	var count int
	withStdin(t, "", func() {
		err := ReadJSONL(func(msg json.RawMessage) error {
			count++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if count != 0 {
		t.Errorf("ReadJSONL empty input should yield 0 calls, got %d", count)
	}
}

func TestReadJSONL_CallbackError(t *testing.T) {
	input := `{"a":1}
{"a":2}
`
	expectedErr := io.ErrUnexpectedEOF
	withStdin(t, input, func() {
		err := ReadJSONL(func(msg json.RawMessage) error {
			return expectedErr
		})
		if err != expectedErr {
			t.Errorf("ReadJSONL should propagate callback error, got %v", err)
		}
	})
}

// ---------- MustMarshal ----------

func TestMustMarshal_ValidInput(t *testing.T) {
	type s struct {
		X int `json:"x"`
	}
	b := MustMarshal(s{X: 42})
	if string(b) != `{"x":42}` {
		t.Errorf("MustMarshal mismatch: %s", b)
	}
}

func TestMustMarshal_MapInput(t *testing.T) {
	b := MustMarshal(map[string]string{"key": "val"})
	var parsed map[string]string
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["key"] != "val" {
		t.Errorf("MustMarshal map mismatch: %+v", parsed)
	}
}

func TestMustMarshal_NilInput(t *testing.T) {
	b := MustMarshal(nil)
	if string(b) != "null" {
		t.Errorf("MustMarshal nil should produce 'null', got %s", b)
	}
}

func TestMustMarshal_PanicsOnChannel(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("MustMarshal should panic on channel type")
		}
	}()
	ch := make(chan int)
	MustMarshal(ch)
}

func TestMustMarshal_PanicsOnFunc(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("MustMarshal should panic on func type")
		}
	}()
	fn := func() {}
	MustMarshal(fn)
}
