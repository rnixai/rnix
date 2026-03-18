# Story 26.5: 文档更新——统一推理循环

Status: done

## Story

As a 平台构建者,
I want 更新所有文档使其与统一推理循环架构保持一致,
So that PRD、架构决策、project-context 和 CLAUDE.md 准确反映代码库的当前状态。

**FRs:** FR112-FR118（文档与代码对齐验证）

## Previous Story Context

### Story 26-1（已完成）
- 删除了 `kernel/ooda.go`（531 行）、所有 OODA 类型和测试文件
- 统一 Spawn 入口为 `k.reasonStep(proc, llmFD, opts)`
- 删除了 `SpawnOpts.ReasoningMode` 字段

### Story 26-2（已完成）
- ActionType 扩展到 7 种：text, tool_call, plan, spawn, complete, replan, specialize
- `AgentManifest.Reasoning string` → `Planning *bool`
- `linearToolProtocol` → `toolProtocol`，新增 `planProtocol`
- agents/loader 验证逻辑重写

### Story 26-3（已完成）
- ActionSpecialize stub 替换为完整实现（~125 行）
- TOCTOU 双重检查、DiffMemory 记录、Lineage 记录、AllowedDevices 更新
- 17 个 ATDD 测试覆盖

### Story 26-4（已完成）
- VFS flags 自动降级（`isEmpty → O_RDONLY`，否则 `O_RDWR`）
- 熔断机制（`consecutiveToolErrors >= 3` 触发 circuit_breaker 退出）
- 12 个统一推理循环测试矩阵（覆盖 9 个核心场景 + 3 个额外场景）
- `make all` 全部通过

### 代码变更总结

本 Story 无需修改任何 Go 代码。所有代码变更已在 Story 26-1 到 26-4 中完成。本 Story 纯文档更新。

## Acceptance Criteria (AC)

### AC-1: PRD 功能需求更新
**Given** `_bmad-output/planning-artifacts/prd/functional-requirements.md` 第 229-241 行存在 OODA 章节
**When** 重写 FR112-FR118
**Then** 章节标题从 "OODA Autonomous Decision（OODA 自主决策，Phase 3）" 改为 "Unified Reasoning Loop（统一推理循环，Phase 3）"
**And** FR112-FR117 重写为统一循环版本（按 Sprint Change Proposal §4.1 PRD-1 内容）
**And** FR118 中 "OODA 循环" 改为 "统一推理循环"

### AC-2: PRD 非功能需求更新
**Given** `_bmad-output/planning-artifacts/prd/non-functional-requirements.md` 第 91 行存在 NFR44
**When** 重写 NFR44
**Then** 从 "OODA 单轮循环（Observe→Orient→Decide→Act）的框架开销（不含 LLM）≤ 200ms" 改为 "统一推理循环单步框架开销（不含 LLM 调用时间）≤ 50ms"

### AC-3: PRD 项目范围更新
**Given** `_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md` 第 147 行存在 OODA 引用
**When** 更新 Phase 3 描述
**Then** "OODA 自主决策" 改为 "统一推理循环（Unified Reasoning Loop）"
**And** "Observe/Orient/Decide/Act 循环/reasoning:ooda/任务式指挥" 改为 "单一 reasonStep 循环/LLM 自主 action 选择/planning 配置/熔断机制"

### AC-4: PRD 索引更新
**Given** `_bmad-output/planning-artifacts/prd/index.md` 第 69 行存在 OODA TOC 条目
**When** 更新目录
**Then** 链接文本和锚点从 "OODA Autonomous Decision" 更新为 "Unified Reasoning Loop（统一推理循环）"

### AC-5: 架构决策文档——新增 Decision 23
**Given** `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md`
**When** 新增 Decision 23
**Then** 在 Decision 22（进程级配置快照）之后、"配置系统决策影响分析" 之前添加统一推理循环架构决策段落
**And** 内容按 Sprint Change Proposal §4.2 ARCH-1 定义

### AC-6: 架构验证结果更新
**Given** `_bmad-output/planning-artifacts/architecture/architecture-validation-results.md` 第 55 行
**When** 更新 OODA 引用
**Then** "OODA" 改为 "统一推理循环（Epic 26 已实现）"

### AC-7: project-context.md 推理循环段落重写
**Given** `_bmad-output/project-context.md` 中 "推理循环模式" 段落（约第 117-128 行）
**When** 重写段落
**Then** 删除双模式描述，替换为统一推理循环描述（按 Sprint Change Proposal §4.2 ARCH-2 内容）
**And** 包含 7 种 ActionType 说明
**And** 包含 planning 配置说明
**And** 包含内置安全机制（VFS flags 降级、错误注入、熔断）

### AC-8: CLAUDE.md 架构描述更新
**Given** 项目根目录 `CLAUDE.md`
**When** 更新推理循环相关描述
**Then** Process 描述中提及统一推理循环（而非 linear/OODA）
**And** 如有 OODA 相关内容则移除

### AC-9: 编译验证
**Given** 所有文档变更完成
**When** 运行 `make all`
**Then** lint + vet + test + build 全部通过（纯文档变更不应影响编译）

## Tasks / Subtasks

### Task 1: 重写 PRD 功能需求 [AC-1] [x]

修改 `_bmad-output/planning-artifacts/prd/functional-requirements.md`。

找到第 229-241 行的 OODA 章节，替换为：

```markdown
## Unified Reasoning Loop（统一推理循环，Phase 3）

- **FR112:** 系统提供统一推理循环，LLM 每步自主决策行为类型，包括：tool_call（直接执行工具）、plan（输出执行计划）、spawn（创建子进程）、complete（输出最终结果）、specialize（动态加载 Skill）、replan（修正计划）
- **FR113:** 统一推理循环每步仅调用一次 LLM，LLM 根据任务复杂度自主选择是否需要先规划（plan）再执行
- **FR114:** 系统提供 planning 配置开关（`planning: true/false`，默认 true），false 时 prompt 不注入 plan 指引，LLM 直接执行工具调用
- **FR115:** 统一推理循环内置熔断机制，连续 3 次工具调用失败时自动终止进程并报告错误
- **FR116:** 统一推理循环中工具调用错误必须以 tool message 格式注入 LLM 上下文，确保 LLM 可感知并调整策略
- **FR117:** 统一推理循环中智能体可自主决定 spawn 子智能体（任务式指挥），只下达意图不规定执行细节
```

然后找到 FR118（Stem Cell Differentiation 段落下），将：
```
- **FR118:** 系统提供通用基底智能体（Stem Agent），只包含最基础的推理能力和 OODA 循环，不绑定任何特定 Skill
```
替换为：
```
- **FR118:** 系统提供通用基底智能体（Stem Agent），只包含最基础的推理能力和统一推理循环，不绑定任何特定 Skill
```

### Task 2: 更新 PRD 非功能需求 [AC-2] [x]

修改 `_bmad-output/planning-artifacts/prd/non-functional-requirements.md`。

找到第 91 行，将：
```
- **NFR44:** OODA 单轮循环（Observe→Orient→Decide→Act）的框架开销（不含 LLM）≤ 200ms
```
替换为：
```
- **NFR44:** 统一推理循环单步框架开销（不含 LLM 调用时间）≤ 50ms
```

### Task 3: 更新 PRD 项目范围 [AC-3] [x]

修改 `_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md`。

找到第 147 行的 OODA 条目，将：
```
| OODA 自主决策 | Observe/Orient/Decide/Act 循环/reasoning:ooda/任务式指挥 | FR112-117 |
```
替换为：
```
| 统一推理循环 | 单一 reasonStep 循环/LLM 自主 action 选择/planning 配置/熔断机制 | FR112-117 |
```

### Task 4: 更新 PRD 索引 [AC-4] [x]

修改 `_bmad-output/planning-artifacts/prd/index.md`。

找到第 69 行，将：
```
    - [OODA Autonomous Decision（OODA 自主决策，Phase 3）](./functional-requirements.md#ooda-autonomous-decisionooda-自主决策phase-3)
```
替换为：
```
    - [Unified Reasoning Loop（统一推理循环，Phase 3）](./functional-requirements.md#unified-reasoning-loop统一推理循环phase-3)
```

### Task 5: 新增架构 Decision 23 [AC-5] [x]

修改 `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md`。

在 Decision 22（进程级配置快照，约第 1089 行）之后、"配置系统决策影响分析"（约第 1093 行）之前，插入：

```markdown
## Decision 23: 统一推理循环 — 废弃双模式，单一 reasonStep

**决策：** 废弃 linear/OODA 双推理模式，统一为单一 reasonStep 循环。LLM 每步自主选择行为（tool_call/plan/spawn/complete/specialize/replan），planning 作为可配置能力而非独立模式。

**理由：**
- OODA 双 LLM 调用导致语义漂移和 ~20s 性能损耗
- 人类硬编码 reasoning mode 选择不如 LLM 自主判断灵活
- 双代码路径增加维护负担，OODA 路径存在 bug 而 linear 路径无此问题
- 统一循环净删除 ~2000 行代码，减少代码复杂度

**配置：**
- `planning: true`（默认）— prompt 注入 plan 指引，LLM 可选择规划
- `planning: false` — prompt 不含 plan 指引，LLM 直接执行

**ActionType 枚举（7 种）：**

| ActionType | 说明 |
|------------|------|
| `text` | 纯文本输出（最终答案） |
| `tool_call` | 直接执行 VFS 工具调用 |
| `plan` | 输出执行计划，以 RoleAssistant 写入上下文 |
| `spawn` | 创建子进程（任务式指挥） |
| `complete` | 输出最终结果并退出（code=0） |
| `replan` | 修正当前计划 |
| `specialize` | 动态加载 Skill（Stem Cell 渐进式特化） |

**内置安全机制：**
- VFS flags 自动降级：空 payload 时 `O_RDONLY`，非空时 `O_RDWR`
- 工具错误以 tool message 注入 LLM 上下文
- 连续 3 次 tool_call/spawn 失败触发熔断退出（code=1）
- plan/replan/specialize 失败不计入熔断（可恢复逻辑错误）

**FR/NFR 覆盖：** FR8（扩展）、FR10（扩展）、FR112-FR118（重写）、NFR44（重写：≤50ms）
```

### Task 6: 更新架构验证结果 [AC-6] [x]

修改 `_bmad-output/planning-artifacts/architecture/architecture-validation-results.md`。

找到第 55 行，将 "OODA" 改为 "统一推理循环（Epic 26 已实现）"。

### Task 7: 重写 project-context.md 推理循环段落 [AC-7] [x]

修改 `_bmad-output/project-context.md`。

找到"推理循环模式"段落（约第 117-128 行），将整个段落替换为：

```markdown
#### 统一推理循环

单一 reasonStep 循环，LLM 每步自主决策行为类型：
- `tool_call` — 直接执行 VFS 工具调用
- `plan` — 输出执行计划，以 RoleAssistant 写入上下文（需 `planning: true`）
- `spawn` — 创建子进程（任务式指挥）
- `specialize` — 动态加载 Skill（Stem Cell 渐进式特化）
- `replan` — 修正当前计划
- `complete` — 输出最终结果并退出
- `text` — 纯文本输出（最终答案）

配置开关：`planning: true|false`（默认 true），false 时 prompt 不注入 plan 指引。planning 为 false 且 LLM 返回 plan 时，按 text 处理。

内置安全机制：
- VFS flags 自动降级：空 payload 时 O_RDONLY，写入时 O_RDWR
- 工具错误以 tool message 注入上下文，LLM 可感知并调整策略
- 连续 3 次 tool_call/spawn 失败触发熔断退出（code=1）
- specialize/plan/replan 失败不计入熔断（可恢复逻辑错误）
```

同时更新"上下文传播编码规范"中任何 "OODA specialize action" 引用为 "specialize action"。

### Task 8: 更新 CLAUDE.md [AC-8] [x]

修改项目根目录 `CLAUDE.md`。

当前 `CLAUDE.md` 中 Process 描述已经是通用的 `reasonStep` 描述，无需大改。检查是否有 OODA 相关文字，如有则移除。

确保 Process 描述与统一推理循环一致。可在 "Key Abstractions" 段落的 Process 描述后添加简要说明：

```
**Unified Reasoning Loop**: Single `reasonStep` loop where LLM autonomously selects action type per step: tool_call, plan, spawn, complete, specialize, replan, text. Planning is a configurable capability (`planning: true/false`, default true), not a separate mode.
```

### Task 9: 编译验证 [AC-9] [x]

```bash
make all
```

纯文档变更，不应影响编译。此步骤确保没有意外修改到 Go 源文件。

## Dev Notes

### 变更文件清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `_bmad-output/planning-artifacts/prd/functional-requirements.md` | 修改 | 重写 FR112-FR117 章节 + 更新 FR118 措辞 |
| `_bmad-output/planning-artifacts/prd/non-functional-requirements.md` | 修改 | 重写 NFR44 |
| `_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md` | 修改 | 更新 Phase 3 OODA 条目 |
| `_bmad-output/planning-artifacts/prd/index.md` | 修改 | 更新 TOC 链接 |
| `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md` | 修改 | 新增 Decision 23 段落 |
| `_bmad-output/planning-artifacts/architecture/architecture-validation-results.md` | 修改 | 更新 OODA 引用 |
| `_bmad-output/project-context.md` | 修改 | 重写推理循环模式段落 |
| `CLAUDE.md` | 修改 | 添加统一推理循环说明 |

### 不修改的文件（确认）

| 文件 | 原因 |
|------|------|
| `_bmad-output/planning-artifacts/epics/epic-list.md` | Epic 26 已在 L129-136 列出 |
| `_bmad-output/planning-artifacts/epics/index.md` | Epic 26 已在 L166-170 链接 |
| 任何 `*.go` 文件 | 纯文档 Story，所有代码变更在 26-1~26-4 完成 |
| `_bmad-output/planning-artifacts/architecture/project-context-analysis.md` | Epic 定义中未要求更新此文件 |

### Sprint Change Proposal 变更定义参考

本 Story 的所有文档变更内容严格遵循 Sprint Change Proposal §4 的定义：

| Proposal ID | 文件 | 操作 | 对应 Task |
|-------------|------|------|-----------|
| PRD-1 | functional-requirements.md | 重写 FR112-FR117 | Task 1 |
| PRD-2 | functional-requirements.md | 更新 FR118 | Task 1 |
| PRD-3 | non-functional-requirements.md | 重写 NFR44 | Task 2 |
| — | project-scoping-phased-development.md | 更新 OODA 引用 | Task 3 |
| — | prd/index.md | 更新 TOC | Task 4 |
| ARCH-1 | core-architectural-decisions.md | 新增 Decision 23 | Task 5 |
| — | architecture-validation-results.md | 更新 OODA 引用 | Task 6 |
| ARCH-2 | project-context.md | 重写推理循环段落 | Task 7 |
| — | CLAUDE.md | 更新架构描述 | Task 8 |

### 注意事项

1. **Markdown 锚点一致性**：更改 FR 章节标题后，`prd/index.md` 中的锚点链接必须同步更新。新的锚点格式为 `#unified-reasoning-loop统一推理循环phase-3`（GitHub/markdown 自动生成规则：小写 + 连字符）
2. **Decision 编号**：Decision 23 是新增的，紧跟 Decision 22 之后。当前文档中 Decision 13 是最后一个通用决策，Decision 14-22 属于配置系统重构专区。Decision 23 应插入在 Decision 22 之后、"配置系统决策影响分析" 段落之前
3. **project-context.md 的"Skills 热加载方式"条目**（约 L355）：当前写的是 "OODA specialize action 或 gdb 技能通过 SkillLoader 加载"，需要将 "OODA specialize action" 改为 "specialize action"
4. **不要遗漏 `project-context.md` 中的 `ooda.go` 文件引用**：在"子系统独立文件"列表（约 L319）中，`ooda.go` 已不存在，需要从列表中移除

### 执行顺序

1. Task 1-4: PRD 文件更新（可并行）
2. Task 5-6: 架构文件更新（可并行）
3. Task 7: project-context.md 更新
4. Task 8: CLAUDE.md 更新
5. Task 9: `make all` 验证

### 风险评估

- **极低风险**：纯 Markdown 文档修改，不影响任何代码编译或运行
- **唯一风险点**：Markdown 锚点不匹配导致文档内链接失效——通过 Task 4 的锚点同步更新缓解

### 关键引用

- Sprint Change Proposal §4.1（PRD-1/PRD-2/PRD-3）：定义了 FR/NFR 的精确替换内容
- Sprint Change Proposal §4.2（ARCH-1/ARCH-2）：定义了架构决策和 project-context 的精确替换内容
- Sprint Change Proposal §4.3（EPIC-1）：定义了 Epic 26 的 Story 分解和 Story 26.5 范围

## References

- Epic 定义：`_bmad-output/planning-artifacts/epics/epic-26-统一推理循环-unified-reasoning-loop.md`（Story 26.5 文档更新部分）
- Sprint Change Proposal：`_bmad-output/planning-artifacts/sprint-change-proposal-2026-03-18.md`（§4.1 PRD 变更 + §4.2 架构变更）
- 前序 Story 26-4：`_bmad-output/implementation-artifacts/26-4-test-rewrite-and-cleanup.md`
- 前序 Story 26-3：`_bmad-output/implementation-artifacts/26-3-specialize-capability-migration.md`

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

无（纯文档变更，无调试需求）

### Completion Notes List

- ✅ Task 1: 重写 functional-requirements.md FR112-FR117 章节标题从 OODA → Unified Reasoning Loop，6 条 FR 全部重写为统一推理循环版本
- ✅ Task 1: 更新 FR118 措辞 "OODA 循环" → "统一推理循环"
- ✅ Task 2: NFR44 从 "OODA 单轮循环≤200ms" 改为 "统一推理循环单步≤50ms"
- ✅ Task 3: project-scoping Phase 3 表格 OODA 条目替换为统一推理循环
- ✅ Task 4: prd/index.md TOC 锚点从 ooda-autonomous-decision 更新为 unified-reasoning-loop
- ✅ Task 5: core-architectural-decisions.md 新增 Decision 23（统一推理循环），含配置/ActionType/安全机制/FR-NFR 覆盖
- ✅ Task 6: architecture-validation-results.md "OODA" → "统一推理循环（Epic 26 已实现）"
- ✅ Task 7: project-context.md 推理循环段落从双模式重写为统一循环（7 种 ActionType + planning 配置 + 安全机制）
- ✅ Task 7: project-context.md 移除 ooda.go 文件引用（L319）、"OODA specialize action" → "specialize action"（L355）
- ✅ Task 8: CLAUDE.md Key Abstractions 新增 Unified Reasoning Loop 段落（CLAUDE.md 中无 OODA 残留）
- ✅ Task 9: `make all` 全部通过（lint 0 issues, vet 通过, 22 包测试通过, build 成功）

### File List

- `_bmad-output/planning-artifacts/prd/functional-requirements.md` — 重写 FR112-FR117 + 更新 FR118
- `_bmad-output/planning-artifacts/prd/non-functional-requirements.md` — 重写 NFR44
- `_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md` — 更新 Phase 3 OODA 条目
- `_bmad-output/planning-artifacts/prd/index.md` — 更新 TOC 链接和锚点
- `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md` — 新增 Decision 23
- `_bmad-output/planning-artifacts/architecture/architecture-validation-results.md` — 更新 OODA 引用
- `_bmad-output/project-context.md` — 重写推理循环段落 + 移除 ooda.go 引用 + 更新 specialize 措辞
- `CLAUDE.md` — 新增 Unified Reasoning Loop 说明
- `_bmad-output/implementation-artifacts/26-5-documentation-update.md` — story 文件本身
