// Command clock is the main CLI orchestrator for the Clock toolbox.
// It dispatches subcommands (init, ask, fix, start, stop, status, doctor)
// by shelling out to individual tool binaries via os/exec.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
)

// ---------------------------------------------------------------------------
// Tool runner helpers
// ---------------------------------------------------------------------------

// toolDir returns the directory containing the clock binary itself,
// used as a fallback search path for sibling tool binaries.
func toolDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// findTool resolves the path of a tool binary. It checks:
// 1. The same directory as the clock binary
// 2. PATH
func findTool(name string) (string, error) {
	// Check sibling directory first
	if dir := toolDir(); dir != "" {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Fall back to PATH
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("tool %q not found in PATH or alongside clock binary", name)
	}
	return p, nil
}

// runTool executes a tool binary by name, optionally feeding stdinData via
// stdin. It returns the combined stdout bytes. On non-zero exit, an error is
// returned that includes stderr.
func runTool(name string, stdinData []byte, extraArgs ...string) ([]byte, error) {
	bin, err := findTool(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, extraArgs...)
	if len(stdinData) > 0 {
		cmd.Stdin = bytes.NewReader(stdinData)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %v\nstderr: %s", name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// pipe runs tool A, then feeds its stdout into tool B's stdin.
func pipe(toolA string, inputA []byte, toolB string, extraArgsB ...string) ([]byte, error) {
	outA, err := runTool(toolA, inputA)
	if err != nil {
		return nil, fmt.Errorf("pipe %s: %w", toolA, err)
	}
	return runTool(toolB, outA, extraArgsB...)
}

// mustJSON marshals v to JSON bytes, panicking on failure.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json marshal: %v", err))
	}
	return b
}

// outputJSON writes v as pretty-printed JSON to stdout.
func outputJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// fatal prints a JSON error to stderr and exits 1.
func fatal(msg string) {
	fmt.Fprintf(os.Stderr, `{"error": %q}`+"\n", msg)
	os.Exit(1)
}

// traceLog sends a trace event to the trce tool.
func traceLog(event, tool string, data interface{}) {
	ev := common.TraceEvent{
		TS:    time.Now().UnixMilli(),
		Event: event,
		Tool:  tool,
		Data:  data,
	}
	_, _ = runTool("trce", mustJSON(ev))
}

// readDossier reads the .clock/doss.md file if it exists.
func readDossier() string {
	b, err := os.ReadFile(".clock/doss.md")
	if err != nil {
		return ""
	}
	return string(b)
}

// readPolicy reads the .clock/policy.json file if it exists.
func readPolicy() json.RawMessage {
	b, err := os.ReadFile(".clock/policy.json")
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

// ---------------------------------------------------------------------------
// Subcommand: init
// ---------------------------------------------------------------------------

func cmdInit() {
	// 1. Create .clock/ directory structure
	dirs := []string{
		".clock/queue",
		".clock/mem",
		".clock/approvals/inbox",
		".clock/tools",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fatal(fmt.Sprintf("mkdir %s: %v", d, err))
		}
	}

	// 2. Run scan
	scanOut, err := runTool("scan", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: scan: %v\n", err)
	}

	// 3. Run scope
	_, err = runTool("scope", scanOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: scope: %v\n", err)
	}

	// 4. Run map, ctrt, flow (can use scan output)
	_, err = runTool("map", scanOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: map: %v\n", err)
	}

	_, err = runTool("ctrt", scanOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ctrt: %v\n", err)
	}

	_, err = runTool("flow", scanOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: flow: %v\n", err)
	}

	// 5. Run doss to compile dossier
	dossOut, err := runTool("doss", scanOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: doss: %v\n", err)
	}
	if len(dossOut) > 0 {
		_ = os.WriteFile(".clock/doss.md", dossOut, 0o644)
	}

	// 6. Create default policy.json
	policy := map[string]interface{}{
		"max_files":       10,
		"max_lines":       500,
		"forbidden_paths": []string{"*.env", "*.key", "*.pem"},
		"require_context": 3,
		"deny_binary":     true,
	}
	policyBytes, _ := json.MarshalIndent(policy, "", "  ")
	if err := os.WriteFile(".clock/policy.json", append(policyBytes, '\n'), 0o644); err != nil {
		fatal(fmt.Sprintf("write policy.json: %v", err))
	}

	// 7. Touch trce.jsonl
	trcePath := ".clock/trce.jsonl"
	f, err := os.OpenFile(trcePath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(fmt.Sprintf("touch trce.jsonl: %v", err))
	}
	f.Close()

	// 8. Output result
	outputJSON(map[string]interface{}{
		"ok": true,
		"artifacts": []string{
			".clock/doss.md",
			".clock/policy.json",
			".clock/trce.jsonl",
		},
	})
}

// ---------------------------------------------------------------------------
// Subcommand: ask <question>
// ---------------------------------------------------------------------------

func cmdAsk(question string) {
	// 1. Run rfrsh to check dossier freshness
	_, err := runTool("rfrsh", mustJSON(map[string]interface{}{
		"root": ".",
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: rfrsh: %v\n", err)
	}

	// 2. Run srch with the question as query
	srchOut, err := runTool("srch", mustJSON(map[string]string{
		"query": question,
	}))
	if err != nil {
		fatal(fmt.Sprintf("srch: %v", err))
	}

	// 3. Run slce on top search results (first 5)
	var hits []common.SearchHit
	_ = json.Unmarshal(srchOut, &hits)
	if len(hits) > 5 {
		hits = hits[:5]
	}

	var slices []json.RawMessage
	for _, hit := range hits {
		slceIn := mustJSON(map[string]interface{}{
			"path":  hit.Path,
			"line":  hit.Line,
			"above": 10,
			"below": 10,
		})
		slceOut, err := runTool("slce", slceIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: slce %s:%d: %v\n", hit.Path, hit.Line, err)
			continue
		}
		slices = append(slices, json.RawMessage(slceOut))
	}

	// 4. Run pack with dossier + slices + goal
	dossier := readDossier()
	packIn := mustJSON(map[string]interface{}{
		"dossier": dossier,
		"slices":  slices,
		"goal":    question,
	})
	packOut, err := runTool("pack", packIn)
	if err != nil {
		fatal(fmt.Sprintf("pack: %v", err))
	}

	// 5. Run llm with the bundle
	provider := os.Getenv("CLOCK_PROVIDER")
	if provider == "" {
		provider = "anthropic"
	}
	model := os.Getenv("CLOCK_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	var bundle common.PackBundle
	_ = json.Unmarshal(packOut, &bundle)

	llmIn := mustJSON(map[string]interface{}{
		"provider": provider,
		"model":    model,
		"bundle":   bundle,
	})
	llmOut, err := runTool("llm", llmIn)
	if err != nil {
		fatal(fmt.Sprintf("llm: %v", err))
	}

	// 6. Run rpt to format
	rptOut, err := runTool("rpt", llmOut)
	if err != nil {
		// If rpt fails, output the raw llm response
		fmt.Fprintf(os.Stderr, "warning: rpt: %v\n", err)
		os.Stdout.Write(llmOut)
		return
	}

	// 7. Print report to stdout
	os.Stdout.Write(rptOut)
}

// ---------------------------------------------------------------------------
// Subcommand: fix <goal>
// ---------------------------------------------------------------------------

func cmdFix(goal string) {
	const maxIterations = 10

	// 1. Run rfrsh
	_, err := runTool("rfrsh", mustJSON(map[string]interface{}{
		"root": ".",
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: rfrsh: %v\n", err)
	}

	// 2. Run scope
	_, err = runTool("scope", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: scope: %v\n", err)
	}

	// 3. Run dect for verify plan
	dectOut, err := runTool("dect", mustJSON(map[string]string{
		"goal": goal,
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: dect: %v\n", err)
	}
	_ = dectOut

	// 4. Log goal via trce
	traceLog("goal.set", "clock", map[string]string{"goal": goal})

	// Read provider/model config
	provider := os.Getenv("CLOCK_PROVIDER")
	if provider == "" {
		provider = "anthropic"
	}
	model := os.Getenv("CLOCK_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	dossier := readDossier()
	policy := readPolicy()

	var lastFailure string

	// 5. Agent loop
	for iter := 0; iter < maxIterations; iter++ {
		traceLog("loop.iter", "clock", map[string]interface{}{
			"iteration": iter,
			"goal":      goal,
		})

		// a. Run srch with goal (incorporate failure feedback if any)
		searchQuery := goal
		if lastFailure != "" {
			searchQuery = goal + " (previous attempt failed: " + lastFailure + ")"
		}

		srchOut, err := runTool("srch", mustJSON(map[string]string{
			"query": searchQuery,
		}))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: srch: %v\n", err)
			traceLog("tool.err", "srch", map[string]string{"error": err.Error()})
			continue
		}

		// b. Run slce on top results
		var hits []common.SearchHit
		_ = json.Unmarshal(srchOut, &hits)
		if len(hits) > 5 {
			hits = hits[:5]
		}

		var slices []json.RawMessage
		for _, hit := range hits {
			slceIn := mustJSON(map[string]interface{}{
				"path":  hit.Path,
				"line":  hit.Line,
				"above": 10,
				"below": 10,
			})
			slceOut, err := runTool("slce", slceIn)
			if err != nil {
				continue
			}
			slices = append(slices, json.RawMessage(slceOut))
		}

		// c. Run pack with dossier + slices
		packIn := mustJSON(map[string]interface{}{
			"dossier": dossier,
			"slices":  slices,
			"goal":    goal,
			"policy":  policy,
		})
		packOut, err := runTool("pack", packIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: pack: %v\n", err)
			traceLog("tool.err", "pack", map[string]string{"error": err.Error()})
			continue
		}

		// d. Run llm
		var bundle common.PackBundle
		_ = json.Unmarshal(packOut, &bundle)

		llmIn := mustJSON(map[string]interface{}{
			"provider": provider,
			"model":    model,
			"bundle":   bundle,
		})
		llmOut, err := runTool("llm", llmIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: llm: %v\n", err)
			traceLog("tool.err", "llm", map[string]string{"error": err.Error()})
			continue
		}

		// e. Run act to validate
		actOut, err := runTool("act", llmOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: act: %v\n", err)
			traceLog("tool.err", "act", map[string]string{"error": err.Error()})
			continue
		}

		// Parse action output
		var action struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(actOut, &action); err != nil {
			fmt.Fprintf(os.Stderr, "warning: parse act output: %v\n", err)
			traceLog("tool.err", "act.parse", map[string]string{"error": err.Error()})
			continue
		}

		traceLog("act.dispatch", action.Kind, action.Payload)

		// f. Dispatch based on action kind
		switch action.Kind {
		case "srch", "slce":
			// Refine search, continue to next iteration
			traceLog("loop.refine", action.Kind, nil)
			continue

		case "patch":
			// Run guard -> risk -> (eval if high) -> aply -> vrfy
			ok := handlePatch(action.Payload, dossier, policy)
			if !ok {
				lastFailure = "patch verification failed"
				traceLog("loop.patch_fail", "clock", map[string]string{"failure": lastFailure})
				continue
			}
			lastFailure = ""

		case "run":
			// Execute the command
			execOut, err := runTool("exec", action.Payload)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: exec: %v\n", err)
				traceLog("tool.err", "exec", map[string]string{"error": err.Error()})
			} else {
				traceLog("tool.out", "exec", json.RawMessage(execOut))
			}

		case "done":
			// Break the loop
			traceLog("loop.done", "clock", action.Payload)
			goto loopEnd

		default:
			traceLog("loop.unknown_kind", action.Kind, nil)
			continue
		}

		// g. Log step
		traceLog("loop.step_end", "clock", map[string]interface{}{
			"iteration": iter,
			"kind":      action.Kind,
		})
	}

loopEnd:
	// 6. Generate report via rpt
	rptIn := mustJSON(map[string]interface{}{
		"goal":    goal,
		"dossier": dossier,
	})
	rptOut, err := runTool("rpt", rptIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: rpt: %v\n", err)
		fmt.Printf(`{"ok": true, "goal": %q, "message": "fix loop completed"}`, goal)
		fmt.Println()
		return
	}
	os.Stdout.Write(rptOut)
}

// handlePatch runs the guard -> risk -> eval -> aply -> vrfy pipeline.
// Returns true if the patch was applied and verified successfully.
func handlePatch(payload json.RawMessage, dossier string, policy json.RawMessage) bool {
	// Extract the diff from the payload
	var patchData struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(payload, &patchData); err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse patch payload: %v\n", err)
		return false
	}

	// Guard check
	guardIn := mustJSON(map[string]interface{}{
		"diff":   patchData.Diff,
		"policy": json.RawMessage(policy),
	})
	guardOut, err := runTool("guard", guardIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: guard: %v\n", err)
		return false
	}
	traceLog("tool.out", "guard", json.RawMessage(guardOut))

	var guardResult common.GuardResult
	if err := json.Unmarshal(guardOut, &guardResult); err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse guard result: %v\n", err)
		return false
	}

	if !guardResult.OK {
		fmt.Fprintf(os.Stderr, "guard rejected patch: %v\n", guardResult.Reasons)
		traceLog("guard.reject", "guard", guardResult.Reasons)
		return false
	}

	// Risk assessment
	riskIn := mustJSON(map[string]interface{}{
		"diff": patchData.Diff,
		"doss": dossier,
	})
	riskOut, err := runTool("risk", riskIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: risk: %v\n", err)
		return false
	}
	traceLog("tool.out", "risk", json.RawMessage(riskOut))

	var riskResult common.RiskResult
	if err := json.Unmarshal(riskOut, &riskResult); err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse risk result: %v\n", err)
		return false
	}

	// If risk is high, run eval
	if riskResult.Class == "high" {
		evalIn := mustJSON(map[string]interface{}{
			"diff": patchData.Diff,
			"risk": riskResult,
		})
		evalOut, err := runTool("eval", evalIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: eval: %v\n", err)
			return false
		}
		traceLog("tool.out", "eval", json.RawMessage(evalOut))
	}

	// Apply the patch
	aplyOut, err := runTool("aply", mustJSON(map[string]interface{}{
		"diff": patchData.Diff,
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: aply: %v\n", err)
		return false
	}
	traceLog("tool.out", "aply", json.RawMessage(aplyOut))

	var aplyResult common.ApplyResult
	if err := json.Unmarshal(aplyOut, &aplyResult); err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse aply result: %v\n", err)
		return false
	}

	if !aplyResult.OK {
		fmt.Fprintf(os.Stderr, "aply failed: %s\n", aplyResult.Err)
		return false
	}

	// Verify the patch
	vrfyOut, err := runTool("vrfy", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: vrfy: %v\n", err)
		// Verification failed, undo
		undoPatch(aplyResult.ChkID)
		return false
	}
	traceLog("tool.out", "vrfy", json.RawMessage(vrfyOut))

	var vrfyResult common.VerifyResult
	if err := json.Unmarshal(vrfyOut, &vrfyResult); err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse vrfy result: %v\n", err)
		undoPatch(aplyResult.ChkID)
		return false
	}

	if !vrfyResult.OK {
		fmt.Fprintf(os.Stderr, "verification failed: %s\n", vrfyResult.Logs)
		traceLog("vrfy.fail", "vrfy", vrfyResult.Fail)
		undoPatch(aplyResult.ChkID)
		return false
	}

	return true
}

// undoPatch runs the undo tool to revert a patch by checkpoint ID.
func undoPatch(chkID string) {
	undoIn := mustJSON(map[string]interface{}{
		"chk": chkID,
	})
	_, err := runTool("undo", undoIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: undo: %v\n", err)
	}
	traceLog("tool.out", "undo", map[string]string{"chk": chkID})
}

// ---------------------------------------------------------------------------
// Subcommand: start
// ---------------------------------------------------------------------------

func cmdStart() {
	// Find the dock binary
	bin, err := findTool("dock")
	if err != nil {
		fatal(fmt.Sprintf("dock: %v", err))
	}

	// Start dock as a background process
	cmd := exec.Command(bin)
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Detach from parent process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		fatal(fmt.Sprintf("start dock: %v", err))
	}

	pid := cmd.Process.Pid

	// Write PID file
	pidPath := ".clock/dock.pid"
	if err := os.MkdirAll(".clock", 0o755); err != nil {
		fatal(fmt.Sprintf("mkdir .clock: %v", err))
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		fatal(fmt.Sprintf("write pid file: %v", err))
	}

	// Release the process so it runs independently
	_ = cmd.Process.Release()

	outputJSON(map[string]interface{}{
		"ok":     true,
		"pid":    pid,
		"status": "started",
	})
}

// ---------------------------------------------------------------------------
// Subcommand: stop
// ---------------------------------------------------------------------------

func cmdStop() {
	pidPath := ".clock/dock.pid"

	// 1. Read PID file
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fatal(fmt.Sprintf("read pid file: %v (is dock running?)", err))
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fatal(fmt.Sprintf("invalid pid: %v", err))
	}

	// 2. Send SIGTERM
	proc, err := os.FindProcess(pid)
	if err != nil {
		fatal(fmt.Sprintf("find process %d: %v", pid, err))
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "warning: send SIGTERM to %d: %v\n", pid, err)
	}

	// 3. Clean up PID file
	_ = os.Remove(pidPath)

	outputJSON(map[string]interface{}{
		"ok":     true,
		"pid":    pid,
		"status": "stopped",
	})
}

// ---------------------------------------------------------------------------
// Subcommand: status
// ---------------------------------------------------------------------------

func cmdStatus() {
	result := map[string]interface{}{}

	// 1. Check if dock is running
	pidPath := ".clock/dock.pid"
	dockRunning := false
	dockPID := 0
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			dockPID = pid
			// Check if process is alive by sending signal 0
			proc, err := os.FindProcess(pid)
			if err == nil {
				if err := proc.Signal(syscall.Signal(0)); err == nil {
					dockRunning = true
				}
			}
		}
	}
	result["dock"] = map[string]interface{}{
		"running": dockRunning,
		"pid":     dockPID,
	}

	// 2. Run q status
	qOut, err := runTool("q", nil, "status")
	if err != nil {
		result["queue"] = map[string]string{"error": err.Error()}
	} else {
		var qStatus interface{}
		if json.Unmarshal(qOut, &qStatus) == nil {
			result["queue"] = qStatus
		} else {
			result["queue"] = string(qOut)
		}
	}

	// 3. Run mode get
	modeOut, err := runTool("mode", nil, "get")
	if err != nil {
		result["mode"] = map[string]string{"error": err.Error()}
	} else {
		var modeStatus interface{}
		if json.Unmarshal(modeOut, &modeStatus) == nil {
			result["mode"] = modeStatus
		} else {
			result["mode"] = strings.TrimSpace(string(modeOut))
		}
	}

	// 4. Run lease list
	leaseOut, err := runTool("lease", nil, "list")
	if err != nil {
		result["leases"] = map[string]string{"error": err.Error()}
	} else {
		var leaseStatus interface{}
		if json.Unmarshal(leaseOut, &leaseStatus) == nil {
			result["leases"] = leaseStatus
		} else {
			result["leases"] = string(leaseOut)
		}
	}

	outputJSON(result)
}

// ---------------------------------------------------------------------------
// Subcommand: doctor
// ---------------------------------------------------------------------------

func cmdDoctor() {
	deps := []string{"go", "rg", "git", "jq", "sed", "awk", "sqlite3"}
	results := make([]map[string]interface{}, 0, len(deps))

	allOK := true
	for _, dep := range deps {
		entry := map[string]interface{}{
			"name": dep,
		}

		path, err := exec.LookPath(dep)
		if err != nil {
			entry["ok"] = false
			entry["error"] = fmt.Sprintf("not found in PATH")
			allOK = false
		} else {
			entry["ok"] = true
			entry["path"] = path

			// Try to get version
			var versionFlag string
			switch dep {
			case "sed", "awk":
				versionFlag = "--version"
			default:
				versionFlag = "--version"
			}

			cmd := exec.Command(path, versionFlag)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err == nil {
				// Take first line of version output
				version := strings.TrimSpace(out.String())
				if lines := strings.Split(version, "\n"); len(lines) > 0 {
					entry["version"] = lines[0]
				}
			}
		}

		results = append(results, entry)
	}

	outputJSON(map[string]interface{}{
		"ok":           allOK,
		"dependencies": results,
	})
}

// ---------------------------------------------------------------------------
// Subcommand: chat — launch interactive REPL
// ---------------------------------------------------------------------------

func cmdChat() {
	// Find and exec the chat binary
	chatBin, err := findTool("chat")
	if err != nil {
		fatal("chat tool not found — run 'make all' to build it")
	}
	cmd := exec.Command(chatBin, os.Args[2:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `clock - CLI orchestrator for the Clock toolbox

Usage:
  clock <command> [arguments]

Commands:
  init             Initialize .clock/ directory and build project context
  ask <question>   Read-only analysis: search, slice, pack, query LLM
  fix <goal>       Full agent loop: search, plan, patch, verify
  start            Start the dock daemon in the background
  stop             Stop the dock daemon
  status           Show system status (dock, queue, mode, leases)
  chat             Interactive REPL for continuous AI-assisted development
  doctor           Check that all required dependencies are installed`)
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	if len(os.Args) < 2 {
		// No args: launch interactive chat by default
		cmdChat()
		return
	}

	subcmd := os.Args[1]

	switch subcmd {
	case "init":
		cmdInit()

	case "ask":
		if len(os.Args) < 3 {
			fatal("usage: clock ask <question>")
		}
		question := strings.Join(os.Args[2:], " ")
		cmdAsk(question)

	case "fix":
		if len(os.Args) < 3 {
			fatal("usage: clock fix <goal>")
		}
		goal := strings.Join(os.Args[2:], " ")
		cmdFix(goal)

	case "start":
		cmdStart()

	case "stop":
		cmdStop()

	case "status":
		cmdStatus()

	case "chat":
		cmdChat()

	case "doctor":
		cmdDoctor()

	case "help", "-h", "--help":
		printUsage(os.Stdout)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", subcmd)
		printUsage(os.Stderr)
		os.Exit(1)
	}
}
