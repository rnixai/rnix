# Story 2.2: 宿主文件系统驱动（/dev/fs）

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 智能体,
I want 通过 `/dev/fs` 设备读取宿主文件系统上的文件,
So that 我可以分析用户的源代码和文档。

## Acceptance Criteria

1. **HostFS 驱动实现** — Given `drivers/fs/hostfs.go` 已实现，When 调用 `Open("/dev/fs/path/to/file", O_RDONLY)`，Then 打开宿主文件系统对应文件，返回 VFSFile 封装
2. **文件读取** — Given 文件已打开，When 调用 `Read(fd, length)`，Then 读取文件内容并返回，And 额外延迟 < 10ms，不超过直接文件 I/O 的 2 倍（NFR4）
3. **无读取权限** — Given 文件存在但无读取权限，When 调用 Open，Then 返回 `*SyscallError`，`Code` 为 `ErrPermission`，And 遵循宿主 OS 的文件权限模型（NFR13）
4. **路径不存在** — Given 文件路径不存在，When 调用 Open，Then 返回 `*SyscallError`，`Code` 为 `ErrNotFound`
5. **设备注册** — Given HostFS 驱动已创建，When 在 `cmd/crux/main.go` 中注册，Then `devRegistry.Register("/dev/fs", hostFSDriver.FileFactory())`

## Tasks / Subtasks

- [ ] Task 1: 创建 drivers/fs/hostfs.go — HostFS 驱动核心实现 (AC: #1, #2, #3, #4)
  - [ ] 1.1 创建 `drivers/fs/hostfs.go` 文件，包名 `fs`
  - [ ] 1.2 定义 `HostFSFile` 结构体，实现 `vfs.VFSFile` 接口
    - 字段：`file *os.File`（底层文件句柄）、`path string`（完整文件路径）、`closed bool`（关闭状态标志）
  - [ ] 1.3 实现 `HostFSFile.Read(length int) ([]byte, error)`
    - 使用 `io.ReadAll` 或 `io.LimitReader` 读取文件内容
    - 已关闭时返回错误
  - [ ] 1.4 实现 `HostFSFile.Write(ctx context.Context, data []byte) error`
    - MVP 阶段返回 `ErrPermission` 错误（/dev/fs 为只读设备）
    - 符合 VFSFile 接口要求，Write 方法签名必须接受 `context.Context`
  - [ ] 1.5 实现 `HostFSFile.Close() error`
    - 关闭底层 `os.File`
    - 设置 `closed = true`
    - 重复 Close 返回错误
  - [ ] 1.6 实现 `HostFSFile.Stat() (vfs.FileStat, error)`
    - 调用 `os.File.Stat()` 获取文件信息
    - 映射为 `vfs.FileStat{Name, Size, IsDevice: false, DevicePath: "/dev/fs"}`

- [ ] Task 2: 创建 FileFactory 工厂函数 (AC: #1, #3, #4, #5)
  - [ ] 2.1 实现 `FileFactory() vfs.VFSFileFactory` 函数
    - 返回闭包 `func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error)`
    - `subpath` 参数来自 DeviceRegistry 的前缀匹配（如 `/dev/fs/path/to/file` → subpath = `/path/to/file`）
  - [ ] 2.2 路径处理：
    - subpath 是以 `/` 开头的绝对路径，直接作为宿主文件系统路径使用
    - 验证路径不为空
  - [ ] 2.3 错误映射：
    - `os.IsNotExist(err)` → `*kernel.SyscallError{Syscall: "Open", Code: ErrNotFound}`
    - `os.IsPermission(err)` → `*kernel.SyscallError{Syscall: "Open", Code: ErrPermission}`
    - 其他 OS 错误 → `*kernel.SyscallError{Syscall: "Open", Code: ErrDriver}`
  - [ ] 2.4 OpenFlag 处理：
    - `O_RDONLY` → `os.O_RDONLY`
    - `O_WRONLY` / `O_RDWR` → MVP 阶段返回 `ErrPermission`（/dev/fs 只读）

- [ ] Task 3: 创建 drivers/fs/hostfs_test.go — 单元测试 (AC: #1-5)
  - [ ] 3.1 创建 `drivers/fs/hostfs_test.go` 文件
  - [ ] 3.2 `TestFileFactory_ReadSuccess` — 使用 testdata 文件验证 Open → Read → Close 流程
  - [ ] 3.3 `TestFileFactory_FileNotFound` — 不存在的路径返回 `*kernel.SyscallError` + `ErrNotFound`
  - [ ] 3.4 `TestFileFactory_PermissionDenied` — 无权限文件返回 `*kernel.SyscallError` + `ErrPermission`（在 CI 环境中可能需要 skip）
  - [ ] 3.5 `TestHostFSFile_Stat` — 验证 Stat 返回正确的 FileStat 字段
  - [ ] 3.6 `TestHostFSFile_Write_ReadOnly` — 对只读设备执行 Write 返回 `ErrPermission`
  - [ ] 3.7 `TestHostFSFile_Close_DoubleClose` — 重复 Close 返回错误
  - [ ] 3.8 `TestHostFSFile_Read_AfterClose` — Close 后 Read 返回错误
  - [ ] 3.9 `TestFileFactory_EmptySubpath` — 空 subpath 返回错误
  - [ ] 3.10 `TestFileFactory_WriteFlag_Rejected` — O_WRONLY / O_RDWR flag 被拒绝

- [ ] Task 4: 创建 testdata 测试夹具 (AC: #1, #2)
  - [ ] 4.1 创建 `drivers/fs/testdata/sample.txt`，包含已知内容用于读取验证
  - [ ] 4.2 创建 `drivers/fs/testdata/nested/deep.txt`，验证嵌套路径访问

- [ ] Task 5: 集成到 CLI 入口 — 设备注册 (AC: #5)
  - [ ] 5.1 在 `cmd/crux/main.go` 中导入 `drivers/fs` 包
  - [ ] 5.2 在设备注册区域添加：`devReg.Register("/dev/fs", fs.FileFactory())`
  - [ ] 5.3 确认注册顺序与其他设备一致

- [ ] Task 6: 全量回归测试 (AC: #1-5)
  - [ ] 6.1 `go test -race ./drivers/fs/...` 全部通过
  - [ ] 6.2 `go test -race ./...` 全量通过（确认无回归）
  - [ ] 6.3 `go vet ./...` 无警告

## Dev Notes

### 核心实现分析

**Story 2.2 是 Epic 2 的文件访问基础** — HostFS 驱动让智能体从"能说话"升级到"能读取文件"。后续 Story 2.5（code-analyst 参考 Skill）直接依赖此能力来读取用户代码文件。Story 2.4（Skill 注入与设备权限白名单）中 Skill manifest 的 `tools: ["/dev/fs"]` 声明将控制此设备的访问权限。

**`drivers/fs/` 目录已存在（空目录含 `.gitkeep`）** — 只需在其中创建实现文件。

**核心设计决策：MVP 阶段 /dev/fs 为只读设备** — 符合 FR16（"智能体可以通过 `/dev/fs` 读取宿主文件系统上的文件"）的表述，写入能力不在 MVP 范围内。

### 架构约束（必须遵循）

**依赖方向：**
```
drivers/fs/ → vfs/（使用 VFSFile 接口、VFSFileFactory 类型、OpenFlag、FileStat）
drivers/fs/ → kernel/（使用 SyscallError、NewSyscallError）
drivers/fs/ → internal/types/（使用 ErrCode 常量）
drivers/fs/ 不导入 → kernel/（除 errors.go）、context/、cmd/、debug/、drivers/llm/、skills/
```

**VFSFile 接口签名（必须精确匹配）：**
```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

**注意：** `Write` 方法签名接受 `context.Context` 作为第一个参数（Story 2.0 变更）。

**VFSFileFactory 签名（必须精确匹配）：**
```go
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**DeviceRegistry 前缀匹配机制：**
- 注册 `/dev/fs` 后，访问 `/dev/fs/home/user/file.go` 时，工厂收到 `subpath = "/home/user/file.go"`
- subpath 以 `/` 开头，是宿主文件系统的绝对路径

**错误处理模式：**
- 文件不存在 → `*kernel.SyscallError{Syscall: "Open", PID: 0, Device: "/dev/fs" + subpath, Err: err, Code: ErrNotFound}`
- 权限不足 → `*kernel.SyscallError{Syscall: "Open", PID: 0, Device: "/dev/fs" + subpath, Err: err, Code: ErrPermission}`
- PID 为 0，因为驱动层不知道调用者 PID（与 SkillLoader 相同模式）

### 参考实现模式（必须遵循 LLM 驱动的 VFSFile 适配器模式）

**参考文件：** `drivers/llm/vfsfile.go` — LLMFile 实现了 VFSFile 接口

```go
// LLMFile 的 FileFactory 模式（供参考）
func FileFactory(driver LLMDriver, basePath string) vfs.VFSFileFactory {
    return func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
        return &LLMFile{
            driver:     driver,
            devicePath: basePath,
        }, nil
    }
}
```

**HostFS 的 FileFactory 应类似但更简单：** 不需要 driver 实例，直接使用 `os.Open`。

### 已有代码复用（严格遵循）

**`vfs.VFSFile` 接口** — 定义在 `vfs/vfs.go:15-20`
**`vfs.VFSFileFactory` 类型** — 定义在 `vfs/vfs.go:23`
**`vfs.OpenFlag` 常量** — 定义在 `vfs/vfs.go:25-31`：`O_RDONLY`、`O_WRONLY`、`O_RDWR`
**`vfs.FileStat` 结构** — 定义在 `vfs/vfs.go:33-38`
**`kernel.NewSyscallError`** — 定义在 `kernel/errors.go:29-37`
**`types.ErrNotFound`、`types.ErrPermission`、`types.ErrDriver`** — 定义在 `internal/types/types.go:17-23`

### 设备注册参考（cmd/crux/main.go）

当前的设备注册代码（约第 182 行）：
```go
devReg := vfs.NewDeviceRegistry()
vfsInst := vfs.NewVFS(devReg)
claudeDriver := llm.NewClaudeCliDriver()
_ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
```

Story 2.2 需要在此处添加：
```go
_ = devReg.Register("/dev/fs", fs.FileFactory())
```

### 前序 Story 经验（必须吸收）

**Story 2.1 YAML 库选择变更：**
- 原指定 `gopkg.in/yaml.v3` 被替换为 `github.com/goccy/go-yaml`
- 本 Story 不涉及 YAML 解析

**Story 2.1 Code Review 修复：**
- [L2] 加载器未验证路径是目录 → 已修复增加 `IsDir()` 检查
- **启示：** HostFS 驱动也需要区分文件和目录——`Open` 应该只允许打开普通文件，不允许打开目录

**Story 2.0 VFSFile.Write 签名变更：**
- `Write(ctx context.Context, data []byte) error` — 必须包含 ctx 参数
- 本 Story 的 `HostFSFile.Write` 签名必须匹配

**Story 1.5 CommandBuilder 注入模式：**
- 外部命令调用通过注入实现可测试性
- HostFS 驱动直接使用 `os.Open`/`os.ReadFile`，无需注入 command builder
- 但可以考虑注入 `os.Open` 函数用于测试（可选，因为可以直接用 testdata）

### Git 智能分析

**最近 5 次提交：**
```
75925e4 Finalize Story 2.1: Skill Loader and Manifest Parsing Implementation
9aa7f1d Update Story 2.1 status to review and finalize skill loader implementation
02f7d73 Add Story 2.1: Skill Loader and Manifest Parsing Implementation
12f0533 Refactor reasoning step output format and enhance CLI parameters
f067733 Update DefaultTimeout in Claude CLI to 5 minutes for improved task handling
```

**相关模式：**
- 每个 Story 作为独立单元实现和提交
- 测试与实现在同一 Story 完成
- `go test -race ./...` 作为质量门禁
- 代码结构严格遵循架构边界

### NFR 合规要求

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR4 | VFS 本地文件读取额外延迟 < 10ms | 直接使用 `os.Open` + `os.File.Read`，无额外层级开销 |
| NFR13 | 遵循宿主 OS 的文件权限 | 直接透传 `os.Open` 的权限检查，不做额外权限逻辑 |
| NFR15 | 继承当前用户权限，不额外提权 | `os.Open` 自然继承调用者权限 |

### Project Structure Notes

**本 Story 创建的文件：**

```
drivers/fs/
├── hostfs.go           (新建 — HostFSFile + FileFactory)
├── hostfs_test.go      (新建 — 10 个测试用例)
└── testdata/
    ├── sample.txt      (新建 — 读取验证 fixture)
    └── nested/
        └── deep.txt    (新建 — 嵌套路径验证 fixture)
```

**需要修改的文件：**
- `cmd/crux/main.go` — 新增 `/dev/fs` 设备注册（~2 行：import + Register 调用）

**不需要修改的文件：**
- `vfs/` 下任何文件 — VFSFile 接口已存在
- `kernel/` 下任何文件 — SyscallError 已存在
- `internal/types/types.go` — ErrCode 常量已存在
- `drivers/llm/` 下任何文件 — 独立驱动
- `drivers/shell/` 下任何文件 — 独立驱动
- `skills/` 下任何文件
- `context/` 下任何文件
- `debug/` 下任何文件
- `internal/ui/` 下任何文件
- `go.mod` / `go.sum` — 无新外部依赖（仅使用标准库 `os`、`io`、`path/filepath`）

### 与架构文档的一致性

| 架构要求 | 本 Story 实现 |
|---------|-------------|
| `drivers/fs/hostfs.go` — HostFSDriver | ✅ Task 1+2 |
| `drivers/fs/hostfs_test.go` — 单元测试 | ✅ Task 3 |
| VFSFile 接口实现 | ✅ Task 1.2-1.6 |
| FileFactory 工厂模式 | ✅ Task 2 |
| 注册到 DeviceRegistry | ✅ Task 5 |
| 错误映射为 SyscallError | ✅ Task 2.3 |
| NFR4 延迟 < 10ms | ✅ 直接 os.Open，零中间层 |
| NFR13 遵循宿主权限 | ✅ 透传 os.Open 权限检查 |

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.2] — AC 和 User Story 定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — drivers/fs/ 目录结构
- [Source: _bmad-output/planning-artifacts/architecture.md#VFS 实现策略] — VFSFile 接口和设备注册
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — drivers/fs/ 的依赖约束
- [Source: _bmad-output/project-context.md#VFS 设备模型] — VFSFile 接口签名（含 Write ctx 参数）
- [Source: _bmad-output/project-context.md#错误处理] — SyscallError 规范
- [Source: _bmad-output/project-context.md#测试规则] — 测试命名和 -race 要求
- [Source: vfs/vfs.go:15-38] — VFSFile、VFSFileFactory、OpenFlag、FileStat 定义
- [Source: vfs/dev.go] — DeviceRegistry 前缀匹配机制
- [Source: drivers/llm/vfsfile.go] — VFSFile 适配器参考实现（LLMFile + FileFactory）
- [Source: drivers/llm/vfsfile_test.go] — VFSFile 适配器测试参考
- [Source: kernel/errors.go:29-37] — NewSyscallError 构造函数
- [Source: internal/types/types.go:17-23] — ErrCode 常量定义
- [Source: cmd/crux/main.go:179-185] — 设备注册依赖注入点
- [Source: 2-1-skill-loader-and-manifest-parsing.md] — 前序 Story 经验

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
