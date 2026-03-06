// Command rank is a retrieval ranking improvement layer.
// It reads search candidate JSONL from stdin, applies history and boost
// adjustments, and outputs re-ranked JSONL sorted by score descending.
// Subcommand "record" updates the history file with feedback.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Candidate is a search result to be re-ranked.
type Candidate struct {
	Path  string  `json:"path"`
	Line  int     `json:"line"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// History tracks which files helped in past jobs.
type History struct {
	Files map[string]FileHistory `json:"files"`
}

// FileHistory is the help/total record for a single file.
type FileHistory struct {
	Helped int `json:"helped"`
	Total  int `json:"total"`
}

// BoostConfig holds manual boost rules.
type BoostConfig struct {
	Patterns []BoostPattern `json:"patterns"`
}

// BoostPattern is a single glob+boost rule.
type BoostPattern struct {
	Glob  string  `json:"glob"`
	Boost float64 `json:"boost"`
}

// RecordInput is the input for the "record" subcommand.
type RecordInput struct {
	Path   string `json:"path"`
	Helped bool   `json:"helped"`
}

func main() {
	// Check for subcommand
	if len(os.Args) > 1 && os.Args[1] == "record" {
		runRecord(os.Args[2:])
		return
	}

	historyPath := flag.String("history", "", "path to rank_history.json (default: .clock/rank_history.json)")
	boostPath := flag.String("boost", "", "path to rank_boosts.json (default: .clock/rank_boosts.json)")
	flag.Parse()

	if *historyPath == "" {
		*historyPath = filepath.Join(".clock", "rank_history.json")
	}
	if *boostPath == "" {
		*boostPath = filepath.Join(".clock", "rank_boosts.json")
	}

	// Load history
	history := loadHistory(*historyPath)

	// Load boosts
	boosts := loadBoosts(*boostPath)

	// Read candidates from stdin JSONL
	var candidates []Candidate
	err := jsonutil.ReadJSONL(func(raw json.RawMessage) error {
		var c Candidate
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("parse candidate: %w", err)
		}
		candidates = append(candidates, c)
		return nil
	})
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read candidates: %v", err))
	}

	if len(candidates) == 0 {
		return
	}

	// Re-rank
	rerank(candidates, history, boosts)

	// Sort by score descending
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Output JSONL
	for _, c := range candidates {
		if err := jsonutil.WriteJSONL(c); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
	}
}

func rerank(candidates []Candidate, history History, boosts BoostConfig) {
	// Apply history and boost adjustments
	for i := range candidates {
		score := candidates[i].Score

		// History multiplier: multiply by (1 + helped/total)
		if fh, ok := history.Files[candidates[i].Path]; ok && fh.Total > 0 {
			ratio := float64(fh.Helped) / float64(fh.Total)
			score *= (1.0 + ratio)
		}

		// Boost: add boost if path matches any boost pattern
		for _, bp := range boosts.Patterns {
			if matchGlob(bp.Glob, candidates[i].Path) {
				score += bp.Boost
			}
		}

		candidates[i].Score = score
	}

	// Normalize scores to 0.0-1.0
	normalize(candidates)
}

func normalize(candidates []Candidate) {
	if len(candidates) == 0 {
		return
	}

	maxScore := math.Inf(-1)
	minScore := math.Inf(1)
	for _, c := range candidates {
		if c.Score > maxScore {
			maxScore = c.Score
		}
		if c.Score < minScore {
			minScore = c.Score
		}
	}

	spread := maxScore - minScore
	for i := range candidates {
		if spread == 0 {
			// All same score, set to 1.0
			candidates[i].Score = 1.0
		} else {
			candidates[i].Score = (candidates[i].Score - minScore) / spread
		}
		// Round to 4 decimal places
		candidates[i].Score = math.Round(candidates[i].Score*10000) / 10000
	}
}

// matchGlob matches a glob pattern against a path.
// Supports * (any non-separator chars) and ** (any chars including separator).
func matchGlob(pattern, path string) bool {
	// Convert glob to a simple matcher
	// Handle ** first by replacing with a sentinel, then *
	// Use filepath.Match for single-segment globs
	// For ** patterns, do manual matching

	if strings.Contains(pattern, "**") {
		// Split on ** and check that all parts match in sequence
		parts := strings.Split(pattern, "**")
		remaining := path
		for i, part := range parts {
			part = strings.Trim(part, "/")
			if part == "" {
				continue
			}
			// For first part, must match from start
			if i == 0 {
				if !strings.HasPrefix(remaining, part) {
					// Try filepath.Match on the part
					matched, _ := filepath.Match(part, extractSegment(remaining, part))
					if !matched {
						return false
					}
				}
				idx := strings.Index(remaining, part)
				if idx < 0 {
					return false
				}
				remaining = remaining[idx+len(part):]
			} else {
				idx := findGlobMatch(remaining, part)
				if idx < 0 {
					return false
				}
				remaining = remaining[idx:]
			}
		}
		return true
	}

	// Simple glob - use filepath.Match
	matched, _ := filepath.Match(pattern, path)
	return matched
}

func extractSegment(path, pattern string) string {
	// Extract the first segment of path that has the same depth as pattern
	pDepth := strings.Count(pattern, "/") + 1
	parts := strings.SplitN(path, "/", pDepth+1)
	if len(parts) >= pDepth {
		return strings.Join(parts[:pDepth], "/")
	}
	return path
}

func findGlobMatch(haystack, pattern string) int {
	// Find pattern (possibly with globs) in haystack
	for i := 0; i < len(haystack); i++ {
		sub := haystack[i:]
		matched, _ := filepath.Match(pattern, sub)
		if matched {
			return i + len(sub)
		}
		// Also try matching against segments
		for j := i; j <= len(haystack); j++ {
			segment := haystack[i:j]
			matched, _ := filepath.Match(pattern, segment)
			if matched {
				return j
			}
		}
	}
	return -1
}

func loadHistory(path string) History {
	h := History{Files: make(map[string]FileHistory)}
	data, err := os.ReadFile(path)
	if err != nil {
		return h // File doesn't exist, return empty
	}
	if err := json.Unmarshal(data, &h); err != nil {
		return History{Files: make(map[string]FileHistory)}
	}
	if h.Files == nil {
		h.Files = make(map[string]FileHistory)
	}
	return h
}

func loadBoosts(path string) BoostConfig {
	b := BoostConfig{}
	data, err := os.ReadFile(path)
	if err != nil {
		return b
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return BoostConfig{}
	}
	return b
}

func runRecord(args []string) {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	historyPath := fs.String("history", "", "path to rank_history.json (default: .clock/rank_history.json)")
	fs.Parse(args)

	if *historyPath == "" {
		*historyPath = filepath.Join(".clock", "rank_history.json")
	}

	// Read record inputs from stdin (JSONL)
	history := loadHistory(*historyPath)

	err := jsonutil.ReadJSONL(func(raw json.RawMessage) error {
		var rec RecordInput
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("parse record input: %w", err)
		}
		if rec.Path == "" {
			return nil
		}

		fh := history.Files[rec.Path]
		fh.Total++
		if rec.Helped {
			fh.Helped++
		}
		history.Files[rec.Path] = fh
		return nil
	})
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read record input: %v", err))
	}

	// Ensure directory exists
	dir := filepath.Dir(*historyPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", dir, err))
	}

	// Write updated history
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal history: %v", err))
	}
	if err := os.WriteFile(*historyPath, append(data, '\n'), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write history: %v", err))
	}

	// Output confirmation
	if err := jsonutil.WriteOutput(map[string]interface{}{
		"ok":      true,
		"history": *historyPath,
		"files":   len(history.Files),
	}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
