// Command shrd is a content-addressed shared artifact store.
// Artifacts are stored as .clock/shrd/<sha256-prefix-2>/<sha256>.json.
// Subcommands: put, get, has, list, gc, stats.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const defaultStoreDir = ".clock/shrd"

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: shrd <put|get|has|list|gc|stats> [args]")
	}

	storeDir := os.Getenv("CLOCK_SHRD_DIR")
	if storeDir == "" {
		storeDir = defaultStoreDir
	}

	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", storeDir, err))
	}

	cmd := os.Args[1]
	switch cmd {
	case "put":
		doPut(storeDir)
	case "get":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: shrd get <ref>")
		}
		doGet(storeDir, os.Args[2])
	case "has":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: shrd has <ref>")
		}
		doHas(storeDir, os.Args[2])
	case "list":
		doList(storeDir)
	case "gc":
		doGC(storeDir)
	case "stats":
		doStats(storeDir)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use put, get, has, list, gc, stats", cmd))
	}
}

// parseRef extracts the hex hash from a ref string like "sha256:<hex>".
func parseRef(ref string) (string, error) {
	if strings.HasPrefix(ref, "sha256:") {
		return strings.TrimPrefix(ref, "sha256:"), nil
	}
	// Allow bare hex hash
	if len(ref) == 64 {
		return ref, nil
	}
	return "", fmt.Errorf("invalid ref %q: expected sha256:<hex> or 64-char hex", ref)
}

// artifactPath returns the filesystem path for a given hash.
func artifactPath(storeDir, hash string) string {
	prefix := hash[:2]
	return filepath.Join(storeDir, prefix, hash+".json")
}

// doPut reads artifact JSON from stdin, computes SHA256, stores the file.
func doPut(storeDir string) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read stdin: %v", err))
	}
	if len(data) == 0 {
		jsonutil.Fatal("empty input")
	}

	// Validate that input is valid JSON
	if !json.Valid(data) {
		jsonutil.Fatal("input is not valid JSON")
	}

	// Compute SHA256 of the content
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	// Ensure prefix directory exists
	prefix := hash[:2]
	prefixDir := filepath.Join(storeDir, prefix)
	if err := os.MkdirAll(prefixDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", prefixDir, err))
	}

	// Write file atomically
	path := artifactPath(storeDir, hash)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write tmp: %v", err))
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		jsonutil.Fatal(fmt.Sprintf("rename: %v", err))
	}

	result := map[string]interface{}{
		"ref":  "sha256:" + hash,
		"size": len(data),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doGet retrieves an artifact by ref and outputs content to stdout.
func doGet(storeDir, ref string) {
	hash, err := parseRef(ref)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	path := artifactPath(storeDir, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			jsonutil.Fatal(fmt.Sprintf("artifact not found: %s", ref))
		}
		jsonutil.Fatal(fmt.Sprintf("read artifact: %v", err))
	}

	// Write raw content to stdout
	if _, err := os.Stdout.Write(data); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write stdout: %v", err))
	}
}

// doHas checks if an artifact exists.
func doHas(storeDir, ref string) {
	hash, err := parseRef(ref)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	path := artifactPath(storeDir, hash)
	_, statErr := os.Stat(path)
	exists := statErr == nil

	result := map[string]interface{}{
		"exists": exists,
		"ref":    "sha256:" + hash,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doList lists all artifacts as JSONL.
func doList(storeDir string) {
	prefixDirs, err := os.ReadDir(storeDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read store dir: %v", err))
	}

	enc := json.NewEncoder(os.Stdout)
	for _, pd := range prefixDirs {
		if !pd.IsDir() || len(pd.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(storeDir, pd.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || strings.HasSuffix(f.Name(), ".tmp") {
				continue
			}
			hash := strings.TrimSuffix(f.Name(), ".json")
			info, err := f.Info()
			if err != nil {
				continue
			}
			entry := map[string]interface{}{
				"ref":     "sha256:" + hash,
				"size":    info.Size(),
				"created": info.ModTime().UTC().Format(time.RFC3339),
			}
			if err := enc.Encode(entry); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
			}
		}
	}
}

// doGC removes artifacts older than 7 days.
func doGC(storeDir string) {
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	var removed int
	var freedBytes int64

	prefixDirs, err := os.ReadDir(storeDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read store dir: %v", err))
	}

	for _, pd := range prefixDirs {
		if !pd.IsDir() || len(pd.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(storeDir, pd.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || strings.HasSuffix(f.Name(), ".tmp") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				path := filepath.Join(subDir, f.Name())
				freedBytes += info.Size()
				if err := os.Remove(path); err == nil {
					removed++
				}
			}
		}
		// Remove empty prefix directory
		remaining, _ := os.ReadDir(subDir)
		if len(remaining) == 0 {
			os.Remove(subDir)
		}
	}

	result := map[string]interface{}{
		"removed":     removed,
		"freed_bytes": freedBytes,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doStats outputs artifact count and total size.
func doStats(storeDir string) {
	var count int
	var totalBytes int64

	prefixDirs, err := os.ReadDir(storeDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read store dir: %v", err))
	}

	for _, pd := range prefixDirs {
		if !pd.IsDir() || len(pd.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(storeDir, pd.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || strings.HasSuffix(f.Name(), ".tmp") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			count++
			totalBytes += info.Size()
		}
	}

	result := map[string]interface{}{
		"count":       count,
		"total_bytes": totalBytes,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
