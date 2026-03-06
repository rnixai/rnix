---
title: 'OpenAI 兼容基座 LLM 驱动'
slug: 'openai-compat-llm-driver'
created: '2026-03-06'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'net/http', 'net/http/httptest', 'encoding/json', 'bufio']
files_to_modify: ['drivers/llm/driver.go', 'drivers/llm/tools_test.go', 'drivers/llm/openai_compat.go (new)', 'drivers/llm/openai_compat_test.go (new)']
code_patterns: ['Adapter Pattern', 'Functional Options', 'Interface Segregation', 'SSE Streaming', 'httptest Mock Server']
test_patterns: ['httptest.NewServer mock', 'interface compliance (var _ LLMDriver = (*T)(nil))', 'errors.Is/As matching', 'SSE stream simulation']
---

# Tech-Spec: OpenAI 兼容基座 LLM 驱动

**Created:** 2026-03-06

## Overview

### Problem Statement

目前只有 Claude CLI 一个 LLM 驱动实现，无法验证 `LLMDriver` 和 `ToolCallingDriver` 接口抽象的通用性。需要第二个驱动来检验接口设计是否足够通用，发现问题就地修正。

### Solution

实现 OpenAI 兼容基座驱动（`OpenAICompatDriver`），使用纯 `net/http` 对接 `/v1/chat/completions` 端点。同时实现 `ToolCallingDriver` 扩展接口。一套代码可覆盖所有 OpenAI 兼容端点（Ollama、Groq、DeepSeek 等），只需配置 baseURL/apiKey。

**接口修正**：对抗性审查发现 `Message` 结构体缺少 `ToolCalls` 字段，无法承载 assistant 的工具调用消息，阻断多轮 tool calling 对话。本规格包含对 `driver.go` 的接口修正。

### Scope

**In Scope:**
- **接口修正**：`Message` 结构体新增 `ToolCalls []ToolCall` 字段（修复多轮 tool calling 断裂）
- `OpenAICompatDriver` 实现 `LLMDriver` 接口（Call + Stream + Info）
- `OpenAICompatDriver` 实现 `ToolCallingDriver` 接口（CallWithTools + StreamWithTools）
- 纯 `net/http` + SSE 解析，零新外部依赖
- `/v1/chat/completions` 端点请求/响应映射
- Intent → Messages 降级转换（Intent 非空时包装为 user message）
- LLMError 类型化错误（复用现有 sentinel errors）
- 单元测试（httptest mock server）
- 可配置 baseURL/apiKey/defaultModel/httpClient

**Out of Scope:**
- 多模态输入支持
- Kernel 侧集成和 VFS 注册
- 真实 API 端点测试
- OpenAI Responses API（仅用 Chat Completions）
- 重试/断路器逻辑

## Context for Development

### Codebase Patterns

- **接口隔离**：基础 `LLMDriver`（Call/Stream/Info）+ 扩展 `ToolCallingDriver`（CallWithTools/StreamWithTools），运行时通过类型断言检测
- **Functional Options**：`ClaudeCliOption` 模式已在使用，新驱动遵循同样模式
- **错误规范**：所有 LLM 错误包装为 `*LLMError`，携带 Provider/StatusCode/Err
- **流式设计**：`<-chan StreamEvent` + goroutine 消费，channel buffer 64
- **依赖方向**：`drivers/` 不导入 `kernel/`，不导入项目 `context/` 包
- **Stream 启动阶段错误**：遵循 ClaudeCliDriver 模式——`doHTTP`/连接失败等系统级错误返回裸 `fmt.Errorf`（不包装 `*LLMError`）；仅 HTTP 响应中的 API 错误和 goroutine 内部错误使用 `*LLMError`

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `drivers/llm/driver.go` | LLMDriver 接口、LLMRequest/LLMResponse/StreamEvent/Message/DriverInfo 类型定义——**需修改** |
| `drivers/llm/tools.go` | ToolCallingDriver 接口、ToolDef/ToolCall/ToolResult 类型 |
| `drivers/llm/tools_test.go` | Message JSON 兼容性测试——**需更新** |
| `drivers/llm/errors.go` | LLMError 类型、sentinel errors（ErrRateLimit/ErrAuth/ErrTimeout 等） |
| `drivers/llm/claude_cli.go` | 现有 ClaudeCliDriver 实现，参考 Functional Options 和 Stream goroutine 模式 |
| `drivers/llm/registry.go` | DriverRegistry，新驱动通过此注册 |
| `drivers/llm/vfsfile.go` | LLMFile VFS 适配，新驱动自动兼容 |

### Technical Decisions

- **Message.ToolCalls 字段**：`Message` 新增 `ToolCalls []ToolCall` 字段（`json:"tool_calls,omitempty"`），用于承载 assistant 角色的工具调用消息。这使得多轮 tool calling 对话可以将 assistant 的 tool_calls 消息塞回 `Messages` 给下一轮请求。ClaudeCliDriver 不受影响（忽略此字段）
- **Intent → Messages 降级**：如果 `LLMRequest.Messages` 非空则直接使用；否则将 `Intent` 包装为 `[{role: "user", content: intent}]`。`SystemPrompt` 处理策略：仅当 Messages 的第一条消息 **不是** `role: "system"` 时才前置 system message，避免重复注入
- **Provider 名称**：`Info().Provider` 返回构造时传入的 name（如 "ollama"、"groq"），不硬编码 "openai"
- **HTTP 超时**：复用 `LLMRequest.TimeoutMs`，通过 `context.WithTimeout` 控制。默认超时 5 分钟（与 ClaudeCliDriver 一致）
- **baseURL 规范化**：构造函数中 `strings.TrimRight(baseURL, "/")` 去除末尾斜杠，防止 URL 双斜杠
- **SSE 解析**：`bufio.Scanner` 逐行读取，通过 `scanner.Buffer(buf, 1024*1024)` 设置 1MB 最大行大小（防止大 tool arguments 超限）。匹配 `data: ` 前缀，`data: [DONE]` 标记结束。跳过空行和非 `data:` 行
- **Token 用量**：同步模式从 `response.usage` 提取；流式模式默认不发送 `stream_options`（兼容 Ollama 等），token 用量从最后一个含 `usage` 的 chunk 提取（OpenAI 在最后一个 chunk 自带 usage）。提供 `WithStreamUsage(bool)` Option 控制是否发送 `stream_options.include_usage: true`
- **ToolCalling 映射**：`ToolDef` → `{type: "function", function: {name, description, parameters}}`；响应 `tool_calls[].function.arguments`（JSON 字符串）→ `json.Unmarshal` → `ToolCall.Input`（`map[string]any`）
- **HTTP Client 可注入**：`CompatOption` 支持 `WithHTTPClient(*http.Client)`，测试用 httptest server 的 client
- **错误分类（双层匹配）**：先检查 HTTP status code（401/429/404），再检查 error body。对于 400 错误，同时匹配 `error.code` 和 `error.message` 中的 "context_length" 关键词，兼容不同端点的错误格式
- **resp.Body 生命周期**：Call 方法中 `defer resp.Body.Close()` 紧跟 `doHTTP` 成功返回；Stream 方法中 `defer resp.Body.Close()` 放在 goroutine 内部
- **Stream cancel 泄漏防护**：Stream 方法在 goroutine 启动前若 `doHTTP` 或 HTTP status 检查失败，必须显式调用 `cancel()` 后再返回 error（与 ClaudeCliDriver 模式一致）
- **Stream 启动阶段错误类型**：遵循 ClaudeCliDriver 先例——`doHTTP` 失败（DNS/连接等系统级错误）返回裸 `fmt.Errorf`；HTTP 非 2xx（API 级错误）返回 `*LLMError`
- **oaiMessage.name 字段**：包含可选 `name` 字段（`json:"name,omitempty"`），兼容 function calling 遗留格式

## Implementation Plan

### Tasks

- [x] Task 0: 修正 `Message` 结构体——新增 `ToolCalls` 字段
  - File: `drivers/llm/driver.go`
  - Action:
    - 在 `Message` 结构体中新增字段：`ToolCalls []ToolCall \`json:"tool_calls,omitempty"\``
    - 这使得 `role: "assistant"` 的消息可以携带工具调用信息，支持多轮 tool calling 对话
  - File: `drivers/llm/tools_test.go`
  - Action:
    - 更新 `TestMessage_JSONRoundTrip`：增加含 `ToolCalls` 字段的测试用例
    - 更新 `TestMessage_JSONCompatWithContextMessage`：验证新字段不破坏与 `context.Message` 的 JSON 兼容性（`context.Message` 无此字段，反序列化时自动忽略）
  - Notes: `ClaudeCliDriver` 不受影响——它不使用 `Messages` 字段。kernel 侧 `llmRequest` 暂不同步（独立 spec）

- [x] Task 1: 定义 `OpenAICompatDriver` 结构体、Options、内部 API 类型
  - File: `drivers/llm/openai_compat.go` (new)
  - Action:
    - 定义 `OpenAICompatDriver` 结构体：`baseURL string`、`apiKey string`、`name string`、`defaultModel string`、`defaultTimeout time.Duration`、`httpClient *http.Client`、`streamUsage bool`
    - 定义 `CompatOption func(*OpenAICompatDriver)` 及 Options：`WithCompatModel`、`WithCompatTimeout`、`WithHTTPClient`、`WithAPIKey`、`WithStreamUsage`
    - 构造函数 `NewOpenAICompatDriver(name, baseURL string, opts ...CompatOption) *OpenAICompatDriver`：内部 `strings.TrimRight(baseURL, "/")` 规范化 URL；默认 `httpClient = http.DefaultClient`；默认 `streamUsage = false`
    - `Info() DriverInfo` 方法
    - 定义内部 OpenAI 请求/响应类型（不导出）：
      - `oaiRequest`：`model`、`messages []oaiMessage`、`temperature *float64`、`max_tokens *int`（指针，`omitempty`，零值时省略）、`stream bool`、`stream_options *oaiStreamOptions`（指针，nil 时省略）、`tools []oaiTool`（`omitempty`）
      - `oaiMessage`：`role`、`content`、`name`（`omitempty`）、`tool_calls []oaiToolCall`（`omitempty`）、`tool_call_id`（`omitempty`）
      - `oaiResponse`：`choices []oaiChoice`、`usage oaiUsage`
      - `oaiChoice`：`index int`、`message oaiMessage`、`finish_reason string`
      - `oaiStreamChunk`：`choices []oaiStreamChoice`、`usage *oaiUsage`
      - `oaiStreamChoice`：`index int`、`delta oaiMessage`、`finish_reason *string`（指针，区分空字符串和 null）
      - `oaiStreamOptions`：`include_usage bool`
      - `oaiTool`：`type string`（固定 "function"）、`function oaiFunction`
      - `oaiFunction`：`name string`、`description string`（`omitempty`）、`parameters map[string]any`（`omitempty`）
      - `oaiToolCall`：`index int`（流式用）、`id string`、`type string`、`function oaiToolCallFunction`
      - `oaiToolCallFunction`：`name string`、`arguments string`
      - `oaiErrorResponse`：嵌套 `Error struct { Message string; Type string; Code string }`
    - 编译期接口检查：`var _ LLMDriver = (*OpenAICompatDriver)(nil)` 和 `var _ ToolCallingDriver = (*OpenAICompatDriver)(nil)`
  - Notes: 所有 oai 类型不导出，JSON tag 使用 snake_case。`max_tokens` 用 `*int` + `omitempty` 避免零值序列化为 `"max_tokens": 0`

- [x] Task 2: 实现请求构建辅助方法
  - File: `drivers/llm/openai_compat.go`
  - Depends on: Task 1
  - Action:
    - `buildMessages(req LLMRequest) []oaiMessage`：
      - 如果 `req.Messages` 非空，映射为 `oaiMessage` 数组。映射规则：
        - `Role` → `role`
        - `Content` → `content`
        - `ToolCallID` → `tool_call_id`（非空时）
        - `ToolCalls` → `tool_calls`（非空时，将 `ToolCall` 映射为 `oaiToolCall`，其中 `Input` 需 `json.Marshal` 为 string 赋给 `arguments`）
      - 否则将 `req.Intent` 包装为 `{role: "user", content: intent}`
      - `SystemPrompt` 处理：仅当最终消息列表的第一条 **不是** `role: "system"` 时，前置 `{role: "system", content: systemPrompt}`
    - `buildOAIRequest(req LLMRequest, stream bool, tools []ToolDef) oaiRequest`：
      - 构建完整 oaiRequest，映射 model/temperature/max_tokens
      - model 默认值取 `d.defaultModel`
      - `max_tokens`：`req.MaxTokens > 0` 时设置指针值，否则 nil
      - stream 为 true 且 `d.streamUsage` 为 true 时设置 `stream_options: {include_usage: true}`
      - tools 非空时映射 `ToolDef` → `oaiTool`
    - `doHTTP(ctx context.Context, body oaiRequest) (*http.Response, error)`：
      - JSON 序列化 body
      - POST 到 `d.baseURL + "/chat/completions"`
      - 设置 `Content-Type: application/json`
      - apiKey 非空时设置 `Authorization: Bearer {apiKey}`
      - 使用 `d.httpClient.Do(req)`
    - `classifyHTTPError(statusCode int, body []byte) *LLMError`：
      - 尝试解析 `oaiErrorResponse`
      - 401 → `NewLLMError(d.name, 401, ErrAuth)`
      - 429 → `NewLLMError(d.name, 429, ErrRateLimit)`
      - 404 → `NewLLMError(d.name, 404, ErrModelNotFound)`
      - 400：检查 `error.code` 或 `error.message` 是否含 "context_length"（`strings.Contains` + `strings.ToLower`），匹配则 `NewLLMError(d.name, 400, ErrContextLength)`
      - 其他 → `NewLLMError(d.name, status, fmt.Errorf("%s", errMsg))`
    - `parseToolCalls(oaiCalls []oaiToolCall) []ToolCall`：
      - 将 `oaiToolCall` 映射为 `ToolCall`
      - `function.arguments`（JSON 字符串）→ `json.Unmarshal` → `map[string]any`
      - 解析失败时 Input 设为 nil（不 panic）

- [x] Task 3: 实现 `Call` 方法（内部为 `callInternal`）
  - File: `drivers/llm/openai_compat.go`
  - Depends on: Task 2
  - Action:
    - 实现 `callInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error)`
    - 构建 timeout context：`ctx, cancel := context.WithTimeout(ctx, timeout)`，`defer cancel()`
    - 调用 `doHTTP` 发送非流式请求；失败时返回裸 `fmt.Errorf`（系统级错误）
    - 超时检查：`ctx.Err() == context.DeadlineExceeded` → `NewLLMError(d.name, 0, ErrTimeout)`
    - `defer resp.Body.Close()` 紧跟 doHTTP 成功
    - 读取 resp.Body，检查 HTTP status 非 2xx → `classifyHTTPError`
    - 成功时解析 `oaiResponse`：
      - `choices[0].message.content` → `LLMResponse.Content`
      - `usage` → `TokensUsed/InputTokens/OutputTokens`
      - `choices[0].message.tool_calls` → `parseToolCalls` → `LLMResponse.ToolCalls`
    - `Call(ctx, req)` 方法：调用 `callInternal(ctx, req, nil)`
  - Notes: Call/CallWithTools 共享 callInternal，区别仅在 tools 参数

- [x] Task 4: 实现 `Stream` 方法（内部为 `streamInternal`）
  - File: `drivers/llm/openai_compat.go`
  - Depends on: Task 2
  - Action:
    - 实现 `streamInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error)`
    - 构建 timeout context：`ctx, cancel := context.WithTimeout(ctx, timeout)`
    - 调用 `doHTTP` 发送流式请求（`stream: true`）
      - **doHTTP 失败**：`cancel()` 后返回裸 `fmt.Errorf`
      - **HTTP 非 2xx**：`resp.Body.Close()`、`cancel()`，通过 `classifyHTTPError` 返回 `*LLMError`
    - 创建 `ch := make(chan StreamEvent, streamChanBuffer)`
    - 启动 goroutine：
      - `defer close(ch)`、`defer resp.Body.Close()`、`defer cancel()`
      - 创建 scanner：`scanner := bufio.NewScanner(resp.Body)`，`scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)` 设置最大 1MB 行
      - **工具调用累积状态**：`pendingToolCalls map[int]*toolCallAccumulator`，每个 accumulator 包含 `id string`、`name strings.Builder`、`arguments strings.Builder`
      - 逐行处理：
        - 跳过空行、非 `data:` 前缀行
        - `data: [DONE]`：
          - 如果 pendingToolCalls 非空，先将所有累积的 tool calls 解析为 `[]ToolCall` 附到 done 事件
          - 发送 `StreamEvent{Type: "done", TokensUsed: ..., ToolCalls: ...}` 并 return
        - 否则 `json.Unmarshal` 为 `oaiStreamChunk`：
          - **content delta**：`choices[0].delta.content` 非空 → 发送 `StreamEvent{Type: "content", Content: ...}`
          - **tool_calls delta**：遍历 `choices[0].delta.tool_calls`，以 `index` 字段为 key 累积到 `pendingToolCalls`。首次出现时记录 `id` 和 `function.name`（可能分片），后续追加 `function.arguments` 片段
          - **finish_reason**：`finish_reason == "tool_calls"` 时与 `[DONE]` 相同——将累积的 tool calls 解析后发出
          - **usage**：记录 `chunk.usage` 到局部变量（覆盖式，最后一个 chunk 的 usage 为准）
          - **JSON 解析失败**：发送 `StreamEvent{Type: "error", Err: NewLLMError(d.name, 0, ...)}`，continue
      - scanner.Err() 非 nil：发送 error StreamEvent
    - `Stream(ctx, req)` 方法：调用 `streamInternal(ctx, req, nil)`

- [x] Task 5: 实现 `CallWithTools` 和 `StreamWithTools`
  - File: `drivers/llm/openai_compat.go`
  - Depends on: Task 3, Task 4
  - Action:
    - `CallWithTools(ctx, req, tools)`: 调用 `callInternal(ctx, req, tools)`
    - `StreamWithTools(ctx, req, tools)`: 调用 `streamInternal(ctx, req, tools)`

- [x] Task 6: 单元测试 — 接口合规 + Call 方法
  - File: `drivers/llm/openai_compat_test.go` (new)
  - Depends on: Task 3
  - Action:
    - 编译期接口检查：`var _ LLMDriver = (*OpenAICompatDriver)(nil)` 和 `var _ ToolCallingDriver = (*OpenAICompatDriver)(nil)`
    - `newTestDriver(handler)` 辅助函数：创建 `httptest.NewServer` + 返回配置好的 driver + cleanup func
    - `TestOpenAICompatDriver_Info`：验证 Name/Provider/DefaultModel
    - `TestOpenAICompatDriver_Call_Success`：mock 返回标准 oaiResponse，验证 Content/TokensUsed/InputTokens/OutputTokens
    - `TestOpenAICompatDriver_Call_WithMessages`：Messages 直接映射（不走 Intent 降级）
    - `TestOpenAICompatDriver_Call_IntentFallback`：Intent → user message 降级
    - `TestOpenAICompatDriver_Call_SystemPrompt`：SystemPrompt 注入为 system message
    - `TestOpenAICompatDriver_Call_SystemPromptNoDouble`：Messages 已含 system 消息时，SystemPrompt 不重复注入
    - `TestOpenAICompatDriver_Call_Timeout`：mock sleep + 短超时，`errors.Is(err, ErrTimeout)`
    - `TestOpenAICompatDriver_Call_AuthError`：401，`errors.Is(err, ErrAuth)` + `LLMError.StatusCode == 401`
    - `TestOpenAICompatDriver_Call_RateLimit`：429，`errors.Is(err, ErrRateLimit)`
    - `TestOpenAICompatDriver_Call_ModelNotFound`：404，`errors.Is(err, ErrModelNotFound)`
    - `TestOpenAICompatDriver_Call_ContextLength`：400 + error.code 含 context_length
    - `TestOpenAICompatDriver_Call_ContextLengthInMessage`：400 + error.message 含 context_length（code 字段缺失，兼容端点场景）
    - `TestOpenAICompatDriver_Call_NoAPIKey`：无 apiKey 时请求不含 Authorization header
    - `TestOpenAICompatDriver_Call_TrailingSlash`：baseURL 末尾有 `/` 时 URL 拼接正确
    - `TestOpenAICompatDriver_Call_ToolResultMessage`：Messages 含 `role: "tool"` + `tool_call_id` 的消息，验证正确映射到 OpenAI 格式
    - `TestOpenAICompatDriver_Call_AssistantToolCallsMessage`：Messages 含 `role: "assistant"` + `ToolCalls` 的消息，验证正确映射（ToolCall.Input → JSON string arguments）

- [x] Task 7: 单元测试 — Stream 方法 + ToolCalling
  - File: `drivers/llm/openai_compat_test.go`
  - Depends on: Task 4, Task 5
  - Action:
    - `TestOpenAICompatDriver_Stream_Success`：mock 发送多个 SSE chunks + `[DONE]`，验证 content 事件 + done 事件 + token 用量
    - `TestOpenAICompatDriver_Stream_HTTPError`：mock 返回 401，验证 Stream 直接返回 `*LLMError`
    - `TestOpenAICompatDriver_Stream_SystemError`：mock 连接拒绝，验证 Stream 返回裸 error（非 `*LLMError`）
    - `TestOpenAICompatDriver_Stream_InvalidSSE`：mock 发送无效 JSON 行，验证 error StreamEvent
    - `TestOpenAICompatDriver_Stream_LargeChunk`：mock 发送超过 64KB 的单行 JSON，验证 scanner 不报 token too long
    - `TestOpenAICompatDriver_CallWithTools_Success`：mock 返回含 tool_calls 的响应，验证 `LLMResponse.ToolCalls`（ID/Name/Input）
    - `TestOpenAICompatDriver_CallWithTools_ArgumentsParsing`：`function.arguments` JSON 字符串正确反序列化为 `map[string]any`
    - `TestOpenAICompatDriver_StreamWithTools_SingleTool`：mock 分多个 chunk 发送单个 tool_call 的 arguments，验证累积拼接正确
    - `TestOpenAICompatDriver_StreamWithTools_MultiTool`：mock 交错发送多个 tool_calls（不同 index），验证 index 分辨 + 独立累积 + 最终结果正确
    - `TestOpenAICompatDriver_StreamWithTools_FinishReason`：mock 在 `finish_reason: "tool_calls"` 的 chunk 触发 ToolCalls 发出（早于 `[DONE]`）

### Acceptance Criteria

- [x] AC 1: Given `Message{Role: "assistant", ToolCalls: [...]}` 序列化为 JSON，when 反序列化，then ToolCalls 字段完整保留
- [x] AC 2: Given `Message` 新增 ToolCalls 字段后，when 运行所有现有 `drivers/llm/` 测试，then 全部通过（零回归）
- [x] AC 3: Given `OpenAICompatDriver` 实例，when 调用 `Call(ctx, LLMRequest{Intent: "hello"})` 对接 mock server，then 返回 `LLMResponse` 含正确 Content/TokensUsed/InputTokens/OutputTokens
- [x] AC 4: Given `LLMRequest` 仅含 `Intent`（Messages 为空），when 驱动构建 HTTP 请求，then 请求体 messages 包含 `{role: "user", content: intent}`
- [x] AC 5: Given `LLMRequest` 含 `Messages`（首条为 system）和 `SystemPrompt`，when 驱动构建请求，then 不重复注入 system message
- [x] AC 6: Given mock server 返回 401/429/404/400(context_length)，when 调用 `Call`，then 分别返回 ErrAuth/ErrRateLimit/ErrModelNotFound/ErrContextLength，且 LLMError 携带正确 StatusCode 和 Provider
- [x] AC 7: Given 请求超时，when 调用 `Call`，then `errors.Is(err, ErrTimeout)` 为 true
- [x] AC 8: Given mock server 发送 SSE 流（多个 `data:` 行 + `data: [DONE]`），when 调用 `Stream`，then channel 接收到 content 事件和 done 事件
- [x] AC 9: Given `CallWithTools` 对接 mock server 返回含 `tool_calls` 的响应，then `LLMResponse.ToolCalls` 含正确的 ID/Name/Input（Input 为 `map[string]any`）
- [x] AC 10: Given `StreamWithTools` 对接 mock server 分多个 chunk 交错发送多个 tool_calls（不同 index），then 按 index 独立累积，最终 ToolCalls 完整正确
- [x] AC 11: Given `OpenAICompatDriver` 构造时未传 apiKey，when 发送请求，then HTTP 请求不含 `Authorization` header
- [x] AC 12: Given `LLMRequest.Messages` 包含 `role: "tool"` + `ToolCallID` 的消息，when 驱动构建请求，then 正确映射为 OpenAI tool result message 格式
- [x] AC 13: Given `LLMRequest.Messages` 包含 `role: "assistant"` + `ToolCalls` 的消息，when 驱动构建请求，then `ToolCall.Input` 被 `json.Marshal` 为 `function.arguments` 字符串
- [x] AC 14: Given baseURL 末尾有 `/`，when 发送请求，then URL 不含双斜杠
- [x] AC 15: Given 400 错误且 `error.code` 为空但 `error.message` 含 "context_length"，when 调用 `Call`，then `errors.Is(err, ErrContextLength)` 为 true
- [x] AC 16: Given SSE 流中单行 JSON 超过 64KB，when Stream 解析，then 正常处理不报 token too long
- [x] AC 17: Given `doHTTP` 失败（DNS/连接错误），when 调用 `Stream`，then 返回裸 error（非 `*LLMError`）且不泄漏 context
- [x] AC 18: Given 所有现有测试，when 运行 `make test`，then 全部通过（零回归）
- [x] AC 19: Given 编译期检查 `var _ LLMDriver = (*OpenAICompatDriver)(nil)` 和 `var _ ToolCallingDriver = (*OpenAICompatDriver)(nil)`，when 编译，then 通过

## Additional Context

### Dependencies

- 无新外部依赖
- 仅使用标准库：`net/http`、`net/http/httptest`、`encoding/json`、`bufio`、`context`、`fmt`、`strings`、`time`、`io`
- 复用现有 `drivers/llm/` 包内类型（LLMError、sentinel errors、StreamEvent、ToolCall 等）

### Testing Strategy

**单元测试（httptest mock server）：**
- 每个测试创建 `httptest.NewServer`，handler 根据测试场景返回不同的响应
- SSE 流式测试：handler 写入 `data: {...}\n\n` 格式的 SSE 行，最后 `data: [DONE]\n\n`
- 错误测试：handler 返回对应 HTTP status code + OpenAI 格式的 error JSON
- 超时测试：handler 内 `time.Sleep` + driver 配置短超时
- 大行测试：handler 写入超过 64KB 的单行 JSON，验证 scanner buffer 设置生效
- 工具调用累积测试：handler 发送交错的 tool_calls delta chunks，验证 index 分辨和拼接

**回归验证：**
- `make test` 全量通过（含 `-race`）
- `make lint` 通过

### Notes

- **接口验证结论（修正）**：对抗性审查发现 `Message` 缺少 `ToolCalls` 字段是一个真实的接口缺陷。修复后，接口设计足够支持 OpenAI 兼容驱动的所有功能
- **流式工具调用是最复杂的部分**：OpenAI 分多个 chunk 发送同一个 tool_call 的 `function.arguments`，且多个 tool_calls 可以交错（通过 `index` 字段区分）。实现使用 `map[int]*toolCallAccumulator` 按 index 独立累积
- **OpenAI `function.arguments` 是 JSON 字符串**：与 `ToolCall.Input`（`map[string]any`）之间需要双向转换——响应解析时 `json.Unmarshal`，请求构建时 `json.Marshal`
- **MaxTurns 被忽略**：OpenAI Chat Completions 没有多轮自动循环概念
- **`stream_options` 兼容性**：默认不发送（`streamUsage = false`），避免 Ollama 等不支持此字段的端点报错。通过 `WithStreamUsage(true)` 显式启用
- **后续扩展点**：此驱动可通过配置不同 baseURL 接入 Ollama (`http://localhost:11434/v1`)、Groq (`https://api.groq.com/openai/v1`)、DeepSeek (`https://api.deepseek.com/v1`) 等

### 对抗性审查修复追踪

| Finding | 修复方式 |
|---|---|
| F1 Message 缺 ToolCalls | Task 0: 新增字段 + 测试更新 |
| F2 流式 tool call 累积不足 | Task 4: 展开 pendingToolCalls + index + accumulator 细节 |
| F3 max_tokens 零值 | Task 1: `*int` + `omitempty` |
| F4 resp.Body.Close() | Task 3: 明确 `defer resp.Body.Close()` |
| F5 cancel 泄漏 | Task 4: doHTTP 失败时显式 cancel() |
| F6 stream_options 兼容 | 默认 false + WithStreamUsage Option |
| F7 SystemPrompt 重复 | Task 2: 仅当首条非 system 时注入 |
| F8 错误分类模糊 | Task 2: classifyHTTPError 双层匹配 code+message |
| F9 Scanner 行大小 | Task 4: scanner.Buffer 1MB |
| F10 200 中途出错 | Task 4: goroutine 内 JSON 解析失败发 error event |
| F11 Stream 错误类型 | Technical Decisions: 遵循 ClaudeCliDriver 先例 |
| F12 测试缺 tool result | Task 6: 新增 ToolResultMessage + AssistantToolCallsMessage 测试 |
| F13 oaiMessage.name | Task 1: 包含 name 字段 |
| F14 trailing slash | Task 1: 构造函数 TrimRight |
| F15 registry 注册 | 确认为 Out of Scope（噪声） |

## Review Notes

- 对抗性代码审查已完成
- 发现: 15 项，9 项已修复，6 项跳过（noise/设计决策）
- 解决方式: 自动修复
- 关键修复: flushToolCalls 非连续 index 丢失（F7 High）、buildMessages 错误传播（F2）、classifyHTTPError 保留错误消息（F1）、io.LimitReader 防 OOM（F5）、http.DefaultClient 隔离（F4）
