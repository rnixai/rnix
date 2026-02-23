# Story 1.3: VFS 框架与设备注册

Status: ready-for-dev

## Story

As a 智能体,
I want 通过统一的 VFS 接口访问所有设备（LLM、文件系统、Shell）,
So that 我不需要知道底层实现细节，只需操作文件描述符。

## Acceptance Criteria

1. **VFS 接口完整** — Given `vfs/vfs.go` 已实现，When 调用 VFS 操作，Then 提供 `Open(path, flags) (FD, error)`、`Read(fd, length) ([]byte, error)`、`Write(fd, data) error`、`Close(fd) error`、`Stat(path) (FileStat, error)` 接口，And `VFSFile` 接口定义 `Read`、`Write`、`Close`、`Stat` 方法
2. **设备注册** — Given `vfs/dev.go` 已实现，When 调用 `DeviceRegistry.Register("/dev/llm/claude", factory)`，Then 后续 `Open("/dev/llm/claude", O_RDWR)` 返回驱动封装的 `VFSFile`
3. **FD 分配与 FDTable** — Given 设备已注册，When 调用 `Open` 传入已注册路径，Then 返回进程内递增的 FD，FD 存入进程的 `FDTable`
4. **未注册路径错误** — Given 设备未注册，When 调用 `Open` 传入未注册路径，Then 返回 `*SyscallError`，`Code` 为 `ErrNotFound`
5. **Close 与 FD 失效** — Given FD 已打开，When 调用 `Close(fd)`，Then FD 从 `FDTable` 移除，后续 `Read`/`Write` 该 FD 返回错误

## Tasks / Subtasks

- [ ] Task 1: 定义 VFSFile 接口与核心类型 (AC: #1)
  - [ ] 1.1 在 `vfs/vfs.go` 中定义 `VFSFile` 接口（`Read(length int) ([]byte, error)`、`Write(data []byte) error`、`Close() error`、`Stat() (FileStat, error)`）
  - [ ] 1.2 定义 `FileStat` 结构体（`Name string`、`Size int64`、`IsDevice bool`、`DevicePath string`）
  - [ ] 1.3 定义 `OpenFlag int` 常量（`O_RDONLY`、`O_WRONLY`、`O_RDWR`）
  - [ ] 1.4 定义 `VFSFileFactory func(flags OpenFlag) (VFSFile, error)` 工厂函数类型
- [ ] Task 2: 实现 DeviceRegistry (AC: #2)
  - [ ] 2.1 在 `vfs/dev.go` 中定义 `DeviceRegistry` 结构体，内部基于 `xsync.Registry[VFSFileFactory]`
  - [ ] 2.2 实现 `NewDeviceRegistry() *DeviceRegistry`
  - [ ] 2.3 实现 `Register(path string, factory VFSFileFactory) error`（委托给 `Registry[T].Register`，路径重复返回错误）
  - [ ] 2.4 实现 `Open(path string, flags OpenFlag) (VFSFile, error)`（查找 factory → 调用 factory(flags) → 返回 VFSFile；未找到返回 `*SyscallError{Code: ErrNotFound}`）
  - [ ] 2.5 实现路径前缀匹配：`Open("/dev/fs/path/to/file")` 应匹配注册的 `/dev/fs` 前缀，并将剩余路径传递给驱动（通过 factory 参数或 VFSFile 内部设置）
- [ ] Task 3: 实现 VFS 核心结构体 (AC: #1, #3, #4, #5)
  - [ ] 3.1 在 `vfs/vfs.go` 中定义 `VFS` 结构体（持有 `devRegistry *DeviceRegistry`）
  - [ ] 3.2 实现 `NewVFS(devRegistry *DeviceRegistry) *VFS`
  - [ ] 3.3 实现 `Open(pid types.PID, path string, flags OpenFlag) (types.FD, error)`：通过 devRegistry.Open 获取 VFSFile → 分配 FD → 存入 fdTable → 返回 FD
  - [ ] 3.4 实现 `Read(pid types.PID, fd types.FD, length int) ([]byte, error)`：从 fdTable 查找 VFSFile → 调用 file.Read
  - [ ] 3.5 实现 `Write(pid types.PID, fd types.FD, data []byte) error`：从 fdTable 查找 VFSFile → 调用 file.Write
  - [ ] 3.6 实现 `Close(pid types.PID, fd types.FD) error`：调用 file.Close → 从 fdTable 移除 FD
  - [ ] 3.7 实现 `Stat(path string) (FileStat, error)`：通过 devRegistry 查询设备信息
  - [ ] 3.8 FD 分配策略：每进程维护 `fdCounter`，递增分配，从 FD(3) 开始（0/1/2 保留给 stdin/stdout/stderr 惯例）
- [ ] Task 4: FDTable 管理 (AC: #3, #5)
  - [ ] 4.1 在 VFS 中实现每进程 FDTable 管理：`fdTables map[types.PID]*fdTable` 或 `SyncMap[types.PID, *fdTable]`
  - [ ] 4.2 `fdTable` 结构：`files map[types.FD]VFSFile` + `nextFD types.FD` + `mu sync.Mutex`
  - [ ] 4.3 实现 `allocFD(pid) types.FD`（递增分配）
  - [ ] 4.4 实现 `getFD(pid, fd) (VFSFile, error)`（未找到返回 `*SyscallError{Code: ErrNotFound}`）
  - [ ] 4.5 实现 `removeFD(pid, fd) error`（移除后后续访问返回错误）
  - [ ] 4.6 实现 `closeAll(pid) error`（进程退出时关闭所有 FD，Story 1.6/4.1 回收时调用）
- [ ] Task 5: 更新 Process.FDTable 类型 (AC: #3)
  - [ ] 5.1 将 `kernel/process.go` 中 `FDTable map[types.FD]any` 改为 `FDTable map[types.FD]vfs.VFSFile`
  - [ ] 5.2 更新 `NewProcess` 构造函数中 FDTable 初始化
  - [ ] 5.3 确保 `kernel/process_test.go` 中不依赖 `any` 类型的 FDTable 赋值（如有，需改为 mock VFSFile）
  - [ ] 5.4 验证 `kernel/` 导入 `vfs/` 符合依赖方向（kernel → vfs ✓）
- [ ] Task 6: 编写完整单元测试 (AC: all)
  - [ ] 6.1 `vfs/dev_test.go` — DeviceRegistry 测试：注册、获取、重复注册错误、未注册路径返回 ErrNotFound、路径前缀匹配
  - [ ] 6.2 `vfs/vfs_test.go` — VFS 集成测试：Open 返回递增 FD、Read/Write 委托给 VFSFile、Close 后再访问返回错误、Stat 正常工作
  - [ ] 6.3 使用 mock VFSFile 实现（`vfs/vfs_test.go` 中定义 `mockFile struct` 实现 VFSFile 接口）
  - [ ] 6.4 FDTable 并发安全测试：多 goroutine 并发 Open/Read/Write/Close
  - [ ] 6.5 全部测试通过 `go test -race ./vfs/...`
  - [ ] 6.6 全量回归 `go test -race ./...` 确保不破坏已有测试

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** `vfs/vfs.go` 放 VFS 接口 + VFS 结构体，`vfs/dev.go` 放 DeviceRegistry，`vfs/proc.go` 本 Story 不实现（Story 4.3 实现 ProcFS）
- **依赖方向：** `kernel/` → `vfs/` ✓；`vfs/` → `internal/types/` ✓；`vfs/` → `internal/xsync/` ✓。**绝对禁止** `vfs/` 导入 `kernel/`
- **此 Story 不实现：** ProcFS（`/proc/{pid}/`，Story 4.3）、具体驱动实现（LLM/Shell/FS 驱动分别在 Story 1.5/2.3/2.2）、reasonStep 中的 VFS 调用（Story 1.6）
- **此 Story 实现的核心：** VFSFile 接口定义、DeviceRegistry 设备路由、FD 管理、VFS 的 Open/Read/Write/Close/Stat 入口

### 已有代码（必须复用，禁止重新实现）

**`internal/types/types.go` — 已定义的类型：**

```go
type PID uint64          // 进程 ID
type FD int              // 文件描述符 — VFS 的核心返回值
type CtxID uint64        // 上下文 ID
type ErrCode string      // 错误码

const (
    ErrTimeout    ErrCode = "TIMEOUT"
    ErrNotFound   ErrCode = "NOT_FOUND"    // 用于未注册路径、无效 FD
    ErrPermission ErrCode = "PERMISSION"
    ErrInternal   ErrCode = "INTERNAL"
    ErrDriver     ErrCode = "DRIVER"
)

type SyscallEvent struct { ... } // DebugChan 事件，本 Story 暂不使用
```

**`internal/xsync/registry.go` — DeviceRegistry 的基础：**

```go
type Registry[T any] struct { mu sync.RWMutex; items map[string]T }
func NewRegistry[T any]() *Registry[T]
func (r *Registry[T]) Register(name string, item T) error  // 重复注册返回 error
func (r *Registry[T]) Get(name string) (T, bool)
func (r *Registry[T]) List() []T
```

DeviceRegistry 必须基于 `Registry[VFSFileFactory]` 实现，不要手写 `map + mutex`。

**`internal/xsync/syncmap.go` — 可用于 FDTable 管理：**

```go
type SyncMap[K comparable, V any] struct { mu sync.RWMutex; m map[K]V }
func NewSyncMap[K, V]() *SyncMap[K, V]
func (s *SyncMap[K, V]) Load/Store/Delete/Range/Len
```

**`kernel/errors.go` — VFS 所有错误必须使用此类型：**

```go
type SyscallError struct {
    Syscall string       // "Open", "Read", "Write", "Close", "Stat"
    PID     types.PID
    Device  string       // 设备路径（如 "/dev/llm/claude"）
    Err     error
    Code    types.ErrCode
}
func NewSyscallError(syscall string, pid types.PID, device string, err error, code types.ErrCode) *SyscallError
```

**`kernel/process.go` — 需修改的占位字段：**

```go
type Process struct {
    // ...
    FDTable   map[types.FD]any  // ← 替换为 map[types.FD]vfs.VFSFile
    // ...
}
```

### 关键设计决策

**1. VFS 的进程关联方式**

VFS 需要知道"当前操作属于哪个进程"来管理每进程的 FDTable。两种方案：
- **方案 A（推荐）：** VFS 方法签名包含 `pid types.PID` 参数，如 `Open(pid, path, flags)`。这避免了 VFS 持有 Process 指针（保持依赖方向清洁）
- **方案 B：** VFS 内部维护 `fdTables SyncMap[PID, *fdTable]`，通过 PID 查找。这允许 VFS 独立管理 FD 生命周期

采用方案 A+B 组合：VFS 方法接受 PID 参数，内部通过 `SyncMap` 管理每进程的 fdTable。这样 VFS 不需要知道 Process 结构体。

**2. FD 分配起始值**

FD 从 3 开始分配（0=stdin, 1=stdout, 2=stderr 保留）。每个进程独立的 FD 计数器。

**3. 路径前缀匹配 vs 精确匹配**

DeviceRegistry 需要支持路径前缀匹配：
- 注册 `/dev/fs` → `Open("/dev/fs/path/to/file")` 应匹配
- 注册 `/dev/llm/claude` → `Open("/dev/llm/claude")` 精确匹配

实现方式：优先精确匹配，失败后按路径层级向上回溯查找最长前缀匹配。匹配后剩余路径（如 `/path/to/file`）可通过 factory 参数传递或存储在返回的 VFSFile 中。

**建议实现：** factory 签名为 `func(flags OpenFlag) (VFSFile, error)`，前缀匹配后创建 VFSFile 时通过工厂闭包捕获子路径。或者增加 `VFSFileFactory` 为 `func(subpath string, flags OpenFlag) (VFSFile, error)`，使子路径显式传递。**推荐后者**——这样更清晰：

```go
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**4. Process.FDTable 的双重管理**

Story 1-2 中 `Process.FDTable` 定义在 `kernel/process.go`。本 Story 中 VFS 也需要维护 FDTable。有两种处理方式：
- **方案 A：** 删除 `Process.FDTable` 字段，FDTable 完全由 VFS 管理（通过 PID 查找）
- **方案 B（推荐）：** 保留 `Process.FDTable` 字段，但更新类型为 `map[types.FD]vfs.VFSFile`。VFS 维护自己的 fdTables 副本用于操作路由。这样 Process 仍持有 FD 信息（进程回收时需要关闭所有 FD）

采用方案 B：Process.FDTable 作为进程资源的"所有权记录"，VFS 内部 fdTables 作为"操作路由表"。两者保持同步。

**但实际上更简洁的做法是：** VFS 持有唯一的 fdTables，Process.FDTable 字段可以移除或改为只在 VFS 中管理。考虑到依赖方向（kernel → vfs），kernel 可以在进程退出时调用 `vfs.CloseAll(pid)` 来清理。**最终决策：VFS 是 FDTable 的唯一管理者，Process 结构体中的 FDTable 字段移除或保留为引用。**

**推荐：保留 `Process.FDTable` 字段但改为 `map[types.FD]any`（保持不变），实际的 FD → VFSFile 映射由 VFS 内部管理。** 这样避免 `kernel/` 导入 `vfs/`（如果有些场景 kernel 不需要知道 VFSFile 的具体类型）。

**但架构文档明确写了** `FDTable map[FD]VFSFile`，且依赖方向 `kernel/ → vfs/` 是允许的。所以按架构文档来：`Process.FDTable` 改为 `map[types.FD]vfs.VFSFile`。

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写 | `vfs` |
| 导出类型 | PascalCase | `VFS`, `VFSFile`, `DeviceRegistry`, `FileStat`, `OpenFlag` |
| 非导出类型 | camelCase | `fdTable`, `fdEntry` |
| 导出函数 | PascalCase | `NewVFS`, `NewDeviceRegistry` |
| 接口 | 名词 | `VFSFile`（不用 `IVFSFile`） |
| 错误 | `Err` 前缀 | 使用 `types.ErrNotFound` |
| 文件名 | 下划线分隔 | `vfs.go`, `dev.go`, `vfs_test.go`, `dev_test.go` |

### 错误处理模式

所有 VFS 操作的错误必须包装为 `*kernel.SyscallError`：

```go
// 正确：完整包装
return 0, kernel.NewSyscallError("Open", pid, path,
    fmt.Errorf("device not found: %s", path), types.ErrNotFound)

// 正确：FD 无效
return nil, kernel.NewSyscallError("Read", pid, "",
    fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)

// 错误：返回裸 error
return 0, fmt.Errorf("not found")  // ← 禁止
```

**注意：** `vfs/` 包需要导入 `kernel/` 包的 `SyscallError` 和 `NewSyscallError`——但这违反依赖方向（`vfs/` 不能导入 `kernel/`）！

**解决方案：** `SyscallError` 和 `NewSyscallError` 应移至 `internal/types/` 或者定义一个 `errors` 接口让 VFS 使用。但当前它们在 `kernel/errors.go` 中。

**实际处理：** 检查当前代码结构——如果 `SyscallError` 在 `kernel/` 包中，VFS 不能直接导入它。有两种解决方案：
1. **将 SyscallError 移至 `internal/types/`**（破坏性较大，影响现有代码）
2. **VFS 返回自己的错误类型，在 kernel 层包装为 SyscallError**（推荐）
3. **在 `internal/types/` 中定义 SyscallError**，`kernel/errors.go` 改为别名或重导出

**推荐方案 2：** VFS 定义自己的 `VFSError` 类型（或直接返回标准 error），在 kernel 的 FileSystem syscall 方法中包装为 `*SyscallError`。这保持依赖方向干净。

**或者更简单的方案 3：** 将 `SyscallError` 移到 `internal/types/` 包（它本来就应该在那里——所有包都需要使用它）。检查 Story 1-1 的结构，`SyscallError` 放在 `kernel/errors.go` 可能是遗留问题。

**最终决策：** 优先方案 2——VFS 返回带 `ErrCode` 标注的标准 error，kernel 层负责包装。如果开发中发现方案 2 过于繁琐，可退化为方案 3（移动 SyscallError）。

### 测试规范

**测试文件位置：**
- `vfs/vfs_test.go` — VFS 集成测试
- `vfs/dev_test.go` — DeviceRegistry 单元测试

**Mock VFSFile 定义（在测试文件中）：**

```go
type mockFile struct {
    readData  []byte
    readErr   error
    writeData []byte
    writeErr  error
    closed    bool
    closeErr  error
    stat      FileStat
    statErr   error
}

func (m *mockFile) Read(length int) ([]byte, error) { return m.readData, m.readErr }
func (m *mockFile) Write(data []byte) error          { m.writeData = data; return m.writeErr }
func (m *mockFile) Close() error                     { m.closed = true; return m.closeErr }
func (m *mockFile) Stat() (FileStat, error)          { return m.stat, m.statErr }
```

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestDeviceRegistry_Register` | 注册成功、重复注册返回错误 |
| `TestDeviceRegistry_Open` | 已注册路径返回 VFSFile、未注册返回 ErrNotFound |
| `TestDeviceRegistry_PrefixMatch` | `/dev/fs/path/to/file` 匹配注册的 `/dev/fs` |
| `TestVFS_Open` | 返回递增 FD（从 3 开始）、存入 FDTable |
| `TestVFS_ReadWrite` | 通过 FD 正确委托到 VFSFile 的 Read/Write |
| `TestVFS_Close` | Close 后 FD 失效、再次 Read/Write 返回错误 |
| `TestVFS_InvalidFD` | 未打开的 FD 调用 Read/Write/Close 返回错误 |
| `TestVFS_MultiProcess` | 不同 PID 的 FD 互相隔离 |
| `TestVFS_ConcurrentAccess` | 多 goroutine 并发 Open/Read/Write/Close |
| `TestVFS_Stat` | Stat 返回正确的 FileStat |

**测试模式（对齐 Story 1-1/1-2 风格）：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- 使用 `t.Fatal` / `t.Fatalf` / `t.Errorf`
- 并发测试使用 `sync.WaitGroup`
- 全部通过 `go test -race ./vfs/...`

### Story 1-2 经验教训（必须吸收）

1. **data race 敏感：** Story 1-2 的 Future[T] 和 Process 状态机都因并发问题返工过。VFS 的 FDTable 访问必须从一开始就用 mutex 或 SyncMap 保护
2. **SyscallEvent.Timestamp 是 `time.Duration` 不是 `time.Time`：** 本 Story 暂不写入 SyscallEvent，但需了解
3. **测试使用 `t.Logf` 不用 `fmt.Printf`**
4. **使用 `slices.Contains` 等标准库简化代码**（Story 1-2 lint 建议采纳）
5. **先实现核心逻辑再写测试**（Story 1-2 的工作流程：Task 1-3 实现，Task 4 统一测试）
6. **GetState() 线程安全读取模式：** Story 1-2 code review 中补充了 `GetState()` 方法。VFS 类似场景也需考虑线程安全读取

### Git 智能（最近工作模式）

**最近 6 个提交分析：**

| 提交 | 内容 | 启示 |
|------|------|------|
| `a6cccfa` | Story 1-2 实现完成 | process.go/kernel.go 模式是模板 |
| `0d83562` | Story 1-2 文档更新 | 文档与代码分离提交 |
| `4aac131` | project-context.md | 75 条规则，必须遵循 |
| `fa402a5` | 模块路径更新 | `github.com/gonewx/crux` |
| `c82bc50` | 项目初始化 | 目录结构、Makefile、.golangci.yml |

**代码惯例提取：**
- 包级文档注释：`// Package kernel implements the Crux microkernel.`
- 构造函数：`New<Type>()` 模式
- 方法接收器：简短单字母（`k *KernelImpl`、`p *Process`）
- 测试分组：`t.Run("子测试名", func(t *testing.T) {...})`
- 导入分组：标准库 → 空行 → 项目内部包

### Project Structure Notes

**本 Story 新增/修改的文件：**

```
vfs/
├── vfs.go          (新增 — Task 1, 3, 4)
├── dev.go          (新增 — Task 2)
├── vfs_test.go     (新增 — Task 6)
└── dev_test.go     (新增 — Task 6)

kernel/
├── process.go      (修改 — Task 5: FDTable 类型更新)
└── process_test.go (修改 — Task 5: 如有 FDTable 相关测试需更新)
```

**不要创建的文件：**
- `vfs/proc.go` — ProcFS 在 Story 4.3
- `drivers/` 下的任何实现文件 — 驱动在后续 Story
- `vfs/types.go` — 类型统一放在 `vfs/vfs.go` 中（文件数少时不需要拆分）

### References

- [Source: architecture.md#Decision 3: VFS 实现策略] — VFSFile 接口、FD 管理、/dev 设备注册
- [Source: architecture.md#Project Structure & Boundaries] — VFS ↔ Drivers 边界、DeviceRegistry、VFSFileFactory
- [Source: architecture.md#Implementation Patterns > 命名模式] — VFS 路径命名、Go 代码命名规则
- [Source: architecture.md#Implementation Patterns > 结构模式] — 依赖方向：kernel/ → vfs/ → drivers/
- [Source: architecture.md#Implementation Patterns > 泛型使用模式] — Registry[T] 用于设备注册表
- [Source: epics.md#Story 1.3] — 原始用户故事和验收标准
- [Source: 1-2-process-model-and-lifecycle-state-machine.md] — 前序 Story 产出、Process.FDTable 占位、经验教训
- [Source: project-context.md#架构框架规则 > VFS 设备模型] — FD 管理、VFSFile 接口要求
- [Source: project-context.md#关键防错规则 > 反模式] — 禁止裸 error、禁止反向依赖、禁止手写 map+mutex

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
