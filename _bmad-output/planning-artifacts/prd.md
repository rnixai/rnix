---
stepsCompleted:
  - step-01-init
  - step-02-discovery
  - step-02b-vision
  - step-02c-executive-summary
  - step-03-success
  - step-04-journeys
  - step-05-domain
  - step-06-innovation
  - step-07-project-type
  - step-08-scoping
  - step-09-functional
  - step-10-nonfunctional
  - step-11-polish
inputDocuments:
  - '_bmad-output/planning-artifacts/product-brief-2026-02-23.md'
  - '_bmad-output/planning-artifacts/agent-os-architecture.md'
  - '_bmad-output/brainstorming/brainstorming-session-2026-02-23.md'
documentCounts:
  briefs: 1
  research: 0
  brainstorming: 1
  projectDocs: 1
classification:
  projectType: developer_tool
  domain: ai_infrastructure
  complexity: high
  projectContext: greenfield
workflowType: 'prd'
lastEdited: '2026-02-23'
editHistory:
  - date: '2026-02-23'
    changes: '基于验证报告修复 11 处问题：4 处实现泄露解耦（FR9/NFR11/NFR12/NFR20）、1 处 NFR 量化（NFR4）、4 处 FR 格式统一（FR13/FR38/FR39/FR40）、2 处可测量性增强（FR27/FR31）'
---

# Product Requirements Document - Crux

**Author:** Decker
**Date:** 2026-02-23
**Status:** Draft
**Version:** 1.0

## Executive Summary

**Crux** 是一个面向 AI 智能体的操作系统，用 Go 语言从零构建。它将智能体视为操作系统的一等计算单元，通过进程模型、虚拟文件系统、系统调用接口等 OS 级原语，解决当前多智能体系统的三大核心问题：调试黑盒、能力不可复用、多智能体协调困难。

当前所有主流框架（LangGraph、AutoGen、CrewAI、MetaGPT）都在应用层重复发明操作系统的功能——调度、隔离、文件抽象、权限控制。但应用层抽象存在天花板：能不断加功能，但加不出层次。Crux 不在应用层做编排，而是在 OS 层提供完整的原语支持，让构建生产级多智能体系统从"在应用层拼凑"变为"在正确的抽象层级自然完成"。

**目标用户：** 平台构建者（在 Crux 上构建基础设施和 Skill 包）和应用开发者（通过 AgentShell 和 Compose 组装多智能体应用）。

**实现语言：** Go（goroutine = 智能体进程，channel = IPC，interface = syscall 契约）。

**架构路线：** Gamma 混合——底层微内核保可靠性，上层涌现层释放创新潜力。

### What Makes This Special

**OS 级调试工具链（杀手级入口）：** `astrace` 追踪所有 syscall，将多智能体 bug 定位时间从"天级"降至"分钟级"。这是用户进门的钩子——因为调试黑盒是开发者在现有框架中最大的痛点，且没有任何现有框架提供 OS 级追踪能力。

**正确的抽象层级（留下的理由）：** 多智能体系统的问题不是"缺一个更好的框架"，而是"缺一个操作系统"。Crux 的进程模型、VFS 一切皆文件、Skills 共享库生态、45 个标准 syscall 构成了一个完整的 OS 范式——框架在应用层只能模拟这些能力，而 Crux 在 OS 层原生提供。

**时机：** 2024-2025 年多智能体框架百花齐放但全部撞上应用层天花板。行业正处于需要一个 OS 层统一抽象的临界点。

## Project Classification

| 维度 | 分类 |
|------|------|
| **项目类型** | 开发者工具 / 运行时框架 |
| **领域** | AI 基础设施 |
| **复杂度** | 高——范式级创新，涉及微内核、VFS、进程模型、syscall ABI 设计，加上 Skill 生态系统构建 |
| **项目状态** | Greenfield（全新项目） |

## Success Criteria

### User Success

**用户 A（平台构建者）：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 调试效率 | 定位多智能体 bug 的时间 | 从"天级"降至"分钟级" |
| 能力复用率 | 单个 Skill 被引用的项目数 | ≥ 3 个项目 |
| 上手门槛 | 安装到跑通第一个 demo | ≤ 15 分钟 |
| 顿悟时刻 | `astrace` 首次定位到真实问题 | 用户确认"这比翻日志快得多" |

**用户 B（应用开发者）：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 构建效率 | 完成多智能体工作流的代码量 | 比现有框架减少 90% |
| Skill 生态 | skillpkg 可用 Skill 数量 | 持续增长 |
| 上手门槛 | 安装到跑通 compose 模板 | ≤ 30 分钟 |

### Business Success

**北极星指标：GitHub Stars**（早期阶段，Stars 是最直接的社区认可和传播信号）

| 时间节点 | 目标 |
|---------|------|
| Phase 1 完成 | 自举成功——Crux 用自身 syscall 层分析自身源码，**能正确识别代码中真实存在的问题** |
| 6 个月 | 首次公开发布，README + demo 完整，接受外部 contributor |
| 12 个月 | Stars 作为社区认可度核心指标，支撑指标（demo 成功率、Skill 数量、Contributor 数量、外部引用）同步跟踪 |

### Technical Success

**功能性验收（MVP）：**

| 检查项 | 通过条件 |
|--------|---------|
| 进程生命周期 | spawn → running → zombie → dead 完整流转 |
| VFS 读写 | 通过 `/dev/fs` 读取宿主文件系统文件 |
| LLM 调用 | 通过 `/dev/llm/claude-sonnet` 完成推理 |
| Skill 加载 | `code-analyst` Skill 正确注入 system prompt |
| reasonStep 循环 | tool_call → 执行 → 追加结果 → 继续推理 → text → 完成 |
| astrace 追踪 | `crux astrace 1` 输出完整 syscall 链路（名称、耗时、token）|
| 自举验证 | 用 Crux 分析 Crux 自身源码，识别出真实存在的代码问题 |

**可靠性验收（MVP）：**

| 检查项 | 通过条件 |
|--------|---------|
| 基本稳定性 | 连续 20 次 spawn→完成路径，成功率 ≥ 95% |
| 进程状态一致性 | LLM API 超时/错误时，进程正确转入 Zombie 状态而非卡死 |
| 资源回收 | 进程退出后，goroutine 和 context 内存正确释放，无泄漏 |

### Measurable Outcomes

| 维度 | 核心可测量结果 |
|------|--------------|
| 自举 | Crux 分析自身源码 → 输出中包含至少 1 个可验证的真实代码问题 |
| 调试差异化 | `astrace` 输出的 syscall 链路能回溯到导致错误结果的具体步骤 |
| 端到端延迟 | 单智能体 spawn→完成（含 LLM 调用），≤ 30 秒 |

## User Journeys

### 旅程 1：陈明的调试顿悟（用户 A — 平台构建者，成功路径）

陈明又一次盯着终端发呆。他用 LangGraph 搭的 3 智能体代码审查系统上线两周了，其中一个智能体偶尔给出错误的审查意见——大概每 20 次出现一次。他翻了三天日志，在数千行对话记录中搜索"到底是哪一步推理出了问题"，但日志只有扁平的文本输出，没有因果链，没有上下文快照。他开始怀疑是不是该放弃这个项目。

然后他在 GitHub 上看到了 Crux。README 里的一句话抓住了他："astrace — 像 strace 一样追踪智能体的每一个 syscall"。他决定试试。

`go install` 安装 Crux。把他的代码审查逻辑迁移到一个 `code-analyst` Skill——写 `manifest.yaml` 定义工具依赖，写 `instructions.md` 注入审查策略。`crux "审查这段代码"` 启动第一个智能体。跑通了。

然后他复现了那个偶现 bug。这次，他运行 `crux astrace 1`。

终端输出了完整的 syscall 链路——每一步调用了什么（Open、Read、Write、CtxWrite），传了什么参数，返回了什么，花了多久。他立刻看到：在第 7 步，智能体通过 `/dev/fs` 读取了一个错误的文件路径——它把 `src/auth/login.go` 读成了 `src/auth/logout.go`。这个错误的文件内容被写入上下文，导致后续所有推理都偏了。

三分钟。从三天到三分钟。

**陈明的新日常：** 他开始用 Crux 重建他所有的多智能体项目。他写的 `db-migrator` Skill 被其他三个项目引用。他成了 skillpkg 的早期贡献者。

**旅程揭示的能力需求：**
- `go install` 级别的零配置安装体验
- Skill 编写流程（manifest.yaml + instructions.md）
- `crux spawn` 单命令启动
- `astrace` syscall 追踪输出（名称、参数、返回值、耗时）
- VFS `/dev/fs` 文件读取路径透明可见
- Skill 发布到 skillpkg 的流程

---

### 旅程 2：陈明遇到 LLM 超时（用户 A — 平台构建者，异常路径）

陈明在跑一个复杂的代码分析任务。智能体需要读取一个 500 行的文件并分析性能瓶颈。中途，Claude API 超时了。

他看终端：`[agent/3] error: /dev/llm/claude-sonnet: request timeout (30s)`。然后：`[kernel] PID 3 state: running → zombie (exit code: 1, reason: llm_timeout)`。

进程没有卡死。状态正确转入了 Zombie。他运行 `crux ps`，看到 PID 3 标记为 Zombie，等待 wait 回收。资源没有泄漏。

他重新运行 `crux "分析 ./src/scheduler.go"`，这次成功了。

**旅程揭示的能力需求：**
- LLM 驱动超时处理和错误上报
- 进程状态正确转移（不卡死在 Running）
- `crux ps` 进程状态查看
- Zombie 进程回收机制
- 清晰的错误信息（设备路径 + 错误原因）

---

### 旅程 3：林薇的 30 分钟工作流（用户 B — 应用开发者，成功路径）

林薇在 AI 初创公司负责产品开发。老板要一个"PR 提交后自动审查代码质量、生成变更文档"的流水线。她之前用 LangGraph 评估过——画有向图、写节点逻辑、处理状态传递，预估要两周。

同事推荐了 Crux。她打开 skillpkg：

```bash
skill install pr-reviewer code-analyst tech-writer
```

三个 Skill 装好了。她写了一个 `crux-compose.yaml`：

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
  writer:
    intent: "生成变更文档"
    skills: [tech-writer]
    depends_on:
      analyst: completed
```

`crux compose up`。20 行 YAML，三个社区 Skill，一个完整的 CI 审查流水线跑起来了。`crux top` 看到三个智能体按依赖顺序执行，token 消耗实时显示。

她把原来准备花两周写的 LangGraph 代码删了。

**旅程揭示的能力需求：**
- `skill install` 批量安装
- `crux-compose.yaml` 声明式编排
- `depends_on` 依赖管理
- `crux compose up` 一键启动
- `crux top` 实时监控（状态 + token）
- 社区 Skill 生态（可搜索、可安装、可组合）

---

### 旅程 4：林薇的调试时刻（用户 B — 应用开发者，排障路径）

林薇的 PR 审查流水线跑了一周，突然有一个 PR 的审查结果明显不对——把一个正确的函数标记为"有安全漏洞"。

她不熟悉 Crux 内核，但她知道怎么看日志。`crux log 5` 输出了 PID 5（reviewer 智能体）的推理日志，按 `[think]` / `[tool]` / `[output]` 分类。她看到 `[tool]` 部分：智能体读取了 PR diff，但 diff 内容被截断了——只读到了一半。截断后的代码看起来确实像有漏洞。

问题定位了。她调整了 Compose 配置，给 reviewer 加大了上下文预算。重跑，正常了。

**旅程揭示的能力需求：**
- `crux log <pid>` 分类推理日志
- 日志按 think/tool/output 结构化分类
- 上下文预算配置（compose.yaml 中可调）
- 无需深入内核就能排障的分层调试体验

---

### Journey Requirements Summary

| 能力领域 | 旅程来源 | MVP 必需 | Post-MVP |
|---------|---------|---------|----------|
| 安装体验（go install） | 旅程 1 | ✓ | |
| Skill 编写（manifest + instructions） | 旅程 1 | ✓ | |
| `crux spawn` 单命令 | 旅程 1, 2 | ✓ | |
| `astrace` syscall 追踪 | 旅程 1 | ✓ | |
| VFS `/dev/fs` 文件读取 | 旅程 1 | ✓ | |
| LLM 超时处理 + 进程状态正确转移 | 旅程 2 | ✓ | |
| `crux ps` 进程查看 | 旅程 2 | ✓ | |
| Zombie 回收 | 旅程 2 | ✓ | |
| `skill install` 包安装 | 旅程 3 | | ✓ (Phase 2) |
| `crux-compose.yaml` 编排 | 旅程 3 | | ✓ (Phase 2) |
| `crux top` 实时监控 | 旅程 3 | | ✓ (Phase 2) |
| `crux log` 分类日志 | 旅程 4 | | ✓ (Phase 2) |
| 上下文预算配置 | 旅程 4 | | ✓ (Phase 2) |
| skillpkg 社区 Skill 生态 | 旅程 3 | | ✓ (Phase 2) |

## Innovation & Novel Patterns

### Detected Innovation Areas

**范式级创新——Agent OS：** Crux 不是在现有多智能体框架上做增量改进，而是提出了一个全新范式：将智能体视为操作系统的一等计算单元。这对应 Unix 对计算机行业的影响——从"每个应用自建基础设施"到"OS 提供统一原语"。

**核心创新点：**

1. **智能体即进程：** spawn/kill/wait/signal 进程语义，进程树管理，生命周期状态机。现有框架没有这个抽象层。
2. **一切皆文件 VFS：** 工具 = `/dev/` 设备，MCP = `/mnt/mcp/` 挂载，智能体状态 = `/proc/` 文件。统一接口消除了工具/服务/状态的碎片化。
3. **OS 级调试（astrace）：** syscall 级追踪能力，在任何现有多智能体框架中都不存在。
4. **Skills 即共享库：** 领域知识包像 npm 包一样安装、版本管理、依赖解析——填补了 Tools（太底层）和 MCP（太外部）之间的空白。
5. **AgentShell DSL：** 类 Unix 语法操作智能体，管道组合 `spawn "分析" | spawn "写文档"` 取代硬编码编排。

### Validation Approach

**Phase 1 验证（自举）：** Crux 用自身 syscall 层分析自身源码并识别真实问题。这验证 OS 范式的核心可行性——智能体能否通过 OS 原语完成实际任务。

**公开发布前验证（待定）：** 比较验证推迟到有真实用户反馈时执行。早期阶段，自举成功 + astrace 调试体验 + 社区反馈是更可靠的验证信号。

### Risk Mitigation

详见 [Project Scoping & Phased Development > Risk Mitigation Strategy](#risk-mitigation-strategy)，其中包含完整的技术/市场/资源风险分析。

## Developer Tool Specific Requirements

### Project-Type Overview

Crux 是一个运行时框架而非传统库/SDK。开发者不写 Go 代码来使用 Crux——他们通过三个接口层与系统交互：

| 接口层 | 格式 | 用途 | 阶段 |
|--------|------|------|------|
| **AgentShell CLI** | 命令行 | `crux "意图"`、`crux astrace`、`crux ps` | MVP |
| **Skill 定义** | YAML + Markdown | `manifest.yaml` + `instructions.md` | MVP |
| **Agent Compose** | YAML | `crux-compose.yaml` 多智能体编排 | Phase 2 |
| **Go SDK（待定）** | Go | 嵌入式使用，根据用户反馈决策 | Phase 2+ |

### Installation & Distribution

| 方式 | 阶段 | 说明 |
|------|------|------|
| `go install` | MVP | 唯一安装方式，单二进制，零依赖 |
| 预编译二进制 / brew / docker | Phase 2+ | 根据社区需求扩展 |

**MVP 安装体验目标：** `go install github.com/decker/crux/cmd/crux@latest` → 可用。不需要配置文件、不需要额外依赖、不需要 Docker。

### API Surface (Syscall ABI)

Crux 的"API"不是 REST 端点或 Go 函数——而是 **~15 个核心 syscall**（MVP 阶段）：

**进程管理：** `Spawn`、`Kill`、`Wait`、`GetPID`、`PS`
**上下文管理：** `CtxAlloc`、`CtxRead`、`CtxWrite`、`CtxFree`
**文件系统：** `Open`、`Read`、`Write`、`Close`、`Stat`
**调试：** `DebugRecord`

这个 ABI 是 Crux 的"宪法"——一旦确立，所有上层组件围绕它构建。**MVP 阶段的 ABI 设计必须足够稳定，能支撑 Phase 2 扩展到 ~45 个 syscall 而不破坏向后兼容。**

### Documentation Strategy

| 文档类型 | 内容 | 阶段 |
|---------|------|------|
| **概念文档** | 为什么是 Agent OS、核心概念（进程、VFS、Skill、syscall）、与现有框架对比 | MVP |
| **快速上手** | 安装 → spawn 第一个智能体 → 看 astrace 输出（≤ 15 分钟目标） | MVP |
| **参考手册** | syscall 列表、VFS 路径规范、manifest.yaml 字段、CLI 命令 | MVP |
| **教程** | 写第一个 Skill、调试第一个 bug、组合多智能体 | Phase 2 |
| **架构文档** | 微内核设计、进程模型、驱动层、上下文管理 | Phase 2 |

### Code Examples & First Skill

MVP 交付一个完整的示例 Skill——`code-analyst`：

```
lib/skills/code-analyst/
├── manifest.yaml        # 元信息：工具依赖、模型偏好、上下文预算
└── instructions.md      # 核心指令：注入 system prompt
```

这个 Skill 同时承担三个角色：
1. **自举验证的载体**——用它来分析 Crux 自身源码
2. **Skill 格式的参考实现**——开发者照着它写自己的 Skill
3. **快速上手文档的素材**——demo 中直接使用

### Implementation Considerations

**Go 语言特性利用：**
- goroutine → 智能体进程（轻量、高并发）
- channel → IPC（类型安全、阻塞语义）
- interface → syscall 契约（编译时检查）
- 单二进制编译 → 零依赖部署

**不适用项（Skip）：**
- Visual design — CLI 工具，无 UI
- Store compliance — 不涉及应用商店
- IDE integration — MVP 阶段不需要，后续可考虑 LSP
- Migration guide — 全新范式，无迁移路径

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP 类型：** 平台验证型——验证"智能体即进程"OS 范式的核心可行性，而非面向最终用户的最小可用产品。

**Phase 1 用户：** 构建者自己（Decker）。先让自己能用，再让别人能用。

**MVP 设计原则：**
- 内核功能必须可靠，用户体验可以粗糙
- ABI 设计必须稳定到能支撑 Phase 2 扩展
- 自举验证是唯一的硬性验收标准

**资源需求：** 单人开发，Go 技术栈，依赖 Claude Code CLI。

### LLM Driver Strategy: Claude Code CLI

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
| `--system-prompt` | Skill `instructions.md` 注入 | MVP |
| `--tools` / `--allowedTools` | `/dev/` 设备权限，Skill manifest tools 声明 | MVP |
| `--model sonnet/haiku` | Skill manifest `models.preferred` | MVP |
| `--max-turns` | reasonStep 循环上限 | MVP |
| `--stream-json` | `astrace` 实时追踪数据源 | MVP |
| `--max-budget-usd` | Token 预算控制 | Phase 2 |
| `--mcp-config` | `/mnt/mcp/` 挂载实现 | Phase 2 |
| `--agents` | 多智能体子进程 spawn | Phase 2+ |

**安装前置条件：** Crux MVP 要求用户已安装 Claude Code CLI。

**架构影响：** Crux 内核专注于进程管理、VFS、上下文组装；LLM 交互（调用、工具执行、重试）全部委托给 Claude Code CLI。这大幅简化了 MVP 的 LLM 驱动层实现。

### MVP Feature Set (Phase 1)

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
| Skill 加载 | `skills/loader.go` | manifest.yaml → `--system-prompt` + `--tools` 参数映射 |
| 示例 Skill | `lib/skills/code-analyst/` | 自举验证载体 + 参考实现 |
| CLI 入口 | `cmd/crux/main.go` | `crux "意图"` + `crux astrace <pid>` + `crux ps` |
| astrace | `debug/astrace.go` | syscall 追踪（基于 `--stream-json` 实时数据） |

**MVP 实现的 ~15 个 syscall：**
详见 [API Surface (Syscall ABI)](#api-surface-syscall-abi) 中的完整列表。

**MVP 文档交付：**
- 概念文档（为什么是 Agent OS）
- 快速上手（安装 → spawn → astrace，≤ 15 分钟）
- 参考手册（syscall 列表、VFS 路径、manifest 字段、CLI 命令）

### Post-MVP Features

**Phase 2（能力栈建设）：**

| 能力 | 说明 |
|------|------|
| 完整 Tools/MCP/Skills 三层能力栈 | Tools `/dev/` + MCP `/mnt/mcp/`（复用 `--mcp-config`）+ Skills `/lib/skills/` |
| skillpkg 包管理器 | `skill install/search/update` + 社区仓库 |
| AgentShell 完整语法 | 管道、变量、脚本 |
| Agent Compose | `crux-compose.yaml` + `crux compose up` |
| 三级智能体模型 + IPC | 进程/线程/协程 + Send/Recv/Pipe |
| Supervisor 树 | Erlang OTP 式容错 |
| `crux top` / `crux log` | 实时监控 + 分类推理日志 |
| 上下文预算管理 | 复用 `--max-budget-usd` |
| init 引导系统 | 完整启动序列 |
| 教程 + 架构文档 | 文档体系补完 |

**Phase 3（涌现与智能）：**

| 能力 | 说明 |
|------|------|
| 声明式意图 + Reconciler | 用户声明"要什么"，控制器调和"怎么做" |
| OODA 自主决策 | 智能体内部感知-决策-行动循环 |
| 干细胞分化 | 通用基底智能体通过 Skill 自适应特化 |
| Token 经济 + 合约 SLA + 声誉系统 | 资源调度和协作治理 |
| 完整调试工具链 | agdb、时间旅行、分布式追踪、ctx-profiler |
| 可视化调试面板 | 智能体树 + 追踪时间线 + 上下文热力图 |

### Risk Mitigation Strategy

**技术风险：**

| 风险 | 缓解 |
|------|------|
| Claude Code CLI 接口变更破坏 Crux 驱动层 | 驱动层抽象隔离，CLI 交互封装在 `drivers/llm/claude.go` 单文件中，变更时只改一处 |
| reasonStep 循环与 CLI 交互不稳定 | 可靠性验收要求 20 次连续成功率 ≥ 95% |
| ABI 设计不够前瞻，Phase 2 需要破坏性变更 | MVP 的 15 个 syscall 严格遵循架构文档的 45 syscall 子集 |
| Go 单二进制对 Skill 动态加载的限制 | Skill 是文本注入（YAML + Markdown → `--system-prompt`），不是 Go plugin |

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

## Functional Requirements

### Agent Lifecycle Management（智能体生命周期管理）

- **FR1:** 用户可以通过自然语言意图创建（spawn）一个新的智能体进程
- **FR2:** 系统可以管理智能体进程的完整生命周期状态（Created → Running → Zombie → Dead）
- **FR3:** 用户可以终止（kill）一个正在运行的智能体进程
- **FR4:** 用户可以等待（wait）一个智能体进程完成并获取退出状态
- **FR5:** 系统可以在父进程退出后将孤儿进程重新挂载到 init（PID=1）
- **FR6:** 系统可以回收已完成的 Zombie 进程并释放其资源
- **FR7:** 用户可以查看所有活跃进程的列表及其状态（ps）

### Agent Reasoning（智能体推理）

- **FR8:** 系统可以驱动智能体执行推理循环（reasonStep），在 LLM 调用与工具执行之间交替直到任务完成
- **FR9:** 系统可以通过 LLM 驱动层以非交互模式调用 LLM 并获取结构化响应
- **FR10:** 系统可以解析 LLM 响应中的 action 类型（text 最终输出 / tool_call 工具调用 / spawn 创建子进程）
- **FR11:** 系统可以在 LLM 调用超时或失败时正确将进程状态转为 Zombie 并上报错误信息
- **FR12:** 系统可以将工具执行结果追加到智能体上下文中供后续推理使用

### File System & Resource Access（文件系统与资源访问）

- **FR13:** 系统可以提供统一的虚拟文件系统（VFS）接口（Open/Read/Write/Close/Stat）
- **FR14:** 系统可以通过 `/proc/{pid}/` 动态暴露每个智能体的运行时状态（status、intent、context）
- **FR15:** 系统可以通过 `/dev/` 路径注册和路由设备驱动（LLM、Shell、文件系统）
- **FR16:** 智能体可以通过 `/dev/fs` 读取宿主文件系统上的文件
- **FR17:** 智能体可以通过 `/dev/llm/claude-sonnet` 访问 LLM 推理能力
- **FR18:** 智能体可以通过 `/dev/shell` 执行宿主系统的 shell 命令

### Context Management（上下文管理）

- **FR19:** 系统可以为每个智能体分配独立的上下文空间（ctx_alloc）
- **FR20:** 系统可以读取和写入智能体上下文内容（ctx_read / ctx_write）
- **FR21:** 系统可以将上下文内容组装为完整的 LLM prompt（包含 system prompt + 对话历史 + 工具结果）
- **FR22:** 系统可以在进程退出后释放其上下文空间（ctx_free）

### Skill Management（Skill 能力管理）

- **FR23:** 系统可以从 `manifest.yaml` 读取 Skill 的元信息（名称、工具依赖、模型偏好、上下文预算）
- **FR24:** 系统可以从 `instructions.md` 读取 Skill 的核心指令并注入智能体的 system prompt
- **FR25:** 用户可以在 spawn 时指定加载一个或多个 Skill
- **FR26:** Skill 的 tools 声明可以映射为智能体的可用 `/dev/` 设备权限
- **FR27:** 系统交付一个完整的参考 Skill（code-analyst），能够分析代码并识别至少 1 个可验证的真实代码问题（与 Success Criteria 中自举验证标准对齐）

### Debugging & Observability（调试与可观测性）

- **FR28:** 用户可以通过 `astrace` 实时追踪指定智能体的所有 syscall 调用
- **FR29:** astrace 输出包含每个 syscall 的名称、参数、返回值和耗时
- **FR30:** 系统可以记录 syscall 调用数据（DebugRecord）供 astrace 消费
- **FR31:** 用户可以通过 astrace 输出定位到产生错误结果的具体 syscall 调用记录
- **FR32:** 系统在智能体完成时输出汇总信息（退出码、token 消耗、总耗时）

### Command Line Interface（命令行接口）

- **FR33:** 用户可以通过 `crux "意图"` 单命令启动一个智能体
- **FR34:** 用户可以通过 `crux astrace <pid>` 追踪指定进程的 syscall
- **FR35:** 用户可以通过 `crux ps` 查看所有进程状态
- **FR36:** CLI 提供清晰的错误信息，包含设备路径和错误原因
- **FR37:** 系统可以通过 `go install` 一条命令完成安装，单二进制，零额外依赖（需预装 Claude Code CLI）

### Documentation（文档）

- **FR38:** 系统可以提供概念文档，覆盖进程、VFS、Skill、syscall 四个核心概念，每个概念含定义和至少一个示例
- **FR39:** 系统可以提供快速上手指南，引导用户从安装到跑通第一个 demo（目标 ≤ 15 分钟）
- **FR40:** 系统可以提供参考手册，覆盖 syscall 列表、VFS 路径规范、manifest.yaml 字段、CLI 命令

## Non-Functional Requirements

### Performance

- **NFR1:** 单智能体 spawn→完成（含 LLM 调用），端到端延迟 ≤ 30 秒（简单任务如单文件分析）
- **NFR2:** `crux ps` 响应时间 ≤ 100ms（本地进程表查询，不涉及 LLM）
- **NFR3:** `astrace` 输出延迟 ≤ 500ms（从 syscall 发生到终端显示）
- **NFR4:** VFS 本地文件读取（`/dev/fs`）额外延迟 < 10ms，不超过直接文件 I/O 延迟的 2 倍
- **NFR5:** 上下文组装（ctx → prompt）时间 ≤ 1 秒（不含 LLM 调用本身）

### Reliability

- **NFR6:** 连续 20 次 spawn→完成路径，成功率 ≥ 95%
- **NFR7:** LLM API 超时/错误时，进程在 5 秒内正确转入 Zombie 状态，不卡死在 Running
- **NFR8:** 进程退出后，goroutine 和 context 内存在 10 秒内释放，无泄漏
- **NFR9:** 内核进程表在任意进程异常退出后保持一致性（无悬挂 PID、无状态不一致）
- **NFR10:** CLI 进程（crux 二进制本身）在智能体异常退出时不崩溃

### Integration

- **NFR11:** LLM 驱动层调用时，正确传递 system prompt、工具声明、模型选择、输出格式等参数
- **NFR12:** LLM 驱动层支持流式结构化输出模式，用于 astrace 实时数据采集
- **NFR13:** 宿主文件系统通过 `/dev/fs` 访问时，遵循宿主 OS 的文件权限（不绕过宿主权限模型）
- **NFR14:** Shell 驱动（`/dev/shell`）执行命令时，继承当前用户的环境变量和 PATH

### Security

- **NFR15:** `/dev/shell` 执行的命令继承当前用户权限，不提供额外提权能力
- **NFR16:** Skill 的 `manifest.yaml` 中 tools 声明作为智能体可访问设备的白名单——未声明的设备不可访问
- **NFR17:** MVP 阶段不实现完整 Capability 权限系统，但 Skill tools 白名单作为最小安全边界

### Maintainability（可维护性）

- **NFR18:** 内核代码遵循 Go 标准项目布局，通过 `go vet` 和 `golint` 无警告
- **NFR19:** syscall ABI 设计遵循 45 syscall 架构规范的子集，确保 Phase 2 扩展时向后兼容
- **NFR20:** LLM 驱动层封装在单一模块中，外部 LLM 接口变更时只需修改此模块
