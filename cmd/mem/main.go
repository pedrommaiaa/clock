// Command mem is a durable knowledge store using file-based JSON storage.
// Subcommands: put, get, list, search, delete, stats.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const storeDir = ".clock/mem"

// Valid kinds for storage.
var validKinds = map[string]bool{
	"fix":         true,
	"fingerprint": true,
	"command":     true,
	"playbook":    true,
	"dossier":     true,
}

// PutInput is the input for the put subcommand.
type PutInput struct {
	Key  string      `json:"key"`
	Kind string      `json:"kind"`
	Data interface{} `json:"data"`
}

// MemEntry is a stored entry.
type MemEntry struct {
	Key       string      `json:"key"`
	Kind      string      `json:"kind"`
	Data      interface{} `json:"data"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}

// StatsOutput is the output of the stats subcommand.
type StatsOutput struct {
	Fix         int `json:"fix"`
	Fingerprint int `json:"fingerprint"`
	Command     int `json:"command"`
	Playbook    int `json:"playbook"`
	Dossier     int `json:"dossier"`
}

// ErrorOutput is a JSON error response.
type ErrorOutput struct {
	Error string `json:"error"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: mem <put|get|list|search|delete|stats> [args...]")
	}

	ensureDirs()

	cmd := os.Args[1]
	switch cmd {
	case "put":
		doPut()
	case "get":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: mem get <key>")
		}
		doGet(os.Args[2])
	case "list":
		kindFilter := ""
		if len(os.Args) >= 3 {
			kindFilter = os.Args[2]
		}
		doList(kindFilter)
	case "search":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: mem search <query>")
		}
		doSearch(strings.Join(os.Args[2:], " "))
	case "delete":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: mem delete <key>")
		}
		doDelete(os.Args[2])
	case "stats":
		doStats()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", cmd))
	}
}

func ensureDirs() {
	for kind := range validKinds {
		dir := filepath.Join(storeDir, kind)
		if err := os.MkdirAll(dir, 0755); err != nil {
			jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", dir, err))
		}
	}
}

func doPut() {
	var input PutInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Key == "" {
		jsonutil.Fatal("key is required")
	}
	if input.Kind == "" {
		jsonutil.Fatal("kind is required")
	}
	if !validKinds[input.Kind] {
		jsonutil.Fatal(fmt.Sprintf("invalid kind: %s (valid: fix, fingerprint, command, playbook, dossier)", input.Kind))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(storeDir, input.Kind, input.Key+".json")

	// Check if entry exists for created_at preservation
	createdAt := now
	if data, err := os.ReadFile(path); err == nil {
		var existing MemEntry
		if err := json.Unmarshal(data, &existing); err == nil && existing.CreatedAt != "" {
			createdAt = existing.CreatedAt
		}
	}

	entry := MemEntry{
		Key:       input.Key,
		Kind:      input.Kind,
		Data:      input.Data,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal: %v", err))
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write %s: %v", path, err))
	}

	if err := jsonutil.WriteOutput(entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doGet(key string) {
	// Search across all kind dirs
	for kind := range validKinds {
		path := filepath.Join(storeDir, kind, key+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry MemEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if err := jsonutil.WriteOutput(entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	if err := jsonutil.WriteOutput(ErrorOutput{Error: fmt.Sprintf("key not found: %s", key)}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doList(kindFilter string) {
	kinds := make([]string, 0)
	if kindFilter != "" {
		if !validKinds[kindFilter] {
			jsonutil.Fatal(fmt.Sprintf("invalid kind: %s", kindFilter))
		}
		kinds = append(kinds, kindFilter)
	} else {
		for kind := range validKinds {
			kinds = append(kinds, kind)
		}
	}

	for _, kind := range kinds {
		dir := filepath.Join(storeDir, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var entry MemEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}
			jsonutil.WriteJSONL(entry)
		}
	}
}

func doSearch(query string) {
	queryLower := strings.ToLower(query)

	for kind := range validKinds {
		dir := filepath.Join(storeDir, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			// Substring search across the entire JSON content
			if !strings.Contains(strings.ToLower(string(data)), queryLower) {
				continue
			}

			var entry MemEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}
			jsonutil.WriteJSONL(entry)
		}
	}
}

func doDelete(key string) {
	for kind := range validKinds {
		path := filepath.Join(storeDir, kind, key+".json")
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				jsonutil.Fatal(fmt.Sprintf("remove %s: %v", path, err))
			}
			if err := jsonutil.WriteOutput(map[string]interface{}{
				"ok":  true,
				"key": key,
			}); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
			return
		}
	}

	if err := jsonutil.WriteOutput(ErrorOutput{Error: fmt.Sprintf("key not found: %s", key)}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doStats() {
	stats := StatsOutput{}

	countKind := func(kind string) int {
		dir := filepath.Join(storeDir, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0
		}
		count := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				count++
			}
		}
		return count
	}

	stats.Fix = countKind("fix")
	stats.Fingerprint = countKind("fingerprint")
	stats.Command = countKind("command")
	stats.Playbook = countKind("playbook")
	stats.Dossier = countKind("dossier")

	if err := jsonutil.WriteOutput(stats); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
