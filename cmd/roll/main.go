// Command roll manages tool version rollback using an append-only history
// log of registry changes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// HistoryEntry is a single entry in the registry history log.
type HistoryEntry struct {
	Timestamp int64                 `json:"timestamp"`
	Action    string                `json:"action"` // snapshot, install, uninstall, pin, rollback
	Tool      string                `json:"tool,omitempty"`
	Version   string                `json:"version,omitempty"`
	Registry  []common.ToolManifest `json:"registry"`
}

// RollbackResult is the output of the roll back command.
type RollbackResult struct {
	Tool     string `json:"tool"`
	From     string `json:"from"`
	To       string `json:"to"`
	Restored bool   `json:"restored"`
}

// RollbackAllResult is the output of the roll back-all command.
type RollbackAllResult struct {
	Restored bool   `json:"restored"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// SnapshotResult is the output of the roll snapshot command.
type SnapshotResult struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	Tools     int    `json:"tools"`
}

func registryPath() string {
	return filepath.Join(".clock", "registry.json")
}

func historyPath() string {
	return filepath.Join(".clock", "registry_history.jsonl")
}

// readRegistry reads the current registry.
func readRegistry() ([]common.ToolManifest, error) {
	data, err := os.ReadFile(registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []common.ToolManifest{}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	if len(data) == 0 {
		return []common.ToolManifest{}, nil
	}
	var registry []common.ToolManifest
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return registry, nil
}

// writeRegistry writes the registry atomically.
func writeRegistry(registry []common.ToolManifest) error {
	dir := filepath.Dir(registryPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	tmpFile := registryPath() + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmpFile, registryPath()); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// readHistory reads all history entries.
func readHistory() ([]HistoryEntry, error) {
	data, err := os.ReadFile(historyPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryEntry{}, nil
		}
		return nil, fmt.Errorf("read history: %w", err)
	}
	if len(data) == 0 {
		return []HistoryEntry{}, nil
	}

	var entries []HistoryEntry
	dec := json.NewDecoder(jsonReader(data))
	for dec.More() {
		var entry HistoryEntry
		if err := dec.Decode(&entry); err != nil {
			break
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// jsonReader wraps byte data for JSONL decoding.
type byteReader struct {
	data []byte
	pos  int
}

func jsonReader(data []byte) *byteReader {
	return &byteReader{data: data}
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// appendHistory appends a history entry.
func appendHistory(entry HistoryEntry) error {
	dir := filepath.Dir(historyPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(historyPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}

// findTool returns the index of a tool by name, or -1.
func findTool(registry []common.ToolManifest, name string) int {
	for i, m := range registry {
		if m.Name == name {
			return i
		}
	}
	return -1
}

func doSnapshot() {
	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	ts := time.Now().Unix()
	entry := HistoryEntry{
		Timestamp: ts,
		Action:    "snapshot",
		Registry:  registry,
	}

	if err := appendHistory(entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("append history: %v", err))
	}

	result := SnapshotResult{
		Status:    "snapshot_saved",
		Timestamp: ts,
		Tools:     len(registry),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doRollback(name string) {
	if name == "" {
		jsonutil.Fatal("tool name is required")
	}

	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	history, err := readHistory()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read history: %v", err))
	}

	if len(history) == 0 {
		jsonutil.Fatal("no history available for rollback")
	}

	// Find current version
	currentIdx := findTool(registry, name)
	currentVersion := ""
	if currentIdx >= 0 {
		currentVersion = registry[currentIdx].Version
	}

	// Search history backwards for a previous version of this tool
	var previousManifest *common.ToolManifest
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		idx := findTool(entry.Registry, name)
		if idx >= 0 {
			candidate := entry.Registry[idx]
			if candidate.Version != currentVersion {
				previousManifest = &candidate
				break
			}
		}
	}

	if previousManifest == nil {
		jsonutil.Fatal(fmt.Sprintf("no previous version found for %q", name))
	}

	// Restore the previous version
	if currentIdx >= 0 {
		registry[currentIdx] = *previousManifest
	} else {
		registry = append(registry, *previousManifest)
	}

	if err := writeRegistry(registry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write registry: %v", err))
	}

	// Record rollback in history
	entry := HistoryEntry{
		Timestamp: time.Now().Unix(),
		Action:    "rollback",
		Tool:      name,
		Version:   previousManifest.Version,
		Registry:  registry,
	}
	appendHistory(entry)

	result := RollbackResult{
		Tool:     name,
		From:     currentVersion,
		To:       previousManifest.Version,
		Restored: true,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doRollbackAll() {
	history, err := readHistory()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read history: %v", err))
	}

	if len(history) < 2 {
		jsonutil.Fatal("not enough history for rollback-all (need at least 2 snapshots)")
	}

	// Find the second-to-last entry (the previous state)
	current := history[len(history)-1]
	previous := history[len(history)-2]

	if err := writeRegistry(previous.Registry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write registry: %v", err))
	}

	// Record rollback in history
	entry := HistoryEntry{
		Timestamp: time.Now().Unix(),
		Action:    "rollback-all",
		Registry:  previous.Registry,
	}
	appendHistory(entry)

	result := RollbackAllResult{
		Restored: true,
		From:     fmt.Sprintf("snapshot@%d", current.Timestamp),
		To:       fmt.Sprintf("snapshot@%d", previous.Timestamp),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// ToolHistory is a version history entry for display.
type ToolHistory struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Action    string `json:"action"`
	Timestamp int64  `json:"timestamp"`
}

func doHistory(name string) {
	history, err := readHistory()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read history: %v", err))
	}

	// Track version changes per tool
	seen := make(map[string]string) // tool -> last known version

	for _, entry := range history {
		for _, m := range entry.Registry {
			if name != "" && m.Name != name {
				continue
			}
			prevVersion, exists := seen[m.Name]
			if !exists || prevVersion != m.Version {
				th := ToolHistory{
					Tool:      m.Name,
					Version:   m.Version,
					Action:    entry.Action,
					Timestamp: entry.Timestamp,
				}
				if err := jsonutil.WriteJSONL(th); err != nil {
					jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
				}
				seen[m.Name] = m.Version
			}
		}
	}
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		jsonutil.Fatal("usage: roll <back|back-all|history|snapshot> [args...]")
	}

	switch args[0] {
	case "back":
		if len(args) < 2 {
			jsonutil.Fatal("usage: roll back <name>")
		}
		doRollback(args[1])
	case "back-all":
		doRollbackAll()
	case "history":
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		doHistory(name)
	case "snapshot":
		doSnapshot()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", args[0]))
	}
}
