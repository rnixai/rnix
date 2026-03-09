---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-10'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/19-1-intent-declaration-and-task-decomposition.md
  - _bmad/tea/config.yaml
  - compose/types.go
  - compose/dag.go
  - compose/dag_test.go
  - compose/engine_test.go
  - internal/types/types.go
---

# ATDD Checklist - Epic 19, Story 19.1: 意图声明与任务分解

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit (Go backend)
**Detected Stack:** backend

---

## Story Summary

Story 19.1 实现声明式意图系统的核心——用户通过 `rnix apply "高层意图"` 声明期望状态，系统自动将其分解为子意图树（Intent Tree），按 DAG 拓扑顺序调度执行，支持并行、失败级联和状态查询。

**As a** 应用开发者
**I want** 通过 `rnix apply "高层意图"` 声明期望状态，系统自动分解为子意图树
**So that** 我可以用自然语言描述目标而不需要手动编排

---

## Acceptance Criteria

1. `rnix apply "高层意图"` → 系统递归分解为子意图树，每个子意图对应智能体进程
2. 分解完成后显示子任务列表、依赖关系和执行计划，等待用户确认
3. 用户确认后按 DAG 拓扑顺序调度子意图，无依赖并行执行
4. `rnix intent status` 显示意图树当前状态
5. `--yes` 标志跳过确认直接执行
6. 子意图失败时停止下游，独立分支继续，汇报失败详情

---

## Test Strategy

### Test Level Selection (Backend Go Project)

| AC | Test Level | Rationale |
|----|-----------|-----------|
| #1 | Unit | IntentTree 构造、Decomposer 分解逻辑、DAG 构建 |
| #2 | Unit | IntentTree Progress/Status 方法 |
| #3 | Unit | DAG 拓扑排序、Engine 调度执行 |
| #4 | Unit | Manager.Status/ListActive |
| #5 | Unit (deferred) | CLI --yes flag (CLI 测试在 IPC 集成后覆盖) |
| #6 | Unit | MarkFailed 级联、Engine 部分失败 |

### Priority Assignment

| Priority | Tests | Description |
|----------|-------|-------------|
| P0 | 15 | IntentTree 数据模型、DAG 拓扑排序、Engine 执行核心路径 |
| P1 | 18 | Decomposer 分解、Manager 生命周期、失败级联、Callbacks |
| P2 | 7 | 边界情况（空结果、自循环、超时、独立分支） |

---

## Failing Tests Created (RED Phase)

### Unit Tests — `intent/types_test.go` (9 tests)

**File:** `intent/types_test.go` (258 lines)

- **Test:** `TestIntentTree_Progress`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.Progress not yet implemented")`
  - **Verifies:** AC#4 — Progress 正确计算完成/总计数
  - **Priority:** P0

- **Test:** `TestIntentTree_RunnableNodes`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.RunnableNodes not yet implemented")`
  - **Verifies:** AC#3 — 正确返回依赖已满足的 pending 节点
  - **Priority:** P0

- **Test:** `TestIntentTree_RunnableNodes_NoneReady`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.RunnableNodes not yet implemented")`
  - **Verifies:** AC#3 — 所有依赖未满足时返回空
  - **Priority:** P1

- **Test:** `TestIntentTree_MarkCompleted`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.MarkCompleted not yet implemented")`
  - **Verifies:** AC#1 — 标记节点完成并设置 result
  - **Priority:** P0

- **Test:** `TestIntentTree_MarkFailed`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.MarkFailed not yet implemented")`
  - **Verifies:** AC#6 — 失败级联到下游节点
  - **Priority:** P0

- **Test:** `TestIntentTree_MarkFailed_IndependentBranchNotAffected`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.MarkFailed not yet implemented")`
  - **Verifies:** AC#6 — 失败不影响独立分支
  - **Priority:** P2

- **Test:** `TestIntentTree_IsTerminal_AllCompleted`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")`
  - **Verifies:** AC#4 — 全部完成时返回 true
  - **Priority:** P0

- **Test:** `TestIntentTree_IsTerminal_MixedCompletedAndFailed`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")`
  - **Verifies:** AC#4 — 混合 completed/failed 也是 terminal
  - **Priority:** P1

- **Test:** `TestIntentTree_IsTerminal_StillExecuting`
  - **Status:** RED — `t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")`
  - **Verifies:** AC#4 — 有 executing 节点时返回 false
  - **Priority:** P1

### Unit Tests — `intent/dag_test.go` (9 tests)

**File:** `intent/dag_test.go` (339 lines)

- **Test:** `TestBuildIntentDAG_NoDeps`
  - **Status:** RED — `t.Skip("ATDD RED: BuildIntentDAG not yet implemented")`
  - **Verifies:** AC#1 — 无依赖 DAG 构建
  - **Priority:** P0

- **Test:** `TestBuildIntentDAG_LinearDeps`
  - **Status:** RED — `t.Skip("ATDD RED: BuildIntentDAG not yet implemented")`
  - **Verifies:** AC#1 — 线性依赖链
  - **Priority:** P0

- **Test:** `TestBuildIntentDAG_DiamondDeps`
  - **Status:** RED — `t.Skip("ATDD RED: BuildIntentDAG not yet implemented")`
  - **Verifies:** AC#1,#3 — 菱形依赖
  - **Priority:** P0

- **Test:** `TestBuildIntentDAG_CycleDetection`
  - **Status:** RED — `t.Skip("ATDD RED: BuildIntentDAG cycle detection not yet implemented")`
  - **Verifies:** AC#1 — 循环依赖检测
  - **Priority:** P0

- **Test:** `TestBuildIntentDAG_SelfCycle`
  - **Status:** RED — `t.Skip("ATDD RED: BuildIntentDAG self-cycle detection not yet implemented")`
  - **Verifies:** AC#1 — 自循环检测
  - **Priority:** P2

- **Test:** `TestTopologicalSort_AllParallel`
  - **Status:** RED — `t.Skip("ATDD RED: TopologicalSort not yet implemented")`
  - **Verifies:** AC#3 — 全并行节点单层排序
  - **Priority:** P0

- **Test:** `TestTopologicalSort_Sequential`
  - **Status:** RED — `t.Skip("ATDD RED: TopologicalSort not yet implemented")`
  - **Verifies:** AC#3 — 串行链多层排序
  - **Priority:** P0

- **Test:** `TestTopologicalSort_Diamond`
  - **Status:** RED — `t.Skip("ATDD RED: TopologicalSort not yet implemented")`
  - **Verifies:** AC#3 — 菱形拓扑排序
  - **Priority:** P0

- **Test:** `TestTopologicalSort_ComplexGraph`
  - **Status:** RED — `t.Skip("ATDD RED: TopologicalSort not yet implemented")`
  - **Verifies:** AC#3 — 复杂图依赖约束
  - **Priority:** P1

### Unit Tests — `intent/decomposer_test.go` (7 tests)

**File:** `intent/decomposer_test.go` (210 lines)

- **Test:** `TestDecomposer_Decompose_Success`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose not yet implemented")`
  - **Verifies:** AC#1 — LLM 返回有效 JSON 成功构建 IntentTree
  - **Priority:** P0

- **Test:** `TestDecomposer_Decompose_InvalidJSON`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose JSON parsing not yet implemented")`
  - **Verifies:** AC#1 — 无效 JSON 处理
  - **Priority:** P1

- **Test:** `TestDecomposer_Decompose_CyclicDeps`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose cycle validation not yet implemented")`
  - **Verifies:** AC#1 — LLM 返回循环依赖时报错
  - **Priority:** P1

- **Test:** `TestDecomposer_Decompose_EmptyResult`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose empty validation not yet implemented")`
  - **Verifies:** AC#1 — LLM 返回空结果时报错
  - **Priority:** P1

- **Test:** `TestDecomposer_Decompose_LLMError`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose error handling not yet implemented")`
  - **Verifies:** AC#1 — LLM 调用错误传播
  - **Priority:** P1

- **Test:** `TestDecomposer_Decompose_Timeout`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose timeout handling not yet implemented")`
  - **Verifies:** AC#1 — 超时处理
  - **Priority:** P2

- **Test:** `TestDecomposer_Decompose_ModelPassthrough`
  - **Status:** RED — `t.Skip("ATDD RED: Decomposer.Decompose model passthrough not yet implemented")`
  - **Verifies:** AC#1 — model 参数传递
  - **Priority:** P1

### Unit Tests — `intent/engine_test.go` (8 tests)

**File:** `intent/engine_test.go` (427 lines)

- **Test:** `TestEngine_Execute_Sequential`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute not yet implemented")`
  - **Verifies:** AC#3 — 串行依赖的有序执行
  - **Priority:** P0

- **Test:** `TestEngine_Execute_Parallel`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute parallel scheduling not yet implemented")`
  - **Verifies:** AC#3 — 无依赖节点并行执行
  - **Priority:** P0

- **Test:** `TestEngine_Execute_AllSuccess`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute success path not yet implemented")`
  - **Verifies:** AC#3 — 菱形依赖全部成功
  - **Priority:** P0

- **Test:** `TestEngine_Execute_CascadeFailure`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute failure cascade not yet implemented")`
  - **Verifies:** AC#6 — 失败级联到下游
  - **Priority:** P0

- **Test:** `TestEngine_Execute_PartialFailure`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute partial failure not yet implemented")`
  - **Verifies:** AC#6 — 独立分支继续执行
  - **Priority:** P1

- **Test:** `TestEngine_Execute_ContextCancel`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute context cancellation not yet implemented")`
  - **Verifies:** 健壮性 — ctx 取消时正确停止
  - **Priority:** P2

- **Test:** `TestEngine_Execute_Callbacks`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute callbacks not yet implemented")`
  - **Verifies:** AC#2,#4 — OnNodeStart/OnNodeComplete 回调
  - **Priority:** P1

- **Test:** `TestEngine_Execute_ProgressCallback`
  - **Status:** RED — `t.Skip("ATDD RED: Engine.Execute progress callback not yet implemented")`
  - **Verifies:** AC#4 — OnProgress 回调正确报告进度
  - **Priority:** P1

### Unit Tests — `intent/manager_test.go` (7 tests)

**File:** `intent/manager_test.go` (207 lines)

- **Test:** `TestManager_Apply`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Apply not yet implemented")`
  - **Verifies:** AC#1 — 创建意图并分解
  - **Priority:** P0

- **Test:** `TestManager_Apply_GeneratesUniqueIDs`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Apply ID generation not yet implemented")`
  - **Verifies:** AC#1 — ID 唯一性
  - **Priority:** P1

- **Test:** `TestManager_Confirm`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Confirm not yet implemented")`
  - **Verifies:** AC#2 — 确认后状态转换
  - **Priority:** P1

- **Test:** `TestManager_Confirm_NotFound`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Confirm error handling not yet implemented")`
  - **Verifies:** AC#2 — 不存在的 intent 报错
  - **Priority:** P2

- **Test:** `TestManager_Status`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Status not yet implemented")`
  - **Verifies:** AC#4 — 查询意图状态
  - **Priority:** P1

- **Test:** `TestManager_Status_NotFound`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.Status error handling not yet implemented")`
  - **Verifies:** AC#4 — 不存在的 intent 报错
  - **Priority:** P2

- **Test:** `TestManager_ListActive`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.ListActive not yet implemented")`
  - **Verifies:** AC#4 — 列出所有活跃意图
  - **Priority:** P1

- **Test:** `TestManager_ListActive_ExcludesTerminal`
  - **Status:** RED — `t.Skip("ATDD RED: Manager.ListActive terminal exclusion not yet implemented")`
  - **Verifies:** AC#4 — 排除已终止意图
  - **Priority:** P2

---

## Stub Files Created (Minimal Compilation Stubs)

### `intent/types.go`

**Exports:** IntentID, IntentState, IntentNode, IntentTree, state constants
**Methods:** Progress(), RunnableNodes(), MarkCompleted(), MarkFailed(), IsTerminal()
**Status:** All methods return zero/nil values (RED stubs)

### `intent/dag.go`

**Exports:** DAG, DAGNode, BuildIntentDAG()
**Methods:** DetectCycle(), TopologicalSort()
**Status:** All functions return nil (RED stubs)

### `intent/decomposer.go`

**Exports:** LLMCaller (interface), Decomposer, NewDecomposer()
**Methods:** Decompose()
**Status:** Decompose returns nil (RED stub)

### `intent/engine.go`

**Exports:** KernelSpawner (interface), ExitStatus, EngineCallbacks, Engine, NewEngine()
**Methods:** Execute()
**Status:** All functions return nil (RED stubs)

### `intent/manager.go`

**Exports:** ApplyRequest, Manager, NewManager()
**Methods:** Apply(), Confirm(), Execute(), Status(), ListActive()
**Status:** All methods return nil (RED stubs)

---

## Mock Infrastructure Created

### `mockLLMCaller` (in `decomposer_test.go`)

- Configurable response string and error
- Configurable delay for timeout testing
- Implements `LLMCaller` interface

### `recordingLLMCaller` (in `decomposer_test.go`)

- Records call parameters (prompt, model) for verification
- Implements `LLMCaller` interface

### `mockIntentSpawner` (in `engine_test.go`)

- Thread-safe spawn recording with mutex
- Configurable per-PID exit results and delays
- Implements `KernelSpawner` interface
- `getSpawnOrder()` helper for execution order verification

---

## Implementation Checklist

### Test: IntentTree Data Model (9 tests in `intent/types_test.go`)

**Tasks to make these tests pass:**

- [ ] Implement `IntentTree.Progress()` — iterate nodes, count state==completed vs total
- [ ] Implement `IntentTree.RunnableNodes()` — find pending nodes whose all DependsOn are completed
- [ ] Implement `IntentTree.MarkCompleted()` — set node state and result
- [ ] Implement `IntentTree.MarkFailed()` — set node state/error, DFS cascade to dependent downstream
- [ ] Implement `IntentTree.IsTerminal()` — all nodes in completed or failed state
- [ ] Remove `t.Skip()` from all 9 tests
- [ ] Run: `go test -race -v ./intent/... -run "TestIntentTree_"`
- [ ] All 9 tests pass (GREEN)

**Estimated Effort:** 1-2 hours

### Test: Intent DAG (9 tests in `intent/dag_test.go`)

**Tasks to make these tests pass:**

- [ ] Implement `BuildIntentDAG()` — construct DAG from IntentTree nodes with DependsOn edges
- [ ] Implement `DAG.DetectCycle()` — DFS three-color cycle detection (same pattern as compose/dag.go)
- [ ] Implement `DAG.TopologicalSort()` — Kahn's algorithm BFS with in-degree counting
- [ ] Remove `t.Skip()` from all 9 tests
- [ ] Run: `go test -race -v ./intent/... -run "TestBuildIntentDAG_|TestTopologicalSort_"`
- [ ] All 9 tests pass (GREEN)

**Estimated Effort:** 2-3 hours

### Test: Decomposer (7 tests in `intent/decomposer_test.go`)

**Tasks to make these tests pass:**

- [ ] Implement `Decomposer.Decompose()` — build prompt, call LLM, parse JSON, validate, construct IntentTree
- [ ] Define decompose prompt template (Go constant)
- [ ] Handle JSON parse errors, empty results, cyclic dependencies
- [ ] Propagate LLM errors and context cancellation
- [ ] Pass model parameter through to LLMCaller
- [ ] Remove `t.Skip()` from all 7 tests
- [ ] Run: `go test -race -v ./intent/... -run "TestDecomposer_"`
- [ ] All 7 tests pass (GREEN)

**Estimated Effort:** 2-3 hours

### Test: Intent Engine (8 tests in `intent/engine_test.go`)

**Tasks to make these tests pass:**

- [ ] Implement `NewEngine()` — validate tree, build DAG
- [ ] Implement `Engine.Execute()` — event-driven scheduling loop
- [ ] Implement sequential execution: DAG topo sort → layer-by-layer spawn → wait
- [ ] Implement parallel execution: concurrent goroutines for same-layer nodes
- [ ] Implement failure cascade: MarkFailed on node failure, skip downstream
- [ ] Implement context cancellation: check ctx.Done() in scheduling loop
- [ ] Implement callbacks: fire OnNodeStart/OnNodeComplete/OnNodeFailed/OnProgress
- [ ] Remove `t.Skip()` from all 8 tests
- [ ] Run: `go test -race -v ./intent/... -run "TestEngine_"`
- [ ] All 8 tests pass (GREEN)

**Estimated Effort:** 3-4 hours

### Test: Intent Manager (7 tests in `intent/manager_test.go`)

**Tasks to make these tests pass:**

- [ ] Implement `Manager.Apply()` — generate ID, call decomposer, store tree
- [ ] Implement `Manager.Confirm()` — find tree, validate state, transition
- [ ] Implement `Manager.Status()` — find tree by ID or return error
- [ ] Implement `Manager.ListActive()` — filter non-terminal trees
- [ ] Implement unique ID generation with atomic counter
- [ ] Remove `t.Skip()` from all 7 tests
- [ ] Run: `go test -race -v ./intent/... -run "TestManager_"`
- [ ] All 7 tests pass (GREEN)

**Estimated Effort:** 2-3 hours

---

## Running Tests

```bash
# Run all ATDD tests for Story 19.1 (all will SKIP in RED phase)
go test -race -v -count=1 ./intent/...

# Run specific test file
go test -race -v -count=1 ./intent/... -run "TestIntentTree_"
go test -race -v -count=1 ./intent/... -run "TestBuildIntentDAG_|TestTopologicalSort_"
go test -race -v -count=1 ./intent/... -run "TestDecomposer_"
go test -race -v -count=1 ./intent/... -run "TestEngine_"
go test -race -v -count=1 ./intent/... -run "TestManager_"

# Run all project tests (includes intent)
go test -race ./...

# Run with coverage
go test -race -cover ./intent/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 40 tests written and skipping with `t.Skip("ATDD RED: ...")`
- Mock infrastructure created (mockLLMCaller, mockIntentSpawner, recordingLLMCaller)
- Stub files compile with zero-value returns
- Race detector passes
- Implementation checklist created

**Verification:**

- All 40 tests run and SKIP as expected
- Tests are well-structured with Given/When/Then comments
- Tests follow project patterns (manual mocks, `t.Fatalf`/`t.Errorf`, `Test<Type>_<Method>_<Scenario>`)
- No compilation errors

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. Pick one test group (start with `types_test.go` — foundation for everything)
2. Remove `t.Skip()` from tests in that group
3. Implement minimal code to make tests pass
4. Run tests to verify GREEN
5. Move to next group: dag → decomposer → engine → manager
6. Repeat until all 40 tests pass

**Recommended Implementation Order:**

1. `intent/types.go` — IntentTree methods (foundation)
2. `intent/dag.go` — DAG + cycle detection + topo sort
3. `intent/decomposer.go` — LLM decomposition
4. `intent/engine.go` — execution engine
5. `intent/manager.go` — lifecycle management
6. IPC protocol, server, client extensions
7. CLI commands (`apply`, `intent status`)
8. UI rendering components

---

### REFACTOR Phase (After All Tests Pass)

1. Verify all 40 tests pass with `go test -race ./intent/...`
2. Review concurrent access patterns (Engine goroutines, Manager mutex)
3. Ensure error messages include context (intent ID, node ID)
4. Check DAG code for consistency with `compose/dag.go` patterns
5. Run `go vet ./intent/...` for static analysis
6. Run full suite: `go test -race ./...`

---

## Acceptance Criteria Coverage Matrix

| AC | Tests Covering | Count |
|----|----------------|-------|
| #1 (apply + decompose) | types_test(4), dag_test(5), decomposer_test(7), manager_test(2) | 18 |
| #2 (show plan + confirm) | manager_test(2), engine_test(1) | 3 |
| #3 (DAG execution) | dag_test(4), engine_test(4) | 8 |
| #4 (intent status) | types_test(5), manager_test(4), engine_test(2) | 11 |
| #5 (--yes flag) | (deferred to CLI integration tests) | 0 |
| #6 (failure handling) | types_test(2), engine_test(2) | 4 |

**Note:** AC#5 (--yes flag) is tested at CLI level, deferred to `cmd/rnix/apply_test.go` during implementation phase.

---

## Deferred Tests (Not in ATDD Scope)

The following tests from Story 19.1 Task 13 are **not** generated in this ATDD round because they test integration points that require existing infrastructure modifications:

- `cmd/rnix/apply_test.go` — CLI command registration and flag parsing
- `cmd/rnix/intent_test.go` — CLI intent subcommand
- `internal/ui/intent_test.go` — UI rendering components
- IPC server/client integration tests

These will be added during the GREEN/implementation phase when the IPC protocol and CLI commands are wired up.

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -v -count=1 ./intent/...`

**Results:**

```
40 tests total
 0 passing (expected)
40 skipping (expected — all with "ATDD RED: ..." messages)
 0 failing (no compilation or runtime errors)
```

**Summary:**

- Total tests: 40
- Passing: 0 (expected)
- Skipping: 40 (expected — RED phase)
- Failing: 0 (tests compile and skip correctly)
- Race detector: PASS
- Status: RED phase verified

---

## Notes

- Go 项目使用 `t.Skip("ATDD RED: ...")` 替代 TypeScript 的 `test.skip()` 作为 TDD RED 阶段标记
- 存根文件仅包含类型定义和返回零值的方法签名，确保测试编译通过
- Mock 基础设施遵循项目既有模式（手动 mock struct，无第三方 mock 库）
- `intent/` 包独立设计，不导入 `kernel/`、`cmd/`、`ipc/`（遵循架构约束）
- DAG 实现参考 `compose/dag.go` 模式但独立编写（避免跨包耦合）

---

## Knowledge Base References Applied

- **test-quality.md** — Given/When/Then 结构、确定性测试、隔离性、单一断言
- **data-factories.md** — Factory 模式用于 mock 数据构建（适配为 Go mock struct）
- **test-levels-framework.md** — Unit 级别选择（纯逻辑、无外部依赖）
- **test-priorities-matrix.md** — P0/P1/P2 优先级分配
- **component-tdd.md** — Red-Green-Refactor 循环
- **test-healing-patterns.md** — 避免 time.Sleep、动态数据断言

---

**Generated by BMad TEA Agent** — 2026-03-10
