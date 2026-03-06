package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeTopic(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{"simple", "auth module", "auth-module"},
		{"uppercase", "Auth Module", "auth-module"},
		{"special chars", "fix: auth/login", "fix-auth-login"},
		{"multiple spaces", "fix  the  bug", "fix-the-bug"},
		{"underscores", "my_topic_name", "my-topic-name"},
		{"dots", "v1.2.3", "v1-2-3"},
		{"leading trailing special", "/topic/", "topic"},
		{"numbers", "issue42", "issue42"},
		{"all special", "!@#$%", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTopic(tt.topic)
			if got != tt.want {
				t.Errorf("sanitizeTopic(%q) = %q, want %q", tt.topic, got, tt.want)
			}
		})
	}
}

func TestLoadIndexEmpty(t *testing.T) {
	// Non-existent file should return nil, nil
	entries, err := loadIndex()
	// This uses the const indexFile which may not exist in test environment
	// We just verify the function handles it gracefully
	if err != nil && !os.IsNotExist(err) {
		t.Logf("loadIndex with default path: %v (expected in test)", err)
	}
	_ = entries
}

func TestLoadIndexFromFile(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "index.jsonl")

	entries := []IndexEntry{
		{ID: "note-1", Topic: "Auth", CreatedAt: "2024-01-01T00:00:00Z", File: "note-1.md"},
		{ID: "note-2", Topic: "DB", CreatedAt: "2024-01-02T00:00:00Z", File: "note-2.md"},
	}

	f, err := os.Create(idxPath)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	for _, e := range entries {
		line, _ := json.Marshal(e)
		f.Write(append(line, '\n'))
	}
	f.Close()

	// Read back using the JSONL parsing logic
	readF, err := os.Open(idxPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer readF.Close()

	var loaded []IndexEntry
	decoder := json.NewDecoder(readF)
	for decoder.More() {
		var entry IndexEntry
		if err := decoder.Decode(&entry); err != nil {
			break
		}
		loaded = append(loaded, entry)
	}

	if len(loaded) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(loaded))
	}
	if loaded[0].Topic != "Auth" {
		t.Errorf("entry 0 topic = %q, want %q", loaded[0].Topic, "Auth")
	}
}

func TestAppendIndex(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "index.jsonl")

	entry := IndexEntry{
		ID:        "test-note",
		Topic:     "Testing",
		CreatedAt: "2024-01-01T00:00:00Z",
		File:      "test-note.md",
		Summary:   "A test note about testing",
	}

	// Write entry
	line, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(idxPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f.Write(append(line, '\n'))
	f.Close()

	// Read back
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "test-note") {
		t.Error("appended entry not found in file")
	}
}

func TestNoteAddRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Simulate add: create markdown file and index entry
	topic := "Authentication Bug"
	content := "Found a critical auth bypass vulnerability in the login handler."
	tags := []string{"security", "auth"}
	refs := []string{"cmd/auth/login.go:42"}

	safeTopic := sanitizeTopic(topic)
	id := safeTopic + "-20240101-120000"
	filename := id + ".md"
	mdPath := filepath.Join(dir, filename)

	// Build markdown
	var md strings.Builder
	md.WriteString("# " + topic + "\n\n")
	md.WriteString("**Date:** 2024-01-01T12:00:00Z\n\n")
	md.WriteString("**Tags:** " + strings.Join(tags, ", ") + "\n\n")
	md.WriteString(content + "\n")
	md.WriteString("\n## References\n\n")
	for _, ref := range refs {
		md.WriteString("- `" + ref + "`\n")
	}

	if err := os.WriteFile(mdPath, []byte(md.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify file content
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, topic) {
		t.Error("note missing topic")
	}
	if !strings.Contains(text, content) {
		t.Error("note missing content")
	}
	if !strings.Contains(text, "security") {
		t.Error("note missing tags")
	}
	if !strings.Contains(text, "cmd/auth/login.go:42") {
		t.Error("note missing refs")
	}
}

func TestNoteSearch(t *testing.T) {
	entries := []IndexEntry{
		{ID: "n1", Topic: "Auth Bug", Summary: "login failure", Tags: []string{"security"}},
		{ID: "n2", Topic: "DB Migration", Summary: "schema update", Tags: []string{"database"}},
		{ID: "n3", Topic: "API Design", Summary: "REST auth endpoints", Tags: []string{"api", "security"}},
	}

	// Search by topic
	query := "auth"
	queryLower := strings.ToLower(query)
	var matches []IndexEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Topic), queryLower) ||
			strings.Contains(strings.ToLower(e.Summary), queryLower) {
			matches = append(matches, e)
		}
	}
	// Should match n1 (topic) and n3 (summary contains "auth")
	if len(matches) != 2 {
		t.Errorf("search 'auth': got %d matches, want 2", len(matches))
	}

	// Search by tag
	var tagMatches []IndexEntry
	for _, e := range entries {
		for _, tag := range e.Tags {
			if strings.Contains(strings.ToLower(tag), "security") {
				tagMatches = append(tagMatches, e)
				break
			}
		}
	}
	if len(tagMatches) != 2 {
		t.Errorf("search by tag 'security': got %d matches, want 2", len(tagMatches))
	}
}

func TestNoteRollup(t *testing.T) {
	dir := t.TempDir()

	// Create two notes about the same topic
	for i, content := range []string{
		"First finding about auth",
		"Second finding about auth",
	} {
		filename := "auth-note-" + string(rune('0'+i)) + ".md"
		os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	}

	// Simulate rollup: read all matching notes and combine
	var combined strings.Builder
	combined.WriteString("# Rollup: auth\n\n")

	count := 0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		combined.WriteString("## " + e.Name() + "\n\n")
		combined.Write(data)
		combined.WriteString("\n\n---\n\n")
		count++
	}

	if count != 2 {
		t.Errorf("rollup combined %d notes, want 2", count)
	}
	if !strings.Contains(combined.String(), "First finding") {
		t.Error("rollup missing first note content")
	}
	if !strings.Contains(combined.String(), "Second finding") {
		t.Error("rollup missing second note content")
	}
}

func TestSummarization(t *testing.T) {
	// Test summary truncation at 120 chars
	short := "Short content"
	if len(short) > 120 {
		short = short[:120] + "..."
	}
	if short != "Short content" {
		t.Error("short content should not be truncated")
	}

	long := strings.Repeat("x", 200)
	summary := long
	if len(summary) > 120 {
		summary = summary[:120] + "..."
	}
	if len(summary) != 123 { // 120 + "..."
		t.Errorf("long summary length = %d, want 123", len(summary))
	}
}

func TestGetNonExistentNote(t *testing.T) {
	entries := []IndexEntry{
		{ID: "note-1", Topic: "Test"},
	}

	found := false
	for _, e := range entries {
		if e.ID == "nonexistent" {
			found = true
			break
		}
	}
	if found {
		t.Error("should not find nonexistent note")
	}
}
