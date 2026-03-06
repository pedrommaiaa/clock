// Command orch is a global orchestrator for multi-repo, multi-goal campaigns.
// Campaigns are stored as .clock/campaigns/<name>.json.
// Subcommands: create, run, status, list, history, pause, resume.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const defaultCampaignDir = ".clock/campaigns"

// CampaignTask is a task within a campaign.
type CampaignTask struct {
	Goal     string `json:"goal"`
	Priority string `json:"priority,omitempty"`
}

// CampaignSpec is the input for creating a campaign.
type CampaignSpec struct {
	Name      string         `json:"name"`
	Objective string         `json:"objective"`
	Repos     []string       `json:"repos"`
	Schedule  string         `json:"schedule,omitempty"`
	Tasks     []CampaignTask `json:"tasks"`
	Mode      string         `json:"mode,omitempty"` // pr, commit, etc.
}

// RunRecord records a single campaign execution.
type RunRecord struct {
	RunID     string   `json:"run_id"`
	Timestamp string   `json:"timestamp"`
	JobIDs    []string `json:"job_ids"`
	Status    string   `json:"status"` // completed, partial, failed
}

// Campaign is the full persisted campaign state.
type Campaign struct {
	CampaignSpec
	ID        string      `json:"id"`
	Status    string      `json:"status"` // created, active, paused, completed
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
	Runs      []RunRecord `json:"runs,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: orch <create|run|status|list|history|pause|resume> [args]")
	}

	campaignDir := os.Getenv("CLOCK_CAMPAIGN_DIR")
	if campaignDir == "" {
		campaignDir = defaultCampaignDir
	}

	if err := os.MkdirAll(campaignDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", campaignDir, err))
	}

	cmd := os.Args[1]
	switch cmd {
	case "create":
		doCreate(campaignDir)
	case "run":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: orch run <name>")
		}
		doRun(campaignDir, os.Args[2])
	case "status":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: orch status <name>")
		}
		doStatus(campaignDir, os.Args[2])
	case "list":
		doList(campaignDir)
	case "history":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: orch history <name>")
		}
		doHistory(campaignDir, os.Args[2])
	case "pause":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: orch pause <name>")
		}
		doPause(campaignDir, os.Args[2])
	case "resume":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: orch resume <name>")
		}
		doResume(campaignDir, os.Args[2])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use create, run, status, list, history, pause, resume", cmd))
	}
}

// campaignPath returns the file path for a named campaign.
func campaignPath(campaignDir, name string) string {
	return filepath.Join(campaignDir, name+".json")
}

// loadCampaign reads a campaign from disk.
func loadCampaign(campaignDir, name string) (*Campaign, error) {
	path := campaignPath(campaignDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("campaign not found: %s", name)
		}
		return nil, fmt.Errorf("read campaign: %w", err)
	}
	var c Campaign
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal campaign: %w", err)
	}
	return &c, nil
}

// saveCampaign writes a campaign to disk atomically.
func saveCampaign(campaignDir string, c *Campaign) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal campaign: %w", err)
	}
	path := campaignPath(campaignDir, c.Name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// generateID creates a short random ID.
func generateID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// doCreate reads a campaign spec from stdin and creates it.
func doCreate(campaignDir string) {
	var spec CampaignSpec
	if err := jsonutil.ReadInput(&spec); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if spec.Name == "" {
		jsonutil.Fatal("name is required")
	}
	if len(spec.Repos) == 0 {
		jsonutil.Fatal("at least one repo is required")
	}
	if len(spec.Tasks) == 0 {
		jsonutil.Fatal("at least one task is required")
	}
	if spec.Mode == "" {
		spec.Mode = "pr"
	}

	// Check if campaign already exists
	path := campaignPath(campaignDir, spec.Name)
	if _, err := os.Stat(path); err == nil {
		jsonutil.Fatal(fmt.Sprintf("campaign already exists: %s", spec.Name))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	campaign := Campaign{
		CampaignSpec: spec,
		ID:           generateID("camp"),
		Status:       "created",
		CreatedAt:    now,
		UpdatedAt:    now,
		Runs:         []RunRecord{},
	}

	if err := saveCampaign(campaignDir, &campaign); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save campaign: %v", err))
	}

	result := map[string]interface{}{
		"id":     campaign.ID,
		"status": "created",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doRun executes a campaign by creating jobs for each repo+task combination.
// Jobs are enqueued as JobSpecs that could be piped to `q put`.
func doRun(campaignDir, name string) {
	campaign, err := loadCampaign(campaignDir, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	if campaign.Status == "paused" {
		jsonutil.Fatal(fmt.Sprintf("campaign %q is paused; use 'orch resume' first", name))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	runID := generateID("run")

	enc := json.NewEncoder(os.Stdout)
	var jobIDs []string

	for _, repo := range campaign.Repos {
		for _, task := range campaign.Tasks {
			jobID := generateID("job")

			job := common.JobSpec{
				ID:       jobID,
				Goal:     task.Goal,
				Repo:     repo,
				Priority: task.Priority,
				Mode:     campaign.Mode,
			}

			if err := enc.Encode(job); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write job: %v", err))
			}
			jobIDs = append(jobIDs, jobID)
		}
	}

	// Record the run
	run := RunRecord{
		RunID:     runID,
		Timestamp: now,
		JobIDs:    jobIDs,
		Status:    "completed",
	}
	campaign.Runs = append(campaign.Runs, run)
	campaign.Status = "active"
	campaign.UpdatedAt = now

	if err := saveCampaign(campaignDir, campaign); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save campaign: %v", err))
	}
}

// doStatus checks the status of a campaign.
func doStatus(campaignDir, name string) {
	campaign, err := loadCampaign(campaignDir, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	totalTasks := len(campaign.Repos) * len(campaign.Tasks)

	// Try to count job statuses from the queue
	// We look at the most recent run's job IDs and check the queue
	var completed, failed, pending int

	if len(campaign.Runs) > 0 {
		lastRun := campaign.Runs[len(campaign.Runs)-1]

		// Check queue directories for each job
		queueDir := os.Getenv("CLOCK_QUEUE_DIR")
		if queueDir == "" {
			queueDir = ".clock/queue"
		}

		statusDirs := map[string]string{
			"done":    "done",
			"failed":  "failed",
			"dead":    "dead",
			"pending": "pending",
			"leased":  "leased",
		}

		// Build a set of job IDs from the run
		jobIDSet := make(map[string]bool)
		for _, id := range lastRun.JobIDs {
			jobIDSet[id] = true
		}

		// Scan queue directories
		for qStatus, dir := range statusDirs {
			fullDir := filepath.Join(queueDir, dir)
			files, err := os.ReadDir(fullDir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || strings.HasSuffix(f.Name(), ".tmp") {
					continue
				}
				// Read the queue entry to check if it belongs to this campaign
				data, err := os.ReadFile(filepath.Join(fullDir, f.Name()))
				if err != nil {
					continue
				}
				var entry common.QueueEntry
				if err := json.Unmarshal(data, &entry); err != nil {
					continue
				}
				// Check if this job is from our campaign run
				if jobIDSet[entry.Job.ID] || jobIDSet[entry.ID] {
					switch qStatus {
					case "done":
						completed++
					case "failed", "dead":
						failed++
					case "pending", "leased":
						pending++
					}
				}
			}
		}
	}

	// If we couldn't find any in the queue, report based on task count
	if completed+failed+pending == 0 {
		pending = totalTasks
	}

	result := map[string]interface{}{
		"name":         campaign.Name,
		"status":       campaign.Status,
		"total_tasks":  totalTasks,
		"completed":    completed,
		"failed":       failed,
		"pending":      pending,
		"total_runs":   len(campaign.Runs),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doList lists all campaigns as JSONL.
func doList(campaignDir string) {
	entries, err := os.ReadDir(campaignDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read campaigns dir: %v", err))
	}

	enc := json.NewEncoder(os.Stdout)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(campaignDir, e.Name()))
		if err != nil {
			continue
		}
		var c Campaign
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}

		summary := map[string]interface{}{
			"name":        c.Name,
			"id":          c.ID,
			"status":      c.Status,
			"objective":   c.Objective,
			"repos":       len(c.Repos),
			"tasks":       len(c.Tasks),
			"runs":        len(c.Runs),
			"created_at":  c.CreatedAt,
			"updated_at":  c.UpdatedAt,
		}
		if err := enc.Encode(summary); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doHistory shows past runs of a campaign.
func doHistory(campaignDir, name string) {
	campaign, err := loadCampaign(campaignDir, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	if len(campaign.Runs) == 0 {
		// Output empty array
		fmt.Println("[]")
		return
	}

	enc := json.NewEncoder(os.Stdout)
	for _, run := range campaign.Runs {
		if err := enc.Encode(run); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}

// doPause sets a campaign to paused state.
func doPause(campaignDir, name string) {
	campaign, err := loadCampaign(campaignDir, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	if campaign.Status == "paused" {
		jsonutil.Fatal(fmt.Sprintf("campaign %q is already paused", name))
	}

	campaign.Status = "paused"
	campaign.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := saveCampaign(campaignDir, campaign); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save campaign: %v", err))
	}

	result := map[string]interface{}{
		"ok":     true,
		"name":   name,
		"status": "paused",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doResume sets a paused campaign back to active.
func doResume(campaignDir, name string) {
	campaign, err := loadCampaign(campaignDir, name)
	if err != nil {
		jsonutil.Fatal(err.Error())
	}

	if campaign.Status != "paused" {
		jsonutil.Fatal(fmt.Sprintf("campaign %q is not paused (status: %s)", name, campaign.Status))
	}

	campaign.Status = "active"
	campaign.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := saveCampaign(campaignDir, campaign); err != nil {
		jsonutil.Fatal(fmt.Sprintf("save campaign: %v", err))
	}

	result := map[string]interface{}{
		"ok":     true,
		"name":   name,
		"status": "active",
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
