// Command risk performs change risk scoring on a unified diff.
// It reads a RiskInput JSON from stdin and outputs a RiskResult JSON.
package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// RiskInput is the input schema for the risk tool.
type RiskInput struct {
	Diff string `json:"diff"`
	Doss string `json:"doss,omitempty"` // dossier text
	Hist string `json:"hist,omitempty"` // optional history context
}

// File extension risk weights.
var highRiskExtensions = map[string]bool{
	".yaml": true, ".yml": true, ".toml": true, ".json": true,
	".env": true, ".ini": true, ".cfg": true, ".conf": true,
	".tf": true, ".hcl": true,
}

var testExtensions = map[string]bool{
	"_test.go": true, ".test.js": true, ".test.ts": true,
	".spec.js": true, ".spec.ts": true, "_test.py": true,
}

// High-risk filename patterns.
var highRiskPatterns = []string{
	"migration", "migrate",
	"Dockerfile", "docker-compose",
	".github/workflows", ".gitlab-ci", "Jenkinsfile", ".circleci",
	"package.json", "package-lock.json", "go.mod", "go.sum",
	"Gemfile", "Cargo.toml", "requirements.txt", "pom.xml",
	"Makefile",
}

func main() {
	var input RiskInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Diff == "" {
		result := common.RiskResult{
			Risk:  0,
			Class: "low",
			Why:   []string{"empty diff"},
		}
		jsonutil.WriteOutput(result)
		return
	}

	analysis := analyzeDiff(input.Diff, input.Doss)

	if err := jsonutil.WriteOutput(analysis); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func analyzeDiff(diff, dossier string) common.RiskResult {
	lines := strings.Split(diff, "\n")

	var (
		filesChanged  []string
		linesAdded    int
		linesDeleted  int
		currentFile   string
		configFiles   int
		testFiles     int
		highRiskFiles int
		dossierHits   []string
		whys          []string
		musts         []string
	)

	// Parse the unified diff
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "+++ b/") {
			if strings.HasPrefix(line, "+++ b/") {
				currentFile = strings.TrimPrefix(line, "+++ b/")
				filesChanged = append(filesChanged, currentFile)
			}
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "@@") {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			linesAdded++
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			linesDeleted++
		}
	}

	// Deduplicate files
	seen := map[string]bool{}
	var uniqueFiles []string
	for _, f := range filesChanged {
		if !seen[f] {
			seen[f] = true
			uniqueFiles = append(uniqueFiles, f)
		}
	}
	filesChanged = uniqueFiles

	// Classify files
	for _, f := range filesChanged {
		ext := filepath.Ext(f)
		base := filepath.Base(f)

		// Check test files
		isTest := false
		for pattern := range testExtensions {
			if strings.HasSuffix(f, pattern) {
				isTest = true
				testFiles++
				break
			}
		}

		if isTest {
			continue
		}

		// Check config files
		if highRiskExtensions[ext] {
			configFiles++
		}

		// Check high-risk patterns
		for _, pattern := range highRiskPatterns {
			if strings.Contains(f, pattern) || strings.Contains(base, pattern) {
				highRiskFiles++
				whys = append(whys, fmt.Sprintf("high-risk file: %s", f))
				break
			}
		}
	}

	// Check dossier for risky zone matches
	if dossier != "" {
		dossierLower := strings.ToLower(dossier)
		for _, f := range filesChanged {
			fLower := strings.ToLower(f)
			// Check if any file path component appears in the dossier as a risky zone
			parts := strings.Split(fLower, "/")
			for _, part := range parts {
				if part != "" && len(part) > 2 && strings.Contains(dossierLower, part) {
					// Look for risk-related keywords near the mention
					idx := strings.Index(dossierLower, part)
					start := idx - 100
					if start < 0 {
						start = 0
					}
					end := idx + len(part) + 100
					if end > len(dossierLower) {
						end = len(dossierLower)
					}
					context := dossierLower[start:end]
					riskKeywords := []string{"risk", "danger", "caution", "careful", "critical", "sensitive", "fragile", "legacy", "deprecated", "unstable"}
					for _, kw := range riskKeywords {
						if strings.Contains(context, kw) {
							dossierHits = append(dossierHits, fmt.Sprintf("file %s touches risky zone mentioned in dossier (%s)", f, kw))
							break
						}
					}
				}
			}
		}
	}

	// Calculate risk score (0.0 to 1.0)
	risk := 0.0

	// File count factor
	nFiles := len(filesChanged)
	if nFiles > 0 {
		whys = append(whys, fmt.Sprintf("%d file(s) changed", nFiles))
	}
	if nFiles >= 10 {
		risk += 0.25
		whys = append(whys, "large number of files changed")
	} else if nFiles >= 5 {
		risk += 0.15
	} else if nFiles >= 1 {
		risk += 0.05
	}

	// Lines changed factor
	totalLines := linesAdded + linesDeleted
	whys = append(whys, fmt.Sprintf("+%d/-%d lines", linesAdded, linesDeleted))
	if totalLines > 500 {
		risk += 0.2
		whys = append(whys, "large change volume")
	} else if totalLines > 100 {
		risk += 0.1
	} else if totalLines > 0 {
		risk += 0.03
	}

	// Config file factor
	if configFiles > 0 {
		risk += 0.15 * math.Min(float64(configFiles)/3.0, 1.0)
		whys = append(whys, fmt.Sprintf("%d config file(s) modified", configFiles))
	}

	// High-risk file factor
	if highRiskFiles > 0 {
		risk += 0.2 * math.Min(float64(highRiskFiles)/3.0, 1.0)
		musts = append(musts, "review high-risk files carefully")
	}

	// Test file discount
	if testFiles > 0 && testFiles == nFiles {
		risk *= 0.5 // all test files — lower risk
		whys = append(whys, "all changes are test files")
	} else if testFiles > 0 {
		whys = append(whys, fmt.Sprintf("%d test file(s) included", testFiles))
	} else if nFiles > 0 {
		risk += 0.05
		musts = append(musts, "add or update tests")
	}

	// Dossier hits
	if len(dossierHits) > 0 {
		risk += 0.15 * math.Min(float64(len(dossierHits))/3.0, 1.0)
		whys = append(whys, dossierHits...)
		musts = append(musts, "verify changes in risky zones flagged by dossier")
	}

	// Clamp risk
	if risk > 1.0 {
		risk = 1.0
	}
	risk = math.Round(risk*100) / 100

	// Classify
	class := "low"
	if risk >= 0.6 {
		class = "high"
	} else if risk >= 0.3 {
		class = "med"
	}

	return common.RiskResult{
		Risk:  risk,
		Class: class,
		Must:  musts,
		Why:   whys,
	}
}
