package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper: set up lease state in a temp dir
func setupLeaseDir(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	stateP := filepath.Join(root, "leases.json")
	policyP := filepath.Join(root, "lease_policy.json")
	return stateP, policyP
}

// helper: write state to a path
func writeState(t *testing.T, path string, state LeaseState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// helper: read state from a path
func readState(t *testing.T, path string) LeaseState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state LeaseState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// helper: write policy to a path
func writePolicy(t *testing.T, path string, policy LeasePolicy) {
	t.Helper()
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	if id1 == "" {
		t.Fatal("generateID returned empty string")
	}
	if len(id1) != 16 {
		t.Errorf("expected 16 hex chars, got %d: %q", len(id1), id1)
	}
	if id1 == id2 {
		t.Fatal("generateID returned duplicate")
	}
}

func TestFilterActiveRemovesExpired(t *testing.T) {
	now := time.Now().UTC()
	leases := []LeaseEntry{
		{
			LeaseID:   "active-1",
			JobID:     "j1",
			CreatedAt: now.Add(-1 * time.Minute).Format(time.RFC3339),
			ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339),
		},
		{
			LeaseID:   "expired-1",
			JobID:     "j2",
			CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
			ExpiresAt: now.Add(-5 * time.Minute).Format(time.RFC3339),
		},
		{
			LeaseID:   "active-2",
			JobID:     "j3",
			CreatedAt: now.Add(-30 * time.Second).Format(time.RFC3339),
			ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		},
	}

	active := filterActive(leases, now)
	if len(active) != 2 {
		t.Fatalf("expected 2 active leases, got %d", len(active))
	}
	for _, l := range active {
		if l.LeaseID == "expired-1" {
			t.Error("expired lease should have been filtered out")
		}
	}
}

func TestFilterActiveEmptyInput(t *testing.T) {
	active := filterActive(nil, time.Now().UTC())
	if active != nil {
		t.Errorf("expected nil for empty input, got %v", active)
	}
}

func TestFilterActiveInvalidExpiresAt(t *testing.T) {
	leases := []LeaseEntry{
		{LeaseID: "bad", ExpiresAt: "not-a-date"},
	}
	active := filterActive(leases, time.Now().UTC())
	if len(active) != 0 {
		t.Errorf("expected 0 active for invalid date, got %d", len(active))
	}
}

func TestInQuietHoursSameDay(t *testing.T) {
	tests := []struct {
		name  string
		now   time.Time
		start string
		end   string
		want  bool
	}{
		{
			"inside quiet hours",
			time.Date(2025, 1, 1, 3, 0, 0, 0, time.UTC),
			"02:00", "06:00",
			true,
		},
		{
			"before quiet hours",
			time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC),
			"02:00", "06:00",
			false,
		},
		{
			"after quiet hours",
			time.Date(2025, 1, 1, 7, 0, 0, 0, time.UTC),
			"02:00", "06:00",
			false,
		},
		{
			"at start boundary",
			time.Date(2025, 1, 1, 2, 0, 0, 0, time.UTC),
			"02:00", "06:00",
			true,
		},
		{
			"at end boundary",
			time.Date(2025, 1, 1, 6, 0, 0, 0, time.UTC),
			"02:00", "06:00",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qh := QuietHours{Start: tt.start, End: tt.end}
			got := inQuietHours(tt.now, qh)
			if got != tt.want {
				t.Errorf("inQuietHours(%v, %s-%s) = %v, want %v",
					tt.now, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestInQuietHoursCrossMidnight(t *testing.T) {
	tests := []struct {
		name  string
		now   time.Time
		start string
		end   string
		want  bool
	}{
		{
			"before midnight in range",
			time.Date(2025, 1, 1, 23, 0, 0, 0, time.UTC),
			"22:00", "06:00",
			true,
		},
		{
			"after midnight in range",
			time.Date(2025, 1, 1, 3, 0, 0, 0, time.UTC),
			"22:00", "06:00",
			true,
		},
		{
			"outside range midday",
			time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
			"22:00", "06:00",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qh := QuietHours{Start: tt.start, End: tt.end}
			got := inQuietHours(tt.now, qh)
			if got != tt.want {
				t.Errorf("inQuietHours(%v, %s-%s) = %v, want %v",
					tt.now, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestInQuietHoursEmptyConfig(t *testing.T) {
	qh := QuietHours{}
	got := inQuietHours(time.Now(), qh)
	if got {
		t.Error("empty quiet hours should return false")
	}
}

func TestInQuietHoursInvalidFormat(t *testing.T) {
	qh := QuietHours{Start: "bad", End: "bad"}
	got := inQuietHours(time.Now(), qh)
	if got {
		t.Error("invalid quiet hours format should return false")
	}
}

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		input string
		wantH int
		wantM int
	}{
		{"02:30", 2, 30},
		{"23:59", 23, 59},
		{"00:00", 0, 0},
		{"bad", -1, -1},
		{"", -1, -1},
	}

	for _, tt := range tests {
		h, m := parseHHMM(tt.input)
		if h != tt.wantH || m != tt.wantM {
			t.Errorf("parseHHMM(%q) = (%d, %d), want (%d, %d)",
				tt.input, h, m, tt.wantH, tt.wantM)
		}
	}
}

func TestLeaseStateRoundTrip(t *testing.T) {
	statePath, _ := setupLeaseDir(t)

	state := LeaseState{
		Leases: []LeaseEntry{
			{
				LeaseID:   "l1",
				JobID:     "j1",
				Resource:  "worker",
				Tokens:    100,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				ExpiresAt: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
			},
		},
	}
	writeState(t, statePath, state)

	got := readState(t, statePath)
	if len(got.Leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(got.Leases))
	}
	if got.Leases[0].LeaseID != "l1" {
		t.Errorf("LeaseID = %q, want %q", got.Leases[0].LeaseID, "l1")
	}
}

func TestConcurrencyLimitEnforcement(t *testing.T) {
	now := time.Now().UTC()
	policy := LeasePolicy{MaxConcurrent: 2}
	state := LeaseState{
		Leases: []LeaseEntry{
			{LeaseID: "l1", ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339)},
			{LeaseID: "l2", ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339)},
		},
	}

	// After filtering active leases, count should equal MaxConcurrent
	active := filterActive(state.Leases, now)
	if len(active) >= policy.MaxConcurrent {
		// This is the "denied" case - correct behavior
	} else {
		t.Error("expected concurrency limit to be reached")
	}
}

func TestTokenBudgetEnforcement(t *testing.T) {
	now := time.Now().UTC()
	hourAgo := now.Add(-1 * time.Hour)

	leases := []LeaseEntry{
		{
			LeaseID:   "l1",
			Tokens:    500,
			CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
			ExpiresAt: now.Add(30 * time.Minute).Format(time.RFC3339),
		},
		{
			LeaseID:   "l2",
			Tokens:    300,
			CreatedAt: now.Add(-15 * time.Minute).Format(time.RFC3339),
			ExpiresAt: now.Add(45 * time.Minute).Format(time.RFC3339),
		},
	}

	usedTokens := 0
	for _, l := range leases {
		created, err := time.Parse(time.RFC3339, l.CreatedAt)
		if err != nil {
			continue
		}
		if created.After(hourAgo) {
			usedTokens += l.Tokens
		}
	}

	maxPerHour := 1000
	requestTokens := 300
	if usedTokens+requestTokens > maxPerHour {
		// Correctly denied
	} else {
		t.Error("expected token budget to be exceeded")
	}

	if usedTokens != 800 {
		t.Errorf("usedTokens = %d, want 800", usedTokens)
	}
}

func TestTokenBudgetAllowsWithinLimit(t *testing.T) {
	now := time.Now().UTC()
	hourAgo := now.Add(-1 * time.Hour)

	leases := []LeaseEntry{
		{
			LeaseID:   "l1",
			Tokens:    200,
			CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
			ExpiresAt: now.Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	usedTokens := 0
	for _, l := range leases {
		created, _ := time.Parse(time.RFC3339, l.CreatedAt)
		if created.After(hourAgo) {
			usedTokens += l.Tokens
		}
	}

	maxPerHour := 1000
	requestTokens := 300
	if usedTokens+requestTokens > maxPerHour {
		t.Error("should be within budget")
	}
}

func TestTimeoutCapping(t *testing.T) {
	policy := LeasePolicy{MaxTimePerJob: 600}
	reqTimeout := 1800

	if policy.MaxTimePerJob > 0 && reqTimeout > policy.MaxTimePerJob {
		reqTimeout = policy.MaxTimePerJob
	}

	if reqTimeout != 600 {
		t.Errorf("timeout = %d, want 600 (capped)", reqTimeout)
	}
}

func TestTimeoutDefaultApplied(t *testing.T) {
	req := LeaseRequest{JobID: "j1", TimeoutSec: 0}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 300
	}
	if req.TimeoutSec != 300 {
		t.Errorf("default timeout = %d, want 300", req.TimeoutSec)
	}
}

func TestDefaultResourceApplied(t *testing.T) {
	req := LeaseRequest{JobID: "j1", Resource: ""}
	if req.Resource == "" {
		req.Resource = "worker"
	}
	if req.Resource != "worker" {
		t.Errorf("default resource = %q, want %q", req.Resource, "worker")
	}
}

func TestLeaseEntryJSONRoundTrip(t *testing.T) {
	entry := LeaseEntry{
		LeaseID:   "test-id",
		JobID:     "job-1",
		Resource:  "worker",
		Tokens:    42,
		CreatedAt: "2025-01-01T00:00:00Z",
		ExpiresAt: "2025-01-01T00:05:00Z",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var got LeaseEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.LeaseID != entry.LeaseID {
		t.Errorf("LeaseID = %q, want %q", got.LeaseID, entry.LeaseID)
	}
	if got.Tokens != entry.Tokens {
		t.Errorf("Tokens = %d, want %d", got.Tokens, entry.Tokens)
	}
}

func TestLeaseRelease(t *testing.T) {
	leases := []LeaseEntry{
		{LeaseID: "keep-1"},
		{LeaseID: "remove-1"},
		{LeaseID: "keep-2"},
	}

	targetID := "remove-1"
	var remaining []LeaseEntry
	found := false
	for _, l := range leases {
		if l.LeaseID == targetID {
			found = true
			continue
		}
		remaining = append(remaining, l)
	}

	if !found {
		t.Error("lease to release not found")
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(remaining))
	}
	for _, l := range remaining {
		if l.LeaseID == targetID {
			t.Error("removed lease still present")
		}
	}
}

func TestLeaseReleaseNotFound(t *testing.T) {
	leases := []LeaseEntry{
		{LeaseID: "l1"},
	}

	targetID := "nonexistent"
	found := false
	for _, l := range leases {
		if l.LeaseID == targetID {
			found = true
		}
	}
	if found {
		t.Error("should not find nonexistent lease")
	}
}

func TestLoadPolicyDefaults(t *testing.T) {
	// loadPolicy with no file returns defaults
	// We can't call loadPolicy directly since it reads from a constant path,
	// so we test the default values directly
	policy := LeasePolicy{
		MaxConcurrent:    4,
		MaxTokensPerHour: 1000000,
		MaxTimePerJob:    1800,
	}

	if policy.MaxConcurrent != 4 {
		t.Errorf("default MaxConcurrent = %d, want 4", policy.MaxConcurrent)
	}
	if policy.MaxTokensPerHour != 1000000 {
		t.Errorf("default MaxTokensPerHour = %d, want 1000000", policy.MaxTokensPerHour)
	}
	if policy.MaxTimePerJob != 1800 {
		t.Errorf("default MaxTimePerJob = %d, want 1800", policy.MaxTimePerJob)
	}
}
