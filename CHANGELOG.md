# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[0.8.0]: https://github.com/rnixai/rnix/compare/v0.7.3...v0.8.0
[0.7.3]: https://github.com/rnixai/rnix/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/rnixai/rnix/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/rnixai/rnix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/rnixai/rnix/compare/v0.6.8...v0.7.0
[0.6.8]: https://github.com/rnixai/rnix/compare/v0.6.6...v0.6.8
[0.6.6]: https://github.com/rnixai/rnix/compare/v0.1.0...v0.6.6
[0.1.0]: https://github.com/rnixai/rnix/releases/tag/v0.1.0
