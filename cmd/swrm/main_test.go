package main

import (
	"testing"
)

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
		want     bool
	}{
		{"single match", "fix the bug", []string{"fix"}, true},
		{"second keyword matches", "review code", []string{"fix", "review"}, true},
		{"no match", "hello world", []string{"fix", "bug"}, false},
		{"empty text", "", []string{"fix"}, false},
		{"empty keywords", "fix", nil, false},
		{"substring match", "refactoring code", []string{"refactor"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAny(tt.text, tt.keywords...)
			if got != tt.want {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.text, tt.keywords, got, tt.want)
			}
		})
	}
}

func TestDedupe(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"no duplicates", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"with duplicates", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"all same", []string{"x", "x", "x"}, []string{"x"}},
		{"empty", nil, nil},
		{"single", []string{"a"}, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupe(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("dedupe(%v) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("dedupe(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGoalForRole(t *testing.T) {
	tests := []struct {
		role       string
		parentGoal string
		wantPrefix string
	}{
		{"plan", "fix auth", "analyze and create step-by-step plan for:"},
		{"edit", "fix auth", "implement changes for:"},
		{"revw", "fix auth", "review proposed changes for:"},
		{"test", "fix auth", "verify and test changes for:"},
		{"sec", "fix auth", "security audit for:"},
		{"perf", "fix auth", "performance analysis for:"},
		{"unknown", "fix auth", "unknown:"},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := goalForRole(tt.role, tt.parentGoal)
			if got == "" {
				t.Fatal("goalForRole returned empty string")
			}
			// All should contain the parent goal
			if !containsAny(got, tt.parentGoal) {
				t.Errorf("goalForRole(%q, %q) = %q, does not contain parent goal", tt.role, tt.parentGoal, got)
			}
		})
	}
}

func TestDetermineRoles(t *testing.T) {
	tests := []struct {
		name          string
		goal          string
		explicitRoles []string
		wantContains  []string
		wantFirst     string
	}{
		{
			name:         "refactor goal",
			goal:         "Refactor the auth module",
			wantContains: []string{"plan", "edit", "revw", "test"},
			wantFirst:    "plan",
		},
		{
			name:         "fix bug goal",
			goal:         "Fix the login bug",
			wantContains: []string{"plan", "edit", "test"},
			wantFirst:    "plan",
		},
		{
			name:         "review goal",
			goal:         "Review the API changes",
			wantContains: []string{"plan", "revw", "sec"},
			wantFirst:    "plan",
		},
		{
			name:         "performance goal",
			goal:         "Improve performance of search",
			wantContains: []string{"plan", "perf", "edit"},
			wantFirst:    "plan",
		},
		{
			name:         "security goal",
			goal:         "Check security vulnerabilities",
			wantContains: []string{"plan", "sec", "revw"},
			wantFirst:    "plan",
		},
		{
			name:         "default goal",
			goal:         "add a new feature",
			wantContains: []string{"plan", "edit", "test"},
			wantFirst:    "plan",
		},
		{
			name:          "explicit roles filter",
			goal:          "Refactor the auth module",
			explicitRoles: []string{"edit"},
			wantContains:  []string{"plan", "edit"},
			wantFirst:     "plan",
		},
		{
			name:          "explicit roles always keep plan",
			goal:          "fix a bug",
			explicitRoles: []string{"test"},
			wantContains:  []string{"plan", "test"},
			wantFirst:     "plan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineRoles(tt.goal, tt.explicitRoles)
			if len(got) == 0 {
				t.Fatal("determineRoles returned empty slice")
			}
			if got[0] != tt.wantFirst {
				t.Errorf("first role = %q, want %q", got[0], tt.wantFirst)
			}
			gotSet := make(map[string]bool)
			for _, r := range got {
				gotSet[r] = true
			}
			for _, want := range tt.wantContains {
				if !gotSet[want] {
					t.Errorf("roles %v missing expected role %q", got, want)
				}
			}
		})
	}
}

func TestBuildTaskGraph(t *testing.T) {
	tests := []struct {
		name      string
		goal      string
		roles     []string
		wantTasks int
	}{
		{
			name:      "plan only",
			goal:      "do something",
			roles:     []string{"plan"},
			wantTasks: 1,
		},
		{
			name:      "plan + edit + test",
			goal:      "fix bug",
			roles:     []string{"plan", "edit", "test"},
			wantTasks: 3,
		},
		{
			name:      "plan + edit + revw + test",
			goal:      "refactor",
			roles:     []string{"plan", "edit", "revw", "test"},
			wantTasks: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, graph := buildTaskGraph(tt.goal, tt.roles)
			if len(tasks) != tt.wantTasks {
				t.Fatalf("got %d tasks, want %d", len(tasks), tt.wantTasks)
			}
			// Each task should have an entry in the graph
			for _, task := range tasks {
				if _, ok := graph[task.ID]; !ok {
					t.Errorf("task %s missing from graph", task.ID)
				}
			}
			// Plan tasks should have no deps
			for _, task := range tasks {
				if task.Role == "plan" {
					if len(task.Deps) != 0 {
						t.Errorf("plan task %s has deps %v, want none", task.ID, task.Deps)
					}
				}
			}
			// Specialist tasks should depend on plan
			planIDs := make(map[string]bool)
			for _, task := range tasks {
				if task.Role == "plan" {
					planIDs[task.ID] = true
				}
			}
			for _, task := range tasks {
				if task.Role != "plan" && task.Role != "revw" && task.Role != "test" {
					for _, dep := range task.Deps {
						if !planIDs[dep] {
							t.Errorf("specialist task %s has dep %s which is not a plan task", task.ID, dep)
						}
					}
				}
			}
		})
	}
}

func TestBuildTaskGraphDependencyOrder(t *testing.T) {
	roles := []string{"plan", "edit", "revw", "test"}
	tasks, graph := buildTaskGraph("refactor code", roles)

	// Verify phases: plan -> edit (specialist) -> revw, test (final)
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	// plan task (t1) has no deps
	if len(graph[tasks[0].ID]) != 0 {
		t.Errorf("plan task should have 0 deps, got %d", len(graph[tasks[0].ID]))
	}

	// edit task (t2) depends on plan (t1)
	editDeps := graph[tasks[1].ID]
	if len(editDeps) != 1 || editDeps[0] != tasks[0].ID {
		t.Errorf("edit task deps = %v, want [%s]", editDeps, tasks[0].ID)
	}

	// revw (t3) depends on edit (t2)
	revwDeps := graph[tasks[2].ID]
	if len(revwDeps) != 1 || revwDeps[0] != tasks[1].ID {
		t.Errorf("revw task deps = %v, want [%s]", revwDeps, tasks[1].ID)
	}

	// test (t4) depends on edit (t2)
	testDeps := graph[tasks[3].ID]
	if len(testDeps) != 1 || testDeps[0] != tasks[1].ID {
		t.Errorf("test task deps = %v, want [%s]", testDeps, tasks[1].ID)
	}
}
