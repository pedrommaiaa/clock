// Package common provides shared types for the Clock toolbox.
package common

// ActionEnvelope is the standard LLM response format.
type ActionEnvelope struct {
	Kind    string      `json:"kind"`              // tool, patch, done, run
	Name    string      `json:"name,omitempty"`    // tool name (for kind=tool)
	Args    interface{} `json:"args,omitempty"`    // tool arguments
	Diff    string      `json:"diff,omitempty"`    // unified diff (for kind=patch)
	Answer  string      `json:"answer,omitempty"`  // final answer (for kind=done)
	Why     string      `json:"why,omitempty"`     // reasoning
	Payload interface{} `json:"payload,omitempty"` // normalized payload from act
}

// TraceEvent represents a single event in the trace log.
type TraceEvent struct {
	TS    int64       `json:"ts"`
	Event string      `json:"event"` // tool.call, tool.out, llm.in, llm.out
	Tool  string      `json:"tool,omitempty"`
	Data  interface{} `json:"data,omitempty"`
	Ms    int64       `json:"ms,omitempty"`
	ChkID string     `json:"chk,omitempty"`
}

// GuardResult is the output of the guard tool.
type GuardResult struct {
	OK      bool     `json:"ok"`
	Risk    float64  `json:"risk"`
	Reasons []string `json:"reasons,omitempty"`
	Needs   []string `json:"needs,omitempty"`
}

// ApplyResult is the output of the aply tool.
type ApplyResult struct {
	OK    bool     `json:"ok"`
	ChkID string   `json:"chk,omitempty"`
	Files []string `json:"files,omitempty"`
	Lines struct {
		Add int `json:"add"`
		Del int `json:"del"`
	} `json:"lines"`
	Err string `json:"err,omitempty"`
}

// VerifyResult is the output of the vrfy tool.
type VerifyResult struct {
	OK    bool          `json:"ok"`
	Steps []VerifyStep  `json:"steps,omitempty"`
	Fail  *VerifyStep   `json:"fail,omitempty"`
	Logs  string        `json:"logs,omitempty"`
}

// VerifyStep is a single verification step result.
type VerifyStep struct {
	Name   string `json:"name"`
	Cmd    string `json:"cmd"`
	OK     bool   `json:"ok"`
	Code   int    `json:"code"`
	Output string `json:"output,omitempty"`
	Ms     int64  `json:"ms"`
}

// RiskResult is the output of the risk tool.
type RiskResult struct {
	Risk  float64  `json:"risk"`
	Class string   `json:"class"` // low, med, high
	Must  []string `json:"must,omitempty"`
	Why   []string `json:"why,omitempty"`
}

// ExecResult is the output of the exec tool.
type ExecResult struct {
	Code int    `json:"code"`
	Out  string `json:"out"`
	Err  string `json:"err"`
	Ms   int64  `json:"ms"`
}

// ScanResult is the output of the scan tool.
type ScanResult struct {
	Root      string            `json:"root"`
	Languages []string          `json:"languages"`
	Managers  []string          `json:"managers"`
	Commands  map[string]string `json:"commands"`
	KeyFiles  []string          `json:"key_files"`
}

// ScopeEntry is a single file in the workset.
type ScopeEntry struct {
	Path string `json:"path"`
}

// SearchHit is a single search result.
type SearchHit struct {
	Path  string  `json:"path"`
	Line  int     `json:"line"`
	Col   int     `json:"col,omitempty"`
	Text  string  `json:"text"`
	Score float64 `json:"score,omitempty"`
}

// SliceResult is the output of the slce tool.
type SliceResult struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// MapResult is the output of the map tool.
type MapResult struct {
	Outline   []DirEntry   `json:"outline"`
	KeyFiles  []string     `json:"key_files"`
	HotSpots  []HotSpot    `json:"hot_spots,omitempty"`
}

// DirEntry represents a directory in the tree.
type DirEntry struct {
	Path     string     `json:"path"`
	Files    int        `json:"files"`
	Children []DirEntry `json:"children,omitempty"`
}

// HotSpot is a frequently-changed or large file.
type HotSpot struct {
	Path    string `json:"path"`
	Reason  string `json:"reason"`
	Value   int    `json:"value,omitempty"`
}

// ContractResult is the output of the ctrt tool.
type ContractResult struct {
	Exports []ContractEntry `json:"exports,omitempty"`
	Routes  []ContractEntry `json:"routes,omitempty"`
	CLI     []ContractEntry `json:"cli,omitempty"`
	Env     []ContractEntry `json:"env,omitempty"`
	Schemas []ContractEntry `json:"schemas,omitempty"`
}

// ContractEntry is a single contract item.
type ContractEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// FlowResult is the output of the flow tool.
type FlowResult struct {
	Pipelines []FlowPipeline `json:"pipelines,omitempty"`
	Edges     []FlowEdge     `json:"edges,omitempty"`
	SideFx    []string       `json:"sidefx,omitempty"`
	Notes     string         `json:"notes,omitempty"`
}

// FlowPipeline is a data flow pipeline.
type FlowPipeline struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

// FlowEdge is a dependency edge.
type FlowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

// PackBundle is the prompt bundle built by pack.
type PackBundle struct {
	System   string      `json:"system"`
	Messages []Message   `json:"messages"`
	Tools    []ToolDef   `json:"tools,omitempty"`
	Policy   interface{} `json:"policy,omitempty"`
	Citations []Citation `json:"citations,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolDef is a tool definition for the LLM.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Schema      interface{} `json:"input_schema,omitempty"`
}

// Citation is a source reference.
type Citation struct {
	Path  string `json:"path"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

// JobSpec is a V2 job specification.
type JobSpec struct {
	ID       string   `json:"id"`
	Goal     string   `json:"goal"`
	Repo     string   `json:"repo"`
	Scope    []string `json:"scope,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Plan     []string `json:"plan,omitempty"`
	Mode     string   `json:"mode,omitempty"`
}

// JobResult is the outcome of a job.
type JobResult struct {
	ID     string      `json:"id"`
	OK     bool        `json:"ok"`
	Goal   string      `json:"goal"`
	Diff   string      `json:"diff,omitempty"`
	Verify *VerifyResult `json:"verify,omitempty"`
	Report string      `json:"report,omitempty"`
	Err    string      `json:"err,omitempty"`
}

// QueueEntry is a job in the queue.
type QueueEntry struct {
	ID        string  `json:"id"`
	Job       JobSpec `json:"job"`
	Status    string  `json:"status"` // pending, leased, done, failed, dead
	Retries   int     `json:"retries"`
	CreatedAt int64   `json:"created_at"`
	LeasedAt  int64   `json:"leased_at,omitempty"`
	DoneAt    int64   `json:"done_at,omitempty"`
}

// EventMsg is a V2 event message.
type EventMsg struct {
	Type   string      `json:"type"` // git.push, ci.fail, issue.open, schedule
	Repo   string      `json:"repo,omitempty"`
	Branch string      `json:"branch,omitempty"`
	Meta   interface{} `json:"meta,omitempty"`
}

// LeaseRequest is the input to the lease tool.
type LeaseRequest struct {
	JobID      string `json:"job_id"`
	Resource   string `json:"resource"`
	Tokens     int    `json:"tokens,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// LeaseResponse is the output of the lease tool.
type LeaseResponse struct {
	Granted bool   `json:"granted"`
	Reason  string `json:"reason,omitempty"`
	LeaseID string `json:"lease_id,omitempty"`
}

// SwarmTask is a sub-task in a swarm decomposition.
type SwarmTask struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Goal string `json:"goal"`
	Deps []string `json:"deps,omitempty"`
}

// RoleSpec is a role definition.
type RoleSpec struct {
	Name    string      `json:"name"`
	System  string      `json:"system"`
	Policy  interface{} `json:"policy,omitempty"`
	Rubric  interface{} `json:"rubric,omitempty"`
	Tools   []string    `json:"tools,omitempty"`
}

// GraphNode is a node in the knowledge/import graph.
type GraphNode struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"` // file, module, function, symbol
	Name  string            `json:"name"`
	Path  string            `json:"path,omitempty"`
	Props map[string]string `json:"props,omitempty"`
}

// GraphEdge is an edge in the graph.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // imports, calls, depends
}

// ToolManifest is a V4 tool registry entry.
type ToolManifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	SHA256       string   `json:"sha256"`
	Entrypoint   string   `json:"entrypoint"`
	SchemaIn     interface{} `json:"schema_in,omitempty"`
	SchemaOut    interface{} `json:"schema_out,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"` // read, write, run, net
	RiskClass    string   `json:"risk_class,omitempty"`
	TestsReq     bool     `json:"tests_required"`
	Owner        string   `json:"owner"` // core, userland
	Signatures   []string `json:"signatures,omitempty"`
}

// Proposal is a V4 self-improvement proposal.
type Proposal struct {
	Type     string   `json:"type"`     // tool, playbook, policy, workflow
	Name     string   `json:"name"`
	Reason   string   `json:"reason"`
	Symptoms []string `json:"symptoms,omitempty"`
	Criteria []string `json:"criteria,omitempty"`
}

// DiagIssue is a diagnostics finding.
type DiagIssue struct {
	Tool    string  `json:"tool"`
	Problem string  `json:"problem"`
	Impact  float64 `json:"impact,omitempty"`
	Count   int     `json:"count,omitempty"`
}
