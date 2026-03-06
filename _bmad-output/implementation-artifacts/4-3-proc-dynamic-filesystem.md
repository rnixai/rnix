# Story 4.3: /proc 动态文件系统

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 `/proc/{pid}/` 路径查看智能体的运行时状态,
So that 我可以程序化地获取进程信息（为 `rnix ps` 和未来诊断工具提供数据源）。

## Acceptance Criteria

1. **`/proc/{pid}/status` 返回实时状态 JSON** — Given ProcFS 已注册，When 调用 `Open("/proc/1/status")`，Then 返回 PID 1 的实时状态 JSON（包含 pid、state、intent、skills、tokens、elapsed 字段）

2. **`/proc/{pid}/intent` 返回原始意图** — Given ProcFS 已注册，When 调用 `Open("/proc/1/intent")`，Then 返回 PID 1 的原始意图文本

3. **`/proc/{pid}/context` 返回上下文摘要** — Given ProcFS 已注册，When 调用 `Open("/proc/1/context")`，Then 返回 PID 1 的当前上下文内容摘要

4. **PID 不存在返回 ErrNotFound** — Given PID 999 不存在，When 调用 `Open("/proc/999/status")`，Then 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

5. **通过 ProcessInfoProvider 接口解耦** — Given ProcFS 需要读取进程信息，When 查看实现，Then 通过 `ProcessInfoProvider` 接口读取（不直接依赖 kernel 包，避免反向依赖 vfs/ → kernel/）

6. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 定义 ProcessInfoProvider 接口和 ProcInfo 数据结构 (AC: #5)
  - [x] 1.1 在 `vfs/proc.go` 中定义 `ProcessInfoProvider` 接口（`GetProcInfo(pid) (*ProcInfo, error)` + `ListProcs() []ProcInfo`）
  - [x] 1.2 在 `vfs/proc.go` 中定义 `ProcInfo` 结构体（PID、PPID、State、Intent、Skills、TokensUsed、CreatedAt、CtxID、Result、AllowedDevices）
  - [x] 1.3 定义 `ContextSummaryProvider` 接口（`GetContextSummary(ctxID types.CtxID) (string, error)`），用于获取上下文摘要

- [x] Task 2: 实现 ProcFS 驱动 (AC: #1, #2, #3, #4)
  - [x] 2.1 在 `vfs/proc.go` 实现 `ProcFS` 结构体，持有 `ProcessInfoProvider` 和 `ContextSummaryProvider`
  - [x] 2.2 实现 `NewProcFS(provider, ctxProvider)` 构造函数
  - [x] 2.3 实现 `FileFactory() VFSFileFactory` 方法——解析 subpath `/{pid}/{file}` 格式
  - [x] 2.4 实现 subpath 解析逻辑：提取 PID（字符串→数字）和文件名（status/intent/context）
  - [x] 2.5 无效路径格式返回 `*VFSError{Code: ErrNotFound}`
  - [x] 2.6 不支持的文件名（非 status/intent/context）返回 `*VFSError{Code: ErrNotFound}`

- [x] Task 3: 实现 procFile（只读 VFSFile）(AC: #1, #2, #3)
  - [x] 3.1 实现 `procFile` 结构体——持有预计算的 `content []byte` 和读取偏移
  - [x] 3.2 实现 `Read(length)` — 返回内容（支持部分读取）
  - [x] 3.3 实现 `Write(ctx, data)` — 返回只读错误（`/proc is read-only`）
  - [x] 3.4 实现 `Close()` — 返回 nil（无资源需释放）
  - [x] 3.5 实现 `Stat()` — 返回 FileStat（Name=虚拟路径，Size=内容长度）

- [x] Task 4: 实现三种虚拟文件内容生成 (AC: #1, #2, #3)
  - [x] 4.1 实现 `buildStatusJSON(info *ProcInfo)` — 生成 JSON 格式状态（pid、state、ppid、intent、skills、tokens_used、elapsed_ms、allowed_devices）
  - [x] 4.2 实现 `buildIntent(info *ProcInfo)` — 直接返回 intent 文本
  - [x] 4.3 实现 `buildContext(ctxProvider, ctxID)` — 调用 ContextSummaryProvider 获取上下文摘要

- [x] Task 5: 在 KernelImpl 实现 ProcessInfoProvider (AC: #5)
  - [x] 5.1 在 `kernel/kernel.go` 添加 `GetProcInfo(pid) (*vfs.ProcInfo, error)` 方法
  - [x] 5.2 在 `kernel/kernel.go` 添加 `ListProcs() []vfs.ProcInfo` 方法
  - [x] 5.3 从 Process 字段安全提取信息（在 `proc.mu` 保护下读取可变字段）

- [x] Task 6: 在 Context Manager 实现 ContextSummaryProvider (AC: #3)
  - [x] 6.1 在 `context/context.go` 添加 `GetContextSummary(ctxID) (string, error)` 方法
  - [x] 6.2 返回上下文中的消息数量、system prompt 长度、最近消息预览等摘要信息

- [x] Task 7: 注册 ProcFS 到 DeviceRegistry (AC: #1, #2, #3)
  - [x] 7.1 在 `cmd/rnix/main.go` 的 `runRoot` 中：创建 ProcFS 实例并注册到 `/proc`
  - [x] 7.2 在 `cmd/rnix/main.go` 的 `initKernel` 中：同样注册 ProcFS（用于 astrace 等子命令）
  - [x] 7.3 确保注册顺序：先创建 kernel，再创建 ProcFS（需要 kernel 作为 provider）

- [x] Task 8: 单元测试 (AC: #1, #2, #3, #4, #5, #6)
  - [x] 8.1 在 `vfs/proc_test.go` 中用 mock ProcessInfoProvider 测试 ProcFS
  - [x] 8.2 测试 `/proc/{pid}/status` — 验证 JSON 格式、字段完整性
  - [x] 8.3 测试 `/proc/{pid}/intent` — 验证返回原始意图文本
  - [x] 8.4 测试 `/proc/{pid}/context` — 验证返回上下文摘要
  - [x] 8.5 测试 PID 不存在 — 验证返回 ErrNotFound
  - [x] 8.6 测试无效路径格式 — 验证错误处理（非数字 PID、不支持的文件名、空路径）
  - [x] 8.7 测试 Write 拒绝 — 验证 /proc 只读
  - [x] 8.8 测试 kernel GetProcInfo/ListProcs — 在 `kernel/kernel_test.go` 中验证
  - [x] 8.9 测试并发读取 — 多 goroutine 同时读 /proc 无竞态
  - [x] 8.10 运行 `go test -race ./...` 和 `go vet ./...` 确认全部通过

## Dev Notes

### 核心设计决策

#### 接口定义位置——消费方定义原则

**设计决策：** `ProcessInfoProvider` 和 `ContextSummaryProvider` 接口定义在 `vfs/proc.go` 中（消费方），不定义在 `kernel/` 中。

**理由：**
- Go 惯例：接口定义在使用方，不在实现方
- 避免反向依赖：`vfs/` 不导入 `kernel/`（架构约束）
- `KernelImpl` 通过鸭子类型（duck typing）自动满足接口，无需显式声明 `implements`
- 依赖注入在 `cmd/rnix/main.go` 中完成：`vfs.NewProcFS(kernel, ctxMgr)`

```go
// vfs/proc.go — 接口定义在消费方
type ProcessInfoProvider interface {
    GetProcInfo(pid types.PID) (*ProcInfo, error)
    ListProcs() []ProcInfo
}

type ContextSummaryProvider interface {
    GetContextSummary(ctxID types.CtxID) (string, error)
}

// kernel/kernel.go — 实现方（鸭子类型自动满足）
func (k *KernelImpl) GetProcInfo(pid types.PID) (*vfs.ProcInfo, error) { ... }
func (k *KernelImpl) ListProcs() []vfs.ProcInfo { ... }

// context/context.go — 实现方
func (m *Manager) GetContextSummary(ctxID types.CtxID) (string, error) { ... }

// cmd/rnix/main.go — 依赖注入
procFS := vfs.NewProcFS(kern, ctxMgr)
devReg.Register("/proc", procFS.FileFactory())
```

#### ProcInfo 数据结构——快照而非引用

**设计决策：** `ProcInfo` 是值类型（struct），不是 `*Process` 引用。

**理由：**
- `vfs/` 不能导入 `kernel/` 的 `Process` 类型
- 值类型复制避免了并发问题——`ProcInfo` 在创建瞬间获取快照
- JSON 序列化基于快照数据，不依赖活跃的 Process 对象

```go
type ProcInfo struct {
    PID            types.PID
    PPID           types.PID
    State          types.ProcessState
    Intent         string
    Skills         []string
    TokensUsed     int
    CreatedAt      time.Time
    CtxID          types.CtxID
    Result         string
    AllowedDevices []string
}
```

#### /proc/{pid}/status JSON 格式

遵循架构文档的 JSON 命名约定（snake_case），确保与 `--json` flag 输出一致：

```json
{
    "pid": 1,
    "ppid": 0,
    "state": "running",
    "intent": "分析代码",
    "skills": ["code-analysis"],
    "tokens_used": 1500,
    "elapsed_ms": 6200,
    "allowed_devices": ["/dev/fs", "/dev/shell"]
}
```

**state 字段值映射：**

| ProcessState 常量 | JSON 字符串 |
|-------------------|------------|
| StateCreated | `"created"` |
| StateRunning | `"running"` |
| StateZombie | `"zombie"` |
| StateDead | `"dead"` |

#### /proc/{pid}/context 摘要格式

上下文摘要提供调试时有用的概览，不暴露完整 prompt（可能很大）：

```
Messages: 5 (system: 1, user: 2, assistant: 2)
System Prompt: 1200 chars
Last Message: [assistant] 分析完成，发现 3 个问题...
```

#### subpath 解析逻辑

ProcFS 注册在 `/proc`，DeviceRegistry 前缀匹配后传入 subpath：

```
VFS.Open("/proc/1/status")
  → DeviceRegistry.Open("/proc/1/status")
    → 匹配 "/proc"，subpath = "/1/status"
      → ProcFS.FileFactory("/1/status", flags)
        → 解析：pid=1, file="status"
```

解析规则：
1. subpath 必须以 `/` 开头
2. 格式为 `/{pid}/{file}`，其中 pid 为正整数，file 为 status/intent/context
3. 不符合格式返回 `*VFSError{Code: ErrNotFound}`

```go
func parseProcPath(subpath string) (types.PID, string, error) {
    // subpath 示例: "/1/status", "/42/intent"
    parts := strings.SplitN(strings.TrimPrefix(subpath, "/"), "/", 2)
    if len(parts) != 2 {
        return 0, "", fmt.Errorf("invalid proc path: %s", subpath)
    }
    pid, err := strconv.ParseUint(parts[0], 10, 64)
    if err != nil {
        return 0, "", fmt.Errorf("invalid PID: %s", parts[0])
    }
    file := parts[1]
    if file != "status" && file != "intent" && file != "context" {
        return 0, "", fmt.Errorf("unknown proc file: %s", file)
    }
    return types.PID(pid), file, nil
}
```

#### procFile 读取语义——Open 时快照

**设计决策：** 在 `Open` 时一次性生成内容快照存入 `procFile.content`，后续 `Read` 从快照读取。

**理由：**
- 语义清晰：每次 Open 获取的是打开瞬间的快照
- 避免多次 Read 间数据不一致
- 符合 Unix `/proc` 语义（读取 /proc/pid/status 得到的是一致快照）
- 实现简单，无需持有 provider 引用

```go
type procFile struct {
    content []byte
    offset  int
    path    string  // 虚拟路径（用于 Stat）
}
```

### 前序 Story 经验（Story 4.2）

**直接适用的经验：**

1. **reapOnce/shutdownOnce 模式** — 测试中必须 `defer k.Shutdown()`，所有新增测试必须遵循
2. **emitEvent 并发安全** — 在 `proc.mu` 下读取 DebugChan 后发送，`nil` 检查防止 panic。本 Story 的 `GetProcInfo` 也需要在 `proc.mu` 下安全读取可变字段
3. **PID 0 作为虚拟 init** — PID 0 不在进程表中，`GetProcInfo(0)` 应返回 `ErrNotFound`
4. **CloseAll 在 Spawn goroutine defer 中** — ProcFS 的 procFile 没有需要 CloseAll 管理的资源
5. **测试 helper** — `newTestKernel(t)` 和 `newSimpleKernel(t)` 已自动注册 `t.Cleanup(k.Shutdown)`

**Git 提交模式（保持一致）：**
```
92787da Finalize Story 4.2: Orphan Process Reparenting and Zombie Auto-Reap Implementation
85bf6a1 Add Story 4.2: Orphan Process Reparenting and Zombie Auto-Reap Implementation
841ce08 Implement Story 4.1: Kill and Wait Syscalls
```

### 已有代码关键 API 参考

**vfs/vfs.go — VFSFile 接口（第 31-37 行）：**
```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

**vfs/vfs.go — VFSFileFactory（第 39-41 行）：**
```go
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**vfs/vfs.go — FileStat（第 24-29 行）：**
```go
type FileStat struct {
    Name       string
    Size       int64
    IsDevice   bool
    DevicePath string
}
```

**vfs/vfs.go — VFSError（第 44-60 行）：**
```go
type VFSError struct {
    Op     string
    PID    types.PID
    Device string
    Err    error
    Code   types.ErrCode
}
```

**vfs/dev.go — DeviceRegistry.Open 前缀匹配（第 34-57 行）：**
```go
func (d *DeviceRegistry) Open(path string, flags OpenFlag) (VFSFile, error) {
    // 精确匹配 → 最长前缀匹配
    // subpath = strings.TrimPrefix(path, bestPrefix)
}
```

**kernel/kernel.go — KernelImpl（第 92-103 行）：**
```go
type KernelImpl struct {
    procTable    *xsync.SyncMap[types.PID, *Process]
    vfs          *vfs.VFS
    ctxMgr       *rnixctx.Manager
    callbacks    KernelCallbacks
    reapCh       chan types.PID
    stopCh       chan struct{}
    reaperWg     sync.WaitGroup
    shutdownOnce sync.Once
}
```

**kernel/kernel.go — GetProcess/ListProcesses（第 597-654 行）：**
```go
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool) {
    return k.procTable.Load(pid)
}
func (k *KernelImpl) ListProcesses() []*Process {
    var procs []*Process
    k.procTable.Range(func(_ types.PID, p *Process) bool {
        procs = append(procs, p)
        return true
    })
    return procs
}
```

**kernel/process.go — Process 结构体（第 32-54 行）：**
```go
type Process struct {
    PID            types.PID
    PPID           types.PID
    State          types.ProcessState
    Intent         string
    Skills         []string
    Children       []types.PID
    FDTable        map[types.FD]vfs.VFSFile
    DebugChan      chan types.SyscallEvent
    Done           chan ExitStatus
    CreatedAt      time.Time
    Exit           *ExitStatus
    CtxID          types.CtxID
    Result         string
    TokensUsed     int
    AllowedDevices []string
    mu             sync.Mutex
    cancel         context.CancelFunc
    ctx            context.Context
    wg             sync.WaitGroup
    reapOnce       sync.Once
}
```

**kernel/process.go — GetState（第 80-85 行）：**
```go
func (p *Process) GetState() types.ProcessState {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.State
}
```

**context/context.go — Manager 结构体（第 65-68 行）：**
```go
type Manager struct {
    contexts *xsync.SyncMap[types.CtxID, *Context]
    nextID   atomic.Uint64
}
```

**context/context.go — Context 结构体（第 15-23 行）：**
```go
type Context struct {
    ID           types.CtxID
    SystemPrompt string
    Messages     []Message
    mu           sync.Mutex
}
```

**cmd/rnix/main.go — 设备注册模式（runRoot 约第 210-220 行）：**
```go
devReg := vfs.NewDeviceRegistry()
devReg.Register("/dev/llm/claude", llm.FileFactory(llmTimeout))
devReg.Register("/dev/fs", fs.FileFactory())
devReg.Register("/dev/shell", shell.FileFactory(shellTimeout))
// Story 4.3: devReg.Register("/proc", procFS.FileFactory())
```

### 注意事项与防错

#### GetProcInfo 中的线程安全读取

Process 的可变字段（State、TokensUsed、Result、Exit）需要在 `proc.mu` 下读取。不可变字段（PID、PPID、Intent、Skills、CreatedAt）可以直接读取。

```go
func (k *KernelImpl) GetProcInfo(pid types.PID) (*vfs.ProcInfo, error) {
    proc, ok := k.GetProcess(pid)
    if !ok {
        return nil, &vfs.VFSError{
            Op: "GetProcInfo", Device: "/proc",
            Err: fmt.Errorf("process %d not found", pid),
            Code: types.ErrNotFound,
        }
    }

    proc.mu.Lock()
    info := &vfs.ProcInfo{
        PID:            proc.PID,
        PPID:           proc.PPID,
        State:          proc.State,      // 在锁内读取
        Intent:         proc.Intent,
        Skills:         proc.Skills,
        TokensUsed:     proc.TokensUsed, // 在锁内读取
        CreatedAt:      proc.CreatedAt,
        CtxID:          proc.CtxID,
        Result:         proc.Result,     // 在锁内读取
        AllowedDevices: proc.AllowedDevices,
    }
    proc.mu.Unlock()
    return info, nil
}
```

#### VFSError vs SyscallError

ProcFS 是 VFS 层组件，应返回 `*VFSError`（不是 `*SyscallError`）。`SyscallError` 用于 kernel 层 syscall 实现。VFS 层通过 `VFSError` 上报，kernel 层 `Open` 等 syscall 再包装为 `SyscallError`。

#### /proc 注册必须在 kernel 创建后

注册顺序依赖：
1. 创建 `ctxMgr` → 2. 创建 `kernel`（需要 ctxMgr）→ 3. 创建 `ProcFS`（需要 kernel 和 ctxMgr）→ 4. 注册到 DeviceRegistry

在 `cmd/rnix/main.go` 中，当前 DeviceRegistry 在 Kernel 创建前注册设备。ProcFS 需要 Kernel 实例，所以必须在 Kernel 创建后注册。

#### ProcessState 到 JSON 字符串映射

需要一个 helper 函数将 ProcessState 常量映射为人类可读字符串：

```go
func stateToString(s types.ProcessState) string {
    switch s {
    case types.StateCreated:
        return "created"
    case types.StateRunning:
        return "running"
    case types.StateZombie:
        return "zombie"
    case types.StateDead:
        return "dead"
    default:
        return "unknown"
    }
}
```

这个函数可以放在 `vfs/proc.go` 中（因为是 ProcFS 的内部实现），不需要导出。

#### Read 的偏移支持

`procFile.Read(length)` 应支持多次调用，返回从当前偏移开始的内容。第一次调用返回内容，后续调用（偏移超出内容长度时）返回空（EOF）。

```go
func (f *procFile) Read(length int) ([]byte, error) {
    if f.offset >= len(f.content) {
        return nil, nil // EOF
    }
    end := f.offset + length
    if end > len(f.content) {
        end = len(f.content)
    }
    data := f.content[f.offset:end]
    f.offset = end
    return data, nil
}
```

#### 不要在 ProcFS 内部调用 emitEvent

ProcFS 是 VFS 层组件，不负责 SyscallEvent 记录。SyscallEvent 在 kernel 层的 `Open`/`Read` syscall 实现中已有记录（入口/出口）。ProcFS 只需返回正确的数据即可。

### 接口合规

**Kernel 接口（架构文档要求）：**

Story 4.3 不修改 Kernel 接口签名（ProcessManager/ContextManager/FileSystem/Debugger）。新增的 `GetProcInfo` 和 `ListProcs` 方法是 `KernelImpl` 的具体方法（不是接口方法），用于满足 `ProcessInfoProvider` 接口（定义在 vfs/ 中）。

**FileSystem 接口不变：**
```go
type FileSystem interface {
    Open(path string, flags int) (FD, error)
    Read(fd FD, length int) ([]byte, error)
    Write(fd FD, data []byte) error
    Close(fd FD) error
    Stat(path string) (FileStat, error)
}
```

ProcFS 作为一个 VFS 设备驱动注册到 DeviceRegistry，通过现有的 `VFS.Open` 流程被调用。用户层面的 `kernel.Open("/proc/1/status")` → `VFS.Open` → `DeviceRegistry.Open` → `ProcFS.FileFactory` → `procFile`。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR2 | `rnix ps` ≤100ms | ProcFS 内存操作，无 I/O，响应 < 1ms |
| NFR9 | 进程表一致性 | ProcInfo 是快照值类型，不影响进程表 |
| NFR10 | CLI 不崩溃 | 所有路径返回 error，nil 检查，只读文件系统 |

### 范围边界

**本 Story 包含：**
- `vfs/proc.go` — ProcFS 驱动实现 + ProcessInfoProvider/ContextSummaryProvider 接口 + ProcInfo 结构体 + procFile 只读文件
- `vfs/proc_test.go` — ProcFS 单元测试（mock provider）
- `kernel/kernel.go` — 添加 GetProcInfo/ListProcs 方法（实现 ProcessInfoProvider）
- `kernel/kernel_test.go` — GetProcInfo/ListProcs 测试
- `context/context.go` — 添加 GetContextSummary 方法（实现 ContextSummaryProvider）
- `context/context_test.go` — GetContextSummary 测试
- `cmd/rnix/main.go` — 注册 ProcFS 到 `/proc`

**本 Story 不包含：**
- `rnix ps` CLI 命令和 Process Table UI（Story 4.4）
- `rnix kill <pid>` CLI 子命令（Story 4.4）
- `/proc` 写操作支持（/proc 是只读的）
- `/proc` 目录列表操作（如 ls /proc/）
- 上下文释放 ctx_free 独立测试（Story 4.5）

### Project Structure Notes

**新增文件：**
```
vfs/proc.go              — ProcFS 驱动 + 接口定义 + ProcInfo + procFile
vfs/proc_test.go         — ProcFS 单元测试
```

**修改文件：**
```
kernel/kernel.go         — 添加 GetProcInfo/ListProcs 方法
kernel/kernel_test.go    — 添加 GetProcInfo/ListProcs 测试
context/context.go       — 添加 GetContextSummary 方法
context/context_test.go  — 添加 GetContextSummary 测试
cmd/rnix/main.go         — 注册 ProcFS 设备
```

**不修改文件：**
```
vfs/vfs.go               — VFSFile/VFSFileFactory/VFSError 接口不变
vfs/dev.go               — DeviceRegistry 不变（前缀匹配已支持）
kernel/process.go        — Process 结构体不变
kernel/reap.go           — Wait/reapProcess 不变
kernel/errors.go         — SyscallError 不变
internal/types/types.go  — 共享类型不变
internal/ui/*            — UI 组件不变
debug/*                  — astrace 不变
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.3] — Story 定义和验收标准（第 830-857 行）
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 3] — VFS 实现策略，/proc 纯内存动态生成
- [Source: _bmad-output/planning-artifacts/architecture.md#架构边界] — Kernel↔VFS 边界，ProcessInfoProvider 接口
- [Source: _bmad-output/planning-artifacts/architecture.md#项目结构] — vfs/proc.go 文件定义
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — vfs/ 不导入 kernel/，通过接口解耦
- [Source: _bmad-output/planning-artifacts/architecture.md#格式模式] — JSON 字段 snake_case 规范
- [Source: _bmad-output/planning-artifacts/prd.md#FR14] — /proc/{pid}/ 动态暴露运行时状态
- [Source: _bmad-output/planning-artifacts/prd.md#NFR2] — rnix ps ≤100ms
- [Source: _bmad-output/project-context.md#VFS设备模型] — VFS 路径约定，FD 管理
- [Source: _bmad-output/project-context.md#依赖方向] — 严格单向依赖
- [Source: _bmad-output/implementation-artifacts/4-2-orphan-process-reparent-and-zombie-auto-reap.md] — 前序 Story 经验，Shutdown 模式，emitEvent 并发安全
- [Source: vfs/vfs.go:31-37] — VFSFile 接口定义
- [Source: vfs/vfs.go:39-41] — VFSFileFactory 类型定义
- [Source: vfs/vfs.go:24-29] — FileStat 结构体
- [Source: vfs/vfs.go:44-60] — VFSError 结构体
- [Source: vfs/dev.go:34-57] — DeviceRegistry.Open 前缀匹配逻辑
- [Source: kernel/kernel.go:92-103] — KernelImpl 结构体
- [Source: kernel/kernel.go:597-600] — GetProcess 方法
- [Source: kernel/kernel.go:647-654] — ListProcesses 方法
- [Source: kernel/process.go:32-54] — Process 结构体字段
- [Source: kernel/process.go:80-85] — GetState 线程安全读取
- [Source: context/context.go:15-23] — Context 结构体（Messages 字段）
- [Source: context/context.go:65-68] — Manager 结构体
- [Source: cmd/rnix/main.go:~210-220] — 现有设备注册模式

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1-4: 在 `vfs/proc.go` 中实现了完整的 ProcFS 驱动，包括 `ProcessInfoProvider`/`ContextSummaryProvider` 接口、`ProcInfo` 结构体、`ProcFS` 驱动（FileFactory + subpath 解析）、`procFile` 只读文件（Read/Write/Close/Stat）、三种虚拟文件内容生成器（status JSON、intent 文本、context 摘要）
- Task 5: 在 `kernel/kernel.go` 添加 `GetProcInfo`/`ListProcs` 方法，在 `proc.mu` 保护下安全读取可变字段（State、TokensUsed、Result），返回值类型快照避免并发问题
- Task 6: 在 `context/context.go` 添加 `GetContextSummary` 方法，返回消息计数（按角色分类）、system prompt 长度、最近消息预览（截断至 80 字符）
- Task 7: 在 `cmd/rnix/main.go` 的 `runRoot` 和 `initKernel` 中注册 ProcFS 到 `/proc`，确保注册顺序（kernel 先于 ProcFS 创建）
- Task 8: 全面的单元测试覆盖——`vfs/proc_test.go` 17 个测试用例（mock provider、JSON 验证、intent/context/错误路径/只读/并发/偏移），`kernel/kernel_test.go` 7 个新测试用例（GetProcInfo 快照/NotFound/PID0/可变字段/ListProcs/并发安全），`context/context_test.go` 5 个新测试用例（基本摘要/空上下文/NotFound/长消息截断/tool 消息）
- 设计决策：接口定义在消费方 `vfs/` 中（Go 惯例），通过鸭子类型满足；`ProcInfo` 为值类型快照；nil skills/allowed_devices 序列化为 `[]` 而非 `null`；status JSON 使用 snake_case
- `go test -race ./...` 全部通过，`go vet ./...` 无警告

### Code Review Fixes (2026-02-26)

- **[H1] 修复 reasonStep 中 TokensUsed/Result 写入的数据竞争** — `kernel/kernel.go:421,428` 的写入现在在 `proc.mu.Lock()` 保护下执行，与 `GetProcInfo` 的读取使用同一把锁
- **[M1] 修复 GetProcInfo/ListProcs 中 Skills/AllowedDevices 的浅拷贝** — 使用 `append([]string(nil), slice...)` 创建独立副本，确保 ProcInfo 快照语义
- **[M2] 替换自定义 asVFSError 为标准 errors.As** — 删除 `vfs/proc_test.go` 中的自定义 helper，统一使用标准库
- **[M3] 消除重复的 ProcessState→string 映射** — 在 `internal/types/types.go` 添加 `ProcessState.String()` 方法，`vfs/proc.go` 和 `cmd/rnix/main.go` 统一委托
- **[L1] 删除自定义 containsStr/findSubstr** — 替换为 `strings.Contains`

### File List

**新增文件：**
- `vfs/proc.go` — ProcFS 驱动 + 接口定义 + ProcInfo + procFile + 内容生成器
- `vfs/proc_test.go` — ProcFS 单元测试（17 个测试用例）

**修改文件：**
- `kernel/kernel.go` — 添加 GetProcInfo/ListProcs 方法；修复 reasonStep 中 TokensUsed/Result 写入竞争；深拷贝 slice 字段
- `kernel/kernel_test.go` — 添加 GetProcInfo/ListProcs 测试（7 个测试用例）
- `context/context.go` — 添加 GetContextSummary 方法 + strings import
- `context/context_test.go` — 添加 GetContextSummary 测试（5 个测试用例）+ strings import
- `cmd/rnix/main.go` — runRoot 和 initKernel 注册 ProcFS 到 /proc；消除重复 processStateNames map
- `internal/types/types.go` — 添加 ProcessState.String() 方法
