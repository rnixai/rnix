---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-10'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/19-2-intent-state-model-and-event-driven-reconciler.md
  - intent/types.go
  - intent/engine.go
  - intent/manager.go
  - intent/decomposer.go
  - intent/engine_test.go
  - intent/types_test.go
  - intent/manager_test.go
  - intent/decomposer_test.go
  - ipc/protocol.go
  - ipc/server.go
  - ipc/intent_adapter.go
  - internal/ui/intent.go
  - internal/ui/intent_test.go
---

# ATDD Checklist - Epic 19, Story 19.2: 意图状态模型与事件驱动 Reconciler

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

作为应用开发者，系统维护意图状态模型并通过事件驱动的 Reconciler 自动调和差异，子任务失败或超时时系统自动处理，无需手动干预。

**As a** 应用开发者
**I want** 系统维护意图状态模型并通过事件驱动的 Reconciler 自动调和差异
**So that** 子任务失败或超时时系统自动处理，无需我手动干预

---

## Acceptance Criteria

1. **AC#1** 维护 Desired/Current/Drift 三态模型
2. **AC#2** 子任务失败时自动重新规划和重试，drift→action 延迟 ≤ 5s (NFR40)
3. **AC#3** 子任务成功时更新 Current，检查并启动后续任务
4. **AC#4** 重试次数达上限时标记最终失败，级联标记依赖下游
5. **AC#5** 子任务超时时先终止进程，再按重试策略处理
6. **AC#6** `rnix intent status` 包含 drift 列表、重试计数、Desired/Current 对比

---

## Failing Tests Created (RED Phase)

### Unit Tests — intent/reconciler_test.go (12 tests)

**File:** `intent/reconciler_test.go` (约 540 行)

- ✅ **Test:** `TestReconciler_Execute_AllSuccess`
  - **Status:** RED — Execute 是 no-op stub，节点保持 pending 状态
  - **Verifies:** AC#2, AC#3 — Reconciler 执行所有节点到完成

- ✅ **Test:** `TestReconciler_Execute_RetrySuccess`
  - **Status:** RED — 未实现重试逻辑，节点保持 pending
  - **Verifies:** AC#2 — 节点失败后自动重试并最终成功

- ✅ **Test:** `TestReconciler_Execute_RetryExhausted`
  - **Status:** RED — 未实现重试耗尽→最终失败逻辑
  - **Verifies:** AC#4 — 重试次数达上限后标记失败+级联

- ✅ **Test:** `TestReconciler_Execute_Timeout`
  - **Status:** RED — 未实现超时检测和重试
  - **Verifies:** AC#5 — 节点超时后重试

- ✅ **Test:** `TestReconciler_Execute_TimeoutExhausted`
  - **Status:** RED — 未实现超时耗尽逻辑
  - **Verifies:** AC#5 — 超时多次后最终失败

- ✅ **Test:** `TestReconciler_Execute_CascadeAfterExhausted`
  - **Status:** RED — 未实现级联失败
  - **Verifies:** AC#4 — 重试耗尽后级联所有下游节点

- ✅ **Test:** `TestReconciler_Execute_ParallelWithRetry`
  - **Status:** RED — 未实现并行执行+选择性重试
  - **Verifies:** AC#2 — 并行节点中一个重试不影响另一个

- ✅ **Test:** `TestReconciler_Execute_ContextCancel`
  - **Status:** RED — Execute 不响应 ctx 取消
  - **Verifies:** 基础 — context 取消时正确停止

- ✅ **Test:** `TestReconciler_Execute_DriftDetectedCallback`
  - **Status:** RED — 未触发 OnDriftDetected 回调
  - **Verifies:** AC#1, AC#2 — drift 事件正确回调

- ✅ **Test:** `TestReconciler_Execute_DriftResolvedCallback`
  - **Status:** RED — 未触发 OnDriftResolved 回调
  - **Verifies:** AC#2 — 重试成功后 drift 清除

- ✅ **Test:** `TestReconciler_Execute_NFR40_Latency`
  - **Status:** RED — 未触发 OnNodeFailed/OnNodeRetry 回调
  - **Verifies:** NFR40 — drift→action 延迟 ≤ 5s

- ✅ **Test:** `TestReconciler_Callbacks`
  - **Status:** RED — 未触发任何回调
  - **Verifies:** 基础 — 所有回调类型均被调用

### Unit Tests — intent/types_test.go 新增 (7 tests)

**File:** `intent/types_test.go` (新增约 140 行)

- ✅ **Test:** `TestIntentNode_CanRetry`
  - **Status:** RED — stub 返回 false，预期 true
  - **Verifies:** AC#2 — 重试次数未超限返回 true

- ⚠️ **Test:** `TestIntentNode_CanRetry_Exhausted`
  - **Status:** PASS (coincidental) — stub 返回 false，测试预期 false
  - **Verifies:** AC#4 — 重试次数达上限返回 false

- ✅ **Test:** `TestIntentNode_IncrRetry`
  - **Status:** RED — stub 不修改 RetryCount
  - **Verifies:** AC#2 — 重试计数递增 + LastFailedAt 更新

- ✅ **Test:** `TestIntentTree_InitDesired`
  - **Status:** RED — stub 不初始化 DesiredNodes
  - **Verifies:** AC#1 — 所有节点 desired 为 completed

- ✅ **Test:** `TestIntentTree_ComputeDrifts`
  - **Status:** RED — stub 返回 nil
  - **Verifies:** AC#1 — 正确计算差异

- ✅ **Test:** `TestIntentTree_AddDrift_ClearDrift`
  - **Status:** RED — stub 不操作 Drifts 切片
  - **Verifies:** AC#1 — drift 增删

- ✅ **Test:** `TestIntentTree_ActiveDrifts`
  - **Status:** RED — stub 返回 nil
  - **Verifies:** AC#6 — 返回未解决 drift

### Unit Tests — intent/decomposer_test.go 新增 (1 test)

**File:** `intent/decomposer_test.go` (新增约 25 行)

- ✅ **Test:** `TestDecomposer_Decompose_InitDesired`
  - **Status:** RED — Decompose 不调用 InitDesired
  - **Verifies:** AC#1 — 分解后 DesiredNodes 已初始化

### UI Render Tests — internal/ui/intent_test.go 新增 (6 tests)

**File:** `internal/ui/intent_test.go` (新增约 100 行)

- ✅ **Test:** `TestRenderIntentNodeRetry_TTY`
  - **Status:** RED — TTY stub 不输出内容
  - **Verifies:** AC#6 — 重试事件渲染

- ⚠️ **Test:** `TestRenderIntentNodeRetry_JSON`
  - **Status:** PASS — JSON 路径已在 stub 中实现
  - **Verifies:** AC#6 — JSON 模式重试事件

- ✅ **Test:** `TestRenderIntentNodeTimeout_TTY`
  - **Status:** RED — TTY stub 不输出内容
  - **Verifies:** AC#6 — 超时事件渲染

- ✅ **Test:** `TestRenderDriftList_TTY`
  - **Status:** RED — TTY stub 不输出内容
  - **Verifies:** AC#6 — drift 表格渲染

- ⚠️ **Test:** `TestRenderDriftList_JSON`
  - **Status:** PASS — JSON 路径已在 stub 中实现
  - **Verifies:** AC#6 — JSON 模式 drift 列表

- ✅ **Test:** `TestRenderDriftList_Empty`
  - **Status:** RED — stub 不输出"无 drift"消息
  - **Verifies:** AC#6 — 空 drift 显示"无 drift"

---

## Stubs Created (Compilation Support)

### intent/types.go 扩展

- `IntentRetrying` state 常量
- `IntentNode` 新增字段：`RetryCount`, `MaxRetries`, `Timeout`, `LastFailedAt`
- `IntentNode` stub 方法：`CanRetry()` (returns false), `IncrRetry()` (no-op)
- `DriftType`, `DriftItem` 新类型
- `IntentTree` 新增字段：`DesiredNodes`, `Drifts`
- `IntentTree` stub 方法：`InitDesired()`, `ComputeDrifts()`, `AddDrift()`, `ClearDrift()`, `ActiveDrifts()` (all no-op/nil)

### intent/reconciler.go (新文件)

- `ReconcilerConfig` struct + `DefaultReconcilerConfig()`
- `ReconcilerCallbacks` struct (EngineCallbacks 超集)
- `reconcileEvent`, `reconcileEventType` internal types
- `Reconciler` struct
- `NewReconciler()` — 创建 Reconciler（有效）
- `Execute()` — **stub: returns nil without executing any nodes**

### ipc/protocol.go 扩展

- 4 个新 `StreamEventType` 常量：`intent_node_retry`, `intent_node_timeout`, `intent_drift_detected`, `intent_drift_resolved`
- `IntentNodeEventPayload` 新增字段：`RetryAttempt`, `MaxRetries`, `DriftType`
- `IntentNodeWire` 新增字段：`RetryCount`, `MaxRetries`, `TimeoutMs`
- `IntentTreeWire` 新增字段：`Drifts []DriftItemWire`
- `DriftItemWire` 新类型

### internal/ui/intent.go 扩展

- `RenderIntentNodeRetry()` — JSON 路径实现，TTY stub
- `RenderIntentNodeTimeout()` — JSON 路径实现，TTY stub
- `RenderDriftList()` — JSON 路径实现，TTY stub
- `intentStateIcon()` — 新增 `retrying` → `↻`

---

## Implementation Checklist

### Test: TestIntentNode_CanRetry + TestIntentNode_CanRetry_Exhausted + TestIntentNode_IncrRetry

**File:** `intent/types_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `IntentNode.CanRetry()` — 返回 `RetryCount < MaxRetries`
- [ ] 实现 `IntentNode.IncrRetry()` — `RetryCount++`，设置 `LastFailedAt = time.Now()`
- [ ] Run test: `go test -run 'TestIntentNode_CanRetry|TestIntentNode_IncrRetry' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestIntentTree_InitDesired + TestIntentTree_ComputeDrifts + TestIntentTree_AddDrift_ClearDrift + TestIntentTree_ActiveDrifts

**File:** `intent/types_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `IntentTree.InitDesired()` — 遍历 Nodes，为每个设置 `DesiredNodes[id] = IntentCompleted`
- [ ] 实现 `IntentTree.ComputeDrifts()` — 对比 DesiredNodes 与 Current state，返回差异
- [ ] 实现 `IntentTree.AddDrift()` — 追加到 `Drifts` 切片
- [ ] 实现 `IntentTree.ClearDrift()` — 从 `Drifts` 中移除指定 nodeID 的记录
- [ ] 实现 `IntentTree.ActiveDrifts()` — 返回当前 Drifts 切片
- [ ] Run test: `go test -run 'TestIntentTree_InitDesired|TestIntentTree_ComputeDrifts|TestIntentTree_AddDrift|TestIntentTree_ActiveDrifts' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestDecomposer_Decompose_InitDesired

**File:** `intent/decomposer_test.go`

**Tasks to make this test pass:**

- [ ] 修改 `Decomposer.Decompose()` — 分解完成后调用 `tree.InitDesired()`
- [ ] Run test: `go test -run 'TestDecomposer_Decompose_InitDesired' ./intent/...`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestReconciler_Execute_AllSuccess + TestReconciler_Callbacks

**File:** `intent/reconciler_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `Reconciler.Execute()` 基本事件驱动循环
  - [ ] 启动 runnable 节点 goroutine
  - [ ] 每个节点 Spawn → Wait
  - [ ] 主循环从 eventCh 消费事件
  - [ ] evNodeCompleted → MarkCompleted → 启动新 runnable
  - [ ] 终态检查 → 退出
- [ ] 触发所有回调（OnNodeStart, OnNodeComplete, OnProgress）
- [ ] Run test: `go test -run 'TestReconciler_Execute_AllSuccess|TestReconciler_Callbacks' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 2 hours

---

### Test: TestReconciler_Execute_RetrySuccess + TestReconciler_Execute_RetryExhausted

**File:** `intent/reconciler_test.go`

**Tasks to make these tests pass:**

- [ ] 实现节点失败后的重试逻辑
  - [ ] evNodeFailed → 检查 CanRetry()
  - [ ] 可重试 → IncrRetry() + 状态重置为 IntentPending + 重新调度
  - [ ] 不可重试 → MarkFailed + cascadeFailure
- [ ] 触发 OnNodeRetry 回调
- [ ] Run test: `go test -run 'TestReconciler_Execute_RetrySuccess|TestReconciler_Execute_RetryExhausted' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestReconciler_Execute_Timeout + TestReconciler_Execute_TimeoutExhausted

**File:** `intent/reconciler_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `executeNodeWithTimeout` — 使用 `context.WithTimeout`
- [ ] 超时发送 evNodeTimeout 事件
- [ ] evNodeTimeout 走与 evNodeFailed 相同的重试路径
- [ ] 触发 OnNodeTimeout 回调
- [ ] Run test: `go test -run 'TestReconciler_Execute_Timeout' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestReconciler_Execute_CascadeAfterExhausted

**File:** `intent/reconciler_test.go`

**Tasks to make this test pass:**

- [ ] 确保重试耗尽后调用 `MarkFailed` + `cascadeFailure`
- [ ] 验证所有下游节点（包括间接依赖）均被标记为 failed
- [ ] Run test: `go test -run 'TestReconciler_Execute_CascadeAfterExhausted' ./intent/...`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestReconciler_Execute_ParallelWithRetry

**File:** `intent/reconciler_test.go`

**Tasks to make this test pass:**

- [ ] 确保并行节点独立执行：一个节点重试不阻塞另一个
- [ ] Run test: `go test -run 'TestReconciler_Execute_ParallelWithRetry' ./intent/...`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestReconciler_Execute_ContextCancel

**File:** `intent/reconciler_test.go`

**Tasks to make this test pass:**

- [ ] Execute 主循环监听 `ctx.Done()`
- [ ] ctx 取消时设置 tree.State = IntentFailed 并返回 ctx.Err()
- [ ] Run test: `go test -run 'TestReconciler_Execute_ContextCancel' ./intent/...`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestReconciler_Execute_DriftDetectedCallback + TestReconciler_Execute_DriftResolvedCallback

**File:** `intent/reconciler_test.go`

**Tasks to make these tests pass:**

- [ ] 节点失败时调用 `tree.AddDrift()` + `callbacks.OnDriftDetected`
- [ ] 重试成功后调用 `tree.ClearDrift()` + `callbacks.OnDriftResolved`
- [ ] Run test: `go test -run 'TestReconciler_Execute_DriftDetected|TestReconciler_Execute_DriftResolved' ./intent/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestReconciler_Execute_NFR40_Latency

**File:** `intent/reconciler_test.go`

**Tasks to make this test pass:**

- [ ] 事件驱动模式自然满足（channel 事件消费是即时的）
- [ ] 确保 OnNodeFailed 和 OnNodeRetry 回调之间延迟 < 5s
- [ ] Run test: `go test -run 'TestReconciler_Execute_NFR40_Latency' ./intent/...`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours（由事件驱动架构自然保证）

---

### Test: UI Render Tests (TTY)

**File:** `internal/ui/intent_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `RenderIntentNodeRetry` TTY 格式输出（黄色/橙色重试提示）
- [ ] 实现 `RenderIntentNodeTimeout` TTY 格式输出（红色超时提示）
- [ ] 实现 `RenderDriftList` TTY 格式输出（drift 表格/列表）
- [ ] 实现 `RenderDriftList` 空列表显示"无 drift"消息
- [ ] Run test: `go test -run 'TestRenderIntentNodeRetry_TTY|TestRenderIntentNodeTimeout_TTY|TestRenderDriftList_TTY|TestRenderDriftList_Empty' ./internal/ui/...`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Deferred Tests (Need Source Code Changes First)

以下测试需要在修改生产代码签名后才能编写：

- [ ] `intent/manager_test.go` — `TestManager_Execute_WithReconciler`：验证 Execute 使用 Reconciler 而非 Engine
  - **Blocked on:** `Manager.Execute` 签名变更为接受 `ReconcilerCallbacks`
  - **Blocked on:** `NewManager` 签名变更为接受 `ReconcilerConfig`

- [ ] `ipc/server_test.go` — `intentManager` 接口扩展后的测试
  - **Blocked on:** `ExecuteIntent` 签名增加 4 个新回调参数

- [ ] `ipc/intent_adapter_test.go` — wire 转换新字段测试
  - **Blocked on:** `intentNodeToWire` 和 `intentTreeToWire` 更新

- [ ] `cmd/rnix/apply_test.go` — 新 StreamEvent 类型处理测试
  - **Blocked on:** `onEvent` switch 新增分支

---

## Running Tests

```bash
# Run all failing tests for this story (RED phase)
go test -v -run 'TestReconciler_|TestIntentNode_CanRetry|TestIntentNode_IncrRetry|TestIntentTree_InitDesired|TestIntentTree_ComputeDrifts|TestIntentTree_AddDrift|TestIntentTree_ActiveDrifts|TestDecomposer_Decompose_InitDesired' ./intent/...

# Run UI tests
go test -v -run 'TestRenderIntentNodeRetry|TestRenderIntentNodeTimeout|TestRenderDriftList' ./internal/ui/...

# Run specific test file
go test -v -run 'TestReconciler_' ./intent/...

# Run with race detector
go test -race -run 'TestReconciler_' ./intent/...

# Run all intent tests (existing + new)
go test -v ./intent/...

# Run full test suite
go test -race ./intent/... ./ipc/... ./internal/ui/... ./cmd/rnix/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 26 tests written (23 failing, 3 coincidental pass)
- ✅ Stub types and functions created for compilation
- ✅ IPC wire types extended for reconciler fields
- ✅ UI render stubs created
- ✅ Implementation checklist created
- ✅ Existing tests verified unaffected (all still pass)

**Verification:**

```
intent package:    19 FAIL, 1 PASS (coincidental)
internal/ui:        4 FAIL, 2 PASS (JSON path implemented)
Total:             23 FAIL, 3 coincidental PASS
Existing tests:    ALL PASS (no regression)
```

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one failing test** from implementation checklist (start with types.go methods)
2. **Read the test** to understand expected behavior
3. **Implement minimal code** to make that specific test pass
4. **Run the test** to verify it now passes (green)
5. **Check off the task** in implementation checklist
6. **Move to next test** and repeat

**Recommended Order:**

1. `IntentNode.CanRetry()` + `IncrRetry()` (types.go) — 最简单
2. `IntentTree.InitDesired()` + 三态方法 (types.go) — 基础设施
3. `Decomposer.Decompose` 调用 `InitDesired()` (decomposer.go) — 一行修改
4. `Reconciler.Execute()` 基本循环 (reconciler.go) — 核心
5. 重试逻辑 (reconciler.go) — 增强
6. 超时逻辑 (reconciler.go) — 增强
7. Drift 回调 (reconciler.go) — 事件
8. UI TTY 渲染 (intent.go) — 显示层
9. Manager/IPC 集成 — 最后

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有测试通过 (green phase complete)
2. `-race` 竞态检测
3. 代码质量审查
4. 提取公共模式（Reconciler 与 Engine 的共享逻辑）
5. 性能验证（NFR40 延迟指标）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -v -run 'TestReconciler_|TestIntentNode_CanRetry|TestIntentNode_IncrRetry|TestIntentTree_Init|TestIntentTree_Compute|TestIntentTree_AddDrift|TestIntentTree_ActiveDrifts|TestDecomposer_Decompose_InitDesired' ./intent/...`

**Results:**

```
--- FAIL: TestDecomposer_Decompose_InitDesired (0.00s)
--- FAIL: TestReconciler_Execute_AllSuccess (0.00s)
--- FAIL: TestReconciler_Execute_RetrySuccess (0.00s)
--- FAIL: TestReconciler_Execute_RetryExhausted (0.00s)
--- FAIL: TestReconciler_Execute_Timeout (0.00s)
--- FAIL: TestReconciler_Execute_TimeoutExhausted (0.00s)
--- FAIL: TestReconciler_Execute_CascadeAfterExhausted (0.00s)
--- FAIL: TestReconciler_Execute_ParallelWithRetry (0.00s)
--- FAIL: TestReconciler_Execute_ContextCancel (0.00s)
--- FAIL: TestReconciler_Execute_DriftDetectedCallback (0.00s)
--- FAIL: TestReconciler_Execute_DriftResolvedCallback (0.00s)
--- FAIL: TestReconciler_Execute_NFR40_Latency (0.00s)
--- FAIL: TestReconciler_Callbacks (0.00s)
--- FAIL: TestIntentNode_CanRetry (0.00s)
--- PASS: TestIntentNode_CanRetry_Exhausted (0.00s)
--- FAIL: TestIntentNode_IncrRetry (0.00s)
--- FAIL: TestIntentTree_InitDesired (0.00s)
--- FAIL: TestIntentTree_ComputeDrifts (0.00s)
--- FAIL: TestIntentTree_AddDrift_ClearDrift (0.00s)
--- FAIL: TestIntentTree_ActiveDrifts (0.00s)
FAIL
```

**Summary:**

- Total tests: 26 (intent: 20, ui: 6)
- Failing: 23 (expected)
- Passing: 3 (coincidental — stub behavior matches expected for negative cases)
- Status: ✅ RED phase verified

---

## Notes

- **Go TDD 模式**：通过 stub 实现让测试编译通过但运行失败，而非 JS 的 `test.skip()` 模式
- **coincidental pass**：3 个测试由于 stub 返回值恰好匹配预期而通过（如 `CanRetry_Exhausted` stub 返回 false = 预期值），实现后仍然通过
- **竞态安全**：Reconciler 设计遵循 Engine 的 mutex 模式，回调在 mutex 外调用
- **Deferred tests**：Manager/IPC/CLI 层测试需在生产代码签名变更后才能编写，已在清单中标记

---

## Knowledge Base References Applied

- **test-quality.md** — Given-When-Then 测试设计原则，一个断言一个测试
- **data-factories.md** — mock spawner 模式（从 engine_test.go 复用）
- **test-levels-framework.md** — Unit 测试为主（backend Go 项目无需 E2E/Browser）
- **Go testing patterns** — 标准库 testing 包，table-driven 测试，-race 竞态检测

---

**Generated by BMad TEA Agent** — 2026-03-10
