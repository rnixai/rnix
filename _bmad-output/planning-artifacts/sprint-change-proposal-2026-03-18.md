# Sprint Change Proposal: 统一推理循环架构变更

> **日期：** 2026-03-18
> **提案人：** Scrum Master (bmad-correct-course)
> **触发来源：** Epic 20 交付后 strace 分析 + Party Mode 架构讨论
> **变更范围：** Major — 需架构师 + 开发者协同执行

---

## 1. 问题摘要

### 问题陈述

Epic 20（自主智能体 — OODA + Stem Cell）交付后，通过 strace 跟踪分析发现 OODA 推理循环存在 **6 个问题**（2 Critical / 2 High / 2 Medium）。在问题根因分析过程中，团队形成了更深层的架构决策：**废弃 linear/OODA 双推理模式，统一为单一推理循环**。

### 发现上下文

- **何时发现：** Epic 20 全部 Story（20.1-20.5）交付且 Epic 标记为 done 后
- **如何发现：** 使用 `rnix strace` 工具对 OODA 模式进程进行运行时跟踪
- **参与者：** Decker（产品负责人）、Winston（架构师）、John（PM）、Amelia（开发者）、Murat（测试架构师）

### 问题清单

| # | 问题 | 严重度 | 根因 |
|---|------|--------|------|
| 1 | Orient→Act 链路丢失子路径（空 /dev/fs） | Critical | LLM 在 Decide 阶段生成裸 `/dev/fs`，Orient 计划未忠实执行 |
| 2 | read-only 设备拒绝读操作（flags 问题） | Critical | `ooda.go:334` 和 `kernel.go:1256` 硬编码 `O_RDWR`，hostfs 要求 `O_RDONLY` |
| 3a | 工具错误未以 tool message 格式注入上下文 | High | `ooda.go:336` 错误路径不调用 `AppendToolResult` |
| 3b | 缺少重复失败熔断机制 | High | 仅有 `maxCycles` 限制，无连续错误计数 |
| 4 | Orient/Decide 两步 LLM 调用语义漂移 | Medium | 两次独立 LLM 调用不保证一致性 |
| 5 | 不支持并行 tool_call | Medium | 每 cycle 只处理单个 decision |
| 6 | 双 LLM 调用 ~20s 额外延迟 | Medium | 两阶段职责重叠造成性能浪费 |

### 核心洞察（Decker）

> "不应该分什么 linear 和 planexec，这个选择应该也是智能过程，由 LLM 根据任务智能选择需不需要 plan，还是直接执行。"

---

## 2. 影响分析

### 2.1 Epic 影响

#### Epic 20: 自主智能体（已完成 → 部分重构）

Epic 20 的 5 个 Story 受影响程度不同：

| Story | 影响 | 处理 |
|-------|------|------|
| 20.1 OODA 循环核心 | **完全替换** | 新 Story 实现统一 reasonStep |
| 20.2 OODA 配置与指挥 | **完全替换** | 新 Story 实现 planning 配置 |
| 20.3 Stem Agent 自动分化 | 保留核心，修改集成点 | 从 OODA 框架迁移到统一循环 |
| 20.4 渐进式特化 | 保留核心，修改集成点 | `oodaActSpecialize` → `ActionSpecialize` 分支 |
| 20.5 分化谱系图 | 保留，微调引用 | 去除 OODA 字样 |

#### 其他 Epic

- Epic 1-19, 21-25：**无直接影响**（OODA 是 Epic 20 内聚功能）
- 新增 **Epic 26: 统一推理循环**

### 2.2 PRD 文档影响

#### 需修改的功能需求（Functional Requirements）

**废弃 → 重写的 FR：**

| FR | 原内容 | 新内容 |
|----|--------|--------|
| FR112 | OODA Observe 阶段 | → 统一推理循环：LLM 每步自主决策，可选择 tool_call 直接执行 |
| FR113 | OODA Orient 阶段 | → 统一推理循环：LLM 可选择 plan 输出执行计划 |
| FR114 | OODA Decide 阶段 | → 统一推理循环：LLM 可选择 spawn/specialize/replan/complete |
| FR115 | OODA Act 阶段 | → 统一推理循环：每步仅一次 LLM 调用，结果直接执行 |
| FR116 | `reasoning: ooda` 配置 | → `planning: true/false` 可选配置（默认 true） |
| FR117 | OODA 任务式指挥 | → 统一循环中 `ActionSpawn` 实现任务式指挥（语义保留） |

**需修改的 FR：**

| FR | 修改内容 |
|----|---------|
| FR8 | 扩展 action 类型描述：text/tool_call/plan/spawn/complete/replan/specialize |
| FR10 | 扩展 action 解析类型列表 |
| FR118 | 移除"OODA 循环"措辞，改为"统一推理循环" |

**废弃的 NFR：**

| NFR | 原内容 | 处理 |
|-----|--------|------|
| NFR44 | OODA 框架开销 ≤200ms | 废弃 — 统一循环无独立框架开销概念，改为统一推理循环单步开销指标 |

**保留的 NFR：**

| NFR | 内容 | 理由 |
|-----|------|------|
| NFR45 | Skill 匹配 ≤3s | 不受推理模式变更影响 |

#### 需修改的 PRD 段落

| 文件 | 段落 | 操作 |
|------|------|------|
| `prd/functional-requirements.md` | §OODA Autonomous Decision | 重写为"Unified Reasoning Loop（统一推理循环）" |
| `prd/functional-requirements.md` | §Stem Cell Differentiation | 更新 FR118 措辞 |
| `prd/non-functional-requirements.md` | NFR44 | 重写为统一循环性能指标 |
| `prd/project-scoping-phased-development.md` | Phase 3 FR 范围描述 | 更新 OODA 引用 |
| `prd/index.md` | 目录条目 | 更新段落标题 |

### 2.3 架构文档影响

| 文件 | 改动 |
|------|------|
| `architecture/core-architectural-decisions.md` | 新增 Decision 23: 统一推理循环架构 |
| `architecture/architecture-validation-results.md` | 更新 OODA 相关验证条目 |
| `architecture/project-context-analysis.md` | 更新 Phase 3 概览 |
| `project-context.md` §推理循环模式 | **重写**（12 行 → 新的统一循环描述） |

### 2.4 代码影响

#### 需删除的文件（~2000 行）

| 文件 | 行数 | 内容 |
|------|------|------|
| `kernel/ooda.go` | 531 | OODA 全部类型、函数、常量、prompt 模板 |
| `kernel/ooda_test.go` | 819 | OODA 核心单元测试 |
| `kernel/ooda_reasoning_test.go` | 650 | OODA 推理配置测试 |
| `lib/agents/ooda-demo/` | 目录 | OODA 演示 agent |
| `agents/testdata/ooda-agent/` | 目录 | OODA 测试 agent |

#### 需修改的源文件

| 文件 | 改动 |
|------|------|
| `kernel/kernel.go` | 删除 OODA 分支，扩展 reasonStep：ActionPlan/ActionSpawn/ActionComplete/ActionSpecialize/ActionReplan 处理 + VFS flags 自动降级 + 熔断机制 |
| `kernel/process.go` | 删除 `oodaEnabled`/`oodaState`/`IsOODA()`/`GetOODAState()`/`SetOODAPhase()` |
| `agents/types.go` | `Reasoning string` → `Planning *bool` |
| `agents/loader.go` | 删除 reasoning 验证，添加 planning 字段处理 |
| `internal/types/types.go` | 删除 `LogOODA` 常量 |
| `cmd/rnix/lineage.go` | `"ooda-specialize"` → `"specialize"` |

#### 需修改的测试文件

| 文件 | 改动 |
|------|------|
| `kernel/stem_integration_test.go` | 删除 `reasoning: "ooda"` 和 `IsOODA()` 断言 |
| `kernel/diffmemory_test.go` | 更新 OODA 相关注释 |
| `kernel/diffmemory_integration_test.go` | 重写 specialize 测试（~30 处引用） |
| `kernel/lineage_integration_test.go` | 重写 specialize lineage 测试（~15 处引用） |
| `agents/loader_reasoning_test.go` | 重写为 planning 字段测试 |

#### 需修改的配置文件

| 文件 | 改动 |
|------|------|
| `lib/agents/stem/agent.yaml` | `reasoning: ooda` → `planning: true`（或删除，默认 true） |

### 2.5 UI/UX 影响

无直接影响。`rnix lineage` 命令保持不变，输出格式不变。

---

## 3. 推荐方案

### 选择：直接调整 — 创建新 Epic 实现统一推理循环

### 理由

1. **简化而非重构** — 从 2 条代码路径减少到 1 条，净删除 ~2000 行代码
2. **同步修复 Critical Bug** — VFS flags 降级和错误注入问题在统一循环中一并解决
3. **架构更优** — LLM 自主选择行为比人类硬编码模式选择更智能、更灵活
4. **性能提升** — 每步 1 次 LLM 调用（原 OODA 模式为 2 次），减少 ~20s 延迟
5. **零排期冲突** — 所有 25 个 Epic 已完成，无并行工作干扰

### 工作量评估

- **预估 Story 数：** 3-5 个
- **工作量级别：** 中等（主要是删除和迁移，非全新开发）
- **风险等级：** 低（简化操作，有完整的提案文档和代码位置速查）

### 权衡考量

| 维度 | 当前（不变更） | 变更后 |
|------|---------------|--------|
| 代码路径 | 2 条（需维护两套逻辑） | 1 条 |
| Bug 数 | 2 Critical + 4 Medium/High | 全部修复 |
| 每步 LLM 调用 | Linear 1 次 / OODA 2 次 | 统一 1 次 |
| 配置复杂度 | `reasoning: "linear|ooda"` | `planning: true|false`（可选） |
| 智能度 | 人类硬编码选择 | LLM 运行时自主判断 |

---

## 4. 详细变更提案

### 4.1 PRD 变更

#### 变更 PRD-1: 重写 OODA FR 段落

```
文件: prd/functional-requirements.md
段落: "OODA Autonomous Decision（OODA 自主决策，Phase 3）"

旧:
## OODA Autonomous Decision（OODA 自主决策，Phase 3）
- FR112: 智能体可以在推理循环中执行 OODA 感知阶段（Observe）...
- FR113: 智能体可以在推理循环中执行 OODA 判断阶段（Orient）...
- FR114: 智能体可以在推理循环中执行 OODA 决策阶段（Decide）...
- FR115: 智能体可以在推理循环中执行 OODA 行动阶段（Act）...
- FR116: 系统提供 OODA 模式的 reasonStep 扩展，智能体的 agent.yaml 可以声明 reasoning: ooda...
- FR117: OODA 模式下智能体可以自主决定 spawn 子智能体（任务式指挥）...

新:
## Unified Reasoning Loop（统一推理循环，Phase 3）
- FR112: 系统提供统一推理循环，LLM 每步自主决策行为类型，包括：tool_call（直接执行工具）、plan（输出执行计划）、spawn（创建子进程）、complete（输出最终结果）、specialize（动态加载 Skill）、replan（修正计划）
- FR113: 统一推理循环每步仅调用一次 LLM，LLM 根据任务复杂度自主选择是否需要先规划（plan）再执行
- FR114: 系统提供 planning 配置开关（planning: true/false，默认 true），false 时 prompt 不注入 plan 指引，LLM 直接执行工具调用
- FR115: 统一推理循环内置熔断机制，连续 3 次工具调用失败时自动终止进程并报告错误
- FR116: 统一推理循环中工具调用错误必须以 tool message 格式注入 LLM 上下文，确保 LLM 可感知并调整策略
- FR117: 统一推理循环中智能体可自主决定 spawn 子智能体（任务式指挥），只下达意图不规定执行细节

理由: OODA 四阶段循环的 2 次 LLM 调用导致语义漂移和性能损耗，统一为单次调用的推理循环更简洁高效
```

#### 变更 PRD-2: 更新 Stem Cell FR

```
文件: prd/functional-requirements.md
段落: "Stem Cell Differentiation"

旧:
- FR118: 系统提供通用基底智能体（Stem Agent），只包含最基础的推理能力和 OODA 循环，不绑定任何特定 Skill

新:
- FR118: 系统提供通用基底智能体（Stem Agent），只包含最基础的推理能力和统一推理循环，不绑定任何特定 Skill

理由: 措辞更新，反映架构变更
```

#### 变更 PRD-3: 更新 NFR44

```
文件: prd/non-functional-requirements.md

旧:
- NFR44: OODA 单轮循环（Observe→Orient→Decide→Act）的框架开销（不含 LLM）≤ 200ms

新:
- NFR44: 统一推理循环单步框架开销（不含 LLM 调用时间）≤ 50ms

理由: 统一循环每步仅需解析 JSON + 执行 action，无多阶段框架开销，性能指标相应调整
```

### 4.2 架构文档变更

#### 变更 ARCH-1: 新增 Decision 23

```
文件: architecture/core-architectural-decisions.md
操作: 新增段落

内容:
## Decision 23: 统一推理循环 — 废弃双模式，单一 reasonStep

**决策：** 废弃 linear/OODA 双推理模式，统一为单一 reasonStep 循环。
LLM 每步自主选择行为（tool_call/plan/spawn/complete/specialize/replan），
planning 作为可配置能力而非独立模式。

**理由：**
- OODA 双 LLM 调用导致语义漂移（问题 #4）和 ~20s 性能损耗（问题 #6）
- 人类硬编码 reasoning mode 选择不如 LLM 自主判断灵活
- 双代码路径增加维护负担，OODA 路径存在 bug（#1-#3）而 linear 路径无此问题
- 统一循环净删除 ~2000 行代码，减少代码复杂度

**配置：**
- `planning: true` (默认) — prompt 注入 plan 指引，LLM 可选择规划
- `planning: false` — prompt 不含 plan 指引，LLM 直接执行

**ActionType 枚举：**
text / tool_call / plan / spawn / complete / replan / specialize
```

#### 变更 ARCH-2: 重写 project-context.md 推理循环段落

```
文件: project-context.md
段落: "推理循环模式"

旧:
#### 推理循环模式
两种推理模式通过 SpawnOpts.ReasoningMode 选择：
- "" (线性模式，默认)：经典 reasonStep 循环...
- "ooda" (OODA 模式)：4 阶段循环 Observe → Orient → Decide → Act...

新:
#### 统一推理循环
单一 reasonStep 循环，LLM 每步自主决策行为类型：
- `tool_call` — 直接执行 VFS 工具调用
- `plan` — 输出执行计划，写入上下文
- `spawn` — 创建子进程（任务式指挥）
- `specialize` — 动态加载 Skill（Stem Cell 分化）
- `replan` — 修正当前计划
- `complete` — 输出最终结果并退出
- `text` — 纯文本输出（最终答案）

配置开关：`planning: true|false`（默认 true），false 时 prompt 不注入 plan 指引。

内置安全机制：
- VFS flags 自动降级：读取时 O_RDONLY，写入时 O_RDWR
- 工具错误以 tool message 注入上下文
- 连续 3 次工具失败触发熔断退出
```

### 4.3 Epic 变更

#### 变更 EPIC-1: 新建 Epic 26

```
Epic 26: 统一推理循环（Unified Reasoning Loop）

描述: 废弃 linear/OODA 双推理模式，统一为单一 reasonStep 循环。
修复 strace 分析发现的 6 个问题，迁移 Stem Cell 能力到统一循环。

预估 Story 分解:

Story 26.1: 统一 reasonStep 与 ActionType 扩展
- 删除 kernel/ooda.go（全文件）
- 删除 Process 的 OODA 字段
- 扩展 ActionType: plan/spawn/complete/replan/specialize
- 实现 VFS flags 自动降级（#2 修复）
- 实现工具错误 tool message 注入（#3a 修复）
- 实现熔断机制（#3b 修复）
- 统一 toolProtocol prompt 模板 + planProtocol 可选注入
- FRs: FR112-FR116 (重写版)

Story 26.2: Planning 配置与 Agent 适配
- agents/types.go: Reasoning string → Planning *bool
- agents/loader.go: 删除 reasoning 验证，添加 planning
- lib/agents/stem/agent.yaml: 更新配置
- 删除 lib/agents/ooda-demo/
- FR: FR114 (重写版)

Story 26.3: Specialize 能力迁移
- 将 oodaActSpecialize 逻辑迁移到统一 reasonStep 的 ActionSpecialize 分支
- 包含 skill 加载、AllowedDevices 更新、DiffMemory 记录、Lineage 记录
- 更新 diffmemory/lineage 集成测试
- FRs: FR120, FR121

Story 26.4: 测试重写与清理
- 删除 ooda_test.go、ooda_reasoning_test.go
- 删除 ooda-agent testdata
- 重写 stem_integration_test.go、diffmemory_integration_test.go、
  lineage_integration_test.go、loader_reasoning_test.go
- 新增统一循环测试矩阵（见提案 §6）

Story 26.5: 文档更新
- 更新 PRD FR112-FR118、NFR44
- 更新 architecture core-decisions（新增 Decision 23）
- 更新 project-context.md
- 更新 CLAUDE.md
- 更新 epics/epic-list.md 和 index.md
```

---

## 5. 实施交接

### 变更范围分类：**Major**

本变更涉及核心推理架构重构，需要架构师确认技术方案 + 开发者实施 + 测试验证。

### 交接计划

| 角色 | 职责 |
|------|------|
| **架构师** | 审批 Decision 23，确认统一循环 ActionType 设计和 prompt 模板 |
| **PM** | 审批 PRD FR 重写（FR112-FR118, NFR44）|
| **开发者** | 实施 Story 26.1-26.4（代码删除、迁移、新测试） |
| **技术文档** | 实施 Story 26.5（PRD、架构、project-context 更新） |
| **SM** | 更新 sprint-status.yaml，追踪 Epic 26 进度 |

### 成功标准

1. `make all` 全部通过（lint + vet + test + build）
2. 所有 20 个 Go 包测试通过（`-race` 检测）
3. `kernel/ooda.go` 及相关 OODA 文件完全删除
4. 统一循环测试矩阵 9 个场景全部覆盖
5. PRD、架构、project-context 文档与代码一致

---

## 附录: 检查清单完成状态

| Section | ID | 状态 |
|---------|-----|------|
| 1. 触发与上下文 | 1.1-1.3 | [x] Done |
| 2. Epic 影响 | 2.1 | [!] Action-needed → Epic 26 |
| 2. Epic 影响 | 2.2-2.5 | [x] Done |
| 3. 文档冲突 | 3.1 PRD | [!] Action-needed → FR112-118, NFR44 |
| 3. 文档冲突 | 3.2 架构 | [!] Action-needed → Decision 23, project-context |
| 3. 文档冲突 | 3.3 UX | [N/A] 无影响 |
| 3. 文档冲突 | 3.4 其他 | [!] Action-needed → sprint-status, CLAUDE.md |
| 4. 前进路径 | 4.1 直接调整 | [x] Viable（推荐）|
| 4. 前进路径 | 4.2 回滚 | [x] Not viable |
| 4. 前进路径 | 4.3 MVP 审查 | [N/A] |
| 4. 前进路径 | 4.4 推荐方案 | [x] Done — 方案 1 |
| 5. 提案组件 | 5.1-5.5 | [x] Done |
| 6. 最终审查 | 6.1-6.2 | [x] Done |
| 6. 最终审查 | 6.3 用户审批 | [x] Done — Decker 已批准 (2026-03-18) |
| 6. 最终审查 | 6.4 sprint-status | [x] Done — Epic 26 已添加 (backlog) |
| 6. 最终审查 | 6.5 交接确认 | [x] Done |
