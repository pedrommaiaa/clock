// Command note provides long-term human-readable notes storage.
// Notes are stored as Markdown files in .clock/notes/ with a JSONL index.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const (
	notesDir  = ".clock/notes"
	indexFile = ".clock/notes/index.jsonl"
)

// NoteInput is the input for add subcommand.
type NoteInput struct {
	Topic   string   `json:"topic"`
	Content string   `json:"content"`
	Refs    []string `json:"refs,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// IndexEntry is a single entry in the JSONL index.
type IndexEntry struct {
	ID        string   `json:"id"`
	Topic     string   `json:"topic"`
	Tags      []string `json:"tags,omitempty"`
	Refs      []string `json:"refs,omitempty"`
	File      string   `json:"file"`
	CreatedAt string   `json:"created_at"`
	Summary   string   `json:"summary,omitempty"`
}

// NoteContent is the output of get subcommand.
type NoteContent struct {
	ID        string   `json:"id"`
	Topic     string   `json:"topic"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags,omitempty"`
	Refs      []string `json:"refs,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// AddResult is the output of add subcommand.
type AddResult struct {
	OK   bool   `json:"ok"`
	ID   string `json:"id"`
	File string `json:"file"`
}

// RollupResult is the output of rollup subcommand.
type RollupResult struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	File  string `json:"file"`
	Count int    `json:"count"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: note <subcommand> [args]\nsubcommands: add, list, search, get, rollup, recent")
	}

	sub := os.Args[1]
	switch sub {
	case "add":
		cmdAdd()
	case "list":
		cmdList()
	case "search":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: note search <query>")
		}
		cmdSearch(os.Args[2])
	case "get":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: note get <id>")
		}
		cmdGet(os.Args[2])
	case "rollup":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: note rollup <topic>")
		}
		cmdRollup(os.Args[2])
	case "recent":
		n := 10
		if len(os.Args) >= 3 {
			var err error
			n, err = strconv.Atoi(os.Args[2])
			if err != nil || n <= 0 {
				jsonutil.Fatal("usage: note recent [N] — N must be a positive integer")
			}
		}
		cmdRecent(n)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", sub))
	}
}

func ensureDir() {
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", notesDir, err))
	}
}

func sanitizeTopic(topic string) string {
	// Replace non-alphanumeric chars with dashes for file-safe names
	var b strings.Builder
	for _, r := range strings.ToLower(topic) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' || r == '-' || r == '/' || r == '.' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Collapse multiple dashes
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func loadIndex() ([]IndexEntry, error) {
	f, err := os.Open(indexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []IndexEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry IndexEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func appendIndex(entry IndexEntry) error {
	ensureDir()
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal index entry: %w", err)
	}
	f, err := os.OpenFile(indexFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	return nil
}

func cmdAdd() {
	var input NoteInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}
	if input.Topic == "" {
		jsonutil.Fatal("topic is required")
	}
	if input.Content == "" {
		jsonutil.Fatal("content is required")
	}

	ensureDir()

	now := time.Now()
	ts := now.Format("20060102-150405")
	safeTopic := sanitizeTopic(input.Topic)
	id := fmt.Sprintf("%s-%s", safeTopic, ts)
	filename := fmt.Sprintf("%s.md", id)
	mdPath := filepath.Join(notesDir, filename)

	// Build Markdown content
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# %s\n\n", input.Topic))
	md.WriteString(fmt.Sprintf("**Date:** %s\n\n", now.Format(time.RFC3339)))
	if len(input.Tags) > 0 {
		md.WriteString(fmt.Sprintf("**Tags:** %s\n\n", strings.Join(input.Tags, ", ")))
	}
	md.WriteString(input.Content)
	md.WriteString("\n")
	if len(input.Refs) > 0 {
		md.WriteString("\n## References\n\n")
		for _, ref := range input.Refs {
			md.WriteString(fmt.Sprintf("- `%s`\n", ref))
		}
	}

	// Write markdown file
	if err := os.WriteFile(mdPath, []byte(md.String()), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write note: %v", err))
	}

	// Create summary (first 120 chars of content)
	summary := input.Content
	if len(summary) > 120 {
		summary = summary[:120] + "..."
	}
	// Remove newlines from summary
	summary = strings.ReplaceAll(summary, "\n", " ")

	// Append to index
	entry := IndexEntry{
		ID:        id,
		Topic:     input.Topic,
		Tags:      input.Tags,
		Refs:      input.Refs,
		File:      filename,
		CreatedAt: now.Format(time.RFC3339),
		Summary:   summary,
	}
	if err := appendIndex(entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("append index: %v", err))
	}

	result := AddResult{OK: true, ID: id, File: mdPath}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdList() {
	entries, err := loadIndex()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load index: %v", err))
	}
	for _, e := range entries {
		if err := jsonutil.WriteJSONL(e); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

func cmdSearch(query string) {
	entries, err := loadIndex()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load index: %v", err))
	}

	queryLower := strings.ToLower(query)
	for _, e := range entries {
		match := strings.Contains(strings.ToLower(e.Topic), queryLower) ||
			strings.Contains(strings.ToLower(e.Summary), queryLower) ||
			strings.Contains(strings.ToLower(e.ID), queryLower)

		if !match {
			for _, tag := range e.Tags {
				if strings.Contains(strings.ToLower(tag), queryLower) {
					match = true
					break
				}
			}
		}

		if !match {
			// Also search the file content if not matched by index fields
			mdPath := filepath.Join(notesDir, e.File)
			content, err := os.ReadFile(mdPath)
			if err == nil && strings.Contains(strings.ToLower(string(content)), queryLower) {
				match = true
			}
		}

		if match {
			if err := jsonutil.WriteJSONL(e); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
		}
	}
}

func cmdGet(id string) {
	entries, err := loadIndex()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load index: %v", err))
	}

	for _, e := range entries {
		if e.ID == id {
			mdPath := filepath.Join(notesDir, e.File)
			content, err := os.ReadFile(mdPath)
			if err != nil {
				jsonutil.Fatal(fmt.Sprintf("read note: %v", err))
			}
			note := NoteContent{
				ID:        e.ID,
				Topic:     e.Topic,
				Content:   string(content),
				Tags:      e.Tags,
				Refs:      e.Refs,
				CreatedAt: e.CreatedAt,
			}
			if err := jsonutil.WriteOutput(note); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
			return
		}
	}

	jsonutil.Fatal(fmt.Sprintf("note not found: %s", id))
}

func cmdRollup(topic string) {
	entries, err := loadIndex()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load index: %v", err))
	}

	topicLower := strings.ToLower(topic)
	var matching []IndexEntry
	for _, e := range entries {
		if strings.ToLower(e.Topic) == topicLower ||
			strings.Contains(strings.ToLower(e.Topic), topicLower) {
			matching = append(matching, e)
		}
	}

	if len(matching) == 0 {
		jsonutil.Fatal(fmt.Sprintf("no notes found for topic: %s", topic))
	}

	// Sort by created_at
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt < matching[j].CreatedAt
	})

	ensureDir()

	// Build rollup content
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# Rollup: %s\n\n", topic))
	md.WriteString(fmt.Sprintf("**Date:** %s\n", time.Now().Format(time.RFC3339)))
	md.WriteString(fmt.Sprintf("**Notes combined:** %d\n\n", len(matching)))
	md.WriteString("---\n\n")

	allTags := make(map[string]bool)
	var allRefs []string
	refSeen := make(map[string]bool)

	for _, e := range matching {
		mdPath := filepath.Join(notesDir, e.File)
		content, err := os.ReadFile(mdPath)
		if err != nil {
			md.WriteString(fmt.Sprintf("## [%s] (file missing)\n\n", e.CreatedAt))
			continue
		}
		md.WriteString(fmt.Sprintf("## %s (%s)\n\n", e.Topic, e.CreatedAt))
		md.Write(content)
		md.WriteString("\n\n---\n\n")

		for _, tag := range e.Tags {
			allTags[tag] = true
		}
		for _, ref := range e.Refs {
			if !refSeen[ref] {
				refSeen[ref] = true
				allRefs = append(allRefs, ref)
			}
		}
	}

	// Collect tags
	var tagList []string
	for tag := range allTags {
		tagList = append(tagList, tag)
	}
	sort.Strings(tagList)

	now := time.Now()
	ts := now.Format("20060102-150405")
	safeTopic := sanitizeTopic(topic)
	id := fmt.Sprintf("%s-rollup-%s", safeTopic, ts)
	filename := fmt.Sprintf("%s.md", id)
	mdPath := filepath.Join(notesDir, filename)

	if err := os.WriteFile(mdPath, []byte(md.String()), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write rollup: %v", err))
	}

	entry := IndexEntry{
		ID:        id,
		Topic:     fmt.Sprintf("rollup: %s", topic),
		Tags:      tagList,
		Refs:      allRefs,
		File:      filename,
		CreatedAt: now.Format(time.RFC3339),
		Summary:   fmt.Sprintf("Rollup of %d notes about %s", len(matching), topic),
	}
	if err := appendIndex(entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("append index: %v", err))
	}

	result := RollupResult{OK: true, ID: id, File: mdPath, Count: len(matching)}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdRecent(n int) {
	entries, err := loadIndex()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load index: %v", err))
	}

	// Sort by created_at descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt > entries[j].CreatedAt
	})

	if n > len(entries) {
		n = len(entries)
	}

	for i := 0; i < n; i++ {
		if err := jsonutil.WriteJSONL(entries[i]); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}
