# Story 6.4: Signal 信号系统

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 智能体,
I want 通过 Signal syscall 向其他进程发送信号（中断、暂停、恢复），并通过 SigBlock/SigUnblock 控制信号处理,
So that 智能体之间可以协调执行节奏，实现暂停/恢复推理循环等精细控制。

## Acceptance Criteria

1. **发送信号** — Given `kernel/signal.go` 已实现，When 调用 `Signal(targetPID, sig)`，Then 目标进程的信号处理器被触发

2. **阻塞信号** — Given 目标进程调用 `SigBlock(sig)`，When 有该类型信号到达，Then 信号被暂存到 pending 集合

3. **解除阻塞** — Given 目标进程调用 `SigUnblock(sig)`，When pending 集合中有该类型信号，Then 立即触发信号处理器/默认行为

4. **默认行为** — Given 目标进程未注册特定信号处理器，When 收到信号，Then 执行默认行为（SIGTERM/SIGKILL/SIGINT → 终止进程，SIGPAUSE → 暂停推理循环，SIGRESUME → 恢复）

## Tasks / Subtasks

- [x] Task 1: 扩展 Signal 类型定义 (AC: #1-4)
  - [x] 1.1 在 `internal/types/types.go` 中新增 `SIGINT Signal = 3`、`SIGPAUSE Signal = 4`、`SIGRESUME Signal = 5`
  - [x] 1.2 更新 `Signal.Valid()` 方法，包含全部 5 个信号值
  - [x] 1.3 新增 `Signal.String() string` 方法，返回 "SIGTERM"/"SIGKILL"/"SIGINT"/"SIGPAUSE"/"SIGRESUME"
  - [x] 1.4 新增 `Signal.IsTermination() bool` 辅助方法 — SIGTERM/SIGKILL/SIGINT 返回 true
  - [x] 1.5 新增 `Signal.Blockable() bool` 辅助方法 — SIGKILL 返回 false（不可阻塞），其余返回 true
  - [x] 1.6 验证现有测试通过（`make test`）

- [x] Task 2: Process 新增信号状态字段 (AC: #1-4)
  - [x] 2.1 在 `kernel/signal.go` 中定义 `SignalHandler` 类型：`type SignalHandler func(types.Signal)`
  - [x] 2.2 在 `kernel/process.go` 的 Process 结构体中新增信号字段（mu 保护）：
    - `sigHandlers map[types.Signal]SignalHandler` — 已注册的信号处理器
    - `blockedSignals map[types.Signal]struct{}` — 已阻塞信号集合
    - `pendingSignals map[types.Signal]struct{}` — 待处理信号集合（每类型至多 1 个）
    - `resumeCh chan struct{}` — Pause/Resume 通道（nil=未暂停，non-nil=已暂停）
  - [x] 2.3 实现 `Process.SetHandler(sig, handler)` — 注册信号处理器
  - [x] 2.4 实现 `Process.GetHandler(sig) (SignalHandler, bool)` — 获取处理器
  - [x] 2.5 实现 `Process.BlockSignal(sig)` / `Process.UnblockSignal(sig) bool` / `Process.IsBlocked(sig) bool` — 阻塞/解除管理，UnblockSignal 同时返回是否有 pending
  - [x] 2.6 实现 `Process.AddPending(sig)` / `Process.HasPending(sig) bool` / `Process.ClearPending(sig)` — pending 信号管理
  - [x] 2.7 实现 `Process.Pause()` — 创建 `resumeCh = make(chan struct{})`
  - [x] 2.8 实现 `Process.Resume()` — 关闭 `resumeCh` 并置 nil（幂等，未暂停时为 noop）
  - [x] 2.9 实现 `Process.WaitIfPaused() <-chan struct{}` — 返回 resumeCh（nil 表示未暂停）
  - [x] 2.10 实现 `Process.IsPaused() bool`
  - [x] 2.11 实现 `Process.ClearSignalState()` — 清理所有信号状态（reapProcess 用）

- [x] Task 3: 创建 SignalManager 接口和实现 (AC: #1-3)
  - [x] 3.1 创建 `kernel/signal.go`，定义 `SignalManager` 子接口：
    ```go
    type SignalManager interface {
        Signal(pid types.PID, sig types.Signal) error
        SigBlock(pid types.PID, sig types.Signal) error
        SigUnblock(pid types.PID, sig types.Signal) error
    }
    ```
  - [x] 3.2 添加编译期检查 `var _ SignalManager = (*KernelImpl)(nil)`
  - [x] 3.3 实现 `Signal(pid, sig)` — 完整信号分发逻辑：
    1. 验证信号有效（`sig.Valid()`）
    2. 查找进程（`GetProcess`），验证活跃状态
    3. 检查信号是否被阻塞 → 加入 pending，返回
    4. 检查自定义处理器 → 调用处理器
    5. 无处理器 → 执行默认行为：
       - `IsTermination()` → `proc.Cancel()`
       - `SIGPAUSE` → `proc.Pause()`
       - `SIGRESUME` → `proc.Resume()`
    6. 发射 SyscallEvent
  - [x] 3.4 实现 `SigBlock(pid, sig)` — 验证 + `proc.BlockSignal(sig)` + SyscallEvent
  - [x] 3.5 实现 `SigUnblock(pid, sig)` — 验证 + `proc.UnblockSignal(sig)` + 若有 pending 则递归调用 `Signal` 投递 + SyscallEvent
  - [x] 3.6 所有错误包装为 `*SyscallError`
  - [x] 3.7 SIGKILL 调用 `SigBlock` 时返回 `ErrInvalid`（SIGKILL 不可阻塞）

- [x] Task 4: 重构 Kill 使用 Signal 分发 (AC: #1, #4)
  - [x] 4.1 在 `kernel/kernel.go` 中重构 `Kill(pid, signal)` — 复用 `Signal()` 进行实际分发
  - [x] 4.2 保留 Kill 的现有 SyscallEvent 发射模式（"Kill" 事件名不变）
  - [x] 4.3 Kill 对 Zombie/Dead 进程保持幂等（noop）行为不变
  - [x] 4.4 确保所有现有 Kill 测试通过
  - [x] 4.5 验证 `SignalGroup`（Story 6.3，内部调用 Kill）仍正确工作

- [x] Task 5: reasonStep 集成 Pause/Resume (AC: #4)
  - [x] 5.1 在 `kernel/kernel.go` 的 reasonStep 循环中，每步开始前检查 `proc.WaitIfPaused()`
  - [x] 5.2 若 resumeCh 非 nil，select 等待 `<-resumeCh` 或 `<-proc.ctx.Done()`
  - [x] 5.3 暂停/恢复时发射 ReasonStep SyscallEvent（action: "paused" / "resumed"）
  - [x] 5.4 确保 ctx 取消优先于暂停等待（Kill 可终止已暂停进程）

- [x] Task 6: reapProcess 集成信号状态清理 (AC: #1-4)
  - [x] 6.1 在 `kernel/reap.go` 的 reapProcess 中，removeFromAllGroups 之后、CtxFree 之前，添加信号状态清理
  - [x] 6.2 调用 `proc.Resume()` 释放可能阻塞在 WaitIfPaused 的 goroutine
  - [x] 6.3 调用 `proc.ClearSignalState()` 清理 handlers/blocked/pending
  - [x] 6.4 确保清理在进程表移除之前执行

- [x] Task 7: 单元测试 (AC: #1-4)
  - [x] 7.1 `kernel/signal_test.go` — TestSignal_Basic：发送 SIGTERM，验证 context 被取消
  - [x] 7.2 TestSignal_SIGINT：发送 SIGINT，验证 context 被取消
  - [x] 7.3 TestSignal_InvalidSignal：无效信号返回 ErrInvalid
  - [x] 7.4 TestSignal_ProcessNotFound：不存在 PID 返回 ErrNotFound
  - [x] 7.5 TestSignal_ZombieProcess：对 Zombie/Dead 进程发信号返回 ErrNotFound
  - [x] 7.6 TestSignal_SIGPAUSE：发送 SIGPAUSE，验证 `IsPaused()` 返回 true
  - [x] 7.7 TestSignal_SIGRESUME：发送 SIGRESUME 到已暂停进程，验证恢复
  - [x] 7.8 TestSignal_ResumeNotPaused：对未暂停进程发 SIGRESUME 为 noop
  - [x] 7.9 TestSigBlock_Basic：阻塞 SIGTERM 后发送，信号进入 pending 不触发
  - [x] 7.10 TestSigBlock_SIGKILL_Rejected：阻塞 SIGKILL 返回 ErrInvalid
  - [x] 7.11 TestSigUnblock_TriggersPending：解除阻塞后 pending 信号立即投递
  - [x] 7.12 TestSigUnblock_NoPending：无 pending 时解除阻塞为 noop
  - [x] 7.13 TestSignalHandler_Custom：注册自定义 handler，验证 handler 被调用而非默认行为
  - [x] 7.14 TestSignalHandler_Override：handler 覆盖默认终止行为（进程不被 cancel）
  - [x] 7.15 TestKill_DelegatesToSignal：Kill 仍正常工作（向后兼容）
  - [x] 7.16 TestKill_WithSIGPAUSE：Kill(pid, SIGPAUSE) 暂停进程
  - [x] 7.17 TestSignalGroup_WithNewSignals：SignalGroup + SIGPAUSE 暂停组内所有成员
  - [x] 7.18 TestReapProcess_CleanupSignalState：进程回收时信号状态正确清理
  - [x] 7.19 TestReapProcess_ResumeBeforeCleanup：暂停进程被 reap 时先 resume 再清理
  - [x] 7.20 TestSignal_Concurrent：100 goroutine 并发 Signal/SigBlock/SigUnblock，无 race
  - [x] 7.21 TestSignal_SyscallEvent：验证 DebugChan 收到 Signal/SigBlock/SigUnblock 事件
  - [x] 7.22 TestSignal_PauseResumeIntegration：暂停进程 → 验证 WaitIfPaused 阻塞 → 恢复 → 验证继续

- [x] Task 8: 集成验证 (AC: #1-4)
  - [x] 8.1 `make test` 全部通过（含 `-race`）
  - [x] 8.2 `make lint` 通过
  - [x] 8.3 `make build` 编译成功
  - [x] 8.4 验证 Story 6.1/6.2/6.3 所有现有测试无回归

## Dev Notes

### 核心设计决策

**SignalManager 作为独立子接口**：遵循架构决策 Decision 1（分类接口组合），新增 `SignalManager` 子接口嵌入 Kernel。信号系统是独立的控制域——处理信号分发、阻塞/解除、自定义处理器，与 ProcessManager（Kill/Wait/Spawn）和 IPCManager（Send/Recv/Pipe）正交。

**Signal 与 Kill 的关系**：`Signal(pid, sig)` 是通用信号分发方法，支持完整的 handler/blocking 机制。`Kill(pid, signal)` 重构为调用 `Signal` 进行实际分发，保留自身的 SyscallEvent 发射（"Kill" 事件名不变，向后兼容）。`SignalGroup`（Story 6.3）内部调用 `Kill`，自动获得新信号支持。

**信号不可阻塞规则**：SIGKILL 不可阻塞、不可自定义处理器（类似 Unix SIGKILL 语义），确保 force kill 始终生效。其余信号（SIGTERM/SIGINT/SIGPAUSE/SIGRESUME）可阻塞、可注册自定义 handler。

**Pending 集合语义**：使用 `map[Signal]struct{}` 而非 slice，每种信号类型至多 1 个 pending（标准信号不排队，类似 Unix standard signals）。

**Pause/Resume 机制**：基于 channel 的阻塞等待，与 Go context 集成。`resumeCh` 为 nil 表示未暂停；非 nil 时 reasonStep 通过 select 等待 resume 或 ctx 取消。避免 `sync.Cond`（不与 context 兼容）。

**内核内部实现**：与 Story 6.1/6.2/6.3 一致，信号系统是**内核内部**概念，实现在 `kernel/signal.go`，不修改 `ipc/` 包。

### SignalManager 接口定义

```go
// kernel/signal.go

// SignalHandler is a custom signal handler function.
type SignalHandler func(types.Signal)

// SignalManager manages signal delivery, blocking, and handling.
type SignalManager interface {
    Signal(pid types.PID, sig types.Signal) error
    SigBlock(pid types.PID, sig types.Signal) error
    SigUnblock(pid types.PID, sig types.Signal) error
}
```

### Signal 类型扩展

```go
// internal/types/types.go

const (
    SIGTERM   Signal = iota + 1 // 1 — 终止（可阻塞、可处理）
    SIGKILL                      // 2 — 强制终止（不可阻塞、不可处理）
    SIGINT                       // 3 — 中断（可阻塞、可处理）
    SIGPAUSE                     // 4 — 暂停推理循环（可阻塞、可处理）
    SIGRESUME                    // 5 — 恢复推理循环（可阻塞、可处理）
)

func (s Signal) Valid() bool {
    return s >= SIGTERM && s <= SIGRESUME
}

func (s Signal) String() string {
    switch s {
    case SIGTERM:
        return "SIGTERM"
    case SIGKILL:
        return "SIGKILL"
    case SIGINT:
        return "SIGINT"
    case SIGPAUSE:
        return "SIGPAUSE"
    case SIGRESUME:
        return "SIGRESUME"
    default:
        return fmt.Sprintf("Signal(%d)", s)
    }
}

// IsTermination reports whether the signal terminates the process by default.
func (s Signal) IsTermination() bool {
    return s == SIGTERM || s == SIGKILL || s == SIGINT
}

// Blockable reports whether the signal can be blocked. SIGKILL cannot be blocked.
func (s Signal) Blockable() bool {
    return s != SIGKILL
}
```

### Process 信号状态扩展

```go
// kernel/process.go — Process 新增字段（mu 保护）
type Process struct {
    // ... 现有字段 ...

    // Signal system (mu protected)
    sigHandlers    map[types.Signal]SignalHandler
    blockedSignals map[types.Signal]struct{}
    pendingSignals map[types.Signal]struct{}
    resumeCh       chan struct{} // nil=not paused; non-nil=paused, close to resume
}
```

**Pause/Resume 实现要点：**

```go
func (p *Process) Pause() {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.resumeCh == nil {
        p.resumeCh = make(chan struct{})
    }
}

func (p *Process) Resume() {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.resumeCh != nil {
        close(p.resumeCh)
        p.resumeCh = nil
    }
}

// WaitIfPaused returns a channel that blocks until Resume is called.
// Returns nil if not paused (caller should skip select).
func (p *Process) WaitIfPaused() <-chan struct{} {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.resumeCh
}

func (p *Process) IsPaused() bool {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.resumeCh != nil
}

func (p *Process) ClearSignalState() {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.sigHandlers = nil
    p.blockedSignals = nil
    p.pendingSignals = nil
    if p.resumeCh != nil {
        close(p.resumeCh)
        p.resumeCh = nil
    }
}
```

### Signal 分发实现要点

```go
func (k *KernelImpl) Signal(pid types.PID, sig types.Signal) error {
    start := time.Now()

    // 1. 验证信号有效
    if !sig.Valid() {
        return NewSyscallError("Signal", pid, "",
            fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
    }

    // 2. 查找进程
    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("Signal", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }

    // 3. 检查进程状态
    state := proc.GetState()
    if state == types.StateZombie || state == types.StateDead {
        return NewSyscallError("Signal", pid, "",
            fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
    }

    // 4. 检查信号是否被阻塞（SIGKILL 永远不会被阻塞）
    if proc.IsBlocked(sig) {
        proc.AddPending(sig)
        k.emitEvent(proc, "Signal", map[string]any{
            "pid":    pid,
            "signal": sig.String(),
            "action": "blocked_pending",
        }, nil, nil, time.Since(start))
        return nil
    }

    // 5. 检查自定义处理器
    if handler, ok := proc.GetHandler(sig); ok {
        handler(sig)
        k.emitEvent(proc, "Signal", map[string]any{
            "pid":    pid,
            "signal": sig.String(),
            "action": "handler",
        }, nil, nil, time.Since(start))
        return nil
    }

    // 6. 默认行为
    action := k.defaultSignalAction(proc, sig)
    k.emitEvent(proc, "Signal", map[string]any{
        "pid":    pid,
        "signal": sig.String(),
        "action": action,
    }, nil, nil, time.Since(start))

    return nil
}

// defaultSignalAction executes the default behavior for a signal.
func (k *KernelImpl) defaultSignalAction(proc *Process, sig types.Signal) string {
    switch {
    case sig.IsTermination():
        proc.Cancel()
        return "terminated"
    case sig == types.SIGPAUSE:
        proc.Pause()
        return "paused"
    case sig == types.SIGRESUME:
        proc.Resume()
        return "resumed"
    default:
        return "ignored"
    }
}
```

### SigBlock/SigUnblock 实现要点

```go
func (k *KernelImpl) SigBlock(pid types.PID, sig types.Signal) error {
    start := time.Now()

    if !sig.Valid() {
        return NewSyscallError("SigBlock", pid, "",
            fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
    }

    // SIGKILL 不可阻塞
    if !sig.Blockable() {
        return NewSyscallError("SigBlock", pid, "",
            fmt.Errorf("signal %s cannot be blocked", sig), types.ErrInvalid)
    }

    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("SigBlock", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }

    proc.BlockSignal(sig)

    k.emitEvent(proc, "SigBlock", map[string]any{
        "pid":    pid,
        "signal": sig.String(),
    }, nil, nil, time.Since(start))

    return nil
}

func (k *KernelImpl) SigUnblock(pid types.PID, sig types.Signal) error {
    start := time.Now()

    if !sig.Valid() {
        return NewSyscallError("SigUnblock", pid, "",
            fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
    }

    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("SigUnblock", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }

    // 解除阻塞并检查 pending
    hasPending := proc.UnblockSignal(sig)

    k.emitEvent(proc, "SigUnblock", map[string]any{
        "pid":        pid,
        "signal":     sig.String(),
        "had_pending": hasPending,
    }, nil, nil, time.Since(start))

    // 若有 pending 信号，立即投递
    if hasPending {
        proc.ClearPending(sig)
        return k.Signal(pid, sig)
    }

    return nil
}
```

### Kill 重构要点

```go
// kernel/kernel.go — Kill 重构
func (k *KernelImpl) Kill(pid types.PID, signal types.Signal) error {
    start := time.Now()

    if !signal.Valid() {
        return NewSyscallError("Kill", pid, "",
            fmt.Errorf("invalid signal %d", signal), types.ErrInvalid)
    }

    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("Kill", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }

    // Kill 入口事件
    k.emitEvent(proc, "Kill", map[string]any{
        "pid":    pid,
        "signal": signal.String(),
    }, nil, nil, 0)

    // Zombie/Dead 幂等
    state := proc.GetState()
    if state == types.StateZombie || state == types.StateDead {
        k.emitEvent(proc, "Kill", map[string]any{
            "pid":    pid,
            "signal": signal.String(),
            "action": "noop",
        }, nil, nil, time.Since(start))
        return nil
    }

    // 委托给 Signal 进行实际分发（handler/blocking/default 逻辑）
    // 注意：Signal 会发射自己的 "Signal" 事件，Kill 额外发射 "Kill" 事件
    err := k.Signal(pid, signal)

    k.emitEvent(proc, "Kill", map[string]any{
        "pid":    pid,
        "signal": signal.String(),
        "action": "dispatched",
    }, nil, err, time.Since(start))

    return err
}
```

**重要**：Kill 和 Signal 会各自发射 SyscallEvent。Kill 的事件名保持 "Kill" 不变，确保向后兼容。开发者可考虑在 Kill 委托 Signal 时跳过 Signal 的事件发射，或接受双重事件（取决于 astrace 的可读性偏好）。推荐方案：Kill 保持现有事件格式，内部调用一个 `deliverSignal` 私有方法避免双重事件。

**优化方案（推荐）：**
```go
// 提取私有分发方法，Kill 和 Signal 共用
func (k *KernelImpl) deliverSignal(proc *Process, sig types.Signal) string {
    // 检查阻塞 → pending
    // 检查 handler → 调用
    // 默认行为
    // 返回 action 字符串
}
```

### reasonStep Pause 集成

```go
// kernel/kernel.go — reasonStep 循环中新增暂停检查
for step := 1; step <= maxSteps; step++ {
    stepStart := time.Now()

    // 🆕 检查暂停状态（Story 6.4）
    if ch := proc.WaitIfPaused(); ch != nil {
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "paused",
        }, nil, nil, 0)

        select {
        case <-ch:
            // 已恢复，继续执行
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":   step,
                "action": "resumed",
            }, nil, nil, time.Since(stepStart))
        case <-proc.ctx.Done():
            // 暂停期间被取消（Kill 可终止已暂停进程）
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":   step,
                "action": "cancelled_while_paused",
            }, nil, proc.ctx.Err(), time.Since(stepStart))
            k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while paused"})
            return
        }
    }

    // 现有的 context 取消检查
    select {
    case <-proc.ctx.Done():
        // ... 现有逻辑 ...
    default:
    }

    // ... 正常 reasonStep 逻辑 ...
}
```

### reapProcess 集成

```go
// kernel/reap.go — reapProcess 资源释放序列新增信号清理

func (k *KernelImpl) reapProcess(proc *Process) {
    proc.reapOnce.Do(func() {
        // 1. 处理孤儿子进程
        k.handleOrphanChildren(proc)

        // 2. cancel() — 取消 context
        proc.Cancel()

        // 3. wg.Wait() — 等待 goroutine
        proc.wg.Wait()

        // 4. 关闭 DebugChan
        // ... 现有逻辑 ...

        // 5. 关闭 msgQueue（Story 6.1）
        // ... 现有逻辑 ...

        // 6. 进程组清理（Story 6.3）
        k.removeFromAllGroups(proc.PID, proc)

        // 🆕 7. 信号状态清理（Story 6.4）
        proc.ClearSignalState()

        // 8. CtxFree
        _ = k.ctxMgr.CtxFree(proc.CtxID)

        // 9. Reap (Zombie → Dead)
        _ = proc.Reap()

        // 10. 移除进程表
        k.RemoveProcess(proc.PID)
    })
}
```

**资源释放顺序（完整，含 Story 6.4 新增步骤）：**
```
handleOrphanChildren → cancel() → wg.Wait() → 关闭 DebugChan → msgQueue.close()
→ removeFromAllGroups → 🆕 ClearSignalState → CtxFree → Reap → Remove
```

**注意**：`ClearSignalState()` 中的 `Resume()` 调用（关闭 resumeCh）必须在 `wg.Wait()` 之后执行，因为此时 reasonStep goroutine 已退出，不会有 goroutine 阻塞在 WaitIfPaused 上。但为安全起见，ClearSignalState 仍然关闭 resumeCh。

### 错误码使用

| 场景 | Syscall | ErrCode | 说明 |
|------|---------|---------|------|
| PID 不存在 | Signal | ErrNotFound | procTable 中找不到 |
| PID Zombie/Dead | Signal | ErrNotFound | 进程已终止 |
| 无效信号 | Signal | ErrInvalid | sig.Valid() 返回 false |
| 无效信号 | SigBlock | ErrInvalid | sig.Valid() 返回 false |
| SIGKILL 阻塞 | SigBlock | ErrInvalid | sig.Blockable() 返回 false |
| PID 不存在 | SigBlock | ErrNotFound | procTable 中找不到 |
| 无效信号 | SigUnblock | ErrInvalid | sig.Valid() 返回 false |
| PID 不存在 | SigUnblock | ErrNotFound | procTable 中找不到 |

### SyscallEvent 记录规范

| Syscall | Args | 说明 |
|---------|------|------|
| Signal | `pid`, `signal`, `action` | action: "blocked_pending" / "handler" / "terminated" / "paused" / "resumed" / "ignored" |
| SigBlock | `pid`, `signal` | 阻塞信号 |
| SigUnblock | `pid`, `signal`, `had_pending` | 解除阻塞 |
| Kill | `pid`, `signal`, `action` | 保持现有格式（"noop" / "dispatched"） |
| ReasonStep | `step`, `action` | 新增 "paused" / "resumed" / "cancelled_while_paused" |

### 并发安全要点

1. **Process 信号字段**：复用现有 `Process.mu` 保护所有信号字段（sigHandlers、blockedSignals、pendingSignals、resumeCh），与 PID/State/groups 等字段共享锁
2. **Pause/Resume channel**：`resumeCh` 创建和关闭都在 mu 锁内完成；`WaitIfPaused()` 获取 channel 引用后释放锁，select 在锁外等待（避免长时间持锁）
3. **Signal 分发无锁调用**：`deliverSignal` 在获取 handler/blocked 状态后释放锁，再调用 handler 或默认行为（避免持锁调用 proc.Cancel() 导致死锁）
4. **SigUnblock 递归调用**：SigUnblock 中先 ClearPending 再调用 Signal，Signal 内部重新获取锁检查状态，无死锁风险（Go mutex 非递归，但这里是先释放再重新获取）
5. **reapProcess 安全**：ClearSignalState 在 wg.Wait() 之后调用，此时 reasonStep goroutine 已退出，不存在竞态

### NFR 合规

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR24 | ≥10 并发进程表操作延迟 ≤ 2x | Signal 操作纯内存，微秒级；Process.mu 细粒度锁，不同进程的信号操作完全并行 |
| NFR19 | 不破坏 Phase 1 ABI | SignalManager 作为新子接口嵌入 Kernel；Kill 签名不变；现有 ProcessManager 不变 |
| NFR8 | 进程退出后资源释放无泄漏 | ClearSignalState 清理所有信号资源；Resume 关闭 channel 防止 goroutine 泄漏 |

### 反模式警告

- **禁止在 `ipc/` 包中实现**：信号系统是内核内部概念，实现在 `kernel/signal.go`
- **禁止使用 sync.Cond 实现 Pause/Resume**：sync.Cond 不与 context.Context 兼容，无法在暂停时响应取消
- **禁止持锁调用 proc.Cancel() 或 handler**：获取 handler/状态后释放锁，再执行动作，避免死锁
- **禁止 SIGKILL 可阻塞**：SIGKILL 必须始终立即生效（force kill 语义）
- **禁止返回裸 error**：所有 signal 方法错误必须包装为 `*SyscallError`
- **禁止使用 `sync.Map` 替代 Process.mu**：信号字段与现有字段共享 mu，不引入额外锁
- **禁止 pending 信号排队**：使用 `map[Signal]struct{}` 集合，每类型至多 1 个 pending（标准信号语义）
- **禁止在 Pause 等待中使用忙等待或 sleep**：必须使用 channel select，零 CPU 消耗等待
- **禁止跳过 reasonStep 暂停检查**：暂停检查必须在每步开始前执行，不能遗漏

### 与 Story 6.1/6.2/6.3 的关系

Story 6.1 确立了 IPC 基础模式（子接口扩展、SyscallEvent 记录、进程验证、reapProcess 集成）。Story 6.2 确立了 VFS FD 集成模式。Story 6.3 确立了进程组管理和批量信号模式。Story 6.4 沿用这些模式：
- 复用 `emitEvent` 记录 SyscallEvent
- 复用进程验证逻辑（GetProcess + 状态检查）
- 复用 reapProcess 资源释放扩展模式（在 removeFromAllGroups 后添加新清理步骤）
- 复用 `*SyscallError` 错误包装
- 不修改 Story 6.1 已有的 Send/Recv/MessageQueue 实现
- 不修改 Story 6.2 已有的 Pipe/pipeBuffer 实现
- **Story 6.3 的 `SignalGroup` 内部调用 `Kill`，Kill 重构后自动支持新信号类型**

### 从 Story 6.3 的学习

1. **子接口模式稳定**：IPCManager → ProcGroupManager → SignalManager，模式一致可复用
2. **编译期检查必须**：`var _ SignalManager = (*KernelImpl)(nil)` 防止接口不匹配
3. **reapProcess 扩展顺序**：严格在上一个清理步骤之后、CtxFree 之前插入新步骤
4. **SyscallEvent 发射**：使用 `findGroupEventSource` 的经验表明，事件发射需要有效的 Process 引用
5. **测试辅助函数**：复用 `newIPCTestProcess` 和 `newSimpleKernel`，在 signal_test.go 中按需创建新辅助

### 测试辅助函数

复用现有 `kernel/ipc_test.go` 中的 `newIPCTestProcess` 和 `kernel/kernel_test.go` 中的 `newSimpleKernel` 辅助函数。`newIPCTestProcess` 创建 Running 状态的轻量测试进程（带 ctx/cancel），满足 signal 测试需求。

新增辅助函数（如需要）：
```go
// kernel/signal_test.go

// newSignalTestProcess 创建带 DebugChan 的测试进程（用于验证事件）
func newSignalTestProcess(t *testing.T, k *KernelImpl) *Process {
    t.Helper()
    proc := newIPCTestProcess(t, k)
    proc.mu.Lock()
    proc.DebugChan = make(chan types.SyscallEvent, 256)
    proc.mu.Unlock()
    return proc
}
```

### Project Structure Notes

**新增文件：**
```
kernel/signal.go       — SignalManager 接口 + SignalHandler 类型 + Signal/SigBlock/SigUnblock 实现 + deliverSignal 私有方法
kernel/signal_test.go  — 信号系统单元测试（22 个测试用例）
```

**修改文件：**
```
internal/types/types.go   — 新增 SIGINT/SIGPAUSE/SIGRESUME 常量 + String()/IsTermination()/Blockable() 方法
kernel/process.go         — Process 新增 sigHandlers/blockedSignals/pendingSignals/resumeCh 字段 + 信号状态方法
kernel/kernel.go          — Kill 重构使用 Signal 分发 + reasonStep 新增暂停检查
kernel/reap.go            — reapProcess 新增信号状态清理步骤（步骤 7）
```

**不修改的文件：**
```
kernel/ipc.go             — IPCManager 不变
kernel/procgroup.go       — ProcGroupManager 不变（SignalGroup 通过 Kill 自动获得新信号支持）
kernel/errors.go          — 无需新 ErrCode（复用 ErrNotFound、ErrInvalid）
vfs/                      — VFS 层不涉及信号系统
ipc/                      — 跨终端 IPC daemon，与内核信号系统无关
cmd/rnix/main.go          — CLI 层暂不暴露信号命令（由 AgentShell/Compose 使用）
```

### 必需导入

```go
// kernel/signal.go
import (
    "fmt"
    "time"

    "github.com/rnixai/rnix/internal/types"
)

// kernel/signal_test.go
import (
    "sync"
    "testing"
    "time"

    "github.com/rnixai/rnix/internal/types"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-6-ipc-跨进程通信inter-process-communication.md#Story 6.4] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR45] — 信号 syscall 需求
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR24] — ≥10 并发进程表操作 ≤ 2x 延迟
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR19] — Phase 2 扩展向后兼容
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 1] — Syscall ABI 分类接口组合设计
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 2] — 进程模型与并发
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md] — 命名模式、错误处理模式、泛型使用模式
- [Source: _bmad-output/implementation-artifacts/6-3-process-group-and-batch-signal.md] — Story 6.3 实现细节（子接口模式、reapProcess 集成、SignalGroup 实现）
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文
- [Source: kernel/kernel.go] — KernelImpl、Kill 当前实现（626-667 行）、emitEvent（269-283 行）、reasonStep 循环（335-354 行）
- [Source: kernel/process.go] — Process 结构体（32-56 行）、Cancel()（143-150 行）、状态机方法
- [Source: kernel/reap.go] — reapProcess 资源释放序列（10-51 行）
- [Source: kernel/signal.go] — 新文件（Story 6.4 创建）
- [Source: kernel/procgroup.go] — ProcGroupManager 接口（11-20 行）、SignalGroup 实现（156-202 行）
- [Source: kernel/ipc.go] — IPCManager 接口（24-32 行）
- [Source: kernel/errors.go] — SyscallError 定义和 NewSyscallError 工厂
- [Source: internal/types/types.go] — Signal 类型（36-47 行）、ErrCode 常量
- [Source: kernel/ipc_test.go] — newIPCTestProcess 助手（15-27 行）
- [Source: kernel/kernel_test.go] — newSimpleKernel 助手（155-162 行）

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Kill SyscallEvent 的 `signal` 字段从 `types.Signal`(int) 改为 `signal.String()`(string)，更新了 `kernel_test.go:TestKill_SyscallEvent` 断言以匹配新格式

### Completion Notes List

- Task 1: 在 `internal/types/types.go` 中新增 SIGINT/SIGPAUSE/SIGRESUME 信号常量，以及 String()、IsTermination()、Blockable() 辅助方法。使用 iota 保持信号值连续性，Valid() 改为范围检查。
- Task 2: 在 `kernel/process.go` 中添加 sigHandlers/blockedSignals/pendingSignals/resumeCh 字段（mu 保护），实现完整的信号状态管理方法（SetHandler/GetHandler/BlockSignal/UnblockSignal/IsBlocked/AddPending/HasPending/ClearPending/Pause/Resume/WaitIfPaused/IsPaused/ClearSignalState）。
- Task 3: 创建 `kernel/signal.go`，定义 SignalManager 接口和 SignalHandler 类型，实现 Signal/SigBlock/SigUnblock。提取 deliverSignal 私有方法供 Kill 和 Signal 共用，避免双重事件发射。
- Task 4: 重构 Kill 使用 deliverSignal 进行实际分发，保留 Kill 独立的 SyscallEvent（事件名 "Kill" 不变）。Kill 的 signal 参数在事件中改为 String() 格式以与 Signal 事件保持一致。
- Task 5: 在 reasonStep 循环中每步开始前检查 WaitIfPaused()，通过 select 等待 resumeCh 或 ctx.Done()，确保 Kill 可终止已暂停进程。
- Task 6: 在 reapProcess 中 removeFromAllGroups 后添加 Resume() + ClearSignalState() 调用，更新步骤编号。
- Task 7: 创建 `kernel/signal_test.go` 包含 22 个测试用例，覆盖信号发送、阻塞/解除、自定义处理器、Pause/Resume、并发安全、事件记录等场景。
- Task 8: 全部测试通过（含 -race）、lint 0 issues、编译成功、Story 6.1/6.2/6.3 无回归。

### File List

- `internal/types/types.go` — 修改：新增 SIGINT/SIGPAUSE/SIGRESUME 常量 + String()/IsTermination()/Blockable() 方法
- `internal/types/types_test.go` — 新增：Signal/ProcessState 方法单元测试（Code Review 修复 M3）
- `kernel/signal.go` — 新增：SignalManager 接口 + SignalHandler 类型 + Signal/SigBlock/SigUnblock/deliverSignal/defaultSignalAction 实现；Code Review 修复：deliverSignal 使用 resolveSignalDisposition 原子化分发（H1+M1），SigBlock/SigUnblock 添加进程状态检查（H2）
- `kernel/signal_test.go` — 新增：25 个信号系统单元测试（含 3 个 Code Review 新增用例：SIGKILL handler 忽略、SigBlock/SigUnblock Zombie 状态检查）
- `kernel/process.go` — 修改：Process 新增 sigHandlers/blockedSignals/pendingSignals/resumeCh 字段 + 信号状态方法 + resolveSignalDisposition 原子分发方法
- `kernel/kernel.go` — 修改：Kill 重构使用 deliverSignal + reasonStep 新增暂停检查
- `kernel/kernel_test.go` — 修改：TestKill_SyscallEvent 断言更新（signal 字段 int→string）
- `kernel/reap.go` — 修改：reapProcess 新增 ClearSignalState() 清理步骤（移除冗余 Resume 调用，Code Review 修复 M2）
