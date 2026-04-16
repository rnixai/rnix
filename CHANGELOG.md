# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - 2026-04-16

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

### Changed

- Claude CLI driver uses `--bare` flag to prevent native tool interference
- Driver-reported tools merged into existing nativeToolDefs without overwriting
- Action execution guidelines and task handling instructions updated

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

[0.7.3]: https://github.com/rnixai/rnix/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/rnixai/rnix/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/rnixai/rnix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/rnixai/rnix/compare/v0.6.8...v0.7.0
[0.6.8]: https://github.com/rnixai/rnix/compare/v0.6.6...v0.6.8
[0.6.6]: https://github.com/rnixai/rnix/compare/v0.1.0...v0.6.6
[0.1.0]: https://github.com/rnixai/rnix/releases/tag/v0.1.0
