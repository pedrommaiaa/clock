// Command farm is a distributed worker pool manager.
// Workers are registered in .clock/farm/workers.json and can be leased for jobs.
// Subcommands: register, unregister, list, lease, release, health, status.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const defaultFarmDir = ".clock/farm"

// Worker represents a registered worker in the pool.
type Worker struct {
	ID           string `json:"id"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Status       string `json:"status"`        // active, idle, offline, busy
	ToolsVersion string `json:"tools_version"` // version of tools installed
	LastSeen     string `json:"last_seen"`
	Capacity     int    `json:"capacity"`
	LeasedJob    string `json:"leased_job,omitempty"` // job ID if busy
}

// RegisterInput is the stdin input for register.
type RegisterInput struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Capacity     int    `json:"capacity"`
	ToolsVersion string `json:"tools_version"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: farm <register|unregister|list|lease|release|health|status> [args]")
	}

	farmDir := os.Getenv("CLOCK_FARM_DIR")
	if farmDir == "" {
		farmDir = defaultFarmDir
	}

	if err := os.MkdirAll(farmDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", farmDir, err))
	}

	cmd := os.Args[1]
	switch cmd {
	case "register":
		doRegister(farmDir)
	case "unregister":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: farm unregister <id>")
		}
		doUnregister(farmDir, os.Args[2])
	case "list":
		doList(farmDir)
	case "lease":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: farm lease <job_id>")
		}
		doLease(farmDir, os.Args[2])
	case "release":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: farm release <worker_id>")
		}
		doRelease(farmDir, os.Args[2])
	case "health":
		doHealth(farmDir)
	case "status":
		doStatus(farmDir)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use register, unregister, list, lease, release, health, status", cmd))
	}
}

// workersFile returns the path to the workers registry.
func workersFile(farmDir string) string {
	return filepath.Join(farmDir, "workers.json")
}

// lockFile acquires an exclusive flock on a lock file within the farm dir.
func lockFile(farmDir string) (*os.File, error) {
	lockPath := filepath.Join(farmDir, ".lock")
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

// loadWorkers reads the workers registry from disk.
func loadWorkers(farmDir string) ([]Worker, error) {
	path := workersFile(farmDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Worker{}, nil
		}
		return nil, fmt.Errorf("read workers: %w", err)
	}
	if len(data) == 0 {
		return []Worker{}, nil
	}
	var workers []Worker
	if err := json.Unmarshal(data, &workers); err != nil {
		return nil, fmt.Errorf("unmarshal workers: %w", err)
	}
	return workers, nil
}

// saveWorkers writes the workers registry to disk atomically.
func saveWorkers(farmDir string, workers []Worker) error {
	data, err := json.MarshalIndent(workers, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workers: %w", err)
	}
	path := workersFile(farmDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// generateWorkerID creates a short worker ID.
func generateWorkerID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("w-%d", time.Now().UnixNano()%100000)
	}
	return "w-" + hex.EncodeToString(b)
}

// doRegister reads worker info from stdin and adds to the registry.
func doRegister(farmDir string) {
	var input RegisterInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Host == "" {
		jsonutil.Fatal("host is required")
	}
	if input.Port <= 0 {
		jsonutil.Fatal("port must be positive")
	}
	if input.Capacity <= 0 {
		input.Capacity = 1
	}
	if input.ToolsVersion == "" {
		input.ToolsVersion = "1.0"
	}

	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	id := generateWorkerID()
	now := time.Now().UTC().Format(time.RFC3339)

	worker := Worker{
		ID:           id,
		Host:         input.Host,
		Port:         input.Port,
		Status:       "active",
		ToolsVersion: input.ToolsVersion,
		LastSeen:     now,
		Capacity:     input.Capacity,
	}

	workers = append(workers, worker)

	if err := saveWorkers(farmDir, workers); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save workers: %v", err))
	}

	result := map[string]interface{}{
		"id":     id,
		"status": "active",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doUnregister removes a worker from the registry.
func doUnregister(farmDir, id string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	found := false
	filtered := make([]Worker, 0, len(workers))
	for _, w := range workers {
		if w.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, w)
	}

	if !found {
		jsonutil.Fatal(fmt.Sprintf("worker not found: %s", id))
	}

	if err := saveWorkers(farmDir, filtered); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save workers: %v", err))
	}

	result := map[string]interface{}{
		"ok": true,
		"id": id,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doList outputs all workers as JSONL.
func doList(farmDir string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	enc := json.NewEncoder(os.Stdout)
	for _, w := range workers {
		if err := enc.Encode(w); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doLease finds an idle worker and marks it as busy for the given job.
func doLease(farmDir, jobID string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Find an idle or active worker (prefer idle, then active)
	bestIdx := -1
	for i, w := range workers {
		if w.Status == "idle" || w.Status == "active" {
			if bestIdx == -1 || w.Status == "idle" {
				bestIdx = i
				if w.Status == "idle" {
					break // idle is preferred, stop searching
				}
			}
		}
	}

	if bestIdx == -1 {
		result := map[string]interface{}{
			"worker_id": "",
			"reason":    "no idle workers",
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	workers[bestIdx].Status = "busy"
	workers[bestIdx].LeasedJob = jobID
	workers[bestIdx].LastSeen = now

	if err := saveWorkers(farmDir, workers); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save workers: %v", err))
	}

	result := map[string]interface{}{
		"worker_id": workers[bestIdx].ID,
		"host":      workers[bestIdx].Host,
		"port":      workers[bestIdx].Port,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doRelease marks a busy worker as idle again.
func doRelease(farmDir, workerID string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	found := false
	for i, w := range workers {
		if w.ID == workerID {
			found = true
			workers[i].Status = "idle"
			workers[i].LeasedJob = ""
			workers[i].LastSeen = time.Now().UTC().Format(time.RFC3339)
			break
		}
	}

	if !found {
		jsonutil.Fatal(fmt.Sprintf("worker not found: %s", workerID))
	}

	if err := saveWorkers(farmDir, workers); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save workers: %v", err))
	}

	result := map[string]interface{}{
		"ok":        true,
		"worker_id": workerID,
		"status":    "idle",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doHealth checks all workers and marks stale ones as offline.
func doHealth(farmDir string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	now := time.Now().UTC()
	cutoff := now.Add(-5 * time.Minute)

	type HealthEntry struct {
		ID       string `json:"id"`
		Host     string `json:"host"`
		Previous string `json:"previous_status"`
		Current  string `json:"current_status"`
		LastSeen string `json:"last_seen"`
	}

	var report []HealthEntry
	changed := false

	for i, w := range workers {
		prev := w.Status
		lastSeen, err := time.Parse(time.RFC3339, w.LastSeen)
		if err != nil {
			// Can't parse last_seen; mark as offline
			workers[i].Status = "offline"
			changed = true
			report = append(report, HealthEntry{
				ID:       w.ID,
				Host:     w.Host,
				Previous: prev,
				Current:  "offline",
				LastSeen: w.LastSeen,
			})
			continue
		}

		if lastSeen.Before(cutoff) && w.Status != "offline" {
			workers[i].Status = "offline"
			workers[i].LeasedJob = ""
			changed = true
		}

		report = append(report, HealthEntry{
			ID:       w.ID,
			Host:     w.Host,
			Previous: prev,
			Current:  workers[i].Status,
			LastSeen: w.LastSeen,
		})
	}

	if changed {
		if err := saveWorkers(farmDir, workers); err != nil {
			jsonutil.Fatal(fmt.Sprintf("save workers: %v", err))
		}
	}

	enc := json.NewEncoder(os.Stdout)
	for _, entry := range report {
		if err := enc.Encode(entry); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doStatus outputs worker counts by status.
func doStatus(farmDir string) {
	lk, err := lockFile(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer unlock(lk)

	workers, err := loadWorkers(farmDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load workers: %v", err))
	}

	counts := map[string]int{
		"total":   len(workers),
		"active":  0,
		"idle":    0,
		"offline": 0,
		"busy":    0,
	}

	for _, w := range workers {
		switch w.Status {
		case "active":
			counts["active"]++
		case "idle":
			counts["idle"]++
		case "offline":
			counts["offline"]++
		case "busy":
			counts["busy"]++
		}
	}

	if err := jsonutil.WriteOutput(counts); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// Ensure unused imports don't cause build errors.
var _ = strings.Contains
