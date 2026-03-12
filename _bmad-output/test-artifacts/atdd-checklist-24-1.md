---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/24-1-openai-http-server-core-framework.md'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/registry.go'
  - 'ipc/server_test.go'
---

# ATDD Checklist - Epic 24, Story 1: OpenAI HTTP Server 核心框架

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

Story 24.1 在 `ipc/` 包中建立 OpenAI 兼容 HTTP Server 的核心框架和类型系统，为后续端点实现提供统一的基础设施（路由、类型、错误格式、安全绑定）。

**As a** 开发者
**I want** 在 `ipc/` 包中建立 OpenAI 兼容 HTTP Server 的核心框架和类型系统
**So that** 后续端点实现有统一的基础设施（路由、类型、错误格式、安全绑定）

---

## Acceptance Criteria

1. **AC1 - OpenAIServer 构造函数:** `NewOpenAIServer(driverReg, addr)` 持有 `DriverRegistry` 引用，配置监听地址
2. **AC2 - OpenAI 兼容类型:** `ChatCompletionRequest`、`ChatCompletionResponse`、`ChatCompletionChunk`、`ChatMessage`、`ChatUsage` 类型字段名和 JSON tag 与 OpenAI API 规范一致（`snake_case`）
3. **AC3 - parseModel 路由函数:** 正确解析 `"ollama:llama3"` → provider=ollama, model=llama3；`"ollama"` → provider=ollama, model=""；`"cursor:claude-3.5-sonnet"` → provider=cursor, model=claude-3.5-sonnet
4. **AC4 - 错误响应格式:** provider 不存在返回 HTTP 404 + `model_not_found`；请求体格式错误返回 HTTP 400 + `invalid_request`
5. **AC5 - /health 端点:** GET /health 返回 HTTP 200 + 服务状态
6. **AC6 - 安全绑定:** 默认仅绑定 `127.0.0.1`，不暴露到外部网络接口（NFR52）

---

## Failing Tests Created (RED Phase)

### Unit Tests (23 tests)

**File:** `ipc/http_openai_test.go` (673 行)

#### AC1 — OpenAIServer 构造函数 (2 tests)

- RED **Test:** TestNewOpenAIServer_DefaultAddr
  - **Status:** RED — `NewOpenAIServer` 函数未定义
  - **Verifies:** AC#1 — 构造函数正确设置 driverReg 和 listenAddr

- RED **Test:** TestNewOpenAIServer_CustomAddr
  - **Status:** RED — `NewOpenAIServer` 函数未定义
  - **Verifies:** AC#1 — 自定义地址正确设置

#### AC2 — OpenAI 兼容类型 JSON 序列化 (7 tests)

- RED **Test:** TestChatCompletionRequest_JSONTags
  - **Status:** RED — `ChatCompletionRequest` 类型未定义
  - **Verifies:** AC#2 — 请求类型 JSON tag 使用 snake_case

- RED **Test:** TestChatCompletionResponse_JSONTags
  - **Status:** RED — `ChatCompletionResponse`、`ChatChoice`、`ChatUsage` 类型未定义
  - **Verifies:** AC#2 — 响应类型包含 id、object、created、model、choices、usage 字段

- RED **Test:** TestChatCompletionChunk_JSONTags
  - **Status:** RED — `ChatCompletionChunk`、`ChatChunkChoice` 类型未定义
  - **Verifies:** AC#2 — 流式响应类型 JSON 字段正确

- RED **Test:** TestChatMessage_JSONTags
  - **Status:** RED — `ChatMessage` 类型未定义
  - **Verifies:** AC#2 — 消息类型 role/content 字段正确

- RED **Test:** TestChatUsage_JSONTags
  - **Status:** RED — `ChatUsage` 类型未定义
  - **Verifies:** AC#2 — token 用量字段使用 snake_case（prompt_tokens、completion_tokens、total_tokens）

- RED **Test:** TestChatCompletionRequest_JSONRoundTrip
  - **Status:** RED — `ChatCompletionRequest` 类型未定义
  - **Verifies:** AC#2 — OpenAI 规范 JSON 请求体正确反序列化到结构体

#### AC3 — parseModel 路由函数 (1 test, 7 sub-tests)

- RED **Test:** TestParseModel
  - **Status:** RED — `parseModel` 函数未定义
  - **Verifies:** AC#3 — table-driven 测试覆盖 provider:model、仅 provider、model 含冒号、空字符串等情况
  - **Sub-tests:**
    - `ollama:llama3` → provider=ollama, model=llama3
    - `cursor:claude-3.5-sonnet` → provider=cursor, model=claude-3.5-sonnet
    - `claude:claude-3-opus` → provider=claude, model=claude-3-opus
    - `ollama` → provider=ollama, model=""
    - `claude` → provider=claude, model=""
    - `provider:model:with:colons` → provider=provider, model=model:with:colons
    - `""` → provider="", model=""

#### AC4 — OpenAI 兼容错误响应 (4 tests)

- RED **Test:** TestWriteError_ModelNotFound
  - **Status:** RED — `writeError` 函数和 `OpenAIErrorResponse` 类型未定义
  - **Verifies:** AC#4 — HTTP 404 + error.type=invalid_request_error + error.code=model_not_found

- RED **Test:** TestWriteError_InvalidRequest
  - **Status:** RED — `writeError` 函数未定义
  - **Verifies:** AC#4 — HTTP 400 + error.code=invalid_request

- RED **Test:** TestWriteError_ContentType
  - **Status:** RED — `writeError` 函数未定义
  - **Verifies:** AC#4 — 错误响应 Content-Type 为 application/json

- RED **Test:** TestOpenAIErrorResponse_JSONFormat
  - **Status:** RED — `OpenAIErrorResponse`、`OpenAIErrorDetail` 类型未定义
  - **Verifies:** AC#4 — 错误响应 JSON 格式 `{"error": {"message", "type", "code"}}`

#### AC5 — /health 端点 (4 tests)

- RED **Test:** TestHandleHealth_Returns200
  - **Status:** RED — `NewOpenAIServer` 和 `handleHealth` 方法未定义
  - **Verifies:** AC#5 — GET /health 返回 HTTP 200

- RED **Test:** TestHandleHealth_ReturnsJSON
  - **Status:** RED — `handleHealth` 方法未定义
  - **Verifies:** AC#5 — 返回有效 JSON，Content-Type 为 application/json

- RED **Test:** TestHandleHealth_ContainsStatus
  - **Status:** RED — `handleHealth` 方法未定义
  - **Verifies:** AC#5 — 响应包含 status 字段

- RED **Test:** TestHandleHealth_ContainsProviders
  - **Status:** RED — `handleHealth` 方法未定义
  - **Verifies:** AC#5 — 响应包含 providers 数量（等于注册的 driver 数量）

#### AC6 — 安全绑定 (2 tests)

- RED **Test:** TestNewOpenAIServer_DefaultBindsLocalhost
  - **Status:** RED — `NewOpenAIServer` 函数未定义
  - **Verifies:** AC#6 — listenAddr 以 127.0.0.1 开头

- RED **Test:** TestNewOpenAIServer_RejectsWildcardBind
  - **Status:** RED — `NewOpenAIServer` 函数未定义
  - **Verifies:** AC#6 — 默认不绑定 0.0.0.0

#### 路由和 Stub 验证 (3 tests)

- RED **Test:** TestHandleChatCompletions_Stub
  - **Status:** RED — `handleChatCompletions` 方法未定义
  - **Verifies:** AC#1 — POST /v1/chat/completions stub 返回 501

- RED **Test:** TestHandleListModels_Stub
  - **Status:** RED — `handleListModels` 方法未定义
  - **Verifies:** AC#1 — GET /v1/models stub 返回 501

- RED **Test:** TestOpenAIServer_RoutesRegistered
  - **Status:** RED — `buildMux` 方法未定义
  - **Verifies:** AC#1 — ServeMux 路由集成测试，各端点正确路由

#### 补充验证 (1 test)

- RED **Test:** TestOpenAIServer_ShutdownExists
  - **Status:** RED — `NewOpenAIServer` 和 `Shutdown` 方法未定义
  - **Verifies:** Shutdown 方法存在且不 panic

---

## Data Factories Created

### stubDriver Test Helper

**File:** `ipc/http_openai_test.go`（内嵌于测试文件）

**Exports:**

- `stubDriver` — 实现 `llm.LLMDriver` 接口的桩实现
- `newTestRegistry(t)` — 创建包含 claude 和 ollama 两个 stub driver 的测试用 DriverRegistry

---

## Mock Requirements

### LLM Driver Mock

本 Story 测试不需要外部服务 mock。`stubDriver` 结构体内嵌于测试文件中，实现 `llm.LLMDriver` 接口即可。

- `Call()` 返回固定 `LLMResponse{Content: "stub"}`
- `Stream()` 返回空的已关闭 channel
- `Info()` 返回预设的 `DriverInfo`

---

## Implementation Checklist

### Test: TestNewOpenAIServer_DefaultAddr / TestNewOpenAIServer_CustomAddr

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `ipc/http_openai.go` 文件
- [ ] 定义 `OpenAIServer` 结构体（driverReg, listenAddr, server 字段）
- [ ] 实现 `NewOpenAIServer(driverReg *llm.DriverRegistry, addr string) *OpenAIServer`
- [ ] Run test: `go test -race -run TestNewOpenAIServer ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestChatCompletionRequest_JSONTags / TestChatCompletionResponse_JSONTags / TestChatCompletionChunk_JSONTags / TestChatMessage_JSONTags / TestChatUsage_JSONTags / TestChatCompletionRequest_JSONRoundTrip

**File:** `ipc/http_openai_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `ChatCompletionRequest` 结构体（model, messages, stream, temperature, max_tokens）及 JSON tag
- [ ] 定义 `ChatMessage` 结构体（role, content）及 JSON tag
- [ ] 定义 `ChatCompletionResponse` 结构体（id, object, created, model, choices, usage）及 JSON tag
- [ ] 定义 `ChatChoice` 结构体（index, message, finish_reason）及 JSON tag
- [ ] 定义 `ChatCompletionChunk` 结构体（id, object, created, model, choices）及 JSON tag
- [ ] 定义 `ChatChunkChoice` 结构体（index, delta, finish_reason）及 JSON tag
- [ ] 定义 `ChatUsage` 结构体（prompt_tokens, completion_tokens, total_tokens）及 JSON tag
- [ ] 所有 JSON tag 使用 snake_case，验证与 OpenAI API 规范一致
- [ ] Run test: `go test -race -run TestChat ./ipc/...`
- [ ] Tests pass (green phase)

---

### Test: TestParseModel

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `parseModel(model string) (provider, modelName string)` 函数
- [ ] 使用 `strings.SplitN(model, ":", 2)` 分割
- [ ] 处理空字符串输入
- [ ] Run test: `go test -race -run TestParseModel ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestWriteError_ModelNotFound / TestWriteError_InvalidRequest / TestWriteError_ContentType / TestOpenAIErrorResponse_JSONFormat

**File:** `ipc/http_openai_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `OpenAIErrorResponse` 结构体
- [ ] 定义 `OpenAIErrorDetail` 结构体（message, type, code）
- [ ] 实现 `writeError(w http.ResponseWriter, statusCode int, errType, code, message string)` 函数
- [ ] 设置 Content-Type: application/json
- [ ] 写入 HTTP 状态码和 JSON 错误体
- [ ] Run test: `go test -race -run TestWriteError ./ipc/...`
- [ ] Tests pass (green phase)

---

### Test: TestHandleHealth_Returns200 / TestHandleHealth_ReturnsJSON / TestHandleHealth_ContainsStatus / TestHandleHealth_ContainsProviders

**File:** `ipc/http_openai_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `(s *OpenAIServer) handleHealth(w http.ResponseWriter, r *http.Request)` 方法
- [ ] 返回 HTTP 200
- [ ] 设置 Content-Type: application/json
- [ ] 返回包含 status 和 providers 字段的 JSON
- [ ] providers 数量来自 `driverReg.Len()`
- [ ] Run test: `go test -race -run TestHandleHealth ./ipc/...`
- [ ] Tests pass (green phase)

---

### Test: TestNewOpenAIServer_DefaultBindsLocalhost / TestNewOpenAIServer_RejectsWildcardBind

**File:** `ipc/http_openai_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 `NewOpenAIServer` 默认使用 `127.0.0.1` 地址
- [ ] Run test: `go test -race -run TestNewOpenAIServer_Default ./ipc/...`
- [ ] Tests pass (green phase)

---

### Test: TestHandleChatCompletions_Stub / TestHandleListModels_Stub

**File:** `ipc/http_openai_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `(s *OpenAIServer) handleChatCompletions(w http.ResponseWriter, r *http.Request)` stub（返回 501）
- [ ] 实现 `(s *OpenAIServer) handleListModels(w http.ResponseWriter, r *http.Request)` stub（返回 501）
- [ ] Run test: `go test -race -run TestHandle.*Stub ./ipc/...`
- [ ] Tests pass (green phase)

---

### Test: TestOpenAIServer_RoutesRegistered

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `(s *OpenAIServer) buildMux() *http.ServeMux` 方法
- [ ] 注册路由：POST /v1/chat/completions、GET /v1/models、GET /health
- [ ] 使用 Go 1.22+ ServeMux 方法匹配语法
- [ ] Run test: `go test -race -run TestOpenAIServer_RoutesRegistered ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestOpenAIServer_ShutdownExists

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `(s *OpenAIServer) Shutdown(ctx context.Context) error` 方法
- [ ] 实现 `(s *OpenAIServer) ListenAndServe() error` 方法
- [ ] Run test: `go test -race -run TestOpenAIServer_ShutdownExists ./ipc/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestNewOpenAIServer|TestChatCompletion|TestChatMessage|TestChatUsage|TestParseModel|TestWriteError|TestOpenAIError|TestHandleHealth|TestHandleChatCompletions|TestHandleListModels|TestOpenAIServer" ./ipc/...

# Run specific test
go test -race -run TestParseModel ./ipc/...

# Run tests with verbose output
go test -race -v -run "TestNewOpenAIServer|TestChatCompletion" ./ipc/...

# Run all ipc tests
go test -race ./ipc/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 23 tests written and failing (compilation error: types/functions undefined)
- Test helper (stubDriver + newTestRegistry) created
- Implementation checklist created
- Test file: `ipc/http_openai_test.go`

**Verification:**

- Tests fail to compile: `undefined: NewOpenAIServer`
- Failure is due to missing implementation, not test bugs
- All tests reference types and functions defined in Story 24.1

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **创建 `ipc/http_openai.go`**
2. **逐步实现**（按 Implementation Checklist 顺序）:
   - OpenAIServer 结构体 + NewOpenAIServer
   - OpenAI 兼容类型（ChatCompletionRequest 等）
   - parseModel 函数
   - writeError + 错误类型
   - handleHealth 端点
   - handleChatCompletions / handleListModels stub
   - buildMux 路由注册
   - ListenAndServe / Shutdown
3. **每实现一组，运行对应测试**
4. **所有测试通过后进入 REFACTOR 阶段**

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有测试通过
2. 检查代码质量（可读性、命名一致性）
3. 确保无重复代码
4. 运行 `make lint` 和 `make vet`
5. 运行完整测试 `make test`

---

## Next Steps

1. **运行失败测试确认 RED 阶段**: `go test -race ./ipc/...`（编译失败）
2. **开始实现** — 使用 Implementation Checklist 作为指南
3. **逐测试通过** — 每次实现一组功能后运行对应测试
4. **所有测试通过后** — 运行 `make all` 全量检查
5. **完成后更新** sprint-status.yaml

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go vet ./ipc/`

**Results:**

```
# github.com/rnixai/rnix/ipc
ipc/http_openai_test.go:57:9: undefined: NewOpenAIServer
```

**Summary:**

- Total tests: 23
- Passing: 0 (expected — compilation failure)
- Failing: 23 (expected — types and functions undefined)
- Status: RED phase verified

---

## Notes

- 本 Story 仅建立框架，`handleChatCompletions` 和 `handleListModels` 为 stub（返回 501）
- `buildMux` 方法用于测试路由注册，`ListenAndServe` 内部调用 `buildMux`
- stubDriver 使用 `context.Context` 参数匹配 `llm.LLMDriver` 接口签名
- 测试文件在同包 `ipc` 下，可以访问未导出的 `parseModel`、`writeError` 等函数

---

**Generated by BMad TEA Agent** - 2026-03-13
