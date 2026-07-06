# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.11.0] - 2026-07-06

Theme: **CLI Driver Reliability & Shell-Channel Orchestration (Epics 61–63)** — orchestration exit codes now reflect real failures instead of incidental tool errors, the Codex driver's sandbox can be configured explicitly, and processes spawned from outside the daemon can attach correctly to the process tree and be waited on synchronously.

### Added

- **`rnix wait <pid>`**: blocks until a process reaches a terminal state and propagates its exit code, with a `--timeout` flag (exits with code 124 on timeout) and `--json` output. Already-finished processes return immediately from history.
- **`--parent <pid>` spawn option** (also settable via `RNIX_PARENT_PID`): lets a process spawned from outside a running Rnix process attach to the correct place in the process tree, so the Dashboard shows accurate parent/child relationships and spawn-depth limits still apply.
- **Codex driver `sandbox_mode` setting**: provider configuration now accepts an explicit `read-only` / `workspace-write` / `danger-full-access` sandbox mode for the Codex CLI driver, replacing a previously hardcoded mode that could fail closed in some workspace layouts.
- **Feature-profile mismatch warning**: the CLI now warns when `RNIX_FEATURE_PROFILE` is set in the environment but ignored by an already-running daemon, making a common misconfiguration visible instead of silent.

### Changed

- **Dashboard identity display**: UUIDs are now shown by their distinguishing trailing characters instead of a shared leading prefix; timestamps show time-only for today and a date prefix for older entries.
- **Agent Tree ordering**: row order is now fully deterministic, so entries no longer reshuffle between refreshes.

### Fixed

- **Orchestration exit codes**: a process that completes successfully no longer reports a failed exit code merely because of a handled, non-fatal tool error; failure is now driven by genuine child-task failures or repeated tool errors.
- **Codex stream reliability**: an idle or truncated Codex response stream no longer reports a false success with no output.
- **Transport timeout retries**: connection and handshake timeouts against LLM endpoints are now retried instead of immediately failing the process.
- **Dashboard Timeline navigation**: keyboard navigation and expand/collapse no longer freeze or jump unpredictably for long-running processes once step aggregation kicks in.

## [0.10.1] - 2026-06-25

Theme: **Dashboard Monitoring at Scale** — the Dashboard handles large process histories smoothly and reports process health more accurately, building on the observability work in 0.10.0.

### Added

- **Process-list pagination**: the Dashboard now loads processes page by page in most-recent-first order, so sessions with large process histories stay responsive instead of fetching everything at once. Historical processes are preserved as new pages load, keeping the process tree intact.

### Fixed

- **More accurate real-time health counts**: realtime monitoring now distinguishes the active process set from historical entries and folds in recently failed processes, so fast Running→Dead transitions are no longer missed or misreported.
- **Tree pane scrolling**: the process tree now scrolls correctly when the title bar grows taller, so the last row is always reachable instead of being clipped.

## [0.10.0] - 2026-06-20

Theme: **Deep Observability & Reasoning Control (Epics 55, 56)** — every LLM call can now be captured and inspected after the fact, model reasoning becomes visible in the UI, sub-agents spawned inside CLI drivers are reconstructed into the process tree, MCP gains an HTTP transport, and reasoning effort can be controlled end to end.

### Added

- **Raw LLM request/response inspection (Epic 56)**: each LLM call can be recorded and inspected after the fact — the request and response for API-based providers, and the full command invocation plus output for CLI-based providers. This makes it possible to verify exactly what was sent to a model (prompts, parameters, reasoning effort) when debugging behavior. Credentials (authorization headers, API keys, and similar secrets) are automatically redacted before anything is written. Captures are queryable three ways: `rnix strace <pid> --raw`, a new **Raw I/O** view in the Dashboard inspector, and over IPC. Enabled by default, with size limits and a retention policy.
- **LLM reasoning visualization**: a model's thinking/reasoning output is now aggregated into events and rendered in the Dashboard and Timeline, making the reasoning process visible alongside tool calls and results.
- **CLI sub-agent process tree reconstruction (Epic 56)**: when the Claude CLI driver runs an agentic loop containing sub-agents (e.g. Task/Agent) inside a single process, those sub-agents — and their internal steps — are now reconstructed as nodes in the process tree and shown in the Dashboard. Reconstructed nodes are marked as synthetic and are excluded from resume.
- **MCP Streamable HTTP transport**: MCP devices can now connect over a Streamable HTTP transport in addition to stdio, with HTTP protocol-version negotiation and hot-reload of `mcp.yaml`.
- **`reasoning_effort` across all LLM drivers (Epic 55)**: providers gain a `reasoning_effort` setting, passed through verbatim to each driver's native surface (OpenAI, openai-compat, Anthropic, Gemini, claude-cli, codex-cli). cursor-cli and qwen-cli have no effort parameter and intentionally no-op with a warning. See [docs/reasoning-effort.md](docs/reasoning-effort.md).
- **Per-request `reasoning_effort` entry points (Epic 55)**: effort can now be set per spawn — a CLI `--reasoning-effort` flag, a `reasoning_effort` field in compose YAML, an AgentShell `spawn --effort` option, and a `models.reasoning_effort` default in agent manifests. Resolution follows a four-tier fallback: per-spawn → agent → provider → native default.
- **Project-root `AGENTS.md` injection (Story 35.7)**: at spawn time, a project-root `AGENTS.md` is read and injected into the system prompt as a cached section, aligning with the AGENTS.md industry standard (same mechanism as CLAUDE.md injection). Uses nearest-wins lookup and can be disabled per agent via `project_doc: false`. See [docs/agents-md-injection.md](docs/agents-md-injection.md).
- **AgentShell per-spawn provider overrides**: `spawn` now accepts `--provider`, `--fallback-provider`, and `--fallback-model`, so the provider and fallback chain can be chosen per spawn.
- **Configurable shell command timeout**: shell command execution now supports a configurable timeout, with more robust timeout and cancellation handling.

### Changed

- **Anthropic / Gemini `thinking_budget` → effort migration (Epic 55)**: where `reasoning_effort` is set, it now takes priority over the legacy `thinking_budget`. The budget path is retained as a fallback for providers that still require it. Note: Gemini effort levels are uppercase (`HIGH`) while OpenAI/Anthropic are lowercase (`high`); the value is passed through without case normalization.

### Fixed

- **CLI tool-input display (Story 40.4)**: tool-call inputs that previously showed up empty or truncated in the Dashboard Timeline (for some CLI driver streaming sequences) are now captured and displayed correctly.
- **Cross-project resume**: resume now resolves configuration from the resumed process's own project directory, so processes can be resumed correctly across different projects.
- **Spawn cleanup & early event capture**: observation data is now attached earlier in the spawn path so early events are no longer lost, and failed spawns clean up their partial data more reliably.
- **Dashboard failed-process display**: a failed process's exit reason is now folded into its card's first line for at-a-glance visibility.

## [0.9.4] - 2026-06-14

Theme: **Feature Profiles, Tool Naming Standardization & Device-Path Internalization (Epics 52, 53, 54)** — feature flags become a first-class runtime profile surfaced through a new `rnix config` command; tool names converge on a uniform PascalCase convention aligned with the broader agent ecosystem; and device paths (`/dev/*`) become a pure internal routing concern, no longer leaking into prompts, agent instructions, or skill manifests.

### Added

- **Feature flags & profiles (Epic 52)**: a `FeatureFlags` / `FeatureProfile` model now controls which emergent subsystems are active at runtime. Named profiles (`baseline`, `core`, `adaptive`, `full`, `custom`) map to flag sets, and `custom` applies only the flags explicitly listed (the rest default to enabled). Conditional injection wires these flags into process management and tool definitions at the kernel layer, so a disabled subsystem adds no prompt or tool-definition overhead.
- **`rnix config` command**: `rnix config show` displays the active feature profile and individual flags, reading live state from the running daemon and falling back to the global config file when the daemon is down. Daemon status responses now include the feature profile.
- **`rnix daemon start` command**: explicit foreground start command complements the existing auto-start-from-CLI path, with a deterministic shutdown sequence and timeout.
- **Tool-level enforcement (Epic 54)**: process permission is now evaluated at the **tool granularity** rather than the device granularity. Declaring `allowed-tools: Read` on a skill grants `Read` but not `Write`, even though both belong to the same underlying device. MCP tools continue to use additive-permit semantics so dynamic tool surfaces from MCP servers are unaffected.
- **`using-rnix` capability skill (Epic 53)**: a new built-in skill (`lib/skills/using-rnix/`) gives external agents a portable capability map and verified CLI usage for driving Rnix from the outside — spawning agents, orchestrating workflows, decomposing intents, inspecting and resuming processes — without having to read Rnix source.
- **`.agents/skills/` scope at spawn time**: spawn now loads skills from both project (`<projectDir>/.agents/skills/`) and user (`~/.agents/skills/`) scopes, completing the runtime loop for the agentskills-style cross-tool skill convention.
- **`Glob` for directory listing**: directory enumeration is now expressed as a glob pattern via `Glob` rather than a separate `list_dir` tool; this is one tool fewer, with strictly more expressive matching.
- **Smart sync of embedded agents and skills**: on daemon startup, embedded agents and skills are synced into the workspace with content-aware diffing — local edits are preserved while upstream additions and updates land automatically.
- **`rnix apply` auto-resume**: orchestrator-driven apply runs reconnect to the daemon and auto-resume in-flight processes after transient disconnects, with clearer error messaging when orchestrator startup fails.
- **`make test-cover`**: a Makefile target mirroring CI's coverage path (`go test -race -coverprofile -covermode=atomic ./...`), so failures that only surface under coverage instrumentation can be reproduced locally before pushing.

### Changed

- **Tool naming convention — PascalCase across the board (Epic 53/54, ADR 44/45)**: every public tool name (driver registrations, ToolDef metadata, skill `allowed-tools`, agent instructions, system-prompt templates) now uses PascalCase semantic names — `Read`, `Write`, `Edit`, `Glob`, `Grep`, `Bash`, `WebFetch`, `WebSearch`, `MemoryCommit`, `MemoryRecall`, `IntentDecompose`, `IntentConfirm`, `IntentExecute`, `IntentStatus`, etc. The naming convention is now codified as an ADR.
- **Device paths are pure internal routing keys**: `/dev/*` paths no longer appear in user-facing or LLM-facing surfaces. System-prompt templates, `ToolDef.Description` text, runtime tool result/error messages, agent instructions for `orchestrator` and `playwright-demo`, and built-in skill `allowed-tools` frontmatter all reference semantic tool names instead. Device paths remain in place internally for routing and as an additive-permit channel for MCP mounts.
- **Spawn device-inheritance unified (Epic 37)**: child processes spawned via `ActionSpawn` now derive their device set the same way `IntentDecompose` children do — orchestration-only devices are stripped, leaving only executable devices, with recursive-orchestration prevented through a denylist. This eliminates a class of spawn-storm dead-locks where pure-orchestrator parents would propagate non-executable device sets to children.
- **Per-project memory isolation**: project-scope memory now resolves consistently from the project's data directory regardless of daemon working directory, preventing cross-project bleed.
- **Cumulative tool-error breaker decays on success**: instead of resetting to zero on the first successful tool call, the breaker decays gradually, preserving signal across mixed success/failure streams.

### Fixed

- **Intent provider inheritance**: intent decomposition now correctly inherits the calling process's LLM provider, supports project-level provider configuration, and propagates project context to reconciler-spawned children.
- **Intent recursive escape blocked**: intent-spawned processes can no longer re-enter `IntentDecompose` or shell out to `rnix apply` to escape the orchestration sandbox.
- **Intent `auto_start` daemon-restart guard**: declarative intents that would self-trigger a daemon restart are now detected and refused with a clear error instead of silently looping.
- **Resume preserves `DeniedDevices`**: the device denylist is now persisted alongside the allowlist so recursive-orchestration guards survive checkpoint/reap/resume cycles.
- **`rnix ps` records observation data without project config**: step and event data are now persisted for every process, even those running outside an initialized project — Dashboard inspection works uniformly.
- **`rnix apply` recursive-escape blocked**: shell processes can no longer recursively invoke `rnix apply` to bypass spawn-recursion limits.
- **Dashboard agent tree by UUID**: the agent tree now keys on UUID rather than PID, eliminating cross-merge artifacts when PIDs are reused after daemon restart.
- **Dashboard model display robustness**: long model identifiers no longer overflow the inspector pane; truncation appends an ellipsis to make the truncation visible rather than silently dropping characters.
- **Shell `spawn --agent`/`--model` strict variable expansion**: agent/model arguments to `spawn` now follow the same strict-expansion rules as the rest of AgentShell, surfacing missing variables instead of substituting empty strings.
- **Shell stage counting**: nested function calls are now recursively expanded when counting pipeline stages, so progress reporting falls back to an unbounded indicator only when the total is genuinely unknowable.
- **Inspector content wrapping**: inspector content now wraps to viewport width across all panes.
- **diffmemory**: `Lookup` validates the available-skill count before use, and recording/lookup now track that count accurately.
- **llm**: skip binary resolution when a custom command builder is supplied, avoiding spurious PATH lookups for embedded / test drivers.
- **Test isolation from host env / coverage mode**: the `claude-cli` fallback binary-resolution tests now use an injectable extended-search-dir seam (new `WithExtendedBinDirs` option) instead of the host's real `$HOME`/`$NVM_DIR`, so a real `~/.local/bin/claude` no longer shadows the sandboxed candidates; and the `drivers/shell` helper-process test injects a throwaway `GOCOVERDIR` so the re-exec'd child's exit-time coverage warning no longer pollutes the merged output under `go test -cover`. Both are test-only fixes; runtime behavior is unchanged.

### Removed

- **`list_dir` tool**: directory listing is now expressed via `Glob`. Skills and agents that previously declared `list_dir` should declare `Glob` instead.

### Breaking Changes

> Upgrade note: the following changes affect skill manifests, agent instructions, and any external automation that hard-codes tool names or device paths

- **Tool names converged on PascalCase**: skills, agent `instructions.md`, and any external scripts that reference tool names by string need to use the new PascalCase forms (e.g. `read_file` → `Read`, `shell` → `Bash`, `list_dir` → `Glob`, `memory_commit` → `MemoryCommit`). Built-in skills and agents have already been migrated.
- **`list_dir` removed**: replace with `Glob` plus a pattern such as `*` or `**/*`.
- **`allowed-tools` semantics tightened**: declaring a tool name no longer transitively grants every tool on the same device. If a skill needs both `Read` and `Write`, both must be declared explicitly. Existing manifests that use device paths (e.g. `allowed-tools: /dev/fs`) continue to work via a backward-compatibility expansion but should be migrated to semantic tool names.
- **Device paths removed from user-facing text**: prompts, error messages, and built-in skill/agent text no longer reference `/dev/*` paths. Custom tooling that pattern-matches on device-path strings in LLM output should switch to matching on the semantic tool name.

## [0.9.3] - 2026-06-02

Theme: **Claude CLI Compatibility & Adaptive Intelligence (Epics 40, 50, 51)** — Claude CLI driver gains version-adaptive capability probing and fallback binaries; adaptive immune system ships enabled by default; stem agents accumulate differentiation memory across restarts and use reputation-driven skill ranking.

### Added

- **Claude CLI driver capability probing (Epic 40)**: the driver now probes the installed CLI at spawn time to detect supported features (`--include-partial-messages`, `--add-dir`, `--permission-mode`) and adapts its behavior accordingly; incompatible flags are silently omitted instead of causing errors
- **Fallback binary resolution**: when `claude` is not found on PATH, the driver automatically searches for alternative binaries (e.g. `openclaude`) across common install locations (`~/.local/bin`, nvm, bun); the resolved binary path is visible in Dashboard and strace
- **Prompt context injection**: Claude CLI processes receive project working directory, skill paths, and user attachments in the initial prompt, reducing unnecessary tool calls during startup
- **Driver metadata in Dashboard**: process detail view now displays the resolved binary path, active permission mode, and detected CLI capabilities for Claude CLI processes
- **Differentiation memory persistence**: stem agent differentiation paths (intent-to-skill mappings learned at runtime) are now persisted to disk and restored on daemon restart, so agents retain accumulated specialization across sessions
- **Reputation-driven skill reranking**: stem agents now factor in historical reputation scores and synergy data when selecting skills at spawn time, with configurable exploration probability to prevent lock-in
- **Capability-based task migration**: when a process exhausts its retry budget, the kernel can migrate the task to a similar agent based on a capability similarity matrix, improving fault tolerance
- **Immune system enabled by default**: the adaptive immune system now ships enabled in warn-only mode — it observes and logs anomalies without suspending processes, graduating to enforcement when the operator sets `warn_only: false`
- **Spawn recursion guard**: a configurable depth limit prevents runaway recursive spawning; processes that exceed the limit are rejected with a clear error
- **Global data directory management**: centralized data directory resolution with project registry support
- **Unified context budget**: `context_budget` is now enforced as a per-step context window guard, providing consistent resource control across all process types
- **`rnix ps --all`**: new `-a`/`--all` flag shows both active and historical processes in a single listing

### Changed

- **Immune system mode**: anomaly detection defaults to warn-only — stall and deviation events are recorded for observability but do not trigger process suspension unless explicitly configured

### Fixed

- Spawn no longer inherits parent's full device permissions, preventing unintended privilege escalation in child processes
- Improved error messages when orchestrator agent startup fails

## [0.9.2] - 2026-05-29

Theme: **MCP Subsystem Production Hardening (Epic 48)** — MCP server tools surface as first-class agent capabilities, survive process resume, shut down cleanly, and gain operational tooling for inspection, health checks, and per-server tuning.

### Added

- **MCP tools as native agent capabilities**: tools exposed by mounted MCP servers are now presented to agents like any built-in capability, so an agent can call them directly without special handling
- **`rnix mcp list` / `rnix mcp test`**: inspect configured MCP servers and verify connectivity from the command line
- **`rnix mcp logs`**: capture and review an MCP server's diagnostic output for troubleshooting
- **`rnix check mcp`**: subsystem diagnostics command that reports MCP configuration and runtime health at a glance
- **`rnix init --with-mcp-examples`**: bootstrap a project with ready-to-use MCP example configuration and an example agent
- **MCP health checks and liveness probes**: mounted servers are monitored for availability, with automatic reconnection on transient failures
- **Per-server configuration**: each MCP server can set its own mount timeout, request timeout, and output size limit
- **MCP mount restoration on resume**: resumed processes automatically re-establish their MCP server mounts, so resumed agents retain full tool access

### Changed

- **Concurrent MCP mounting**: multiple MCP servers now mount in parallel rather than sequentially, substantially reducing startup time when several servers (including slow-starting ones) are configured
- **Graceful MCP shutdown**: MCP server processes are now torn down cleanly with proper process-group isolation, preventing orphaned child processes
- **Default compose file path**: the default multi-agent workflow file now resolves to `.rnix/compose.yaml`

### Fixed

- **MCP server tool availability**: agents that declare MCP servers now reliably receive those tools; a missing MCP configuration surfaces a clear error instead of silently dropping the servers
- **MCP permissions no longer override base devices**: mounting an MCP server adds to an agent's capabilities rather than restricting it to only those servers
- **Output truncation and validation**: improved handling of large MCP tool outputs and stricter configuration validation

## [0.9.1] - 2026-05-28

Theme: **Skill Trust & Installer Hardening** — project trust checks, single-scope installer refactor, and comprehensive test coverage for the skill management subsystem.

### Added

- **Project trust checks in skill operations**: skill installation and update now verify project-level trust before modifying `.rnix/` directories; enhanced diagnostics surface trust status clearly
- **Comprehensive skill management documentation**: `docs/skill-loading.md` covering skill lifecycle, scope resolution, and trust mechanics
- **Test coverage expansion**: tests for multi-scope installer, skill list filtering/diagnostics, `SkillScope`/`SkillNamespace` string representations, `resolveSkillScopes`, and project trust checks for project scope handling (5 test files, ~6200 lines)

### Changed

- **Installer single-scope migration**: installer refactored from multi-scope to single-scope approach, replacing `MultiScopeInstaller` with simplified `Installer` and streamlined loading diagnostics
- **`resolveWriteScope` signature simplified**: unused parameter removed

### Fixed

- **Shadow warning emission**: `ListAll` now only emits shadow warnings when a skill is genuinely shadowed, eliminating false positives

## [0.9.0] - 2026-05-27

Theme: **Process lifecycle reshape** — resume from history, unified subtree pause/resume, and heartbeat subsystem redirected from "active supervision" to "passive observation".

### Added

- **Process Session Resumption from History (Epic 42)**:
  - `rnix resume <uuid>` now works on any Dead / Zombie / context_full / circuit-broken process — no longer requires Suspended as a precondition (Story 42-1)
  - `rnix resume --fork <uuid>` forks a new UUID process while the original UUID remains independently resumable; `proc-info.json` records the `origin_uuid` lineage field
  - Periodic best-effort checkpoints: long-running processes refresh recoverable snapshots under `.rnix/data/steps/<uuid>/` (Story 42-2)
  - Daemon startup scans Suspended processes from disk and loads them into procTable (`LoadSuspendedFromDisk`); manual resume continues to work across restarts (Story 42-2)
  - `--from-step` step-level fork + Dashboard Lineage view (Story 42-3)
  - Disk GC governance: `gc.retention_days` + `gc.max_entries` dual safeguards; Running / Suspended processes are permanently exempt (Story 42-5)
  - Design doc: `docs/process-resumption.md`

- **Unified Subtree Pause/Resume Semantics (Epic 44)**:
  - New `SubtreeManager`: all pause/resume entry points (dashboard `p` / CLI `rnix kill -SIGPAUSE` / Ctrl+C) converge on the Suspended state machine (Story 44-1)
  - Pausing PID=X recursively suspends **X and all descendants**; parents and siblings are untouched. Resume symmetrically restores every Suspended node in X's subtree (Story 44-1)
  - Script-runners are now suspendable: Ctrl+C is equivalent to "press `p` on the root script-runner" — daemon does not cancel ctx; awaits manual resume (Story 44-2)
  - Suspended processes persist across daemon restarts: new paused fields in `proc-info.json`; after restart, `reasonStep` does not auto-start — it waits for `rnix resume` (Story 44-3)
  - In-flight LLM call suspension protocol: pending LLM calls are cancelled on suspend; replayed from historical context on resume (Story 44-5)

- **Heartbeat Subsystem Reform (Epic 45)**:
  - Cross-repo version observability: three-source version fallback — ldflags injection → BuildInfo VCS → `(devel)` fallback; `rnix --version` / `rnix daemon status` / startup banner uniformly show commit + dirty flag (Story 45-1)
  - HeartbeatMonitor switched to **warn-only**: emits `HEARTBEAT_STALL` events for observability only; no more `cancel_step`, no more auto-suspend (Story 45-2)
  - **Dashboard Stall Intensity Heatmap**: detail pane renders stall intensity so heartbeat anomalies surface as signals to the user rather than daemon-decided actions (Story 45-5)

- **Script-Runner Observability (Epic 43)**:
  - ScriptExecutor now writes to `events.jsonl`: while-iter, spawn-return, variable assignment, and condition eval become first-class observable events (Story 43-2)
  - Dashboard Timeline gains formatters for 5 Script* syscalls + a ScriptAggGroup fold (Story 43-3)
  - HB-1 heartbeat lifecycle is hoisted to `handleExecScript` so coverage extends across statement gaps, paused children, and user operations (Story 43-1) — retained as a warn-only signal under the Epic 45 P4 philosophy

- **Shell Process Group Isolation (Unix)**:
  - Shell driver launches subprocesses in their own process group by default; Ctrl+C no longer kills the wrong group. Background process behavior is also enhanced

### Changed

- **HeartbeatMonitor passive mode** — daemon stops guessing whether a business process has died. The HB-1 lifecycle ticker and the supervisor auto-recovery path are removed (Story 45-3). Slow LLM inference, retries, and long-running calls are normal business — hard failures are handled by the driver / `reasonStep` error-propagation chain
- **Tool names standardized to PascalCase**: unified naming style across drivers; docs updated in lock-step (commit `6dea40a`)
- **Dashboard ExitCode source change** — Timeline EXIT severity now uses the authoritative process `ExitCode` field instead of inferring from the result message (commit `6f6bc61`)
- **`IsFailedResult` → `IsProcessFailed`** — Dashboard failure judgement now uses process state rather than result text (commit `8e39604`)

### Fixed

- **PID reuse bug**: after daemon restart, the PID counter is seeded from disk to avoid colliding with persisted historical-process UUIDs (commit `fccd5ea`)
- **Lost project context for Suspended processes**: rehydrate correctly restores project context and placeholder runtime state (commits `7e033bf`, `3b901e7`)
- **`WaitChildInReason` guard**: race during the mid-state Suspend → Done transition (commit `cb6fbf9`)
- **Preserve relative paths when FS workDir is unset**: avoids being mistakenly resolved against CWD (commit `e333bc2`)
- **Dashboard rendering**: UUID truncation removed for clarity; DEBUG header deduplicated between titleBar and Timeline pane; detail-pane state-flicker regression test (commits `14311b8`, `4fd56d0`, `996fdaf`)
- **`TestATDD_42_2_INT_003_E2E_CrashRecovery` flakiness**: mock-LLM handshake gate eliminates the timing race between reap and `ListResumable` (commits `b6f056c`, `4cbd755`)

### Removed

- **HB-1 lifecycle ticker** — the 10s self-heartbeat ticker inside `ipc/handleExecScript` is gone (Story 45-3)
- **Supervisor auto-recovery path** — no more automatic restart or resume of children based on stall signals (Story 45-3)
- **Legacy version variables** — `version` / `gitCommit` / `buildDate` in package `main` are removed; resolution is unified through `buildVersionInfo()` (Story 45-1)
- **`reactivateCliDisconnectedAncestors`** — the Epic 43 ancestor-wakeup chain is deleted; resume is now strictly manual with no automatic ancestor cascade (Epic 44 background)

### Breaking Changes

> Upgrade note: the following behavioral changes may be observable from external scripts / automation

- **Daemon no longer auto-suspends or cancels processes**: scripts relying on `HeartbeatMonitor`-driven suspends will find the daemon no longer "intervenes" — only the `HEARTBEAT_STALL` warn event remains. LLM error recovery falls back to the driver / `reasonStep` error-propagation chain
- **Pause/resume semantics unified on the Suspended state machine**: the old SoftPause (process State stays Running on the signal path) is gone; all of `p` / `r` / Ctrl+C / `rnix kill -SIGPAUSE` now trigger state transitions. Clients that read `process_state` directly should expect Suspended to appear more often
- **Ctrl+C no longer terminates script-runners**: it is equivalent to "subtree SIGPAUSE"; the CLI exits to the shell but the daemon keeps the process Suspended — call `rnix resume <uuid>` to continue. Any assumption that "Ctrl+C kills the daemon-side task" is invalid
- **Tool names changed to PascalCase**: driver-layer tool registration names changed case; skill manifests / test fixtures with hard-coded old names need to be updated

## [0.8.0] - 2026-05-14

### Added

- **Agent Memory System (Epic 35)**:
  - MemoryStore with dual-scope providers (global + project) and security scanning
  - `/dev/memory/commit`: persistent memory management VFS device
  - `/dev/memory/recall`: cross-process knowledge search VFS device
  - `/dev/memory/profile`: user profile management VFS device
  - Writeback: async knowledge extraction from conversation context
  - Dynamic skill management: runtime skill add/remove/list operations

- **New LLM Drivers**:
  - Native Gemini driver (`gemini`)
  - Anthropic official SDK driver (`anthropic`)
  - Qwen Code CLI driver (`qwen-cli`)

- **Dashboard Enhancements**:
  - `p` key for pause/resume processes directly from process tree
  - Enhanced debug mode event handling and error reporting
  - Debug mode process selection stability improvements

- **CLI Driver Improvements**:
  - Claude CLI driver dynamic prompt construction
  - `extra_args` provider config field for CLI driver sandbox bypass

- **Dashboard Inspector & Tree-Timeline Unification (Epic 36)**:
  - Step Inspector merge: unified step + system event timeline replaces separate panels (Story 36-1)
  - Agent tree sort fix: time-based priority sorting and new process highlight with fade animation (Story 36-2)
  - Timeline information architecture refactor: default 1-line layout, tool call aggregation with expand, action-typed icons (Story 36-3)
  - Timeline ascending sort default (`o` key toggles direction) aligned with debug mode reading order (Story 36-4)
  - Three-state expand mode: `e` (expand all, sticky), `E` (errors only), `C` (collapse all) with new-step stickiness (Story 36-4)
  - Header indicators for sort direction and expand mode with ASCII fallback (Story 36-4)
  - One-time migration notice for ascending sort change, persisted to `~/.config/rnix/ui-state.json` (Story 36-4)

- **Dashboard Timeline Navigation (Epic 41)**:
  - Fold/expand navigation: `j`/`k`/`↑`/`↓` move across visible rows; `Enter` toggles group fold or drills into leaf detail
  - `[`/`]` jump between group headers, or cycle search matches when search is active
  - Fold markers: `▶` collapsed / `▼` expanded (ASCII fallback: `>` / `v`)
  - Dynamic per-model context window size displayed in timeline header
  - Cache hit rate column per step with driver-aware semantics (openai-compat vs. Anthropic native)

- **Multi-Backend Web Search**:
  - `/dev/web` search backend registry: Tavily, Exa, and SearXNG now supported alongside the built-in fetcher
  - Backend auto-selection and transparent fallback; configure via `TAVILY_API_KEY`, `EXA_API_KEY`, or self-hosted `SEARXNG_URL`

- **Anthropic Prompt Caching**:
  - Three `cache_control` breakpoints injected automatically (system prompt, tool definitions, last user turn)
  - Cache hit rate computed with Anthropic-specific semantics: `CacheReadInputTokens / (input_tokens + CacheReadInputTokens)`

- **LLM Permission Mode & Capability Probing**:
  - Processes declare required permission level; kernel gates execution before the first LLM call
  - Drivers report supported feature flags at init time; unsupported features are gracefully downgraded

- **DeepSeek V4 `thinking_budget` Support**:
  - `thinking_budget` parameter available via both `anthropic` and `openai-compat` drivers

- **Context Slot Tracking**:
  - `CtxSize()` and available-slots API expose precise slot headroom alongside token usage
  - Auto-compact triggers on slot saturation in addition to token thresholds
  - Atomic capacity checks for assistant messages with tool calls prevent split-turn overflow

- **Kernel Observability**:
  - TraceID auto-generated for all top-level processes (no manual `--trace` flag needed)
  - Suspend reason recorded: `budget`, `loop_detect`, `manual`, or `timeout`
  - `FinalSystemPrompt` captured and persisted per process for post-mortem inspection

### Changed

- Claude CLI driver uses `--bare` flag to prevent native tool interference
- Driver-reported tools merged into existing nativeToolDefs without overwriting
- Action execution guidelines and task handling instructions updated
- **Dashboard Architecture Overhaul (Story 38-5 PR11)**:
  - `PaneModel.OnTick(OnTickContext)` adopted across all 8 panes — eliminates ad-hoc poll timers and unifies the refresh contract
  - `StateProvider` interface replaces direct field access; all panes read state through stable accessors
  - Inspector helpers, diff state, hint builders, and status renderers extracted to `internal/dashboard/{inspector,status}` packages
  - `selectPIDMsg` broadcast replaces per-pane PID-change handlers — single source of truth for focused process
  - End-to-end integration test suite added for `dashboardModel`
- Tool I/O lens now shows raw LLM response payload alongside parsed tool calls
- Context reasoning handling enhanced in assistant messages for multi-step accuracy

### Fixed

- LLM viewer navigation and fetching logic
- Parent heartbeat kept alive while waiting for child in SpawnAndWait
- Paused child processes no longer killed on parent context cancel
- Stale detection skipped for paused processes in tree pane
- `Process.Model` backfilled from driver default at spawn time
- EventWriter flushed on every write for immediate disk persistence
- `PausedAt` added to ListProcs wire protocol
- Heartbeat monitor skips paused processes correctly
- Elapsed timer freezes in tree pane when process is paused
- Real step timestamps used in timeline offset column
- `IsPaused` added to ProcInfoWire for IPC transmission
- Tool error handling and recovery logic
- Empty LLM response detection and orphan process tree structure preservation
- Gemini nil parameters schema serialization
- System prompt `env_info` and format consistency
- `ParentUUID` added to IPC wire protocol for correct agent tree hierarchy
- Cross-session tree pollution prevented via ParentUUID
- Error step data recorded at all failure points in reason loop
- Dashboard scroll clipping, orphan tree display, and failed process observability
- Wall-clock timestamps added across all dashboard panels
- Dashboard debug event handling and historical data management
- `P` key unresponsive when timeline has 0 steps + 0 events
- LLM viewer showing "no step data" for processes with data
- "No step data" message for `P`/`L` keys on 0-step processes
- **DeepSeek V4 / Gemini compatibility**:
  - `thinking_budget` correctly wired for both Anthropic-native and OpenAI-compat drivers
  - Gemini rounds-trips `ThoughtSignature` on multi-turn thinking conversations
  - LLM factory warns when `driver=openai` targets a non-OpenAI `base_url`
- Step inspector data fidelity restored across all 5 lenses (Meta, Tool I/O, Context, Raw, Diff)
- Dashboard `DetailModel` cache initialized correctly; no panic on first render after PID change
- Context compaction correctly respects both timeout and slot budget simultaneously
- LLM call timeout and cancellation errors propagate to process state machine instead of hanging
- Tool error handling preserves original error across all three recovery layers (driver → kernel → dashboard)
- Base system prompt injected for all processes regardless of whether an agent config is present

## [0.7.3] - 2026-04-12

### Fixed

- **`rnix run` script runner visibility and killability**: Script runner is now registered as a `SkipReasonLoop` process in the kernel process table, making it visible in `rnix top` and killable via `K`. Child agents receive the script runner's PID as `ParentPID` so they appear correctly nested in the process tree. A three-way context ties together SIGTERM/SIGKILL, daemon shutdown, and client disconnect (Ctrl+C), with `Finish()+Reap()` called on completion to ensure full resource cleanup

## [0.7.2] - 2026-04-12

### Added

- **Reliability & Recovery (Epic 30)**:
  - Unlimited reasoning steps: `DefaultMaxSteps` set to `0` (no limit); `LoopDetector` detects repeated actions and auto-suspends
  - Step persistence and snapshot checkpoints: `CheckpointData` written asynchronously, resumable from last checkpoint
  - Process suspend state: new `Suspended` state with `Suspend()` / `Unsuspend()` kernel methods
  - Resume mechanism: `rnix resume <pid|uuid>` command restores context from checkpoint and allocates a new PID
  - Heartbeat liveness detection: processes emit periodic heartbeats during execution; dashboard shows a stale indicator
  - Supervisor heartbeat monitor: `HeartbeatMonitor` scans for timed-out processes with a three-tier recovery strategy (retry step → suspend → notify)
  - Resource budgets: `MaxTokens` / `MaxCost` config fields; processes are auto-suspended when budget is exceeded
  - Long-task observability in dashboard: Focus Card shows resource budget and heartbeat status; `R` key resumes suspended processes; Timeline auto-aggregates beyond 100 steps

- **Context & Token Management (Epic 31)**:
  - Token metering and VFS result control: `ToolDef` gains `MaxResultTokens`; file reads and shell output are auto-truncated when over limit
  - Context compaction and transfer: manual and automatic compact trigger; compacted context can be handed off to a new process
  - Two-phase shutdown and IPC message persistence: SIGTERM first, escalated to SIGKILL after grace period; IPC messages for permanent processes are persisted to disk and restored on restart

- **Skill System Enhancements (Epic 32)**:
  - New file operations: `edit_file` (exact string replacement), `glob` (path pattern matching), `grep` (content search)
  - `ToolDef` safety metadata: `IsReadOnly`, `IsConcurrencySafe`, `IsDestructive` flags; enforced read-before-write protection and mtime tracking
  - Declarative sectioned system-prompt assembly: `PromptSection` + `SectionRegistry` with static/dynamic partition caching; automatic invalidation on agent compact
  - Deferred skill loading: `agent.yaml` gains `deferred_skills` field (metadata-only load); new `discover_skill` action type loads skills on demand by keyword scoring

- **New VFS Devices (Epic 33)**:
  - `/dev/web`: web content fetching and search with built-in result cache
  - `/dev/lsp`: code intelligence with 9 operations (go-to-definition, find references, hover docs, symbol search, and more)
  - `/dev/tty`: process-to-user interaction for questions and confirmations
  - `/dev/tasks`: dynamic task management — processes can create and track subtasks
  - `/dev/cron`: scheduled task execution

- **Dashboard V2 Event-Stream Architecture (Epic 34)**:
  - Four-tier information hierarchy: process tree + event stream + global status bar + alert bar, replacing the original equal-weight 8-panel layout
  - Process tree visual enhancements: status badges, context usage metrics, retained exited-process hierarchy, common-prefix folding, highlight of most active process
  - Alert bar and unified Timeline: all event types (reasoning steps, compact, budget, spawn, exit, heartbeat timeout) merged into a single timeline; alert bar highlights errors and anomalies
  - Detail Card integration: replaces Focus Card; combines process details, context, and resource info; process tree adapts its width
  - Debug mode: press `d` to enter; strace events interwoven with reasoning steps; context Profile card (Active/Warm/Cold/Leaked); device latency card (per-VFS-device average latency)
  - Orchestration relationship visualization: Compose DAG dependency edge annotations (`◄╌deps`), Pipeline stage markers (`│►[i/n]`); full end-to-end trace across process model, wire protocol, and disk persistence

### Changed

- **VFS device prompts**: all 6 device prompt files (`read_file`, `write_file`, `edit_file`, `glob`, `grep`, `shell`) rewritten following Architecture Decision 33 (CC Baseline principle), restoring canonical CC behavioral guidelines
- **AgentShell code split**: `shell/script.go` (2152 lines) split into 5 single-responsibility files with zero API changes
- **Dashboard render framework**: introduced `renderFixedPanel` to enforce consistent panel dimensions and prevent content overflow

### Fixed

- Dashboard history view freezing when paginating
- Dashboard history view scroll position out of sync with IPC read timeout
- `treeCursor` out of sync with selected process in history view
- CJK character display-width padding errors in the agent tree intent column
- Spurious newlines in intent and result text within agent tree rows
- `computeCtxPercent` incorrectly falling back to global average when PID is not found

### Removed

- History View: functionality consolidated into the Expanded Tree view; press `L` from Expanded Tree to enter the LLM Viewer

## [0.7.1] - 2026-03-28

### Fixed

- **Dashboard Prompt Viewer**:
  - Prompt Viewer Messages tab showing empty `[user]` entries for CLI driver processes — seed initial intent as first user message and skip nil-content user events from CLI drivers
  - Prompt Viewer Tools tab showing `Tools (0)` for CLI driver processes — build tool definitions from tool_call events as fallback when system event doesn't include tools
  - Prompt Viewer Tools tab showing incomplete tool info — now displays step-level tool details: name, description, path, input, result, duration from StepRecord
  - StepRecord Action field incorrectly set to `"tool_call"` instead of actual tool name
  - StepRecord missing ToolResult and ToolInput for CLI driver processes
  - Cursor CLI driver not extracting full tool args and result from tool_call events
  - `extractContentText` not handling `map[string]any` and `string` content formats for assistant/user events
  - `tool_result` content in `[]any` format not being parsed for text extraction

## [0.7.0] - 2026-03-24

### Added

- **Dashboard UX Redesign**: Complete overhaul of the dashboard interaction experience
  - View mode system with navigation restructuring
  - Default focus card view
  - Dashboard file splitting for better maintainability
- **Dashboard Process Inspection**:
  - Kernel process history and IPC query support
  - History view with LLM conversation viewer
  - Process detail panel with timeline drill-down (three-level detail)
  - Prompt view for step-level inspection
- **Dashboard Observability**:
  - StepRecord type with automatic step data logging
  - GetStepDetail IPC method for detailed process step retrieval
  - Distributed tracing integration
  - Security anomaly panel
  - Intent tree integration
- **Dashboard Evaluation & Navigation**:
  - Multi-agent evaluation view
  - Evaluation pane key bindings and navigation hints
  - Top-to-dashboard navigation
- **Process Identification Upgrade**:
  - UUID v7 for process identification
  - StepRecord path migration to UUID-based paths
  - IPC PID-to-UUID mapping
  - Dashboard PID validity checks
- **Unified Reasoning Loop**: Replaced OODA-based dual-loop with a single reasoning loop
  - Extended ActionType with unified prompt templates and planning configuration
  - Circuit breaker and VFS flag downgrade mechanism
  - Comprehensive ATDD tests
- **Configurable Immune System**: Immune system now configurable, disabled by default
- **Native ToolCalls Support**: Native tool calling with VFS device self-describing architecture
- **MaxSteps / MaxTokens Configuration**: New configuration options for step and token limits
- **Project Environment Variable Loading**: Automatic loading of project-level environment variables

### Fixed

- Evaluation pane key binding and navigation hint issues
- Signal handling continuation after timeout in `runRoot`

## [0.6.8] - 2026-03-17

### Added

- Project-level environment variable and provider configuration loading
- VFS tool call protocol injection for both reasoning modes

### Fixed

- Provider merging by name instead of wholesale slice replacement

## [0.6.6] - 2026-03-15

### Added

- **Platform-specific Daemon Handling**: Cross-platform daemon process management
- **Version Management**: ldflags-based version injection with release workflow
- **GitHub Actions CI**: Quality gate CI pipeline
- **GoReleaser**: Automated release configuration with GitHub Actions
- **Process CLI Output Enhancement**: Provider/Model details in process and CLI output
- **Configuration System Redesign**:
  - `rnix init` command with global configuration loading
  - Project-level configuration merging and module adaptation
  - Deprecated configuration file cleanup
- **Community Files**: MIT License, CONTRIBUTING.md, GitHub issue/PR templates
- **Multilingual README**: Chinese and English README files
- **Project Environment Loading**: Project-specific environment variable loading

### Changed

- Configuration file structure and documentation updates
- Enhanced Makefile with GitHub CLI command integration
- Upgraded golangci-lint version with improved coverage handling

## [0.1.0] - 2026-02-23

### Added

- **Project Initialization**: Crux AI Agent OS project structure and infrastructure
- **Process Model**: Process lifecycle state machine (Created → Running → Zombie → Dead)
- **Virtual Filesystem (VFS)**: VFS framework with device registration and FD table
- **Context Management**: Per-process conversation history (CtxAlloc/CtxWrite/BuildPrompt)
- **LLM Driver**: Claude Code CLI driver for LLM interactions
- **Kernel Reasoning Loop**: Spawn and ReasonStep execution loop
- **CLI & UI**: Command-line interface with terminal UI components
- **End-to-End Integration**: Full integration and acceptance testing
- **Skill System**:
  - Skill loader with YAML manifest parsing
  - Host filesystem driver (`/dev/fs`)
  - Shell driver (`/dev/shell`)
  - Skill injection with device permission whitelisting
  - Code-Analyst reference skill
- **LLM Driver Error Handling**: Enhanced error handling and context propagation
- **Step Output Streaming**: Real-time step output streaming
- **Strace Observability**: ConfigResolve strace event tracking
- **Daemon Model**: Background daemon with Unix domain socket communication
- **IPC Protocol**: NDJSON over Unix socket request/response protocol
- **VFS Devices**: `/dev/llm/claude`, `/dev/fs`, `/dev/shell` device implementations

[0.10.1]: https://github.com/rnixai/rnix/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/rnixai/rnix/compare/v0.9.4...v0.10.0
[0.9.4]: https://github.com/rnixai/rnix/compare/v0.9.3...v0.9.4
[0.9.3]: https://github.com/rnixai/rnix/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/rnixai/rnix/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/rnixai/rnix/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/rnixai/rnix/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/rnixai/rnix/compare/v0.7.3...v0.8.0
[0.7.3]: https://github.com/rnixai/rnix/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/rnixai/rnix/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/rnixai/rnix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/rnixai/rnix/compare/v0.6.8...v0.7.0
[0.6.8]: https://github.com/rnixai/rnix/compare/v0.6.6...v0.6.8
[0.6.6]: https://github.com/rnixai/rnix/compare/v0.1.0...v0.6.6
[0.1.0]: https://github.com/rnixai/rnix/releases/tag/v0.1.0
