---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/24-4-models-endpoint-provider-discovery.md'
  - 'ipc/http_openai.go'
  - 'ipc/http_openai_test.go'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/registry.go'
  - '_bmad-output/planning-artifacts/epics/epic-24-llm-serve-openai兼容网关-llm-serve-gateway.md'
  - '_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md'
---

# ATDD Checklist - Epic 24, Story 4: /v1/models Provider 发现

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit（Go `testing` + `httptest`）
**Detected Stack:** backend（Go 1.26）

---

## Story Summary

实现 OpenAI 兼容的 `/v1/models` GET 端点，替换当前的 501 stub，返回所有已注册且健康的 LLM provider 及其模型列表。外部工具（如 Open WebUI）可通过此端点自动发现可用模型。

**As a** 外部工具用户
**I want** 通过 `/v1/models` 端点发现所有可用的 LLM provider 和模型
**So that** 工具（如 Open WebUI）可以自动发现并展示可用模型列表

---

## Acceptance Criteria

1. **AC1**: GET `/v1/models` 返回所有已注册且健康的 provider 及其可用模型列表，响应格式兼容 OpenAI Models API（FR149）
2. **AC2**: 响应包含 `object: "list"` 和 `data` 数组，每个 entry 包含 `id`、`object: "model"`、`created`、`owned_by`
3. **AC3**: 多个 provider 已注册时，返回包含所有 provider 的模型列表
4. **AC4**: unhealthy provider 不返回，healthy 和 unchecked provider 正常列出

---

## Failing Tests Created (RED Phase)

### Unit Tests（8 tests）

**File:** `ipc/http_openai_test.go`

所有测试在 RED 阶段将失败，因为 `handleListModels` 当前返回 HTTP 501（Not Implemented）。

- **Test:** `TestListModels_Basic`
  - **Priority:** P0
  - **Status:** RED - handler 返回 501 而非 200 + 有效 JSON
  - **Verifies:** AC1, AC3 — 注册 2 个 provider 后 GET /v1/models 返回 200 + `object: "list"` + data 数组含所有 provider
  - **Setup:** 创建含 2 个 stubDriver 的 registry（claude + ollama）
  - **Assert:** HTTP 200, 响应 body 可解析为 `ModelListResponse`，`Object == "list"`，`Data` 数组长度 >= 2

- **Test:** `TestListModels_ResponseFormat`
  - **Priority:** P0
  - **Status:** RED - 501 响应无法解析为 ModelListResponse
  - **Verifies:** AC2 — 每个 entry 的字段格式正确
  - **Setup:** 创建含 1 个 stubDriver 的 registry，设置 `DefaultModel: "llama3"`
  - **Assert:** entry.ID 非空，entry.Object == "model"，entry.Created > 0，entry.OwnedBy 非空

- **Test:** `TestListModels_ModelID`
  - **Priority:** P1
  - **Status:** RED - 无有效响应
  - **Verifies:** AC2, AC3 — provider 名和 provider:model 格式的 entry
  - **Setup:** 创建含 1 个 stubDriver 的 registry（name="ollama"，DefaultModel="llama3"）
  - **Assert:** data 数组包含 id="ollama" 的 entry 和 id="ollama:llama3" 的 entry

- **Test:** `TestListModels_HealthFiltering`
  - **Priority:** P0
  - **Status:** RED - handler 未实现健康过滤
  - **Verifies:** AC4 — unhealthy provider 被排除
  - **Setup:** 注册 2 个 driver（alpha + beta），标记 beta 为 `HealthStatusUnhealthy`
  - **Assert:** 返回列表仅包含 alpha，不包含 beta

- **Test:** `TestListModels_UncheckedHealth`
  - **Priority:** P1
  - **Status:** RED - 无有效响应
  - **Verifies:** AC4 — unchecked provider 包含在列表中
  - **Setup:** 注册 1 个 driver（初始状态为 unchecked）
  - **Assert:** 返回列表包含该 provider

- **Test:** `TestListModels_EmptyRegistry`
  - **Priority:** P1
  - **Status:** RED - 空 registry 也返回 501
  - **Verifies:** 边界情况 — 空 registry 返回空列表
  - **Setup:** `llm.NewDriverRegistry()`（空 registry）
  - **Assert:** HTTP 200，`object: "list"`，`data: []`（空数组）

- **Test:** `TestListModels_ContentType`
  - **Priority:** P1
  - **Status:** RED - Content-Type 不匹配
  - **Verifies:** AC1 — 响应 Content-Type 为 application/json
  - **Setup:** 标准 registry
  - **Assert:** `w.Header().Get("Content-Type")` 包含 "application/json"

- **Test:** `TestListModels_Sorted`
  - **Priority:** P2
  - **Status:** RED - 无有效响应可验证排序
  - **Verifies:** 质量 — 返回的 model 列表按 ID 排序
  - **Setup:** 注册 3 个 driver（zeta, alpha, middle），验证 data 数组按 ID 字母序排列
  - **Assert:** data[0].ID < data[1].ID < data[2].ID（或检查字母序）

---

## Data Factories Created

### Go 测试 Helper（复用已有）

**File:** `ipc/http_openai_test.go`

**已有可复用的 helpers:**

- `stubDriver` — 实现 LLMDriver 接口，`Info()` 返回可配置的 `DriverInfo`
- `newTestRegistry()` — 注册 "claude" 和 "ollama" 两个 stubDriver

**新增 Helper（建议）:**

- `newModelsTestServer(reg *llm.DriverRegistry) *OpenAIServer` — 创建测试用 OpenAIServer 实例
- `doModelsRequest(s *OpenAIServer) *httptest.ResponseRecorder` — 发送 GET /v1/models 请求并返回 recorder

---

## Fixtures Created

### 无需额外 Fixtures

Go 测试使用 `httptest.NewRecorder()` + `http.NewRequest()` 模式，无需外部 fixtures。

---

## Mock Requirements

### 无外部服务 Mock

`/v1/models` 端点仅读取 `DriverRegistry` 内存状态，不调用外部服务。使用 `stubDriver` 即可满足所有测试需求。

---

## Required data-testid Attributes

### 不适用

纯后端 API 端点，无 UI 组件。

---

## Implementation Checklist

### Test: TestListModels_Basic

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 定义 `ModelListResponse` 类型（Object string + Data []ModelEntry）
- [ ] 定义 `ModelEntry` 类型（ID, Object, Created, OwnedBy）
- [ ] 替换 `handleListModels` 方法体，遍历 `driverReg.Names()` 构建 entry 列表
- [ ] 返回 `ModelListResponse{Object: "list", Data: entries}` 为 JSON
- [ ] Run test: `go test -race -run TestListModels_Basic ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_ResponseFormat

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 确保 `ModelEntry` JSON tag 正确（`id`, `object`, `created`, `owned_by`）
- [ ] 确保 `entry.Object` 固定为 `"model"`
- [ ] 确保 `entry.Created` 为有效时间戳
- [ ] 确保 `entry.OwnedBy` 为 provider 名
- [ ] Run test: `go test -race -run TestListModels_ResponseFormat ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_ModelID

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 基础 entry：ID = provider 名（如 "ollama"）
- [ ] 如果 `driver.Info().DefaultModel` 非空，额外生成 ID = "provider:model" 的 entry
- [ ] Run test: `go test -race -run TestListModels_ModelID ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_HealthFiltering

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 在遍历 provider 时调用 `driverReg.GetHealth(name)` 检查状态
- [ ] 如果 `HealthStatusUnhealthy`，跳过该 provider
- [ ] Run test: `go test -race -run TestListModels_HealthFiltering ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_UncheckedHealth

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] `HealthStatusUnchecked` 和 `HealthStatusHealthy` 都包含在返回列表中
- [ ] Run test: `go test -race -run TestListModels_UncheckedHealth ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_EmptyRegistry

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 空 registry 时 `Names()` 返回空切片
- [ ] 返回 `{"object":"list","data":[]}` 而非错误
- [ ] Run test: `go test -race -run TestListModels_EmptyRegistry ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_ContentType

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 设置 `w.Header().Set("Content-Type", "application/json")`
- [ ] Run test: `go test -race -run TestListModels_ContentType ./ipc/...`
- [ ] Test passes (green phase)

---

### Test: TestListModels_Sorted

**File:** `ipc/http_openai_test.go`

**Tasks to make this test pass:**

- [ ] 使用 `sort.Slice` 或利用 `Names()` 已排序特性确保输出排序
- [ ] Run test: `go test -race -run TestListModels_Sorted ./ipc/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run TestListModels ./ipc/...

# Run specific test
go test -race -run TestListModels_Basic ./ipc/...

# Run with verbose output
go test -race -v -run TestListModels ./ipc/...

# Run all ipc tests (regression check)
go test -race ./ipc/...

# Run with coverage
go test -race -cover -run TestListModels ./ipc/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 8 test scenarios defined with expected behavior
- Test helpers identified (stubDriver, newTestRegistry)
- Implementation checklist maps each test to concrete tasks
- All tests will fail due to handleListModels 501 stub

**Verification:**

- All tests run and fail as expected (501 instead of 200)
- Failure messages are clear: expected 200, got 501
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. Define `ModelListResponse` and `ModelEntry` types
2. Implement `handleListModels` replacing 501 stub
3. Add health filtering logic
4. Write all 8 test functions
5. Run `go test -race ./ipc/...` to verify all pass

---

### REFACTOR Phase (After All Tests Pass)

- Verify all existing tests still pass (regression)
- Check code for consistency with Story 24.1/24.2/24.3 patterns
- Run `make all` for full quality gate

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run TestListModels ./ipc/...`

**Expected Results:**

```
--- FAIL: TestListModels_Basic
    Expected status 200, got 501
--- FAIL: TestListModels_ResponseFormat
    Expected status 200, got 501
--- FAIL: TestListModels_HealthFiltering
    Expected status 200, got 501
...
FAIL
```

**Summary:**

- Total tests: 8
- Passing: 0 (expected)
- Failing: 8 (expected - all due to 501 stub)
- Status: RED phase verified

---

## Notes

- Go 后端项目无需 Playwright/E2E 浏览器测试
- 所有测试在 `ipc/http_openai_test.go` 中，与已有测试模式一致
- 使用 `httptest.NewRecorder()` + `buildMux().ServeHTTP()` 模式
- 竞态检测（`-race`）始终启用
- `stubDriver.Info()` 已返回 `DriverInfo`，包含 `DefaultModel` 字段

---

**Generated by BMad TEA Agent** - 2026-03-13
