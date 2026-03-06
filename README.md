# Clock

A modular AI-assisted engineering system built on Unix philosophy.

Clock is a collection of small, composable tools that work together through structured JSON pipelines to create a transparent, safe, and programmable alternative to monolithic coding agents.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash
```

Or with custom install directory:

```bash
CLOCK_INSTALL_DIR=$HOME/.local/bin curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash
```

To uninstall:

```bash
curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash -s -- --uninstall
```

**Requirements:** Go 1.21+, git. Optional: ripgrep (`rg`), `jq`.

## Philosophy

1. **Unix philosophy** — small programs that do one thing well, composed via `stdin | stdout`
2. **Transparency** — every step is visible, logged, and replayable via trace logs
3. **Safety first** — all changes are patches, all patches are validated, all changes are verified, rollback is always available

## Architecture

Clock is organized into five layers:

```
Layer 5: Self-Evolution    — diag, self, spec, forge, test, bench, gate, prom, roll, audit
Layer 4: Distributed       — swrm, role, hub, shrd, knox, link, pbk, judge, ablt, repl, farm, sync, orch
Layer 3: Autonomous        — dock, tick, watch, q, job, work, mem, eval, aprv, push, lease, mode
Layer 2: Core Toolbox      — scan, scope, srch, slce, pack, map, ctrt, flow, doss, rfrsh
Layer 1: Agent Loop        — llm, act, guard, aply, vrfy, undo, rpt + microtools
Layer 0: Infrastructure    — trce, exec, jolt, budg, anch, risk, dect, graf, mcp
```

## Quick Start

### Prerequisites

```bash
# Required
go 1.21+
git
rg (ripgrep)
jq

# Optional
sqlite3
```

Verify dependencies:

```bash
make doctor
```

### Build

```bash
# Build all tools (Go binaries + shell scripts)
make all

# Binaries are placed in bin/
ls bin/
```

### Initialize a project

```bash
cd /path/to/your/repo
clock init
```

This creates a `.clock/` directory with:
- `doss.md` — project dossier (macro context for the AI)
- `policy.json` — edit/run safety policies
- `trce.jsonl` — trace log

### Ask questions about the codebase

```bash
clock ask "how does the authentication system work?"
clock ask "what are the main API endpoints?"
```

### Fix issues with AI assistance

```bash
clock fix "fix the failing login test"
clock fix "add input validation to the signup handler"
```

The agent loop:
1. Searches the codebase for relevant code
2. Builds context with the project dossier + code slices
3. Sends to the LLM for analysis
4. Validates proposed patches with `guard`
5. Applies changes atomically with checkpoints
6. Runs verification (tests, lint, etc.)
7. Rolls back automatically on failure

### Start autonomous mode

```bash
# Start the daemon (watches for events, runs jobs)
clock start

# Check status
clock status

# Stop
clock stop
```

## Tool Reference

### Core Toolbox (V1)

All tools communicate via JSON/JSONL on stdin/stdout.

| Tool | Type | Purpose |
|------|------|---------|
| `scan` | shell | Repo facts and runnable commands discovery |
| `scope` | shell | Define workset (in-bounds files) |
| `srch` | shell | Fast code search with ranking (wraps ripgrep) |
| `slce` | shell | Deterministic file excerpt extraction |
| `map` | shell | Repo structure and entrypoints mapper |
| `ctrt` | shell | Contracts extractor (APIs, schemas, env vars) |
| `flow` | shell | Data flow and side effects map |
| `doss` | shell | Macro brief compiler (project dossier) |
| `pack` | shell | Context packer (dossier + code slices) |
| `rpt` | shell | Human-readable summary report |
| `undo` | shell | Revert to checkpoint |
| `llm` | Go | LLM API client (Anthropic, OpenAI, Ollama) |
| `act` | Go | Action grammar enforcer and dispatcher |
| `guard` | Go | Diff validator with policy enforcement |
| `aply` | Go | Atomic patch application with checkpoints |
| `vrfy` | Go | Run verification checks (test, lint, fmt) |
| `rfrsh` | Go | Dossier refresh trigger |

### Go Microtools

| Tool | Purpose |
|------|---------|
| `jolt` | Streaming JSONL plumbing (pick, filter, merge, count) |
| `budg` | Hard context budgeting and packing |
| `anch` | Resilient patch anchoring (handles line drift) |
| `risk` | Change risk scoring (0.0-1.0) |
| `exec` | Policy-based command runner with sandboxing |
| `dect` | Repo capability detection (auto-configure verify plans) |
| `graf` | Lightweight import/call graph |
| `trce` | Append-only trace log and replay metadata |
| `mcp` | MCP protocol bridge |

### Autonomous System (V2)

| Tool | Type | Purpose |
|------|------|---------|
| `dock` | Go | Clock daemon supervisor |
| `tick` | shell | Cron-like scheduler |
| `watch` | shell | Event watcher (git, CI) |
| `q` | Go | Durable job queue (file-spool) |
| `job` | shell | Event-to-job compiler |
| `work` | Go | Worker executor (runs V1 agent loop) |
| `mem` | Go | Durable knowledge store |
| `eval` | Go | Plan safety evaluator |
| `aprv` | Go | Human approval gateway |
| `push` | Go | Result delivery (GitHub, Slack, file) |
| `lease` | Go | Resource governor (concurrency, budgets) |
| `mode` | Go | Autonomy mode controller |

### Distributed / Swarm (V3)

| Tool | Purpose |
|------|---------|
| `swrm` | Swarm coordinator (decompose jobs into sub-tasks) |
| `role` | Role prompt and policy loader (plan, edit, revw, test, sec, perf) |
| `hub` | Message bus for agent coordination |
| `shrd` | Content-addressed artifact store |
| `knox` | Knowledge graph store |
| `link` | Evidence linker (facts extraction to graph) |
| `pbk` | Playbook generator (learn procedures from success) |
| `note` | Long-term notes and memory |
| `rank` | Retrieval ranking improvement |
| `judge` | Automated evaluation harness |
| `ablt` | A/B and ablation runner |
| `repl` | Replay engine (deterministic re-execution) |
| `farm` | Distributed worker pool manager |
| `sync` | State synchronizer across nodes |
| `orch` | Global orchestrator (multi-repo campaigns) |

### Self-Improvement (V4)

| Tool | Purpose |
|------|---------|
| `diag` | System diagnostics (trace analysis, bottleneck detection) |
| `self` | Self-analysis entry point (improvement proposals) |
| `spec` | Tool specification generator |
| `forge` | Tool builder (generate candidate implementations) |
| `test` | Tool test runner |
| `bench` | Performance benchmark |
| `gate` | Promotion gatekeeper |
| `prom` | Tool promotion (install to registry) |
| `roll` | Rollback tool versions |
| `audit` | Security audit for tools |

### LLM and Remote Access

| Tool | Purpose |
|------|---------|
| `lmux` | LLM multiplexer/router (fallback, race, round-robin) |
| `mset` | Model preset manager (role-to-model mappings) |
| `rcon` | Remote connection manager (SSH + Tailscale + tmux) |
| `keys` | SSH key and device access manager |

## Pipelines

Clock tools compose like Unix pipes:

```bash
# Search, extract context, ask the LLM
srch | slce | pack | llm | act

# Full change pipeline
srch | slce | pack | llm | act | guard | aply | vrfy

# Build macro context
scan | map + ctrt + flow | doss

# Autonomous event pipeline
watch | job | q put
q take | work | push
```

## LLM Configuration

### Environment variables

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
export CLOCK_PROVIDER="anthropic"       # default provider
export CLOCK_MODEL="claude-sonnet-4-20250514"  # default model
```

### Model presets (via mset)

```bash
# Set models per role
echo '{"action":"set","role":"planner","model":"anth:sonnet"}' | mset set
echo '{"action":"set","role":"editor","model":"oai:gpt4o"}' | mset set
echo '{"action":"set","role":"reviewer","model":"anth:opus"}' | mset set

# Set fallback chains
echo '{"action":"set","role":"editor","model":["oai:gpt4o","oll:llama3"]}' | mset set

# View all mappings
mset list
```

### Model naming convention

```
anth:sonnet    -> Anthropic Claude Sonnet
anth:opus      -> Anthropic Claude Opus
oai:gpt4o      -> OpenAI GPT-4o
oll:llama3     -> Ollama (local) Llama 3
vllm:qwen      -> vLLM server
lcpp:mistral   -> llama.cpp server
```

## Autonomy Modes

```bash
# Set autonomy level
mode set read       # analysis only
mode set suggest    # propose changes, don't apply
mode set pr         # open PRs, no direct branch edits
mode set auto       # apply low-risk fixes directly
mode set ops        # multi-repo campaigns (requires governance)

# Check if an action is allowed
mode check patch
```

## Remote Development

Connect to your dev machine from your phone:

```bash
# Add a connection
echo '{"name":"devbox","host":"mydevbox","user":"clock","repo":"/home/clock/projects/myrepo","mode":"ro","transport":"tailscale"}' | rcon add

# Get the SSH command
rcon connect devbox

# Run a command remotely
rcon run devbox clock ask "summarize this repo"
```

### Device key management

```bash
# Authorize a device
echo '{"action":"add","name":"iphone","pubkey":"ssh-ed25519 AAAA...","mode":"ro"}' | keys add

# List authorized devices
keys list

# Generate SSH authorized_keys with restrictions
keys sync >> ~/.ssh/authorized_keys
```

## Safety Model

Clock enforces strict safety at every level:

1. **All code changes are patches** — no freestyle file writes
2. **Patches must pass validation** — `guard` checks policies (max files, forbidden paths, etc.)
3. **Risk scoring** — `risk` scores changes 0.0-1.0; high-risk triggers `eval` + `aprv`
4. **Verification runs after every change** — `vrfy` runs tests, lint, format checks
5. **Rollback always available** — `undo` restores from checkpoints
6. **All actions are traced** — `trce` logs everything to `.clock/trce.jsonl`
7. **High-risk changes require approval** — `aprv` provides human-in-the-loop
8. **Resource governance** — `lease` prevents runaway compute/cost

## Self-Improvement Pipeline

Clock can analyze and improve itself:

```
diag → self → spec → forge → test + audit + bench → gate → prom → [rollback via roll]
```

1. **Observe** — `diag` analyzes trace logs for bottlenecks and failures
2. **Propose** — `self` generates improvement proposals
3. **Specify** — `spec` creates a machine-readable tool contract
4. **Build** — `forge` generates candidate implementations
5. **Validate** — `test` + `audit` + `bench` verify correctness, security, performance
6. **Gate** — `gate` enforces acceptance criteria
7. **Promote** — `prom` installs to the tool registry
8. **Rollback** — `roll` reverts if regressions appear

The kernel (act, eval, exec, trce, q, shrd) is immutable and cannot be self-modified.

## Project Structure

```
clock/
  cmd/                  # Go binary entrypoints (one per tool)
    clock/main.go       # Main CLI orchestrator
    llm/main.go         # LLM API client
    act/main.go         # Action enforcer
    guard/main.go       # Diff validator
    ...                 # 52 Go tools total
  scripts/              # Shell script tools
    scan.sh             # Repo scanner
    srch.sh             # Code search
    ...                 # 15 shell tools total
  internal/
    common/types.go     # Shared types (ActionEnvelope, TraceEvent, etc.)
    jsonutil/jsonutil.go # JSON I/O helpers
  docs/                 # Design specifications
  bin/                  # Built binaries (67 tools)
  .clock/               # Runtime state (per-project)
    doss.md             # Project dossier
    policy.json         # Safety policies
    trce.jsonl          # Trace log
    queue/              # Job queue (file-spool)
    mem/                # Durable memory
    knox/               # Knowledge graph
    hub/                # Message bus channels
    shrd/               # Content-addressed artifacts
    tools/              # V4 mutable toolspace
    approvals/          # Human approval requests
    playbooks/          # Learned procedures
    notes/              # Long-term notes
    campaigns/          # Multi-repo campaign state
    farm/               # Distributed worker registry
  Makefile
  go.mod
```

## Installation

**One-liner:**

```bash
curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash
```

**Manual:**

```bash
git clone https://github.com/pedrommaiaa/clock
cd clock
make all
```

To install system-wide:

```bash
sudo make install
```

Or add `bin/` to your PATH:

```bash
export PATH="/path/to/clock/bin:$PATH"
```

**Uninstall:**

```bash
curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash -s -- --uninstall
```

## License

MIT
