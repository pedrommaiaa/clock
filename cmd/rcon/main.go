// Command rcon is a remote connection manager.
// It manages SSH connection configurations and provides subcommands
// for listing, connecting, running remote commands, and checking status.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Config is the rcon configuration file structure.
type Config struct {
	Connections []Connection `json:"connections"`
}

// Connection is a single SSH connection configuration.
type Connection struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	User        string `json:"user"`
	Key         string `json:"key,omitempty"`
	Port        int    `json:"port,omitempty"`
	Tunnel      string `json:"tunnel,omitempty"`    // legacy field, mapped to Transport
	TmuxSession string `json:"tmux_session,omitempty"` // legacy field, mapped to Sess
	Repo        string `json:"repo,omitempty"`      // path to cd into after connecting
	Mode        string `json:"mode,omitempty"`      // "ro" or "rw" (default: "ro")
	Sess        string `json:"sess,omitempty"`      // "tmux" or "none" (default: "tmux")
	Transport   string `json:"transport,omitempty"` // "tailscale" or "direct" (default: "tailscale")
}

// ListOutput is the output of the list subcommand.
type ListOutput struct {
	Connections []ConnectionInfo `json:"connections"`
	Count       int              `json:"count"`
}

// ConnectionInfo is a summary of a connection for listing.
type ConnectionInfo struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	User   string `json:"user"`
	Port   int    `json:"port"`
	Tunnel string `json:"tunnel,omitempty"`
}

// ConnectOutput is the output of the connect subcommand.
type ConnectOutput struct {
	OK        bool     `json:"ok"`
	Transport string   `json:"transport"`
	Host      string   `json:"host"`
	User      string   `json:"user"`
	Repo      string   `json:"repo,omitempty"`
	Mode      string   `json:"mode"`
	Sess      string   `json:"sess"`
	Connect   string   `json:"connect"`
	Notes     []string `json:"notes"`
}

// StdinInput is the JSON input accepted on stdin when rcon is called with no subcommand.
type StdinInput struct {
	Host      string `json:"host"`
	User      string `json:"user"`
	Repo      string `json:"repo,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Sess      string `json:"sess,omitempty"`
	Cmd       string `json:"cmd,omitempty"`
	Transport string `json:"transport,omitempty"`
}

// RunOutput is the output of the run subcommand.
type RunOutput struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Code     int    `json:"code"`
	Output   string `json:"output"`
	ErrorOut string `json:"error,omitempty"`
	Ms       int64  `json:"ms"`
}

// AddOutput is the output of the add subcommand.
type AddOutput struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// RemoveOutput is the output of the remove subcommand.
type RemoveOutput struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// StatusOutput is the output of the status subcommand.
type StatusOutput struct {
	Name      string `json:"name"`
	Reachable bool   `json:"reachable"`
	Ms        int64  `json:"ms"`
	Error     string `json:"error,omitempty"`
}

const configFile = ".clock/rcon.json"

func main() {
	if len(os.Args) < 2 {
		// No subcommand: read JSON from stdin
		cmdStdin()
		return
	}

	sub := os.Args[1]
	switch sub {
	case "list":
		cmdList()
	case "connect":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: rcon connect <name>")
		}
		cmdConnect(os.Args[2])
	case "run":
		if len(os.Args) < 4 {
			jsonutil.Fatal("usage: rcon run <name> <cmd...>")
		}
		cmdRun(os.Args[2], os.Args[3:])
	case "add":
		cmdAdd()
	case "remove":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: rcon remove <name>")
		}
		cmdRemove(os.Args[2])
	case "status":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: rcon status <name>")
		}
		cmdStatus(os.Args[2])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", sub))
	}
}

// loadConfig reads the rcon configuration file.
func loadConfig() Config {
	var cfg Config
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Connections: []Connection{}}
		}
		jsonutil.Fatal(fmt.Sprintf("read config: %v", err))
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		jsonutil.Fatal(fmt.Sprintf("parse config: %v", err))
	}
	if cfg.Connections == nil {
		cfg.Connections = []Connection{}
	}
	return cfg
}

// saveConfig writes the rcon configuration file.
func saveConfig(cfg Config) {
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir: %v", err))
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal config: %v", err))
	}
	if err := os.WriteFile(configFile, data, 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write config: %v", err))
	}
}

// findConnection looks up a connection by name.
func findConnection(cfg Config, name string) (*Connection, error) {
	for i := range cfg.Connections {
		if cfg.Connections[i].Name == name {
			return &cfg.Connections[i], nil
		}
	}
	return nil, fmt.Errorf("connection not found: %s", name)
}

// expandPath expands ~ in key paths.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// buildSSHArgs builds SSH command arguments for a connection.
func buildSSHArgs(conn *Connection) []string {
	args := []string{}

	if conn.Key != "" {
		args = append(args, "-i", expandPath(conn.Key))
	}

	port := conn.Port
	if port == 0 {
		port = 22
	}
	args = append(args, "-p", fmt.Sprintf("%d", port))

	// Add connection timeout
	args = append(args, "-o", "ConnectTimeout=10")
	// Disable strict host key checking for convenience
	args = append(args, "-o", "StrictHostKeyChecking=accept-new")

	target := fmt.Sprintf("%s@%s", conn.User, conn.Host)
	args = append(args, target)

	return args
}

// applyDefaults fills in default values for a connection and migrates legacy fields.
func applyDefaults(conn *Connection) {
	// Migrate legacy fields
	if conn.Transport == "" && conn.Tunnel != "" {
		conn.Transport = conn.Tunnel
	}
	if conn.Sess == "" && conn.TmuxSession != "" {
		conn.Sess = "tmux"
	}

	// Apply defaults
	if conn.Mode == "" {
		conn.Mode = "ro"
	}
	if conn.Sess == "" {
		conn.Sess = "tmux"
	}
	if conn.Transport == "" {
		conn.Transport = "tailscale"
	}
}

// tmuxSessionName returns the tmux session name for a connection.
func tmuxSessionName(conn *Connection) string {
	if conn.TmuxSession != "" {
		return conn.TmuxSession
	}
	return conn.User + "-main"
}

// buildSSHCommand builds the full SSH command string for display.
// The optional cmd parameter is a command to run after connecting.
func buildSSHCommand(conn *Connection, cmd string) string {
	args := []string{"ssh"}

	if conn.Key != "" {
		args = append(args, "-i", expandPath(conn.Key))
	}

	port := conn.Port
	if port == 0 {
		port = 22
	}
	args = append(args, "-p", fmt.Sprintf("%d", port))

	target := fmt.Sprintf("%s@%s", conn.User, conn.Host)
	args = append(args, target)

	// Build remote command chain
	var remoteParts []string

	if conn.Repo != "" {
		remoteParts = append(remoteParts, fmt.Sprintf("cd %s", conn.Repo))
	}

	if cmd != "" {
		remoteParts = append(remoteParts, cmd)
	}

	if conn.Sess == "tmux" {
		sessName := tmuxSessionName(conn)
		if len(remoteParts) > 0 {
			// If we have cd or cmd, chain them before tmux
			setup := strings.Join(remoteParts, " && ")
			tmuxCmd := fmt.Sprintf("%s && tmux attach -t %s || tmux new -s %s", setup, sessName, sessName)
			args = append(args, "-t", fmt.Sprintf("'%s'", tmuxCmd))
		} else {
			tmuxCmd := fmt.Sprintf("tmux attach -t %s || tmux new -s %s", sessName, sessName)
			args = append(args, "-t", fmt.Sprintf("'%s'", tmuxCmd))
		}
	} else if len(remoteParts) > 0 {
		// No tmux, but we have cd or cmd
		remote := strings.Join(remoteParts, " && ")
		args = append(args, fmt.Sprintf("'%s'", remote))
	}

	return strings.Join(args, " ")
}

func cmdList() {
	cfg := loadConfig()

	infos := make([]ConnectionInfo, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		port := c.Port
		if port == 0 {
			port = 22
		}
		infos = append(infos, ConnectionInfo{
			Name:   c.Name,
			Host:   c.Host,
			User:   c.User,
			Port:   port,
			Tunnel: c.Tunnel,
		})
	}

	output := ListOutput{
		Connections: infos,
		Count:       len(infos),
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdConnect(name string) {
	cfg := loadConfig()
	conn, err := findConnection(cfg, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	applyDefaults(conn)

	notes := []string{}
	if conn.Sess == "tmux" {
		notes = append(notes, fmt.Sprintf("tmux session: %s", tmuxSessionName(conn)))
	}
	if conn.Mode == "ro" {
		notes = append(notes, "read-only mode")
	}

	output := ConnectOutput{
		OK:        true,
		Transport: conn.Transport,
		Host:      conn.Host,
		User:      conn.User,
		Repo:      conn.Repo,
		Mode:      conn.Mode,
		Sess:      conn.Sess,
		Connect:   buildSSHCommand(conn, ""),
		Notes:     notes,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// cmdStdin handles JSON input on stdin when no subcommand is given.
func cmdStdin() {
	var input StdinInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Host == "" {
		jsonutil.Fatal("host is required")
	}
	if input.User == "" {
		jsonutil.Fatal("user is required")
	}

	conn := &Connection{
		Host:      input.Host,
		User:      input.User,
		Repo:      input.Repo,
		Mode:      input.Mode,
		Sess:      input.Sess,
		Transport: input.Transport,
	}
	applyDefaults(conn)

	notes := []string{}
	if conn.Sess == "tmux" {
		notes = append(notes, fmt.Sprintf("tmux session: %s", tmuxSessionName(conn)))
	}
	if conn.Mode == "ro" {
		notes = append(notes, "read-only mode")
	}

	output := ConnectOutput{
		OK:        true,
		Transport: conn.Transport,
		Host:      conn.Host,
		User:      conn.User,
		Repo:      conn.Repo,
		Mode:      conn.Mode,
		Sess:      conn.Sess,
		Connect:   buildSSHCommand(conn, input.Cmd),
		Notes:     notes,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdRun(name string, cmdArgs []string) {
	cfg := loadConfig()
	conn, err := findConnection(cfg, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	sshArgs := buildSSHArgs(conn)
	remoteCmd := strings.Join(cmdArgs, " ")
	sshArgs = append(sshArgs, remoteCmd)

	start := time.Now()

	cmd := exec.Command("ssh", sshArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	output := RunOutput{
		Name:     name,
		Command:  remoteCmd,
		Code:     exitCode,
		Output:   stdout.String(),
		ErrorOut: stderr.String(),
		Ms:       elapsed,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdAdd() {
	var conn Connection
	if err := jsonutil.ReadInput(&conn); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if conn.Name == "" {
		jsonutil.Fatal("connection name is required")
	}
	if conn.Host == "" {
		jsonutil.Fatal("host is required")
	}
	if conn.User == "" {
		jsonutil.Fatal("user is required")
	}
	if conn.Port == 0 {
		conn.Port = 22
	}

	cfg := loadConfig()

	// Check for duplicate
	for _, c := range cfg.Connections {
		if c.Name == conn.Name {
			jsonutil.Fatal(fmt.Sprintf("connection %q already exists", conn.Name))
		}
	}

	cfg.Connections = append(cfg.Connections, conn)
	saveConfig(cfg)

	output := AddOutput{
		Name:   conn.Name,
		Status: "added",
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdRemove(name string) {
	cfg := loadConfig()

	found := false
	newConns := make([]Connection, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		if c.Name == name {
			found = true
			continue
		}
		newConns = append(newConns, c)
	}

	if !found {
		jsonutil.Fatal(fmt.Sprintf("connection not found: %s", name))
	}

	cfg.Connections = newConns
	saveConfig(cfg)

	output := RemoveOutput{
		Name:   name,
		Status: "removed",
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdStatus(name string) {
	cfg := loadConfig()
	conn, err := findConnection(cfg, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	// Check reachability with a short SSH connection
	sshArgs := buildSSHArgs(conn)
	sshArgs = append(sshArgs, "-o", "ConnectTimeout=5")
	sshArgs = append(sshArgs, "echo", "ok")

	start := time.Now()

	cmd := exec.Command("ssh", sshArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	output := StatusOutput{
		Name:      name,
		Reachable: runErr == nil && strings.TrimSpace(stdout.String()) == "ok",
		Ms:        elapsed,
	}

	if runErr != nil {
		output.Error = runErr.Error()
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
