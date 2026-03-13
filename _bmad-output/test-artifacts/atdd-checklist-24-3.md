---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-finalize']
lastStep: 'step-05-finalize'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/24-3-sse-streaming-response.md'
  - 'ipc/http_openai.go'
  - 'ipc/http_openai_test.go'
  - 'drivers/llm/driver.go'
---

# ATDD Checklist - Epic 24, Story 3: SSE 流式响应

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit (Go `testing` + `httptest`)
**Stack:** Backend (Go 1.26)

---

## Story Summary

Story 24.3 为 `/v1/chat/completions` 端点添加 SSE 流式响应模式（`stream: true`），将 LLMDriver.Stream() 返回的 StreamEvent 通道转换为 OpenAI 兼容的 SSE 格式输出。

**As a** 外部工具用户
**I want** 通过 `stream: true` 获取 SSE 流式 LLM 响应
**So that** 支持实时流式输出的工具（如 Open WebUI）可以获得逐 token 的响应体验

---

## Acceptance Criteria

1. **AC1** — SSE 响应头：Content-Type=`text/event-stream`、Cache-Control=`no-cache`、Connection=`keep-alive`
2. **AC2** — StreamEvent → ChatCompletionChunk 转换，`data: {json}\n\n` 格式，每 chunk 后 Flush
3. **AC3** — 通道关闭后写入 `data: [DONE]\n\n` 终止标记
4. **AC4** — 客户端断连 → context 取消传播到 `driver.Stream()`
5. **AC5** — 流式错误处理：写入 SSE 错误事件后关闭

---

## Failing Tests Created (RED Phase)

### Unit Tests (12 tests)

**File:** `ipc/http_openai_test.go`（扩展现有测试文件）

**测试框架:** Go standard `testing` + `net/http/httptest`
**Mock 模式:** 扩展 `configurableDriver` 增加 `streamFn` 字段

---

#### Test 1: TestStreamingResponse_SSEHeaders

- **Status:** RED — stream:true 当前返回 501 Not Implemented
- **Verifies:** AC1 — SSE 响应头正确设置
- **Priority:** P0
- **Details:**
  - 发送 `stream: true` 请求
  - 验证 `Content-Type` = `text/event-stream`
  - 验证 `Cache-Control` = `no-cache`
  - 验证 `Connection` = `keep-alive`
  - 验证 HTTP status = 200

#### Test 2: TestStreamingResponse_ChunkFormat

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC2 — 每个 chunk 以 `data: {json}\n\n` 格式写入
- **Priority:** P0
- **Details:**
  - Mock driver.Stream() 返回 3 个 content events + 1 个 done event
  - 验证响应 body 包含 4 个 `data: ` 前缀行
  - 验证每行间用 `\n\n` 分隔
  - 验证每个 `data:` 后的 JSON 可解析为 `ChatCompletionChunk`

#### Test 3: TestStreamingResponse_ChunkFields

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC2 — ChatCompletionChunk 字段正确
- **Priority:** P0
- **Details:**
  - 验证 `object` = `"chat.completion.chunk"`
  - 验证 `model` = 请求中的 model 字符串
  - 验证 content chunk 的 `choices[0].delta.content` 包含事件内容
  - 验证 `choices[0].delta.role` 为空（流式中 role 仅在首个 chunk 可选）
  - 验证 `created` 为有效 Unix 时间戳

#### Test 4: TestStreamingResponse_ConsistentChunkID

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC2 — 同一流中所有 chunk 共享相同 ID
- **Priority:** P1
- **Details:**
  - Mock driver.Stream() 返回多个 events
  - 收集所有 chunk 的 `id` 字段
  - 验证所有 `id` 值相同且以 `"chatcmpl-"` 为前缀

#### Test 5: TestStreamingResponse_FinishReason

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC2 — FinishReason 映射
- **Priority:** P1
- **Details:**
  - Content event chunk: `finish_reason` 为空/省略（omitempty）
  - Done event chunk: `finish_reason` = `"stop"`

#### Test 6: TestStreamingResponse_DoneMarker

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC3 — `data: [DONE]\n\n` 终止标记
- **Priority:** P0
- **Details:**
  - Mock driver.Stream() 返回 content + done events
  - 验证响应 body 的最后一个非空行为 `data: [DONE]`
  - 验证在 done chunk 之后写入

#### Test 7: TestStreamingResponse_ClientDisconnect

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC4 — 客户端断连处理
- **Priority:** P0
- **Details:**
  - 使用 `context.WithCancel` 创建可控 context
  - Mock driver.Stream() 返回持续产生事件的通道
  - 在收到首个 chunk 后取消 context
  - 验证 handler 正常退出（不 panic、不 deadlock）
  - 验证不再有数据写入 ResponseWriter

#### Test 8: TestStreamingResponse_StreamInitError

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC5 — Stream 初始化错误返回 JSON 错误
- **Priority:** P1
- **Details:**
  - Mock driver.Stream() 返回 `(nil, error)`
  - 验证响应为标准 JSON 错误格式（非 SSE）
  - 验证 HTTP status = 502
  - 验证 `Content-Type` = `application/json`

#### Test 9: TestStreamingResponse_StreamInitTimeout

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC5 — Stream 初始化超时
- **Priority:** P1
- **Details:**
  - Mock driver.Stream() 返回 `(nil, context.DeadlineExceeded)`
  - 验证 HTTP status = 504
  - 验证 error.code = `"timeout"`

#### Test 10: TestStreamingResponse_MidStreamError

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC5 — 流中错误事件
- **Priority:** P1
- **Details:**
  - Mock driver.Stream() 返回 1 个 content event + 1 个 error event
  - 验证前面的 content chunk 正常写入
  - 验证错误以 SSE 事件格式写入：`data: {"error": {...}}\n\n`
  - 验证错误后不再有 `[DONE]` 标记

#### Test 11: TestStreamingResponse_EmptyStream

- **Status:** RED — stream:true 当前返回 501
- **Verifies:** AC3 边界 — 通道立即关闭
- **Priority:** P2
- **Details:**
  - Mock driver.Stream() 返回立即关闭的通道
  - 验证只写入 `data: [DONE]\n\n`
  - 验证 SSE 头正确设置

#### Test 12: TestStreamingResponse_SyncModeRegression

- **Status:** GREEN（回归测试，应始终通过）
- **Verifies:** 同步模式不受流式实现影响
- **Priority:** P0
- **Details:**
  - 发送 `stream: false` 请求（与 Story 24.2 测试相同）
  - 验证返回标准 JSON `ChatCompletionResponse`
  - 验证 `Content-Type` = `application/json`

---

## Mock Requirements

### configurableDriver 扩展

**现有 Mock**（`ipc/http_openai_test.go:39-54`）只有 `callFn`，需增加 `streamFn`：

```go
type configurableDriver struct {
    info     llm.DriverInfo
    callFn   func(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error)
    streamFn func(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error)
}

func (d *configurableDriver) Stream(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error) {
    if d.streamFn != nil {
        return d.streamFn(ctx, req)
    }
    ch := make(chan llm.StreamEvent)
    close(ch)
    return ch, nil
}
```

### 测试 Helper 函数

```go
// makeStreamEvents 创建一组标准测试 StreamEvent
func makeStreamEvents(contents ...string) <-chan llm.StreamEvent {
    ch := make(chan llm.StreamEvent, len(contents)+1)
    for _, c := range contents {
        ch <- llm.StreamEvent{Type: "content", Content: c}
    }
    ch <- llm.StreamEvent{Type: "done"}
    close(ch)
    return ch
}

// parseSSELines 从响应 body 中提取所有 data: 行
func parseSSELines(body string) []string { ... }

// parseSSEChunks 解析 SSE body 为 ChatCompletionChunk 切片
func parseSSEChunks(t *testing.T, body string) []ChatCompletionChunk { ... }
```

---

## Implementation Checklist

### Test: TestStreamingResponse_SSEHeaders (AC1)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 移除 `handleChatCompletions` 中 `req.Stream == true` 的 501 返回（`http_openai.go:107-111`）
- [ ] 添加 SSE 响应头设置（Content-Type, Cache-Control, Connection）
- [ ] 断言 `w.(http.Flusher)` 获取 Flusher
- [ ] Run test: `go test -race -run TestStreamingResponse_SSEHeaders ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestStreamingResponse_ChunkFormat + ChunkFields (AC2)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 调用 `driver.Stream(r.Context(), llmReq)` 获取事件通道
- [ ] 实现 `toStreamChunk()` 或 `toChatCompletionChunk()` 转换函数
- [ ] 遍历通道，每个 event 转换为 chunk 后 `data: {json}\n\n` 格式写入
- [ ] 每次写入后调用 `flusher.Flush()`
- [ ] Run test: `go test -race -run "TestStreamingResponse_Chunk" ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestStreamingResponse_DoneMarker (AC3)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 通道 range 结束后写入 `data: [DONE]\n\n`
- [ ] 写入后调用 `flusher.Flush()`
- [ ] Run test: `go test -race -run TestStreamingResponse_DoneMarker ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestStreamingResponse_ClientDisconnect (AC4)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 在 range 循环中检查 `r.Context().Err() != nil`
- [ ] context 取消时退出循环（不写 [DONE]）
- [ ] Run test: `go test -race -run TestStreamingResponse_ClientDisconnect ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestStreamingResponse_StreamInitError + MidStreamError (AC5)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] Stream 初始化错误：SSE 头未写入前使用 `writeError()` 返回 JSON 错误
- [ ] 流中错误：检查 `event.Type == "error"` 或 `event.Err != nil`，写入 SSE 错误事件
- [ ] Run test: `go test -race -run "TestStreamingResponse_Stream.*Error" ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestStreamingResponse_ConsistentChunkID + FinishReason (AC2 细节)

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 在流开始前生成 `streamID`，传入每次 `toStreamChunk()` 调用
- [ ] Content event: `FinishReason = ""`（omitempty 不序列化）
- [ ] Done event: `FinishReason = "stop"`
- [ ] Run test: `go test -race -run "TestStreamingResponse_(Consistent|Finish)" ./ipc/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all streaming tests for this story
go test -race -run "TestStreamingResponse" ./ipc/...

# Run all ipc tests (including Story 24.1 and 24.2 regression)
go test -race ./ipc/...

# Run specific test
go test -race -run TestStreamingResponse_SSEHeaders ./ipc/...

# Run tests with verbose output
go test -race -v -run "TestStreamingResponse" ./ipc/...

# Run all project tests
make test
```

---

## Red-Green-Refactor Workflow

### RED Phase (ATDD Checklist) ✅

**TEA Agent Responsibilities:**

- ✅ 12 个测试场景设计完成
- ✅ Mock 驱动扩展方案定义（configurableDriver + streamFn）
- ✅ 测试 Helper 函数设计（makeStreamEvents, parseSSELines, parseSSEChunks）
- ✅ Implementation checklist 映射完成
- ✅ 回归测试覆盖（同步模式不受影响）

**验证:**

- 所有测试场景覆盖 5 个 AC
- 包含负面和边界场景（空 stream、流中错误、客户端断连）
- 测试失败原因明确（stream:true 当前返回 501）

---

### GREEN Phase (DEV Team)

1. 扩展 `configurableDriver` 增加 `streamFn` 支持
2. 实现 `handleStreamingResponse` 或在 `handleChatCompletions` 中添加流式分支
3. 实现 `toStreamChunk()` 转换函数
4. 逐个测试验证通过
5. 确认同步模式回归测试通过

### REFACTOR Phase

1. 确保 stream/sync 分支共享验证逻辑
2. 无代码重复
3. 所有测试通过 + `-race` 检测

---

## AC ↔ Test Traceability

| AC | Test(s) | 优先级 |
|----|---------|-------|
| AC1: SSE 响应头 | TestStreamingResponse_SSEHeaders | P0 |
| AC2: Chunk 格式 | TestStreamingResponse_ChunkFormat, ChunkFields | P0 |
| AC2: Chunk ID | TestStreamingResponse_ConsistentChunkID | P1 |
| AC2: FinishReason | TestStreamingResponse_FinishReason | P1 |
| AC3: [DONE] 标记 | TestStreamingResponse_DoneMarker | P0 |
| AC4: 客户端断连 | TestStreamingResponse_ClientDisconnect | P0 |
| AC5: 初始化错误 | TestStreamingResponse_StreamInitError, StreamInitTimeout | P1 |
| AC5: 流中错误 | TestStreamingResponse_MidStreamError | P1 |
| 回归 | TestStreamingResponse_SyncModeRegression | P0 |
| 边界 | TestStreamingResponse_EmptyStream | P2 |

---

## Notes

- 所有测试使用 Go 标准 `testing` 包 + `httptest.NewRecorder()`
- `httptest.ResponseRecorder` 实现了 `http.Flusher` 接口，SSE 测试可行
- 测试不需要启动真实 HTTP 服务器，直接调用 handler 函数
- 竞态检测通过 `go test -race` 启用
- Story 24.2 的 `TestChatCompletions_StreamTrue_501` 需更新为 `TestStreamingResponse_SSEHeaders` 或移除

---

**Generated by BMad TEA Agent** - 2026-03-13
