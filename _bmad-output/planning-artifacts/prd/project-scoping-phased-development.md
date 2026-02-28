# Project Scoping & Phased Development

## MVP Strategy & Philosophy

**MVP 类型：** 平台验证型——验证"智能体即进程"OS 范式的核心可行性，而非面向最终用户的最小可用产品。

**Phase 1 用户：** 构建者自己（Decker）。先让自己能用，再让别人能用。

**MVP 设计原则：**
- 内核功能必须可靠，用户体验可以粗糙
- ABI 设计必须稳定到能支撑 Phase 2 扩展
- 自举验证是唯一的硬性验收标准

**资源需求：** 单人开发，Go 技术栈，依赖 Claude Code CLI。

## LLM Driver Strategy: Claude Code CLI

**核心决策：** Crux 的 `/dev/llm/` 驱动不直接调用 Claude API，而是通过 Claude Code CLI 作为 LLM 设备驱动。

**理由：**
- 认证、重试、rate limiting 由 Claude Code CLI 处理
- 开发者机器已安装 Claude Code，零额外配置
- Claude Code CLI 本身是完整的代理运行时，能力远超裸 API 调用

**Claude Code CLI → Crux 能力映射：**

| Claude Code CLI | Crux 映射 | 阶段 |
|----------------|---------|------|
| `claude -p "query"` | `reasonStep` 非交互调用 | MVP |
| `--output-format json` | 解析 action（tool_call/text/spawn） | MVP |
| `--system-prompt` | Agent `instructions.md` + Skill `SKILL.md` body 组合注入 | MVP |
| `--tools` / `--allowedTools` | `/dev/` 设备权限，Skill `SKILL.md` `allowed-tools` 聚合 | MVP |
| `--model sonnet/haiku` | Agent `agent.yaml` `models.preferred` | MVP |
| `--max-turns` | reasonStep 循环上限 | MVP |
| `--stream-json` | `astrace` 实时追踪数据源 | MVP |
| `--max-budget-usd` | Token 预算控制 | Phase 2 |
| `--mcp-config` | `/mnt/mcp/` 挂载实现，agent.yaml `mcp:` 字段引用 MCP 服务器 | Phase 2 |
| `--agents` | 多智能体子进程 spawn | Phase 2+ |

**安装前置条件：** Crux MVP 要求用户已安装 Claude Code CLI。

**架构影响：** Crux 内核专注于进程管理、VFS、上下文组装；LLM 交互（调用、工具执行、重试）全部委托给 Claude Code CLI。这大幅简化了 MVP 的 LLM 驱动层实现。

## MVP Feature Set (Phase 1)

**核心旅程支持：**
- 旅程 1（陈明的调试顿悟）— 完整支持
- 旅程 2（陈明遇到 LLM 超时）— 完整支持

**Must-Have 能力清单：**

| 组件 | 文件 | 说明 |
|------|------|------|
| 微内核 | `kernel/kernel.go` | Kernel 结构体 + `Spawn()` + `reasonStep()` 心跳循环 |
| 进程模型 | `kernel/process.go` | Process 结构体 + 生命周期状态机 |
| 进程回收 | `kernel/reap.go` | wait + 孤儿进程 reparent to init |
| VFS 接口 | `vfs/vfs.go` | Open/Read/Write/Close/Stat |
| /proc FS | `vfs/proc.go` | `/proc/{pid}/` 动态文件系统 |
| /dev 注册 | `vfs/dev.go` | 设备注册与路由 |
| LLM 驱动 | `drivers/llm/claude.go` | 通过 Claude Code CLI (`claude -p`) 交互 + 超时处理 + 错误上报 |
| Shell 驱动 | `drivers/shell/shell.go` | Shell 执行驱动 |
| 上下文管理 | `context/context.go` | ctx_alloc/ctx_read/ctx_write/prompt 组装 |
| Agent 加载 | `agents/loader.go` | agent.yaml + instructions.md → AgentInfo（模型偏好 + Skill 引用 + 聚合权限） |
| Skill 加载 | `skills/loader.go` | SKILL.md 解析（渐进式加载）→ `--system-prompt` + `--tools` 参数映射 |
| 参考 Agent | `lib/agents/code-analyst/` | 自举验证载体 + Agent 参考实现 |
| 参考 Skill | `lib/skills/code-analysis/` | SKILL.md 标准格式参考实现 |
| CLI 入口 | `cmd/crux/main.go` | `crux "意图"` + `crux astrace <pid>` + `crux ps` |
| astrace | `debug/astrace.go` | syscall 追踪（基于 `--stream-json` 实时数据） |

**MVP 实现的 ~15 个 syscall：**
详见 [API Surface (Syscall ABI)](#api-surface-syscall-abi) 中的完整列表。

**MVP 文档交付：**
- 概念文档（为什么是 Agent OS）
- 快速上手（安装 → spawn → astrace，≤ 15 分钟）
- 参考手册（syscall 列表、VFS 路径、manifest 字段、CLI 命令）

## Post-MVP Features

**Phase 2（能力栈建设）：**

**核心旅程支持：**
- 旅程 3（林薇的 30 分钟工作流）— 完整支持（Compose + skillpkg + crux top）
- 旅程 4（林薇的调试时刻）— 完整支持（crux log + 上下文预算）

**Must-Have 能力清单：**

| 组件 | 文件 | 说明 |
|------|------|------|
| IPC 通信 | `kernel/ipc.go` | Send/Recv/Pipe syscall + 消息队列 + 管道实现 |
| 进程组 | `kernel/procgroup.go` | Process Group 管理 + 批量信号 + JoinGroup/GetProcGroup |
| 信号系统 | `kernel/signal.go` | Signal/SigBlock/SigUnblock + 信号处理器注册 |
| Compose 引擎 | `compose/engine.go` | YAML 解析 + DAG 依赖调度 + 并行执行 |
| Compose CLI | `cmd/crux/compose.go` | `crux compose up/down` 命令 |
| skillpkg 客户端 | `skillpkg/client.go` | 社区仓库 API 交互 + Skill 下载 + 版本解析 |
| skillpkg CLI | `cmd/crux/skill.go` | `skill install/search/update/list` 命令 |
| MCP 挂载 | `vfs/mcp.go` | `/mnt/mcp/` 路径挂载 + MCP 协议适配 |
| MCP 驱动 | `drivers/mcp/mcp.go` | MCP 服务器生命周期管理 + 工具暴露 |
| Supervisor | `kernel/supervisor.go` | Supervisor 树 + 三种重启策略（one_for_one/all/rest_for_one） |
| init 引导 | `kernel/init.go` | 系统启动序列 + 服务初始化 |
| crux top | `cmd/crux/top.go` | 实时监控 TUI（bubbletea）+ 智能体树 + token 消耗 |
| crux log | `cmd/crux/log.go` | 分类推理日志（think/tool/output）+ 过滤 |
| 上下文预算 | `context/budget.go` | token 预算管理 + `--max-budget-usd` 映射 |
| AgentShell 管道 | `shell/pipe.go` | `spawn "A" \| spawn "B"` 管道语法解析与执行 |
| AgentShell 脚本 | `shell/script.go` | 变量、环境传递、多行脚本、流程控制 |
| Capability 权限 | `kernel/capability.go` | CapGrant/Revoke/Check + 完整权限系统 |
| VFS 扩展 | `vfs/mount.go` | Mount/Unmount syscall + 文件系统挂载表 |
| 教程文档 | `docs/tutorials/` | 编写 Skill + 调试 bug + 多智能体工作流 |
| 架构文档 | `docs/architecture.md` | 微内核 + 进程模型 + 驱动层 + 上下文管理 |

**Phase 2 实现的 ~30 个新增 syscall：**
详见 [API Surface (Syscall ABI)](#api-surface-syscall-abi) 中的 Phase 2 完整列表。

**Phase 2 文档交付：**
- 教程（编写第一个 Skill、调试第一个 bug、组合多智能体工作流）
- 架构文档（微内核设计、进程模型、驱动层、上下文管理）

**Phase 3（涌现与智能）：**

| 能力 | 说明 |
|------|------|
| 声明式意图 + Reconciler | 用户声明"要什么"，控制器调和"怎么做" |
| OODA 自主决策 | 智能体内部感知-决策-行动循环 |
| 干细胞分化 | 通用基底智能体通过 Skill 自适应特化 |
| Token 经济 + 合约 SLA + 声誉系统 | 资源调度和协作治理 |
| 完整调试工具链 | agdb、时间旅行、分布式追踪、ctx-profiler |
| 可视化调试面板 | 智能体树 + 追踪时间线 + 上下文热力图 |

## Risk Mitigation Strategy

**技术风险：**

| 风险 | 缓解 |
|------|------|
| Claude Code CLI 接口变更破坏 Crux 驱动层 | 驱动层抽象隔离，CLI 交互封装在 `drivers/llm/claude.go` 单文件中，变更时只改一处 |
| reasonStep 循环与 CLI 交互不稳定 | 可靠性验收要求 20 次连续成功率 ≥ 95% |
| ABI 设计不够前瞻，Phase 2 需要破坏性变更 | MVP 的 15 个 syscall 严格遵循架构文档的 45 syscall 子集 |
| Go 单二进制对 Skill 动态加载的限制 | Agent + Skill 是文本注入（agent.yaml + instructions.md + SKILL.md → `--system-prompt` + `--tools`），不是 Go plugin |

**市场风险：**

| 风险 | 缓解 |
|------|------|
| OS 范式对开发者太抽象 | 概念文档 + astrace demo 作为具象化入口，用调试痛点传播 |
| 现有框架快速迭代缩小差距 | 应用层天花板是结构性的，框架加功能不等于加层次 |

**资源风险：**

| 风险 | 缓解 |
|------|------|
| 单人开发进度不可控 | MVP 是最小竖切片，Claude Code CLI 驱动策略进一步简化实现 |
| 依赖 Claude Code CLI 可用性 | Claude Code 是 Anthropic 核心产品，持续维护可预期；驱动层抽象允许未来切换到直接 API 调用 |
