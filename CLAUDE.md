# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rnix is an operating system for AI agents, inspired by Unix design philosophy. It provides process management, a virtual filesystem (VFS), IPC via Unix domain sockets, multi-agent orchestration, intent decomposition, and debugging tools. Written in Go 1.26, module `github.com/rnixai/rnix`.

## Build & Development Commands

每次会话的编码工作完成后，要运行一次 `make all` 检查是否有错误需要修复。

```bash
make build          # Build binary → ./rnix
make test           # Run all tests with race detection
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + test + build

# Run a single test
go test -race -run TestFunctionName ./package/...

# Run tests for a specific package
go test -race ./kernel/...
go test -race ./ipc/...
```

Prerequisite: Go 1.26+ (managed via `mise.toml`).

## Architecture

### Daemon Model

Rnix runs as a background daemon holding the kernel and process table. CLI commands communicate with the daemon over a Unix domain socket (`$XDG_RUNTIME_DIR/rnix/rnix.sock` or `/tmp/rnix-$UID/rnix.sock`). The CLI auto-starts the daemon via `EnsureDaemon()` if not running. Manage the daemon with `rnix daemon status` and `rnix daemon stop`.

### Core Data Flow

```
CLI (cmd/rnix) → IPC Client → Unix Socket → IPC Server → Kernel
                                                            ↓
                                              Process ReasonStep Loop
                                                  ↓           ↑
                                             VFS Devices    Context
                                            (LLM, FS, Shell, MCP)
```

### Package Dependency Hierarchy

```
cmd/rnix           ← Entry point, Cobra CLI, all commands
├── ipc            ← Client/Server, NDJSON protocol over Unix socket
├── kernel         ← Microkernel: process table, spawn, kill, wait, reaper
│   ├── vfs        ← VFS file abstraction, device registry, FD table
│   ├── context    ← Per-process conversation history (CtxAlloc/Write/BuildPrompt)
│   └── debug      ← Strace, recording, distributed tracing, GDB
├── drivers/       ← VFS device implementations
│   ├── llm        ← /dev/llm/claude (Claude CLI), /dev/llm/cursor (Cursor CLI)
│   ├── fs         ← /dev/fs - sandboxed host filesystem
│   ├── shell      ← /dev/shell - subprocess execution
│   └── mcp        ← /dev/mcp/* - MCP server stdio transport
├── intent         ← Declarative intent decomposition & reconciliation (Epic 19)
├── compose        ← DAG-based multi-agent orchestration from YAML
├── shell          ← AgentShell scripting language (spawn, pipe, variables, control flow)
├── agents         ← Agent loader (lib/agents/{name}/agent.yaml + instructions.md)
├── skills         ← Skill loader (lib/skills/{name}/SKILL.md with YAML frontmatter)
├── skillpkg       ← Skill package management (install/search/update from registry)
└── internal/      ← Shared utilities
    ├── types      ← PID, FD, CtxID, Signal, ProcessState, ErrCode
    ├── xsync      ← Thread-safe SyncMap, Registry, Future
    └── ui         ← Terminal rendering, progress reporting
```

### Key Abstractions

**Process** (`kernel/process.go`): The primary compute unit. State machine: Created → Running → Zombie → Dead. Each process runs a `reasonStep` goroutine that loops LLM calls through VFS devices. Stores `Provider` and `Model` fields (immutable after spawn) for display in spawn/exit output. Reaped processes are persisted to `.rnix/data/steps/<uuid>/proc-info.json` and loaded on daemon startup via `LoadHistory()`. Per-process observation data is fully persisted: `steps.jsonl` (reasoning steps), `events.jsonl` (syscall events), `ctx-profile.json` (context heatmap snapshot), `process-meta.json` (system prompt + tool defs).

**Resume 设计哲学** (ADR Decision 40 / Bundle 1: A5 + B1 + C1): Dead 是冻结状态而非终态——进程数据保留在 `.rnix/data/steps/<uuid>/` 直到 gc 清理。Resume = 基于历史的新 Spawn，状态机零改动（参考 ADR Decision 40 / Bundle 1: A5 + B1 + C1）；通过 `rnix resume <uuid>` (续跑保 UUID) 或 `rnix resume --fork <uuid>` (分叉新 UUID) 触发。保留策略：`gc.retention_days` + `gc.max_entries` 双重退路；Running/Suspended 进程永久豁免。详见 [docs/process-resumption.md](docs/process-resumption.md)。

**VFS** (`vfs/`): All resources (LLM, filesystem, shell, MCP) are accessed as files via Open/Read/Write/Close. Devices register path prefixes. Each process has an FD table.

**Context** (`context/`): Per-process message history. `CtxAlloc` → `CtxWrite` → `BuildPrompt` cycle. Fixed-size message array with configurable MaxSize (default 256). When token usage or slot usage exceeds thresholds, Compact replaces history with an LLM-generated summary plus restored context (files, skills, plan).

**Kernel** (`kernel/kernel.go`): Composed of sub-interfaces — ProcessManager, MountManager, IPCManager, SignalManager, ProcGroupManager. Holds SyncMap-based process table.

**Intent System** (`intent/`): LLM-based decomposition of high-level intent into a DAG of sub-tasks. Reconciler executes with retry, timeout, and drift detection. States: pending → decomposing → await_confirm → executing → completed/failed.

**Unified Reasoning Loop**: Single `reasonStep` loop where LLM autonomously selects action type per step: tool_call, plan, spawn, complete, specialize, replan, text. Planning is a configurable capability (`planning: true/false`, default true), not a separate mode.

### IPC Protocol

NDJSON over Unix socket. Request: `{"method": "spawn|kill|list_procs|...", "payload": {...}}`. Response: `{"ok": true/false, "payload": {...}}`. Streaming: progress events during spawn.

### Agent & Skill Loading

- Agents: `lib/agents/{name}/agent.yaml` (manifest with model, skills, MCP) + `instructions.md` (system prompt)
- Skills: `lib/skills/{name}/SKILL.md` (YAML frontmatter with allowed_tools + markdown body)
- System prompt = agent instructions + concatenated skill bodies
- `AllowedDevices` = union of all skill `allowed_tools`

### Configuration Files

Two-tier configuration: global (`~/.config/rnix/`) + project (`.rnix/`). Run `rnix init` to bootstrap.

- `providers.yaml` — LLM provider definitions (`default_provider`, driver type, model, base URL, API key env)
- `init.yaml` — Bootstrap services and supervisor trees
- `compose.yaml` — Multi-agent workflow DAGs
- `agents/*/agent.yaml` — Agent manifests
- `skills/*/SKILL.md` — Skill definitions

## Conventions

- Import aliases: `rnixctx` for context, `drivershell` for drivers/shell, `agentshell` for shell (to avoid stdlib conflicts)
- Custom types in `internal/types`: use `types.PID`, `types.FD`, `types.CtxID`, etc. — not raw integers
- Thread safety via `internal/xsync.SyncMap` — not stdlib sync.Map
- Error codes: use `types.ErrCode` constants (TIMEOUT, NOT_FOUND, PERMISSION, etc.)
- Signals: `types.SIGTERM`, `types.SIGKILL`, `types.SIGINT`, `types.SIGPAUSE`, `types.SIGRESUME`
- Environment: `RNIX_ASCII=1` forces ASCII mode (disables Unicode glyphs in UI)
- Environment: `RNIX_ENV` selects .env file set (default: `development`); CLI passes to daemon via IPC
- Project `.env` files: loaded per-spawn from project root (`.env` → `.env.local` → `.env.{RNIX_ENV}` → `.env.{RNIX_ENV}.local`); API keys resolved via env snapshot, not `os.Getenv`

### Prompt Design Convention (Architecture Decision 33)

All prompt text (system prompts, VFS device descriptions, Action Protocol, compact prompts) follows the "Claude Code Baseline" principle:
- Reference Claude Code source files in `cc-src/src/` for established prompt patterns
- Apply concept mapping (Tool → VFS Device, Session → Process, Team → ProcGroup, etc.)
- Do NOT rewrite validated behavioral guidelines — adapt minimally
- New Rnix-specific content (signals, supervisor, intent) follows CC writing style

### VFS Device Registration Convention (Architecture Decision 35)

VFS device registration must declare full ToolDef metadata:
- Set IsReadOnly, IsConcurrencySafe, IsDestructive explicitly (fail-closed defaults: all false)
- Set MaxResultTokens to prevent context overflow
- Use ShouldDefer + SearchHint for non-core devices
- Device descriptions use Go embed templates (not hardcoded strings)

### Timeline Fold & Navigation (Story 41-3)

**toolAggGroup vs RootIntent**: These are distinct fold granularities. toolAggGroup (≥3 consecutive steps with same ToolPath, defined in `event.BuildToolAggGroups`) is the Timeline pane's fold unit. RootIntent is the Intent pane's collapse unit. Do not confuse them.

**HasExpandableContent**: Returns false when detail is loaded but has no additional content beyond the summary. This is by design — "already loaded, nothing new to show" is not a bug.

**V2.1 key bindings**:
- `j`/`k`/`↑`/`↓` — visible-row navigation (skips collapsed group internals)
- `Enter` — context-aware: on group header → toggle fold; on leaf → drill-in (Level 2 expand)
- `[`/`]` — dual-mode: no search → jump to prev/next group; search active → cycle matches
- `e`/`E`/`C` — sticky expand mode (unchanged)
- Fold markers: `▶` = collapsed, `▼` = expanded (ASCII: `>` / `v`)

## BMAD Workflow

Story artifacts live in `_bmad-output/implementation-artifacts/`. Sprint status tracked in `sprint-status.yaml`. Development follows the BMAD pipeline: create-story → ATDD → dev-story → code-review → traceability.

## Known Test Issues

- `TestRunTop_NoDaemon` fails in environments without `/dev/tty` (CI/containers)
- `TestClaudeCliDriver_Call_DefaultArgs` may fail if default model constant changes

## Driver Token Semantics (Cache Hit Rate)

The `input_tokens` field has driver-dependent semantics:
- **openai-compat / DeepSeek / OpenAI**: prompt_tokens INCLUDES cached_tokens; hit rate = cached / input
- **Anthropic** (native API): input_tokens EXCLUDES CacheReadInputTokens; hit rate = cached / (input + cached)
- **CLI drivers** (claude-cli / cursor-cli / codex-cli): use OpenAI fallback semantics

See `internal/dashboard/inspector/meta_lens.go:ComputeCacheHitRate` for branching.
