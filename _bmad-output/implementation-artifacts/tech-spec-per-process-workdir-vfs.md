---
title: 'Per-Process WorkDir — VFS 设备工作目录感知'
slug: 'per-process-workdir-vfs'
created: '2026-03-19'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'VFS (vfs/)', 'Kernel (kernel/)', 'Drivers (drivers/fs, drivers/shell, drivers/mcp)']
files_to_modify: ['vfs/vfs.go', 'vfs/dev.go', 'vfs/mcp.go', 'vfs/proc.go', 'drivers/fs/hostfs.go', 'drivers/shell/shell.go', 'drivers/mcp/transport.go', 'kernel/kernel.go', 'ipc/server.go', 'cmd/rnix/main.go', 'drivers/llm/vfsfile.go']
code_patterns: ['VFSFileFactory closure pattern', 'SyncMap per-process state', 'DeviceRegistry global singleton', 'CommandBuilder injection for testing']
test_patterns: ['inline anonymous factory closures (vfs_test, dev_test, kernel/*_test)', 'real factory + testdata (hostfs_test)', 'mock CommandBuilder + TestHelperProcess (shell_test)', 'mock transport factory (mount_test)']
adversarial_review_findings: 32
---

# Tech-Spec: Per-Process WorkDir — VFS 设备工作目录感知

**Created:** 2026-03-19

## Overview

### Problem Statement

VFS 设备在处理相对路径时依赖 daemon 进程的 CWD，但 daemon 的 CWD 不是用户项目目录。当智能体使用相对路径访问文件或执行 shell 命令时，操作会因为路径解析错误而失败。

**受影响的设备（确认）：**
- `/dev/fs` (`hostfs.go:86`): `os.Open(subpath)` — subpath 以 `/` 开头（如 `/src/main.go`），被当作绝对路径打开，不经过项目目录解析
- `/dev/shell` (`shell.go:87`): `exec.CommandContext` 无 `cmd.Dir` — 命令在错误目录执行
- `/dev/mcp` (`transport.go:70`): `exec.CommandContext` 启动 MCP server — MCP server 进程在错误目录启动

**不受影响的设备（审查确认）：**
- `/dev/llm` — Claude CLI / Cursor CLI 接收 prompt 返回 text，CWD 对 LLM 调用无实际影响；OpenAI-compat driver 使用 HTTP 调用远程 API。Factory 签名需适配但内部忽略 workDir。

### Critical Path Convention: subpath 前导 `/` 处理

**关键发现（F22/F23）：** DeviceRegistry.Open 传给 factory 的 subpath **始终以 `/` 开头**。

例：VFS.Open(pid, "/dev/fs/src/main.go", flags) → bestPrefix="/dev/fs" → subpath=`strings.TrimPrefix("/dev/fs/src/main.go", "/dev/fs")` = **`/src/main.go`**。

`filepath.IsAbs("/src/main.go")` = **true**，所以朴素的 `if !filepath.IsAbs(subpath)` 判断**永远不会触发 workDir 拼接**。

**解决方案：** 在每个需要使用 workDir 的 factory 内部，先 `strings.TrimPrefix(subpath, "/")` 去掉前导 `/`，再判断是否为绝对路径。这样：
- `/src/main.go` → `src/main.go` → 相对路径 → `filepath.Join(workDir, "src/main.go")`
- `//etc/hosts` → `/etc/hosts` → 绝对路径 → 穿透

不改 DeviceRegistry 的 subpath 语义，影响范围最小。

### Solution

扩展 `VFSFileFactory` 签名，增加 `workDir string` 参数。VFS 层维护一个 per-process WorkDir 注册表（`SyncMap[PID, string]`），在 `Open` 时通过 PID 查询 WorkDir 并传递给 DeviceRegistry 和 FileFactory。每个设备的 Factory 自己决定如何使用 WorkDir：

- `/dev/fs`: `TrimPrefix(subpath, "/")` → 如果不是绝对路径则 `filepath.Join(workDir, subpath)`
- `/dev/shell`: 创建 ShellFile 时带 workDir，Write 时设 `cmd.Dir = workDir`
- `/dev/mcp`: transport Connect 时设 `cmd.Dir = workDir`（通过 `TransportConfig.WorkDir` 传入）
- `/dev/llm`, `/proc`: Factory 签名适配，内部忽略 workDir

绝对路径（双斜杠 `//etc/hosts`）穿透不受影响。WorkDir 为空串时行为等价于当前。

### Scope

**In Scope:**
- VFS 核心：WorkDir 注册表 + `VFSFileFactory` 签名扩展 + `DeviceRegistry.Open` 签名扩展
- 3 个设备驱动真正适配（fs、shell、mcp）
- 签名适配但不使用 workDir 的设备（llm、proc）
- Kernel Spawn 时注册 WorkDir
- Kernel ActionSpawn（子进程）传递 ProjectConfig
- MCPConfig 加 WorkDir 字段（MountManager 接口签名不变）
- VFS.CloseAll 时清理 WorkDir 注册
- 所有受影响的单元测试更新（78 处跨 17 个文件）
- 绝对路径兼容（双斜杠穿透）

**Out of Scope:**
- Per-process mount namespace（完整设备表隔离）
- VFS DeviceRegistry 架构重构
- LLMDriver 接口变更
- 安全沙箱（限制路径访问范围）

## Context for Development

### Codebase Patterns

**VFSFileFactory 模式：**
- 当前签名：`func(subpath string, flags OpenFlag) (VFSFile, error)`
- 新签名：`func(subpath string, flags OpenFlag, workDir string) (VFSFile, error)`
- 设备在 daemon 启动时全局注册一次（`cmd/rnix/main.go:1171-1173`），workDir 在 Open 时传入
- 共 78 处调用跨 17 个文件需要适配新签名

**subpath 语义（关键约束）：**
- DeviceRegistry.Open L58: `subpath = strings.TrimPrefix(path, bestPrefix)` — subpath **始终以 `/` 开头**（如 `/src/main.go`）
- `filepath.IsAbs("/src/main.go")` = **true** — 所以不能直接用 IsAbs 判断
- **Factory 内必须先 `strings.TrimPrefix(subpath, "/")` 再判断绝对路径**
- exact match（factory 直接注册的路径）传 subpath="" — 不受影响

**绝对路径穿透约定：**
- 用户传 `/dev/fs//etc/hosts` → subpath=`//etc/hosts` → TrimPrefix → `/etc/hosts` → IsAbs=true → 穿透
- 用户传 `/dev/fs/src/main.go` → subpath=`/src/main.go` → TrimPrefix → `src/main.go` → IsAbs=false → Join(workDir, "src/main.go")

**Per-process 状态管理：**
- `VFS.fdTables` 已是 `SyncMap[PID, *fdTable]` — WorkDir 表遵循同样模式
- `VFS.CloseAll(pid)` 使用 `LoadAndDelete` 原子清理

**子进程 spawn（F24 修复）：**
- `ActionSpawn` 处理（kernel.go L1545-1608）构建 `childOpts`，**当前不传 ProjectConfig**
- 修复：加 `childOpts.ProjectConfig = proc.ProjectConfig`，子进程继承父进程的 WorkDir

**MCP mount 时序（关键约束）：**
- `MountManager.Mount()` → `NewStdioTransport()` → `Connect()` → `cmd.Start()` 在 Spawn 时执行
- MCP server 进程在 Mount 时启动，workDir 必须在 Mount 阶段传入 `TransportConfig`
- 两个调用点：auto-mount（kernel.go L643，有 ProjectDir）和 IPC 手动 mount（L2479，需从请求获取 workDir）

**ipc/server.go LLMFileOpener 闭包：**
- `projCfg.LLMFileOpener = func(provider, flags) { factory("", OpenFlag(flags)) }`
- 签名变更后需加 workDir 参数：`factory("", OpenFlag(flags), projectDir)`

### Files to Reference

| File | Purpose | 改动类型 |
| ---- | ------- | -------- |
| `vfs/vfs.go` | VFS 核心 — 加 workDirs 注册表，Open 查 workDir 传给 DeviceRegistry | 修改 |
| `vfs/dev.go` | DeviceRegistry.Open — 签名加 workDir，传给 Factory | 修改 |
| `vfs/mcp.go` | mcpFileFactory — 闭包签名加 workDir（内部忽略） | 修改 |
| `vfs/proc.go` | ProcFS.FileFactory — 闭包签名加 workDir（内部忽略） | 修改 |
| `vfs/mount.go` | MountManager — 签名不变，workDir 通过 MCPConfig.WorkDir 流入 | 无需改 |
| `drivers/fs/hostfs.go` | HostFS Factory — TrimPrefix+IsAbs 判断后用 workDir 解析 | 修改 |
| `drivers/shell/shell.go` | Shell Factory — ShellFile 加 workDir 字段，Write 设 cmd.Dir | 修改 |
| `drivers/mcp/transport.go` | TransportConfig 加 WorkDir 字段，Connect 时设 cmd.Dir | 修改 |
| `drivers/llm/vfsfile.go` | LLM FileFactory — 闭包签名加 workDir（内部忽略） | 签名适配 |
| `cmd/rnix/main.go` | transportFactory 闭包：MCPConfig.WorkDir → TransportConfig.WorkDir | 修改 |
| `kernel/kernel.go` | Spawn 注册 WorkDir；ActionSpawn 传 ProjectConfig；auto-mount 设 mcpCfg.WorkDir | 修改 |
| `ipc/server.go` | LLMFileOpener 闭包 factory 调用加 workDir | 修改 |

**受影响的测试文件（78 处跨 17 个文件）：**

| 文件 | inline factory 处数 |
| ---- | ---- |
| `kernel/reap_test.go` | 12 |
| `kernel/e2e_test.go` | 11 |
| `kernel/kernel_test.go` | 11 |
| `vfs/dev_test.go` | 11 |
| `vfs/vfs_test.go` | 10 |
| `kernel/phase2_toolerror_test.go` | 7 |
| `kernel/supervisor_test.go` | 4 |
| `vfs/mount_test.go` | 2 |
| `kernel/mount_test.go` | 2 |
| `drivers/shell/shell.go` | 1 |
| `drivers/llm/vfsfile.go` | 1 |
| `drivers/fs/hostfs.go` | 1 |
| `vfs/mcp.go` | 1 |
| `vfs/proc.go` | 1 |
| `kernel/spawn_mcp_test.go` | 1 |
| `kernel/init_test.go` | 1 |
| `drivers/llm/factory_test.go` | 1 |

### Technical Decisions

1. **WorkDir 表放 VFS 层**（`workDirs *xsync.SyncMap[PID, string]`）——VFS 自管自查
2. **扩展 VFSFileFactory 签名**加 `workDir string`——通用方案，每个设备自决策
3. **扩展 DeviceRegistry.Open 签名**加 `workDir string`——透传给 Factory
4. **subpath 前导 `/` 在 factory 内处理**——`strings.TrimPrefix(subpath, "/")` 后再 `filepath.IsAbs` 判断。不改 DeviceRegistry 的 subpath 语义
5. **绝对路径穿透约定**——用户传 `/dev/fs//etc/hosts`（双斜杠）表示绝对路径，TrimPrefix 后仍是 `/etc/hosts`
6. **WorkDir 为空串时行为不变**——向后兼容
7. **`/dev/llm` 不使用 workDir**——Factory 签名适配但内部忽略
8. **MCP workDir 放 MCPConfig 字段**——`MCPConfig` 加 `WorkDir string` 字段（`yaml:"work_dir,omitempty"`）。Kernel auto-mount 时设 `mcpCfg.WorkDir = projectDir`。TransportFactory 闭包在 `MCPConfig → TransportConfig` 转换时传 `tc.WorkDir = cfg.WorkDir`。**不改 MountManager 接口签名、不改 TransportFactory 签名**——workDir 在 config 内部流动
9. **子进程继承 ProjectConfig**——ActionSpawn 处理中加 `childOpts.ProjectConfig = proc.ProjectConfig`
10. **KernelImpl.Mount 签名不变**——workDir 通过 MCPConfig.WorkDir 传入，MountManager 接口签名不变

## Implementation Plan

### Tasks

- [x] Task 1: 扩展 VFSFileFactory 签名
  - File: `vfs/vfs.go:49`
  - Action: `VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)` → `VFSFileFactory func(subpath string, flags OpenFlag, workDir string) (VFSFile, error)`

- [x] Task 2: VFS 添加 per-process WorkDir 注册表
  - File: `vfs/vfs.go`
  - Action:
    - `VFS` 结构体加 `workDirs *xsync.SyncMap[types.PID, string]`
    - `NewVFS()` 中初始化
    - 新增 `SetWorkDir(pid types.PID, dir string)` — 仅在 Spawn 中、reasonStep goroutine 启动前调用
    - 新增 `GetWorkDir(pid types.PID) string` — 未注册返回空串
    - `CloseAll(pid)` 中加 `v.workDirs.Delete(pid)`

- [x] Task 3: DeviceRegistry.Open 传递 workDir
  - File: `vfs/dev.go`
  - Action:
    - `Open(path, flags)` → `Open(path, flags, workDir string)`
    - exact match: `factory("", flags, workDir)`
    - prefix match: `factory(subpath, flags, workDir)`

- [x] Task 4: VFS.Open 查 WorkDir 并传递
  - File: `vfs/vfs.go`
  - Action:
    - `VFS.Open(pid, path, flags)` 中查 `v.GetWorkDir(pid)`
    - 调用 `v.devRegistry.Open(path, flags, workDir)`

- [x] Task 5: `/dev/fs` 适配 — HostFS FileFactory 使用 workDir
  - File: `drivers/fs/hostfs.go`
  - Action:
    - 闭包签名加 `workDir string`
    - **关键：** 在 `os.Open(subpath)` 之前：
      ```go
      trimmed := strings.TrimPrefix(subpath, "/")
      if workDir != "" && trimmed != "" && !filepath.IsAbs(trimmed) {
          subpath = filepath.Join(workDir, trimmed)
      }
      ```
    - 更新 `device` 变量用于错误信息（保持 `/dev/fs` + 原始 subpath）
  - Notes: 这是 F22/F23 的核心修复。TrimPrefix 后：`src/main.go`→Join(workDir)，`/etc/hosts`→穿透。**重要：TrimPrefix 逻辑必须放在现有 `if subpath == ""` 检查之后**，避免 subpath="/" 被 TrimPrefix 为 "" 后误判

- [x] Task 6: `/dev/shell` 适配 — ShellFile 使用 workDir
  - File: `drivers/shell/shell.go`
  - Action:
    - `ShellFile` 结构体加 `workDir string`
    - `FileFactory` 闭包签名加 `workDir string`，创建 ShellFile 时传入
    - `ShellFile.Write()` 中：`cmd := f.driver.cmdBuilder(...)` 后加 `if f.workDir != "" { cmd.Dir = f.workDir }`

- [x] Task 7: `/dev/mcp` 适配 — MCPConfig + TransportConfig + TransportFactory
  - Files: `vfs/mcp.go`, `drivers/mcp/transport.go`, `cmd/rnix/main.go`
  - Action:
    - `MCPConfig` 加 `WorkDir string` 字段（`json:"work_dir,omitempty" yaml:"work_dir,omitempty"`）
    - `TransportConfig` 加 `WorkDir string` 字段
    - `StdioTransport.Connect()`: `if t.config.WorkDir != "" { cmd.Dir = t.config.WorkDir }`
    - `cmd/rnix/main.go:1214` transportFactory 闭包：`tc.WorkDir = cfg.WorkDir`
    - `mcpFileFactory` 闭包签名加 `workDir string`（签名适配，内部忽略——MCP server CWD 在 Connect 时已设置）
  - Notes: MountManager 接口签名不变、TransportFactory 签名不变、KernelImpl.Mount 签名不变。workDir 通过 MCPConfig 内部流动

- [x] Task 8: `/dev/llm` 签名适配
  - File: `drivers/llm/vfsfile.go`
  - Action: `FileFactory` 闭包签名加 `workDir string`，内部忽略

- [x] Task 9: `/proc` 签名适配
  - File: `vfs/proc.go`
  - Action: `ProcFS.FileFactory()` 闭包签名加 `workDir string`，内部忽略

- [x] Task 10: Kernel Spawn 注册 WorkDir + auto-mount 传 workDir
  - File: `kernel/kernel.go`
  - Action:
    - Spawn 中 `proc.ProjectConfig = opts.ProjectConfig` 之后：
      ```go
      if opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != "" {
          k.vfs.SetWorkDir(proc.PID, opts.ProjectConfig.ProjectDir)
      }
      ```
    - MCP auto-mount（~L640）：在 mount 前设 `mcpCfg.WorkDir = opts.ProjectConfig.ProjectDir`
  - Notes: MountManager/KernelImpl.Mount 接口签名不变，workDir 通过 MCPConfig.WorkDir 传入

- [x] Task 11: Kernel ActionSpawn 传递 ProjectConfig 给子进程
  - File: `kernel/kernel.go` ~L1545
  - Action: 在 `childOpts := SpawnOpts{...}` 中加 `ProjectConfig: proc.ProjectConfig`
  - Notes: F24 修复——确保子进程继承父进程的 WorkDir

- [x] Task 12: ipc/server.go 适配
  - File: `ipc/server.go`
  - Action:
    - LLMFileOpener 闭包：`factory("", vfs.OpenFlag(flags))` → `factory("", vfs.OpenFlag(flags), projectDir)`
  - Notes: IPC 层不暴露 Mount 操作（无 mount handler），无需额外修改

- [x] Task 13: 更新 VFS 测试
  - Files: `vfs/vfs_test.go`（10处）, `vfs/dev_test.go`（11处）
  - Action:
    - 所有 inline factory 闭包加 `workDir string` 参数
    - 新增 TestVFS_SetWorkDir / TestVFS_CloseAll_CleansWorkDir / TestDeviceRegistry_Open_PassesWorkDir

- [x] Task 14: 更新 HostFS 测试
  - File: `drivers/fs/hostfs_test.go`
  - Action:
    - factory 调用加 `""` 作为 workDir
    - 新增 TestFileFactory_RelativePathWithWorkDir — 验证 `/src/main.go` + workDir 解析为绝对路径
    - 新增 TestFileFactory_AbsolutePathIgnoresWorkDir — 验证 `//etc/hosts` 穿透
    - 新增 TestFileFactory_EmptyWorkDir_BackwardsCompat

- [x] Task 15: 更新 Shell 测试
  - File: `drivers/shell/shell_test.go`
  - Action:
    - FileFactory 闭包调用加 workDir
    - 新增 TestShellFile_Write_UsesWorkDir

- [x] Task 16: 更新 MCP/Mount 测试
  - Files: `vfs/mount_test.go`（2处 factory 签名）, `vfs/mcp_test.go`, `drivers/mcp/transport_test.go`
  - Action: mcpFileFactory 闭包签名适配；TransportConfig 构造加 WorkDir 字段验证；kernel/mount_test.go 不需改（MountManager.Mount 签名不变）

- [x] Task 17: 更新 Kernel 测试（49 处）
  - Files: `kernel/kernel_test.go`（11）, `kernel/reap_test.go`（12）, `kernel/e2e_test.go`（11）, `kernel/phase2_toolerror_test.go`（7）, `kernel/supervisor_test.go`（4）, `kernel/mount_test.go`（2）, `kernel/spawn_mcp_test.go`（1）, `kernel/init_test.go`（1）
  - Action: 所有 inline factory 闭包加 `workDir string` — 搜索 `func(subpath string, flags`

- [x] Task 18: 更新 LLM 测试
  - File: `drivers/llm/factory_test.go`
  - Action: mock DeviceRegisterer 中 factory 调用加 workDir

- [x] Task 19: 运行 `make all` 验证
  - Action: `make all`（lint + vet + test + build）

### Acceptance Criteria

- [x] AC1: Given 进程有 WorkDir="/home/user/project"，when VFS.Open(pid, "/dev/fs/src/main.go", O_RDONLY)，then subpath="/src/main.go" → TrimPrefix → "src/main.go" → `filepath.Join("/home/user/project", "src/main.go")` = `/home/user/project/src/main.go`
- [x] AC2: Given 进程有 WorkDir="/home/user/project"，when VFS.Open(pid, "/dev/fs//etc/hosts", O_RDONLY)，then subpath="//etc/hosts" → TrimPrefix → "/etc/hosts" → IsAbs=true → 穿透打开 `/etc/hosts`
- [x] AC3: Given 进程无 ProjectConfig（nil），when VFS.Open(pid, "/dev/fs/relative", O_RDONLY)，then workDir=""，行为与当前一致
- [x] AC4: Given 进程有 WorkDir，when /dev/shell 执行 `ls src/`，then cmd.Dir=WorkDir
- [x] AC5: Given 进程退出（CloseAll），then workDirs 注册表中该 PID 被清理
- [x] AC6: Given VFSFileFactory 签名变更，when `make all`，then 全部通过
- [x] AC7: Given MCP auto-mount 时 projectDir 非空，when Connect 启动子进程，then cmd.Dir=projectDir
- [x] AC8: Given PID=0 或无注册 WorkDir，when VFS.Open，then workDir=""，行为不变
- [x] AC9: Given 父进程有 ProjectConfig，when ActionSpawn 创建子进程，then 子进程继承 ProjectConfig 和 WorkDir

## Additional Context

### Dependencies

- Story 25.3（Project Config Merge）已完成
- IPC 协议中 `SpawnRequest.ProjectDir` 已存在
- 无外部库依赖

### Testing Strategy

**单元测试（自动化）：**
- VFS 层：WorkDir 注册/查询/清理、Open 传递 workDir
- `/dev/fs`：**TrimPrefix + IsAbs 逻辑**（核心）、workDir 拼接、绝对路径穿透、空 workDir 兼容
- `/dev/shell`：cmd.Dir 设置验证
- MCP：TransportConfig.WorkDir 传递到 cmd.Dir
- 签名兼容：所有现有 78 处 factory 加 workDir="" 后仍通过

**集成测试（手动）：**
- 启动 daemon，从项目目录 spawn，验证 `/dev/fs` 读取相对路径文件
- 验证 `/dev/shell` 命令在项目目录执行

### Adversarial Review Findings Summary

六轮对抗性审查共 32 项（5 Critical, 10 High, 11 Medium, 6 Low），已全部处理：

| 修复动作 | 涉及 Findings |
| ---- | ---- |
| **subpath 前导 `/` 处理（最关键修复）** | **F22, F23** |
| **子进程继承 ProjectConfig** | **F24** |
| **IPC Mount 传 workDir** | **F25** |
| 从 scope 移除 `/dev/llm` CWD 修复 | F3, F4, F16, F17, F18 |
| 测试影响修正为 78 处/17 文件 | F1, F10 |
| MCP workDir 走 TransportConfig + MountManager.Mount | F2, F11 |
| 补全 proc.go、mcp.go 签名适配 | F5, F6 |
| 补全 ipc/server.go LLMFileOpener 闭包适配 | F8, F13 |
| 补全 factory.go 和 factory_test.go | F7 |
| cmd/rnix/main.go 确认无需改 | F9 |
| Task 8 改为签名适配 | F14 |
| 注明 SetWorkDir 调用时序 | F15 |
| 补全子进程 WorkDir 继承 | F20 |
| AC8 覆盖 PID=0 边界 | F21 |
| VFS.Stat 不需要 workDir | F19 |
| 路径遍历警告强化到 Notes | F26 |
| Task 5 标注 TrimPrefix 在 empty check 之后 | F27 |
| 简化 Task 12，IPC 层无 Mount handler | F28 |
| 补充 KernelImpl.Mount 签名 + mount_test 8 处 | F29 |
| workDir 放 MCPConfig 字段，不改 MountManager/TransportFactory 签名 | F30, F31, F32 |

### Notes

**高风险项：**
- **subpath 前导 `/` 处理**是核心逻辑——`strings.TrimPrefix(subpath, "/")` 必须在 IsAbs 判断之前、在 empty subpath 检查之后执行
- **路径遍历警告（F26）**：`filepath.Join(workDir, "../../../etc/passwd")` = `/etc/passwd`。**workDir 不提供沙箱保护**，只是路径解析基准。实施者不要误以为 workDir 能限制文件访问范围
- VFSFileFactory 签名变更影响 78 处，机械性变更，风险可控

**已知限制：**
- 不做路径安全检查（路径遍历），安全沙箱是独立功能
- WorkDir 不可变（Spawn 后不能改）
- IPC 手动 Mount 的 workDir 来源需在实施时确认（可能从 IPC 请求中获取，或默认空串）

**未来演进：**
- WorkDir → MountTable（per-process 设备表隔离）
- 安全沙箱（限制 /dev/fs 到 WorkDir 子树）
