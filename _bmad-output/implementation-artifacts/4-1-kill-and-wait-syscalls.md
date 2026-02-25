# Story 4.1: Kill 与 Wait 系统调用

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 终止运行中的智能体并等待其完成,
So that 我可以管理智能体的生命周期。

## Acceptance Criteria

1. **Kill syscall 实现** — Given `kernel/kernel.go` 中 Kill 已实现，When 调用 `Kill(pid, signal)`，Then 向目标进程发送取消信号（`cancel()`），进程 reasonStep 循环检测到取消后停止，进程状态转为 Zombie

2. **Wait syscall 实现** — Given `kernel/reap.go` 中 Wait 已实现，When 调用 `Wait(pid)`，Then 阻塞直到目标进程状态变为 Zombie，返回 ExitStatus（退出码 + 错误信息），触发资源释放：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree，状态转为 Dead，从进程表移除

3. **PID 不存在处理** — Given 目标 PID 不存在，When 调用 Kill 或 Wait，Then 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

4. **Kill 幂等性** — Given 进程已经是 Zombie，When 调用 Kill，Then 操作为空操作（幂等），不返回错误

5. **SyscallEvent 记录** — Given DebugChan 非 nil，When Kill 或 Wait syscall 执行，Then 入口/出口均写入 SyscallEvent（包含 Syscall 名称、Args、Result、Duration）

6. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 实现 Kill syscall (AC: #1, #3, #4, #5)
  - [x] 1.1 在 `kernel/kernel.go` 添加 `Kill(pid types.PID, signal types.Signal) error` 方法
  - [x] 1.2 Kill 查找进程表中的目标进程，PID 不存在返回 `*SyscallError{Code: ErrNotFound}`
  - [x] 1.3 进程已是 Zombie 或 Dead 时，Kill 返回 nil（幂等）
  - [x] 1.4 进程处于 Running 时，调用 `proc.Cancel()` 发送取消信号
  - [x] 1.5 Kill 入口/出口写入 SyscallEvent（DebugChan 非 nil 时）
  - [x] 1.6 编写 `kernel/kernel_test.go` Kill 单元测试（正常 kill、PID 不存在、已 Zombie 幂等、SyscallEvent 记录）

- [x] Task 2: 实现 Wait syscall 和资源释放 (AC: #2, #3, #5)
  - [x] 2.1 在 `kernel/reap.go` 添加 `Wait(pid types.PID) (ExitStatus, error)` 方法
  - [x] 2.2 Wait 查找进程表中的目标进程，PID 不存在返回 `*SyscallError{Code: ErrNotFound}`
  - [x] 2.3 Wait 阻塞等待 `proc.Done` channel 接收 ExitStatus
  - [x] 2.4 收到 ExitStatus 后，执行完整资源释放序列：
    - 2.4.1 `proc.Cancel()` — 确保 context 取消（幂等操作）
    - 2.4.2 `proc.wg.Wait()` — 等待所有 goroutine 完成（注意：CloseAll 在 wg 内 defer 执行）
    - 2.4.3 关闭 `proc.DebugChan`（close channel）
    - 2.4.4 `k.ctxMgr.CtxFree(proc.CtxID)` — 释放上下文空间
    - 2.4.5 `proc.Reap()` — Zombie → Dead 状态转换
    - 2.4.6 `k.RemoveProcess(pid)` — 从进程表移除
  - [x] 2.5 Wait 入口/出口写入 SyscallEvent（DebugChan 非 nil 时，注意：必须在 close DebugChan 之前写出口事件）
  - [x] 2.6 编写 `kernel/reap_test.go` Wait 单元测试（正常 wait、PID 不存在、资源释放验证、并发安全）

- [x] Task 3: 确保 Kernel 接口符合架构规范 (AC: #1, #2)
  - [x] 3.1 验证 `ProcessManager` 接口（在 `kernel/kernel.go`）已包含 `Kill` 和 `Wait` 方法签名
  - [x] 3.2 如未定义，在接口中添加 `Kill(pid PID, signal Signal) error` 和 `Wait(pid PID) (ExitStatus, error)`
  - [x] 3.3 确认 `KernelImpl` 满足 `ProcessManager` 接口（编译时检查）

- [x] Task 4: 集成测试 (AC: #1, #2, #6)
  - [x] 4.1 在 `cmd/crux/integration_test.go` 添加 Kill + Wait 集成测试
  - [x] 4.2 测试完整流程：Spawn → Kill → Wait → 验证资源释放
  - [x] 4.3 测试 Kill 后 reasonStep 循环正确退出
  - [x] 4.4 运行 `go test -race ./...` 和 `go vet ./...` 确认全部通过

## Dev Notes

### 核心设计决策

#### Kill 实现策略

Kill 是一个**异步操作**——调用 `proc.Cancel()` 后立即返回，不等待进程实际停止。进程停止是由 reasonStep 循环检测到 `proc.ctx.Done()` 后自行转 Zombie。

```go
func (k *KernelImpl) Kill(pid types.PID, signal types.Signal) error {
    proc, ok := k.GetProcess(pid)
    if !ok {
        return &SyscallError{Syscall: "Kill", PID: pid, Code: types.ErrNotFound, ...}
    }

    state := proc.GetState()
    if state == types.StateZombie || state == types.StateDead {
        return nil  // 幂等：已停止的进程不报错
    }

    proc.Cancel()  // 异步取消，reasonStep 自行检测并 finishProcess
    return nil
}
```

**Signal 处理：** MVP 阶段，SIGTERM 和 SIGKILL 行为相同（都调用 `cancel()`）。Phase 2 可扩展为 SIGTERM 给予优雅退出时间。

#### Wait 实现策略——资源释放顺序

Wait 是**同步阻塞操作**——阻塞直到进程进入 Zombie 状态，然后执行完整的资源释放链。

**资源释放顺序（严格遵循架构文档）：**
```
1. cancel()           — 确保 context 取消（幂等，Kill 可能已调用过）
2. wg.Wait()          — 等待 reasonStep goroutine 完成
                        ↳ goroutine 内部 defer 已执行 CloseAll（关闭所有 FD）
3. close(DebugChan)   — 关闭 debug 事件通道
4. CtxFree(CtxID)     — 释放上下文空间
5. proc.Reap()        — Zombie → Dead 状态转换
6. RemoveProcess(pid) — 从进程表移除
```

**关键注意事项：**
- `CloseAll` 已在 Spawn 中 goroutine 的 `defer` 中调用（kernel/kernel.go:209），Wait 中不需要再次调用
- SyscallEvent 出口事件必须在 `close(DebugChan)` **之前**写入
- `wg.Wait()` 确保 goroutine 完全退出后再继续，避免竞态

```go
func (k *KernelImpl) Wait(pid types.PID) (ExitStatus, error) {
    proc, ok := k.GetProcess(pid)
    if !ok {
        return ExitStatus{}, &SyscallError{Syscall: "Wait", PID: pid, Code: types.ErrNotFound, ...}
    }

    // 1. 阻塞等待进程完成
    exit := <-proc.Done

    // 2. 资源释放序列
    proc.Cancel()                    // 幂等
    proc.wg.Wait()                   // 等待 goroutine 完成（内部 defer CloseAll 已执行）
    close(proc.DebugChan)            // 关闭 debug 通道
    _ = k.ctxMgr.CtxFree(proc.CtxID) // 释放上下文
    _ = proc.Reap()                  // Zombie → Dead
    k.RemoveProcess(pid)             // 从进程表移除

    return exit, nil
}
```

#### SyscallEvent 记录模式

参考已有的 `emitEvent` 辅助函数（kernel/kernel.go），Kill 和 Wait 都需要在入口/出口写入事件：

```go
// Kill 事件记录
k.emitEvent(proc, "Kill", map[string]any{"pid": pid, "signal": signal}, nil, nil, duration)

// Wait 事件记录——注意必须在 close(DebugChan) 之前写
k.emitEvent(proc, "Wait", map[string]any{"pid": pid}, exit, nil, duration)
```

#### Done Channel 阻塞安全性

`proc.Done` 是缓冲为 1 的 channel：
- `finishProcess` 通过 `select { case proc.Done <- exit: default: }` 非阻塞写入
- `Wait` 通过 `exit := <-proc.Done` 阻塞读取
- 如果没有 Wait 调用者，ExitStatus 留在 channel 中不丢失
- **多个 goroutine 同时 Wait 同一个 PID**：第一个读取成功，后续的将永久阻塞——MVP 阶段不处理此场景，限定为单次 Wait

### 前序 Story 经验（Story 3.4 / Epic 3）

**已完成的 API（直接使用）：**
- `kernel/kernel.go` — `KernelImpl`、`Spawn`、`reasonStep`、`finishProcess`、`emitEvent`、`AddProcess`、`GetProcess`、`RemoveProcess`
- `kernel/process.go` — `Process`、`NewProcess`、`Cancel`、`Terminate`、`Reap`、`GetState`
- `kernel/errors.go` — `SyscallError`、`NewSyscallError`
- `internal/types/types.go` — `PID`、`Signal`（SIGTERM=1, SIGKILL=2）、`ProcessState`（StateCreated/Running/Zombie/Dead）、`ErrCode`
- `context/context.go` — `Manager.CtxFree(cid)`
- `vfs/vfs.go` — `CloseAll(pid)`（已在 Spawn goroutine 的 defer 中调用）
- `kernel/kernel.go:206` — 注释 `"Note: CtxFree deferred to Wait/Reap (Story 4.1) per resource release order"` 明确指出 CtxFree 需在本 Story 中实现

**前序 Story 已知限制（本 Story 直接解决）：**
- [H1] `kernel/kernel.go:finishProcess()` 不关闭 `proc.DebugChan`。Story 3.3 测试通过手动 `close(proc.DebugChan)` 绕过。**本 Story 的 Wait 实现将正式关闭 DebugChan**

**Git 提交模式（保持一致）：**
```
101842a Finalize Story 3.4: Syscall Trace Line UI Component Implementation
82fb76f Finalize Story 3.3: Astrace CLI Command Implementation and Testing
```
- 英文动词短语开头，Story ID + 标题

### 已有代码关键 API 参考

**kernel/kernel.go — KernelImpl 方法清单：**
```go
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl
func (k *KernelImpl) Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error)
func (k *KernelImpl) AddProcess(p *Process)
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool)
func (k *KernelImpl) RemoveProcess(pid types.PID)
func (k *KernelImpl) ListProcesses() []*Process
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, dur time.Duration)
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus)
```

**kernel/process.go — Process 结构体：**
```go
type Process struct {
    PID        types.PID
    PPID       types.PID
    State      types.ProcessState
    Intent     string
    Skills     []string
    Children   []types.PID
    FDTable    map[types.FD]vfs.VFSFile
    DebugChan  chan types.SyscallEvent   // 缓冲 256
    Done       chan ExitStatus           // 缓冲 1
    CreatedAt  time.Time
    Exit       *ExitStatus
    CtxID      types.CtxID
    Result     string
    TokensUsed int
    AllowedDevices []string

    mu     sync.Mutex      // 保护 State
    cancel context.CancelFunc
    ctx    context.Context
    wg     sync.WaitGroup
}
```

**kernel/process.go — 关键方法：**
```go
func (p *Process) Cancel()                      // 调用 cancel()，触发 ctx.Done()
func (p *Process) Terminate(exit ExitStatus) error // Running → Zombie，设置 p.Exit
func (p *Process) Reap() error                  // Zombie → Dead
func (p *Process) GetState() types.ProcessState // 线程安全读取状态
```

**kernel/kernel.go — reasonStep 取消检查（第 275-286 行）：**
```go
select {
case <-proc.ctx.Done():
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step": step, "action": "cancelled",
    }, nil, proc.ctx.Err(), time.Since(stepStart))
    k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
    return
default:
}
```

**kernel/kernel.go — finishProcess（第 236-251 行）：**
```go
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
    _ = proc.Terminate(exit)  // Running → Zombie
    if k.callbacks != nil {
        if exit.Err != nil { k.callbacks.OnError(proc.PID, exit.Err) }
        k.callbacks.OnComplete(proc.PID, proc.Result, exit)
    }
    select {
    case proc.Done <- exit:  // 非阻塞写入 Done channel
    default:
    }
}
```

**kernel/kernel.go — Spawn goroutine 资源清理 defer（第 207-213 行）：**
```go
proc.wg.Add(1)
go func() {
    defer proc.wg.Done()
    defer k.vfs.CloseAll(proc.PID)  // ← CloseAll 已在此处 defer
    _ = proc.Start()                // Created → Running
    k.reasonStep(proc, llmFD, opts)
}()
```

**context/context.go — CtxFree：**
```go
func (m *Manager) CtxFree(cid types.CtxID) error  // LoadAndDelete 原子删除上下文
```

**internal/types/types.go — Signal：**
```go
type Signal int
const (
    SIGTERM Signal = iota + 1  // 值为 1
    SIGKILL                     // 值为 2
)
```

### 注意事项与防错

#### Wait 中 DebugChan 关闭时序

SyscallEvent 出口事件必须在 `close(proc.DebugChan)` **之前**写入。序列：
```
emitEvent(proc, "Wait", ...)   // ← 先写事件
close(proc.DebugChan)          // ← 再关闭通道
```

如果反过来，写入已关闭的 channel 会 panic。

#### CloseAll 在 wg 内执行，Wait 中不要再调用

`CloseAll` 已在 Spawn 的 goroutine `defer` 中注册（`defer k.vfs.CloseAll(proc.PID)`）。`proc.wg.Wait()` 会等待该 goroutine 完成，此时 CloseAll 已执行。Wait 中**不应**再次调用 CloseAll，否则会导致 "fd table not found" 错误。

#### Kill 是异步的，不要在 Kill 中等待进程停止

Kill 只调用 `proc.Cancel()` 然后返回。进程实际何时停止取决于 reasonStep 循环检测到 `ctx.Done()` 的时机。如果需要等待，应该在 Kill 之后调用 Wait。

#### 多次 Wait 同一 PID

`proc.Done` 是缓冲 1 的 channel，第一次 `<-proc.Done` 读取成功后 channel 为空。第二次 Wait 会永久阻塞。MVP 阶段不处理此场景，但测试中需避免。

#### Process.cancel 可能为 nil

如果进程还在 Created 状态（Spawn 尚未设置 cancel），`proc.Cancel()` 内部已有 nil 检查，安全。

#### 进程状态检查——Created 状态的 Kill

如果进程处于 Created 状态（Spawn 刚创建但 goroutine 尚未启动），Kill 调用 Cancel() 后：
- context 被取消，但 reasonStep goroutine 尚未开始执行
- goroutine 启动后立即检测到 `ctx.Done()`，调用 `finishProcess` 转 Zombie
- 这是安全的，无需特殊处理

### 接口合规

**ProcessManager 接口（架构文档要求）：**
```go
type ProcessManager interface {
    Spawn(intent string, agent AgentInfo, opts SpawnOpts) (PID, error)
    Kill(pid PID, signal Signal) error
    Wait(pid PID) (ExitStatus, error)
    GetPID() PID
    PS(filter PSFilter) ([]ProcInfo, error)
}
```

其中 `Kill` 和 `Wait` 是本 Story 新增。`GetPID` 和 `PS` 属于后续 Story（4.4）。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR7 | LLM 超时/错误时，进程在 5 秒内转入 Zombie | Kill 调用 cancel() 后，reasonStep 在下一次循环检查点检测到取消，调用 finishProcess 转 Zombie |
| NFR8 | 进程退出后 goroutine/context 在 10 秒内释放 | Wait 通过 wg.Wait() 确保 goroutine 退出 + CtxFree 释放上下文 |
| NFR9 | 进程表在异常退出后保持一致性 | Wait 在完整释放后调用 RemoveProcess 移除进程 |
| NFR10 | CLI 进程不崩溃 | Kill/Wait 所有路径返回 error 或 nil，不 panic |

### 范围边界

**本 Story 包含：**
- `kernel/kernel.go` — 添加 Kill 方法
- `kernel/reap.go` — 添加 Wait 方法（含完整资源释放链）
- `kernel/kernel_test.go` — Kill 单元测试
- `kernel/reap_test.go` — Wait 单元测试（可能需新建）
- `cmd/crux/integration_test.go` — Kill + Wait 集成测试

**本 Story 不包含：**
- `crux kill <pid>` CLI 子命令（Story 4.4 或后续）
- 孤儿进程 reparent（Story 4.2）
- Zombie 自动回收 reaper（Story 4.2）
- `/proc` 动态文件系统（Story 4.3）
- `crux ps` 命令和 Process Table UI（Story 4.4）
- `ctx_free` 独立测试（Story 4.5）
- ProcessManager 接口中的 `GetPID()` 和 `PS()`（Story 4.4）

### Project Structure Notes

**新建/修改文件：**
```
kernel/kernel.go           — 添加 Kill 方法 + SyscallEvent 记录
kernel/reap.go             — 添加 Wait 方法 + 完整资源释放链
kernel/kernel_test.go      — Kill 单元测试（新增测试用例）
kernel/reap_test.go        — Wait 单元测试（新建或扩展）
cmd/crux/integration_test.go — Kill + Wait 集成测试
```

**不修改文件：**
```
kernel/process.go          — Cancel/Terminate/Reap 已实现，不修改
kernel/errors.go           — SyscallError 已定义，不修改
internal/types/types.go    — Signal/ProcessState 已定义，不修改
context/context.go         — CtxFree 已实现，只调用不修改
vfs/vfs.go                 — CloseAll 已在 Spawn defer 中调用，不修改
debug/astrace.go           — 不修改
internal/ui/*              — 不修改
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 2] — 进程模型与并发，资源释放顺序
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 1] — Syscall ABI 设计，ProcessManager 接口
- [Source: _bmad-output/planning-artifacts/architecture.md#实现模式] — 错误处理模式、状态转移规则
- [Source: _bmad-output/planning-artifacts/prd.md#FR3] — Kill 功能需求
- [Source: _bmad-output/planning-artifacts/prd.md#FR4] — Wait 功能需求
- [Source: _bmad-output/planning-artifacts/prd.md#NFR7-NFR9] — 可靠性非功能需求
- [Source: _bmad-output/project-context.md#进程状态机] — 合法转移规则
- [Source: _bmad-output/project-context.md#资源释放顺序] — cancel→wg.Wait→FD→DebugChan→CtxFree→Dead→移除
- [Source: kernel/kernel.go:206] — "CtxFree deferred to Wait/Reap (Story 4.1)" 注释
- [Source: kernel/kernel.go:207-213] — Spawn goroutine defer CloseAll
- [Source: kernel/kernel.go:236-251] — finishProcess 实现
- [Source: kernel/kernel.go:275-286] — reasonStep 取消检查
- [Source: kernel/process.go:134-140] — Process.Cancel() 方法
- [Source: kernel/process.go:117-126] — Process.Terminate() 方法
- [Source: kernel/process.go:129-131] — Process.Reap() 方法
- [Source: context/context.go:98-109] — CtxFree 实现
- [Source: _bmad-output/implementation-artifacts/3-4-syscall-trace-line-ui-component.md] — 前序 Story 经验、已知限制 [H1]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1: Kill syscall 实现完成。`kernel/kernel.go` 添加 `Kill(pid, signal)` 方法，支持 PID 查找、Zombie/Dead 幂等、Running 状态调用 `proc.Cancel()`、SyscallEvent 入口/出口记录。4 个单元测试全部通过。
- Task 2: Wait syscall 实现完成。新建 `kernel/reap.go`，添加 `Wait(pid)` 方法，实现完整资源释放序列：`Cancel → wg.Wait → close(DebugChan) → CtxFree → Reap → RemoveProcess`。修复了 `emitEvent` 与 `close(DebugChan)` 的并发竞态——在 `emitEvent` 中持有 `proc.mu` 保护 channel 发送，在 Wait 中先 nil 化 `DebugChan` 再关闭。6 个单元测试全部通过。
- Task 3: ProcessManager 接口定义完成。在 `kernel/kernel.go` 添加 `ProcessManager` 接口（包含 Spawn、Kill、Wait），编译时检查 `var _ ProcessManager = (*KernelImpl)(nil)` 通过。
- Task 4: 集成测试完成。在 `cmd/crux/integration_test.go` 添加 3 个 Kill+Wait 集成测试：完整生命周期（Spawn→Kill→Wait→资源释放验证）、reasonStep 退出验证、并发竞态检测。`go test -race ./...` 全部通过，`go vet ./...` 无警告。

### File List

- `kernel/kernel.go` — 添加 Kill 方法、ProcessManager 接口定义、编译时接口检查、emitEvent 并发安全修复（proc.mu 保护 channel 发送）
- `kernel/reap.go` — 新建文件，Wait 方法 + 完整资源释放链实现
- `kernel/kernel_test.go` — 添加 4 个 Kill 单元测试（TestKill_RunningProcess、TestKill_PIDNotFound、TestKill_ZombieIdempotent、TestKill_SyscallEvent）
- `kernel/reap_test.go` — 新建文件，6 个 Wait 单元测试（TestWait_NormalCompletion、TestWait_KillThenWait、TestWait_PIDNotFound、TestWait_ResourceRelease、TestWait_ConcurrentSafe、TestWait_SyscallEvent）
- `cmd/crux/integration_test.go` — 添加 3 个 Kill+Wait 集成测试（TestE2E_KillWait_FullLifecycle、TestE2E_KillWait_ReasonStepExits、TestE2E_KillWait_RaceDetection）
