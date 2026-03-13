# Story 24.2: /v1/chat/completions 同步模式

Status: done

## Story

As a 外部工具用户,
I want 通过标准 OpenAI Chat Completion API 发起同步 LLM 请求,
So that 任何支持 OpenAI API 的工具可以通过 Rnix 网关调用已注册的 LLM provider。

## Acceptance Criteria

1. **Given** `/v1/chat/completions` POST 端点已实现
   **When** 发送 `{"model": "ollama:llama3", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
   **Then** 系统将 model 解析为 provider=`"ollama"`, model=`"llama3"`
   **And** 通过 `driverReg.Get("ollama")` 获取驱动实例
   **And** 将 `ChatMessage` 数组转换为 `LLMRequest`
   **And** 调用 `driver.Call(ctx, req)` 获取同步响应
   **And** 将 `LLMResponse` 转换为 `ChatCompletionResponse` 格式返回（FR148）

2. **Given** model 参数为仅 provider 名
   **When** 发送 `{"model": "cursor", "messages": [...]}`
   **Then** 使用该 provider 的 `default_model` 执行请求（FR151）

3. **Given** model 参数支持 provider:model 复合格式
   **When** 发送 `{"model": "cursor:claude-3.5-sonnet", "messages": [...]}`
   **Then** 使用指定 model 覆盖 provider 的默认模型（FR151）

4. **Given** 请求发送到不存在的 provider
   **When** 发送 `{"model": "nonexistent", "messages": [...]}`
   **Then** 返回 HTTP 404 + OpenAI 错误格式 + 可用 provider 列表提示

5. **Given** LLM 驱动内部错误
   **When** 驱动返回超时或 5xx 错误
   **Then** 返回 HTTP 504（超时）或 502（上游错误）+ OpenAI 错误格式

6. **Given** HTTP 请求处理开销测量
   **When** 正常请求（不含 LLM 推理本身）
   **Then** HTTP 处理开销 <= 50ms（NFR50）

## Tasks / Subtasks

### Task 1: 实现 handleChatCompletions 主处理函数（AC: #1, #2, #3）

- [x] 1.1 替换 `ipc/http_openai.go` 中 `handleChatCompletions` 的 501 stub 实现
- [x] 1.2 解析请求体 `json.NewDecoder(r.Body).Decode(&req)` 为 `ChatCompletionRequest`
- [x] 1.3 验证必填字段：`model` 非空、`messages` 非空
- [x] 1.4 调用已有 `parseModel(req.Model)` 获取 provider 和 model 名
- [x] 1.5 通过 `s.driverReg.Get(provider)` 查找驱动实例
- [x] 1.6 如 `stream: true` 则返回 501（Story 24.3 实现），本 Story 仅处理同步模式

### Task 2: 消息转换 ChatMessage -> llm.LLMRequest（AC: #1）

- [x] 2.1 实现 `toLLMRequest(req ChatCompletionRequest, modelOverride string) llm.LLMRequest` 转换函数
- [x] 2.2 将 `ChatMessage` 数组映射为 `llm.Message` 数组（role + content 直接对应）
- [x] 2.3 设置 `LLMRequest.Model`：如 `parseModel` 返回非空 model 则使用，否则留空让 driver 使用 `default_model`
- [x] 2.4 映射可选字段：`Temperature`、`MaxTokens`

### Task 3: 响应转换 llm.LLMResponse -> ChatCompletionResponse（AC: #1）

- [x] 3.1 实现 `toChatCompletionResponse(llmResp *llm.LLMResponse, model string) ChatCompletionResponse` 转换函数
- [x] 3.2 生成响应 ID：`fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())` 格式
- [x] 3.3 设置 `Object = "chat.completion"`、`Created = time.Now().Unix()`
- [x] 3.4 将 `LLMResponse.Content` 包装为 `ChatChoice{Index: 0, Message: ChatMessage{Role: "assistant", Content: resp.Content}, FinishReason: "stop"}`
- [x] 3.5 映射 token 用量：`LLMResponse.InputTokens` -> `PromptTokens`、`OutputTokens` -> `CompletionTokens`、`TokensUsed` -> `TotalTokens`

### Task 4: 错误处理和映射（AC: #4, #5）

- [x] 4.1 provider 不存在：HTTP 404 + `model_not_found` + 可用 provider 列表（使用 `s.driverReg.Names()`）
- [x] 4.2 请求体 JSON 解析失败：HTTP 400 + `invalid_request`
- [x] 4.3 必填字段缺失（model 为空、messages 为空）：HTTP 400 + `invalid_request`
- [x] 4.4 driver.Call 返回超时错误（`context.DeadlineExceeded`）：HTTP 504 + `timeout`
- [x] 4.5 driver.Call 返回其他错误：HTTP 502 + `upstream_error`

### Task 5: 单元测试（AC: #1-#6）

- [x] 5.1 测试成功同步请求：mock driver 返回固定响应，验证完整 `ChatCompletionResponse` 格式
- [x] 5.2 测试仅 provider 名（model="" → 使用 default_model）
- [x] 5.3 测试 provider:model 复合格式
- [x] 5.4 测试 provider 不存在 → 404 + 可用 provider 列表
- [x] 5.5 测试 JSON 解析失败 → 400
- [x] 5.6 测试 messages 为空 → 400
- [x] 5.7 测试 model 为空 → 400
- [x] 5.8 测试 driver.Call 超时 → 504
- [x] 5.9 测试 driver.Call 内部错误 → 502
- [x] 5.10 测试 stream:true → 501（留给 Story 24.3）
- [x] 5.11 测试 HTTP 处理开销（不含 LLM 推理）<= 50ms
- [x] 5.12 所有测试启用 `-race` 检测

## Dev Notes

### 核心设计决策

**1. 替换 handleChatCompletions stub，保持同一文件 `ipc/http_openai.go`。**
- Story 24.1 已创建该文件（195 行），本 Story 在其中扩展 `handleChatCompletions` 方法
- 新增 2 个转换函数：`toLLMRequest` 和 `toChatCompletionResponse`
- 预计净增 ~80-120 行代码

**2. 消息转换使用直接字段映射，不引入中间层。**
- `ChatMessage{Role, Content}` → `llm.Message{Role, Content}` — 字段名和语义完全一致
- 注意：`llm.Message` 有 `ToolCallID` 和 `ToolCalls` 字段，但 Story 24.2 不处理 tool_calls（超出范围）
- `ChatCompletionRequest.Temperature` 是 `*float64`（可选），`LLMRequest.Temperature` 也是 `*float64` — 直接赋值

**3. 响应 ID 使用简化的 timestamp-based 格式。**
- OpenAI 格式：`"chatcmpl-"` + 随机字符串
- 简化实现：`fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())`
- 无需 UUID 库，保持零额外依赖

**4. stream:true 请求直接返回 501 Not Implemented。**
- 流式模式在 Story 24.3 实现
- 本 Story 在 handleChatCompletions 开头检测 `req.Stream == true`，若是则返回 501
- 不返回 400（stream 是合法参数，只是尚未实现）

**5. 错误映射遵循架构文档中的错误响应表。**
- 复用 Story 24.1 已实现的 `writeError()` 辅助函数
- 超时判断：检查 `errors.Is(err, context.DeadlineExceeded)`
- 非超时错误统一映射为 502 `upstream_error`

### 关键类型映射参考

**请求转换：ChatCompletionRequest → LLMRequest**

| ChatCompletionRequest 字段 | LLMRequest 字段 | 映射规则 |
|---------------------------|----------------|---------|
| `Model` | — | 已由 `parseModel` 解析，model 部分传入 `LLMRequest.Model` |
| `Messages[].Role` | `Messages[].Role` | 直接映射 |
| `Messages[].Content` | `Messages[].Content` | 直接映射 |
| `Temperature` | `Temperature` | 直接赋值（均为 `*float64`） |
| `MaxTokens` | `MaxTokens` | 直接赋值 |
| `Stream` | — | 本 Story 中 stream=true 提前返回 501 |

**响应转换：LLMResponse → ChatCompletionResponse**

| LLMResponse 字段 | ChatCompletionResponse 字段 | 映射规则 |
|-----------------|---------------------------|---------|
| `Content` | `Choices[0].Message.Content` | 包装为单选项 |
| — | `Choices[0].Message.Role` | 固定 `"assistant"` |
| — | `Choices[0].FinishReason` | 固定 `"stop"` |
| `InputTokens` | `Usage.PromptTokens` | 直接映射 |
| `OutputTokens` | `Usage.CompletionTokens` | 直接映射 |
| `TokensUsed` | `Usage.TotalTokens` | 直接映射 |
| — | `ID` | `"chatcmpl-"` + timestamp |
| — | `Object` | 固定 `"chat.completion"` |
| — | `Created` | `time.Now().Unix()` |
| — | `Model` | 原始请求中的 model 字符串 |

### DriverRegistry API 速查

```go
// 获取驱动（Story 24.2 核心调用）
driver, ok := s.driverReg.Get("ollama")  // (LLMDriver, bool)

// 获取所有 provider 名（用于错误提示）
names := s.driverReg.Names()  // []string（已排序）

// 驱动信息（获取 default_model）
info := driver.Info()  // DriverInfo{Name, Provider, DefaultModel, DriverType}

// 同步调用（Story 24.2 核心）
resp, err := driver.Call(ctx, llm.LLMRequest{
    Model:    "llama3",
    Messages: []llm.Message{{Role: "user", Content: "hello"}},
})
// resp: &LLMResponse{Content, TokensUsed, InputTokens, OutputTokens}
```

### 已有代码中可直接复用的部分

| 已有代码 | 文件位置 | 复用方式 |
|---------|---------|---------|
| `parseModel()` | `ipc/http_openai.go:184-194` | 直接调用 |
| `writeError()` | `ipc/http_openai.go:165-175` | 直接调用 |
| `ChatCompletionRequest` 类型 | `ipc/http_openai.go:94-100` | 已定义 |
| `ChatCompletionResponse` 类型 | `ipc/http_openai.go:109-116` | 已定义 |
| `ChatChoice` 类型 | `ipc/http_openai.go:119-123` | 已定义 |
| `ChatMessage` 类型 | `ipc/http_openai.go:103-106` | 已定义 |
| `ChatUsage` 类型 | `ipc/http_openai.go:142-146` | 已定义 |
| `OpenAIErrorResponse` 类型 | `ipc/http_openai.go:153-155` | 已定义 |
| `stubDriver` 测试 mock | `ipc/http_openai_test.go:20-34` | 测试中复用 |
| `newTestRegistry()` | `ipc/http_openai_test.go:36-46` | 测试中复用 |

### 变更范围

| 文件 | 变更类型 | 估算行数 |
|------|---------|---------|
| `ipc/http_openai.go` | **修改** | +80~120 行（替换 stub + 新增转换函数） |
| `ipc/http_openai_test.go` | **修改** | +150~200 行（新增 ~12 个测试函数） |

**不修改的文件：**
- `drivers/llm/driver.go` — LLMDriver 接口不变
- `drivers/llm/registry.go` — DriverRegistry 不变
- `ipc/server.go` — Unix socket IPC server 不受影响
- `kernel/` — 内核不受影响（HTTP 绕过 Kernel）
- `cmd/rnix/` — serve 命令在 Story 24.5 实现

### 架构合规

- **依赖方向**：`ipc/` → `drivers/llm/`（Decision 13 允许，与 Story 24.1 一致）
- **不引入新依赖**：仅使用已有的 `context`、`errors`、`encoding/json`、`fmt`、`net/http`、`time` 标准库
- **线程安全**：`handleChatCompletions` 无可变状态，`DriverRegistry.Get()` 线程安全，`driver.Call()` 由各驱动保证线程安全
- **错误格式**：使用 OpenAI 标准错误格式（不使用项目内部的 `SyscallError`）
- **HTTP 绕过 Kernel**：直接调用 `DriverRegistry`，不创建 Rnix 进程，最小化 HTTP 开销（NFR50）

### 测试策略

- **Mock 驱动**：扩展已有 `stubDriver`，使 `Call()` 返回可控响应或错误
- **httptest.NewRecorder**：用于验证响应状态码、Content-Type、JSON body
- **Table-driven 测试**：错误场景使用 table-driven 格式覆盖所有错误码
- **性能测试**：使用 `testing.B` 或计时断言验证 HTTP 处理开销 <= 50ms
- **竞态检测**：所有测试启用 `-race`

### 前置 Story 智能

**来自 Story 24-1（OpenAI HTTP Server 核心框架）：**
- `OpenAIServer` 结构体和 `buildMux()` 已在 `ipc/http_openai.go` 完整实现
- 所有 7 个 OpenAI 兼容类型已定义（`ChatCompletionRequest`、`ChatMessage`、`ChatCompletionResponse`、`ChatChoice`、`ChatCompletionChunk`、`ChatChunkChoice`、`ChatUsage`）
- `parseModel()` 使用 `strings.SplitN(model, ":", 2)` 正确处理各种格式
- `writeError()` 辅助函数已实现，直接调用即可
- `handleChatCompletions` 当前为 501 stub（`http_openai.go:78-81`），需替换为完整实现
- Code Review 反馈：已添加 `ReadHeaderTimeout: 10 * time.Second` 防止 slowloris 攻击
- 测试 mock：`stubDriver` 和 `newTestRegistry()` 已在测试文件中定义

**来自 Epic 23（多 LLM Provider 管理）：**
- `DriverRegistry.Get(name)` 返回 `(LLMDriver, bool)` — 第二个返回值是是否找到
- `DriverRegistry.Names()` 返回已排序的 provider 名列表
- `driver.Info().DefaultModel` 获取默认模型名
- `driver.Call(ctx, LLMRequest)` 同步调用，返回 `(*LLMResponse, error)`
- `LLMRequest.Messages` 使用 `[]llm.Message`（有 `Role` 和 `Content` 字段）

### Git 智能

最近提交：
- `b33b865 feat: story 24-1` — 实现 OpenAI HTTP Server 核心框架
- `5bdc57f feat: add Epic 24 for LLM Serve Gateway and update related artifacts`

提交建议：`feat: implement Story 24.2 - /v1/chat/completions sync mode`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 | 说明 |
|------|------|------|
| SSE 流式响应（`stream: true`） | Story 24.3 | 本 Story 遇到 stream=true 返回 501 |
| `/v1/models` 端点 | Story 24.4 | handleListModels 保持 stub |
| `rnix serve` CLI 命令 | Story 24.5 | Cobra 命令和 daemon 集成 |
| `tool_calls` 支持 | 未规划 | `ChatMessage` 未含 tool_calls 字段 |
| 认证/API Key 验证 | 未规划 | 当前本地信任模型 |

### Project Structure Notes

- 仅修改 `ipc/http_openai.go` 和 `ipc/http_openai_test.go` — 不新建文件
- 与 Story 24.1 建立的文件结构完全一致
- 与架构文档 Epic 24 -> `ipc/` 包映射一致

### References

- [Source: ipc/http_openai.go] — Story 24.1 已实现的 OpenAIServer 核心框架、类型定义、parseModel、writeError
- [Source: ipc/http_openai_test.go] — Story 24.1 的 23 个测试函数、stubDriver mock、newTestRegistry
- [Source: drivers/llm/driver.go] — LLMDriver 接口、LLMRequest/LLMResponse/Message 类型定义
- [Source: drivers/llm/registry.go] — DriverRegistry.Get/Names/Len 方法
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision-13] — LLM Serve Gateway 架构决策
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#LLM-Serve-Gateway-模式] — 错误响应格式规范
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR148] — /v1/chat/completions 端点
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR151] — provider:model 复合格式路由
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR50] — HTTP 处理开销 <= 50ms
- FRs covered: FR148, FR151
- NFRs covered: NFR50

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

No issues encountered during implementation.

### Completion Notes List

- Replaced `handleChatCompletions` 501 stub with full sync mode implementation including request parsing, validation, model routing, driver invocation, and response formatting.
- Implemented `toLLMRequest()` conversion: maps ChatMessage[] to llm.Message[], passes Temperature and MaxTokens, sets model override from parseModel result.
- Implemented `toChatCompletionResponse()` conversion: generates chatcmpl-{timestamp} ID, wraps LLMResponse.Content as single ChatChoice with role=assistant and finish_reason=stop, maps token usage fields.
- Error handling: 400 for invalid JSON/empty model/empty messages, 404 for unknown provider with available provider list, 501 for stream:true, 504 for context.DeadlineExceeded, 502 for other driver errors.
- Added `configurableDriver` test helper allowing per-test control of Call behavior.
- 15 new test functions covering all ACs: sync success, provider-only model, compound provider:model format, 404/400/501/502/504 error scenarios, HTTP overhead <= 50ms (NFR50), message mapping, response format, optional field passthrough.
- Updated existing `TestHandleChatCompletions_Stub` to `TestHandleChatCompletions_Success` since endpoint is now fully implemented.
- Updated `TestOpenAIServer_RoutesRegistered` to expect 200 for /v1/chat/completions.
- All 21 test packages pass with race detection, lint 0 issues, build successful.

### File List

| File | Change Type |
|------|------------|
| `ipc/http_openai.go` | Modified (+65 lines: handleChatCompletions implementation, toLLMRequest, toChatCompletionResponse, imports) |
| `ipc/http_openai_test.go` | Modified (+350 lines: 15 new test functions, configurableDriver helper, updated 2 existing tests) |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | Modified (24-2 status: ready-for-dev -> in-progress -> review) |
| `_bmad-output/implementation-artifacts/24-2-chat-completions-sync-mode.md` | Modified (tasks marked complete, dev agent record, file list, status) |

### Change Log

- **2026-03-13**: Implemented Story 24.2 - /v1/chat/completions sync mode. Replaced stub with full implementation: request parsing, model routing (provider-only and provider:model compound format), ChatMessage-to-LLMRequest conversion, LLMResponse-to-ChatCompletionResponse conversion, comprehensive error handling (400/404/501/502/504). Added 15 new tests, all passing with race detection.
- **2026-03-13**: Code Review (AI) - Found 7 issues (2 HIGH, 3 MEDIUM, 2 LOW). Fixed: (H1) Added context.Canceled handling for client disconnect, (H2) Added http.MaxBytesReader 4MB body limit, (M1) Removed internal error detail leak from 502 responses, (M2) Added nil LLMResponse guard, (M3) Added 4 new tests for review fixes. All 21 test packages pass with race detection, 0 lint issues.

## Senior Developer Review (AI)

### Review Date: 2026-03-13
### Reviewer: Claude Opus 4.6

### Findings Summary

| Severity | Count | Fixed |
|----------|-------|-------|
| HIGH | 2 | 2 |
| MEDIUM | 3 | 3 |
| LOW | 2 | 0 (accepted) |

### HIGH Issues (Fixed)

1. **H1: Missing context.Canceled handling** - `handleChatCompletions` did not handle `context.Canceled` from client disconnect, incorrectly mapping it to 502 upstream_error. Fixed: added early return on `context.Canceled`. (`ipc/http_openai.go:125-128`)
2. **H2: No request body size limit** - `json.NewDecoder(r.Body).Decode()` accepted unlimited body sizes, enabling OOM attacks. Fixed: added `http.MaxBytesReader(w, r.Body, 4<<20)` to cap at 4MB. (`ipc/http_openai.go:83-85`)

### MEDIUM Issues (Fixed)

3. **M1: Error message leaks internal driver details** - 502 error response included `err.Error()` from driver, potentially exposing secrets. Fixed: changed to generic "LLM driver returned an error". (`ipc/http_openai.go:133-134`)
4. **M2: No nil LLMResponse guard** - A buggy driver returning `(nil, nil)` would cause panic. Fixed: added nil check before `toChatCompletionResponse`. (`ipc/http_openai.go:137-140`)
5. **M3: Missing tests for review fixes** - Added 4 new tests: oversized body, client disconnect, nil response, error leak prevention.

### LOW Issues (Accepted)

6. **L1: ToolCalls asymmetry** - `llm.Message` has `ToolCalls` field but `ChatMessage` does not. Acceptable: tool_calls is out of scope for Story 24.2.
7. **L2: Non-unique response IDs** - `time.Now().UnixNano()` could produce duplicate IDs under extreme concurrency. Acceptable: IDs are ephemeral.

### Outcome: APPROVED

