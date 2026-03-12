---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-red-phase-verify'
lastStep: 'step-05-red-phase-verify'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/23-5-provider-fallback.md'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'agents/types.go'
  - 'drivers/llm/errors.go'
  - 'kernel/atdd_23_3_dynamic_provider_resolution_test.go'
  - 'kernel/kernel_test.go'
---

# ATDD Checklist - Epic 23, Story 23-5: Provider Fallback 降级机制

**Date:** 2026-03-12
**Author:** Decker
**Primary Test Level:** Unit / Integration (Go backend)

---

## Story Summary

当首选 LLM provider 调用失败时，自动切换到备选 provider，确保智能体任务不因单个 provider 故障而中断。支持同 provider 内模型降级（sonnet -> haiku）和跨 provider 降级（ollama -> claude）。

**As a** 用户
**I want** 当首选 provider 调用失败时自动切换到备选 provider
**So that** 智能体任务不会因单个 provider 故障而中断

---

## Acceptance Criteria

1. **AC1**: 同 provider 内模型降级 - preferred 模型 ErrModelNotFound 时自动使用 fallback 模型重试
2. **AC2**: 跨 provider fallback - 主 provider 失败（HTTP 5xx、连接超时、连接拒绝、认证失败）时自动切换到 fallback provider，切换延迟 <= 1 秒 (NFR33)
3. **AC3**: 所有 provider 均不可用时进程转为 Zombie 状态，错误信息包含所有尝试过的 provider 列表和各自失败原因
4. **AC4**: strace 输出中可见 provider 切换事件
5. **AC5**: Agent 未配置 fallback 时直接报错，不尝试 fallback

---

## Test Strategy

**Stack Detection:** `backend` (Go 1.26, `go.mod` detected)
**Generation Mode:** AI Generation（后端项目，无浏览器录制需求）

### Test Level Selection

| AC | Test Level | Rationale |
|----|-----------|-----------|
| AC1 | Unit + Integration | 同 provider 内模型降级，需 mock VFS 设备验证 |
| AC2 | Unit + Integration | 跨 provider 切换，需双设备 mock + 延迟测量 |
| AC3 | Integration | 完整错误链验证，需进程完整生命周期 |
| AC4 | Integration | strace 事件验证，需 DebugChan 检查 |
| AC5 | Unit + Integration | 无 fallback 快速失败路径验证 |

### Priority Assignment

| 测试 | Priority | Rationale |
|------|----------|-----------|
| AC1 同 provider fallback | P0 | 核心功能，最常见降级场景 |
| AC2 跨 provider fallback | P0 | 核心功能，跨 provider 容灾 |
| AC2 延迟验证 (NFR33) | P1 | 非功能需求，性能指标 |
| AC3 全部失败错误链 | P0 | 错误可观测性，调试关键 |
| AC4 strace 事件 | P1 | 可观测性，非核心功能 |
| AC5 无 fallback 快速失败 | P0 | 向后兼容，防止意外行为 |

---

## Failing Tests Created (RED Phase)

### Unit/Integration Tests (15 tests)

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

#### AC1: 同 Provider 内模型降级 (2 tests)

- **Test:** `TestATDD_23_5_AC1_SameProviderFallback`
  - **Status:** RED (t.Skip) - Fallback mechanism not yet implemented in reasonStep
  - **Verifies:** preferred 模型 ErrModelNotFound 时自动使用 fallback 模型重试

- **Test:** `TestATDD_23_5_AC1_SameProviderModelDowngrade`
  - **Status:** RED (t.Skip) - Fallback mechanism not yet implemented
  - **Verifies:** 同一 provider 设备，model 字段切换为 fallback model

#### AC2: 跨 Provider Fallback (4 tests)

- **Test:** `TestATDD_23_5_AC2_CrossProviderFallback`
  - **Status:** RED (t.Skip) - Cross-provider fallback not yet implemented
  - **Verifies:** 主 provider HTTP 500 时自动切换到 fallback provider

- **Test:** `TestATDD_23_5_AC2_ConnectionRefused`
  - **Status:** RED (t.Skip) - Fallback on connection refused not yet implemented
  - **Verifies:** 连接拒绝错误触发 fallback

- **Test:** `TestATDD_23_5_AC2_AuthFailure`
  - **Status:** RED (t.Skip) - Fallback on auth failure not yet implemented
  - **Verifies:** ErrAuth 错误触发 fallback

- **Test:** `TestATDD_23_5_AC2_FallbackLatency`
  - **Status:** RED (t.Skip) - Fallback latency measurement not yet implemented (NFR33)
  - **Verifies:** 切换延迟 <= 1 秒

#### AC3: 全部 Provider 失败 (2 tests)

- **Test:** `TestATDD_23_5_AC3_AllProvidersExhausted`
  - **Status:** RED (t.Skip) - All-providers-exhausted error chain not yet implemented
  - **Verifies:** primary + fallback 均失败时进程 Zombie，错误含两个 provider 信息

- **Test:** `TestATDD_23_5_AC3_ErrorContainsBothProviders`
  - **Status:** RED (t.Skip) - Comprehensive error chain not yet implemented
  - **Verifies:** 错误消息包含两个 provider 名称和各自失败原因

#### AC4: Strace 事件 (2 tests)

- **Test:** `TestATDD_23_5_AC4_StraceShowsFallback`
  - **Status:** RED (t.Skip) - Fallback strace event emission not yet implemented
  - **Verifies:** DebugChan 收到 fallback 事件，含 primary_device、fallback_device、primary_error

- **Test:** `TestATDD_23_5_AC4_StraceShowsExhausted`
  - **Status:** RED (t.Skip) - Fallback exhausted strace event not yet implemented
  - **Verifies:** DebugChan 收到 fallback_exhausted 事件，含双方错误信息

#### AC5: 无 Fallback 配置 (2 tests)

- **Test:** `TestATDD_23_5_AC5_NoFallbackConfigured`
  - **Status:** RED (t.Skip) - No-fallback fast-fail path not yet implemented
  - **Verifies:** fallback 为空时主 provider 失败直接报错

- **Test:** `TestATDD_23_5_AC5_EmptyFallbackNoRetry`
  - **Status:** RED (t.Skip) - Explicit no-retry verification not yet implemented
  - **Verifies:** 无 fallback 时不产生 fallback 相关 strace 事件

#### 边界场景 (3 tests)

- **Test:** `TestATDD_23_5_FallbackProviderNotRegistered`
  - **Status:** RED (t.Skip) - Fallback provider resolution not yet implemented
  - **Verifies:** fallback provider 未注册时行为等同 AC5

- **Test:** `TestATDD_23_5_AgentModels_FallbackProvider_YAMLParsing`
  - **Status:** RED (t.Skip) - FallbackProvider field not yet added
  - **Verifies:** AgentModels 结构体新增 FallbackProvider 字段

- **Test:** `TestATDD_23_5_Process_FallbackFields`
  - **Status:** RED (t.Skip) - Fallback fields not yet added to Process
  - **Verifies:** Process 结构体新增 FallbackModel, FallbackProvider, FallbackDevice 字段

---

## Test Infrastructure

### Mock Pattern

使用 kernel 包现有的 `mockLLMFile` + `vfs.NewDeviceRegistry()` 测试模式：

- **Primary 设备:** `writeErr` 设置为 LLMError 模拟 provider 故障
- **Fallback 设备:** `readData` 设置为成功响应
- **双设备注册:** 两个 mock 设备注册到不同的 VFS 路径
- **Provider Resolver:** 通过 `SetProviderResolver` 注册两个 provider

### Helper Functions

- `newFallbackTestKernel(t, primaryFile, fallbackFile, primaryName, fallbackName)` - 创建带双 mock 设备的 kernel
- `fallbackAgentInfo(provider, preferred, fallback, fallbackProvider)` - 创建带 fallback 配置的 AgentInfo

### Strace 验证

- 通过 `proc.DebugChan` 读取 `types.SyscallEvent`
- 检查 `evt.Args["action"]` 为 `"fallback"` 或 `"fallback_exhausted"`
- 验证事件包含 `primary_device`、`fallback_device`、`primary_error` 等字段

---

## Implementation Checklist

### Test: TestATDD_23_5_AgentModels_FallbackProvider_YAMLParsing

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] 在 `agents/types.go` 的 `AgentModels` 结构体中新增 `FallbackProvider string` 字段
- [ ] 新增 testdata fixture `agents/testdata/cross-provider-agent/agent.yaml`
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AgentModels ./kernel/...`
- [ ] Test passes (green phase)

---

### Test: TestATDD_23_5_Process_FallbackFields

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/process.go` 的 `Process` 结构体中新增 `FallbackModel`, `FallbackProvider`, `FallbackDevice` 字段
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_Process ./kernel/...`
- [ ] Test passes (green phase)

---

### Test: TestATDD_23_5_AC1_SameProviderFallback & AC1_SameProviderModelDowngrade

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/kernel.go` Spawn 中解析 fallback 配置并存入 Process
- [ ] 实现 `attemptFallback` 辅助函数
- [ ] 在 `reasonStep` 的 LLM Write 失败路径中调用 `attemptFallback`
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AC1 ./kernel/...`
- [ ] Tests pass (green phase)

---

### Test: TestATDD_23_5_AC2_* (4 tests)

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] `attemptFallback` 支持跨 provider 设备切换
- [ ] 处理多种错误类型（HTTP 5xx、连接拒绝、ErrAuth）
- [ ] 确保切换延迟 <= 1 秒 (NFR33)
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AC2 ./kernel/...`
- [ ] Tests pass (green phase)

---

### Test: TestATDD_23_5_AC3_* (2 tests)

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] 当 `attemptFallback` 也失败时，构建包含两个 provider 信息的完整错误链
- [ ] `finishProcess` 的 `ExitStatus.Err` 包含 `fmt.Errorf("primary %s: %v; fallback %s: %v", ...)`
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AC3 ./kernel/...`
- [ ] Tests pass (green phase)

---

### Test: TestATDD_23_5_AC4_* (2 tests)

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] 在 `attemptFallback` 中通过 `k.emitEvent` 发出 `action: "fallback"` 事件
- [ ] 在 fallback 也失败时发出 `action: "fallback_exhausted"` 事件
- [ ] 事件包含 `primary_device`, `fallback_device`, `primary_error`, `fallback_model` 等字段
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AC4 ./kernel/...`
- [ ] Tests pass (green phase)

---

### Test: TestATDD_23_5_AC5_* (2 tests)

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] `attemptFallback` 在 `proc.FallbackDevice == ""` 时直接返回 primaryErr
- [ ] 确保无 fallback 配置时不产生 fallback 相关 strace 事件
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_AC5 ./kernel/...`
- [ ] Tests pass (green phase)

---

### Test: TestATDD_23_5_FallbackProviderNotRegistered

**File:** `kernel/atdd_23_5_provider_fallback_test.go`

**Tasks to make this test pass:**

- [ ] Spawn 中 fallback provider 的 `resolveLLMDevice` 失败时不阻断 spawn
- [ ] 行为等同 AC5（无 fallback 可用）
- [ ] 移除 t.Skip，运行测试: `go test -race -run TestATDD_23_5_FallbackProvider ./kernel/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestATDD_23_5" ./kernel/ -v

# Run specific AC tests
go test -race -run "TestATDD_23_5_AC1" ./kernel/ -v
go test -race -run "TestATDD_23_5_AC2" ./kernel/ -v
go test -race -run "TestATDD_23_5_AC3" ./kernel/ -v
go test -race -run "TestATDD_23_5_AC4" ./kernel/ -v
go test -race -run "TestATDD_23_5_AC5" ./kernel/ -v

# Run all kernel tests (regression)
go test -race ./kernel/...

# Run with verbose output
go test -race -run "TestATDD_23_5" ./kernel/ -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 15 tests written and skipping (t.Skip)
- Test infrastructure created (helper functions, mock patterns)
- Implementation checklist created
- All tests compile and pass `go vet`

**Verification:**

```
=== RUN   TestATDD_23_5_AC1_SameProviderFallback
--- SKIP: TestATDD_23_5_AC1_SameProviderFallback (0.00s)
... (all 15 tests SKIP)
PASS
ok   github.com/rnixai/rnix/kernel  1.012s
```

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Task 1:** Add `FallbackProvider` field to `agents/types.go` (AgentModels)
2. **Task 2:** Add fallback fields to `kernel/process.go` (Process struct)
3. **Task 3:** Parse fallback config in `kernel/kernel.go` Spawn function
4. **Task 4:** Implement `attemptFallback` helper in `kernel/kernel.go`
5. **Task 5:** Integrate fallback into `reasonStep` Write failure path
6. **Task 6:** Add strace event emission for fallback events
7. **Remove t.Skip** one group at a time, verify each group passes
8. **Run full suite:** `go test -race -run "TestATDD_23_5" ./kernel/ -v`

---

### REFACTOR Phase (After All Tests Pass)

1. Review `attemptFallback` for code clarity
2. Ensure no resource leaks (fallback FD properly closed)
3. Verify thread safety (fallback fields set at spawn, read-only in reasonStep)
4. Run full regression: `go test -race ./kernel/...`

---

## Next Steps

1. **Share this checklist** with the dev workflow
2. **Review** AC coverage: 5 AC mapped to 15 tests
3. **Run failing tests** to confirm RED phase: `go test -race -run "TestATDD_23_5" ./kernel/ -v`
4. **Begin implementation** following Task 1-6 order
5. **Remove t.Skip** incrementally as each task completes

---

## FRs / NFRs Covered

- **FR144**: Provider Fallback 降级机制
- **NFR33**: Fallback 切换延迟 <= 1 秒

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestATDD_23_5" ./kernel/ -v`

**Results:**

```
=== RUN   TestATDD_23_5_AC1_SameProviderFallback
--- SKIP: TestATDD_23_5_AC1_SameProviderFallback (0.00s)
=== RUN   TestATDD_23_5_AC1_SameProviderModelDowngrade
--- SKIP: TestATDD_23_5_AC1_SameProviderModelDowngrade (0.00s)
=== RUN   TestATDD_23_5_AC2_CrossProviderFallback
--- SKIP: TestATDD_23_5_AC2_CrossProviderFallback (0.00s)
=== RUN   TestATDD_23_5_AC2_ConnectionRefused
--- SKIP: TestATDD_23_5_AC2_ConnectionRefused (0.00s)
=== RUN   TestATDD_23_5_AC2_AuthFailure
--- SKIP: TestATDD_23_5_AC2_AuthFailure (0.00s)
=== RUN   TestATDD_23_5_AC2_FallbackLatency
--- SKIP: TestATDD_23_5_AC2_FallbackLatency (0.00s)
=== RUN   TestATDD_23_5_AC3_AllProvidersExhausted
--- SKIP: TestATDD_23_5_AC3_AllProvidersExhausted (0.00s)
=== RUN   TestATDD_23_5_AC3_ErrorContainsBothProviders
--- SKIP: TestATDD_23_5_AC3_ErrorContainsBothProviders (0.00s)
=== RUN   TestATDD_23_5_AC4_StraceShowsFallback
--- SKIP: TestATDD_23_5_AC4_StraceShowsFallback (0.00s)
=== RUN   TestATDD_23_5_AC4_StraceShowsExhausted
--- SKIP: TestATDD_23_5_AC4_StraceShowsExhausted (0.00s)
=== RUN   TestATDD_23_5_AC5_NoFallbackConfigured
--- SKIP: TestATDD_23_5_AC5_NoFallbackConfigured (0.00s)
=== RUN   TestATDD_23_5_AC5_EmptyFallbackNoRetry
--- SKIP: TestATDD_23_5_AC5_EmptyFallbackNoRetry (0.00s)
=== RUN   TestATDD_23_5_FallbackProviderNotRegistered
--- SKIP: TestATDD_23_5_FallbackProviderNotRegistered (0.00s)
=== RUN   TestATDD_23_5_AgentModels_FallbackProvider_YAMLParsing
--- SKIP: TestATDD_23_5_AgentModels_FallbackProvider_YAMLParsing (0.00s)
=== RUN   TestATDD_23_5_Process_FallbackFields
--- SKIP: TestATDD_23_5_Process_FallbackFields (0.00s)
PASS
ok   github.com/rnixai/rnix/kernel  1.012s
```

**Summary:**

- Total tests: 15
- Passing: 0 (expected)
- Skipping: 15 (expected, RED phase)
- Status: RED phase verified

---

## Notes

- 测试遵循 kernel 包现有的 ATDD 模式（参考 `atdd_23_3_dynamic_provider_resolution_test.go`）
- 使用 `mockLLMFile` + `vfs.NewDeviceRegistry()` 进行 VFS 设备 mock
- 所有测试启用 `-race` 检测
- Fallback 逻辑在 `reasonStep` 中实现，不修改 VFS 层或 driver 层
- `FallbackProvider` 字段为空时使用主 provider（同 provider 内降级）

---

**Generated by BMad TEA Agent** - 2026-03-12
