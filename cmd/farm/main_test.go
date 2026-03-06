package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWorkersFile(t *testing.T) {
	got := workersFile("/tmp/farm")
	want := "/tmp/farm/workers.json"
	if got != want {
		t.Errorf("workersFile = %q, want %q", got, want)
	}
}

func TestGenerateWorkerID(t *testing.T) {
	id1 := generateWorkerID()
	id2 := generateWorkerID()

	if id1 == "" {
		t.Error("generateWorkerID returned empty string")
	}
	if id1 == id2 {
		t.Error("generateWorkerID returned duplicate IDs")
	}
	if len(id1) < 3 || id1[:2] != "w-" {
		t.Errorf("generateWorkerID should start with 'w-', got %q", id1)
	}
}

func TestLoadWorkersEmpty(t *testing.T) {
	dir := t.TempDir()
	workers, err := loadWorkers(dir)
	if err != nil {
		t.Fatalf("loadWorkers: %v", err)
	}
	if len(workers) != 0 {
		t.Errorf("expected empty workers, got %d", len(workers))
	}
}

func TestLoadWorkersEmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(workersFile(dir), []byte(""), 0o644)

	workers, err := loadWorkers(dir)
	if err != nil {
		t.Fatalf("loadWorkers: %v", err)
	}
	if len(workers) != 0 {
		t.Errorf("expected empty workers, got %d", len(workers))
	}
}

func TestSaveAndLoadWorkers(t *testing.T) {
	dir := t.TempDir()

	workers := []Worker{
		{
			ID:           "w-001",
			Host:         "localhost",
			Port:         8080,
			Status:       "active",
			ToolsVersion: "1.0",
			LastSeen:     time.Now().UTC().Format(time.RFC3339),
			Capacity:     2,
		},
		{
			ID:           "w-002",
			Host:         "192.168.1.1",
			Port:         9090,
			Status:       "idle",
			ToolsVersion: "1.1",
			LastSeen:     time.Now().UTC().Format(time.RFC3339),
			Capacity:     4,
		},
	}

	if err := saveWorkers(dir, workers); err != nil {
		t.Fatalf("saveWorkers: %v", err)
	}

	loaded, err := loadWorkers(dir)
	if err != nil {
		t.Fatalf("loadWorkers: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d workers, want 2", len(loaded))
	}
	if loaded[0].ID != "w-001" {
		t.Errorf("worker 0 ID = %q, want %q", loaded[0].ID, "w-001")
	}
	if loaded[1].Host != "192.168.1.1" {
		t.Errorf("worker 1 Host = %q, want %q", loaded[1].Host, "192.168.1.1")
	}
}

func TestLockAndUnlock(t *testing.T) {
	dir := t.TempDir()
	lk, err := lockFile(dir)
	if err != nil {
		t.Fatalf("lockFile: %v", err)
	}
	if lk == nil {
		t.Fatal("lockFile returned nil")
	}
	unlock(lk)
}

func TestLeasePreferIdle(t *testing.T) {
	workers := []Worker{
		{ID: "w-1", Status: "active"},
		{ID: "w-2", Status: "idle"},
		{ID: "w-3", Status: "busy"},
	}

	bestIdx := -1
	for i, w := range workers {
		if w.Status == "idle" || w.Status == "active" {
			if bestIdx == -1 || w.Status == "idle" {
				bestIdx = i
				if w.Status == "idle" {
					break
				}
			}
		}
	}

	if bestIdx != 1 {
		t.Errorf("lease should prefer idle worker (idx 1), got idx %d", bestIdx)
	}
	if workers[bestIdx].ID != "w-2" {
		t.Errorf("leased worker = %q, want %q", workers[bestIdx].ID, "w-2")
	}
}

func TestLeaseNoIdleWorkers(t *testing.T) {
	workers := []Worker{
		{ID: "w-1", Status: "busy"},
		{ID: "w-2", Status: "offline"},
		{ID: "w-3", Status: "busy"},
	}

	bestIdx := -1
	for i, w := range workers {
		if w.Status == "idle" || w.Status == "active" {
			if bestIdx == -1 || w.Status == "idle" {
				bestIdx = i
			}
		}
	}

	if bestIdx != -1 {
		t.Error("should not find any idle/active worker")
	}
}

func TestReleaseWorker(t *testing.T) {
	dir := t.TempDir()
	workers := []Worker{
		{ID: "w-1", Status: "busy", LeasedJob: "job-1"},
		{ID: "w-2", Status: "idle"},
	}
	saveWorkers(dir, workers)

	// Simulate release
	loaded, _ := loadWorkers(dir)
	for i, w := range loaded {
		if w.ID == "w-1" {
			loaded[i].Status = "idle"
			loaded[i].LeasedJob = ""
		}
	}
	saveWorkers(dir, loaded)

	final, _ := loadWorkers(dir)
	for _, w := range final {
		if w.ID == "w-1" {
			if w.Status != "idle" {
				t.Errorf("released worker status = %q, want %q", w.Status, "idle")
			}
			if w.LeasedJob != "" {
				t.Errorf("released worker LeasedJob = %q, want empty", w.LeasedJob)
			}
		}
	}
}

func TestUnregisterWorker(t *testing.T) {
	dir := t.TempDir()
	workers := []Worker{
		{ID: "w-1", Host: "a"},
		{ID: "w-2", Host: "b"},
		{ID: "w-3", Host: "c"},
	}
	saveWorkers(dir, workers)

	// Remove w-2
	loaded, _ := loadWorkers(dir)
	filtered := make([]Worker, 0)
	for _, w := range loaded {
		if w.ID != "w-2" {
			filtered = append(filtered, w)
		}
	}
	saveWorkers(dir, filtered)

	final, _ := loadWorkers(dir)
	if len(final) != 2 {
		t.Fatalf("expected 2 workers after unregister, got %d", len(final))
	}
	for _, w := range final {
		if w.ID == "w-2" {
			t.Error("unregistered worker should not be present")
		}
	}
}

func TestHealthCheckMarksStale(t *testing.T) {
	staleTime := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	freshTime := time.Now().UTC().Format(time.RFC3339)

	workers := []Worker{
		{ID: "w-1", Status: "active", LastSeen: staleTime},
		{ID: "w-2", Status: "active", LastSeen: freshTime},
	}

	cutoff := time.Now().UTC().Add(-5 * time.Minute)
	for i, w := range workers {
		lastSeen, err := time.Parse(time.RFC3339, w.LastSeen)
		if err != nil {
			continue
		}
		if lastSeen.Before(cutoff) && w.Status != "offline" {
			workers[i].Status = "offline"
			workers[i].LeasedJob = ""
		}
	}

	if workers[0].Status != "offline" {
		t.Errorf("stale worker should be marked offline, got %q", workers[0].Status)
	}
	if workers[1].Status != "active" {
		t.Errorf("fresh worker should remain active, got %q", workers[1].Status)
	}
}

func TestStatusCounts(t *testing.T) {
	workers := []Worker{
		{ID: "w-1", Status: "active"},
		{ID: "w-2", Status: "idle"},
		{ID: "w-3", Status: "busy"},
		{ID: "w-4", Status: "offline"},
		{ID: "w-5", Status: "idle"},
	}

	counts := map[string]int{"total": len(workers)}
	for _, w := range workers {
		counts[w.Status]++
	}

	if counts["total"] != 5 {
		t.Errorf("total = %d, want 5", counts["total"])
	}
	if counts["active"] != 1 {
		t.Errorf("active = %d, want 1", counts["active"])
	}
	if counts["idle"] != 2 {
		t.Errorf("idle = %d, want 2", counts["idle"])
	}
	if counts["busy"] != 1 {
		t.Errorf("busy = %d, want 1", counts["busy"])
	}
	if counts["offline"] != 1 {
		t.Errorf("offline = %d, want 1", counts["offline"])
	}
}

func TestWorkerJSON(t *testing.T) {
	w := Worker{
		ID:           "w-abc",
		Host:         "example.com",
		Port:         8080,
		Status:       "active",
		ToolsVersion: "2.0",
		LastSeen:     "2024-01-01T00:00:00Z",
		Capacity:     3,
		LeasedJob:    "job-123",
	}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Worker
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.ID != w.ID || loaded.Host != w.Host || loaded.Port != w.Port {
		t.Errorf("JSON round-trip failed: %+v", loaded)
	}
	if loaded.LeasedJob != "job-123" {
		t.Errorf("LeasedJob = %q, want %q", loaded.LeasedJob, "job-123")
	}
}
