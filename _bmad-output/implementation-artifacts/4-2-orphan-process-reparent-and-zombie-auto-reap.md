# Story 4.2: 孤儿进程 reparent 与 Zombie 自动回收

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 内核开发者,
I want 孤儿进程自动挂载到 init，Zombie 进程自动回收,
So that 系统不会积累无主进程或资源泄漏。

## Acceptance Criteria

1. **孤儿进程 reparent** — Given 父进程退出，When 子进程仍在运行，Then 子进程的 PPID 自动变更为 PID 0（init/kernel），And 内核 reaper 负责后续 Wait 回收

2. **Zombie 自动回收** — Given 进程状态变为 Zombie，When 内核 reaper 检测到该进程为孤儿（父进程不在进程表中），Then 自动执行 Wait 逻辑：资源释放 → 状态转 Dead → 移除进程表，And 资源释放顺序：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → Dead → 移除

3. **资源释放时效** — Given 进程退出后，When 10 秒内检测 goroutine 状态，Then 所有关联 goroutine 和 context 内存已释放，无泄漏（NFR8）

4. **进程表一致性** — Given 进程异常退出（panic、timeout），When 检查进程表，Then 进程表保持一致性，无悬挂 PID（NFR9）

5. **SyscallEvent 记录** — Given DebugChan 非 nil，When reparent 或 auto-reap 执行，Then 入口/出口均写入 SyscallEvent

6. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 添加 Children 管理方法到 Process (AC: #1, #4)
  - [x] 1.1 在 `kernel/process.go` 添加 `AddChild(pid types.PID)` 方法（在 `proc.mu` 保护下追加到 Children 切片）
  - [x] 1.2 添加 `RemoveChild(pid types.PID)` 方法（在 `proc.mu` 保护下从 Children 切片移除）
  - [x] 1.3 添加 `GetChildren() []types.PID` 方法（在 `proc.mu` 保护下返回 Children 切片的副本）
  - [x] 1.4 在 `kernel/process_test.go` 添加 Children 管理单元测试（AddChild、RemoveChild、并发安全）

- [x] Task 2: 更新 Spawn 支持父子进程追踪 (AC: #1, #4)
  - [x] 2.1 在 `SpawnOpts` 结构体中添加 `ParentPID types.PID` 字段（默认 0 表示顶层进程）
  - [x] 2.2 修改 `Spawn` 方法：当 `opts.ParentPID > 0` 时，设置 `proc.PPID = opts.ParentPID`
  - [x] 2.3 修改 `Spawn` 方法：当 `opts.ParentPID > 0` 时，查找父进程并调用 `parent.AddChild(proc.PID)`
  - [x] 2.4 验证父进程存在，不存在则返回 `*SyscallError{Code: ErrNotFound}`
  - [x] 2.5 在 `kernel/kernel_test.go` 添加 Spawn 父子追踪测试

- [x] Task 3: 提取 reap 逻辑为共享 helper (AC: #2, #3)
  - [x] 3.1 在 `kernel/reap.go` 提取 Wait 中的资源释放序列为 `reapProcess(proc *Process)` 内部方法
  - [x] 3.2 `reapProcess` 执行完整释放序列：cancel → wg.Wait → emitEvent → close(DebugChan) → CtxFree → Reap → RemoveProcess
  - [x] 3.3 `reapProcess` 检查进程是否仍在进程表中且为 Zombie，否则跳过（防止重复 reap）
  - [x] 3.4 重构 `Wait` 使用 `reapProcess`，保持行为不变
  - [x] 3.5 运行现有 Wait 测试确认重构无回归

- [x] Task 4: 实现孤儿进程 reparent (AC: #1, #5)
  - [x] 4.1 在 `kernel/reap.go` 添加 `handleOrphanChildren(proc *Process)` 内部方法
  - [x] 4.2 在 Wait 的资源释放序列中、RemoveProcess 之前调用 `handleOrphanChildren`
  - [x] 4.3 handleOrphanChildren 遍历 `proc.GetChildren()`，对每个子进程：
    - Running 状态：设置 `child.PPID = 0`（reparent 到 init/kernel）
    - Zombie 状态：推送到 `reapCh` 通道进行自动回收
  - [x] 4.4 reparent 时写入 SyscallEvent（DebugChan 非 nil 时）
  - [x] 4.5 在 `kernel/reap_test.go` 添加孤儿 reparent 单元测试

- [x] Task 5: 实现 Zombie 自动回收 Reaper (AC: #2, #3, #5)
  - [x] 5.1 在 `KernelImpl` 添加 `reapCh chan types.PID`（缓冲 64）、`stopCh chan struct{}`、`reaperWg sync.WaitGroup` 字段
  - [x] 5.2 实现 `startReaper()` 方法：启动后台 goroutine，从 `reapCh` 读取 PID 并调用 `reapProcess`
  - [x] 5.3 实现 `stopReaper()` 方法：关闭 `stopCh`，等待 `reaperWg.Done()`
  - [x] 5.4 在 `finishProcess` 中添加孤儿检测：进程变 Zombie 后，检查父进程是否在进程表中，不在则推送到 `reapCh`
  - [x] 5.5 在 `NewKernel` 中调用 `startReaper()`
  - [x] 5.6 添加 `Shutdown()` 方法到 `KernelImpl`（停止 reaper + 清理）
  - [x] 5.7 在 `kernel/reap_test.go` 添加自动回收单元测试

- [x] Task 6: 综合测试 (AC: #1, #2, #3, #4, #6)
  - [x] 6.1 测试完整流程：Parent Spawn Child → Parent Finish → Child Reparent → Child Finish → Auto-Reap
  - [x] 6.2 测试多子进程场景：父进程有 3 个子进程（1 Running + 1 Zombie + 1 已完成）
  - [x] 6.3 测试进程表一致性：异常退出后无悬挂 PID（NFR9）
  - [x] 6.4 测试资源释放时效：验证 goroutine/context 在 10 秒内释放（NFR8）
  - [x] 6.5 测试并发安全：多个进程同时退出，reaper 并发处理
  - [x] 6.6 运行 `go test -race ./...` 和 `go vet ./...` 确认全部通过

## Dev Notes

### 核心设计决策

#### Init 进程概念——PID 0 作为虚拟 Init

**设计决策：** PID 0 作为虚拟 init/kernel 进程，不是真实的 Process 对象。

**理由：**
- 当前 PID 计数器从 1 开始（`atomic.AddUint64(&pidCounter, 1)`），PID 0 永远不会被分配给真实进程
- 所有 CLI 直接 spawn 的进程已经使用 PPID=0（表示"由内核/CLI 创建"）
- 孤儿进程 reparent 到 PID 0，语义一致："由内核管理"
- 避免了"PID 1 是 init 还是用户进程"的歧义——PID 1 是第一个用户进程

**与 AC 的差异说明：** AC 原文写 "PPID 自动变更为 PID 1（init）"，实现中使用 PID 0 作为 init。原因是 PID 1 是第一个用户创建的进程（会退出），而 PID 0 是稳定的虚拟 init，不会被分配也不会退出。

```go
// PID 0 = 虚拟 init/kernel（不在进程表中）
// 所有 CLI spawn 的进程：PPID = 0
// 孤儿进程 reparent 后：PPID = 0
// Reaper 负责自动回收 PPID=0 的孤儿 zombie
```

#### 事件驱动的 Reaper 架构

**设计决策：** 使用 channel（`reapCh`）驱动 Zombie 自动回收，而非轮询。

```
Process → Zombie（finishProcess）
  ├─ 父进程存在 → 等待父进程 Wait
  └─ 父进程不存在 → 推送到 reapCh → Reaper goroutine 自动回收

Parent Wait（reap parent）
  ├─ Running 子进程 → reparent（PPID=0）
  └─ Zombie 子进程 → 推送到 reapCh → Reaper goroutine 自动回收
```

**优势：**
- 零轮询开销：只在有工作时唤醒
- 低延迟：zombie 产生后立即排队回收
- 可测试：通过 reapCh 验证回收行为

```go
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
    vfs       *vfs.VFS
    ctxMgr    *cruxctx.Manager
    callbacks KernelCallbacks

    // Reaper infrastructure (Story 4.2)
    reapCh   chan types.PID     // 需要自动回收的 PID 队列
    stopCh   chan struct{}       // 停止 reaper 信号
    reaperWg sync.WaitGroup     // 等待 reaper goroutine 退出
}
```

#### reapProcess 共享 Helper

从 Wait 中提取资源释放逻辑为 `reapProcess`，供 Wait 和 auto-reaper 共用。

```go
// reapProcess 执行完整的资源释放序列（Zombie → Dead → 移除）
// 前置条件：proc 必须处于 Zombie 状态
// 幂等安全：如果进程已不在表中或已是 Dead，则跳过
func (k *KernelImpl) reapProcess(proc *Process) {
    // 安全检查：进程是否仍在表中且为 Zombie
    current, ok := k.GetProcess(proc.PID)
    if !ok || current != proc || proc.GetState() != types.StateZombie {
        return // 已被其他 goroutine reap，跳过
    }

    // 处理孤儿子进程（递归）
    k.handleOrphanChildren(proc)

    // 资源释放序列
    proc.Cancel()                       // 1. 取消 context（幂等）
    proc.wg.Wait()                      // 2. 等待 goroutine 完成
    // 注意：SyscallEvent 必须在 close(DebugChan) 之前写入
    // 3. close(DebugChan) — 需要先 nil 化再关闭（与 emitEvent 的并发安全）
    proc.mu.Lock()
    ch := proc.DebugChan
    proc.DebugChan = nil
    proc.mu.Unlock()
    if ch != nil {
        close(ch)
    }
    _ = k.ctxMgr.CtxFree(proc.CtxID)   // 4. 释放上下文
    _ = proc.Reap()                     // 5. Zombie → Dead
    k.RemoveProcess(proc.PID)           // 6. 从进程表移除
}
```

**关键：** `reapProcess` 开头的安全检查确保即使多个 goroutine（Wait + Reaper）同时尝试 reap 同一个进程，也只有一个会执行清理。但由于 `GetProcess` + 状态检查不是原子的，需要更安全的机制——使用 `sync.Once` 或在 Process 中添加一个 `reaping` 标志位。

**推荐方案：** 在 Process 中添加 `reapOnce sync.Once`：

```go
type Process struct {
    // ... 现有字段 ...
    reapOnce sync.Once  // 确保 reap 只执行一次
}
```

```go
func (k *KernelImpl) reapProcess(proc *Process) {
    proc.reapOnce.Do(func() {
        k.handleOrphanChildren(proc)
        proc.Cancel()
        proc.wg.Wait()
        // ... 完整释放序列 ...
        k.RemoveProcess(proc.PID)
    })
}
```

#### Wait 重构——使用 reapProcess

```go
func (k *KernelImpl) Wait(pid types.PID) (ExitStatus, error) {
    proc, ok := k.GetProcess(pid)
    if !ok {
        return ExitStatus{}, &SyscallError{
            Syscall: "Wait", PID: pid, Code: types.ErrNotFound,
            Err: fmt.Errorf("process %d not found", pid),
        }
    }

    start := time.Now()

    // 1. 阻塞等待进程完成（Zombie）
    exit := <-proc.Done

    // 2. 写 SyscallEvent（在 reapProcess 关闭 DebugChan 之前）
    k.emitEvent(proc, "Wait", map[string]any{"pid": pid}, exit, nil, time.Since(start))

    // 3. 调用共享的 reap 逻辑
    k.reapProcess(proc)

    return exit, nil
}
```

#### handleOrphanChildren 实现

```go
func (k *KernelImpl) handleOrphanChildren(parent *Process) {
    children := parent.GetChildren()
    for _, childPID := range children {
        child, ok := k.GetProcess(childPID)
        if !ok {
            continue // 子进程已被移除
        }

        state := child.GetState()
        switch state {
        case types.StateRunning, types.StateCreated:
            // Reparent 到 init（PID 0）
            child.mu.Lock()
            child.PPID = 0
            child.mu.Unlock()
            // SyscallEvent 记录
            k.emitEvent(child, "Reparent", map[string]any{
                "child_pid": childPID, "old_ppid": parent.PID, "new_ppid": types.PID(0),
            }, nil, nil, 0)

        case types.StateZombie:
            // 孤儿 Zombie → 推送到 reapCh 自动回收
            select {
            case k.reapCh <- childPID:
            default:
                // reapCh 满了，降级为同步 reap
                go k.reapProcess(child)
            }
        }
        // StateDead: 已清理，忽略
    }
}
```

#### finishProcess 中的孤儿检测

```go
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
    _ = proc.Terminate(exit)  // Running → Zombie
    if k.callbacks != nil {
        if exit.Err != nil { k.callbacks.OnError(proc.PID, exit.Err) }
        k.callbacks.OnComplete(proc.PID, proc.Result, exit)
    }
    select {
    case proc.Done <- exit:
    default:
    }

    // Story 4.2: 孤儿检测
    // 如果父进程已不在进程表中，推送到 reapCh
    if proc.PPID > 0 {
        if _, parentExists := k.GetProcess(proc.PPID); !parentExists {
            select {
            case k.reapCh <- proc.PID:
            default:
                go k.reapProcess(proc)
            }
        }
    }
    // PPID=0 的进程由 CLI 或外部 Wait 负责回收，不自动 reap
}
```

**注意：** PPID=0 的进程（CLI 直接 spawn）不会被自动 reap——它们由 CLI 的 `<-proc.Done` + 手动 Wait 负责。只有 PPID > 0 且父进程已不存在的进程才会被自动 reap。这确保 CLI 流程不受影响。

#### Reaper Goroutine

```go
func (k *KernelImpl) startReaper() {
    k.reaperWg.Add(1)
    go func() {
        defer k.reaperWg.Done()
        for {
            select {
            case pid := <-k.reapCh:
                proc, ok := k.GetProcess(pid)
                if !ok {
                    continue // 已被 Wait reap
                }
                if proc.GetState() == types.StateZombie {
                    k.reapProcess(proc)
                }
            case <-k.stopCh:
                // 清空 reapCh 中剩余的 PID
                for {
                    select {
                    case pid := <-k.reapCh:
                        if proc, ok := k.GetProcess(pid); ok && proc.GetState() == types.StateZombie {
                            k.reapProcess(proc)
                        }
                    default:
                        return
                    }
                }
            }
        }
    }()
}

func (k *KernelImpl) Shutdown() {
    close(k.stopCh)
    k.reaperWg.Wait()
}
```

#### SpawnOpts 扩展

```go
type SpawnOpts struct {
    Model        string
    SystemPrompt string
    MaxTurns     int
    TimeoutMs    int64
    ParentPID    types.PID  // 父进程 PID（0 表示顶层/CLI spawn）
}
```

在 Spawn 中：

```go
func (k *KernelImpl) Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error) {
    // ... 现有逻辑 ...
    proc := NewProcess(opts.ParentPID, intent, skillNames)

    // 维护父进程的 Children 列表
    if opts.ParentPID > 0 {
        parent, ok := k.GetProcess(opts.ParentPID)
        if !ok {
            return 0, &SyscallError{
                Syscall: "Spawn", PID: opts.ParentPID,
                Code: types.ErrNotFound,
                Err: fmt.Errorf("parent process %d not found", opts.ParentPID),
            }
        }
        parent.AddChild(proc.PID)
    }

    k.AddProcess(proc)
    // ... 启动 goroutine ...
}
```

### 前序 Story 经验（Story 4.1）

**Story 4.1 Completion Notes（直接适用）：**

- `emitEvent` 已有并发安全修复——在 `proc.mu` 保护下检查 DebugChan 非 nil 后发送。Story 4.2 的 `reapProcess` 关闭 DebugChan 时需先 nil 化（与 4.1 的模式一致）
- `Wait` 中 SyscallEvent 出口事件必须在 `close(DebugChan)` 之前写入——Story 4.2 重构 Wait 为 reapProcess 时必须保持此顺序
- `CloseAll` 已在 Spawn goroutine 的 defer 中调用，reapProcess 中不要重复调用
- Done channel 缓冲为 1，finishProcess 非阻塞写入——Story 4.2 不需要修改此机制
- `proc.reapOnce` 是新增的并发安全机制，Story 4.1 中没有（因为只有 Wait 一个调用者），Story 4.2 引入 reaper 后有多调用者

**Story 4.1 已实现的 API（直接使用）：**
- `kernel/kernel.go` — Kill、emitEvent（并发安全版）、finishProcess
- `kernel/reap.go` — Wait（将被重构）
- `kernel/process.go` — Cancel、Terminate、Reap、GetState

**Git 提交模式（保持一致）：**
```
841ce08 Implement Story 4.1: Kill and Wait Syscalls
6f0f994 Refactor emitEvent function for improved race condition handling
```

### 已有代码关键 API 参考

**kernel/kernel.go — KernelImpl 结构体（第 85-97 行）：**
```go
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
    vfs       *vfs.VFS
    ctxMgr    *cruxctx.Manager
    callbacks KernelCallbacks
}
```

**kernel/kernel.go — NewKernel（第 99-106 行）：**
```go
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl {
    return &KernelImpl{
        procTable: xsync.NewSyncMap[types.PID, *Process](),
        vfs:       v,
        ctxMgr:    ctxMgr,
        callbacks: cb,
    }
}
```

**kernel/kernel.go — Spawn 创建进程（第 118 行）：**
```go
proc := NewProcess(0, intent, skillNames)  // ← PPID 总是 0，需修改
```

**kernel/kernel.go — Spawn goroutine defer（第 207-213 行）：**
```go
proc.wg.Add(1)
go func() {
    defer proc.wg.Done()
    defer k.vfs.CloseAll(proc.PID)  // CloseAll 已在此处 defer
    _ = proc.Start()
    k.reasonStep(proc, llmFD, opts)
}()
```

**kernel/kernel.go — finishProcess（第 251-266 行）：**
```go
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
    _ = proc.Terminate(exit)  // Running → Zombie
    if k.callbacks != nil { ... }
    select {
    case proc.Done <- exit:
    default:
    }
    // ← Story 4.2：在此处添加孤儿检测逻辑
}
```

**kernel/kernel.go — emitEvent（第 238-248 行）：**
```go
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, dur time.Duration) {
    proc.mu.Lock()
    ch := proc.DebugChan
    proc.mu.Unlock()
    if ch == nil { return }
    event := types.SyscallEvent{...}
    select {
    case ch <- event:
    default:
    }
}
```

**kernel/reap.go — Wait（第 13-59 行）——将被重构：**
```go
func (k *KernelImpl) Wait(pid types.PID) (ExitStatus, error) {
    proc, ok := k.GetProcess(pid)
    if !ok {
        return ExitStatus{}, &SyscallError{...}
    }
    start := time.Now()
    exit := <-proc.Done
    k.emitEvent(proc, "Wait", ...)
    // 资源释放序列（将提取到 reapProcess）
    proc.Cancel()
    proc.wg.Wait()
    proc.mu.Lock()
    ch := proc.DebugChan
    proc.DebugChan = nil
    proc.mu.Unlock()
    if ch != nil { close(ch) }
    _ = k.ctxMgr.CtxFree(proc.CtxID)
    _ = proc.Reap()
    k.RemoveProcess(pid)
    return exit, nil
}
```

**kernel/process.go — Process 结构体（第 24-46 行）：**
```go
type Process struct {
    PID        types.PID
    PPID       types.PID              // ← 父进程 PID
    State      types.ProcessState
    Intent     string
    Skills     []string
    Children   []types.PID            // ← 子进程列表（当前未维护）
    FDTable    map[types.FD]vfs.VFSFile
    DebugChan  chan types.SyscallEvent
    Done       chan ExitStatus
    CreatedAt  time.Time
    Exit       *ExitStatus
    CtxID      types.CtxID
    Result     string
    TokensUsed int
    AllowedDevices []string

    mu     sync.Mutex
    cancel context.CancelFunc
    ctx    context.Context
    wg     sync.WaitGroup
}
```

**kernel/process.go — NewProcess（第 56-81 行）：**
```go
func NewProcess(ppid types.PID, intent string, skills []string) *Process {
    return &Process{
        PID:       types.PID(atomic.AddUint64(&pidCounter, 1)),
        PPID:      ppid,                    // ← 从参数设置
        State:     types.StateCreated,
        Intent:    intent,
        Skills:    skills,
        Children:  []types.PID{},           // ← 初始化为空切片
        DebugChan: make(chan types.SyscallEvent, 256),
        Done:      make(chan ExitStatus, 1),
        CreatedAt: time.Now(),
    }
}
```

**kernel/process.go — 关键方法：**
```go
func (p *Process) Cancel()                        // 第 134 行
func (p *Process) Terminate(exit ExitStatus) error // 第 117 行：Running → Zombie
func (p *Process) Reap() error                    // 第 129 行：Zombie → Dead
func (p *Process) GetState() types.ProcessState   // 第 112 行
```

**context/context.go — CtxFree（第 97-109 行）：**
```go
func (m *Manager) CtxFree(cid types.CtxID) error {
    _, ok := m.contexts.LoadAndDelete(cid)
    if !ok { return &ContextError{...} }
    return nil
}
```

**kernel/kernel_test.go — 测试 Helper：**
```go
func newTestKernel(llmFile *mockLLMFile) (*KernelImpl, *vfs.VFS, *cruxctx.Manager) // 第 94 行
func newSimpleKernel() *KernelImpl                                                  // 第 153 行
func makeLLMResponse(content string, tokens int) []byte                             // 第 106 行
```

**blockingLLMFile（kernel_test.go 第 652-673 行）——用于 Kill/Wait/Reaper 测试：**
```go
type blockingLLMFile struct {
    blockCh chan struct{}
    closed  bool
}
// Write() 阻塞在 blockCh 上，允许控制进程执行时机
```

### 注意事项与防错

#### reapProcess 与 Wait 的并发安全

当 Reaper goroutine 和外部 Wait 调用同时尝试 reap 同一个进程时，`proc.reapOnce` 确保只有一个执行清理。但 Wait 的调用者需要 ExitStatus 返回值——如果 Reaper 先执行了 reapProcess，Wait 仍然能从 `<-proc.Done` 拿到 ExitStatus（Done channel 的数据在 finishProcess 时就写入了），只是后续的 reapProcess 调用会被 Once.Do 跳过。

```go
// Wait 的安全流程：
exit := <-proc.Done           // ← 一定能拿到（finishProcess 已写入）
k.emitEvent(proc, "Wait",...) // ← 可能 DebugChan 已被 reaper 关闭，emitEvent 有 nil 检查
k.reapProcess(proc)           // ← 如果 reaper 先到，Once.Do 跳过
return exit, nil              // ← 返回正确的 ExitStatus
```

#### finishProcess 中孤儿检测的时序

`finishProcess` 中的孤儿检测（检查父进程是否在表中）可能有竞态：
1. 父进程 Wait 开始 → 2. 子进程 finishProcess 检查父进程（存在）→ 3. 父进程 RemoveProcess → 4. 子进程成为无人 reap 的 zombie

**解决方案：** Wait 中的 `handleOrphanChildren` 在 RemoveProcess 之前执行，此时子进程如果刚变成 Zombie，会被父进程的 handleOrphanChildren 捕获并推送到 reapCh。如果子进程在父进程 RemoveProcess 之后才变成 Zombie，finishProcess 的孤儿检测会捕获它。

关键是两个检查点**覆盖了所有时序**：
- 子进程先于父进程变 Zombie → 父进程 Wait 的 handleOrphanChildren 处理
- 子进程后于父进程变 Zombie → finishProcess 的孤儿检测处理

#### DebugChan 关闭与 emitEvent 的并发

Story 4.1 已解决此问题：emitEvent 在 `proc.mu` 下读取 DebugChan，reapProcess 在 `proc.mu` 下将 DebugChan 设为 nil 再关闭。两者互斥，不会 panic。

#### reapCh 满时的降级策略

`reapCh` 缓冲 64。如果同时有超过 64 个孤儿 zombie 等待回收（极端情况），handleOrphanChildren 使用 `select...default` 降级为启动独立 goroutine 同步 reap。

#### 不要在 reapProcess 中再次调用 CloseAll

`CloseAll` 已在 Spawn 的 goroutine defer 中注册。`proc.wg.Wait()` 确保 goroutine 完成后 CloseAll 已执行。reapProcess 中**不应**调用 CloseAll。

#### NewKernel 必须初始化 Reaper 字段

修改 `NewKernel` 时，必须初始化 `reapCh`、`stopCh` 并调用 `startReaper()`。否则 finishProcess 的孤儿检测会 panic（写入 nil channel）。

#### 测试中必须调用 Shutdown

所有使用 `NewKernel` 的测试（包括现有测试），在测试结束前必须调用 `k.Shutdown()` 停止 reaper goroutine，否则 goroutine 泄漏导致 `-race` 测试失败。使用 `defer k.Shutdown()` 模式。

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

本 Story 不修改接口签名。SpawnOpts 增加字段是向后兼容的（零值 = 默认行为不变）。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR7 | 超时/错误时 5 秒内转 Zombie | 不受本 Story 影响（finishProcess 已保证） |
| NFR8 | 退出后 goroutine/context 10 秒内释放 | reapProcess 通过 wg.Wait + CtxFree 确保；auto-reaper 事件驱动，延迟 < 1 秒 |
| NFR9 | 进程表在异常退出后保持一致性 | reapOnce 确保每个进程只 reap 一次；handleOrphanChildren + finishProcess 双重覆盖 |
| NFR10 | CLI 不崩溃 | 所有路径返回 error 或 nil，channel 操作有 nil 检查和 select default |

### 范围边界

**本 Story 包含：**
- `kernel/process.go` — 添加 AddChild/RemoveChild/GetChildren 方法 + reapOnce 字段
- `kernel/kernel.go` — 修改 NewKernel（初始化 reaper）、Spawn（父子追踪）、finishProcess（孤儿检测）+ 添加 Shutdown 方法
- `kernel/reap.go` — 重构 Wait 使用 reapProcess、添加 reapProcess/handleOrphanChildren/startReaper/stopReaper
- `kernel/process_test.go` — Children 管理测试
- `kernel/kernel_test.go` — Spawn 父子追踪测试 + 现有测试添加 defer k.Shutdown()
- `kernel/reap_test.go` — 孤儿 reparent + zombie auto-reap + 并发安全测试

**本 Story 不包含：**
- `crux kill <pid>` CLI 子命令（Story 4.4）
- `/proc` 动态文件系统（Story 4.3）
- `crux ps` 命令和 Process Table UI（Story 4.4）
- 从 reasonStep 内部 spawn 子进程的实现（Phase 2 Compose）
- `ctx_free` 独立测试（Story 4.5）

### Project Structure Notes

**修改文件：**
```
kernel/process.go          — 添加 AddChild/RemoveChild/GetChildren 方法 + reapOnce 字段
kernel/kernel.go           — 修改 NewKernel（reaper 初始化）、Spawn（父子追踪）、finishProcess（孤儿检测）+ Shutdown 方法
kernel/reap.go             — 重构 Wait + 新增 reapProcess/handleOrphanChildren/startReaper/stopReaper
kernel/kernel_test.go      — Spawn 父子追踪测试 + 现有测试添加 defer k.Shutdown()
kernel/process_test.go     — Children 管理测试
kernel/reap_test.go        — 孤儿 reparent + zombie auto-reap 测试
```

**不修改文件：**
```
kernel/errors.go           — 现有错误码足够（ErrNotFound、ErrInternal）
internal/types/types.go    — 现有类型足够（PID、ProcessState、SyscallEvent）
context/context.go         — CtxFree 已实现，只调用不修改
vfs/vfs.go                 — CloseAll 已在 Spawn defer 中调用，不修改
debug/astrace.go           — 不修改
internal/ui/*              — 不修改
cmd/crux/main.go           — 不修改（暂无 CLI 集成需求）
cmd/crux/integration_test.go — 可选添加集成测试
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.2] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 2] — 进程模型与并发，资源释放顺序，PPID/Children 字段
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 1] — Syscall ABI 设计，ProcessManager 接口
- [Source: _bmad-output/planning-artifacts/architecture.md#实现模式] — 错误处理模式、状态转移规则、Channel 使用规则
- [Source: _bmad-output/planning-artifacts/prd.md#FR5] — 孤儿进程重新挂载到 init（PID=1）
- [Source: _bmad-output/planning-artifacts/prd.md#FR6] — 回收 Zombie 进程并释放资源
- [Source: _bmad-output/planning-artifacts/prd.md#NFR8] — 退出后 10 秒内释放资源
- [Source: _bmad-output/planning-artifacts/prd.md#NFR9] — 进程表在异常退出后保持一致性
- [Source: _bmad-output/project-context.md#进程状态机] — 合法转移规则
- [Source: _bmad-output/project-context.md#资源释放顺序] — cancel→wg.Wait→FD→DebugChan→CtxFree→Dead→移除
- [Source: _bmad-output/implementation-artifacts/4-1-kill-and-wait-syscalls.md] — 前序 Story 经验，Wait 实现，emitEvent 并发安全
- [Source: kernel/kernel.go:85-97] — KernelImpl 结构体定义
- [Source: kernel/kernel.go:99-106] — NewKernel 创建
- [Source: kernel/kernel.go:118] — Spawn 中 NewProcess(0, ...)，PPID 总是 0
- [Source: kernel/kernel.go:207-213] — Spawn goroutine defer CloseAll
- [Source: kernel/kernel.go:238-248] — emitEvent 并发安全实现
- [Source: kernel/kernel.go:251-266] — finishProcess 实现
- [Source: kernel/process.go:24-46] — Process 结构体，PPID 和 Children 字段
- [Source: kernel/process.go:56-81] — NewProcess，Children 初始化为空切片
- [Source: kernel/process.go:112] — GetState 方法
- [Source: kernel/process.go:117-126] — Terminate 方法
- [Source: kernel/process.go:129-131] — Reap 方法
- [Source: kernel/process.go:134-140] — Cancel 方法
- [Source: kernel/reap.go:13-59] — Wait 完整实现（将被重构）
- [Source: kernel/kernel_test.go:94-104] — newTestKernel helper
- [Source: kernel/kernel_test.go:153-158] — newSimpleKernel helper
- [Source: kernel/kernel_test.go:652-673] — blockingLLMFile mock
- [Source: context/context.go:97-109] — CtxFree 实现

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无异常。所有测试一次通过。

### Completion Notes List

- Task 1: 在 `kernel/process.go` 添加了 `AddChild`、`RemoveChild`、`GetChildren` 三个线程安全方法，以及 `reapOnce sync.Once` 字段用于后续 reap 幂等性保证。对应测试覆盖并发安全场景。
- Task 2: 在 `SpawnOpts` 添加 `ParentPID` 字段，`Spawn` 方法在 `ParentPID > 0` 时验证父进程存在并维护 `parent.AddChild(child.PID)` 关系。不存在返回 `ErrNotFound`。
- Task 3: 从 `Wait` 提取完整资源释放序列到 `reapProcess`（使用 `sync.Once` 保证幂等），同时实现 `handleOrphanChildren` 处理孤儿子进程。`Wait` 重构为调用 `reapProcess`，现有 6 个 Wait 测试全部无回归通过。
- Task 4: `handleOrphanChildren` 在 reapProcess 中 RemoveProcess 之前执行。Running 子进程 reparent 到 PID 0，Zombie 子进程推送到 `reapCh` 自动回收。Reparent 操作写入 SyscallEvent。
- Task 5: `KernelImpl` 添加 `reapCh`（缓冲 64）、`stopCh`、`reaperWg`。`NewKernel` 初始化并启动 reaper goroutine。`finishProcess` 添加孤儿检测：PPID > 0 且父进程不在表中时推送到 `reapCh`。`Shutdown` 方法停止 reaper 并排空 reapCh。
- Task 6: 综合测试覆盖完整生命周期、多子进程场景（Running+Zombie+Dead）、进程表一致性（NFR9）、资源释放时效（NFR8）、并发安全。`go test -race ./...` 和 `go vet ./...` 全部通过。

### Senior Developer Review (AI)

**Reviewer:** Amelia (Dev Agent — Code Review Mode)
**Date:** 2026-02-26
**Model:** Claude Opus 4.6

**Issues Found:** 3 High, 3 Medium, 2 Low
**Issues Fixed:** 6 (3 High + 3 Medium)
**Action Items:** 0

**Fixes Applied:**

1. **H1 — finishProcess PPID 读取竞态** (`kernel/kernel.go`): `finishProcess` 中 `proc.PPID` 读取未持锁，与 `handleOrphanChildren` 的 reparent 写入存在数据竞争。修复：在 `proc.mu` 下读取 PPID 到局部变量。
2. **H2 — 30+ 现有测试缺少 Shutdown** (`kernel/kernel_test.go`): `NewKernel` 启动 reaper goroutine，但大量 Story 1-3 遗留测试未调用 `Shutdown()` 导致 goroutine 泄漏。修复：`newTestKernel`/`newSimpleKernel` 改为接受 `testing.TB` 并注册 `t.Cleanup(k.Shutdown)`；所有直接 `NewKernel` 调用添加 `defer k.Shutdown()`。
3. **H3 — Shutdown() 可重入 panic** (`kernel/reap.go`): `close(stopCh)` 二次调用 panic。修复：`KernelImpl` 添加 `shutdownOnce sync.Once`，`Shutdown` 在 `Once.Do` 中 close。
4. **M1 — 缺少 reapOnce 并发测试** (`kernel/reap_test.go`): 新增 `TestReapOnce_ConcurrentReapProcess`，10 goroutine 并发 reapProcess 验证只执行一次清理。
5. **M3 — 缺少 Shutdown 排空测试** (`kernel/reap_test.go`): 新增 `TestShutdown_DrainsReapCh`，验证 Shutdown 时 reapCh 残留 PID 被正确处理。新增 `TestShutdown_Idempotent`，验证多次调用不 panic。

**Unfixed (Low, acceptable):**
- L1: Dev Agent Record File List 声明已更正
- L2: `blockingLLMFile.closed` 非线程安全（Story 4.1 遗留，不属本 Story 范围）

### File List

**修改文件：**
- `kernel/process.go` — 添加 AddChild/RemoveChild/GetChildren 方法 + reapOnce 字段
- `kernel/kernel.go` — 修改 KernelImpl（reaper 字段 + shutdownOnce）、NewKernel（reaper 初始化）、SpawnOpts（ParentPID）、Spawn（父子追踪）、finishProcess（孤儿检测 PPID 加锁读取）+ sync import
- `kernel/reap.go` — 重构 Wait 使用 reapProcess + 新增 reapProcess/handleOrphanChildren/startReaper/Shutdown（幂等）
- `kernel/process_test.go` — 添加 Children 管理单元测试（AddChild/RemoveChild/GetChildren/并发安全）
- `kernel/kernel_test.go` — 添加 Spawn 父子追踪测试 + newTestKernel/newSimpleKernel 接受 testing.TB 并注册 t.Cleanup(k.Shutdown) + 所有直接 NewKernel 调用添加 defer k.Shutdown()
- `kernel/reap_test.go` — 添加 defer k.Shutdown() + 孤儿 reparent 测试 + auto-reap 测试 + 综合集成测试 + reapOnce 并发测试 + Shutdown 排空测试 + Shutdown 幂等测试
