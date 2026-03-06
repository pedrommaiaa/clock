// Package jsonutil provides JSON I/O helpers for Clock tools.
package jsonutil

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ReadInput reads JSON from stdin into the given target.
func ReadInput(target interface{}) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

// WriteOutput writes a value as JSON to stdout.
func WriteOutput(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteCompact writes a value as compact JSON to stdout.
func WriteCompact(v interface{}) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}

// WriteJSONL writes a value as a single JSONL line to stdout.
func WriteJSONL(v interface{}) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}

// ReadJSONL reads JSONL from stdin, calling fn for each parsed object.
func ReadJSONL(fn func(json.RawMessage) error) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(json.RawMessage(line)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// MustMarshal marshals to JSON or panics.
func MustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Fatal prints a JSON error and exits.
func Fatal(msg string) {
	fmt.Fprintf(os.Stderr, `{"error": %q}`+"\n", msg)
	os.Exit(1)
}
