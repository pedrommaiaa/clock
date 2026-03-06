// Command eval is a plan safety evaluator that scores proposed changes for risk.
// It reads an EvalInput JSON from stdin and outputs an EvalResult JSON.
package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// EvalInput is the input schema for the eval tool.
type EvalInput struct {
	Diff    string     `json:"diff"`
	Context string     `json:"context"`
	Policy  EvalPolicy `json:"policy"`
}

// EvalPolicy defines the constraints for plan evaluation.
type EvalPolicy struct {
	ForbidMigrations bool `json:"forbid_migrations"`
	ForbidDeps       bool `json:"forbid_deps"`
	MaxFiles         int  `json:"max_files"`
	MaxLines         int  `json:"max_lines"`
}

// EvalResult is the output of the eval tool.
type EvalResult struct {
	Risk     float64      `json:"risk"`
	Class    string       `json:"class"`
	Requires string       `json:"requires"`
	Reasons  []string     `json:"reasons"`
	Checks   []EvalCheck  `json:"checks"`
}

// EvalCheck is a single evaluation check result.
type EvalCheck struct {
	Name string `json:"name"`
	Pass bool   `json:"pass"`
	Info string `json:"info,omitempty"`
}

// Dependency manifest filenames.
var depManifests = map[string]bool{
	"package.json":      true,
	"package-lock.json": true,
	"go.mod":            true,
	"go.sum":            true,
	"requirements.txt":  true,
	"Pipfile":           true,
	"Pipfile.lock":      true,
	"Cargo.toml":        true,
	"Cargo.lock":        true,
	"Gemfile":           true,
	"Gemfile.lock":      true,
	"pom.xml":           true,
	"build.gradle":      true,
	"yarn.lock":         true,
	"composer.json":     true,
	"composer.lock":     true,
}

// Migration path patterns.
var migrationPatterns = []string{
	"migration", "migrate", "migrations", "db/migrate",
	"alembic", "flyway", "liquibase", "knex",
}

// Dangerous command/SQL patterns.
var dangerousPatterns = []string{
	"rm -rf", "rm -fr",
	"DROP TABLE", "DROP DATABASE", "DROP SCHEMA",
	"DELETE FROM", "TRUNCATE TABLE",
	"force push", "push --force", "push -f",
	"git reset --hard",
	"chmod 777",
	"curl | sh", "curl | bash", "wget | sh", "wget | bash",
}

// Secret/credential patterns.
var secretPatterns = []string{
	"password=", "passwd=",
	"api_key=", "apikey=", "api-key=",
	"secret=", "secret_key=",
	"token=", "auth_token=", "access_token=",
	"private_key=",
	"AWS_SECRET", "AWS_ACCESS_KEY",
	"GITHUB_TOKEN=",
}

func main() {
	var input EvalInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Diff == "" {
		result := EvalResult{
			Risk:     0.0,
			Class:    "low",
			Requires: "auto",
			Reasons:  []string{"empty diff"},
			Checks:   []EvalCheck{{Name: "empty_diff", Pass: true, Info: "no changes to evaluate"}},
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	result := evaluate(input)
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func evaluate(input EvalInput) EvalResult {
	diff := input.Diff
	policy := input.Policy

	lines := strings.Split(diff, "\n")
	var (
		filesChanged []string
		linesAdded   int
		linesDeleted int
		reasons      []string
		checks       []EvalCheck
		risk         float64
	)

	// Parse diff to extract files and line counts
	fileSet := map[string]bool{}
	for _, line := range lines {
		if strings.HasPrefix(line, "+++ b/") {
			f := strings.TrimPrefix(line, "+++ b/")
			if !fileSet[f] {
				fileSet[f] = true
				filesChanged = append(filesChanged, f)
			}
		} else if strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "+++ /dev/null") {
			f := strings.TrimPrefix(line, "+++ ")
			if strings.HasPrefix(f, "b/") {
				f = f[2:]
			}
			if !fileSet[f] {
				fileSet[f] = true
				filesChanged = append(filesChanged, f)
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			linesAdded++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			linesDeleted++
		}
	}
	totalLines := linesAdded + linesDeleted

	// Check 1: Migration files
	touchesMigrations := false
	for _, f := range filesChanged {
		fl := strings.ToLower(f)
		for _, pat := range migrationPatterns {
			if strings.Contains(fl, pat) {
				touchesMigrations = true
				break
			}
		}
		if touchesMigrations {
			break
		}
	}
	migrationPass := true
	if touchesMigrations {
		reasons = append(reasons, "touches migrations")
		risk += 0.3
		if policy.ForbidMigrations {
			migrationPass = false
			risk += 0.2
			reasons = append(reasons, "policy forbids migrations")
		}
	}
	checks = append(checks, EvalCheck{
		Name: "migrations",
		Pass: migrationPass,
		Info: boolInfo(touchesMigrations, "migration files detected", "no migration files"),
	})

	// Check 2: Dependency manifests
	touchesDeps := false
	var depFiles []string
	for _, f := range filesChanged {
		base := f
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			base = f[idx+1:]
		}
		if depManifests[base] {
			touchesDeps = true
			depFiles = append(depFiles, f)
		}
	}
	depsPass := true
	if touchesDeps {
		reasons = append(reasons, fmt.Sprintf("modifies dependencies: %s", strings.Join(depFiles, ", ")))
		risk += 0.2
		if policy.ForbidDeps {
			depsPass = false
			risk += 0.15
			reasons = append(reasons, "policy forbids dependency changes")
		}
	}
	checks = append(checks, EvalCheck{
		Name: "dependencies",
		Pass: depsPass,
		Info: boolInfo(touchesDeps, fmt.Sprintf("dependency files: %s", strings.Join(depFiles, ", ")), "no dependency files"),
	})

	// Check 3: File count
	fileCountPass := true
	if policy.MaxFiles > 0 && len(filesChanged) > policy.MaxFiles {
		fileCountPass = false
		reasons = append(reasons, fmt.Sprintf("file count %d exceeds max %d", len(filesChanged), policy.MaxFiles))
		risk += 0.15
	}
	checks = append(checks, EvalCheck{
		Name: "file_count",
		Pass: fileCountPass,
		Info: fmt.Sprintf("%d files changed (max: %d)", len(filesChanged), policy.MaxFiles),
	})

	// Check 4: Line count
	lineCountPass := true
	if policy.MaxLines > 0 && totalLines > policy.MaxLines {
		lineCountPass = false
		reasons = append(reasons, fmt.Sprintf("line count %d exceeds max %d", totalLines, policy.MaxLines))
		risk += 0.15
	}
	checks = append(checks, EvalCheck{
		Name: "line_count",
		Pass: lineCountPass,
		Info: fmt.Sprintf("+%d/-%d = %d lines (max: %d)", linesAdded, linesDeleted, totalLines, policy.MaxLines),
	})

	// Check 5: Dangerous patterns
	diffLower := strings.ToLower(diff)
	var foundDangerous []string
	for _, pat := range dangerousPatterns {
		if strings.Contains(diffLower, strings.ToLower(pat)) {
			foundDangerous = append(foundDangerous, pat)
		}
	}
	dangerousPass := len(foundDangerous) == 0
	if !dangerousPass {
		reasons = append(reasons, fmt.Sprintf("dangerous patterns: %s", strings.Join(foundDangerous, ", ")))
		risk += 0.15 * math.Min(float64(len(foundDangerous)), 3.0) / 3.0
	}
	checks = append(checks, EvalCheck{
		Name: "dangerous_patterns",
		Pass: dangerousPass,
		Info: boolInfo(!dangerousPass, fmt.Sprintf("found: %s", strings.Join(foundDangerous, ", ")), "none found"),
	})

	// Check 6: Secrets patterns
	var foundSecrets []string
	for _, pat := range secretPatterns {
		if strings.Contains(diffLower, strings.ToLower(pat)) {
			foundSecrets = append(foundSecrets, pat)
		}
	}
	secretsPass := len(foundSecrets) == 0
	if !secretsPass {
		reasons = append(reasons, fmt.Sprintf("possible secrets: %s", strings.Join(foundSecrets, ", ")))
		risk += 0.25
	}
	checks = append(checks, EvalCheck{
		Name: "secrets",
		Pass: secretsPass,
		Info: boolInfo(!secretsPass, fmt.Sprintf("found: %s", strings.Join(foundSecrets, ", ")), "none found"),
	})

	// Clamp risk to [0, 1]
	risk = math.Min(risk, 1.0)
	risk = math.Round(risk*100) / 100

	// Classify
	class := "low"
	if risk >= 0.7 {
		class = "high"
	} else if risk >= 0.4 {
		class = "medium"
	}

	// Determine approval requirement
	requires := "auto"
	if risk > 0.7 {
		requires = "approval"
	}
	// If any policy check fails, require approval regardless
	for _, c := range checks {
		if !c.Pass {
			if requires != "approval" {
				requires = "approval"
			}
			break
		}
	}
	// If secrets found, always deny
	if !secretsPass {
		requires = "deny"
	}

	if len(reasons) == 0 {
		reasons = []string{"no issues detected"}
	}

	return EvalResult{
		Risk:     risk,
		Class:    class,
		Requires: requires,
		Reasons:  reasons,
		Checks:   checks,
	}
}

func boolInfo(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
}
