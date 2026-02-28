# Functional Requirements

## Agent Lifecycle Management（智能体生命周期管理）

- **FR1:** 用户可以通过自然语言意图创建（spawn）一个新的智能体进程
- **FR2:** 系统可以管理智能体进程的完整生命周期状态（Created → Running → Zombie → Dead）
- **FR3:** 用户可以终止（kill）一个正在运行的智能体进程
- **FR4:** 用户可以等待（wait）一个智能体进程完成并获取退出状态
- **FR5:** 系统可以在父进程退出后将孤儿进程重新挂载到 init（PID=1）
- **FR6:** 系统可以回收已完成的 Zombie 进程并释放其资源
- **FR7:** 用户可以查看所有活跃进程的列表及其状态（ps）

## Agent Reasoning（智能体推理）

- **FR8:** 系统可以驱动智能体执行推理循环（reasonStep），在 LLM 调用与工具执行之间交替直到任务完成
- **FR9:** 系统可以通过 LLM 驱动层以非交互模式调用 LLM 并获取结构化响应
- **FR10:** 系统可以解析 LLM 响应中的 action 类型（text 最终输出 / tool_call 工具调用 / spawn 创建子进程）
- **FR11:** 系统可以在 LLM 调用超时或失败时正确将进程状态转为 Zombie 并上报错误信息
- **FR12:** 系统可以将工具执行结果追加到智能体上下文中供后续推理使用

## File System & Resource Access（文件系统与资源访问）

- **FR13:** 系统可以提供统一的虚拟文件系统（VFS）接口（Open/Read/Write/Close/Stat）
- **FR14:** 系统可以通过 `/proc/{pid}/` 动态暴露每个智能体的运行时状态（status、intent、context）
- **FR15:** 系统可以通过 `/dev/` 路径注册和路由设备驱动（LLM、Shell、文件系统）
- **FR16:** 智能体可以通过 `/dev/fs` 读取宿主文件系统上的文件
- **FR17:** 智能体可以通过 `/dev/llm/claude` 访问 LLM 推理能力
- **FR18:** 智能体可以通过 `/dev/shell` 执行宿主系统的 shell 命令

## Context Management（上下文管理）

- **FR19:** 系统可以为每个智能体分配独立的上下文空间（ctx_alloc）
- **FR20:** 系统可以读取和写入智能体上下文内容（ctx_read / ctx_write）
- **FR21:** 系统可以将上下文内容组装为完整的 LLM prompt（包含 system prompt + 对话历史 + 工具结果）
- **FR22:** 系统可以在进程退出后释放其上下文空间（ctx_free）

## Agent Management（智能体定义管理）

- **FR23:** 系统可以从 `agent.yaml` 读取 Agent 的元信息（名称、描述、模型偏好、上下文预算、Skill 引用列表）
- **FR24:** 系统可以从 Agent 的 `instructions.md` 读取角色定义并注入智能体的 system prompt
- **FR25:** 用户可以在 spawn 时通过 `--agent=<name>` 指定 Agent 定义

## Skill Management（能力模块管理，遵循 Agent Skills 行业标准）

- **FR25a:** 系统可以从 `SKILL.md` 解析 Skill 元信息（name、description、allowed-tools），格式遵循 Agent Skills 开放标准（agentskills.io）
- **FR25b:** 系统可以对 Skill 进行渐进式加载——启动时仅加载 frontmatter（≤ 100 tokens/skill），激活时加载完整 SKILL.md body（≤ 5000 tokens），执行时按需加载 scripts/references/assets
- **FR26:** Agent 引用的所有 Skill 的 `allowed-tools` 聚合后映射为智能体的可用 `/dev/` 设备权限白名单
- **FR27:** 系统交付参考 Agent（code-analyst）+ 参考 Skill（code-analysis），能够分析代码并识别至少 1 个可验证的真实代码问题（与 Success Criteria 中自举验证标准对齐）

## Debugging & Observability（调试与可观测性）

- **FR28:** 用户可以通过 `astrace` 实时追踪指定智能体的所有 syscall 调用
- **FR29:** 系统可以在 astrace 输出中展示每个 syscall 的名称、参数、返回值和耗时
- **FR30:** 系统可以记录 syscall 调用数据（DebugRecord）供 astrace 消费
- **FR31:** 用户可以通过 astrace 输出定位到产生错误结果的具体 syscall 调用记录
- **FR32:** 系统在智能体完成时输出汇总信息（退出码、token 消耗、总耗时）

## Command Line Interface（命令行接口）

- **FR33:** 用户可以通过 `crux "意图"` 单命令启动一个智能体
- **FR34:** 用户可以通过 `crux astrace <pid>` 追踪指定进程的 syscall
- **FR35:** 用户可以通过 `crux ps` 查看所有进程状态
- **FR36:** 系统可以在 CLI 中输出结构化错误信息，包含设备路径、错误码和错误原因
- **FR37:** 系统可以通过 `go install` 一条命令完成安装，单二进制，零额外依赖（需预装 Claude Code CLI）

## Documentation（文档）

- **FR38:** 系统可以提供概念文档，覆盖进程、VFS、Skill、syscall 四个核心概念，每个概念含定义和至少一个示例
- **FR39:** 系统可以提供快速上手指南，引导用户从安装到跑通第一个 demo（目标 ≤ 15 分钟）
- **FR40:** 系统可以提供参考手册，覆盖 syscall 列表、VFS 路径规范、agent.yaml 字段 + SKILL.md frontmatter 字段、CLI 命令

## IPC & Multi-Agent Communication（进程间通信与多智能体协作，Phase 2）

- **FR41:** 智能体可以通过 Send/Recv syscall 向指定 PID 的进程发送消息和接收消息
- **FR42:** 系统可以通过 Pipe syscall 创建管道，将一个智能体的输出连接为另一个智能体的输入
- **FR43:** 系统可以管理进程组（Process Group），用户可以通过 JoinGroup/GetProcGroup 管理分组，对组内进程批量发送信号
- **FR44:** 系统可以提供三级智能体并发模型：进程（独立上下文和 LLM 会话）、线程（共享上下文的并行执行单元）、协程（轻量协作调度单元）
- **FR45:** 智能体可以通过 Signal syscall 向其他进程发送信号（中断、暂停、恢复），接收方可以通过 SigBlock/SigUnblock 控制信号处理

## Agent Compose（多智能体编排，Phase 2）

- **FR46:** 用户可以通过 `crux-compose.yaml` 声明式定义多智能体工作流，包含每个智能体的 intent、agent 引用、skills 列表和依赖关系
- **FR47:** Compose 引擎可以解析智能体之间的 `depends_on` 依赖关系，按 DAG 拓扑顺序调度执行，自动并行化无依赖的分支
- **FR48:** 用户可以通过 `crux compose up` 一键启动编排中定义的所有智能体
- **FR49:** 用户可以通过 `crux compose down` 停止编排中所有智能体并释放资源（进程、上下文、文件描述符）

## Skill Package Management（Skill 包管理，Phase 2）

- **FR50:** 用户可以通过 `skill install <name>` 从社区仓库下载并安装 Skill 到本地 `lib/skills/` 目录
- **FR51:** 用户可以通过 `skill search <keyword>` 搜索社区仓库中可用的 Skill，返回名称、描述、版本和下载量
- **FR52:** 用户可以通过 `skill update [name]` 更新已安装 Skill 到最新兼容版本
- **FR53:** 系统维护本地 Skill 注册表，记录已安装 Skill 的元信息、版本和路径，用户可通过 `skill list` 查看

## MCP Integration（MCP 服务集成，Phase 2）

- **FR54:** 系统可以通过 Mount/Unmount syscall 在 `/mnt/mcp/` 路径下挂载和卸载 MCP 服务器
- **FR55:** Agent 的 `agent.yaml` 可以通过 `mcp` 字段引用 MCP 服务器名称列表，系统在 Spawn 时自动挂载对应服务
- **FR56:** 系统可以将 MCP 服务器提供的工具和资源通过 VFS 路径暴露给智能体，智能体通过标准 Open/Read/Write 访问
- **FR57:** 系统可以端到端运行四层能力栈：Agent（身份+策略）→ Skill（程序性知识+工具权限）→ MCP（外部服务集成）→ Device（原生 I/O），用户可以通过 astrace 验证各层调用链路的职责分离

## Monitoring & Observability（监控与可观测性，Phase 2）

- **FR58:** 用户可以通过 `crux top` 实时查看所有运行中智能体的树状关系、状态、token 消耗和执行进度
- **FR59:** 用户可以通过 `crux log <pid>` 查看指定智能体的推理日志
- **FR60:** 系统可以将 `crux log` 输出按 `[think]`/`[tool]`/`[output]` 三段式分类显示，支持 `--filter <category>` 按类别过滤
- **FR61:** 用户可以为智能体设置 token 预算上限（通过 agent.yaml `context_budget` 或 compose 中覆盖），系统在达到上限时终止推理并上报原因
- **FR62:** 用户可以在 `crux top` 中通过交互式操作选中进程并执行 kill 或查看详情

## Supervisor & System Bootstrap（容错与系统引导，Phase 2）

- **FR63:** 系统可以提供 Supervisor 树管理模式，Supervisor 进程监控子智能体的健康状态并在异常退出时自动重启
- **FR64:** Supervisor 可以使用三种重启策略：one_for_one（仅重启崩溃的子进程）、one_for_all（全部子进程重启）、rest_for_one（崩溃进程之后按启动顺序的所有子进程重启）
- **FR65:** 系统可以执行 init 引导序列，daemon 启动时按配置文件初始化系统级服务（日志聚合、Skill 注册表、MCP 服务管理）和 Supervisor 树

## AgentShell Advanced Syntax（AgentShell 高级语法，Phase 2）

- **FR66:** 用户可以在 AgentShell 中通过管道语法组合智能体执行链（`spawn "分析" | spawn "写文档"`），前一个智能体的输出自动注入后一个的上下文
- **FR67:** 用户可以在 AgentShell 中定义变量（`export KEY=VALUE`）和传递环境，spawn 的智能体可以在 intent 和配置中引用环境变量
- **FR68:** 用户可以在 AgentShell 中编写多行命令序列，使用最小控制结构集（`if-else` 条件判断 + `on-error` 错误处理）编排智能体执行流程（完整脚本语言能力推迟至 Phase 3）

## Documentation Phase 2（Phase 2 文档）

- **FR69:** 系统可以提供教程文档，覆盖"编写第一个 Skill"、"调试第一个 bug"、"组合多智能体工作流"三个核心场景，每个教程含完整可运行示例
- **FR70:** 系统可以提供架构文档，覆盖微内核设计、进程模型、驱动层架构、上下文管理四个核心模块，每个模块含设计决策和数据流说明
