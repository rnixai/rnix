# Project Scoping & Phased Development

## MVP Strategy & Philosophy

**MVP 类型：** 平台验证型——验证"智能体即进程"OS 范式的核心可行性，而非面向最终用户的最小可用产品。

**Phase 1 用户：** 构建者自己（Decker）。先让自己能用，再让别人能用。

**MVP 设计原则：**
- 内核功能必须可靠，用户体验可以粗糙
- ABI 设计必须稳定到能支撑 Phase 2 扩展
- 自举验证是唯一的硬性验收标准

**资源需求：** 单人开发，Go 技术栈，依赖至少一个 LLM provider（默认 Claude Code CLI）。

## LLM Driver Strategy: 多 Provider 动态配置

**核心决策：** Rnix 的 `/dev/llm/` 驱动层支持多种 LLM provider 的动态注册与切换，通过 `providers.yaml`（全局 `~/.config/rnix/providers.yaml` 或项目级 `.rnix/providers.yaml`）配置文件驱动，新增 provider 无需修改源码。

**设计演进：**
- Phase 1 以 Claude Code CLI 为唯一 provider，验证"智能体即进程"核心范式
- Phase 2 引入 Cursor CLI 作为第二个 CLI 类 provider
- 当前阶段引入 OpenAI 兼容 HTTP API 驱动，支持接入任意兼容端点（Ollama、Groq、DeepSeek 等）
- 未来可扩展 native SDK 驱动（直连 Anthropic/OpenAI/Gemini API）

**两类驱动模型：**

| 驱动类型 | 特征 | 示例 |
|---------|------|------|
| CLI 驱动 | 调用本地 CLI 工具，认证/重试由 CLI 自身处理 | claude、cursor |
| HTTP API 驱动 | 调用 OpenAI 兼容 `/v1/chat/completions` 端点，需配置 base_url 和 API Key | Ollama、Groq、DeepSeek |

**Provider 配置方式（`providers.yaml`，位于 `~/.config/rnix/` 或 `.rnix/`）：**

| 配置字段 | 说明 | 示例 |
|---------|------|------|
| `driver` | 驱动类型标识 | `claude-cli` / `cursor-cli` / `openai-compat` |
| `command` | CLI 二进制命令名称（仅 CLI 驱动） | `agent` / `claude` / `/usr/local/bin/claude` |
| `default_model` | 该 provider 的默认模型 | `sonnet` / `llama3` |
| `base_url` | HTTP API 端点（仅 HTTP 驱动） | `http://localhost:11434/v1` |
| `api_key_env` | API Key 环境变量名（仅 HTTP 驱动） | `GROQ_API_KEY` |

**Claude Code CLI → Rnix 能力映射（默认 provider）：**

| Claude Code CLI | Rnix 映射 | 阶段 |
|----------------|---------|------|
| `claude -p "query"` | `reasonStep` 非交互调用 | Phase 1 |
| `--output-format json` | 解析 action（tool_call/text/spawn） | Phase 1 |
| `--system-prompt` | Agent `instructions.md` + Skill `SKILL.md` body 组合注入 | Phase 1 |
| `--tools` / `--allowedTools` | `/dev/` 设备权限，Skill `SKILL.md` `allowed-tools` 聚合 | Phase 1 |
| `--model sonnet/haiku` | Agent `agent.yaml` `models.preferred` | Phase 1 |
| `--max-turns` | reasonStep 循环上限 | Phase 1 |
| `--stream-json` | `strace` 实时追踪数据源 | Phase 1 |
| `--max-budget-usd` | Token 预算控制 | Phase 2 |
| `--mcp-config` | `/mnt/mcp/` 挂载实现，agent.yaml `mcp:` 字段引用 MCP 服务器 | Phase 2 |
| `--agents` | 多智能体子进程 spawn | Phase 2+ |

**模型选择优先级：** CLI `--model` 参数 > agent.yaml `models.preferred` > provider `default_model`

**Provider 选择优先级：** CLI `--provider` 参数 > agent.yaml `models.provider` > 系统默认（claude）

**安装前置条件：** 用户需至少配置一个 LLM provider。首次使用 `rnix init` 自动引导全局配置（创建 `~/.config/rnix/`、填写 providers.yaml、复制内置 agents/skills 模板）。CLI 类驱动需预装对应 CLI 工具（如 Claude Code CLI）；HTTP API 类驱动需可达的端点和有效 API Key。

**架构影响：** Rnix 内核专注于进程管理、VFS、上下文组装；LLM 交互委托给对应的 provider 驱动。双层配置目录（全局 `~/.config/rnix/` + 项目 `.rnix/`）遵循 XDG 标准和 Git/npm 等主流工具惯例，配置文件驱动的注册机制使得扩展新 provider 为纯配置操作，保持内核代码稳定。

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
| CLI 入口 | `cmd/rnix/main.go` | `rnix "意图"` + `rnix strace <pid>` + `rnix ps` |
| strace | `debug/strace.go` | syscall 追踪（基于 `--stream-json` 实时数据） |

**MVP 实现的 ~15 个 syscall：**
详见 [API Surface (Syscall ABI)](#api-surface-syscall-abi) 中的完整列表。

**MVP 文档交付：**
- 概念文档（为什么是 Agent OS）
- 快速上手（安装 → spawn → strace，≤ 15 分钟）
- 参考手册（syscall 列表、VFS 路径、manifest 字段、CLI 命令）

## Post-MVP Features

**Phase 2（能力栈建设）：**

**核心旅程支持：**
- 旅程 3（林薇的 30 分钟工作流）— 完整支持（Compose + skillpkg + rnix top）
- 旅程 4（林薇的调试时刻）— 完整支持（rnix log + 上下文预算）

**Must-Have 能力清单：**

| 组件 | 文件 | 说明 |
|------|------|------|
| IPC 通信 | `kernel/ipc.go` | Send/Recv/Pipe syscall + 消息队列 + 管道实现 |
| 进程组 | `kernel/procgroup.go` | Process Group 管理 + 批量信号 + JoinGroup/GetProcGroup |
| 信号系统 | `kernel/signal.go` | Signal/SigBlock/SigUnblock + 信号处理器注册 |
| Compose 引擎 | `compose/engine.go` | YAML 解析 + DAG 依赖调度 + 并行执行 |
| Compose CLI | `cmd/rnix/compose.go` | `rnix compose up/down` 命令 |
| skillpkg 客户端 | `skillpkg/client.go` | 社区仓库 API 交互 + Skill 下载 + 版本解析 |
| skillpkg CLI | `cmd/rnix/skill.go` | `skill install/search/update/list` 命令 |
| MCP 挂载 | `vfs/mcp.go` | `/mnt/mcp/` 路径挂载 + MCP 协议适配 |
| MCP 驱动 | `drivers/mcp/mcp.go` | MCP 服务器生命周期管理 + 工具暴露 |
| Supervisor | `kernel/supervisor.go` | Supervisor 树 + 三种重启策略（one_for_one/all/rest_for_one） |
| init 引导 | `kernel/init.go` | 系统启动序列 + 服务初始化 |
| rnix top | `cmd/rnix/top.go` | 实时监控 TUI（bubbletea）+ 智能体树 + token 消耗 |
| rnix log | `cmd/rnix/log.go` | 分类推理日志（think/tool/output）+ 过滤 |
| 上下文预算 | `context/budget.go` | token 预算管理 + `--max-budget-usd` 映射 |
| AgentShell 管道 | `shell/pipe.go` | `spawn "A" \| spawn "B"` 管道语法解析与执行 |
| AgentShell 脚本 | `shell/script.go` | 变量、环境传递、多行脚本、流程控制 |
| Capability 权限 | `kernel/capability.go` | CapGrant/Revoke/Check + 完整权限系统 |
| VFS 扩展 | `vfs/mount.go` | Mount/Unmount syscall + 文件系统挂载表 |
| LLM Serve 网关 | `ipc/http_openai.go` | OpenAI 兼容 HTTP Server（/v1/chat/completions, /v1/models），路由到已注册 `/dev/llm/*` 驱动 |
| LLM Serve CLI | `cmd/rnix/serve.go` | `rnix serve --port` 命令，启动 HTTP 网关服务 |
| Config System | `internal/config/` | 双层配置目录解析（全局 `~/.config/rnix/` + 项目 `.rnix/`）、deep merge / shadow 合并策略、ProjectDir() 向上遍历 |
| rnix init | `cmd/rnix/init.go` | 全局 + 项目初始化（引导 providers.yaml、复制内置 agent/skill 模板） |
| rnix migrate | `cmd/rnix/migrate.go` | 旧配置自动迁移（根目录 `rnix-*.yaml` → `.rnix/*.yaml`） |
| embed 模板 | `lib/` (embed.FS) | 内置 Agent/Skill 嵌入二进制，init 时复制到 `~/.config/rnix/` |
| 教程文档 | `docs/tutorials/` | 编写 Skill + 调试 bug + 多智能体工作流 |
| 架构文档 | `docs/architecture.md` | 微内核 + 进程模型 + 驱动层 + 上下文管理 |

**Phase 2 实现的 ~30 个新增 syscall：**
详见 [API Surface (Syscall ABI)](#api-surface-syscall-abi) 中的 Phase 2 完整列表。

**Phase 2 文档交付：**
- 教程（编写第一个 Skill、调试第一个 bug、组合多智能体工作流）
- 架构文档（微内核设计、进程模型、驱动层、上下文管理）

**Phase 3（涌现与智能）：**

**核心旅程支持：**
- 旅程 5（待定义）— 声明式意图驱动的端到端自动化体验
- 旅程 6（待定义）— 调试工具链的深度诊断体验

**Must-Have 能力清单：**

| 组件 | 说明 | FR 覆盖 |
|------|------|---------|
| gdb 交互式调试器 | Attach/断点/单步/热修改/Detach | FR71-75, FR72a |
| 时间旅行调试 | 录制/回放/反向单步/上下文 diff/fork-continue | FR76-79, FR76a |
| 分布式因果链追踪 | Trace ID 传播/Span 树/blame 根因定位 | FR80-83 |
| 上下文内存分析器 | 活跃/温/冷/泄漏分类/消费者识别/耗尽预测 | FR84-86 |
| agtest 推理回归测试 | 声明式 YAML 测试/三种断言/批量运行 | FR87-89 |
| 可视化调试面板 | 智能体树/时间线/热力图/联动/离线回放 | FR90-96 |
| AgentShell 完整脚本 | 循环/函数/数组/并行块/source/shebang | FR97-105 |
| 声明式意图 + Reconciler | apply 声明/Intent State/事件驱动调和/增量更新/子意图树 | FR106-111 |
| 统一推理循环 | 单一 reasonStep 循环/LLM 自主 action 选择/planning 配置/熔断机制 | FR112-117 |
| 干细胞分化 | Stem Agent/自动 Skill 匹配/渐进特化/表观遗传/谱系图 | FR118-122 |
| Token 经济 + 声誉 | 预算池/价格信号/合约 SLA/声誉评分/自然选择 | FR123-128 |
| 适应性免疫安全 | Immune Daemon/行为基线/异常检测/威胁记忆库 | FR129-133 |
| 神经可塑性 | 能力迁移/相似度矩阵/强化路径/协作拓扑 | FR134-137 |
| Skill 组合涌现 | synergy 声明/自动检测/组合矩阵 | FR138-140 |

**Phase 3 新增 NFR：** NFR31-NFR46（调试工具链性能、可视化面板性能、脚本性能、涌现层性能）

**验证标准：** 系统能从声明式意图自动规划、分化智能体、自主执行、自愈恢复、持续优化；调试工具链能端到端追踪多智能体系统的完整因果链并支持时间旅行回放

## Risk Mitigation Strategy

**技术风险：**

| 风险 | 缓解 |
|------|------|
| Claude Code CLI 接口变更破坏 Rnix 驱动层 | 驱动层抽象隔离，CLI 交互封装在 `drivers/llm/claude.go` 单文件中，变更时只改一处 |
| reasonStep 循环与 CLI 交互不稳定 | 可靠性验收要求 20 次连续成功率 ≥ 95% |
| ABI 设计不够前瞻，Phase 2 需要破坏性变更 | MVP 的 15 个 syscall 严格遵循架构文档的 45 syscall 子集 |
| Go 单二进制对 Skill 动态加载的限制 | Agent + Skill 是文本注入（agent.yaml + instructions.md + SKILL.md → `--system-prompt` + `--tools`），不是 Go plugin |
| HTTP API provider 端点不可用或限流 | Provider fallback 降级机制（FR144）自动切换备选 provider；NFR48 健康检查在启动时标记不可用 provider |
| API Key 过期或轮换 | 通过环境变量引用（FR146），Key 更新无需修改配置文件，运维层面独立管理 |
| 所有已配置 provider 均失败 | 进程正确转为 Zombie 状态并上报详细错误（含尝试过的 provider 列表和各自失败原因） |
| `providers.yaml` 配置解析错误 | daemon 启动时校验配置文件格式（全局 + 项目级），解析失败时以明确错误拒绝启动而非静默降级 |
| rnix serve 安全风险（未授权访问） | 默认仅绑定 127.0.0.1，外部网络不可达；未来扩展外部监听需显式配置并启用认证机制 |
| 配置迁移数据丢失 | `rnix migrate` 迁移前自动备份原文件，迁移失败时回滚到备份状态（FR162、NFR56） |
| 用户已修改全局 agent/skill 被覆盖 | `rnix init` 仅在目标目录不存在时复制模板，已有文件不覆盖；未来 `rnix upgrade` 需比对用户修改后合并 |
| `.rnix/` 目录查找性能 | ProjectDir() 设置遍历上限（到 `$HOME` 或根停止），NFR54 保证 ≤ 10ms |

**市场风险：**

| 风险 | 缓解 |
|------|------|
| OS 范式对开发者太抽象 | 概念文档 + strace demo 作为具象化入口，用调试痛点传播 |
| 现有框架快速迭代缩小差距 | 应用层天花板是结构性的，框架加功能不等于加层次 |

**资源风险：**

| 风险 | 缓解 |
|------|------|
| 单人开发进度不可控 | MVP 是最小竖切片，Claude Code CLI 驱动策略进一步简化实现 |
| 依赖 Claude Code CLI 可用性 | Claude Code 是 Anthropic 核心产品，持续维护可预期；驱动层抽象允许未来切换到直接 API 调用；多 provider 支持进一步降低单一 provider 依赖风险 |
