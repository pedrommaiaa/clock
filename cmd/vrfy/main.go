// Command vrfy runs verification checks (fmt, lint, test, etc.) in sequence.
// It reads a VerifyInput JSON from stdin, runs each step with a timeout,
// captures output, and returns a VerifyResult JSON.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// VerifyInput is the input schema for the vrfy tool.
type VerifyInput struct {
	Plan    []string          `json:"plan"`
	Cmds    map[string]string `json:"cmds"`
	Timeout int               `json:"timeout"` // total timeout in seconds
}

const maxLogs = 50 * 1024 // 50KB

func main() {
	var input VerifyInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if len(input.Plan) == 0 {
		jsonutil.Fatal("plan is required")
	}
	if input.Cmds == nil {
		jsonutil.Fatal("cmds is required")
	}
	if input.Timeout <= 0 {
		input.Timeout = 900
	}

	// Calculate per-step timeout
	perStep := input.Timeout / len(input.Plan)
	if perStep < 30 {
		perStep = 30
	}

	result := common.VerifyResult{
		OK: true,
	}
	var allLogs strings.Builder

	for _, stepName := range input.Plan {
		cmdStr, ok := input.Cmds[stepName]
		if !ok {
			step := common.VerifyStep{
				Name: stepName,
				OK:   false,
				Code: -1,
				Output: fmt.Sprintf("no command found for step %q", stepName),
			}
			result.OK = false
			result.Steps = append(result.Steps, step)
			result.Fail = &step
			break
		}

		step := runStep(stepName, cmdStr, perStep)
		result.Steps = append(result.Steps, step)

		// Append to combined logs
		allLogs.WriteString(fmt.Sprintf("=== %s ===\n", stepName))
		allLogs.WriteString(step.Output)
		allLogs.WriteString("\n")

		if !step.OK {
			result.OK = false
			failCopy := step
			result.Fail = &failCopy
			break
		}
	}

	// Truncate combined logs
	logs := allLogs.String()
	if len(logs) > maxLogs {
		logs = logs[:maxLogs] + "\n[truncated]"
	}
	result.Logs = logs

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// runStep runs a single verification step with a timeout.
func runStep(name, cmdStr string, timeoutSec int) common.VerifyStep {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Split the command for shell execution
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	exitCode := 0
	ok := true

	if err != nil {
		ok = false
		if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
			combined.WriteString(fmt.Sprintf("\n[timeout after %ds]", timeoutSec))
		} else if exitErr, isExit := err.(*exec.ExitError); isExit {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			combined.WriteString("\n[exec error: " + err.Error() + "]")
		}
	}

	return common.VerifyStep{
		Name:   name,
		Cmd:    cmdStr,
		OK:     ok,
		Code:   exitCode,
		Output: combined.String(),
		Ms:     elapsed,
	}
}
