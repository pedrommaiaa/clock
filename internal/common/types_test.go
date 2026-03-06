package common

import (
	"encoding/json"
	"testing"
)

// roundTrip marshals v to JSON then unmarshals into dst, returning any error.
func roundTrip(v interface{}, dst interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// ---------- ActionEnvelope ----------

func TestActionEnvelope_KindTool(t *testing.T) {
	orig := ActionEnvelope{
		Kind: "tool",
		Name: "exec",
		Args: map[string]interface{}{"cmd": "ls"},
		Why:  "list files",
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "tool" || got.Name != "exec" || got.Why != "list files" {
		t.Errorf("tool envelope mismatch: %+v", got)
	}
}

func TestActionEnvelope_KindPatch(t *testing.T) {
	orig := ActionEnvelope{
		Kind: "patch",
		Diff: "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new",
		Why:  "fix bug",
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "patch" || got.Diff == "" {
		t.Errorf("patch envelope mismatch: %+v", got)
	}
}

func TestActionEnvelope_KindDone(t *testing.T) {
	orig := ActionEnvelope{
		Kind:   "done",
		Answer: "task complete",
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "done" || got.Answer != "task complete" {
		t.Errorf("done envelope mismatch: %+v", got)
	}
}

func TestActionEnvelope_KindRun(t *testing.T) {
	orig := ActionEnvelope{
		Kind:    "run",
		Payload: map[string]interface{}{"script": "build.sh"},
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "run" {
		t.Errorf("run envelope mismatch: %+v", got)
	}
	payload, ok := got.Payload.(map[string]interface{})
	if !ok || payload["script"] != "build.sh" {
		t.Errorf("run payload mismatch: %+v", got.Payload)
	}
}

func TestActionEnvelope_KindSrch(t *testing.T) {
	orig := ActionEnvelope{
		Kind: "srch",
		Name: "grep",
		Args: map[string]interface{}{"pattern": "TODO"},
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "srch" || got.Name != "grep" {
		t.Errorf("srch envelope mismatch: %+v", got)
	}
}

func TestActionEnvelope_KindSlce(t *testing.T) {
	orig := ActionEnvelope{
		Kind: "slce",
		Args: map[string]interface{}{"path": "main.go", "start": float64(1), "end": float64(10)},
	}
	var got ActionEnvelope
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "slce" {
		t.Errorf("slce envelope mismatch: %+v", got)
	}
}

func TestActionEnvelope_ZeroValue(t *testing.T) {
	var orig ActionEnvelope
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got ActionEnvelope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "" {
		t.Errorf("zero value kind should be empty, got %q", got.Kind)
	}
}

// ---------- TraceEvent ----------

func TestTraceEvent_RoundTrip(t *testing.T) {
	orig := TraceEvent{
		TS:    1700000000,
		Event: "tool.call",
		Tool:  "exec",
		Data:  "payload",
		Ms:    42,
		ChkID: "abc123",
	}
	var got TraceEvent
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.TS != orig.TS || got.Event != orig.Event || got.Tool != orig.Tool || got.Ms != orig.Ms || got.ChkID != orig.ChkID {
		t.Errorf("TraceEvent mismatch: %+v", got)
	}
}

func TestTraceEvent_ZeroValue(t *testing.T) {
	var orig TraceEvent
	var got TraceEvent
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.TS != 0 || got.Event != "" {
		t.Errorf("TraceEvent zero value mismatch: %+v", got)
	}
}

// ---------- GuardResult ----------

func TestGuardResult_RoundTrip(t *testing.T) {
	orig := GuardResult{
		OK:      true,
		Risk:    0.15,
		Reasons: []string{"safe operation"},
		Needs:   []string{"approval"},
	}
	var got GuardResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != true || got.Risk != 0.15 || len(got.Reasons) != 1 || len(got.Needs) != 1 {
		t.Errorf("GuardResult mismatch: %+v", got)
	}
}

func TestGuardResult_ZeroValue(t *testing.T) {
	var orig GuardResult
	var got GuardResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != false || got.Risk != 0 {
		t.Errorf("GuardResult zero mismatch: %+v", got)
	}
}

// ---------- ApplyResult ----------

func TestApplyResult_RoundTrip(t *testing.T) {
	orig := ApplyResult{
		OK:    true,
		ChkID: "chk_1",
		Files: []string{"a.go", "b.go"},
		Err:   "",
	}
	orig.Lines.Add = 10
	orig.Lines.Del = 3
	var got ApplyResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != true || got.ChkID != "chk_1" || len(got.Files) != 2 || got.Lines.Add != 10 || got.Lines.Del != 3 {
		t.Errorf("ApplyResult mismatch: %+v", got)
	}
}

func TestApplyResult_ZeroValue(t *testing.T) {
	var orig ApplyResult
	var got ApplyResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != false || got.Lines.Add != 0 || got.Lines.Del != 0 {
		t.Errorf("ApplyResult zero mismatch: %+v", got)
	}
}

// ---------- VerifyResult ----------

func TestVerifyResult_RoundTrip(t *testing.T) {
	failStep := VerifyStep{Name: "lint", Cmd: "golint ./...", OK: false, Code: 1, Output: "error", Ms: 50}
	orig := VerifyResult{
		OK: false,
		Steps: []VerifyStep{
			{Name: "test", Cmd: "go test", OK: true, Code: 0, Ms: 100},
			failStep,
		},
		Fail: &failStep,
		Logs: "some logs",
	}
	var got VerifyResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != false || len(got.Steps) != 2 || got.Fail == nil || got.Fail.Name != "lint" || got.Logs != "some logs" {
		t.Errorf("VerifyResult mismatch: %+v", got)
	}
}

func TestVerifyStep_RoundTrip(t *testing.T) {
	orig := VerifyStep{Name: "build", Cmd: "go build", OK: true, Code: 0, Output: "", Ms: 200}
	var got VerifyStep
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "build" || got.Cmd != "go build" || got.OK != true || got.Code != 0 || got.Ms != 200 {
		t.Errorf("VerifyStep mismatch: %+v", got)
	}
}

// ---------- RiskResult ----------

func TestRiskResult_RoundTrip(t *testing.T) {
	orig := RiskResult{
		Risk:  0.8,
		Class: "high",
		Must:  []string{"review", "test"},
		Why:   []string{"modifies critical path"},
	}
	var got RiskResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Risk != 0.8 || got.Class != "high" || len(got.Must) != 2 || len(got.Why) != 1 {
		t.Errorf("RiskResult mismatch: %+v", got)
	}
}

func TestRiskResult_ZeroValue(t *testing.T) {
	var orig RiskResult
	var got RiskResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Risk != 0 || got.Class != "" {
		t.Errorf("RiskResult zero mismatch: %+v", got)
	}
}

// ---------- ExecResult ----------

func TestExecResult_RoundTrip(t *testing.T) {
	orig := ExecResult{Code: 0, Out: "hello\n", Err: "", Ms: 15}
	var got ExecResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != 0 || got.Out != "hello\n" || got.Err != "" || got.Ms != 15 {
		t.Errorf("ExecResult mismatch: %+v", got)
	}
}

func TestExecResult_ZeroValue(t *testing.T) {
	var orig ExecResult
	var got ExecResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != 0 || got.Out != "" || got.Ms != 0 {
		t.Errorf("ExecResult zero mismatch: %+v", got)
	}
}

// ---------- ScanResult ----------

func TestScanResult_RoundTrip(t *testing.T) {
	orig := ScanResult{
		Root:      "/project",
		Languages: []string{"go", "python"},
		Managers:  []string{"go.mod"},
		Commands:  map[string]string{"test": "go test ./...", "build": "go build ./..."},
		KeyFiles:  []string{"go.mod", "Makefile"},
	}
	var got ScanResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Root != "/project" || len(got.Languages) != 2 || len(got.Commands) != 2 || len(got.KeyFiles) != 2 {
		t.Errorf("ScanResult mismatch: %+v", got)
	}
}

// ---------- ScopeEntry ----------

func TestScopeEntry_RoundTrip(t *testing.T) {
	orig := ScopeEntry{Path: "internal/common/types.go"}
	var got ScopeEntry
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != orig.Path {
		t.Errorf("ScopeEntry mismatch: %+v", got)
	}
}

// ---------- SearchHit ----------

func TestSearchHit_RoundTrip(t *testing.T) {
	orig := SearchHit{Path: "main.go", Line: 42, Col: 5, Text: "func main()", Score: 0.95}
	var got SearchHit
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "main.go" || got.Line != 42 || got.Col != 5 || got.Text != "func main()" || got.Score != 0.95 {
		t.Errorf("SearchHit mismatch: %+v", got)
	}
}

// ---------- SliceResult ----------

func TestSliceResult_RoundTrip(t *testing.T) {
	orig := SliceResult{Path: "main.go", Start: 1, End: 10, Text: "package main\n"}
	var got SliceResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "main.go" || got.Start != 1 || got.End != 10 || got.Text != "package main\n" {
		t.Errorf("SliceResult mismatch: %+v", got)
	}
}

// ---------- MapResult / DirEntry / HotSpot ----------

func TestMapResult_RoundTrip(t *testing.T) {
	orig := MapResult{
		Outline: []DirEntry{
			{Path: "cmd", Files: 3, Children: []DirEntry{{Path: "cmd/main", Files: 1}}},
		},
		KeyFiles: []string{"go.mod"},
		HotSpots: []HotSpot{{Path: "main.go", Reason: "large", Value: 500}},
	}
	var got MapResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Outline) != 1 || got.Outline[0].Path != "cmd" || len(got.Outline[0].Children) != 1 {
		t.Errorf("MapResult outline mismatch: %+v", got)
	}
	if len(got.HotSpots) != 1 || got.HotSpots[0].Value != 500 {
		t.Errorf("MapResult hotspots mismatch: %+v", got)
	}
}

// ---------- ContractResult / ContractEntry ----------

func TestContractResult_RoundTrip(t *testing.T) {
	orig := ContractResult{
		Exports: []ContractEntry{{Name: "Foo", Path: "pkg/foo.go", Line: 10, Kind: "func"}},
		Routes:  []ContractEntry{{Name: "/api/v1", Path: "api.go", Line: 5}},
		CLI:     []ContractEntry{{Name: "run", Path: "cmd.go"}},
		Env:     []ContractEntry{{Name: "PORT", Path: ".env"}},
		Schemas: []ContractEntry{{Name: "User", Path: "schema.go"}},
	}
	var got ContractResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Exports) != 1 || len(got.Routes) != 1 || len(got.CLI) != 1 || len(got.Env) != 1 || len(got.Schemas) != 1 {
		t.Errorf("ContractResult mismatch: %+v", got)
	}
}

// ---------- FlowResult / FlowPipeline / FlowEdge ----------

func TestFlowResult_RoundTrip(t *testing.T) {
	orig := FlowResult{
		Pipelines: []FlowPipeline{{Name: "build", Steps: []string{"compile", "link"}}},
		Edges:     []FlowEdge{{From: "A", To: "B", Kind: "depends"}},
		SideFx:    []string{"writes to disk"},
		Notes:     "simple flow",
	}
	var got FlowResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Pipelines) != 1 || got.Pipelines[0].Name != "build" || len(got.Pipelines[0].Steps) != 2 {
		t.Errorf("FlowResult pipelines mismatch: %+v", got)
	}
	if len(got.Edges) != 1 || got.Edges[0].From != "A" {
		t.Errorf("FlowResult edges mismatch: %+v", got)
	}
}

// ---------- PackBundle / Message / ToolDef / Citation ----------

func TestPackBundle_RoundTrip(t *testing.T) {
	orig := PackBundle{
		System: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
		Tools: []ToolDef{
			{Name: "exec", Description: "run cmd", Schema: map[string]interface{}{"type": "object"}},
		},
		Policy:    map[string]interface{}{"max_tokens": float64(100)},
		Citations: []Citation{{Path: "main.go", Start: 1, End: 10}},
	}
	var got PackBundle
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.System != orig.System || len(got.Messages) != 2 || len(got.Tools) != 1 || len(got.Citations) != 1 {
		t.Errorf("PackBundle mismatch: %+v", got)
	}
	if got.Messages[0].Role != "user" || got.Messages[1].Content != "hi" {
		t.Errorf("PackBundle messages mismatch: %+v", got.Messages)
	}
}

// ---------- JobSpec ----------

func TestJobSpec_RoundTrip(t *testing.T) {
	orig := JobSpec{
		ID:       "job-1",
		Goal:     "fix bug",
		Repo:     "github.com/user/repo",
		Scope:    []string{"src/"},
		Priority: "high",
		Plan:     []string{"read", "fix", "test"},
		Mode:     "auto",
	}
	var got JobSpec
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "job-1" || got.Goal != "fix bug" || got.Mode != "auto" || len(got.Plan) != 3 {
		t.Errorf("JobSpec mismatch: %+v", got)
	}
}

// ---------- JobResult ----------

func TestJobResult_RoundTrip(t *testing.T) {
	vr := &VerifyResult{OK: true, Steps: []VerifyStep{{Name: "test", Cmd: "go test", OK: true, Code: 0, Ms: 50}}}
	orig := JobResult{
		ID:     "job-1",
		OK:     true,
		Goal:   "fix bug",
		Diff:   "+line\n-line",
		Verify: vr,
		Report: "done",
	}
	var got JobResult
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "job-1" || got.OK != true || got.Verify == nil || got.Verify.OK != true {
		t.Errorf("JobResult mismatch: %+v", got)
	}
}

// ---------- QueueEntry ----------

func TestQueueEntry_RoundTrip(t *testing.T) {
	orig := QueueEntry{
		ID:        "q-1",
		Job:       JobSpec{ID: "job-1", Goal: "test", Repo: "repo"},
		Status:    "pending",
		Retries:   0,
		CreatedAt: 1700000000,
	}
	var got QueueEntry
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "q-1" || got.Status != "pending" || got.Job.Goal != "test" || got.CreatedAt != 1700000000 {
		t.Errorf("QueueEntry mismatch: %+v", got)
	}
}

// ---------- EventMsg ----------

func TestEventMsg_RoundTrip(t *testing.T) {
	orig := EventMsg{
		Type:   "git.push",
		Repo:   "owner/repo",
		Branch: "main",
		Meta:   map[string]interface{}{"sha": "abc123"},
	}
	var got EventMsg
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "git.push" || got.Branch != "main" {
		t.Errorf("EventMsg mismatch: %+v", got)
	}
}

// ---------- LeaseRequest / LeaseResponse ----------

func TestLeaseRequest_RoundTrip(t *testing.T) {
	orig := LeaseRequest{JobID: "j1", Resource: "gpu", Tokens: 100, TimeoutSec: 30}
	var got LeaseRequest
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.JobID != "j1" || got.Resource != "gpu" || got.Tokens != 100 || got.TimeoutSec != 30 {
		t.Errorf("LeaseRequest mismatch: %+v", got)
	}
}

func TestLeaseResponse_RoundTrip(t *testing.T) {
	orig := LeaseResponse{Granted: true, Reason: "", LeaseID: "lease-1"}
	var got LeaseResponse
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Granted != true || got.LeaseID != "lease-1" {
		t.Errorf("LeaseResponse mismatch: %+v", got)
	}
}

// ---------- SwarmTask ----------

func TestSwarmTask_RoundTrip(t *testing.T) {
	orig := SwarmTask{ID: "st-1", Role: "coder", Goal: "implement feature", Deps: []string{"st-0"}}
	var got SwarmTask
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "st-1" || got.Role != "coder" || len(got.Deps) != 1 {
		t.Errorf("SwarmTask mismatch: %+v", got)
	}
}

// ---------- RoleSpec ----------

func TestRoleSpec_RoundTrip(t *testing.T) {
	orig := RoleSpec{
		Name:   "reviewer",
		System: "You review code.",
		Policy: map[string]interface{}{"strict": true},
		Rubric: map[string]interface{}{"quality": float64(10)},
		Tools:  []string{"guard", "risk"},
	}
	var got RoleSpec
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "reviewer" || len(got.Tools) != 2 {
		t.Errorf("RoleSpec mismatch: %+v", got)
	}
}

// ---------- GraphNode / GraphEdge ----------

func TestGraphNode_RoundTrip(t *testing.T) {
	orig := GraphNode{
		ID:    "n1",
		Kind:  "file",
		Name:  "main.go",
		Path:  "/src/main.go",
		Props: map[string]string{"lang": "go"},
	}
	var got GraphNode
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "n1" || got.Props["lang"] != "go" {
		t.Errorf("GraphNode mismatch: %+v", got)
	}
}

func TestGraphEdge_RoundTrip(t *testing.T) {
	orig := GraphEdge{From: "n1", To: "n2", Kind: "imports"}
	var got GraphEdge
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.From != "n1" || got.To != "n2" || got.Kind != "imports" {
		t.Errorf("GraphEdge mismatch: %+v", got)
	}
}

// ---------- ToolManifest ----------

func TestToolManifest_RoundTrip(t *testing.T) {
	orig := ToolManifest{
		Name:         "exec",
		Version:      "1.0.0",
		SHA256:       "deadbeef",
		Entrypoint:   "cmd/exec/main.go",
		SchemaIn:     map[string]interface{}{"type": "object"},
		SchemaOut:    map[string]interface{}{"type": "object"},
		Capabilities: []string{"read", "run"},
		RiskClass:    "med",
		TestsReq:     true,
		Owner:        "core",
		Signatures:   []string{"sig1"},
	}
	var got ToolManifest
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "exec" || got.Version != "1.0.0" || got.TestsReq != true || got.Owner != "core" || len(got.Capabilities) != 2 {
		t.Errorf("ToolManifest mismatch: %+v", got)
	}
}

// ---------- Proposal ----------

func TestProposal_RoundTrip(t *testing.T) {
	orig := Proposal{
		Type:     "tool",
		Name:     "better-exec",
		Reason:   "current exec is slow",
		Symptoms: []string{"timeouts"},
		Criteria: []string{"<100ms p99"},
	}
	var got Proposal
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "tool" || got.Name != "better-exec" || len(got.Symptoms) != 1 || len(got.Criteria) != 1 {
		t.Errorf("Proposal mismatch: %+v", got)
	}
}

// ---------- DiagIssue ----------

func TestDiagIssue_RoundTrip(t *testing.T) {
	orig := DiagIssue{Tool: "exec", Problem: "timeout", Impact: 0.9, Count: 5}
	var got DiagIssue
	if err := roundTrip(orig, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "exec" || got.Problem != "timeout" || got.Impact != 0.9 || got.Count != 5 {
		t.Errorf("DiagIssue mismatch: %+v", got)
	}
}

// ---------- Omitempty checks: fields with omitempty should be absent in JSON when zero ----------

func TestOmitempty_ActionEnvelope(t *testing.T) {
	orig := ActionEnvelope{Kind: "done", Answer: "ok"}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// name, args, diff, why, payload should be absent
	for _, key := range []string{"name", "args", "diff", "why", "payload"} {
		if _, exists := m[key]; exists {
			t.Errorf("omitempty field %q should be absent in JSON for kind=done", key)
		}
	}
}

// ---------- JSON from external source ----------

func TestActionEnvelope_UnmarshalFromJSON(t *testing.T) {
	raw := `{"kind":"tool","name":"guard","args":{"path":"/tmp"},"why":"check safety"}`
	var got ActionEnvelope
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "tool" || got.Name != "guard" || got.Why != "check safety" {
		t.Errorf("unmarshal from raw JSON mismatch: %+v", got)
	}
}
