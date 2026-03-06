package main

import (
	"strings"
	"testing"
)

func TestRunStep_PassingCommand(t *testing.T) {
	step := runStep("echo_test", "echo hello", 30)
	if !step.OK {
		t.Errorf("expected OK=true for echo, got false; output: %s", step.Output)
	}
	if step.Code != 0 {
		t.Errorf("exit code = %d, want 0", step.Code)
	}
	if !strings.Contains(step.Output, "hello") {
		t.Errorf("output = %q, want it to contain 'hello'", step.Output)
	}
	if step.Name != "echo_test" {
		t.Errorf("name = %q, want %q", step.Name, "echo_test")
	}
	if step.Cmd != "echo hello" {
		t.Errorf("cmd = %q, want %q", step.Cmd, "echo hello")
	}
	if step.Ms < 0 {
		t.Errorf("ms = %d, should be >= 0", step.Ms)
	}
}

func TestRunStep_FailingCommand(t *testing.T) {
	step := runStep("fail_test", "false", 30)
	if step.OK {
		t.Error("expected OK=false for 'false' command")
	}
	if step.Code == 0 {
		t.Error("exit code should be non-zero for 'false'")
	}
}

func TestRunStep_NonExistentCommand(t *testing.T) {
	step := runStep("bad_cmd", "nonexistent_command_xyz_12345", 30)
	if step.OK {
		t.Error("expected OK=false for non-existent command")
	}
}

func TestRunStep_Timeout(t *testing.T) {
	step := runStep("slow", "sleep 60", 1)
	if step.OK {
		t.Error("expected OK=false for timed-out command")
	}
	if step.Code != -1 {
		t.Errorf("exit code = %d, want -1 for timeout", step.Code)
	}
	if !strings.Contains(step.Output, "timeout") {
		t.Errorf("output should mention timeout, got: %q", step.Output)
	}
}

func TestRunStep_CaptureStderr(t *testing.T) {
	step := runStep("stderr_test", "echo error >&2", 30)
	if !strings.Contains(step.Output, "error") {
		t.Errorf("output should capture stderr, got: %q", step.Output)
	}
}

func TestRunStep_MultiCommand(t *testing.T) {
	step := runStep("multi", "echo first && echo second", 30)
	if !step.OK {
		t.Errorf("expected OK=true; output: %s", step.Output)
	}
	if !strings.Contains(step.Output, "first") || !strings.Contains(step.Output, "second") {
		t.Errorf("output should contain both outputs, got: %q", step.Output)
	}
}

func TestRunStep_ExitCode(t *testing.T) {
	step := runStep("exit42", "exit 42", 30)
	if step.OK {
		t.Error("expected OK=false for exit 42")
	}
	if step.Code != 42 {
		t.Errorf("exit code = %d, want 42", step.Code)
	}
}

func TestRunStep_MultiplePassing(t *testing.T) {
	steps := []struct {
		name string
		cmd  string
	}{
		{"fmt", "echo fmt_ok"},
		{"lint", "echo lint_ok"},
		{"test", "echo test_ok"},
	}

	for _, s := range steps {
		step := runStep(s.name, s.cmd, 30)
		if !step.OK {
			t.Errorf("step %q should pass; output: %s", s.name, step.Output)
		}
	}
}

func TestRunStep_BreakOnFirstFailure(t *testing.T) {
	// Simulate the main loop logic: break on first failure
	plan := []string{"pass", "fail", "skip"}
	cmds := map[string]string{
		"pass": "true",
		"fail": "false",
		"skip": "echo should-not-run",
	}

	var steps []string
	for _, name := range plan {
		step := runStep(name, cmds[name], 30)
		steps = append(steps, name)
		if !step.OK {
			break
		}
	}

	if len(steps) != 2 {
		t.Errorf("should have run 2 steps, ran %d: %v", len(steps), steps)
	}
}
