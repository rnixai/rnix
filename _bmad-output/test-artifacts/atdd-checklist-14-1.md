---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/14-1-execution-recording-and-persistence.md'
  - '_bmad/tea/config.yaml'
  - 'debug/strace.go'
  - 'debug/event.go'
  - 'debug/strace_test.go'
  - 'debug/event_test.go'
  - 'kernel/kernel.go'
  - 'ipc/server.go'
  - 'ipc/server_test.go'
  - 'cmd/rnix/gdb.go'
  - 'internal/types/types.go'
  - 'internal/xsync/syncmap.go'
---

# ATDD Checklist - Epic 14, Story 1: 执行录制与持久化

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with `go test -race`
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 14-1 approved with 3 clear acceptance criteria (AC #1-3)
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19 packages
- Development environment available

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/14-1-execution-recording-and-persistence.md`
- **Acceptance Criteria:** 3 ACs covering recording start/stop, data persistence to JSONL, performance overhead <= 20%
- **Affected Components:** `debug/` (new files), `kernel/kernel.go`, `ipc/server.go`, `ipc/client.go`, `cmd/rnix/` (new record.go)
- **Dependencies:** Builds on Epic 13 debug infrastructure (emitEvent, DebugChan, gdb command loop)

### Framework & Existing Patterns
- Existing test patterns in `debug/strace_test.go` and `debug/event_test.go` (debug package conventions)
- Existing IPC tests in `ipc/server_test.go` (setupTestServer helper, sendRequest helper, dial helper)
- Existing gdb CLI tests in `cmd/rnix/gdb_test.go`
- Helper: `setupTestServer()` creates full IPC test server with context manager
- Test pattern: Go standard table-driven tests, `t.TempDir()` for filesystem tests, `t.Helper()` for helpers
- Concurrency tests use `sync.WaitGroup` + goroutines with race detector

### TEA Config Flags
- `tea_use_playwright_utils`: true (irrelevant for backend)
- `tea_use_pactjs_utils`: true (irrelevant for backend)
- `tea_pact_mcp`: mcp (irrelevant for backend)
- `tea_browser_automation`: auto (irrelevant for backend)
- `test_stack_type`: auto -> backend

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (debug package, kernel hooks, IPC protocol, CLI commands), standard file I/O and data structure operations

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | `rnix record <pid>` 或 gdb `record start` 开始捕获 DebugEvent 写入磁盘 | Unit (debug) + IPC (server) + CLI (parser) | P0 |
| AC#2 | 录制数据持久化到 `$PROJECT/.rnix/records/<pid>-<timestamp>/`，JSONL 格式 | Unit (debug) | P0 |
| AC#3 | 录制性能开销 <= 20% (NFR32) | Benchmark (debug) | P0 |

### Test Levels

- **Unit Tests** (`debug/record_test.go`): RecordEvent 数据模型 JSON 序列化/反序列化
- **Unit Tests** (`debug/recorder_test.go`): Recorder 创建/写入/关闭、并发写入安全性
- **Unit Tests** (`debug/record_manager_test.go`): RecordManager 启动/停止/重复录制防护、ListRecords 扫描
- **Integration Tests** (`ipc/server_test.go`): record_start/record_stop IPC 路由
- **CLI Tests** (`cmd/rnix/record_test.go`): record CLI 命令注册和参数解析
- **Benchmark Tests** (`debug/recorder_bench_test.go`): WriteEvent < 100us 性能验证

### Red Phase Verification
- All tests reference types/functions that do NOT exist yet (RecordEvent, RecordMetadata, Recorder, RecordManager, record_start/record_stop IPC methods)
- Compilation will fail -> RED phase confirmed

---

## Failing Tests Created (RED Phase)

### Unit Tests: Data Model (4 tests)

**File:** `debug/record_test.go` (new)

- **14.1-MODEL-001** [P0] RecordEvent JSON 序列化包含所有必填字段
  - **Status:** RED - `RecordEvent` 类型不存在，编译失败
  - **Verifies:** AC#2 RecordEvent 结构体定义和 JSON 序列化

- **14.1-MODEL-002** [P0] RecordEvent 各 Type 常量 JSON 值正确
  - **Status:** RED - `RecordSyscall` / `RecordContextSnapshot` / `RecordLLMResponse` / `RecordStateChange` 常量不存在
  - **Verifies:** AC#2 RecordEventType 枚举定义

- **14.1-MODEL-003** [P0] RecordEvent omitempty -- 只序列化对应 Type 的子数据
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 JSON 序列化只包含非空子结构

- **14.1-MODEL-004** [P0] RecordMetadata JSON 序列化/反序列化 roundtrip
  - **Status:** RED - `RecordMetadata` 类型不存在
  - **Verifies:** AC#2 RecordMetadata 结构体定义

### Unit Tests: Recorder (7 tests)

**File:** `debug/recorder_test.go` (new)

- **14.1-REC-001** [P0] NewRecorder 创建目录和 events.jsonl 文件
  - **Status:** RED - `NewRecorder` 函数不存在，编译失败
  - **Verifies:** AC#1 AC#2 Recorder 创建录制目录结构

- **14.1-REC-002** [P0] Recorder.WriteEvent 写入 JSONL 格式（每行一个 JSON 对象）
  - **Status:** RED - `Recorder.WriteEvent` 方法不存在
  - **Verifies:** AC#2 JSONL 格式写入

- **14.1-REC-003** [P0] Recorder.WriteEvent 递增 SeqNum
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 事件序列号递增

- **14.1-REC-004** [P0] Recorder.Close 更新 metadata.json status 为 "completed"
  - **Status:** RED - `Recorder.Close` 方法不存在
  - **Verifies:** AC#2 正常结束录制

- **14.1-REC-005** [P0] Recorder.Stop 更新 metadata.json status 为 "stopped"
  - **Status:** RED - `Recorder.Stop` 方法不存在
  - **Verifies:** AC#1 用户手动停止录制

- **14.1-REC-006** [P0] NewRecorder 写入 metadata.json status 为 "recording"
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 初始 metadata 状态

- **14.1-REC-007** [P1] Recorder 并发 WriteEvent 安全性（race detector）
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 并发安全性

### Unit Tests: RecordManager (7 tests)

**File:** `debug/record_manager_test.go` (new)

- **14.1-MGR-001** [P0] StartRecording 创建 Recorder 并返回 recordID
  - **Status:** RED - `NewRecordManager` / `StartRecording` 不存在，编译失败
  - **Verifies:** AC#1 启动录制

- **14.1-MGR-002** [P0] StartRecording 同一 PID 重复录制返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 重复录制防护

- **14.1-MGR-003** [P0] StopRecording 停止并移除活跃录制
  - **Status:** RED - `StopRecording` 不存在
  - **Verifies:** AC#1 停止录制

- **14.1-MGR-004** [P0] StopRecording 不存在的 PID 返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 错误处理

- **14.1-MGR-005** [P0] RecordEvent 写入活跃录制
  - **Status:** RED - `RecordManager.RecordEvent` 不存在
  - **Verifies:** AC#1 事件转发

- **14.1-MGR-006** [P0] RecordEvent 无活跃录制时静默跳过
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 非录制进程无副作用

- **14.1-MGR-007** [P0] IsRecording 返回正确状态
  - **Status:** RED - `IsRecording` 不存在
  - **Verifies:** AC#1 录制状态查询

- **14.1-MGR-008** [P0] CloseAll 关闭所有活跃录制
  - **Status:** RED - `CloseAll` 不存在
  - **Verifies:** AC#1 进程退出时清理

- **14.1-MGR-009** [P0] ListRecords 扫描 baseDir 返回 metadata 列表
  - **Status:** RED - `ListRecords` 不存在
  - **Verifies:** AC#2 录制列表查询

### IPC Server Tests (4 tests)

**File:** `ipc/server_test.go` (appended)

- **14.1-IPC-001** [P0] Server handles record_start 返回 record_id
  - **Status:** RED - `handleRecordCommand` 不存在，IPC server 无 "record_start" 路由
  - **Verifies:** AC#1 IPC 端到端 record_start

- **14.1-IPC-002** [P0] Server handles record_stop 返回 event_count
  - **Status:** RED - handleRecordCommand 不存在
  - **Verifies:** AC#1 IPC 端到端 record_stop

- **14.1-IPC-003** [P1] Server handles record_start 不存在的 PID 返回错误
  - **Status:** RED - handleRecordCommand 不存在
  - **Verifies:** AC#1 错误处理

- **14.1-IPC-004** [P1] Server handles record_start Running 进程验证
  - **Status:** RED - handleRecordCommand 不存在
  - **Verifies:** AC#1 只能录制 Running 状态进程

### CLI Tests (4 tests)

**File:** `cmd/rnix/record_test.go` (new)

- **14.1-CLI-001** [P0] record start 子命令注册并解析 PID 参数
  - **Status:** RED - record.go 不存在，编译失败
  - **Verifies:** AC#1 CLI 命令注册

- **14.1-CLI-002** [P0] record stop 子命令注册并解析 PID 参数
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 CLI 命令注册

- **14.1-CLI-003** [P0] record list 子命令注册
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 CLI 命令注册

- **14.1-CLI-004** [P1] record start 无 PID 参数返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 错误处理

### Benchmark Tests (1 test)

**File:** `debug/recorder_bench_test.go` (new)

- **14.1-BENCH-001** [P0] BenchmarkRecorderWriteEvent 单次 < 100us
  - **Status:** RED - `Recorder` / `WriteEvent` 不存在
  - **Verifies:** AC#3 NFR32 性能要求

---

## Story Summary

**As a** 平台构建者
**I want** 对指定智能体开启完整执行录制，将所有 syscall、LLM 调用、上下文变更持久化到磁盘
**So that** 我可以在智能体完成后离线分析其完整执行历史

---

## Acceptance Criteria

1. `rnix record <pid>` 或 gdb `record start` 开始捕获 DebugEvent 写入磁盘
2. 录制数据持久化到 `$PROJECT/.rnix/records/<pid>-<timestamp>/`，JSONL 格式，包含完整 syscall 序列、上下文快照和 LLM 响应
3. 录制性能开销 <= 20% (NFR32)

---

## Implementation Checklist

### Test: 14.1-MODEL-001 to 14.1-MODEL-004 (Data Model Tests)

**File:** `debug/record_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/record.go` 中定义 `RecordEventType` 类型和常量：`RecordSyscall`, `RecordContextSnapshot`, `RecordLLMResponse`, `RecordStateChange`
- [ ] 定义 `RecordEvent` 结构体（SeqNum, Timestamp, PID, Type, Syscall, Context, LLM, State 字段）
- [ ] 定义子数据结构：`SyscallEventData`, `ContextSnapshotData`, `LLMResponseData`, `StateChangeData`
- [ ] 定义 `RecordMetadata` 结构体（RecordID, PID, Intent, StartTime, EndTime, EventCount, Status）
- [ ] 定义 `RecordStatus` 类型和常量：`StatusRecording`, `StatusCompleted`, `StatusStopped`
- [ ] 实现 `RecordEventFromSyscall(ev types.SyscallEvent) RecordEvent` 转换函数
- [ ] Run test: `go test -race -run 'TestRecordEvent|TestRecordMetadata' ./debug/`

---

### Test: 14.1-REC-001 to 14.1-REC-007 (Recorder Tests)

**File:** `debug/recorder_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/recorder.go` 中定义 `Recorder` 结构体（dir, file, writer, metadata, seqNum, mu, closed）
- [ ] 实现 `NewRecorder(baseDir string, pid types.PID, intent string) (*Recorder, error)`
  - 生成 recordID = `<pid>-<unix_timestamp>`
  - 创建目录 `baseDir/<recordID>/`
  - 创建 `events.jsonl` 文件
  - 初始化 bufio.Writer（64KB 缓冲）
  - 写入 `metadata.json`（status = "recording"）
- [ ] 实现 `Recorder.WriteEvent(event RecordEvent) error`
  - JSON marshal + 换行符
  - 写入 bufio.Writer
  - 每 100 个事件 Flush
  - 递增 seqNum 和 metadata.EventCount
- [ ] 实现 `Recorder.Close() error`（Flush + 关闭 + metadata status = "completed"）
- [ ] 实现 `Recorder.Stop() error`（Flush + 关闭 + metadata status = "stopped"）
- [ ] Run test: `go test -race -run 'TestRecorder|TestNewRecorder' ./debug/`

---

### Test: 14.1-MGR-001 to 14.1-MGR-009 (RecordManager Tests)

**File:** `debug/record_manager_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/record_manager.go` 中定义 `RecordManager` 结构体（baseDir, recorders *xsync.SyncMap）
- [ ] 实现 `NewRecordManager(baseDir string) *RecordManager`
- [ ] 实现 `StartRecording(pid types.PID, intent string) (string, error)`
- [ ] 实现 `StopRecording(pid types.PID) error`
- [ ] 实现 `RecordEvent(pid types.PID, event RecordEvent) error`
- [ ] 实现 `IsRecording(pid types.PID) bool`
- [ ] 实现 `CloseAll()`
- [ ] 实现 `ListRecords() ([]RecordMetadata, error)`
- [ ] Run test: `go test -race -run 'TestRecordManager' ./debug/`

---

### Test: 14.1-IPC-001 to 14.1-IPC-004 (IPC Server Tests)

**File:** `ipc/server_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ipc/server.go` 中新增 `record_start` 和 `record_stop` method 路由
- [ ] 实现 `handleRecordCommand(conn net.Conn, rawPayload json.RawMessage)`
- [ ] `record_start` -> 验证 PID 存在且 Running -> 调用 `s.kern.StartRecording(pid)` -> 返回 record_id
- [ ] `record_stop` -> 调用 `s.kern.StopRecording(pid)` -> 返回 event_count
- [ ] 在 `KernelImpl` 上新增 `StartRecording(pid types.PID) (string, error)` 和 `StopRecording(pid types.PID) error`
- [ ] Run test: `go test -race -run 'TestServer_RecordStart|TestServer_RecordStop' ./ipc/`

---

### Test: 14.1-CLI-001 to 14.1-CLI-004 (CLI Tests)

**File:** `cmd/rnix/record_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/rnix/record.go` 中注册 `record` Cobra 子命令
- [ ] 实现 `record start <pid>` 子命令（连接 IPC，发送 record_start）
- [ ] 实现 `record stop <pid>` 子命令（连接 IPC，发送 record_stop）
- [ ] 实现 `record list` 子命令（扫描录制目录或 IPC 查询）
- [ ] 在 `cmd/rnix/main.go` 中注册 record 子命令
- [ ] Run test: `go test -race -run 'TestRecordCommand' ./cmd/rnix/`

---

### Test: 14.1-BENCH-001 (Benchmark Tests)

**File:** `debug/recorder_bench_test.go`

**Tasks to make this test pass:**

- [ ] 确保 Recorder.WriteEvent 实现使用 bufio.Writer 缓冲
- [ ] 确保 JSON marshal 使用标准 encoding/json
- [ ] Run benchmark: `go test -bench BenchmarkRecorderWriteEvent -benchtime 5s ./debug/`
- [ ] 验证 ns/op < 100000 (100us)

---

### Additional Implementation Tasks (Not directly tested by ATDD but required by story)

- [ ] 在 `kernel/kernel.go` 的 `KernelImpl` 中新增 `recordMgr *debug.RecordManager` 字段
- [ ] 在 `emitEvent` 方法中新增录制钩子：检查 `recordMgr.IsRecording(proc.PID)` -> `recordMgr.RecordEvent()`
- [ ] 在 `reasonStep` 循环中 LLM 响应返回后新增 `RecordLLMResponse` 录制点
- [ ] 在 `reasonStep` 循环中 `CtxWrite` 后新增 `RecordContextSnapshot` 录制点
- [ ] 在 `finishProcess` 中新增 `RecordStateChange` 录制点
- [ ] 在 `reapProcess` 中自动调用 `recordMgr.StopRecording(pid)` 结束录制
- [ ] 在 `ipc/client.go` 中新增 `RecordStart` / `RecordStop` / `RecordList` 方法
- [ ] 在 `cmd/rnix/gdb.go` 命令循环中新增 `record` 分支
- [ ] 更新 `printGdbHelp` 增加 record 命令说明

---

## Running Tests

```bash
# Run all data model tests
go test -race -run 'TestRecordEvent|TestRecordMetadata' ./debug/

# Run all recorder tests
go test -race -run 'TestRecorder|TestNewRecorder' ./debug/

# Run all record manager tests
go test -race -run 'TestRecordManager' ./debug/

# Run all IPC server tests for recording
go test -race -run 'TestServer_RecordStart|TestServer_RecordStop' ./ipc/

# Run CLI record command tests
go test -race -run 'TestRecordCommand' ./cmd/rnix/

# Run all story 14-1 tests across packages
go test -race -run 'RecordEvent|RecordMetadata|Recorder|RecordManager|RecordStart|RecordStop|RecordCommand' ./debug/ ./ipc/ ./cmd/rnix/

# Run benchmark
go test -bench BenchmarkRecorderWriteEvent -benchtime 5s ./debug/

# Run with verbose output for debugging
go test -race -v -run 'TestNewRecorder_CreatesDirectoryAndFile' ./debug/
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 27 tests written and failing (compilation errors due to missing types/methods)
- Tests organized across 6 files matching existing patterns
- Implementation checklist created

**Verification:**

- All tests fail due to missing implementation (undefined symbols: RecordEvent, RecordMetadata, Recorder, NewRecorder, RecordManager, NewRecordManager, handleRecordCommand)
- Failure messages are clear: "undefined: debug.RecordEvent" etc.
- Tests follow existing project patterns (Go standard testing, table-driven where appropriate, t.TempDir for filesystem)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with data model tests** (14.1-MODEL-001 to 004) -- 定义 RecordEvent/RecordMetadata 结构体
2. **Then recorder tests** (14.1-REC-001 to 007) -- 实现 Recorder 文件写入
3. **Then record manager tests** (14.1-MGR-001 to 009) -- 实现 RecordManager 多录制管理
4. **Then IPC server tests** (14.1-IPC-001 to 004) -- 实现 record_start/record_stop 路由
5. **Then CLI tests** (14.1-CLI-001 to 004) -- 实现 record 子命令
6. **Finally** 实现 kernel 录制钩子 (emitEvent/reasonStep/finishProcess/reapProcess)

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 27 tests pass with `go test -race`
2. Run benchmark verify WriteEvent < 100us
3. Review code for consistency with existing debug package patterns (strace.go, event.go)
4. Ensure no duplication between RecordManager and existing strace infrastructure

---

## Test Summary

| Category | Test Count | Priority Breakdown |
|---|---|---|
| Data Model Unit Tests | 4 | 4 P0 |
| Recorder Unit Tests | 7 | 6 P0, 1 P1 |
| RecordManager Unit Tests | 9 | 9 P0 |
| IPC Server Tests | 4 | 2 P0, 2 P1 |
| CLI Tests | 4 | 3 P0, 1 P1 |
| Benchmark Tests | 1 | 1 P0 |
| **Total** | **29** | **25 P0, 4 P1** |

---

## Next Steps

1. **Share this checklist** with the dev workflow
2. **Run failing tests** to confirm RED phase: `go test -race ./debug/ ./ipc/ ./cmd/rnix/`
3. **Begin implementation** using implementation checklist as guide
4. **Work one test group at a time** (data model -> recorder -> manager -> IPC -> CLI)
5. **When all tests pass**, refactor code for quality
6. **Run benchmark** verify NFR32 performance requirement

---

**Generated by BMad TEA Agent** - 2026-03-08
