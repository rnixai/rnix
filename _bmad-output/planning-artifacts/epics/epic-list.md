# Epic List

## Epic 1: 第一个智能体运行（First Agent Runs）
用户安装 Crux 后，输入 `crux "意图"` 即可看到一个智能体启动、调用 LLM 推理、返回结果——完整的端到端体验。包含项目初始化、内核核心（进程模型 + Spawn + reasonStep）、VFS 框架、LLM 驱动（Claude Code CLI）、上下文管理、CLI 入口和基础 UI 组件。
**FRs covered:** FR1, FR2, FR8, FR9, FR10, FR11, FR13, FR15, FR17, FR19, FR20, FR21, FR32, FR33, FR36, FR37

## Epic 2: Agent 能力与文件访问（Agent Skills & File Access）
用户可以通过 Agent 定义赋予智能体专业能力（如代码分析），Agent 引用的 Skill 决定智能体可访问的工具和知识——从"能说话"升级到"能干活"。包含 Agent 加载器、Skill 加载器（SKILL.md，Agent Skills 行业标准）、宿主 FS 驱动、Shell 驱动、allowed-tools 聚合白名单和 code-analyst 参考 Agent + code-analysis 参考 Skill。
**FRs covered:** FR12, FR16, FR18, FR23, FR24, FR25, FR25a, FR25b, FR26, FR27

## Epic 3: 调试追踪（Debug Tracing — astrace）
当智能体输出不符合预期时，用户运行 `crux astrace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Crux 的差异化核心体验。包含 SyscallEvent 记录、DebugChan 事件管道、astrace 命令和 Trace Line UI。
**FRs covered:** FR28, FR29, FR30, FR31, FR34

## Epic 4: 进程管理与可靠性（Process Management & Reliability）
用户可以查看所有进程状态（`crux ps`）、终止进程（`crux kill`）、等待进程完成。系统自动回收 Zombie 进程、处理孤儿进程、暴露 `/proc` 运行时状态——生产级可靠性。
**FRs covered:** FR3, FR4, FR5, FR6, FR7, FR14, FR22, FR35

## Epic 5: 文档体系（Documentation）
新用户可以通过概念文档理解 Crux 的 OS 范式，通过快速上手指南在 15 分钟内跑通 demo，通过参考手册查阅所有 syscall、VFS 路径和 CLI 命令。
**FRs covered:** FR38, FR39, FR40

---

## Phase 2 Epics

## Epic 6: IPC 跨进程通信（Inter-Process Communication）
智能体之间可以通过 Send/Recv 发送消息、通过 Pipe 连接输出与输入、通过进程组批量操作、通过 Signal 控制执行——多智能体协作的通信基础。
**FRs covered:** FR41, FR42, FR43, FR44, FR45
**NFRs:** NFR22 (IPC ≤50ms), NFR23 (Pipe ≥1MB/s), NFR24 (≥10 并发进程)
**Dependencies:** Phase 1 完成

## Epic 7: Compose 多智能体编排（Agent Compose）
用户通过 `crux-compose.yaml` 声明式定义多智能体工作流，`crux compose up` 一键启动，引擎按 DAG 依赖拓扑自动调度并行——林薇旅程的核心体验。
**FRs covered:** FR46, FR47, FR48, FR49
**NFRs:** NFR21 (≤10 个智能体启动 ≤2s)
**Dependencies:** Epic 6（IPC 管道用于智能体间数据传递）

## Epic 8: Skill 包管理与生态（Skill Package Management）
用户通过 `skill install/search/update/list` 管理社区 Skill，安装即可用，零修改引用——生态系统的基石。
**FRs covered:** FR50, FR51, FR52, FR53
**NFRs:** NFR30 (安装即可用)

## Epic 9: MCP 服务集成（MCP Integration）
系统通过 Mount/Unmount 在 `/mnt/mcp/` 挂载 MCP 服务器，智能体通过标准 VFS 访问外部工具——完成 Agent → Skill → MCP → Device 四层能力栈。
**FRs covered:** FR54, FR55, FR56, FR57
**NFRs:** NFR25 (挂载 ≤500ms), NFR26 (异常不影响内核), NFR27 (MCP 标准兼容)
**Dependencies:** Epic 6（VFS 扩展）

## Epic 10: 监控、Supervisor 与运维（Monitoring, Supervisor & Operations）
`crux top` 实时监控面板 + `crux log` 分类日志 + token 预算管理 + Supervisor 容错树 + init 引导——生产级运维能力。
**FRs covered:** FR58, FR59, FR60, FR61, FR62, FR63, FR64, FR65
**NFRs:** NFR28 (top 刷新 ≤500ms), NFR29 (log 延迟 ≤200ms)
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
