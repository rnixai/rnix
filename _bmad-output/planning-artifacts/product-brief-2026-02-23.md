---
stepsCompleted: [1, 2, 3, 4, 5]
inputDocuments:
  - '_bmad-output/brainstorming/brainstorming-session-2026-02-23.md'
  - '_bmad-output/planning-artifacts/agent-os-architecture.md'
  - '.meta/idea.md'
  - '.meta/readme.md'
  - '.meta/prompts.md'
  - '../docs/.meta/idea.md'
  - '../docs/.meta/tool.md'
date: '2026-02-23'
author: Decker
---

# Product Brief: Rnix

<!-- Content will be appended sequentially through collaborative workflow steps -->

## 执行摘要

**Rnix** 是一个面向 AI 智能体的操作系统，用 Go 语言从零构建。它将智能体视为操作系统的一等计算单元，通过进程模型、虚拟文件系统、系统调用接口等 OS 级原语，从根本上解决当前多智能体系统的三大难题：调试黑盒、能力不可复用、多智能体协调困难。

当前所有主流框架（LangGraph、AutoGen、CrewAI、MetaGPT）都在应用层重新发明操作系统的功能——进程调度、内存管理、文件抽象、权限控制。Rnix 不在应用层做编排，而是在 OS 层提供完整的原语支持，让构建生产级多智能体系统从"困难"变为"自然"。

**核心定位：** AI 时代的 Unix。

**实现语言：** Go（goroutine = 智能体进程，channel = IPC，interface = syscall 契约）

**架构路线：** Gamma 混合——底层微内核保可靠性，上层涌现层释放创新潜力。

---

## 核心愿景

### 问题陈述

构建生产级多智能体系统极其困难。开发者面临三个核心痛点：

1. **调试黑盒**：多个智能体协作时，无法追踪因果链——不知道"哪个智能体在什么时候做了什么错误决定导致了最终的失败"。现有框架的调试手段仅限于打日志和看终端输出，面对跨智能体的推理链路追踪完全无能为力。

2. **能力不可复用**：一个智能体的能力（prompt、工具配置、领域知识）和它的定义深度耦合。想把"代码审查"能力复用到另一个项目，只能复制粘贴并重写。没有包管理，没有版本控制，没有依赖解析。

3. **多智能体协调困难**：现有框架依赖硬编码的 graph/workflow 或中央编排器。预规划太刚性，无法适应运行时的变化；中央编排器成为单点瓶颈和故障点。

### 问题影响

这三个问题导致的直接后果是：**多智能体系统停留在 demo 阶段，无法进入生产环境。**

开发者花 80% 的时间在调试和重写上，而不是构建业务逻辑。团队之间无法共享智能体能力，每个项目都从零开始。当智能体数量超过 3-5 个时，协调复杂度急剧上升，系统变得不可维护。

这与 1970 年代计算机行业面临的问题高度同构——每个团队都在应用层构建自己的"操作系统"功能，重复造轮子，无法形成生态。

### 为什么现有方案不够

| 框架 | 调试 | 能力复用 | 协调 | 根本局限 |
|------|------|---------|------|---------|
| **LangGraph** | 图节点日志 | 无机制 | 硬编码有向图 | 扁平工具列表，无层次抽象 |
| **AutoGen** | 对话日志 | 无机制 | 对话轮次驱动 | 能力和角色深度耦合 |
| **CrewAI** | 任务日志 | 角色模板（浅层）| 预定义流程 | 无运行时适应能力 |
| **MetaGPT** | SOP 日志 | SOP 模板 | 刚性 SOP | 预规划不可变更 |

**共同缺陷：** 所有框架都在应用层重新发明进程管理、内存管理、文件抽象和权限控制，但都做得不完整、不统一、不可组合。

### 解决方案

**Rnix 从 OS 层提供智能体作为一等计算单元的完整原语支持：**

- **智能体即进程**：spawn/kill/wait/signal，进程树管理，goroutine 实现三级模型（进程/线程/协程）
- **一切皆文件**：工具 = `/dev/` 设备，MCP 服务 = `/mnt/mcp/` 挂载，智能体状态 = `/proc/` 文件
- **Skills 即共享库**：`skill install` 安装领域知识包，`skill_load` 动态加载，Synergy 涌现组合
- **45 个标准 syscall**：统一的 ABI 契约，所有上层组件围绕这个契约构建
- **声明式意图**：用户描述"要什么"，控制器调和循环负责"怎么做"，智能体内部 OODA 自主决策

### 核心差异化优势

1. **OS 级调试工具链**（解决调试黑盒）：`strace` 追踪所有 syscall，`gdb` 交互式断点调试，时间旅行回放支持"如果当时做了不同决定会怎样"的 what-if 分析。**这在任何现有多智能体框架中都不存在。**

2. **三层能力栈 + Skills 生态**（解决能力复用）：Tools（原子能力 `/dev/`）→ MCP（外部服务 `/mnt/mcp/`）→ Skills（领域知识 `/lib/skills/`）。Skills 像 npm 包一样安装、版本管理、依赖解析。**能力从一次性的 prompt 复制变为可积累的共享资产。**

3. **进程模型 + 管道组合**（解决协调困难）：智能体通过 Unix 管道组合——`spawn "分析代码" | spawn "写文档"`。不需要预定义 graph，不需要中央编排器。**协调从"硬编码"变为"即兴组合"。**

4. **时机优势**：2024-2025 年多智能体框架百花齐放，但都在应用层做编排——行业正处于"需要一个统一 OS 层抽象"的临界点。Rnix 就是这个 Unix 时刻。

---

## 目标用户

### 主要用户

#### 用户 A：平台构建者

**画像：** 陈明，28岁，独立开发者。之前用 LangGraph 搭过一个3智能体的代码审查系统，花了两周写编排逻辑，上线后一个智能体偶尔给出错误审查意见，他花了三天翻日志都没定位到原因，最终放弃了这个项目。

**角色定位：** 在 Rnix 上构建基础设施和 Skill 包。他写 Skill 的 `manifest.yaml` 和 `instructions.md`，定义 Tool 驱动，配置 MCP 挂载，构建可复用的能力模块。

**日常场景：**
- 发现团队需要一个"数据库迁移"能力，于是编写 `db-migrator` Skill，发布到 skillpkg
- 用 `strace` 追踪某个智能体的 syscall 链路，发现它在第3步调用了错误的 Tool
- 用 `gdb` 设断点，在智能体做出关键决策前暂停，检查上下文内容

**当前痛点：**
- 用现有框架构建多智能体系统，80% 时间在调试和写胶水代码
- 写好的 prompt 和工具配置无法跨项目复用，每次从零开始
- 智能体出错时完全是黑盒，只能靠猜

**成功时刻：** `rnix strace 42` 一条命令，立刻看到智能体的完整决策链——"原来是在第7步读了错误的文件导致后续推理全偏了"。从三天缩短到三分钟。

---

#### 用户 B：应用开发者

**画像：** 林薇，32岁，全栈开发者，在一家 AI 初创公司负责产品开发。她不关心 Rnix 内核怎么实现的，她只想快速组装一个能用的多智能体系统来解决业务问题。

**角色定位：** 通过 AgentShell 和 Agent Compose 使用现成的 Skill 组装多智能体应用。她写 `rnix-compose.yaml`，从 skillpkg 安装 Skill，用管道组合智能体，但不会深入到内核或驱动层。

**日常场景：**
- `skill install pr-reviewer code-analyst tech-writer` 安装三个 Skill
- 写一个 `rnix-compose.yaml` 定义"PR 提交后自动审查、分析代码质量、生成变更文档"的流水线
- `rnix compose up` 一键启动，`rnix top` 监控运行状态
- 出问题时看 `rnix log 42`，根据 `[think]/[tool]/[output]` 分类快速定位

**当前痛点：**
- 想构建一个多智能体工作流，但 LangGraph 的 graph 定义太复杂
- 找到一个好用的 prompt，没法直接装到另一个项目里
- 不同框架的智能体无法互操作

**成功时刻：** 写了一个 20 行的 `rnix-compose.yaml`，安装了 3 个社区 Skill，`rnix compose up` 跑起来一个完整的 CI 审查流水线——从零到可用不到 30 分钟。

---

### 次要用户

#### 用户 C：最终用户

不直接接触 Rnix。他们使用 B 类开发者构建的应用（如 AI 代码审查服务、自动化文档生成工具等）。Rnix 对他们完全透明——他们感受到的是更快的响应、更准确的结果和更可靠的服务。

---

### 用户旅程

#### 用户 A 的旅程（平台构建者）

| 阶段 | 行为 | 接触点 |
|------|------|--------|
| **发现** | 在 GitHub/技术博客上看到 Rnix，被"智能体即进程"和 OS 级调试吸引 | GitHub README、技术文章 |
| **上手** | `go install` 安装，跑通第一个 `rnix spawn "hello"` 命令 | AgentShell、README |
| **核心使用** | 编写 Skill，用 `strace`/`gdb` 调试，发布到 skillpkg | AgentShell、VFS、调试工具链 |
| **顿悟时刻** | 第一次用 `strace` 在 3 分钟内定位到一个之前要花 3 天的 bug | `strace`、`gdb` |
| **长期** | 成为 Skill 生态贡献者，构建的 Skill 被社区广泛使用 | skillpkg、社区 |

#### 用户 B 的旅程（应用开发者）

| 阶段 | 行为 | 接触点 |
|------|------|--------|
| **发现** | 看到 A 类用户分享的 Skill 或 Compose 模板，意识到可以快速组装系统 | 社区、博客 |
| **上手** | `skill install` 几个 Skill，复制一个 compose 模板，`rnix compose up` 跑通 | skillpkg、Agent Compose |
| **核心使用** | 编写 `rnix-compose.yaml`，用管道组合智能体，`rnix top` 监控 | Agent Compose、AgentShell |
| **顿悟时刻** | 20 行 YAML + 3 个 Skill = 一个完整的多智能体工作流，替代了之前 2000 行的 LangGraph 代码 | Agent Compose |
| **长期** | 积累自己的 Compose 模板库，团队标准化在 Rnix 上构建 AI 应用 | Compose 模板、团队工作流 |

---

## 成功指标

### 用户成功指标

#### 用户 A（平台构建者）

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 调试效率提升 | 定位多智能体 bug 的时间 | 从"天级"降至"分钟级" |
| 能力复用率 | 单个 Skill 被多少项目使用 | Skill 发布后被 ≥3 个项目引用 |
| 上手门槛 | 从安装到跑通第一个 demo | ≤ 15 分钟 |

#### 用户 B（应用开发者）

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 构建效率 | 完成一个多智能体工作流的代码量 | 比现有框架减少 90%（2000 行 → 20 行 YAML）|
| Skill 生态丰富度 | skillpkg 可用 Skill 数量 | 社区贡献的 Skill 持续增长 |
| 上手门槛 | 从安装到跑通 compose 模板 | ≤ 30 分钟 |

---

### 业务目标（开源项目）

Rnix 是纯开源项目，业务目标以社区影响力和生态健康为核心：

| 时间节点 | 目标 |
|---------|------|
| **Phase 1 完成** | 自举成功——Rnix 能用自身 syscall 层完成一个 Rnix 开发任务 |
| **6 个月** | 首次公开发布，README + demo 完整，接受外部 contributor |
| **12 个月** | GitHub stars 作为核心北极星指标，代表社区认可度和传播力 |

---

### 关键绩效指标（KPI）

**北极星指标：GitHub Stars**

Stars 是开源项目最直接的社区认可信号——它衡量的是"有多少开发者认为这个项目值得关注"。

**支撑指标（驱动 Stars 增长的先行指标）：**

| KPI | 含义 | 为什么重要 |
|-----|------|-----------|
| **自举完成度** | Rnix 能用自身完成多少类型的开发任务 | Phase 1 的硬性验收标准，证明系统可用 |
| **首次 demo 成功率** | 新用户 clone 后能跑通 demo 的比例 | 决定第一印象，直接影响 star 转化 |
| **Skill 包数量** | skillpkg 上可安装的 Skill 数 | 生态丰富度，B 类用户留存的关键 |
| **Contributor 数量** | 提交过 PR 的外部开发者数 | 社区健康度，项目可持续性的基础 |
| **技术博客/讨论引用** | 外部文章、HN/Reddit 讨论中提及 Rnix 的次数 | 传播力，Stars 增长的先行指标 |

---

## MVP 范围

### 核心功能

**Phase 1 竖切片：内核奠基 + 最小调试能力**

MVP 目标是实现 Rnix 的最小可运行内核，验证"智能体即进程"的核心假设，并通过 `strace` 展示 OS 级调试的差异化优势。

**1. 微内核（3 个文件）**

| 组件 | 说明 |
|------|------|
| `kernel.go` | Kernel 结构体 + `Spawn()` + `reasonStep()` 心跳循环 |
| `process.go` | Process 结构体 + 生命周期（Created → Running → Zombie → Dead）|
| `reap.go` | 进程回收（wait + 孤儿进程 reparent to init）|

**2. VFS 框架（3 个文件）**

| 组件 | 说明 |
|------|------|
| `vfs.go` | VFS 接口定义（Open/Read/Write/Close/Stat）|
| `proc.go` | `/proc/{pid}/` 动态文件系统（status、intent、context）|
| `dev.go` | `/dev/` 设备注册与路由 |

**3. 驱动层（2 个文件）**

| 组件 | 说明 |
|------|------|
| `drivers/llm/claude.go` | Claude API 驱动，挂载为 `/dev/llm/claude-sonnet` |
| `drivers/shell/shell.go` | Shell 执行驱动，挂载为 `/dev/shell` |

**4. 上下文管理（1 个文件）**

| 组件 | 说明 |
|------|------|
| `context/context.go` | Context 分配/读写/组装 prompt（ctx_alloc/ctx_read/ctx_write）|

**5. Skill 加载器（1 个文件 + 1 个示例 Skill）**

| 组件 | 说明 |
|------|------|
| `skills/loader.go` | Skill 加载（读取 manifest.yaml + instructions.md 注入 system prompt）|
| `lib/skills/code-analyst/` | 第一个 Skill：代码分析（manifest.yaml + instructions.md）|

**6. CLI 入口（1 个文件）**

| 组件 | 说明 |
|------|------|
| `cmd/rnix/main.go` | AgentShell MVP——仅支持 `rnix "意图"` 单命令 spawn |

**7. 最小调试工具——strace（1 个文件）**

| 组件 | 说明 |
|------|------|
| `debug/strace.go` | 最小 syscall 追踪：拦截并打印所有 syscall 调用（名称、参数、返回值、耗时），支持 `rnix strace <pid>` 命令 |

`strace` 是 Rnix 与所有现有框架的核心差异点。即使在 MVP 中，它也必须能让用户看到智能体的完整 syscall 链路——这是用户 A 的"顿悟时刻"。

**MVP 实现的核心 syscall（~15 个）：**

- **进程：** `Spawn`、`Kill`、`Wait`、`GetPID`、`PS`
- **上下文：** `CtxAlloc`、`CtxRead`、`CtxWrite`、`CtxFree`
- **文件：** `Open`、`Read`、`Write`、`Close`、`Stat`
- **调试：** `DebugRecord`（strace 数据采集）

### MVP 明确排除

| 排除项 | 推迟到 | 理由 |
|--------|--------|------|
| Capability 权限检查 | Phase 2 | MVP 阶段所有操作直接放行，降低复杂度 |
| Token 预算管理 | Phase 3 | 涌现层特性，依赖 Token 经济模型 |
| 进程间通信（IPC） | Phase 2 | MVP 只需单进程 spawn→完成路径 |
| 上下文 swap 换出 | Phase 2 | MVP 阶段上下文窗口足够，无需冷存储 |
| skillpkg 包管理 | Phase 2 | MVP 手动放置 Skill 文件，不需要包管理器 |
| AgentShell 完整语法 | Phase 2 | MVP 仅 `rnix "意图"` 和 `rnix strace <pid>` |
| gdb 交互式调试器 | Phase 2+ | 依赖完整的 debug syscall 集合 |
| 时间旅行调试 | Phase 3 | 依赖 DebugRecord + CtxSnapshot + CoW |
| 声明式意图 / Reconciler | Phase 3 | 涌现层特性 |
| Agent Compose | Phase 2 | 依赖 Skill 系统 + 进程模型完整 |
| MCP 挂载 | Phase 2 | 依赖 VFS mount 系统完整实现 |
| 管道组合 | Phase 2 | 依赖 IPC + Pipe syscall |

### MVP 成功标准

**硬性验收标准：自举验证**

Rnix 能用自身的 syscall 层完成一个 Rnix 开发任务。具体场景：

```bash
$ rnix "分析 ./kernel/scheduler.go 并找出性能瓶颈"

[kernel] spawning PID 1...
[agent/1] loading skill: code-analyst
[agent/1] reading /dev/fs/kernel/scheduler.go...
[agent/1] reasoning step 1/3...
══ 分析结果 ══════════════════════════════
发现 2 个性能瓶颈：
1. scheduler.go:47 — 全局锁，建议分片锁
2. scheduler.go:89 — O(n) 线性扫描，建议改堆
══════════════════════════════════════════
[kernel] PID 1 exited(0) | tokens: 1,847 | elapsed: 6.2s
```

**验收检查清单：**

| 检查项 | 通过条件 |
|--------|---------|
| 进程生命周期 | spawn → running → zombie → dead 完整流转 |
| VFS 读写 | 通过 `/dev/fs` 读取宿主文件系统文件 |
| LLM 调用 | 通过 `/dev/llm/claude-sonnet` 完成推理 |
| Skill 加载 | `code-analyst` Skill 正确注入 system prompt |
| reasonStep 循环 | tool_call → 执行 → 追加结果 → 继续推理 → text → 完成 |
| strace 追踪 | `rnix strace 1` 输出完整 syscall 链路（名称、耗时、token） |
| 自举 | 用 Rnix 分析 Rnix 自身源码并给出有意义的结果 |

### 未来愿景

MVP 是 Rnix 三阶段路线图的起点：

**Phase 2：能力栈建设** — 从"能跑一个智能体"到"能编排多个智能体"
- 完整的 Tools/MCP/Skills 三层能力栈
- skillpkg 包管理器 + 社区仓库
- AgentShell 完整语法（管道、变量、脚本）
- Agent Compose 控制器
- 三级智能体模型 + IPC + Supervisor 树

**Phase 3：涌现与智能** — 从"能编排"到"能自主"
- 声明式意图 + Reconciler 控制器
- OODA 自主决策 + 干细胞分化
- Token 经济 + 合约 SLA + 声誉系统
- 完整调试工具链（gdb、时间旅行、分布式追踪、ctx-profiler）
- 可视化调试面板

**终极愿景：** Rnix 成为 AI 时代的 Unix——一个开发者构建多智能体系统时的默认底层，一个 Skill 生态蓬勃发展的平台，一个让"构建生产级多智能体系统"从"极其困难"变为"自然而然"的操作系统。
