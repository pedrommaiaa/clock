// Command swrm is a swarm coordinator that decomposes jobs into sub-tasks.
// It reads a goal JSON from stdin and outputs a task decomposition with dependency graph.
package main

import (
	"fmt"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// swarmInput is the input to the swrm tool.
type swarmInput struct {
	Goal  string   `json:"goal"`
	Repo  string   `json:"repo"`
	Mode  string   `json:"mode"`
	Roles []string `json:"roles,omitempty"`
}

// swarmOutput is the output of the swrm tool.
type swarmOutput struct {
	Tasks []common.SwarmTask    `json:"tasks"`
	Graph map[string][]string   `json:"graph"`
}

func main() {
	var input swarmInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Goal == "" {
		jsonutil.Fatal("goal is required")
	}
	if input.Repo == "" {
		input.Repo = "."
	}

	// Determine specialist roles based on goal keywords
	roles := determineRoles(input.Goal, input.Roles)

	// Build tasks and dependency graph
	tasks, graph := buildTaskGraph(input.Goal, roles)

	output := swarmOutput{
		Tasks: tasks,
		Graph: graph,
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// determineRoles selects specialist roles based on goal keywords.
// If explicit roles are provided, they are used as a filter.
func determineRoles(goal string, explicitRoles []string) []string {
	lower := strings.ToLower(goal)

	var roles []string

	// Always start with plan
	roles = append(roles, "plan")

	// Keyword-based role assignment
	switch {
	case containsAny(lower, "refactor"):
		roles = append(roles, "edit", "revw", "test")
	case containsAny(lower, "fix", "bug", "error"):
		roles = append(roles, "edit", "test")
	case containsAny(lower, "review", "audit"):
		roles = append(roles, "revw", "sec")
	case containsAny(lower, "performance", "speed", "slow"):
		roles = append(roles, "perf", "edit")
	case containsAny(lower, "security", "vulnerability"):
		roles = append(roles, "sec", "revw")
	default:
		roles = append(roles, "edit", "test")
	}

	// If explicit roles were provided, intersect with determined roles
	// but always keep "plan"
	if len(explicitRoles) > 0 {
		allowed := make(map[string]bool)
		for _, r := range explicitRoles {
			allowed[r] = true
		}
		// Always allow plan
		allowed["plan"] = true

		var filtered []string
		for _, r := range roles {
			if allowed[r] {
				filtered = append(filtered, r)
			}
		}
		roles = filtered
	}

	return dedupe(roles)
}

// containsAny checks if text contains any of the given keywords.
func containsAny(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// dedupe removes duplicate strings while preserving order.
func dedupe(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// goalForRole generates a sub-goal description for a specific role.
func goalForRole(role, parentGoal string) string {
	switch role {
	case "plan":
		return fmt.Sprintf("analyze and create step-by-step plan for: %s", parentGoal)
	case "edit":
		return fmt.Sprintf("implement changes for: %s", parentGoal)
	case "revw":
		return fmt.Sprintf("review proposed changes for: %s", parentGoal)
	case "test":
		return fmt.Sprintf("verify and test changes for: %s", parentGoal)
	case "sec":
		return fmt.Sprintf("security audit for: %s", parentGoal)
	case "perf":
		return fmt.Sprintf("performance analysis for: %s", parentGoal)
	default:
		return fmt.Sprintf("%s: %s", role, parentGoal)
	}
}

// buildTaskGraph creates tasks and their dependency graph.
// Strategy:
//   1. plan runs first (no deps)
//   2. specialists (edit, perf, sec) run in parallel after plan
//   3. review/test tasks run last after all specialists
func buildTaskGraph(goal string, roles []string) ([]common.SwarmTask, map[string][]string) {
	var tasks []common.SwarmTask
	graph := make(map[string][]string)

	// Categorize roles into phases
	var planTasks, specialistTasks, finalTasks []string

	for _, role := range roles {
		switch role {
		case "plan":
			planTasks = append(planTasks, role)
		case "revw", "test":
			finalTasks = append(finalTasks, role)
		default:
			specialistTasks = append(specialistTasks, role)
		}
	}

	taskID := 0
	nextID := func() string {
		taskID++
		return fmt.Sprintf("t%d", taskID)
	}

	// Track IDs for dependency linking
	var planIDs []string
	var specialistIDs []string

	// Phase 1: Plan tasks (no dependencies)
	for _, role := range planTasks {
		id := nextID()
		planIDs = append(planIDs, id)
		tasks = append(tasks, common.SwarmTask{
			ID:   id,
			Role: role,
			Goal: goalForRole(role, goal),
			Deps: []string{},
		})
		graph[id] = []string{}
	}

	// Phase 2: Specialist tasks (depend on all plan tasks)
	for _, role := range specialistTasks {
		id := nextID()
		specialistIDs = append(specialistIDs, id)
		deps := make([]string, len(planIDs))
		copy(deps, planIDs)
		tasks = append(tasks, common.SwarmTask{
			ID:   id,
			Role: role,
			Goal: goalForRole(role, goal),
			Deps: deps,
		})
		graph[id] = deps
	}

	// Phase 3: Final tasks (depend on all specialist tasks, or plan tasks if no specialists)
	for _, role := range finalTasks {
		id := nextID()
		var deps []string
		if len(specialistIDs) > 0 {
			deps = make([]string, len(specialistIDs))
			copy(deps, specialistIDs)
		} else {
			deps = make([]string, len(planIDs))
			copy(deps, planIDs)
		}
		tasks = append(tasks, common.SwarmTask{
			ID:   id,
			Role: role,
			Goal: goalForRole(role, goal),
			Deps: deps,
		})
		graph[id] = deps
	}

	return tasks, graph
}
