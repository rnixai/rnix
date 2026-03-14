# Epic 1: 第一个智能体运行（First Agent Runs）

用户安装 Rnix 后，输入 `rnix "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。

## Story 1.1: 项目初始化与基础设施

As a 开发者,
I want 通过 `go install` 安装 Rnix 并获得一个可构建的项目骨架,
So that 后续所有模块可以在标准化的 Go 项目结构上构建。

**Acceptance Criteria:**

**Given** 用户已安装 Go 1.26
**When** 执行 `go install github.com/rnixai/rnix/cmd/rnix@latest`
**Then** 获得 `rnix` 二进制文件，执行 `rnix version` 输出版本号
**And** 二进制无额外运行时依赖（除 Claude Code CLI）

**Given** 项目目录已创建
**When** 查看目录结构
**Then** 遵循架构文档定义的 OS 隐喻结构（`cmd/rnix/`、`kernel/`、`vfs/`、`drivers/`、`context/`、`skills/`、`debug/`、`internal/types/`、`internal/xsync/`、`internal/ui/`）
**And** 包含 `go.mod`（模块路径 `github.com/rnixai/rnix`）、`Makefile`、`.golangci.yml`、`.gitignore`

**Given** `internal/types/types.go` 已实现
**When** 其他包导入共享类型
**Then** 可使用 `PID`、`FD`、`CtxID`、`ErrCode`、`Signal`、`ProcessState` 等类型
**And** 无循环依赖（`internal/types/` 零外部依赖）

**Given** `internal/xsync/` 已实现
**When** 使用泛型工具
**Then** `Registry[T]` 支持注册/获取/列出操作
**And** `SyncMap[K,V]` 支持并发安全的 Load/Store/Delete/Range
**And** `Future[T]` 支持 Await 阻塞等待结果
**And** `Result[T]` 支持 Ok/Err/Unwrap/Map 操作
**And** 所有泛型类型通过 `-race` 测试

**Given** `kernel/errors.go` 已实现
**When** syscall 产生错误
**Then** 返回 `*SyscallError` 类型，包含 `Syscall`、`PID`、`Device`、`Err`、`Code` 字段
**And** `ErrCode` 常量包含 `ErrTimeout`、`ErrNotFound`、`ErrPermission`、`ErrInternal`、`ErrDriver`

**Given** `Makefile` 已创建
**When** 执行 `make build`
**Then** 编译成功生成二进制
**And** `make test` 运行测试（含 `-race`），`make lint` 运行 golangci-lint

## Story 1.2: 进程模型与生命周期状态机

As a 平台构建者,
I want 智能体进程支持完整的生命周期状态转移,
So that 进程可以在 Created → Running → Zombie → Dead 之间安全转换，我能通过 ps 和 strace 追踪状态变化。

**Acceptance Criteria:**

**Given** `kernel/process.go` 已实现
**When** 创建一个新 Process
**Then** 初始状态为 `Created`，拥有唯一 PID（`atomic.AddUint64` 递增，不回收）
**And** 包含 `Intent`（不可变）、`Skills`、`Children`、`FDTable`、`DebugChan`、`Done` 字段

**Given** Process 处于 `Created` 状态
**When** 调用状态转移到 `Running`
**Then** 状态成功变为 `Running`

**Given** Process 处于 `Running` 状态
**When** 调用状态转移到 `Zombie`（正常完成/错误/超时/kill）
**Then** 状态成功变为 `Zombie`，`ExitStatus` 被记录

**Given** Process 处于 `Zombie` 状态
**When** 调用状态转移到 `Dead`（wait 回收）
**Then** 状态变为 `Dead`

**Given** 尝试非法状态转移（如 Running→Created、Zombie→Running、Dead→任何状态）
**When** 调用转移方法
**Then** 返回错误，状态保持不变

**Given** `KernelImpl` 持有进程表
**When** 多个 goroutine 并发访问进程表
**Then** 通过 `SyncMap[PID, *Process]` 保证并发安全
**And** 通过 `go test -race` 无数据竞争

## Story 1.3: VFS 框架与设备注册

As a 平台构建者,
I want 智能体通过统一的 VFS 接口访问所有设备（LLM、文件系统、Shell）,
So that 所有资源访问方式一致，可通过 strace 统一追踪。

**Acceptance Criteria:**

**Given** `vfs/vfs.go` 已实现
**When** 调用 VFS 操作
**Then** 提供 `Open(path, flags) (FD, error)`、`Read(fd, length) ([]byte, error)`、`Write(fd, data) error`、`Close(fd) error`、`Stat(path) (FileStat, error)` 接口
**And** `VFSFile` 接口定义 `Read`、`Write`、`Close`、`Stat` 方法

**Given** `vfs/dev.go` 已实现
**When** 调用 `DeviceRegistry.Register("/dev/llm/claude", factory)`
**Then** 后续 `Open("/dev/llm/claude", O_RDWR)` 返回驱动封装的 `VFSFile`

**Given** 设备已注册
**When** 调用 `Open` 传入已注册路径
**Then** 返回进程内递增的 FD，FD 存入进程的 `FDTable`

**Given** 设备未注册
**When** 调用 `Open` 传入未注册路径
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** FD 已打开
**When** 调用 `Close(fd)`
**Then** FD 从 `FDTable` 移除，后续 `Read`/`Write` 该 FD 返回错误

## Story 1.4: 上下文管理

As a 平台构建者,
I want 每个智能体拥有独立的上下文空间来累积对话历史和工具结果,
So that 每轮 LLM 调用都能获得完整的推理上下文，进程间互不干扰。

**Acceptance Criteria:**

**Given** `context/context.go` 已实现
**When** 调用 `CtxAlloc(size)`
**Then** 返回唯一的 `CtxID`，分配指定大小的上下文空间

**Given** 上下文已分配
**When** 调用 `CtxWrite(cid, offset, data)`
**Then** 数据写入上下文指定位置
**And** 调用 `CtxRead(cid, offset, length)` 可读回写入的数据

**Given** 上下文包含 system prompt、对话历史和工具结果
**When** 调用 `BuildPrompt(cid)`
**Then** 按正确顺序组装完整的 LLM prompt（system prompt + 历史消息 + 最新工具结果）
**And** 组装时间 ≤ 1 秒（NFR5）

**Given** 上下文已分配
**When** 调用 `CtxFree(cid)`
**Then** 上下文空间释放，后续 Read/Write 该 CtxID 返回错误

## Story 1.5: LLM 驱动层（Claude Code CLI）

As a 平台构建者,
I want 智能体通过 `/dev/llm/claude` 设备调用 Claude Code CLI 进行 LLM 推理,
So that 推理过程对 strace 透明可追踪，且驱动层可替换。

**Acceptance Criteria:**

**Given** `drivers/llm/driver.go` 已实现
**When** 查看 LLMDriver 接口
**Then** 包含 `Call(ctx, req) (*LLMResponse, error)`、`Stream(ctx, req) (<-chan StreamEvent, error)`、`Info() DriverInfo` 方法

**Given** `drivers/llm/claude_cli.go` 已实现
**When** 调用 `ClaudeCliDriver.Call(ctx, req)`
**Then** 通过 `exec.CommandContext` 执行 `claude -p "{intent}" --output-format json`
**And** 正确传递 `--system-prompt`、`--model`、`--max-turns` 参数（NFR11）
**And** 解析 JSON 输出为 `LLMResponse` 结构

**Given** LLM 调用成功
**When** 解析响应
**Then** `LLMResponse` 包含 `Content`（文本内容）和 `TokensUsed`（消耗 token 数）

**Given** LLM 调用超时（默认 30 秒）
**When** `context.WithTimeout` 到期
**Then** `cmd.Process.Kill()` 终止 CLI 进程
**And** 返回 `*SyscallError`，`Code` 为 `ErrTimeout`

**Given** `drivers/llm/registry.go` 已实现
**When** 注册 Claude 驱动
**Then** 基于 `Registry[LLMDriver]`，支持按路径查找驱动

## Story 1.6: 内核推理循环（Spawn + reasonStep）

As a 用户,
I want Spawn 一个智能体后，它自动执行 reasonStep 推理循环直到完成,
So that 我只需提供意图，智能体自动完成推理。

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中 `Spawn` 已实现
**When** 调用 `Spawn(intent, skills, opts)`
**Then** 创建新 Process（状态 Created），分配上下文（CtxAlloc），打开 `/dev/llm/claude`
**And** 启动独立 goroutine 执行 reasonStep 循环
**And** 状态转为 Running
**And** 返回 PID

**Given** reasonStep 循环运行中
**When** LLM 返回 action 类型为 `text`（最终输出）
**Then** 将文本作为最终结果记录，进程正常完成
**And** 状态转为 Zombie（exit code 0）

**Given** reasonStep 循环运行中
**When** LLM 返回 action 类型为 `tool_call`
**Then** 通过 VFS 执行对应工具调用（如 Read /dev/fs/...）
**And** 将工具执行结果追加到上下文（CtxWrite）
**And** 继续下一轮 reasonStep

**Given** reasonStep 循环运行中
**When** LLM 调用超时或失败
**Then** 进程状态在 5 秒内转为 Zombie（NFR7）
**And** ExitStatus 记录错误信息

**Given** 进程完成（成功或失败）
**When** 查看 Done channel
**Then** ExitStatus 写入 Done channel，阻塞的 Wait 调用被唤醒

## Story 1.7: CLI 入口与 UI 组件

As a 用户,
I want 通过 `rnix "意图"` 启动智能体并看到清晰的实时进度和结果输出,
So that 我全程知道智能体在做什么、结果是什么。

**Acceptance Criteria:**

**Given** `cmd/rnix/main.go` 已实现
**When** 执行 `rnix "分析代码"`
**Then** 解析意图文本，调用 `kernel.Spawn`，等待完成并输出结果
**And** 支持全局 flags：`--json`、`--verbose/-v`、`--quiet/-q`
**And** 依赖注入：创建 VFS、DeviceRegistry、注册 Claude 驱动、创建 Kernel

**Given** `internal/ui/renderer.go` 已实现
**When** 程序启动
**Then** 检测 `TerminalProfile`（宽度、IsTTY、颜色级别、Unicode 支持）
**And** 所有组件输出到 `io.Writer`，不直接写 `os.Stdout`

**Given** `internal/ui/styles.go` 已实现
**When** 输出带样式文本
**Then** 使用 lipgloss 集中定义的颜色（内核灰 `#888888`、智能体蓝 `#5B9BD5`、成功绿 `#6BCB77`、错误红 `#FF6B6B`）
**And** 支持 `NO_COLOR` 环境变量降级

**Given** Agent Progress Reporter 组件 (`internal/ui/progress.go`) 已实现
**When** 智能体执行中
**Then** 实时输出 `[kernel] spawning PID 1...`、`[agent/1] reasoning step 1/3...` 等汇报行

**Given** Result Box 组件 (`internal/ui/result.go`) 已实现
**When** 智能体返回结果
**Then** 用双线边框 `══` 包裹结果文本，边框颜色为成功绿

**Given** Error Block 组件 (`internal/ui/error.go`) 已实现
**When** 发生错误
**Then** 输出三行结构：`✗ {设备路径}: {错误原因}` → `→ {影响}` → `→ 建议: {恢复操作}`

**Given** Summary Footer 组件 (`internal/ui/summary.go`) 已实现
**When** 智能体完成
**Then** 输出 `[kernel] PID {N} exited({code}) | tokens: {N} | elapsed: {N}s`

**Given** 非 TTY 输出（管道/重定向）
**When** 执行 `rnix "意图" | cat`
**Then** 自动去除 ANSI 颜色码和 spinner 动画

## Story 1.8: 端到端集成与验收

As a 用户,
I want 完整的端到端体验：`rnix "意图"` → 实时进度 → 结果 → 汇总,
So that 我确认整个系统协同工作正常。

**Acceptance Criteria:**

**Given** 所有 Story 1.1-1.7 已完成
**When** 执行 `rnix "分析 ./README.md"`
**Then** 看到完整输出流：`[kernel] spawning PID 1...` → `[agent/1] reasoning step N/M...` → `══ 分析结果 ══...══` → `[kernel] PID 1 exited(0) | tokens: N | elapsed: Ns`

**Given** Claude Code CLI 已安装并可用
**When** 连续执行 20 次简单任务
**Then** 成功率 ≥ 95%（NFR6）
**And** 简单任务端到端耗时 ≤ 30 秒（NFR1）

**Given** LLM 调用超时
**When** 30 秒后超时触发
**Then** 进程在 5 秒内转为 Zombie（NFR7）
**And** CLI 进程不崩溃（NFR10）
**And** 显示三行错误结构

**Given** 进程异常退出
**When** 查看内核进程表
**Then** 进程表保持一致性，无悬挂 PID、无状态不一致（NFR9）

**Given** `rnix version` 执行
**When** Claude Code CLI 未安装
**Then** 输出 `✗ claude-code CLI not found` + 安装建议

**Given** 执行 `rnix --help`
**When** 查看帮助输出
**Then** 显示 Usage + 可用命令列表 + 全局 flags + 示例

**Given** 信号处理
**When** 用户按 Ctrl+C（首次）
**Then** 当前智能体优雅中断，进程转 zombie，输出中断摘要
**And** 2 秒内二次 Ctrl+C 强制退出

---
