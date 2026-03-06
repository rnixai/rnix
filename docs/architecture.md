# Rnix 架构文档

本文档面向希望深入理解 Rnix 内部设计的贡献者。阅读前建议先熟悉 [核心概念](concepts.md)，这里不再复述概念定义，而是聚焦**设计决策、接口边界、数据流和扩展路径**。

> 如需查询具体 API 签名和参数细节，请参阅 [参考手册](reference.md)。
> 如需实战操作指引，请参阅 [教程](tutorials/README.md)。

---

## 目录

1. [微内核设计](#1-微内核设计)
2. [进程模型](#2-进程模型)
3. [驱动层](#3-驱动层)
4. [上下文管理](#4-上下文管理)

---

## 1. 微内核设计

### 1.1 设计哲学

Rnix 内核采用**接口组合模式**——将系统调用按功能分类为独立的子接口，由一个统一的 `KernelImpl` 结构体组合实现。这个设计选择源于 OS 隐喻与 Go 语言特性的交汇：

- **Unix 微内核隐喻**：传统微内核将进程管理、文件系统、IPC 分离为独立的服务器。Rnix 在单进程内以接口边界模拟这种分离，每个子接口各自承担一个功能域的职责。
- **Go 接口组合的天然适配**：Go 的小接口 + 组合优于大接口的哲学恰好匹配——每个子接口只定义 2~5 个方法，职责清晰、可独立测试、可独立演进。

决策记录：接口组合模式 vs 单一大接口 vs 函数集合——选择接口组合的核心理由是**扩展性**：新增一类 syscall 只需定义新接口并在 `KernelImpl` 上实现，不影响已有子接口的编译和测试。

### 1.2 KernelImpl 与子接口

`KernelImpl` 是内核的核心实现结构体，定义于 `kernel/kernel.go`：

```go
type KernelImpl struct {
    procTable   *xsync.SyncMap[types.PID, *Process]
    vfs         *vfs.VFS
    ctxMgr      *rnixctx.Manager
    callbacks   KernelCallbacks
    reapCh      chan types.PID
    stopCh      chan struct{}
    reaperWg    sync.WaitGroup
    shutdownOnce sync.Once
    msgQueues   *xsync.SyncMap[types.PID, *MessageQueue]
    msgSeq      atomic.Uint64
    procGroups  *xsync.SyncMap[types.PGID, *ProcGroup]
    mountMgr    MountManager
}
```

它通过方法集合隐式实现以下 6 个分类子接口：

| 子接口 | 方法 | 职责 |
|--------|------|------|
| **ProcessManager** | `Spawn(intent, agent, opts) (PID, error)` | 进程创建（分配 PID、上下文、FD，启动推理 goroutine） |
|  | `Kill(pid, signal) error` | 向进程发送信号（SIGTERM/SIGKILL/SIGPAUSE/SIGRESUME） |
|  | `Wait(pid) (ExitStatus, error)` | 阻塞等待进程结束并触发 reapProcess 资源释放 |
| **MountManager** | `Mount(path, config) error` | 挂载 MCP 服务器到 `/mnt/mcp/` |
|  | `Unmount(path) error` | 卸载 MCP 服务器 |
|  | `UnmountAll() error` | 卸载所有 MCP（Shutdown 时调用） |
| **IPCManager** | `Send(senderPID, targetPID, data) error` | 向目标进程发送消息 |
|  | `Recv(pid) (*Message, error)` | 阻塞接收消息 |
|  | `Pipe(writerPID, readerPID) (writeFD, readFD, error)` | 创建进程间管道 |
| **SignalManager** | `Signal(pid, sig) error` | 投递信号（含自定义 handler 分发） |
|  | `SigBlock(pid, sig) error` | 阻塞信号 |
|  | `SigUnblock(pid, sig) error` | 解除信号阻塞并投递 pending |
| **ProcGroupManager** | `JoinGroup(pid, groupID) error` | 加入进程组 |
|  | `LeaveGroup(pid, groupID) error` | 离开进程组 |
|  | `GetProcGroup(groupID) ([]PID, error)` | 查询进程组成员列表 |
|  | `SignalGroup(groupID, signal) error` | 向进程组广播信号 |
| **SupervisorManager** | `SpawnSupervisor(spec) (PID, error)` | 创建 Supervisor 树节点 |

编译时接口合规检查确保 `KernelImpl` 满足 `ProcessManager` 约束：

```go
var _ ProcessManager = (*KernelImpl)(nil)
```

### 1.3 KernelCallbacks 回调机制

`KernelCallbacks` 是内核到 CLI/UI 层的通知通道，解耦内核与展示逻辑：

```go
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent string)
    OnStep(pid types.PID, step int, total int)
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}
```

| 回调 | 触发时机 | 用途 |
|------|---------|------|
| OnSpawn | 进程注册到进程表后 | CLI 显示 `[kernel] spawning PID N...` |
| OnStep | 每次 reasonStep 循环开头 | CLI 显示 `[agent] step X/N` |
| OnComplete | finishProcess 写入 ExitStatus 后 | CLI 显示最终结果和退出码 |
| OnError | finishProcess 中 exit.Err 非 nil | CLI 显示错误信息 |

传入 `nil` 可关闭回调（静默模式），适用于测试和嵌入式集成。

### 1.4 数据流：从 Spawn 到完成

一次完整的智能体执行遵循以下数据流：

```
CLI 层                    内核层                   VFS/驱动层
  │                        │                         │
  │  Spawn(intent, agent)  │                         │
  ├───────────────────────>│                         │
  │                        │  CtxAlloc(64)           │
  │                        ├────────────────────────>│ context.Manager
  │                        │                         │
  │                        │  SetSystemPrompt(cid)   │
  │                        ├────────────────────────>│ context.Manager
  │                        │                         │
  │                        │  Open(/dev/llm/claude)  │
  │                        ├────────────────────────>│ vfs.DeviceRegistry
  │                        │                         │
  │                        │  [启动推理 goroutine]    │
  │                        │                         │
  │     OnSpawn(pid)       │                         │
  │<───────────────────────│                         │
  │                        │                         │
  │                     ┌──┤ reasonStep 循环         │
  │     OnStep(1/10)    │  │                         │
  │<────────────────────┤  │  BuildPrompt(cid)       │
  │                     │  ├────────────────────────>│ context.Manager
  │                     │  │                         │
  │                     │  │  Write(llmFD, req)      │
  │                     │  ├────────────────────────>│ /dev/llm/claude
  │                     │  │                         │
  │                     │  │  Read(llmFD)            │
  │                     │  ├────────────────────────>│ /dev/llm/claude
  │                     │  │                         │
  │                     │  │  [解析 action]           │
  │                     │  │                         │
  │                     │  │  如果 tool_call:         │
  │                     │  │  Open/Write/Read/Close   │
  │                     │  ├────────────────────────>│ /dev/fs, /dev/shell, ...
  │                     │  │                         │
  │                     │  │  AppendToolResult(cid)   │
  │                     │  ├────────────────────────>│ context.Manager
  │                     │  │                         │
  │                     └──┤ 如果 text → 完成        │
  │                        │                         │
  │                        │  finishProcess(exit)     │
  │   OnComplete(result)   │                         │
  │<───────────────────────│                         │
  │                        │                         │
  │  Wait(pid) / Reap      │                         │
  ├───────────────────────>│  reapProcess 序列       │
  │                        ├────────────────────────>│ CtxFree, CloseAll, ...
```

### 1.5 扩展路径

**添加新 syscall：**

1. 在 `kernel/kernel.go` 中定义新接口（如 `type FooManager interface { ... }`）
2. 在 `KernelImpl` 上实现方法
3. 添加编译时检查：`var _ FooManager = (*KernelImpl)(nil)`
4. 在 `debug.NewEvent` 中注册新的 syscall 名称
5. 添加 IPC method（`ipc/protocol.go`）使 CLI 可调用

**添加新设备驱动：**

1. 实现 `vfs.VFSFileFactory` 函数
2. 在 `cmd/rnix/main.go` 的初始化代码中调用 `devRegistry.Register(path, factory)`
3. VFS 自动处理 Open/Read/Write/Close 路由

---

## 2. 进程模型

### 2.1 Process 结构体设计

`Process` 定义于 `kernel/process.go`，是 Rnix 进程的完整运行时表示。字段按功能分组：

**身份与状态（不可变 / mu 保护）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| PID | `types.PID` | 全局唯一，创建后不可变 |
| PPID | `types.PID` | 父进程 PID，孤儿进程被 reparent 时可修改 |
| State | `types.ProcessState` | 状态机当前状态，mu 保护 |
| Intent | `string` | 创建时的意图描述，不可变 |
| Skills | `[]string` | 加载的 Skill 名称列表 |
| Children | `[]types.PID` | 子进程 PID 列表 |
| CreatedAt | `time.Time` | 进程创建时间（用于 elapsed 和 astrace 时间戳） |
| Exit | `*ExitStatus` | Zombie/Dead 时非 nil，记录退出状态 |

**资源与通道：**

| 字段 | 类型 | 说明 |
|------|------|------|
| FDTable | `map[types.FD]vfs.VFSFile` | 文件描述符表（VFS 内部管理实际状态） |
| DebugChan | `chan types.SyscallEvent` | 缓冲 256，astrace 追踪通道 |
| LogChan | `chan types.LogEntry` | 缓冲 256，推理日志通道 |
| Done | `chan ExitStatus` | 缓冲 1，进程退出信号 |
| CtxID | `types.CtxID` | 关联的上下文空间 ID |

**推理状态：**

| 字段 | 类型 | 说明 |
|------|------|------|
| Result | `string` | 最终推理输出 |
| TokensUsed | `int` | 累计 token 消耗 |
| ContextBudget | `int` | token 预算（0 = 无限制） |
| AllowedDevices | `[]string` | 设备白名单（nil = 全部允许） |
| MCPMounts | `[]string` | 自动挂载的 MCP 路径 |

**并发子系统（均由 mu 保护）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| groups | `[]types.PGID` | 进程组成员关系 |
| sigHandlers | `map[Signal]SignalHandler` | 自定义信号处理器 |
| blockedSignals | `map[Signal]struct{}` | 被阻塞的信号集 |
| pendingSignals | `map[Signal]struct{}` | 待投递的信号集 |
| resumeCh | `chan struct{}` | SIGPAUSE/SIGRESUME 协调 |
| threads | `map[TID]*Thread` | 线程表 |
| coroutines | `map[CoID]*Coroutine` | 协程表 |

**同步原语：**

| 字段 | 类型 | 说明 |
|------|------|------|
| mu | `sync.Mutex` | 保护所有可变状态 |
| cancel | `context.CancelFunc` | 取消推理 goroutine |
| ctx | `context.Context` | 推理 goroutine 的上下文 |
| wg | `sync.WaitGroup` | 等待推理 goroutine 完成 |
| reapOnce | `sync.Once` | 确保 reapProcess 只执行一次 |

### 2.2 状态机

进程状态严格遵循单向转移规则，不允许回退：

```
Created ──Start()──→ Running ──Terminate()──→ Zombie ──Reap()──→ Dead
```

| 转移 | 方法 | 触发条件 |
|------|------|---------|
| Created → Running | `Start()` | 推理 goroutine 启动 |
| Running → Zombie | `Terminate(exit)` | 推理完成/出错/超时/Kill/预算超限 |
| Zombie → Dead | `Reap()` | Wait 调用或自动 reaper 清理 |

状态转移逻辑使用 `validTransitions` 表驱动：

```go
var validTransitions = map[types.ProcessState][]types.ProcessState{
    types.StateCreated: {types.StateRunning},
    types.StateRunning: {types.StateZombie},
    types.StateZombie:  {types.StateDead},
}
```

非法转移返回 `*SyscallError`（`ErrInternal`）。`transitionLocked` 在持有 mu 的情况下检查转移合法性，确保并发安全。

### 2.3 PID 分配策略

PID 使用包级 `atomic.Uint64` 全局递增分配：

```go
var pidCounter atomic.Uint64

func nextPID() types.PID {
    return types.PID(pidCounter.Add(1))
}
```

设计决策：

- **不回收**：PID 单调递增，永不复用。这简化了进程引用的生命周期管理——持有旧 PID 的引用不会意外指向新进程。
- **从 1 开始**：PID 0 保留给"内核/init"虚拟进程，CLI 直接 Spawn 的顶层进程 PPID 为 0。
- **原子操作**：无需加锁，多个并发 Spawn 安全。

### 2.4 goroutine 生命周期管理

每个进程拥有一个**专属推理 goroutine**，在 Spawn 中启动：

```go
proc.wg.Add(1)
go func() {
    defer proc.wg.Done()
    defer func() { _ = k.vfs.CloseAll(proc.PID) }()
    _ = proc.Start()    // Created → Running
    k.reasonStep(proc, llmFD, opts)
}()
```

关键约束：

1. **wg 追踪**：`wg.Add(1)` 在 goroutine 启动前调用，`wg.Done()` 通过 defer 确保执行。reapProcess 中 `wg.Wait()` 等待 goroutine 退出。
2. **context.Cancel 取消**：Kill(SIGKILL) 调用 `proc.Cancel()`，reasonStep 循环在每个 step 开头检查 `proc.ctx.Done()`。
3. **defer CloseAll**：goroutine 退出前关闭所有打开的 VFS 文件描述符。
4. **SIGPAUSE/SIGRESUME**：reasonStep 在每个 step 开头调用 `proc.WaitIfPaused()`，如果 `resumeCh` 非 nil 则阻塞，直到 Resume 关闭 channel。

### 2.5 资源释放顺序

`reapProcess`（定义于 `kernel/reap.go`）执行严格的资源释放序列。通过 `reapOnce` 确保幂等——Wait 和自动 reaper 可能并发调用，只有第一个执行：

| 步骤 | 操作 | 目的 |
|------|------|------|
| 1 | `handleOrphanChildren(proc)` | Running 子进程 reparent 到 PID 0，Zombie 子进程推入 reapCh |
| 2 | `proc.Cancel()` | 取消 context，通知推理 goroutine 停止 |
| 3 | `proc.wg.Wait()` | 等待推理 goroutine 退出（其 defer 会调用 CloseAll） |
| 4 | `close(DebugChan)`, `close(LogChan)` | 先 nil 化再 close，防止与 emitEvent 竞态 |
| 5 | `msgQueue.close()` | 关闭消息队列，解除 Recv 阻塞 |
| 6 | `removeFromAllGroups` | 清理进程组成员关系 |
| 7 | `ClearSignalState()` | 清理信号 handler/blocked/pending/resumeCh |
| 8 | `ClearThreads()` | 取消所有线程并等待完成 |
| 9 | `ClearCoroutines()` | 清理协程（关闭 resumeCh，排空 yieldCh） |
| 10 | `CtxFree(CtxID)` | 释放上下文空间 |
| 11 | `proc.Reap()` | Zombie → Dead 状态转移 |
| 12 | `RemoveProcess(pid)` | 从进程表移除 |

步骤顺序至关重要：必须先处理孤儿子进程（步骤 1），再停止 goroutine（步骤 2-3），再关闭通道（步骤 4-5），最后释放资源（步骤 10-12）。

### 2.6 三级并发模型

Rnix 提供三种粒度的并发原语，映射到不同的使用场景：

| 级别 | 原语 | 调度模型 | 资源隔离 | 适用场景 |
|------|------|---------|---------|---------|
| **Process** | `Spawn` | 抢占式（独立 goroutine + context） | 独立 PID、独立 CtxID、独立 FD 表 | 独立任务 |
| **Thread** | `SpawnThread` | 抢占式（独立 goroutine，共享父 ctx） | 共享父进程上下文 | 并行子任务 |
| **Coroutine** | `SpawnCoroutine` | 协作式（yield/resume） | 共享父进程上下文 | 流式处理、状态机 |

Thread 结构体包含 TID、ParentPID、Intent、State、Done、Result、Err 以及内部同步字段（mu、cancel、ctx），通过 `context.WithCancel(parentCtx)` 派生子 context，父进程被 Kill 时子 Thread 的 context 也被取消。

Coroutine 使用 `yieldCh` / `resumeCh` 通道对实现协作式让出和恢复，支持值传递。ClearCoroutines 在进程回收时需要处理两种阻塞情况：阻塞在 `yieldCh <- value`（通过 drain goroutine 排空）和阻塞在 `<-resumeCh`（通过关闭 channel 解除）。

### 2.7 进程组与信号系统

**进程组**允许将多个进程逻辑分组，通过 `SignalGroup` 向组内所有成员广播信号，适用于 Compose 编排中的批量控制。

**信号系统**支持 5 种信号：

| 信号 | 可阻塞 | 可自定义 handler | 默认行为 |
|------|--------|-----------------|---------|
| SIGTERM | ✓ | ✓ | Cancel context |
| SIGKILL | ✗ | ✗ | 强制 Cancel |
| SIGINT | ✓ | ✓ | Cancel context |
| SIGPAUSE | ✓ | ✓ | 暂停推理循环 |
| SIGRESUME | ✓ | ✓ | 恢复推理循环 |

信号投递使用 `resolveSignalDisposition` 在单次锁持有中原子确定分发路径（blocked → pending / handler / default），避免 TOCTOU 竞态。

---

## 3. 驱动层

### 3.1 VFS 设备注册机制

VFS（虚拟文件系统）是 Rnix 的资源抽象层。所有外部资源——LLM、文件系统、Shell、MCP 工具——统一表现为可 Open/Read/Write/Close 的"文件"。

**核心抽象（`vfs/vfs.go`）：**

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}

type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**设备注册表（`vfs/dev.go`）：**

`DeviceRegistry` 使用 `xsync.Registry`（基于 `sync.Map` 的带注册/反注册语义的注册表）管理路径到工厂的映射：

```go
type DeviceRegistry struct {
    registry *xsync.Registry[VFSFileFactory]
}
```

- `Register(path, factory)`：注册设备（路径唯一，重复注册报错）
- `Unregister(path)`：反注册设备（MCP Unmount 时使用）
- `Open(path, flags)`：先精确匹配，再最长前缀匹配（`/dev/llm/claude` 匹配 `/dev/llm/claude/subpath`）

**路径解析策略：**

1. 精确匹配：`Open("/dev/fs", ...)` → factory("", flags)
2. 最长前缀匹配：`Open("/dev/fs/src/main.go", ...)` → factory("/src/main.go", flags)

这使设备驱动可以处理子路径——例如 `/dev/fs` 驱动通过 subpath 访问宿主文件系统中的任意文件。

**FD 表：**

每个进程在 VFS 层拥有独立的 FD 表（`fdTable`），FD 从 3 开始分配（0/1/2 保留给 stdin/stdout/stderr 的语义对齐）。FD 表由 VFS 管理，不是 Process 结构体直接持有——Process.FDTable 仅用于跟踪 FD 是否存在。

### 3.2 已注册设备

系统启动时在 `cmd/rnix/main.go` 中注册以下设备：

| 设备路径 | 驱动包 | 描述 |
|----------|--------|------|
| `/dev/llm/claude` | `drivers/llm` | LLM 调用（Claude Code CLI） |
| `/dev/fs` | `drivers/fs` | 宿主文件系统只读访问 |
| `/dev/shell` | `drivers/shell` | Shell 命令执行 |
| `/proc` | `vfs.ProcFS` | 动态进程信息（`/proc/{pid}/status`, `intent`, `context`） |
| `/mnt/mcp/{pid}-{server}` | 动态注册 | MCP 工具（Spawn 时自动挂载） |

### 3.3 LLMDriver 接口

LLMDriver 定义于 `drivers/llm/driver.go`，是 LLM 能力的抽象：

```go
type LLMDriver interface {
    Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
    Info() DriverInfo
}
```

**LLMRequest：**

| 字段 | 类型 | 说明 |
|------|------|------|
| Intent | `string` | 用户意图 |
| SystemPrompt | `string` | 系统提示词 |
| Model | `string` | 模型标识（空 = 驱动默认） |
| MaxTurns | `int` | 最大交互轮次 |
| TimeoutMs | `int64` | 超时毫秒数 |

**LLMResponse：**

| 字段 | 类型 | 说明 |
|------|------|------|
| Content | `string` | LLM 输出内容 |
| TokensUsed | `int` | 本次消耗 token 数 |

当前实现为 Claude Code CLI 驱动。添加新 LLM 驱动只需实现 `LLMDriver` 接口并注册到 VFS。内核 reasonStep 通过 VFS Read/Write 与 LLM 交互，不直接依赖具体驱动实现。

### 3.4 MCP 挂载机制

MCP（Model Context Protocol）集成通过动态挂载实现，使 MCP 工具以 VFS 路径暴露给智能体。

**MCPTransport 接口（`vfs/mcp.go`）：**

```go
type MCPTransport interface {
    Connect(ctx context.Context) error
    Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
    Close() error
    Ping(ctx context.Context) error
}

type TransportFactory func(config MCPConfig) (MCPTransport, error)
```

接口定义在 `vfs` 包中（而非 `drivers/mcp`），这是**依赖反转**的设计——vfs 定义接口，drivers/mcp 提供实现，避免 vfs → drivers 的反向依赖。

**MountManager（接口定义于 `kernel/kernel.go`，实现于 `vfs/mount.go` 的 `vfs.MountManager` 结构体）：**

Mount 流程：
1. `TransportFactory(config)` → 创建 transport
2. `transport.Connect(ctx)` → 建立连接（500ms 超时，NFR25）
3. `mcpFileFactory(transport)` → 创建 VFSFileFactory
4. `devReg.Register(path, factory)` → 注册到设备注册表
5. 存储 mount 记录

Unmount 流程：
1. 从 mounts 表移除
2. `transport.Close()` → 关闭连接
3. `devReg.Unregister(path)` → 从设备注册表移除

**VFS 子路径映射：**

挂载点下的子路径映射到 MCP 协议操作：

| VFS 路径 | MCP 操作 | Read 行为 | Write 行为 |
|----------|----------|----------|------------|
| `/mnt/mcp/{mount}/` | — | 返回 `["tools","resources"]` | — |
| `/mnt/mcp/{mount}/tools` | `tools/list` | 返回工具列表 | — |
| `/mnt/mcp/{mount}/tools/{name}` | `tools/call` | 返回上次调用结果 | 发起工具调用 |
| `/mnt/mcp/{mount}/resources` | `resources/list` | 返回资源列表 | — |
| `/mnt/mcp/{mount}/resources/{uri}` | `resources/read` | 读取资源内容 | — |

### 3.5 Agent 自动挂载生命周期

Agent 的 `agent.yaml` 可声明 MCP 依赖。Spawn 时自动处理：

1. **挂载**：遍历 `agent.MCPConfigs`，为每个 MCP 服务器执行 `Mount("/mnt/mcp/{pid}-{serverName}", config)`
2. **白名单注入**：挂载路径自动添加到 `proc.AllowedDevices`
3. **失败回滚**：任一 MCP 挂载失败则回滚已挂载的路径，释放上下文，返回错误
4. **自动卸载**：`finishProcess` 在终止进程前逐个调用 `Unmount`，卸载失败不阻塞进程退出

---

## 4. 上下文管理

### 4.1 Context 结构体

`Context` 定义于 `context/context.go`，表示一个独立的对话空间：

```go
type Context struct {
    ID           types.CtxID
    SystemPrompt string
    Messages     []Message
    MaxSize      int
    mu           sync.RWMutex
}

type Message struct {
    Role       Role   `json:"role"`
    Content    string `json:"content"`
    ToolCallID string `json:"tool_call_id,omitempty"`
}
```

Role 枚举：`system`、`user`、`assistant`、`tool`。

MaxSize 限制 Messages 切片长度（消息数量）。当前 MVP 不限制单条消息的字节大小。

### 4.2 Manager 方法

`Manager` 管理上下文的完整生命周期，方法分为三类：

**分配与释放：**

| 方法 | 签名 | 说明 |
|------|------|------|
| CtxAlloc | `(size int) (CtxID, error)` | 分配上下文，size 为消息容量 |
| CtxFree | `(cid CtxID) error` | 释放上下文（reapProcess 步骤 10） |

**内容操作：**

| 方法 | 签名 | 说明 |
|------|------|------|
| SetSystemPrompt | `(cid, prompt) error` | 设置/更新系统提示词 |
| AppendMessage | `(cid, role, content) error` | 追加对话消息 |
| AppendToolResult | `(cid, toolCallID, content) error` | 追加工具执行结果 |
| CtxWrite | `(cid, offset, data) error` | 低级写入（offset=0 追加，>0 覆盖） |
| CtxRead | `(cid, offset, length) ([]byte, error)` | 低级读取（JSON 序列化） |

**查询：**

| 方法 | 签名 | 说明 |
|------|------|------|
| BuildPrompt | `(cid) (*PromptResult, error)` | 组装完整 LLM prompt |
| GetContextSummary | `(ctxID) (string, error)` | `/proc/{pid}/context` 的摘要 |

### 4.3 Prompt 组装流程

`BuildPrompt` 返回 `PromptResult`，包含 `SystemPrompt` 和 `Messages` 两个字段。reasonStep 将其组装为 LLM 请求：

1. **系统提示词构建**（Spawn 阶段）：
   - `Agent.SystemPrompt()` = instructions.md 内容 + 所有激活 Skill 的 body 注入
   - 如果 SpawnOpts 也提供了 SystemPrompt，拼接：`opts.SystemPrompt + "\n\n" + agentPrompt`

2. **消息历史累积**（reasonStep 循环）：
   - 初始：AppendMessage(user, intent)
   - 每轮 LLM 响应：AppendMessage(assistant, resp.Content)
   - 工具调用结果：AppendToolResult(toolPath, result)

3. **发送给 LLM**：
   - `BuildPrompt(cid)` → PromptResult（SystemPrompt + Messages 快照）
   - 序列化为 `llmRequest{Intent, SystemPrompt, Model, Messages}`
   - 写入 LLM VFS 设备

### 4.4 Token 预算管理

Token 预算防止单个进程过度消耗 LLM 资源。

**预算来源优先级（从高到低）：**

1. `SpawnOpts.ContextBudget`（CLI `--budget` 或 Compose 配置）
2. `AgentManifest.ContextBudget`（agent.yaml 中的配置）
3. 0（无限制）

负数预算在 Spawn 时规范化为 0。

**执行逻辑**（在 reasonStep 循环中）：

```
每次 LLM Read 返回后:
    proc.TokensUsed += resp.TokensUsed

    if budget > 0 && TokensUsed >= ContextBudget:
        emitLog("Token budget exceeded: N/M")
        emitEvent(action: "budget_exceeded")
        finishProcess(ExitStatus{Code: 2, Reason: "budget_exceeded"})
        return
```

退出码约定：
- **0** — 正常完成
- **1** — 错误（LLM 失败、工具失败、超时等）
- **2** — 预算超限（`budget_exceeded`）

### 4.5 上下文与进程的生命周期绑定

上下文的生命周期严格绑定到拥有它的进程：

| 进程事件 | 上下文操作 |
|---------|-----------|
| Spawn 开始 | `CtxAlloc(64)` 分配上下文 |
| Spawn 失败（MCP 挂载出错等） | `CtxFree(cid)` 立即释放 |
| reasonStep 循环 | 持续 AppendMessage / BuildPrompt |
| reapProcess 步骤 10 | `CtxFree(cid)` 最终释放 |

Thread 和 Coroutine 共享父进程的上下文（通过 CtxID），不独立分配。这意味着并发线程对同一上下文的 AppendMessage 调用由 `Context.mu` 序列化，保证消息顺序一致性。

---

## 延伸阅读

- [核心概念](concepts.md) — 建立 Rnix 的心智模型
- [参考手册](reference.md) — 精确的 API 签名和参数细节
- [教程](tutorials/README.md) — 手把手实战操作
  - [编写第一个 Skill](tutorials/writing-first-skill.md)
  - [调试第一个 bug](tutorials/debugging-first-bug.md)
  - [组合多智能体工作流](tutorials/composing-multi-agent-workflow.md)
