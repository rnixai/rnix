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
│   └── debug      ← Strace, recording (event time-travel + fork-continue, not deterministic replay; superseded in practice by always-on raw/events/steps), distributed tracing, GDB
├── drivers/       ← VFS device implementations
│   ├── llm        ← /dev/llm/claude (Claude CLI), /dev/llm/cursor (Cursor CLI)
│   ├── fs         ← /dev/fs - sandboxed host filesystem
│   ├── shell      ← /dev/shell - subprocess execution
│   └── mcp        ← /dev/mcp/* - MCP server stdio transport
├── intent         ← Declarative intent decomposition (Epic 19)
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

**Process** (`kernel/process.go`): The primary compute unit. State machine: Created → Running → Zombie → Dead. Each process runs a `reasonStep` goroutine that loops LLM calls through VFS devices. Stores `Provider` and `Model` fields (immutable after spawn) for display in spawn/exit output. Reaped processes are persisted to `.rnix/data/steps/<uuid>/proc-info.json` and loaded on daemon startup via `LoadHistory()`. Per-process observation data is fully persisted: `steps.jsonl` (reasoning steps), `events.jsonl` (syscall events; spawn 早期 `ConfigResolve`/Mount 等事件自 Story 56.5/CAP-5 起落盘——EventWriter attach 提前到 `ConfigResolve` emit 之前，命中 0→>0), `raw.jsonl` (raw LLM request/response captures, Story 56.1 envelope + 56.2 API drivers + 56.3 CLI drivers = 8/8 drivers active; one NDJSON line per reasonStep; queried 事后 via Story 56.4 三路——`rnix strace <pid> --raw`、dashboard inspector `❻ Raw I/O` lens、IPC `get_raw_capture`,共用单一后端), `ctx-profile.json` (context heatmap snapshot), `process-meta.json` (system prompt + tool defs).

**Resume 设计哲学** (ADR Decision 40 / Bundle 1: A5 + B1 + C1): Dead 是冻结状态而非终态——进程数据保留在 `.rnix/data/steps/<uuid>/` 直到 gc 清理。Resume = 基于历史的新 Spawn，状态机零改动（参考 ADR Decision 40 / Bundle 1: A5 + B1 + C1）；通过 `rnix resume <uuid>` (续跑保 UUID) 或 `rnix resume --fork <uuid>` (分叉新 UUID) 触发。保留策略：`gc.retention_days` + `gc.max_entries` 双重退路；Running/Suspended 进程永久豁免。详见 [docs/process-resumption.md](docs/process-resumption.md)。

**VFS** (`vfs/`): All resources (LLM, filesystem, shell, MCP) are accessed as files via Open/Read/Write/Close. Devices register path prefixes. Each process has an FD table.

**Context** (`context/`): Per-process message history. `CtxAlloc` → `CtxWrite` → `BuildPrompt` cycle. Fixed-size message array with configurable MaxSize (default 256). When token usage or slot usage exceeds thresholds, Compact replaces history with an LLM-generated summary plus restored context (files, skills, plan).

**Kernel** (`kernel/kernel.go`): Composed of sub-interfaces — ProcessManager, MountManager, IPCManager, SignalManager. Holds SyncMap-based process table.

**Intent System** (`intent/`): LLM-based decomposition of high-level intent into a DAG of sub-tasks. Reconciler executes with retry, timeout, and cascade-failure handling. States: pending → decomposing → await_confirm → executing → completed/failed.

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
- Environment: `RNIX_FEATURE_PROFILE` overrides the feature profile — **read once at daemon startup**（daemon 进程内 `os.Getenv`）；对已运行的 daemon 无效（CLI 侧检测到不匹配会打 stderr 警告），需 `rnix daemon stop` 后重跑生效；CLI 自启动 daemon 时因 env 继承而生效
- Project `.env` files: loaded per-spawn from project root (`.env` → `.env.local` → `.env.{RNIX_ENV}` → `.env.{RNIX_ENV}.local`); API keys resolved via env snapshot, not `os.Getenv`

### Prompt Design Convention (Architecture Decision 33)

All prompt text (system prompts, VFS device descriptions, Action Protocol, compact prompts) follows the "Claude Code Baseline" principle:
- Reference Claude Code source files in `cc-src/src/` for established prompt patterns
- Apply concept mapping (Tool → VFS Device, Session → Process, etc.)
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

### Codex CLI Driver 沙箱配置约定 (Epic 62)

Codex CLI driver (`/dev/llm/codex`) configures shell-command sandboxing through `providers.yaml`:

```yaml
providers:
  - name: codex
    driver: codex-cli
    default_model: gpt-5.1-codex
    sandbox_mode: danger-full-access  # worktree with protected metadata symlinks
```

- **Config key**: `sandbox_mode` (codex-cli only)
- **Valid values**: `read-only`, `workspace-write`, `danger-full-access`
- **Default**: empty means `workspace-write`; the driver emits `codex exec --sandbox workspace-write`, not deprecated `--full-auto`
- **Safety visibility**: `danger-full-access` logs a construction-time warning because Codex sandboxing is fully disabled
- **Non-codex providers**: `sandbox_mode` is ignored with a warning; do not map it to Claude `permission_mode`
- **Escape hatch**: prefer `sandbox_mode` for normal use. `extra_args` remains a raw Codex escape hatch, but `--yolo` / `--dangerously-bypass-approvals-and-sandbox` can conflict with `--sandbox`; for worktrees that need no sandbox, use `sandbox_mode: danger-full-access` instead of `extra_args: [--yolo]`

The Epic 62 production failure had two causes: `--yolo` conflicted with the old hardcoded `--full-auto`, and `--full-auto` forced Codex into `workspace-write` where Linux bubblewrap fail-closed on protected metadata symlinks such as `.agents` in worktrees. The config channel exists so operators can select the right sandbox strength explicitly.

### 工具 Input/Result 权威回填与 flush 时序 (Story 40-4 / 62-5)

CLI driver（`/dev/llm/claude`、qwen 等）的工具调用 Input 经 **assistant 块权威回填 + flush 时序修正**，Result 经 **user(tool_result) 精确回填**，不再受 `--include-partial-messages` 下的交错时序影响：

- **根因**：claude CLI 把上一轮 `user`(tool_result) 与下一轮工具的 partial input deltas 交错输出。`kernel/observe.go::setupDriverStreamHandler` 旧逻辑在 `user` 事件无条件 `flushPendingTool()`，把刚 `started`、只累积首分片的下一轮工具提前截断 flush（全局 4834 工具步骤中 51 空 + 192 截断）。
- **修复**：①assistant tool_use 三种 content 形态（`[]map[string]any` / `[]any` / `map[string]any`）均读 `block["input"]`，按 `call_id` 存为权威 input；②assistant 只记录权威 input，不立即 flush；③`user` 事件只在 tool_result 的 `tool_use_id` 命中 pending `call_id` 时回填 result 并 flush，避免误 flush 正在收 input 的下一轮工具；④done backstop 仍负责终端工具兜底，result 允许为空。
- **可观测取舍**：claude/qwen 的 steps.jsonl 写盘时机从 assistant 后移到 user(tool_result) 或 done；步骤序号仍按 started 顺序，实时视图出现会略晚，但 ToolInput/ToolResult 完整性优先。
- **driver 兼容**：codex/cursor 工具仍由各自的 `completed` 事件 flush，不受影响（它们不 emit 携带 tool_use block 的 assistant 事件）。渲染层（`internal/dashboard/timeline/render.go`）零改动——`detail.ToolInput` 完整即自动正确。

### Timeline Fold & Navigation (Story 41-3)

**toolAggGroup vs RootIntent**: These are distinct fold granularities. toolAggGroup (≥3 consecutive steps with same ToolPath, defined in `event.BuildToolAggGroups`) is the Timeline pane's fold unit. RootIntent is the Intent pane's collapse unit. Do not confuse them.

**HasExpandableContent**: Returns false when detail is loaded but has no additional content beyond the summary. This is by design — "already loaded, nothing new to show" is not a bug.

**V2.1 key bindings**:
- `j`/`k`/`↑`/`↓` — visible-row navigation (skips collapsed group internals)
- `Enter` — context-aware: on group header → toggle fold; on leaf → drill-in (Level 2 expand)
- `[`/`]` — dual-mode: no search → jump to prev/next group; search active → cycle matches
- `e`/`E`/`C` — sticky expand mode (unchanged)
- Fold markers: `▶` = collapsed, `▼` = expanded (ASCII: `>` / `v`)

### 项目根 AGENTS.md 注入 (Story 35.7 / Architecture Decision 47)

进程 spawn 时自动读取项目根 `AGENTS.md` 并作为 `project_doc` cached section 注入 system prompt，对齐 AGENTS.md 行业标准（Linux Foundation AAIF 托管、30+ 工具原生读取；机制同 CLAUDE.md）。详见 [docs/agents-md-injection.md](docs/agents-md-injection.md)。

- **落点**：`kernel/sections.go registerSections` 在 `agent_instructions` 之后、`memory` 之前注册 `project_doc`（`cached=true`）；helper `internal/config.FindNearestAgentsMD(startDir, projectRoot)` 仿 `internal/config/paths.go ProjectDir` 向上遍历（nearest-wins，边界=projectRoot 不越界）。
- **冻结快照**：eager 闭包捕获（仿 `agent_instructions`，强于 `memory` 的 lazy ComputeFn）——spawn 时读盘一次、`Invalidate`（specialize 重建 prompt）也不重读，保护 LLM prompt cache 命中率（同 35-2 教训）。注入正文经 `Build()` → `proc.FinalSystemPrompt` → `process-meta.json` 的 `system_prompt` 字段可见。
- **排他只认 `AGENTS.md`**：硬编码文件名，**绝不**回退读本仓库根写给 Claude Code 的 `CLAUDE.md`（避免内容错位 + 文件双主人 + 文件名冒名三重冲突，见 `SPEC-agents-md-injection` Non-goal）。
- **降级 + 截断**：`ProjectConfig==nil`/文件缺失/读失败 → 空段（显式 `os.ReadFile` error 处理，不靠 `Build()` 的 panic recovery）；超 `config.MaxAgentsMDBytes`（64 KiB，仿 Story 48.6 `max_output_bytes`）→ UTF-8 边界安全截断 + 尾标记 + `log.Printf` 警告，进程不 crash。
- **默认开 + 可禁用**：`agent.yaml` 的 `project_doc: false`（manifest `ProjectDoc *bool`，nil=开，仿 `planning`）禁用，传导至 `proc.ProjectDocInjection`（`NewProcess` 默认 true）。无 agent 的直接 spawn 默认开。⚠️ 当前架构进程工作目录==项目根（`ProjectConfig` 无独立 cwd 字段，`spawn.go SetWorkDir(PID, ProjectDir)`），真实 spawn 只命中根级；nearest-wins 语义由单元测试喂深 startDir 验证。

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

⚠️ **大小写不统一陷阱**：透传语义下 rnix 不转换大小写——Gemini 的 `ThinkingLevel` 是**大写**（`HIGH`），OpenAI/Anthropic 是**小写**（`high`）。为 gemini provider 配 `reasoning_effort` 必须写大写（agent.yaml 入口同样适用此陷阱）。

**解析优先级（四级兜底，`kernel/spawn.go`，与 model 同构）**：`opts.ReasoningEffort`（per-spawn：CLI `--effort`/compose/intent）> `agent.Manifest.Models.ReasoningEffort`（`lib/agents/{name}/agent.yaml` 的 `models.reasoning_effort`）> driver 快照（`providers.yaml` 实例级）> 透传 `""`（API/CLI 原生默认）。高级别非空即胜出。⚠️ **providers.yaml 一旦设值即成该 provider 的事实地板**——agent/任务都没指定时，落到的是 provider 值而非 API 原生默认（第 4 级仅在前三级全空时到达）。

配置文档与示例见 [docs/reasoning-effort.md](docs/reasoning-effort.md)。
