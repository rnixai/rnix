# Story 24.3: SSE 流式响应

Status: done

## Story

As a 外部工具用户,
I want 通过 `stream: true` 获取 SSE 流式 LLM 响应,
So that 支持实时流式输出的工具（如 Open WebUI）可以获得逐 token 的响应体验。

## Acceptance Criteria

1. **Given** `/v1/chat/completions` 端点的流式模式已实现
   **When** 发送 `{"model": "ollama:llama3", "messages": [...], "stream": true}`
   **Then** 响应 Content-Type 为 `text/event-stream`
   **And** 响应 Cache-Control 为 `no-cache`
   **And** 响应 Connection 为 `keep-alive`

2. **Given** LLM 驱动返回 StreamEvent 通道
   **When** 每个 StreamEvent 到达
   **Then** 转换为 `ChatCompletionChunk` 格式
   **And** 以 `data: {json}\n\n` 格式写入响应（FR150）
   **And** 每个 chunk 后立即 Flush

3. **Given** 流式输出完成
   **When** StreamEvent 通道关闭
   **Then** 写入 `data: [DONE]\n\n` 终止标记并 Flush

4. **Given** 客户端主动断开连接
   **When** HTTP 请求的 context 被取消
   **Then** 取消传播到 `driver.Stream(ctx, req)` 的 context
   **And** 驱动停止生成事件，资源正确释放

5. **Given** 流式模式下的错误处理
   **When** 驱动在流式过程中返回错误
   **Then** 写入包含错误信息的 SSE 事件后关闭连接

## Tasks / Subtasks

### Task 1: 替换 stream:true 的 501 stub 为流式处理逻辑（AC: #1, #2, #3）

- [x] 1.1 在 `handleChatCompletions` 中替换 `req.Stream == true` 的 501 返回，改为调用新方法 `handleStreamingResponse`
- [x] 1.2 实现 `handleStreamingResponse(w http.ResponseWriter, r *http.Request, driver llm.LLMDriver, llmReq llm.LLMRequest, model string)`
- [x] 1.3 设置 SSE 响应头：`Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive`
- [x] 1.4 使用 `w.(http.Flusher)` 断言获取 Flusher 接口；如果不支持则回退返回 500 错误
- [x] 1.5 调用 `driver.Stream(r.Context(), llmReq)` 获取 `<-chan StreamEvent`
- [x] 1.6 如果 `driver.Stream` 返回错误，写入 OpenAI 错误响应（未开始 SSE 前可正常返回 JSON 错误）

### Task 2: StreamEvent 到 ChatCompletionChunk 的转换和写入（AC: #2）

- [x] 2.1 实现 `toChatCompletionChunk(event llm.StreamEvent, model string, chunkID string) ChatCompletionChunk` 转换函数
- [x] 2.2 生成 chunkID：在流开始时生成一次 `fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())`，所有 chunk 共享同一 ID
- [x] 2.3 对于 `event.Type == "content"`：设置 `Delta.Content = event.Content`，`FinishReason = ""`
- [x] 2.4 对于 `event.Type == "done"`：设置 `Delta = {}` 空消息，`FinishReason = "stop"`，可选填充 Usage
- [x] 2.5 每个 chunk 写入格式：`fmt.Fprintf(w, "data: %s\n\n", jsonBytes)` 后调用 `flusher.Flush()`

### Task 3: 流终止和 [DONE] 标记（AC: #3）

- [x] 3.1 当 StreamEvent 通道关闭（range 结束）后，写入 `data: [DONE]\n\n`
- [x] 3.2 写入后调用 `flusher.Flush()` 确保客户端收到终止标记
- [x] 3.3 注意：如果最后一个 event 是 `done` 类型，先写入该 chunk（含 finish_reason=stop），再写 [DONE]

### Task 4: 客户端断开和 context 取消传播（AC: #4）

- [x] 4.1 在 `range eventCh` 循环中检查 `r.Context().Err() != nil`，若客户端已断开则退出循环
- [x] 4.2 `driver.Stream(r.Context(), ...)` 已经传入 request context，驱动层会响应取消
- [x] 4.3 确保退出循环后不再向 ResponseWriter 写入数据

### Task 5: 流式错误处理（AC: #5）

- [x] 5.1 对于 `event.Type == "error"` 或 `event.Err != nil`：写入一个包含错误信息的 SSE 事件
- [x] 5.2 错误 SSE 事件格式：`data: {"error": {"message": "...", "type": "server_error", "code": "stream_error"}}\n\n`
- [x] 5.3 写入错误事件后 Flush 并退出循环（不再写 [DONE]）

### Task 6: 单元测试（AC: #1-#5）

- [x] 6.1 测试流式成功：mock driver.Stream 返回多个 content event + done event，验证 SSE 格式和 Content-Type
- [x] 6.2 测试 SSE 响应头：验证 Content-Type=text/event-stream、Cache-Control=no-cache、Connection=keep-alive
- [x] 6.3 测试 chunk 格式：每行 `data: {...}\n\n`，JSON 含 id/object/created/model/choices
- [x] 6.4 测试 [DONE] 终止标记：最后一行为 `data: [DONE]\n\n`
- [x] 6.5 测试 chunk ID 一致性：同一流中所有 chunk 的 `id` 字段相同
- [x] 6.6 测试 finish_reason：content chunk 的 finish_reason 为空或 null，done chunk 的 finish_reason 为 "stop"
- [x] 6.7 测试 driver.Stream 返回错误：未开始 SSE 前返回正常 JSON 错误
- [x] 6.8 测试流中错误：driver 发送 error event，验证写入错误 SSE 事件
- [x] 6.9 测试客户端断开：使用可取消的 context，验证循环退出
- [x] 6.10 更新或移除 `TestChatCompletions_StreamTrue_501`（该测试已不适用）
- [x] 6.11 所有测试启用 `-race` 检测

## Dev Notes

### 核心设计决策

**1. 替换 `handleChatCompletions` 中 stream:true 的 501 返回为真正的流式处理。**
- Story 24.2 留下了 `if req.Stream { return 501 }` 的占位逻辑（`ipc/http_openai.go:107-111`）
- 本 Story 将该分支替换为调用 `handleStreamingResponse` 方法
- 请求解析、model 解析、driver 查找逻辑复用已有代码，流式分支仅在获取 driver 后分叉

**2. SSE 输出严格遵循 OpenAI 流式格式。**
- 每个 chunk 一行：`data: {json}\n\n`（注意双换行）
- 最后写入 `data: [DONE]\n\n` 终止标记
- `ChatCompletionChunk.Object` = `"chat.completion.chunk"`
- `ChatChunkChoice.Delta` 替代 `ChatChoice.Message`（delta 只含变化部分）
- 同一流的所有 chunk 共享相同的 `id`

**3. Flusher 接口是流式输出的关键。**
- `http.ResponseWriter` 需要断言为 `http.Flusher` 才能逐 chunk 推送
- `httptest.NewRecorder()` 实现了 `http.Flusher`，测试可用
- 如果 ResponseWriter 不支持 Flush（极端情况），返回 500 错误

**4. 错误处理分两种时机。**
- **流开始前**（`driver.Stream()` 返回错误）：可正常返回 JSON 错误响应（HTTP 头未发送）
- **流过程中**（event.Err 不为 nil）：HTTP 头已发送，只能通过 SSE 事件传递错误

### 关键类型和接口参考

**`LLMDriver.Stream` 方法签名：**
```go
Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
```

**`StreamEvent` 类型（`drivers/llm/driver.go:39-47`）：**
```go
type StreamEvent struct {
    Type         string     `json:"type"` // "content", "done", "error"
    Content      string     `json:"content,omitempty"`
    TokensUsed   int        `json:"tokens_used,omitempty"`
    InputTokens  int        `json:"input_tokens,omitempty"`
    OutputTokens int        `json:"output_tokens,omitempty"`
    ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
    Err          error      `json:"-"`
}
```

**`ChatCompletionChunk` 类型（`ipc/http_openai.go:199-205`）：**
```go
type ChatCompletionChunk struct {
    ID      string            `json:"id"`
    Object  string            `json:"object"`
    Created int64             `json:"created"`
    Model   string            `json:"model"`
    Choices []ChatChunkChoice `json:"choices"`
}
```

**`ChatChunkChoice` 类型（`ipc/http_openai.go:208-212`）：**
```go
type ChatChunkChoice struct {
    Index        int         `json:"index"`
    Delta        ChatMessage `json:"delta"`
    FinishReason string      `json:"finish_reason,omitempty"`
}
```

### DriverRegistry 和 Driver API 速查

```go
// 获取驱动（复用 Story 24.2 已有逻辑）
driver, ok := s.driverReg.Get(provider)  // (LLMDriver, bool)

// 流式调用（本 Story 核心）
eventCh, err := driver.Stream(r.Context(), llmReq)
// eventCh: <-chan StreamEvent — 生产者关闭通道表示结束

// 转换请求（复用 Story 24.2 的 toLLMRequest）
llmReq := toLLMRequest(req, modelName)
```

### 已有代码中可直接复用的部分

| 已有代码 | 文件位置 | 复用方式 |
|---------|---------|---------|
| 请求解析和验证逻辑 | `ipc/http_openai.go:82-104` | 不变，stream 分支在验证后 |
| `parseModel()` | `ipc/http_openai.go:303-312` | 直接调用 |
| `toLLMRequest()` | `ipc/http_openai.go:257-271` | 直接调用 |
| `writeError()` | `ipc/http_openai.go:238-248` | 流开始前的错误场景使用 |
| `ChatCompletionChunk` 类型 | `ipc/http_openai.go:199-205` | Story 24.1 已定义 |
| `ChatChunkChoice` 类型 | `ipc/http_openai.go:208-212` | Story 24.1 已定义 |
| `ChatMessage` 类型 | `ipc/http_openai.go:176-179` | 用于 Delta 字段 |
| `configurableDriver` 测试 mock | `ipc/http_openai_test.go:39-54` | 需扩展 Stream 方法 |
| `newTestRegistry()` | `ipc/http_openai_test.go:56-66` | 测试中复用 |

### 变更范围

| 文件 | 变更类型 | 估算行数 |
|------|---------|---------|
| `ipc/http_openai.go` | **修改** | +50~80 行（handleStreamingResponse + toChatCompletionChunk） |
| `ipc/http_openai_test.go` | **修改** | +150~200 行（~10 个新测试函数，扩展 mock Stream 方法） |

**不修改的文件：**
- `drivers/llm/driver.go` — LLMDriver 接口不变，StreamEvent 类型不变
- `drivers/llm/registry.go` — DriverRegistry 不变
- `ipc/server.go` — Unix socket IPC server 不受影响
- `kernel/` — 内核不受影响（HTTP 绕过 Kernel）
- `cmd/rnix/` — serve 命令在 Story 24.5 实现

### 架构合规

- **依赖方向**：`ipc/` -> `drivers/llm/`（与 Story 24.1/24.2 一致，Decision 13 允许）
- **不引入新依赖**：仅使用已有的标准库 `encoding/json`、`fmt`、`net/http`、`time`
- **线程安全**：`handleStreamingResponse` 无可变共享状态；`driver.Stream()` 返回的 channel 是单消费者模型
- **错误格式**：流开始前用 OpenAI JSON 错误格式，流过程中用 SSE 事件传递错误
- **HTTP 绕过 Kernel**：与 Story 24.1/24.2 一致，直接调用 DriverRegistry

### 测试策略

**mock 驱动扩展：**
- 扩展 `configurableDriver`，增加 `streamFn` 字段以控制 `Stream()` 行为
- 或创建新的 `streamableDriver` 测试 helper

**SSE 输出验证：**
- `httptest.NewRecorder()` 实现了 `http.Flusher`，可捕获完整 SSE 输出
- 按行分割响应 body，验证每行 `data: ` 前缀和 `\n\n` 分隔
- 解析每个 chunk 的 JSON，验证 `ChatCompletionChunk` 结构

**竞态检测：**
- 所有测试启用 `-race`
- channel 读写是天然安全的，但需验证 Flush 调用不会 race

### 前置 Story 智能

**来自 Story 24-2（/v1/chat/completions 同步模式）：**
- `handleChatCompletions` 已实现完整的请求解析、验证、model 路由、driver 查找
- stream:true 当前返回 501（`http_openai.go:107-111`），本 Story 替换此分支
- `toLLMRequest()` 已实现并可复用
- Code Review 修复了 MaxBytesReader（4MB 限制）、context.Canceled 处理、nil 响应保护
- 测试中 `configurableDriver` 的 `Stream()` 方法当前返回空 channel（需扩展）
- `TestChatCompletions_StreamTrue_501` 测试需更新或替换

**来自 Story 24-1（OpenAI HTTP Server 核心框架）：**
- `ChatCompletionChunk` 和 `ChatChunkChoice` 类型已定义
- Code Review 指出 `ChatCompletionChunk` 缺少 `Usage` 字段（L6），本 Story 可根据需要添加

**来自架构文档（SSE 流式输出规则）：**
- Content-Type: `text/event-stream`
- 事件格式: `data: {json}\n\n`
- 终止标记: `data: [DONE]\n\n`
- Flush 时机: 每个 chunk 后立即 Flush
- 超时处理: 使用 `r.Context()` 传播客户端断开

### Git 智能

最近提交：
- `b1761ec feat: 24-2implement synchronous mode for /v1/chat/completions`
- `b33b865 feat: story 24-1`

提交建议：`feat: implement Story 24.3 - SSE streaming response`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 | 说明 |
|------|------|------|
| `/v1/models` 端点 | Story 24.4 | handleListModels 保持 stub |
| `rnix serve` CLI 命令 | Story 24.5 | Cobra 命令和 daemon 集成 |
| `tool_calls` 流式支持 | 未规划 | StreamEvent 有 ToolCalls 字段但本 Story 不处理 |
| Usage 统计在流式最后一个 chunk | 可选 | OpenAI 在 done chunk 中包含 Usage，可实现但非必须 |

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| stream:true 分支 | 同步分支（stream:false） | 互斥：同一 handler 中 if/else 分支 | 是（确保同步模式不受影响） |
| handleStreamingResponse | driver.Stream() | 调用：获取 event channel 并消费 | 是（mock 验证） |
| SSE Flusher | http.ResponseWriter | 接口断言：w.(http.Flusher) | 是（测试 Flusher 可用性） |
| context 取消 | r.Context() | 传播：request cancel -> driver.Stream cancel | 是（客户端断开测试） |
| MaxBytesReader | 请求体解析 | 复用：流式和同步共享请求解析逻辑 | 否（已在 24.2 测试） |

### Project Structure Notes

- 仅修改 `ipc/http_openai.go` 和 `ipc/http_openai_test.go` — 不新建文件
- 与 Story 24.1 和 24.2 建立的文件结构完全一致
- 与架构文档 Epic 24 -> `ipc/` 包映射一致

### References

- [Source: ipc/http_openai.go:107-111] — stream:true 的 501 stub 代码（需替换）
- [Source: ipc/http_openai.go:199-212] — ChatCompletionChunk 和 ChatChunkChoice 类型定义
- [Source: ipc/http_openai.go:257-271] — toLLMRequest 转换函数（可复用）
- [Source: ipc/http_openai.go:238-248] — writeError 辅助函数（流开始前使用）
- [Source: ipc/http_openai_test.go:39-54] — configurableDriver mock（需扩展 Stream 方法）
- [Source: ipc/http_openai_test.go:1158-1178] — TestChatCompletions_StreamTrue_501（需更新）
- [Source: drivers/llm/driver.go:39-47] — StreamEvent 类型定义
- [Source: drivers/llm/driver.go:58-62] — LLMDriver 接口含 Stream 方法
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#SSE] — SSE 流式输出规则
- [Source: _bmad-output/planning-artifacts/epics/epic-24-*#Story-24.3] — Epic 24 Story 24.3 AC 定义
- FRs covered: FR150
- NFRs covered: N/A

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

No issues encountered during implementation.

### Completion Notes List

- Replaced `handleChatCompletions` stream:true 501 stub with full SSE streaming implementation via new `handleStreamingResponse` method.
- Refactored `handleChatCompletions` to share request parsing/validation/model routing between sync and stream paths, branching after driver lookup.
- Implemented `toChatCompletionChunk()` conversion: maps StreamEvent types to ChatCompletionChunk with correct delta/finish_reason semantics.
- SSE headers: Content-Type=text/event-stream, Cache-Control=no-cache, Connection=keep-alive, with immediate Flush after headers.
- Stream ID consistency: single `chatcmpl-{timestamp}` ID generated once per stream, shared across all chunks.
- Error handling: pre-stream errors use standard JSON error response (502/504), mid-stream errors use SSE error event format with code=stream_error.
- Client disconnect: context cancellation check in event loop, early return without [DONE] marker.
- Extended `configurableDriver` test mock with `streamFn` field for controllable Stream() behavior.
- Added 12 new test functions: SSEHeaders, ChunkFormat, ChunkFields, ConsistentChunkID, FinishReason, DoneMarker, ClientDisconnect, StreamInitError, StreamInitTimeout, MidStreamError, EmptyStream, SyncModeRegression.
- Added test helpers: `makeStreamEvents()`, `parseSSELines()`, `parseSSEChunks()`, `newStreamingTestServer()`.
- Replaced `TestChatCompletions_StreamTrue_501` (obsolete 501 stub test) with streaming tests.
- All 21 test packages pass with race detection, 0 lint issues, build successful.

### File List

| File | Change Type |
|------|------------|
| `ipc/http_openai.go` | Modified (+85 lines: handleStreamingResponse method, toChatCompletionChunk function, ChatDelta type, refactored handleChatCompletions branching) |
| `ipc/http_openai_test.go` | Modified (+418 lines: 15 streaming test functions, 4 test helpers, extended configurableDriver with streamFn, replaced 501 stub test) |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | Modified (24-3 status: ready-for-dev -> in-progress -> review -> done) |
| `_bmad-output/implementation-artifacts/24-3-sse-streaming-response.md` | Modified (tasks marked complete, dev agent record, file list, status) |

### Change Log

- **2026-03-13**: Implemented Story 24.3 - SSE streaming response for /v1/chat/completions. Replaced stream:true 501 stub with full SSE implementation: handleStreamingResponse method with SSE headers, StreamEvent-to-ChatCompletionChunk conversion, [DONE] terminator, client disconnect propagation, and dual error handling (pre-stream JSON + mid-stream SSE). Added 12 new tests + 4 helpers, all passing with -race.
- **2026-03-13 Code Review (AI)**: Fixed 4 issues:
  - [H1] Added context.Canceled handling in streaming pre-stream error path (consistency with sync handler)
  - [M1] Created ChatDelta type with omitempty tags for correct OpenAI SSE delta serialization (no empty role/content fields)
  - [M2] First content chunk now includes role:"assistant" per OpenAI streaming spec
  - [M3] Error event Content field now propagated to SSE error response instead of hardcoded message
  - Added 3 new tests: DeltaRoleOnlyOnFirst, StreamInitCanceled, MidStreamErrorContent
  - All 21 test packages pass with -race, 0 lint issues, build OK
