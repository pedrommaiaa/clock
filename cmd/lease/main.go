// Command lease is a resource governor that manages concurrent leases,
// token budgets, and quiet-hours enforcement for Clock jobs.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// LeaseEntry represents a single active lease.
type LeaseEntry struct {
	LeaseID   string `json:"lease_id"`
	JobID     string `json:"job_id"`
	Resource  string `json:"resource"`
	Tokens    int    `json:"tokens,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// LeaseState is the persisted lease state file.
type LeaseState struct {
	Leases []LeaseEntry `json:"leases"`
}

// LeasePolicy is the policy configuration.
type LeasePolicy struct {
	MaxConcurrent    int        `json:"max_concurrent"`
	MaxTokensPerHour int        `json:"max_tokens_per_hour"`
	QuietHours       QuietHours `json:"quiet_hours"`
	MaxTimePerJob    int        `json:"max_time_per_job"`
}

// QuietHours defines a time window when leases are denied.
type QuietHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// LeaseRequest is the input for lease request.
type LeaseRequest struct {
	JobID      string `json:"job_id"`
	Resource   string `json:"resource"`
	Tokens     int    `json:"tokens,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// LeaseResponse is the output of lease request/release.
type LeaseResponse struct {
	Granted   bool   `json:"granted"`
	Reason    string `json:"reason,omitempty"`
	LeaseID   string `json:"lease_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

const (
	stateFile  = ".clock/leases.json"
	policyFile = ".clock/lease_policy.json"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		// Default: request from stdin
		doRequest()
		return
	}

	switch args[0] {
	case "request":
		doRequest()
	case "release":
		if len(args) < 2 {
			jsonutil.Fatal("usage: lease release <lease_id>")
		}
		doRelease(args[1])
	case "list":
		doList()
	case "clean":
		doClean()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", args[0]))
	}
}

func doRequest() {
	var req LeaseRequest
	if err := jsonutil.ReadInput(&req); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if req.JobID == "" {
		jsonutil.Fatal("job_id is required")
	}
	if req.Resource == "" {
		req.Resource = "worker"
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 300
	}

	policy := loadPolicy()
	state := loadState()

	// Clean expired leases first
	now := time.Now().UTC()
	state.Leases = filterActive(state.Leases, now)

	// Check quiet hours
	if inQuietHours(now, policy.QuietHours) {
		resp := LeaseResponse{
			Granted: false,
			Reason:  fmt.Sprintf("quiet hours active (%s-%s UTC)", policy.QuietHours.Start, policy.QuietHours.End),
		}
		writeAndExit(resp)
		return
	}

	// Check max concurrent
	if policy.MaxConcurrent > 0 && len(state.Leases) >= policy.MaxConcurrent {
		resp := LeaseResponse{
			Granted: false,
			Reason:  fmt.Sprintf("max concurrent reached (%d/%d)", len(state.Leases), policy.MaxConcurrent),
		}
		writeAndExit(resp)
		return
	}

	// Check max_time_per_job
	if policy.MaxTimePerJob > 0 && req.TimeoutSec > policy.MaxTimePerJob {
		req.TimeoutSec = policy.MaxTimePerJob
	}

	// Check max tokens per hour
	if policy.MaxTokensPerHour > 0 && req.Tokens > 0 {
		hourAgo := now.Add(-1 * time.Hour)
		usedTokens := 0
		for _, l := range state.Leases {
			created, err := time.Parse(time.RFC3339, l.CreatedAt)
			if err != nil {
				continue
			}
			if created.After(hourAgo) {
				usedTokens += l.Tokens
			}
		}
		if usedTokens+req.Tokens > policy.MaxTokensPerHour {
			resp := LeaseResponse{
				Granted: false,
				Reason:  fmt.Sprintf("token budget exceeded (%d+%d > %d/hr)", usedTokens, req.Tokens, policy.MaxTokensPerHour),
			}
			writeAndExit(resp)
			return
		}
	}

	// Grant lease
	leaseID := generateID()
	expiresAt := now.Add(time.Duration(req.TimeoutSec) * time.Second)

	entry := LeaseEntry{
		LeaseID:   leaseID,
		JobID:     req.JobID,
		Resource:  req.Resource,
		Tokens:    req.Tokens,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}
	state.Leases = append(state.Leases, entry)
	saveState(state)

	resp := LeaseResponse{
		Granted:   true,
		LeaseID:   leaseID,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}
	writeAndExit(resp)
}

func doRelease(leaseID string) {
	state := loadState()
	found := false
	var remaining []LeaseEntry
	for _, l := range state.Leases {
		if l.LeaseID == leaseID {
			found = true
			continue
		}
		remaining = append(remaining, l)
	}

	if !found {
		resp := LeaseResponse{
			Granted: false,
			Reason:  fmt.Sprintf("lease %s not found", leaseID),
		}
		writeAndExit(resp)
		return
	}

	state.Leases = remaining
	if state.Leases == nil {
		state.Leases = []LeaseEntry{}
	}
	saveState(state)

	resp := LeaseResponse{
		Granted: true,
		LeaseID: leaseID,
	}
	writeAndExit(resp)
}

func doList() {
	state := loadState()
	now := time.Now().UTC()
	state.Leases = filterActive(state.Leases, now)
	if state.Leases == nil {
		state.Leases = []LeaseEntry{}
	}
	if err := jsonutil.WriteOutput(state); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doClean() {
	state := loadState()
	before := len(state.Leases)
	now := time.Now().UTC()
	state.Leases = filterActive(state.Leases, now)
	if state.Leases == nil {
		state.Leases = []LeaseEntry{}
	}
	removed := before - len(state.Leases)
	saveState(state)

	result := map[string]interface{}{
		"cleaned": removed,
		"active":  len(state.Leases),
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func loadPolicy() LeasePolicy {
	policy := LeasePolicy{
		MaxConcurrent:    4,
		MaxTokensPerHour: 1000000,
		MaxTimePerJob:    1800,
	}

	data, err := os.ReadFile(policyFile)
	if err != nil {
		return policy
	}
	_ = json.Unmarshal(data, &policy)
	return policy
}

func loadState() LeaseState {
	state := LeaseState{Leases: []LeaseEntry{}}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if state.Leases == nil {
		state.Leases = []LeaseEntry{}
	}
	return state
}

func saveState(state LeaseState) {
	dir := filepath.Dir(stateFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("create dir: %v", err))
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal state: %v", err))
	}
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write state: %v", err))
	}
}

func filterActive(leases []LeaseEntry, now time.Time) []LeaseEntry {
	var active []LeaseEntry
	for _, l := range leases {
		expires, err := time.Parse(time.RFC3339, l.ExpiresAt)
		if err != nil {
			continue
		}
		if now.Before(expires) {
			active = append(active, l)
		}
	}
	return active
}

func inQuietHours(now time.Time, qh QuietHours) bool {
	if qh.Start == "" || qh.End == "" {
		return false
	}

	startH, startM := parseHHMM(qh.Start)
	endH, endM := parseHHMM(qh.End)
	if startH < 0 || endH < 0 {
		return false
	}

	nowMin := now.Hour()*60 + now.Minute()
	startMin := startH*60 + startM
	endMin := endH*60 + endM

	if startMin <= endMin {
		// Same day: e.g., 02:00 - 06:00
		return nowMin >= startMin && nowMin < endMin
	}
	// Crosses midnight: e.g., 22:00 - 06:00
	return nowMin >= startMin || nowMin < endMin
}

func parseHHMM(s string) (int, int) {
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return -1, -1
	}
	return h, m
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeAndExit(resp LeaseResponse) {
	if err := jsonutil.WriteOutput(resp); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
