# Story 10.4: Supervisor 树与重启策略

Status: ready-for-dev

## Story

As a 平台构建者,
I want 系统提供 Supervisor 树管理子智能体，自动重启异常退出的子进程,
So that 多智能体系统具备容错能力。

## Acceptance Criteria

1. **AC1: Supervisor 进程创建与子进程监控**
   - Given `kernel/supervisor.go` 已实现
   - When 调用 `SpawnSupervisor(spec)` 创建 Supervisor 进程
   - Then Supervisor 按 ChildSpec 顺序启动所有子智能体
   - And Supervisor 持续监控子智能体的 Done channel
   - And Supervisor 本身是一个 kernel Process（拥有 PID、PPID、State）

2. **AC2: 异常退出自动重启**
   - Given 子智能体异常退出（ExitStatus.Code != 0）
   - When Supervisor 检测到
   - Then Supervisor 在 5 秒内按配置的策略自动重启（FR63）
   - And 新子进程的 ParentPID = Supervisor PID
   - And emitEvent 记录 "SupervisorRestart" 事件

3. **AC3: one_for_one 重启策略**
   - Given 重启策略为 `one_for_one`
   - When 子进程 B 崩溃
   - Then 仅重启 B
   - And 其他子进程不受影响

4. **AC4: one_for_all 重启策略**
   - Given 重启策略为 `one_for_all`
   - When 子进程 B 崩溃
   - Then 按逆启动顺序停止所有存活子进程
   - And 按原始启动顺序重启所有子进程

5. **AC5: rest_for_one 重启策略**
   - Given 重启策略为 `rest_for_one`
   - When 子进程 B 崩溃（B 是第 2 个启动的）
   - Then 按逆启动顺序停止 B 之后的所有子进程
   - And 按原始顺序重启 B 及其之后的所有子进程（FR64）

6. **AC6: 重启风暴保护**
   - Given 子进程短时间内反复崩溃
   - When 滑动窗口内重启次数超过 MaxRestarts 阈值
   - Then Supervisor 自身退出，ExitStatus{Code: 1, Reason: "max_restarts_exceeded"}
   - And emitEvent 记录 "SupervisorShutdown" 事件

7. **AC7: ChildRestart 模式**
   - Given ChildSpec.Restart = "permanent"
   - When 子进程退出（无论正常/异常）
   - Then 始终重启
   - Given ChildSpec.Restart = "transient"
   - When 子进程正常退出（Code = 0）
   - Then 不重启
   - Given ChildSpec.Restart = "temporary"
   - When 子进程退出（无论正常/异常）
   - Then 永不重启

8. **AC8: Supervisor 取消/Kill 清理**
   - Given Supervisor 收到 Kill 信号或 context 取消
   - When Supervisor 检测到自身取消
   - Then 按逆启动顺序停止所有存活子进程
   - And Supervisor 自身转 Zombie
   - And 不触发任何重启逻辑

## Tasks / Subtasks

- [ ] Task 1: Supervisor 核心类型定义 (AC: #1)
  - [ ] 1.1 `kernel/supervisor.go`：定义 `RestartStrategy` 类型和常量 `OneForOne`/`OneForAll`/`RestForOne`
  - [ ] 1.2 `kernel/supervisor.go`：定义 `ChildRestart` 类型和常量 `RestartPermanent`/`RestartTransient`/`RestartTemporary`
  - [ ] 1.3 `kernel/supervisor.go`：定义 `ChildSpec` 结构体（Name, Intent, Agent, Model, ContextBudget, Restart）
  - [ ] 1.4 `kernel/supervisor.go`：定义 `SupervisorSpec` 结构体（Strategy, MaxRestarts, MaxWindow, Children）
  - [ ] 1.5 `kernel/supervisor.go`：定义 `Supervisor` 内部结构体（proc, spec, kernel, children, exitCh, restartTimes, mu）
  - [ ] 1.6 `kernel/supervisor.go`：定义 `supervisedChild` 内部结构体（spec, pid, index, alive）

- [ ] Task 2: SpawnSupervisor 内核方法 (AC: #1)
  - [ ] 2.1 `kernel/kernel.go`：添加 `SupervisorManager` 接口 `SpawnSupervisor(spec SupervisorSpec) (types.PID, error)`
  - [ ] 2.2 `kernel/kernel.go`：添加编译时接口检查 `var _ SupervisorManager = (*KernelImpl)(nil)`
  - [ ] 2.3 `kernel/supervisor.go`：实现 `SpawnSupervisor` 方法——创建 Process、设置 ctx/cancel、注册到进程表、启动 supervisor goroutine
  - [ ] 2.4 `kernel/supervisor.go`：`newSupervisor()` 构造函数

- [ ] Task 3: Supervisor 启动与监控循环 (AC: #1, #2)
  - [ ] 3.1 `kernel/supervisor.go`：`run()` 主方法——Phase 1 按序启动所有子进程，Phase 2 进入监控循环
  - [ ] 3.2 `kernel/supervisor.go`：`startChild(idx)` 启动单个子进程——调用 `k.Spawn()`，启动 monitor goroutine 监控 Done channel 并发送到聚合 exitCh
  - [ ] 3.3 `kernel/supervisor.go`：`stopChild(idx)` 停止单个子进程——`k.Kill(pid, SIGKILL)` + 等待 Done + `k.Reap(pid)`，超时 5 秒
  - [ ] 3.4 `kernel/supervisor.go`：`shutdownAll()` 按逆启动顺序停止所有存活子进程
  - [ ] 3.5 `kernel/supervisor.go`：主循环 select 监听 `exitCh` 和 `s.proc.ctx.Done()`

- [ ] Task 4: 重启策略实现 (AC: #3, #4, #5, #7)
  - [ ] 4.1 `kernel/supervisor.go`：`handleChildExit(idx, exit)` 统一入口——判断 shouldRestart → 检查频率限制 → 分发策略
  - [ ] 4.2 `kernel/supervisor.go`：`shouldRestart(spec, exit)` 根据 ChildRestart 模式和 ExitStatus 判断
  - [ ] 4.3 `kernel/supervisor.go`：`restartOneForOne(idx)` 仅重启崩溃子进程
  - [ ] 4.4 `kernel/supervisor.go`：`restartOneForAll()` 逆序停止全部 → 顺序重启全部
  - [ ] 4.5 `kernel/supervisor.go`：`restartRestForOne(idx)` 逆序停止 idx 及之后 → 顺序重启 idx 及之后

- [ ] Task 5: 重启频率保护 (AC: #6)
  - [ ] 5.1 `kernel/supervisor.go`：`recordRestart()` 记录重启时间到 restartTimes 切片
  - [ ] 5.2 `kernel/supervisor.go`：`exceedsRestartLimit()` 滑动窗口内计数，超过 MaxRestarts 返回 true
  - [ ] 5.3 `kernel/supervisor.go`：超限时调用 `shutdownAll()` + `finishProcess(proc, ExitStatus{Code:1, Reason:"max_restarts_exceeded"})` + emitEvent "SupervisorShutdown"

- [ ] Task 6: 事件记录与可观测性 (AC: #2, #6)
  - [ ] 6.1 `kernel/supervisor.go`：每次子进程启动 emitEvent "SupervisorStartChild"
  - [ ] 6.2 `kernel/supervisor.go`：每次子进程退出 emitEvent "SupervisorChildExit"
  - [ ] 6.3 `kernel/supervisor.go`：每次重启 emitEvent "SupervisorRestart"（含策略、子进程名、重启计数）
  - [ ] 6.4 `kernel/supervisor.go`：Supervisor 自身退出 emitEvent "SupervisorShutdown"（含原因）

- [ ] Task 7: 测试 (AC: all)
  - [ ] 7.1 `kernel/supervisor_test.go`：one_for_one 策略——子进程 B 崩溃仅重启 B，A/C 不受影响
  - [ ] 7.2 `kernel/supervisor_test.go`：one_for_all 策略——子进程 B 崩溃重启 A+B+C
  - [ ] 7.3 `kernel/supervisor_test.go`：rest_for_one 策略——子进程 B 崩溃重启 B+C，A 不受影响
  - [ ] 7.4 `kernel/supervisor_test.go`：重启频率超限——3 次快速崩溃后 Supervisor 退出，ExitStatus.Reason = "max_restarts_exceeded"
  - [ ] 7.5 `kernel/supervisor_test.go`：permanent 模式——正常退出也重启
  - [ ] 7.6 `kernel/supervisor_test.go`：transient 模式——正常退出不重启，异常退出重启
  - [ ] 7.7 `kernel/supervisor_test.go`：temporary 模式——永不重启
  - [ ] 7.8 `kernel/supervisor_test.go`：Supervisor Kill → 子进程清理 → 无重启
  - [ ] 7.9 `kernel/supervisor_test.go`：重启时间 ≤ 5 秒验证
  - [ ] 7.10 `kernel/supervisor_test.go`：启动阶段子进程失败 → 回滚已启动的子进程 → Supervisor 退出
  - [ ] 7.11 `kernel/supervisor_test.go`：所有子进程正常完成（temporary）→ Supervisor 正常退出
  - [ ] 7.12 在 `cmd/crux/main_test.go` 中确认无命令注册回归

## Dev Notes

### 关键架构决策

#### Erlang/OTP 风格的 Supervisor 树

设计直接借鉴 Erlang/OTP 的 Supervisor 行为模块，适配 Go 和 Crux 的进程模型：

| Erlang/OTP 概念 | Crux 对应实现 |
|----------------|-------------|
| supervisor 行为模块 | `kernel/supervisor.go` |
| child_spec | `ChildSpec` 结构体 |
| start_child(Mod, Fun, Args) | `k.Spawn(intent, agent, opts)` |
| one_for_one / one_for_all / rest_for_one | `RestartStrategy` 常量 |
| permanent / transient / temporary | `ChildRestart` 常量 |
| {MaxR, MaxT} 重启频率限制 | `MaxRestarts` + `MaxWindow` 滑动窗口 |
| supervisor process | kernel `Process`（运行 supervisorLoop 而非 reasonStep） |

#### Supervisor 是一个 kernel Process

Supervisor 本身是一个 `*Process`，拥有 PID、PPID、State 等全部进程属性。与普通进程的区别是：它不运行 `reasonStep`（LLM 推理循环），而是运行 `supervisorLoop`（子进程监控循环）。这意味着：

- `crux top` 中 Supervisor 显示为普通进程（AGENT 列可显示 "supervisor"）
- `crux ps` 中子进程的 PPID 指向 Supervisor PID
- Kill Supervisor → 同样触发 `handleOrphanChildren` 逻辑
- Supervisor 可以被另一个 Supervisor 监控（树状嵌套）

#### 不需要 LLM FD

Supervisor 不调用 LLM，因此 Spawn 时不打开 `/dev/llm/*`。`SpawnSupervisor` 是独立于 `Spawn` 的方法，跳过 LLM 初始化。

### Supervisor 核心类型定义

```go
// kernel/supervisor.go

type RestartStrategy string

const (
    OneForOne  RestartStrategy = "one_for_one"
    OneForAll  RestartStrategy = "one_for_all"
    RestForOne RestartStrategy = "rest_for_one"
)

type ChildRestart string

const (
    RestartPermanent ChildRestart = "permanent"  // always restart
    RestartTransient ChildRestart = "transient"   // restart only on abnormal exit (code != 0)
    RestartTemporary ChildRestart = "temporary"   // never restart
)

type ChildSpec struct {
    Name          string
    Intent        string
    Agent         *agents.AgentInfo // pre-loaded, nil = agent-less spawn
    Model         string
    ContextBudget int
    Restart       ChildRestart // default: RestartPermanent
}

type SupervisorSpec struct {
    Strategy    RestartStrategy
    MaxRestarts int           // max restarts within MaxWindow (default: 3)
    MaxWindow   time.Duration // sliding window size (default: 60s)
    Children    []ChildSpec   // ordered list, startup order = index order
}
```

### SpawnSupervisor 实现

```go
// kernel/kernel.go — SupervisorManager 接口
type SupervisorManager interface {
    SpawnSupervisor(spec SupervisorSpec) (types.PID, error)
}

// kernel/supervisor.go — 实现
func (k *KernelImpl) SpawnSupervisor(spec SupervisorSpec) (types.PID, error) {
    start := time.Now()

    // Validate spec
    if len(spec.Children) == 0 {
        return 0, NewSyscallError("SpawnSupervisor", 0, "",
            fmt.Errorf("supervisor must have at least one child spec"), types.ErrInvalid)
    }
    if spec.MaxRestarts <= 0 {
        spec.MaxRestarts = 3
    }
    if spec.MaxWindow <= 0 {
        spec.MaxWindow = 60 * time.Second
    }
    for i, cs := range spec.Children {
        if cs.Restart == "" {
            spec.Children[i].Restart = RestartPermanent
        }
    }

    proc := NewProcess(0, "supervisor:"+spec.Strategy, nil)
    ctx, cancel := gocontext.WithCancel(gocontext.Background())
    proc.cancel = cancel
    proc.ctx = ctx

    k.AddProcess(proc)
    k.msgQueues.Store(proc.PID, newMessageQueue())

    sup := newSupervisor(proc, spec, k)

    k.emitEvent(proc, "SpawnSupervisor", map[string]any{
        "strategy":     string(spec.Strategy),
        "max_restarts": spec.MaxRestarts,
        "max_window":   spec.MaxWindow.String(),
        "children":     len(spec.Children),
    }, proc.PID, nil, time.Since(start))

    proc.wg.Add(1)
    go func() {
        defer proc.wg.Done()
        _ = proc.Start() // Created → Running
        sup.run()
    }()

    if k.callbacks != nil {
        k.callbacks.OnSpawn(proc.PID, proc.Intent)
    }

    return proc.PID, nil
}
```

### Supervisor 内部结构

```go
type childExit struct {
    index int
    exit  ExitStatus
}

type supervisedChild struct {
    spec  ChildSpec
    pid   types.PID
    alive bool
}

type Supervisor struct {
    proc     *Process
    spec     SupervisorSpec
    kernel   *KernelImpl
    children []*supervisedChild
    exitCh   chan childExit     // 聚合所有子进程退出通知
    restartTimes []time.Time   // 滑动窗口重启记录
    mu       sync.Mutex
}

func newSupervisor(proc *Process, spec SupervisorSpec, k *KernelImpl) *Supervisor {
    return &Supervisor{
        proc:     proc,
        spec:     spec,
        kernel:   k,
        children: make([]*supervisedChild, len(spec.Children)),
        exitCh:   make(chan childExit, len(spec.Children)),
    }
}
```

### 主循环设计

```go
func (s *Supervisor) run() {
    // Phase 1: 按序启动所有子进程
    for i, childSpec := range s.spec.Children {
        pid, err := s.startChild(i, childSpec)
        if err != nil {
            // 启动失败：回滚所有已启动的子进程
            s.shutdownStarted(i)
            s.kernel.finishProcess(s.proc, ExitStatus{
                Code:   1,
                Reason: fmt.Sprintf("child %q start failed: %v", childSpec.Name, err),
                Err:    err,
            })
            return
        }
        s.children[i] = &supervisedChild{
            spec:  childSpec,
            pid:   pid,
            alive: true,
        }
    }

    // Phase 2: 监控循环
    for {
        select {
        case ce := <-s.exitCh:
            s.handleChildExit(ce.index, ce.exit)
            // 检查是否所有子进程已终止且不需要重启
            if s.allDone() {
                s.kernel.finishProcess(s.proc, ExitStatus{
                    Code: 0, Reason: "all children completed",
                })
                return
            }
        case <-s.proc.ctx.Done():
            // Supervisor 被 Kill 或取消
            s.shutdownAll()
            s.kernel.finishProcess(s.proc, ExitStatus{
                Code: 0, Reason: "supervisor cancelled",
            })
            return
        }
    }
}
```

### 子进程启动与监控

```go
func (s *Supervisor) startChild(idx int, spec ChildSpec) (types.PID, error) {
    opts := SpawnOpts{
        ParentPID:     s.proc.PID,
        Model:         spec.Model,
        ContextBudget: spec.ContextBudget,
    }
    pid, err := s.kernel.Spawn(spec.Intent, spec.Agent, opts)
    if err != nil {
        return 0, err
    }

    s.kernel.emitEvent(s.proc, "SupervisorStartChild", map[string]any{
        "child_name":  spec.Name,
        "child_pid":   pid,
        "child_index": idx,
    }, pid, nil, 0)

    // 启动 monitor goroutine：读 child Done → 发送到聚合 exitCh
    childProc, _ := s.kernel.GetProcess(pid)
    go func(index int, done <-chan ExitStatus) {
        exit := <-done
        s.exitCh <- childExit{index: index, exit: exit}
    }(idx, childProc.Done)

    return pid, nil
}
```

**关键设计点**：monitor goroutine 从 `child.Done` 读取退出状态，然后发送到 `s.exitCh`。这样主循环只需 select 单个聚合 channel，避免 reflect.Select 的复杂性。

### 停止子进程

```go
func (s *Supervisor) stopChild(idx int) {
    child := s.children[idx]
    if child == nil || !child.alive {
        return
    }
    child.alive = false

    // Kill + Wait Done（超时 5 秒）
    _ = s.kernel.Kill(child.pid, types.SIGKILL)
    timer := time.NewTimer(5 * time.Second)
    defer timer.Stop()
    select {
    case <-s.getChildDone(child.pid):
    case <-timer.C:
    }
    s.kernel.Reap(child.pid)
}
```

注意：`stopChild` 后该 index 的 monitor goroutine 可能还会向 `exitCh` 发送事件。主循环需要忽略已标记 `alive=false` 的子进程退出事件。

### 重启策略实现

**shouldRestart 判断：**

```go
func (s *Supervisor) shouldRestart(spec ChildSpec, exit ExitStatus) bool {
    switch spec.Restart {
    case RestartPermanent:
        return true
    case RestartTransient:
        return exit.Code != 0 // 仅异常退出重启
    case RestartTemporary:
        return false
    default:
        return true // 默认 permanent
    }
}
```

**one_for_one：**

```go
func (s *Supervisor) restartOneForOne(idx int) error {
    child := s.children[idx]
    s.kernel.Reap(child.pid) // 清理旧进程

    pid, err := s.startChild(idx, child.spec)
    if err != nil {
        return err
    }
    s.children[idx] = &supervisedChild{
        spec: child.spec, pid: pid, alive: true,
    }
    return nil
}
```

**one_for_all：**

```go
func (s *Supervisor) restartOneForAll(crashedIdx int) error {
    // 1. 逆序停止所有存活子进程（崩溃的已经死了，跳过）
    for i := len(s.children) - 1; i >= 0; i-- {
        if i != crashedIdx {
            s.stopChild(i)
        }
    }
    // 崩溃子进程也需要 Reap
    s.kernel.Reap(s.children[crashedIdx].pid)

    // 2. 顺序重启所有子进程
    for i, child := range s.children {
        pid, err := s.startChild(i, child.spec)
        if err != nil {
            s.shutdownStarted(i)
            return err
        }
        s.children[i] = &supervisedChild{
            spec: child.spec, pid: pid, alive: true,
        }
    }
    return nil
}
```

**rest_for_one：**

```go
func (s *Supervisor) restartRestForOne(crashedIdx int) error {
    // 1. 逆序停止 crashedIdx 之后的存活子进程
    for i := len(s.children) - 1; i > crashedIdx; i-- {
        s.stopChild(i)
    }
    // 崩溃子进程 Reap
    s.kernel.Reap(s.children[crashedIdx].pid)

    // 2. 顺序重启 crashedIdx 及之后的所有子进程
    for i := crashedIdx; i < len(s.children); i++ {
        child := s.children[i]
        pid, err := s.startChild(i, child.spec)
        if err != nil {
            s.shutdownStarted(i)
            return err
        }
        s.children[i] = &supervisedChild{
            spec: child.spec, pid: pid, alive: true,
        }
    }
    return nil
}
```

### 重启频率保护（滑动窗口）

```go
func (s *Supervisor) recordRestart() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.restartTimes = append(s.restartTimes, time.Now())
}

func (s *Supervisor) exceedsRestartLimit() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    cutoff := time.Now().Add(-s.spec.MaxWindow)
    count := 0
    for _, t := range s.restartTimes {
        if t.After(cutoff) {
            count++
        }
    }
    return count >= s.spec.MaxRestarts
}
```

超限时 Supervisor 自身退出：

```go
// 在 handleChildExit 中
if s.exceedsRestartLimit() {
    s.kernel.emitEvent(s.proc, "SupervisorShutdown", map[string]any{
        "reason":       "max_restarts_exceeded",
        "max_restarts": s.spec.MaxRestarts,
        "max_window":   s.spec.MaxWindow.String(),
    }, nil, nil, 0)
    s.shutdownAll()
    s.kernel.finishProcess(s.proc, ExitStatus{
        Code:   1,
        Reason: "max_restarts_exceeded",
        Err:    fmt.Errorf("supervisor restart limit exceeded: %d restarts in %s",
            s.spec.MaxRestarts, s.spec.MaxWindow),
    })
    return // 触发 run() 退出
}
```

### handleChildExit 完整流程

```go
func (s *Supervisor) handleChildExit(idx int, exit ExitStatus) {
    child := s.children[idx]
    if child == nil || !child.alive {
        return // 已处理或已标记不活跃
    }
    child.alive = false

    s.kernel.emitEvent(s.proc, "SupervisorChildExit", map[string]any{
        "child_name":  child.spec.Name,
        "child_pid":   child.pid,
        "child_index": idx,
        "exit_code":   exit.Code,
        "exit_reason": exit.Reason,
    }, nil, exit.Err, 0)

    if !s.shouldRestart(child.spec, exit) {
        s.kernel.Reap(child.pid) // 清理不需要重启的子进程
        return
    }

    s.recordRestart()
    if s.exceedsRestartLimit() {
        // 重启风暴 → Supervisor 退出
        s.kernel.emitEvent(s.proc, "SupervisorShutdown", map[string]any{
            "reason": "max_restarts_exceeded",
        }, nil, nil, 0)
        s.shutdownAll()
        s.kernel.finishProcess(s.proc, ExitStatus{
            Code:   1,
            Reason: "max_restarts_exceeded",
        })
        // 标记 run() 循环退出（通过检查 proc state）
        return
    }

    s.kernel.emitEvent(s.proc, "SupervisorRestart", map[string]any{
        "strategy":    string(s.spec.Strategy),
        "child_name":  child.spec.Name,
        "child_index": idx,
    }, nil, nil, 0)

    var err error
    switch s.spec.Strategy {
    case OneForOne:
        err = s.restartOneForOne(idx)
    case OneForAll:
        err = s.restartOneForAll(idx)
    case RestForOne:
        err = s.restartRestForOne(idx)
    }

    if err != nil {
        // 重启过程中启动失败 → Supervisor 退出
        s.kernel.finishProcess(s.proc, ExitStatus{
            Code:   1,
            Reason: fmt.Sprintf("restart failed: %v", err),
            Err:    err,
        })
    }
}
```

### 与现有系统的集成

#### finishProcess 和 handleOrphanChildren

当 Supervisor 退出时，`finishProcess` → `handleOrphanChildren` 会将存活的子进程 reparent 到 PID 0。`shutdownAll()` 在 `finishProcess` 之前执行，确保正常情况下子进程已全部停止。极端情况下（如 shutdownAll 超时），`handleOrphanChildren` 作为安全网。

#### crux top / crux ps

Supervisor 进程在进程列表中显示为：
- PID: 分配的 PID
- PPID: 0（顶层 Supervisor）或父 Supervisor PID（嵌套 Supervisor）
- STATE: Running（监控中）或 Zombie（退出后）
- INTENT: "supervisor:one_for_one"（策略作为 intent 的一部分）
- TOKENS: 0（Supervisor 不消耗 LLM token）

子进程的 PPID 指向 Supervisor PID，树状视图中自然嵌套显示。

#### Process Groups

Supervisor 的子进程可加入同一进程组（PGID）。`one_for_all` 策略可利用 `SignalGroup` 发送 SIGKILL 批量停止。但 MVP 实现不依赖进程组——直接遍历 children 列表逐个 Kill 更简单且可控。

### 复用现有代码

**必须复用（不要重新实现）：**
- `kernel/kernel.go`：`Spawn(intent, agent, opts)` — 子进程创建
- `kernel/kernel.go`：`Kill(pid, signal)` — 子进程停止
- `kernel/kernel.go`：`finishProcess(proc, exit)` — 进程终止
- `kernel/kernel.go`：`emitEvent(proc, ...)` — 事件记录
- `kernel/process.go`：`NewProcess(ppid, intent, skills)` — 进程创建
- `kernel/process.go`：`Start()` / `Terminate()` / `Cancel()` — 生命周期管理
- `kernel/reap.go`：`Reap(pid)` — 僵尸进程清理
- `kernel/reap.go`：`handleOrphanChildren` — 孤儿处理（Supervisor 退出时的安全网）

**复用的架构模式：**
- reasonStep goroutine 模式 → supervisorLoop goroutine（同样 `proc.wg.Add(1)` + `defer proc.wg.Done()`）
- finishProcess 退出模式 → Supervisor 退出时调用 finishProcess
- Done channel 通知模式 → 子进程监控基于 Done channel
- Reap 清理模式 → 子进程重启前调用 Reap
- emitEvent 事件模式 → Supervisor 事件（SupervisorRestart, SupervisorChildExit 等）

### 不需要修改的包

- `vfs/`、`drivers/`、`context/`、`debug/`、`agents/`、`skills/` — 均不涉及
- `compose/` — Supervisor 与 compose 集成留到 10.5（init bootstrap）
- `ipc/` — SpawnSupervisor 的 IPC 暴露留到 10.5
- `cmd/crux/` — 无需新 CLI 命令；crux top/ps 已自动显示 Supervisor 进程
- `internal/ui/` — 无需新 UI 组件

### 修改文件清单

**新文件：**
- `kernel/supervisor.go` — Supervisor 核心实现（~350 行）
- `kernel/supervisor_test.go` — 12 个测试用例

**修改文件：**
- `kernel/kernel.go` — 添加 `SupervisorManager` 接口 + 编译时检查（~5 行）

### 测试策略

#### Mock 模式（复用现有 pattern）

使用 `newTestKernel(t, llmFile)` 创建测试内核。子进程通过不同的 mock LLM 文件控制行为：

- **successLLM**：返回正常响应，子进程正常退出（Code=0）
- **failLLM**：返回错误，子进程异常退出（Code=1）
- **crashAfterNLLM**：前 N 次正常，第 N+1 次崩溃——用于测试重启后的行为
- **slowLLM**：延迟响应——用于测试 Kill/超时场景

每个测试创建 `SupervisorSpec` 并调用 `SpawnSupervisor`，然后通过 `k.Wait(supervisorPID)` 等待 Supervisor 完成，验证：
- ExitStatus 的 Code 和 Reason
- 子进程的 PID 分配（重启后 PID 变化）
- emitEvent 事件序列
- 重启计数

#### 测试用例分组

| 测试 ID | 类别 | 验证内容 |
|---------|------|---------|
| 10.4-UNIT-001 | P0 | one_for_one：B 崩溃 → 仅 B 重启 |
| 10.4-UNIT-002 | P0 | one_for_all：B 崩溃 → A+B+C 全部重启 |
| 10.4-UNIT-003 | P0 | rest_for_one：B 崩溃 → B+C 重启，A 不变 |
| 10.4-UNIT-004 | P0 | 重启频率超限 → Supervisor 退出 |
| 10.4-UNIT-005 | P1 | permanent：正常退出也重启 |
| 10.4-UNIT-006 | P1 | transient：正常退出不重启，异常退出重启 |
| 10.4-UNIT-007 | P1 | temporary：永不重启 |
| 10.4-UNIT-008 | P1 | Supervisor Kill → 子进程清理 |
| 10.4-INT-001 | P0 | 重启时间 ≤ 5 秒 |
| 10.4-INT-002 | P1 | 启动失败 → 回滚 → Supervisor 退出 |
| 10.4-INT-003 | P1 | 所有 temporary 子进程完成 → Supervisor 正常退出 |
| 10.4-REG-001 | P2 | 现有测试全部通过（回归检查） |

### 边界情况

- **单子进程 Supervisor**：只有一个 ChildSpec，三种策略行为一致（仅重启该子进程）
- **全部 temporary 子进程**：所有子进程退出后 Supervisor 正常退出（Code=0）
- **Supervisor 启动时第一个子进程就失败**：立即退出，无需回滚其他子进程
- **重启过程中 Supervisor 被 Kill**：`shutdownAll` 在 ctx.Done 分支处理，重启逻辑通过 `s.proc.GetState()` 检查避免竞态
- **子进程在重启过程中退出**：one_for_all/rest_for_one 中，正在被停止的子进程可能已经自行退出。`stopChild` 中 Kill 对已退出的进程返回 ErrNotFound，安全忽略
- **exitCh 积压**：exitCh 缓冲大小 = 子进程数。极端情况下所有子进程同时崩溃，缓冲足够容纳
- **嵌套 Supervisor**：ChildSpec.Intent 可以是另一个 Supervisor（通过 SpawnSupervisor 启动）。但 MVP 不需要支持——ChildSpec 使用 `k.Spawn` 启动普通进程
- **MaxRestarts = 0**：第一次重启就超限，等价于"不允许重启"。spec 验证时设默认值 3

### Epic 9 回顾教训

根据 `epic-9-retro-2026-03-02.md` 的关键发现：

1. **reapProcess 集成测试（CRITICAL 前置）**：Supervisor 依赖 reapProcess 链（Reap → reapProcess → 资源释放）。确保测试中验证 Supervisor 调用 Reap 后子进程确实被清理。
2. **新领域风险清单**：Supervisor 是全新领域（进程监控+自动重启），需特别注意：
   - monitor goroutine 泄漏：确保所有 monitor goroutine 在 Supervisor 退出后不会永远阻塞
   - exitCh 未关闭导致的 goroutine 泄漏：Supervisor 退出前 shutdownAll 确保所有子进程已退出，monitor goroutine 收到 Done 后退出
   - 竞态条件：handleChildExit 可能与 ctx.Done 并发——使用 `s.proc.GetState()` 检查避免 finishProcess 重复调用
3. **不用"已有模式"做借口**：如果发现 `handleOrphanChildren` 或 `finishProcess` 的行为不适合 Supervisor 场景，应该修复模式而非绕过。

### Project Structure Notes

- **新文件**：
  - `kernel/supervisor.go` — Supervisor 核心实现（类型定义 + SpawnSupervisor + run + 策略实现 + 频率保护）
  - `kernel/supervisor_test.go` — 12 个测试用例
- **修改文件**：
  - `kernel/kernel.go` — `SupervisorManager` 接口（~5 行新增）
- **不修改**：astrace、crux log/top/ps、驱动层、context 包、agents 包、skills 包、compose 包、IPC 层、UI 组件
- **不需要新依赖**

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-10-监控supervisor-与运维monitoring-supervisor-operations.md#Story 10.4]
- [Source: _bmad-output/planning-artifacts/archive/prd.md#FR63 Supervisor 树管理模式]
- [Source: _bmad-output/planning-artifacts/archive/prd.md#FR64 三种重启策略]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#Decision 2: 进程模型与并发]
- [Source: _bmad-output/project-context.md#进程状态机]
- [Source: _bmad-output/project-context.md#Channel 使用规则]
- [Source: _bmad-output/implementation-artifacts/epic-9-retro-2026-03-02.md#Supervisor + reapProcess 风险]
- [Source: kernel/kernel.go#SpawnOpts + Spawn 方法]
- [Source: kernel/kernel.go#finishProcess]
- [Source: kernel/kernel.go#KernelImpl struct]
- [Source: kernel/kernel.go#emitEvent]
- [Source: kernel/process.go#Process struct + 状态机]
- [Source: kernel/process.go#NewProcess + Start + Terminate + Cancel]
- [Source: kernel/reap.go#reapProcess + handleOrphanChildren + Reap + Wait]
- [Source: kernel/signal.go#Signal + Kill 机制]
- [Source: kernel/procgroup.go#ProcGroupManager + SignalGroup]
- [Source: internal/types/types.go#PID + ProcessState + Signal]
- [Source: _bmad-output/implementation-artifacts/10-3-token-budget-management.md#SpawnOpts 预算优先级模式]
- [Source: _bmad-output/implementation-artifacts/10-1-crux-top-realtime-monitoring-tui.md#进程树构建]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
