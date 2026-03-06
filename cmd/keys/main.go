// Command keys is an SSH key and device access manager.
// It stores SSH public keys with device metadata in .clock/keys.json
// and can generate restricted SSH authorized_keys entries.
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

// DeviceEntry is a single authorized device with its SSH key.
type DeviceEntry struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`    // ro or rw
	PubKey  string `json:"pubkey"`  // e.g. "ssh-ed25519 AAAA..."
	Created string `json:"created"` // ISO8601 timestamp
}

// KeyStore is the on-disk device key storage.
type KeyStore struct {
	Devices []DeviceEntry `json:"devices"`
}

// AddInput is the JSON input for the add subcommand (read from stdin).
type AddInput struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	PubKey string `json:"pubkey"`
	Mode   string `json:"mode"`
}

// ListOutput is the output of the list subcommand.
type ListOutput struct {
	OK      bool             `json:"ok"`
	Devices []ListDeviceItem `json:"devices"`
}

// ListDeviceItem is a device summary in the list output.
type ListDeviceItem struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// CheckOutput is the output of the check subcommand.
type CheckOutput struct {
	Name       string `json:"name"`
	Authorized bool   `json:"authorized"`
	Mode       string `json:"mode,omitempty"`
}

// ResultOutput is a generic ok/error output.
type ResultOutput struct {
	OK     bool   `json:"ok"`
	Status string `json:"status,omitempty"`
	Name   string `json:"name,omitempty"`
}

const storeFile = ".clock/keys.json"

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: keys <subcommand> [args]\nsubcommands: add, remove, list, check, sync")
	}

	sub := os.Args[1]
	switch sub {
	case "add":
		cmdAdd()
	case "remove":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: keys remove <name>")
		}
		cmdRemove(os.Args[2])
	case "list":
		cmdList()
	case "check":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: keys check <name>")
		}
		cmdCheck(os.Args[2])
	case "sync":
		cmdSync()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", sub))
	}
}

// loadStore reads the key store from disk.
func loadStore() KeyStore {
	var store KeyStore
	data, err := os.ReadFile(storeFile)
	if err != nil {
		if os.IsNotExist(err) {
			return KeyStore{Devices: []DeviceEntry{}}
		}
		jsonutil.Fatal(fmt.Sprintf("read store: %v", err))
	}
	if err := json.Unmarshal(data, &store); err != nil {
		jsonutil.Fatal(fmt.Sprintf("parse store: %v", err))
	}
	if store.Devices == nil {
		store.Devices = []DeviceEntry{}
	}
	return store
}

// saveStore writes the key store to disk with 0600 permissions.
func saveStore(store KeyStore) {
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir: %v", err))
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal store: %v", err))
	}
	if err := os.WriteFile(storeFile, data, 0o600); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write store: %v", err))
	}
}

// findDevice returns the index of a device by name, or -1 if not found.
func findDevice(store KeyStore, name string) int {
	for i, d := range store.Devices {
		if d.Name == name {
			return i
		}
	}
	return -1
}

// cmdAdd reads JSON from stdin and adds a device key.
func cmdAdd() {
	var input AddInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Name == "" {
		jsonutil.Fatal("name is required")
	}
	if input.PubKey == "" {
		jsonutil.Fatal("pubkey is required")
	}
	if input.Mode == "" {
		input.Mode = "ro"
	}
	if input.Mode != "ro" && input.Mode != "rw" {
		jsonutil.Fatal(fmt.Sprintf("invalid mode: %q (must be ro or rw)", input.Mode))
	}

	store := loadStore()

	// Replace if device name already exists
	idx := findDevice(store, input.Name)
	entry := DeviceEntry{
		Name:    input.Name,
		Mode:    input.Mode,
		PubKey:  input.PubKey,
		Created: time.Now().UTC().Format(time.RFC3339),
	}

	if idx >= 0 {
		store.Devices[idx] = entry
	} else {
		store.Devices = append(store.Devices, entry)
	}
	saveStore(store)

	out := ResultOutput{OK: true, Status: "added", Name: input.Name}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// cmdRemove removes a device key by name.
func cmdRemove(name string) {
	store := loadStore()
	idx := findDevice(store, name)
	if idx < 0 {
		jsonutil.Fatal(fmt.Sprintf("device not found: %s", name))
	}

	store.Devices = append(store.Devices[:idx], store.Devices[idx+1:]...)
	saveStore(store)

	out := ResultOutput{OK: true, Status: "removed", Name: name}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// cmdList lists all authorized devices.
func cmdList() {
	store := loadStore()

	items := make([]ListDeviceItem, len(store.Devices))
	for i, d := range store.Devices {
		items[i] = ListDeviceItem{Name: d.Name, Mode: d.Mode}
	}

	out := ListOutput{OK: true, Devices: items}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// cmdCheck checks if a device is authorized.
func cmdCheck(name string) {
	store := loadStore()
	idx := findDevice(store, name)

	if idx < 0 {
		out := CheckOutput{Name: name, Authorized: false}
		if err := jsonutil.WriteOutput(out); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	out := CheckOutput{
		Name:       name,
		Authorized: true,
		Mode:       store.Devices[idx].Mode,
	}
	if err := jsonutil.WriteOutput(out); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// cmdSync generates SSH authorized_keys entries with restrictions and writes to stdout.
func cmdSync() {
	store := loadStore()

	var lines []string
	for _, d := range store.Devices {
		var prefix string
		switch d.Mode {
		case "ro":
			prefix = `command="/usr/local/bin/clock-shell --mode ro",no-port-forwarding,no-agent-forwarding`
		case "rw":
			prefix = `no-port-forwarding`
		default:
			continue
		}
		line := fmt.Sprintf("%s %s", prefix, strings.TrimSpace(d.PubKey))
		lines = append(lines, line)
	}

	// Write raw authorized_keys content to stdout (user can redirect to ~/.ssh/authorized_keys)
	for _, l := range lines {
		fmt.Fprintln(os.Stdout, l)
	}
}
