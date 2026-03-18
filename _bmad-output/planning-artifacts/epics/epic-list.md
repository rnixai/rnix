# Epic List

## Epic 1: 第一个智能体运行（First Agent Runs）
用户安装 Rnix 后，输入 `rnix "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。包含项目初始化、内核核心（进程模型 + Spawn + reasonStep）、VFS 框架、LLM 驱动（Claude Code CLI）、上下文管理、CLI 入口和基础 UI 组件。
**FRs covered:** FR1, FR2, FR8, FR9, FR10, FR11, FR13, FR15, FR17, FR19, FR20, FR21, FR32, FR33, FR36, FR37

## Epic 2: Agent 能力与文件访问（Agent Skills & File Access）
用户可以通过 Agent 定义赋予智能体专业能力（如代码分析），Agent 引用的 Skill 决定智能体可访问的工具和知识——从"能说话"升级到"能干活"。包含 Agent 加载器、Skill 加载器（SKILL.md，Agent Skills 行业标准）、宿主 FS 驱动、Shell 驱动、allowed-tools 聚合白名单和 code-analyst 参考 Agent + code-analysis 参考 Skill。
**FRs covered:** FR12, FR16, FR18, FR23, FR24, FR25, FR25a, FR25b, FR26, FR27

## Epic 3: 调试追踪（Debug Tracing — strace）
当智能体输出不符合预期时，用户运行 `rnix strace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Rnix 的差异化核心体验。包含 SyscallEvent 记录、DebugChan 事件管道、strace 命令、Trace Line UI、配置解析来源追踪（ConfigResolve 事件）和推理步骤逐步输出。
**FRs covered:** FR28, FR29, FR30, FR31, FR34

## Epic 4: 进程管理与可靠性（Process Management & Reliability）
用户可以查看所有进程状态（`rnix ps`）、终止进程（`rnix kill`）、等待进程完成。系统自动回收 Zombie 进程、处理孤儿进程、暴露 `/proc` 运行时状态——生产级可靠性。
**FRs covered:** FR3, FR4, FR5, FR6, FR7, FR14, FR22, FR35

## Epic 5: 文档体系（Documentation）
新用户可以通过概念文档理解 Rnix 的 OS 范式，通过快速上手指南在 15 分钟内跑通 demo，通过参考手册查阅所有 syscall、VFS 路径和 CLI 命令。
**FRs covered:** FR38, FR39, FR40

---

## Phase 2 Epics

## Epic 6: IPC 跨进程通信（Inter-Process Communication）
智能体之间可以通过 Send/Recv 发送消息、通过 Pipe 连接输出与输入、通过进程组批量操作、通过 Signal 控制执行——多智能体协作的通信基础。
**FRs covered:** FR41, FR42, FR43, FR44, FR45
**NFRs:** NFR22 (IPC ≤50ms), NFR23 (Pipe ≥1MB/s), NFR24 (≥10 并发进程)
**Dependencies:** Phase 1 完成

## Epic 7: Compose 多智能体编排（Agent Compose）
用户通过 `rnix-compose.yaml` 声明式定义多智能体工作流，`rnix compose up` 一键启动，引擎按 DAG 依赖拓扑自动调度并行——林薇旅程的核心体验。
**FRs covered:** FR46, FR47, FR48, FR49
**NFRs:** NFR21 (≤10 个智能体启动 ≤2s)
**Dependencies:** Epic 6（IPC 管道用于智能体间数据传递）

## Epic 8: Skill 包管理与生态（Skill Package Management）
用户通过 `skill install/search/update/list` 管理社区 Skill，安装即可用，零修改引用——生态系统的基石。客户端通过 HTTP API 与社区注册中心（`registry.rnix.ai`）交互，注册中心服务端部署为独立运维任务。
**FRs covered:** FR50, FR51, FR52, FR53
**NFRs:** NFR30 (安装即可用)
**Infra prerequisite:** 社区注册中心服务端（`/index.yaml`, `/packages/{name}/latest.yaml`, `*.tar.gz`）

## Epic 9: MCP 服务集成（MCP Integration）
系统通过 Mount/Unmount 在 `/mnt/mcp/` 挂载 MCP 服务器，智能体通过标准 VFS 访问外部工具——完成 Agent → Skill → MCP → Device 四层能力栈。
**FRs covered:** FR54, FR55, FR56, FR57
**NFRs:** NFR25 (挂载 ≤500ms), NFR26 (异常不影响内核), NFR27 (MCP 标准兼容)
**Dependencies:** Epic 6（VFS 扩展）

## Epic 10: 监控与可观测性（Monitoring & Observability）
`rnix top` 实时监控面板 + `rnix log` 分类日志 + token 预算管理——生产级可观测能力。
**FRs covered:** FR58, FR59, FR60, FR61, FR62
**NFRs:** NFR28 (top 刷新 ≤500ms), NFR29 (log 延迟 ≤200ms)

## Epic 10b: Supervisor 与系统引导（Supervisor & System Bootstrap）
Supervisor 容错树自动管理子智能体生命周期 + init 引导序列初始化系统级服务——多智能体系统的可靠性基础。
**FRs covered:** FR63, FR64, FR65
**Dependencies:** Epic 6（进程组用于 Supervisor 树）

## Epic 11: AgentShell 高级语法（AgentShell Advanced Syntax）
管道组合 `spawn "A" | spawn "B"`、变量环境 `export KEY=VALUE`、最小控制结构 `if-else` + `on-error`——从单命令到脚本编排。
**FRs covered:** FR66, FR67, FR68
**Dependencies:** Epic 6（IPC Pipe）

## Epic 12: Phase 2 文档（Phase 2 Documentation）
三个核心教程（编写 Skill、调试 bug、多智能体工作流）+ 四模块架构文档（微内核、进程模型、驱动层、上下文管理）。
**FRs covered:** FR69, FR70
**Dependencies:** Epic 7-11 完成后编写

---

## Phase 3 Epics

## Epic 13: 交互式智能体调试（Interactive Agent Debugging — gdb）
用户可以附着到运行中的智能体，设置断点（syscall/推理/质量/预算四种类型）、单步执行、检查和热修改运行时参数，实现类 GDB 的交互式调试体验。
**FRs covered:** FR71, FR72, FR72a, FR73, FR74, FR75
**NFRs:** NFR31

## Epic 14: 时间旅行调试（Time Travel Debugging）
用户可以录制智能体的完整执行历史并持久化，回放和反向追踪执行轨迹，查看任意时间点的上下文 diff，在历史分叉点探索替代执行路径。
**FRs covered:** FR76, FR76a, FR77, FR78, FR79
**NFRs:** NFR32
**Dependencies:** Epic 13（DebugRecord 录制基础）

## Epic 15: 分布式追踪与上下文分析（Distributed Tracing & Context Analysis）
用户可以追踪跨多智能体系统的完整因果链，通过 blame 定位性能瓶颈和错误根因，分析每个智能体的上下文使用效率（活跃/温/冷/泄漏分类），识别最大消费者并获得优化建议。
**FRs covered:** FR80, FR81, FR82, FR83, FR84, FR85, FR86
**NFRs:** NFR33, NFR34
**Dependencies:** Phase 2 Compose + IPC

## Epic 16: 推理回归测试（Reasoning Regression Testing — agtest）
用户可以通过声明式 YAML 编写智能体行为测试用例，使用推理断言/syscall 断言/质量断言验证行为，批量运行回归测试确保修改不破坏已有功能。
**FRs covered:** FR87, FR88, FR89
**NFRs:** NFR35

## Epic 17: 可视化调试面板（Visual Debugging Dashboard）
用户可以在统一的全屏 TUI 面板中同时查看智能体树、追踪时间线和上下文热力图，窗格间联动交互，直接操作进程，支持从录制文件离线回放分析。
**FRs covered:** FR90, FR91, FR92, FR93, FR94, FR95, FR96
**NFRs:** NFR36, NFR37
**Dependencies:** Epic 13-15（聚合调试/追踪数据）

## Epic 18: AgentShell 完整脚本语言（AgentShell Complete Scripting）
用户可以编写包含循环（for/while）、函数定义、数据结构（数组/映射）、spawn 返回值捕获、并行执行块、模块导入的完整脚本，通过 `rnix run` 执行自动化编排。
**FRs covered:** FR97, FR98, FR99, FR100, FR101, FR102, FR103, FR104, FR105
**NFRs:** NFR38, NFR39
**Dependencies:** Phase 2 AgentShell 基础语法

## Epic 19: 声明式意图与自动规划（Declarative Intent & Auto Planning）
用户只需通过 `rnix apply` 声明期望状态，系统自动分解为子意图树、分配智能体执行，Reconciler 持续监测差异并自动调和，支持运行中增量更新意图。
**FRs covered:** FR106, FR107, FR108, FR109, FR110, FR111
**NFRs:** NFR40

## Epic 20: 自主智能体（Autonomous Agents — OODA + Stem Cell Differentiation）
智能体可以通过 OODA 循环（感知-判断-决策-行动）自主执行任务，通用基底智能体根据意图自动匹配 Skill 完成分化，支持渐进式特化、分化记忆和谱系图追溯。
**FRs covered:** FR112, FR113, FR114, FR115, FR116, FR117, FR118, FR119, FR120, FR121, FR122
**NFRs:** NFR41, NFR42

## Epic 21: Token 经济、声誉与 Skill 协同（Token Economy, Reputation & Skill Synergy）
系统智能管理 Compose 编排的 token 预算分配，通过合约 SLA 约束协作质量，基于历史表现建立声誉评分并自动择优选择；同时自动检测 Skill 间的协同效应，维护有效组合矩阵。
**FRs covered:** FR123, FR124, FR125, FR126, FR127, FR128, FR138, FR139, FR140
**NFRs:** NFR43, NFR46

## Epic 22: 适应性安全与自愈（Adaptive Security & Self-Healing）
系统通过 Immune Daemon 持续监控智能体行为模式、建立基线、检测异常并自动拦截；维护威胁记忆库快速识别已知攻击模式；故障时自动将任务迁移到相似能力的智能体，维护协作拓扑和强化路径。
**FRs covered:** FR129, FR130, FR131, FR132, FR133, FR134, FR135, FR136, FR137
**NFRs:** NFR44, NFR45

## Epic 23: 多 LLM Provider 动态配置（Multi-LLM Provider Management）
用户通过 `rnix-providers.yaml` 声明式定义 LLM provider（CLI 类或 HTTP API 类），daemon 启动时动态注册到 VFS，Agent 可指定 provider 并支持 fallback 降级——从单一 Claude CLI 演进为灵活的多模型架构。
**FRs covered:** FR141, FR142, FR143, FR144, FR145, FR146
**NFRs:** NFR31 (配置解析 ≤2s), NFR32 (健康检查 ≤3s), NFR33 (fallback ≤1s)
**Dependencies:** Phase 1-2 LLM 驱动层基础（drivers/llm/ 已有 LLMDriver 接口、DriverRegistry、OpenAICompatDriver）
**User Journey:** 旅程 5（陈明切换本地 Ollama + Claude fallback）

## Epic 24: LLM Serve — OpenAI 兼容网关（LLM Serve Gateway）
通过 `rnix serve` 启动 OpenAI 兼容 HTTP 服务器，将 daemon 已注册的 `/dev/llm/*` provider 暴露为标准 OpenAI API 端点。外部工具（Aider、Open WebUI、Python `openai` 库等）无需了解 Rnix 内部即可消费 LLM 能力——一个端口统一所有 LLM 访问。
**FRs covered:** FR147, FR148, FR149, FR150, FR151, FR152
**NFRs:** NFR50 (HTTP 开销 ≤50ms), NFR51 (≥10 并发), NFR52 (仅绑定 127.0.0.1)
**Dependencies:** Epic 23（DriverRegistry + rnix-providers.yaml 配置驱动）
**User Journey:** 旅程 6（陈明通过 rnix serve 让外部工具使用 LLM）

## Epic 25: 配置系统重构（Configuration System Redesign）
用户安装 Rnix 后运行 `rnix init` 即可创建 `~/.config/rnix/` 全局配置环境，内置 agent/skill 从二进制模板自动提取并可自由定制。项目中创建 `.rnix/` 后，项目级配置自动与全局 deep merge，项目级 agent/skill shadow 全局同名定义。CLI 自动从 CWD 向上发现 `.rnix/`，通过 IPC 传入 daemon。同一 daemon 同时服务多项目，每进程持有不可变配置快照。
**FRs covered:** FR153, FR154, FR155, FR156, FR157, FR158, FR159, FR160, FR163, FR164
**NFRs:** NFR53 (init ≤3s), NFR54 (ProjectDir ≤10ms), NFR55 (合并 ≤50ms)
**Dependencies:** 无（全新 `internal/config/` 包）
**Architecture:** Decision 14-22（配置系统架构决策）

## Epic 26: 统一推理循环（Unified Reasoning Loop）
废弃 linear/OODA 双推理模式，统一为单一 `reasonStep` 循环。LLM 每步自主决策行为类型（tool_call/plan/spawn/complete/specialize/replan），planning 作为可配置能力而非独立模式。同步修复 strace 分析发现的 6 个问题（2 Critical + 2 High + 2 Medium）。净删除 ~2000 行代码。
**FRs covered:** FR8（扩展）, FR10（扩展）, FR112-FR118（重写）
**NFRs:** NFR44（重写：统一循环单步框架开销 ≤50ms）, NFR45（保留）
**Dependencies:** Epic 20（替换其 OODA 实现）
**Architecture:** Decision 23（统一推理循环）

---
