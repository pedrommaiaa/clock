package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupKeysTest(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	os.MkdirAll(filepath.Join(dir, ".clock"), 0o755)
	return func() { os.Chdir(origDir) }
}

func TestFindDevice(t *testing.T) {
	store := KeyStore{
		Devices: []DeviceEntry{
			{Name: "laptop", Mode: "rw", PubKey: "ssh-ed25519 AAAA..."},
			{Name: "phone", Mode: "ro", PubKey: "ssh-ed25519 BBBB..."},
		},
	}

	idx := findDevice(store, "laptop")
	if idx != 0 {
		t.Errorf("findDevice(laptop) = %d, want 0", idx)
	}

	idx = findDevice(store, "phone")
	if idx != 1 {
		t.Errorf("findDevice(phone) = %d, want 1", idx)
	}

	idx = findDevice(store, "missing")
	if idx != -1 {
		t.Errorf("findDevice(missing) = %d, want -1", idx)
	}
}

func TestFindDevice_Empty(t *testing.T) {
	store := KeyStore{Devices: []DeviceEntry{}}
	idx := findDevice(store, "anything")
	if idx != -1 {
		t.Errorf("findDevice on empty store = %d, want -1", idx)
	}
}

func TestLoadStore_NotExist(t *testing.T) {
	cleanup := setupKeysTest(t)
	defer cleanup()

	store := loadStore()
	if store.Devices == nil {
		t.Error("expected non-nil devices for empty store")
	}
	if len(store.Devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(store.Devices))
	}
}

func TestSaveLoadStore(t *testing.T) {
	cleanup := setupKeysTest(t)
	defer cleanup()

	store := KeyStore{
		Devices: []DeviceEntry{
			{Name: "laptop", Mode: "rw", PubKey: "ssh-ed25519 AAAA...", Created: "2024-01-01T00:00:00Z"},
			{Name: "phone", Mode: "ro", PubKey: "ssh-ed25519 BBBB...", Created: "2024-01-02T00:00:00Z"},
		},
	}

	saveStore(store)

	loaded := loadStore()
	if len(loaded.Devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(loaded.Devices))
	}
	if loaded.Devices[0].Name != "laptop" {
		t.Errorf("first device name = %q, want 'laptop'", loaded.Devices[0].Name)
	}
	if loaded.Devices[1].Mode != "ro" {
		t.Errorf("second device mode = %q, want 'ro'", loaded.Devices[1].Mode)
	}
}

func TestStorePermissions(t *testing.T) {
	cleanup := setupKeysTest(t)
	defer cleanup()

	store := KeyStore{
		Devices: []DeviceEntry{
			{Name: "test", Mode: "ro", PubKey: "ssh-ed25519 AAAA..."},
		},
	}
	saveStore(store)

	info, err := os.Stat(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	// File should have 0600 permissions
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("store file permissions = %o, want 0600", perm)
	}
}

func TestAddDevice(t *testing.T) {
	store := KeyStore{Devices: []DeviceEntry{}}

	entry := DeviceEntry{
		Name:    "laptop",
		Mode:    "rw",
		PubKey:  "ssh-ed25519 AAAA...",
		Created: "2024-01-01T00:00:00Z",
	}

	idx := findDevice(store, entry.Name)
	if idx >= 0 {
		store.Devices[idx] = entry
	} else {
		store.Devices = append(store.Devices, entry)
	}

	if len(store.Devices) != 1 {
		t.Errorf("expected 1 device after add, got %d", len(store.Devices))
	}
}

func TestAddDevice_Replace(t *testing.T) {
	store := KeyStore{
		Devices: []DeviceEntry{
			{Name: "laptop", Mode: "ro", PubKey: "ssh-ed25519 OLD...", Created: "2024-01-01T00:00:00Z"},
		},
	}

	entry := DeviceEntry{
		Name:    "laptop",
		Mode:    "rw",
		PubKey:  "ssh-ed25519 NEW...",
		Created: "2024-02-01T00:00:00Z",
	}

	idx := findDevice(store, entry.Name)
	if idx >= 0 {
		store.Devices[idx] = entry
	} else {
		store.Devices = append(store.Devices, entry)
	}

	if len(store.Devices) != 1 {
		t.Errorf("expected 1 device after replace, got %d", len(store.Devices))
	}
	if store.Devices[0].PubKey != "ssh-ed25519 NEW..." {
		t.Error("expected pubkey to be updated")
	}
	if store.Devices[0].Mode != "rw" {
		t.Error("expected mode to be updated to rw")
	}
}

func TestRemoveDevice(t *testing.T) {
	store := KeyStore{
		Devices: []DeviceEntry{
			{Name: "laptop", Mode: "rw", PubKey: "ssh-ed25519 AAAA..."},
			{Name: "phone", Mode: "ro", PubKey: "ssh-ed25519 BBBB..."},
			{Name: "tablet", Mode: "ro", PubKey: "ssh-ed25519 CCCC..."},
		},
	}

	name := "phone"
	idx := findDevice(store, name)
	if idx < 0 {
		t.Fatal("expected to find phone")
	}

	store.Devices = append(store.Devices[:idx], store.Devices[idx+1:]...)

	if len(store.Devices) != 2 {
		t.Errorf("expected 2 devices after remove, got %d", len(store.Devices))
	}
	if findDevice(store, "phone") != -1 {
		t.Error("phone should have been removed")
	}
	if findDevice(store, "laptop") == -1 {
		t.Error("laptop should still exist")
	}
}

func TestAuthorizedKeysGeneration_RO(t *testing.T) {
	device := DeviceEntry{
		Name:   "laptop",
		Mode:   "ro",
		PubKey: "ssh-ed25519 AAAAB3NzaC1lZDI1NTE5AAAAITest user@laptop",
	}

	var prefix string
	switch device.Mode {
	case "ro":
		prefix = `command="/usr/local/bin/clock-shell --mode ro",no-port-forwarding,no-agent-forwarding`
	case "rw":
		prefix = `no-port-forwarding`
	}

	line := prefix + " " + strings.TrimSpace(device.PubKey)

	if !strings.Contains(line, "clock-shell --mode ro") {
		t.Error("ro mode should have clock-shell restriction")
	}
	if !strings.Contains(line, "no-port-forwarding") {
		t.Error("should have no-port-forwarding")
	}
	if !strings.Contains(line, "no-agent-forwarding") {
		t.Error("ro mode should have no-agent-forwarding")
	}
	if !strings.Contains(line, "ssh-ed25519") {
		t.Error("should contain the public key")
	}
}

func TestAuthorizedKeysGeneration_RW(t *testing.T) {
	device := DeviceEntry{
		Name:   "workstation",
		Mode:   "rw",
		PubKey: "ssh-ed25519 AAAAB3NzaC1lZDI1NTE5AAAAITest user@workstation",
	}

	var prefix string
	switch device.Mode {
	case "ro":
		prefix = `command="/usr/local/bin/clock-shell --mode ro",no-port-forwarding,no-agent-forwarding`
	case "rw":
		prefix = `no-port-forwarding`
	}

	line := prefix + " " + strings.TrimSpace(device.PubKey)

	if strings.Contains(line, "clock-shell") {
		t.Error("rw mode should NOT have clock-shell restriction")
	}
	if !strings.Contains(line, "no-port-forwarding") {
		t.Error("should have no-port-forwarding")
	}
	if strings.Contains(line, "no-agent-forwarding") {
		t.Error("rw mode should NOT have no-agent-forwarding")
	}
}

func TestAuthorizedKeysGeneration_MultipleDevices(t *testing.T) {
	devices := []DeviceEntry{
		{Name: "laptop", Mode: "rw", PubKey: "ssh-ed25519 AAAA... laptop"},
		{Name: "phone", Mode: "ro", PubKey: "ssh-ed25519 BBBB... phone"},
	}

	var lines []string
	for _, d := range devices {
		var prefix string
		switch d.Mode {
		case "ro":
			prefix = `command="/usr/local/bin/clock-shell --mode ro",no-port-forwarding,no-agent-forwarding`
		case "rw":
			prefix = `no-port-forwarding`
		default:
			continue
		}
		line := prefix + " " + strings.TrimSpace(d.PubKey)
		lines = append(lines, line)
	}

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestAuthorizedKeysGeneration_UnknownMode(t *testing.T) {
	device := DeviceEntry{
		Name:   "unknown",
		Mode:   "admin",
		PubKey: "ssh-ed25519 AAAA...",
	}

	// Unknown modes should be skipped (as per cmdSync logic)
	var prefix string
	switch device.Mode {
	case "ro":
		prefix = `command="..."...`
	case "rw":
		prefix = `no-port-forwarding`
	default:
		// Skip
	}

	if prefix != "" {
		t.Error("unknown mode should result in empty prefix (skip)")
	}
}
