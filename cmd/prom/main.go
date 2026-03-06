// Command prom is a tool promotion manager that installs tools into the
// registry, lists registered tools, and manages tool versions.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// InstallInput is the input for the install subcommand.
type InstallInput struct {
	Artifact string              `json:"artifact"`
	Manifest common.ToolManifest `json:"manifest"`
}

// InstallResult is the output of the install subcommand.
type InstallResult struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// PinResult is the output of the pin subcommand.
type PinResult struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// UninstallResult is the output of the uninstall subcommand.
type UninstallResult struct {
	Tool   string `json:"tool"`
	Status string `json:"status"`
}

func registryPath() string {
	return filepath.Join(".clock", "registry.json")
}

func toolsDir() string {
	return filepath.Join(".clock", "tools")
}

// readRegistry reads the registry file, returning an empty slice if it doesn't exist.
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

// writeRegistry writes the registry atomically (write to temp, rename).
func writeRegistry(registry []common.ToolManifest) error {
	dir := filepath.Dir(registryPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	data = append(data, '\n')

	tmpFile := registryPath() + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("write temp registry: %w", err)
	}
	if err := os.Rename(tmpFile, registryPath()); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename registry: %w", err)
	}
	return nil
}

// findTool returns the index of a tool by name, or -1 if not found.
func findTool(registry []common.ToolManifest, name string) int {
	for i, m := range registry {
		if m.Name == name {
			return i
		}
	}
	return -1
}

func doInstall() {
	var input InstallInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	manifest := input.Manifest
	if manifest.Name == "" {
		jsonutil.Fatal("manifest name is required")
	}
	if manifest.Version == "" {
		manifest.Version = "1.0"
	}
	if input.Artifact != "" && manifest.SHA256 == "" {
		manifest.SHA256 = input.Artifact
	}

	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	// Create tool directory
	toolDir := filepath.Join(toolsDir(), manifest.Name, manifest.Version)
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("create tool dir: %v", err))
	}

	// Write manifest to tool directory
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal manifest: %v", err))
	}
	if err := os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifestData, 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write manifest: %v", err))
	}

	// Update or add to registry
	idx := findTool(registry, manifest.Name)
	if idx >= 0 {
		registry[idx] = manifest
	} else {
		registry = append(registry, manifest)
	}

	if err := writeRegistry(registry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write registry: %v", err))
	}

	result := InstallResult{
		Tool:    manifest.Name,
		Version: manifest.Version,
		Status:  "installed",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doList() {
	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	for _, m := range registry {
		if err := jsonutil.WriteJSONL(m); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

func doGet(name string) {
	if name == "" {
		jsonutil.Fatal("tool name is required")
	}

	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	idx := findTool(registry, name)
	if idx < 0 {
		jsonutil.Fatal(fmt.Sprintf("tool %q not found in registry", name))
	}

	if err := jsonutil.WriteOutput(registry[idx]); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doPin(name, version string) {
	if name == "" || version == "" {
		jsonutil.Fatal("tool name and version are required")
	}

	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	idx := findTool(registry, name)
	if idx < 0 {
		jsonutil.Fatal(fmt.Sprintf("tool %q not found in registry", name))
	}

	registry[idx].Version = version

	if err := writeRegistry(registry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write registry: %v", err))
	}

	result := PinResult{
		Tool:    name,
		Version: version,
		Status:  "pinned",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doUninstall(name string) {
	if name == "" {
		jsonutil.Fatal("tool name is required")
	}

	registry, err := readRegistry()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read registry: %v", err))
	}

	idx := findTool(registry, name)
	if idx < 0 {
		jsonutil.Fatal(fmt.Sprintf("tool %q not found in registry", name))
	}

	// Remove tool files
	toolDir := filepath.Join(toolsDir(), name)
	os.RemoveAll(toolDir)

	// Remove from registry
	registry = append(registry[:idx], registry[idx+1:]...)

	if err := writeRegistry(registry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write registry: %v", err))
	}

	result := UninstallResult{
		Tool:   name,
		Status: "uninstalled",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		// Default: install from stdin
		doInstall()
		return
	}

	switch args[0] {
	case "install":
		doInstall()
	case "list":
		doList()
	case "get":
		if len(args) < 2 {
			jsonutil.Fatal("usage: prom get <name>")
		}
		doGet(args[1])
	case "pin":
		if len(args) < 3 {
			jsonutil.Fatal("usage: prom pin <name> <version>")
		}
		doPin(args[1], args[2])
	case "uninstall":
		if len(args) < 2 {
			jsonutil.Fatal("usage: prom uninstall <name>")
		}
		doUninstall(args[1])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", args[0]))
	}
}
