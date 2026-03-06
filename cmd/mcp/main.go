// Command mcp is an MCP (Model Context Protocol) bridge.
// It implements a basic JSON-RPC 2.0 server over stdio, exposing
// Clock tools as MCP tool definitions. It handles initialize,
// tools/list, and tools/call methods.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// JSON-RPC 2.0 types

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP types

// ServerInfo describes the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is returned for the initialize method.
type InitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo        `json:"serverInfo"`
}

// ServerCapabilities lists what the server supports.
type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability indicates tool support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPTool is a tool definition in MCP format.
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ToolsListResult is the result of tools/list.
type ToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// ToolCallParams are the params for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult is the result of tools/call.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a content block in tool results.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Clock tool definitions
var clockTools = []MCPTool{
	{
		Name:        "budg",
		Description: "Hard context budgeting and packing. Reads JSONL snippets from stdin, sorts by score, packs within byte budget.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"snippets": map[string]interface{}{
					"type":        "string",
					"description": "JSONL string of candidate snippets with path, start, end, text, score fields",
				},
				"max": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum budget in bytes (default 120000)",
				},
				"dedup": map[string]interface{}{
					"type":        "boolean",
					"description": "Merge overlapping ranges from the same file",
				},
			},
			"required": []string{"snippets"},
		},
	},
	{
		Name:        "anch",
		Description: "Resilient patch anchoring. Parses unified diff, finds drifted context lines, and rebases hunks.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"diff": map[string]interface{}{
					"type":        "string",
					"description": "Unified diff to anchor",
				},
				"policy": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"max_drift":      map[string]interface{}{"type": "integer"},
						"require_unique": map[string]interface{}{"type": "boolean"},
					},
				},
			},
			"required": []string{"diff"},
		},
	},
	{
		Name:        "dect",
		Description: "Repo capability detection. Scans for project markers and outputs detected commands.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"root": map[string]interface{}{
					"type":        "string",
					"description": "Root directory to scan (default \".\")",
				},
			},
		},
	},
	{
		Name:        "graf",
		Description: "Lightweight import/call graph. Walks files, parses imports and symbols.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"paths": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Paths to scan",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"imports", "symbols", "both"},
					"description": "Analysis mode",
				},
			},
		},
	},
	{
		Name:        "exec",
		Description: "Policy-based command runner with sandboxing.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"cmd": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Command and arguments",
				},
				"cwd":     map[string]interface{}{"type": "string"},
				"timeout": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"cmd"},
		},
	},
	{
		Name:        "risk",
		Description: "Change risk scoring on a unified diff.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"diff": map[string]interface{}{
					"type":        "string",
					"description": "Unified diff to score",
				},
			},
			"required": []string{"diff"},
		},
	},
	{
		Name:        "trce",
		Description: "Append-only trace log with replay metadata.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ts":    map[string]interface{}{"type": "integer"},
				"event": map[string]interface{}{"type": "string"},
				"tool":  map[string]interface{}{"type": "string"},
				"data":  map[string]interface{}{},
			},
			"required": []string{"event"},
		},
	},
	{
		Name:        "jolt",
		Description: "Streaming JSONL plumbing tool with filter, pick, merge, count, and head operations.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type":        "string",
					"description": "JSONL input string",
				},
				"pick":     map[string]interface{}{"type": "string"},
				"filter":   map[string]interface{}{"type": "string"},
				"merge":    map[string]interface{}{"type": "string"},
				"count":    map[string]interface{}{"type": "boolean"},
				"head":     map[string]interface{}{"type": "integer"},
				"validate": map[string]interface{}{"type": "boolean"},
			},
			"required": []string{"input"},
		},
	},
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	enc := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := Response{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &RPCError{
					Code:    -32700,
					Message: "Parse error",
					Data:    err.Error(),
				},
			}
			enc.Encode(resp)
			continue
		}

		resp := handleRequest(req)
		enc.Encode(resp)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		os.Exit(1)
	}
}

func handleRequest(req Request) Response {
	switch req.Method {
	case "initialize":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities: ServerCapabilities{
					Tools: &ToolsCapability{},
				},
				ServerInfo: ServerInfo{
					Name:    "clock-mcp",
					Version: "0.1.0",
				},
			},
		}

	case "notifications/initialized":
		// Notification — no response needed, but we return empty for consistency
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}

	case "tools/list":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolsListResult{
				Tools: clockTools,
			},
		}

	case "tools/call":
		return handleToolCall(req)

	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32601,
				Message: "Method not found",
				Data:    fmt.Sprintf("unknown method: %s", req.Method),
			},
		}
	}
}

func handleToolCall(req Request) Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	// Find the tool binary
	toolBin := findToolBinary(params.Name)
	if toolBin == "" {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: fmt.Sprintf("unknown tool: %s", params.Name),
			},
		}
	}

	// Prepare input for the tool
	var inputBytes []byte

	// For tools that accept JSONL via stdin (budg, jolt), extract the input field
	if params.Name == "budg" || params.Name == "jolt" {
		var args map[string]interface{}
		if err := json.Unmarshal(params.Arguments, &args); err == nil {
			if inp, ok := args["input"].(string); ok {
				inputBytes = []byte(inp)
			} else if snippets, ok := args["snippets"].(string); ok {
				inputBytes = []byte(snippets)
			} else {
				inputBytes = params.Arguments
			}
		} else {
			inputBytes = params.Arguments
		}
	} else {
		// For other tools, pass arguments as JSON
		inputBytes = params.Arguments
	}

	// Build command arguments for tools that use flags
	var cmdArgs []string
	if params.Name == "budg" {
		var args map[string]interface{}
		if err := json.Unmarshal(params.Arguments, &args); err == nil {
			if max, ok := args["max"].(float64); ok {
				cmdArgs = append(cmdArgs, "-max", fmt.Sprintf("%d", int(max)))
			}
			if dedup, ok := args["dedup"].(bool); ok && dedup {
				cmdArgs = append(cmdArgs, "-dedup")
			}
		}
	}

	// Run the tool as a subprocess
	cmd := exec.Command(toolBin, cmdArgs...)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: errMsg}},
				IsError: true,
			},
		}
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: stdout.String()}},
		},
	}
}

// findToolBinary locates the tool binary. It checks:
// 1. The same directory as the mcp binary
// 2. Go build output in cmd/<name>/
// 3. PATH
func findToolBinary(name string) string {
	// Validate tool name
	validTools := map[string]bool{
		"budg": true, "anch": true, "dect": true, "graf": true,
		"exec": true, "risk": true, "trce": true, "jolt": true,
		"act": true, "aply": true, "guard": true, "vrfy": true,
		"llm": true, "rfrsh": true, "clock": true,
	}
	if !validTools[name] {
		return ""
	}

	// Check next to our own binary
	self, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(self)
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check in PATH
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}

	// Try to use go run as fallback
	// Find the module root by looking for go.mod
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				cmdDir := filepath.Join(dir, "cmd", name)
				mainFile := filepath.Join(cmdDir, "main.go")
				if _, err := os.Stat(mainFile); err == nil {
					// Build the tool on the fly
					tmpBin := filepath.Join(os.TempDir(), "clock-"+name)
					buildCmd := exec.Command("go", "build", "-o", tmpBin, "./cmd/"+name)
					buildCmd.Dir = dir
					var buildErr bytes.Buffer
					buildCmd.Stderr = &buildErr
					if err := buildCmd.Run(); err == nil {
						return tmpBin
					}
				}
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Not a valid searchable name but might have special handling
	_ = strings.TrimSpace(name)
	return ""
}
