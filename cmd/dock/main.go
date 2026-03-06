// Command dock is the Clock daemon supervisor.
// It manages worker goroutines that pull jobs from the queue, a watcher that
// polls for git changes, and a scheduler for cron-like entries.
// Input: JSON config from stdin or --config flag.
// Output: JSON events on stdout describing activity.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Config is the dock daemon configuration.
type Config struct {
	Root          string `json:"root"`
	Workers       int    `json:"workers"`
	Queue         string `json:"queue"`
	WatchInterval int    `json:"watch_interval"` // seconds
}

// DockEvent is a JSON event emitted on stdout.
type DockEvent struct {
	TS    int64       `json:"ts"`
	Type  string      `json:"type"`
	Data  interface{} `json:"data,omitempty"`
}

// ScheduleEntry is a cron-like schedule item.
type ScheduleEntry struct {
	Name     string          `json:"name"`
	Interval int             `json:"interval"` // seconds
	Job      common.JobSpec  `json:"job"`
	LastRun  int64           `json:"last_run,omitempty"`
}

func main() {
	var cfg Config

	// Try reading config from stdin
	if err := jsonutil.ReadInput(&cfg); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read config: %v", err))
	}

	// Apply defaults
	if cfg.Root == "" {
		cfg.Root = "."
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Queue == "" {
		cfg.Queue = ".clock/queue"
	}
	if cfg.WatchInterval <= 0 {
		cfg.WatchInterval = 60
	}

	// Ensure queue directories exist
	for _, sub := range []string{"pending", "leased", "done", "failed", "dead"} {
		dir := filepath.Join(cfg.Queue, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", dir, err))
		}
	}

	// Write PID file
	pidPath := filepath.Join(".clock", "dock.pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir .clock: %v", err))
	}
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write pid file: %v", err))
	}
	defer os.Remove(pidPath)

	emitEvent("dock.start", map[string]interface{}{
		"pid":            pid,
		"workers":        cfg.Workers,
		"queue":          cfg.Queue,
		"watch_interval": cfg.WatchInterval,
	})

	// Set up context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGTERM and SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runWorker(ctx, workerID, cfg)
		}(i)
	}

	// Start watcher goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWatcher(ctx, cfg)
	}()

	// Start scheduler goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		runScheduler(ctx, cfg)
	}()

	// Wait for shutdown signal
	select {
	case sig := <-sigCh:
		emitEvent("dock.shutdown", map[string]interface{}{
			"signal": sig.String(),
		})
		cancel()
	case <-ctx.Done():
	}

	// Wait for all goroutines to finish
	wg.Wait()

	emitEvent("dock.stopped", map[string]interface{}{
		"pid": pid,
	})
}

// runWorker is a worker goroutine that loops: q take -> work -> q ack/fail.
func runWorker(ctx context.Context, id int, cfg Config) {
	emitEvent("worker.start", map[string]interface{}{
		"worker_id": id,
	})

	for {
		select {
		case <-ctx.Done():
			emitEvent("worker.stop", map[string]interface{}{
				"worker_id": id,
			})
			return
		default:
		}

		// Try to take a job from the queue
		entry, err := qTake(cfg.Queue)
		if err != nil {
			// No jobs available or error - back off
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		emitEvent("worker.job.start", map[string]interface{}{
			"worker_id": id,
			"job_id":    entry.ID,
			"goal":      entry.Job.Goal,
		})

		// Run the work tool with the job
		result, err := runWork(entry.Job)
		if err != nil {
			emitEvent("worker.job.fail", map[string]interface{}{
				"worker_id": id,
				"job_id":    entry.ID,
				"error":     err.Error(),
			})

			// Mark job as failed
			if failErr := qFail(cfg.Queue, entry.ID); failErr != nil {
				emitEvent("worker.q.fail.error", map[string]interface{}{
					"worker_id": id,
					"job_id":    entry.ID,
					"error":     failErr.Error(),
				})
			}
			continue
		}

		if result.OK {
			emitEvent("worker.job.done", map[string]interface{}{
				"worker_id": id,
				"job_id":    entry.ID,
				"ok":        true,
			})

			// Acknowledge the job
			if ackErr := qAck(cfg.Queue, entry.ID); ackErr != nil {
				emitEvent("worker.q.ack.error", map[string]interface{}{
					"worker_id": id,
					"job_id":    entry.ID,
					"error":     ackErr.Error(),
				})
			}
		} else {
			emitEvent("worker.job.fail", map[string]interface{}{
				"worker_id": id,
				"job_id":    entry.ID,
				"error":     result.Err,
			})

			if failErr := qFail(cfg.Queue, entry.ID); failErr != nil {
				emitEvent("worker.q.fail.error", map[string]interface{}{
					"worker_id": id,
					"job_id":    entry.ID,
					"error":     failErr.Error(),
				})
			}
		}
	}
}

// runWatcher polls git status periodically and enqueues jobs for changes.
func runWatcher(ctx context.Context, cfg Config) {
	emitEvent("watcher.start", map[string]interface{}{
		"interval": cfg.WatchInterval,
	})

	ticker := time.NewTicker(time.Duration(cfg.WatchInterval) * time.Second)
	defer ticker.Stop()

	var lastStatus string

	for {
		select {
		case <-ctx.Done():
			emitEvent("watcher.stop", nil)
			return
		case <-ticker.C:
			status, err := gitStatus(cfg.Root)
			if err != nil {
				emitEvent("watcher.error", map[string]interface{}{
					"error": err.Error(),
				})
				continue
			}

			if status != lastStatus && status != "" {
				emitEvent("watcher.change", map[string]interface{}{
					"status": truncate(status, 500),
				})
				lastStatus = status
			}
		}
	}
}

// runScheduler checks .clock/schedules.json for cron-like entries.
func runScheduler(ctx context.Context, cfg Config) {
	emitEvent("scheduler.start", nil)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			emitEvent("scheduler.stop", nil)
			return
		case <-ticker.C:
			schedPath := filepath.Join(".clock", "schedules.json")
			data, err := os.ReadFile(schedPath)
			if err != nil {
				// No schedule file is normal
				continue
			}

			var schedules []ScheduleEntry
			if err := json.Unmarshal(data, &schedules); err != nil {
				emitEvent("scheduler.error", map[string]interface{}{
					"error": fmt.Sprintf("parse schedules.json: %v", err),
				})
				continue
			}

			now := time.Now().Unix()
			updated := false

			for i := range schedules {
				s := &schedules[i]
				if s.Interval <= 0 {
					continue
				}
				if now-s.LastRun >= int64(s.Interval) {
					// Time to run this schedule
					emitEvent("scheduler.trigger", map[string]interface{}{
						"name": s.Name,
						"job":  s.Job,
					})

					// Enqueue the job
					if err := qPut(cfg.Queue, s.Job); err != nil {
						emitEvent("scheduler.enqueue.error", map[string]interface{}{
							"name":  s.Name,
							"error": err.Error(),
						})
					} else {
						s.LastRun = now
						updated = true
					}
				}
			}

			// Write back updated schedule if any LastRun changed
			if updated {
				newData, err := json.MarshalIndent(schedules, "", "  ")
				if err == nil {
					os.WriteFile(schedPath, newData, 0o644)
				}
			}
		}
	}
}

// qTake invokes `q take` and returns the leased queue entry.
func qTake(queueDir string) (common.QueueEntry, error) {
	var entry common.QueueEntry

	toolPath := findTool("q")
	cmd := exec.Command(toolPath, "take")
	cmd.Env = append(os.Environ(), "CLOCK_QUEUE_DIR="+queueDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return entry, fmt.Errorf("q take: %w (stderr: %s)", err, stderr.String())
	}

	// Check for "no pending jobs" response
	var raw map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err == nil {
		if ok, exists := raw["ok"]; exists {
			if okBool, isBool := ok.(bool); isBool && !okBool {
				return entry, fmt.Errorf("no pending jobs")
			}
		}
	}

	if err := json.Unmarshal(stdout.Bytes(), &entry); err != nil {
		return entry, fmt.Errorf("parse q take output: %w", err)
	}

	if entry.ID == "" {
		return entry, fmt.Errorf("no pending jobs")
	}

	return entry, nil
}

// qAck invokes `q ack <id>`.
func qAck(queueDir, id string) error {
	toolPath := findTool("q")
	cmd := exec.Command(toolPath, "ack", id)
	cmd.Env = append(os.Environ(), "CLOCK_QUEUE_DIR="+queueDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("q ack %s: %w (stderr: %s)", id, err, stderr.String())
	}
	return nil
}

// qFail invokes `q fail <id>`.
func qFail(queueDir, id string) error {
	toolPath := findTool("q")
	cmd := exec.Command(toolPath, "fail", id)
	cmd.Env = append(os.Environ(), "CLOCK_QUEUE_DIR="+queueDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("q fail %s: %w (stderr: %s)", id, err, stderr.String())
	}
	return nil
}

// qPut invokes `q put` with a job spec.
func qPut(queueDir string, job common.JobSpec) error {
	input, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	toolPath := findTool("q")
	cmd := exec.Command(toolPath, "put")
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "CLOCK_QUEUE_DIR="+queueDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("q put: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// runWork invokes the `work` tool with a job spec and returns the result.
func runWork(job common.JobSpec) (common.JobResult, error) {
	var result common.JobResult

	input, err := json.Marshal(job)
	if err != nil {
		return result, fmt.Errorf("marshal job: %w", err)
	}

	toolPath := findTool("work")
	cmd := exec.Command(toolPath)
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("work: %w (stderr: %s)", err, truncate(stderr.String(), 500))
	}

	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return result, fmt.Errorf("parse work output: %w", err)
	}

	return result, nil
}

// gitStatus returns the output of `git status --porcelain`.
func gitStatus(root string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// findTool locates a tool binary by name.
func findTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	binPath := filepath.Join("bin", name)
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	if p, err := exec.LookPath("clock-" + name); err == nil {
		return p
	}
	return name
}

// emitEvent writes a DockEvent as JSON to stdout (thread-safe via mutex).
var emitMu sync.Mutex

func emitEvent(eventType string, data interface{}) {
	ev := DockEvent{
		TS:   time.Now().UnixMilli(),
		Type: eventType,
		Data: data,
	}

	emitMu.Lock()
	defer emitMu.Unlock()

	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(ev)
}

// truncate limits a string to maxLen bytes.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
