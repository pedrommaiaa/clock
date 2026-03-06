// Command pbk is a playbook generator that learns reusable procedures
// from successful job traces and matches them against future goals/errors.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const playbookDir = ".clock/playbooks"

// Playbook is a reusable procedure learned from traces.
type Playbook struct {
	ID          string   `json:"id"`
	Trigger     Trigger  `json:"trigger"`
	Steps       []Step   `json:"steps"`
	Verify      []string `json:"verify"`
	Risk        string   `json:"risk"`
	SuccessRate float64  `json:"success_rate"`
	Uses        int      `json:"uses"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Trigger defines when a playbook should be applied.
type Trigger struct {
	ErrorPattern string   `json:"error_pattern,omitempty"`
	FileTypes    []string `json:"file_types,omitempty"`
	Goal         string   `json:"goal,omitempty"`
	Tools        []string `json:"tools,omitempty"`
}

// Step is a single action in the playbook.
type Step struct {
	Tool string                 `json:"tool"`
	Args map[string]interface{} `json:"args,omitempty"`
	Desc string                 `json:"desc,omitempty"`
}

// LearnInput is the input for the learn subcommand.
type LearnInput struct {
	Job   JobResult    `json:"job"`
	Trace []TraceEvent `json:"trace"`
}

// JobResult mirrors common.JobResult for standalone compilation.
type JobResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Goal   string `json:"goal"`
	Diff   string `json:"diff,omitempty"`
	Report string `json:"report,omitempty"`
	Err    string `json:"err,omitempty"`
}

// TraceEvent mirrors common.TraceEvent for standalone compilation.
type TraceEvent struct {
	TS    int64       `json:"ts"`
	Event string      `json:"event"`
	Tool  string      `json:"tool,omitempty"`
	Data  interface{} `json:"data,omitempty"`
	Ms    int64       `json:"ms,omitempty"`
	ChkID string     `json:"chk,omitempty"`
}

// UpdateInput is the input for the update subcommand.
type UpdateInput struct {
	OK bool `json:"ok"`
}

// LearnResult is the output of the learn subcommand.
type LearnResult struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`
	Steps    int    `json:"steps"`
	File     string `json:"file"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: pbk <subcommand> [args]\nsubcommands: learn, match, list, get, update")
	}

	sub := os.Args[1]
	switch sub {
	case "learn":
		cmdLearn()
	case "match":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: pbk match <query>")
		}
		cmdMatch(strings.Join(os.Args[2:], " "))
	case "list":
		cmdList()
	case "get":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: pbk get <id>")
		}
		cmdGet(os.Args[2])
	case "update":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: pbk update <id>")
		}
		cmdUpdate(os.Args[2])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", sub))
	}
}

func ensureDir() {
	if err := os.MkdirAll(playbookDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", playbookDir, err))
	}
}

func makeID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return fmt.Sprintf("pb-%x", h[:6])
}

func cmdLearn() {
	var input LearnInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if len(input.Trace) == 0 {
		jsonutil.Fatal("no trace events provided")
	}

	// Extract tool call sequence
	var toolCalls []TraceEvent
	for _, ev := range input.Trace {
		if ev.Event == "tool.call" && ev.Tool != "" {
			toolCalls = append(toolCalls, ev)
		}
	}

	// Build steps from tool calls
	var steps []Step
	toolSet := make(map[string]bool)
	for _, tc := range toolCalls {
		step := Step{
			Tool: tc.Tool,
		}
		// Try to extract args from data
		if tc.Data != nil {
			if m, ok := tc.Data.(map[string]interface{}); ok {
				step.Args = m
			}
		}
		steps = append(steps, step)
		toolSet[tc.Tool] = true
	}

	if len(steps) == 0 {
		// Fallback: use all events with tool names
		for _, ev := range input.Trace {
			if ev.Tool != "" {
				steps = append(steps, Step{Tool: ev.Tool})
				toolSet[ev.Tool] = true
			}
		}
	}

	// Detect patterns for trigger
	trigger := Trigger{}

	// Extract error pattern from job
	if input.Job.Err != "" {
		trigger.ErrorPattern = extractErrorPattern(input.Job.Err)
	}

	// Extract goal keywords
	if input.Job.Goal != "" {
		trigger.Goal = input.Job.Goal
	}

	// Detect file types from diff
	if input.Job.Diff != "" {
		trigger.FileTypes = extractFileTypes(input.Job.Diff)
	}

	// Record tools used
	var tools []string
	for t := range toolSet {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	trigger.Tools = tools

	// Detect verify steps (tool calls that look like verification)
	var verify []string
	verifyTools := map[string]bool{"vrfy": true, "test": true, "exec": true, "guard": true}
	for _, s := range steps {
		if verifyTools[s.Tool] {
			verify = appendUnique(verify, s.Tool)
		}
	}
	if len(verify) == 0 {
		verify = []string{"test"}
	}

	// Assess risk based on tools used
	risk := "low"
	riskyTools := map[string]bool{"exec": true, "aply": true, "push": true}
	for _, s := range steps {
		if riskyTools[s.Tool] {
			risk = "med"
			break
		}
	}

	// Generate description
	desc := fmt.Sprintf("Learned from job %s", input.Job.ID)
	if input.Job.Goal != "" {
		desc = fmt.Sprintf("Procedure for: %s", truncate(input.Job.Goal, 80))
	}

	now := time.Now()
	id := makeID(input.Job.ID, now.Format(time.RFC3339Nano))

	playbook := Playbook{
		ID:          id,
		Trigger:     trigger,
		Steps:       steps,
		Verify:      verify,
		Risk:        risk,
		SuccessRate: 1.0,
		Uses:        1,
		CreatedAt:   now.Format(time.RFC3339),
		Description: desc,
	}

	// Save playbook
	ensureDir()
	pbFile := filepath.Join(playbookDir, fmt.Sprintf("%s.json", id))
	data, err := json.MarshalIndent(playbook, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal playbook: %v", err))
	}
	if err := os.WriteFile(pbFile, data, 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write playbook: %v", err))
	}

	result := LearnResult{
		OK:    true,
		ID:    id,
		Steps: len(steps),
		File:  pbFile,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdMatch(query string) {
	playbooks, err := loadAllPlaybooks()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load playbooks: %v", err))
	}

	queryLower := strings.ToLower(query)

	type scored struct {
		pb    Playbook
		score float64
	}

	var matches []scored

	for _, pb := range playbooks {
		score := 0.0

		// Match against error pattern
		if pb.Trigger.ErrorPattern != "" {
			pat, err := regexp.Compile("(?i)" + regexp.QuoteMeta(pb.Trigger.ErrorPattern))
			if err == nil && pat.MatchString(query) {
				score += 3.0
			} else if strings.Contains(queryLower, strings.ToLower(pb.Trigger.ErrorPattern)) {
				score += 2.0
			}
		}

		// Match against goal
		if pb.Trigger.Goal != "" {
			goalLower := strings.ToLower(pb.Trigger.Goal)
			// Count word overlaps
			queryWords := strings.Fields(queryLower)
			goalWords := strings.Fields(goalLower)
			for _, qw := range queryWords {
				for _, gw := range goalWords {
					if qw == gw && len(qw) > 2 {
						score += 0.5
					}
				}
			}
			if strings.Contains(goalLower, queryLower) || strings.Contains(queryLower, goalLower) {
				score += 2.0
			}
		}

		// Match description
		if pb.Description != "" && strings.Contains(strings.ToLower(pb.Description), queryLower) {
			score += 1.0
		}

		// Match against tools in trigger
		for _, tool := range pb.Trigger.Tools {
			if strings.Contains(queryLower, strings.ToLower(tool)) {
				score += 0.5
			}
		}

		// Match against file types
		for _, ft := range pb.Trigger.FileTypes {
			if strings.Contains(queryLower, strings.ToLower(ft)) {
				score += 0.5
			}
		}

		// Weight by success rate
		score *= pb.SuccessRate

		if score > 0 {
			matches = append(matches, scored{pb: pb, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	for _, m := range matches {
		if err := jsonutil.WriteJSONL(m.pb); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

func cmdList() {
	playbooks, err := loadAllPlaybooks()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load playbooks: %v", err))
	}

	// Sort by created_at descending
	sort.Slice(playbooks, func(i, j int) bool {
		return playbooks[i].CreatedAt > playbooks[j].CreatedAt
	})

	for _, pb := range playbooks {
		if err := jsonutil.WriteJSONL(pb); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

func cmdGet(id string) {
	pb, err := loadPlaybook(id)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load playbook: %v", err))
	}
	if err := jsonutil.WriteOutput(pb); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdUpdate(id string) {
	var input UpdateInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	pb, err := loadPlaybook(id)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load playbook: %v", err))
	}

	// Update success rate with exponential moving average
	pb.Uses++
	successVal := 0.0
	if input.OK {
		successVal = 1.0
	}
	// EMA: new_rate = old_rate * (n-1)/n + new_val * 1/n
	n := float64(pb.Uses)
	pb.SuccessRate = pb.SuccessRate*(n-1)/n + successVal/n
	pb.UpdatedAt = time.Now().Format(time.RFC3339)

	// Save updated playbook
	pbFile := filepath.Join(playbookDir, fmt.Sprintf("%s.json", id))
	data, err := json.MarshalIndent(pb, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal playbook: %v", err))
	}
	if err := os.WriteFile(pbFile, data, 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write playbook: %v", err))
	}

	if err := jsonutil.WriteOutput(pb); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func loadPlaybook(id string) (*Playbook, error) {
	pbFile := filepath.Join(playbookDir, fmt.Sprintf("%s.json", id))
	data, err := os.ReadFile(pbFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pbFile, err)
	}
	var pb Playbook
	if err := json.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("parse %s: %w", pbFile, err)
	}
	return &pb, nil
}

func loadAllPlaybooks() ([]Playbook, error) {
	entries, err := os.ReadDir(playbookDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var playbooks []Playbook
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(playbookDir, entry.Name()))
		if err != nil {
			continue
		}
		var pb Playbook
		if err := json.Unmarshal(data, &pb); err != nil {
			continue
		}
		playbooks = append(playbooks, pb)
	}
	return playbooks, nil
}

func extractErrorPattern(errMsg string) string {
	// Remove specific identifiers to create a general pattern
	// Strip hex hashes, UUIDs, timestamps, line numbers
	cleaned := errMsg
	cleaned = regexp.MustCompile(`[0-9a-f]{8,}`).ReplaceAllString(cleaned, "...")
	cleaned = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`).ReplaceAllString(cleaned, "...")
	cleaned = regexp.MustCompile(`line \d+`).ReplaceAllString(cleaned, "line ...")
	cleaned = regexp.MustCompile(`:\d+:\d+`).ReplaceAllString(cleaned, ":...:...")

	if len(cleaned) > 100 {
		cleaned = cleaned[:100]
	}
	return strings.TrimSpace(cleaned)
}

func extractFileTypes(diff string) []string {
	scanner := bufio.NewScanner(strings.NewReader(diff))
	extSet := make(map[string]bool)

	diffFile := regexp.MustCompile(`^(?:---|\+\+\+)\s+[ab]/(.+)$`)
	for scanner.Scan() {
		line := scanner.Text()
		if m := diffFile.FindStringSubmatch(line); m != nil {
			file := m[1]
			if file == "/dev/null" {
				continue
			}
			ext := filepath.Ext(file)
			if ext != "" {
				extSet[ext] = true
			}
		}
	}

	var types []string
	for ext := range extSet {
		types = append(types, ext)
	}
	sort.Strings(types)
	return types
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
