---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-checklist'
lastStep: 'step-05-checklist'
lastSaved: '2026-02-28'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/6-5-three-level-concurrency-model.md'
  - 'kernel/signal_test.go'
  - 'kernel/ipc_test.go'
  - 'kernel/procgroup_test.go'
  - 'kernel/kernel_test.go'
  - 'internal/types/types.go'
  - 'kernel/process.go'
  - 'kernel/reap.go'
  - 'kernel/kernel.go'
  - 'kernel/errors.go'
---

# ATDD Checklist - Epic 6, Story 6.5: 三级并发模型 (Three-Level Concurrency Model)

**Date:** 2026-02-28
**Author:** Decker
**Primary Test Level:** Unit / Integration

---

## Story Summary

三级并发模型为 Crux 操作系统提供进程、线程、协程三级并发原语，使平台构建者可以为不同粒度的任务选择最合适的并发模型。

**As a** 平台构建者
**I want** 系统提供进程、线程、协程三级并发原语
**So that** 我可以为不同粒度的任务选择最合适的并发模型

---

## Acceptance Criteria

1. **AC #1 — 进程级（Process-level）**: 创建进程级智能体（`Spawn`），拥有独立上下文和独立 LLM 会话，完全隔离，通过 IPC 通信
2. **AC #2 — 线程级（Thread-level）**: 创建线程级执行单元，共享父进程的上下文空间，拥有独立执行流（goroutine），通过共享上下文交换数据
3. **AC #3 — 协程级（Coroutine-level）**: 创建协程级执行单元，轻量协作调度，yield 语义，适用于上下文内的子任务分解
4. **AC #4 — 并发性能（NFR24）**: >= 10 个并发智能体（进程级）同时运行，进程表操作延迟不超过单进程场景的 2 倍

---

## 技术栈检测

- **detected_stack**: `backend`（Go 项目，`go.mod` 存在）
- **test_framework**: Go 标准 `testing` 包 + `-race` 检测
- **test_dir**: `kernel/` 包内测试（`*_test.go`）
- **generation_mode**: AI Generation（后端项目，无浏览器录制需求）

---

## 测试策略

### 测试级别选择

| AC | 测试级别 | 理由 |
|----|---------|------|
| AC #1 | Unit | 进程级并发已由 Spawn 实现（Story 1.2），此 Story 确认其定位 |
| AC #2 | Unit | SpawnThread/JoinThread 纯内核逻辑，无外部依赖 |
| AC #3 | Unit | SpawnCoroutine/Yield/ResumeCoroutine 基于 channel，纯内核逻辑 |
| AC #4 | Integration/Benchmark | 并发性能需要多进程+线程协同测试 |

### 优先级

| 优先级 | 测试 | AC |
|--------|------|-----|
| P0 | SpawnThread/JoinThread 基本流程 | AC #2 |
| P0 | SpawnCoroutine/Yield/ResumeCoroutine 基本流程 | AC #3 |
| P0 | 错误处理（父进程不存在/非 Running） | AC #2, #3 |
| P1 | 线程上下文共享（parent ctx 级联取消） | AC #2 |
| P1 | 多次 yield/resume 循环 | AC #3 |
| P1 | reapProcess 清理线程和协程 | AC #2, #3 |
| P1 | SyscallEvent 发射验证 | AC #2, #3 |
| P2 | 并发安全（-race 检测） | AC #2, #3 |
| P2 | 10 进程并发性能（NFR24） | AC #4 |

---

## Failing Tests Created (RED Phase)

### Unit Tests — Thread (14 tests)

**File:** `kernel/thread_test.go` (275 lines)

- **Test:** `TestSpawnThread_Basic`
  - **Status:** RED — `k.SpawnThread` undefined (type `*KernelImpl` has no field or method `SpawnThread`)
  - **Verifies:** AC #2 — 基本线程创建，TID 分配，线程注册到父进程

- **Test:** `TestSpawnThread_ParentNotFound`
  - **Status:** RED — `k.SpawnThread` undefined
  - **Verifies:** AC #2 — 父进程不存在返回 `ErrNotFound`

- **Test:** `TestSpawnThread_ParentNotRunning`
  - **Status:** RED — `k.SpawnThread` undefined
  - **Verifies:** AC #2 — 父进程非 Running 状态返回 `ErrInvalid`

- **Test:** `TestJoinThread_Basic`
  - **Status:** RED — `k.SpawnThread`/`k.JoinThread` undefined
  - **Verifies:** AC #2 — 等待线程完成并返回

- **Test:** `TestJoinThread_ThreadNotFound`
  - **Status:** RED — `k.JoinThread` undefined
  - **Verifies:** AC #2 — 线程不存在返回 `ErrNotFound`

- **Test:** `TestJoinThread_ParentNotFound`
  - **Status:** RED — `k.JoinThread` undefined
  - **Verifies:** AC #2 — 父进程不存在返回 `ErrNotFound`

- **Test:** `TestThread_SharesContext`
  - **Status:** RED — `k.SpawnThread`/`proc.GetThread` undefined
  - **Verifies:** AC #2 — 线程共享父进程上下文（ctx 级联取消）

- **Test:** `TestThread_ParentKill`
  - **Status:** RED — `k.SpawnThread`/`proc.GetThread` undefined
  - **Verifies:** AC #2 — Kill 父进程时子线程自动取消

- **Test:** `TestThread_IndependentExecution`
  - **Status:** RED — `k.SpawnThread`/`proc.GetThread` undefined
  - **Verifies:** AC #2 — 多线程并行注册和独立执行

- **Test:** `TestThread_MultipleTIDsAreProcessLocal`
  - **Status:** RED — `k.SpawnThread` undefined
  - **Verifies:** AC #2 — TID 是进程内局部的

- **Test:** `TestSpawnThread_SyscallEvent`
  - **Status:** RED — `k.SpawnThread` undefined
  - **Verifies:** AC #2 — SpawnThread 发射 SyscallEvent

- **Test:** `TestThread_Concurrent`
  - **Status:** RED — `k.SpawnThread`/`k.JoinThread` undefined
  - **Verifies:** AC #2 — 50 goroutine 并发 SpawnThread/JoinThread 无 race

### Unit Tests — Coroutine (14 tests)

**File:** `kernel/coroutine_test.go` (372 lines)

- **Test:** `TestSpawnCoroutine_Basic`
  - **Status:** RED — `k.SpawnCoroutine` undefined
  - **Verifies:** AC #3 — 基本协程创建和 yield

- **Test:** `TestSpawnCoroutine_ParentNotFound`
  - **Status:** RED — `k.SpawnCoroutine` undefined
  - **Verifies:** AC #3 — 父进程不存在返回 `ErrNotFound`

- **Test:** `TestSpawnCoroutine_ParentNotRunning`
  - **Status:** RED — `k.SpawnCoroutine` undefined
  - **Verifies:** AC #3 — 父进程非 Running 返回 `ErrInvalid`

- **Test:** `TestYield_Basic`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 协程 yield 值传递到 caller

- **Test:** `TestResumeCoroutine_Basic`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 多次 yield/resume 返回正确值

- **Test:** `TestResumeCoroutine_NotSuspended`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 恢复非挂起状态返回 `ErrInvalid` 或 `ErrNotFound`

- **Test:** `TestResumeCoroutine_NotFound`
  - **Status:** RED — `k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 恢复不存在的协程返回 `ErrNotFound`

- **Test:** `TestResumeCoroutine_ParentNotFound`
  - **Status:** RED — `k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 父进程不存在返回 `ErrNotFound`

- **Test:** `TestCoroutine_MultipleYields`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 多次 yield/resume 循环（3 次 yield + 最终返回）

- **Test:** `TestCoroutine_Completion`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 协程正常完成后自动清理

- **Test:** `TestSpawnCoroutine_SyscallEvent`
  - **Status:** RED — `k.SpawnCoroutine` undefined
  - **Verifies:** AC #3 — SpawnCoroutine 发射 SyscallEvent

- **Test:** `TestResumeCoroutine_SyscallEvent`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — ResumeCoroutine 发射 yielded/completed 事件

- **Test:** `TestCoroutine_Concurrent`
  - **Status:** RED — `k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #3 — 20 goroutine 并发 SpawnCoroutine/Yield/Resume 无 race

### Integration Tests — Reap & Performance (3 tests)

**File:** `kernel/coroutine_test.go` (与协程测试同文件)

- **Test:** `TestReapProcess_CleansThreadsAndCoroutines`
  - **Status:** RED — `k.SpawnThread`/`k.SpawnCoroutine`/`proc.threads`/`proc.coroutines` undefined
  - **Verifies:** AC #2, #3 — reapProcess 正确清理线程和协程

- **Test:** `TestConcurrent_10Processes`
  - **Status:** RED — `k.SpawnThread` undefined
  - **Verifies:** AC #4/NFR24 — 10 进程并发操作延迟合规

- **Test:** `TestConcurrency_SyscallEvents`
  - **Status:** RED — `k.SpawnThread`/`k.SpawnCoroutine`/`k.ResumeCoroutine` undefined
  - **Verifies:** AC #2, #3 — 所有并发 syscall 正确发射事件

---

## 辅助函数

### `newConcurrencyTestProcess` (kernel/thread_test.go)

创建一个处于 Running 状态的测试进程，用于并发测试。模式与现有的 `newIPCTestProcess`、`newSignalTestProcess` 一致。

---

## 类型依赖（需新增）

### `types.TID` (internal/types/types.go)

```go
type TID uint64
```

### `types.CoID` (internal/types/types.go)

```go
type CoID uint64
```

---

## 接口依赖（需新增）

### `ConcurrencyManager` (kernel/concurrency.go)

```go
type ConcurrencyManager interface {
    SpawnThread(parentPID types.PID, intent string) (types.TID, error)
    JoinThread(parentPID types.PID, tid types.TID) error
    SpawnCoroutine(parentPID types.PID, fn CoroutineFunc) (types.CoID, error)
    Yield(parentPID types.PID, coID types.CoID, value any) error
    ResumeCoroutine(parentPID types.PID, coID types.CoID) (any, error)
}
```

### `Process` 新增方法 (kernel/process.go)

```go
func (p *Process) GetThread(tid types.TID) (*Thread, bool)
func (p *Process) AddThread(t *Thread)
func (p *Process) RemoveThread(tid types.TID)
func (p *Process) ClearThreads()
func (p *Process) GetCoroutine(coID types.CoID) (*Coroutine, bool)
func (p *Process) AddCoroutine(c *Coroutine)
func (p *Process) RemoveCoroutine(coID types.CoID)
func (p *Process) ClearCoroutines()
```

---

## Implementation Checklist

### Test: TestSpawnThread_Basic

**File:** `kernel/thread_test.go`

**Tasks to make this test pass:**

- [ ] 在 `internal/types/types.go` 新增 `TID` 类型
- [ ] 创建 `kernel/concurrency.go` 定义 `ConcurrencyManager` 接口和 `CoroutineFunc` 类型
- [ ] 创建 `kernel/thread.go` 定义 `Thread` 结构体
- [ ] 在 `kernel/process.go` 新增 `threads`/`tidCounter` 字段和 `AddThread`/`GetThread` 方法
- [ ] 实现 `KernelImpl.SpawnThread` 方法
- [ ] Run test: `go test ./kernel/ -run TestSpawnThread_Basic -race`
- [ ] Test passes (green phase)

---

### Test: TestSpawnThread_ParentNotFound / TestSpawnThread_ParentNotRunning

**File:** `kernel/thread_test.go`

**Tasks to make these tests pass:**

- [ ] SpawnThread 中验证父进程存在（ErrNotFound）
- [ ] SpawnThread 中验证父进程为 Running 状态（ErrInvalid）
- [ ] 错误包装为 `*SyscallError`
- [ ] Run test: `go test ./kernel/ -run 'TestSpawnThread_Parent' -race`
- [ ] Tests pass (green phase)

---

### Test: TestJoinThread_Basic / TestJoinThread_ThreadNotFound / TestJoinThread_ParentNotFound

**File:** `kernel/thread_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `KernelImpl.JoinThread` 方法
- [ ] JoinThread 等待 `thread.Done` channel
- [ ] 验证父进程和线程存在
- [ ] Run test: `go test ./kernel/ -run TestJoinThread -race`
- [ ] Tests pass (green phase)

---

### Test: TestThread_SharesContext / TestThread_ParentKill

**File:** `kernel/thread_test.go`

**Tasks to make these tests pass:**

- [ ] Thread.ctx 继承自 `context.WithCancel(proc.ctx)`
- [ ] 验证 cancel 级联传播
- [ ] Run test: `go test ./kernel/ -run 'TestThread_Shares|TestThread_Parent' -race`
- [ ] Tests pass (green phase)

---

### Test: TestSpawnCoroutine_Basic / TestSpawnCoroutine_ParentNotFound / TestSpawnCoroutine_ParentNotRunning

**File:** `kernel/coroutine_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `internal/types/types.go` 新增 `CoID` 类型
- [ ] 创建 `kernel/coroutine.go` 定义 `Coroutine` 结构体和 `coroutineState` 枚举
- [ ] 在 `kernel/process.go` 新增 `coroutines`/`coIDCounter` 字段和管理方法
- [ ] 实现 `KernelImpl.SpawnCoroutine` 方法
- [ ] Run test: `go test ./kernel/ -run TestSpawnCoroutine -race`
- [ ] Tests pass (green phase)

---

### Test: TestYield_Basic / TestResumeCoroutine_Basic / TestCoroutine_MultipleYields

**File:** `kernel/coroutine_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `KernelImpl.ResumeCoroutine` 方法
- [ ] yieldCh/resumeCh 无缓冲 channel 同步语义
- [ ] 正确处理 yield/resume 循环
- [ ] Run test: `go test ./kernel/ -run 'TestYield|TestResumeCoroutine_Basic|TestCoroutine_Multiple' -race`
- [ ] Tests pass (green phase)

---

### Test: TestResumeCoroutine_NotSuspended / TestResumeCoroutine_NotFound / TestResumeCoroutine_ParentNotFound

**File:** `kernel/coroutine_test.go`

**Tasks to make these tests pass:**

- [ ] ResumeCoroutine 验证协程状态为 Suspended
- [ ] 协程不存在返回 ErrNotFound
- [ ] 父进程不存在返回 ErrNotFound
- [ ] Run test: `go test ./kernel/ -run 'TestResumeCoroutine_Not' -race`
- [ ] Tests pass (green phase)

---

### Test: TestCoroutine_Completion

**File:** `kernel/coroutine_test.go`

**Tasks to make this test pass:**

- [ ] 协程完成时 close(yieldCh) 信号完成
- [ ] 自动清理 coroutine 从 process.coroutines
- [ ] Run test: `go test ./kernel/ -run TestCoroutine_Completion -race`
- [ ] Test passes (green phase)

---

### Test: TestReapProcess_CleansThreadsAndCoroutines

**File:** `kernel/coroutine_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/process.go` 实现 `ClearThreads()` 和 `ClearCoroutines()` 方法
- [ ] 在 `kernel/reap.go` 的 `reapProcess` 中 ClearSignalState 后添加 ClearThreads/ClearCoroutines
- [ ] Run test: `go test ./kernel/ -run TestReapProcess_CleansThreadsAndCoroutines -race`
- [ ] Test passes (green phase)

---

### Test: TestConcurrent_10Processes

**File:** `kernel/coroutine_test.go`

**Tasks to make this test pass:**

- [ ] 所有并发方法正确实现
- [ ] SyncMap 保证并发安全
- [ ] Run test: `go test ./kernel/ -run TestConcurrent_10Processes -race`
- [ ] Test passes (green phase)

---

### Test: TestConcurrency_SyscallEvents

**File:** `kernel/coroutine_test.go`

**Tasks to make this test pass:**

- [ ] SpawnThread/SpawnCoroutine/ResumeCoroutine 调用 `emitEvent`
- [ ] 事件 Args 包含正确字段（tid/co_id/action）
- [ ] Run test: `go test ./kernel/ -run TestConcurrency_SyscallEvents -race`
- [ ] Test passes (green phase)

---

### Test: TestThread_Concurrent / TestCoroutine_Concurrent

**Files:** `kernel/thread_test.go` / `kernel/coroutine_test.go`

**Tasks to make these tests pass:**

- [ ] Thread.mu 保护 State/Result/Err
- [ ] Process.mu 保护 threads/coroutines 表
- [ ] yieldCh/resumeCh 为无缓冲 channel
- [ ] Run test: `go test ./kernel/ -run 'TestThread_Concurrent|TestCoroutine_Concurrent' -race`
- [ ] Tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation exists)
go test ./kernel/ -run 'TestSpawnThread|TestJoinThread|TestThread_|TestSpawnCoroutine|TestYield|TestResumeCoroutine|TestCoroutine_|TestReapProcess_CleansThreads|TestConcurrent_10|TestConcurrency_Syscall' -race -v

# Run specific test file
go test ./kernel/ -run TestSpawnThread_Basic -race -v

# Run all kernel tests (including regression)
go test ./kernel/ -race -v

# Run with coverage
go test ./kernel/ -race -coverprofile=coverage.out && go tool cover -html=coverage.out

# Run with verbose timing
go test ./kernel/ -run 'TestConcurrent_10Processes' -race -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 31 tests written and failing (compilation errors due to missing implementation)
- Test helper `newConcurrencyTestProcess` created matching existing patterns
- Tests follow existing project conventions (standard `testing` package, no testify)
- Tests cover all 4 acceptance criteria
- Error code assertions match existing patterns (`*SyscallError` + `ErrCode`)

**Verification:**

- All tests fail to compile (`undefined` errors for unimplemented methods)
- Failure is due to missing implementation, not test bugs
- Existing tests are unaffected (same patterns, independent files once implementation exists)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with types**: Add `TID`, `CoID` to `internal/types/types.go`
2. **Define interface**: Create `kernel/concurrency.go` with `ConcurrencyManager`
3. **Implement Thread**: Create `kernel/thread.go`, add Process fields, implement SpawnThread/JoinThread
4. **Implement Coroutine**: Create `kernel/coroutine.go`, implement SpawnCoroutine/Yield/ResumeCoroutine
5. **Integrate reap**: Update `kernel/reap.go` with ClearThreads/ClearCoroutines
6. **Run tests incrementally**: `go test ./kernel/ -run TestSpawnThread_Basic -race` first

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback with `-race`)

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass: `go test ./kernel/ -race`
2. Run lint: `make lint`
3. Verify full suite: `make test`
4. Build: `make build`
5. Check no regression on Stories 6.1-6.4

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./kernel/ -run 'TestSpawnThread|TestJoinThread|TestThread_|TestSpawnCoroutine|TestYield|TestResumeCoroutine|TestCoroutine_|TestReapProcess_CleansThreads|TestConcurrent_10|TestConcurrency_Syscall' -race`

**Results:**

```
# github.com/gonewx/crux/kernel [github.com/gonewx/crux/kernel.test]
kernel/coroutine_test.go:18:17: k.SpawnCoroutine undefined (type *KernelImpl has no field or method SpawnCoroutine)
kernel/coroutine_test.go:33:16: k.ResumeCoroutine undefined (type *KernelImpl has no field or method ResumeCoroutine)
kernel/coroutine_test.go:47:14: k.SpawnCoroutine undefined (type *KernelImpl has no field or method SpawnCoroutine)
...
kernel/thread_test.go:36:15: k.SpawnThread undefined (type *KernelImpl has no field or method SpawnThread)
kernel/thread_test.go:46:22: proc.GetThread undefined (type *Process has no field or method GetThread)
...
FAIL    github.com/gonewx/crux/kernel [build failed]
```

**Summary:**

- Total tests: 31
- Passing: 0 (expected — compilation fails)
- Failing: 31 (expected — implementation not yet written)
- Status: RED phase verified

**Expected Failure Messages:**
- `k.SpawnThread undefined (type *KernelImpl has no field or method SpawnThread)`
- `k.JoinThread undefined (type *KernelImpl has no field or method JoinThread)`
- `k.SpawnCoroutine undefined (type *KernelImpl has no field or method SpawnCoroutine)`
- `k.ResumeCoroutine undefined (type *KernelImpl has no field or method ResumeCoroutine)`
- `proc.GetThread undefined (type *Process has no field or method GetThread)`
- `proc.threads undefined (type *Process has no field or method threads)`
- `proc.coroutines undefined (type *Process has no field or method coroutines)`

---

## Notes

- Go ATDD 的 RED 阶段表现为编译失败（`undefined` 错误），而非运行时失败。这是因为 Go 编译整个包，新测试文件引用的未实现方法会导致编译错误。
- 测试文件使用与现有 `signal_test.go`、`ipc_test.go`、`procgroup_test.go` 完全一致的模式：`newSimpleKernel`、`newXxxTestProcess` helper、`*SyscallError` 断言、`DebugChan` 事件验证。
- 进程级并发（AC #1）无需新测试 — 已有的 `Spawn` 就是进程级并发原语，Story 1.2 已覆盖。
- 并发测试（`TestThread_Concurrent`、`TestCoroutine_Concurrent`）需配合 `-race` flag 运行以检测数据竞争。

---

## Next Steps

1. **DEV agent 开始实现**：按 Implementation Checklist 顺序，从 types -> interface -> thread -> coroutine -> reap
2. **逐步编译验证**：每实现一个方法后 `go build ./kernel/` 检查编译
3. **逐步测试验证**：每完成一组相关实现后运行对应测试
4. **全量回归**：所有测试通过后 `make test` 验证无回归
5. **更新 Story 状态**：所有测试通过且 `make all` 成功后标记 Story 6.5 为 done

---

## Knowledge Base References Applied

- **test-quality.md** — Given-When-Then 结构、单一断言、确定性、隔离性
- **test-levels-framework.md** — Unit/Integration 级别选择（后端项目无 E2E）
- **existing test patterns** — `signal_test.go`/`ipc_test.go`/`procgroup_test.go` 的测试辅助函数和断言模式

---

**Generated by BMad TEA Agent** - 2026-02-28
