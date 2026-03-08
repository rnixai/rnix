---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-checklist']
lastStep: 'step-05-checklist'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/planning-artifacts/epics/epic-14-时间旅行调试-time-travel-debugging.md'
  - '_bmad-output/implementation-artifacts/14-3-context-snapshot-diff.md'
  - 'debug/record.go'
  - 'debug/replay.go'
  - 'debug/replay_format.go'
  - 'debug/record_reader.go'
  - 'cmd/rnix/replay.go'
---

# ATDD Checklist - Epic 14, Story 3: 上下文快照对比

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Story Summary

用户在回放过程中查看任意两个时间点之间的上下文差异，准确理解哪个步骤导致了上下文的关键变化。

**As a** 平台构建者
**I want** 在回放过程中查看任意两个时间点之间的上下文差异
**So that** 我可以准确理解哪个步骤导致了上下文的关键变化

---

## Acceptance Criteria

1. **AC1**: 用户执行 `diff <seq1> <seq2>` 时，系统展示两个时间点之间的上下文差异，高亮标记新增、删除和修改的内容
2. **AC2**: diff 包含 token 消耗变化时，同时显示各段 token 增减量
3. **AC3**: 指定的时间点没有 context_snapshot 时，自动查找最近的前一个 context_snapshot
4. **AC4**: 录制中没有任何 context_snapshot 时，提示 "no context snapshots available"
5. **AC5**: 只提供一个时间点时，展示该时间点与当前 cursor 位置之间的差异

---

## Failing Tests Created (RED Phase)

### Unit Tests (20 tests)

**File:** `debug/snapshot_diff_test.go` (约 340 行)

SnapshotFinder Tests (AC #3, #4):

- RED **14.3-SNAP-001:** TestNewSnapshotFinder
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** NewSnapshotFinder 构造函数存在

- RED **14.3-SNAP-002:** TestSnapshotFinder_HasSnapshots_True
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** HasSnapshots 在有 context_snapshot 事件时返回 true

- RED **14.3-SNAP-003:** TestSnapshotFinder_HasSnapshots_False
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** HasSnapshots 在无 context_snapshot 事件时返回 false

- RED **14.3-SNAP-004:** TestSnapshotFinder_FindNearestBefore_ExactMatch
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** SeqNum 本身是 context_snapshot 时直接返回

- RED **14.3-SNAP-005:** TestSnapshotFinder_FindNearestBefore_SearchBackward
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** 向前搜索找到最近的 context_snapshot

- RED **14.3-SNAP-006:** TestSnapshotFinder_FindNearestBefore_SecondSnapshot
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** 正确找到第二个 snapshot（非第一个）

- RED **14.3-SNAP-007:** TestSnapshotFinder_FindNearestBefore_NoSnapshotBefore
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** SeqNum 前没有 snapshot 时返回错误

- RED **14.3-SNAP-008:** TestSnapshotFinder_FindNearestBefore_NoSnapshots
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** 事件列表无 snapshot 时返回错误

- RED **14.3-SNAP-009:** TestSnapshotFinder_FindNearestBefore_EmptyEvents
  - **Status:** RED - undefined: NewSnapshotFinder
  - **Verifies:** 空事件列表时返回错误

ComputeContextDiff Tests (AC #1, #2):

- RED **14.3-DIFF-001:** TestComputeContextDiff_MessageAdditions
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 检测消息新增

- RED **14.3-DIFF-002:** TestComputeContextDiff_SystemPromptChanged
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 检测 system prompt hash 变化

- RED **14.3-DIFF-003:** TestComputeContextDiff_SystemPromptUnchanged
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 相同 hash 标记为未变化

- RED **14.3-DIFF-004:** TestComputeContextDiff_TokenDeltaPositive
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 正向 token 增减计算

- RED **14.3-DIFF-005:** TestComputeContextDiff_TokenDeltaNegative
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 负向 token 增减计算

- RED **14.3-DIFF-006:** TestComputeContextDiff_NoChanges
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 完全相同的 snapshot 返回无差异

- RED **14.3-DIFF-007:** TestComputeContextDiff_MessageRemovals
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 检测消息删除

- RED **14.3-DIFF-008:** TestComputeContextDiff_Timestamps
  - **Status:** RED - undefined: ComputeContextDiff
  - **Verifies:** 从事件中正确提取时间戳

FormatContextDiff Tests (AC #1, #2):

- RED **14.3-FMT-001:** TestFormatContextDiff_WithChanges
  - **Status:** RED - undefined: FormatContextDiff/ContextDiff
  - **Verifies:** 有变化时的格式化输出

- RED **14.3-FMT-002:** TestFormatContextDiff_NoChanges
  - **Status:** RED - undefined: FormatContextDiff/ContextDiff
  - **Verifies:** 无变化时的格式化输出

- RED **14.3-FMT-003:** TestFormatContextDiff_SystemPromptChange
  - **Status:** RED - undefined: FormatContextDiff/ContextDiff
  - **Verifies:** system prompt 变化的显示

- RED **14.3-FMT-004:** TestFormatContextDiff_RemovedMessages
  - **Status:** RED - undefined: FormatContextDiff/ContextDiff
  - **Verifies:** 删除消息用 - 前缀显示

- RED **14.3-FMT-005:** TestFormatContextDiffJSON
  - **Status:** RED - undefined: FormatContextDiffJSON/ContextDiff
  - **Verifies:** JSON 格式输出有效且包含必需字段

- RED **14.3-FMT-006:** TestFormatContextDiff_NegativeTokenDelta
  - **Status:** RED - undefined: FormatContextDiff/ContextDiff
  - **Verifies:** 负 token 差的显示

Type Tests:

- RED **14.3-TYPE-001:** TestContextDiff_JSONSerialization
  - **Status:** RED - undefined: ContextDiff
  - **Verifies:** ContextDiff 结构体的 JSON 序列化/反序列化

### Integration Tests - ReplaySession (8 tests)

**File:** `debug/replay_test.go` (新增约 180 行)

- RED **14.3-REPLAY-001:** TestReplaySession_Diff
  - **Status:** RED - session.Diff undefined
  - **Verifies:** 两个有效 SeqNum 的 diff

- RED **14.3-REPLAY-002:** TestReplaySession_Diff_AutoFindSnapshots
  - **Status:** RED - session.Diff undefined
  - **Verifies:** 自动查找最近 snapshot (AC #3)

- RED **14.3-REPLAY-003:** TestReplaySession_Diff_NoSnapshots
  - **Status:** RED - session.Diff undefined
  - **Verifies:** 无 snapshot 时的错误处理 (AC #4)

- RED **14.3-REPLAY-004:** TestReplaySession_Diff_NoSnapshotBeforeSeq1
  - **Status:** RED - session.Diff undefined
  - **Verifies:** seq1 前无 snapshot 时返回错误

- RED **14.3-REPLAY-005:** TestReplaySession_Diff_DoesNotMoveCursor
  - **Status:** RED - session.Diff undefined
  - **Verifies:** diff 不影响 cursor 位置

- RED **14.3-REPLAY-006:** TestReplaySession_DiffFromCursor
  - **Status:** RED - session.DiffFromCursor undefined
  - **Verifies:** 从 cursor 位置 diff (AC #5)

- RED **14.3-REPLAY-007:** TestReplaySession_DiffFromCursor_NotStarted
  - **Status:** RED - session.DiffFromCursor undefined
  - **Verifies:** cursor 未开始时返回错误

- RED **14.3-REPLAY-008:** TestReplaySession_DiffFromCursor_AfterNavigation
  - **Status:** RED - session.DiffFromCursor undefined
  - **Verifies:** 导航后使用 cursor 位置

### CLI Tests (2 tests)

**File:** `cmd/rnix/replay_test.go` (新增约 30 行)

- RED **14.3-CLI-001:** TestReplayCommand_DiffInHelp
  - **Status:** RED - help 输出不包含 "diff"
  - **Verifies:** diff 命令在 help 中有记录

- RED **14.3-CLI-002:** TestReplayCommand_DiffHelpFormat
  - **Status:** RED - help 输出不包含 "diff"
  - **Verifies:** diff 命令的用法格式在 help 中显示

---

## Mock Requirements

无外部服务需要 mock。所有 diff 逻辑是纯本地操作，基于内存中的事件列表。

---

## Implementation Checklist

### Test: SnapshotFinder (14.3-SNAP-001 ~ 009)

**File:** `debug/snapshot_diff_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/snapshot_diff.go` 中定义 `SnapshotFinder` 结构体
- [ ] 实现 `NewSnapshotFinder(events []RecordEvent) *SnapshotFinder`
- [ ] 实现 `HasSnapshots() bool`
- [ ] 实现 `FindNearestBefore(seqNum uint64) (*RecordEvent, error)`
- [ ] Run test: `go test ./debug/ -run "TestSnapshotFinder" -v`
- [ ] All 9 SnapshotFinder tests pass (green phase)

### Test: ComputeContextDiff (14.3-DIFF-001 ~ 008)

**File:** `debug/snapshot_diff_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/snapshot_diff.go` 中定义 `ContextDiff`, `PromptDiff`, `MessagesDiff`, `TokenDelta` 结构体
- [ ] 实现 `ComputeContextDiff(from, to *ContextSnapshotData, fromEv, toEv *RecordEvent) *ContextDiff`
- [ ] Messages diff 使用共同前缀算法
- [ ] Run test: `go test ./debug/ -run "TestComputeContextDiff" -v`
- [ ] All 8 ComputeContextDiff tests pass (green phase)

### Test: FormatContextDiff (14.3-FMT-001 ~ 006)

**File:** `debug/snapshot_diff_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `FormatContextDiff(diff *ContextDiff) string`
- [ ] 实现 `FormatContextDiffJSON(diff *ContextDiff) ([]byte, error)`
- [ ] 格式化输出包含 header、system prompt、messages、tokens 各段
- [ ] Run test: `go test ./debug/ -run "TestFormatContextDiff" -v`
- [ ] All 6 FormatContextDiff tests pass (green phase)

### Test: ReplaySession.Diff/DiffFromCursor (14.3-REPLAY-001 ~ 008)

**File:** `debug/replay_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/replay.go` 中新增 `Diff(seq1, seq2 uint64) (*ContextDiff, error)` 方法
- [ ] 在 `debug/replay.go` 中新增 `DiffFromCursor(seq uint64) (*ContextDiff, error)` 方法
- [ ] Diff 使用 SnapshotFinder 查找 context_snapshot
- [ ] DiffFromCursor 使用 cursor 位置作为 T1
- [ ] Run test: `go test ./debug/ -run "14.3-REPLAY" -v`
- [ ] All 8 ReplaySession tests pass (green phase)

### Test: CLI diff command (14.3-CLI-001 ~ 002)

**File:** `cmd/rnix/replay_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/rnix/replay.go` 的 switch/case 中新增 `diff` 命令分支
- [ ] 支持 `diff <seq1> <seq2>` 双参数形式
- [ ] 支持 `diff <seq>` 单参数形式
- [ ] 在 `printReplayHelp` 中添加 diff 命令说明
- [ ] Run test: `go test ./cmd/rnix/ -run "14.3-CLI" -v`
- [ ] All 2 CLI tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./debug/ ./cmd/rnix/ -run "14\.3" -v -count=1

# Run specific test file (snapshot_diff only)
go test ./debug/ -run "TestSnapshotFinder|TestComputeContextDiff|TestFormatContextDiff|TestContextDiff" -v -count=1

# Run replay session diff tests
go test ./debug/ -run "TestReplaySession_Diff" -v -count=1

# Run CLI diff tests
go test ./cmd/rnix/ -run "TestReplayCommand_Diff" -v -count=1

# Run all debug package tests
go test ./debug/ -v -count=1

# Run with race detector
go test ./debug/ -race -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 30 tests written and failing
- Test data helpers created (buildSnapshotTestEvents, buildNoSnapshotEvents, createTestRecordingWithSnapshots)
- Implementation checklist created
- No external mocks needed

**Verification:**

- `debug/snapshot_diff_test.go`: 23 tests - compile failure (undefined types/functions)
- `debug/replay_test.go`: 8 new tests - compile failure (undefined methods)
- `cmd/rnix/replay_test.go`: 2 new tests - runtime failure (help output missing "diff")

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. 创建 `debug/snapshot_diff.go` — 定义 SnapshotFinder、ContextDiff 等类型和函数
2. 扩展 `debug/replay.go` — 新增 Diff/DiffFromCursor 方法
3. 扩展 `cmd/rnix/replay.go` — 新增 diff 命令分支 + 更新 help
4. 逐个测试通过后检查

---

### REFACTOR Phase (After All Tests Pass)

- 确认所有测试通过
- 检查代码质量
- 确保 diff 不影响 replay 现有功能

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./debug/ ./cmd/rnix/ -run "14\.3" -v -count=1`

**Results:**

```
# github.com/rnixai/rnix/debug [github.com/rnixai/rnix/debug.test]
debug/replay_test.go:553:23: session.Diff undefined (type *ReplaySession has no field or method Diff)
debug/replay_test.go:580:23: session.Diff undefined (type *ReplaySession has no field or method Diff)
debug/replay_test.go:602:19: session.Diff undefined (type *ReplaySession has no field or method Diff)
debug/replay_test.go:623:19: session.Diff undefined (type *ReplaySession has no field or method Diff)
debug/replay_test.go:644:10: session.Diff undefined (type *ReplaySession has no field or method Diff)
debug/replay_test.go:668:23: session.DiffFromCursor undefined (type *ReplaySession has no field or method DiffFromCursor)
debug/replay_test.go:691:19: session.DiffFromCursor undefined (type *ReplaySession has no field or method DiffFromCursor)
debug/replay_test.go:711:23: session.DiffFromCursor undefined (type *ReplaySession has no field or method DiffFromCursor)
debug/snapshot_diff_test.go:66:12: undefined: NewSnapshotFinder
debug/snapshot_diff_test.go:75:12: undefined: NewSnapshotFinder
debug/snapshot_diff_test.go:75:12: too many errors
FAIL    github.com/rnixai/rnix/debug [build failed]

--- FAIL: TestReplayCommand_DiffInHelp (0.00s)
    replay_test.go:83: expected replay help to contain 'diff' command
--- FAIL: TestReplayCommand_DiffHelpFormat (0.00s)
    replay_test.go:93: expected help to show diff usage
FAIL    github.com/rnixai/rnix/cmd/rnix
```

**Summary:**

- Total tests: 30 (23 unit + 5 integration + 2 CLI)
- Passing: 0 (expected)
- Failing: 30 (expected - compile errors + runtime failures)
- Status: RED phase verified

---

## Notes

- 所有 diff 逻辑应集中在 `debug/snapshot_diff.go` 一个新文件中
- ReplaySession 扩展方法加在 `debug/replay.go` 中
- diff 是纯本地操作，不需要 IPC 连接
- 测试使用 `buildSnapshotTestEvents()` 和 `buildNoSnapshotEvents()` 辅助函数构建测试数据
- 测试使用 `createTestRecordingWithSnapshots()` 创建包含 context_snapshot 的完整录制目录

---

**Generated by BMad TEA Agent** - 2026-03-08
