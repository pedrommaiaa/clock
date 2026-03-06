// Command trce is an append-only trace log with replay metadata.
// It reads a TraceEvent JSON from stdin, appends it as JSONL to the trace file,
// and echoes it to stdout. Use -f <path> to override the default trace file.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

func main() {
	traceFile := flag.String("f", "", "override trace file path (default: .clock/trce.jsonl)")
	flag.Parse()

	var ev common.TraceEvent
	if err := jsonutil.ReadInput(&ev); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	// Add timestamp if not set
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}

	// Determine trace file path
	path := *traceFile
	if path == "" {
		path = filepath.Join(".clock", "trce.jsonl")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", dir, err))
	}

	// Marshal to JSON line
	line, err := json.Marshal(ev)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal: %v", err))
	}

	// Append to trace file
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("open %s: %v", path, err))
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		jsonutil.Fatal(fmt.Sprintf("write: %v", err))
	}
	if err := f.Close(); err != nil {
		jsonutil.Fatal(fmt.Sprintf("close: %v", err))
	}

	// Echo to stdout
	if err := jsonutil.WriteOutput(ev); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
