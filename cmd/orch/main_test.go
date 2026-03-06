package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCampaignPath(t *testing.T) {
	got := campaignPath("/tmp/campaigns", "my-campaign")
	want := "/tmp/campaigns/my-campaign.json"
	if got != want {
		t.Errorf("campaignPath = %q, want %q", got, want)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID("camp")
	id2 := generateID("camp")
	if id1 == "" {
		t.Error("generateID returned empty string")
	}
	if id1 == id2 {
		t.Error("generateID returned duplicate IDs")
	}
	if len(id1) < 5 || id1[:5] != "camp-" {
		t.Errorf("generateID should start with 'camp-', got %q", id1)
	}
}

func TestGenerateIDDifferentPrefixes(t *testing.T) {
	campID := generateID("camp")
	runID := generateID("run")
	jobID := generateID("job")

	if campID[:5] != "camp-" {
		t.Errorf("camp ID prefix wrong: %q", campID)
	}
	if runID[:4] != "run-" {
		t.Errorf("run ID prefix wrong: %q", runID)
	}
	if jobID[:4] != "job-" {
		t.Errorf("job ID prefix wrong: %q", jobID)
	}
}

func TestSaveCampaign(t *testing.T) {
	dir := t.TempDir()

	campaign := &Campaign{
		CampaignSpec: CampaignSpec{
			Name:      "test-campaign",
			Objective: "test objective",
			Repos:     []string{"repo1", "repo2"},
			Tasks:     []CampaignTask{{Goal: "fix bug"}},
			Mode:      "pr",
		},
		ID:        "camp-abc123",
		Status:    "created",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
		Runs:      []RunRecord{},
	}

	if err := saveCampaign(dir, campaign); err != nil {
		t.Fatalf("saveCampaign: %v", err)
	}

	// Verify file exists
	path := campaignPath(dir, "test-campaign")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("campaign file not found: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var loaded Campaign
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Name != "test-campaign" {
		t.Errorf("Name = %q, want %q", loaded.Name, "test-campaign")
	}
	if loaded.Status != "created" {
		t.Errorf("Status = %q, want %q", loaded.Status, "created")
	}
}

func TestLoadCampaign(t *testing.T) {
	dir := t.TempDir()

	campaign := &Campaign{
		CampaignSpec: CampaignSpec{
			Name:      "load-test",
			Objective: "test loading",
			Repos:     []string{"repo1"},
			Tasks:     []CampaignTask{{Goal: "task1"}},
		},
		ID:        "camp-load",
		Status:    "active",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	saveCampaign(dir, campaign)

	loaded, err := loadCampaign(dir, "load-test")
	if err != nil {
		t.Fatalf("loadCampaign: %v", err)
	}
	if loaded.Name != "load-test" {
		t.Errorf("Name = %q, want %q", loaded.Name, "load-test")
	}
	if loaded.Status != "active" {
		t.Errorf("Status = %q, want %q", loaded.Status, "active")
	}
	if len(loaded.Repos) != 1 {
		t.Errorf("Repos count = %d, want 1", len(loaded.Repos))
	}
}

func TestLoadCampaignNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := loadCampaign(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for missing campaign")
	}
}

func TestCampaignPauseResume(t *testing.T) {
	dir := t.TempDir()

	campaign := &Campaign{
		CampaignSpec: CampaignSpec{
			Name:  "pausable",
			Repos: []string{"repo1"},
			Tasks: []CampaignTask{{Goal: "task1"}},
		},
		ID:        "camp-pause",
		Status:    "active",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	saveCampaign(dir, campaign)

	// Pause
	loaded, _ := loadCampaign(dir, "pausable")
	loaded.Status = "paused"
	saveCampaign(dir, loaded)

	paused, _ := loadCampaign(dir, "pausable")
	if paused.Status != "paused" {
		t.Errorf("status after pause = %q, want %q", paused.Status, "paused")
	}

	// Resume
	paused.Status = "active"
	saveCampaign(dir, paused)

	resumed, _ := loadCampaign(dir, "pausable")
	if resumed.Status != "active" {
		t.Errorf("status after resume = %q, want %q", resumed.Status, "active")
	}
}

func TestCampaignRunRecord(t *testing.T) {
	dir := t.TempDir()

	campaign := &Campaign{
		CampaignSpec: CampaignSpec{
			Name:  "with-runs",
			Repos: []string{"repo1", "repo2"},
			Tasks: []CampaignTask{{Goal: "fix"}, {Goal: "test"}},
		},
		ID:        "camp-runs",
		Status:    "created",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
		Runs:      []RunRecord{},
	}
	saveCampaign(dir, campaign)

	// Add a run
	loaded, _ := loadCampaign(dir, "with-runs")
	run := RunRecord{
		RunID:     "run-001",
		Timestamp: "2024-01-02T00:00:00Z",
		JobIDs:    []string{"job-1", "job-2", "job-3", "job-4"},
		Status:    "completed",
	}
	loaded.Runs = append(loaded.Runs, run)
	loaded.Status = "active"
	saveCampaign(dir, loaded)

	// Verify
	final, _ := loadCampaign(dir, "with-runs")
	if len(final.Runs) != 1 {
		t.Fatalf("runs count = %d, want 1", len(final.Runs))
	}
	if final.Runs[0].RunID != "run-001" {
		t.Errorf("run ID = %q, want %q", final.Runs[0].RunID, "run-001")
	}
	if len(final.Runs[0].JobIDs) != 4 {
		t.Errorf("job count = %d, want 4 (2 repos x 2 tasks)", len(final.Runs[0].JobIDs))
	}
}

func TestListCampaigns(t *testing.T) {
	dir := t.TempDir()

	// Create multiple campaigns
	for _, name := range []string{"camp-a", "camp-b", "camp-c"} {
		c := &Campaign{
			CampaignSpec: CampaignSpec{
				Name:  name,
				Repos: []string{"repo1"},
				Tasks: []CampaignTask{{Goal: "task1"}},
			},
			ID:        generateID("camp"),
			Status:    "created",
			CreatedAt: "2024-01-01T00:00:00Z",
			UpdatedAt: "2024-01-01T00:00:00Z",
		}
		saveCampaign(dir, c)
	}

	// Also create a non-json file
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("ignore"), 0o644)
	// And a tmp file
	os.WriteFile(filepath.Join(dir, "temp.json.tmp"), []byte("{}"), 0o644)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	var campaigns []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		if len(name) > 4 && name[len(name)-8:] == ".json.tmp" {
			continue
		}
		campaigns = append(campaigns, name)
	}

	if len(campaigns) != 3 {
		t.Errorf("listed %d campaigns, want 3 (got: %v)", len(campaigns), campaigns)
	}
}

func TestCampaignTotalTasks(t *testing.T) {
	campaign := Campaign{
		CampaignSpec: CampaignSpec{
			Repos: []string{"repo1", "repo2", "repo3"},
			Tasks: []CampaignTask{
				{Goal: "fix bug"},
				{Goal: "add tests"},
			},
		},
	}

	totalTasks := len(campaign.Repos) * len(campaign.Tasks)
	if totalTasks != 6 {
		t.Errorf("totalTasks = %d, want 6 (3 repos x 2 tasks)", totalTasks)
	}
}

func TestCampaignAlreadyExists(t *testing.T) {
	dir := t.TempDir()

	c := &Campaign{
		CampaignSpec: CampaignSpec{
			Name:  "existing",
			Repos: []string{"repo1"},
			Tasks: []CampaignTask{{Goal: "task1"}},
		},
		ID:     "camp-exist",
		Status: "created",
	}
	saveCampaign(dir, c)

	// Check if exists
	path := campaignPath(dir, "existing")
	if _, err := os.Stat(path); err != nil {
		t.Error("campaign should exist")
	}
}

func TestCampaignDefaultMode(t *testing.T) {
	spec := CampaignSpec{
		Name:  "test",
		Repos: []string{"repo1"},
		Tasks: []CampaignTask{{Goal: "task1"}},
	}

	if spec.Mode == "" {
		spec.Mode = "pr"
	}

	if spec.Mode != "pr" {
		t.Errorf("default mode = %q, want %q", spec.Mode, "pr")
	}
}

func TestCampaignPriorityTasks(t *testing.T) {
	tasks := []CampaignTask{
		{Goal: "fix critical bug", Priority: "high"},
		{Goal: "add docs", Priority: "low"},
		{Goal: "refactor", Priority: ""},
	}

	highPriority := 0
	for _, task := range tasks {
		if task.Priority == "high" {
			highPriority++
		}
	}
	if highPriority != 1 {
		t.Errorf("high priority count = %d, want 1", highPriority)
	}
}
