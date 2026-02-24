# Story 2.3: Shell 驱动（/dev/shell）

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 智能体,
I want 通过 `/dev/shell` 设备执行宿主系统的 shell 命令,
So that 我可以运行构建工具、检查环境、执行脚本。

## Acceptance Criteria

1. **Shell 命令执行** — Given `drivers/shell/shell.go` 已实现，When 调用 `Write(fd, []byte("ls -la"))`，Then 通过 `exec.CommandContext` 执行 shell 命令，And 继承当前用户的环境变量和 PATH（NFR14），And 继承当前用户权限，不提供额外提权（NFR15）
2. **命令输出读取** — Given shell 命令执行完成，When 调用 `Read(fd, length)`，Then 返回 stdout + stderr 合并输出
3. **命令超时处理** — Given shell 命令超时（默认 30 秒），When 超时触发，Then 终止命令进程，返回 `*types.DriverError`，`Code` 为 `ErrTimeout`
4. **设备注册** — Given ShellDriver 已创建，When 在 `cmd/crux/main.go` 中注册，Then `devReg.Register("/dev/shell", shell.FileFactory(shellDriver, "/dev/shell"))`

## Tasks / Subtasks

- [x] Task 1: 创建 drivers/shell/shell.go — ShellFile VFSFile 实现 + ShellDriver + FileFactory (AC: #1, #2, #3, #4)
  - [x] 1.1 创建 `drivers/shell/shell.go` 文件，包名 `shell`
  - [x] 1.2 定义 `ShellDriver` 结构体及构造函数
    - 字段：`defaultTimeout time.Duration`（默认 30 秒）、`cmdBuilder CommandBuilder`（可注入，用于测试）
    - `CommandBuilder` 类型：`func(ctx context.Context, name string, args ...string) *exec.Cmd`
    - `defaultCommandBuilder` 包装 `exec.CommandContext`
    - `NewDriver()` 使用默认配置创建、`NewDriverWithOptions(opts DriverOpts)` 支持自定义配置
  - [x] 1.3 定义 `ShellFile` 结构体，实现 `vfs.VFSFile` 接口
    - 字段：`driver *ShellDriver`、`devicePath string`、`response []byte`（缓冲的命令输出）、`offset int`（读取偏移）、`closed bool`
  - [x] 1.4 实现 `ShellFile.Write(ctx context.Context, data []byte) error` — **Write-then-Read 语义**
    - 接收命令字符串（`data` 即 shell 命令文本，UTF-8 编码）
    - 已关闭时返回 `*types.DriverError{Code: ErrDriver}`（closed 错误）
    - 空命令时返回 `*types.DriverError{Code: ErrDriver}`（empty command 错误）
    - 通过 `exec.CommandContext` 执行：`sh -c "<command>"`（使用 `sh -c` 包装以支持管道、重定向等 shell 特性）
    - `context.WithTimeout(ctx, driver.defaultTimeout)` 设置超时，defer cancel()
    - 捕获 stdout + stderr 合并到 `bytes.Buffer`
    - 超时时（`ctx.Err() == context.DeadlineExceeded`）返回 `*types.DriverError{Code: ErrTimeout}`
    - 命令执行完成后将输出缓冲到 `f.response`，重置 `f.offset = 0`
    - **注意：** 非零退出码 **不是** 驱动层错误——命令正常执行但返回非零退出码是合法的，输出仍然缓冲到 response，exitCode 信息包含在输出中
  - [x] 1.5 实现 `ShellFile.Read(length int) ([]byte, error)` — 读取缓冲的命令输出
    - 已关闭时返回错误
    - `f.response == nil` 时返回错误（"no output available: write a command first"）
    - `length <= 0` 时返回剩余全部输出
    - `length > 0` 时返回指定长度，维护 `offset`
    - 模式参考 `drivers/llm/vfsfile.go` 的 Read 实现
  - [x] 1.6 实现 `ShellFile.Close() error`
    - 设置 `f.closed = true`
    - 清空 `f.response`
    - 重复 Close 返回错误
  - [x] 1.7 实现 `ShellFile.Stat() (vfs.FileStat, error)`
    - 已关闭时返回错误
    - 返回 `vfs.FileStat{Name: f.devicePath, Size: int64(len(f.response)), IsDevice: true, DevicePath: "/dev/shell"}`
  - [x] 1.8 实现 `FileFactory(driver *ShellDriver, basePath string) vfs.VFSFileFactory`
    - 返回闭包 `func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error)`
    - `/dev/shell` 支持 `O_RDWR`（读写模式：Write 命令 + Read 结果）
    - `O_RDONLY` 和 `O_WRONLY` 也可接受（允许灵活使用）
    - 返回 `&ShellFile{driver: driver, devicePath: basePath + subpath}`

- [x] Task 2: 创建 drivers/shell/shell_test.go — 单元测试 (AC: #1-4)
  - [x] 2.1 创建 `drivers/shell/shell_test.go` 文件
  - [x] 2.2 创建 mock CommandBuilder 用于测试
    - 使用 `exec.Command("echo", "mock output")` 或自定义脚本模拟成功/失败/超时
    - 参考 `drivers/llm/claude_cli_test.go` 的 CommandBuilder mock 模式
  - [x] 2.3 `TestShellFile_WriteAndRead_Success` — Write 命令 → Read 输出，验证端到端流程
  - [x] 2.4 `TestShellFile_Write_EmptyCommand` — 空命令返回 DriverError
  - [x] 2.5 `TestShellFile_Write_Timeout` — 超时返回 `ErrTimeout`（使用 mock 执行 `sleep` 命令）
  - [x] 2.6 `TestShellFile_Read_BeforeWrite` — 未执行命令时 Read 返回错误
  - [x] 2.7 `TestShellFile_Read_PartialLength` — 指定 length 读取部分输出
  - [x] 2.8 `TestShellFile_Close_DoubleClose` — 重复 Close 返回错误
  - [x] 2.9 `TestShellFile_Write_AfterClose` — Close 后 Write 返回错误
  - [x] 2.10 `TestShellFile_Read_AfterClose` — Close 后 Read 返回错误
  - [x] 2.11 `TestShellFile_Stat` — 验证 Stat 返回正确的 FileStat 字段
  - [x] 2.12 `TestShellFile_Stat_AfterClose` — Close 后 Stat 返回错误
  - [x] 2.13 `TestShellFile_Write_NonZeroExitCode` — 非零退出码不是驱动错误，输出正常缓冲
  - [x] 2.14 `TestShellDriver_InheritsEnv` — 验证命令继承当前环境变量（`echo $HOME`）
  - [x] 2.15 `TestFileFactory_ReturnsShellFile` — FileFactory 返回正确的 ShellFile 实例

- [x] Task 3: 集成到 CLI 入口 — 设备注册 (AC: #4)
  - [x] 3.1 在 `cmd/crux/main.go` 中导入 `drivers/shell` 包
  - [x] 3.2 创建 ShellDriver 实例：`shellDriver := shell.NewDriver()`
  - [x] 3.3 注册设备：`_ = devReg.Register("/dev/shell", shell.FileFactory(shellDriver, "/dev/shell"))`
  - [x] 3.4 确认注册顺序与其他设备一致（在 `/dev/llm/claude` 和 `/dev/fs` 之后）

- [x] Task 4: 全量回归测试 (AC: #1-4)
  - [x] 4.1 `go test -race ./drivers/shell/...` 全部通过
  - [x] 4.2 `go test -race ./...` 全量通过（确认无回归）
  - [x] 4.3 `go vet ./...` 无警告

## Dev Notes

### 核心实现分析

**Story 2.3 是 Epic 2 的命令执行能力** — Shell 驱动让智能体从"能读取文件"升级到"能执行命令"。后续 Story 2.5（code-analyst 参考 Skill）将依赖此能力来运行构建工具和代码分析命令。Story 2.4（Skill 注入与设备权限白名单）中 Skill manifest 的 `tools: ["/dev/shell"]` 声明将控制此设备的访问权限。

**`drivers/shell/` 目录已存在（空目录含 `.gitkeep`）** — 只需在其中创建实现文件。

**核心设计决策：Write-then-Read 语义** — 与 LLM 驱动（`drivers/llm/vfsfile.go`）相同的交互模式：先 Write 命令，再 Read 输出。这是 VFSFile 接口适配 shell 命令执行的自然方式。

**核心设计决策：使用 `sh -c` 包装命令** — Write 的 data 参数是完整的 shell 命令字符串（非 JSON），通过 `sh -c "<command>"` 执行，支持管道、重定向、变量展开等 shell 特性。这比 JSON 请求更简洁，因为智能体发送的就是自然语言形式的 shell 命令。

**非零退出码处理** — 与 LLM 驱动不同，shell 命令返回非零退出码是正常行为（如 `grep` 没有匹配时返回 1）。因此 Write 方法在命令正常执行完成但退出码非零时 **不应返回错误**，而是将完整输出（stdout + stderr + exitCode 信息）缓冲到 response 供 Read 读取。

### 架构约束（必须遵循）

**依赖方向：**
```
drivers/shell/ → vfs/（使用 VFSFile 接口、VFSFileFactory 类型、OpenFlag、FileStat）
drivers/shell/ → internal/types/（使用 DriverError、ErrCode 常量）
drivers/shell/ 不导入 → kernel/、context/、cmd/、debug/、drivers/llm/、drivers/fs/、skills/
```

**⚠️ 关键架构约束：drivers/ 不导入 kernel/** — Story 2.2 Code Review 发现 H1 级别问题：`drivers/fs/` 导入 `kernel/` 违反架构规则。修复方案是使用 `types.DriverError` 替代 `kernel.SyscallError`。Shell 驱动 **必须** 从一开始就使用 `types.DriverError`，绝不导入 kernel 包。

**VFSFile 接口签名（必须精确匹配）：**
```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

**VFSFileFactory 签名（必须精确匹配）：**
```go
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)
```

**DeviceRegistry 匹配机制：**
- `/dev/shell` 注册为精确匹配路径
- 访问 `/dev/shell` 时，FileFactory 收到 `subpath = ""`
- Shell 驱动不需要 subpath（不像 `/dev/fs` 需要文件路径）

**错误处理模式（必须使用 `*types.DriverError`）：**
```go
// 超时 → *types.DriverError{Op: "Write", Device: "/dev/shell", Err: ..., Code: types.ErrTimeout}
// 空命令 → *types.DriverError{Op: "Write", Device: "/dev/shell", Err: ..., Code: types.ErrDriver}
// 已关闭 → *types.DriverError{Op: "Write"/"Read", Device: "/dev/shell", Err: ..., Code: types.ErrDriver}
```

### 参考实现模式

**必须遵循的两个参考实现：**

1. **`drivers/llm/vfsfile.go`** — Write-then-Read 语义、response 缓冲、offset 管理、closed 检查
2. **`drivers/llm/claude_cli.go`** — exec.CommandContext、CommandBuilder 注入、超时处理、stdout/stderr 捕获

**LLMFile 的 Write-then-Read 模式（供参考）：**
```go
// Write 接受请求，执行操作，缓冲响应
func (f *LLMFile) Write(ctx context.Context, data []byte) error {
    if f.closed { return ... }
    // 执行操作（LLM调用 / Shell命令）
    // 缓冲结果到 f.response
    f.response = resultBytes
    f.offset = 0
    return nil
}

// Read 返回缓冲的响应
func (f *LLMFile) Read(length int) ([]byte, error) {
    if f.closed { return nil, ... }
    if f.response == nil { return nil, ... }
    remaining := f.response[f.offset:]
    ...
}
```

**ClaudeCliDriver 的 CommandBuilder 注入模式（供参考）：**
```go
type CommandBuilder func(ctx context.Context, name string, args ...string) *exec.Cmd

func defaultCommandBuilder(ctx context.Context, name string, args ...string) *exec.Cmd {
    return exec.CommandContext(ctx, name, args...)
}

type ClaudeCliDriver struct {
    cmdBuilder CommandBuilder
}
```

**Shell 驱动的 FileFactory 应类似 LLM 驱动：**
```go
func FileFactory(driver *ShellDriver, basePath string) vfs.VFSFileFactory {
    return func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
        return &ShellFile{
            driver:     driver,
            devicePath: basePath + subpath,
        }, nil
    }
}
```

### 已有代码复用（严格遵循）

**`vfs.VFSFile` 接口** — 定义在 `vfs/vfs.go:15-20`
**`vfs.VFSFileFactory` 类型** — 定义在 `vfs/vfs.go:23`
**`vfs.OpenFlag` 常量** — 定义在 `vfs/vfs.go:25-31`：`O_RDONLY`、`O_WRONLY`、`O_RDWR`
**`vfs.FileStat` 结构** — 定义在 `vfs/vfs.go:33-38`
**`types.DriverError`** — 定义在 `internal/types/types.go`
**`types.NewDriverError`** — 定义在 `internal/types/types.go`
**`types.ErrTimeout`、`types.ErrDriver`、`types.ErrNotFound`** — 定义在 `internal/types/types.go`

### 设备注册参考（cmd/crux/main.go）

当前的设备注册代码（约第 182 行）：
```go
devReg := vfs.NewDeviceRegistry()
vfsInst := vfs.NewVFS(devReg)
claudeDriver := llm.NewClaudeCliDriver()
_ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
_ = devReg.Register("/dev/fs", fs.FileFactory())
```

Story 2.3 需要在此处添加：
```go
shellDriver := shell.NewDriver()
_ = devReg.Register("/dev/shell", shell.FileFactory(shellDriver, "/dev/shell"))
```

### 前序 Story 经验（必须吸收）

**Story 2.2 Code Review 关键发现：**

| 问题 | 严重度 | Shell 驱动启示 |
|------|--------|-------------|
| H1: `drivers/fs/` 导入 `kernel/` 违反架构规则 | HIGH | **Shell 驱动必须从一开始就只用 `types.DriverError`，不导入 kernel/** |
| H2: 错误类型不一致（Write 用 SyscallError，Read 用 fmt.Errorf） | MEDIUM | **所有方法统一使用 `*types.DriverError` 或 `fmt.Errorf`，不混用** |
| R2-M1: Write 返回 fmt.Errorf 导致 VFS 层错误码降级 | MEDIUM | **Write 返回 `*types.DriverError` 而非 `fmt.Errorf`，确保 VFS 层能提取正确错误码** |
| R2-M2: VFS 层 Read/Write/Close 未提取 DriverError 码 | MEDIUM | **VFS 层已修复（`driverErrCode()` 辅助函数），Shell 驱动只需正确返回 DriverError** |
| R2-M3: HostFSFile.Write 不检查 closed 状态 | MEDIUM | **Shell 驱动所有方法（Read/Write/Close/Stat）都必须检查 closed 状态** |

**Story 2.2 的 DriverError 模式（已验证可行）：**
- FileFactory 错误 → `types.NewDriverError("Open", device, err, code)`
- Write 错误 → `&types.DriverError{Op: "Write", Device: ..., Err: ..., Code: ...}`
- Read/Close/Stat 的 closed 错误 → `fmt.Errorf("read from closed ...")` 或 `*types.DriverError`

**Story 1.5 CommandBuilder 注入模式（已验证可行）：**
- `ClaudeCliDriver` 使用可注入的 `CommandBuilder` 实现可测试性
- Shell 驱动应该复用完全相同的模式
- 测试中注入 mock CommandBuilder，不依赖真实 shell

**Story 2.0 VFSFile.Write 签名变更：**
- `Write(ctx context.Context, data []byte) error` — 必须包含 ctx 参数
- ctx 用于超时控制：`context.WithTimeout(ctx, timeout)`

### Git 智能分析

**最近 10 次提交：**
```
d401528 Enhance Host Filesystem Driver with Error Handling and Unit Tests
f4a42dd Finalize Story 2.2: Host Filesystem Driver Implementation
3d0ef3e Implement Host Filesystem Driver for /dev/fs
09c670c Add Story 2.2: Host Filesystem Driver Implementation
75925e4 Finalize Story 2.1: Skill Loader and Manifest Parsing Implementation
9aa7f1d Update Story 2.1 status to review and finalize skill loader implementation
02f7d73 Add Story 2.1: Skill Loader and Manifest Parsing Implementation
12f0533 Refactor reasoning step output format and enhance CLI parameters
f067733 Update DefaultTimeout in Claude CLI to 5 minutes for improved task handling
ecd4fd9 Finalize Story 2.0: LLM Driver Error Handling Enhancements
```

**相关模式：**
- 每个 Story 作为独立单元实现和提交
- Code Review 反馈直接应用（不需要再迭代）
- `go test -race ./...` 作为质量门禁
- DriverError 模式在 Story 2.2 中已成熟
- CommandBuilder 注入模式在 Story 1.5 中已成熟

**Story 2.2 的 drivers/fs/hostfs.go 变更记录：**
- Round 1: 移除 kernel 依赖，使用 types.DriverError
- Round 2: Write 返回 DriverError（非 fmt.Errorf），统一错误码提取，添加 closed 检查
- 这些教训 Shell 驱动必须在第一轮就做对

### NFR 合规要求

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR14 | Shell 命令继承当前用户的环境变量和 PATH | `exec.CommandContext` 默认继承父进程环境（Go 标准行为），不需要额外设置 |
| NFR15 | 继承当前用户权限，不提供额外提权 | `exec.CommandContext` 自然继承调用者权限，不设置 SysProcAttr |

### Project Structure Notes

**本 Story 创建的文件：**

```
drivers/shell/
├── shell.go           (新建 — ShellDriver + ShellFile + FileFactory)
└── shell_test.go      (新建 — 15 个测试用例)
```

**需要修改的文件：**
- `cmd/crux/main.go` — 新增 `/dev/shell` 设备注册（~3 行：import + NewDriver + Register）

**不需要修改的文件：**
- `vfs/` 下任何文件 — VFSFile 接口已存在，driverErrCode 已在 Story 2.2 中修复
- `internal/types/types.go` — DriverError 和 ErrCode 常量已存在
- `kernel/` 下任何文件 — 独立，Shell 驱动不导入
- `drivers/llm/` 下任何文件 — 独立驱动
- `drivers/fs/` 下任何文件 — 独立驱动
- `skills/` 下任何文件
- `context/` 下任何文件
- `debug/` 下任何文件
- `internal/ui/` 下任何文件
- `go.mod` / `go.sum` — 无新外部依赖（仅使用标准库 `os/exec`、`bytes`、`context`、`fmt`、`strings`）

### 与架构文档的一致性

| 架构要求 | 本 Story 实现 |
|---------|-------------|
| `drivers/shell/shell.go` — ShellDriver | ✅ Task 1 |
| `drivers/shell/shell_test.go` — 单元测试 | ✅ Task 2 |
| VFSFile 接口实现 | ✅ Task 1.3-1.7 |
| FileFactory 工厂模式 | ✅ Task 1.8 |
| 注册到 DeviceRegistry | ✅ Task 3 |
| CommandBuilder 注入（可测试性） | ✅ Task 1.2 |
| exec.CommandContext 超时控制 | ✅ Task 1.4 |
| 错误映射为 DriverError | ✅ Task 1.4-1.7 |
| NFR14 继承环境变量 | ✅ exec.CommandContext 默认行为 |
| NFR15 继承用户权限 | ✅ exec.CommandContext 默认行为 |

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.3] — AC 和 User Story 定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — drivers/shell/ 目录结构
- [Source: _bmad-output/planning-artifacts/architecture.md#VFS 实现策略] — VFSFile 接口和设备注册
- [Source: _bmad-output/planning-artifacts/architecture.md#Claude Code CLI 集成] — exec.CommandContext 模式
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — drivers/shell/ 的依赖约束
- [Source: _bmad-output/project-context.md#VFS 设备模型] — VFSFile 接口签名（含 Write ctx 参数）
- [Source: _bmad-output/project-context.md#错误处理] — DriverError 规范
- [Source: _bmad-output/project-context.md#测试规则] — 测试命名和 -race 要求
- [Source: _bmad-output/project-context.md#安全规则] — 继承用户权限，不额外提权
- [Source: vfs/vfs.go:15-38] — VFSFile、VFSFileFactory、OpenFlag、FileStat 定义
- [Source: vfs/dev.go] — DeviceRegistry 精确匹配 + 前缀匹配机制
- [Source: drivers/llm/vfsfile.go] — VFSFile 适配器参考实现（LLMFile + FileFactory）— Write-then-Read 语义
- [Source: drivers/llm/claude_cli.go] — exec.CommandContext + CommandBuilder 注入 + 超时处理
- [Source: drivers/llm/claude_cli_test.go] — CommandBuilder mock 测试参考
- [Source: drivers/fs/hostfs.go] — HostFS 驱动参考（DriverError 错误处理、closed 检查）
- [Source: drivers/fs/hostfs_test.go] — 驱动测试参考（15 个测试用例覆盖模式）
- [Source: internal/types/types.go] — DriverError + NewDriverError + ErrCode 常量
- [Source: cmd/crux/main.go:179-185] — 设备注册依赖注入点
- [Source: 2-2-host-filesystem-driver-dev-fs.md] — 前序 Story 经验（Code Review H1/H2/R2-M1/R2-M2/R2-M3）

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无异常。

### Completion Notes List

- ✅ Task 1: 创建 `drivers/shell/shell.go`，实现 ShellDriver + ShellFile (VFSFile) + FileFactory
  - ShellDriver 含可注入 CommandBuilder，默认 30s 超时
  - ShellFile 实现 Write-then-Read 语义：Write 接收命令字符串，通过 `sh -c` 执行，缓冲 stdout+stderr 合并输出
  - 非零退出码不返回错误，将 exit_code 信息追加到输出
  - 超时返回 `*types.DriverError{Code: ErrTimeout}`
  - 所有方法（Read/Write/Close/Stat）统一检查 closed 状态
  - 错误统一使用 `*types.DriverError`，不导入 kernel 包（吸收 Story 2.2 H1 教训）
- ✅ Task 2: 创建 `drivers/shell/shell_test.go`，15 个测试全部通过
  - 使用 TestHelperProcess + mockCmdBuilder 模式（与 claude_cli_test.go 一致）
  - 覆盖：成功执行、空命令、超时、Write前Read、部分长度Read、双重Close、Close后操作、Stat、非零退出码、环境变量继承、FileFactory
- ✅ Task 3: `cmd/crux/main.go` 添加 import + NewDriver + Register（3 行），在 /dev/fs 之后注册
- ✅ Task 4: `go vet ./...` 零警告，`go test -race ./...` 全量通过，零回归

### File List

- `drivers/shell/shell.go` — 新建（ShellDriver + ShellFile + FileFactory，161 行）
- `drivers/shell/shell_test.go` — 新建（15 个测试用例，292 行）
- `cmd/crux/main.go` — 修改（新增 import + 2 行设备注册）
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 修改（状态更新）
- `_bmad-output/implementation-artifacts/2-3-shell-driver-dev-shell.md` — 修改（任务标记 + Dev Agent Record）
