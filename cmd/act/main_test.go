package main

import (
	"testing"

	"github.com/pedrommaiaa/clock/internal/common"
)

func TestValidate_AllValidKinds(t *testing.T) {
	tests := []struct {
		name    string
		env     common.ActionEnvelope
		wantErr bool
	}{
		{name: "srch kind", env: common.ActionEnvelope{Kind: "srch"}, wantErr: false},
		{name: "slce kind", env: common.ActionEnvelope{Kind: "slce"}, wantErr: false},
		{name: "patch kind with diff", env: common.ActionEnvelope{Kind: "patch", Diff: "some diff"}, wantErr: false},
		{name: "run kind", env: common.ActionEnvelope{Kind: "run"}, wantErr: false},
		{name: "done kind with answer", env: common.ActionEnvelope{Kind: "done", Answer: "42"}, wantErr: false},
		{name: "tool kind with valid name", env: common.ActionEnvelope{Kind: "tool", Name: "srch"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validate(tt.env)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_InvalidKinds(t *testing.T) {
	tests := []struct {
		name string
		env  common.ActionEnvelope
	}{
		{name: "empty kind", env: common.ActionEnvelope{Kind: ""}},
		{name: "unknown kind", env: common.ActionEnvelope{Kind: "bogus"}},
		{name: "close but wrong", env: common.ActionEnvelope{Kind: "search"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validate(tt.env)
			if err == nil {
				t.Error("validate() expected error for invalid kind, got nil")
			}
		})
	}
}

func TestValidate_DirectKindsReturnPayload(t *testing.T) {
	args := map[string]interface{}{"query": "hello"}
	env := common.ActionEnvelope{Kind: "srch", Args: args}
	out, err := validate(env)
	if err != nil {
		t.Fatalf("validate() unexpected error: %v", err)
	}
	if out.Kind != "srch" {
		t.Errorf("kind = %q, want %q", out.Kind, "srch")
	}
	if out.Payload == nil {
		t.Error("payload should not be nil for direct kind")
	}
}

func TestHandleTool_ValidTools(t *testing.T) {
	tools := []string{"srch", "slce", "pack", "map", "ctrt", "flow", "doss", "vrfy", "exec", "graf"}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			env := common.ActionEnvelope{Kind: "tool", Name: tool}
			out, err := handleTool(env)
			if err != nil {
				t.Fatalf("handleTool(%q) unexpected error: %v", tool, err)
			}
			if out.Kind != tool {
				t.Errorf("kind = %q, want %q", out.Kind, tool)
			}
		})
	}
}

func TestHandleTool_InvalidTool(t *testing.T) {
	tests := []struct {
		name string
		env  common.ActionEnvelope
	}{
		{name: "empty name", env: common.ActionEnvelope{Kind: "tool", Name: ""}},
		{name: "unknown tool", env: common.ActionEnvelope{Kind: "tool", Name: "nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleTool(tt.env)
			if err == nil {
				t.Error("handleTool() expected error, got nil")
			}
		})
	}
}

func TestHandlePatch(t *testing.T) {
	tests := []struct {
		name    string
		diff    string
		wantErr bool
	}{
		{name: "empty diff", diff: "", wantErr: true},
		{name: "non-empty diff", diff: "--- a/foo\n+++ b/foo\n@@ -1 +1 @@\n-old\n+new", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := common.ActionEnvelope{Kind: "patch", Diff: tt.diff}
			out, err := handlePatch(env)
			if (err != nil) != tt.wantErr {
				t.Errorf("handlePatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if out.Kind != "patch" {
					t.Errorf("kind = %q, want %q", out.Kind, "patch")
				}
				payload, ok := out.Payload.(map[string]string)
				if !ok {
					t.Fatal("payload is not map[string]string")
				}
				if payload["diff"] != tt.diff {
					t.Errorf("diff mismatch")
				}
			}
		})
	}
}

func TestHandleDone(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		why     string
		wantErr bool
	}{
		{name: "empty answer", answer: "", why: "", wantErr: true},
		{name: "with answer", answer: "done!", why: "because", wantErr: false},
		{name: "answer no why", answer: "ok", why: "", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := common.ActionEnvelope{Kind: "done", Answer: tt.answer, Why: tt.why}
			out, err := handleDone(env)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleDone() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if out.Kind != "done" {
					t.Errorf("kind = %q, want %q", out.Kind, "done")
				}
				payload, ok := out.Payload.(map[string]string)
				if !ok {
					t.Fatal("payload is not map[string]string")
				}
				if payload["answer"] != tt.answer {
					t.Errorf("answer = %q, want %q", payload["answer"], tt.answer)
				}
				if payload["why"] != tt.why {
					t.Errorf("why = %q, want %q", payload["why"], tt.why)
				}
			}
		})
	}
}

func TestHandleRun(t *testing.T) {
	tests := []struct {
		name    string
		args    interface{}
		wantCmd interface{}
	}{
		{
			name:    "map with cmd key",
			args:    map[string]interface{}{"cmd": "ls -la"},
			wantCmd: "ls -la",
		},
		{
			name:    "map without cmd key",
			args:    map[string]interface{}{"foo": "bar"},
			wantCmd: map[string]interface{}{"foo": "bar"},
		},
		{
			name:    "string args",
			args:    "echo hi",
			wantCmd: "echo hi",
		},
		{
			name:    "nil args",
			args:    nil,
			wantCmd: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := common.ActionEnvelope{Kind: "run", Args: tt.args}
			out, err := handleRun(env)
			if err != nil {
				t.Fatalf("handleRun() unexpected error: %v", err)
			}
			if out.Kind != "run" {
				t.Errorf("kind = %q, want %q", out.Kind, "run")
			}
			payload, ok := out.Payload.(map[string]interface{})
			if !ok {
				t.Fatal("payload is not map[string]interface{}")
			}
			// For map-with-cmd-key case, the cmd value should match
			if tt.name == "map with cmd key" {
				if payload["cmd"] != tt.wantCmd {
					t.Errorf("cmd = %v, want %v", payload["cmd"], tt.wantCmd)
				}
			}
		})
	}
}
