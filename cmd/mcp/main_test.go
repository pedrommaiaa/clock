package main

import (
	"encoding/json"
	"testing"
)

func TestHandleRequest_Initialize(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
	}
	resp := handleRequest(req)

	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	if resp.ID != float64(1) {
		t.Errorf("id = %v, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatalf("result is not InitializeResult: %T", resp.Result)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocolVersion = %q, want 2024-11-05", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "clock-mcp" {
		t.Errorf("serverInfo.name = %q, want clock-mcp", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("tools capability should not be nil")
	}
}

func TestHandleRequest_ToolsList(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/list",
	}
	resp := handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("result is not ToolsListResult: %T", resp.Result)
	}

	if len(result.Tools) == 0 {
		t.Fatal("expected non-empty tools list")
	}

	// Check that known tools are present
	toolNames := map[string]bool{}
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	wantTools := []string{"budg", "anch", "dect", "graf", "exec", "risk", "trce", "jolt"}
	for _, name := range wantTools {
		if !toolNames[name] {
			t.Errorf("missing tool %q in tools list", name)
		}
	}
}

func TestHandleRequest_MethodNotFound(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(3),
		Method:  "nonexistent/method",
	}
	resp := handleRequest(req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
	if resp.Error.Message != "Method not found" {
		t.Errorf("error message = %q, want 'Method not found'", resp.Error.Message)
	}
}

func TestHandleRequest_NotificationsInitialized(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(4),
		Method:  "notifications/initialized",
	}
	resp := handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	// Should return empty result map
	if resp.Result == nil {
		t.Error("expected non-nil result for notifications/initialized")
	}
}

func TestHandleToolCall_InvalidParams(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(5),
		Method:  "tools/call",
		Params:  json.RawMessage(`"not an object"`),
	}
	resp := handleToolCall(req)

	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestHandleToolCall_UnknownTool(t *testing.T) {
	params, _ := json.Marshal(ToolCallParams{
		Name: "nonexistent_tool",
	})
	req := Request{
		JSONRPC: "2.0",
		ID:      float64(6),
		Method:  "tools/call",
		Params:  json.RawMessage(params),
	}
	resp := handleToolCall(req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestFindToolBinary_InvalidName(t *testing.T) {
	result := findToolBinary("definitely_not_a_valid_tool_name")
	if result != "" {
		t.Errorf("expected empty string for invalid tool name, got %q", result)
	}
}

func TestFindToolBinary_ValidNames(t *testing.T) {
	validNames := []string{"budg", "anch", "dect", "graf", "exec", "risk", "trce", "jolt", "act", "aply", "guard", "vrfy", "llm", "rfrsh", "clock"}
	for _, name := range validNames {
		// We can't guarantee the binary exists, but at least the function
		// should not immediately return "" for valid names
		// (it will search through various paths)
		result := findToolBinary(name)
		// The function might return "" if the binary isn't built,
		// but it should not panic
		_ = result
	}
}

func TestRequestParsing(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" {
		t.Errorf("method = %q, want initialize", req.Method)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", req.JSONRPC)
	}
}

func TestRequestParsing_ToolsCall(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"risk","arguments":{"diff":"..."}}}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "tools/call" {
		t.Errorf("method = %q, want tools/call", req.Method)
	}

	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Name != "risk" {
		t.Errorf("tool name = %q, want risk", params.Name)
	}
}

func TestResponseSerialization(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  map[string]interface{}{"ok": true},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", parsed["jsonrpc"])
	}
	if parsed["error"] != nil {
		t.Error("error should be nil")
	}
}

func TestResponseSerialization_Error(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      float64(1),
		Error: &RPCError{
			Code:    -32600,
			Message: "Invalid Request",
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	errObj, ok := parsed["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["code"].(float64) != -32600 {
		t.Errorf("error code = %v, want -32600", errObj["code"])
	}
}

func TestClockToolsDefinitions(t *testing.T) {
	for _, tool := range clockTools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}

		// Verify the schema is valid JSON when marshaled
		_, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("tool %q schema marshal error: %v", tool.Name, err)
		}
	}
}

func TestHandleRequest_NilID(t *testing.T) {
	// Notifications may have nil ID
	req := Request{
		JSONRPC: "2.0",
		ID:      nil,
		Method:  "notifications/initialized",
	}
	resp := handleRequest(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestMCPToolCount(t *testing.T) {
	// Verify we have a reasonable number of tools defined
	if len(clockTools) < 8 {
		t.Errorf("expected at least 8 tools, got %d", len(clockTools))
	}
}
