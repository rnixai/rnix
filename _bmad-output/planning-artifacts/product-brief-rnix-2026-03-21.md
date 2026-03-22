---
stepsCompleted: [1, 2, 3, 4, 5]
inputDocuments:
  - '_bmad-output/brainstorming/brainstorming-session-2026-02-23.md'
  - '_bmad-output/planning-artifacts/product-brief-2026-02-23.md'
  - '_bmad-output/project-context.md'
  - '_bmad-output/planning-artifacts/research/technical-multi-llm-integration-research-2026-03-06.md'
  - '_bmad-output/planning-artifacts/research/technical-vfs-device-tooldef-architecture-research-2026-03-20.md'
date: '2026-03-21'
author: Decker
---

# Product Brief: Rnix — 统一观察系统 (Watch & Observability)

<!-- Content will be appended sequentially through collaborative workflow steps -->

## 执行摘要

Rnix 的调试与可观测性系统已具备完整的底层能力——strace 系统调用追踪、log 思维链日志、trace 分布式 Span 树、record 执行录制/回放、top 全局进程仪表板。但这些能力以**6 个孤立的专业命令**呈现，用户需要在多个终端间切换、手动查找 PID、记忆不同命令的语法，才能回答一个简单的问题："这个 agent 在干什么？"

本产品简报定义 Rnix **统一观察系统**的产品方案：通过引入 `watch` 融合视图和 `top` 下钻能力，将 6 个碎片化工具统一为一个**三层观察模型**（top → watch → strace/log/trace），让用户从全局鸟瞰到单进程深潜在一个连贯的操作流中完成。同时补齐 prompt 级别的可观测性盲区，实现从"agent 做了什么"到"agent 收到了什么指令"的完整因果链追踪。

---

## 核心愿景

### 问题陈述

Rnix 用户在观察 agent 行为时面临**三个断裂点**：

1. **时间窗口压力**：启动 agent 后，需要快速打开新终端、执行 `rnix ps` 找到 PID、再输入 `rnix strace <PID>`——越快完成的任务越难 attach。
2. **上下文切换**：从 top 看到异常进程后，必须退出 top、切终端、记 PID、输新命令——心流被打断。
3. **认知负担**：6 个命令各有不同语法和输出格式，用户需要知道"什么场景用哪个工具"——而实际场景中调试、监控、理解行为往往交织在一起，不是孤立的。

### 问题影响

这三个断裂点导致的直接后果是：**可观测性能力虽然存在，但使用体验停留在开发者工具的原型阶段，而非产品级体验。**

用户 A（平台构建者）在调试多 agent 系统时，把大量时间花在"找到正确的观察视角"而不是"分析问题"。用户 B（应用开发者）则可能完全不知道 strace/trace/record 的存在，只用 top 看个大概。

此外，还存在一个**架构级的可观测性盲区**：prompt（发送给 LLM 的完整指令）在 `kernel.go` 的 `reasonStep` 循环中被完整构建并发送，但 Write/Read 事件只记录字节数和元数据（`size`、`model`），**不记录内容**。这意味着用户能看到 agent "做了什么"（tool call）、"想了什么"（think log），但看不到 agent "收到了什么指令"（prompt）。当 agent 行为异常时，无法追溯其根因是否来自 prompt 构造。

### 为什么现有方案不够

| 现有命令 | 定位 | 不足 |
|---------|------|------|
| `top` | 全局进程仪表板 | 无法下钻到单进程详情 |
| `strace` | 系统调用追踪 | 不含 prompt 内容，需独立终端 |
| `log` | 思维链日志 | 不含 prompt 内容，与 strace 信息分离 |
| `trace/blame` | 事后分布式分析 | 仅事后使用，无实时能力 |
| `record` | 执行录制/回放 | 只记录响应摘要（500 字符截断），无完整 prompt |

**共同缺陷：** 每个工具都是单一视角的专业工具，缺少一个统一的日常入口。用户需要自己组装不同工具的输出来拼凑完整画面。

### 解决方案

**三层观察模型：**

```
Layer 1（全局）：top — "系统里有什么在跑？"
    │
    └── 选中进程 → 回车 → 下钻到 dashboard
            │
Layer 2（全景调试）：dashboard — "这个 agent 在做什么？"
    │           ├─ 智能体树窗格：进程状态全览
    │           ├─ 时间线窗格：syscall 事件流 + 三级详细度（v/V 键）
    │           ├─ 热力图窗格：上下文 token 分布
    │           ├─ p 键：查看完整 prompt
    │           └─ q 键：返回 top
    │
Layer 3（专业）：strace / log / trace / record
                 独立使用的深度工具
```

**核心产品设计：**

1. **`dashboard` 增强**（增强现有）— 在时间线窗格中增加三级详细度（摘要→展开→调试级含 prompt），复用已有的多窗格联动能力。
2. **`top` 下钻到 dashboard**（增强）— 在 top 中选中进程按回车跳转到 dashboard 并聚焦该进程，消除"切终端找 PID"的痛点。
3. **`spawn --dashboard`**（新增）— 启动即进入 dashboard 观察，零延迟 attach。
4. **Prompt 可观测性**（已实现底层能力）— 按需拉取模式：进程的 StepRecord 记录每步完整数据，GetStepDetail IPC 方法支持按需查看完整 prompt。不影响 DebugChan 的实时性能。
5. **`log --prompt`**（增强）— 在思维链日志中插入 prompt 摘要。
6. **`record --full`**（增强）— 可选记录完整 prompt，支持事后回放。

### 核心差异化优势

1. **htop 式连贯体验**：top → watch → 返回，一个入口逐层深入。不需要 6 个独立命令和多个终端。
2. **信息密度分层**：频率越高的操作密度越低（top 一行一进程），深度越深的操作密度越高（strace 完整 syscall）。用户永远不会被不该有的信息淹没。
3. **Prompt 因果链追踪**：从"agent 做了什么"到"agent 收到了什么指令"的完整链路，在所有多 agent 框架中独一无二。
4. **零改动向后兼容**：所有现有命令（strace/log/trace/record）保持原样，watch 是新增的融合层，不替代而是补充。

---

## 目标用户

### 主要用户

#### 用户 A：平台构建者 — 陈明

**画像：** 28岁，独立开发者。正在用 Rnix 构建一个 3-agent 协作的代码审查系统。他熟悉 strace 和 log 命令，但每次调试都像在打仗。

**问题体验：**
- 用 `rnix spawn` 启动 agent 后，急忙切到另一个终端输入 `rnix ps`，在列表中找到 PID，再输 `rnix strace 42`——agent 已经跑了一半
- 看 strace 发现某步调用了错误的工具，但不知道 agent 为什么做这个决定——想看 prompt，但 strace 只显示 `Write(fd=5, size=4532, model=haiku)`，完全看不到内容
- 切到 log 看思维链，和 strace 输出对不上时间线，需要人肉比对时间戳
- 想同时看全局进程状态和某个进程的详情，但 top 不能下钻，只能退出 top 再开 strace

**成功时刻：** 打开 `rnix top`，看到新 spawn 的 agent 出现在列表里，按回车进入 watch 视图，看到实时的每步摘要。第 3 步标红了——自动展开详情，看到它调用了错误的文件路径。按 `p` 查看 prompt，发现 system prompt 中的工作目录注入有误。**从发现问题到定位根因：2 分钟。**

#### 用户 B：应用开发者 — 林薇

**画像：** 32岁，全栈开发者。用 `rnix-compose.yaml` 组装了一个 PR 审查流水线，日常用 `rnix top` 监控运行状态。她不知道 strace、trace、record 的存在。

**问题体验：**
- `rnix compose up` 启动后开 top 看状态，看到某个 agent 的 TOKEN 列飙高但不知道为什么
- top 只显示 PID/状态/Token/耗时，想看更多细节，但不知道还有什么命令可以用
- 偶尔 agent 输出结果质量差，不知道该从哪里入手排查——是 prompt 的问题？还是工具调用的问题？

**成功时刻：** 在 top 中看到某 agent Token 异常高，按回车下钻到 watch 视图，看到每步的 token 消耗。第 5 步消耗了 3000 tokens——按 `v` 展开，发现它读了一个超大文件。**从"不知道怎么查"到"看到问题在哪"：30 秒。**

### 次要用户

#### Skill/驱动开发者

为 Rnix 编写新的 VFS 设备驱动或 Skill 的开发者。他们需要验证自己的驱动是否被正确调用、参数是否正确传递。watch 视图的详细模式让他们不需要学习 strace 语法就能看到设备调用的完整链路。

### 用户旅程

#### 用户 A（平台构建者）的旅程

| 阶段 | 行为 | 接触点 |
|------|------|--------|
| **日常监控** | 保持 `rnix top` 在一个终端窗口常开 | top TUI |
| **发现异常** | 在 top 中看到某进程状态异常或 Token 飙高 | top → 光标选中 |
| **下钻观察** | 按回车进入 watch 视图，看实时进度流 | top → watch |
| **定位问题** | 按 `v` 展开异常步骤详情，看参数和返回值 | watch Level 2 |
| **追溯根因** | 按 `p` 查看该步的 prompt，确认指令是否正确 | watch Level 3 |
| **深度调试** | 需要更底层信息时，退出 top，用 `strace` 或 `log` 单独查看 | strace / log |
| **回到全局** | 按 `q` 返回 top，继续监控其他进程 | watch → top |

#### 用户 B（应用开发者）的旅程

| 阶段 | 行为 | 接触点 |
|------|------|--------|
| **启动** | `rnix compose up` 启动流水线 | CLI |
| **监控** | 打开 `rnix top` 观察全局状态 | top TUI |
| **好奇** | 看到某进程在跑，想知道它在做什么，按回车 | top → watch |
| **顿悟** | 第一次在 watch 中看到 agent 的每步行为——"原来它是这样工作的！" | watch Level 1 |
| **排查** | 输出质量差时，在 watch 中按 `v` 看详情，快速定位哪步出了问题 | watch Level 2 |
| **成长** | 逐渐了解 strace/log 等专业工具，按需使用 | 渐进式发现 |

---

## 成功指标

### 用户成功指标

#### 用户 A（平台构建者）

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 异常定位时间 | 从发现异常到定位根因的耗时 | 从"天级"降至"分钟级"（≤5 分钟） |
| 操作步骤数 | 从启动 agent 到开始观察所需的操作数 | 从 4 步（切终端→ps→找PID→strace）降至 1 步（top 回车或 spawn --watch） |
| Prompt 可追溯性 | 能否查看任意步骤的完整 prompt | 100%（任意历史步骤均可按需查看） |

#### 用户 B（应用开发者）

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 可观测性门槛 | 不学习新命令即可深入观察 agent 行为 | 仅通过 top + 回车即可完成 90% 的观察需求 |
| 首次深入时间 | 从"只看 top"到"第一次在 watch 中发现有用信息" | ≤30 秒（按回车即可） |
| 工具发现率 | 用户是否知道可以查看更详细的信息 | watch 中的快捷键提示使功能自然被发现 |

### 业务目标

Rnix 是开源项目，业务目标以产品完整度和用户体验为核心：

| 时间节点 | 目标 |
|---------|------|
| **实现完成** | watch 命令 + top 下钻 + prompt 可观测性全部可用 |
| **自举验证** | 用 Rnix 自身的 watch 功能调试 Rnix agent，形成闭环 |
| **体验验证** | 用户 A 场景和用户 B 场景的"成功时刻"均可复现 |

### 关键绩效指标（KPI）

| KPI | 含义 | 衡量方式 |
|-----|------|---------|
| **观察覆盖率** | watch 覆盖的信息维度 | syscall + log + prompt 三通道全覆盖 |
| **零延迟 attach** | spawn --watch 的 attach 延迟 | ≤100ms（spawn 返回 PID 后立即开始） |
| **信息密度适配** | 不同层级的信息量是否合理 | Level 1 每步一行；Level 2 展开 ≤10 行；Level 3 按需拉取 |
| **向后兼容性** | 现有命令行为是否受影响 | strace/log/trace/record 行为完全不变，现有测试 100% 通过 |
| **IPC 方法覆盖** | 新增底层能力的完整性 | GetStepDetail IPC 方法可返回完整 prompt |

---

## MVP 范围

### 核心功能

**P0 — 必须交付（构成最小可用产品）：**

| 功能 | 说明 | 涉及模块 |
|------|------|---------|
| **dashboard 时间线三级详细度** | Level 1 每步一行摘要；Level 2 展开参数/结果；Level 3 含 prompt 摘要（v/V 键切换） | `cmd/rnix/dashboard.go` 增强 |
| **dashboard prompt 查看** | 时间线窗格中按 p 键查看选中步骤的完整 prompt 内容 | `cmd/rnix/dashboard.go` 增强 |
| **`top` 下钻到 dashboard** | top 中选中进程按回车跳转到 dashboard 并聚焦该进程 | `cmd/rnix/top.go` + `cmd/rnix/dashboard.go` |
| **`spawn --dashboard`** | 启动即进入 dashboard 观察，零延迟 attach | `cmd/rnix/main.go` spawn 命令扩展 |
| **StepRecord 自动记录** | 每步自动记录完整数据（已实现 27-1） | `kernel/` |
| **`GetStepDetail` IPC** | 按需拉取某步的完整 prompt 内容（已实现 27-2） | `ipc/` |

**P1 — 增强（MVP 后快速跟进）：**

| 功能 | 说明 |
|------|------|
| **`log --prompt`** | 在思维链日志中插入 prompt 摘要 |
| **时间线异常自动展开** | dashboard 时间线中出错或慢操作的步骤自动展开到 Level 2 |
| **格式化器统一** | 消除 `debug.FormatEvent()` 与 `ui.FormatTraceLine()` 的重叠 |

**P2 — 后续迭代：**

| 功能 | 说明 |
|------|------|
| **`record --full`** | 可选记录完整 prompt，支持事后回放时查看 |
| **时间线 token 消耗统计** | 每步显示增量 token，累计 token |

### MVP 明确排除

| 排除项 | 理由 |
|--------|------|
| 独立的 watch 命令 | 与 dashboard 功能重叠，已回滚（27-3/27-4/27-5） |
| 修改 strace/log 默认行为 | 保持向后兼容，只在 verbose 模式增强 |
| 修改 DebugChan/LogChan 协议 | prompt 走按需拉取，不走实时事件流，不影响性能 |
| 可视化 Web 面板 | 属于更远期的愿景，当前 TUI 满足需求 |
| trace/blame 的实时模式 | 事后分析和实时观察是不同的时间维度，不混合 |

### MVP 成功标准

| 检查项 | 通过条件 |
|--------|---------|
| dashboard 三级详细度 | 时间线窗格中 v/V 键正确切换 Level 1/2/3 |
| prompt 查看 | 时间线窗格中 p 键显示选中步骤的 prompt 摘要和全文 |
| top 下钻 | top 中选中进程按回车跳转到 dashboard |
| spawn --dashboard | `rnix spawn --dashboard "意图"` 启动后立即进入 dashboard |
| 向后兼容 | 现有测试包全部通过，strace/log/trace/record/dashboard 现有行为不变 |
| 自举验证 | 用 `rnix spawn --dashboard` 启动一个 agent 任务，在 dashboard 时间线中观察其完整生命周期并查看 prompt |

### 未来愿景

MVP 是 Rnix 可观测性体系从"专业工具集"到"产品级体验"的关键转折点。后续演进方向：

- **实时告警规则**：用户可定义异常检测规则（如 token 超阈值、连续失败），dashboard 自动高亮
- **dashboard 历史回看**：dashboard 不仅看实时流，也可以回看已完成进程的执行历史（已有离线回放基础）
- **跨进程关联视图**：在 compose DAG 场景中，dashboard 展示多进程间的因果关系
- **导出与分享**：将 dashboard 会话导出为可分享的 HTML 报告
- **从 ToolDefs 自动生成 toolProtocol**：消除 toolProtocol 硬编码，ToolDescriptor 作为单一事实来源
