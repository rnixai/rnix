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
make agtest         # Tier1 agent-behavior regression suite, isolated daemon (PR gate; not part of `all`)
make agtest-live    # Tier2 advisory suite against your ambient daemon + real provider/API key

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
│   └── debug      ← Strace, recording (time-travel navigation + fork-continue, not deterministic replay — superseded in practice by raw/events/steps), distributed tracing, GDB
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

**Process** (`kernel/process.go`): The primary compute unit. State machine: Created → Running → Zombie → Dead. Each process runs a `reasonStep` goroutine that loops LLM calls through VFS devices. Stores `Provider` and `Model` fields (immutable after spawn) for display in spawn/exit output. Reaped processes are persisted to `.rnix/data/steps/<uuid>/proc-info.json` and loaded on daemon startup via `LoadHistory()`. Per-process observation data is fully persisted: `steps.jsonl` (reasoning steps), `events.jsonl` (syscall events; spawn 早期 `ConfigResolve`/Mount 等事件自 Story 56.5/CAP-5 起落盘——EventWriter attach 提前到 `ConfigResolve` emit 之前，命中 0→>0), `raw.jsonl` (raw LLM request/response captures, Story 56.1 envelope + 56.2 API drivers + 56.3 CLI drivers + 68.1 replay driver = 9/9 drivers active; one NDJSON line per reasonStep; queried 事后 via Story 56.4 三路——`rnix strace <pid> --raw`、dashboard inspector `❻ Raw I/O` lens、IPC `get_raw_capture`,共用单一后端), `ctx-profile.json` (context heatmap snapshot), `process-meta.json` (system prompt + tool defs).

**Resume 设计哲学** (ADR Decision 40 / Bundle 1: A5 + B1 + C1): Dead 是冻结状态而非终态——进程数据保留在 `.rnix/data/steps/<uuid>/` 直到 gc 清理。Resume = 基于历史的新 Spawn，状态机零改动（参考 ADR Decision 40 / Bundle 1: A5 + B1 + C1）；通过 `rnix resume <uuid>` (续跑保 UUID) 或 `rnix resume --fork <uuid>` (分叉新 UUID) 触发。保留策略：`gc.retention_days` + `gc.max_entries` 双重退路；Running/Suspended 进程永久豁免。详见 [docs/process-resumption.md](docs/process-resumption.md)。

**VFS** (`vfs/`): All resources (LLM, filesystem, shell, MCP) are accessed as files via Open/Read/Write/Close. Devices register path prefixes. Each process has an FD table.

**Context** (`context/`): Per-process message history. `CtxAlloc` → `CtxWrite` → `BuildPrompt` cycle. 可增长消息切片；`MaxSize` 是**准入上限**（`CtxAlloc` 建的是 `make([]Message, 0)`，不是预分配定长数组）。⚠️ **Story 71.1 起 `MaxSize == 0 = 无上限，且是生产默认值**——`spawn.go` / `ipc/server_record.go` / 两条 resume 路径一律传 0，`CtxAlloc(size<=0)` 不再报错，四处准入校验全部加 `MaxSize > 0 &&` 前缀，`AvailableSlots` 在无上限时返回 `unlimitedSlots`（`math.MaxInt32`）哨兵。旧的 256 默认是**量纲错误**：槽位量的是结构（消息条数），容量量的是体积（token），822 份实测样本下二者无稳定换算率（205 槽 → 36.7k…146.2k tokens，4.0x 跨度），且 slot 轴在约 36k tokens 即触发、**永远先于** token 轴，事故现场 47 次 compact 全部标 `slot_threshold`。`SpawnOpts.CtxSize` / `agent.yaml ctx_size` >0 时仍设上限，作运维逃生阀（但**不跨 resume 存活**——`Deserialize` 恒把 `MaxSize` 置 0 以防旧快照的 `max_size: 256` 复活上限，见 deferred-work）。`ErrContextFull` 的**原子准入**职责（assistant + 全部 tool_result 必须同时入列，否则 provider HTTP 400）逐字保留：无上限时该保证以更强形式满足，有上限时检查逻辑不变，故 `ErrContextFull` → `selfSuspend("context_full")` 整条路径**保持可达可测**。

compact **只由 token 轴触发**（Story 71.1 AC3 废弃了 `autoCompactIfNeeded` 的 `slot_threshold` 与 `reclaimLeakedIfNeeded` 的 `slot_watermark` 两条 slot 轨道，连带删除全仓无写入点的 `SlotCompactThreshold` 三件套；`both` 随之消失）。`SlotUsed` 仍是真实历史长度并保留在 IPC wire / dashboard 中作观测，`SlotMax` / `SlotPercentage` 恒 0（消费端守卫本就存在，零改动）。

**backpressure 提示词注入挂 token 轴**（Story 71.1 AC2）：`kernel/sections.go` 的 `backpressure` section 分子取 `proc.LastInputTokens`（provider 上报的真实 prompt tokens，provenance 最高），分母取 `proc.effectiveContextTokenLimit()`——与 `applyCtxTokenLimit` **同源**，闸门同为 `ContextWindow > 0 && ContextBudget > 0`，否则回落 `DefaultTokenLimit`(200k)。🔴 该 ComputeFn **绝不能调 `ctxMgr.TokenUsage()`**：`TokenUsage` 内部调 `sections.Build()`，`Build()` 又调该 ComputeFn → 无限递归 stack overflow；方案全取 kernel 侧 `proc` 字段正是为了不碰 ctx 锁、零重入（`GetLastInputTokens` 仍取 `proc.mu`——`LastInputTokens` 在 `reason.go` 每步于 `proc.mu` 下写，该锁必要且与 ctx 锁无关）。`backpressureTier` / `backpressureText` 的签名与文案逐字未变（69.1 的两条约束——tier 内 byte-identical、qualitative never quantitative——天然保住）。附带收益：slot 轴在 36k tokens 即报警，故 backpressure 此前**一直在误报**。

⚠️ **排障须知**（Story 69.4）：上下文中冷区的 tool result 正文可能被**主动就地擦除**为占位标记 `context.DefaultPrunePlaceholder`（"[tool output cleared to free context]"），消息条目本身与 Role/ToolCallID 逐字保留。这**只影响后续请求体**——`steps.jsonl` / `raw.jsonl` 是每步独立的写盘快照（写入时即定），历史正文不受影响，故排障时查这两处而非当前上下文。

**token 轴刻度来源**（Story 69.2 接线 + Story 71.1 补项目级来源，配置语义的唯一路径）：`providers.yaml` 的 `providers[].models[<model>].context_window` → **两个来源，项目级优先**：①项目级 `.rnix/providers.yaml` 经 `ipc/server_spawn.go` 的 `DeepMergeYAML` → `ProjectConfig.Providers` → `lookupProjectContextWindow`（`kernel/ctx_token_limit.go`）②全局 `~/.config/rnix/providers.yaml` → `SetContextWindowFunc` 闭包（`cmd/rnix/main.go`）→ `k.contextWindowFunc` → `proc.ContextWindow` → `*9/10` → `proc.ContextBudget`（clamp 到 window）→ `ctx.TokenLimit`（`kernel/ctx_token_limit.go` 的 `applyCtxTokenLimit`，覆盖 spawn / checkpoint resume / disk resume 三条路径）→ `TokenUsage().Limit` → compact 的 token 阈值分母。取 `ContextBudget`（9/10）而非裸 window 是为与四处既有观测分母统一（`debug/ctx_profile.go` 的 `usagePct`、`ipc/server_process.go`、`cmd/rnix/top.go`）。未配 `context_window` 时 `ctx.TokenLimit` 保持 0 并回落 `DefaultTokenLimit`（200k）。

⚠️ 来源①是 Story 71.1 补的（R5）：`SetContextWindowFunc` 的唯一生产注入点按值捕获 daemon **启动期的全局快照**，项目级配置合并后只流向 `ProjectConfig.Providers` / driver factories / status cache 三处，无任何一条边回到 `contextWindowFunc`——于是"只在项目级声明"的 provider 刻度恒为 0（69.2 的接线完好，喂进去的数却是 0）。**写该链路的测试务必走 `SpawnOpts.ProjectConfig` 真实路径**：用 `SetContextWindowFunc` 打桩会天然绕过这个真实来源，缺陷在桩下完全不可见。

⚠️ **`proc.ContextBudget` 是语义重载字段**：除了由 window 派生，它同时是 `agent.yaml` 的 `context_budget` / `init.yaml` / `SpawnOpts` / supervisor `ChildSpec` 设的**单步 InputTokens 勒绳**（`reason.go` 用它判断单步越限即挂起，manifest 里 4096 这类小值很常见）。故 `applyCtxTokenLimit` 的闸门是 `ContextWindow > 0` 而非「budget 非零」——把勒绳当容量刻度会让进程每步都越 80% 阈值并被 compact。

**token 统计口径**：`EstimateMessageTokens`（`context/tokenizer.go`）是**唯一**的 per-message 口径，`TokenUsage` / `estimateMessagesTokens` / `Compact` 的 `postTokens` 三处共用，`GetContextInfo`（gdb `inspect context`）亦按角色循环调它——与 `TokenUsage` 对同一 `effectiveTokenLimit()` 分母保持分子一致（69-2 Review P1，此前 Content-only 分子使 gdb 百分比偏低 4-8 倍）。计入 `Content` + `Reasoning` + `ToolCalls[].ID`/`.Name` + `json.Marshal(ToolCalls[].Input)` + 消息级 `ToolCallID` + `ReasoningBlocks[].Type`/`.Thinking`/`.Data`；刻意不计 `Signature` / `ThoughtSignature`（不透明凭据，非模型可见文本）。`EstimateTokens` 的 3.5 / 1.5 比率不得改、不得加补偿系数。`debug/ctx_profile.go` 的 `TotalTokens` 仍是 Content-only 口径（其 `CtxMessage` schema 只有 Role/Content/ToolCallID），故**低于** `TokenUsage().Used`，属已知残差。

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

## 免疫系统检测语义与存储约定 (IN-3)

`kernel/immune.go` 的 `syscall_freq` 检测经 IN-3（2026-07-30）修复确定性误报退化。碰免疫检测/威胁库/Sec 面板任务前先读[卷宗](_bmad-output/implementation-artifacts/investigations/immune-false-positive-degeneration-investigation.md)。

- **三重门，缺一不报**：`cur > mean+3σ` **且** `cur > minAnomalyFloor(5)` **且** `cur > SyscallMax×histMaxGrowthFactor(1.5)`。旧实现只有第一条，对出现率 p≲0.1 的稀有 syscall 单次正常调用必报（偏差倍数 = 样本数，纯基率算术），重尾混合负载下正常长任务也必越界。`SyscallMax` 为 0/缺失时跳过增长门（兼容 legacy profile）。
- **威胁签名只标注、不报警**：`MatchThreat` 命中**不再**短路。顺序恒为「统计检验先行 → 命中则 Detail 追加 `matches known threat <id> (created <date>)` 并跳过重复铸签名」。`AnomalyAlert.Deviation` 恒为当前实测（旧实现填签名冻结的历史阈值，违反 provenance 原则）。
- **威胁库唯一写路径 = `ImmuneStore.RewriteThreats`**（`SaveThreat` append-only 已删）。内存为准：`upsertThreat` 同 `(template,type,metric)` 键替换；`Start` 跑一次清洗（去重留最新 + `threat_ttl_days` 默认 30 + `max_threats` 默认 500，0 = 关闭）。纠错口 `rnix immune forget <template> [--metric M] | --all`（IPC `immune_forget`）。
- **store 按项目分桶**：`<dataDir>/immune/<projectID>/{<template>.jsonl, profiles/, threats.jsonl}`，`projectID` = `config.ProjectDataID(ProjectDir)`，无项目上下文落 `"global"`。⚠️ 内存 `profiles`/`threats` 键为 scoped（`immuneScopedKey`），但 similarity / coopHistory / `AgentTemplateForPID` **保持 agent 级全局语义不分桶**。旧顶层文件被忽略——这就是存量污染数据的清洗方式，勿写迁移代码。
- **`syscall_freq` 名不副实**：实为 lifetime-count 检验（累计计数 vs 完整运行总量，无时间窗）。枚举字符串值**不可改**（threats.jsonl / `AlertWire.Type` / dashboard `AlertTypeColor` 兼容），正名只在注释与文档层。
- 空 `agentTemplate` 的裸 spawn 在 `OnProcessStart` 直接 no-op（无行为身份，共享 `""` 桶只会制造跨负载误报）。

## 循环检测阈值语义 (Story 70.1)

**新不变量：结果重复是循环判定的必要条件。** `kernel/loop_detector.go` 两条轨道均把 tool result hash 混入判据——细粒度 = `(actionType, toolPath, toolInput, result)`，粗粒度 = `(actionType, toolPath, result)`（仍忽略 input，捕捉「LLM 变参数但结果不动」的 thrashing）。修复前粗粒度 hash 对所有 `Bash` 调用是同一常量，「连续 30 步 Bash」即挂起，与是否原地打转无关——长跑编排器每跑完一个 story 撞线一次。

- **默认值**：`DefaultLoopThreshold` 30（挂起线 60）、`DefaultCoarseLoopThreshold` 60（挂起线 120）。结果判据落地后粗粒度退化为纯兜底，故阈值抬高而非收紧。
- **三级解析**：`SpawnOpts` > `agent.yaml` 的 `loop_threshold` / `coarse_loop_threshold` > 默认常量（`applyLoopThresholds`）。⚠️ 判定用 `!= 0` 而非 `> 0`——照抄 StepTimeout 的 `> 0` 会让负数被当「未设置」，禁用意图静默消失。**不入** CLI flag / IPC wire（运维逃生阀由 manifest 提供）。
- **结果 hash 取全批顺序敏感聚合**（`ToolResultHash`，含 `Error`）。⚠️ 禁止按下标配 `toolCallsAcc[i]` 与 `resp.ToolCalls[i]`——`tool_exec.go` 的 parse_error / think / unknown tool 三分支 `continue` 时不 append，对应关系不成立。action 侧仍只取 `ToolCalls[0]`（deferred-work 已登记），该不对称是刻意的：结果侧取全批更保守（更易判为「不同」= 更少误报）。
- **滞后一格**：检测点在 `executeToolCalls` **之前**，本步 result 尚不存在，故传入的是**上一个** tool_call 步骤的结果 hash（首步哨兵 `0`）。保留「执行前拦截」避免真死循环在挂起前多跑一次副作用工具（`git commit` / `spawn` / `rm`）；连续同 action 同 result 的真循环其 result 序列亦全同，错位只把触发点从 2N 推到 2N+1。
- **已知局限**（不得掩盖）：结果含时间戳 / PID / 随机 ID 时两条轨道**均永不触发**。这是该不变量的固有代价，兜底是 `--max-steps` / `MaxTokens` / `MaxCost` / `StepTimeout` 四道独立闸门。
- **写测试须知**：新默认值 30/60 下「40 步各异」这类反证会**真空 PASS**（40 撞不到 60，判据一行未改也绿）。反证必须显式构造小阈值 `NewLoopDetector(3, 5)`。

**三种零值约定并存对照**（加旋钮前先确认落在哪一栏，勿盲目对齐相邻字段形状）：

| 旋钮 | 类型 | `0` 的含义 | 负数 | 禁用方式 |
|---|---|---|---|---|
| `StepTimeout` | duration 字符串 | **禁用**超时检测 | — | 设 `0` |
| `CompactTimeout` | duration 字符串 | 回落默认 30s（**无**禁用语义） | — | 不可禁用 |
| `loop_threshold` / `coarse_loop_threshold` | **int 步数** | 回落默认 30 / 60 | **禁用该轨道** | 设负数（如 `-1`） |

## 循环检测警告机制 (Story 70.2)

**警告不再注入上下文，仅发 `LoopDetected` 事件。** `kernel/reason.go` 的 `handleLoopDetection` 在 `LoopWarning` 分支只 emit 事件 + 记日志，**不**向 context 追加消息。旧实现把 `LoopWarningMessage` 以 `RoleUser` 身份 `AppendMessage` 进上下文，删除理由是两条实证代价加一条未证实的收益：

- **持久污染**：注入的消息此后在**每次** BuildPrompt 中重放，且伪装 `RoleUser` 身份。事故会话 4 条轨道均被污染，重放 2 / 11 / 16 / **61** 次（`meetup-30a8ac79`）。
- **误导文案**：旧文案「Please try a different approach」对编排器是错误建议——它应继续按既定流程执行。
- **收益未证实**：唯一可观测的样本是**误判**场景，其中编排器正确地忽略了警告、继续完成既定流程直至 16 步后撞 2N 挂起线。这证明警告在误判时无害（LLM 能甄别），但对「真循环时是否有用」**零信息量**——而真循环才是该机制的目的。不要把这条读成「警告已被证明无用」。

⚠️ **两条曾被写进本节的论据已被实测推翻（Story 70.2 code-review 2026-08-01），勿再引用**：

1. **注入并不破坏 prompt 缓存前缀。** `context.AppendMessage` 是 `ctx.Messages = append(...)`——**纯尾部追加，不是「对话中段插入」**，其后无已缓存内容可失效；注入文本内容在注入时即定（`repeated N times` 不随步数变），此后作为稳定前缀持续命中。实测事故轨道注入点前后命中率：step 34 = 97.8% → step 35（emit）= 99.0% → step 36（首个含警告的请求）= 98.9% → step 37-51 稳定 98.3-99.7%，**零塌陷**。
2. **本例与 Epic 69.1 不是同一反模式。** 69.1 是 system prompt 侧的**不变前缀**里嵌**每步都变**的槽位计数，内容一变整段作废，那才是 99.5%→8.9% 的机制；本例是往可变尾部追加一条此后不再改变的静态消息，正是缓存预期的增长方式。**决定性反证**：同会话 `019fb08a` 的警告注入在 step 50，命中率一路 99% 无损到 step 75，真正的塌陷在 **step 76**——即 69.1 的 backpressure 注入点。警告曾被误归因了本属 backpressure 的罪责。

「**不把动态内容写进被缓存的序列**」这条教训（源自 69.1）依然成立且值得守，但它不适用于尾部追加静态内容——判断新改动时请落到「写入位置是否在已缓存前缀之内」和「内容是否逐步变化」两个实际判据上，而非套用本例。

**已知缺口，如实记录**：删除注入后 **LLM 对循环检测完全零感知**。`selfSuspend` → `suspendProcess` 只写 `proc.SuspendReason` + emit `Suspend` 事件，全在内核观测侧；resume 路径唯一因 SuspendReason 改上下文的分支是 `cp.SuspendReason == "context_full"`（`kernel/resume.go:745`），`"loop_detected"` 不匹配。故进程是被**静默**挂起的，「LLM 会在 2N 步看到 LoopSuspend」不成立。这是本次取舍接受的代价，不是补偿渠道。运维侧可观测性由 `LoopDetected` 事件保留（dashboard timeline / strace 按 syscall 名泛化消费；payload `step`/`threshold` 视为兼容约定，但注意目前无代码读这两个字段，故改动无即时破坏面）。`LoopWarningMessage` 函数已随注入一并删除。

## Wiki Knowledge Base（跨项目研究资料）

Path: /mnt/disk0/project/note/claude-obsidian

当需要本项目代码之外的研究上下文时（尤其是 emergent 子系统：stem agents / skill synergy / immune system / feature profiles）：

1. 先读 `wiki/hot.md`（约 500 词最近上下文）
2. 不够再读 `wiki/index.md`
3. 涉及智能涌现设计决策时，读 `wiki/questions/Research - Emergent Intelligence for Agent OS.md`——包含 rnix 各子系统 ↔ 2025-2026 研究（DGM/SEAL/AlphaEvolve、bandit 技能选择、技能库终身学习、轨迹异常检测、多智能体涌现）的映射表与升级路径
4. 需要单篇细节再读 `wiki/sources/` 与 `wiki/concepts/` 下的对应页面

不要为一般编码问题或本项目已有文档能回答的问题读 wiki。
