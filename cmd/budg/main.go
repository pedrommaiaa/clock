// Command budg performs hard context budgeting and packing.
// It reads JSONL candidate snippets from stdin, sorts by score, packs
// them within a byte budget, optionally deduplicates overlapping ranges,
// and outputs the packed snippets with stats.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Snippet is a candidate context snippet.
type Snippet struct {
	Path  string  `json:"path"`
	Start int     `json:"start"`
	End   int     `json:"end"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// BudgOutput is the output of the budg tool.
type BudgOutput struct {
	Snippets []Snippet `json:"snippets"`
	Stats    BudgStats `json:"stats"`
}

// BudgStats contains packing statistics.
type BudgStats struct {
	UsedBytes       int `json:"used_bytes"`
	TotalCandidates int `json:"total_candidates"`
	Dropped         int `json:"dropped"`
	Budget          int `json:"budget"`
}

func main() {
	maxBytes := flag.Int("max", 120000, "maximum budget in bytes")
	dedup := flag.Bool("dedup", false, "merge overlapping ranges from the same file")
	flag.Parse()

	// Read JSONL snippets from stdin
	var candidates []Snippet
	err := jsonutil.ReadJSONL(func(raw json.RawMessage) error {
		var s Snippet
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("parse snippet: %w", err)
		}
		candidates = append(candidates, s)
		return nil
	})
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	totalCandidates := len(candidates)

	// Deduplicate overlapping ranges from the same file if requested
	if *dedup {
		candidates = mergeOverlapping(candidates)
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Pack snippets until budget exhausted
	var packed []Snippet
	usedBytes := 0
	for _, s := range candidates {
		size := len(s.Text)
		if size == 0 {
			// Estimate size from line count if text is empty
			continue
		}
		if usedBytes+size > *maxBytes {
			continue
		}
		packed = append(packed, s)
		usedBytes += size
	}

	if packed == nil {
		packed = []Snippet{}
	}

	output := BudgOutput{
		Snippets: packed,
		Stats: BudgStats{
			UsedBytes:       usedBytes,
			TotalCandidates: totalCandidates,
			Dropped:         totalCandidates - len(packed),
			Budget:          *maxBytes,
		},
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// mergeOverlapping merges snippets from the same file that have overlapping
// line ranges. The merged snippet gets the maximum score of its constituents.
func mergeOverlapping(snippets []Snippet) []Snippet {
	// Group by path
	groups := make(map[string][]Snippet)
	var order []string
	for _, s := range snippets {
		if _, ok := groups[s.Path]; !ok {
			order = append(order, s.Path)
		}
		groups[s.Path] = append(groups[s.Path], s)
	}

	var result []Snippet
	for _, path := range order {
		group := groups[path]
		if len(group) == 1 {
			result = append(result, group...)
			continue
		}

		// Sort by start line
		sort.Slice(group, func(i, j int) bool {
			if group[i].Start == group[j].Start {
				return group[i].End < group[j].End
			}
			return group[i].Start < group[j].Start
		})

		// Merge overlapping ranges
		merged := []Snippet{group[0]}
		for i := 1; i < len(group); i++ {
			last := &merged[len(merged)-1]
			cur := group[i]
			if cur.Start <= last.End+1 {
				// Overlapping or adjacent — merge
				if cur.End > last.End {
					// Extend text: append the non-overlapping suffix
					overlapLines := last.End - cur.Start + 1
					if overlapLines < 0 {
						overlapLines = 0
					}
					// We can't perfectly reconstruct merged text without
					// re-reading the file, so we concatenate with a marker
					// for the overlap. In practice the texts should be
					// contiguous.
					suffix := trimLeadingLines(cur.Text, overlapLines)
					last.Text = last.Text + suffix
					last.End = cur.End
				}
				// Take max score
				if cur.Score > last.Score {
					last.Score = cur.Score
				}
			} else {
				merged = append(merged, cur)
			}
		}
		result = append(result, merged...)
	}

	return result
}

// trimLeadingLines removes the first n lines from s.
func trimLeadingLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	idx := 0
	for i := 0; i < n && idx < len(s); i++ {
		next := indexByte(s[idx:], '\n')
		if next < 0 {
			return ""
		}
		idx += next + 1
	}
	if idx >= len(s) {
		return ""
	}
	return s[idx:]
}

// indexByte returns the index of the first instance of c in s, or -1.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
