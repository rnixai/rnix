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
  - _bmad-output/implementation-artifacts/19-3-incremental-update-and-status-query.md
  - intent/types.go
  - intent/merge_test.go
  - intent/decomposer.go
  - intent/decomposer_test.go
  - intent/manager.go
  - intent/manager_test.go
  - intent/reconciler.go
  - intent/reconciler_test.go
  - ipc/protocol.go
  - internal/ui/intent.go
  - internal/ui/intent_test.go
---

# ATDD Checklist - Epic 19, Story 19.3: 增量更新与状态查询

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

作为应用开发者，在执行过程中更新意图并查看完整状态，以便动态调整需求而不丢失已完成的工作。

**As a** 应用开发者
**I want** 在执行过程中更新意图并查看完整状态
**So that** 我可以动态调整需求而不丢失已完成的工作

---

## Acceptance Criteria

1. **AC#1** 增量更新时，Reconciler 计算增量差异，仅执行新增/变更部分，已完成的工作不回滚
2. **AC#2** `rnix intent status` 显示意图树完整状态：整体进度、各子意图完成度、执行中的智能体列表、待解决的 drift 项
3. **AC#3** 新节点依赖已完成节点时，立即可调度执行，无需重新执行已完成的上游节点
4. **AC#4** 新节点依赖尚未完成的节点时，等待依赖完成后自动调度
5. **AC#5** 增量更新修改了已完成节点的意图描述时，将该节点标记为需要重新执行，状态重置为 pending
6. **AC#6** 正在执行的子任务不受影响，继续运行至完成；新增/变更的节点在合适时机调度
7. **AC#7** 增量分解返回的新节点引用了不存在的依赖时，返回错误，拒绝增量更新

---

## Failing Tests Created (RED Phase)

### Unit Tests — intent/merge_test.go (9 tests, new file)

| # | Test Name | AC | Status | Description |
|---|-----------|-----|--------|-------------|
| 1 | `TestMergeIncremental_AddNewNodes` | #1, #3 | RED | 新增节点正确合并到现有树，状态设为 pending |
| 2 | `TestMergeIncremental_ModifyExistingNode` | #5 | RED | 已有节点 intent 变更后重置状态为 pending，清除 Result |
| 3 | `TestMergeIncremental_UnchangedNodes` | #3 | RED | 未变化节点保持状态和 PID 不变 |
| 4 | `TestMergeIncremental_CompletedNodePreserved` | #1 | RED | 已完成节点且 intent 未变时不重置 |
| 5 | `TestMergeIncremental_ModifiedCompletedNode` | #5 | RED | 已完成节点 intent 变更时重置为 pending |
| 6 | `TestMergeIncremental_InvalidDependency` | #7 | RED | 引用不存在依赖时返回错误，result 为 nil |
| 7 | `TestMergeIncremental_CycleDependency` | #7 | RED | 循环依赖返回错误 |
| 8 | `TestMergeIncremental_DesiredNodesUpdated` | #3 | RED | 合并后 DesiredNodes 包含新增节点 |
| 9 | `TestIntentTree_ResetNode` | #5 | RED | 节点重置：State=pending，清除 Error/PID/Result/RetryCount，保留 MaxRetries/Timeout |

### Unit Tests — intent/decomposer_test.go (2 tests, appended)

| # | Test Name | AC | Status | Description |
|---|-----------|-----|--------|-------------|
| 10 | `TestDecomposer_DecomposeIncremental` | #1 | RED | 增量分解返回正确节点列表，prompt 包含现有节点上下文 |
| 11 | `TestDecomposer_DecomposeIncremental_InvalidJSON` | #1 | RED | LLM 返回无效 JSON 时报错 |

### Unit Tests — intent/manager_test.go (3 tests, appended)

| # | Test Name | AC | Status | Description |
|---|-----------|-----|--------|-------------|
| 12 | `TestManager_ApplyIncremental` | #1 | RED | 增量更新正确合并，返回 MergeResult |
| 13 | `TestManager_ApplyIncremental_NotFound` | #1 | RED | 不存在的 intentID 返回错误 |
| 14 | `TestManager_ApplyIncremental_TerminalState` | #1 | RED | 终态 intent 拒绝增量更新 |

### Unit Tests — intent/reconciler_test.go (2 tests, appended)

| # | Test Name | AC | Status | Description |
|---|-----------|-----|--------|-------------|
| 15 | `TestReconciler_InjectNodes` | #6 | RED | 运行时注入新节点到树，更新 DesiredNodes |
| 16 | `TestReconciler_InjectNodes_WithDependency` | #4, #6 | RED | 注入的节点有未完成依赖时保持 pending |

### Unit Tests — internal/ui/intent_test.go (5 tests, appended)

| # | Test Name | AC | Status | Description |
|---|-----------|-----|--------|-------------|
| 17 | `TestRenderIntentMergeResult_TTY` | #1 | RED | 合并结果 TTY 渲染包含 added 和 modified 节点 |
| 18 | `TestRenderIntentMergeResult_JSON` | #1 | RED | JSON 模式输出有效 JSON 结构 |
| 19 | `TestRenderIntentStatusDetail_TTY` | #2 | RED | 增强状态视图包含进度百分比、节点状态、活跃智能体 |
| 20 | `TestRenderIntentStatusDetail_TTY` | #2 | RED | 包含进度如 "2/5 (40%)" |
| 21 | `TestRenderIntentList_TTY` | #2 | RED | 意图列表渲染包含所有 intent ID 和描述 |
| 22 | `TestRenderIntentList_Empty` | #2 | RED | 空列表显示提示信息 |

---

## AC Coverage Matrix

| AC | Tests |
|----|-------|
| AC#1 增量差异仅执行新增/变更 | #1, #4, #10, #11, #12, #13, #14, #17, #18 |
| AC#2 status 显示完整状态 | #19, #20, #21, #22 |
| AC#3 新节点依赖已完成节点立即调度 | #1, #3, #8 |
| AC#4 新节点依赖未完成节点等待 | #16 |
| AC#5 修改已完成节点重置为 pending | #2, #5, #9 |
| AC#6 运行中节点不受影响 | #15, #16 |
| AC#7 无效依赖返回错误 | #6, #7 |

---

## Functions Under Test (Not Yet Implemented)

| Function | File | Status |
|----------|------|--------|
| `MergeIncremental(existing *IntentTree, newNodes []*IntentNode) (*MergeResult, error)` | `intent/merge.go` | NOT IMPLEMENTED |
| `IntentTree.ResetNode(nodeID string)` | `intent/types.go` | NOT IMPLEMENTED |
| `Decomposer.DecomposeIncremental(ctx, tree, newIntent, model)` | `intent/decomposer.go` | NOT IMPLEMENTED |
| `Manager.ApplyIncremental(ctx, intentID, newIntent, model)` | `intent/manager.go` | NOT IMPLEMENTED |
| `Reconciler.InjectNodes(nodes []*IntentNode) error` | `intent/reconciler.go` | NOT IMPLEMENTED |
| `RenderIntentMergeResult(r, added, modified, mode)` | `internal/ui/intent.go` | NOT IMPLEMENTED |
| `RenderIntentStatusDetail(r, tree, mode)` | `internal/ui/intent.go` | NOT IMPLEMENTED |
| `RenderIntentList(r, trees, mode)` | `internal/ui/intent.go` | NOT IMPLEMENTED |

---

## Types Under Test (Not Yet Implemented)

| Type | File | Status |
|------|------|--------|
| `MergeResult{AddedNodes, ModifiedNodes, UnchangedNodes}` | `intent/merge.go` | NOT IMPLEMENTED |
| `DriftNewRequirement DriftType = "new_requirement"` | `intent/types.go` | NOT IMPLEMENTED |
| `DriftNodeModified DriftType = "node_modified"` | `intent/types.go` | NOT IMPLEMENTED |

---

## Verification Status

- [x] All 22 tests written in RED phase (functions not yet implemented)
- [x] Tests call correct function signatures matching story spec
- [x] All 7 ACs covered by at least one test
- [x] Test patterns match existing codebase conventions
- [x] No side effects: tests are isolated and deterministic
- [ ] GREEN phase: Implementation pending (Story 19.3 dev-story)
