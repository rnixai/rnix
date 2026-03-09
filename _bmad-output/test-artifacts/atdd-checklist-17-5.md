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
  - _bmad-output/implementation-artifacts/17-5-offline-replay-analysis.md
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
  - _bmad/tea/testarch/knowledge/test-healing-patterns.md
  - debug/record.go
  - debug/record_reader.go
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_test.go
---

# ATDD Checklist - Epic 17, Story 5: 离线回放分析

**日期:** 2026-03-09
**作者:** Decker
**主要测试级别:** Unit (Go testing)

---

## Story 概要

Dashboard 支持加载录制文件进行离线回放分析，用户可以在智能体完成后回顾和分析其历史执行。

**As a** 平台构建者
**I want** dashboard 支持加载录制文件进行离线回放分析
**So that** 我可以在智能体完成后回顾和分析其历史执行

---

## 验收标准

1. **AC#1:** `rnix dashboard --load <record-dir>` 从录制文件加载历史数据，所有窗格展示录制内容
2. **AC#2:** 支持播放/暂停、速度调节、时间轴拖拽跳转和逐帧前进/后退

---

## Failing Tests Created (RED Phase)

### Unit Tests (15 tests)

**文件:** `cmd/rnix/dashboard_test.go` (追加到现有文件)

- ✅ **Test:** `TestReplayDashboard_Init`
  - **状态:** RED — `newReplayDashboardModel` 桩返回空模型，字段全为零值
  - **验证:** AC#1 — 回放模型正确初始化所有字段（replayMode, replayCursor, replaySpeed 等）

- ✅ **Test:** `TestRecordEventToWire`
  - **状态:** RED — `recordEventToWire` 桩返回零值 SyscallEventWire
  - **验证:** AC#1 — RecordEvent → SyscallEventWire 转换正确映射字段

- ✅ **Test:** `TestRecordEventToWire_NonSyscall`
  - **状态:** PASS (零值恰好匹配期望行为)
  - **验证:** AC#1 — 非 syscall 事件返回零值 wire

- ✅ **Test:** `TestReplayDashboard_TreePane`
  - **状态:** RED — `buildReplayProcessTree` 桩返回 nil
  - **验证:** AC#1 — 从录制数据正确构建进程树

- ✅ **Test:** `TestReplayDashboard_Timeline`
  - **状态:** RED — `loadReplayTimeline` 桩返回 nil
  - **验证:** AC#1 — 正确加载并过滤事件到 cursor 位置

- ✅ **Test:** `TestReplayDashboard_Heatmap`
  - **状态:** RED — `buildReplayHeatmap` 桩返回 nil
  - **验证:** AC#1 — 正确找到最近的 ContextSnapshot 构建热力图

- ✅ **Test:** `TestReplayDashboard_PlayPause`
  - **状态:** RED — 回放键盘处理未实现
  - **验证:** AC#2 — Space 键切换播放状态

- ✅ **Test:** `TestReplayDashboard_SpeedControl`
  - **状态:** RED — 速度控制未实现
  - **验证:** AC#2 — `[`/`]` 键调整速率，边界 0.5x–8.0x

- ✅ **Test:** `TestReplayDashboard_FrameStep`
  - **状态:** RED — 逐帧控制未实现
  - **验证:** AC#2 — `.`/`,` 键逐帧前进/后退

- ✅ **Test:** `TestReplayDashboard_JumpStartEnd`
  - **状态:** RED — 跳转控制未实现
  - **验证:** AC#2 — `0`/`$` 键跳转到开头/末尾

- ✅ **Test:** `TestReplayDashboard_AutoPlayAdvance`
  - **状态:** RED — 自动播放推进未实现
  - **验证:** AC#2 — tick 中根据 speed 推进 cursor

- ✅ **Test:** `TestReplayDashboard_AutoPlayPauseAtEnd`
  - **状态:** RED — 自动暂停未实现
  - **验证:** AC#2 — 到达末尾自动暂停

- ✅ **Test:** `TestReplayDashboard_LiveKeysBlocked`
  - **状态:** RED — 回放模式键路由未实现
  - **验证:** AC#1,AC#2 — 回放模式屏蔽 k/a/l/r 键

- ✅ **Test:** `TestReplayDashboard_StatusBar`
  - **状态:** RED — 回放状态栏未实现
  - **验证:** AC#1,AC#2 — 状态栏显示 REPLAY、record-id，隐藏 live 键提示

- ✅ **Test:** `TestReplayDashboard_TickNoIPC`
  - **状态:** PASS (空模型不连接 IPC)
  - **验证:** AC#1 — 回放模式 tick 不尝试 IPC 连接

---

## Data Factories Created

### 测试录制数据工厂

**文件:** `cmd/rnix/dashboard_test.go` (内联 helper)

**Exports:**

- `newTestRecordReader(t)` — 创建含 5 个事件的测试 RecordReader（临时目录 + metadata.json + events.jsonl）
- `newTestReplayDashboardModel(t)` — 创建预配置的回放模式 dashboardModel

**事件类型覆盖:**
- 2 × RecordSyscall（Open, Write）
- 1 × RecordStateChange
- 1 × RecordContextSnapshot（含 Messages 和 TokenEstimate）
- 1 × RecordSyscall with Error（Read + EOF）

---

## Fixtures Created

### 测试数据

使用 Go `t.TempDir()` 自动清理机制，无需手动 teardown。

**Setup:**
- 创建临时目录
- 写入 `metadata.json`（RecordID="test-rec-001", PID=2, Intent="review", Status=completed）
- 写入 `events.jsonl`（5 个事件，涵盖所有 RecordEventType）
- 通过 `debug.NewRecordReader` 加载

**Cleanup:** Go testing 的 `t.TempDir()` 自动在测试结束后删除临时目录。

---

## Mock Requirements

无外部服务 Mock 需求。回放模式完全本地化：
- 不连接 IPC daemon
- 不访问网络
- 所有数据从临时目录的录制文件加载

---

## 桩函数 Created

**文件:** `cmd/rnix/dashboard.go`

| 桩函数 | 返回值 | 目的 |
|--------|--------|------|
| `newReplayDashboardModel(reader)` | `dashboardModel{}` | 编译通过，RED 测试失败 |
| `recordEventToWire(ev)` | `ipc.SyscallEventWire{}` | 编译通过，RED 测试失败 |
| `buildReplayProcessTree(reader, cursor)` | `nil` | 编译通过，RED 测试失败 |
| `loadReplayTimeline(reader, cursor)` | `nil` | 编译通过，RED 测试失败 |
| `buildReplayHeatmap(reader, cursor)` | `nil` | 编译通过，RED 测试失败 |
| `resolveRecordDir(loadArg)` | `"", error` | 编译通过，RED 测试失败 |

**新增字段** (`dashboardModel`):
- `replayMode bool`
- `replayReader *debug.RecordReader`
- `replayCursor int`
- `replayPlaying bool`
- `replaySpeed float64`
- `replayLastTick time.Time`
- `prevReplayCursor int`

---

## Implementation Checklist

### Test: TestReplayDashboard_Init

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 实现 `newReplayDashboardModel(reader)`: 设置 replayMode=true, replayReader=reader, replayCursor=-1, replaySpeed=1.0, connected=false, timelineFilters=defaultTimelineFilters(), recording=make(map[types.PID]string)
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_Init -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestRecordEventToWire + TestRecordEventToWire_NonSyscall

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 实现 `recordEventToWire(ev)`: 映射 Timestamp.Milliseconds()→TimestampMs, PID→PID, Syscall.Syscall→Syscall, Syscall.Args→Args, Syscall.Result→Result, Syscall.Err→Error, Syscall.Duration→DurationMs
- [ ] 仅对 ev.Type == RecordSyscall && ev.Syscall != nil 的事件转换
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestRecordEventToWire -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_TreePane

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 实现 `buildReplayProcessTree(reader, cursor)`: 从 reader.Metadata() 获取 PID/Intent，扫描 StateChange 和 ContextSnapshot 推导状态
- [ ] 构建 vfs.ProcInfo（PPID=1, state/intent/tokens）
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_TreePane -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_Timeline

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 实现 `loadReplayTimeline(reader, cursor)`: 过滤 RecordSyscall 事件，seqNum <= cursor
- [ ] 调用 recordEventToWire 转换，classifySyscall 分类
- [ ] cursor == -1 时返回空切片
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_Timeline -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_Heatmap

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 实现 `buildReplayHeatmap(reader, cursor)`: 反向扫描找最近的 RecordContextSnapshot
- [ ] 从 ContextSnapshotData 构建 CtxProfileResult
- [ ] 无 ContextSnapshot 时返回 nil
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_Heatmap -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_PlayPause

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 在 dashboardKey 中添加 replayMode 分支
- [ ] Space 键切换 replayPlaying: true↔false
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_PlayPause -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_SpeedControl

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] `]` 键翻倍 replaySpeed（最高 8.0x）
- [ ] `[` 键减半 replaySpeed（最低 0.5x）
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_SpeedControl -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_FrameStep

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] `.` 键: replayCursor++（不超过 EventCount-1），自动暂停
- [ ] `,` 键: replayCursor--（不低于 0），自动暂停
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_FrameStep -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_JumpStartEnd

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] `0` 键: replayCursor = 0，暂停
- [ ] `$` 键: replayCursor = EventCount-1，暂停
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_JumpStartEnd -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_AutoPlayAdvance

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 在 dashboardTick 中添加 replayMode 分支
- [ ] 若 replayPlaying，根据 replaySpeed 推进 cursor: eventsPerTick = int(replaySpeed)
- [ ] speed < 1.0 时使用 heatmapTickCount 计数逻辑
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_AutoPlayAdvance -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_AutoPlayPauseAtEnd

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] cursor 达到 EventCount-1 时设置 replayPlaying = false
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_AutoPlayPauseAtEnd -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_LiveKeysBlocked

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 回放模式下 k/a/l/r 键设置 statusMsg = "Not available in replay mode"
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_LiveKeysBlocked -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_StatusBar

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 回放模式替换状态栏: 显示 "▶ REPLAY: <record-id>" 或 "⏸ REPLAY: <record-id>"
- [ ] 显示进度 [cursor/total] speed×
- [ ] 不显示 live 模式 k:Kill 等提示
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_StatusBar -v`
- [ ] ✅ 测试通过 (green phase)

### Test: TestReplayDashboard_TickNoIPC

**文件:** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务:**

- [ ] 回放模式 tick 跳过 IPC 连接逻辑
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayDashboard_TickNoIPC -v`
- [ ] ✅ 测试通过 (green phase) — 已通过

---

## Running Tests

```bash
# 运行所有 17-5 failing tests
go test ./cmd/rnix/ -run "TestReplayDashboard|TestRecordEventToWire" -v -count=1

# 运行特定测试
go test ./cmd/rnix/ -run TestReplayDashboard_Init -v -count=1

# 运行所有 dashboard tests（包括 17.1-17.4）
go test ./cmd/rnix/ -run "TestDashboardModel|TestReplayDashboard|TestRecordEventToWire|TestClassify|TestCategory|TestBuildHeatmap|TestMapConsumer" -v -count=1

# 运行并显示测试覆盖率
go test ./cmd/rnix/ -run "TestReplayDashboard|TestRecordEventToWire" -cover -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent 职责:**

- ✅ 15 个测试编写完成，13 个失败如预期
- ✅ 测试 helper 创建（newTestRecordReader, newTestReplayDashboardModel）
- ✅ 桩函数和字段添加（编译通过）
- ✅ 现有测试（17.1-17.4）未被破坏
- ✅ Implementation checklist 创建

**验证:**

- 所有测试编译通过（go vet ✅）
- 13/15 测试失败如预期（RED）
- 2/15 测试通过（零值恰好匹配期望行为）
- 失败原因全部是未实现的逻辑，非测试 bug

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent 职责:**

1. **选取一个失败测试**（建议从 TestReplayDashboard_Init 开始）
2. **阅读测试**理解期望行为
3. **实现最小代码**使该测试通过
4. **运行测试**验证通过 (green)
5. **标记任务完成**
6. **继续下一个测试**

**建议实现顺序:**
1. TestReplayDashboard_Init → Task 2 (model fields)
2. TestRecordEventToWire → Task 3 (conversion)
3. TestReplayDashboard_TreePane → Task 4 (tree pane)
4. TestReplayDashboard_Timeline → Task 5 (timeline pane)
5. TestReplayDashboard_Heatmap → Task 6 (heatmap pane)
6. TestReplayDashboard_PlayPause → Task 7 (play/pause)
7. TestReplayDashboard_SpeedControl → Task 7 (speed)
8. TestReplayDashboard_FrameStep → Task 8 (frame step)
9. TestReplayDashboard_JumpStartEnd → Task 8 (jump)
10. TestReplayDashboard_AutoPlayAdvance → Task 11 (tick)
11. TestReplayDashboard_AutoPlayPauseAtEnd → Task 11 (auto-pause)
12. TestReplayDashboard_LiveKeysBlocked → Task 10 (key routing)
13. TestReplayDashboard_StatusBar → Task 9 (status bar)

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有 15 个测试通过
2. 检查代码质量（可读性、可维护性）
3. 提取重复逻辑
4. 确保测试仍然通过
5. 更新文档

---

## Next Steps

1. **将此 checklist 和 failing tests 交给 dev workflow**
2. **运行 failing tests 确认 RED phase:** `go test ./cmd/rnix/ -run "TestReplayDashboard|TestRecordEventToWire" -v -count=1`
3. **开始实现**，使用 implementation checklist 作为指南
4. **一次一个测试** (red → green)
5. **所有测试通过后** refactor
6. **refactor 完成后** 在 sprint-status.yaml 中更新故事状态为 done

---

## Knowledge Base References Applied

- **test-quality.md** — 测试质量原则：确定性、隔离性、显式断言、<300 行
- **test-levels-framework.md** — 测试级别选择：纯 Go 后端使用 Unit 测试
- **test-healing-patterns.md** — 失败模式识别（适配为 Go nil pointer 保护）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./cmd/rnix/ -run "TestReplayDashboard|TestRecordEventToWire" -v -count=1`

**Summary:**

- Total tests: 15
- Passing: 2 (TestRecordEventToWire_NonSyscall, TestReplayDashboard_TickNoIPC)
- Failing: 13 (expected — stubs return zero values)
- Status: ✅ RED phase verified

**Expected Failure Messages:**
- `replayMode should be true` — newReplayDashboardModel 桩未设置 replayMode
- `TimestampMs should be 100, got 0` — recordEventToWire 桩返回零值
- `expected at least 1 process` — buildReplayProcessTree 桩返回 nil
- `cursor=2: expected 2 syscall events, got 0` — loadReplayTimeline 桩返回 nil
- `should return non-nil for recording with ContextSnapshot` — buildReplayHeatmap 桩返回 nil
- `Space should toggle replayPlaying to true` — 回放键盘处理未实现
- `] should double speed` — 速度控制未实现
- `. should advance cursor` — 逐帧控制未实现
- `0 should jump to start` — 跳转未实现
- `tick should advance cursor` — 自动播放未实现
- `replayReader should not be nil` — 桩未初始化 reader
- `key "k" should set statusMsg` — live 键屏蔽未实现
- `status bar should contain 'REPLAY'` — 回放状态栏未实现

---

## Notes

- Go ATDD 的 RED phase 通过桩函数返回零值实现（等价于 TypeScript 的 test.skip()）
- 2 个测试在 RED phase 即通过，因为零值恰好是期望行为（TestRecordEventToWire_NonSyscall, TestReplayDashboard_TickNoIPC）
- 测试使用 `t.TempDir()` 自动清理，无需手动 teardown
- 所有修改限制在 `dashboard.go` 和 `dashboard_test.go`，未影响其他包

---

**Generated by BMad TEA Agent** — 2026-03-09
