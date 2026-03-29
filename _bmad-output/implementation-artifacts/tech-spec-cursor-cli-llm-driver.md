---
title: 'Cursor CLI LLM 驱动集成'
slug: 'cursor-cli-llm-driver'
created: '2026-03-11'
status: 'implementation-complete'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'Cursor CLI (agent --print)', 'exec.CommandContext', 'NDJSON stream']
files_to_modify: ['drivers/llm/cursor_cli.go (NEW)', 'drivers/llm/cursor_cli_test.go (NEW)', 'kernel/kernel.go', 'cmd/rnix/main.go']
code_patterns: ['LLMDriver interface (Call/Stream/Info)', 'CommandBuilder mock pattern', 'FileFactory VFS bridge', 'TestHelperProcess subprocess mock', 'CursorOption func pattern (独立命名)']
test_patterns: ['独立 TestCursorHelperProcess + GO_TEST_CASE dispatch', 'cursorMockCmdBuilder for CLI simulation', 't.Parallel() on all tests', 'errors.Is/As for LLMError validation']
---

# Tech-Spec: Cursor CLI LLM 驱动集成

**Created:** 2026-03-11

## Overview

### Problem Statement

Rnix 当前只有 Claude CLI 作为 LLM 后端（`/dev/llm/claude`），无法使用 Cursor CLI 提供的模型能力。同时 agent.yaml 缺少 provider 选择机制，驱动选择硬编码在 daemon 启动时。

### Solution

新增 `CursorCliDriver` 实现 `LLMDriver` 接口，通过 `agent --print` 子进程调用 Cursor CLI，注册为 `/dev/llm/cursor` VFS 设备。扩展 kernel Spawn 路径解析逻辑，根据 agent manifest 的 `provider` 字段动态选择 LLM 设备。

### Scope

**In Scope:**
- `CursorCliDriver` 实现（Call / Stream / Info）
- VFS 设备注册 `/dev/llm/cursor`
- agent.yaml `provider` 选择机制（含白名单校验）
- 认证检测（`CURSOR_API_KEY` 环境变量检查）
- 模型名透传到 `--model` 参数
- 无头模式必要标志（`--trust`、`--force`、`--approve-mcps`）

**Out of Scope:**
- ACP JSON-RPC 长连接集成（后续迭代）
- Cloud Agent 模式（`-c`）
- Cursor 特有功能（sandbox 配置、权限管理等）
- Provider 间 fallback 容错（当 cursor 不可用时回退 claude）— 有意推迟，当前 cursor 不可用直接返回错误
- `--sandbox` 模式控制 — Cursor CLI 默认行为足够，后续按需增加

## Context for Development

### Codebase Patterns

- **LLMDriver 接口**：`Call(ctx, req) (*LLMResponse, error)` + `Stream(ctx, req) (<-chan StreamEvent, error)` + `Info() DriverInfo`
- **CommandBuilder 注入**：`type CommandBuilder func(ctx, name, args...) *exec.Cmd` 用于测试 mock
- **Option 函数模式**：ClaudeCliDriver 使用 `WithModel()` 等。CursorCliDriver **必须使用独立命名** `CursorWithModel()` 等避免同包冲突
- **VFS 桥接**：`FileFactory(driver, basePath)` 返回 `VFSFileFactory`，写入 JSON 请求 → 读取 JSON 响应
- **错误分类**：`classifyCliError()` 将 CLI 输出映射为 sentinel error + HTTP status code。CursorCliDriver 复用此函数。
- **设备路径硬编码**：Spawn 方法中 `k.vfs.Open(proc.PID, "/dev/llm/claude", vfs.O_RDWR)` 写死路径，需改为动态解析
- **Provider 字段已存在**：`AgentModels.Provider` 在 `agents/types.go` 已定义但从未参与设备路径选择
- **错误输出硬编码**：`cmd/rnix/main.go` 的 `outputError()` 调用中也硬编码了 `/dev/llm/claude` 和 Claude 专属提示文案

### Cursor CLI 与 Claude CLI 关键差异

| 特性 | Claude CLI | Cursor CLI | 影响 |
| ---- | ---------- | ---------- | ---- |
| 命令名 | `claude` | `agent` | cmdBuilder 第一参数 |
| Prompt 传递 | `-p <prompt>` (参数值) | `--print <prompt>` (位置参数放最后) | **buildArgs 参数序列完全不同** |
| System prompt | `--system-prompt` | 无 | 需前缀拼接到 prompt |
| Max turns | `--max-turns N` | 无 | 静默忽略 |
| Temperature | 无 CLI 参数 | 无 CLI 参数 | 均静默忽略 |
| 无头必要标志 | 无 | `--trust --force --approve-mcps` | 必须传递 |
| stream-json 事件 | `assistant`, `result` | `system`, `assistant`, `tool_call`, `result` | 需处理额外事件类型 |

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `drivers/llm/driver.go` | LLMDriver 接口、LLMRequest/Response/StreamEvent 类型定义（含 Temperature/MaxTokens/Messages 字段） |
| `drivers/llm/claude_cli.go` | ClaudeCliDriver 实现 — 新驱动的参考模板 |
| `drivers/llm/claude_cli_test.go` | TestHelperProcess mock 模式 — 测试模板 |
| `drivers/llm/vfsfile.go` | FileFactory + LLMFile write-then-read 桥接 |
| `drivers/llm/errors.go` | LLMError 类型、sentinel errors、NewLLMError |
| `kernel/kernel.go` Spawn 方法 | **关键锚点** — 搜索 `k.vfs.Open(proc.PID, "/dev/llm/claude"`，该路径出现 3 处（Open 调用、emitEvent args、NewSyscallError device） |
| `kernel/kernel.go` SpawnOpts | SpawnOpts 结构体定义 |
| `agents/types.go` AgentModels | Provider 字段已有但未接线 |
| `cmd/rnix/main.go` runDaemon() | Daemon 中注册 ClaudeCliDriver |
| `cmd/rnix/main.go` outputError() | 搜索 `outputError(renderer, mode, "/dev/llm/claude"` — 两处调用，含 Claude 专属提示文案 |
| `.meta/cursorcli/headless.md` | Cursor CLI 无头模式参考（stream-json 事件结构） |
| `.meta/cursorcli/reference/parameters.md` | Cursor CLI 全部参数（`--print` 是布尔开关，prompt 是位置参数） |

### Technical Decisions

- **子进程调用**：通过 `agent --print` 子进程调用。**注意 Cursor CLI 与 Claude CLI 的参数语义不同**：Claude CLI 用 `claude -p <prompt>`（`-p` 后跟 prompt 值），Cursor CLI 用 `agent --print ... <prompt>`（`--print` 是布尔开关，prompt 是最后的位置参数）。
- **系统提示词**：Cursor CLI 无 `--system-prompt` 参数。方案为将 system prompt 拼接到 prompt 前缀，格式 `"[System Instructions]\n{systemPrompt}\n[End System Instructions]\n\n{intent}"`。不使用 XML 标签，使用明确的纯文本分隔符。
- **无头模式必要标志**：`buildArgs()` 必须包含 `--trust`（避免工作区信任弹窗阻塞）、`--force`（允许工具执行）、`--approve-mcps`（避免 MCP 批准弹窗）。这三个标志是无头模式正常运行的前提。
- **认证检测**：仅检查 `CURSOR_API_KEY` 环境变量是否存在。不调用 `agent status`。环境变量缺失时通过 `log.Printf("[warn] ...")` 输出日志（不引入回调接口，保持与项目现有日志模式一致）。实际认证失败在 Call/Stream 时由 CLI 错误分类处理。
- **模型透传**：当 `req.Model != ""` 时传递 `--model {model}`。当 `req.Model == ""` 且默认模型为空时**省略 `--model` 参数**（让 Cursor CLI 使用其内部默认模型）。
- **忽略的 LLMRequest 字段**：`MaxTurns`、`Temperature`、`MaxTokens`、`Messages` 在 CursorCliDriver 中均**静默忽略**。这与 ClaudeCliDriver 的行为一致（Claude CLI 的 `buildArgs()` 同样不处理 Temperature/MaxTokens/Messages）。内核通过 reasonStep 循环自管理推理步骤。
- **Provider 解析与校验**：kernel Spawn 时根据 `agent.AgentModels.Provider` 解析设备路径。`resolveLLMDevice()` 使用**白名单校验**：仅允许 `""`、`"claude"`、`"cursor"` 三个值，其他值返回错误（防止 `"../fs"` 路径遍历）。
- **错误分类**：复用现有 `classifyCliError()` 函数（其 `strings.Contains` 匹配已覆盖 `"rate limit"`、`"auth"`/`"key"`、`"too long"`/`"context"`）。仅需确保 `NewLLMError` 调用 provider 参数传 `"cursor"` 而非 `"claude"`。不新建独立分类函数。
- **流式格式**：Cursor CLI stream-json 事件类型包括 `system`（init）、`assistant`（内容）、`tool_call`（started/completed）、`result`（完成）。CursorCliDriver 需定义独立的 `cursorStreamEvent` 结构体，Stream 方法处理所有四种事件类型（`system` 和 `tool_call` 安全跳过，只提取 `assistant` 和 `result`）。
- **JSON 响应格式**：实现前需先执行 spike 验证实际 JSON schema。Task 1/2 中标注 `[SPIKE]` 的字段/事件结构为假设值，须以 Task 0 结果替换。
- **Option 函数命名**：`CursorWith` 前缀：`CursorWithModel()`、`CursorWithTimeout()`、`CursorWithCommandBuilder()`
- **stderr 捕获**：与 ClaudeCliDriver 一致，`Call()` 捕获 stderr，非零退出码时用于错误信息。正常退出时忽略 stderr。

## Implementation Plan

### Tasks

- [ ] Task 0: Spike — 验证 Cursor CLI JSON 响应 schema (手工步骤，待验证)
  - Action: 手动执行以下命令并记录输出结构
  - Notes:
    - 同步调用：`agent --print --output-format json --trust --force --approve-mcps "say hello"`
    - 流式调用：`agent --print --output-format stream-json --trust --force --approve-mcps "say hello"`
    - 确认字段：`[SPIKE]` 标记的所有假设（`result`、`is_error`、`input_tokens`、`output_tokens` 等）
    - 确认 `--trust` 和 `--approve-mcps` 是否消除所有交互式弹窗
    - 确认 prompt 作为位置参数的正确位置（最后一个参数）
    - 将结果记录为 `_bmad-output/implementation-artifacts/spike-cursor-cli-json-schema.md`

- [x] Task 1: 新建 `drivers/llm/cursor_cli.go` — CursorCliDriver 核心实现
  - File: `drivers/llm/cursor_cli.go` (NEW)
  - Action: 实现 `CursorCliDriver` 结构体及 `LLMDriver` 接口
  - Notes:
    - 参照 `claude_cli.go` 的结构，复用 `CommandBuilder` 类型
    - **Option 函数独立命名**：`CursorWithModel()`、`CursorWithTimeout()`、`CursorWithCommand()`、`CursorWithCommandBuilder()`，类型为 `type CursorCliOption func(*CursorCliDriver)`
    - 常量：`CursorDefaultTimeout = 5 * time.Minute`。默认模型为空字符串（省略 `--model` 参数）
    - 默认 CLI 命令：`"agent"`（Cursor CLI 的官方命令名，可通过 `CursorWithCommand()` 或配置文件 `command` 字段覆盖）
    - **`buildArgs(req, outputFormat)` 参数序列**（Cursor CLI 的 `--print` 是布尔开关，prompt 是位置参数放最后）：
      ```
      ["--print", "--output-format", format, "--force", "--trust", "--approve-mcps"]
      + 可选 ["--model", model]  // 仅当 model != ""
      + [prompt]                 // prompt 作为最后的位置参数
      ```
    - **系统提示词处理**：当 `req.SystemPrompt != ""` 时，构造 prompt 为 `"[System Instructions]\n{systemPrompt}\n[End System Instructions]\n\n{intent}"`；否则 prompt = intent
    - **静默忽略的字段**：`req.MaxTurns`、`req.Temperature`、`req.MaxTokens`、`req.Messages`（与 ClaudeCliDriver 行为一致）
    - `Call()`: 执行 `agent --print ... <prompt>`，捕获 stdout + stderr，解析 JSON 响应。`cursorCliResponse` 结构体字段 `[SPIKE]` 根据 Task 0 结果定义
    - `Stream()`: 执行 `agent --print ... <prompt>`，逐行解析 NDJSON。定义独立 `cursorStreamEvent` 结构体 `[SPIKE]`。处理 4 种事件：`system`（跳过）、`assistant`（提取 content）、`tool_call`（跳过）、`result`（提取最终内容和 token）
    - `Info()`: 返回 `DriverInfo{Name: "cursor-cli", Provider: "cursor", DefaultModel: d.defaultModel}`
    - **错误分类**：复用现有 `classifyCliError()` 函数，`NewLLMError` 的 provider 参数固定为 `"cursor"`
    - **认证检测**：`NewCursorCliDriver()` 构造时检查 `os.Getenv("CURSOR_API_KEY")`，不存在时 `log.Printf("[warn] CURSOR_API_KEY not set; cursor driver may fail at runtime")`

- [x] Task 2: 新建 `drivers/llm/cursor_cli_test.go` — 完整测试套件
  - File: `drivers/llm/cursor_cli_test.go` (NEW)
  - Action: 实现 **独立的** `TestCursorHelperProcess` mock 和全部测试用例
  - Notes:
    - **独立 mock 入口**：新建 `TestCursorHelperProcess`，配套 `cursorMockCmdBuilder()` 函数
    - `GO_TEST_CASE` 值：`cursor_success`、`cursor_is_error`、`cursor_cli_error`、`cursor_invalid_json`、`cursor_timeout`、`cursor_args_echo`、`cursor_stream_success`、`cursor_stream_error`、`cursor_empty_result`、`cursor_not_authenticated`、`cursor_stream_timeout`
    - mock 数据中标注 `[SPIKE]` 的字段需在 Task 0 完成后调整
    - 测试覆盖：
      - `TestCursorCliDriver_Call_Success` — 正常 JSON 响应
      - `TestCursorCliDriver_Call_Timeout` — 超时 → `ErrTimeout`
      - `TestCursorCliDriver_Call_CLIError` — 非零退出码 + stderr 内容包含在错误中
      - `TestCursorCliDriver_Call_IsError` — JSON `is_error=true`
      - `TestCursorCliDriver_Call_InvalidJSON` — 无效 JSON
      - `TestCursorCliDriver_Call_Args` — 验证参数序列：`--print` 在前、prompt 在最后、含 `--force --trust --approve-mcps`、验证系统提示词拼接格式
      - `TestCursorCliDriver_Call_DefaultArgs` — 默认参数无 `--model`、无 `--max-turns`、prompt 在最后
      - `TestCursorCliDriver_Call_EmptyResult` — 空结果
      - `TestCursorCliDriver_Call_NotAuthenticated` — 认证失败 → `ErrAuth`
      - `TestCursorCliDriver_Stream_Success` — 含 `system`/`assistant`/`tool_call`/`result` 四种事件，验证仅提取 `assistant` 和 `result`
      - `TestCursorCliDriver_Stream_Error` — 流式错误
      - `TestCursorCliDriver_Stream_Timeout` — 流式超时 → channel 关闭、无 goroutine 泄漏
      - `TestCursorCliDriver_Info` — DriverInfo 返回值
      - `TestCursorCliDriver_Options` — `CursorWithModel`/`CursorWithTimeout`/`CursorWithCommandBuilder`

- [x] Task 3: kernel Spawn 中 provider 动态路径解析
  - File: `kernel/kernel.go`
  - Action: 修改 Spawn() 方法中的硬编码 LLM 设备路径
  - Notes:
    - 搜索 `"/dev/llm/claude"` 在 Spawn 方法中出现的 **3 处**位置（均在 `if !opts.SkipReasonLoop` 块内）：
      1. `k.vfs.Open(proc.PID, "/dev/llm/claude", vfs.O_RDWR)` — Open 调用
      2. `"path": "/dev/llm/claude"` — emitEvent 的 args map
      3. `NewSyscallError("Spawn", proc.PID, "/dev/llm/claude", ...)` — 错误构造
    - 新增辅助函数 `resolveLLMDevice(agent *agents.AgentInfo) (string, error)`:
      - **白名单校验**：仅允许 `""`、`"claude"`、`"cursor"` 三个值
      - 若 provider 不在白名单中 → 返回 `"", fmt.Errorf("unsupported LLM provider: %q", provider)`
      - 若 `agent != nil && provider != ""` → 返回 `/dev/llm/{provider}`, nil
      - 否则返回 `/dev/llm/claude`, nil
    - **调用位置**：在 `if !opts.SkipReasonLoop` 块内、`k.vfs.Open` 调用前调用 `llmDevice, err := resolveLLMDevice(agent)`（SkipReasonLoop=true 时不需要 LLM 设备）
    - 将上述 3 处硬编码全部替换为 `llmDevice` 变量
    - 确保向后兼容：provider 为空或 `"claude"` 时行为不变

- [x] Task 4: Daemon 注册 + 客户端错误输出修正
  - File: `cmd/rnix/main.go`
  - Action: 在 `runDaemon()` 中注册 CursorCliDriver，并修正客户端侧硬编码
  - Notes:
    - 在 `runDaemon()` 中 `claudeDriver` 注册后添加:
      ```go
      cursorDriver := llm.NewCursorCliDriver()
      _ = devReg.Register("/dev/llm/cursor", llm.FileFactory(cursorDriver, "/dev/llm/cursor"))
      ```
    - **客户端错误输出修正**：搜索 `outputError(renderer, mode, "/dev/llm/claude"` 找到两处调用。
      - 设备路径参数：改为 `"/dev/llm"`（通用前缀，不绑定特定 provider）
      - 提示文案：`"检查 Claude Code CLI 是否已安装"` → `"检查 LLM CLI 是否已安装（claude 或 agent）"`

### Acceptance Criteria

- [ ] AC 1: Given Cursor CLI 已安装且认证通过, When 发送 LLMRequest 到 CursorCliDriver.Call(), Then 返回包含 content 和 token 信息的 LLMResponse
- [ ] AC 2: Given CursorCliDriver 配置了自定义 model, When 执行 Call(), Then CLI 参数中包含 `--model {model}`
- [ ] AC 3: Given LLMRequest.Model 为空且默认模型为空, When 执行 Call(), Then CLI 参数中**不包含** `--model` 参数
- [ ] AC 4: Given LLMRequest 包含 SystemPrompt, When 执行 Call(), Then prompt 文本前缀包含 `[System Instructions]...[End System Instructions]` 包裹的 system prompt
- [ ] AC 5: Given Cursor CLI 返回错误 JSON (is_error=true), When 解析响应, Then 返回 LLMError 且 Provider 为 "cursor"
- [ ] AC 6: Given Cursor CLI 超时, When context deadline exceeded, Then 返回 errors.Is(err, ErrTimeout) == true
- [ ] AC 7: Given CursorCliDriver.Stream() 调用, When Cursor CLI 输出 stream-json 事件（含 system/assistant/tool_call/result）, Then channel 仅发出从 assistant 和 result 提取的 content/done 事件
- [ ] AC 8: Given agent.yaml 中 models.provider = "cursor", When kernel Spawn 该 agent, Then 进程打开 `/dev/llm/cursor` 而非 `/dev/llm/claude`
- [ ] AC 9: Given agent.yaml 中 models.provider 为空, When kernel Spawn 该 agent, Then 默认打开 `/dev/llm/claude`（向后兼容）
- [ ] AC 10: Given agent.yaml 中 models.provider = "nonexistent", When kernel Spawn, Then 返回 SyscallError 含 "unsupported LLM provider"
- [ ] AC 11: Given agent.yaml 中 models.provider = "../fs", When kernel Spawn, Then 返回 SyscallError（白名单拒绝）
- [ ] AC 12: Given daemon 启动, When runDaemon() 执行, Then `/dev/llm/cursor` 设备已注册且可 Open
- [ ] AC 13: Given `CURSOR_API_KEY` 未设置, When 创建 CursorCliDriver, Then 仅输出 warn 日志，不阻塞
- [ ] AC 14: Given CursorCliDriver.Call() 执行, When 构建 CLI 参数, Then 参数以 `--print` 开头、prompt 在最后、含 `--trust --force --approve-mcps`
- [ ] AC 15: Given LLMRequest.MaxTurns > 0, When CursorCliDriver 构建参数, Then 参数列表中**不包含** `--max-turns`
- [ ] AC 16: Given CursorCliDriver.Stream() 调用, When context 超时, Then channel 正确关闭，无 goroutine 泄漏

## Additional Context

### Dependencies

- Cursor CLI (`agent` 命令) 需已安装在 `$PATH` 中（默认 `~/.local/bin/agent`）
- 认证：`CURSOR_API_KEY` 环境变量或 `agent login` 已完成
- 无新 Go 依赖包引入 — 全部使用标准库 + 现有项目依赖

### Testing Strategy

**单元测试** (`drivers/llm/cursor_cli_test.go`):
- `TestCursorHelperProcess` — 独立子进程 mock 入口
- `TestCursorCliDriver_Call_Success` — 正常调用返回正确 content 和 token
- `TestCursorCliDriver_Call_Timeout` — 超时返回 ErrTimeout
- `TestCursorCliDriver_Call_CLIError` — CLI 非零退出码 + stderr 内容
- `TestCursorCliDriver_Call_IsError` — JSON 中 is_error=true
- `TestCursorCliDriver_Call_InvalidJSON` — 非法 JSON 返回
- `TestCursorCliDriver_Call_Args` — CLI 参数序列验证（`--print` 在前、prompt 在最后、含三标志、system prompt 拼接）
- `TestCursorCliDriver_Call_DefaultArgs` — 默认参数（无 `--model`、prompt 在最后）
- `TestCursorCliDriver_Call_EmptyResult` — 空结果处理
- `TestCursorCliDriver_Call_NotAuthenticated` — 认证失败分类
- `TestCursorCliDriver_Stream_Success` — 流式事件（含 system/tool_call 跳过验证）
- `TestCursorCliDriver_Stream_Error` — 流式错误事件
- `TestCursorCliDriver_Stream_Timeout` — 流式超时 + channel 关闭 + goroutine 清理
- `TestCursorCliDriver_Info` — DriverInfo 返回值
- `TestCursorCliDriver_Options` — CursorWithModel/CursorWithTimeout/CursorWithCommandBuilder

**Kernel 层测试**:
- `TestResolveLLMDevice` 正面用例 — `""` → `/dev/llm/claude`、`"cursor"` → `/dev/llm/cursor`、`"claude"` → `/dev/llm/claude`
- `TestResolveLLMDevice` 负面用例 — `"nonexistent"` → error、`"../fs"` → error、`"claude/../../shell"` → error
- 现有 kernel 测试无需修改 — `resolveLLMDevice()` 在 `agent == nil` 时返回 claude 路径

**手工验证**:
- 安装 Cursor CLI → `agent login` → 配置 agent.yaml `provider: cursor` → `rnix -i "hello"` 验证端到端

### Notes

- Cursor CLI 命令为 `agent`，安装路径 `~/.local/bin/agent`
- **关键差异**：Cursor CLI 的 `-p`/`--print` 是布尔开关（非交互模式），prompt 是最后的位置参数；Claude CLI 的 `-p` 后跟 prompt 值。`buildArgs()` 参数序列完全不同。
- 无头模式三要素：`--trust`（信任工作区）+ `--force`（允许工具执行）+ `--approve-mcps`（自动批准 MCP）
- stream-json 事件类型：`system`（init）、`assistant`（内容）、`tool_call`（started/completed）、`result`（完成）
- 静默忽略的 LLMRequest 字段：`MaxTurns`、`Temperature`、`MaxTokens`、`Messages`
- Provider 白名单：`""`、`"claude"`、`"cursor"`。新增 provider 时需同步更新白名单和 daemon 注册
- Task 1/2 中 `[SPIKE]` 标记的结构体字段和 mock 数据为假设值，须以 Task 0 spike 结果替换后方可实现
- 后续可扩展为 ACP JSON-RPC 模式以获得更精细的控制（会话管理、权限交互等）
