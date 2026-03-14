# Epic 2: Agent 能力与文件访问（Agent Skills & File Access）

智能体可以通过 Agent 定义获得专业能力，Agent 引用的 Skill（遵循 Agent Skills 行业标准）决定工具权限和程序性知识——从"能说话"升级到"能干活"。

## Story 2.1: Agent 加载器与 Skill 加载器

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

## Story 2.2: 宿主文件系统驱动（/dev/fs）

As a 平台构建者,
I want 智能体通过 `/dev/fs` 设备读取宿主文件系统上的文件,
So that 智能体可以分析用户的源代码和文档。

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
**When** 在 `cmd/rnix/main.go` 中注册
**Then** `devRegistry.Register("/dev/fs", hostFSDriver.FileFactory())`

## Story 2.3: Shell 驱动（/dev/shell）

As a 平台构建者,
I want 智能体通过 `/dev/shell` 设备执行宿主系统的 shell 命令,
So that 智能体可以运行构建工具、检查环境、执行脚本。

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
**When** 在 `cmd/rnix/main.go` 中注册
**Then** `devRegistry.Register("/dev/shell", shellDriver.FileFactory())`

## Story 2.4: Agent 注入与设备权限白名单

As a 用户,
I want Spawn 时指定 Agent，系统自动加载 Agent 定义和引用的 Skill，注入 instructions 并限制设备访问范围,
So that 智能体获得身份和专业指令，同时只能访问 Skill 声明的设备。

**Acceptance Criteria:**

**Given** 用户执行 `rnix "分析代码" --agent=code-analyst`
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

## Story 2.5: code-analyst 参考 Agent 与 code-analysis 参考 Skill

As a 用户,
I want 一个预装的 code-analyst Agent 和 code-analysis Skill 作为参考实现,
So that 我可以立即使用 Rnix 分析代码并作为编写自定义 Agent/Skill 的模板。

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
**When** 执行 `rnix "分析 ./kernel/scheduler.go" --agent=code-analyst`
**Then** 智能体读取目标文件，进行分析，输出结构化的分析结果
**And** 能够识别至少 1 个可验证的真实代码问题（FR27）

**Given** `skills/testdata/mock-skill/` 和 `agents/testdata/mock-agent/` 已创建
**When** 运行 Agent/Skill 加载器测试
**Then** 使用 mock 数据作为测试 fixture，验证加载流程

---
