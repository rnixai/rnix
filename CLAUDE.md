# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rnix is an operating system for AI agents, inspired by Unix design philosophy. It provides process management, a virtual filesystem (VFS), IPC via Unix domain sockets, multi-agent orchestration, intent decomposition, and debugging tools. Written in Go 1.26, module `github.com/rnixai/rnix`.

## Project Context

_bmad-output/project-context.md

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

**Process** (`kernel/process.go`): The primary compute unit. State machine: Created → Running → Zombie → Dead. Each process runs a `reasonStep` goroutine that loops LLM calls through VFS devices. Stores `Provider` and `Model` fields (immutable after spawn) for display in spawn/exit output. Reaped processes are persisted to `.rnix/data/steps/<uuid>/proc-info.json` and loaded on daemon startup via `LoadHistory()`. Per-process observation data is fully persisted: `steps.jsonl` (reasoning steps), `events.jsonl` (syscall events), `raw.jsonl` (raw LLM request/response captures, Story 56.1 envelope + 56.2 API drivers + 56.3 CLI drivers = 8/8 drivers active; one NDJSON line per reasonStep), `ctx-profile.json` (context heatmap snapshot), `process-meta.json` (system prompt + tool defs).

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

### Claude CLI Driver 兼容性约定 (Epic 40)

Claude CLI driver (`/dev/llm/claude`) uses capability probing to adapt to different CLI versions:

- **Capability probe**: `claude -p --help` with 5s timeout; scans stdout for `--include-partial-messages`, `--add-dir`, `--permission-mode`
- **Default permission**: `bypassPermissions` (daemon-managed, no-TTY agent loop)
- **Fallback binaries**: `claude` → `openclaude` (configurable via `WithFallbackBins`)
- **Extended search paths**: `~/.local/bin`, nvm latest, `~/.bun/bin`
- **DriverMetaProvider**: optional interface on LLM drivers; kernel spawn path type-asserts to populate `ProcInfo.DriverMeta` and emit `claude_cli.resolve` / `claude_cli.capabilities` strace events

| Key | Value | Example |
|-----|-------|---------|
| `resolved_bin` | Absolute path to resolved binary | `/usr/local/bin/claude` |
| `permission_mode` | Active permission mode | `bypassPermissions` |
| `cap_partial_messages` | `--include-partial-messages` supported | `"true"` / `"false"` |
| `cap_add_dir` | `--add-dir` supported | `"true"` / `"false"` |
| `cap_permission_mode` | `--permission-mode` supported | `"true"` / `"false"` |
| `fallback_candidates` | Comma-joined candidate binary names (split to `candidates[]` in the `claude_cli.resolve` event) | `claude,openclaude` |
| `probe_duration_ms` | Capability-probe wall-clock duration in ms (emitted as `probe_duration_ms` in the `claude_cli.capabilities` event) | `12` |

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
- `TestATDD_42_2_INT_003_E2E_CrashRecovery`（ipc）原低频 flaky 已修复：resume 后 mock LLM 返回 `complete` 会使进程秒退→reap 移出 procTable，与随后的 `ListResumable` 形成时序竞态（`ListResumable` 只过滤 Running 状态，符合 Epic 42 "Dead 可重新 resume" 哲学）。修复方式：给共享 mock（`atdd_42_1_resume_ipc_test.go` 的 `mockLLMFile.parkOnRead`）加握手 gate，让 resumed 进程在首次 LLM Read 处停住（已 past `proc.Start()` → Running），E2E 测试等 `reached` 信号后再断言、断言完 `close(release)` 放行，消除竞态。

## Driver Token Semantics (Cache Hit Rate)

The `input_tokens` field has driver-dependent semantics:
- **openai-compat / DeepSeek / OpenAI**: prompt_tokens INCLUDES cached_tokens; hit rate = cached / input
- **Anthropic** (native API): input_tokens EXCLUDES CacheReadInputTokens; hit rate = cached / (input + cached)
- **CLI drivers** (claude-cli / cursor-cli / codex-cli): use OpenAI fallback semantics

See `internal/dashboard/inspector/meta_lens.go:ComputeCacheHitRate` for branching.

## Reasoning Effort (Epic 55)

`ProviderConfig.reasoning_effort` (`drivers/llm/config.go`) configures discrete reasoning strength — the离散等级语义 that OpenAI/Anthropic/Gemini newest models have converged on, replacing the legacy `thinking_budget(int)`. **透传语义（spec owner 决策 2026-06-14）**：rnix 原样下发该字符串，**不校验、不映射、不维护规范等级集**。SDK 的 effort 字段均为开放 `string` 类型且无运行时校验，因此 `xhigh`（OpenAI gpt-5.1-codex-max+）及厂商未来新增等级无需改代码即可透传——实现禁止 `switch`/枚举白名单。写错的值由底层 API/CLI 自行报错或降级。

各 driver 的 effort 支持形态对照（详见 `_bmad-output/implementation-artifacts/investigations/llm-driver-effort-level-investigation.md`）：

| Driver | 机制 | 写入位置 | 已知取值 | 备注 |
|--------|------|----------|----------|------|
| `openai` | API 参数 | `ChatCompletionNewParams.ReasoningEffort` | none/minimal/low/medium/high/**xhigh**（小写） | 空=省略字段 |
| `openai-compat` | 请求 body | `reasoning_effort` 字段 | 同 OpenAI（DeepSeek V4 等原生接受） | 与 `thinking_budget` **正交可共存**（DeepSeek 多轮工具调用需 budget） |
| `anthropic` | API 参数 | `MessageNewParams.OutputConfig.Effort`（stable 非 beta） | low/medium/high/max（小写） | **迁移**：effort 优先；`thinking_budget` 路径**保留为降级**（DeepSeek V4 Anthropic-兼容端点多轮工具调用必需，缺失 HTTP 400） |
| `gemini` | API 参数 | `ThinkingConfig.ThinkingLevel` | **MINIMAL/LOW/MEDIUM/HIGH（大写！）** | **迁移**：level 与 `thinking_budget` **互斥**（Gemini 3 同传两者报错）；level 非空时不传 budget；budget 保留给 Gemini ≤2.5 |
| `claude-cli` | CLI flag | `--effort <value>`（内置 args → effort → extraArgs） | 透传 | 旧版 CLI 不识别 `--effort` 会自行报错（见「Claude CLI Driver 兼容性约定」，MVP 不做 probe） |
| `codex-cli` | CLI flag | `-c model_reasoning_effort=<value>` | 透传 | 空=不追加 |
| `cursor-cli` | **不支持（no-op + warning）** | — | — | thinking level 绑在 model 名后缀（如 `sonnet-4.5-thinking-high`），无独立 effort 参数；factory 配置非空时记 warning |
| `qwen-cli` | **不支持（no-op + warning）** | — | — | Qwen3-Coder 无 effort 概念（仅 non-thinking）；factory 配置非空时记 warning |

⚠️ **大小写不统一陷阱**：透传语义下 rnix 不转换大小写——Gemini 的 `ThinkingLevel` 是**大写**（`HIGH`），OpenAI/Anthropic 是**小写**（`high`）。为 gemini provider 配 `reasoning_effort` 必须写大写。

配置文档与示例见 [docs/reasoning-effort.md](docs/reasoning-effort.md)。
