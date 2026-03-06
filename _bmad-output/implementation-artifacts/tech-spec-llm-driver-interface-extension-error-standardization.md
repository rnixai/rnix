---
title: 'LLM 驱动接口扩展与错误规范化'
slug: 'llm-driver-interface-extension-error-standardization'
created: '2026-03-06'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'drivers/llm package', 'internal/types (参考)', 'context/ (JSON 兼容)']
files_to_modify: ['drivers/llm/driver.go', 'drivers/llm/claude_cli.go', 'drivers/llm/claude_cli_test.go', 'drivers/llm/errors.go (new)', 'drivers/llm/tools.go (new)', 'drivers/llm/errors_test.go (new)', 'drivers/llm/tools_test.go (new)']
code_patterns: ['Interface Segregation', 'Sentinel Errors', 'Functional Options', 'JSON VFS bridge (kernel↔driver)']
test_patterns: ['errors.Is/As matching', 'interface compliance check (var _ ToolCallingDriver = (*T)(nil))', 'TestHelperProcess mock CLI pattern', 'mockDriver interface mock']
---

# Tech-Spec: LLM 驱动接口扩展与错误规范化

**Created:** 2026-03-06

## Overview

### Problem Statement

当前 LLM 驱动层缺少多轮对话支持（Messages）、生成参数（Temperature/MaxTokens）、类型化错误处理（全用 `fmt.Errorf`）、以及工具调用接口定义——阻塞多提供商集成的后续工作。

### Solution

在 `drivers/llm/` 包内扩展 `LLMRequest`、定义 `LLMError` + sentinel errors、定义 `ToolCallingDriver` 扩展接口及配套类型（`ToolDef`/`ToolCall`/`ToolResult`），`ClaudeCliDriver` 错误处理升级为类型化错误、忽略不支持的新字段。

### Scope

**In Scope:**
- `LLMRequest` 新增 `Messages`/`Temperature`/`MaxTokens`
- `LLMError` 类型 + LLM sentinel errors（`ErrRateLimit`/`ErrAuth`/`ErrContextLength`/`ErrModelNotFound`）
- `ToolCallingDriver` 接口 + `ToolDef`/`ToolCall`/`ToolResult` 类型
- `ClaudeCliDriver` 错误处理升级
- 现有测试全部通过

**Out of Scope:**
- SDK 直连驱动实现（Anthropic/OpenAI/Gemini）
- `ToolCallingDriver` 的具体实现
- `MultimodalDriver`/`ReasoningDriver`
- OpenAI 兼容基座驱动
- kernel 侧 `llmRequest`/`llmResponse` 同步扩展（需独立 spec）

## Context for Development

### Codebase Patterns

- **接口隔离**：基础 `LLMDriver` 保持不变，扩展能力通过独立接口声明，运行时通过类型断言检测
- **错误规范**：项目已有 `types.DriverError`（`internal/types/types.go`）和 `kernel.SyscallError`，LLM 层定义独立的 `LLMError` 类型
- **Sentinel errors**：使用 `errors.New` 定义，通过 `errors.Is` 匹配
- **Functional Options**：`ClaudeCliOption` 模式已在使用
- **依赖方向**：`drivers/` 不导入 `kernel/`、不导入项目 `context/` 包
- **JSON VFS 桥接**：kernel 有独立的 `llmRequest`/`llmResponse` 类型，与 `llm.LLMRequest`/`llm.LLMResponse` 通过 JSON tag 兼容。kernel 已包含 `Messages` 字段，driver 侧需补齐
- **ipc.StreamEvent ≠ llm.StreamEvent**：完全独立类型，互不影响

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `drivers/llm/driver.go` | 现有 LLMDriver 接口、LLMRequest/LLMResponse/StreamEvent 类型 |
| `drivers/llm/claude_cli.go` | ClaudeCliDriver 实现，需升级错误处理（当前全用 `fmt.Errorf`） |
| `drivers/llm/claude_cli_test.go` | 测试用 `strings.Contains` 匹配错误消息，需更新为 `errors.Is`/`errors.As` |
| `drivers/llm/registry.go` | DriverRegistry，无需修改 |
| `drivers/llm/vfsfile.go` | LLMFile VFS 适配，无需修改 |
| `drivers/llm/vfsfile_test.go` | `mockDriver` 实现 LLMDriver 接口，接口签名不变故无需修改 |
| `internal/types/types.go` | 现有 DriverError/ErrCode 定义，参考但不修改 |
| `kernel/kernel.go:62-78` | kernel 侧 `llmRequest`/`llmResponse` 镜像类型（已有 Messages 字段，缺 Temperature/MaxTokens/ToolCalls） |
| `context/context.go:26-30` | `Message{Role, Content, ToolCallID}` — JSON 兼容参考 |

### Technical Decisions

- **LLMError 独立于 types.DriverError**：LLM 层需要 `Provider`/`StatusCode` 字段，与通用 DriverError 职责不同
- **Sentinel errors 放 `drivers/llm/` 包**：LLM 特有错误不属于通用类型层
- **`llm.ErrTimeout` vs `types.ErrTimeout` 命名**：前者是 `error` 接口值（用于 `errors.Is`），后者是 `ErrCode` 字符串常量（用于 `SyscallError.Code`）。不同类型、不同包、不同用途，不构成冲突
- **ClaudeCliDriver 忽略不支持的新字段**：`Messages`/`Temperature`/`MaxTokens` 在 CLI 模式下不映射到参数
- **`llm.Message` 包含 `ToolCallID` 字段**：与 `context.Message` 完整 JSON 兼容（含三个字段：`Role`/`Content`/`ToolCallID`）。`Role` 有意使用 `string` 而非 `context.Role` 类型——driver 层不导入项目 `context/` 包，JSON 层面类型兼容即可
- **ToolCallingDriver 连同配套类型一起落地**：接口 + `ToolDef`/`ToolCall`/`ToolResult` 同步定义
- **测试升级**：`claude_cli_test.go` 从 `strings.Contains` 改为 `errors.Is`/`errors.As` 匹配
- **`Stream()` 直接返回的 error 不包装 LLMError**：`StdoutPipe()`/`cmd.Start()` 失败是系统级错误（非 LLM 特有），保持 `fmt.Errorf` 包装。仅 goroutine 内部通过 `StreamEvent.Err` 返回的错误使用 `LLMError`

## Implementation Plan

### Tasks

- [ ] Task 1: 创建 `drivers/llm/errors.go` — LLMError 类型与 sentinel errors
  - File: `drivers/llm/errors.go` (new)
  - Depends on: 无
  - Action:
    - 定义 sentinel errors：
      ```go
      var (
          ErrRateLimit     = errors.New("llm: rate limit exceeded")
          ErrAuth          = errors.New("llm: authentication failed")
          ErrContextLength = errors.New("llm: context length exceeded")
          ErrModelNotFound = errors.New("llm: model not found")
          ErrTimeout       = errors.New("llm: request timed out")
      )
      ```
    - 定义 `LLMError` 结构体：
      ```go
      type LLMError struct {
          Provider   string // "claude", "openai", "gemini", "ollama"
          StatusCode int    // HTTP status code (0 if not applicable)
          Err        error  // underlying sentinel or raw error
      }
      ```
    - `Error()` 格式定义：`"llm [provider] (status N): underlying message"`，status 为 0 时省略 `(status N)` 部分。示例：
      - `"llm [claude] (status 429): llm: rate limit exceeded"`
      - `"llm [claude]: response truncated: no result (possible max_turns limit)"`
    - 实现 `Unwrap() error` 方法（返回 `Err` 字段）
    - 提供 `NewLLMError(provider string, statusCode int, err error) *LLMError` 构造函数

- [ ] Task 2: 扩展 `drivers/llm/driver.go` — LLMRequest 新增字段 + Message 类型
  - File: `drivers/llm/driver.go`
  - Depends on: 无
  - Action:
    - 新增 `Message` 类型（与 `context.Message` 完整 JSON 兼容）：
      ```go
      type Message struct {
          Role       string `json:"role"`           // "system", "user", "assistant", "tool"
          Content    string `json:"content"`
          ToolCallID string `json:"tool_call_id,omitempty"`
      }
      ```
    - 在 `LLMRequest` 中新增三个字段：
      ```go
      Messages    []Message `json:"messages,omitempty"`
      Temperature *float64  `json:"temperature,omitempty"`
      MaxTokens   int       `json:"max_tokens,omitempty"`
      ```
  - Notes: `Temperature` 用 `*float64` 指针区分"未设置"和"设为 0"。JSON tag 与 kernel 侧 `llmRequest.Messages` 兼容

- [ ] Task 3: 创建 `drivers/llm/tools.go` — ToolCallingDriver 接口与配套类型
  - File: `drivers/llm/tools.go` (new)
  - Depends on: Task 2（ToolCallingDriver 嵌入 LLMDriver，使用 LLMRequest/LLMResponse/StreamEvent）
  - Action:
    - 导入标准库 `"context"`（注意：不是项目的 `context/` 包）
    - 定义工具描述类型：
      ```go
      type ToolDef struct {
          Name        string         `json:"name"`
          Description string         `json:"description,omitempty"`
          Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
      }
      ```
    - 定义工具调用类型：
      ```go
      type ToolCall struct {
          ID    string         `json:"id"`
          Name  string         `json:"name"`
          Input map[string]any `json:"input,omitempty"`
      }
      ```
    - 定义工具结果类型：
      ```go
      type ToolResult struct {
          ToolCallID string `json:"tool_call_id"`
          Content    string `json:"content"`
          IsError    bool   `json:"is_error,omitempty"`
      }
      ```
    - 定义 `ToolCallingDriver` 扩展接口：
      ```go
      type ToolCallingDriver interface {
          LLMDriver
          CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error)
          StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error)
      }
      ```
    - 在 `driver.go` 的 `LLMResponse` 中新增 `ToolCalls []ToolCall` 字段（`json:"tool_calls,omitempty"`）
    - 在 `driver.go` 的 `StreamEvent` 中新增 `ToolCalls []ToolCall` 字段（`json:"tool_calls,omitempty"`）

- [ ] Task 4: 升级 `drivers/llm/claude_cli.go` — 错误处理改用 LLMError
  - File: `drivers/llm/claude_cli.go`
  - Depends on: Task 1（需要 LLMError 和 sentinel errors）
  - Action:
    - **`Call` 方法**中所有 `fmt.Errorf` 替换为 `NewLLMError`：
      - 超时（L109）：`NewLLMError("claude", 0, ErrTimeout)`
      - `is_error` + "rate limit" 关键词：`NewLLMError("claude", 429, ErrRateLimit)`
      - `is_error` + "auth"/"key" 关键词：`NewLLMError("claude", 401, ErrAuth)`
      - `is_error` + "too long"/"context" 关键词：`NewLLMError("claude", 400, ErrContextLength)`
      - 其他 `is_error`（L122-125）：`NewLLMError("claude", 0, fmt.Errorf(errMsg))`
      - 空 result（L128）：`NewLLMError("claude", 0, fmt.Errorf("response truncated: no result (possible max_turns limit)"))`
      - CLI 执行失败/无 JSON（L139）：`NewLLMError("claude", 0, fmt.Errorf("cli failed (exit %d): %s", exitCode, stderr))`
      - JSON 解析失败（L142）：`NewLLMError("claude", 0, fmt.Errorf("invalid json in stdout"))`
    - **`Stream` goroutine 内部**的 3 个 `StreamEvent.Err` 错误点逐一包装：
      - JSON 解析失败（L203）：`NewLLMError("claude", 0, fmt.Errorf("failed to parse stream event: %w", err))`
      - `isError` 结果（L228）：使用同 Call 方法的关键词分类逻辑
      - `scanner.Err()`（L243）：`NewLLMError("claude", 0, fmt.Errorf("stream read error: %w", err))`
    - **`Stream` 方法直接返回的 error**（`StdoutPipe`/`cmd.Start` 失败，L177-183）：**不包装 LLMError**，保持 `fmt.Errorf`（系统级错误）
    - `buildArgs` 方法不变（新字段 Messages/Temperature/MaxTokens 被忽略）
    - 关键词分类逻辑提取为内部辅助函数 `classifyCliError(errMsg string) (error, int)` 返回 `(sentinel, statusCode)`
  - Notes: 关键词检测用 `strings.Contains` + `strings.ToLower`，保持简单。不完美覆盖所有可能的 CLI 错误消息，后续 SDK 驱动会有精确的 status code

- [ ] Task 5: 更新 `drivers/llm/claude_cli_test.go` — 测试改用 errors.Is/As
  - File: `drivers/llm/claude_cli_test.go`
  - Depends on: Task 4
  - Action:
    - 添加 `"errors"` 导入
    - `TestClaudeCliDriver_Call_Timeout`：改用 `errors.Is(err, ErrTimeout)` + `errors.As(err, &llmErr)` 验证 `llmErr.Provider == "claude"`
    - `TestClaudeCliDriver_Call_ExitCodeWithJSON`：改用 `errors.Is(err, ErrRateLimit)` + `errors.As` 验证 `llmErr.StatusCode == 429`（mock 输出 "API rate limited" 匹配 rate limit 关键词）
    - `TestClaudeCliDriver_Call_IsError`：改用 `errors.As` 验证 `LLMError` 类型 + `llmErr.Provider == "claude"`（"LLM error message" 不匹配任何 sentinel，走 "其他 is_error" 路径）
    - `TestClaudeCliDriver_Call_EmptyResult`：改用 `errors.As` 验证 `LLMError` 类型
    - `TestClaudeCliDriver_Call_CLIError`：改用 `errors.As` 验证 `LLMError` 类型
    - `TestClaudeCliDriver_Call_InvalidJSON`：改用 `errors.As` 验证 `LLMError` 类型
    - `TestClaudeCliDriver_Call_IsErrorEmptyResult`：改用 `errors.As` 验证 `LLMError` 类型
    - 流式测试（`Stream_Error`、`Stream_EmptyResult`、`Stream_IsErrorEmptyResult`）同样更新 `StreamEvent.Err` 的断言为 `errors.As`
    - 错误消息内容检查统一用 `llmErr.Error()` 匹配（新格式 `"llm [claude]..."` 替代旧格式）

- [ ] Task 6: 创建 `drivers/llm/errors_test.go` — LLMError 单元测试
  - File: `drivers/llm/errors_test.go` (new)
  - Depends on: Task 1
  - Action:
    - `TestLLMError_Error`：验证格式化输出符合定义格式（含 provider，status 非零时含 status code）
    - `TestLLMError_Error_ZeroStatus`：验证 StatusCode 为 0 时省略 `(status N)`
    - `TestLLMError_Unwrap`：验证 `errors.Unwrap` 返回内部 error
    - `TestLLMError_Is_SentinelErrors`：验证 `errors.Is(NewLLMError("claude", 429, ErrRateLimit), ErrRateLimit)` 为 true
    - `TestLLMError_As`：验证 `errors.As(err, &llmErr)` 提取 Provider/StatusCode
    - `TestSentinelErrors_Distinct`：验证 5 个 sentinel error 互不相等

- [ ] Task 7: 创建 `drivers/llm/tools_test.go` — Tool 类型与 Message 单元测试
  - File: `drivers/llm/tools_test.go` (new)
  - Depends on: Task 2, Task 3
  - Action:
    - `TestToolDef_JSONRoundTrip`：验证 ToolDef 的 JSON 序列化/反序列化
    - `TestToolCall_JSONRoundTrip`：验证 ToolCall 的 JSON 序列化/反序列化
    - `TestToolResult_JSONRoundTrip`：验证 ToolResult 的 JSON 序列化/反序列化
    - `TestToolCallingDriver_InterfaceEmbedding`：验证 `ToolCallingDriver` 嵌入 `LLMDriver`（编译期检查）
    - `TestMessage_JSONRoundTrip`：验证 `llm.Message` 的 JSON 序列化/反序列化（含 ToolCallID）
    - `TestMessage_JSONCompatWithContextMessage`：验证 `llm.Message` 与 `context.Message` 的 JSON 互操作性（同一 JSON 字符串可被两种类型反序列化，字段值一致）
    - `TestStreamEvent_ToolCalls_JSONRoundTrip`：验证 `StreamEvent.ToolCalls` 的 JSON 序列化/反序列化

### Acceptance Criteria

- [ ] AC 1: Given `LLMRequest` 包含 `Messages`/`Temperature`/`MaxTokens` 字段，when JSON 序列化后反序列化，then 所有字段值保持一致，`omitempty` 在零值时不输出
- [ ] AC 2: Given `LLMError` 包装了 `ErrRateLimit`，when 调用 `errors.Is(err, ErrRateLimit)`，then 返回 true
- [ ] AC 3: Given `LLMError` 实例，when 调用 `errors.As(err, &llmErr)`，then 可提取 `Provider`、`StatusCode` 字段
- [ ] AC 4: Given `ClaudeCliDriver.Call` 超时，when 检查返回的 error，then `errors.Is(err, ErrTimeout)` 返回 true 且 `errors.As` 提取的 Provider 为 `"claude"`
- [ ] AC 5: Given `ClaudeCliDriver.Call` 收到 `is_error: true` 且包含 "rate limit" 关键词的 CLI 响应，when 检查返回的 error，then `errors.Is(err, ErrRateLimit)` 为 true 且 `LLMError.StatusCode == 429`
- [ ] AC 6: Given `ClaudeCliDriver.Stream` goroutine 内部收到错误事件，when 检查 `StreamEvent.Err`，then 该错误为 `*LLMError` 类型且 Provider 为 `"claude"`
- [ ] AC 7: Given `LLMRequest` 包含 `Messages` 和 `Temperature` 字段，when 传给 `ClaudeCliDriver.Call`，then 这些字段被忽略，CLI 参数不包含相关 flag，调用正常完成
- [ ] AC 8: Given `ToolCallingDriver` 接口定义，when 某类型同时实现 `LLMDriver` + `CallWithTools` + `StreamWithTools`，then 该类型满足 `ToolCallingDriver` 接口（编译通过）
- [ ] AC 9: Given `ToolDef`/`ToolCall`/`ToolResult` 类型，when JSON 序列化/反序列化，then 字段名与研究报告中的跨提供商统一格式一致
- [ ] AC 10: Given 所有现有测试，when 运行 `make test`，then 全部通过（零回归）
- [ ] AC 11: Given `llm.Message{Role: "tool", Content: "result", ToolCallID: "call_123"}` 序列化为 JSON，when 用 `context.Message` 反序列化，then Role/Content/ToolCallID 值一致
- [ ] AC 12: Given `ClaudeCliDriver.Stream()` 的 `StdoutPipe`/`cmd.Start` 失败，when 检查直接返回的 error，then 该错误**不是** `*LLMError` 类型（系统级错误保持 `fmt.Errorf`）

## Additional Context

### Dependencies

- 无新外部依赖
- 仅涉及 `drivers/llm/` 包内部变更
- kernel 侧 `llmRequest` 已包含 `Messages` 字段，但缺少 `Temperature`/`MaxTokens`——这两个字段目前无法通过 VFS 桥接端到端传递，需后续 spec 同步 kernel 侧

### Testing Strategy

**单元测试（新增）：**
- `errors_test.go`：LLMError 的 Error（含格式验证）/Unwrap/Is/As 行为，sentinel errors 互斥性
- `tools_test.go`：Tool 类型 JSON 往返，ToolCallingDriver 接口嵌入编译检查，Message JSON 往返及与 context.Message 互操作性，StreamEvent.ToolCalls JSON 往返

**单元测试（修改）：**
- `claude_cli_test.go`：所有错误断言从 `strings.Contains` 迁移到 `errors.Is`/`errors.As`，错误消息内容检查改用 `llmErr.Error()` 匹配新格式

**回归验证：**
- `make test` 全量通过（含 `-race`）
- `make lint` 通过

### Notes

- **错误分类基于 CLI 输出的文本匹配**，不完美但足够。后续 SDK 驱动将使用精确的 HTTP status code
- **`ErrModelNotFound` 为预留 sentinel error**，本阶段 ClaudeCliDriver 不使用。预留给后续 SDK 驱动（如 OpenAI 返回 404 model not found）
- **`ToolCallingDriver` 本阶段无实现者**，仅用于接口定义和编译期类型检查
- **`LLMResponse.ToolCalls` 和 `StreamEvent.ToolCalls` 字段为 SDK 驱动预留**，ClaudeCliDriver 不会填充。kernel 侧 `llmResponse` 当前没有 `ToolCalls` 字段——端到端的 Tool Calling 链路需要后续 spec 同步扩展 kernel 侧
- **`Temperature`/`MaxTokens` 端到端断裂**：driver 侧新增了这两个字段，但 kernel 的 `llmRequest` 尚未包含。当前仅在 driver 层面定义，端到端透传需后续 spec 同步 kernel 侧 `llmRequest`
- **`ToolDef.Parameters` 使用 `map[string]any`**：JSON Schema 是嵌套 JSON 结构，`map[string]any` 在 `encoding/json` 中可递归反序列化任意嵌套层级。牺牲编译期类型安全换取 schema 表达灵活性，当前阶段可接受
- 研究报告参考：Section 6.2（推荐接口扩展方案）、Section 6.4（类型化错误规范）
