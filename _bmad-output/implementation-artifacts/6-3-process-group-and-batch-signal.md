# Story 6.3: 进程组与批量信号

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 将多个智能体分组管理，一条命令控制整组进程,
So that 我可以高效管理多智能体工作流。

## Acceptance Criteria

1. **加入进程组** — Given `kernel/procgroup.go` 已实现，When 调用 `JoinGroup(pid, groupID)`，Then 目标进程加入指定组，And 若该组不存在则自动创建

2. **查询进程组** — Given 进程已加入组，When 调用 `GetProcGroup(groupID)`，Then 返回组内所有进程的 PID 列表

3. **批量信号** — Given 进程组存在，When 向组发送信号（如 Kill），Then 组内所有进程收到信号，And 操作延迟不超过单进程的 2 倍（NFR24）

4. **进程退出自动移除** — Given 进程退出，When 该进程属于某个组，Then 自动从组中移除，And 若组变为空则自动销毁

## Tasks / Subtasks

- [x] Task 1: 新增类型和接口定义 (AC: #1, #2, #3)
  - [x] 1.1 在 `internal/types/types.go` 中新增 `PGID uint64` 类型别名
  - [x] 1.2 创建 `kernel/procgroup.go`，定义 `ProcGroupManager` 子接口（`JoinGroup`, `LeaveGroup`, `GetProcGroup`, `SignalGroup`）
  - [x] 1.3 添加编译期检查 `var _ ProcGroupManager = (*KernelImpl)(nil)`
  - [x] 1.4 验证 Phase 1 + Story 6.1/6.2 全部现有测试通过（`make test`）

- [x] Task 2: 实现 ProcGroup 数据结构 (AC: #1, #2, #4)
  - [x] 2.1 在 `kernel/procgroup.go` 中定义 `ProcGroup` 结构体（`sync.RWMutex` + `id PGID` + `members map[types.PID]struct{}`）
  - [x] 2.2 实现 `ProcGroup.Add(pid)` — 添加成员
  - [x] 2.3 实现 `ProcGroup.Remove(pid)` — 移除成员
  - [x] 2.4 实现 `ProcGroup.Members() []PID` — 返回成员列表快照
  - [x] 2.5 实现 `ProcGroup.Size() int` — 返回成员数量
  - [x] 2.6 实现 `ProcGroup.Contains(pid) bool` — 检查是否包含

- [x] Task 3: KernelImpl 扩展与 JoinGroup/LeaveGroup 实现 (AC: #1, #4)
  - [x] 3.1 在 `KernelImpl` 中添加 `procGroups *xsync.SyncMap[types.PGID, *ProcGroup]` 字段
  - [x] 3.2 在 `kernel.New()` 中初始化 `procGroups`
  - [x] 3.3 在 `Process` 结构体中添加 `groups []types.PGID` 字段（mu 保护）
  - [x] 3.4 在 `Process` 中添加 `AddGroup(pgid)`/`RemoveGroup(pgid)`/`GetGroups() []PGID` 方法
  - [x] 3.5 实现 `JoinGroup(pid PID, groupID PGID) error`：验证 PID 存在且活跃 → 获取或创建 ProcGroup → group.Add(pid) → proc.AddGroup(groupID) → emitEvent
  - [x] 3.6 实现 `LeaveGroup(pid PID, groupID PGID) error`：验证 PID 和组存在 → group.Remove(pid) → proc.RemoveGroup(groupID) → 若组空则删除 → emitEvent
  - [x] 3.7 所有错误包装为 `*SyscallError`

- [x] Task 4: GetProcGroup 实现 (AC: #2)
  - [x] 4.1 实现 `GetProcGroup(groupID PGID) ([]PID, error)`：查找组 → 返回成员快照
  - [x] 4.2 组不存在时返回 `*SyscallError`（Code=ErrNotFound）
  - [x] 4.3 emitEvent 记录调用

- [x] Task 5: SignalGroup 实现 (AC: #3)
  - [x] 5.1 实现 `SignalGroup(groupID PGID, signal Signal) error`：获取组成员快照 → 遍历调用 `k.Kill(pid, signal)` → 收集结果
  - [x] 5.2 组不存在时返回 `*SyscallError`（Code=ErrNotFound）
  - [x] 5.3 信号无效时返回 `*SyscallError`（Code=ErrInvalid）
  - [x] 5.4 部分进程已退出：跳过已不存在的进程（Kill 返回 ErrNotFound 时忽略），成功发送给所有存活成员即视为成功
  - [x] 5.5 emitEvent 记录 SignalGroup，Args 包含 `group_id`、`signal`、`member_count`、`success_count`
  - [x] 5.6 验证 NFR24：遍历发信号的耗时 ≤ 单进程 Kill 的 2 倍

- [x] Task 6: reapProcess 集成 — 进程退出自动清理 (AC: #4)
  - [x] 6.1 在 `kernel/reap.go` 的 `reapProcess` 中，在关闭 msgQueue 之后、CtxFree 之前，添加进程组清理步骤
  - [x] 6.2 实现 `removeFromAllGroups(pid PID)`：获取 proc.GetGroups() → 遍历每个 PGID → group.Remove(pid) → 若组空则从 procGroups 删除
  - [x] 6.3 确保清理逻辑在进程表移除之前执行，避免悬挂引用

- [x] Task 7: 单元测试 (AC: #1-4)
  - [x] 7.1 `kernel/procgroup_test.go` — TestJoinGroup_Basic：进程加入组，GetProcGroup 返回该 PID
  - [x] 7.2 TestJoinGroup_MultipleProcesses：多个进程加入同一组，GetProcGroup 返回全部
  - [x] 7.3 TestJoinGroup_MultipleGroups：一个进程加入多个组，各组独立包含
  - [x] 7.4 TestJoinGroup_AutoCreate：首次 JoinGroup 自动创建组
  - [x] 7.5 TestJoinGroup_InvalidPID：不存在或 Dead/Zombie PID 返回 ErrNotFound
  - [x] 7.6 TestJoinGroup_Duplicate：重复加入同一组不报错（幂等）
  - [x] 7.7 TestLeaveGroup_Basic：LeaveGroup 后 GetProcGroup 不再包含该 PID
  - [x] 7.8 TestLeaveGroup_AutoDestroy：最后一个成员离开后组自动销毁
  - [x] 7.9 TestLeaveGroup_NotInGroup：离开未加入的组返回 ErrNotFound
  - [x] 7.10 TestGetProcGroup_NotFound：查询不存在的组返回 ErrNotFound
  - [x] 7.11 TestSignalGroup_Basic：组内所有进程收到信号（Kill）
  - [x] 7.12 TestSignalGroup_PartialExit：部分进程已退出，剩余进程仍收到信号
  - [x] 7.13 TestSignalGroup_EmptyGroup：空组返回 ErrNotFound（已自动销毁）
  - [x] 7.14 TestSignalGroup_InvalidSignal：无效信号返回 ErrInvalid
  - [x] 7.15 TestReapProcess_AutoRemove：进程退出后自动从所有所属组中移除
  - [x] 7.16 TestReapProcess_GroupAutoDestroy：最后成员退出后组自动销毁
  - [x] 7.17 TestProcGroup_Concurrent：100 goroutine 并发 JoinGroup/LeaveGroup/GetProcGroup，无 race
  - [x] 7.18 TestSignalGroup_SyscallEvent：验证 DebugChan 收到 JoinGroup/SignalGroup 的 SyscallEvent
  - [x] 7.19 TestSignalGroup_Performance：10 个进程组内 SignalGroup 延迟 ≤ 单进程 Kill 的 2 倍（NFR24）

- [x] Task 8: 集成验证 (AC: #1-4)
  - [x] 8.1 `make test` 全部通过（含 `-race`）
  - [x] 8.2 `make lint` 通过
  - [x] 8.3 `make build` 编译成功
  - [x] 8.4 验证 Phase 1 + Story 6.1/6.2 所有现有测试无回归

## Dev Notes

### 核心设计决策

**ProcGroupManager 作为独立子接口**：遵循架构决策 Decision 1（分类接口组合），新增 `ProcGroupManager` 子接口嵌入 Kernel。进程组是独立的管理域——用于 Compose 分组（Epic 7）、Supervisor 树（Epic 10）、AgentShell 管道组合（Epic 11），不属于 IPC 消息传递范畴。

**隐式组生命周期**：组在首个进程 JoinGroup 时自动创建，最后一个成员离开/退出时自动销毁。无需显式 CreateGroup/DestroyGroup API，简化 API 表面积。这与 Unix 进程组的行为一致（setpgid 隐式创建）。

**内核内部实现**：与 Story 6.1/6.2 一致，进程组是**内核内部**概念（同一 KernelImpl 内），实现在 `kernel/procgroup.go`，不修改 `ipc/` 包。

**SignalGroup 复用 Kill**：`SignalGroup` 内部遍历组成员调用现有 `k.Kill(pid, signal)`。当 Story 6.4 扩展信号系统后，SignalGroup 自动获得新信号支持。

**双向引用设计**：`KernelImpl.procGroups` 维护正向映射（PGID → ProcGroup），`Process.groups` 维护反向映射（进程所属的组列表）。reapProcess 通过反向映射高效清理，无需遍历所有组。

### ProcGroupManager 接口定义

```go
// kernel/procgroup.go

// ProcGroupManager 管理进程组和批量信号
type ProcGroupManager interface {
    JoinGroup(pid types.PID, groupID types.PGID) error
    LeaveGroup(pid types.PID, groupID types.PGID) error
    GetProcGroup(groupID types.PGID) ([]types.PID, error)
    SignalGroup(groupID types.PGID, signal types.Signal) error
}
```

### ProcGroup 数据结构

```go
// kernel/procgroup.go

// ProcGroup 是进程组的内部表示
type ProcGroup struct {
    mu      sync.RWMutex
    id      types.PGID
    members map[types.PID]struct{}
}

func newProcGroup(id types.PGID) *ProcGroup {
    return &ProcGroup{
        id:      id,
        members: make(map[types.PID]struct{}),
    }
}

func (g *ProcGroup) Add(pid types.PID) {
    g.mu.Lock()
    defer g.mu.Unlock()
    g.members[pid] = struct{}{}
}

func (g *ProcGroup) Remove(pid types.PID) bool {
    g.mu.Lock()
    defer g.mu.Unlock()
    _, existed := g.members[pid]
    delete(g.members, pid)
    return existed
}

func (g *ProcGroup) Members() []types.PID {
    g.mu.RLock()
    defer g.mu.RUnlock()
    pids := make([]types.PID, 0, len(g.members))
    for pid := range g.members {
        pids = append(pids, pid)
    }
    return pids
}

func (g *ProcGroup) Size() int {
    g.mu.RLock()
    defer g.mu.RUnlock()
    return len(g.members)
}

func (g *ProcGroup) Contains(pid types.PID) bool {
    g.mu.RLock()
    defer g.mu.RUnlock()
    _, ok := g.members[pid]
    return ok
}
```

### KernelImpl 扩展

```go
// kernel/kernel.go — KernelImpl 新增字段
type KernelImpl struct {
    // ... 现有字段
    procGroups *xsync.SyncMap[types.PGID, *ProcGroup]  // 进程组表
}
```

在 `kernel.New()` 中初始化：
```go
procGroups: xsync.NewSyncMap[types.PGID, *ProcGroup](),
```

### Process 结构体扩展

```go
// kernel/process.go — Process 新增字段
type Process struct {
    // ... 现有字段
    groups []types.PGID  // mu 保护，进程所属的组列表
}

// 新增方法
func (p *Process) AddGroup(pgid types.PGID) {
    p.mu.Lock()
    defer p.mu.Unlock()
    // 检查是否已存在（幂等）
    for _, g := range p.groups {
        if g == pgid {
            return
        }
    }
    p.groups = append(p.groups, pgid)
}

func (p *Process) RemoveGroup(pgid types.PGID) {
    p.mu.Lock()
    defer p.mu.Unlock()
    for i, g := range p.groups {
        if g == pgid {
            p.groups = append(p.groups[:i], p.groups[i+1:]...)
            return
        }
    }
}

func (p *Process) GetGroups() []types.PGID {
    p.mu.Lock()
    defer p.mu.Unlock()
    result := make([]types.PGID, len(p.groups))
    copy(result, p.groups)
    return result
}
```

### JoinGroup 实现要点

```go
func (k *KernelImpl) JoinGroup(pid types.PID, groupID types.PGID) error {
    start := time.Now()

    // 1. 验证进程存在且活跃
    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("JoinGroup", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }
    if state := proc.GetState(); state == types.StateZombie || state == types.StateDead {
        return NewSyscallError("JoinGroup", pid, "",
            fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
    }

    // 2. 获取或创建进程组（原子操作）
    group, _ := k.procGroups.LoadOrStore(groupID, newProcGroup(groupID))

    // 3. 添加到组
    group.Add(pid)

    // 4. 反向引用
    proc.AddGroup(groupID)

    // 5. SyscallEvent
    k.emitEvent(proc, "JoinGroup", map[string]any{
        "pid":      pid,
        "group_id": groupID,
    }, nil, nil, time.Since(start))

    return nil
}
```

### LeaveGroup 实现要点

```go
func (k *KernelImpl) LeaveGroup(pid types.PID, groupID types.PGID) error {
    start := time.Now()

    // 1. 验证进程存在
    proc, ok := k.GetProcess(pid)
    if !ok {
        return NewSyscallError("LeaveGroup", pid, "",
            fmt.Errorf("process not found"), types.ErrNotFound)
    }

    // 2. 查找组
    group, ok := k.procGroups.Load(groupID)
    if !ok {
        return NewSyscallError("LeaveGroup", pid, "",
            fmt.Errorf("group %d not found", groupID), types.ErrNotFound)
    }

    // 3. 从组中移除
    if !group.Remove(pid) {
        return NewSyscallError("LeaveGroup", pid, "",
            fmt.Errorf("process %d not in group %d", pid, groupID), types.ErrNotFound)
    }

    // 4. 移除反向引用
    proc.RemoveGroup(groupID)

    // 5. 若组空则自动销毁
    if group.Size() == 0 {
        k.procGroups.Delete(groupID)
    }

    // 6. SyscallEvent
    k.emitEvent(proc, "LeaveGroup", map[string]any{
        "pid":      pid,
        "group_id": groupID,
    }, nil, nil, time.Since(start))

    return nil
}
```

### SignalGroup 实现要点

```go
func (k *KernelImpl) SignalGroup(groupID types.PGID, signal types.Signal) error {
    start := time.Now()

    // 1. 验证信号有效
    if !signal.Valid() {
        return NewSyscallError("SignalGroup", 0, "",
            fmt.Errorf("invalid signal: %d", signal), types.ErrInvalid)
    }

    // 2. 查找组
    group, ok := k.procGroups.Load(groupID)
    if !ok {
        return NewSyscallError("SignalGroup", 0, "",
            fmt.Errorf("group %d not found", groupID), types.ErrNotFound)
    }

    // 3. 获取成员快照（避免持锁调用 Kill）
    members := group.Members()
    if len(members) == 0 {
        return NewSyscallError("SignalGroup", 0, "",
            fmt.Errorf("group %d is empty", groupID), types.ErrNotFound)
    }

    // 4. 遍历发送信号
    successCount := 0
    for _, pid := range members {
        err := k.Kill(pid, signal)
        if err == nil {
            successCount++
        }
        // Kill 返回 ErrNotFound（进程已退出）时忽略，继续处理其他成员
    }

    // 5. SyscallEvent（以 PID 0 为事件源，表示组级操作）
    // 注意：需要一个有效的 Process 来调用 emitEvent
    // 使用第一个成功的进程或 members[0] 对应的进程
    if proc, ok := k.GetProcess(members[0]); ok {
        k.emitEvent(proc, "SignalGroup", map[string]any{
            "group_id":      groupID,
            "signal":        signal,
            "member_count":  len(members),
            "success_count": successCount,
        }, nil, nil, time.Since(start))
    }

    return nil
}
```

### reapProcess 集成

```go
// kernel/reap.go — reapProcess 资源释放序列新增进程组清理

func (k *KernelImpl) reapProcess(pid types.PID) {
    proc, ok := k.GetProcess(pid)
    if !ok { return }

    proc.Cancel()          // 1. 取消 context
    proc.WaitDone()        // 2. 等待 goroutine
    // ... 关闭 DebugChan
    // ... 关闭 msgQueue（Story 6.1）

    // 🆕 进程组清理（Story 6.3）
    k.removeFromAllGroups(pid, proc)

    // ... CtxFree
    // ... Reap (Zombie → Dead)
    // ... 移除进程表
}

// removeFromAllGroups 从进程所属的所有组中移除
func (k *KernelImpl) removeFromAllGroups(pid types.PID, proc *Process) {
    for _, pgid := range proc.GetGroups() {
        if group, ok := k.procGroups.Load(pgid); ok {
            group.Remove(pid)
            if group.Size() == 0 {
                k.procGroups.Delete(pgid)
            }
        }
    }
}
```

**资源释放顺序（完整，含 Story 6.3 新增步骤）：**
```
cancel() → wg.Wait() → 关闭 DebugChan → msgQueue.close()
→ 🆕 removeFromAllGroups → CtxFree → Reap → Remove
```

### 错误码使用

| 场景 | Syscall | ErrCode | 说明 |
|------|---------|---------|------|
| PID 不存在 | JoinGroup | ErrNotFound | procTable 中找不到 |
| PID Zombie/Dead | JoinGroup | ErrNotFound | 进程已终止 |
| 组不存在 | GetProcGroup | ErrNotFound | procGroups 中找不到 |
| 组不存在 | LeaveGroup | ErrNotFound | procGroups 中找不到 |
| 进程不在组中 | LeaveGroup | ErrNotFound | group.Remove 返回 false |
| 组不存在 | SignalGroup | ErrNotFound | procGroups 中找不到 |
| 组为空 | SignalGroup | ErrNotFound | 所有成员已退出 |
| 无效信号 | SignalGroup | ErrInvalid | signal.Valid() 返回 false |

### SyscallEvent 记录规范

| Syscall | Args | 说明 |
|---------|------|------|
| JoinGroup | `pid`, `group_id` | 进程加入组 |
| LeaveGroup | `pid`, `group_id` | 进程离开组 |
| GetProcGroup | `group_id`, `member_count` | 查询组成员 |
| SignalGroup | `group_id`, `signal`, `member_count`, `success_count` | 批量信号 |

### 并发安全要点

1. **ProcGroup 内部**：`sync.RWMutex` 保护 `members` map，读操作（Members/Size/Contains）用 RLock，写操作（Add/Remove）用 Lock
2. **KernelImpl.procGroups**：`xsync.SyncMap` 本身并发安全，LoadOrStore 保证原子创建
3. **Process.groups**：复用现有 `Process.mu` 保护，与 PID/State 等字段共享锁
4. **SignalGroup 快照模式**：先获取 Members() 快照（释放 ProcGroup 锁），再遍历调用 Kill（避免持锁调用 Kill 导致死锁）
5. **reapProcess 顺序**：进程组清理在 msgQueue.close() 之后、CtxFree 之前，此时进程仍在 procTable 中但已 cancel，不会有新的 JoinGroup 调用
6. **自动销毁竞态**：group.Size() == 0 判断和 procGroups.Delete(pgid) 之间可能有其他 goroutine JoinGroup。使用 procGroups.LoadOrStore 的幂等特性处理——如果 JoinGroup 在 Delete 后执行，LoadOrStore 会创建新组，不影响正确性

### NFR 合规

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR24 | SignalGroup 延迟 ≤ 单进程 Kill 的 2 倍 | 纯内存遍历 + 逐个 Kill（微秒级），远低于 2x 阈值 |
| NFR19 | 不破坏 Phase 1 ABI | ProcGroupManager 作为新子接口嵌入 Kernel，现有接口不变 |
| NFR24 | ≥10 并发进程表操作延迟 ≤ 2x | SyncMap + RWMutex 细粒度锁，procGroups 操作不影响 procTable |

### 反模式警告

- **禁止在 `ipc/` 包中实现**：进程组是内核内部概念，实现在 `kernel/procgroup.go`
- **禁止全局锁保护多个组操作**：每个 ProcGroup 有独立锁，不同组的操作完全并行
- **禁止 SignalGroup 持锁调用 Kill**：先获取成员快照，释放锁后再遍历 Kill，避免死锁（Kill 可能触发 reapProcess → removeFromAllGroups）
- **禁止返回裸 error**：所有 procgroup 方法错误必须包装为 `*SyscallError`
- **禁止显式 CreateGroup/DestroyGroup API**：组隐式创建/销毁，无需暴露
- **禁止在 Process 退出后保留组引用**：reapProcess 必须清理所有组成员关系
- **禁止使用 `sync.Map` 替代 `xsync.SyncMap`**：项目统一使用泛型 SyncMap
- **禁止修改 Process 结构体中 PID/PPID/Children 的语义**：groups 是独立维度，不替代父子关系

### 与 Story 6.1/6.2 的关系

Story 6.1 确立了 IPC 基础模式（子接口扩展、SyscallEvent 记录、进程验证、reapProcess 集成）。Story 6.2 确立了 VFS FD 集成模式。Story 6.3 沿用这些模式：
- 复用 `emitEvent` 记录 SyscallEvent
- 复用进程验证逻辑（GetProcess + 状态检查）
- 复用 reapProcess 资源释放扩展模式（在 msgQueue 后添加新清理步骤）
- 复用 `xsync.SyncMap` 管理并发数据结构
- 复用 `*SyscallError` 错误包装
- 不修改 Story 6.1 已有的 Send/Recv/MessageQueue 实现
- 不修改 Story 6.2 已有的 Pipe/pipeBuffer 实现

### 与 Story 6.4 (Signal) 的前瞻

Story 6.4 将实现完整信号系统（SIGINT、SIGPAUSE、SIGRESUME + SigBlock/SigUnblock）。当前 `SignalGroup` 通过调用现有 `Kill(pid, signal)` 实现，Kill 目前支持 SIGTERM/SIGKILL。Story 6.4 实现后，SignalGroup 自动获得新信号类型的支持，无需修改。

### 与 Epic 7 (Compose) 的前瞻

Compose 编排器可以为一个工作流中的所有智能体分配一个 PGID，实现：
- `compose down` → `SignalGroup(workflowPGID, SIGTERM)` 一键停止
- `compose status` → `GetProcGroup(workflowPGID)` 查询所有成员状态
- 进程完成后自动从组中移除，Compose 检测组为空 → 工作流完成

### 与 Epic 10 (Supervisor) 的前瞻

Supervisor 树可以使用进程组管理被监督的子进程：
- Supervisor 将所有子进程加入同一 PGID
- one_for_all 重启策略：`SignalGroup` 向全组发送终止信号
- 子进程退出自动从组中移除，触发 Supervisor 重启逻辑

### 测试辅助函数

复用现有 `kernel/ipc_test.go` 中的 `newIPCTestProcess` 和 `newSimpleKernel` 辅助函数。这些函数创建轻量级测试进程，满足 procgroup 测试需求。如需额外辅助函数，在 `kernel/procgroup_test.go` 中定义。

### Project Structure Notes

**新增文件：**
```
kernel/procgroup.go       — ProcGroupManager 接口 + ProcGroup 结构 + JoinGroup/LeaveGroup/GetProcGroup/SignalGroup 实现
kernel/procgroup_test.go  — 进程组单元测试（19 个测试用例）
```

**修改文件：**
```
internal/types/types.go   — 新增 PGID uint64 类型
kernel/kernel.go          — KernelImpl 新增 procGroups 字段 + New() 初始化
kernel/process.go         — Process 新增 groups 字段 + AddGroup/RemoveGroup/GetGroups 方法
kernel/reap.go            — reapProcess 新增进程组清理步骤 + removeFromAllGroups 函数
```

**不修改的文件：**
```
kernel/ipc.go             — IPCManager 不变，进程组是独立子接口
kernel/ipc_test.go        — 现有 IPC/Pipe 测试不变
kernel/errors.go          — 无需新 ErrCode（复用 ErrNotFound、ErrInvalid）
vfs/                      — VFS 层不涉及进程组
ipc/                      — 跨终端 IPC daemon，与内核进程组无关
cmd/crux/main.go          — CLI 层暂不暴露进程组命令（由 Compose/Supervisor 使用）
```

### 必需导入

```go
// kernel/procgroup.go
import (
    "fmt"
    "sync"
    "time"

    "github.com/usecrux/crux/internal/types"
)

// kernel/procgroup_test.go
import (
    "sync"
    "testing"

    "github.com/usecrux/crux/internal/types"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-6-ipc-跨进程通信inter-process-communication.md#Story 6.3] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/epics/epic-list.md#Epic 6] — Epic 概述和 NFR（NFR24）
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR43] — 进程组管理需求
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR24] — ≥10 并发进程表操作 ≤ 2x 延迟
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 1] — Syscall ABI 分类接口组合设计
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 2] — 进程模型与并发（SyncMap、Process 结构）
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md] — 命名模式、错误处理模式、泛型使用模式
- [Source: _bmad-output/implementation-artifacts/6-1-send-recv-messaging.md] — Story 6.1 实现细节（IPCManager 模式、emitEvent、reapProcess 集成）
- [Source: _bmad-output/implementation-artifacts/6-2-pipe.md] — Story 6.2 实现细节（VFS FD 集成、资源释放兼容性）
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文
- [Source: kernel/kernel.go] — KernelImpl 结构体、New()、GetProcess、emitEvent
- [Source: kernel/process.go] — Process 结构体（PID, PPID, State, mu, ctx, cancel, wg, groups 待新增）
- [Source: kernel/reap.go] — reapProcess 资源释放顺序
- [Source: kernel/ipc.go] — IPCManager 接口、SyscallEvent 记录模式
- [Source: kernel/ipc_test.go] — 测试辅助函数（newIPCTestProcess、newSimpleKernel）
- [Source: kernel/errors.go] — SyscallError 定义和 NewSyscallError 工厂
- [Source: internal/types/types.go] — PID、FD、Signal、ErrCode 类型定义（PGID 待新增）
- [Source: internal/xsync/] — SyncMap 泛型并发 Map

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Task 1: 新增 `PGID` 类型到 `internal/types/types.go`，创建 `kernel/procgroup.go` 定义 `ProcGroupManager` 接口，添加编译期检查
- ✅ Task 2: 实现 `ProcGroup` 结构体及 `Add/Remove/Members/Size/Contains` 方法，使用 `sync.RWMutex` 保护
- ✅ Task 3: `KernelImpl` 新增 `procGroups` 字段（`xsync.SyncMap`），`Process` 新增 `groups` 字段及 `AddGroup/RemoveGroup/GetGroups` 方法，实现 `JoinGroup`（含自动创建组、幂等加入）和 `LeaveGroup`（含自动销毁空组）
- ✅ Task 4: 实现 `GetProcGroup` 返回成员快照，组不存在返回 `ErrNotFound`
- ✅ Task 5: 实现 `SignalGroup` 遍历成员调用 `Kill`，处理部分退出场景，记录 `SyscallEvent`
- ✅ Task 6: 在 `reapProcess` 的资源释放序列中集成 `removeFromAllGroups`，位于 msgQueue.close() 之后、CtxFree 之前
- ✅ Task 7: 19 个单元测试全部通过（含 `-race`），覆盖 JoinGroup/LeaveGroup/GetProcGroup/SignalGroup/reap 清理/并发/性能
- ✅ Task 8: `make test` 全部通过、`golangci-lint` 0 issues、`go build` 成功、所有现有测试无回归

### Code Review Fixes (AI)

- **[M1] SignalGroup success_count 准确性修复** (`kernel/procgroup.go`): SignalGroup 在调用 Kill 前检查进程状态，跳过 Zombie/Dead 进程，使 SyscallEvent 中的 `success_count` 仅统计实际存活并收到信号的进程
- **[M2] 组级 SyscallEvent 发射可靠性修复** (`kernel/procgroup.go`): 新增 `findGroupEventSource` 辅助方法，遍历所有成员找到第一个仍在 procTable 中的进程来发射事件，替代原来仅依赖 `members[0]` 的不可靠方式。同时修复 GetProcGroup 和 SignalGroup
- **[M3] TestReapProcess 测试改为通过完整 reapProcess 路径** (`kernel/procgroup_test.go`): `TestReapProcess_AutoRemove` 和 `TestReapProcess_GroupAutoDestroy` 现在调用 `k.reapProcess(proc)` 而非直接调用 `removeFromAllGroups`，验证实际集成点
- **[M4] 新增 TestLeaveGroup_GroupNotFound 测试** (`kernel/procgroup_test.go`): 覆盖 LeaveGroup 对不存在组的错误路径（`procgroup.go:109-113`）

### File List

**新增文件：**
- `kernel/procgroup.go` — ProcGroupManager 接口 + ProcGroup 结构体 + JoinGroup/LeaveGroup/GetProcGroup/SignalGroup/removeFromAllGroups 实现
- `kernel/procgroup_test.go` — 20 个进程组单元测试（含新增 TestLeaveGroup_GroupNotFound）

**修改文件：**
- `internal/types/types.go` — 新增 `PGID uint64` 类型
- `kernel/kernel.go` — KernelImpl 新增 `procGroups` 字段 + `NewKernel()` 初始化
- `kernel/process.go` — Process 新增 `groups` 字段 + `AddGroup/RemoveGroup/GetGroups` 方法
- `kernel/reap.go` — reapProcess 新增进程组清理步骤（步骤 5）
