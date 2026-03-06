// Command chat is an interactive REPL for continuous AI-assisted development.
// It maintains conversation history, auto-loads project context, routes user
// input through the Clock tool pipeline, and asks for approval before applying
// patches. It is the primary way users interact with Clock.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/common"
)

// ---------------------------------------------------------------------------
// Version
// ---------------------------------------------------------------------------

const version = "1.0"

// ---------------------------------------------------------------------------
// ANSI color helpers
// ---------------------------------------------------------------------------

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorBgRed  = "\033[41m"
)

// ---------------------------------------------------------------------------
// Message type for conversation history
// ---------------------------------------------------------------------------

// Message represents a single chat message in the conversation history.
type Message struct {
	Role    string `json:"role"`    // user, assistant, system, tool
	Content string `json:"content"`
	TS      int64  `json:"ts"`
}

// ---------------------------------------------------------------------------
// Session state
// ---------------------------------------------------------------------------

// Session holds all runtime state for the interactive REPL session.
type Session struct {
	RepoRoot       string
	Dossier        string
	Mode           string
	Model          string
	Provider       string
	History        []Message
	AppliedPatches int
	UndoStack      []string // checkpoint IDs for undo
	TotalTraces    int
	TotalToolCalls int
	StartTime      time.Time

	// mu protects cancelFunc during concurrent signal handling.
	mu         sync.Mutex
	cancelFunc func() // kills the currently-running tool process
}

// ---------------------------------------------------------------------------
// Display helpers
// ---------------------------------------------------------------------------

func printBanner(s *Session) {
	toolCount := countTools()
	fmt.Println()
	fmt.Printf("%sClock v%s%s — %s\n", colorBold, version, colorReset, s.RepoRoot)
	fmt.Printf("Model: %s%s%s | Mode: %s%s%s | Tools: %d\n",
		colorCyan, formatModelShort(s.Provider, s.Model), colorReset,
		colorYellow, s.Mode, colorReset,
		toolCount)
	fmt.Println(strings.Repeat("\u2500", 50))
	fmt.Println()
}

func printInfo(msg string) {
	fmt.Printf("  %s%s%s\n", colorDim, msg, colorReset)
}

func printError(msg string) {
	fmt.Printf("  %s%s%s\n", colorRed, msg, colorReset)
}

func printSuccess(msg string) {
	fmt.Printf("  %s%s%s\n", colorGreen, msg, colorReset)
}

func printToolCall(tool, summary string) {
	fmt.Printf("  %s\u25b8 %s:%s %s\n", colorDim, tool, colorReset, summary)
}

func printDiff(diff string) {
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			fmt.Printf("  %s%s%s\n", colorCyan, line, colorReset)
		case strings.HasPrefix(line, "@@"):
			fmt.Printf("  %s%s%s\n", colorYellow, line, colorReset)
		case strings.HasPrefix(line, "+"):
			fmt.Printf("  %s%s%s\n", colorGreen, line, colorReset)
		case strings.HasPrefix(line, "-"):
			fmt.Printf("  %s%s%s\n", colorRed, line, colorReset)
		case strings.HasPrefix(line, "diff "):
			fmt.Printf("  %s%s%s\n", colorBold, line, colorReset)
		default:
			fmt.Printf("  %s\n", line)
		}
	}
}

func printCode(path, code string) {
	fmt.Printf("  %s\u250c\u2500 %s%s\n", colorCyan, path, colorReset)
	for _, line := range strings.Split(code, "\n") {
		fmt.Printf("  %s\u2502%s %s\n", colorCyan, colorReset, line)
	}
	fmt.Printf("  %s\u2514\u2500%s\n", colorCyan, colorReset)
}

func printBoxedDiff(diff string) {
	// Group diff by file for a nice boxed display.
	lines := strings.Split(diff, "\n")
	inFile := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff "):
			if inFile {
				fmt.Printf("  %s\u2514\u2500%s\n\n", colorCyan, colorReset)
			}
			// Extract filename from diff header.
			parts := strings.Fields(line)
			fname := ""
			for _, p := range parts {
				if strings.HasPrefix(p, "b/") {
					fname = strings.TrimPrefix(p, "b/")
					break
				}
			}
			if fname == "" && len(parts) > 3 {
				fname = parts[len(parts)-1]
			}
			fmt.Printf("  %s\u250c\u2500 %s%s\n", colorCyan, fname, colorReset)
			inFile = true
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			// Skip raw diff headers inside box, we already showed the filename.
			continue
		case strings.HasPrefix(line, "@@"):
			fmt.Printf("  %s\u2502%s  %s%s%s\n", colorCyan, colorReset, colorYellow, line, colorReset)
		case strings.HasPrefix(line, "+"):
			fmt.Printf("  %s\u2502%s  %s%s%s\n", colorCyan, colorReset, colorGreen, line, colorReset)
		case strings.HasPrefix(line, "-"):
			fmt.Printf("  %s\u2502%s  %s%s%s\n", colorCyan, colorReset, colorRed, line, colorReset)
		default:
			if inFile && line != "" {
				fmt.Printf("  %s\u2502%s  %s\n", colorCyan, colorReset, line)
			}
		}
	}
	if inFile {
		fmt.Printf("  %s\u2514\u2500%s\n", colorCyan, colorReset)
	}
}

// prompt displays msg and reads a single-character response (y/n/e/q).
func prompt(msg string) string {
	fmt.Printf("\n  %s%s%s ", colorBold, msg, colorReset)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line))
}

func formatModelShort(provider, model string) string {
	// If model already has a prefix like "anth:", show it as-is.
	if strings.Contains(model, ":") {
		return model
	}
	// Otherwise create prefix:shortname.
	shortProvider := provider
	switch provider {
	case "anthropic":
		shortProvider = "anth"
	case "openai":
		shortProvider = "oai"
	case "ollama":
		shortProvider = "oll"
	}
	// Shorten model name if it contains the date suffix.
	shortModel := model
	if idx := strings.LastIndex(model, "-202"); idx > 0 {
		shortModel = model[:idx]
	}
	return shortProvider + ":" + shortModel
}

func countTools() int {
	dir := toolDir()
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Tool runner helpers (adapted from cmd/clock/main.go)
// ---------------------------------------------------------------------------

// toolDir returns the directory containing the chat binary itself.
func toolDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// findTool resolves the path of a tool binary.
func findTool(name string) (string, error) {
	if dir := toolDir(); dir != "" {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("tool %q not found in PATH or alongside chat binary", name)
	}
	return p, nil
}

// runTool executes a tool binary by name. The session's cancelFunc is set so
// that SIGINT can kill an in-progress tool invocation.
func (s *Session) runTool(name string, stdinData []byte, extraArgs ...string) ([]byte, error) {
	bin, err := findTool(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, extraArgs...)
	cmd.Dir = s.RepoRoot
	if len(stdinData) > 0 {
		cmd.Stdin = bytes.NewReader(stdinData)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set the cancel function so SIGINT can kill this process.
	s.mu.Lock()
	s.cancelFunc = func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	s.mu.Unlock()

	err = cmd.Run()

	// Clear the cancel function.
	s.mu.Lock()
	s.cancelFunc = nil
	s.mu.Unlock()

	s.TotalToolCalls++

	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %v\nstderr: %s", name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// runToolSilent executes a tool without affecting the cancel function.
func runToolSilent(name string, stdinData []byte, repoRoot string, extraArgs ...string) ([]byte, error) {
	bin, err := findTool(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, extraArgs...)
	cmd.Dir = repoRoot
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

// mustJSON marshals v to JSON bytes.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json marshal: %v", err))
	}
	return b
}

// traceLog sends a trace event to the trce tool.
func (s *Session) traceLog(event, tool string, data interface{}) {
	ev := common.TraceEvent{
		TS:    time.Now().UnixMilli(),
		Event: event,
		Tool:  tool,
		Data:  data,
	}
	_, _ = runToolSilent("trce", mustJSON(ev), s.RepoRoot)
	s.TotalTraces++
}

// ---------------------------------------------------------------------------
// Repo detection
// ---------------------------------------------------------------------------

// findRepoRoot walks up from cwd looking for .git, go.mod, or package.json.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		for _, marker := range []string{".git", "go.mod", "package.json"} {
			candidate := filepath.Join(dir, marker)
			if _, err := os.Stat(candidate); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fall back to cwd.
	cwd, _ := os.Getwd()
	return cwd, nil
}

// ---------------------------------------------------------------------------
// Config loading
// ---------------------------------------------------------------------------

// loadDossier reads .clock/doss.md from the repo root.
func loadDossier(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".clock", "doss.md"))
	if err != nil {
		return ""
	}
	return string(b)
}

// loadMode reads .clock/mode.json and returns the mode string.
func loadMode(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".clock", "mode.json"))
	if err != nil {
		return "suggest"
	}
	var m struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(b, &m) != nil || m.Mode == "" {
		return "suggest"
	}
	return m.Mode
}

// resolveModel determines provider and model from mset, env vars, or defaults.
func resolveModel(root string) (provider, model string) {
	// 1. Check env vars first.
	provider = os.Getenv("CLOCK_PROVIDER")
	model = os.Getenv("CLOCK_MODEL")

	// 2. Try mset resolve for the "chat" role.
	if model == "" {
		out, err := runToolSilent("mset", nil, root, "resolve", "chat")
		if err == nil {
			resolved := strings.TrimSpace(string(out))
			if resolved != "" {
				model = resolved
			}
		}
	}

	// 3. Try mset resolve for the "default" role.
	if model == "" {
		out, err := runToolSilent("mset", nil, root, "resolve", "default")
		if err == nil {
			resolved := strings.TrimSpace(string(out))
			if resolved != "" {
				model = resolved
			}
		}
	}

	// 4. Defaults.
	if provider == "" {
		provider = "anthropic"
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// If model has a prefix like "anth:", extract provider from it.
	if idx := strings.Index(model, ":"); idx > 0 {
		prefix := model[:idx]
		switch prefix {
		case "anth":
			provider = "anthropic"
		case "oai":
			provider = "openai"
		case "oll":
			provider = "ollama"
		case "vllm":
			provider = "vllm"
		case "lcpp":
			provider = "llama-cpp"
		}
		model = model[idx+1:]
	}

	return provider, model
}

// ---------------------------------------------------------------------------
// History management
// ---------------------------------------------------------------------------

// addMessage appends a message to the in-memory history and persists it.
func (s *Session) addMessage(role, content string) {
	msg := Message{
		Role:    role,
		Content: content,
		TS:      time.Now().UnixMilli(),
	}
	s.History = append(s.History, msg)

	// Persist to chat_history.jsonl (append-only).
	histPath := filepath.Join(s.RepoRoot, ".clock", "chat_history.jsonl")
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}

// recentHistory returns the last N messages for context-window inclusion.
func (s *Session) recentHistory(n int) []common.Message {
	start := 0
	if len(s.History) > n {
		start = len(s.History) - n
	}
	msgs := make([]common.Message, 0, len(s.History)-start)
	for _, m := range s.History[start:] {
		msgs = append(msgs, common.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return msgs
}

// clearHistory clears in-memory history (does not delete persisted file).
func (s *Session) clearHistory() {
	s.History = nil
}

// ---------------------------------------------------------------------------
// Intent classification
// ---------------------------------------------------------------------------

// changeVerbs are keywords that indicate a change intent.
var changeVerbs = []string{
	"fix", "change", "add", "remove", "update", "refactor",
	"implement", "create", "delete", "rename", "move",
	"modify", "replace", "insert", "rewrite", "patch",
	"write", "build", "migrate", "convert", "extract",
}

// classifyIntent returns "change" or "question" based on simple heuristics.
func classifyIntent(input string) string {
	lower := strings.ToLower(input)
	words := strings.Fields(lower)
	for _, w := range words {
		for _, verb := range changeVerbs {
			if w == verb {
				return "change"
			}
		}
	}
	return "question"
}

// ---------------------------------------------------------------------------
// Core operations: search + slice retrieval
// ---------------------------------------------------------------------------

// retrieveContext runs srch and slce to gather relevant code context.
func (s *Session) retrieveContext(query string) ([]json.RawMessage, []common.SearchHit) {
	printInfo("Searching codebase...")

	srchOut, err := s.runTool("srch", mustJSON(map[string]string{
		"query": query,
	}))
	if err != nil {
		printError(fmt.Sprintf("search: %v", err))
		return nil, nil
	}

	var hits []common.SearchHit
	if err := json.Unmarshal(srchOut, &hits); err != nil {
		// Try parsing as a wrapper object.
		var wrapper struct {
			Hits []common.SearchHit `json:"hits"`
		}
		if json.Unmarshal(srchOut, &wrapper) == nil {
			hits = wrapper.Hits
		}
	}

	if len(hits) > 0 {
		printToolCall("srch", fmt.Sprintf("Found %d matches", len(hits)))
		s.traceLog("tool.out", "srch", map[string]interface{}{
			"hits": len(hits),
		})
	} else {
		printInfo("No search results found.")
		return nil, nil
	}

	// Slice top 5 results.
	top := hits
	if len(top) > 5 {
		top = top[:5]
	}

	var slices []json.RawMessage
	for _, hit := range top {
		slceIn := mustJSON(map[string]interface{}{
			"path":  hit.Path,
			"line":  hit.Line,
			"above": 10,
			"below": 10,
		})
		slceOut, err := s.runTool("slce", slceIn)
		if err != nil {
			continue
		}
		slices = append(slices, json.RawMessage(slceOut))
		printToolCall("slce", fmt.Sprintf("%s:%d", hit.Path, hit.Line))
	}

	return slices, hits
}

// ---------------------------------------------------------------------------
// Question handler (read-only)
// ---------------------------------------------------------------------------

func (s *Session) handleQuestion(input string) {
	s.addMessage("user", input)
	s.traceLog("chat.question", "chat", map[string]string{"input": input})

	// 1. Retrieve context.
	slices, hits := s.retrieveContext(input)

	// 2. Build pack input.
	printInfo("Thinking...")
	packIn := mustJSON(map[string]interface{}{
		"dossier":  s.Dossier,
		"slices":   slices,
		"goal":     input,
		"messages": s.recentHistory(10),
	})
	packOut, err := s.runTool("pack", packIn)
	if err != nil {
		printError(fmt.Sprintf("pack: %v", err))
		return
	}
	printToolCall("pack", "Built prompt bundle")

	// 3. Run LLM.
	var bundle common.PackBundle
	_ = json.Unmarshal(packOut, &bundle)

	llmIn := mustJSON(map[string]interface{}{
		"provider": s.Provider,
		"model":    s.Model,
		"bundle":   bundle,
	})

	s.traceLog("llm.in", "llm", map[string]interface{}{
		"provider": s.Provider,
		"model":    s.Model,
		"goal":     input,
	})

	llmOut, err := s.runTool("llm", llmIn)
	if err != nil {
		printError(fmt.Sprintf("llm: %v", err))
		return
	}

	s.traceLog("llm.out", "llm", map[string]interface{}{
		"bytes": len(llmOut),
	})

	// 4. Parse and display the response.
	answer := extractAnswer(llmOut)
	fmt.Println()
	fmt.Printf("  %s\n", answer)

	// Show file references if we had hits.
	if len(hits) > 0 {
		fmt.Println()
		printInfo("References:")
		shown := 0
		for _, hit := range hits {
			if shown >= 5 {
				break
			}
			printInfo(fmt.Sprintf("  %s:%d", hit.Path, hit.Line))
			shown++
		}
	}

	fmt.Println()
	s.addMessage("assistant", answer)
}

// extractAnswer pulls the answer text out of an LLM response.
func extractAnswer(data []byte) string {
	// Try ActionEnvelope first.
	var env common.ActionEnvelope
	if json.Unmarshal(data, &env) == nil && env.Answer != "" {
		return env.Answer
	}

	// Try a generic content field.
	var generic struct {
		Content string `json:"content"`
		Text    string `json:"text"`
		Answer  string `json:"answer"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &generic) == nil {
		if generic.Content != "" {
			return generic.Content
		}
		if generic.Text != "" {
			return generic.Text
		}
		if generic.Answer != "" {
			return generic.Answer
		}
		if generic.Message != "" {
			return generic.Message
		}
	}

	// Fall back to raw text (strip JSON wrapper if it looks like one).
	text := strings.TrimSpace(string(data))
	if len(text) > 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var s string
		if json.Unmarshal(data, &s) == nil {
			return s
		}
	}
	return text
}

// ---------------------------------------------------------------------------
// Change handler (agent loop)
// ---------------------------------------------------------------------------

func (s *Session) handleChange(input string) {
	s.addMessage("user", input)
	s.traceLog("chat.change", "chat", map[string]string{"input": input})

	const maxIterations = 10
	var lastFailure string

	for iter := 0; iter < maxIterations; iter++ {
		s.traceLog("loop.iter", "chat", map[string]interface{}{
			"iteration": iter,
			"goal":      input,
		})

		// 1. Retrieve context.
		searchQuery := input
		if lastFailure != "" {
			searchQuery = input + " (previous attempt failed: " + lastFailure + ")"
		}
		slices, _ := s.retrieveContext(searchQuery)

		// 2. Pack.
		printInfo("Planning changes...")
		packIn := mustJSON(map[string]interface{}{
			"dossier":  s.Dossier,
			"slices":   slices,
			"goal":     input,
			"messages": s.recentHistory(10),
		})
		if lastFailure != "" {
			packIn = mustJSON(map[string]interface{}{
				"dossier":  s.Dossier,
				"slices":   slices,
				"goal":     input,
				"messages": s.recentHistory(10),
				"feedback": lastFailure,
			})
		}
		packOut, err := s.runTool("pack", packIn)
		if err != nil {
			printError(fmt.Sprintf("pack: %v", err))
			return
		}
		printToolCall("pack", "Built prompt bundle")

		// 3. LLM.
		var bundle common.PackBundle
		_ = json.Unmarshal(packOut, &bundle)

		llmIn := mustJSON(map[string]interface{}{
			"provider": s.Provider,
			"model":    s.Model,
			"bundle":   bundle,
		})

		s.traceLog("llm.in", "llm", map[string]interface{}{
			"provider":  s.Provider,
			"model":     s.Model,
			"iteration": iter,
		})

		llmOut, err := s.runTool("llm", llmIn)
		if err != nil {
			printError(fmt.Sprintf("llm: %v", err))
			return
		}
		s.traceLog("llm.out", "llm", map[string]interface{}{
			"bytes": len(llmOut),
		})

		// 4. Act (validate and parse).
		actOut, err := s.runTool("act", llmOut)
		if err != nil {
			printError(fmt.Sprintf("act: %v", err))
			return
		}
		printToolCall("act", "Validated LLM action")

		var action struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(actOut, &action); err != nil {
			printError(fmt.Sprintf("parse act output: %v", err))
			return
		}

		s.traceLog("act.dispatch", action.Kind, action.Payload)

		// 5. Dispatch.
		switch action.Kind {
		case "tool", "srch", "slce":
			// The LLM wants more context; continue the loop.
			printInfo("Refining search...")
			continue

		case "patch":
			result := s.handlePatch(action.Payload)
			switch result {
			case "applied":
				lastFailure = ""
				// Check if there are more iterations needed or we're done.
				// Show summary and break.
				printSuccess("Changes applied successfully.")
				fmt.Println()
				s.addMessage("assistant", "Changes applied successfully for: "+input)
				return
			case "rejected":
				lastFailure = "user rejected the patch"
				printInfo("Trying a different approach...")
				continue
			case "aborted":
				printInfo("Change aborted.")
				fmt.Println()
				s.addMessage("assistant", "Change aborted by user.")
				return
			case "failed":
				lastFailure = "patch application or verification failed"
				printInfo("Patch failed, retrying...")
				continue
			}

		case "run":
			var runData struct {
				Cmd string `json:"cmd"`
			}
			_ = json.Unmarshal(action.Payload, &runData)
			printToolCall("exec", runData.Cmd)
			execOut, err := s.runTool("exec", action.Payload)
			if err != nil {
				printError(fmt.Sprintf("exec: %v", err))
				s.traceLog("tool.err", "exec", map[string]string{"error": err.Error()})
			} else {
				var execResult common.ExecResult
				if json.Unmarshal(execOut, &execResult) == nil {
					if execResult.Out != "" {
						printInfo(execResult.Out)
					}
					if execResult.Err != "" {
						printError(execResult.Err)
					}
				}
				s.traceLog("tool.out", "exec", json.RawMessage(execOut))
			}
			continue

		case "done":
			var doneData struct {
				Answer string `json:"answer"`
			}
			if json.Unmarshal(action.Payload, &doneData) == nil && doneData.Answer != "" {
				fmt.Println()
				fmt.Printf("  %s\n\n", doneData.Answer)
				s.addMessage("assistant", doneData.Answer)
			} else {
				answer := extractAnswer(action.Payload)
				if answer != "" {
					fmt.Println()
					fmt.Printf("  %s\n\n", answer)
					s.addMessage("assistant", answer)
				}
			}
			return

		default:
			printInfo(fmt.Sprintf("Unknown action kind: %s", action.Kind))
			continue
		}
	}

	printError("Reached maximum iterations without completing the change.")
	fmt.Println()
	s.addMessage("assistant", "Failed to complete change after maximum iterations: "+input)
}

// handlePatch processes a patch action: display diff, ask for approval,
// guard -> apply -> verify. Returns "applied", "rejected", "aborted", or "failed".
func (s *Session) handlePatch(payload json.RawMessage) string {
	var patchData struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(payload, &patchData); err != nil {
		printError(fmt.Sprintf("parse patch: %v", err))
		return "failed"
	}

	if patchData.Diff == "" {
		// Try the payload as an ActionEnvelope.
		var env common.ActionEnvelope
		if json.Unmarshal(payload, &env) == nil && env.Diff != "" {
			patchData.Diff = env.Diff
		} else {
			printError("Empty diff in patch action.")
			return "failed"
		}
	}

	// Display the proposed changes.
	fmt.Println()
	printInfo("Proposed changes:")
	printBoxedDiff(patchData.Diff)

	// Ask for approval.
	answer := prompt("Apply changes? [y/n/e(dit)/q(uit)]>")

	switch answer {
	case "y", "yes":
		return s.applyPatch(patchData.Diff)
	case "n", "no":
		return "rejected"
	case "e", "edit":
		return s.editAndApplyPatch(patchData.Diff)
	case "q", "quit":
		return "aborted"
	default:
		printInfo("Invalid response. Skipping patch.")
		return "rejected"
	}
}

// applyPatch runs the guard -> aply -> vrfy pipeline.
func (s *Session) applyPatch(diff string) string {
	// 1. Guard.
	printInfo("Running safety checks...")
	policyBytes, _ := os.ReadFile(filepath.Join(s.RepoRoot, ".clock", "policy.json"))
	guardIn := mustJSON(map[string]interface{}{
		"diff":   diff,
		"policy": json.RawMessage(policyBytes),
	})
	guardOut, err := s.runTool("guard", guardIn)
	if err != nil {
		printError(fmt.Sprintf("guard: %v", err))
		return "failed"
	}
	printToolCall("guard", "Safety check")
	s.traceLog("tool.out", "guard", json.RawMessage(guardOut))

	var guardResult common.GuardResult
	if err := json.Unmarshal(guardOut, &guardResult); err != nil {
		printError(fmt.Sprintf("parse guard result: %v", err))
		return "failed"
	}
	if !guardResult.OK {
		printError(fmt.Sprintf("Guard rejected patch: %s", strings.Join(guardResult.Reasons, "; ")))
		s.traceLog("guard.reject", "guard", guardResult.Reasons)
		return "failed"
	}
	printToolCall("guard", "Passed")

	// 2. Apply.
	printInfo("Applying patch...")
	aplyOut, err := s.runTool("aply", mustJSON(map[string]interface{}{
		"diff": diff,
	}))
	if err != nil {
		printError(fmt.Sprintf("aply: %v", err))
		return "failed"
	}
	s.traceLog("tool.out", "aply", json.RawMessage(aplyOut))

	var aplyResult common.ApplyResult
	if err := json.Unmarshal(aplyOut, &aplyResult); err != nil {
		printError(fmt.Sprintf("parse aply result: %v", err))
		return "failed"
	}
	if !aplyResult.OK {
		printError(fmt.Sprintf("Apply failed: %s", aplyResult.Err))
		return "failed"
	}
	printToolCall("aply", fmt.Sprintf("+%d/-%d lines in %d files",
		aplyResult.Lines.Add, aplyResult.Lines.Del, len(aplyResult.Files)))

	// Push checkpoint for undo.
	if aplyResult.ChkID != "" {
		s.UndoStack = append(s.UndoStack, aplyResult.ChkID)
	}

	// 3. Verify.
	printInfo("Running verification...")
	vrfyOut, err := s.runTool("vrfy", nil)
	if err != nil {
		printError(fmt.Sprintf("vrfy: %v", err))
		s.undoLast()
		return "failed"
	}
	s.traceLog("tool.out", "vrfy", json.RawMessage(vrfyOut))

	var vrfyResult common.VerifyResult
	if err := json.Unmarshal(vrfyOut, &vrfyResult); err != nil {
		printError(fmt.Sprintf("parse vrfy result: %v", err))
		s.undoLast()
		return "failed"
	}

	if !vrfyResult.OK {
		printError("Verification failed!")
		if vrfyResult.Fail != nil {
			printError(fmt.Sprintf("  %s: exit %d", vrfyResult.Fail.Name, vrfyResult.Fail.Code))
			if vrfyResult.Fail.Output != "" {
				lines := strings.Split(vrfyResult.Fail.Output, "\n")
				for _, l := range lines {
					if len(l) > 120 {
						l = l[:120] + "..."
					}
					printError("  " + l)
				}
			}
		}
		printInfo("Reverting changes...")
		s.undoLast()
		s.traceLog("vrfy.fail", "vrfy", vrfyResult.Fail)
		return "failed"
	}

	// Display verification summary.
	passed := 0
	for _, step := range vrfyResult.Steps {
		if step.OK {
			passed++
		}
	}
	printSuccess(fmt.Sprintf("All tests pass (%d/%d)", passed, len(vrfyResult.Steps)))

	s.AppliedPatches++
	s.traceLog("patch.applied", "chat", map[string]interface{}{
		"files":  aplyResult.Files,
		"add":    aplyResult.Lines.Add,
		"del":    aplyResult.Lines.Del,
		"chk_id": aplyResult.ChkID,
	})

	return "applied"
}

// editAndApplyPatch opens the diff in $EDITOR, then applies the edited version.
func (s *Session) editAndApplyPatch(diff string) string {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Write diff to a temp file.
	tmpFile, err := os.CreateTemp("", "clock-diff-*.patch")
	if err != nil {
		printError(fmt.Sprintf("create temp file: %v", err))
		return "failed"
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(diff); err != nil {
		tmpFile.Close()
		printError(fmt.Sprintf("write temp file: %v", err))
		return "failed"
	}
	tmpFile.Close()

	// Open editor.
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		printError(fmt.Sprintf("editor: %v", err))
		return "failed"
	}

	// Read edited diff.
	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		printError(fmt.Sprintf("read edited diff: %v", err))
		return "failed"
	}

	editedDiff := strings.TrimSpace(string(edited))
	if editedDiff == "" {
		printInfo("Empty diff after editing. Skipping.")
		return "rejected"
	}

	return s.applyPatch(editedDiff)
}

// undoLast reverts the last applied patch using the undo tool.
func (s *Session) undoLast() {
	if len(s.UndoStack) == 0 {
		printError("Nothing to undo.")
		return
	}
	chkID := s.UndoStack[len(s.UndoStack)-1]
	s.UndoStack = s.UndoStack[:len(s.UndoStack)-1]

	undoIn := mustJSON(map[string]interface{}{
		"chk": chkID,
	})
	_, err := s.runTool("undo", undoIn)
	if err != nil {
		printError(fmt.Sprintf("undo: %v", err))
	} else {
		printSuccess("Reverted to checkpoint " + chkID)
		s.traceLog("tool.out", "undo", map[string]string{"chk": chkID})
	}
}

// ---------------------------------------------------------------------------
// Slash command handlers
// ---------------------------------------------------------------------------

func (s *Session) cmdHelp() {
	fmt.Println()
	fmt.Println("  Available commands:")
	fmt.Println()
	fmt.Printf("  %s/help%s                  Show this help\n", colorBold, colorReset)
	fmt.Printf("  %s/ask <question>%s        Read-only question\n", colorBold, colorReset)
	fmt.Printf("  %s/fix <goal>%s            Request a code change\n", colorBold, colorReset)
	fmt.Printf("  %s/search <query>%s        Search the codebase\n", colorBold, colorReset)
	fmt.Printf("  %s/read <file> [s:e]%s     Read a file or line range\n", colorBold, colorReset)
	fmt.Printf("  %s/diff%s                  Show uncommitted changes\n", colorBold, colorReset)
	fmt.Printf("  %s/undo%s                  Revert last applied patch\n", colorBold, colorReset)
	fmt.Printf("  %s/run <cmd>%s             Execute a shell command\n", colorBold, colorReset)
	fmt.Printf("  %s/mode [mode]%s           Get or set autonomy mode\n", colorBold, colorReset)
	fmt.Printf("  %s/status%s                Show session statistics\n", colorBold, colorReset)
	fmt.Printf("  %s/clear%s                 Clear conversation history\n", colorBold, colorReset)
	fmt.Printf("  %s/model [name]%s          Get or set current model\n", colorBold, colorReset)
	fmt.Printf("  %s/trace%s                 Show recent trace events\n", colorBold, colorReset)
	fmt.Printf("  %s/init%s                  Re-initialize .clock/\n", colorBold, colorReset)
	fmt.Printf("  %s/refresh%s               Force dossier refresh\n", colorBold, colorReset)
	fmt.Printf("  %s/quit%s or %s/exit%s          Exit\n", colorBold, colorReset, colorBold, colorReset)
	fmt.Println()
	fmt.Println("  Type natural language to ask questions or request changes.")
	fmt.Println("  Change-related keywords (fix, add, remove, etc.) trigger the agent loop.")
	fmt.Println()
}

func (s *Session) cmdSearch(query string) {
	if query == "" {
		printError("Usage: /search <query>")
		return
	}

	s.traceLog("cmd.search", "chat", map[string]string{"query": query})

	srchOut, err := s.runTool("srch", mustJSON(map[string]string{
		"query": query,
	}))
	if err != nil {
		printError(fmt.Sprintf("search: %v", err))
		return
	}

	var hits []common.SearchHit
	if err := json.Unmarshal(srchOut, &hits); err != nil {
		var wrapper struct {
			Hits []common.SearchHit `json:"hits"`
		}
		if json.Unmarshal(srchOut, &wrapper) == nil {
			hits = wrapper.Hits
		}
	}

	if len(hits) == 0 {
		printInfo("No results found.")
		return
	}

	fmt.Println()
	for i, hit := range hits {
		if i >= 20 {
			printInfo(fmt.Sprintf("... and %d more", len(hits)-20))
			break
		}
		score := ""
		if hit.Score > 0 {
			score = fmt.Sprintf(" (%.2f)", hit.Score)
		}
		fmt.Printf("  %s%s:%d%s%s  %s\n",
			colorCyan, hit.Path, hit.Line, colorReset, score,
			strings.TrimSpace(hit.Text))
	}
	fmt.Println()
}

func (s *Session) cmdRead(args string) {
	if args == "" {
		printError("Usage: /read <file> [start:end]")
		return
	}

	parts := strings.Fields(args)
	filePath := parts[0]

	// Resolve relative to repo root.
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.RepoRoot, filePath)
	}

	// Check for line range.
	startLine := 0
	endLine := 0
	if len(parts) > 1 {
		rangeParts := strings.SplitN(parts[1], ":", 2)
		if len(rangeParts) == 2 {
			startLine, _ = strconv.Atoi(rangeParts[0])
			endLine, _ = strconv.Atoi(rangeParts[1])
		}
	}

	// Use slce if we have a line range, otherwise read the file directly.
	if startLine > 0 && endLine > 0 {
		slceIn := mustJSON(map[string]interface{}{
			"path":  filePath,
			"start": startLine,
			"end":   endLine,
		})
		slceOut, err := s.runTool("slce", slceIn)
		if err != nil {
			printError(fmt.Sprintf("slce: %v", err))
			return
		}
		var result common.SliceResult
		if json.Unmarshal(slceOut, &result) == nil {
			printCode(result.Path, result.Text)
		} else {
			fmt.Println(string(slceOut))
		}
	} else {
		data, err := os.ReadFile(filePath)
		if err != nil {
			printError(fmt.Sprintf("read: %v", err))
			return
		}
		printCode(filePath, string(data))
	}
}

func (s *Session) cmdDiff() {
	// Run git diff in the repo root.
	cmd := exec.Command("git", "diff")
	cmd.Dir = s.RepoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// Also try git diff --cached for staged changes.
		cmd2 := exec.Command("git", "diff", "--cached")
		cmd2.Dir = s.RepoRoot
		var out2 bytes.Buffer
		cmd2.Stdout = &out2
		if err2 := cmd2.Run(); err2 != nil {
			printError(fmt.Sprintf("git diff: %v", err))
			return
		}
		if out2.Len() > 0 {
			fmt.Println()
			printInfo("Staged changes:")
			printDiff(out2.String())
			return
		}
		printInfo("No uncommitted changes.")
		return
	}

	diffText := out.String()
	if diffText == "" {
		// Check staged changes too.
		cmd2 := exec.Command("git", "diff", "--cached")
		cmd2.Dir = s.RepoRoot
		var out2 bytes.Buffer
		cmd2.Stdout = &out2
		if cmd2.Run() == nil && out2.Len() > 0 {
			fmt.Println()
			printInfo("Staged changes:")
			printDiff(out2.String())
			return
		}
		printInfo("No uncommitted changes.")
		return
	}

	fmt.Println()
	printDiff(diffText)
}

func (s *Session) cmdUndo() {
	if len(s.UndoStack) == 0 {
		printInfo("No patches to undo.")
		return
	}
	s.undoLast()
	s.AppliedPatches--
	if s.AppliedPatches < 0 {
		s.AppliedPatches = 0
	}
}

func (s *Session) cmdRun(cmdStr string) {
	if cmdStr == "" {
		printError("Usage: /run <command>")
		return
	}

	s.traceLog("cmd.run", "chat", map[string]string{"cmd": cmdStr})

	execIn := mustJSON(map[string]interface{}{
		"cmd": cmdStr,
	})
	execOut, err := s.runTool("exec", execIn)
	if err != nil {
		printError(fmt.Sprintf("exec: %v", err))
		return
	}

	var result common.ExecResult
	if err := json.Unmarshal(execOut, &result); err != nil {
		fmt.Println(string(execOut))
		return
	}

	if result.Out != "" {
		fmt.Println()
		fmt.Print(result.Out)
		if !strings.HasSuffix(result.Out, "\n") {
			fmt.Println()
		}
	}
	if result.Err != "" {
		printError(result.Err)
	}
	if result.Code != 0 {
		printInfo(fmt.Sprintf("exit code: %d (%dms)", result.Code, result.Ms))
	}
}

func (s *Session) cmdMode(args string) {
	if args == "" {
		// Get current mode.
		fmt.Printf("  Mode: %s%s%s\n", colorYellow, s.Mode, colorReset)
		return
	}

	// Set mode via the mode tool.
	_, err := s.runTool("mode", nil, "set", args)
	if err != nil {
		printError(fmt.Sprintf("mode set: %v", err))
		return
	}
	s.Mode = args
	printSuccess(fmt.Sprintf("Mode set to: %s", args))
	s.traceLog("cmd.mode", "chat", map[string]string{"mode": args})
}

func (s *Session) cmdStatus() {
	elapsed := time.Since(s.StartTime).Round(time.Second)
	fmt.Println()
	fmt.Printf("  Mode:         %s%s%s\n", colorYellow, s.Mode, colorReset)
	fmt.Printf("  Model:        %s%s%s\n", colorCyan, formatModelShort(s.Provider, s.Model), colorReset)
	fmt.Printf("  Repo:         %s\n", s.RepoRoot)
	fmt.Printf("  Session:      %s\n", elapsed)
	fmt.Printf("  Messages:     %d\n", len(s.History))
	fmt.Printf("  Applied:      %d\n", s.AppliedPatches)
	fmt.Printf("  Pending undo: %d\n", len(s.UndoStack))
	fmt.Printf("  Tool calls:   %d\n", s.TotalToolCalls)
	fmt.Printf("  Traces:       %d\n", s.TotalTraces)
	fmt.Println()
}

func (s *Session) cmdClear() {
	s.clearHistory()
	printSuccess("Conversation history cleared.")
}

func (s *Session) cmdModel(args string) {
	if args == "" {
		fmt.Printf("  Model: %s%s%s\n", colorCyan, formatModelShort(s.Provider, s.Model), colorReset)
		fmt.Printf("  Provider: %s\n", s.Provider)
		fmt.Printf("  Full model: %s\n", s.Model)
		return
	}

	// Parse provider:model format.
	newModel := args
	newProvider := s.Provider
	if idx := strings.Index(args, ":"); idx > 0 {
		prefix := args[:idx]
		switch prefix {
		case "anth":
			newProvider = "anthropic"
		case "oai":
			newProvider = "openai"
		case "oll":
			newProvider = "ollama"
		case "vllm":
			newProvider = "vllm"
		case "lcpp":
			newProvider = "llama-cpp"
		default:
			newProvider = prefix
		}
		newModel = args[idx+1:]
	}

	s.Provider = newProvider
	s.Model = newModel
	printSuccess(fmt.Sprintf("Model set to: %s", formatModelShort(s.Provider, s.Model)))
	s.traceLog("cmd.model", "chat", map[string]string{
		"provider": s.Provider,
		"model":    s.Model,
	})
}

func (s *Session) cmdTrace() {
	tracePath := filepath.Join(s.RepoRoot, ".clock", "trce.jsonl")
	f, err := os.Open(tracePath)
	if err != nil {
		printInfo("No trace file found.")
		return
	}
	defer f.Close()

	// Read all lines, show last 20.
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}

	fmt.Println()
	for _, line := range lines[start:] {
		var ev common.TraceEvent
		if json.Unmarshal([]byte(line), &ev) == nil {
			ts := time.UnixMilli(ev.TS).Format("15:04:05.000")
			tool := ev.Tool
			if tool == "" {
				tool = "-"
			}
			ms := ""
			if ev.Ms > 0 {
				ms = fmt.Sprintf(" (%dms)", ev.Ms)
			}
			fmt.Printf("  %s%s%s  %s%-15s%s  %s%s\n",
				colorDim, ts, colorReset,
				colorCyan, ev.Event, colorReset,
				tool, ms)
		}
	}
	fmt.Println()
}

func (s *Session) cmdInit() {
	printInfo("Re-initializing .clock/ ...")
	out, err := s.runTool("clock", nil, "init")
	if err != nil {
		printError(fmt.Sprintf("init: %v", err))
		return
	}

	// Check result.
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(out, &result) == nil && result.OK {
		printSuccess("Initialization complete.")
	} else {
		printInfo(string(out))
	}

	// Reload dossier.
	s.Dossier = loadDossier(s.RepoRoot)
	s.Mode = loadMode(s.RepoRoot)
	printInfo("Dossier and mode reloaded.")
}

func (s *Session) cmdRefresh() {
	printInfo("Refreshing dossier...")
	_, err := s.runTool("rfrsh", mustJSON(map[string]interface{}{
		"root": s.RepoRoot,
	}))
	if err != nil {
		printError(fmt.Sprintf("rfrsh: %v", err))
		return
	}

	// Reload the dossier.
	s.Dossier = loadDossier(s.RepoRoot)
	printSuccess("Dossier refreshed.")
}

// ---------------------------------------------------------------------------
// Input loop
// ---------------------------------------------------------------------------

// readMultiline reads input lines, supporting continuation with trailing '\'.
func readMultiline(scanner *bufio.Scanner) (string, bool) {
	var parts []string
	for {
		if !scanner.Scan() {
			if len(parts) > 0 {
				return strings.Join(parts, "\n"), true
			}
			return "", false
		}
		line := scanner.Text()
		if strings.HasSuffix(line, "\\") {
			parts = append(parts, strings.TrimSuffix(line, "\\"))
			fmt.Print("  ... ")
			continue
		}
		parts = append(parts, line)
		break
	}
	return strings.Join(parts, "\n"), true
}

// saveInputHistory appends a raw input line to the history file.
func saveInputHistory(root, input string) {
	histPath := filepath.Join(root, ".clock", "chat_input_history")
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(input + "\n")
}

// dispatch processes a single user input line (slash command or natural language).
func (s *Session) dispatch(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return true
	}

	// Save to input history.
	saveInputHistory(s.RepoRoot, input)

	// Check for slash commands.
	if strings.HasPrefix(input, "/") {
		parts := strings.SplitN(input, " ", 2)
		cmd := strings.ToLower(parts[0])
		args := ""
		if len(parts) > 1 {
			args = strings.TrimSpace(parts[1])
		}

		switch cmd {
		case "/help":
			s.cmdHelp()
		case "/ask":
			if args == "" {
				printError("Usage: /ask <question>")
			} else {
				s.handleQuestion(args)
			}
		case "/fix":
			if args == "" {
				printError("Usage: /fix <goal>")
			} else {
				s.handleChange(args)
			}
		case "/search":
			s.cmdSearch(args)
		case "/read":
			s.cmdRead(args)
		case "/diff":
			s.cmdDiff()
		case "/undo":
			s.cmdUndo()
		case "/run":
			s.cmdRun(args)
		case "/mode":
			s.cmdMode(args)
		case "/status":
			s.cmdStatus()
		case "/clear":
			s.cmdClear()
		case "/model":
			s.cmdModel(args)
		case "/trace":
			s.cmdTrace()
		case "/init":
			s.cmdInit()
		case "/refresh":
			s.cmdRefresh()
		case "/quit", "/exit":
			return false
		default:
			printError(fmt.Sprintf("Unknown command: %s (type /help for available commands)", cmd))
		}
		return true
	}

	// Natural language input: classify intent.
	intent := classifyIntent(input)
	if intent == "change" {
		s.handleChange(input)
	} else {
		s.handleQuestion(input)
	}
	return true
}

// ---------------------------------------------------------------------------
// Signal handling
// ---------------------------------------------------------------------------

func (s *Session) setupSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGINT:
				// If a tool is running, kill it.
				s.mu.Lock()
				cancel := s.cancelFunc
				s.mu.Unlock()

				if cancel != nil {
					cancel()
					fmt.Println("\n  ^C (cancelled tool)")
				} else {
					// At the prompt, just show ^C and a new prompt.
					fmt.Print("\n  ^C\n\nclock> ")
				}

			case syscall.SIGTERM:
				fmt.Println("\nExiting...")
				os.Exit(0)
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

func initSession() *Session {
	// 1. Find repo root.
	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine repo root: %v\n", err)
		os.Exit(1)
	}

	// 2. Check/create .clock directory.
	clockDir := filepath.Join(repoRoot, ".clock")
	if _, err := os.Stat(clockDir); os.IsNotExist(err) {
		fmt.Printf("  %sNo .clock/ directory found. Running initialization...%s\n", colorDim, colorReset)
		initOut, initErr := runToolSilent("clock", nil, repoRoot, "init")
		if initErr != nil {
			fmt.Fprintf(os.Stderr, "  Warning: clock init: %v\n", initErr)
		} else {
			var result struct {
				OK bool `json:"ok"`
			}
			if json.Unmarshal(initOut, &result) == nil && result.OK {
				fmt.Printf("  %sInitialization complete.%s\n", colorGreen, colorReset)
			}
		}
	}

	// 3. Load config.
	dossier := loadDossier(repoRoot)
	mode := loadMode(repoRoot)
	provider, model := resolveModel(repoRoot)

	s := &Session{
		RepoRoot:  repoRoot,
		Dossier:   dossier,
		Mode:      mode,
		Model:     model,
		Provider:  provider,
		History:   make([]Message, 0, 64),
		UndoStack: make([]string, 0, 8),
		StartTime: time.Now(),
	}

	return s
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// Check if being invoked with a direct subcommand (e.g., `chat ask "question"`).
	if len(os.Args) > 1 {
		// Support one-shot mode: `chat ask "..."` or `chat fix "..."`
		switch os.Args[1] {
		case "ask":
			if len(os.Args) < 3 {
				fmt.Fprintf(os.Stderr, "Usage: chat ask <question>\n")
				os.Exit(1)
			}
			s := initSession()
			s.handleQuestion(strings.Join(os.Args[2:], " "))
			return
		case "fix":
			if len(os.Args) < 3 {
				fmt.Fprintf(os.Stderr, "Usage: chat fix <goal>\n")
				os.Exit(1)
			}
			s := initSession()
			s.handleChange(strings.Join(os.Args[2:], " "))
			return
		case "help", "-h", "--help":
			fmt.Println("chat — interactive AI-assisted development REPL")
			fmt.Println()
			fmt.Println("Usage:")
			fmt.Println("  chat                      Start interactive REPL")
			fmt.Println("  chat ask <question>        One-shot question")
			fmt.Println("  chat fix <goal>            One-shot change request")
			fmt.Println("  chat help                  Show this help")
			return
		}
	}

	// Interactive REPL mode.
	s := initSession()
	s.setupSignals()

	printBanner(s)

	// Check stdin is a terminal.
	stat, _ := os.Stdin.Stat()
	isTTY := (stat.Mode() & os.ModeCharDevice) != 0

	if !isTTY {
		// Pipe mode: read all of stdin as a single input.
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		input := strings.TrimSpace(string(data))
		if input != "" {
			s.dispatch(input)
		}
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	s.traceLog("chat.start", "chat", map[string]string{
		"repo":     s.RepoRoot,
		"mode":     s.Mode,
		"provider": s.Provider,
		"model":    s.Model,
	})

	for {
		fmt.Print("clock> ")
		input, ok := readMultiline(scanner)
		if !ok {
			// EOF (Ctrl+D).
			fmt.Println()
			break
		}

		if !s.dispatch(input) {
			break
		}
	}

	// Exit message.
	elapsed := time.Since(s.StartTime).Round(time.Second)
	fmt.Printf("\n%sSession ended.%s Duration: %s | Applied: %d | Tool calls: %d\n",
		colorDim, colorReset, elapsed, s.AppliedPatches, s.TotalToolCalls)

	s.traceLog("chat.end", "chat", map[string]interface{}{
		"duration_sec":   time.Since(s.StartTime).Seconds(),
		"applied":        s.AppliedPatches,
		"tool_calls":     s.TotalToolCalls,
		"messages":       len(s.History),
		"total_traces":   s.TotalTraces,
	})
}
