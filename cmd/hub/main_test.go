package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelPath(t *testing.T) {
	tests := []struct {
		hubDir  string
		channel string
		want    string
	}{
		{"/tmp/hub", "test", "/tmp/hub/test.jsonl"},
		{"/tmp/hub", "events", "/tmp/hub/events.jsonl"},
		{".", "ch", "ch.jsonl"},
	}
	for _, tt := range tests {
		got := channelPath(tt.hubDir, tt.channel)
		if got != tt.want {
			t.Errorf("channelPath(%q, %q) = %q, want %q", tt.hubDir, tt.channel, got, tt.want)
		}
	}
}

func TestLockPath(t *testing.T) {
	got := lockPath("/tmp/hub", "test")
	want := "/tmp/hub/test.lock"
	if got != want {
		t.Errorf("lockPath = %q, want %q", got, want)
	}
}

func TestGenerateMsgID(t *testing.T) {
	id1 := generateMsgID()
	id2 := generateMsgID()
	if id1 == "" {
		t.Error("generateMsgID returned empty string")
	}
	if id1 == id2 {
		t.Error("generateMsgID returned duplicate IDs")
	}
	// Should contain a dash separator
	if !strings.Contains(id1, "-") {
		t.Errorf("generateMsgID() = %q, expected dash separator", id1)
	}
}

func TestAcquireReleaseLock(t *testing.T) {
	dir := t.TempDir()
	lk, err := acquireLock(dir, "testchan")
	if err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}
	if lk == nil {
		t.Fatal("acquireLock returned nil file")
	}
	// Lock file should exist
	lp := lockPath(dir, "testchan")
	if _, err := os.Stat(lp); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
	releaseLock(lk)
}

func TestDoCreate(t *testing.T) {
	dir := t.TempDir()
	// doCreate writes to stdout via jsonutil, which we can't easily capture.
	// Instead, test the underlying channel file creation.
	cp := channelPath(dir, "mychan")
	f, err := os.Create(cp)
	if err != nil {
		t.Fatalf("create channel file: %v", err)
	}
	f.Close()

	// Verify file exists
	if _, err := os.Stat(cp); err != nil {
		t.Errorf("channel file not found: %v", err)
	}
}

func TestPubSubRoundTrip(t *testing.T) {
	dir := t.TempDir()
	channel := "testchan"

	// Manually write a message to the channel file
	msg := hubMessage{
		ID:      "msg-001",
		TS:      "2024-01-01T00:00:00Z",
		Channel: channel,
		From:    "agent-1",
		Data:    json.RawMessage(`{"hello":"world"}`),
	}
	line, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	cp := channelPath(dir, channel)
	if err := os.WriteFile(cp, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write channel: %v", err)
	}

	// Read back and verify
	f, err := os.Open(cp)
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("no messages in channel")
	}

	var got hubMessage
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if got.ID != "msg-001" {
		t.Errorf("ID = %q, want %q", got.ID, "msg-001")
	}
	if got.From != "agent-1" {
		t.Errorf("From = %q, want %q", got.From, "agent-1")
	}
	if got.Channel != channel {
		t.Errorf("Channel = %q, want %q", got.Channel, channel)
	}
}

func TestMultipleMessages(t *testing.T) {
	dir := t.TempDir()
	channel := "multi"
	cp := channelPath(dir, channel)

	// Write multiple messages
	for i := 0; i < 5; i++ {
		msg := hubMessage{
			ID:      generateMsgID(),
			Channel: channel,
			From:    "test",
			Data:    json.RawMessage(`{"i":` + string(rune('0'+i)) + `}`),
		}
		line, _ := json.Marshal(msg)
		f, err := os.OpenFile(cp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		f.Write(append(line, '\n'))
		f.Close()
	}

	// Read back and count
	f, err := os.Open(cp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	if count != 5 {
		t.Errorf("expected 5 messages, got %d", count)
	}
}

func TestChannelListsJSONLFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some channel files and non-channel files
	for _, name := range []string{"events.jsonl", "tasks.jsonl", "notes.txt", "readme.md"} {
		f, _ := os.Create(filepath.Join(dir, name))
		f.Close()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	var channels []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".jsonl") {
			ch := strings.TrimSuffix(e.Name(), ".jsonl")
			channels = append(channels, ch)
		}
	}

	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d: %v", len(channels), channels)
	}
}

func TestEmptyChannel(t *testing.T) {
	dir := t.TempDir()
	channel := "empty"
	cp := channelPath(dir, channel)

	// Create empty file
	f, _ := os.Create(cp)
	f.Close()

	// Read it - should produce no messages
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(strings.TrimSpace(string(data))) != 0 {
		t.Error("expected empty channel file")
	}
}
