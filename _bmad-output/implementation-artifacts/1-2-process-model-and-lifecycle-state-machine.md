# Story 1.2: 进程模型与生命周期状态机

Status: in-progress

## Story

As a 内核开发者,
I want Process 结构体支持完整的生命周期状态转移,
So that 智能体进程可以在 Created → Running → Zombie → Dead 之间安全转换。

## Acceptance Criteria

1. **Process 结构体** — Given `kernel/process.go` 已实现，When 创建一个新 Process，Then 初始状态为 `Created`，拥有唯一 PID（`atomic.AddUint64` 递增，不回收），And 包含 `Intent`（不可变）、`Skills`、`Children`、`FDTable`、`DebugChan`、`Done` 字段
2. **Created → Running** — Given Process 处于 `Created` 状态，When 调用状态转移到 `Running`，Then 状态成功变为 `Running`
3. **Running → Zombie** — Given Process 处于 `Running` 状态，When 调用状态转移到 `Zombie`（正常完成/错误/超时/kill），Then 状态成功变为 `Zombie`，`ExitStatus` 被记录
4. **Zombie → Dead** — Given Process 处于 `Zombie` 状态，When 调用状态转移到 `Dead`（wait 回收），Then 状态变为 `Dead`
5. **非法转移拒绝** — Given 尝试非法状态转移（如 Running→Created、Zombie→Running、Dead→任何状态），When 调用转移方法，Then 返回错误，状态保持不变
6. **进程表并发安全** — Given `KernelImpl` 持有进程表，When 多个 goroutine 并发访问进程表，Then 通过 `SyncMap[PID, *Process]` 保证并发安全，And 通过 `go test -race` 无数据竞争

## Tasks / Subtasks

- [ ] Task 1: 实现 Process 结构体与 ExitStatus (AC: #1)
  - [ ] 1.1 定义 `ExitStatus` 结构体（`Code int`、`Reason string`、`Err error`）→ `kernel/process.go`
  - [ ] 1.2 定义 `Process` 结构体（PID、PPID、State、Intent、Skills、Children、FDTable、DebugChan、Done、CreatedAt、ExitStatus、mu、cancel、wg）→ `kernel/process.go`
  - [ ] 1.3 实现包级 PID 分配器（`atomic.AddUint64` 全局递增，不回收），PID 从 1 开始
  - [ ] 1.4 实现 `NewProcess(ppid PID, intent string, skills []string) *Process` 构造函数，初始状态 `StateCreated`，自动分配 PID
- [ ] Task 2: 实现状态机转移逻辑 (AC: #2, #3, #4, #5)
  - [ ] 2.1 定义合法转移表 `var validTransitions map[ProcessState][]ProcessState`
  - [ ] 2.2 实现 `(p *Process) Transition(target ProcessState) error` 方法，通过 `sync.Mutex` 保护状态转移原子性
  - [ ] 2.3 非法转移返回 `*SyscallError`（Code: `ErrInternal`，Syscall: "transition"）
  - [ ] 2.4 实现便捷方法 `Start() error`（Created→Running）、`Terminate(exit ExitStatus) error`（Running→Zombie，记录 ExitStatus）、`Reap() error`（Zombie→Dead）
- [ ] Task 3: 实现进程表与 KernelImpl 基础 (AC: #6)
  - [ ] 3.1 定义 `KernelImpl` 结构体（`procTable *xsync.SyncMap[types.PID, *Process]`、`pidCounter *atomic.Uint64`）→ `kernel/kernel.go`
  - [ ] 3.2 实现 `NewKernel() *KernelImpl` 构造函数
  - [ ] 3.3 实现 `(k *KernelImpl) AddProcess(p *Process)`、`GetProcess(pid PID) (*Process, bool)`、`RemoveProcess(pid PID)`、`ListProcesses() []*Process`
- [ ] Task 4: 编写完整单元测试 (AC: all)
  - [ ] 4.1 状态机合法转移测试（Created→Running→Zombie→Dead 完整路径）→ `kernel/process_test.go`
  - [ ] 4.2 状态机非法转移测试（Running→Created、Zombie→Running、Dead→任何状态 均返回错误且状态不变）
  - [ ] 4.3 PID 唯一性和单调递增测试（并发分配 100 个 PID，全部唯一且递增）
  - [ ] 4.4 Process 字段初始化测试（Intent 不可变、初始状态 Created、Children 为空切片）
  - [ ] 4.5 进程表并发安全测试（100 个 goroutine 并发 Add/Get/Remove/List）→ `kernel/kernel_test.go`
  - [ ] 4.6 全部测试通过 `go test -race ./kernel/...`

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** `kernel/process.go` 放 Process 结构体 + 状态机，`kernel/kernel.go` 放 KernelImpl + 进程表。不要合并到一个文件
- **依赖方向：** `kernel/` 可导入 `internal/types/` 和 `internal/xsync/`，但 `internal/` 不导入 `kernel/`
- **此 Story 不实现：** reasonStep 循环、Spawn syscall、资源释放逻辑、VFS 集成。仅关注 Process 数据结构和状态机
- **VFSFile 占位：** FDTable 使用 `map[types.FD]any` 作为占位，Story 1.3 定义 VFSFile 后再替换为具体类型

### 已有代码（Story 1-1 产出，必须复用）

**`internal/types/types.go` — 已定义的类型（直接导入，禁止重复定义）：**

```go
type PID uint64                    // 进程 ID
type FD int                        // 文件描述符
type CtxID uint64                  // 上下文 ID
type ErrCode string                // 错误码
type Signal int                    // 信号
type ProcessState int              // 进程状态

const (
    StateCreated ProcessState = iota  // 0
    StateRunning                      // 1
    StateZombie                       // 2
    StateDead                         // 3
)

const (
    SIGTERM Signal = iota + 1  // 1
    SIGKILL                    // 2
)

type SyscallEvent struct {
    Timestamp time.Duration
    PID       PID
    Syscall   string
    Args      map[string]any
    Result    any
    Err       error
    Duration  time.Duration
}
```

**`internal/xsync/syncmap.go` — SyncMap（用于进程表）：**

```go
type SyncMap[K comparable, V any] struct { mu sync.RWMutex; m map[K]V }
func NewSyncMap[K, V]() *SyncMap[K, V]
func (s *SyncMap[K, V]) Load(key K) (V, bool)
func (s *SyncMap[K, V]) Store(key K, value V)
func (s *SyncMap[K, V]) Delete(key K)
func (s *SyncMap[K, V]) Range(fn func(K, V) bool)
func (s *SyncMap[K, V]) Len() int
```

**`kernel/errors.go` — SyscallError（状态转移错误使用此类型）：**

```go
type SyscallError struct {
    Syscall string
    PID     types.PID
    Device  string
    Err     error
    Code    types.ErrCode
}
func NewSyscallError(syscall string, pid types.PID, device string, err error, code types.ErrCode) *SyscallError
```

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写单词 | `kernel` |
| 导出类型 | PascalCase | `Process`, `ExitStatus`, `KernelImpl` |
| 非导出字段 | camelCase | `procTable`, `pidCounter` |
| 错误变量 | `Err` 前缀 | `ErrIllegalTransition` |
| 构造函数 | `New` 前缀 | `NewProcess`, `NewKernel` |

### Process 结构体设计规范

```go
// kernel/process.go
package kernel

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/gonewx/crux/internal/types"
)

// ExitStatus 记录进程退出信息
type ExitStatus struct {
    Code   int       // 0=正常, 非零=异常
    Reason string    // 退出原因描述
    Err    error     // 底层错误（如有）
}

// Process 表示一个智能体进程
type Process struct {
    PID       types.PID
    PPID      types.PID
    State     types.ProcessState    // 通过 mu 保护
    Intent    string                // 不可变，创建后不修改
    Skills    []string
    Children  []types.PID
    FDTable   map[types.FD]any      // 占位，Story 1.3 替换为 VFSFile
    DebugChan chan types.SyscallEvent
    Done      chan ExitStatus
    CreatedAt time.Time
    Exit      *ExitStatus           // Zombie/Dead 时非 nil

    mu     sync.Mutex              // 保护 State 和 Exit 的并发访问
    cancel context.CancelFunc      // goroutine 取消
    wg     sync.WaitGroup          // 子 goroutine 等待
}
```

**关键设计决策：**
- `mu sync.Mutex` 保护状态转移原子性（不用 RWMutex，因为状态转移是写操作，读操作可通过原子操作优化，但 MVP 阶段 Mutex 足够简洁）
- `Done chan ExitStatus` 缓冲为 1，确保写入不阻塞
- `DebugChan` 创建时为 nil（无 astrace 时零开销），后续 Story 使用时设置
- `cancel` 和 `wg` 暴露为非导出字段，仅内核内部使用
- `FDTable` 初始化为空 map，不是 nil

### 状态转移规则

```
合法转移（必须严格遵循）：
  Created  → Running    （Start）
  Running  → Zombie     （Terminate：正常完成 / 错误 / 超时 / kill）
  Zombie   → Dead       （Reap：wait 回收 + 资源释放）

禁止的转移（必须返回错误）：
  Running  → Created
  Zombie   → Running
  Zombie   → Created
  Dead     → 任何状态
  Created  → Zombie     （必须先经过 Running）
  Created  → Dead       （必须先经过 Running → Zombie）
```

**转移方法签名：**

```go
// 通用转移方法，内部校验合法性
func (p *Process) Transition(target types.ProcessState) error

// 便捷方法
func (p *Process) Start() error                    // Created → Running
func (p *Process) Terminate(exit ExitStatus) error // Running → Zombie，记录 exit
func (p *Process) Reap() error                     // Zombie → Dead
```

### PID 分配器规范

```go
// 包级全局 PID 计数器，atomic 保证并发安全
var pidCounter atomic.Uint64

// nextPID 返回下一个唯一 PID（从 1 开始，单调递增，不回收）
func nextPID() types.PID {
    return types.PID(pidCounter.Add(1))
}
```

**注意：** PID 分配器也可以放在 KernelImpl 中作为实例字段，具体取决于是否需要多 Kernel 实例。MVP 阶段使用包级变量即可（全局唯一 Kernel）。

### KernelImpl 进程表规范

```go
// kernel/kernel.go
package kernel

import (
    "github.com/gonewx/crux/internal/types"
    "github.com/gonewx/crux/internal/xsync"
)

// KernelImpl 是微内核的核心实现
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
}

func NewKernel() *KernelImpl {
    return &KernelImpl{
        procTable: xsync.NewSyncMap[types.PID, *Process](),
    }
}
```

**进程表操作使用 SyncMap 的 API：**
- `Store(pid, proc)` — 添加进程
- `Load(pid)` — 获取进程
- `Delete(pid)` — 移除进程
- `Range(fn)` — 遍历所有进程（用于 ListProcesses）

### 错误处理模式

非法状态转移必须返回 `*SyscallError`：

```go
// 非法转移错误示例
return kernel.NewSyscallError(
    "transition",           // Syscall 名称
    p.PID,                  // 进程 PID
    "",                     // Device 为空（非设备操作）
    fmt.Errorf("illegal transition: %d → %d", p.State, target),
    types.ErrInternal,      // 错误码
)
```

### 测试规范

**测试文件位置：**
- `kernel/process_test.go` — Process 结构体和状态机测试
- `kernel/kernel_test.go` — KernelImpl 和进程表测试

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestNewProcess` | 初始状态 Created、PID > 0、Intent 正确、Children 为空切片 |
| `TestLegalTransitions` | Created→Running→Zombie→Dead 完整路径成功 |
| `TestStart` | Created→Running 成功 |
| `TestTerminate` | Running→Zombie 成功，ExitStatus 被记录 |
| `TestReap` | Zombie→Dead 成功 |
| `TestIllegalTransitions` | Running→Created 失败、Zombie→Running 失败、Dead→Running 失败，状态不变 |
| `TestPIDUniqueness` | 并发分配 100 个 PID，全部唯一 |
| `TestPIDMonotonic` | 连续分配的 PID 单调递增 |
| `TestProcessTableConcurrent` | 100 goroutine 并发 Store/Load/Delete/Range |

**测试模式（对齐 Story 1-1 的测试风格）：**
- 使用 Go 标准 `testing` 包，不引入 testify（Story 1-1 未使用）
- 使用 `t.Run` 子测试组织
- 使用 `t.Logf` 输出调试信息（不用 `fmt.Printf`）
- 并发测试使用 `sync.WaitGroup` 等待所有 goroutine

### Story 1-1 经验教训（必须吸收）

1. **Future[T] 的 data race 修复：** 初始实现使用 channel 重建方式存在 data race，改为 `chan struct{}` + `sync.Once`。Process 的状态转移也需要注意并发安全，使用 `sync.Mutex` 保护
2. **SyscallEvent.Timestamp 类型修正：** 是 `time.Duration` 不是 `time.Time`（相对进程启动时间）
3. **SyscallEvent.Args 类型修正：** 是 `map[string]any` 不是 `[]any`
4. **测试中使用 `t.Logf` 而非 `fmt.Printf`**
5. **make lint 依赖 golangci-lint 安装** — 测试验证时使用 `make test` 而非 `make lint`

### 完整目录结构（本 Story 变更）

```
kernel/
├── errors.go          (已有 — Story 1-1)
├── errors_test.go     (已有 — Story 1-1)
├── process.go         (新增 — Task 1, 2)
├── process_test.go    (新增 — Task 4)
├── kernel.go          (新增 — Task 3)
└── kernel_test.go     (新增 — Task 4)
```

### References

- [Source: architecture.md#Decision 2: 进程模型与并发] — Process 结构体定义、PID 分配、goroutine 生命周期
- [Source: architecture.md#Implementation Patterns > 过程模式] — 进程状态转移规则、资源释放顺序
- [Source: architecture.md#Implementation Patterns > 命名模式] — Go 代码命名规则
- [Source: epics.md#Story 1.2] — 原始用户故事和 AC
- [Source: 1-1-project-initialization-and-infrastructure.md] — 前序 Story 产出、经验教训、已建立的代码模式
