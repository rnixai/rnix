---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/14-2-recording-replay-and-navigation.md'
  - 'debug/record.go'
  - 'debug/recorder.go'
  - 'debug/record_manager.go'
  - 'debug/record_manager_test.go'
  - 'debug/record_test.go'
  - 'ipc/protocol.go'
  - 'cmd/rnix/gdb.go'
---

# ATDD Checklist - Epic 14, Story 2: 录制回放与导航

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

本 Story 实现执行录制的回放与导航功能，允许用户加载已完成的录制数据并通过交互式命令循环进行正向播放、反向单步和任意跳转。

**As a** 平台构建者
**I want** 回放录制的执行轨迹，支持正向播放、反向单步和任意跳转到指定时间点
**So that** 我可以自由地浏览智能体的历史执行过程

---

## Acceptance Criteria

1. **AC1:** 加载有效录制文件（completed/stopped），进入回放界面显示摘要
2. **AC2:** 正向播放（next/n）按时间顺序展示下一个 DebugEvent
3. **AC3:** 反向单步（prev/p）回退到上一个 DebugEvent
4. **AC4:** 跳转到指定事件编号（goto seq_num）
5. **AC5:** list 命令显示当前位置附近的事件概览列表

---

## Failing Tests Created (RED Phase)

### RecordReader Unit Tests (13 tests)

**File:** `debug/record_reader_test.go`

- 14.2-RDR-001: **TestRecordReader_LoadCompleted** — RED: RecordReader/NewRecordReader 未定义
  - 验证: AC1 - 加载 completed 状态的录制
- 14.2-RDR-002: **TestRecordReader_LoadStopped** — RED: RecordReader/NewRecordReader 未定义
  - 验证: AC1 - 加载 stopped 状态的录制
- 14.2-RDR-003: **TestRecordReader_RejectRecordingStatus** — RED: NewRecordReader 未定义
  - 验证: AC1 - 拒绝 recording 状态
- 14.2-RDR-004: **TestRecordReader_EmptyEvents** — RED: RecordReader 未定义
  - 验证: AC1 - 空 events.jsonl 处理
- 14.2-RDR-005: **TestRecordReader_CorruptedJSONLines** — RED: NewRecordReader 未定义
  - 验证: AC1 - 损坏 JSON 行跳过
- 14.2-RDR-006: **TestRecordReader_EventCount** — RED: RecordReader.EventCount 未定义
  - 验证: AC1 - 事件计数
- 14.2-RDR-007: **TestRecordReader_EventBySeqNum** — RED: RecordReader.Event 未定义
  - 验证: AC1 - 按 SeqNum 查找事件
- 14.2-RDR-008: **TestRecordReader_EventBySeqNum_NotFound** — RED: RecordReader.Event 未定义
  - 验证: AC1 - 查找不存在的 SeqNum
- 14.2-RDR-009: **TestRecordReader_Events** — RED: RecordReader.Events 未定义
  - 验证: AC1 - 返回完整事件列表
- 14.2-RDR-010: **TestRecordReader_EventsInRange** — RED: RecordReader.EventsInRange 未定义
  - 验证: AC1 - 范围查询
- 14.2-RDR-011: **TestRecordReader_Metadata** — RED: RecordReader.Metadata 未定义
  - 验证: AC1 - 元数据访问
- 14.2-RDR-012: **TestRecordReader_MissingMetadata** — RED: NewRecordReader 未定义
  - 验证: AC1 - 缺失 metadata.json 错误处理
- 14.2-RDR-013: **TestRecordReader_MissingEvents** — RED: NewRecordReader 未定义
  - 验证: AC1 - 缺失 events.jsonl 错误处理

### ReplaySession Unit Tests (18 tests)

**File:** `debug/replay_test.go`

- 14.2-REPLAY-001: **TestReplaySession_Next** — RED: NewReplaySession 未定义
  - 验证: AC2 - 正向单步
- 14.2-REPLAY-002: **TestReplaySession_Next_AtEnd** — RED: ReplaySession.Next 未定义
  - 验证: AC2 - 到末尾返回错误
- 14.2-REPLAY-003: **TestReplaySession_Next_FullTraversal** — RED: ReplaySession 未定义
  - 验证: AC2 - 完整正向遍历
- 14.2-REPLAY-004: **TestReplaySession_Prev** — RED: ReplaySession.Prev 未定义
  - 验证: AC3 - 反向单步
- 14.2-REPLAY-005: **TestReplaySession_Prev_AtBeginning** — RED: ReplaySession.Prev 未定义
  - 验证: AC3 - 到开头返回错误
- 14.2-REPLAY-006: **TestReplaySession_Prev_NotStarted** — RED: ReplaySession.Prev 未定义
  - 验证: AC3 - cursor 未启动时 Prev 错误
- 14.2-REPLAY-007: **TestReplaySession_Goto** — RED: ReplaySession.Goto 未定义
  - 验证: AC4 - 跳转到有效 SeqNum
- 14.2-REPLAY-008: **TestReplaySession_Goto_InvalidSeqNum** — RED: ReplaySession.Goto 未定义
  - 验证: AC4 - 跳转到无效 SeqNum
- 14.2-REPLAY-009: **TestReplaySession_Goto_Boundaries** — RED: ReplaySession.Goto 未定义
  - 验证: AC4 - 跳转到首末事件
- 14.2-REPLAY-010: **TestReplaySession_Current** — RED: ReplaySession.Current 未定义
  - 验证: AC2/3/4 - 当前事件
- 14.2-REPLAY-011: **TestReplaySession_Position** — RED: ReplaySession.Position 未定义
  - 验证: AC5 - 位置追踪
- 14.2-REPLAY-012: **TestReplaySession_List** — RED: ReplaySession.List 未定义
  - 验证: AC5 - 上下文窗口列表
- 14.2-REPLAY-013: **TestReplaySession_List_AtBeginning** — RED: ReplaySession.List 未定义
  - 验证: AC5 - 列表边界（开头）
- 14.2-REPLAY-014: **TestReplaySession_List_AtEnd** — RED: ReplaySession.List 未定义
  - 验证: AC5 - 列表边界（末尾）
- 14.2-REPLAY-015: **TestReplaySession_NextThenPrev** — RED: ReplaySession 未定义
  - 验证: AC2/3 - 混合导航
- 14.2-REPLAY-016: **TestReplaySession_GotoThenNext** — RED: ReplaySession 未定义
  - 验证: AC4/2 - Goto 后 Next
- 14.2-REPLAY-017: **TestReplaySession_EmptyRecording** — RED: ReplaySession 未定义
  - 验证: AC2 - 空录制处理
- 14.2-REPLAY-018: **TestReplayListItem_Fields** — RED: ReplayListItem 未定义
  - 验证: AC5 - ReplayListItem 结构体

### ReplayFormatter Unit Tests (8 tests)

**File:** `debug/replay_format_test.go`

- 14.2-FMT-001: **TestFormatReplayEvent_Syscall** — RED: FormatReplayEvent 未定义
  - 验证: AC2 - syscall 事件格式化
- 14.2-FMT-002: **TestFormatReplayEvent_LLM** — RED: FormatReplayEvent 未定义
  - 验证: AC2 - LLM 响应事件格式化
- 14.2-FMT-003: **TestFormatReplayEvent_Context** — RED: FormatReplayEvent 未定义
  - 验证: AC2 - 上下文快照格式化
- 14.2-FMT-004: **TestFormatReplayEvent_State** — RED: FormatReplayEvent 未定义
  - 验证: AC2 - 状态变更格式化
- 14.2-FMT-005: **TestFormatReplayList_CursorMarker** — RED: FormatReplayList 未定义
  - 验证: AC5 - 列表 cursor 标记
- 14.2-FMT-006: **TestFormatReplayList_Empty** — RED: FormatReplayList 未定义
  - 验证: AC5 - 空列表
- 14.2-FMT-007: **TestFormatReplaySummary** — RED: FormatReplaySummary 未定义
  - 验证: AC1 - 录制摘要格式化
- 14.2-FMT-008: **TestFormatReplayEvent_VerboseMode** — RED: FormatReplayEvent 未定义
  - 验证: AC2 - 详细模式

### RecordManager Extension Tests (4 tests)

**File:** `debug/record_manager_test.go` (追加)

- 14.2-MGR-001: **TestRecordManager_FindRecord** — RED: RecordManager.FindRecord 未定义
  - 验证: AC1 - 查找已存在的录制
- 14.2-MGR-002: **TestRecordManager_FindRecord_NotFound** — RED: RecordManager.FindRecord 未定义
  - 验证: AC1 - 查找不存在的录制
- 14.2-MGR-003: **TestRecordManager_LoadRecord** — RED: RecordManager.LoadRecord 未定义
  - 验证: AC1 - 加载录制为 RecordReader
- 14.2-MGR-004: **TestRecordManager_LoadRecord_NotFound** — RED: RecordManager.LoadRecord 未定义
  - 验证: AC1 - 加载不存在的录制

### IPC Server Tests (2 tests)

**File:** `ipc/server_test.go` (追加)

- 14.2-IPC-001: **TestServer_ReplayLoad** — RED: MethodReplayLoad/ReplayLoadRequest/ReplayLoadResponse 未定义
  - 验证: AC1 - replay_load IPC 请求响应
- 14.2-IPC-002: **TestServer_ReplayLoad_NotFound** — RED: MethodReplayLoad/ReplayLoadRequest 未定义
  - 验证: AC1 - 不存在的录制返回错误

### CLI Tests (3 tests)

**File:** `cmd/rnix/replay_test.go`

- 14.2-CLI-001: **TestReplayCommand_Registered** — RED: replayCmd 未定义
  - 验证: AC1-5 - replay 子命令注册
- 14.2-CLI-002: **TestReplayCommand_RequiresRecordID** — RED: replayCmd 未定义
  - 验证: AC1 - 需要 record-id 参数
- 14.2-CLI-003: **TestReplayCommand_JSONFlag** — RED: replayCmd 未定义
  - 验证: AC1-5 - --json flag 支持

---

## Test Data Helpers Created

### createTestRecording Helper

**File:** `debug/record_reader_test.go`

**功能:**
- `createTestRecording(t, status, eventCount)` — 创建临时录制目录，含 metadata.json 和 events.jsonl
- `writeTestMetadata(t, dir, meta)` — 写入测试用 metadata.json

**说明:** 使用 `t.TempDir()` 确保自动清理，符合 Go 测试最佳实践。

---

## Implementation Checklist

### Test: RecordReader (debug/record_reader.go)

**Tasks to make RecordReader tests pass:**

- [ ] 定义 `RecordReader` 结构体（dir, metadata, events 字段）
- [ ] 实现 `NewRecordReader(recordDir string) (*RecordReader, error)`
  - [ ] 读取 metadata.json
  - [ ] 验证 status 不是 "recording"
  - [ ] 读取 events.jsonl，逐行反序列化
  - [ ] 按 SeqNum 排序
  - [ ] 跳过损坏的 JSON 行
- [ ] 实现 `Metadata() RecordMetadata`
- [ ] 实现 `EventCount() int`
- [ ] 实现 `Event(seqNum uint64) (*RecordEvent, error)`
- [ ] 实现 `Events() []RecordEvent`
- [ ] 实现 `EventsInRange(from, to uint64) []RecordEvent`
- [ ] 运行测试: `go test ./debug/ -run TestRecordReader -v`

### Test: RecordManager.FindRecord/LoadRecord (debug/record_manager.go)

**Tasks to make tests pass:**

- [ ] 实现 `FindRecord(recordID string) (string, error)` — 在 baseDir 查找子目录
- [ ] 实现 `LoadRecord(recordID string) (*RecordReader, error)` — 调用 FindRecord + NewRecordReader
- [ ] 运行测试: `go test ./debug/ -run "TestRecordManager_(Find|Load)Record" -v`

### Test: ReplaySession (debug/replay.go)

**Tasks to make ReplaySession tests pass:**

- [ ] 定义 `ReplaySession` 结构体（reader, cursor 字段）
- [ ] 定义 `ReplayListItem` 结构体（Event, IsCursor 字段）
- [ ] 实现 `NewReplaySession(reader *RecordReader) *ReplaySession`
- [ ] 实现 `Next() (*RecordEvent, error)`
- [ ] 实现 `Prev() (*RecordEvent, error)`
- [ ] 实现 `Goto(seqNum uint64) (*RecordEvent, error)`
- [ ] 实现 `Current() (*RecordEvent, error)`
- [ ] 实现 `Position() (current int, total int)`
- [ ] 实现 `List(context int) []ReplayListItem`
- [ ] 运行测试: `go test ./debug/ -run TestReplaySession -v`

### Test: ReplayFormatter (debug/replay_format.go)

**Tasks to make formatter tests pass:**

- [ ] 实现 `FormatReplayEvent(event *RecordEvent, verbose bool) string`
- [ ] 实现 `FormatReplayList(items []ReplayListItem) string`
- [ ] 实现 `FormatReplaySummary(meta RecordMetadata, eventCount int) string`
- [ ] 运行测试: `go test ./debug/ -run TestFormat -v`

### Test: IPC replay_load (ipc/protocol.go, ipc/server.go)

**Tasks to make IPC tests pass:**

- [ ] 在 protocol.go 新增 `MethodReplayLoad Method = "replay_load"`
- [ ] 定义 `ReplayLoadRequest` 和 `ReplayLoadResponse` 类型
- [ ] 在 server.go 新增 `handleReplayLoad` 路由
- [ ] 运行测试: `go test ./ipc/ -run TestServer_ReplayLoad -v`

### Test: CLI replay (cmd/rnix/replay.go)

**Tasks to make CLI tests pass:**

- [ ] 创建 `replayCmd` Cobra 命令（Use: "replay"）
- [ ] 设置 Args 为 ExactArgs(1)
- [ ] 添加 --json flag
- [ ] 实现交互式命令循环（next/prev/goto/list/info/help/quit）
- [ ] 在 main.go 注册 replayCmd
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayCommand -v`

### Test: IPC Client (ipc/client.go)

**Tasks to make client method work:**

- [ ] 新增 `Client.ReplayLoad(recordID string) (*ReplayLoadResponse, error)`

---

## Running Tests

```bash
# Run all failing tests for this story (will fail due to missing implementation)
go test ./debug/ ./ipc/ ./cmd/rnix/ -run "14.2|RecordReader|ReplaySession|ReplayFormat|ReplayLoad|ReplayCommand" -v

# Run RecordReader tests only
go test ./debug/ -run TestRecordReader -v

# Run ReplaySession tests only
go test ./debug/ -run TestReplaySession -v

# Run ReplayFormatter tests only
go test ./debug/ -run "TestFormat" -v

# Run RecordManager extension tests only
go test ./debug/ -run "TestRecordManager_(Find|Load)Record" -v

# Run IPC tests only
go test ./ipc/ -run TestServer_ReplayLoad -v

# Run CLI tests only
go test ./cmd/rnix/ -run TestReplayCommand -v

# Run with race detector
go test -race ./debug/ ./ipc/ ./cmd/rnix/ -run "RecordReader|ReplaySession|ReplayFormat|ReplayLoad|ReplayCommand" -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 48 tests written and failing (compile errors for undefined types/functions)
- Test data helpers created (createTestRecording, writeTestMetadata)
- Test naming convention: `14.2-{MODULE}-{NUM}` following existing project pattern
- Implementation checklist created

**Verification:**

- All tests fail due to missing implementation (undefined symbols)
- Failure messages are clear and actionable
- Tests are designed for the correct behavior described in story ACs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. Implement `debug/record_reader.go` (RecordReader)
2. Extend `debug/record_manager.go` (FindRecord/LoadRecord)
3. Implement `debug/replay.go` (ReplaySession + ReplayListItem)
4. Implement `debug/replay_format.go` (FormatReplayEvent/List/Summary)
5. Add IPC protocol types and handler (ipc/protocol.go + ipc/server.go)
6. Add IPC client method (ipc/client.go)
7. Implement CLI command (cmd/rnix/replay.go + main.go registration)
8. Run tests after each module to verify green

---

## Test Statistics Summary

| Category | Count | Files |
|----------|-------|-------|
| RecordReader Unit | 13 | `debug/record_reader_test.go` |
| ReplaySession Unit | 18 | `debug/replay_test.go` |
| ReplayFormatter Unit | 8 | `debug/replay_format_test.go` |
| RecordManager Extension | 4 | `debug/record_manager_test.go` |
| IPC Integration | 2 | `ipc/server_test.go` |
| CLI Integration | 3 | `cmd/rnix/replay_test.go` |
| **Total** | **48** | **6 files** |

---

## Knowledge Base References Applied

- **test-quality.md** — Test isolation, determinism, one assertion per test
- **data-factories.md** — createTestRecording helper pattern
- **test-levels-framework.md** — Unit level for pure functions, Integration for IPC
- **test-priorities-matrix.md** — P0 for core functionality, P1 for edge cases and formatting
- **test-healing-patterns.md** — Error handling test patterns

---

## Notes

- Go ATDD RED phase = compile errors (undefined types/functions) rather than test.skip()
- All test files follow existing naming convention: `14.2-{MODULE}-{NUM}`
- Test helpers use `t.TempDir()` for automatic cleanup
- Tests are designed to pass with minimal correct implementation
- No mocks needed -- all tests use real file system via t.TempDir()

---

**Generated by BMad TEA Agent** - 2026-03-08
