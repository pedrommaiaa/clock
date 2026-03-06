// Command role is a role prompt and policy loader.
// It reads a role name from stdin JSON and outputs the corresponding RoleSpec.
package main

import (
	"fmt"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// roleInput is the input to the role tool.
type roleInput struct {
	Role string `json:"role"`
}

// rolePolicy defines access policies for a role.
type rolePolicy struct {
	CanWrite bool `json:"can_write"`
	CanRun   bool `json:"can_run"`
	CanNet   bool `json:"can_net"`
}

// roleRubric defines evaluation criteria for a role.
type roleRubric struct {
	Focus    string   `json:"focus"`
	Criteria []string `json:"criteria"`
}

// roleDef holds the full definition of a built-in role.
type roleDef struct {
	System string
	Policy rolePolicy
	Rubric roleRubric
	Tools  []string
}

var builtinRoles = map[string]roleDef{
	"plan": {
		System: "You are a software architect. Analyze the codebase and create a step-by-step plan.",
		Policy: rolePolicy{CanWrite: false, CanRun: false, CanNet: false},
		Rubric: roleRubric{
			Focus: "architecture and planning",
			Criteria: []string{
				"plan covers all aspects of the goal",
				"steps are ordered by dependency",
				"risks and unknowns are identified",
				"scope is clearly bounded",
			},
		},
		Tools: []string{"srch", "slce", "map", "graf", "ctrt"},
	},
	"edit": {
		System: "You are a senior developer. Implement changes following the plan.",
		Policy: rolePolicy{CanWrite: true, CanRun: false, CanNet: false},
		Rubric: roleRubric{
			Focus: "code implementation",
			Criteria: []string{
				"changes match the plan",
				"code follows existing style",
				"no unnecessary modifications",
				"error handling is complete",
			},
		},
		Tools: []string{"srch", "slce", "pack", "llm", "guard", "aply"},
	},
	"revw": {
		System: "You are a code reviewer. Review proposed changes for correctness, style, and safety.",
		Policy: rolePolicy{CanWrite: false, CanRun: false, CanNet: false},
		Rubric: roleRubric{
			Focus: "code review and quality",
			Criteria: []string{
				"correctness of logic",
				"adherence to coding standards",
				"potential edge cases",
				"security implications",
			},
		},
		Tools: []string{"srch", "slce", "guard", "risk"},
	},
	"test": {
		System: "You are a QA engineer. Verify changes work correctly and don't break existing functionality.",
		Policy: rolePolicy{CanWrite: false, CanRun: true, CanNet: false},
		Rubric: roleRubric{
			Focus: "testing and verification",
			Criteria: []string{
				"all changed functionality is tested",
				"existing tests still pass",
				"edge cases are covered",
				"test output is clear",
			},
		},
		Tools: []string{"vrfy", "exec", "srch"},
	},
	"sec": {
		System: "You are a security auditor. Check for vulnerabilities, injection risks, and unsafe patterns.",
		Policy: rolePolicy{CanWrite: false, CanRun: false, CanNet: false},
		Rubric: roleRubric{
			Focus: "security analysis",
			Criteria: []string{
				"input validation is present",
				"no injection vulnerabilities",
				"secrets are not exposed",
				"permissions are least-privilege",
			},
		},
		Tools: []string{"srch", "grep", "guard", "risk"},
	},
	"perf": {
		System: "You are a performance engineer. Profile and optimize for speed and resource usage.",
		Policy: rolePolicy{CanWrite: false, CanRun: true, CanNet: false},
		Rubric: roleRubric{
			Focus: "performance optimization",
			Criteria: []string{
				"bottlenecks are identified",
				"optimizations are measurable",
				"no regression in correctness",
				"resource usage is bounded",
			},
		},
		Tools: []string{"srch", "slce", "exec", "graf"},
	},
}

func main() {
	var input roleInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if input.Role == "" {
		jsonutil.Fatal("role is required")
	}

	def, ok := builtinRoles[input.Role]
	if !ok {
		validRoles := make([]string, 0, len(builtinRoles))
		for k := range builtinRoles {
			validRoles = append(validRoles, k)
		}
		jsonutil.Fatal(fmt.Sprintf("unknown role %q; valid roles: %v", input.Role, validRoles))
	}

	spec := common.RoleSpec{
		Name:   input.Role,
		System: def.System,
		Policy: def.Policy,
		Rubric: def.Rubric,
		Tools:  def.Tools,
	}

	if err := jsonutil.WriteOutput(spec); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
