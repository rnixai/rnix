# Crux 参考手册

本手册是 Crux 的权威技术参考，面向使用 Crux 编写 Agent/Skill 或调试问题的开发者。文档中所有签名、参数、返回值、路径和协议均精确匹配当前代码实现。

> 如需了解 Crux 的设计哲学和核心概念，请参阅 [核心概念文档](concepts.md)。
> 如需快速安装和首次运行指引，请参阅 [快速上手指南](quick-start.md)。

---

## 目录

1. [Syscall 参考](#1-syscall-参考)
   - [1.1 概述](#11-概述)
   - [1.2 进程管理（ProcessManager）](#12-进程管理processmanager)
   - [1.3 上下文管理（ContextManager）](#13-上下文管理contextmanager)
   - [1.4 文件系统（FileSystem）](#14-文件系统filesystem)
   - [1.5 调试（Debugger）](#15-调试debugger)
2. [VFS 路径规范](#2-vfs-路径规范)
   - [2.1 概述](#21-概述)
   - [2.2 /dev/llm/claude — LLM 驱动设备](#22-devllmclaude--llm-驱动设备)
   - [2.3 /dev/fs — 宿主文件系统设备](#23-devfs--宿主文件系统设备)
   - [2.4 /dev/shell — Shell 执行设备](#24-devshell--shell-执行设备)
   - [2.5 /proc/{pid}/ — 动态进程信息](#25-procpid--动态进程信息)
   - [2.6 /lib/agents/ 和 /lib/skills/](#26-libagents-和-libskills)
   - [2.7 VFSFile 接口和 OpenFlag 枚举](#27-vfsfile-接口和-openflag-枚举)
   - [2.8 FD 分配规则](#28-fd-分配规则)
3. [Agent 和 Skill 清单](#3-agent-和-skill-清单)
   - [3.1 agent.yaml 字段说明](#31-agentyaml-字段说明)
   - [3.2 AgentModels 子结构](#32-agentmodels-子结构)
   - [3.3 instructions.md 格式](#33-instructionsmd-格式)
   - [3.4 Agent 加载流程](#34-agent-加载流程)
   - [3.5 SKILL.md 格式](#35-skillmd-格式)
   - [3.6 SkillManifest 字段](#36-skillmanifest-字段)
   - [3.7 渐进式加载策略](#37-渐进式加载策略)
   - [3.8 完整示例](#38-完整示例)
4. [CLI 命令参考](#4-cli-命令参考)
   - [4.1 全局 Flags](#41-全局-flags)
   - [4.2 crux \[intent\] — 根命令](#42-crux-intent--根命令)
   - [4.3 crux ps — 进程列表](#43-crux-ps--进程列表)
   - [4.4 crux kill — 进程终止](#44-crux-kill-pid--进程终止)
   - [4.5 crux astrace — Syscall 追踪](#45-crux-astrace-pid--syscall-追踪)
   - [4.6 crux version — 版本信息](#46-crux-version--版本信息)
   - [4.7 JSON 响应格式](#47-json-响应格式)
5. [IPC 架构](#5-ipc-架构)
   - [5.1 Daemon 生命周期](#51-daemon-生命周期)
   - [5.2 Socket 路径规则](#52-socket-路径规则)
   - [5.3 NDJSON 协议](#53-ndjson-协议)
   - [5.4 Method 枚举](#54-method-枚举)
   - [5.5 StreamEvent 流式协议](#55-streamevent-流式协议)
   - [5.6 连接复用语义](#56-连接复用语义)
   - [5.7 Spawn 流式协议示例](#57-spawn-流式协议示例)
   - [5.8 AttachDebug 流式协议示例](#58-attachdebug-流式协议示例)
6. [错误处理与类型参考](#6-错误处理与类型参考)
   - [6.1 ErrCode 枚举](#61-errcode-枚举)
   - [6.2 SyscallError](#62-syscallerror)
   - [6.3 VFSError](#63-vfserror)
   - [6.4 DriverError](#64-drivererror)
   - [6.5 ContextError](#65-contexterror)
   - [6.6 基础类型](#66-基础类型)
7. [进程模型参考](#7-进程模型参考)
   - [7.1 ProcessState 状态机](#71-processstate-状态机)
   - [7.2 状态转移规则](#72-状态转移规则)
   - [7.3 ExitStatus 结构](#73-exitstatus-结构)
   - [7.4 资源释放顺序](#74-资源释放顺序)
   - [7.5 Signal 定义](#75-signal-定义)

---

## 1. Syscall 参考

### 1.1 概述

Crux 的内核接口由 4 个子接口组合而成，共定义 15 个 syscall：

| 子接口 | Syscall 数量 | 职责 |
|--------|-------------|------|
| ProcessManager | 5 | 进程创建、终止、等待、查询 |
| ContextManager | 4 | 上下文空间分配、读写、释放 |
| FileSystem | 5 | VFS 设备的打开、读写、关闭、元数据查询 |
| Debugger | 1 | Syscall 事件的自动记录与追踪 |

所有 syscall 在出错时返回结构化的 `*SyscallError`（见 [6.2 SyscallError](#62-syscallerror)），包含 syscall 名称、PID、设备路径、底层错误和分类错误码。

所有 syscall 的入口和出口会自动记录 `SyscallEvent` 到进程的 `DebugChan`（见 [1.5 调试](#15-调试debugger)）。

### 1.2 进程管理（ProcessManager）

#### Spawn

创建并启动一个智能体进程，自动进入推理循环。

```
签名: Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (PID, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `intent` | `string` | 用户意图字符串 |
| `agent` | `*agents.AgentInfo` | Agent 定义（可选，`nil` 表示通用模式） |
| `opts` | `SpawnOpts` | 配置选项（见下表） |

**SpawnOpts 字段：**

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Model` | `string` | `""` | LLM 模型名称（优先级：CLI > Agent manifest > 驱动默认） |
| `SystemPrompt` | `string` | `""` | 系统提示词（非空时追加到 Agent instructions 之后） |
| `MaxTurns` | `int` | `0` | 最大推理步数（`0` = 使用默认值 `DefaultMaxSteps=10`） |
| `TimeoutMs` | `int64` | `0` | 超时毫秒数 |
| `ParentPID` | `PID` | `0` | 父进程 PID（`0` = 顶层 CLI 级 spawn） |

**返回值：** `(PID, error)`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 父进程不存在（`ParentPID > 0` 但查找失败） |
| `INTERNAL` | 上下文分配失败或系统提示词设置失败 |
| `DRIVER` | LLM 设备 `/dev/llm/claude` 打开失败 |

**行为：**

1. 创建 `Process`（分配 PID，记录 Skills、AllowedDevices）
2. 维护父子关系（`ParentPID > 0` 时注册到父进程 Children 列表）
3. 聚合 Agent 的 `SystemPrompt()` 和 `AllowedTools()`
4. `CtxAlloc(64)` → 分配上下文空间
5. `SetSystemPrompt` + `AppendMessage(user, intent)` → 初始化上下文
6. `Open("/dev/llm/claude", O_RDWR)` → 获取 LLM 设备 FD
7. 启动 goroutine → `Created → Running` → 进入 `reasonStep` 循环
8. 触发 `OnSpawn` 回调通知

**示例：**

```go
pid, err := kern.Spawn("分析代码", agentInfo, kernel.SpawnOpts{
    Model:    "sonnet",
    MaxTurns: 5,
})
```

---

#### Kill

向目标进程发送终止信号。

```
签名: Kill(pid PID, signal Signal) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `pid` | `PID` | 目标进程 ID |
| `signal` | `Signal` | `SIGTERM(1)` 或 `SIGKILL(2)` |

**返回值：** `error`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 进程不存在 |
| `INVALID` | 无效信号值（非 SIGTERM 或 SIGKILL） |

**幂等性：** Kill 已处于 Zombie 或 Dead 状态的进程为 no-op，不返回错误。

**行为：** 调用进程的 `Cancel()` 取消 context，导致推理 goroutine 中的 LLM 调用被中断。

**示例：**

```go
err := kern.Kill(1, types.SIGTERM)
```

---

#### Wait

阻塞等待目标进程进入 Zombie 状态，然后执行完整的资源释放序列。

```
签名: Wait(pid PID) (ExitStatus, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `pid` | `PID` | 目标进程 ID |

**返回值：** `(ExitStatus, error)`

`ExitStatus` 结构：

| 字段 | 类型 | 说明 |
|------|------|------|
| `Code` | `int` | 退出码（`0` = 正常，非零 = 异常） |
| `Reason` | `string` | 人类可读的退出原因 |
| `Err` | `error` | 底层错误（正常退出时为 `nil`） |

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 进程不存在 |

**行为：** 阻塞读取 `proc.Done` 通道，收到退出状态后触发 `reapProcess`（资源释放序列，见 [7.4 资源释放顺序](#74-资源释放顺序)）。`reapProcess` 通过 `sync.Once` 保证幂等——即使 Wait 和后台 reaper 并发调用也只执行一次。

**示例：**

```go
exit, err := kern.Wait(1)
fmt.Printf("exit code: %d, reason: %s\n", exit.Code, exit.Reason)
```

---

#### ListProcs

返回所有进程的快照列表。

```
签名: ListProcs() []ProcInfo
```

**参数：** 无

**返回值：** `[]ProcInfo`

**ProcInfo 结构：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `PID` | `PID` | 进程 ID |
| `PPID` | `PID` | 父进程 ID |
| `State` | `ProcessState` | 进程状态 |
| `Intent` | `string` | 用户意图 |
| `Skills` | `[]string` | Skill 名称列表 |
| `TokensUsed` | `int` | 累计 token 消耗 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `CtxID` | `CtxID` | 上下文 ID |
| `Result` | `string` | 最终输出结果 |
| `AllowedDevices` | `[]string` | 设备权限白名单 |

**行为：** 遍历进程表，对每个进程加锁读取快照。返回值是值拷贝，不包含对进程对象的引用。

**示例：**

```go
procs := kern.ListProcs()
for _, p := range procs {
    fmt.Printf("PID %d: %s (%s)\n", p.PID, p.Intent, p.State)
}
```

---

#### GetPID

获取当前进程 PID。

> **状态：** 延迟实现。当前通过 `ListProcs()` 替代。

---

### 1.3 上下文管理（ContextManager）

#### CtxAlloc

分配一个新的上下文空间。

```
签名: CtxAlloc(size int) (CtxID, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `size` | `int` | 最大消息数量 |

**返回值：** `(CtxID, error)`

**默认值：** `DefaultCtxSize = 64`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `INTERNAL` | `size <= 0` |

**行为：** 分配全局递增的 `CtxID`，创建空的 `Context` 对象（`Messages` 为空切片，`MaxSize` 为指定值）。

**示例：**

```go
cid, err := ctxMgr.CtxAlloc(64)
```

---

#### CtxRead

读取上下文内容。

```
签名: CtxRead(cid CtxID, offset int, length int) ([]byte, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `cid` | `CtxID` | 上下文 ID |
| `offset` | `int` | 消息起始索引（0-based） |
| `length` | `int` | 读取消息数量 |

**特殊用法：** `offset=0, length=0` 读取全部内容。

**返回值：** `([]byte, error)` — JSON 序列化的上下文

**返回格式：**

```json
{
  "system_prompt": "...",
  "messages": [
    {"role": "user", "content": "..."},
    {"role": "assistant", "content": "..."}
  ]
}
```

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 上下文不存在 |
| `INTERNAL` | JSON 序列化失败 |

**示例：**

```go
data, err := ctxMgr.CtxRead(cid, 0, 0) // 读取全部
```

---

#### CtxWrite

向上下文写入消息。

```
签名: CtxWrite(cid CtxID, offset int, data []byte) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `cid` | `CtxID` | 上下文 ID |
| `offset` | `int` | `0` = 追加新消息；`1..N` = 覆写第 `offset-1` 个消息（1-based） |
| `data` | `[]byte` | JSON 序列化的 `Message` |

**Message 格式：**

```json
{"role": "system|user|assistant|tool", "content": "...", "tool_call_id": "..."}
```

**Role 枚举：** `system`、`user`、`assistant`、`tool`

**返回值：** `error`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 上下文不存在 |
| `INTERNAL` | JSON 解析失败、容量已满（`offset=0` 时）、offset 越界（`offset < 1` 或 `offset > len(Messages)`） |

**示例：**

```go
msg := `{"role": "user", "content": "分析代码"}`
err := ctxMgr.CtxWrite(cid, 0, []byte(msg)) // 追加消息
```

---

#### CtxFree

释放上下文空间。

```
签名: CtxFree(cid CtxID) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `cid` | `CtxID` | 上下文 ID |

**返回值：** `error`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 上下文不存在 |

**示例：**

```go
err := ctxMgr.CtxFree(cid)
```

---

### 1.4 文件系统（FileSystem）

#### Open

打开 VFS 设备路径，返回文件描述符。

```
签名: Open(pid PID, path string, flags OpenFlag) (FD, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `pid` | `PID` | 进程 ID |
| `path` | `string` | VFS 路径（如 `/dev/llm/claude`） |
| `flags` | `OpenFlag` | `O_RDONLY(0)`、`O_WRONLY(1)`、`O_RDWR(2)` |

**返回值：** `(FD, error)` — FD 从 3 开始递增

**路径匹配规则：**

1. **精确匹配** — 路径完全一致（如 `/dev/shell`）
2. **最长前缀匹配** — 选择最长前缀，剩余部分作为 subpath 传给设备工厂
   - 例：`/dev/fs/path/to/file` → 匹配 `/dev/fs`，subpath = `/path/to/file`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 设备不存在 |
| `DRIVER` | 设备工厂创建文件失败 |

**示例：**

```go
fd, err := v.Open(pid, "/dev/llm/claude", vfs.O_RDWR)
```

---

#### Read

从文件描述符读取数据。

```
签名: Read(pid PID, fd FD, length int) ([]byte, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `pid` | `PID` | 进程 ID |
| `fd` | `FD` | 文件描述符 |
| `length` | `int` | 最大读取字节数 |

**返回值：** `([]byte, error)`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | FD 无效（进程无 FDTable 或 FD 不存在） |
| `DRIVER` | 驱动读取失败 |

**示例：**

```go
data, err := v.Read(pid, fd, 65536)
```

---

#### Write

向文件描述符写入数据。

```
签名: Write(ctx context.Context, pid PID, fd FD, data []byte) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 支持取消（Kill 信号中断 LLM 调用） |
| `pid` | `PID` | 进程 ID |
| `fd` | `FD` | 文件描述符 |
| `data` | `[]byte` | 写入数据 |

**返回值：** `error`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | FD 无效 |
| `DRIVER` | 驱动写入失败 |

> `Write` 接受 `context.Context` 参数以支持 Kill 时中断正在进行的 LLM 调用。这是 VFS 中唯一需要 `ctx` 参数的操作。

**示例：**

```go
err := v.Write(ctx, pid, fd, []byte(`{"intent":"分析代码"}`))
```

---

#### Close

关闭文件描述符。

```
签名: Close(pid PID, fd FD) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `pid` | `PID` | 进程 ID |
| `fd` | `FD` | 文件描述符 |

**返回值：** `error`

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | FD 无效 |
| `DRIVER` | 驱动关闭失败 |

**行为：** 调用设备的 `Close()` 方法并从 FDTable 中原子移除 FD。

**示例：**

```go
err := v.Close(pid, fd)
```

---

#### Stat

查询路径元数据。

```
签名: Stat(path string) (FileStat, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `path` | `string` | VFS 路径 |

**返回值：** `(FileStat, error)`

**FileStat 结构：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 路径名称 |
| `Size` | `int64` | 文件大小 |
| `IsDevice` | `bool` | 是否为设备 |
| `DevicePath` | `string` | 匹配的设备注册路径 |

**错误码：**

| 错误码 | 触发条件 |
|--------|---------|
| `NOT_FOUND` | 设备不存在 |
| `DRIVER` | 元数据获取失败 |

**示例：**

```go
stat, err := v.Stat("/dev/llm/claude")
```

---

### 1.5 调试（Debugger）

#### SyscallEvent 自动记录

所有 syscall 的入口和出口会自动记录为 `SyscallEvent`，通过进程的 `DebugChan`（缓冲 256）传递。

**事件创建：**

```go
event := debug.NewEvent(pid, createdAt, syscall, args)
```

**事件完成：**

```go
debug.CompleteEvent(&event, result, err, duration)
```

**SyscallEvent 结构：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `Timestamp` | `time.Duration` | 相对于进程创建时间的偏移量 |
| `PID` | `PID` | 进程 ID |
| `Syscall` | `string` | 与接口方法名一致（`"Spawn"`、`"Open"`、`"CtxWrite"` 等） |
| `Args` | `map[string]any` | 调用参数快照 |
| `Result` | `any` | 返回值 |
| `Err` | `error` | 错误信息 |
| `Duration` | `time.Duration` | 执行耗时 |

**传递机制：**

- 通过 `debug.EmitEvent(ch, event)` 非阻塞写入 `DebugChan`
- 缓冲满时静默丢弃（不阻塞 syscall 执行）
- `DebugChan` 为 `nil` 时跳过（零开销）
- 关闭前先将 `proc.DebugChan` 置 `nil`（持锁操作），防止并发写入

**消费方式：** 通过 IPC `attach_debug` 方法流式获取（见 [5.8 AttachDebug 流式协议示例](#58-attachdebug-流式协议示例)）。

---

## 2. VFS 路径规范

### 2.1 概述

VFS（虚拟文件系统）是 Crux 的统一资源抽象层，遵循 Unix "一切皆文件"的哲学。所有外部资源通过 VFS 设备路径访问。

**设备模型：** 每个 VFS 路径映射到一个 `VFSFileFactory`，由 `DeviceRegistry` 管理注册和查找。

**路径匹配机制：**

1. **精确匹配** — 路径与注册路径完全一致
2. **最长前缀匹配** — 路径以注册路径开头，选择最长前缀；剩余部分作为 `subpath` 传递给设备工厂

**已注册设备路径：**

| VFS 路径 | 驱动模块 | 匹配方式 | 说明 |
|---------|---------|---------|------|
| `/dev/llm/claude` | `drivers/llm` | 精确匹配 | Claude Code CLI 调用 |
| `/dev/fs` | `drivers/fs` | 前缀匹配 | 宿主文件系统（subpath 作为文件路径） |
| `/dev/shell` | `drivers/shell` | 精确匹配 | Shell 命令执行 |
| `/proc` | `vfs/proc.go` | 前缀匹配 | 动态进程信息 |

设备注册在 daemon 启动时通过依赖注入完成（`cmd/crux/main.go`）。

### 2.2 /dev/llm/claude — LLM 驱动设备

**路径：** `/dev/llm/claude`
**驱动：** `drivers/llm.ClaudeCliDriver`
**匹配：** 精确匹配

**Write 请求格式（JSON）：**

```json
{
  "intent": "分析代码",
  "system_prompt": "...",
  "model": "sonnet",
  "max_turns": 1,
  "timeout_ms": 30000,
  "messages": [{"role": "user", "content": "..."}]
}
```

**Read 响应格式（JSON）：**

```json
{
  "content": "LLM 响应内容",
  "tokens_used": 1234
}
```

**底层实现：** 每次 Write 调用 = 一次 `exec.CommandContext` 执行 `claude -p` CLI。支持 context 取消（Kill 信号中断）。

### 2.3 /dev/fs — 宿主文件系统设备

**路径：** `/dev/fs`
**驱动：** `drivers/fs.HostFSDriver`
**匹配：** 前缀匹配

**路径解析：** `/dev/fs/path/to/file` → subpath = `/path/to/file` → 映射到宿主文件系统路径

**操作：**

- **Write** — 写入操作参数（文件路径、读取请求等）
- **Read** — 读取文件内容
- **Close** — 释放资源

### 2.4 /dev/shell — Shell 执行设备

**路径：** `/dev/shell`
**驱动：** `drivers/shell.ShellDriver`
**匹配：** 精确匹配

**操作：**

- **Write** — 写入 Shell 命令
- **Read** — 读取命令执行结果
- **Close** — 释放资源

**底层实现：** 通过 `exec.CommandContext` 执行 Shell 命令，继承当前用户权限。

### 2.5 /proc/{pid}/ — 动态进程信息

**路径：** `/proc`
**驱动：** `vfs.ProcFS`
**匹配：** 前缀匹配

**只读文件系统** — Write 操作返回 `PERMISSION` 错误。

**子路径：**

| 子路径 | 格式 | 内容 |
|--------|------|------|
| `/proc/{pid}/status` | JSON | 进程状态快照 |
| `/proc/{pid}/intent` | 纯文本 | 原始意图字符串 |
| `/proc/{pid}/context` | 纯文本 | 上下文摘要 |

**`/proc/{pid}/status` JSON 格式：**

```json
{
    "pid": 1,
    "ppid": 0,
    "state": "running",
    "intent": "分析代码",
    "skills": ["code-analysis"],
    "tokens_used": 456,
    "elapsed_ms": 3200,
    "allowed_devices": ["/dev/fs", "/dev/shell"]
}
```

**路径解析规则：** subpath 格式为 `/{pid}/{file}`，其中 `{file}` 必须是 `status`、`intent` 或 `context` 之一。

**快照语义：** 内容在 Open 时生成快照，后续 Read 读取快照数据。

### 2.6 /lib/agents/ 和 /lib/skills/

这两个路径是 Agent 和 Skill 的文件系统存储位置，由 `AgentLoader` 和 `SkillLoader` 直接读取（不通过 VFS 设备机制）。

**Agent 目录结构：**

```
lib/agents/{agent-name}/
├── agent.yaml        # Agent 配置清单
└── instructions.md   # Agent 角色指令（系统提示词）
```

**Skill 目录结构：**

```
lib/skills/{skill-name}/
└── SKILL.md          # Skill 定义（YAML frontmatter + Markdown body）
```

### 2.7 VFSFile 接口和 OpenFlag 枚举

所有设备驱动必须实现 `VFSFile` 接口：

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

**VFSFileFactory 签名：**

```go
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**OpenFlag 枚举：**

| 常量 | 值 | 说明 |
|------|-----|------|
| `O_RDONLY` | `0` | 只读 |
| `O_WRONLY` | `1` | 只写 |
| `O_RDWR` | `2` | 读写 |

### 2.8 FD 分配规则

- **起始值：** 3（0/1/2 预留给 stdin/stdout/stderr）
- **分配方式：** 每进程独立 `fdTable`，内部 `nextFD` 计数器单调递增
- **作用域：** 每个 `Process` 拥有独立的 `FDTable`
- **释放：** `Close` 从 `fdTable` 中原子移除 FD；进程退出时 `CloseAll` 关闭所有打开的 FD

---

## 3. Agent 和 Skill 清单

### 3.1 agent.yaml 字段说明

`AgentManifest` 结构定义了 Agent 的配置清单：

| 字段 | 类型 | 是否必需 | 说明 |
|------|------|---------|------|
| `name` | `string` | 必需 | Agent 名称（唯一标识符） |
| `description` | `string` | 可选 | Agent 描述 |
| `models` | `AgentModels` | 可选 | LLM 模型偏好 |
| `context_budget` | `int` | 可选 | 上下文预算（token 数） |
| `skills` | `[]string` | 可选 | 引用的 Skill 名称列表 |

### 3.2 AgentModels 子结构

| 字段 | 类型 | 说明 |
|------|------|------|
| `provider` | `string` | LLM 提供商（如 `claude`） |
| `preferred` | `string` | 首选模型（如 `sonnet`） |
| `fallback` | `string` | 备用模型（如 `haiku`） |

**模型选择优先级：** CLI `--model` flag > Agent manifest `preferred` > 驱动默认值

### 3.3 instructions.md 格式

纯 Markdown 文件，包含 Agent 的角色定义和系统提示词。内容将作为 LLM 系统提示词的一部分。

**拼接规则：** `SystemPrompt() = Agent instructions.md + "\n\n" + Skill A body + "\n\n" + Skill B body + ...`

### 3.4 Agent 加载流程

`AgentLoader.Load(agentName)` 执行以下步骤：

1. **路径安全检查** — 防止目录遍历攻击（检查路径不逃逸出基目录）
2. **读取 agent.yaml** — 解析为 `AgentManifest`
3. **验证必需字段** — `name` 字段必须非空
4. **读取 instructions.md** — 作为系统提示词文本
5. **加载引用的 Skills** — 遍历 `manifest.Skills`，对每个调用 `skillLoader.LoadFull(skillName)`
6. **返回 AgentInfo** — 包含 `Manifest`、`Instructions`、`Skills` 三部分

### 3.5 SKILL.md 格式

SKILL.md 采用 Agent Skills 行业标准格式：YAML frontmatter + Markdown body。

```markdown
---
name: skill-name
description: >
  多行描述文本
allowed-tools: /dev/fs /dev/shell
metadata:
  key: value
---

# Markdown Body（程序性知识）

操作指南、工作流描述等内容...
```

**解析规则：**

1. 文件必须以 `---` 开头
2. 两个 `---` 之间为 YAML frontmatter
3. 第二个 `---` 之后为 Markdown body
4. 不以 `---` 开头 → 错误 `"SKILL.md must start with ---"`
5. 缺少结束 `---` → 错误 `"SKILL.md missing closing ---"`

### 3.6 SkillManifest 字段

| 字段 | YAML 键 | 类型 | 是否必需 | 说明 |
|------|---------|------|---------|------|
| `Name` | `name` | `string` | 必需 | Skill 名称 |
| `Description` | `description` | `string` | 可选 | Skill 描述 |
| `AllowedToolsRaw` | `allowed-tools` | `string` | 关键字段 | 空格分隔的 VFS 设备路径 |
| `Metadata` | `metadata` | `map[string]string` | 可选 | 任意键值对 |

**AllowedTools() 解析：**

- `"/dev/fs /dev/shell"` → `["/dev/fs", "/dev/shell"]`
- 空字符串 → `nil`（无限制，可访问所有设备）

### 3.7 渐进式加载策略

Crux 对 Skill 提供两级加载粒度：

| 方法 | 加载内容 | 估算 Token | 用途 |
|------|---------|-----------|------|
| `LoadMetadata(skillName)` | 仅 YAML frontmatter | ~100 | 发现阶段（枚举名称、描述、权限） |
| `LoadFull(skillName)` | frontmatter + Markdown body | < 5000 | 激活阶段（注入系统提示词） |

### 3.8 完整示例

**agent.yaml 示例（`lib/agents/code-analyst/agent.yaml`）：**

```yaml
name: code-analyst
description: "分析代码质量、识别潜在问题并提供改进建议的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 8192
skills:
  - code-analysis
```

**SKILL.md 示例（`lib/skills/code-analysis/SKILL.md`）：**

```markdown
---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities.
allowed-tools: /dev/fs /dev/shell
metadata:
  author: crux
  version: "1.0"
---

# Code Analysis

## When to use this skill
...
```

---

## 4. CLI 命令参考

### 4.1 全局 Flags

| Flag | 短选项 | 类型 | 说明 |
|------|--------|------|------|
| `--json` | — | `bool` | JSON 格式输出 |
| `--verbose` | `-v` | `bool` | 详细输出 |
| `--quiet` | `-q` | `bool` | 静默输出 |

**输出模式优先级：** `--json` > `--quiet` > `--verbose` > 默认

这三个 flag 通过 `PersistentFlags` 注册，对所有子命令生效。

### 4.2 crux [intent] — 根命令

```
用法: crux [intent]
参数: [intent] — 任意长度意图字符串（多个参数以空格拼接）
```

**私有 Flags：**

| Flag | 短选项 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--model` | `-m` | `string` | `""` | LLM 模型（`sonnet`/`opus`/`haiku`） |
| `--max-steps` | — | `int` | `0` | 最大推理步数（`0` = 默认 10） |
| `--agent` | — | `string` | `""` | Agent 定义名称 |

**默认输出示例：**

```
[kernel] spawning PID 1...
[agent/1] reasoning step 1...
[agent/1] reasoning step 2...
══ Result ══════════════════════════════════════════════════════════════════════
  分析结果内容...
════════════════════════════════════════════════════════════════════════════════
[kernel] PID 1 exited(0) | tokens: 1234 | elapsed: 6.2s
```

**JSON 成功响应：**

```json
{"ok": true, "data": {"pid": 1, "result": "...", "tokens_used": 1234, "elapsed_ms": 6200, "exit_code": 0}}
```

**JSON 错误响应：**

```json
{"ok": false, "error": {"code": "TIMEOUT", "message": "...", "syscall": "Write", "device": "/dev/llm/claude"}}
```

### 4.3 crux ps — 进程列表

```
用法: crux ps
参数: 无 (cobra.NoArgs)
```

**四种输出模式：**

**默认模式 — 表格格式：**

```
  PID   STATE     SKILL              TOKENS   ELAPSED
─────   ─────────   ───────────────   ────────   ────────
    1   running   code-analysis        456      3.2s
    2   zombie    —                    123      1.1s

1 active, 1 zombie, 2 total
```

**--verbose — 含额外字段：**

含 PPID、Intent 列。

**--quiet — 逐行 PID：**

```
1
2
```

**--json — 结构化 JSON：**

```json
{
  "ok": true,
  "data": {
    "processes": [
      {
        "pid": 1,
        "ppid": 0,
        "state": "running",
        "intent": "分析代码",
        "skills": ["code-analysis"],
        "tokens_used": 456,
        "elapsed_ms": 3200
      }
    ]
  }
}
```

**无活跃进程时：** `No active processes.`

### 4.4 crux kill \<pid\> — 进程终止

```
用法: crux kill <pid>
参数: <pid> — 进程 ID（十进制数字，恰好 1 个参数）
信号: 固定发送 SIGTERM(1)
```

**成功输出：**

```
[kernel] PID 1: signal sent (SIGTERM)
```

### 4.5 crux astrace \<pid\> — Syscall 追踪

```
用法: crux astrace <pid>
参数: <pid> — 进程 ID（恰好 1 个参数）
```

**三种输出模式：**

**默认模式 — 格式化追踪行：**

```
[astrace] attached to PID 1 (state: running)
[  0.013s] Open(flags=2, path="/dev/llm/claude") → 3    1ms
[  0.014s] Write(fd=3, size=1234) → <nil>    5.20s  ← LLM 调用
[  5.214s] Read(fd=3, length=65536) → 892B      2ms
[  5.216s] Open(flags=2, path="/dev/fs/./src/main.go") → 4    1ms
[  5.217s] Write(fd=4, size=56) → <nil>    0µs
[  5.217s] Read(fd=4, length=1048576) → 2048    1ms
[  5.218s] Close(fd=4) → <nil>    0µs
[astrace] detached from PID 1 (process exited)
```

**注解标记：**

- `← LLM 调用` — 涉及 `/dev/llm/` 设备的操作
- `← 慢操作` — 耗时超过 1 秒的操作

**--verbose — 完整参数和结果**

**--json — 逐行 JSON（SyscallEventWire 结构）：**

```json
{"timestamp_ms": 13, "pid": 1, "syscall": "Open", "args": {"flags": 2, "path": "/dev/llm/claude"}, "result": 3, "duration_ms": 1.0}
```

**SIGINT 行为：** 仅 detach 追踪，不影响被追踪进程。

### 4.6 crux version — 版本信息

```
用法: crux version
```

**默认输出：**

```
crux v0.1.0
claude-code: 1.0.3
```

**Claude CLI 未安装时：**

```
crux v0.1.0
✗ claude-code CLI not found
  → 建议: npm install -g @anthropic-ai/claude-code
```

**JSON 输出：**

```json
{"ok": true, "data": {"version": "0.1.0", "claude_code_available": true, "claude_code": "1.0.3"}}
```

### 4.7 JSON 响应格式

所有支持 `--json` 的命令使用统一的 `JSONResponse` 包装：

```go
type JSONResponse struct {
    OK    bool `json:"ok"`
    Data  any  `json:"data,omitempty"`
    Error any  `json:"error,omitempty"`
}
```

**成功时：** `ok=true`，`data` 包含命令特定数据

**失败时：** `ok=false`，`error` 包含结构化错误信息：

```go
type jsonErrorData struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Syscall string `json:"syscall,omitempty"`
    Device  string `json:"device,omitempty"`
}
```

---

## 5. IPC 架构

### 5.1 Daemon 生命周期

Crux 采用 daemon 架构：一个后台 daemon 持有唯一的内核实例和进程表，所有 CLI 命令作为客户端通过 Unix domain socket 通信。

**自动启动（`EnsureDaemon`）：**

1. CLI 命令调用 `EnsureDaemon()`
2. 尝试连接现有 daemon 并发送 `ping`
3. 连接失败 → 清除 stale socket 文件
4. 启动新 daemon 进程（`crux daemon --internal`，`setsid` 独立进程组）
5. 轮询等待就绪（每 100ms 重试，最多 3 秒超时）
6. 返回已连接的 `*Client`

**自动停止（空闲超时）：**

- 默认超时：60 秒（`DefaultIdleTimeout`）
- 停止条件：无活跃进程 AND 无活跃连接
- 检查周期：每 5 秒（`idleCheckEvery`）
- 有进程运行或有连接时，暂停超时计时器

**Stale socket 清理：**

- ping 现有 socket 超时 → 删除旧 socket 文件 → 启动新 daemon
- daemon 启动时将 PID 写入 `crux.pid` 文件（诊断用途）

### 5.2 Socket 路径规则

Socket 路径按以下优先级确定：

1. **`$XDG_RUNTIME_DIR/crux/crux.sock`** — 如 `/run/user/1000/crux/crux.sock`
2. **`/tmp/crux-{uid}/crux.sock`** — 降级方案（`$XDG_RUNTIME_DIR` 未设置时）

目录权限：`0700`（仅当前用户可访问）。

测试可通过 `SocketPathOverride` 变量注入自定义路径。

### 5.3 NDJSON 协议

IPC 通信使用 NDJSON（Newline Delimited JSON）格式，每行一个 JSON 对象。

**Request 格式：**

```json
{"method": "ping|spawn|list_procs|kill|attach_debug|shutdown", "payload": {...}}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `method` | `string` | 请求方法（见 [5.4 Method 枚举](#54-method-枚举)） |
| `payload` | `object` | 方法特定的请求参数（可选） |

**Response 格式：**

```json
{"ok": true, "payload": {...}}
{"ok": false, "error": {"code": "...", "message": "..."}}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ok` | `bool` | 是否成功 |
| `payload` | `object` | 方法特定的响应数据（成功时） |
| `error` | `object` | 结构化错误信息（失败时） |

### 5.4 Method 枚举

| Method | 类型 | Payload 类型 | 说明 |
|--------|------|-------------|------|
| `ping` | 请求-响应 | — | 活性检查，返回版本号 |
| `spawn` | 流式 | `SpawnRequest` | 创建进程，流式返回进度事件 |
| `list_procs` | 请求-响应 | — | 获取所有进程列表 |
| `kill` | 请求-响应 | `KillRequest` | 发送信号到进程 |
| `attach_debug` | 流式 | `AttachDebugRequest` | 订阅 SyscallEvent 流 |
| `shutdown` | 请求-响应 | — | 优雅关闭 daemon |

**SpawnRequest：**

```json
{"intent": "分析代码", "agent": "code-analyst", "model": "sonnet", "max_steps": 10}
```

**KillRequest：**

```json
{"pid": 1, "signal": 1}
```

**AttachDebugRequest：**

```json
{"pid": 1}
```

**PingResponse：**

```json
{"version": "0.1.0"}
```

### 5.5 StreamEvent 流式协议

流式方法（`spawn`、`attach_debug`）使用 `StreamEvent` 逐行推送事件：

```json
{"type": "progress|complete|error|syscall_event|eof", "payload": {...}}
```

**StreamEventType 枚举：**

| 类型 | 说明 | 使用场景 |
|------|------|---------|
| `progress` | 推理步骤进度 | spawn 流 |
| `complete` | 进程完成 | spawn 流 |
| `error` | 错误 | spawn 流 |
| `syscall_event` | SyscallEvent | attach_debug 流 |
| `eof` | 流结束标记 | attach_debug 流 |

**ProgressPayload 结构（spawn 流）：**

| 字段 | 类型 | 事件 | 说明 |
|------|------|------|------|
| `event` | `string` | 所有 | `"spawn"`、`"step"`、`"complete"`、`"error"` |
| `pid` | `PID` | 所有 | 进程 ID |
| `intent` | `string` | spawn | 用户意图 |
| `step` | `int` | step | 当前步数 |
| `total` | `int` | step | 最大步数 |
| `result` | `string` | complete | 最终结果 |
| `exit_code` | `int` | complete | 退出码 |
| `exit_reason` | `string` | complete | 退出原因 |
| `tokens_used` | `int` | complete | token 消耗 |
| `error_message` | `string` | error | 错误信息 |

**SyscallEventWire 结构（attach_debug 流）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `timestamp_ms` | `int64` | 相对进程创建时间（毫秒） |
| `pid` | `PID` | 进程 ID |
| `syscall` | `string` | Syscall 名称 |
| `args` | `map[string]any` | 调用参数 |
| `result` | `any` | 返回值 |
| `error` | `string` | 错误信息 |
| `duration_ms` | `float64` | 执行耗时（毫秒） |

### 5.6 连接复用语义

IPC Server 采用请求循环连接模型：

**非流式方法（`ping`、`list_procs`、`kill`）：**

- 发送 Response 后，继续在同一连接上等待下一个 Request
- 客户端可在单个连接上发送多次请求
- 用途：`EnsureDaemon()` 的 `ping` 探活与后续操作共用连接

**流式方法（`spawn`、`attach_debug`）：**

- handler 接管连接进行流式传输
- 流结束后 handler 返回，连接关闭
- 同一连接不再接受新请求

**`shutdown` 方法：**

- 发送 Response 后，异步触发 `Shutdown()`，handler 返回并关闭连接

### 5.7 Spawn 流式协议示例

```
Client → Server:  {"method":"spawn","payload":{"intent":"分析代码","agent":"code-analyst"}}

Server → Client:  {"ok":true,"payload":{"pid":1}}
Server → Client:  {"type":"progress","payload":{"event":"spawn","pid":1,"intent":"分析代码"}}
Server → Client:  {"type":"progress","payload":{"event":"step","pid":1,"step":1,"total":10}}
Server → Client:  {"type":"progress","payload":{"event":"step","pid":1,"step":2,"total":10}}
Server → Client:  {"type":"complete","payload":{"event":"complete","pid":1,"result":"分析结果...","exit_code":0,"tokens_used":1234}}

（连接关闭，IPC Server 自动调用 kern.Reap(pid) 清理 Zombie 进程）
```

### 5.8 AttachDebug 流式协议示例

```
Client → Server:  {"method":"attach_debug","payload":{"pid":1}}

Server → Client:  {"ok":true}
Server → Client:  {"type":"syscall_event","payload":{"timestamp_ms":13,"pid":1,"syscall":"Open","args":{"flags":2,"path":"/dev/llm/claude"},"result":3,"duration_ms":1.0}}
Server → Client:  {"type":"syscall_event","payload":{"timestamp_ms":14,"pid":1,"syscall":"Write","args":{"fd":3,"size":1234},"duration_ms":5200.0}}
Server → Client:  {"type":"syscall_event","payload":{"timestamp_ms":5214,"pid":1,"syscall":"Read","args":{"fd":3,"length":65536},"result":892,"duration_ms":2.0}}
...
Server → Client:  {"type":"eof"}

（进程退出 → DebugChan 关闭 → range 循环结束 → 发送 eof → 连接关闭）
```

---

## 6. 错误处理与类型参考

### 6.1 ErrCode 枚举

所有错误类型共享统一的 `ErrCode` 分类码：

| 错误码 | 值 | 含义 |
|--------|-----|------|
| `ErrTimeout` | `"TIMEOUT"` | 操作超时 |
| `ErrNotFound` | `"NOT_FOUND"` | 资源不存在（进程、上下文、FD、设备） |
| `ErrPermission` | `"PERMISSION"` | 权限拒绝（如写入只读 /proc） |
| `ErrInternal` | `"INTERNAL"` | 内部错误（状态异常、序列化失败等） |
| `ErrDriver` | `"DRIVER"` | 设备驱动错误（LLM 调用失败、文件读写失败等） |
| `ErrInvalid` | `"INVALID"` | 无效参数（如无效信号值） |

### 6.2 SyscallError

内核层错误，所有 syscall 出错时返回此类型。

```go
type SyscallError struct {
    Syscall string        // 出错的 syscall 名称
    PID     types.PID     // 发起 syscall 的进程 PID
    Device  string        // 涉及的 VFS 路径
    Err     error         // 底层错误
    Code    types.ErrCode // 分类错误码
}
```

**格式化输出：** `[TIMEOUT] PID 1 Spawn: /dev/llm/claude (context deadline exceeded)`

**`Unwrap()` 支持：** 实现 `errors.Unwrap` 接口，支持 `errors.Is` 和 `errors.As` 链式错误检查。

### 6.3 VFSError

VFS 层错误，VFS 操作出错时返回此类型。

```go
type VFSError struct {
    Op     string        // 操作名称（"Open"、"Read"、"Write"、"Close"、"Stat"）
    PID    types.PID     // 进程 PID
    Device string        // VFS 路径
    Err    error         // 底层错误
    Code   types.ErrCode // 分类错误码
}
```

**格式化输出：** `[NOT_FOUND] PID 1 Open: /dev/unknown (device not found: /dev/unknown)`

**`Unwrap()` 支持：** 是

### 6.4 DriverError

驱动层错误，设备驱动内部使用，避免 `drivers/` → `kernel/` 的循环依赖。

```go
type DriverError struct {
    Op     string        // 操作名称
    Device string        // 设备路径
    Err    error         // 底层错误
    Code   types.ErrCode // 分类错误码
}
```

**格式化输出：** `[DRIVER] Write: /dev/llm/claude (exec: command not found)`

**`Unwrap()` 支持：** 是

**错误码传播：** VFS 层通过 `errors.As` 提取 `DriverError` 中的 `Code`，传播到 `VFSError`。

### 6.5 ContextError

上下文层错误。

```go
type ContextError struct {
    Op   string        // 操作名称（"CtxAlloc"、"CtxRead"、"CtxWrite"、"CtxFree"）
    CID  types.CtxID   // 上下文 ID
    Err  error         // 底层错误
    Code types.ErrCode // 分类错误码
}
```

**格式化输出：** `[NOT_FOUND] CtxID 1 CtxFree: context not found`

**`Unwrap()` 支持：** 是

### 6.6 基础类型

| 类型 | Go 定义 | 说明 |
|------|---------|------|
| `PID` | `uint64` | 进程 ID（从 1 递增，不回收） |
| `FD` | `int` | 文件描述符（从 3 递增） |
| `CtxID` | `uint64` | 上下文 ID（从 1 递增） |
| `ErrCode` | `string` | 错误分类码 |
| `Signal` | `int` | 进程信号 |
| `ProcessState` | `int` | 进程状态 |

---

## 7. 进程模型参考

### 7.1 ProcessState 状态机

```
Created ──→ Running ──→ Zombie ──→ Dead
   │           │           │
   │  Start()  │ Terminate │  Reap()
   │  开始推理  │ 完成/错误  │  Wait 回收
   │           │ /超时/Kill │  资源释放
```

| 常量 | 值 | 字符串表示 | 说明 |
|------|-----|---------|------|
| `StateCreated` | `0` | `"created"` | 进程对象已分配，推理未开始 |
| `StateRunning` | `1` | `"running"` | 推理循环执行中 |
| `StateZombie` | `2` | `"zombie"` | 推理已结束，等待资源回收 |
| `StateDead` | `3` | `"dead"` | 所有资源已释放 |

### 7.2 状态转移规则

**合法转移：**

| 起始状态 | 目标状态 | 触发条件 |
|---------|---------|---------|
| Created | Running | `Start()` — 推理 goroutine 启动 |
| Running | Zombie | `Terminate()` — 完成/错误/超时/Kill |
| Zombie | Dead | `Reap()` — Wait 回收 |

**非法转移：** 所有其他组合均为非法。尝试非法转移返回 `*SyscallError`（`INTERNAL`）。

`StateDead` 没有合法的后续状态。

### 7.3 ExitStatus 结构

```go
type ExitStatus struct {
    Code   int    // 0 = 正常退出，非零 = 异常
    Reason string // 人类可读的退出原因
    Err    error  // 底层错误（正常退出时为 nil）
}
```

**常见退出原因：**

| Code | Reason | 说明 |
|------|--------|------|
| `0` | `"completed"` | 正常完成 |
| `1` | `"unexpected exit"` | 意外退出 |
| `1` | `"max steps exceeded"` | 超过最大推理步数 |
| `1` | 错误描述 | 推理过程中出错 |

### 7.4 资源释放顺序

`reapProcess` 按以下严格顺序执行资源释放（通过 `sync.Once` 保证幂等）：

| 步骤 | 操作 | 说明 |
|------|------|------|
| 0 | `handleOrphanChildren` | 处理孤儿子进程：Running 子进程 reparent 到 PID 0；Zombie 子进程推入 reapCh |
| 1 | `Cancel()` | 取消进程 context（幂等） |
| 2 | `wg.Wait()` | 等待推理 goroutine 完成（goroutine 内部 defer 执行 `CloseAll` 关闭所有 FD） |
| 3 | `close(DebugChan)` | 先将 `proc.DebugChan` 置 `nil`（持锁），然后关闭 channel |
| 4 | `CtxFree(CtxID)` | 释放上下文空间 |
| 5 | `Reap()` | 状态转移 Zombie → Dead |
| 6 | `RemoveProcess(pid)` | 从进程表中移除 |

### 7.5 Signal 定义

| 常量 | 值 | 说明 |
|------|-----|------|
| `SIGTERM` | `1` | 终止信号（优雅关闭） |
| `SIGKILL` | `2` | 强制杀死 |

**有效性检查：** `Signal.Valid()` 方法检查信号值是否为 SIGTERM 或 SIGKILL。

**Kill 行为：** 无论信号类型，当前实现均调用 `proc.Cancel()` 取消 context。未来版本可能区分 SIGTERM（优雅）和 SIGKILL（强制）的行为。
