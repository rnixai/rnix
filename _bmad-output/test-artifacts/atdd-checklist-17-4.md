---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-09'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/17-4-pane-linkage-and-process-operations.md
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_test.go
---

# ATDD Checklist - Epic 17, Story 4: 窗格联动与进程操作

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit

---

## Story Summary

实现 dashboard 中智能体节点选中时的即时窗格联动（timeline 和 heatmap 自动切换），以及全局进程操作快捷键（kill/gdb/log/record），让用户可以在多个视图间高效切换并快速执行操作。

**As a** 平台构建者
**I want** 在 dashboard 中点击智能体节点自动联动切换其他窗格，并可直接对进程执行操作
**So that** 我可以高效地在多个视图间切换并快速执行操作

---

## Acceptance Criteria

1. **AC1:** 用户在智能体树中点击一个节点时，时间线窗格和热力图窗格自动切换到该智能体的数据
2. **AC2:** 用户选中一个进程后，可通过快捷键（k=kill / a=attach gdb / l=view log / r=start recording）执行操作，界面更新反映操作结果，敏感操作（kill）需确认

---

## Failing Tests Created (RED Phase)

### Unit Tests (15 tests)

**File:** `cmd/rnix/dashboard_test.go` (1523 lines)

- **Test:** `TestDashboardModel_PIDChangeImmediateLinkage`
  - **Status:** RED — cmd is nil (tree key handler returns nil instead of timeline+heatmap fetch cmds)
  - **Verifies:** AC1 — tree j/k 导航改变 selectedPID 时即时返回联动命令

- **Test:** `TestDashboardModel_HandlePIDChangeNoPID`
  - **Status:** GREEN (regression) — stub naturally returns nil cmd for PID=0
  - **Verifies:** AC1 — selectedPID=0 时不发起 IPC 调用

- **Test:** `TestDashboardModel_HandlePIDChangeClearsData`
  - **Status:** RED — stub handlePIDChange doesn't clear timeline/heatmap data
  - **Verifies:** AC1 — PID 变化时清空旧 timeline 事件和 heatmap profile

- **Test:** `TestDashboardModel_GlobalKillConfirmTimeline`
  - **Status:** RED — k in timeline goes to handleTimelineKey (cursor movement) instead of kill
  - **Verifies:** AC2 — 非 tree 窗格按 k 触发 kill 确认

- **Test:** `TestDashboardModel_GlobalKillNoSelectedPID`
  - **Status:** GREEN (regression) — k without PID doesn't trigger kill
  - **Verifies:** AC2 — 无选中进程时 k 不触发操作

- **Test:** `TestDashboardModel_TreeKNavigatesUpNotKill`
  - **Status:** GREEN (regression) — tree k navigates up as expected
  - **Verifies:** AC2 — tree 窗格中 k 保持向上导航，不触发 kill

- **Test:** `TestExecResultMsg_Success`
  - **Status:** RED — stub handler doesn't set statusMsg
  - **Verifies:** AC2 — GDB/Log 执行成功后设置状态消息

- **Test:** `TestExecResultMsg_Error`
  - **Status:** RED — stub handler doesn't set statusMsg
  - **Verifies:** AC2 — GDB/Log 执行失败时显示错误消息

- **Test:** `TestRecordToggleMsg_Start`
  - **Status:** RED — stub handler doesn't update recording map
  - **Verifies:** AC2 — 启动录制时 recording map 写入 recordID

- **Test:** `TestRecordToggleMsg_Stop`
  - **Status:** RED — stub handler doesn't clear recording map
  - **Verifies:** AC2 — 停止录制时 recording map 清除 PID 条目

- **Test:** `TestRecordToggleMsg_Error`
  - **Status:** RED — stub handler doesn't set statusMsg
  - **Verifies:** AC2 — 录制操作错误时显示错误消息

- **Test:** `TestDashboardModel_StatusBarRecording`
  - **Status:** RED — renderDashboardStatus doesn't check recording map
  - **Verifies:** AC2 — 录制中显示 ●REC 红色指示符

- **Test:** `TestDashboardModel_StatusBarOperationKeys`
  - **Status:** RED — status bar doesn't contain k:Kill/a:GDB/l:Log/r:Record hints
  - **Verifies:** AC2 — 状态栏显示全局操作键提示

- **Test:** `TestDashboardModel_StatusMsgTTL`
  - **Status:** RED — dashboardTick doesn't decrement statusMsgTTL
  - **Verifies:** AC2 — statusMsg 自动清空倒计时

- **Test:** `TestDashboardModel_TreeRecordingIndicator`
  - **Status:** RED — renderDashboardTreePane doesn't check recording map
  - **Verifies:** AC2 — 录制中的 PID 行尾追加红色 ● 指示符

---

## Data Factories Created

N/A — Go 测试使用 `mockDashboardProcs()`, `newTestDashboardModel()`, `newTestTimelineDashboardModel()` 等已有测试辅助函数，无需单独工厂。

---

## Fixtures Created

N/A — Go 标准测试框架，使用已有测试辅助函数构造 dashboardModel。

---

## Mock Requirements

### IPC Socket

使用 `ipc.SocketPathOverride` 指向不存在的 socket 路径（`/tmp/rnix-nonexistent-dashboard-test.sock`），隔离测试不依赖真实 daemon。

已有模式，无需新建 mock。

---

## Required data-testid Attributes

N/A — 纯终端 TUI 应用（非浏览器 UI），无 data-testid 需求。

---

## Implementation Checklist

### Test: TestDashboardModel_PIDChangeImmediateLinkage

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `dashboardKey` 的 tree 导航分支中，当 `selectedPID` 变化时调用 `handlePIDChange()`
- [ ] `handlePIDChange` 返回的 cmd 包含 `startTimelineCmd` + `fetchHeatmapCmd`
- [ ] 将返回的 cmd 与现有 cmd batch 在一起
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_PIDChangeImmediateLinkage -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_HandlePIDChangeClearsData

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `handlePIDChange()` — 调用 `stopTimelineStream()`, `handleTimelinePIDChange()`, `handleHeatmapPIDChange()`
- [ ] 设置 `timelineAttachedPID` 和 `heatmapPID` 为 `selectedPID`
- [ ] PID>0 时返回 `tea.Batch(startTimelineCmd, fetchHeatmapCmd)`
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_HandlePIDChangeClearsData -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_GlobalKillConfirmTimeline

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `dashboardKey` 中，窗格特定键之后添加全局操作路由
- [ ] k 键在非 tree 窗格中触发 `confirmKill = true`
- [ ] 排除 timeline 窗格中已有 k 键冲突（timeline k = cursor up），或按路由优先级处理
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_GlobalKillConfirmTimeline -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestExecResultMsg_Success + TestExecResultMsg_Error

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `Update` 的 `case execResultMsg:` 中设置 `statusMsg`
- [ ] err==nil → `statusMsg = "Returned to dashboard"`
- [ ] err!=nil → `statusMsg = fmt.Sprintf("Command error: %v", msg.err)`
- [ ] 设置 `statusMsgTTL = 4`
- [ ] Run test: `go test ./cmd/rnix/ -run "TestExecResultMsg" -v`
- [ ] Tests pass (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestRecordToggleMsg_Start + Stop + Error

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `Update` 的 `case recordToggleMsg:` 中处理三种场景
- [ ] 启动成功 → `m.recording[pid] = recordID`, `statusMsg = "Recording started"`
- [ ] 停止成功 → `delete(m.recording, pid)`, `statusMsg = fmt.Sprintf("Recording stopped (%d events)", eventCount)`
- [ ] 错误 → `statusMsg = fmt.Sprintf("Record error: %v", err)`
- [ ] 设置 `statusMsgTTL = 4`
- [ ] Run test: `go test ./cmd/rnix/ -run "TestRecordToggleMsg" -v`
- [ ] Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_StatusBarRecording

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `renderDashboardStatus()` 中检查 `m.recording[m.selectedPID]`
- [ ] 录制中显示 `●REC` 红色指示符
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_StatusBarRecording -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestDashboardModel_StatusBarOperationKeys

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 更新 `renderDashboardStatus()` 默认返回值包含 `k:Kill  a:GDB  l:Log  r:Record`
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_StatusBarOperationKeys -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestDashboardModel_StatusMsgTTL

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `dashboardTick()` 开头添加 TTL 递减逻辑
- [ ] `statusMsgTTL > 0` → 递减，归零后清空 `statusMsg`
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_StatusMsgTTL -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestDashboardModel_TreeRecordingIndicator

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 `renderDashboardTreePane` 中检查 `m.recording[row.proc.PID]`
- [ ] 录制中的 PID 行尾追加红色 `●`
- [ ] Run test: `go test ./cmd/rnix/ -run TestDashboardModel_TreeRecordingIndicator -v`
- [ ] Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

## Running Tests

```bash
# Run all 17.4 failing tests
go test ./cmd/rnix/ -run "PIDChangeImmediate|HandlePIDChange|GlobalKill|TreeKNavigates|ExecResult|RecordToggle|StatusBar|StatusMsgTTL|TreeRecording" -v

# Run specific test
go test ./cmd/rnix/ -run TestDashboardModel_PIDChangeImmediateLinkage -v

# Run with race detector
go test ./cmd/rnix/ -run "PIDChangeImmediate|HandlePIDChange|GlobalKill|TreeKNavigates|ExecResult|RecordToggle|StatusBar|StatusMsgTTL|TreeRecording" -race -v

# Run with coverage
go test ./cmd/rnix/ -run "PIDChangeImmediate|HandlePIDChange|GlobalKill|TreeKNavigates|ExecResult|RecordToggle|StatusBar|StatusMsgTTL|TreeRecording" -cover -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 15 tests written and compiled
- 12/15 tests failing (RED) due to stub implementations
- 3/15 tests passing (GREEN regression) — expected behavior preserved
- Minimal stubs added to `dashboard.go` for compilation
- Implementation checklist created

**Verification:**

- All tests run and 12 fail as expected
- Failure messages are clear and actionable
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. Pick one failing test from implementation checklist
2. Read the test to understand expected behavior
3. Implement minimal code to make that specific test pass
4. Run the test to verify it now passes (green)
5. Check off the task in implementation checklist
6. Move to next test and repeat

**Recommended Implementation Order:**

1. `handlePIDChange()` 统一方法 (unblocks tests 001, 002, 003)
2. `statusMsgTTL` 递减逻辑 (test 014)
3. `execResultMsg` 处理 (tests 007, 008)
4. `recordToggleMsg` 处理 (tests 009, 010, 011)
5. 全局操作键路由 (test 004)
6. 状态栏更新 (tests 012, 013)
7. 树窗格录制指示 (test 015)

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 15 tests pass
2. Verify 17-1/17-2/17-3 tests still pass (regression)
3. `dashboardTick` 中复用 `handlePIDChange()`，移除重复逻辑 (DRY)
4. Run full suite: `go test ./cmd/rnix/ -v`

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./cmd/rnix/ -run "PIDChangeImmediate|HandlePIDChange|GlobalKill|TreeKNavigates|ExecResult|RecordToggle|StatusBar|StatusMsgTTL|TreeRecording" -v`

**Results:**

```
--- FAIL: TestDashboardModel_PIDChangeImmediateLinkage (0.00s)
--- PASS: TestDashboardModel_HandlePIDChangeNoPID (0.00s)
--- FAIL: TestDashboardModel_HandlePIDChangeClearsData (0.00s)
--- FAIL: TestDashboardModel_GlobalKillConfirmTimeline (0.00s)
--- PASS: TestDashboardModel_GlobalKillNoSelectedPID (0.00s)
--- PASS: TestDashboardModel_TreeKNavigatesUpNotKill (0.00s)
--- FAIL: TestExecResultMsg_Success (0.00s)
--- FAIL: TestExecResultMsg_Error (0.00s)
--- FAIL: TestRecordToggleMsg_Start (0.00s)
--- FAIL: TestRecordToggleMsg_Stop (0.00s)
--- FAIL: TestRecordToggleMsg_Error (0.00s)
--- FAIL: TestDashboardModel_StatusBarRecording (0.00s)
--- FAIL: TestDashboardModel_StatusBarOperationKeys (0.00s)
--- FAIL: TestDashboardModel_StatusMsgTTL (0.00s)
--- FAIL: TestDashboardModel_TreeRecordingIndicator (0.00s)
```

**Summary:**

- Total tests: 15
- Passing: 3 (regression tests — expected)
- Failing: 12 (expected — stubs not implemented)
- Status: RED phase verified

---

## Stubs Added (Compilation-Only)

**File:** `cmd/rnix/dashboard.go`

以下最小桩代码已添加以确保测试编译通过，但不含行为实现：

1. `execResultMsg` struct — 新消息类型
2. `recordToggleMsg` struct — 新消息类型
3. `recording map[types.PID]string` 字段 — dashboardModel
4. `statusMsgTTL int` 字段 — dashboardModel
5. `handlePIDChange() (dashboardModel, tea.Cmd)` — 返回 `(m, nil)` 桩
6. `case execResultMsg:` 在 Update 中 — 返回 `(m, nil)` 桩
7. `case recordToggleMsg:` 在 Update 中 — 返回 `(m, nil)` 桩
8. `recording` 初始化在 `newDashboardModel` 中

---

## Notes

- 全局操作键路由需注意 timeline 窗格 `l` 键冲突（timeline 中 l = 右滚），story dev notes 中有详细冲突解决方案
- `tea.ExecProcess` 暂停/恢复 TUI 的行为不可在 `go test` 中测试，仅测试消息处理（`execResultMsg`）
- `handlePIDChange` 统一方法需在 `dashboardTick` 中复用以消除重复逻辑 (DRY refactor)
- `os.Args[0]` 在测试中指向测试二进制，因此不测试实际子进程调用

---

## Next Steps

1. **Review this checklist** — 确认测试覆盖所有 AC
2. **Run failing tests** — `go test ./cmd/rnix/ -run "PIDChangeImmediate|HandlePIDChange|GlobalKill|TreeKNavigates|ExecResult|RecordToggle|StatusBar|StatusMsgTTL|TreeRecording" -v`
3. **Begin implementation** — 按 Implementation Checklist 顺序逐个实现
4. **Work one test at a time** — RED → GREEN for each
5. **When all pass** — 重构 DRY (dashboardTick 复用 handlePIDChange)
6. **Full regression** — `go test ./cmd/rnix/ -v`

---

**Generated by BMad TEA Agent** - 2026-03-09
