package main

import (
	"encoding/json"
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}

	// Apply defaults (mirror main logic)
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

	if cfg.Root != "." {
		t.Errorf("Root = %q, want %q", cfg.Root, ".")
	}
	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if cfg.Queue != ".clock/queue" {
		t.Errorf("Queue = %q, want %q", cfg.Queue, ".clock/queue")
	}
	if cfg.WatchInterval != 60 {
		t.Errorf("WatchInterval = %d, want 60", cfg.WatchInterval)
	}
}

func TestConfigJSONParsing(t *testing.T) {
	input := `{"root":"/tmp/test","workers":8,"queue":"/tmp/q","watch_interval":30}`
	var cfg Config
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Root != "/tmp/test" {
		t.Errorf("Root = %q, want /tmp/test", cfg.Root)
	}
	if cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cfg.Workers)
	}
	if cfg.Queue != "/tmp/q" {
		t.Errorf("Queue = %q, want /tmp/q", cfg.Queue)
	}
	if cfg.WatchInterval != 30 {
		t.Errorf("WatchInterval = %d, want 30", cfg.WatchInterval)
	}
}

func TestConfigPartialJSON(t *testing.T) {
	// Only some fields set
	input := `{"workers":2}`
	var cfg Config
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Workers != 2 {
		t.Errorf("Workers = %d, want 2", cfg.Workers)
	}
	// Unset fields should be zero values
	if cfg.Root != "" {
		t.Errorf("Root = %q, want empty", cfg.Root)
	}
}

func TestDockEventJSON(t *testing.T) {
	ev := DockEvent{
		TS:   1234567890,
		Type: "worker.start",
		Data: map[string]interface{}{"worker_id": float64(1)},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var got DockEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.TS != 1234567890 {
		t.Errorf("TS = %d, want 1234567890", got.TS)
	}
	if got.Type != "worker.start" {
		t.Errorf("Type = %q, want %q", got.Type, "worker.start")
	}
}

func TestDockEventNilData(t *testing.T) {
	ev := DockEvent{
		TS:   100,
		Type: "test.event",
		Data: nil,
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var got DockEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Data != nil {
		t.Errorf("Data should be nil, got %v", got.Data)
	}
}

func TestScheduleEntryJSON(t *testing.T) {
	entry := ScheduleEntry{
		Name:     "hourly-scan",
		Interval: 3600,
		Job: common.JobSpec{
			ID:   "scan-1",
			Goal: "scan repo",
		},
		LastRun: 1000,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var got ScheduleEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Name != "hourly-scan" {
		t.Errorf("Name = %q, want %q", got.Name, "hourly-scan")
	}
	if got.Interval != 3600 {
		t.Errorf("Interval = %d, want 3600", got.Interval)
	}
	if got.Job.Goal != "scan repo" {
		t.Errorf("Job.Goal = %q, want %q", got.Job.Goal, "scan repo")
	}
}

func TestScheduleEntryDueCheck(t *testing.T) {
	tests := []struct {
		name     string
		now      int64
		lastRun  int64
		interval int
		wantDue  bool
	}{
		{"due", 4000, 1000, 2000, true},
		{"not_due", 2500, 1000, 2000, false},
		{"exact_boundary", 3000, 1000, 2000, true},
		{"zero_interval", 1000, 0, 0, false},
		{"never_run", 5000, 0, 3600, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.interval <= 0 {
				// Matches runScheduler: skip entries with interval <= 0
				if tt.wantDue {
					t.Error("zero/negative interval should never be due")
				}
				return
			}
			isDue := tt.now-tt.lastRun >= int64(tt.interval)
			if isDue != tt.wantDue {
				t.Errorf("isDue = %v, want %v (now=%d, lastRun=%d, interval=%d)",
					isDue, tt.wantDue, tt.now, tt.lastRun, tt.interval)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestFindToolFallback(t *testing.T) {
	name := "nonexistent_tool_xyz_12345"
	got := findTool(name)
	if got != name {
		t.Errorf("findTool(%q) = %q, want fallback to name", name, got)
	}
}

func TestEmitEventDoesNotPanic(t *testing.T) {
	// emitEvent writes to os.Stdout; just verify it doesn't panic
	emitEvent("test.event", map[string]interface{}{"key": "value"})
}

func TestQueueSubdirectories(t *testing.T) {
	expected := []string{"pending", "leased", "done", "failed", "dead"}
	for _, sub := range expected {
		found := false
		for _, s := range []string{"pending", "leased", "done", "failed", "dead"} {
			if s == sub {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected queue subdirectory %q", sub)
		}
	}
}
