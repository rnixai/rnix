---
stepsCompleted:
  - step-01-validate-prerequisites
  - step-02-design-epics
  - step-03-create-stories
  - step-04-final-validation
inputDocuments:
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/planning-artifacts/architecture.md'
  - '_bmad-output/planning-artifacts/ux-design-specification.md'
project_name: Crux
date: '2026-02-23'
---

# Crux - Epic Breakdown

## Overview

本文档提供 Crux（AI Agent OS）的完整 Epic 和 Story 拆解，将 PRD、架构文档和 UX 设计规范中的需求分解为可实施的 Story，供开发团队使用。

## Requirements Inventory

### Functional Requirements

**智能体生命周期管理（FR1-FR7）**

- FR1: 用户可以通过自然语言意图创建（spawn）一个新的智能体进程
- FR2: 系统可以管理智能体进程的完整生命周期状态（Created → Running → Zombie → Dead）
- FR3: 用户可以终止（kill）一个正在运行的智能体进程
- FR4: 用户可以等待（wait）一个智能体进程完成并获取退出状态
- FR5: 系统可以在父进程退出后将孤儿进程重新挂载到 init（PID=1）
- FR6: 系统可以回收已完成的 Zombie 进程并释放其资源
- FR7: 用户可以查看所有活跃进程的列表及其状态（ps）

**智能体推理（FR8-FR12）**

- FR8: 系统可以驱动智能体执行推理循环（reasonStep），在 LLM 调用与工具执行之间交替直到任务完成
- FR9: 系统可以通过 LLM 驱动层以非交互模式调用 LLM 并获取结构化响应
- FR10: 系统可以解析 LLM 响应中的 action 类型（text 最终输出 / tool_call 工具调用 / spawn 创建子进程）
- FR11: 系统可以在 LLM 调用超时或失败时正确将进程状态转为 Zombie 并上报错误信息
- FR12: 系统可以将工具执行结果追加到智能体上下文中供后续推理使用

**文件系统与资源访问（FR13-FR18）**

- FR13: 系统可以提供统一的虚拟文件系统（VFS）接口（Open/Read/Write/Close/Stat）
- FR14: 系统可以通过 `/proc/{pid}/` 动态暴露每个智能体的运行时状态（status、intent、context）
- FR15: 系统可以通过 `/dev/` 路径注册和路由设备驱动（LLM、Shell、文件系统）
- FR16: 智能体可以通过 `/dev/fs` 读取宿主文件系统上的文件
- FR17: 智能体可以通过 `/dev/llm/claude` 访问 LLM 推理能力
- FR18: 智能体可以通过 `/dev/shell` 执行宿主系统的 shell 命令

**上下文管理（FR19-FR22）**

- FR19: 系统可以为每个智能体分配独立的上下文空间（ctx_alloc）
- FR20: 系统可以读取和写入智能体上下文内容（ctx_read / ctx_write）
- FR21: 系统可以将上下文内容组装为完整的 LLM prompt（包含 system prompt + 对话历史 + 工具结果）
- FR22: 系统可以在进程退出后释放其上下文空间（ctx_free）

**Skill 能力管理（FR23-FR27）**

- FR23: 系统可以从 `manifest.yaml` 读取 Skill 的元信息（名称、工具依赖、模型偏好、上下文预算）
- FR24: 系统可以从 `instructions.md` 读取 Skill 的核心指令并注入智能体的 system prompt
- FR25: 用户可以在 spawn 时指定加载一个或多个 Skill
- FR26: Skill 的 tools 声明可以映射为智能体的可用 `/dev/` 设备权限白名单
- FR27: 系统交付一个完整的参考 Skill（code-analyst），能够分析代码并识别至少 1 个可验证的真实代码问题

**调试与可观测性（FR28-FR32）**

- FR28: 用户可以通过 `astrace` 实时追踪指定智能体的所有 syscall 调用
- FR29: astrace 输出包含每个 syscall 的名称、参数、返回值和耗时
- FR30: 系统可以记录 syscall 调用数据（DebugRecord）供 astrace 消费
- FR31: 用户可以通过 astrace 输出定位到产生错误结果的具体 syscall 调用记录
- FR32: 系统在智能体完成时输出汇总信息（退出码、token 消耗、总耗时）

**命令行接口（FR33-FR37）**

- FR33: 用户可以通过 `crux "意图"` 单命令启动一个智能体
- FR34: 用户可以通过 `crux astrace <pid>` 追踪指定进程的 syscall
- FR35: 用户可以通过 `crux ps` 查看所有进程状态
- FR36: CLI 提供清晰的错误信息，包含设备路径和错误原因
- FR37: 系统可以通过 `go install` 一条命令完成安装，单二进制，零额外依赖（需预装 Claude Code CLI）

**文档（FR38-FR40）**

- FR38: 系统可以提供概念文档，覆盖进程、VFS、Skill、syscall 四个核心概念，每个概念含定义和至少一个示例
- FR39: 系统可以提供快速上手指南，引导用户从安装到跑通第一个 demo（目标 ≤ 15 分钟）
- FR40: 系统可以提供参考手册，覆盖 syscall 列表、VFS 路径规范、manifest.yaml 字段、CLI 命令

### NonFunctional Requirements

**性能（NFR1-5）**

- NFR1: 单智能体 spawn→完成（含 LLM 调用），端到端延迟 ≤ 30 秒（简单任务如单文件分析）
- NFR2: `crux ps` 响应时间 ≤ 100ms（本地进程表查询，不涉及 LLM）
- NFR3: `astrace` 输出延迟 ≤ 500ms（从 syscall 发生到终端显示）
- NFR4: VFS 本地文件读取（`/dev/fs`）额外延迟 < 10ms，不超过直接文件 I/O 延迟的 2 倍
- NFR5: 上下文组装（ctx → prompt）时间 ≤ 1 秒（不含 LLM 调用本身）

**可靠性（NFR6-10）**

- NFR6: 连续 20 次 spawn→完成路径，成功率 ≥ 95%
- NFR7: LLM API 超时/错误时，进程在 5 秒内正确转入 Zombie 状态，不卡死在 Running
- NFR8: 进程退出后，goroutine 和 context 内存在 10 秒内释放，无泄漏
- NFR9: 内核进程表在任意进程异常退出后保持一致性（无悬挂 PID、无状态不一致）
- NFR10: CLI 进程（crux 二进制本身）在智能体异常退出时不崩溃

**集成（NFR11-14）**

- NFR11: LLM 驱动层调用时，正确传递 system prompt、工具声明、模型选择、输出格式等参数
- NFR12: LLM 驱动层支持流式结构化输出模式（stream-json），用于 astrace 实时数据采集
- NFR13: 宿主文件系统通过 `/dev/fs` 访问时，遵循宿主 OS 的文件权限（不绕过宿主权限模型）
- NFR14: Shell 驱动（`/dev/shell`）执行命令时，继承当前用户的环境变量和 PATH

**安全（NFR15-17）**

- NFR15: `/dev/shell` 执行的命令继承当前用户权限，不提供额外提权能力
- NFR16: Skill 的 `manifest.yaml` 中 tools 声明作为智能体可访问设备的白名单——未声明的设备不可访问
- NFR17: MVP 阶段不实现完整 Capability 权限系统，但 Skill tools 白名单作为最小安全边界

**可维护性（NFR18-20）**

- NFR18: 内核代码遵循 Go 标准项目布局，通过 `go vet` 和 `golint` 无警告
- NFR19: syscall ABI 设计遵循 45 syscall 架构规范的子集，确保 Phase 2 扩展时向后兼容
- NFR20: LLM 驱动层封装在单一模块中，外部 LLM 接口变更时只需修改此模块

### Additional Requirements

**来自架构文档的技术需求：**

- 项目初始化（Starter）：领域驱动 OS 隐喻结构（方案 C），`go mod init github.com/gonewx/crux`，这是 Epic 1 Story 1 的基础
- Go 1.26：利用 Green Tea GC、Goroutine Leak Profiler（实验性）、new(expr) 表达式初始化、自引用泛型
- 泛型工具包：Registry[T]、SyncMap[K,V]、Future[T]、Result[T] 放在 `internal/xsync/`
- 共享类型：PID、FD、CtxID、ErrCode 等放在 `internal/types/types.go`（避免循环依赖）
- /dev/fs 驱动：需独立实现 `drivers/fs/hostfs.go`（FR16 支撑）
- 依赖方向严格单向：`cmd/ → kernel/ → vfs/ → drivers/`，禁止反向依赖
- 错误处理模式：所有 syscall 实现必须返回 `*SyscallError`（含 Syscall、PID、Device、Err、Code 字段）
- DebugChan 贯穿：所有 syscall 入口/出口必须写入 SyscallEvent（DebugChan 非 nil 时）
- 进程状态转移规则：只允许合法转移（Created→Running、Running→Zombie、Zombie→Dead），禁止逆向或跳跃
- 资源释放顺序：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → 状态转 Dead → 移除进程表
- 构建工具：Makefile（build/install/test/lint/vet/clean）+ golangci-lint（`.golangci.yml`）
- 测试策略：`go test -race` 默认开启，接口 mock，testify assertions，Goroutine Leak Profiler 验证 NFR8
- 依赖注入点：`cmd/crux/main.go` 是唯一组装点

**来自 UX 设计规范的实现需求：**

- Charm 生态（MVP）：cobra（命令框架）+ lipgloss（样式）+ bubbles/spinner（等待指示器）+ Charm table（表格）
- 6 个自定义 UI 组件（全部 P0，Process Table 为 P1）：Agent Progress Reporter、Result Box、Error Block、Summary Footer、Syscall Trace Line、Process Table
- TerminalProfile 检测（启动时执行一次）：宽度（`golang.org/x/term`）、IsTTY、颜色级别（0/1/2）、Unicode 支持
- Renderer 接口抽象：所有组件输出到 `io.Writer`，不直接写 `os.Stdout`
- 4 种输出密度模式：`--quiet/-q`（静默）、默认（结构化汇报）、`--verbose/-v`（详细）、`--json`（机器可读）
- 管道检测：非 TTY 输出自动去除 ANSI 颜色和 spinner 动画
- 环境变量：`NO_COLOR` 支持（颜色完全去除）、`CRUX_ASCII=1` 支持（Unicode 降级为 ASCII）
- 三段式错误结构：`✗ {设备路径}: {错误原因}` → `{影响}` → `建议: {恢复命令}`
- 信号处理：SIGINT 首次优雅中断（goroutine 清理），二次（2 秒内）强制退出
- 终端宽度自适应：< 60/60-79/80-119（目标）/120+ 列四档，表格列按优先级取舍

### FR Coverage Map

- FR1: Epic 1 — 通过自然语言意图创建智能体进程
- FR2: Epic 1 — 管理进程完整生命周期状态机
- FR3: Epic 4 — 终止正在运行的智能体进程（kill）
- FR4: Epic 4 — 等待智能体完成并获取退出状态（wait）
- FR5: Epic 4 — 孤儿进程重新挂载到 init
- FR6: Epic 4 — 回收 Zombie 进程并释放资源
- FR7: Epic 4 — 查看所有活跃进程列表（ps）
- FR8: Epic 1 — 驱动智能体执行 reasonStep 推理循环
- FR9: Epic 1 — 通过 LLM 驱动层非交互调用 LLM
- FR10: Epic 1 — 解析 LLM 响应 action 类型
- FR11: Epic 1 — LLM 超时/失败时进程正确转入 Zombie
- FR12: Epic 2 — 工具执行结果追加到智能体上下文
- FR13: Epic 1 — 提供统一 VFS 接口
- FR14: Epic 4 — /proc/{pid}/ 动态暴露运行时状态
- FR15: Epic 1 — /dev/ 路径注册和路由设备驱动
- FR16: Epic 2 — 通过 /dev/fs 读取宿主文件系统
- FR17: Epic 1 — 通过 /dev/llm/claude 访问 LLM
- FR18: Epic 2 — 通过 /dev/shell 执行 shell 命令
- FR19: Epic 1 — 为智能体分配独立上下文空间
- FR20: Epic 1 — 读写智能体上下文内容
- FR21: Epic 1 — 将上下文组装为完整 LLM prompt
- FR22: Epic 4 — 进程退出后释放上下文空间
- FR23: Epic 2 — 从 manifest.yaml 读取 Skill 元信息
- FR24: Epic 2 — 从 instructions.md 注入 system prompt
- FR25: Epic 2 — spawn 时指定加载 Skill
- FR26: Epic 2 — Skill tools 声明映射为设备权限白名单
- FR27: Epic 2 — 交付 code-analyst 参考 Skill
- FR28: Epic 3 — astrace 实时追踪所有 syscall
- FR29: Epic 3 — astrace 输出含名称、参数、返回值、耗时
- FR30: Epic 3 — 记录 syscall 调用数据（DebugRecord）
- FR31: Epic 3 — 通过 astrace 定位具体错误 syscall
- FR32: Epic 1 — 智能体完成时输出汇总信息
- FR33: Epic 1 — `crux "意图"` 单命令启动智能体
- FR34: Epic 3 — `crux astrace <pid>` 追踪命令
- FR35: Epic 4 — `crux ps` 查看进程状态
- FR36: Epic 1 — CLI 提供清晰错误信息
- FR37: Epic 1 — `go install` 安装，单二进制零依赖
- FR38: Epic 5 — 概念文档
- FR39: Epic 5 — 快速上手指南
- FR40: Epic 5 — 参考手册

## Epic List

### Epic 1: 第一个智能体运行（First Agent Runs）
用户安装 Crux 后，输入 `crux "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。包含项目初始化、内核核心（进程模型 + Spawn + reasonStep）、VFS 框架、LLM 驱动（Claude Code CLI）、上下文管理、CLI 入口和基础 UI 组件。
**FRs covered:** FR1, FR2, FR8, FR9, FR10, FR11, FR13, FR15, FR17, FR19, FR20, FR21, FR32, FR33, FR36, FR37

### Epic 2: Skill 能力与文件访问（Skills & File Access）
用户可以通过 Skill 赋予智能体专业能力（如代码分析），智能体可以读取宿主文件系统文件、执行 shell 命令——从"能说话"升级到"能干活"。包含 Skill 加载器、宿主 FS 驱动、Shell 驱动、权限白名单和 code-analyst 参考 Skill。
**FRs covered:** FR12, FR16, FR18, FR23, FR24, FR25, FR26, FR27

### Epic 3: 调试追踪（Debug Tracing — astrace）
当智能体输出不符合预期时，用户运行 `crux astrace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Crux 的差异化核心体验。包含 SyscallEvent 记录、DebugChan 事件管道、astrace 命令和 Trace Line UI。
**FRs covered:** FR28, FR29, FR30, FR31, FR34

### Epic 4: 进程管理与可靠性（Process Management & Reliability）
用户可以查看所有进程状态（`crux ps`）、终止进程（`crux kill`）、等待进程完成。系统自动回收 Zombie 进程、处理孤儿进程、暴露 `/proc` 运行时状态——生产级可靠性。
**FRs covered:** FR3, FR4, FR5, FR6, FR7, FR14, FR22, FR35

### Epic 5: 文档体系（Documentation）
新用户可以通过概念文档理解 Crux 的 OS 范式，通过快速上手指南在 15 分钟内跑通 demo，通过参考手册查阅所有 syscall、VFS 路径和 CLI 命令。
**FRs covered:** FR38, FR39, FR40

---

## Epic 1: 第一个智能体运行（First Agent Runs）

用户安装 Crux 后，输入 `crux "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。

### Story 1.1: 项目初始化与基础设施

As a 开发者,
I want 通过 `go install` 安装 Crux 并获得一个可构建的项目骨架,
So that 后续所有模块可以在标准化的 Go 项目结构上构建。

**Acceptance Criteria:**

**Given** 用户已安装 Go 1.26
**When** 执行 `go install github.com/gonewx/crux/cmd/crux@latest`
**Then** 获得 `crux` 二进制文件，执行 `crux version` 输出版本号
**And** 二进制无额外运行时依赖（除 Claude Code CLI）

**Given** 项目目录已创建
**When** 查看目录结构
**Then** 遵循架构文档定义的 OS 隐喻结构（`cmd/crux/`、`kernel/`、`vfs/`、`drivers/`、`context/`、`skills/`、`debug/`、`internal/types/`、`internal/xsync/`、`internal/ui/`）
**And** 包含 `go.mod`（模块路径 `github.com/gonewx/crux`）、`Makefile`、`.golangci.yml`、`.gitignore`

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

### Story 1.2: 进程模型与生命周期状态机

As a 内核开发者,
I want Process 结构体支持完整的生命周期状态转移,
So that 智能体进程可以在 Created → Running → Zombie → Dead 之间安全转换。

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

### Story 1.3: VFS 框架与设备注册

As a 智能体,
I want 通过统一的 VFS 接口访问所有设备（LLM、文件系统、Shell）,
So that 我不需要知道底层实现细节，只需操作文件描述符。

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

### Story 1.4: 上下文管理

As a 智能体,
I want 拥有独立的上下文空间来累积对话历史和工具结果,
So that 每轮 LLM 调用都能获得完整的推理上下文。

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

### Story 1.5: LLM 驱动层（Claude Code CLI）

As a 智能体,
I want 通过 `/dev/llm/claude` 设备调用 Claude Code CLI 进行 LLM 推理,
So that 我可以获得 LLM 的结构化响应来完成任务。

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

### Story 1.6: 内核推理循环（Spawn + reasonStep）

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

### Story 1.7: CLI 入口与 UI 组件

As a 用户,
I want 通过 `crux "意图"` 启动智能体并看到清晰的实时进度和结果输出,
So that 我全程知道智能体在做什么、结果是什么。

**Acceptance Criteria:**

**Given** `cmd/crux/main.go` 已实现
**When** 执行 `crux "分析代码"`
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
**When** 执行 `crux "意图" | cat`
**Then** 自动去除 ANSI 颜色码和 spinner 动画

### Story 1.8: 端到端集成与验收

As a 用户,
I want 完整的端到端体验：`crux "意图"` → 实时进度 → 结果 → 汇总,
So that 我确认整个系统协同工作正常。

**Acceptance Criteria:**

**Given** 所有 Story 1.1-1.7 已完成
**When** 执行 `crux "分析 ./README.md"`
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

**Given** `crux version` 执行
**When** Claude Code CLI 未安装
**Then** 输出 `✗ claude-code CLI not found` + 安装建议

**Given** 执行 `crux --help`
**When** 查看帮助输出
**Then** 显示 Usage + 可用命令列表 + 全局 flags + 示例

**Given** 信号处理
**When** 用户按 Ctrl+C（首次）
**Then** 当前智能体优雅中断，进程转 zombie，输出中断摘要
**And** 2 秒内二次 Ctrl+C 强制退出

---

## Epic 2: Skill 能力与文件访问（Skills & File Access）

智能体可以通过 Skill 获得专业能力，读取宿主文件系统文件、执行 shell 命令——从"能说话"升级到"能干活"。

### Story 2.1: Skill 加载器与 manifest 解析

As a 用户,
I want 系统能从 `manifest.yaml` 和 `instructions.md` 加载 Skill 定义,
So that 智能体可以获得专业化的能力和指令。

**Acceptance Criteria:**

**Given** `skills/types.go` 已实现
**When** 查看 SkillManifest 类型
**Then** 包含 `Name`、`Description`、`Tools`（[]string 设备路径列表）、`Models`（provider/preferred/fallback）、`ContextBudget` 字段

**Given** `skills/loader.go` 已实现
**When** 调用 `SkillLoader.Load("lib/skills/code-analyst")`
**Then** 解析 `manifest.yaml` 为 `SkillManifest` 结构（使用泛型 `LoadYAML[SkillManifest]`）
**And** 读取 `instructions.md` 为原始文本
**And** 返回完整的 `SkillInfo`（manifest + instructions 内容）

**Given** manifest.yaml 格式无效或缺少必填字段
**When** 调用 Load
**Then** 返回清晰的错误信息，标注具体缺失字段

**Given** Skill 目录不存在
**When** 调用 Load
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

### Story 2.2: 宿主文件系统驱动（/dev/fs）

As a 智能体,
I want 通过 `/dev/fs` 设备读取宿主文件系统上的文件,
So that 我可以分析用户的源代码和文档。

**Acceptance Criteria:**

**Given** `drivers/fs/hostfs.go` 已实现
**When** 调用 `Open("/dev/fs/path/to/file", O_RDONLY)`
**Then** 打开宿主文件系统对应文件，返回 VFSFile 封装

**Given** 文件已打开
**When** 调用 `Read(fd, length)`
**Then** 读取文件内容并返回
**And** 额外延迟 < 10ms，不超过直接文件 I/O 的 2 倍（NFR4）

**Given** 文件存在但无读取权限
**When** 调用 Open
**Then** 返回 `*SyscallError`，`Code` 为 `ErrPermission`
**And** 遵循宿主 OS 的文件权限模型（NFR13）

**Given** 文件路径不存在
**When** 调用 Open
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** HostFS 驱动已创建
**When** 在 `cmd/crux/main.go` 中注册
**Then** `devRegistry.Register("/dev/fs", hostFSDriver.FileFactory())`

### Story 2.3: Shell 驱动（/dev/shell）

As a 智能体,
I want 通过 `/dev/shell` 设备执行宿主系统的 shell 命令,
So that 我可以运行构建工具、检查环境、执行脚本。

**Acceptance Criteria:**

**Given** `drivers/shell/shell.go` 已实现
**When** 调用 `Write(fd, []byte("ls -la"))`
**Then** 通过 `exec.CommandContext` 执行 shell 命令
**And** 继承当前用户的环境变量和 PATH（NFR14）
**And** 继承当前用户权限，不提供额外提权（NFR15）

**Given** shell 命令执行完成
**When** 调用 `Read(fd, length)`
**Then** 返回 stdout + stderr 合并输出

**Given** shell 命令超时（默认 30 秒）
**When** 超时触发
**Then** 终止命令进程，返回 `*SyscallError`，`Code` 为 `ErrTimeout`

**Given** ShellDriver 已创建
**When** 在 `cmd/crux/main.go` 中注册
**Then** `devRegistry.Register("/dev/shell", shellDriver.FileFactory())`

### Story 2.4: Skill 注入与设备权限白名单

As a 用户,
I want Spawn 时指定 Skill，系统自动注入 instructions 并限制设备访问范围,
So that 智能体获得专业指令同时只能访问 Skill 声明的设备。

**Acceptance Criteria:**

**Given** 用户执行 `crux "分析代码" --skill=code-analyst`
**When** Spawn 创建进程
**Then** 加载 code-analyst Skill 的 instructions.md 内容
**And** 注入到 LLM 调用的 `--system-prompt` 参数中

**Given** Skill manifest 声明 `tools: ["/dev/fs", "/dev/shell"]`
**When** 智能体尝试 `Open("/dev/llm/claude")`
**Then** 如果 `/dev/llm/claude` 不在 tools 白名单中，返回 `*SyscallError`，`Code` 为 `ErrPermission`（NFR16）

**Given** Skill manifest 声明 `models.provider: claude`、`models.preferred: sonnet`
**When** Spawn 创建进程
**Then** LLM 调用自动使用 `/dev/llm/claude` 驱动和 `sonnet` 模型

**Given** 用户未指定 `--skill`
**When** Spawn 创建进程
**Then** 使用通用模式（无 Skill instructions 注入），所有设备可访问（NFR17 最小安全边界）

**Given** 工具执行产生结果
**When** reasonStep 处理 tool_call 返回值
**Then** 结果追加到智能体上下文中（CtxWrite）供后续推理使用（FR12）

### Story 2.5: code-analyst 参考 Skill

As a 用户,
I want 一个预装的 code-analyst Skill 作为参考实现,
So that 我可以立即使用 Crux 分析代码并作为编写自定义 Skill 的模板。

**Acceptance Criteria:**

**Given** `lib/skills/code-analyst/manifest.yaml` 已创建
**When** 查看 manifest 内容
**Then** 包含 `name: code-analyst`、`tools: ["/dev/fs", "/dev/shell"]`、`models.provider: claude`、`models.preferred: sonnet`

**Given** `lib/skills/code-analyst/instructions.md` 已创建
**When** 查看 instructions 内容
**Then** 包含代码分析的系统指令（角色定义、分析策略、输出格式要求）

**Given** code-analyst Skill 已加载
**When** 执行 `crux "分析 ./kernel/scheduler.go" --skill=code-analyst`
**Then** 智能体读取目标文件，进行分析，输出结构化的分析结果
**And** 能够识别至少 1 个可验证的真实代码问题（FR27）

**Given** `skills/testdata/mock-skill/` 已创建
**When** 运行 Skill 加载器测试
**Then** 使用 mock-skill 作为测试 fixture，验证加载流程

---

## Epic 3: 调试追踪（Debug Tracing — astrace）

当智能体输出不符合预期时，用户运行 `crux astrace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Crux 的差异化核心体验。

### Story 3.1: SyscallEvent 记录基础设施

As a 内核开发者,
I want 每个 syscall 的入口和出口都自动记录为 SyscallEvent,
So that astrace 可以消费完整的调用链路数据。

**Acceptance Criteria:**

**Given** `debug/event.go` 已实现
**When** 查看 SyscallEvent 结构
**Then** 包含 `Timestamp`（相对进程启动）、`PID`、`Syscall`（名称）、`Args`（map[string]any）、`Result`（any）、`Err`（error）、`Duration`（耗时）字段

**Given** 进程的 `DebugChan` 非 nil
**When** 任意 syscall（Open/Read/Write/Close/Stat/CtxAlloc/CtxRead/CtxWrite 等）执行
**Then** 入口处构造 SyscallEvent（记录 Syscall 名称和 Args）
**And** 出口处补充 Result、Err、Duration
**And** 写入 `Process.DebugChan`（缓冲 256）

**Given** 进程的 `DebugChan` 为 nil（无 astrace 附着）
**When** syscall 执行
**Then** 跳过事件记录（零开销）

**Given** DebugChan 缓冲已满
**When** 写入新事件
**Then** 不阻塞 syscall 执行（非阻塞写入或丢弃最旧事件）

### Story 3.2: astrace 事件消费与格式化

As a 用户,
I want `crux astrace <pid>` 实时流式输出 syscall 调用链路,
So that 我能看到智能体的每一步操作及其结果。

**Acceptance Criteria:**

**Given** `debug/astrace.go` 已实现
**When** 调用 astrace 附着到指定 PID
**Then** 消费目标进程的 DebugChan
**And** 每个 SyscallEvent 格式化为一行输出

**Given** astrace 流式输出中
**When** 收到一个 SyscallEvent
**Then** 输出格式为 `[N.NNNs] SyscallName(args) → result    duration`
**And** 时间戳固定宽度 `[N.NNNs]`
**And** syscall 名称与接口方法名一致（如 `Spawn`、`Open`、`CtxWrite`）（FR29）

**Given** 某个 syscall 耗时 > 1 秒
**When** 格式化该事件
**Then** 行尾自动添加 `← 慢操作` 标注（暗灰色）

**Given** 某个 syscall 返回错误
**When** 格式化该事件
**Then** 整行用红色高亮显示（FR31）

**Given** astrace 输出延迟
**When** 从 syscall 发生到终端显示
**Then** 延迟 ≤ 500ms（NFR3）

### Story 3.3: astrace CLI 命令

As a 用户,
I want 通过 `crux astrace <pid>` 命令启动 syscall 追踪,
So that 我可以在任何时候调试正在运行的智能体。

**Acceptance Criteria:**

**Given** `cmd/crux/main.go` 中 astrace 子命令已注册
**When** 执行 `crux astrace 1`
**Then** 附着到 PID 1 的 DebugChan，开始流式输出 syscall 事件

**Given** 指定的 PID 不存在
**When** 执行 `crux astrace 999`
**Then** 输出 `✗ PID 999: process not found` + `→ 建议: crux ps  查看活跃进程`

**Given** astrace 正在追踪
**When** 用户按 Ctrl+C
**Then** 仅 detach 追踪，不影响被追踪进程的运行

**Given** 被追踪进程完成
**When** DebugChan 关闭
**Then** astrace 输出 detach 汇总后退出

**Given** 使用 `--verbose` flag
**When** 格式化 SyscallEvent
**Then** 展开完整的参数和返回值（默认模式可能截断长参数）

**Given** 使用 `--json` flag
**When** 格式化 SyscallEvent
**Then** 每行输出一个 JSON 对象，字段为 snake_case

### Story 3.4: Syscall Trace Line UI 组件

As a 用户,
I want astrace 输出清晰可读，关键信息一眼可见,
So that 我不需要在密集输出中翻找问题。

**Acceptance Criteria:**

**Given** `internal/ui/trace.go` 已实现
**When** 渲染 Trace Line
**Then** 时间戳暗灰色，syscall 名称 Crux Blue 加粗，参数普通文本，返回值 `→` 后跟结果

**Given** 错误 syscall
**When** 渲染
**Then** 整行红色高亮，在密集输出中视觉上"跳出来"

**Given** LLM 相关 syscall（Open/Write/Read `/dev/llm/*`）
**When** 渲染
**Then** 行尾标注 `← LLM 调用`

**Given** NO_COLOR 环境变量设置
**When** 渲染 Trace Line
**Then** 颜色降级为纯文本，错误行前缀 `[ERR]`

---

## Epic 4: 进程管理与可靠性（Process Management & Reliability）

用户可以查看进程状态（`crux ps`）、终止进程（`crux kill`）、等待进程完成。系统自动回收 Zombie、处理孤儿进程、暴露 `/proc` 运行时状态——生产级可靠性。

### Story 4.1: Kill 与 Wait 系统调用

As a 用户,
I want 终止运行中的智能体并等待其完成,
So that 我可以管理智能体的生命周期。

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中 Kill 已实现
**When** 调用 `Kill(pid, signal)`
**Then** 向目标进程发送取消信号（`cancel()`）
**And** 进程 reasonStep 循环检测到取消后停止
**And** 进程状态转为 Zombie

**Given** `kernel/reap.go` 中 Wait 已实现
**When** 调用 `Wait(pid)`
**Then** 阻塞直到目标进程状态变为 Zombie
**And** 返回 ExitStatus（退出码 + 错误信息）
**And** 触发资源释放：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree
**And** 状态转为 Dead，从进程表移除

**Given** 目标 PID 不存在
**When** 调用 Kill 或 Wait
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 进程已经是 Zombie
**When** 调用 Kill
**Then** 操作为空操作（幂等），不返回错误

### Story 4.2: 孤儿进程 reparent 与 Zombie 自动回收

As a 内核开发者,
I want 孤儿进程自动挂载到 init，Zombie 进程自动回收,
So that 系统不会积累无主进程或资源泄漏。

**Acceptance Criteria:**

**Given** 父进程退出
**When** 子进程仍在运行
**Then** 子进程的 PPID 自动变更为 PID 1（init）
**And** init 负责后续 Wait 回收

**Given** 进程状态变为 Zombie
**When** 内核 reaper 检测到
**Then** 自动执行 Wait 逻辑：资源释放 → 状态转 Dead → 移除进程表
**And** 资源释放顺序：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → Dead → 移除

**Given** 进程退出后
**When** 10 秒内检测 goroutine 状态
**Then** 所有关联 goroutine 和 context 内存已释放，无泄漏（NFR8）

**Given** 进程异常退出（panic、timeout）
**When** 检查进程表
**Then** 进程表保持一致性，无悬挂 PID（NFR9）

### Story 4.3: /proc 动态文件系统

As a 用户,
I want 通过 `/proc/{pid}/` 路径查看智能体的运行时状态,
So that 我可以程序化地获取进程信息。

**Acceptance Criteria:**

**Given** `vfs/proc.go` 已实现
**When** 调用 `Open("/proc/1/status")`
**Then** 返回 PID 1 的实时状态 JSON（pid、state、intent、skills、tokens、elapsed）

**Given** ProcFS 已注册
**When** 调用 `Open("/proc/1/intent")`
**Then** 返回 PID 1 的原始意图文本

**Given** ProcFS 已注册
**When** 调用 `Open("/proc/1/context")`
**Then** 返回 PID 1 的当前上下文内容摘要

**Given** PID 不存在
**When** 调用 `Open("/proc/999/status")`
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** ProcFS 需要读取进程信息
**When** 查看实现
**Then** 通过 `ProcessInfoProvider` 接口读取（不直接依赖 kernel 包，避免反向依赖）

### Story 4.4: crux ps 命令与 Process Table UI

As a 用户,
I want 通过 `crux ps` 查看所有进程的状态表格,
So that 我随时了解系统中智能体的全局状态。

**Acceptance Criteria:**

**Given** `cmd/crux/main.go` 中 ps 子命令已注册
**When** 执行 `crux ps`
**Then** 调用 `kernel.PS(filter)` 获取所有进程信息
**And** 通过 Process Table 组件输出对齐表格

**Given** `internal/ui/table.go` 已实现
**When** 渲染进程表格
**Then** 列包含 PID、STATE、SKILL、TOKENS、ELAPSED
**And** 数字右对齐，文本左对齐
**And** STATE 列颜色编码：running=蓝、zombie=黄、dead=灰
**And** 响应时间 ≤ 100ms（NFR2）

**Given** 无活跃进程
**When** 执行 `crux ps`
**Then** 输出 `No active processes.`（不显示空表格）

**Given** 使用 `--json` flag
**When** 执行 `crux ps --json`
**Then** 输出 JSON 数组，每个元素包含 pid、state、skill、tokens、elapsed_ms（snake_case）

**Given** 终端宽度 < 80 列
**When** 渲染表格
**Then** 按优先级保留列：PID + STATE（永远显示）→ SKILL（≥60 列）→ TOKENS + ELAPSED（≥80 列）

### Story 4.5: 上下文释放（ctx_free）

As a 内核开发者,
I want 进程退出后其上下文空间被正确释放,
So that 系统不会因为上下文累积而内存泄漏。

**Acceptance Criteria:**

**Given** 进程状态转为 Dead
**When** 资源释放流程执行
**Then** 调用 `CtxFree(process.Ctx.ID)` 释放上下文空间

**Given** 上下文已释放
**When** 尝试 CtxRead 或 CtxWrite 该 CtxID
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 多个进程同时退出
**When** 并发执行 CtxFree
**Then** 无数据竞争（通过 `-race` 测试验证）

---

## Epic 5: 文档体系（Documentation）

新用户可以通过概念文档理解 Crux 的 OS 范式，通过快速上手指南在 15 分钟内跑通 demo，通过参考手册查阅所有 syscall、VFS 路径和 CLI 命令。

### Story 5.1: 概念文档

As a 新用户,
I want 阅读概念文档理解 Crux 的核心 OS 范式,
So that 我能建立正确的心智模型来使用 Crux。

**Acceptance Criteria:**

**Given** 概念文档已编写
**When** 阅读文档
**Then** 覆盖四个核心概念：进程（Process）、虚拟文件系统（VFS）、Skill、系统调用（Syscall）
**And** 每个概念包含：定义、与 Unix 对应概念的类比、至少一个具体示例
**And** 概念之间的关系清晰（进程通过 syscall 访问 VFS，Skill 注入进程能力）

### Story 5.2: 快速上手指南

As a 新用户,
I want 按照快速上手指南从安装到跑通第一个 demo,
So that 我在 15 分钟内体验到 Crux 的核心价值。

**Acceptance Criteria:**

**Given** 快速上手指南已编写
**When** 按步骤操作
**Then** 覆盖完整流程：安装 Go → 安装 Crux（`go install`）→ 验证（`crux version`）→ 首次执行（`crux "分析 ./README.md"`）→ 查看结果 → 首次 astrace（`crux astrace 1`）
**And** 目标完成时间 ≤ 15 分钟（FR39）
**And** 每一步包含预期输出示例，用户可对照验证

### Story 5.3: 参考手册

As a 开发者,
I want 查阅参考手册获取所有 syscall、VFS 路径和 CLI 命令的完整规范,
So that 我在编写 Skill 或调试时有权威参考。

**Acceptance Criteria:**

**Given** 参考手册已编写
**When** 查阅内容
**Then** 包含 MVP 全部 15 个 syscall 的签名、参数、返回值、错误码、示例
**And** 包含完整 VFS 路径规范（`/proc/{pid}/`、`/dev/llm/`、`/dev/fs`、`/dev/shell`、`/lib/skills/`）
**And** 包含 manifest.yaml 全部字段说明和示例
**And** 包含 CLI 命令完整列表（`crux "意图"`、`crux ps`、`crux astrace`、`crux kill`、`crux version`）及其 flags
**And** 包含 IPC 架构说明：daemon 生命周期（自动启动/自动停止/stale socket 清理）、Unix domain socket 通信机制、IPC 协议概述（NDJSON 消息格式、Method 枚举、流式 StreamEvent）
