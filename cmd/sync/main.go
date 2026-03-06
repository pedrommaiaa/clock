// Command sync is a state synchronizer for distributed Clock state.
// It exports/imports state bundles and resolves conflicts via last-writer-wins.
// Subcommands: export, import, diff, status.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Directories to sync.
var syncDirs = []string{
	".clock/mem",
	".clock/knox",
	".clock/playbooks",
	".clock/shrd",
}

const conflictLog = ".clock/sync_conflicts.jsonl"

// SyncEntry represents a single file in a sync bundle.
type SyncEntry struct {
	Path      string `json:"path"`       // relative path within .clock/
	Content   string `json:"content"`    // base64 or raw JSON content
	Hash      string `json:"hash"`       // SHA256 of content
	ModTime   int64  `json:"mod_time"`   // Unix timestamp (seconds)
	Size      int64  `json:"size"`       // file size in bytes
}

// ConflictEntry logs a sync conflict.
type ConflictEntry struct {
	Path      string `json:"path"`
	LocalHash string `json:"local_hash"`
	RemoteHash string `json:"remote_hash"`
	LocalMod  int64  `json:"local_mod"`
	RemoteMod int64  `json:"remote_mod"`
	Winner    string `json:"winner"` // "local" or "remote"
	Timestamp string `json:"timestamp"`
}

// ImportResult is the output of the import subcommand.
type ImportResult struct {
	Imported  int `json:"imported"`
	Conflicts int `json:"conflicts"`
	Skipped   int `json:"skipped"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: sync <export|import|diff|status> [args]")
	}

	cmd := os.Args[1]
	switch cmd {
	case "export":
		doExport()
	case "import":
		doImport()
	case "diff":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: sync diff <bundle-path>")
		}
		doDiff(os.Args[2])
	case "status":
		doSyncStatus()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use export, import, diff, status", cmd))
	}
}

// hashContent computes SHA256 of content bytes.
func hashContent(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// collectFiles walks sync directories and collects all files.
func collectFiles() ([]SyncEntry, error) {
	var entries []SyncEntry

	for _, dir := range syncDirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue // directory doesn't exist, skip
		}

		err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if fi.IsDir() {
				return nil
			}
			// Skip temp files and lock files
			if strings.HasSuffix(path, ".tmp") || strings.HasSuffix(path, ".lock") || fi.Name() == ".lock" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil // skip unreadable files
			}

			entries = append(entries, SyncEntry{
				Path:    path,
				Content: string(data),
				Hash:    hashContent(data),
				ModTime: fi.ModTime().Unix(),
				Size:    fi.Size(),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	// Sort by path for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

// doExport exports local state as a JSONL sync bundle to stdout.
func doExport() {
	entries, err := collectFiles()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("collect files: %v", err))
	}

	enc := json.NewEncoder(os.Stdout)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doImport reads a sync bundle from stdin and merges into local state.
func doImport() {
	// Read all remote entries from stdin
	var remoteEntries []SyncEntry
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry SyncEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("parse sync entry: %v", err))
		}
		remoteEntries = append(remoteEntries, entry)
	}
	if err := scanner.Err(); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read stdin: %v", err))
	}

	result := ImportResult{}
	now := time.Now().UTC().Format(time.RFC3339)

	for _, remote := range remoteEntries {
		localInfo, err := os.Stat(remote.Path)

		if err != nil {
			// Local file doesn't exist -- import it
			dir := filepath.Dir(remote.Path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", dir, err))
			}
			if err := os.WriteFile(remote.Path, []byte(remote.Content), 0o644); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write %s: %v", remote.Path, err))
			}
			result.Imported++
			continue
		}

		// Local file exists -- compare
		localData, err := os.ReadFile(remote.Path)
		if err != nil {
			jsonutil.Fatal(fmt.Sprintf("read local %s: %v", remote.Path, err))
		}
		localHash := hashContent(localData)

		if localHash == remote.Hash {
			// Content identical, skip
			result.Skipped++
			continue
		}

		// Content differs -- check timestamps
		localMod := localInfo.ModTime().Unix()

		if localMod > remote.ModTime {
			// Local is newer, keep local
			result.Skipped++
			// Log conflict
			logConflict(ConflictEntry{
				Path:       remote.Path,
				LocalHash:  localHash,
				RemoteHash: remote.Hash,
				LocalMod:   localMod,
				RemoteMod:  remote.ModTime,
				Winner:     "local",
				Timestamp:  now,
			})
			result.Conflicts++
			continue
		}

		if remote.ModTime > localMod {
			// Remote is newer, overwrite local
			if err := os.WriteFile(remote.Path, []byte(remote.Content), 0o644); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write %s: %v", remote.Path, err))
			}
			result.Imported++
			// Log conflict
			logConflict(ConflictEntry{
				Path:       remote.Path,
				LocalHash:  localHash,
				RemoteHash: remote.Hash,
				LocalMod:   localMod,
				RemoteMod:  remote.ModTime,
				Winner:     "remote",
				Timestamp:  now,
			})
			result.Conflicts++
			continue
		}

		// Same timestamp but different content -- remote wins (last-writer-wins)
		if err := os.WriteFile(remote.Path, []byte(remote.Content), 0o644); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write %s: %v", remote.Path, err))
		}
		result.Imported++
		logConflict(ConflictEntry{
			Path:       remote.Path,
			LocalHash:  localHash,
			RemoteHash: remote.Hash,
			LocalMod:   localMod,
			RemoteMod:  remote.ModTime,
			Winner:     "remote",
			Timestamp:  now,
		})
		result.Conflicts++
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// logConflict appends a conflict entry to the conflict log.
func logConflict(entry ConflictEntry) {
	dir := filepath.Dir(conflictLog)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return // best effort
	}
	f, err := os.OpenFile(conflictLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return // best effort
	}
	defer f.Close()

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
}

// doDiff compares local state against an exported bundle file.
func doDiff(bundlePath string) {
	// Read the bundle file
	f, err := os.Open(bundlePath)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("open bundle: %v", err))
	}
	defer f.Close()

	remoteByPath := make(map[string]SyncEntry)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry SyncEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		remoteByPath[entry.Path] = entry
	}
	if err := scanner.Err(); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read bundle: %v", err))
	}

	// Collect local state
	localEntries, err := collectFiles()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("collect local files: %v", err))
	}
	localByPath := make(map[string]SyncEntry)
	for _, e := range localEntries {
		localByPath[e.Path] = e
	}

	type DiffEntry struct {
		Path       string `json:"path"`
		Status     string `json:"status"` // added, removed, modified, unchanged
		LocalHash  string `json:"local_hash,omitempty"`
		RemoteHash string `json:"remote_hash,omitempty"`
		LocalMod   int64  `json:"local_mod,omitempty"`
		RemoteMod  int64  `json:"remote_mod,omitempty"`
	}

	// Collect all unique paths
	allPaths := make(map[string]bool)
	for p := range localByPath {
		allPaths[p] = true
	}
	for p := range remoteByPath {
		allPaths[p] = true
	}

	// Sort paths for deterministic output
	sortedPaths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	enc := json.NewEncoder(os.Stdout)
	for _, p := range sortedPaths {
		local, hasLocal := localByPath[p]
		remote, hasRemote := remoteByPath[p]

		var diff DiffEntry
		diff.Path = p

		switch {
		case hasLocal && !hasRemote:
			diff.Status = "added"
			diff.LocalHash = local.Hash
			diff.LocalMod = local.ModTime
		case !hasLocal && hasRemote:
			diff.Status = "removed"
			diff.RemoteHash = remote.Hash
			diff.RemoteMod = remote.ModTime
		case local.Hash == remote.Hash:
			diff.Status = "unchanged"
			diff.LocalHash = local.Hash
			diff.RemoteHash = remote.Hash
		default:
			diff.Status = "modified"
			diff.LocalHash = local.Hash
			diff.RemoteHash = remote.Hash
			diff.LocalMod = local.ModTime
			diff.RemoteMod = remote.ModTime
		}

		if err := enc.Encode(diff); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doSyncStatus outputs a fingerprint of the current state.
func doSyncStatus() {
	entries, err := collectFiles()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("collect files: %v", err))
	}

	// Build a composite hash from all file paths + mod times + hashes
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s:%d:%s\n", e.Path, e.ModTime, e.Hash)
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))

	result := map[string]interface{}{
		"fingerprint": fingerprint,
		"files":       len(entries),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// Ensure unused imports don't cause build errors.
var _ = io.ReadAll
