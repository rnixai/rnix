---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-04c-aggregate'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/24-2-chat-completions-sync-mode.md'
  - 'ipc/http_openai.go'
  - 'ipc/http_openai_test.go'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/registry.go'
---

# ATDD 检查清单 - Epic 24, Story 2: /v1/chat/completions 同步模式

**日期:** 2026-03-13
**作者:** Decker
**主要测试级别:** API/集成测试（Go httptest）

---

## Story 摘要

实现 OpenAI 兼容的 `/v1/chat/completions` 同步模式端点，将请求路由到已注册的 LLM provider 驱动，并返回标准 OpenAI 格式响应。

**As a** 外部工具用户
**I want** 通过标准 OpenAI Chat Completion API 发起同步 LLM 请求
**So that** 任何支持 OpenAI API 的工具可以通过 Rnix 网关调用已注册的 LLM provider

---

## 验收标准

1. **AC1**: POST `/v1/chat/completions` 成功同步请求 -- 解析 model 为 provider:model，通过 DriverRegistry 获取驱动，将 ChatMessage 转为 LLMRequest，调用 driver.Call()，将 LLMResponse 转为 ChatCompletionResponse 返回
2. **AC2**: 仅 provider 名（无 model） -- 使用 provider 的 default_model
3. **AC3**: provider:model 复合格式 -- 使用指定 model 覆盖默认模型
4. **AC4**: provider 不存在 -- 返回 HTTP 404 + OpenAI 错误格式 + 可用 provider 列表
5. **AC5**: LLM 驱动内部错误 -- 超时返回 504，其他错误返回 502
6. **AC6**: HTTP 处理开销 <= 50ms（NFR50）

---

## 测试策略（后端 Go 项目）

### 测试级别选择

| 验收标准 | 测试级别 | 优先级 | 理由 |
|---------|---------|--------|------|
| AC1 成功同步请求 | 单元/集成 | P0 | 核心功能路径 |
| AC2 仅 provider 名 | 单元 | P0 | model 路由核心逻辑 |
| AC3 provider:model 格式 | 单元 | P0 | model 路由核心逻辑 |
| AC4 provider 不存在 | 单元 | P1 | 错误处理 |
| AC5 驱动超时/错误 | 单元 | P1 | 错误映射 |
| AC6 性能开销 | 性能基准 | P2 | 非功能需求 |

### 生成模式

**AI 生成模式**（后端项目，无需浏览器录制）

### 执行模式

**顺序执行**（SEQUENTIAL），Go 测试文件位于同一包 `ipc/`

---

## 失败测试设计（RED 阶段）

### API/集成测试（12 个测试）

**文件:** `ipc/http_openai_test.go`（追加到现有文件）

所有测试设计为在 `handleChatCompletions` 实现前**必须失败**（当前为 501 stub）。

---

### 测试 1: TestHandleChatCompletions_SyncSuccess [P0]

- **验证:** AC1 -- 成功同步请求完整流程
- **状态:** RED -- stub 返回 501，期望 200
- **前提:** mock driver 返回固定 LLMResponse
- **断言:**
  - HTTP 200
  - Content-Type: application/json
  - 响应体包含 `id`（以 "chatcmpl-" 前缀）
  - `object` = "chat.completion"
  - `choices[0].message.role` = "assistant"
  - `choices[0].message.content` = mock 返回内容
  - `choices[0].finish_reason` = "stop"
  - `usage.prompt_tokens` / `completion_tokens` / `total_tokens` 正确映射

### 测试 2: TestHandleChatCompletions_ProviderOnly [P0]

- **验证:** AC2 -- 仅 provider 名，使用 default_model
- **状态:** RED -- stub 返回 501
- **请求:** `{"model": "ollama", "messages": [...]}`
- **断言:** HTTP 200，驱动被调用且 LLMRequest.Model 为空

### 测试 3: TestHandleChatCompletions_ProviderModelFormat [P0]

- **验证:** AC3 -- provider:model 复合格式
- **状态:** RED -- stub 返回 501
- **请求:** `{"model": "cursor:claude-3.5-sonnet", "messages": [...]}`
- **断言:** HTTP 200，驱动被调用且 LLMRequest.Model = "claude-3.5-sonnet"

### 测试 4: TestHandleChatCompletions_ProviderNotFound [P1]

- **验证:** AC4 -- provider 不存在返回 404
- **状态:** RED -- stub 返回 501（不是 404）
- **请求:** `{"model": "nonexistent", "messages": [...]}`
- **断言:**
  - HTTP 404
  - error.code = "model_not_found"
  - error.message 包含可用 provider 列表

### 测试 5: TestHandleChatCompletions_InvalidJSON [P1]

- **验证:** AC4 补充 -- JSON 解析失败返回 400
- **状态:** RED -- stub 返回 501（不解析 body）
- **请求:** 非法 JSON body
- **断言:** HTTP 400, error.code = "invalid_request"

### 测试 6: TestHandleChatCompletions_EmptyMessages [P1]

- **验证:** AC1 补充 -- messages 为空返回 400
- **状态:** RED -- stub 返回 501
- **请求:** `{"model": "ollama", "messages": []}`
- **断言:** HTTP 400, error.code = "invalid_request"

### 测试 7: TestHandleChatCompletions_EmptyModel [P1]

- **验证:** AC1 补充 -- model 为空返回 400
- **状态:** RED -- stub 返回 501
- **请求:** `{"model": "", "messages": [...]}`
- **断言:** HTTP 400, error.code = "invalid_request"

### 测试 8: TestHandleChatCompletions_DriverTimeout [P1]

- **验证:** AC5 -- 驱动超时返回 504
- **状态:** RED -- stub 返回 501
- **前提:** mock driver 返回 context.DeadlineExceeded
- **断言:** HTTP 504, error.code = "timeout"

### 测试 9: TestHandleChatCompletions_DriverError [P1]

- **验证:** AC5 -- 驱动内部错误返回 502
- **状态:** RED -- stub 返回 501
- **前提:** mock driver 返回一般 error
- **断言:** HTTP 502, error.code = "upstream_error"

### 测试 10: TestHandleChatCompletions_StreamTrue501 [P1]

- **验证:** stream:true 返回 501（Story 24.3 实现）
- **状态:** RED -- 当前 stub 返回 501 但原因不对
- **请求:** `{"model": "ollama", "messages": [...], "stream": true}`
- **断言:** HTTP 501, error.code = "not_implemented"

### 测试 11: TestHandleChatCompletions_TemperatureMapping [P2]

- **验证:** AC1 补充 -- Temperature 可选字段正确传递
- **状态:** RED -- stub 返回 501
- **断言:** mock driver 收到的 LLMRequest.Temperature 与请求一致

### 测试 12: TestHandleChatCompletions_OverheadBenchmark [P2]

- **验证:** AC6 -- HTTP 处理开销 <= 50ms
- **状态:** RED -- stub 返回 501（无法测量有效开销）
- **方法:** 计时 mock driver 场景下的端到端 HTTP 处理时间
- **断言:** 单次请求处理时间 < 50ms

---

## Mock/Stub 需求

### 可控 stubDriver

**需要扩展现有 `stubDriver`** 以支持可控响应和错误：

```go
type callableStubDriver struct {
    info     llm.DriverInfo
    callFunc func(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error)
}
```

**用途:**
- 成功场景：返回固定 `LLMResponse{Content, TokensUsed, InputTokens, OutputTokens}`
- 超时场景：返回 `context.DeadlineExceeded`
- 错误场景：返回一般 `errors.New("upstream failure")`
- 捕获场景：记录传入的 `LLMRequest` 用于断言参数映射

---

## 实现检查清单

### 测试: TestHandleChatCompletions_SyncSuccess

**文件:** `ipc/http_openai_test.go`

**使测试通过的任务:**

- [ ] 替换 `handleChatCompletions` 的 501 stub
- [ ] 实现 JSON 请求体解析
- [ ] 调用 `parseModel()` 解析 provider/model
- [ ] 通过 `s.driverReg.Get(provider)` 获取驱动
- [ ] 实现 `toLLMRequest()` 转换函数
- [ ] 调用 `driver.Call(ctx, req)`
- [ ] 实现 `toChatCompletionResponse()` 转换函数
- [ ] 返回 JSON 响应
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_SyncSuccess ./ipc/...`

### 测试: TestHandleChatCompletions_ProviderOnly

**使测试通过的任务:**

- [ ] `toLLMRequest()` 中：当 modelName 为空时，LLMRequest.Model 留空
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_ProviderOnly ./ipc/...`

### 测试: TestHandleChatCompletions_ProviderModelFormat

**使测试通过的任务:**

- [ ] `toLLMRequest()` 中：当 modelName 非空时，设置 LLMRequest.Model
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_ProviderModelFormat ./ipc/...`

### 测试: TestHandleChatCompletions_ProviderNotFound

**使测试通过的任务:**

- [ ] `driverReg.Get()` 返回 false 时，HTTP 404
- [ ] 错误消息包含 `s.driverReg.Names()` 返回的 provider 列表
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_ProviderNotFound ./ipc/...`

### 测试: TestHandleChatCompletions_InvalidJSON

**使测试通过的任务:**

- [ ] `json.NewDecoder(r.Body).Decode(&req)` 失败时返回 400
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_InvalidJSON ./ipc/...`

### 测试: TestHandleChatCompletions_EmptyMessages

**使测试通过的任务:**

- [ ] 验证 `len(req.Messages) == 0` 时返回 400
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_EmptyMessages ./ipc/...`

### 测试: TestHandleChatCompletions_EmptyModel

**使测试通过的任务:**

- [ ] 验证 `req.Model == ""` 时返回 400
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_EmptyModel ./ipc/...`

### 测试: TestHandleChatCompletions_DriverTimeout

**使测试通过的任务:**

- [ ] `driver.Call()` 返回 `context.DeadlineExceeded` 时返回 504
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_DriverTimeout ./ipc/...`

### 测试: TestHandleChatCompletions_DriverError

**使测试通过的任务:**

- [ ] `driver.Call()` 返回其他 error 时返回 502
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_DriverError ./ipc/...`

### 测试: TestHandleChatCompletions_StreamTrue501

**使测试通过的任务:**

- [ ] 检测 `req.Stream == true` 时返回 501 + "not_implemented"
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_StreamTrue501 ./ipc/...`

### 测试: TestHandleChatCompletions_TemperatureMapping

**使测试通过的任务:**

- [ ] `toLLMRequest()` 中映射 `Temperature` 字段
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_TemperatureMapping ./ipc/...`

### 测试: TestHandleChatCompletions_OverheadBenchmark

**使测试通过的任务:**

- [ ] 完整实现后，验证 mock driver 场景下处理时间 < 50ms
- [ ] 运行测试: `go test -race -run TestHandleChatCompletions_OverheadBenchmark ./ipc/...`

---

## 运行测试

```bash
# 运行 Story 24-2 所有失败测试
go test -race -run "TestHandleChatCompletions_" ./ipc/...

# 运行单个测试
go test -race -run TestHandleChatCompletions_SyncSuccess ./ipc/...

# 运行所有 ipc 测试（含 Story 24.1）
go test -race -v ./ipc/...

# 带覆盖率运行
go test -race -cover -run "TestHandleChatCompletions_" ./ipc/...
```

---

## Red-Green-Refactor 工作流

### RED 阶段（当前） -- TEA 职责

- 所有 12 个测试已设计并记录
- 测试针对当前 501 stub 必然失败
- Mock 需求已明确
- 实现检查清单已创建

**验证:**
- 所有测试运行后失败（因为 handleChatCompletions 返回 501）
- 失败原因清晰：stub 未实现功能

### GREEN 阶段（DEV 团队）

1. 从实现检查清单中选择一个失败测试
2. 阅读测试，理解预期行为
3. 实现最小代码使该测试通过
4. 运行测试验证通过
5. 重复直到所有测试通过

### REFACTOR 阶段（DEV 团队）

1. 所有测试通过后审查代码质量
2. 消除重复代码
3. 确保重构后测试仍通过

---

## 下一步

1. 将此检查清单交付 DEV 工作流
2. 在 `ipc/http_openai_test.go` 中添加 12 个测试函数
3. 运行 `go test -race -run "TestHandleChatCompletions_" ./ipc/...` 确认 RED 阶段
4. 开始实现，逐个使测试通过
5. 全部通过后重构代码
6. 更新 `sprint-status.yaml` 状态

---

## 验证摘要

| 项目 | 值 |
|------|-----|
| Story ID | 24-2 |
| 主要测试级别 | 单元/集成（Go httptest） |
| 测试总数 | 12 |
| P0 测试 | 3 |
| P1 测试 | 7 |
| P2 测试 | 2 |
| 测试文件 | ipc/http_openai_test.go |
| Mock 需求 | 1（callableStubDriver） |
| AC 覆盖率 | 6/6（100%） |
| 输出文件 | _bmad-output/test-artifacts/atdd-checklist-24-2.md |

---

**Generated by BMad TEA Agent** - 2026-03-13
