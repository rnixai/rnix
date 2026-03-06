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
date: '2026-02-27'
phase2Added: '2026-02-27'
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

**智能体定义管理（FR23-FR25）**

- FR23: 系统可以从 `agent.yaml` 读取 Agent 的元信息（名称、描述、模型偏好、上下文预算、Skill 引用列表）
- FR24: 系统可以从 Agent 的 `instructions.md` 读取角色定义并注入智能体的 system prompt
- FR25: 用户可以在 spawn 时通过 `--agent=<name>` 指定 Agent 定义

**能力模块管理（FR25a-FR27，遵循 Agent Skills 行业标准）**

- FR25a: 系统可以从 `SKILL.md` 解析 Skill 元信息（name、description、allowed-tools），格式遵循 Agent Skills 开放标准（agentskills.io）
- FR25b: 系统支持 Skill 的渐进式加载——启动时仅加载 frontmatter（~100 tokens/skill），激活时加载完整 SKILL.md body（< 5000 tokens），执行时按需加载 scripts/references/assets
- FR26: Agent 引用的所有 Skill 的 `allowed-tools` 聚合后映射为智能体的可用 `/dev/` 设备权限白名单
- FR27: 系统交付参考 Agent（code-analyst）+ 参考 Skill（code-analysis），能够分析代码并识别至少 1 个可验证的真实代码问题

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
- FR40: 系统可以提供参考手册，覆盖 syscall 列表、VFS 路径规范、agent.yaml / SKILL.md 字段、CLI 命令

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
- NFR16: Skill 的 `SKILL.md` 中 `allowed-tools` 声明作为智能体可访问设备的白名单——Agent 引用的所有 Skill 的 `allowed-tools` 聚合后，未声明的设备不可访问
- NFR17: MVP 阶段不实现完整 Capability 权限系统，但 Skill `allowed-tools` 聚合白名单作为最小安全边界

**可维护性（NFR18-20）**

- NFR18: 内核代码遵循 Go 标准项目布局，通过 `go vet` 和 `golint` 无警告
- NFR19: syscall ABI 设计遵循 45 syscall 架构规范的子集，确保 Phase 2 扩展时向后兼容
- NFR20: LLM 驱动层封装在单一模块中，外部 LLM 接口变更时只需修改此模块

### Additional Requirements

**来自架构文档的技术需求：**

- 项目初始化（Starter）：领域驱动 OS 隐喻结构（方案 C），`go mod init github.com/usecrux/crux`，这是 Epic 1 Story 1 的基础
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
- Agent/Skill 双层架构：Agent（agent.yaml + instructions.md）定义身份+策略，Skill（SKILL.md，Agent Skills 行业标准）定义程序性知识+工具权限
- Spawn 流程：AgentLoader 加载 agent.yaml → 读 instructions.md → SkillLoader 加载引用的 Skill → 聚合 allowed-tools → 组装 system prompt
- 渐进式 Skill 加载：发现（frontmatter ~100 tokens）→ 激活（body < 5000 tokens）→ 执行（scripts/references/assets 按需加载）

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
- FR23: Epic 2 — 从 agent.yaml 读取 Agent 元信息（名称、Skill 引用列表等）
- FR24: Epic 2 — 从 Agent 的 instructions.md 注入 system prompt
- FR25: Epic 2 — spawn 时通过 --agent=<name> 指定 Agent 定义
- FR25a: Epic 2 — 从 SKILL.md 解析 Skill 元信息（Agent Skills 行业标准）
- FR25b: Epic 2 — Skill 渐进式加载（frontmatter → body → assets）
- FR26: Epic 2 — Agent 引用的所有 Skill 的 allowed-tools 聚合为设备权限白名单
- FR27: Epic 2 — 交付参考 Agent（code-analyst）+ 参考 Skill（code-analysis）
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

### Epic 2: Agent 能力与文件访问（Agent Skills & File Access）
用户可以通过 Agent 定义赋予智能体专业能力（如代码分析），Agent 引用的 Skill 决定智能体可访问的工具和知识——从"能说话"升级到"能干活"。包含 Agent 加载器、Skill 加载器（SKILL.md，Agent Skills 行业标准）、宿主 FS 驱动、Shell 驱动、allowed-tools 聚合白名单和 code-analyst 参考 Agent + code-analysis 参考 Skill。
**FRs covered:** FR12, FR16, FR18, FR23, FR24, FR25, FR25a, FR25b, FR26, FR27

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

### Phase 2 Epics

### Epic 6: IPC 跨进程通信（Inter-Process Communication）
智能体之间可以通过 Send/Recv 发送消息、通过 Pipe 连接输出与输入、通过进程组批量操作、通过 Signal 控制执行——多智能体协作的通信基础。
**FRs covered:** FR41, FR42, FR43, FR44, FR45
**NFRs:** NFR22 (IPC ≤50ms), NFR23 (Pipe ≥1MB/s), NFR24 (≥10 并发进程)
**Dependencies:** Phase 1 完成

### Epic 7: Compose 多智能体编排（Agent Compose）
用户通过 `crux-compose.yaml` 声明式定义多智能体工作流，`crux compose up` 一键启动，引擎按 DAG 依赖拓扑自动调度并行——林薇旅程的核心体验。
**FRs covered:** FR46, FR47, FR48, FR49
**NFRs:** NFR21 (≤10 个智能体启动 ≤2s)
**Dependencies:** Epic 6（IPC 管道用于智能体间数据传递）

### Epic 8: Skill 包管理与生态（Skill Package Management）
用户通过 `skill install/search/update/list` 管理社区 Skill，安装即可用，零修改引用——生态系统的基石。
**FRs covered:** FR50, FR51, FR52, FR53
**NFRs:** NFR30 (安装即可用)

### Epic 9: MCP 服务集成（MCP Integration）
系统通过 Mount/Unmount 在 `/mnt/mcp/` 挂载 MCP 服务器，智能体通过标准 VFS 访问外部工具——完成 Agent → Skill → MCP → Device 四层能力栈。
**FRs covered:** FR54, FR55, FR56, FR57
**NFRs:** NFR25 (挂载 ≤500ms), NFR26 (异常不影响内核), NFR27 (MCP 标准兼容)
**Dependencies:** Epic 6（VFS 扩展）

### Epic 10: 监控、Supervisor 与运维（Monitoring, Supervisor & Operations）
`crux top` 实时监控面板 + `crux log` 分类日志 + token 预算管理 + Supervisor 容错树 + init 引导——生产级运维能力。
**FRs covered:** FR58, FR59, FR60, FR61, FR62, FR63, FR64, FR65
**NFRs:** NFR28 (top 刷新 ≤500ms), NFR29 (log 延迟 ≤200ms)
**Dependencies:** Epic 6（进程组用于 Supervisor 树）

### Epic 11: AgentShell 高级语法（AgentShell Advanced Syntax）
管道组合 `spawn "A" | spawn "B"`、变量环境 `export KEY=VALUE`、最小控制结构 `if-else` + `on-error`——从单命令到脚本编排。
**FRs covered:** FR66, FR67, FR68
**Dependencies:** Epic 6（IPC Pipe）

### Epic 12: Phase 2 文档（Phase 2 Documentation）
三个核心教程（编写 Skill、调试 bug、多智能体工作流）+ 四模块架构文档（微内核、进程模型、驱动层、上下文管理）。
**FRs covered:** FR69, FR70
**Dependencies:** Epic 7-11 完成后编写

---

## Epic 1: 第一个智能体运行（First Agent Runs）

用户安装 Crux 后，输入 `crux "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。

### Story 1.1: 项目初始化与基础设施

As a 开发者,
I want 通过 `go install` 安装 Crux 并获得一个可构建的项目骨架,
So that 后续所有模块可以在标准化的 Go 项目结构上构建。

**Acceptance Criteria:**

**Given** 用户已安装 Go 1.26
**When** 执行 `go install github.com/usecrux/crux/cmd/crux@latest`
**Then** 获得 `crux` 二进制文件，执行 `crux version` 输出版本号
**And** 二进制无额外运行时依赖（除 Claude Code CLI）

**Given** 项目目录已创建
**When** 查看目录结构
**Then** 遵循架构文档定义的 OS 隐喻结构（`cmd/crux/`、`kernel/`、`vfs/`、`drivers/`、`context/`、`skills/`、`debug/`、`internal/types/`、`internal/xsync/`、`internal/ui/`）
**And** 包含 `go.mod`（模块路径 `github.com/usecrux/crux`）、`Makefile`、`.golangci.yml`、`.gitignore`

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

## Epic 2: Agent 能力与文件访问（Agent Skills & File Access）

智能体可以通过 Agent 定义获得专业能力，Agent 引用的 Skill（遵循 Agent Skills 行业标准）决定工具权限和程序性知识——从"能说话"升级到"能干活"。

### Story 2.1: Agent 加载器与 Skill 加载器

As a 用户,
I want 系统能从 `agent.yaml` + `instructions.md` 加载 Agent 定义，并从 `SKILL.md` 加载 Skill 定义,
So that 智能体可以获得身份、策略和专业化能力。

**Acceptance Criteria:**

**Given** `agents/types.go` 已实现
**When** 查看 AgentManifest 类型
**Then** 包含 `Name`、`Description`、`Skills`（[]string Skill 引用列表）、`Models`（provider/preferred/fallback）、`ContextBudget` 字段

**Given** `agents/loader.go` 已实现
**When** 调用 `AgentLoader.Load("lib/agents/code-analyst")`
**Then** 解析 `agent.yaml` 为 `AgentManifest` 结构（使用泛型 `LoadYAML[AgentManifest]`）
**And** 读取 `instructions.md` 为原始文本
**And** 返回完整的 `AgentInfo`（manifest + instructions 内容）

**Given** `skills/types.go` 已实现
**When** 查看 SkillManifest 类型
**Then** 包含 `Name`、`Description`、`AllowedTools`（[]string 设备路径列表）字段，遵循 Agent Skills 行业标准（SKILL.md YAML frontmatter 格式）

**Given** `skills/loader.go` 已实现
**When** 调用 `SkillLoader.Load("lib/skills/code-analysis")`
**Then** 解析 `SKILL.md` 的 YAML frontmatter 为 `SkillManifest` 结构
**And** 读取 SKILL.md body 为 Skill 指令内容
**And** 支持渐进式加载：发现（仅 frontmatter ~100 tokens）→ 激活（body < 5000 tokens）→ 执行（scripts/references/assets 按需加载）（FR25b）

**Given** agent.yaml 或 SKILL.md 格式无效或缺少必填字段
**When** 调用 Load
**Then** 返回清晰的错误信息，标注具体缺失字段

**Given** Agent 或 Skill 目录不存在
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

### Story 2.4: Agent 注入与设备权限白名单

As a 用户,
I want Spawn 时指定 Agent，系统自动加载 Agent 定义和引用的 Skill，注入 instructions 并限制设备访问范围,
So that 智能体获得身份和专业指令，同时只能访问 Skill 声明的设备。

**Acceptance Criteria:**

**Given** 用户执行 `crux "分析代码" --agent=code-analyst`
**When** Spawn 创建进程
**Then** 加载 code-analyst Agent 的 `agent.yaml` + `instructions.md`
**And** 加载 agent.yaml 中引用的所有 Skill（如 code-analysis）
**And** 将 Agent instructions + Skill 指令注入到 LLM 调用的 `--system-prompt` 参数中

**Given** Agent 引用的 Skill 声明 `allowed-tools: ["/dev/fs", "/dev/shell"]`
**When** 智能体尝试 `Open("/dev/llm/claude")`
**Then** 如果 `/dev/llm/claude` 不在所有 Skill 的 `allowed-tools` 聚合白名单中，返回 `*SyscallError`，`Code` 为 `ErrPermission`（NFR16）

**Given** Agent 的 agent.yaml 声明 `models.provider: claude`、`models.preferred: sonnet`
**When** Spawn 创建进程
**Then** LLM 调用自动使用 `/dev/llm/claude` 驱动和 `sonnet` 模型

**Given** 用户未指定 `--agent`
**When** Spawn 创建进程
**Then** 使用通用模式（无 Agent/Skill 注入），所有设备可访问（NFR17 最小安全边界）

**Given** 工具执行产生结果
**When** reasonStep 处理 tool_call 返回值
**Then** 结果追加到智能体上下文中（CtxWrite）供后续推理使用（FR12）

### Story 2.5: code-analyst 参考 Agent 与 code-analysis 参考 Skill

As a 用户,
I want 一个预装的 code-analyst Agent 和 code-analysis Skill 作为参考实现,
So that 我可以立即使用 Crux 分析代码并作为编写自定义 Agent/Skill 的模板。

**Acceptance Criteria:**

**Given** `lib/agents/code-analyst/agent.yaml` 已创建
**When** 查看 agent.yaml 内容
**Then** 包含 `name: code-analyst`、`skills: ["code-analysis"]`、`models.provider: claude`、`models.preferred: sonnet`

**Given** `lib/agents/code-analyst/instructions.md` 已创建
**When** 查看 instructions 内容
**Then** 包含 code-analyst 的角色定义（身份、策略、输出格式要求）

**Given** `lib/skills/code-analysis/SKILL.md` 已创建
**When** 查看 SKILL.md 内容
**Then** YAML frontmatter 包含 `name: code-analysis`、`allowed-tools: ["/dev/fs", "/dev/shell"]`
**And** body 包含代码分析的程序性知识（分析策略、步骤、输出格式）

**Given** code-analyst Agent 已加载
**When** 执行 `crux "分析 ./kernel/scheduler.go" --agent=code-analyst`
**Then** 智能体读取目标文件，进行分析，输出结构化的分析结果
**And** 能够识别至少 1 个可验证的真实代码问题（FR27）

**Given** `skills/testdata/mock-skill/` 和 `agents/testdata/mock-agent/` 已创建
**When** 运行 Agent/Skill 加载器测试
**Then** 使用 mock 数据作为测试 fixture，验证加载流程

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
**And** 包含 agent.yaml 全部字段说明和示例、SKILL.md（Agent Skills 行业标准）全部字段说明和示例
**And** 包含 CLI 命令完整列表（`crux "意图"`、`crux ps`、`crux astrace`、`crux kill`、`crux version`）及其 flags
**And** 包含 IPC 架构说明：daemon 生命周期（自动启动/自动停止/stale socket 清理）、Unix domain socket 通信机制、IPC 协议概述（NDJSON 消息格式、Method 枚举、流式 StreamEvent）、连接复用语义（非流式请求 Ping/ListProcs/Kill 复用同一连接，流式请求 Spawn/AttachDebug 终结连接）

---

## Phase 2 FR Coverage Map

- FR41: Epic 6 — Send/Recv syscall 消息传递
- FR42: Epic 6 — Pipe syscall 管道连接
- FR43: Epic 6 — 进程组管理（JoinGroup/GetProcGroup）
- FR44: Epic 6 — 三级并发模型（进程/线程/协程）
- FR45: Epic 6 — Signal syscall 信号系统
- FR46: Epic 7 — crux-compose.yaml 声明式定义
- FR47: Epic 7 — Compose 引擎 DAG 依赖调度
- FR48: Epic 7 — crux compose up 一键启动
- FR49: Epic 7 — crux compose down 停止释放
- FR50: Epic 8 — skill install 安装
- FR51: Epic 8 — skill search 搜索
- FR52: Epic 8 — skill update 更新
- FR53: Epic 8 — skill list 本地注册表
- FR54: Epic 9 — Mount/Unmount syscall
- FR55: Epic 9 — agent.yaml mcp 字段引用
- FR56: Epic 9 — VFS 路径暴露 MCP 工具
- FR57: Epic 9 — 四层能力栈端到端
- FR58: Epic 10 — crux top 实时监控
- FR59: Epic 10 — crux log 推理日志
- FR60: Epic 10 — crux log think/tool/output 分类
- FR61: Epic 10 — token 预算管理
- FR62: Epic 10 — crux top 交互式操作
- FR63: Epic 10 — Supervisor 树管理
- FR64: Epic 10 — 三种重启策略
- FR65: Epic 10 — init 引导序列
- FR66: Epic 11 — 管道语法 spawn|spawn
- FR67: Epic 11 — 变量与环境传递
- FR68: Epic 11 — 最小控制结构 if-else + on-error
- FR69: Epic 12 — 教程文档（3 个场景）
- FR70: Epic 12 — 架构文档（4 个模块）

---

## Epic 6: IPC 跨进程通信（Inter-Process Communication）

智能体之间可以通过消息传递、管道连接、进程组管理和信号控制实现协作——多智能体系统的通信基础设施。

### Story 6.1: Send/Recv 消息传递

As a 智能体,
I want 通过 Send/Recv syscall 向其他智能体发送消息和接收消息,
So that 多个智能体之间可以交换数据和协调工作。

**Acceptance Criteria:**

**Given** `kernel/ipc.go` 中 Send/Recv 已实现
**When** 智能体 A 调用 `Send(targetPID, message)`
**Then** 消息进入目标进程的消息队列
**And** 目标进程调用 `Recv()` 时获取到该消息
**And** 单条消息端到端延迟 ≤ 50ms（NFR22）

**Given** 目标 PID 不存在
**When** 调用 `Send(999, message)`
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 接收队列为空
**When** 调用 `Recv()`
**Then** 阻塞直到有新消息到达或 context 取消

**Given** `IPCManager` 子接口已定义
**When** 嵌入到 Kernel 接口
**Then** 不破坏 Phase 1 现有 ABI（NFR19）

**Given** 多个智能体并发 Send
**When** 同时向同一目标进程发送消息
**Then** 消息全部到达，无丢失，无数据竞争（`-race` 测试通过）

### Story 6.2: Pipe 管道

As a 用户,
I want 通过 Pipe 将一个智能体的输出连接为另一个智能体的输入,
So that 智能体可以流式传递数据，实现链式处理。

**Acceptance Criteria:**

**Given** `kernel/ipc.go` 中 Pipe 已实现
**When** 调用 `Pipe()` 创建管道
**Then** 返回 `(readFD, writeFD)` 一对文件描述符
**And** 写入 writeFD 的数据可从 readFD 读取

**Given** 管道已创建
**When** 智能体 A 向 writeFD 写入，智能体 B 从 readFD 读取
**Then** 数据正确传递，吞吐量 ≥ 1MB/s（NFR23）

**Given** 写端关闭
**When** 读端继续 Read
**Then** 返回 EOF，不阻塞

**Given** 读端关闭
**When** 写端继续 Write
**Then** 返回 `*SyscallError`，`Code` 为 `ErrBrokenPipe`

**Given** 管道用于 Compose 编排
**When** 前置智能体完成后
**Then** 其输出通过管道自动注入下游智能体的上下文

### Story 6.3: 进程组与批量信号

As a 用户,
I want 将多个智能体分组管理，一条命令控制整组进程,
So that 我可以高效管理多智能体工作流。

**Acceptance Criteria:**

**Given** `kernel/procgroup.go` 已实现
**When** 调用 `JoinGroup(pid, groupID)`
**Then** 目标进程加入指定组

**Given** 进程已加入组
**When** 调用 `GetProcGroup(groupID)`
**Then** 返回组内所有进程的 PID 列表

**Given** 进程组存在
**When** 向组发送信号（如 Kill）
**Then** 组内所有进程收到信号
**And** 操作延迟不超过单进程的 2 倍（NFR24）

**Given** 进程退出
**When** 该进程属于某个组
**Then** 自动从组中移除

### Story 6.4: Signal 信号系统

As a 智能体,
I want 通过 Signal syscall 向其他进程发送信号（中断、暂停、恢复）,
So that 智能体之间可以协调执行节奏。

**Acceptance Criteria:**

**Given** `kernel/signal.go` 已实现
**When** 调用 `Signal(targetPID, sig)`
**Then** 目标进程的信号处理器被触发

**Given** 目标进程调用 `SigBlock(sig)`
**When** 有该类型信号到达
**Then** 信号被暂存到 pending 队列

**Given** 目标进程调用 `SigUnblock(sig)`
**When** pending 队列中有该类型信号
**Then** 立即触发信号处理器

**Given** 目标进程未注册特定信号处理器
**When** 收到信号
**Then** 执行默认行为（Kill 信号 → 终止进程，Pause → 暂停推理循环，Resume → 恢复）

### Story 6.5: 三级并发模型

As a 平台构建者,
I want 系统提供进程、线程、协程三级并发原语,
So that 我可以为不同粒度的任务选择最合适的并发模型。

**Acceptance Criteria:**

**Given** 三级并发模型已实现
**When** 创建进程级智能体（`Spawn`）
**Then** 拥有独立上下文和独立 LLM 会话
**And** 完全隔离，通过 IPC 通信

**Given** 三级并发模型已实现
**When** 创建线程级执行单元
**Then** 共享父进程的上下文空间
**And** 拥有独立执行流（goroutine）
**And** 通过共享上下文交换数据

**Given** 三级并发模型已实现
**When** 创建协程级执行单元
**Then** 轻量协作调度，yield 语义
**And** 适用于上下文内的子任务分解

**Given** ≥ 10 个并发智能体（进程级）
**When** 同时运行
**Then** 进程表操作延迟不超过单进程场景的 2 倍（NFR24）

---

## Epic 7: Compose 多智能体编排（Agent Compose）

用户通过 `crux-compose.yaml` 声明式定义多智能体工作流，Compose 引擎解析 DAG 依赖自动调度——20 行 YAML 替代 2000 行硬编码。

### Story 7.1: crux-compose.yaml 解析与 DAG 调度引擎

As a 用户,
I want 通过 YAML 文件声明式定义多智能体工作流及其依赖关系,
So that 系统自动按正确顺序调度执行。

**Acceptance Criteria:**

**Given** `compose/engine.go` 已实现
**When** 解析 `crux-compose.yaml`
**Then** 正确提取每个智能体的 `intent`、`agent` 引用、`skills` 列表和 `depends_on` 依赖
**And** 构建 DAG（有向无环图）表示依赖关系

**Given** YAML 中存在循环依赖
**When** 解析
**Then** 返回清晰的错误信息，标注循环路径

**Given** DAG 已构建
**When** 执行调度
**Then** 按拓扑顺序启动智能体
**And** 无依赖的分支自动并行化
**And** ≤ 10 个智能体的启动延迟 ≤ 2s（不含 LLM 调用，NFR21）

**Given** 智能体 B 声明 `depends_on: { A: completed }`
**When** 智能体 A 完成
**Then** 智能体 B 自动启动
**And** 智能体 A 的输出可通过管道注入 B 的上下文

**Given** crux-compose.yaml 格式示例
**When** 用户编写
**Then** 支持以下格式：
```yaml
version: "1.0"
intent: "PR 审查 + 代码分析 + 变更文档"
agents:
  reviewer:
    intent: "审查 PR 变更"
    skills: [pr-reviewer]
  analyst:
    intent: "分析代码质量"
    skills: [code-analyst]
    depends_on:
      reviewer: completed
```

### Story 7.2: crux compose up 命令

As a 用户,
I want 通过 `crux compose up` 一键启动编排定义的所有智能体,
So that 完整的多智能体工作流一条命令即可运行。

**Acceptance Criteria:**

**Given** `cmd/crux/compose.go` 中 compose up 子命令已注册
**When** 执行 `crux compose up`
**Then** 读取当前目录的 `crux-compose.yaml`
**And** 按 DAG 顺序 Spawn 所有智能体
**And** 实时输出每个智能体的启动和完成状态

**Given** 指定自定义文件
**When** 执行 `crux compose up -f my-workflow.yaml`
**Then** 使用指定文件而非默认文件

**Given** 编排中某个智能体失败
**When** 该智能体退出非零码
**Then** 依赖它的下游智能体不启动
**And** 输出明确的错误信息，标注失败的智能体和受影响的下游

**Given** 所有智能体完成
**When** 查看输出
**Then** 显示编排汇总：每个智能体的退出码、token 消耗、耗时

### Story 7.3: crux compose down 命令

As a 用户,
I want 通过 `crux compose down` 停止编排中所有智能体并释放资源,
So that 我可以清理中断的工作流。

**Acceptance Criteria:**

**Given** `cmd/crux/compose.go` 中 compose down 子命令已注册
**When** 执行 `crux compose down`
**Then** 向编排中所有运行中的智能体发送 Kill 信号
**And** 等待所有进程转为 Dead
**And** 释放所有资源（进程、上下文、文件描述符）

**Given** 部分智能体已完成，部分仍在运行
**When** 执行 `crux compose down`
**Then** 仅终止仍在运行的智能体
**And** 输出释放汇总（终止了 N 个进程，释放了 M 个上下文）

### Story 7.4: Compose 端到端验收

As a 用户,
I want 验证完整的 Compose 编排流程：定义 → 启动 → 依赖调度 → 数据传递 → 完成,
So that 确认多智能体编排系统协同工作正常。

**Acceptance Criteria:**

**Given** 编写包含 ≥ 3 个智能体的 crux-compose.yaml（有 DAG 依赖）
**When** 执行 `crux compose up`
**Then** 智能体按依赖顺序执行，无依赖分支并行
**And** 前置智能体的输出正确传递给下游
**And** 3 智能体编排从 YAML 到全部完成，总耗时 ≤ 90 秒

**Given** `crux top` 同时运行
**When** 编排执行中
**Then** 实时看到所有智能体的树状关系和状态

---

## Epic 8: Skill 包管理与生态（Skill Package Management）

用户通过 CLI 命令管理社区 Skill：搜索、安装、更新、列出——构建 Crux 的能力生态系统。

### Story 8.1: skill install 安装

As a 用户,
I want 通过 `skill install <name>` 从社区仓库安装 Skill,
So that 我可以快速获取社区共享的能力模块。

**Acceptance Criteria:**

**Given** `skillpkg/client.go` 已实现
**When** 调用社区仓库 API
**Then** 支持 Skill 下载、版本解析、完整性验证

**Given** `cmd/crux/skill.go` 中 install 子命令已注册
**When** 执行 `skill install code-analysis`
**Then** 从社区仓库下载 Skill 包
**And** 安装到本地 `lib/skills/code-analysis/` 目录
**And** 更新本地 Skill 注册表

**Given** 批量安装
**When** 执行 `skill install pr-reviewer code-analyst tech-writer`
**Then** 依次安装三个 Skill，每个显示安装进度

**Given** Skill 已安装
**When** 再次执行 `skill install code-analysis`
**Then** 提示已安装且显示当前版本，询问是否覆盖

**Given** 安装的 Skill 包含有效的 SKILL.md
**When** Agent 引用该 Skill
**Then** 无需任何修改即可使用（NFR30）

### Story 8.2: skill search 搜索

As a 用户,
I want 通过 `skill search <keyword>` 搜索社区仓库中可用的 Skill,
So that 我可以发现适合我需求的能力模块。

**Acceptance Criteria:**

**Given** `cmd/crux/skill.go` 中 search 子命令已注册
**When** 执行 `skill search code`
**Then** 返回匹配的 Skill 列表
**And** 每条结果包含：名称、描述、版本、下载量

**Given** 搜索结果为空
**When** 无匹配关键词
**Then** 输出 `No skills found for "keyword".` + 建议（检查拼写或浏览全部 Skill）

**Given** 使用 `--json` flag
**When** 搜索
**Then** 输出 JSON 数组，字段 snake_case

### Story 8.3: skill update 更新

As a 用户,
I want 通过 `skill update [name]` 更新已安装的 Skill,
So that 我始终使用最新兼容版本的能力模块。

**Acceptance Criteria:**

**Given** `cmd/crux/skill.go` 中 update 子命令已注册
**When** 执行 `skill update code-analysis`
**Then** 检查社区仓库中的最新兼容版本
**And** 如果有更新，下载并替换本地版本
**And** 更新本地注册表

**Given** 不指定名称
**When** 执行 `skill update`
**Then** 检查所有已安装 Skill 的更新
**And** 显示可更新列表，确认后批量更新

**Given** 已是最新版本
**When** 执行更新
**Then** 输出 `code-analysis is already up to date (v1.2.0).`

### Story 8.4: 本地 Skill 注册表与 skill list

As a 用户,
I want 通过 `skill list` 查看所有已安装的 Skill,
So that 我了解本地可用的能力模块。

**Acceptance Criteria:**

**Given** 本地 Skill 注册表已维护
**When** 执行 `skill list`
**Then** 输出表格：NAME、VERSION、PATH、DESCRIPTION
**And** 包含系统自带 Skill 和社区安装的 Skill

**Given** 无已安装 Skill（除系统自带）
**When** 执行 `skill list`
**Then** 显示系统自带 Skill + `Tip: skill search <keyword> 发现更多 Skill`

**Given** 使用 `--json` flag
**When** 列出
**Then** 输出 JSON 数组

---

## Epic 9: MCP 服务集成（MCP Integration）

系统通过 VFS 挂载 MCP 服务器，智能体通过标准文件操作访问外部工具——完成 Agent → Skill → MCP → Device 四层能力栈。

### Story 9.1: Mount/Unmount syscall

As a 平台构建者,
I want 通过 Mount/Unmount syscall 在 VFS 中挂载和卸载 MCP 服务器,
So that 外部服务可以作为文件路径被智能体访问。

**Acceptance Criteria:**

**Given** `vfs/mcp.go` 已实现
**When** 调用 `Mount("/mnt/mcp/github", mcpConfig)`
**Then** 在 `/mnt/mcp/github/` 路径下挂载 MCP 服务器
**And** 挂载延迟 ≤ 500ms（NFR25）

**Given** MCP 服务器已挂载
**When** 调用 `Unmount("/mnt/mcp/github")`
**Then** 卸载服务器，关闭连接，清理 VFS 路径

**Given** MCP 服务器异常退出
**When** 智能体访问 `/mnt/mcp/github/` 下的路径
**Then** 3 秒内返回 `ErrServiceUnavailable` 错误（NFR26）
**And** 不影响内核稳定性

**Given** 已挂载路径
**When** 重复 Mount
**Then** 返回 `*SyscallError`（路径已占用）

### Story 9.2: agent.yaml mcp 字段与自动挂载

As a 用户,
I want Agent 的 agent.yaml 中通过 `mcp` 字段引用 MCP 服务器，Spawn 时自动挂载,
So that 我不需要手动管理 MCP 服务器的生命周期。

**Acceptance Criteria:**

**Given** agent.yaml 包含 `mcp: ["github", "slack"]`
**When** Spawn 该 Agent 的智能体
**Then** 系统自动 Mount 引用的 MCP 服务器到 `/mnt/mcp/{name}/`
**And** 进程退出时自动 Unmount

**Given** `drivers/mcp/mcp.go` 已实现
**When** MCP 服务器启动
**Then** 管理 MCP 服务器进程生命周期（启动、健康检查、停止）

**Given** MCP 配置缺失或无效
**When** Spawn 时引用该 MCP
**Then** 返回清晰错误信息，标注具体配置问题

### Story 9.3: VFS 路径暴露 MCP 工具

As a 智能体,
I want 通过标准 VFS Open/Read/Write 访问 MCP 服务器提供的工具和资源,
So that 我不需要知道 MCP 协议细节，只需操作文件。

**Acceptance Criteria:**

**Given** MCP 服务器已挂载（如 `/mnt/mcp/github/`）
**When** 调用 `Open("/mnt/mcp/github/tools/create-issue")`
**Then** 返回 VFSFile 封装的 MCP 工具接口

**Given** MCP 工具已打开
**When** 调用 `Write(fd, toolParams)` + `Read(fd, maxLen)`
**Then** 调用 MCP 服务器执行工具并返回结果

**Given** MCP 兼容性
**When** 接入符合 MCP 标准的第三方服务器
**Then** 无需 Crux 侧代码修改即可挂载和使用（NFR27）

### Story 9.4: 四层能力栈端到端验证

As a 用户,
I want 验证 Agent → Skill → MCP → Device 四层能力栈端到端工作,
So that 确认各层职责分离且协同正确。

**Acceptance Criteria:**

**Given** 配置了包含 Skill 和 MCP 引用的 Agent
**When** Spawn 并执行任务
**Then** Agent 层提供身份和策略
**And** Skill 层提供程序性知识和工具权限
**And** MCP 层提供外部服务集成
**And** Device 层提供原生 I/O（`/dev/`）

**Given** `crux astrace` 追踪该进程
**When** 查看 syscall 链路
**Then** 可以清晰看到四层的调用边界和数据流向（FR57）

---

## Epic 10: 监控、Supervisor 与运维（Monitoring, Supervisor & Operations）

`crux top` 实时面板 + `crux log` 分类日志 + Supervisor 容错树 + init 引导——生产级运维能力全集。

### Story 10.1: crux top 实时监控 TUI

As a 用户,
I want 通过 `crux top` 实时查看所有智能体的树状关系、状态和 token 消耗,
So that 我随时掌握系统全局运行态。

**Acceptance Criteria:**

**Given** `cmd/crux/top.go` 已实现（bubbletea TUI）
**When** 执行 `crux top`
**Then** 全屏显示实时监控面板
**And** 上方汇总区：活跃进程数、总 token 消耗、系统运行时间
**And** 下方进程列表：PID、PPID（树状缩进）、STATE、AGENT、TOKENS、ELAPSED

**Given** TUI 运行中
**When** 进程状态变化
**Then** 刷新间隔 ≤ 500ms（NFR28）
**And** 单核 CPU 占用 ≤ 5%（10 个并发进程场景）

**Given** 用户在 TUI 中选中进程
**When** 按 `k` 键
**Then** Kill 选中的进程（FR62）

**Given** 用户在 TUI 中选中进程
**When** 按 `Enter` 键
**Then** 显示进程详情（intent、skills、context 摘要）（FR62）

**Given** 按 `q` 键
**When** 退出 TUI
**Then** 恢复终端状态，不影响运行中的进程

### Story 10.2: crux log 分类推理日志

As a 用户,
I want 通过 `crux log <pid>` 查看智能体的推理日志，按类别分类显示,
So that 我无需深入内核就能排查问题。

**Acceptance Criteria:**

**Given** `cmd/crux/log.go` 已实现
**When** 执行 `crux log 5`
**Then** 输出 PID 5 的推理日志
**And** 按 `[think]`（推理过程）、`[tool]`（工具调用）、`[output]`（最终输出）三段式分类显示（FR60）

**Given** 使用过滤
**When** 执行 `crux log 5 --filter tool`
**Then** 仅显示 `[tool]` 类别的日志条目

**Given** 日志输出
**When** 从推理事件发生到终端显示
**Then** 延迟 ≤ 200ms（NFR29）

**Given** PID 不存在
**When** 执行 `crux log 999`
**Then** 输出 `✗ PID 999: process not found` + 建议

### Story 10.3: Token 预算管理

As a 用户,
I want 为智能体设置 token 预算上限，超限时系统自动终止推理,
So that 我可以控制 LLM 调用的成本。

**Acceptance Criteria:**

**Given** `context/budget.go` 已实现
**When** Agent 的 agent.yaml 设置 `context_budget: 5000`
**Then** 系统在智能体消耗达到 5000 token 时终止推理（FR61）
**And** 进程转 Zombie，ExitStatus 记录原因为 `budget_exceeded`

**Given** Compose 中覆盖预算
**When** compose.yaml 中为特定智能体设置 `context_budget: 10000`
**Then** 使用 compose 中的值覆盖 agent.yaml 中的默认值

**Given** 预算即将耗尽（剩余 < 10%）
**When** 推理循环继续
**Then** 在 crux top 中显示黄色警告标记

### Story 10.4: Supervisor 树与重启策略

As a 平台构建者,
I want 系统提供 Supervisor 树管理子智能体，自动重启异常退出的子进程,
So that 多智能体系统具备容错能力。

**Acceptance Criteria:**

**Given** `kernel/supervisor.go` 已实现
**When** 创建 Supervisor 进程
**Then** Supervisor 监控其子智能体的健康状态

**Given** 子智能体异常退出
**When** Supervisor 检测到
**Then** 在 5 秒内按配置的策略自动重启（FR63）

**Given** 重启策略为 `one_for_one`
**When** 子进程 B 崩溃
**Then** 仅重启 B

**Given** 重启策略为 `one_for_all`
**When** 子进程 B 崩溃
**Then** 重启所有子进程

**Given** 重启策略为 `rest_for_one`
**When** 子进程 B 崩溃（B 是第 2 个启动的）
**Then** 重启 B 及其之后按启动顺序的所有子进程（FR64）

**Given** 子进程短时间内反复崩溃
**When** 超过重启频率阈值
**Then** Supervisor 自身退出，上报错误（避免重启风暴）

### Story 10.5: init 引导序列

As a 系统,
I want daemon 启动时按配置初始化系统级服务和 Supervisor 树,
So that 系统启动后所有基础设施就位。

**Acceptance Criteria:**

**Given** `kernel/init.go` 已实现
**When** daemon 启动
**Then** 按配置文件初始化系统级服务（FR65）：
**And** 日志聚合服务启动
**And** Skill 注册表初始化（扫描 `lib/skills/`）
**And** MCP 服务管理器初始化
**And** Supervisor 树按配置构建

**Given** 初始化过程中某服务启动失败
**When** 为必须服务
**Then** daemon 启动失败，输出具体错误信息和恢复建议

**Given** 初始化过程中某服务启动失败
**When** 为可选服务
**Then** 记录警告，继续启动其余服务

---

## Epic 11: AgentShell 高级语法（AgentShell Advanced Syntax）

从单命令到 Shell 脚本——管道组合、变量环境、流程控制，让智能体编排像写 bash 一样自然。

### Story 11.1: 管道语法

As a 用户,
I want 在 AgentShell 中通过管道语法组合智能体执行链,
So that 前一个智能体的输出自动成为后一个的输入。

**Acceptance Criteria:**

**Given** `shell/pipe.go` 已实现
**When** 执行 `spawn "分析代码" | spawn "写文档"`
**Then** 系统解析管道语法
**And** Spawn 第一个智能体执行"分析代码"
**And** 其输出通过 Pipe 自动注入第二个智能体"写文档"的上下文（FR66）

**Given** 管道链包含 ≥ 3 个智能体
**When** 执行 `spawn "A" | spawn "B" | spawn "C"`
**Then** 按顺序链式传递，A→B→C

**Given** 管道中某个智能体失败
**When** 退出非零码
**Then** 下游智能体不启动，管道中断并报告错误位置

### Story 11.2: 变量与环境传递

As a 用户,
I want 在 AgentShell 中定义变量和传递环境给智能体,
So that 智能体可以引用动态参数。

**Acceptance Criteria:**

**Given** `shell/script.go` 已实现
**When** 执行 `export TARGET=./src/auth.go`
**Then** 变量 `TARGET` 存储在 shell 环境中

**Given** 变量已定义
**When** Spawn 的智能体 intent 中引用 `$TARGET`
**Then** 变量值被替换后注入 intent（FR67）

**Given** 多个变量
**When** 在脚本中使用
**Then** 支持标准 `$VAR` 和 `${VAR}` 引用语法

### Story 11.3: 最小控制结构

As a 用户,
I want 在 AgentShell 中使用 if-else 和 on-error 编排执行流程,
So that 智能体工作流可以有条件分支和错误处理。

**Acceptance Criteria:**

**Given** `shell/script.go` 中控制结构已实现
**When** 执行多行脚本：
```
result = spawn "分析代码"
if $result.exitcode == 0
  spawn "生成报告"
else
  spawn "记录失败原因"
end
```
**Then** 按条件分支正确执行（FR68）

**Given** 使用 on-error
**When** 执行：
```
spawn "危险操作" on-error spawn "回滚"
```
**Then** "危险操作"失败时自动执行"回滚"

**Given** 嵌套控制结构
**When** 超过 1 层嵌套
**Then** 正确执行（完整脚本语言能力推迟至 Phase 3）

---

## Epic 12: Phase 2 文档（Phase 2 Documentation）

三个核心教程覆盖开发者最关键的场景，四模块架构文档为贡献者提供深入理解——生态建设的文档基础。

### Story 12.1: 教程文档

As a 开发者,
I want 阅读教程文档学会编写 Skill、调试 bug 和组合多智能体工作流,
So that 我可以在 Crux 上构建自己的应用。

**Acceptance Criteria:**

**Given** 教程文档已编写
**When** 阅读"编写第一个 Skill"教程
**Then** 包含从创建 SKILL.md 到 Agent 引用到 spawn 执行的完整流程
**And** 包含完整可运行示例（FR69）

**Given** 教程文档已编写
**When** 阅读"调试第一个 bug"教程
**Then** 包含故意引入 bug → astrace 定位 → 修复 → 验证的完整流程
**And** 包含完整可运行示例

**Given** 教程文档已编写
**When** 阅读"组合多智能体工作流"教程
**Then** 包含编写 crux-compose.yaml → compose up → crux top 监控 → 查看结果的完整流程
**And** 包含完整可运行示例

### Story 12.2: 架构文档

As a 贡献者,
I want 阅读架构文档理解 Crux 的内部设计,
So that 我可以参与内核开发和 Skill 生态贡献。

**Acceptance Criteria:**

**Given** 架构文档已编写
**When** 阅读微内核设计章节
**Then** 包含 Kernel 接口组合设计、分类子接口职责、扩展路径的设计决策和数据流说明

**Given** 架构文档已编写
**When** 阅读进程模型章节
**Then** 包含 Process 结构体设计、状态机转移规则、PID 分配策略、goroutine 生命周期管理（FR70）

**Given** 架构文档已编写
**When** 阅读驱动层章节
**Then** 包含 LLMDriver 接口、VFS 设备注册、MCP 挂载机制

**Given** 架构文档已编写
**When** 阅读上下文管理章节
**Then** 包含上下文分配/读写/释放、prompt 组装、token 预算管理
