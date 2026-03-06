package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupRconTest(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	os.MkdirAll(filepath.Join(dir, ".clock"), 0o755)
	return func() { os.Chdir(origDir) }
}

func TestFindConnection(t *testing.T) {
	cfg := Config{
		Connections: []Connection{
			{Name: "dev", Host: "dev.example.com", User: "alice"},
			{Name: "prod", Host: "prod.example.com", User: "bob"},
		},
	}

	conn, err := findConnection(cfg, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Host != "dev.example.com" {
		t.Errorf("Host = %q, want dev.example.com", conn.Host)
	}
}

func TestFindConnection_NotFound(t *testing.T) {
	cfg := Config{Connections: []Connection{}}

	_, err := findConnection(cfg, "missing")
	if err == nil {
		t.Error("expected error for missing connection")
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		input string
		check func(string) bool
	}{
		{"/absolute/path", func(s string) bool { return s == "/absolute/path" }},
		{"relative/path", func(s string) bool { return s == "relative/path" }},
		{"~/keys/id_ed25519", func(s string) bool { return !strings.HasPrefix(s, "~/") || s == "~/keys/id_ed25519" }},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandPath(tt.input)
			if !tt.check(got) {
				t.Errorf("expandPath(%q) = %q, unexpected result", tt.input, got)
			}
		})
	}
}

func TestBuildSSHArgs(t *testing.T) {
	conn := &Connection{
		Name: "dev",
		Host: "dev.example.com",
		User: "alice",
		Port: 2222,
		Key:  "/home/alice/.ssh/id_ed25519",
	}

	args := buildSSHArgs(conn)

	// Check key flag
	foundKey := false
	for i, a := range args {
		if a == "-i" && i+1 < len(args) && args[i+1] == "/home/alice/.ssh/id_ed25519" {
			foundKey = true
		}
	}
	if !foundKey {
		t.Error("expected -i flag with key path")
	}

	// Check port flag
	foundPort := false
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && args[i+1] == "2222" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Error("expected -p 2222 flag")
	}

	// Check target
	target := "alice@dev.example.com"
	found := false
	for _, a := range args {
		if a == target {
			found = true
		}
	}
	if !found {
		t.Errorf("expected target %q in args %v", target, args)
	}
}

func TestBuildSSHArgs_DefaultPort(t *testing.T) {
	conn := &Connection{
		Host: "example.com",
		User: "user",
		Port: 0, // should default to 22
	}

	args := buildSSHArgs(conn)
	foundPort := false
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && args[i+1] == "22" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Error("expected default port 22")
	}
}

func TestBuildSSHArgs_NoKey(t *testing.T) {
	conn := &Connection{
		Host: "example.com",
		User: "user",
		Port: 22,
	}

	args := buildSSHArgs(conn)
	for _, a := range args {
		if a == "-i" {
			t.Error("should not have -i flag when no key is set")
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	conn := &Connection{
		Host: "example.com",
		User: "user",
	}

	applyDefaults(conn)

	if conn.Mode != "ro" {
		t.Errorf("Mode = %q, want 'ro'", conn.Mode)
	}
	if conn.Sess != "tmux" {
		t.Errorf("Sess = %q, want 'tmux'", conn.Sess)
	}
	if conn.Transport != "tailscale" {
		t.Errorf("Transport = %q, want 'tailscale'", conn.Transport)
	}
}

func TestApplyDefaults_LegacyMigration(t *testing.T) {
	conn := &Connection{
		Host:        "example.com",
		User:        "user",
		Tunnel:      "tailscale",
		TmuxSession: "my-session",
	}

	applyDefaults(conn)

	if conn.Transport != "tailscale" {
		t.Errorf("Transport = %q, want 'tailscale' (migrated from Tunnel)", conn.Transport)
	}
	if conn.Sess != "tmux" {
		t.Errorf("Sess = %q, want 'tmux' (migrated from TmuxSession)", conn.Sess)
	}
}

func TestApplyDefaults_PreserveExisting(t *testing.T) {
	conn := &Connection{
		Host:      "example.com",
		User:      "user",
		Mode:      "rw",
		Sess:      "none",
		Transport: "direct",
	}

	applyDefaults(conn)

	if conn.Mode != "rw" {
		t.Errorf("Mode = %q, want 'rw' (should preserve existing)", conn.Mode)
	}
	if conn.Sess != "none" {
		t.Errorf("Sess = %q, want 'none' (should preserve existing)", conn.Sess)
	}
	if conn.Transport != "direct" {
		t.Errorf("Transport = %q, want 'direct' (should preserve existing)", conn.Transport)
	}
}

func TestTmuxSessionName(t *testing.T) {
	tests := []struct {
		conn *Connection
		want string
	}{
		{&Connection{User: "alice", TmuxSession: "my-session"}, "my-session"},
		{&Connection{User: "alice"}, "alice-main"},
		{&Connection{User: "bob", TmuxSession: ""}, "bob-main"},
	}

	for _, tt := range tests {
		got := tmuxSessionName(tt.conn)
		if got != tt.want {
			t.Errorf("tmuxSessionName(%+v) = %q, want %q", tt.conn, got, tt.want)
		}
	}
}

func TestBuildSSHCommand(t *testing.T) {
	conn := &Connection{
		Host: "dev.example.com",
		User: "alice",
		Port: 22,
		Sess: "none",
	}

	cmd := buildSSHCommand(conn, "")
	if !strings.Contains(cmd, "ssh") {
		t.Error("expected 'ssh' in command")
	}
	if !strings.Contains(cmd, "alice@dev.example.com") {
		t.Error("expected target in command")
	}
}

func TestBuildSSHCommand_WithRepo(t *testing.T) {
	conn := &Connection{
		Host: "dev.example.com",
		User: "alice",
		Port: 22,
		Repo: "/home/alice/project",
		Sess: "none",
	}

	cmd := buildSSHCommand(conn, "")
	if !strings.Contains(cmd, "cd /home/alice/project") {
		t.Errorf("expected 'cd /home/alice/project' in command, got: %s", cmd)
	}
}

func TestBuildSSHCommand_WithTmux(t *testing.T) {
	conn := &Connection{
		Host: "dev.example.com",
		User: "alice",
		Port: 22,
		Sess: "tmux",
	}

	cmd := buildSSHCommand(conn, "")
	if !strings.Contains(cmd, "tmux") {
		t.Errorf("expected 'tmux' in command, got: %s", cmd)
	}
}

func TestBuildSSHCommand_WithCmd(t *testing.T) {
	conn := &Connection{
		Host: "dev.example.com",
		User: "alice",
		Port: 22,
		Sess: "none",
	}

	cmd := buildSSHCommand(conn, "ls -la")
	if !strings.Contains(cmd, "ls -la") {
		t.Errorf("expected 'ls -la' in command, got: %s", cmd)
	}
}

func TestLoadSaveConfig(t *testing.T) {
	cleanup := setupRconTest(t)
	defer cleanup()

	cfg := Config{
		Connections: []Connection{
			{Name: "dev", Host: "dev.example.com", User: "alice", Port: 22},
		},
	}

	saveConfig(cfg)

	loaded := loadConfig()
	if len(loaded.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(loaded.Connections))
	}
	if loaded.Connections[0].Name != "dev" {
		t.Errorf("expected name 'dev', got %q", loaded.Connections[0].Name)
	}
}

func TestLoadConfig_NotExist(t *testing.T) {
	cleanup := setupRconTest(t)
	defer cleanup()

	cfg := loadConfig()
	if cfg.Connections == nil {
		t.Error("expected non-nil connections for empty config")
	}
	if len(cfg.Connections) != 0 {
		t.Errorf("expected 0 connections, got %d", len(cfg.Connections))
	}
}

func TestConfigValidation(t *testing.T) {
	// Test that a connection can be properly serialized/deserialized
	conn := Connection{
		Name:      "test",
		Host:      "example.com",
		User:      "alice",
		Port:      2222,
		Key:       "~/.ssh/id_ed25519",
		Mode:      "rw",
		Sess:      "tmux",
		Transport: "tailscale",
		Repo:      "/home/alice/project",
	}

	data, err := json.Marshal(conn)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Connection
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Name != conn.Name || decoded.Host != conn.Host || decoded.Port != conn.Port {
		t.Errorf("decoded connection doesn't match: %+v vs %+v", decoded, conn)
	}
}
