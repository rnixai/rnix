# Functional Requirements

> **编号说明：** FR 编号按 Phase 分组分配，后续迭代新增的需求使用跳跃编号以保持逻辑归属：
> - Phase 1: FR1-FR40（含 FR25a/b）
> - Phase 2: FR41-FR70 + FR141-FR152（Multi-LLM Provider + LLM Serve Gateway）+ FR153-FR164（Configuration System）+ FR165-FR173（Unified Observation System — Dashboard 增强）+ FR177-FR180（Process Identity System — PID 标识体系）
> - Phase 3: FR71-FR140（含 FR72a, FR76a）+ FR174-FR176（Dashboard Advanced Integration）
>
> NFR 编号同样存在跳跃：NFR1-NFR33（Phase 1-2 原始需求）+ NFR34-NFR49（Phase 3）+ NFR50-NFR52（LLM Serve Gateway）+ NFR53-NFR56（Configuration System）+ NFR57-NFR65（Unified Observation System + Process Identity）。
> 跳跃不影响覆盖完整性，仅反映需求演进历史。

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
- **FR17:** 智能体可以通过 `/dev/llm/<provider>` 访问已配置的 LLM provider 推理能力（默认 `/dev/llm/claude`）
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
- **FR25b:** 系统可以对 Skill 进行渐进式加载——启动时仅加载元信息摘要，激活时加载完整指令内容，执行时按需加载附属资源，以最小化启动开销和内存占用
- **FR26:** Agent 引用的所有 Skill 的 `allowed-tools` 聚合后映射为智能体的可用 `/dev/` 设备权限白名单
- **FR27:** 系统交付参考 Agent（code-analyst）+ 参考 Skill（code-analysis），能够分析代码并识别至少 1 个可验证的真实代码问题（与 Success Criteria 中自举验证标准对齐）

## Debugging & Observability（调试与可观测性）

- **FR28:** 用户可以通过 `strace` 实时追踪指定智能体的所有 syscall 调用
- **FR29:** 系统可以在 strace 输出中展示每个 syscall 的名称、参数、返回值和耗时
- **FR30:** 系统可以记录 syscall 调用数据供 strace 消费
- **FR31:** 用户可以通过 strace 输出定位到产生错误结果的具体 syscall 调用记录
- **FR32:** 系统在智能体完成时输出汇总信息（退出码、token 消耗、总耗时）

## Command Line Interface（命令行接口）

- **FR33:** 用户可以通过 `rnix "意图"` 单命令启动一个智能体
- **FR34:** 用户可以通过 `rnix strace <pid>` 追踪指定进程的 syscall
- **FR35:** 用户可以通过 `rnix ps` 查看所有进程状态
- **FR36:** 系统可以在 CLI 中输出结构化错误信息，包含设备路径、错误码和错误原因
- **FR37:** 系统可以通过 `go install` 一条命令完成安装，单二进制，零额外依赖（需至少配置一个 LLM provider）

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

- **FR46:** 用户可以通过 `.rnix/compose.yaml` 声明式定义多智能体工作流，包含每个智能体的 intent、agent 引用、skills 列表和依赖关系
- **FR47:** Compose 引擎可以解析智能体之间的 `depends_on` 依赖关系，按 DAG 拓扑顺序调度执行，自动并行化无依赖的分支
- **FR48:** 用户可以通过 `rnix compose up` 一键启动编排中定义的所有智能体
- **FR49:** 用户可以通过 `rnix compose down` 停止编排中所有智能体并释放资源（进程、上下文、文件描述符）

## Skill Package Management（Skill 包管理，Phase 2）

- **FR50:** 用户可以通过 `skill install <name>` 从社区仓库下载并安装 Skill 到项目级 `.rnix/skills/` 或全局 `~/.config/rnix/skills/` 目录
- **FR51:** 用户可以通过 `skill search <keyword>` 搜索社区仓库中可用的 Skill，返回名称、描述、版本和下载量
- **FR52:** 用户可以通过 `skill update [name]` 更新已安装 Skill 到最新兼容版本
- **FR53:** 系统维护本地 Skill 注册表，记录已安装 Skill 的元信息、版本和路径，用户可通过 `skill list` 查看

## MCP Integration（MCP 服务集成，Phase 2）

- **FR54:** 系统可以通过 Mount/Unmount syscall 在 `/mnt/mcp/` 路径下挂载和卸载 MCP 服务器
- **FR55:** Agent 的 `agent.yaml` 可以通过 `mcp` 字段引用 MCP 服务器名称列表，系统在 Spawn 时自动挂载对应服务
- **FR56:** 系统可以将 MCP 服务器提供的工具和资源通过 VFS 路径暴露给智能体，智能体通过标准 Open/Read/Write 访问
- **FR57:** 系统可以端到端运行四层能力栈：Agent（身份+策略）→ Skill（程序性知识+工具权限）→ MCP（外部服务集成）→ Device（原生 I/O），用户可以通过 strace 验证各层调用链路的职责分离

## Monitoring & Observability（监控与可观测性，Phase 2）

- **FR58:** 用户可以通过 `rnix top` 实时查看所有运行中智能体的树状关系、状态、token 消耗和执行进度
- **FR59:** 用户可以通过 `rnix log <pid>` 查看指定智能体的推理日志，支持 `--prompt` 参数在每步 think 日志前插入该步的 prompt 摘要（消息数、token 数、首条消息预览）
- **FR60:** 系统可以将 `rnix log` 输出按 `[think]`/`[tool]`/`[output]` 三段式分类显示，支持 `--filter <category>` 按类别过滤
- **FR61:** 用户可以为智能体设置 token 预算上限（通过 agent.yaml `context_budget` 或 compose 中覆盖），系统在达到上限时终止推理并上报原因
- **FR62:** 用户可以在 `rnix top` 中通过交互式操作选中进程，按回车跳转到 `rnix dashboard` 并自动聚焦该进程，按 q 返回 top 全局视图

## Unified Observation System（统一观察系统 — Dashboard 增强，Phase 2）

- **FR165:** 用户可以在 `rnix dashboard` 的时间线窗格中切换三级详细度：Level 1（默认）每步一行摘要（步骤号 + 动作类型 + 目标 + 耗时）；Level 2（按 v 键）展开当前步骤的参数、返回值和 token 消耗；Level 3（按 V 键）调试级详情含 prompt 摘要
- **FR166:** dashboard 时间线窗格中出错（错误返回）或慢操作（耗时 > 1 秒）的步骤自动展开到 Level 2，用户无需手动切换即可看到异常详情
- **FR167:** 用户可以在 dashboard 时间线窗格中按 p 键查看选中步骤的完整 prompt 内容，进入类似 less 的翻页查看模式，按 q 返回时间线
- **FR168:** 用户可以通过 `rnix spawn --dashboard "意图"` 启动智能体并立即进入 dashboard 视图，spawn 返回 PID 后零延迟聚焦该进程，消除手动查找 PID 的操作
- **FR169:** 系统为每个进程默认维护 StepRecord 全量步骤记录，在每个 reasonStep 完成后自动将该步的完整数据（BuildPrompt 时的 Messages 深拷贝、LLM 原始响应、工具执行结果、token 统计）以 NDJSON 格式 append 写入磁盘文件（`.rnix/data/steps/<pid>/steps.jsonl`），无需用户手动开启（已实现：Story 27-1）*（注：路径中的 `<pid>` 将由 FR178 迁移为 `<uuid>`，消除 daemon 重启后 PID 复用导致的数据覆盖风险）*
- **FR170:** 系统提供 GetStepDetail IPC 方法，支持按需拉取指定进程指定步骤的完整 prompt 内容（SystemPrompt + Messages + Tools 定义），仅在用户明确请求时传输，不走实时事件流（已实现：Story 27-2）
- **FR171:** 系统默认为每个进程记录完整的 LLM 请求数据（StepRecord 包含每步的完整 Messages 快照和 LLM 原始响应），`rnix record list` 和 `rnix replay` 直接从 steps.jsonl 读取，支持事后回放时查看"agent 当时收到了什么指令"，无需预先开启特殊录制模式
- **FR172:** 用户可以在 dashboard 中选中进程后查看进程详情面板，展示进程的完整运行时信息：环境变量快照、已加载 Skill 列表、FD 表（已打开的 VFS 设备）、上下文统计（消息数、token 消耗、上下文预算使用率）
- **FR173:** 用户可以在 dashboard 中查看 Intent DAG 可视化窗格，以树状 / DAG 图展示意图分解结构，节点按状态着色（pending/executing/completed/failed），点击节点联动切换到对应进程的时间线和上下文视图

## Process Identity System（进程标识体系，Phase 2）

- **FR177:** 系统为每个进程在 Spawn 时生成 UUID v7 唯一标识符，UUID 在跨 daemon 重启后保持唯一，PID 保留为 daemon 内递增的用户友好短标识
- **FR178:** 所有持久化数据路径使用 UUID 而非 PID（StepRecord 路径 `.rnix/data/steps/<uuid>/`、process-meta.json 等），避免 daemon 重启后 PID 复用导致的数据覆盖
- **FR179:** IPC 方法（GetStepDetail 等）支持按 PID 或 UUID 查询——PID 在当前 daemon 生命周期内唯一，UUID 全局唯一；客户端可使用任一标识，daemon 内部统一转换为 UUID
- **FR180:** Dashboard 通过 UUID 验证进程同一性——selectedPID 对应的进程死亡并被 Reaper 清理后，Dashboard 正确检测并清除选中状态，防止新进程复用 PID 导致的误显示

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

## Multi-LLM Provider Management（多 LLM 提供方管理，Phase 2）

- **FR141:** 系统通过 `providers.yaml` 配置文件（全局 `~/.config/rnix/providers.yaml` 或项目级 `.rnix/providers.yaml`）声明式定义 LLM provider，daemon 启动时动态解析并注册到 VFS `/dev/llm/<name>` 路径，新增 provider 无需修改源码；CLI 类 provider 支持通过 `command` 字段配置命令别名（如 cursor-cli 默认命令为 `agent`，可覆盖为 `cursor-agent`）
- **FR142:** 系统支持两类 provider 驱动：CLI 驱动（通过本地 CLI 工具交互，如 claude/cursor）和 HTTP API 驱动（通过 OpenAI 兼容 API 端点交互，如 Ollama/Groq/DeepSeek）
- **FR143:** Agent 的 `agent.yaml` 中 `models.provider` 字段指定 LLM provider，系统在 spawn 时解析为对应的 `/dev/llm/<provider>` VFS 设备路径
- **FR144:** 系统支持 provider fallback 降级——当 `models.preferred` 对应的 provider 调用失败（HTTP 5xx、连接超时、连接拒绝、认证失败）时，自动尝试 `models.fallback` 指定的备选 provider/model 组合
- **FR145:** 用户可通过 CLI `--provider` 参数在 spawn 时覆盖 agent.yaml 中的 provider 配置
- **FR146:** HTTP API 类型的 provider 支持通过环境变量引用配置 API Key（如 `api_key_env: GROQ_API_KEY`），系统不明文存储密钥

## LLM Serve Gateway（LLM 网关服务，Phase 2）

- **FR147:** 用户可通过 `rnix serve` 启动 OpenAI 兼容 HTTP 服务器，仅监听 localhost，将已注册的 `/dev/llm/*` provider 暴露为标准 OpenAI API 端点，外部工具无需了解 Rnix 内部即可消费 LLM 能力
- **FR148:** 服务支持 `/v1/chat/completions` 端点，将请求中的 model 参数路由到对应的 VFS LLM 驱动（如 `model: "cursor"` → `/dev/llm/cursor`），请求/响应格式兼容 OpenAI Chat Completion API
- **FR149:** 服务支持 `/v1/models` 端点，返回所有已注册且健康的 provider 及其可用模型列表，格式兼容 OpenAI Models API
- **FR150:** `/v1/chat/completions` 端点支持 SSE 流式响应（`stream: true`），事件格式兼容 OpenAI 流式协议（`data: {...}\n\n`）
- **FR151:** model 参数支持 `provider:model` 复合格式路由（如 `cursor:claude-3.5-sonnet`），当仅指定 provider 名时使用该 provider 的 `default_model`
- **FR152:** 服务共享 daemon 已注册的驱动实例和 providers 配置，新增或变更 provider 后重启 daemon 即可生效，无需独立配置

## Configuration System（配置系统，Phase 2）

- **FR153:** 系统提供双层配置目录结构——全局级 `~/.config/rnix/`（遵循 XDG_CONFIG_HOME 标准）存储用户级配置（API key、默认偏好、全局 agent/skill），项目级 `.rnix/` 存储项目特定配置（编排、自定义 agent/skill、运行时数据）
- **FR154:** 用户可通过 `rnix init` 初始化配置环境，命令自动判断全局配置是否存在——未配置时先初始化全局（创建 `~/.config/rnix/` 目录、引导填写 providers.yaml、从内置模板复制 agents 和 skills），再初始化当前项目（创建 `.rnix/` 目录结构）
- **FR155:** 系统从 CWD 向上遍历目录树查找 `.rnix/` 目录（类似 git 查找 `.git/`），到 `$HOME` 或文件系统根停止；CLI 将发现的 `project_dir` 通过 IPC 传入 daemon
- **FR156:** YAML 配置文件（providers.yaml、config.yaml、mcp.yaml）采用 deep merge 合并策略——项目级字段覆盖全局级同名字段；Agent 和 Skill 目录采用 shadow 策略——项目级同名定义完全遮蔽全局级，不做字段合并
- **FR157:** Agent/Skill 查找按项目级 → 全局级顺序——项目级 `.rnix/agents/<name>/` 优先于全局级 `~/.config/rnix/agents/<name>/`，同名时项目级完全遮蔽全局级
- **FR158:** 内置 Agent/Skill（当前 `lib/agents/` 和 `lib/skills/`）打包在二进制中（内置模板随二进制分发），不再作为运行时查找路径；`rnix init` 全局初始化时复制到 `~/.config/rnix/agents/` 和 `~/.config/rnix/skills/`，用户获得独立副本可自由修改
- **FR159:** 配置文件进入 `.rnix/` 或 `~/.config/rnix/` 目录后去掉 `rnix-` 前缀（`rnix-providers.yaml` → `providers.yaml`、`rnix-init.yaml` → `init.yaml`、`rnix-compose.yaml` → `compose.yaml`）
- **FR160:** IPC spawn 请求 payload 增加 `project_dir` 字段，daemon 端根据 `project_dir` 读取并合并项目级 `.rnix/` 配置；同一 daemon 可同时服务不同项目的进程
- **FR161:** ~~推迟~~ 系统检测到根目录旧配置文件（如 `rnix-providers.yaml`）时输出 deprecation warning，旧文件仍可识别但优先使用新路径 *（推迟：全新项目无现有用户需要迁移，待有实际迁移需求时在后续 Epic 中实现）*
- **FR162:** ~~推迟~~ 用户可通过 `rnix migrate` 自动将旧配置迁移到新结构（根目录 `rnix-*.yaml` → `.rnix/*.yaml`，`.rnix/` 根目录运行时数据 → `.rnix/data/`） *（推迟：全新项目无现有用户需要迁移，待有实际迁移需求时在后续 Epic 中实现）*
- **FR163:** 运行时数据（records、traces、reputation、immune）存放在 `.rnix/data/` 子目录下，与配置文件物理隔离
- **FR164:** daemon 启动时加载全局配置（`~/.config/rnix/` 下 providers.yaml、config.yaml、mcp.yaml），spawn 请求时按 `project_dir` 合并项目级配置；项目级配置作为进程上下文的配置快照绑定到进程生命周期

---

## gdb Interactive Debugger（gdb 交互式调试器，Phase 3）

- **FR71:** 用户可以通过 `rnix gdb <pid>` 附着（Attach）到一个运行中的智能体进程，进入交互式调试会话
- **FR72:** 用户可以在 gdb 中设置断点（Breakpoint），支持四种断点类型：syscall 断点（指定 syscall 名触发）、推理断点（LLM 调用前触发）、质量断点（输出不满足条件时触发）、预算断点（token 消耗达阈值时触发）
- **FR72a:** 质量断点支持两种模式：（1）模式匹配——用户定义输出必须包含/不得包含的关键词或正则表达式；（2）LLM 评估——用户提供自然语言质量标准（如"输出必须包含代码示例"、"不得出现幻觉性断言"），系统通过轻量模型（haiku）自动评估，不满足时触发断点
- **FR73:** 用户可以在 gdb 中单步执行（Step），逐个 syscall 或逐个推理步骤前进，查看每步的参数、返回值和上下文变化
- **FR74:** 用户可以在 gdb 中检查和热修改智能体的运行时参数，包括上下文内容、model 偏好、Skill 引用列表和环境变量，修改立即生效于下一个推理步骤
- **FR75:** 用户可以在 gdb 中通过 Detach 断开调试会话，智能体继续正常执行

## Time-Travel Debugging（时间旅行调试，Phase 3）

- **FR76:** 系统可以对指定进程开启完整执行录制（Record），捕获每个 syscall、LLM 调用、上下文变更和工具执行结果
- **FR76a:** 录制数据持久化到 `$PROJECT/.rnix/records/<pid>-<timestamp>/` 目录，包含完整的 syscall 序列、上下文快照和 LLM 响应，格式为 JSON Lines（每行一个事件），支持离线分析
- **FR77:** 用户可以通过 `rnix replay <record-id>` 回放录制的执行轨迹，支持正向播放、反向单步和任意跳转到指定时间点
- **FR78:** 用户可以在回放过程中查看任意时间点的完整上下文快照（context diff），对比两个时间点之间的上下文变化
- **FR79:** 用户可以在回放的任意时间点执行 fork-continue，从该历史点创建一个新分支，修改上下文后重新执行（产生真实 LLM 调用），验证"如果当时做了不同决定会怎样"

## Distributed Causal Tracing（分布式因果链追踪，Phase 3）

- **FR80:** 系统可以为每个 Compose 编排生成唯一的 Trace ID，并在智能体间通过 IPC 自动传播，形成跨进程的因果链
- **FR81:** 系统可以在每个智能体内记录 Span（起止时间、syscall 序列、token 消耗），Span 之间通过 parent-child 关系构成追踪树
- **FR82:** 用户可以通过 `rnix trace <trace-id>` 查看完整的分布式追踪视图，包含所有参与智能体的时序关系和依赖链路
- **FR83:** 用户可以通过 `rnix trace blame <trace-id>` 自动分析追踪数据，定位耗时最长、token 消耗最大或产生错误的关键路径节点

## Context Memory Profiler（上下文内存分析器，Phase 3）

- **FR84:** 用户可以通过 `rnix ctx-profile <pid>` 查看指定智能体的上下文使用分析，将上下文内容分为活跃（当前推理引用）、温（近期使用）、冷（未引用）、泄漏（已无用但未释放）四类
- **FR85:** 系统可以识别上下文中的最大消费者（哪个 Skill 或工具结果占用最多 token），并给出优化建议
- **FR86:** 系统可以预测当前上下文增长趋势，在预计耗尽前发出告警

## Reasoning Regression Testing — agtest（推理回归测试，Phase 3）

- **FR87:** 用户可以通过声明式 YAML 文件定义智能体行为测试用例，包含输入意图、Agent 配置、预期行为断言
- **FR88:** 系统可以在 agtest 中支持三种断言类型：推理断言（LLM 输出包含/不包含特定内容）、syscall 断言（执行了/未执行特定 syscall 序列）、质量断言（输出满足自定义评估标准）
- **FR89:** 用户可以通过 `rnix agtest [test-file]` 批量运行测试并输出结果报告（通过/失败/跳过 + 失败原因）

## Visualization Dashboard（可视化调试面板，Phase 3）

- **FR90:** 用户可以通过 `rnix dashboard` 启动可视化调试面板，在单一 TUI 界面中展示多窗格视图：智能体树（进程关系）、追踪时间线（syscall 序列）、上下文热力图（token 使用分布）
- **FR91:** 智能体树窗格实时显示所有进程的父子关系、状态（Running/Zombie/Dead）、当前执行阶段和 token 消耗，用户可以展开/折叠子树
- **FR92:** 追踪时间线窗格以时间轴形式展示选中智能体（或 Compose 编排全体）的 syscall 事件流，支持缩放、滚动和按类别过滤（LLM/Tool/IPC/VFS）
- **FR93:** 上下文热力图窗格可视化选中智能体的上下文组成——按来源（system prompt / skill 指令 / 工具结果 / 对话历史）着色，面积正比于 token 占比，颜色深浅表示活跃度（活跃/温/冷）
- **FR94:** 用户可以在 dashboard 中点击任意智能体节点，联动切换时间线和热力图到该智能体的数据
- **FR95:** 用户可以在 dashboard 中直接对选中进程执行操作：kill、attach gdb、查看 log、开启录制
- **FR96:** dashboard 支持从持久化的录制文件加载历史数据，提供离线回放和分析能力

## Dashboard Advanced Integration（Dashboard 高级集成，Phase 3）

- **FR174:** 用户可以在 dashboard 中查看安全异常面板，集成 Immune Daemon 的实时告警信息（异常行为检测、已挂起进程、威胁模式匹配），按严重度排序，点击告警可跳转到对应进程详情（依赖 Phase 3 FR129-FR133 Adaptive Immune Security 底层能力）
- **FR175:** 用户可以在 dashboard 中查看分布式追踪集成窗格，以 span 树形式展示 Compose 编排的跨进程追踪数据（时序关系、调用链路、耗时瀑布图），与 `rnix trace` 命令行数据一致（依赖 Phase 3 FR80-FR83 Distributed Causal Tracing 底层能力）
- **FR176:** 用户可以在 dashboard 中查看多智能体评价视图，集成声誉系统数据（各 Agent 模板的成功率、token 效率、SLA 达标率）、协作拓扑图（进程间通信频率和方向）和能力重叠度矩阵（依赖 Phase 3 FR123-FR128 Token Economy + Reputation 底层能力）

## AgentShell Complete Scripting Language（AgentShell 完整脚本语言，Phase 3）

- **FR97:** 用户可以在 AgentShell 脚本中使用循环结构：`for item in <list>`（遍历）和 `while <condition>`（条件循环），循环体内可嵌套 spawn 和其他控制结构
- **FR98:** 用户可以在 AgentShell 脚本中定义和调用函数（`fn name(args) { ... }`），支持参数传递和返回值，实现脚本逻辑复用
- **FR99:** 用户可以在 AgentShell 中使用数组和映射数据结构，支持基本的集合操作（遍历、索引、长度、追加）
- **FR100:** AgentShell 支持 spawn 表达式返回值捕获（`result = spawn "分析" --agent=analyst`），将智能体输出绑定到变量供后续使用
- **FR101:** 用户可以在 AgentShell 中使用并行执行块（`parallel { spawn "A"; spawn "B"; spawn "C" }`），块内所有 spawn 并行执行，块结束时等待全部完成
- **FR102:** 用户可以在 AgentShell 脚本中通过 `source <file>` 导入其他脚本文件，实现模块化组织
- **FR103:** AgentShell 提供内置命令集用于流程控制：`wait <pid>`（等待进程）、`sleep <duration>`（延时）、`exit <code>`（退出脚本）
- **FR104:** AgentShell 支持字符串插值（`"分析 ${file_path} 的代码质量"`），在 intent 和参数中引用变量值
- **FR105:** 用户可以通过 `rnix run <script.ash>` 执行 AgentShell 脚本文件，脚本以 `#!/usr/bin/env rnix run` 作为 shebang 也可直接执行

## Declarative Intent + Reconciler（声明式意图 + 控制器调和，Phase 3）

- **FR106:** 用户可以通过声明式意图描述期望状态（如 `rnix apply "我要一个完整的博客系统"`），系统自动分解为子任务并分配给智能体执行
- **FR107:** 系统维护一个意图状态模型（Intent State），包含期望状态（Desired）、当前状态（Current）和差异（Drift），Reconciler 持续监测并消除差异
- **FR108:** Reconciler 采用事件驱动模式——当子任务完成、失败或超时时触发调和循环，自动重新规划和重试，无需用户手动干预
- **FR109:** 用户可以在执行过程中更新期望状态（`rnix apply "加上评论功能"`），Reconciler 计算增量差异并仅执行变更部分，已完成的工作不回滚
- **FR110:** 系统可以将高层意图递归分解为子意图树（Intent Tree），每个子意图对应一个或多个智能体进程，父意图的完成取决于所有子意图的达成
- **FR111:** 用户可以通过 `rnix intent status` 查看意图树的当前状态，包含每个子意图的完成度、执行中的智能体和待解决的 drift

## Unified Reasoning Loop（统一推理循环，Phase 3）

- **FR112:** 系统提供统一推理循环，LLM 每步自主决策行为类型，包括：tool_call（直接执行工具）、plan（输出执行计划）、spawn（创建子进程）、complete（输出最终结果）、specialize（动态加载 Skill）、replan（修正计划）
- **FR113:** 统一推理循环每步仅调用一次 LLM，LLM 根据任务复杂度自主选择是否需要先规划（plan）再执行
- **FR114:** 系统提供 planning 配置开关（`planning: true/false`，默认 true），false 时 prompt 不注入 plan 指引，LLM 直接执行工具调用
- **FR115:** 统一推理循环内置熔断机制，连续 3 次工具调用失败时自动终止进程并报告错误
- **FR116:** 统一推理循环中工具调用错误必须以 tool message 格式注入 LLM 上下文，确保 LLM 可感知并调整策略
- **FR117:** 统一推理循环中智能体可自主决定 spawn 子智能体（任务式指挥），只下达意图不规定执行细节

## Stem Cell Differentiation（干细胞分化 + Skill 驱动特化，Phase 3）

- **FR118:** 系统提供通用基底智能体（Stem Agent），只包含最基础的推理能力和统一推理循环，不绑定任何特定 Skill
- **FR119:** 基底智能体可以根据接收到的意图（环境信号），自动匹配并加载最相关的 Skill 组合，完成分化过程——从通用体变为特定领域专家
- **FR120:** 分化过程支持渐进式特化：基底先加载核心 Skill 开始工作，执行过程中根据任务需要动态加载额外 Skill 进一步特化
- **FR121:** 分化后的智能体保持"表观遗传"记忆——同一基底在不同项目中分化为不同专家后，分化路径（哪些 Skill 被加载、加载顺序）被记录，下次相似意图可快速复现分化
- **FR122:** 系统维护分化谱系图（Lineage），记录从基底到特化体的完整分化路径，用户可以通过 `rnix lineage <pid>` 查看

## Token Economy + Contract SLA + Reputation（Token 经济 + 合约 SLA + 声誉系统，Phase 3）

- **FR123:** 系统为每个 Compose 编排分配总 token 预算池，各智能体从预算池中申请 token 配额，系统按优先级和任务关键度分配
- **FR124:** 当多个智能体竞争有限 token 预算时，系统通过价格信号机制调度——高优先级或关键路径上的智能体获得更多配额，低优先级任务被降级或排队
- **FR125:** 智能体之间的协作通过显式合约（Contract）约束，合约定义输入格式、输出质量标准、最大 token 消耗和超时时限
- **FR126:** 系统在合约执行完成后自动评估是否满足 SLA（输出质量、token 消耗、响应时间），评估结果记录到智能体模板的声誉分数
- **FR127:** 声誉系统跟踪每个 Agent 模板的历史表现（成功率、平均 token 效率、SLA 达标率），用户可以通过 `rnix reputation [agent]` 查看
- **FR128:** 系统在自动选择 Agent 模板时（如 Reconciler 分解任务时），优先选择声誉高的模板，实现"自然选择"——表现好的模板被更多使用

## Adaptive Immune Security（适应性免疫安全，Phase 3）

- **FR129:** 系统运行安全监控守护进程（Immune Daemon），持续监控所有智能体的行为模式（syscall 频率、资源访问模式、token 消耗速率）
- **FR130:** Immune Daemon 维护行为基线（Normal Profile），基于历史执行数据建立每种 Agent 模板的正常行为范围
- **FR131:** 当智能体行为偏离基线超过阈值时（如异常高频文件写入、未预期的 shell 命令模式），Immune Daemon 触发告警并可自动挂起（suspend）该进程
- **FR132:** 系统维护威胁记忆库（Antibody Memory），已识别的异常行为模式被记录，后续相同模式出现时立即拦截，无需重新检测
- **FR133:** 用户可以通过 `rnix immune status` 查看安全监控状态，包括当前告警、已挂起进程和威胁记忆库条目

## Neuroplasticity — Capability Migration（神经可塑性 — 能力迁移，Phase 3）

- **FR134:** 当 Compose 编排中某个智能体异常退出且 Supervisor 重启失败时，系统可以将其未完成的任务迁移到具有相似 Skill 的相邻智能体继续执行
- **FR135:** 系统维护能力相似度矩阵——基于 Skill 重叠度和历史协作记录，计算任意两个智能体之间的功能可替代性
- **FR136:** 高频协作路径（A 频繁 spawn B，B 频繁向 C 发送消息）被系统自动识别并记录为强化路径，在后续编排中优先复用
- **FR137:** 用户可以通过 `rnix topology` 查看智能体协作拓扑图，包含协作频率、能力重叠度和强化路径

## Skill Synergy Emergence（Skill 组合涌现，Phase 3）

- **FR138:** Skill 的 SKILL.md 可以声明 `synergy` 字段，定义与其他特定 Skill 同时加载时激活的涌现能力描述和额外指令
- **FR139:** 系统在智能体加载多个 Skill 时，自动检测已声明的 synergy 组合，将涌现指令追加到 system prompt 中
- **FR140:** 系统维护 Skill 组合矩阵，记录哪些 Skill 组合在历史执行中产生了显著优于单 Skill 的表现（基于声誉系统数据），用户可以通过 `rnix synergy list` 查看已知的有效组合
