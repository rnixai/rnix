---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/23-6-health-check-and-status.md'
  - 'drivers/llm/registry.go'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/openai_compat.go'
  - 'drivers/llm/factory.go'
  - 'drivers/llm/registry_test.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'kernel/atdd_23_5_provider_fallback_test.go'
  - 'drivers/llm/atdd_23_4_api_key_management_test.go'
---

# ATDD Checklist - Epic 23, Story 23-6: Provider 健康检查与状态报告

**Date:** 2026-03-12
**Author:** Decker
**Primary Test Level:** Unit / Integration (Go backend)

---

## Story Summary

daemon 启动时对 HTTP API 类 provider 执行轻量健康检查（GET /models），标记各 provider 的可用性状态。CLI 类 provider 跳过检查。健康状态通过 `rnix daemon status` 命令展示。

**As a** 运维人员
**I want** daemon 启动时对 HTTP API provider 执行健康检查
**So that** 我能及时知道哪些 provider 不可用

---

## Acceptance Criteria

1. **AC1**: HTTP API 类 provider 注册后，daemon 执行轻量健康检查（GET /models），单个检查耗时 <= 3 秒 (NFR32)
2. **AC2**: 健康检查失败时，daemon 正常启动（不拒绝启动），provider 标记为 unhealthy，日志输出 warning
3. **AC3**: CLI 类 provider（claude、cursor）跳过健康检查，状态保持 unchecked
4. **AC4**: `rnix daemon status` 显示所有已注册 provider 的状态（healthy / unhealthy / unchecked）

---

## Test Strategy

**Stack Detection:** `backend` (Go 1.26, `go.mod` detected)
**Generation Mode:** AI Generation（后端项目，无浏览器录制需求）

### Test Level Selection

| AC | Test Level | Rationale |
|----|-----------|-----------|
| AC1 | Unit + Integration | HealthCheck 方法 + RunHealthChecks 异步流程验证 |
| AC2 | Unit + Integration | 不可达端点、超时、HTTP 错误码、非阻塞行为验证 |
| AC3 | Unit | 类型断言验证 CLI driver 不实现 HealthChecker |
| AC4 | Unit + Integration | DriverRegistry.HealthStatuses + IPC ProviderStatus 方法 |

### Priority Assignment

| 测试 | Priority | Rationale |
|------|----------|-----------|
| AC1 HTTP 健康检查成功 | P0 | 核心功能 |
| AC1 调用 /models 端点 | P1 | 接口正确性 |
| AC1 超时限制 (NFR32) | P1 | 非功能需求 |
| AC2 不可达标记 unhealthy | P0 | 核心容错 |
| AC2 超时标记 unhealthy | P0 | 边界条件 |
| AC2 HTTP 401 标记 unhealthy | P1 | 错误分类 |
| AC2 非阻塞检查 | P0 | daemon 启动关键路径 |
| AC3 CLI 跳过检查 | P0 | 核心逻辑分支 |
| AC4 状态列表排序 | P0 | 状态查询功能 |
| AC4 默认 unchecked | P1 | 初始状态正确性 |
| AC4 IPC 查询 | P0 | 端到端可观测性 |

---

## Failing Tests Created (RED Phase)

### Integration/Acceptance Tests (14 tests)

**File:** `kernel/atdd_23_6_health_check_status_test.go` (165 lines)

#### AC1: HTTP API Provider 健康检查 (3 tests)

- **Test:** `TestATDD_23_6_AC1_HTTPProviderHealthCheck`
  - **Status:** RED (t.Skip) - HealthChecker 接口和 HealthCheck 方法未实现
  - **Verifies:** 健康 HTTP provider 注册后执行检查，标记为 healthy

- **Test:** `TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint`
  - **Status:** RED (t.Skip) - HealthCheck 方法未实现
  - **Verifies:** 健康检查调用 GET /models 端点并携带 API Key

- **Test:** `TestATDD_23_6_AC1_HealthCheckWithinTimeout`
  - **Status:** RED (t.Skip) - 超时限制未实现 (NFR32)
  - **Verifies:** 健康检查在 3 秒内完成

#### AC2: 健康检查失败不阻塞 Daemon (4 tests)

- **Test:** `TestATDD_23_6_AC2_UnreachableProvider`
  - **Status:** RED (t.Skip) - RunHealthChecks 和健康状态存储未实现
  - **Verifies:** 不可达端点标记为 unhealthy，daemon 正常运行

- **Test:** `TestATDD_23_6_AC2_HealthCheckTimeout`
  - **Status:** RED (t.Skip) - 超时处理未实现
  - **Verifies:** 超过 deadline 后标记 unhealthy

- **Test:** `TestATDD_23_6_AC2_HTTP401Unhealthy`
  - **Status:** RED (t.Skip) - HTTP 错误分类未实现
  - **Verifies:** HTTP 401 返回错误含 "HTTP 401"

- **Test:** `TestATDD_23_6_AC2_DaemonDoesNotBlock`
  - **Status:** RED (t.Skip) - RunHealthChecks 非阻塞行为未实现
  - **Verifies:** RunHealthChecks 函数返回耗时 < 100ms（不阻塞）

#### AC3: CLI Provider 跳过健康检查 (3 tests)

- **Test:** `TestATDD_23_6_AC3_CLIProviderSkipped`
  - **Status:** RED (t.Skip) - HealthChecker 可选接口未定义
  - **Verifies:** Claude CLI driver 不实现 HealthChecker，状态保持 unchecked

- **Test:** `TestATDD_23_6_AC3_CursorCLIProviderSkipped`
  - **Status:** RED (t.Skip) - HealthChecker 接口未定义
  - **Verifies:** Cursor CLI driver 不实现 HealthChecker

- **Test:** `TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker`
  - **Status:** RED (t.Skip) - HealthChecker 接口未定义
  - **Verifies:** OpenAICompatDriver 实现 HealthChecker 接口（正面验证）

#### AC4: Provider 状态查询 (3 tests)

- **Test:** `TestATDD_23_6_AC4_RegistryHealthStatuses`
  - **Status:** RED (t.Skip) - HealthStatuses() 方法和 ProviderStatus 类型未实现
  - **Verifies:** 多 provider 混合状态查询，返回按 name 排序的列表

- **Test:** `TestATDD_23_6_AC4_RegistryDefaultUnchecked`
  - **Status:** RED (t.Skip) - GetHealth 方法未实现
  - **Verifies:** 注册后默认健康状态为 unchecked

- **Test:** `TestATDD_23_6_AC4_IPCProviderStatusResponse`
  - **Status:** RED (t.Skip) - IPC MethodProviderStatus 和客户端方法未实现
  - **Verifies:** 通过 IPC 查询 provider 状态，返回 name/driver/health 列表

#### 集成测试 (1 test)

- **Test:** `TestATDD_23_6_Integration_MixedProviderHealthChecks`
  - **Status:** RED (t.Skip) - RunHealthChecks 函数未实现
  - **Verifies:** 混合 provider（healthy HTTP + broken HTTP + CLI）端到端健康检查

---

## Unit Tests (Implementation Phase)

以下单元测试将在实现阶段创建（不在 ATDD RED phase 范围内）：

### drivers/llm/registry_test.go (4 tests)

| 测试 | 场景 | 期望结果 |
|------|------|----------|
| `TestDriverRegistry_HealthStatus_DefaultUnchecked` | 注册 driver 后不设健康状态 | GetHealth 返回 unchecked |
| `TestDriverRegistry_SetHealth_Healthy` | SetHealth("x", Healthy) | GetHealth("x") 返回 healthy |
| `TestDriverRegistry_SetHealth_Unhealthy` | SetHealth("x", Unhealthy) | GetHealth("x") 返回 unhealthy |
| `TestDriverRegistry_HealthStatuses_Sorted` | 注册多个 driver | HealthStatuses() 按 name 排序 |

### drivers/llm/openai_compat_test.go (4 tests)

| 测试 | 场景 | 期望结果 |
|------|------|----------|
| `TestOpenAICompatDriver_HealthCheck_Success` | httptest 返回 200 | HealthCheck 返回 nil |
| `TestOpenAICompatDriver_HealthCheck_ServerDown` | 无可达服务器 | HealthCheck 返回 error |
| `TestOpenAICompatDriver_HealthCheck_HTTP401` | httptest 返回 401 | 错误含 "HTTP 401" |
| `TestOpenAICompatDriver_HealthCheck_Timeout` | 延迟 5 秒 + 1 秒 deadline | deadline exceeded |

### drivers/llm/factory_test.go (4 tests)

| 测试 | 场景 | 期望结果 |
|------|------|----------|
| `TestRunHealthChecks_HTTPProvider_Healthy` | 健康 httptest server | 状态变为 healthy |
| `TestRunHealthChecks_HTTPProvider_Unhealthy` | 不可达地址 | 状态变为 unhealthy |
| `TestRunHealthChecks_CLIProvider_Skipped` | Claude CLI driver | 状态保持 unchecked |
| `TestRunHealthChecks_NonBlocking` | 调用后立即返回 | 耗时 < 100ms |

---

## Test Infrastructure

### Mock Pattern

使用 `httptest.NewServer` + `llm.NewOpenAICompatDriver` 测试 HTTP API provider：

- **Healthy 端点:** 返回 200 + `{"data":[]}` JSON
- **Unhealthy 端点:** 不可达地址 `http://127.0.0.1:1`
- **超时端点:** `time.Sleep(5s)` 模拟慢响应
- **认证失败:** 返回 401 + error JSON

### Type Assertion 验证

- `drv.(llm.HealthChecker)` 验证 OpenAICompatDriver 实现可选接口
- `drv.(llm.HealthChecker)` 验证 ClaudeCliDriver / CursorCliDriver 不实现

### 异步等待

- `RunHealthChecks` 是异步的，测试使用 `time.Sleep` 等待状态变更
- 非阻塞测试验证函数返回耗时 < 100ms

---

## Implementation Checklist

### Task 1: DriverRegistry 增加健康状态存储

**Files:** `drivers/llm/registry.go`

**Tests to make pass:**
- `TestATDD_23_6_AC4_RegistryDefaultUnchecked`
- `TestATDD_23_6_AC4_RegistryHealthStatuses`

**Tasks:**

- [ ] 定义 `HealthStatus` 类型和 `HealthStatusHealthy`/`HealthStatusUnhealthy`/`HealthStatusUnchecked` 常量
- [ ] 在 `DriverRegistry` 新增 `health *xsync.SyncMap[string, HealthStatus]` 字段
- [ ] 实现 `SetHealth(name, status)` 方法
- [ ] 实现 `GetHealth(name) HealthStatus` 方法（默认返回 unchecked）
- [ ] 定义 `ProviderStatus` 结构体
- [ ] 实现 `HealthStatuses() []ProviderStatus` 方法（按 name 排序）
- [ ] Register 方法中自动设置默认 unchecked 状态
- [ ] 运行: `go test -race -run "TestDriverRegistry_Health" ./drivers/llm/...`

---

### Task 2: OpenAICompatDriver 增加 HealthCheck 方法

**Files:** `drivers/llm/driver.go`, `drivers/llm/openai_compat.go`

**Tests to make pass:**
- `TestATDD_23_6_AC1_HTTPProviderHealthCheck`
- `TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint`
- `TestATDD_23_6_AC1_HealthCheckWithinTimeout`
- `TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker`

**Tasks:**

- [ ] 在 `driver.go` 定义 `HealthChecker` 可选接口：`HealthCheck(ctx context.Context) error`
- [ ] 在 `openai_compat.go` 实现 `HealthCheck` 方法：GET baseURL+"/models"，携带 API Key
- [ ] HTTP 400+ 返回 `fmt.Errorf("HTTP %d", statusCode)`
- [ ] 确认编译时接口检查：`var _ HealthChecker = (*OpenAICompatDriver)(nil)`
- [ ] 运行: `go test -race -run "TestOpenAICompatDriver_HealthCheck" ./drivers/llm/...`

---

### Task 3: RunHealthChecks 异步健康检查

**Files:** `drivers/llm/factory.go`

**Tests to make pass:**
- `TestATDD_23_6_AC2_UnreachableProvider`
- `TestATDD_23_6_AC2_HealthCheckTimeout`
- `TestATDD_23_6_AC2_HTTP401Unhealthy`
- `TestATDD_23_6_AC2_DaemonDoesNotBlock`
- `TestATDD_23_6_AC3_CLIProviderSkipped`
- `TestATDD_23_6_Integration_MixedProviderHealthChecks`

**Tasks:**

- [ ] 实现 `RunHealthChecks(cfg, reg, timeout)` 函数
- [ ] 每个 provider 独立 goroutine，使用 `context.WithTimeout`
- [ ] 通过 `drv.(HealthChecker)` 类型断言检测是否支持健康检查
- [ ] CLI 类 driver 不实现 HealthChecker，自然跳过（状态保持 unchecked）
- [ ] 健康检查成功 → `reg.SetHealth(name, HealthStatusHealthy)`
- [ ] 健康检查失败 → `reg.SetHealth(name, HealthStatusUnhealthy)` + log.Printf warning
- [ ] 运行: `go test -race -run "TestRunHealthChecks" ./drivers/llm/...`

---

### Task 4: IPC ProviderStatus 方法

**Files:** `ipc/protocol.go`, `ipc/server.go`, `ipc/client.go`

**Tests to make pass:**
- `TestATDD_23_6_AC4_IPCProviderStatusResponse`

**Tasks:**

- [ ] 在 `protocol.go` 新增 `MethodProviderStatus` + `ProviderStatusResponse` + `ProviderStatusWire`
- [ ] 在 `server.go` 新增 `providerStatuses func() []ProviderStatusWire` 字段
- [ ] 新增 `SetProviderStatusFunc(fn)` setter
- [ ] 在 `handleConnection` switch 新增 `case MethodProviderStatus`
- [ ] 实现 `handleProviderStatus(conn)` handler
- [ ] 在 `client.go` 新增 `ProviderStatus() ([]ProviderStatusWire, error)` 方法
- [ ] 运行: `go test -race -run "TestATDD_23_6_AC4_IPC" ./kernel/...`

---

### Task 5: Daemon 集成

**Files:** `cmd/rnix/main.go`

**Tasks:**

- [ ] 在 `runDaemon` 中 `RegisterProviders` 之后调用 `llm.RunHealthChecks(cfg, reg, 3*time.Second)`
- [ ] 通过 `srv.SetProviderStatusFunc(driverReg.HealthStatuses)` 注入 provider 状态函数
- [ ] 在 `runDaemonStatus` 中新增 provider 状态输出
- [ ] 手动测试: `rnix daemon status` 显示 providers 列表

---

### Task 6: 移除 t.Skip，验证 GREEN Phase

**File:** `kernel/atdd_23_6_health_check_status_test.go`

**Tasks:**

- [ ] 移除所有 `t.Skip()` 调用
- [ ] 将注释中的 assertion 代码取消注释，转为实际 Go 代码
- [ ] 运行全量 ATDD: `go test -race -run "TestATDD_23_6" ./kernel/ -v`
- [ ] 运行回归: `go test -race ./kernel/... ./drivers/llm/... ./ipc/...`
- [ ] 所有 14 个 ATDD 测试 + 12 个单元测试全部通过

---

## Running Tests

```bash
# Run all ATDD tests for this story (RED phase - all SKIP)
go test -race -run "TestATDD_23_6" ./kernel/ -v

# Run specific AC tests
go test -race -run "TestATDD_23_6_AC1" ./kernel/ -v
go test -race -run "TestATDD_23_6_AC2" ./kernel/ -v
go test -race -run "TestATDD_23_6_AC3" ./kernel/ -v
go test -race -run "TestATDD_23_6_AC4" ./kernel/ -v

# Run unit tests for health check (after implementation)
go test -race -run "TestDriverRegistry_Health" ./drivers/llm/ -v
go test -race -run "TestOpenAICompatDriver_HealthCheck" ./drivers/llm/ -v
go test -race -run "TestRunHealthChecks" ./drivers/llm/ -v

# Full regression
go test -race ./kernel/... ./drivers/llm/... ./ipc/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 14 ATDD tests written and skipping (t.Skip)
- Test infrastructure documented (httptest patterns, type assertions)
- Implementation checklist created (6 tasks)
- All tests compile and pass `go vet`

**Verification:**

```
=== RUN   TestATDD_23_6_AC1_HTTPProviderHealthCheck
--- SKIP: TestATDD_23_6_AC1_HTTPProviderHealthCheck (0.00s)
=== RUN   TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint
--- SKIP: TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint (0.00s)
=== RUN   TestATDD_23_6_AC1_HealthCheckWithinTimeout
--- SKIP: TestATDD_23_6_AC1_HealthCheckWithinTimeout (0.00s)
=== RUN   TestATDD_23_6_AC2_UnreachableProvider
--- SKIP: TestATDD_23_6_AC2_UnreachableProvider (0.00s)
=== RUN   TestATDD_23_6_AC2_HealthCheckTimeout
--- SKIP: TestATDD_23_6_AC2_HealthCheckTimeout (0.00s)
=== RUN   TestATDD_23_6_AC2_HTTP401Unhealthy
--- SKIP: TestATDD_23_6_AC2_HTTP401Unhealthy (0.00s)
=== RUN   TestATDD_23_6_AC2_DaemonDoesNotBlock
--- SKIP: TestATDD_23_6_AC2_DaemonDoesNotBlock (0.00s)
=== RUN   TestATDD_23_6_AC3_CLIProviderSkipped
--- SKIP: TestATDD_23_6_AC3_CLIProviderSkipped (0.00s)
=== RUN   TestATDD_23_6_AC3_CursorCLIProviderSkipped
--- SKIP: TestATDD_23_6_AC3_CursorCLIProviderSkipped (0.00s)
=== RUN   TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker
--- SKIP: TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker (0.00s)
=== RUN   TestATDD_23_6_AC4_RegistryHealthStatuses
--- SKIP: TestATDD_23_6_AC4_RegistryHealthStatuses (0.00s)
=== RUN   TestATDD_23_6_AC4_RegistryDefaultUnchecked
--- SKIP: TestATDD_23_6_AC4_RegistryDefaultUnchecked (0.00s)
=== RUN   TestATDD_23_6_AC4_IPCProviderStatusResponse
--- SKIP: TestATDD_23_6_AC4_IPCProviderStatusResponse (0.00s)
=== RUN   TestATDD_23_6_Integration_MixedProviderHealthChecks
--- SKIP: TestATDD_23_6_Integration_MixedProviderHealthChecks (0.00s)
PASS
ok   github.com/rnixai/rnix/kernel  1.016s
```

**Summary:**

- Total tests: 14
- Passing: 0 (expected)
- Skipping: 14 (expected, RED phase)
- Status: RED phase verified

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Task 1:** 在 `drivers/llm/registry.go` 新增 HealthStatus 类型和 health SyncMap
2. **Task 2:** 在 `drivers/llm/driver.go` 定义 HealthChecker 接口，`openai_compat.go` 实现 HealthCheck
3. **Task 3:** 在 `drivers/llm/factory.go` 实现 RunHealthChecks 异步函数
4. **Task 4:** IPC 扩展 4 步流程（protocol → server → client → CLI）
5. **Task 5:** 在 `cmd/rnix/main.go` 集成健康检查和 daemon status 输出
6. **Task 6:** 移除 t.Skip，验证所有测试通过

---

### REFACTOR Phase (After All Tests Pass)

1. 审查 HealthCheck 和 RunHealthChecks 的错误处理
2. 确保 SyncMap 并发安全（健康检查 goroutine 写入，IPC handler 读取）
3. 验证 io.Copy(io.Discard, resp.Body) 防止连接泄漏
4. 运行全量回归: `go test -race ./kernel/... ./drivers/llm/... ./ipc/...`

---

## Next Steps

1. **Share this checklist** with the dev workflow
2. **Review** AC coverage: 4 AC mapped to 14 ATDD tests + 12 unit tests
3. **Run failing tests** to confirm RED phase: `go test -race -run "TestATDD_23_6" ./kernel/ -v`
4. **Begin implementation** following Task 1-6 order
5. **Remove t.Skip** incrementally as each task completes

---

## FRs / NFRs Covered

- **FR141**: 配置解析后的可用性验证
- **NFR32**: 单个健康检查耗时 <= 3 秒

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestATDD_23_6" ./kernel/ -v`

**Results:**

```
=== RUN   TestATDD_23_6_AC1_HTTPProviderHealthCheck
--- SKIP: TestATDD_23_6_AC1_HTTPProviderHealthCheck (0.00s)
=== RUN   TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint
--- SKIP: TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint (0.00s)
=== RUN   TestATDD_23_6_AC1_HealthCheckWithinTimeout
--- SKIP: TestATDD_23_6_AC1_HealthCheckWithinTimeout (0.00s)
=== RUN   TestATDD_23_6_AC2_UnreachableProvider
--- SKIP: TestATDD_23_6_AC2_UnreachableProvider (0.00s)
=== RUN   TestATDD_23_6_AC2_HealthCheckTimeout
--- SKIP: TestATDD_23_6_AC2_HealthCheckTimeout (0.00s)
=== RUN   TestATDD_23_6_AC2_HTTP401Unhealthy
--- SKIP: TestATDD_23_6_AC2_HTTP401Unhealthy (0.00s)
=== RUN   TestATDD_23_6_AC2_DaemonDoesNotBlock
--- SKIP: TestATDD_23_6_AC2_DaemonDoesNotBlock (0.00s)
=== RUN   TestATDD_23_6_AC3_CLIProviderSkipped
--- SKIP: TestATDD_23_6_AC3_CLIProviderSkipped (0.00s)
=== RUN   TestATDD_23_6_AC3_CursorCLIProviderSkipped
--- SKIP: TestATDD_23_6_AC3_CursorCLIProviderSkipped (0.00s)
=== RUN   TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker
--- SKIP: TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker (0.00s)
=== RUN   TestATDD_23_6_AC4_RegistryHealthStatuses
--- SKIP: TestATDD_23_6_AC4_RegistryHealthStatuses (0.00s)
=== RUN   TestATDD_23_6_AC4_RegistryDefaultUnchecked
--- SKIP: TestATDD_23_6_AC4_RegistryDefaultUnchecked (0.00s)
=== RUN   TestATDD_23_6_AC4_IPCProviderStatusResponse
--- SKIP: TestATDD_23_6_AC4_IPCProviderStatusResponse (0.00s)
=== RUN   TestATDD_23_6_Integration_MixedProviderHealthChecks
--- SKIP: TestATDD_23_6_Integration_MixedProviderHealthChecks (0.00s)
PASS
ok   github.com/rnixai/rnix/kernel  1.016s
```

**Summary:**

- Total tests: 14
- Passing: 0 (expected)
- Skipping: 14 (expected, RED phase)
- Status: RED phase verified

---

## Notes

- 测试遵循 kernel 包现有的 ATDD 模式（参考 `atdd_23_5_provider_fallback_test.go`）
- 使用 `httptest.NewServer` + `llm.NewOpenAICompatDriver` 进行 HTTP API provider mock
- 所有测试启用 `-race` 检测
- HealthChecker 是可选接口，通过类型断言检测——不修改 LLMDriver 主接口
- RunHealthChecks 异步执行，不阻塞 daemon 启动
- IPC 扩展遵循标准 4 步流程：protocol → server → client → CLI
- 健康状态存储在 DriverRegistry 的 SyncMap 中，保证并发安全

---

**Generated by BMad TEA Agent** - 2026-03-12
