# Story 6.2: Pipe 管道

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 Pipe 将一个智能体的输出连接为另一个智能体的输入,
So that 智能体可以流式传递数据，实现链式处理。

## Acceptance Criteria

1. **基本管道创建** — Given `kernel/ipc.go` 中 Pipe 已实现，When 调用 `Pipe()` 创建管道，Then 返回 `(readFD, writeFD)` 一对文件描述符，And 写入 writeFD 的数据可从 readFD 读取

2. **跨进程数据传递** — Given 管道已创建，When 智能体 A 向 writeFD 写入，智能体 B 从 readFD 读取，Then 数据正确传递，吞吐量 ≥ 1MB/s（NFR23）

3. **写端关闭 EOF** — Given 写端关闭，When 读端继续 Read，Then 返回 EOF，不阻塞

4. **读端关闭 BrokenPipe** — Given 读端关闭，When 写端继续 Write，Then 返回 `*SyscallError`，`Code` 为 `ErrBrokenPipe`

5. **Compose 集成预备** — Given 管道用于 Compose 编排，When 前置智能体完成后，Then 其输出通过管道自动注入下游智能体的上下文（注：此 AC 仅要求管道 API 设计兼容，实际 Compose 编排在 Epic 7 实现）

## Tasks / Subtasks

- [x] Task 1: 新增类型和接口扩展 (AC: #1, #4)
  - [x] 1.1 在 `internal/types/types.go` 中新增 `ErrBrokenPipe ErrCode = "BROKEN_PIPE"`
  - [x] 1.2 在 `kernel/ipc.go` 的 `IPCManager` 接口中新增 `Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, error)` 方法
  - [x] 1.3 更新 `var _ IPCManager = (*KernelImpl)(nil)` 编译期检查确保新接口满足
  - [x] 1.4 验证 Phase 1 全部现有测试通过（`make test`）

- [x] Task 2: VFS 扩展 — RegisterFD (AC: #1, #2)
  - [x] 2.1 在 `vfs/vfs.go` 中新增 `RegisterFD(pid types.PID, file VFSFile) types.FD` 方法
  - [x] 2.2 实现：获取或创建 pid 的 fdTable，调用 `t.alloc(file)` 分配 FD 并返回
  - [x] 2.3 在 `vfs/vfs_test.go` 中为 RegisterFD 添加单元测试

- [x] Task 3: 实现 pipeBuffer 共享缓冲区 (AC: #1, #3, #4)
  - [x] 3.1 在 `kernel/ipc.go` 中新增 `pipeBuffer` 结构体（`sync.Mutex` + `bytes.Buffer` + `chan struct{}` 通知 + `wrClosed`/`rdClosed` 标志）
  - [x] 3.2 实现 `newPipeBuffer() *pipeBuffer`
  - [x] 3.3 实现 `pipeBuffer.write(data []byte) (int, error)` — 写入数据并发信号，`rdClosed` 时返回 `ErrBrokenPipe`
  - [x] 3.4 实现 `pipeBuffer.read(length int, cancelCh <-chan struct{}) ([]byte, error)` — 阻塞读取，`wrClosed` 时缓冲区空返回 `io.EOF`，`cancelCh` 关闭时返回 `context.Canceled`
  - [x] 3.5 实现 `pipeBuffer.closeWrite()` — 标记写端关闭，发信号唤醒读端
  - [x] 3.6 实现 `pipeBuffer.closeRead()` — 标记读端关闭，发信号唤醒写端

- [x] Task 4: 实现 pipeReadEnd / pipeWriteEnd VFSFile (AC: #1, #2, #3, #4)
  - [x] 4.1 在 `kernel/ipc.go` 中新增 `pipeReadEnd` 结构体（`*pipeBuffer` + `cancelCh <-chan struct{}` + `closed bool` + `mu sync.Mutex`）
  - [x] 4.2 实现 `pipeReadEnd.Read(length int) ([]byte, error)` — 委托 `pipeBuffer.read(length, cancelCh)`
  - [x] 4.3 实现 `pipeReadEnd.Write(ctx, data) error` — 返回 ErrInvalid（只读端禁写）
  - [x] 4.4 实现 `pipeReadEnd.Close() error` — 调用 `pipeBuffer.closeRead()`，幂等
  - [x] 4.5 实现 `pipeReadEnd.Stat() (FileStat, error)` — 返回 `{Name: "pipe:read", IsDevice: true}`
  - [x] 4.6 在 `kernel/ipc.go` 中新增 `pipeWriteEnd` 结构体（`*pipeBuffer` + `closed bool` + `mu sync.Mutex`）
  - [x] 4.7 实现 `pipeWriteEnd.Read(length int) ([]byte, error)` — 返回 ErrInvalid（只写端禁读）
  - [x] 4.8 实现 `pipeWriteEnd.Write(ctx, data) error` — 委托 `pipeBuffer.write(data)`，检查 ctx 取消
  - [x] 4.9 实现 `pipeWriteEnd.Close() error` — 调用 `pipeBuffer.closeWrite()`，幂等
  - [x] 4.10 实现 `pipeWriteEnd.Stat() (FileStat, error)` — 返回 `{Name: "pipe:write", IsDevice: true}`
  - [x] 4.11 编译期接口检查：`var _ vfs.VFSFile = (*pipeReadEnd)(nil)` 和 `var _ vfs.VFSFile = (*pipeWriteEnd)(nil)`

- [x] Task 5: 实现 Pipe syscall (AC: #1, #2, #5)
  - [x] 5.1 实现 `func (k *KernelImpl) Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, error)`
  - [x] 5.2 Pipe 内部：验证 writerPID 和 readerPID 存在且非 Zombie/Dead
  - [x] 5.3 Pipe 内部：获取 readerPID 进程的 `ctx.Done()` 作为 cancelCh
  - [x] 5.4 Pipe 内部：创建 `pipeBuffer`
  - [x] 5.5 Pipe 内部：创建 `pipeWriteEnd` 和 `pipeReadEnd`（传入 cancelCh）
  - [x] 5.6 Pipe 内部：通过 `k.vfs.RegisterFD(writerPID, writeEnd)` 注册 writeFD
  - [x] 5.7 Pipe 内部：通过 `k.vfs.RegisterFD(readerPID, readEnd)` 注册 readFD
  - [x] 5.8 Pipe 入口/出口写入 SyscallEvent（emitEvent，记录双方 PID 和 FD）
  - [x] 5.9 所有错误包装为 `*SyscallError`（Syscall="Pipe"）

- [x] Task 6: 单元测试 (AC: #1-4)
  - [x] 6.1 `kernel/ipc_test.go` — TestPipe_Basic：同进程创建管道，写入后读取验证内容
  - [x] 6.2 TestPipe_CrossProcess：writerPID ≠ readerPID，A 写 B 读验证数据传递
  - [x] 6.3 TestPipe_WriteCloseEOF：关闭写端后，读端返回 io.EOF
  - [x] 6.4 TestPipe_ReadCloseBrokenPipe：关闭读端后，写端返回 ErrBrokenPipe
  - [x] 6.5 TestPipe_BlockUntilData：读端阻塞直到写端写入数据
  - [x] 6.6 TestPipe_CancelUnblocksRead：进程 context 取消后，阻塞的 Read 立即返回
  - [x] 6.7 TestPipe_Concurrent：多 goroutine 并发写入同一管道，读端全部读取无丢失无 race
  - [x] 6.8 TestPipe_LargeData：写入 ≥1MB 数据验证 NFR23 吞吐量
  - [x] 6.9 TestPipe_InvalidPID：不存在的 PID 返回 ErrNotFound
  - [x] 6.10 TestPipe_DeadProcess：Zombie/Dead 进程返回 ErrNotFound
  - [x] 6.11 TestPipe_SyscallEvent：验证 DebugChan 收到 Pipe 的 SyscallEvent
  - [x] 6.12 TestPipe_DoubleClose：重复关闭同一端不 panic（幂等）

- [x] Task 7: 集成验证 (AC: #1, #2, #5)
  - [x] 7.1 `make test` 全部通过（含 `-race`）
  - [x] 7.2 `make lint` 通过
  - [x] 7.3 `make build` 编译成功
  - [x] 7.4 验证 Phase 1 + Story 6.1 所有现有测试无回归

## Dev Notes

### 核心设计决策

**Pipe 作为 IPCManager 扩展**：遵循架构决策 Decision 1（分类接口组合），在 `IPCManager` 子接口中新增 `Pipe` 方法，与 `Send`/`Recv` 并列。这延续了 Story 6.1 确立的 IPC 扩展路径。

**内核内部 Pipe vs 跨终端**：与 Story 6.1 相同，Pipe 是**内核内部**进程间数据管道（同一 KernelImpl 内不同 Process 之间），实现在 `kernel/ipc.go`，不修改 `ipc/` 包。

**VFS FD 集成**：Pipe 的读写端实现 `vfs.VFSFile` 接口，通过 VFS 的 FD 系统管理。这确保进程退出时通过 `vfs.CloseAll()` 自动关闭管道端，与现有资源释放顺序兼容。

**双 PID API 设计**：`Pipe(writerPID, readerPID)` 而非 `Pipe(creatorPID)` — 直接将写端 FD 注册到 writer 进程的 fdTable，读端 FD 注册到 reader 进程的 fdTable。这避免了需要 `DupFD` 或 FD 传递机制，直接满足 AC #2 跨进程需求和 Epic 7 Compose 编排需求。

### 关键数据结构设计

```go
// kernel/ipc.go — 新增 Pipe 相关类型

// pipeBuffer 是管道的共享内部缓冲区
type pipeBuffer struct {
    mu       sync.Mutex
    buf      bytes.Buffer   // 动态增长的字节缓冲区
    notify   chan struct{}   // 缓冲 1 的信号量，通知状态变化
    wrClosed bool           // 写端是否已关闭
    rdClosed bool           // 读端是否已关闭
}

// pipeReadEnd 是管道的读端，实现 vfs.VFSFile
type pipeReadEnd struct {
    pipe     *pipeBuffer
    cancelCh <-chan struct{}  // 读进程的 ctx.Done()，用于取消阻塞读
    mu       sync.Mutex
    closed   bool
}

// pipeWriteEnd 是管道的写端，实现 vfs.VFSFile
type pipeWriteEnd struct {
    pipe   *pipeBuffer
    mu     sync.Mutex
    closed bool
}
```

### VFS 扩展

```go
// vfs/vfs.go — 新增 RegisterFD 方法

// RegisterFD registers a pre-created VFSFile in a process's fdTable.
// Used by the kernel to register pipe endpoints without going through device registry.
func (v *VFS) RegisterFD(pid types.PID, file VFSFile) types.FD {
    t := v.getOrCreateFDTable(pid)
    return t.alloc(file)
}
```

### IPCManager 接口扩展

```go
// kernel/ipc.go — IPCManager 新增 Pipe 方法

type IPCManager interface {
    Send(senderPID, targetPID types.PID, data []byte) error
    Recv(pid types.PID) (*Message, error)
    Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, error)
}
```

### Pipe 实现要点

```go
func (k *KernelImpl) Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, error) {
    start := time.Now()

    // 1. 验证 writerPID 存在且非 Dead/Zombie
    writerProc, ok := k.GetProcess(writerPID)
    if !ok {
        return 0, 0, NewSyscallError("Pipe", writerPID, "",
            fmt.Errorf("writer process not found"), types.ErrNotFound)
    }
    if state := writerProc.GetState(); state == types.StateZombie || state == types.StateDead {
        return 0, 0, NewSyscallError("Pipe", writerPID, "",
            fmt.Errorf("writer process %d is %s", writerPID, state), types.ErrNotFound)
    }

    // 2. 验证 readerPID 存在且非 Dead/Zombie
    readerProc, ok := k.GetProcess(readerPID)
    if !ok {
        return 0, 0, NewSyscallError("Pipe", readerPID, "",
            fmt.Errorf("reader process not found"), types.ErrNotFound)
    }
    if state := readerProc.GetState(); state == types.StateZombie || state == types.StateDead {
        return 0, 0, NewSyscallError("Pipe", readerPID, "",
            fmt.Errorf("reader process %d is %s", readerPID, state), types.ErrNotFound)
    }

    // 3. 创建管道
    pipe := newPipeBuffer()
    writeEnd := &pipeWriteEnd{pipe: pipe}
    readEnd := &pipeReadEnd{pipe: pipe, cancelCh: readerProc.ctx.Done()}

    // 4. 注册 FD（通过 VFS RegisterFD）
    wfd := k.vfs.RegisterFD(writerPID, writeEnd)
    rfd := k.vfs.RegisterFD(readerPID, readEnd)

    // 5. SyscallEvent（以 writerProc 为事件来源）
    k.emitEvent(writerProc, "Pipe", map[string]any{
        "writer_pid": writerPID,
        "reader_pid": readerPID,
        "write_fd":   wfd,
        "read_fd":    rfd,
    }, nil, nil, time.Since(start))

    return wfd, rfd, nil
}
```

### pipeBuffer 核心方法

```go
func newPipeBuffer() *pipeBuffer {
    return &pipeBuffer{
        notify: make(chan struct{}, 1),
    }
}

func (p *pipeBuffer) write(data []byte) (int, error) {
    p.mu.Lock()
    if p.rdClosed {
        p.mu.Unlock()
        return 0, types.NewDriverError("Write", "pipe",
            fmt.Errorf("broken pipe"), types.ErrBrokenPipe)
    }
    if p.wrClosed {
        p.mu.Unlock()
        return 0, types.NewDriverError("Write", "pipe",
            fmt.Errorf("write end closed"), types.ErrInvalid)
    }
    n, _ := p.buf.Write(data)  // bytes.Buffer.Write never fails
    p.mu.Unlock()

    // 非阻塞信号通知读端
    select {
    case p.notify <- struct{}{}:
    default:
    }
    return n, nil
}

func (p *pipeBuffer) read(length int, cancelCh <-chan struct{}) ([]byte, error) {
    for {
        p.mu.Lock()
        if p.buf.Len() > 0 {
            // 有数据可读
            data := make([]byte, min(length, p.buf.Len()))
            n, _ := p.buf.Read(data)
            p.mu.Unlock()
            return data[:n], nil
        }
        if p.wrClosed {
            // 写端已关闭且缓冲区空 → EOF
            p.mu.Unlock()
            return nil, io.EOF
        }
        p.mu.Unlock()

        // 阻塞等待数据、写端关闭或 context 取消
        select {
        case <-p.notify:
            // 重新检查
        case <-cancelCh:
            return nil, context.Canceled
        }
    }
}

func (p *pipeBuffer) closeWrite() {
    p.mu.Lock()
    p.wrClosed = true
    p.mu.Unlock()
    // 唤醒阻塞的读端
    select {
    case p.notify <- struct{}{}:
    default:
    }
}

func (p *pipeBuffer) closeRead() {
    p.mu.Lock()
    p.rdClosed = true
    p.mu.Unlock()
    // 唤醒写端（如果有 goroutine 在等待写入空间）
    select {
    case p.notify <- struct{}{}:
    default:
    }
}
```

### pipeReadEnd 实现（vfs.VFSFile）

```go
func (r *pipeReadEnd) Read(length int) ([]byte, error) {
    return r.pipe.read(length, r.cancelCh)
}

func (r *pipeReadEnd) Write(_ context.Context, _ []byte) error {
    return types.NewDriverError("Write", "pipe:read",
        fmt.Errorf("cannot write to read end of pipe"), types.ErrInvalid)
}

func (r *pipeReadEnd) Close() error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.closed {
        return nil  // 幂等
    }
    r.closed = true
    r.pipe.closeRead()
    return nil
}

func (r *pipeReadEnd) Stat() (vfs.FileStat, error) {
    return vfs.FileStat{Name: "pipe:read", IsDevice: true, DevicePath: "pipe"}, nil
}
```

### pipeWriteEnd 实现（vfs.VFSFile）

```go
func (w *pipeWriteEnd) Read(_ int) ([]byte, error) {
    return nil, types.NewDriverError("Read", "pipe:write",
        fmt.Errorf("cannot read from write end of pipe"), types.ErrInvalid)
}

func (w *pipeWriteEnd) Write(ctx context.Context, data []byte) error {
    // 检查 ctx 取消
    select {
    case <-ctx.Done():
        return types.NewDriverError("Write", "pipe",
            ctx.Err(), types.ErrTimeout)
    default:
    }

    _, err := w.pipe.write(data)
    return err
}

func (w *pipeWriteEnd) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.closed {
        return nil  // 幂等
    }
    w.closed = true
    w.pipe.closeWrite()
    return nil
}

func (w *pipeWriteEnd) Stat() (vfs.FileStat, error) {
    return vfs.FileStat{Name: "pipe:write", IsDevice: true, DevicePath: "pipe"}, nil
}
```

### Read 阻塞取消机制

**关键问题**：`vfs.VFSFile.Read(length int)` 不接受 `context.Context` 参数，但管道读取需要支持进程取消（Kill 后 Read 必须解除阻塞）。

**解决方案**：`pipeReadEnd` 构造时持有读进程的 `ctx.Done()` channel（`cancelCh`）。当进程被 Kill，`ctx.Done()` 关闭，阻塞的 `pipeBuffer.read` 通过 `select` 立即返回 `context.Canceled`。

**资源释放兼容性**：进程退出流程中，`cancel()` 先于 `CloseAll()`。管道 Read 因 `cancelCh` 关闭而解除阻塞 → goroutine 继续执行 → `defer CloseAll()` 关闭管道端 → `wg.Done()` → reapProcess 继续。无死锁风险。

### 进程退出时管道自动清理

**场景 A — 写端进程先退出：**
```
writerProc 退出 → CloseAll() → pipeWriteEnd.Close() → pipe.closeWrite()
→ 读端下次 Read 发现 wrClosed && buf 空 → 返回 io.EOF
```

**场景 B — 读端进程先退出：**
```
readerProc 退出 → CloseAll() → pipeReadEnd.Close() → pipe.closeRead()
→ 写端下次 Write 发现 rdClosed → 返回 ErrBrokenPipe
```

**场景 C — 双方同时退出：**
```
CloseAll() 各自关闭自己的管道端 → pipeBuffer 无外部引用 → GC 回收
```

无需额外的 pipe 注册表或引用计数，GC 自动处理。

### 错误码使用

| 场景 | Syscall | ErrCode | 说明 |
|------|---------|---------|------|
| writerPID 不存在 | Pipe | ErrNotFound | procTable 中找不到 |
| readerPID 不存在 | Pipe | ErrNotFound | procTable 中找不到 |
| writer/reader Zombie/Dead | Pipe | ErrNotFound | 进程已终止 |
| 写端已关闭 + 缓冲区空 | Read（VFS 层） | io.EOF | 正常 EOF |
| 读端已关闭 | Write（VFS 层） | ErrBrokenPipe | 读端已关闭 |
| 读端 context 取消 | Read（VFS 层） | context.Canceled | Kill 或超时 |
| 写读端 Write 错误 | Write（VFS 层） | ErrInvalid | 只读端禁写 |
| 写端 Read 错误 | Read（VFS 层） | ErrInvalid | 只写端禁读 |

### SyscallEvent 记录规范

Pipe syscall 通过 `emitEvent` 记录：
- **Syscall 字段值**：`"Pipe"`
- **Args 记录**：`writer_pid`, `reader_pid`, `write_fd`, `read_fd`
- **事件归属进程**：writerProc（作为管道创建的发起方记录）
- **DebugChan 为 nil 时跳过**（零开销）

管道数据读写通过 VFS 的标准 Read/Write 路径，已有 SyscallEvent 记录（来自 reasonStep 的 emitEvent），无需在管道端重复记录。

### 反模式警告

- **禁止在 `kernel/` 外创建管道实现文件**：Pipe 实现全部在 `kernel/ipc.go` 中，pipeReadEnd/pipeWriteEnd 也在此文件
- **禁止返回裸 error**：所有 Pipe 错误必须包装为 `*SyscallError`；VFSFile 方法中的错误使用 `*types.DriverError` 以便 VFS 层正确提取 ErrCode
- **禁止使用 `sync.Mutex + map` 管理管道**：无需全局管道注册表，管道生命周期由 VFS FD 系统管理
- **禁止修改 `ipc/` 包**：`ipc/` 是跨终端通信（CLI ↔ daemon），与内核 Pipe 无关
- **禁止在 pipeBuffer.write 中阻塞**：Write 必须立即写入缓冲区（无容量限制），保持非阻塞语义
- **禁止忽略 cancelCh**：pipeBuffer.read 的 select 必须包含 cancelCh 分支，否则进程 Kill 导致死锁
- **禁止在 Read/Write 中添加 context 参数到 VFSFile.Read**：Read 接口签名不变，通过构造时注入 cancelCh 解决

### 并发安全要点

1. **pipeBuffer 内部**：`sync.Mutex` 保护 `bytes.Buffer`、`wrClosed`、`rdClosed` 标志
2. **pipeReadEnd/pipeWriteEnd**：各自 `sync.Mutex` 保护 `closed` 标志（Close 幂等）
3. **notify 信号量**：缓冲 1 的 `chan struct{}`，write/closeWrite/closeRead 时非阻塞发送（`select { case notify <- struct{}{}: default: }`），read 循环保证不丢消息
4. **cancelCh**：只读 channel（`<-chan struct{}`），由进程 context 管理，无竞争
5. **VFS RegisterFD**：fdTable 内部有 `sync.RWMutex` 保护，并发安全
6. **多 goroutine 并发写入测试**：参考 Story 6.1 `TestSend_Concurrent` 的 100 goroutine 模式

### NFR 合规

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR23 | Pipe 吞吐量 ≥ 1MB/s | 纯内存 bytes.Buffer + mutex，预期 >> 100MB/s |
| NFR19 | 不破坏 Phase 1 ABI | IPCManager 新增方法，现有方法签名不变 |
| NFR24 | ≥10 并发进程表操作 ≤ 2x 延迟 | 管道通过 VFS FD 系统管理，不增加进程表压力 |

### 与 Story 6.1 的关系

Story 6.1 建立了 IPC 基础：`IPCManager` 接口、`KernelImpl` 的 IPC 字段、SyscallEvent 记录模式、进程生命周期集成。Story 6.2 在此基础上扩展：
- 复用 IPCManager 接口扩展模式
- 复用 emitEvent 记录 SyscallEvent
- 复用进程验证逻辑（GetProcess + 状态检查）
- 不修改 Story 6.1 已有的 Send/Recv/MessageQueue 实现

### 与 Epic 7 (Compose) 的前瞻

AC #5 要求 Pipe API 兼容 Compose 编排。设计预留：
- `Pipe(writerPID, readerPID)` 的双 PID 签名允许 Compose 编排器在 Spawn 后直接连接管道
- Compose 流程：创建进程 A → 创建进程 B → `Pipe(A.PID, B.PID)` → A 的输出通过 writeFD 流向 B 的 readFD
- 无需 FD 传递或 dup 机制，编排器直接创建管道到指定进程

### Project Structure Notes

**新增文件：**
```
无新文件 — 所有代码添加到现有文件中
```

**修改文件：**
```
kernel/ipc.go           — IPCManager 接口新增 Pipe + pipeBuffer/pipeReadEnd/pipeWriteEnd + Pipe 实现
kernel/ipc_test.go      — Pipe 单元测试（12 个测试用例）
vfs/vfs.go              — 新增 RegisterFD 方法
vfs/vfs_test.go         — RegisterFD 单元测试
internal/types/types.go — 新增 ErrBrokenPipe ErrCode
```

**不修改的文件：**
```
kernel/kernel.go        — KernelImpl 无需新字段（管道通过 VFS FD 管理）
kernel/reap.go          — 资源释放顺序不变（CloseAll 自动关闭管道端）
kernel/process.go       — Process 结构体不变
ipc/                    — 跨终端 IPC daemon，与内核 Pipe 无关
cmd/crux/main.go        — CLI 层不直接暴露 Pipe（由 Compose 编排使用）
```

### 必需导入

```go
// kernel/ipc.go 新增导入
import (
    "bytes"      // pipeBuffer
    "io"         // io.EOF
    "context"    // context.Canceled（已有 gocontext 别名引用）

    "github.com/gonewx/crux/vfs"  // VFSFile 接口、FileStat、VFS.RegisterFD
)
```

**注意**：`kernel` 包当前不导入 `vfs` 包。但 `kernel/process.go` 已有 `"github.com/gonewx/crux/vfs"` 导入（FDTable 的类型是 `map[types.FD]vfs.VFSFile`）。因此 `kernel/ipc.go` 中导入 `vfs` 不会引入新的依赖方向。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-6-ipc-跨进程通信inter-process-communication.md#Story 6.2] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/epics/epic-list.md#Epic 6] — Epic 概述和 NFR（NFR22, NFR23, NFR24）
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR23] — Pipe 吞吐量 ≥ 1MB/s
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 1] — Syscall ABI 分类接口组合设计
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 3] — VFS 实现策略（FD 管理）
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 6] — 错误处理与恢复（SyscallError）
- [Source: _bmad-output/implementation-artifacts/6-1-send-recv-messaging.md] — Story 6.1 完整实现细节（IPCManager 模式、错误码、SyscallEvent 模式）
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文
- [Source: kernel/ipc.go] — 现有 IPCManager 接口、Message、MessageQueue、Send/Recv 实现
- [Source: kernel/ipc_test.go] — IPC 测试模式（newIPCTestProcess 辅助函数、newSimpleKernel）
- [Source: kernel/kernel.go] — KernelImpl 结构体、Spawn 流程、emitEvent 模式
- [Source: kernel/process.go:31-54] — Process 结构体（PID, ctx, cancel, wg, mu 等字段）
- [Source: kernel/reap.go:13-48] — reapProcess 资源释放顺序（cancel → wg.Wait → CloseAll → DebugChan → msgQueues → CtxFree → Reap → Remove）
- [Source: kernel/errors.go:10-16] — SyscallError 定义和 NewSyscallError 工厂
- [Source: vfs/vfs.go] — VFS 实现（fdTable、Open、Read、Write、Close、CloseAll）
- [Source: vfs/vfs.go:32-37] — VFSFile 接口定义（Read/Write/Close/Stat）
- [Source: internal/types/types.go:18-30] — ErrCode 枚举（需新增 ErrBrokenPipe）
- [Source: _bmad-output/planning-artifacts/epics/epic-list.md#Epic 7] — Compose 多智能体编排依赖 Pipe

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

### Completion Notes List

- ✅ Task 1: 新增 `ErrBrokenPipe` ErrCode，扩展 `IPCManager` 接口添加 `Pipe` 方法，编译期检查通过
- ✅ Task 2: VFS 新增 `RegisterFD` 方法，允许内核直接注册 VFSFile 到进程 fdTable（无需通过 DeviceRegistry），含 4 个子测试
- ✅ Task 3: 实现 `pipeBuffer` 共享缓冲区，基于 `sync.Mutex` + `bytes.Buffer` + 缓冲 1 的 `chan struct{}` 信号量，支持阻塞读、非阻塞写、双向关闭通知
- ✅ Task 4: 实现 `pipeReadEnd`/`pipeWriteEnd` 两个 `vfs.VFSFile` 实现，含编译期接口检查、幂等 Close、cancelCh 支持
- ✅ Task 5: 实现 `Pipe` syscall，验证双方进程状态、创建管道、通过 VFS RegisterFD 注册 FD、发射 SyscallEvent，错误统一包装为 `*SyscallError`
- ✅ Task 6: 12 个 Pipe 测试全部通过（含 `-race`）：基本读写、跨进程传递、写端关闭 EOF、读端关闭 BrokenPipe、阻塞读、context 取消、并发写、1MB 吞吐量、无效 PID、Dead 进程、SyscallEvent、双重关闭
- ✅ Task 7: 全量回归测试通过（kernel, vfs, context, debug, agents, skills, internal 等包），go vet 通过，编译成功

### File List

- `internal/types/types.go` — 新增 `ErrBrokenPipe ErrCode`
- `kernel/ipc.go` — 扩展 IPCManager 接口 + pipeBuffer + pipeReadEnd + pipeWriteEnd + Pipe 实现（~210 行新增）
- `kernel/ipc_test.go` — 12 个 Pipe 测试用例 + 新增 imports（errors, vfs）
- `vfs/vfs.go` — 新增 `RegisterFD` 方法
- `vfs/vfs_test.go` — 4 个 RegisterFD 子测试

### Change Log

- 2026-02-28: Story 6.2 Pipe 管道实现完成，所有 7 个 Task 及子任务完成