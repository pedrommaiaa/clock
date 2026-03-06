// Command link is an evidence linker that extracts facts from artifacts
// and links them in the knox knowledge graph.
// It reads artifacts from stdin, extracts entities (files, functions, modules),
// creates knox nodes and edges, and outputs a summary.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// LinkInput is the input schema for the link tool.
type LinkInput struct {
	Artifacts []Artifact `json:"artifacts"`
	TraceID   string     `json:"trace_id,omitempty"`
}

// Artifact is a single evidence artifact.
type Artifact struct {
	Type    string `json:"type"`    // diff, slice, log, trace
	Content string `json:"content"`
	Source  string `json:"source"` // file:line
}

// LinkOutput is the output of the link tool.
type LinkOutput struct {
	NodesAdded int      `json:"nodes_added"`
	EdgesAdded int      `json:"edges_added"`
	Facts      []string `json:"facts"`
}

// KnoxNode matches knox's node format.
type KnoxNode struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"`
	Name  string            `json:"name"`
	Path  string            `json:"path,omitempty"`
	Props map[string]string `json:"props,omitempty"`
}

// KnoxEdge matches knox's edge format.
type KnoxEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// Regex patterns for extraction.
var (
	// Diff patterns
	diffFilePattern = regexp.MustCompile(`^(?:---|\+\+\+)\s+[ab]/(.+)$`)
	diffHunkPattern = regexp.MustCompile(`^@@\s+-\d+(?:,\d+)?\s+\+\d+(?:,\d+)?\s+@@\s*(.*)$`)

	// Function definition patterns (in diff context, lines starting with + or -)
	funcGoPattern     = regexp.MustCompile(`^[+-]\s*func\s+(?:\([^)]*\)\s+)?(\w+)`)
	funcJSPattern     = regexp.MustCompile(`^[+-]\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	funcPyPattern     = regexp.MustCompile(`^[+-]\s*def\s+(\w+)`)
	funcGenericChange = regexp.MustCompile(`^[+-]\s*(?:func|function|def)\s+(?:\([^)]*\)\s+)?(\w+)`)

	// Import patterns (for slices)
	importGoPattern = regexp.MustCompile(`import\s+"([^"]+)"`)
	importGoMulti   = regexp.MustCompile(`"([^"]+)"`)
	importJSPattern = regexp.MustCompile(`(?:import|require)\s*(?:\(?\s*)?['"]([^'"]+)['"]`)
	importPyPattern = regexp.MustCompile(`(?:^import\s+|^from\s+)([\w.]+)`)

	// Log patterns
	logErrorPattern = regexp.MustCompile(`(?i)(?:error|fail|panic|fatal|exception)[\s:]+(.{10,80})`)
	logTestPattern  = regexp.MustCompile(`(?i)(?:PASS|FAIL|SKIP|---)\s+(Test\w+)`)

	// Module reference patterns
	moduleRefPattern = regexp.MustCompile(`(?:package|module)\s+(\S+)`)
)

func main() {
	var input LinkInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if len(input.Artifacts) == 0 {
		jsonutil.Fatal("no artifacts provided")
	}

	output := LinkOutput{
		Facts: []string{},
	}

	// Track what we've already added to avoid duplicates
	addedNodes := make(map[string]bool)
	addedEdges := make(map[string]bool)

	for _, artifact := range input.Artifacts {
		switch artifact.Type {
		case "diff":
			processDiff(artifact, input.TraceID, &output, addedNodes, addedEdges)
		case "slice":
			processSlice(artifact, input.TraceID, &output, addedNodes, addedEdges)
		case "log":
			processLog(artifact, input.TraceID, &output, addedNodes, addedEdges)
		case "trace":
			processTrace(artifact, input.TraceID, &output, addedNodes, addedEdges)
		default:
			output.Facts = append(output.Facts, fmt.Sprintf("unknown artifact type: %s", artifact.Type))
		}
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func makeID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return fmt.Sprintf("%x", h[:8])
}

func addNode(node KnoxNode, output *LinkOutput, added map[string]bool) {
	if added[node.ID] {
		return
	}

	data, err := json.Marshal(node)
	if err != nil {
		return
	}

	cmd := exec.Command("knox", "add-node")
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// If knox isn't available, try with ./knox or bin/knox
		cmd2 := exec.Command("./knox", "add-node")
		cmd2.Stdin = strings.NewReader(string(data))
		cmd2.Stderr = os.Stderr
		if err2 := cmd2.Run(); err2 != nil {
			// Last resort: try bin/knox
			cmd3 := exec.Command("bin/knox", "add-node")
			cmd3.Stdin = strings.NewReader(string(data))
			cmd3.Stderr = os.Stderr
			cmd3.Run() // best effort
		}
	}

	added[node.ID] = true
	output.NodesAdded++
}

func addEdge(edge KnoxEdge, output *LinkOutput, added map[string]bool) {
	key := fmt.Sprintf("%s->%s:%s", edge.From, edge.To, edge.Kind)
	if added[key] {
		return
	}

	data, err := json.Marshal(edge)
	if err != nil {
		return
	}

	cmd := exec.Command("knox", "add-edge")
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cmd2 := exec.Command("./knox", "add-edge")
		cmd2.Stdin = strings.NewReader(string(data))
		cmd2.Stderr = os.Stderr
		if err2 := cmd2.Run(); err2 != nil {
			cmd3 := exec.Command("bin/knox", "add-edge")
			cmd3.Stdin = strings.NewReader(string(data))
			cmd3.Stderr = os.Stderr
			cmd3.Run()
		}
	}

	added[key] = true
	output.EdgesAdded++
}

func processDiff(artifact Artifact, traceID string, output *LinkOutput, addedNodes, addedEdges map[string]bool) {
	lines := strings.Split(artifact.Content, "\n")
	var filesChanged []string
	var funcsModified []string

	for _, line := range lines {
		// Extract files from diff headers
		if m := diffFilePattern.FindStringSubmatch(line); m != nil {
			file := m[1]
			if file != "/dev/null" {
				filesChanged = appendUnique(filesChanged, file)
			}
		}

		// Extract functions modified
		if m := funcGenericChange.FindStringSubmatch(line); m != nil {
			funcsModified = appendUnique(funcsModified, m[1])
		}

		// Also check hunk headers for function context
		if m := diffHunkPattern.FindStringSubmatch(line); m != nil {
			ctx := strings.TrimSpace(m[1])
			if ctx != "" {
				// Hunk context often contains the function name
				for _, pat := range []*regexp.Regexp{
					regexp.MustCompile(`func\s+(?:\([^)]*\)\s+)?(\w+)`),
					regexp.MustCompile(`function\s+(\w+)`),
					regexp.MustCompile(`def\s+(\w+)`),
				} {
					if fm := pat.FindStringSubmatch(ctx); fm != nil {
						funcsModified = appendUnique(funcsModified, fm[1])
					}
				}
			}
		}
	}

	// Create nodes and edges for files
	for _, file := range filesChanged {
		nodeID := makeID("file", file)
		props := map[string]string{"source": artifact.Source}
		if traceID != "" {
			props["trace_id"] = traceID
		}
		props["discovered_at"] = time.Now().Format(time.RFC3339)

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "file",
			Name:  file,
			Path:  file,
			Props: props,
		}, output, addedNodes)

		output.Facts = append(output.Facts, fmt.Sprintf("file modified: %s", file))
	}

	// Create nodes and edges for functions
	for _, fn := range funcsModified {
		nodeID := makeID("function", fn)
		props := map[string]string{"source": artifact.Source}
		if traceID != "" {
			props["trace_id"] = traceID
		}

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "function",
			Name:  fn,
			Props: props,
		}, output, addedNodes)

		// Link function to files it was modified in
		for _, file := range filesChanged {
			fileID := makeID("file", file)
			addEdge(KnoxEdge{
				From: nodeID,
				To:   fileID,
				Kind: "modifies",
			}, output, addedEdges)
		}

		output.Facts = append(output.Facts, fmt.Sprintf("function modified: %s", fn))
	}
}

func processSlice(artifact Artifact, traceID string, output *LinkOutput, addedNodes, addedEdges map[string]bool) {
	content := artifact.Content

	// Extract module references
	for _, m := range moduleRefPattern.FindAllStringSubmatch(content, -1) {
		modName := m[1]
		nodeID := makeID("module", modName)
		props := map[string]string{"source": artifact.Source}
		if traceID != "" {
			props["trace_id"] = traceID
		}

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "module",
			Name:  modName,
			Props: props,
		}, output, addedNodes)

		output.Facts = append(output.Facts, fmt.Sprintf("module referenced: %s", modName))
	}

	// Extract imports
	var imports []string
	for _, m := range importGoPattern.FindAllStringSubmatch(content, -1) {
		imports = appendUnique(imports, m[1])
	}
	for _, m := range importJSPattern.FindAllStringSubmatch(content, -1) {
		imports = appendUnique(imports, m[1])
	}
	for _, line := range strings.Split(content, "\n") {
		if m := importPyPattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			imports = appendUnique(imports, m[1])
		}
	}

	// Go multi-line imports
	goMulti := regexp.MustCompile(`(?s)import\s*\(([^)]+)\)`)
	for _, m := range goMulti.FindAllStringSubmatch(content, -1) {
		block := m[1]
		for _, im := range importGoMulti.FindAllStringSubmatch(block, -1) {
			imports = appendUnique(imports, im[1])
		}
	}

	// Create import edges
	sourceID := makeID("source", artifact.Source)
	if len(imports) > 0 {
		addNode(KnoxNode{
			ID:   sourceID,
			Kind: "file",
			Name: artifact.Source,
			Path: artifact.Source,
		}, output, addedNodes)
	}

	for _, imp := range imports {
		impID := makeID("module", imp)
		addNode(KnoxNode{
			ID:   impID,
			Kind: "module",
			Name: imp,
		}, output, addedNodes)

		addEdge(KnoxEdge{
			From: sourceID,
			To:   impID,
			Kind: "references",
		}, output, addedEdges)

		output.Facts = append(output.Facts, fmt.Sprintf("import: %s -> %s", artifact.Source, imp))
	}
}

func processLog(artifact Artifact, traceID string, output *LinkOutput, addedNodes, addedEdges map[string]bool) {
	content := artifact.Content

	// Extract error patterns
	for _, m := range logErrorPattern.FindAllStringSubmatch(content, -1) {
		errMsg := strings.TrimSpace(m[1])
		nodeID := makeID("fact", "error", errMsg)
		props := map[string]string{
			"source":  artifact.Source,
			"pattern": errMsg,
		}
		if traceID != "" {
			props["trace_id"] = traceID
		}

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "fact",
			Name:  fmt.Sprintf("error: %s", truncate(errMsg, 60)),
			Props: props,
		}, output, addedNodes)

		output.Facts = append(output.Facts, fmt.Sprintf("error pattern: %s", truncate(errMsg, 80)))
	}

	// Extract test names
	for _, m := range logTestPattern.FindAllStringSubmatch(content, -1) {
		testName := m[1]
		nodeID := makeID("function", testName)
		props := map[string]string{"source": artifact.Source}
		if traceID != "" {
			props["trace_id"] = traceID
		}

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "function",
			Name:  testName,
			Props: props,
		}, output, addedNodes)

		// Determine if it's a pass or fail
		line := ""
		for _, l := range strings.Split(content, "\n") {
			if strings.Contains(l, testName) {
				line = l
				break
			}
		}
		if strings.Contains(strings.ToUpper(line), "FAIL") {
			// Create a "causes" edge from the error to the test
			errNodes := findErrorNodes(content, addedNodes)
			for _, errID := range errNodes {
				addEdge(KnoxEdge{
					From: errID,
					To:   nodeID,
					Kind: "causes",
				}, output, addedEdges)
			}
		}

		output.Facts = append(output.Facts, fmt.Sprintf("test: %s", testName))
	}
}

func processTrace(artifact Artifact, traceID string, output *LinkOutput, addedNodes, addedEdges map[string]bool) {
	// Parse trace events and link tool calls
	var events []map[string]interface{}
	for _, line := range strings.Split(artifact.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}

	for _, ev := range events {
		tool, _ := ev["tool"].(string)
		event, _ := ev["event"].(string)
		if tool == "" {
			continue
		}

		nodeID := makeID("function", tool)
		props := map[string]string{"event_type": event}
		if traceID != "" {
			props["trace_id"] = traceID
		}

		addNode(KnoxNode{
			ID:    nodeID,
			Kind:  "function",
			Name:  tool,
			Props: props,
		}, output, addedNodes)

		output.Facts = append(output.Facts, fmt.Sprintf("tool call: %s (%s)", tool, event))
	}

	// Create call edges between consecutive tool calls
	var prevID string
	for _, ev := range events {
		tool, _ := ev["tool"].(string)
		event, _ := ev["event"].(string)
		if tool == "" || event != "tool.call" {
			continue
		}

		nodeID := makeID("function", tool)
		if prevID != "" && prevID != nodeID {
			addEdge(KnoxEdge{
				From: prevID,
				To:   nodeID,
				Kind: "calls",
			}, output, addedEdges)
		}
		prevID = nodeID
	}
}

func findErrorNodes(content string, addedNodes map[string]bool) []string {
	var ids []string
	for _, m := range logErrorPattern.FindAllStringSubmatch(content, -1) {
		errMsg := strings.TrimSpace(m[1])
		nodeID := makeID("fact", "error", errMsg)
		if addedNodes[nodeID] {
			ids = append(ids, nodeID)
		}
	}
	return ids
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
