// Command exec is a policy-based command runner with sandboxing features.
// It reads an ExecInput JSON from stdin, validates the command against an
// optional allowlist, runs it with a timeout, scrubs sensitive env vars,
// and outputs an ExecResult JSON.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// ExecInput is the input schema for the exec tool.
type ExecInput struct {
	Cmd     []string   `json:"cmd"`
	Cwd     string     `json:"cwd,omitempty"`
	Timeout int        `json:"timeout,omitempty"` // seconds
	Allow   *AllowSpec `json:"allow,omitempty"`
}

// AllowSpec defines an allowlist for commands.
type AllowSpec struct {
	Cmds []string `json:"cmds,omitempty"`
}

// sensitiveEnvPrefixes lists env var prefixes that should be scrubbed.
var sensitiveEnvPrefixes = []string{
	"API_KEY", "APIKEY", "API_SECRET",
	"SECRET", "TOKEN", "PASSWORD", "PASSWD",
	"AWS_SECRET", "AWS_SESSION_TOKEN",
	"GITHUB_TOKEN", "GH_TOKEN",
	"OPENAI_API", "ANTHROPIC_API",
	"PRIVATE_KEY", "CREDENTIALS",
}

const maxOutput = 100 * 1024 // 100KB

func main() {
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read stdin: %v", err))
	}

	var input ExecInput
	if err := json.Unmarshal(data, &input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("parse input: %v", err))
	}

	if len(input.Cmd) == 0 {
		jsonutil.Fatal("cmd is required")
	}

	// Validate command against allowlist
	if input.Allow != nil && len(input.Allow.Cmds) > 0 {
		cmdName := input.Cmd[0]
		allowed := false
		for _, a := range input.Allow.Cmds {
			if a == cmdName {
				allowed = true
				break
			}
		}
		if !allowed {
			result := common.ExecResult{
				Code: -1,
				Err:  fmt.Sprintf("command %q not in allowlist %v", cmdName, input.Allow.Cmds),
			}
			jsonutil.WriteOutput(result)
			return
		}
	}

	// Set timeout
	timeout := time.Duration(input.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, input.Cmd[0], input.Cmd[1:]...)

	if input.Cwd != "" {
		cmd.Dir = input.Cwd
	}

	// Scrub sensitive environment variables
	cmd.Env = scrubEnv(os.Environ())

	// Set process group for cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Kill the process group on timeout
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			exitCode = -1
			stderr.WriteString("\n[timeout after " + fmt.Sprintf("%d", input.Timeout) + "s]")
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			stderr.WriteString("\n[exec error: " + err.Error() + "]")
		}
	}

	result := common.ExecResult{
		Code: exitCode,
		Out:  truncate(stdout.String(), maxOutput),
		Err:  truncate(stderr.String(), maxOutput),
		Ms:   elapsed,
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// scrubEnv removes environment variables that look sensitive.
func scrubEnv(env []string) []string {
	var clean []string
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		upper := strings.ToUpper(key)
		sensitive := false
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.Contains(upper, prefix) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			clean = append(clean, e)
		}
	}
	return clean
}

// truncate limits a string to maxLen bytes.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n[truncated]"
}
