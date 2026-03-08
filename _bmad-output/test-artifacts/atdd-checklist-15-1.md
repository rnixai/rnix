---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/15-1-trace-id-generation-and-span-recording.md'
  - '_bmad/tea/config.yaml'
  - 'debug/record.go'
  - 'debug/record_manager.go'
  - 'debug/recorder.go'
  - 'debug/event.go'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/ipc.go'
  - 'kernel/reap.go'
  - 'compose/engine.go'
  - 'compose/types.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'cmd/rnix/compose.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 15, Story 1: Trace ID 生成与 Span 记录

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
- Story 15-1 approved with 3 clear acceptance criteria (AC #1-3)
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages
- Development environment available

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/15-1-trace-id-generation-and-span-recording.md`
- **Acceptance Criteria:** 3 ACs covering Compose TraceID generation, IPC TraceID propagation, Span recording (syscall count + tokens)
- **Affected Components:** `debug/` (new trace.go), `kernel/` (process.go, kernel.go, ipc.go, reap.go), `compose/` (engine.go, types.go), `ipc/` (protocol.go, server.go), `cmd/rnix/` (compose.go), `internal/types/` (types.go)
- **Dependencies:** Builds on Epic 14 recording infrastructure, Epic 7 Compose system, Epic 6 IPC system

### Framework & Existing Patterns
- Existing test patterns in `debug/*_test.go` (record, replay, snapshot_diff, fork tests)
- Existing Compose tests in `compose/engine_test.go` (mock KernelSpawner)
- Existing IPC tests in `ipc/server_test.go` (setupTestServer, sendRequest)
- Existing kernel tests in `kernel/*_test.go` (Spawn, emitEvent)
- Test pattern: Go table-driven tests, `t.TempDir()` for filesystem, `t.Helper()`, `-race` detector
- Concurrency tests: `sync.WaitGroup` + goroutines (reference: `internal/xsync/syncmap_test.go`)

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (debug/trace, kernel hooks, IPC protocol, Compose engine)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | Compose 编排自动生成 TraceID，所有 Spawn 进程继承 TraceID 并生成独立 SpanID | Unit (debug/trace) + Integration (compose/engine) + IPC (server) | P0 |
| AC#2 | IPC Send/Recv 自动携带 TraceID，接收方 Span parent 指向发送方 | Unit (kernel/ipc) + Integration (kernel) | P0 |
| AC#3 | Span 记录起止时间、syscall 序列和 token 消耗，传播延迟 < 10ms (NFR33) | Unit (debug/trace) + Benchmark (kernel/ipc) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests | ~25 | TraceID/SpanID 生成、Span 结构体操作、SpanRecorder CRUD、SpanWriter/Reader |
| Integration Tests | ~12 | Compose→Spawn TraceID 传播、IPC TraceID 传播、emitEvent→SpanRecorder 集成 |
| Benchmark Tests | ~3 | IPC Send/Recv 延迟影响、SpanRecorder.RecordSyscall 开销、Span 持久化开销 |
| **Total** | **~40** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — debug/trace_test.go

**File:** `debug/trace_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestGenerateTraceID_Format` | #1 | P0 | TraceID 为 32 字符 hex 字符串 |
| 2 | `TestGenerateTraceID_Unique` | #1 | P0 | 两次生成结果不同 |
| 3 | `TestGenerateSpanID_Format` | #1 | P0 | SpanID 为 32 字符 hex 字符串 |
| 4 | `TestGenerateSpanID_Unique` | #1 | P0 | 两次生成结果不同 |
| 5 | `TestSpanRecorder_StartSpan` | #3 | P0 | StartSpan 创建 Span，记录 StartTime、TraceID、SpanID、ParentSpanID |
| 6 | `TestSpanRecorder_RecordSyscall` | #3 | P0 | RecordSyscall 递增 SyscallCount |
| 7 | `TestSpanRecorder_RecordTokens` | #3 | P0 | RecordTokens 累加 TokensUsed |
| 8 | `TestSpanRecorder_EndSpan` | #3 | P0 | EndSpan 设置 EndTime、Duration、Status |
| 9 | `TestSpanRecorder_EndSpan_Error` | #3 | P0 | EndSpan 以 ERROR 状态结束时正确记录 |
| 10 | `TestSpanRecorder_EndSpan_Timeout` | #3 | P0 | EndSpan 以 TIMEOUT 状态结束时正确记录 |
| 11 | `TestSpanRecorder_GetSpan` | #3 | P0 | GetSpan 返回当前 Span 副本 |
| 12 | `TestSpanRecorder_GetSpan_NotFound` | #3 | P1 | GetSpan 对不存在的 PID 返回 nil |
| 13 | `TestSpanRecorder_GetTraceSpans` | #1 | P0 | 返回同一 Trace 下所有 Span |
| 14 | `TestSpanRecorder_GetTraceSpans_Empty` | #1 | P1 | 不存在的 TraceID 返回空切片 |
| 15 | `TestSpanRecorder_ConcurrentAccess` | #3 | P0 | 多 goroutine 并发 Start/Record/End，-race 通过 |
| 16 | `TestSpanWriter_WriteSpan` | #3 | P0 | Span 写入 JSONL 文件，格式正确 |
| 17 | `TestSpanWriter_AppendMultiple` | #3 | P0 | 多个 Span 追加写入同一文件 |
| 18 | `TestSpanReader_ReadSpans` | #3 | P0 | 从 JSONL 文件读取并还原 Span |
| 19 | `TestSpanReader_ReadSpans_Empty` | #3 | P1 | 空文件返回空切片 |
| 20 | `TestSpanStatus_Values` | #3 | P1 | SpanStatusOK/ERROR/TIMEOUT 值正确 |

### Unit Tests — kernel/ipc_test.go (新增)

**File:** `kernel/ipc_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 21 | `TestSend_TraceIDPropagation` | #2 | P0 | Send 将发送者 TraceID/SpanID 写入 Message |
| 22 | `TestSend_NoTraceID` | #2 | P0 | 无 TraceID 时 Message.TraceID 为空（向后兼容） |
| 23 | `TestRecv_InheritTraceID` | #2 | P0 | Recv 方无 TraceID 时从消息继承 |
| 24 | `TestRecv_KeepExistingTraceID` | #2 | P0 | Recv 方已有 TraceID 时保持不变 |
| 25 | `TestRecv_CreateNewSpan` | #2 | P0 | Recv 继承 TraceID 时创建新 Span，parent 指向发送方 SpanID |

### Integration Tests — kernel/kernel_test.go (新增)

**File:** `kernel/kernel_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 26 | `TestSpawn_WithTraceID` | #1 | P0 | Spawn 传入 TraceID → Process 继承 TraceID 并生成 SpanID |
| 27 | `TestSpawn_WithoutTraceID` | #1 | P0 | Spawn 不传 TraceID → Process 无 TraceID（向后兼容） |
| 28 | `TestSpawn_WithParentSpanID` | #1 | P0 | Spawn 传入 ParentSpanID → Process.ParentSpanID 正确设置 |
| 29 | `TestSpawn_SpanRecorderStarted` | #3 | P0 | Spawn 有 TraceID 时调用 SpanRecorder.StartSpan |
| 30 | `TestEmitEvent_SpanSyscallCount` | #3 | P0 | emitEvent 递增 Span 的 SyscallCount |
| 31 | `TestEmitEvent_NoTraceID_NoSpanRecord` | #3 | P0 | 无 TraceID 时 emitEvent 不调用 SpanRecorder |
| 32 | `TestReasonStep_SpanTokenRecord` | #3 | P0 | reasonStep 中 LLM 响应后 SpanRecorder.RecordTokens |
| 33 | `TestReapProcess_EndSpan_OK` | #3 | P0 | 正常退出时 EndSpan 状态为 OK |
| 34 | `TestReapProcess_EndSpan_Error` | #3 | P0 | 错误退出时 EndSpan 状态为 ERROR |
| 35 | `TestReapProcess_EndSpan_Timeout` | #3 | P0 | 超时退出时 EndSpan 状态为 TIMEOUT |

### Integration Tests — compose/engine_test.go (新增)

**File:** `compose/engine_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 36 | `TestExecute_GeneratesTraceID` | #1 | P0 | Execute 自动生成 TraceID |
| 37 | `TestExecute_AllNodesShareTraceID` | #1 | P0 | 所有 Spawned 进程共享同一 TraceID |
| 38 | `TestExecute_ParentChildSpanRelation` | #1 | P0 | 依赖节点的 ParentSpanID 指向上游节点的 SpanID |

### Integration Tests — ipc/server_test.go (新增)

**File:** `ipc/server_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 39 | `TestHandleSpawn_TraceIDPassthrough` | #1 | P0 | SpawnRequest.TraceID 传递到 kernel.SpawnOpts |

### Benchmark Tests

**File:** `kernel/ipc_bench_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 40 | `BenchmarkSendRecv_WithTraceID` | #3 | P0 | 带 TraceID 的 Send/Recv 延迟增加 < 10ms (NFR33) |
| 41 | `BenchmarkSendRecv_WithoutTraceID` | #3 | P0 | 不带 TraceID 的 Send/Recv 基准延迟 |
| 42 | `BenchmarkSpanRecorder_RecordSyscall` | #3 | P1 | RecordSyscall 单次调用开销 |

---

## Fixtures & Helpers

### Span Test Helpers

**位置:** `debug/trace_test.go` 内部

- `makeTestSpan(traceID, spanID, parentSpanID)` — 创建测试用 Span
- `assertSpanEquals(t, expected, actual)` — 比较两个 Span 的关键字段

### Compose Test Helpers

**位置:** `compose/engine_test.go` 内部

- 复用现有 `mockKernelSpawner` — 扩展记录 TraceID/ParentSpanID
- `assertAllSameTraceID(t, spawns)` — 验证所有 spawn 使用同一 TraceID

### IPC Test Helpers

**位置:** `kernel/ipc_test.go` 内部

- 复用现有 `setupTestKernel` helper
- `spawnWithTrace(k, intent, traceID, parentSpanID)` — 便捷的带 trace 的 Spawn

---

## Mock Requirements

### Mock KernelSpawner (Compose Tests)

复用 `compose/engine_test.go` 中现有的 `mockKernelSpawner`，扩展以记录接收到的 TraceID 和 ParentSpanID。

### Mock SpanRecorder (Kernel Tests)

对于需要隔离测试 Spawn/emitEvent/reapProcess 的场景，SpanRecorder 接口化或直接使用真实 SpanRecorder（内存操作，无需 mock）。

---

## Implementation Checklist

### Phase 1: 数据类型定义 (Tests 1-4, 20)

- [ ] 在 `internal/types/types.go` 新增 TraceID、SpanID 类型
- [ ] 在 `debug/trace.go` 实现 GenerateTraceID、GenerateSpanID
- [ ] 在 `debug/trace.go` 定义 Span、SpanEvent、SpanStatus 类型
- [ ] Run: `go test -race ./debug/ -run TestGenerate`
- [ ] ✅ Tests 1-4, 20 pass

### Phase 2: SpanRecorder (Tests 5-15)

- [ ] 实现 SpanRecorder.StartSpan
- [ ] 实现 SpanRecorder.RecordSyscall
- [ ] 实现 SpanRecorder.RecordTokens
- [ ] 实现 SpanRecorder.EndSpan
- [ ] 实现 SpanRecorder.GetSpan / GetTraceSpans
- [ ] Run: `go test -race ./debug/ -run TestSpanRecorder`
- [ ] ✅ Tests 5-15 pass

### Phase 3: Span 持久化 (Tests 16-19)

- [ ] 实现 SpanWriter（JSONL 追加写入）
- [ ] 实现 SpanReader（JSONL 读取还原）
- [ ] Run: `go test -race ./debug/ -run TestSpan(Writer|Reader)`
- [ ] ✅ Tests 16-19 pass

### Phase 4: Kernel 集成 (Tests 26-35)

- [ ] Process 新增 TraceID/SpanID/ParentSpanID 字段
- [ ] SpawnOpts 新增 TraceID/ParentSpanID 字段
- [ ] KernelImpl 新增 spanRecorder 字段
- [ ] 修改 Spawn 传播 TraceID 并启动 Span
- [ ] 修改 emitEvent 递增 SyscallCount
- [ ] 修改 reasonStep 记录 TokensUsed
- [ ] 修改 reapProcess 结束 Span
- [ ] Run: `go test -race ./kernel/ -run TestSpawn_With`
- [ ] Run: `go test -race ./kernel/ -run TestEmitEvent_Span`
- [ ] Run: `go test -race ./kernel/ -run TestReapProcess_EndSpan`
- [ ] ✅ Tests 26-35 pass

### Phase 5: IPC TraceID 传播 (Tests 21-25, 39)

- [ ] Message 新增 TraceID/SpanID 字段
- [ ] 修改 Send 写入 TraceID/SpanID
- [ ] 修改 Recv 继承 TraceID（条件：接收方无 TraceID）
- [ ] SpawnRequest 新增 TraceID/ParentSpanID
- [ ] handleSpawn 传递 TraceID 到 SpawnOpts
- [ ] SyscallEventWire 新增 TraceID/SpanID
- [ ] Run: `go test -race ./kernel/ -run TestSend_Trace`
- [ ] Run: `go test -race ./kernel/ -run TestRecv_`
- [ ] Run: `go test -race ./ipc/ -run TestHandleSpawn_TraceID`
- [ ] ✅ Tests 21-25, 39 pass

### Phase 6: Compose 集成 (Tests 36-38)

- [ ] ComposeSpawnOpts 新增 TraceID/ParentSpanID
- [ ] Engine.Execute 生成 TraceID
- [ ] executeNode 传递 TraceID/ParentSpanID
- [ ] ipcKernelSpawner 传递 TraceID 到 SpawnRequest
- [ ] Run: `go test -race ./compose/ -run TestExecute_`
- [ ] ✅ Tests 36-38 pass

### Phase 7: 性能验证 (Tests 40-42)

- [ ] 编写 Benchmark 测试
- [ ] Run: `go test -bench BenchmarkSendRecv -benchtime 5s ./kernel/`
- [ ] 验证 NFR33: 延迟增加 < 10ms
- [ ] ✅ Tests 40-42 pass

---

## Running Tests

```bash
# Run all tests for story 15-1 (affected packages)
go test -race -v ./debug/ ./kernel/ ./compose/ ./ipc/ ./cmd/rnix/

# Run only trace-related tests
go test -race -v ./debug/ -run TestGenerate
go test -race -v ./debug/ -run TestSpanRecorder
go test -race -v ./debug/ -run TestSpan

# Run IPC trace propagation tests
go test -race -v ./kernel/ -run "TestSend_Trace|TestRecv_"

# Run Compose trace integration tests
go test -race -v ./compose/ -run TestExecute_

# Run benchmark tests (NFR33 verification)
go test -bench BenchmarkSendRecv -benchtime 5s -benchmem ./kernel/

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 42 tests designed and specified
- ✅ Test strategy mapped to acceptance criteria
- ✅ Implementation checklist created with phased approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (debug.GenerateTraceID, debug.SpanRecorder, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (data types) → Tests 1-4 pass
2. Implement Phase 2 (SpanRecorder) → Tests 5-15 pass
3. Implement Phase 3 (persistence) → Tests 16-19 pass
4. Implement Phase 4 (kernel) → Tests 26-35 pass
5. Implement Phase 5 (IPC) → Tests 21-25, 39 pass
6. Implement Phase 6 (Compose) → Tests 36-38 pass
7. Implement Phase 7 (benchmark) → Tests 40-42 pass
8. Run full suite: `go test -race ./...` → All 19+ packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, test framework configured)
- [x] Test strategy maps to all 3 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] Concurrency safety tests included (SpanRecorder, IPC)
- [x] Performance benchmark for NFR33 included
- [x] Implementation checklist covers all 8 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- 本 story 是 Epic 15 的第一层，后续 story (15-2 trace view, 15-3 trace blame) 依赖本 story 的 SpanReader 和 GetTraceSpans
- SpanRecorder 使用 mutex 保护，需要通过 `-race` 检测
- Benchmark 测试的 NFR33 阈值 (10ms) 只需验证"增加的延迟"，不是绝对延迟
- Compose 测试复用现有 mockKernelSpawner，需要扩展以捕获 TraceID

---

**Generated by BMad TEA Agent** - 2026-03-08
