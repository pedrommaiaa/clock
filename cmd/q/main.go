// Command q is a durable file-spool job queue.
// Jobs are JSON files stored in .clock/queue/{pending,leased,done,failed,dead}/.
// Subcommands: put, take, ack, fail, list, status.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Queue directory layout
const defaultQueueDir = ".clock/queue"

var statusDirs = []string{"pending", "leased", "done", "failed", "dead"}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: q <put|take|ack|fail|list|status> [args]")
	}

	queueDir := os.Getenv("CLOCK_QUEUE_DIR")
	if queueDir == "" {
		queueDir = defaultQueueDir
	}

	// Ensure all subdirectories exist
	for _, sub := range statusDirs {
		if err := os.MkdirAll(filepath.Join(queueDir, sub), 0o755); err != nil {
			jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", sub, err))
		}
	}

	cmd := os.Args[1]
	switch cmd {
	case "put":
		doPut(queueDir)
	case "take":
		doTake(queueDir)
	case "ack":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: q ack <id>")
		}
		doAck(queueDir, os.Args[2])
	case "fail":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: q fail <id>")
		}
		doFail(queueDir, os.Args[2])
	case "list":
		doList(queueDir)
	case "status":
		doStatus(queueDir)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use put, take, ack, fail, list, status", cmd))
	}
}

// generateID creates a timestamp-based UUID: <unix_ms>-<random_hex>
func generateID() string {
	ts := time.Now().UnixMilli()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based uniqueness
		return fmt.Sprintf("%d-%d", ts, time.Now().UnixNano()%100000)
	}
	return fmt.Sprintf("%d-%s", ts, hex.EncodeToString(b))
}

// lockFile acquires an exclusive flock on a lock file within the queue dir.
// Returns the file handle (caller must close it to release the lock).
func lockFile(queueDir string) (*os.File, error) {
	lockPath := filepath.Join(queueDir, ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}

// unlock releases and closes the lock file.
func unlock(f *os.File) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// readEntry reads a QueueEntry from a JSON file.
func readEntry(path string) (common.QueueEntry, error) {
	var entry common.QueueEntry
	data, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal(data, &entry)
	return entry, err
}

// writeEntry writes a QueueEntry to a JSON file atomically.
func writeEntry(path string, entry common.QueueEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	// Write to temp file first, then rename for atomicity
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// doPut reads a JobSpec from stdin and creates a pending job.
func doPut(queueDir string) {
	var job common.JobSpec
	if err := jsonutil.ReadInput(&job); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	id := generateID()
	if job.ID != "" {
		// Allow caller to specify ID, but still use ours for queue entry
		id = job.ID + "-" + id
	}

	entry := common.QueueEntry{
		ID:        id,
		Job:       job,
		Status:    "pending",
		Retries:   0,
		CreatedAt: time.Now().UnixMilli(),
	}

	path := filepath.Join(queueDir, "pending", id+".json")
	if err := writeEntry(path, entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write entry: %v", err))
	}

	result := map[string]string{
		"id":     id,
		"status": "pending",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doTake leases the oldest pending job.
func doTake(queueDir string) {
	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	pendingDir := filepath.Join(queueDir, "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read pending dir: %v", err))
	}

	// Filter .json files and sort by name (timestamp prefix = oldest first)
	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".tmp") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}

	if len(jsonFiles) == 0 {
		// No pending jobs
		result := map[string]interface{}{
			"ok":    false,
			"error": "no pending jobs",
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	sort.Strings(jsonFiles)
	oldest := jsonFiles[0]

	srcPath := filepath.Join(pendingDir, oldest)
	entry, err := readEntry(srcPath)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read entry %s: %v", oldest, err))
	}

	// Update entry
	entry.Status = "leased"
	entry.LeasedAt = time.Now().UnixMilli()

	// Write updated entry to leased dir, then remove from pending
	dstPath := filepath.Join(queueDir, "leased", oldest)
	if err := writeEntry(dstPath, entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write leased entry: %v", err))
	}
	if err := os.Remove(srcPath); err != nil {
		// Try to clean up the leased copy on failure
		os.Remove(dstPath)
		jsonutil.Fatal(fmt.Sprintf("remove pending entry: %v", err))
	}

	if err := jsonutil.WriteOutput(entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doAck moves a leased job to done.
func doAck(queueDir string, id string) {
	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	filename := id + ".json"
	srcPath := filepath.Join(queueDir, "leased", filename)
	entry, err := readEntry(srcPath)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read leased entry %s: %v", id, err))
	}

	entry.Status = "done"
	entry.DoneAt = time.Now().UnixMilli()

	dstPath := filepath.Join(queueDir, "done", filename)
	if err := writeEntry(dstPath, entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write done entry: %v", err))
	}
	if err := os.Remove(srcPath); err != nil {
		os.Remove(dstPath)
		jsonutil.Fatal(fmt.Sprintf("remove leased entry: %v", err))
	}

	result := map[string]interface{}{
		"ok": true,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doFail handles a failed job: retry or move to dead.
func doFail(queueDir string, id string) {
	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	filename := id + ".json"
	srcPath := filepath.Join(queueDir, "leased", filename)
	entry, err := readEntry(srcPath)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read leased entry %s: %v", id, err))
	}

	entry.Retries++

	const maxRetries = 3

	if entry.Retries < maxRetries {
		// Move back to pending with backoff
		entry.Status = "pending"
		entry.LeasedAt = 0

		// Rename file with a backoff-adjusted timestamp prefix so it gets
		// picked up after a delay. We embed the retry count in the entry.
		// For simplicity, we just rename immediately but the entry carries retry info.
		dstPath := filepath.Join(queueDir, "pending", filename)
		if err := writeEntry(dstPath, entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write pending entry: %v", err))
		}
		if err := os.Remove(srcPath); err != nil {
			os.Remove(dstPath)
			jsonutil.Fatal(fmt.Sprintf("remove leased entry: %v", err))
		}

		result := map[string]interface{}{
			"id":      id,
			"status":  "pending",
			"retries": entry.Retries,
			"backoff": backoffSeconds(entry.Retries),
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	} else {
		// Move to dead letter queue
		entry.Status = "dead"
		dstPath := filepath.Join(queueDir, "dead", filename)
		if err := writeEntry(dstPath, entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write dead entry: %v", err))
		}
		if err := os.Remove(srcPath); err != nil {
			os.Remove(dstPath)
			jsonutil.Fatal(fmt.Sprintf("remove leased entry: %v", err))
		}

		result := map[string]interface{}{
			"id":      id,
			"status":  "dead",
			"retries": entry.Retries,
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

// backoffSeconds returns an exponential backoff delay in seconds.
func backoffSeconds(retries int) int {
	// 5s, 25s, 125s
	delay := 5
	for i := 1; i < retries; i++ {
		delay *= 5
	}
	return delay
}

// doList outputs all jobs across all status directories as JSONL.
func doList(queueDir string) {
	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	type listEntry struct {
		entry     common.QueueEntry
		sortKey   string
	}

	var all []listEntry
	for _, status := range statusDirs {
		dir := filepath.Join(queueDir, status)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || strings.HasSuffix(f.Name(), ".tmp") {
				continue
			}
			entry, err := readEntry(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			// Sort key: created_at for ordering
			all = append(all, listEntry{
				entry:   entry,
				sortKey: strconv.FormatInt(entry.CreatedAt, 10),
			})
		}
	}

	// Sort by creation time
	sort.Slice(all, func(i, j int) bool {
		return all[i].sortKey < all[j].sortKey
	})

	enc := json.NewEncoder(os.Stdout)
	for _, item := range all {
		if err := enc.Encode(item.entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doStatus outputs counts per status directory.
func doStatus(queueDir string) {
	lk, err := lockFile(queueDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	counts := make(map[string]int)
	for _, status := range statusDirs {
		dir := filepath.Join(queueDir, status)
		files, err := os.ReadDir(dir)
		if err != nil {
			counts[status] = 0
			continue
		}
		n := 0
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") && !strings.HasSuffix(f.Name(), ".tmp") {
				n++
			}
		}
		counts[status] = n
	}

	if err := jsonutil.WriteOutput(counts); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
