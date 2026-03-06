# Requirements Inventory

## Functional Requirements

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

- FR28: 用户可以通过 `strace` 实时追踪指定智能体的所有 syscall 调用
- FR29: strace 输出包含每个 syscall 的名称、参数、返回值和耗时
- FR30: 系统可以记录 syscall 调用数据（DebugRecord）供 strace 消费
- FR31: 用户可以通过 strace 输出定位到产生错误结果的具体 syscall 调用记录
- FR32: 系统在智能体完成时输出汇总信息（退出码、token 消耗、总耗时）

**命令行接口（FR33-FR37）**

- FR33: 用户可以通过 `rnix "意图"` 单命令启动一个智能体
- FR34: 用户可以通过 `rnix strace <pid>` 追踪指定进程的 syscall
- FR35: 用户可以通过 `rnix ps` 查看所有进程状态
- FR36: CLI 提供清晰的错误信息，包含设备路径和错误原因
- FR37: 系统可以通过 `go install` 一条命令完成安装，单二进制，零额外依赖（需预装 Claude Code CLI）

**文档（FR38-FR40）**

- FR38: 系统可以提供概念文档，覆盖进程、VFS、Skill、syscall 四个核心概念，每个概念含定义和至少一个示例
- FR39: 系统可以提供快速上手指南，引导用户从安装到跑通第一个 demo（目标 ≤ 15 分钟）
- FR40: 系统可以提供参考手册，覆盖 syscall 列表、VFS 路径规范、agent.yaml / SKILL.md 字段、CLI 命令

## NonFunctional Requirements

**性能（NFR1-5）**

- NFR1: 单智能体 spawn→完成（含 LLM 调用），端到端延迟 ≤ 30 秒（简单任务如单文件分析）
- NFR2: `rnix ps` 响应时间 ≤ 100ms（本地进程表查询，不涉及 LLM）
- NFR3: `strace` 输出延迟 ≤ 500ms（从 syscall 发生到终端显示）
- NFR4: VFS 本地文件读取（`/dev/fs`）额外延迟 < 10ms，不超过直接文件 I/O 延迟的 2 倍
- NFR5: 上下文组装（ctx → prompt）时间 ≤ 1 秒（不含 LLM 调用本身）

**可靠性（NFR6-10）**

- NFR6: 连续 20 次 spawn→完成路径，成功率 ≥ 95%
- NFR7: LLM API 超时/错误时，进程在 5 秒内正确转入 Zombie 状态，不卡死在 Running
- NFR8: 进程退出后，goroutine 和 context 内存在 10 秒内释放，无泄漏
- NFR9: 内核进程表在任意进程异常退出后保持一致性（无悬挂 PID、无状态不一致）
- NFR10: CLI 进程（rnix 二进制本身）在智能体异常退出时不崩溃

**集成（NFR11-14）**

- NFR11: LLM 驱动层调用时，正确传递 system prompt、工具声明、模型选择、输出格式等参数
- NFR12: LLM 驱动层支持流式结构化输出模式（stream-json），用于 strace 实时数据采集
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

## Additional Requirements

**来自架构文档的技术需求：**

- 项目初始化（Starter）：领域驱动 OS 隐喻结构（方案 C），`go mod init github.com/rnixai/rnix`，这是 Epic 1 Story 1 的基础
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
- 依赖注入点：`cmd/rnix/main.go` 是唯一组装点
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
- 环境变量：`NO_COLOR` 支持（颜色完全去除）、`RNIX_ASCII=1` 支持（Unicode 降级为 ASCII）
- 三段式错误结构：`✗ {设备路径}: {错误原因}` → `{影响}` → `建议: {恢复命令}`
- 信号处理：SIGINT 首次优雅中断（goroutine 清理），二次（2 秒内）强制退出
- 终端宽度自适应：< 60/60-79/80-119（目标）/120+ 列四档，表格列按优先级取舍

## FR Coverage Map

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
- FR28: Epic 3 — strace 实时追踪所有 syscall
- FR29: Epic 3 — strace 输出含名称、参数、返回值、耗时
- FR30: Epic 3 — 记录 syscall 调用数据（DebugRecord）
- FR31: Epic 3 — 通过 strace 定位具体错误 syscall
- FR32: Epic 1 — 智能体完成时输出汇总信息
- FR33: Epic 1 — `rnix "意图"` 单命令启动智能体
- FR34: Epic 3 — `rnix strace <pid>` 追踪命令
- FR35: Epic 4 — `rnix ps` 查看进程状态
- FR36: Epic 1 — CLI 提供清晰错误信息
- FR37: Epic 1 — `go install` 安装，单二进制零依赖
- FR38: Epic 5 — 概念文档
- FR39: Epic 5 — 快速上手指南
- FR40: Epic 5 — 参考手册
