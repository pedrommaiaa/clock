# Clock — Implementation TODO

Comprehensive implementation plan derived from `docs/v1.txt` through `docs/v4.txt`, `docs/tools.txt`, and `docs/self.txt`.

---

## Phase 0 — Project Bootstrap

- [x] Initialize Go module (`go mod init`)
- [x] Create directory structure:
  - `cmd/` — binary entrypoints (one per tool)
  - `internal/` — shared internal packages
  - `scripts/` — shell-based tools
  - `pkg/` — public library code (if needed)
- [x] Set up `.clock/` runtime directory structure:
  - `.clock/config.json` — system configuration
  - `.clock/policy.json` — edit/run policies
  - `.clock/trce.jsonl` — trace log
  - `.clock/doss.md` — macro dossier
  - `.clock/tools/` — V4 mutable toolspace
  - `.clock/q.db` — job queue (file-spool based)
  - `.clock/mem/` — durable memory (file-based)
  - `.clock/approvals/inbox/` — approval requests
- [x] Define shared JSON schemas for tool I/O contracts
- [x] Set up build system (Makefile or Taskfile) to compile all Go binaries + install shell scripts
- [x] Add `clock doctor` command to verify external deps (`rg`, `git`, `sed`, `awk`, `jq`, `sqlite3`)

---

## Phase 1 — V1 Core Toolbox

### Shell Tools

- [x] **scan** — Repo facts & runnable commands discovery
  - Inspect filesystem, config files (`package.json`, `go.mod`, `pyproject.toml`, `Makefile`, CI configs), git metadata
  - Output JSON: repo root, languages, package managers, candidate commands (format/lint/test/build), key files
- [x] **scope** — Define workset (in-bounds files)
  - Use `git ls-files` + include/exclude globs
  - Output JSONL: `{ "path": "..." }` per file
  - Support `mode: tracked|all`, `limit` cap
- [x] **srch** — Fast code search with ranking
  - Wrap `rg` with JSONL output
  - Input: query string, path filters, globs, max results
  - Output JSONL: `{ "path", "line", "col", "text", "score" }`
  - Lightweight ranking: match count, proximity, file weight
- [x] **slce** — Deterministic excerpt extraction
  - Use `sed`/`awk` for line-range extraction
  - Input: path, start, end, pad (context expansion)
  - Output JSON: `{ "path", "start", "end", "text" }`

### Shell or Go Tools (start shell, upgrade later)

- [x] **map** — Repo structure & entrypoints mapper
  - Tree summary, key directories, entrypoints, configs, hot spots
  - Input: paths, max_depth, max_files
  - Output JSON: directory outline, key files, hot spots
- [x] **ctrt** — Contracts extractor (APIs, schemas, CLIs, env vars)
  - Pattern-based extraction using `rg` heuristics
  - Output JSON: exports, routes, cli flags, env vars, schemas
- [x] **flow** — Data flow & side effects map
  - Combine map + ctrt + targeted searches
  - Output JSON: pipelines, edges, side effects, notes
- [x] **doss** — Macro brief compiler (dossier)
  - Summarize map/ctrt/flow into compact artifact
  - Output: Markdown (primary) or JSON
  - Sections: overview, module roles, invariants, safe/risky zones, how to run checks
- [x] **pack** — Context packer (micro + macro)
  - Combine dossier + selected slices; dedupe; enforce size limits
  - Output JSON: `{ "system", "messages", "tools", "policy", "citations" }`
  - *Upgrade to Go if budgeting + dedupe + stable ordering needed*

### Go Binaries — Agent Loop

- [x] **llm** — LLM API client
  - Support providers: Anthropic, OpenAI, Ollama
  - Strict JSON enforcement + retries
  - Input: provider, model, prompt bundle
  - Output: action envelope `{ "kind": "tool|patch|done", ... }`
- [x] **act** — Action grammar enforcer / dispatcher
  - Schema validation + normalization
  - Input: LLM envelope
  - Output: `{ "kind": "srch|slce|patch|run|done", "payload": {...} }`
  - Reject ambiguous/malformed output
- [x] **guard** — Diff validator
  - Parse unified diffs + apply policies
  - Checks: max files/lines, forbidden paths, require context lines, deny binary
  - Output: `{ "ok", "risk", "reasons", "needs" }`
- [x] **aply** — Atomic patch application
  - Create checkpoint (stash/commit), apply diff, emit summary, refuse on conflict
  - Output: `{ "ok", "chk", "files", "lines", "err" }`
- [x] **vrfy** — Run verification checks
  - Use commands from `scan` or user-provided plan
  - Structured capture + timeouts
  - Output: `{ "ok", "steps", "fail", "logs" }`

### Shell Tools — Post-loop

- [x] **undo** — Revert to checkpoint
  - Restore from stash/commit/checkpoint ID from `aply`
  - Output: `{ "ok", "did", "err" }`
- [x] **rpt** — Human-readable summary report
  - Combine goal + diffs + verify results + citations
  - Output: Markdown (summary, files changed, rationale, verification, next steps)
- [x] **rfrsh** — Dossier refresh trigger (Go)
  - Compare git diff / file timestamps to thresholds
  - Rebuild dossier when stale via map/ctrt/flow/doss
  - Output: `{ "stale", "reason", "did" }`

### Go Microtools

- [x] **jolt** — Streaming JSONL plumbing
  - Operations: pick, merge, filter, validate, count, head
  - Input: JSONL stdin + flags
  - Output: JSONL stdout
- [x] **budg** — Hard context budgeting and packing
  - Rank + dedupe + compress; guarantee budget compliance
  - Input JSONL: candidate snippets with scores + constraints
  - Output JSON: packed set + stats `{ "used_bytes", "dropped", "rationale" }`
- [x] **anch** — Resilient patch anchoring
  - Re-anchor hunks using nearby unique anchors; refuse on ambiguity
  - Output: `{ "ok", "rebased", "diff2", "why" }`
- [x] **risk** — Change risk scoring
  - Score diffs by touched zones, file types, hunk size, test impact
  - Output: `{ "risk", "class", "must", "why" }`
- [x] **exec** — Policy-based command runner (sandbox)
  - Enforce allowlist, restrict cwd, scrub env, kill process tree, truncate logs
  - Output: `{ "code", "out", "err", "ms" }`
- [x] **dect** — Repo capability detection
  - Autoconfigure verify plans across unknown repos
  - Detect scripts, toolchains, monorepo layout, CI hints
  - Output: `{ "fmt", "lint", "test", "build", "notes" }`
- [x] **graf** — Lightweight import/call graph
  - Fast parsing or heuristics per language
  - Output: `{ "nodes", "edges", "index" }`
- [x] **trce** — Append-only trace log + replay metadata
  - Events: tool.call, tool.out, llm.in, llm.out
  - Output: JSONL appended to `.clock/trce.jsonl`
- [x] **mcp** — MCP protocol bridge
  - Expose Clock tools as MCP endpoints
  - Consume external MCP tools as local commands

### CLI Orchestrator

- [x] **clock init** — Initialization flow
  - Run: scan → scope → map + ctrt + flow → doss
  - Write: `.clock/doss.md`, `.clock/policy.json`, `.clock/trce.jsonl`
- [x] **clock ask** — Read-only analysis flow
  - Run: rfrsh → scope → srch → slce → pack → llm → rpt
- [x] **clock fix** — Full agent loop (change flow)
  - Step A (Prepare): rfrsh → scope → scan/dect → trce
  - Step B (Iterate): srch → slce → pack → llm → act → guard/risk → aply/anch → vrfy → undo (on fail)
  - Step C (Finish): git diff summary → verify results → rpt
- [x] Enforce safety invariants:
  - Only write path: patch → guard → aply
  - Every apply creates checkpoint
  - Every patch followed by vrfy
  - Failures trigger automatic undo
  - All actions traceable via trce

---

## Phase 2 — V2 Autonomous System

### Priority order (highest leverage first)

- [x] **q** — Durable job queue (Go, file-spool)
  - Operations: put, take, ack, fail
  - Support: leases, retries with backoff, dead-letter queue
  - Store in `.clock/queue/`
- [x] **work** — Worker executor (Go)
  - Fetch job from queue
  - Run V1 loop: rfrsh → scope → srch → slce → pack → llm → act → guard → aply → vrfy
  - Handle: checkpoint, rollback, retry, produce result artifact
- [x] **watch** — Event watcher (shell, polling-based)
  - Sources: git commits, CI failures
  - Output JSONL events: `{ "type": "git.push", "repo", "branch" }`
- [x] **dock** — Clock daemon supervisor (Go)
  - Load config, initialize workers, launch watchers, manage queue consumers
  - Enforce resource limits, record traces
  - No agent logic — only orchestration
- [x] **eval** — Plan safety evaluator (Go)
  - Score proposed actions for risk
  - Check: touching migrations, editing deps, large refactors
  - Output: `{ "risk", "class", "requires" }`
- [x] **aprv** — Human approval gateway (Go)
  - Modes: approve, reject, defer
  - Interface: `.clock/approvals/inbox/*.json` + `clock ok <id>`
- [x] **push** — Result delivery / notifier (Go)
  - Targets: GitHub PR comments, Slack, file, stdout
  - Input: result JSON → delivery confirmation
- [x] **mem** — Durable knowledge store (Go, file-based)
  - Store: repo fingerprints, successful fixes, command heuristics, dossier history, playbooks
  - Query interface for retrieval
- [x] **lease** — Resource governor (Go)
  - Controls: job concurrency, token budgets, time limits, quiet hours
  - Output: permit or denial
- [x] **tick** — Scheduler (shell)
  - Cron-like task scheduling
  - Emit job events to queue
- [x] **job** — Event-to-job compiler (shell)
  - Match event patterns → job specs
  - Output: `{ "goal", "scope", "priority", "verify_plan" }`
- [x] **mode** — Autonomy mode controller (Go)
  - Modes: read, suggest, pr, auto, ops
  - Enforce capabilities per mode

### CLI Commands

- [x] **clock start** — Start daemon (dock + watch + tick + workers + queue)
- [x] **clock stop** — Stop daemon gracefully
- [x] **clock status** — Show system status

---

## Phase 3 — V3 Distributed / Swarm

### Priority order

- [x] **shrd** — Shared artifact store (Go)
  - Content-addressed storage (sha256)
  - Store: prompt bundles, slices, diffs, verify logs, dossiers
  - Input: `{ "put": { "type", "data" } }` → Output: `{ "ref": "sha256:..." }`
- [x] **swrm** — Swarm coordinator (Go)
  - Decompose job into sub-tasks
  - Assign tasks to roles (planner/editor/reviewer/tester/sec/perf)
  - Merge results into single outcome
- [x] **role** — Role prompt + policy loader (Go)
  - Role templates: system prompt + allowed tools + constraints
  - Built-in roles: plan, edit, revw, test, sec, perf
  - Output: `{ "system", "policy", "rubric" }`
- [x] **hub** — Message bus for agents (Go)
  - Publish/subscribe channels
  - Job/task status updates
  - Shared context artifact references
- [x] **knox** — Knowledge graph store (Go, file-based)
  - Store: modules→responsibilities, APIs→invariants, failures→fixes→outcomes
  - Graph operations: add node/edge, query, attach evidence
- [x] **link** — Evidence linker (Go)
  - Extract candidate facts from slices/diffs/logs
  - Attach citations (file/line, trace IDs)
  - Write updates to knox
- [x] **pbk** — Playbook generator (Go)
  - Detect repeated patterns from job outcomes + traces
  - Output: trigger conditions, steps, verification requirements, risk class
- [x] **judge** — Automated evaluation harness (Go)
  - Replay bench suite corpus of tasks
  - Score: correctness, minimal diffs, policy adherence, latency/cost
  - Compare prompt/tool versions
- [x] **repl** — Replay engine (Go)
  - Deterministic re-execution from traces + shrd artifacts
  - Optionally substitute stored LLM responses
- [x] **farm** — Distributed worker pool manager (Go)
  - Register worker nodes, lease jobs, ensure consistent tool versions
  - Route artifacts via shrd
- [x] **sync** — State synchronizer (Go)
  - Sync: queue states, artifact refs, graph updates, playbooks
  - Conflict resolution via timestamps + content hashing
- [x] **orch** — Global orchestrator (Go)
  - Multi-repo, multi-goal campaigns
  - Higher-level objectives: "keep all repos green", "reduce build times"
  - Track results over time
- [x] **note** — Long-term notes (Go)
  - Distill trace logs into short notes
  - Store "what we learned" — rollups by repo/component
- [x] **rank** — Retrieval ranking improvement (Go)
  - Learn which slices helped; boost frequently-relevant files
  - Feed into budg and pack
- [x] **ablt** — A/B and ablation runner (Go)
  - Test tool/prompt variations (different models, retrieval policies)
  - Select best outcome per rubric

### Autonomy Modes (V3 extension)

- [x] Add `ops` mode to **mode** tool — multi-repo campaigns, requires strict governance + audit

---

## Phase 4 — V4 Self-Improvement

### Architecture

- [x] Define **Immutable Kernel** boundary
  - Kernel = tool protocol (act), policy gates (eval, aprv), sandbox (exec), artifact hashing (shrd), trace (trce), queue (q), version registry, rollback
  - Kernel cannot modify itself automatically
- [x] Define **Mutable Toolspace** structure
  - `.clock/tools/<toolname>/<version>/...`
  - Each tool: manifest with version, permissions, I/O schema, hash, tests
- [x] Implement **Tool Registry** (the "constitution")
  - Fields: name, version, sha256, entrypoint, schema_in, schema_out, capabilities, risk_class, tests_required, owner (core/userland), signatures

### Self-Improvement Tools

- [x] **diag** — System diagnostics (Go)
  - Analyze: trace logs, job outcomes, token usage, retry rates, slow tools
  - Output: ranked pain points / issues list
- [x] **self** — Self-analysis entry point (Go)
  - Analyze traces, failures, slow operations, repetitive work
  - Output: structured improvement proposals (target area, symptoms, hypothesis, change type, acceptance criteria)
- [x] **spec** — Tool specification generator (Go)
  - Machine-readable tool contract: name, goal, I/O schema, permissions, test plan, examples
- [x] **forge** — Tool builder (Go)
  - Generate candidate implementation as artifact bundle (source, tests, manifest, build instructions)
  - Store in shrd, not installed
- [x] **test** — Tool test runner (shell wrapper around exec)
  - Run: unit tests, schema validation, I/O conformance
- [x] **bench** — Performance benchmark (Go)
  - Measure: latency, memory, token cost, accuracy
  - Compare candidate vs baseline
- [x] **gate** — Promotion gatekeeper (Go)
  - Enforce: all tests pass, no policy violations, no regressions, approval for high-risk, signatures present
- [x] **prom** — Tool promotion (Go)
  - Install artifact into toolspace, update registry, pin version hash
- [x] **roll** — Rollback tool versions (Go)
  - Revert registry state to previous version
- [x] **audit** — Security audit (Go)
  - Check: permissions match spec, forbidden syscalls/patterns, supply-chain integrity, hash verification

### Self-Improvement Pipeline

- [x] Implement the full pipeline:
  1. **Observe**: diag → identify weaknesses
  2. **Propose**: self → improvement proposal
  3. **Define**: spec → tool contract
  4. **Build**: forge → artifact bundle
  5. **Validate**: test + audit + bench
  6. **Gate**: gate → approve/reject (+ aprv for high-risk)
  7. **Promote**: prom → registry update
  8. **Rollback**: roll → revert if regressions

### Safety Model

- [x] Implement capability-based permissions per tool (read:repo, write:repo, run:cmd, net:outbound)
- [x] Implement signed artifacts (content-addressed, registry-pinned, optional key signing)
- [x] Enforce kernel immutability — kernel changes always require human approval

---

## Cross-Cutting Concerns

### LLM Independence

- [x] **lmux** — LLM multiplexer / router
  - Route requests to multiple providers simultaneously
  - Support: Anthropic, OpenAI, Ollama (via fallback/race/round-robin strategies)
- [x] **mset** — Model preset manager (role-to-model mappings with fallback chains)

### Remote Development

- [x] **rcon** — Remote connection manager
  - SSH + Tailscale + tmux integration
- [x] **keys** — Key/credential management for remote access

### Packaging & Distribution

- [x] Bundle all Go binaries into single `clock` release
- [x] Ship shell scripts alongside binaries
- [ ] Document external dependencies
- [x] `clock doctor` — verify all dependencies present

### Testing & Quality

- [ ] Unit tests for each Go tool
- [ ] Integration tests for each CLI workflow (init, ask, fix)
- [ ] End-to-end test: full agent loop on sample repo
- [ ] Trace replay tests (deterministic re-execution)

---

## Implementation Language Summary

| Tier | Language | Tools |
|------|----------|-------|
| A | Unix (no code) | Use `rg`, `git`, `sed`, `awk`, `jq` directly |
| B | Shell scripts | scan, scope, srch, slce, map, ctrt, flow, doss, pack, undo, rpt, tick, job, watch, test |
| C | Go binaries | llm, act, guard, aply, vrfy, rfrsh, jolt, budg, anch, risk, exec, dect, graf, trce, mcp, dock, q, work, mem, eval, aprv, push, lease, mode, swrm, role, hub, shrd, knox, link, pbk, note, rank, judge, ablt, repl, farm, sync, orch, diag, self, spec, forge, bench, gate, prom, roll, audit, lmux, rcon, keys |

### Decision Policy for New Tools

1. Hot path (runs many times per job)? → Go
2. Needs JSONL manipulation beyond trivial? → Go
3. Touches network/storage/concurrency? → Go
4. Just `rg | sed | awk | jq` with minor glue? → Shell
5. Existing Unix tool does it perfectly? → Don't write anything

---

## Recommended Build Order (Across All Phases)

### Milestone 1 — Minimum Viable V1 (interactive CLI)
1. ~~Project bootstrap (go.mod, dirs, Makefile)~~ DONE
2. ~~trce (trace logging — needed by everything)~~ DONE
3. ~~scan + dect (repo detection)~~ DONE
4. ~~scope + srch + slce (retrieval)~~ DONE
5. ~~jolt (JSONL plumbing)~~ DONE
6. ~~pack + budg (context packing)~~ DONE
7. ~~llm (LLM client)~~ DONE
8. ~~act (action enforcer)~~ DONE
9. ~~guard + risk (diff validation)~~ DONE
10. ~~aply + undo (patch application)~~ DONE
11. ~~exec + vrfy (verification)~~ DONE
12. ~~rpt (reporting)~~ DONE
13. ~~map + ctrt + flow + doss + rfrsh (macro context)~~ DONE
14. ~~graf + anch (graph + resilient patching)~~ DONE
15. ~~mcp (protocol bridge)~~ DONE
16. ~~clock init / clock ask / clock fix~~ DONE

### Milestone 2 — Autonomous Agent (V2)
17. ~~q (durable queue)~~ DONE
18. ~~work (worker runner)~~ DONE
19. ~~watch (event detection)~~ DONE
20. ~~dock (daemon)~~ DONE
21. ~~eval + aprv (governance)~~ DONE
22. ~~push (notifications)~~ DONE
23. ~~mem (durable memory)~~ DONE
24. ~~lease (resource governor)~~ DONE
25. ~~tick + job (scheduling + event compilation)~~ DONE
26. ~~mode (autonomy control)~~ DONE
27. ~~clock start / clock stop / clock status~~ DONE

### Milestone 3 — Distributed Swarm (V3)
28. ~~shrd (artifact store)~~ DONE
29. ~~swrm + role (multi-agent)~~ DONE
30. ~~hub (message bus)~~ DONE
31. ~~knox + link (knowledge graph)~~ DONE
32. ~~pbk (playbooks)~~ DONE
33. ~~judge + repl (evaluation + replay)~~ DONE
34. ~~farm + sync (distributed workers)~~ DONE
35. ~~orch (campaigns)~~ DONE
36. ~~note + rank + ablt (learning + improvement)~~ DONE

### Milestone 4 — Self-Improvement (V4)
37. ~~Tool registry + manifest system~~ DONE
38. ~~diag + self (diagnostics + proposals)~~ DONE
39. ~~spec + forge (spec-first tool creation)~~ DONE
40. ~~test + audit + bench (validation)~~ DONE
41. ~~gate + prom + roll (promotion pipeline)~~ DONE
42. ~~Capability-based permissions~~ DONE
43. ~~Signed artifacts~~ DONE

### Milestone 5 — Polish
44. ~~lmux (LLM independence)~~ DONE
45. ~~rcon + keys (remote development)~~ DONE
46. ~~clock doctor (dependency checker)~~ DONE
47. Unit/integration test suite — REMAINING
48. Documentation — REMAINING
49. Release packaging — REMAINING
