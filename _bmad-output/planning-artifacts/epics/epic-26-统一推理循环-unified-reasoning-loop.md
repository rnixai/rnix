# Epic 26: 统一推理循环（Unified Reasoning Loop）

废弃 linear/OODA 双推理模式，统一为单一 `reasonStep` 循环。LLM 每步自主决策行为类型（tool_call/plan/spawn/complete/specialize/replan），planning 作为可配置能力而非独立模式。同步修复 strace 分析发现的 6 个问题（2 Critical + 2 High + 2 Medium）。

> **设计基础**
>
> 本 Epic 基于以下规划文档：
>
> | 文档 | 说明 |
> |------|------|
> | [统一推理循环提案](../unified-reasoning-loop-proposal.md) | Party Mode 多智能体讨论产出 |
> | [Sprint Change Proposal 2026-03-18](../sprint-change-proposal-2026-03-18.md) | 航向修正评估与审批 |
> | [PRD FR112-FR118（重写版）](../prd/functional-requirements.md#unified-reasoning-loop统一推理循环phase-3) | 统一推理循环功能需求 |
> | [Architecture Decision 23](../architecture/core-architectural-decisions.md#decision-23-统一推理循环) | 统一推理循环架构决策 |
>
> **变更背景：** Epic 20 交付后 strace 分析发现 OODA 推理循环存在 6 个问题，其中 2 个为阻塞性 bug（VFS flags 硬编码、错误未注入上下文）。团队决策废弃双模式架构，统一为单一推理循环。

**架构决策：** Decision 23（统一推理循环）
**FRs covered:** FR8（扩展）, FR10（扩展）, FR112-FR118（重写）
**NFRs:** NFR44（重写：统一循环单步框架开销 ≤50ms）, NFR45（保留）

---

## Story 26.1: OODA 代码删除与统一分支入口

As a 平台构建者,
I want 删除所有 OODA 相关代码并统一推理循环入口为单一 `reasonStep`,
So that 代码库只有一条推理路径，消除双模式维护负担。

**FRs:** 无（纯删除，为后续 Story 清理地基）

**Acceptance Criteria:**

**Given** `kernel/ooda.go` 文件存在（~531 行）
**When** 执行删除
**Then** 整个文件被删除，包含所有 OODA 类型定义（`OODAPhase`、`OODAState`、`OODADecision`、`OODAActionType`）、prompt 模板、`oodaReasonStep`、`oodaAct`、`oodaActToolCall`、`oodaActSpawn`、`oodaActSpecialize`、`oodaCallLLM` 函数

**Given** `kernel/ooda_test.go`（~819 行）和 `kernel/ooda_reasoning_test.go`（~650 行）存在
**When** 执行删除
**Then** 两个测试文件完全删除

**Given** `lib/agents/ooda-demo/` 目录存在
**When** 执行删除
**Then** 目录及其中 `agent.yaml` 和 `instructions.md` 完全删除

**Given** `agents/testdata/ooda-agent/` 目录存在
**When** 执行删除
**Then** 目录及其中 `agent.yaml` 和 `instructions.md` 完全删除

**Given** `kernel/process.go` 中存在 OODA 相关字段和方法
**When** 清理 Process 结构体
**Then** 删除 `oodaEnabled bool` 字段（原 process.go:89）
**And** 删除 `oodaState *OODAState` 字段（原 process.go:90）
**And** 删除 `IsOODA()` 方法
**And** 删除 `GetOODAState()` 方法
**And** 删除 `SetOODAPhase()` 方法

**Given** `kernel/kernel.go` 中 Spawn 方法存在 OODA 分支（原 kernel.go:611-628）
**When** 统一分支入口
**Then** 删除 `if opts.ReasoningMode == "ooda"` 条件块（~6 行）
**And** 删除 `if proc.oodaEnabled` 分支（~4 行）
**And** 所有进程统一走 `k.reasonStep(proc, llmFD, opts)`
**And** `SpawnOpts.ReasoningMode` 字段删除

**Given** `internal/types/types.go` 中存在 `LogOODA` 常量
**When** 清理类型定义
**Then** 删除 `LogOODA` 常量

**Given** `cmd/rnix/main.go` 中存在 OODA 相关注释
**When** 清理注释
**Then** 删除所有 OODA 相关注释

**Given** 所有删除完成
**When** 运行 `go build ./cmd/rnix/`
**Then** 编译成功，零错误
**And** 运行 `go vet ./...` 无警告

**Technical Notes:**
- 删除文件：`kernel/ooda.go`、`kernel/ooda_test.go`、`kernel/ooda_reasoning_test.go`
- 删除目录：`lib/agents/ooda-demo/`、`agents/testdata/ooda-agent/`
- 修改文件：`kernel/process.go`（删除 OODA 字段和方法）、`kernel/kernel.go`（删除 OODA 分支）、`internal/types/types.go`（删除 LogOODA）、`cmd/rnix/main.go`（删除注释）
- 此 Story 完成后代码必须能编译通过——暂时丢失 specialize/spawn/complete 能力（Story 26.2 恢复）
- 编译时如有其他文件引用已删除符号，一并修复（如 `stem_integration_test.go` 中的 `IsOODA()` 断言）

---

## Story 26.2: ActionType 扩展与统一 Prompt 模板

As a 平台构建者,
I want 统一推理循环支持 7 种 action 类型（text/tool_call/plan/spawn/complete/replan/specialize），LLM 每步自主选择行为,
So that 智能体能力不受限于预设模式，由 LLM 根据任务复杂度智能决策。

**FRs:** FR112, FR113, FR114, FR116, FR117

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中 `ActionType` 定义
**When** 扩展 ActionType 常量
**Then** 包含以下 7 种类型：

```go
const (
    ActionText       ActionType = "text"
    ActionToolCall   ActionType = "tool_call"
    ActionPlan       ActionType = "plan"
    ActionSpawn      ActionType = "spawn"
    ActionComplete   ActionType = "complete"
    ActionReplan     ActionType = "replan"
    ActionSpecialize ActionType = "specialize"
)
```

**Given** `kernel/kernel.go` 中 `linearToolProtocol` 常量
**When** 重命名并更新为统一 prompt 模板
**Then** 重命名为 `toolProtocol`（删除 "linear" 前缀）
**And** 保留现有 tool_call 协议内容不变
**And** 新增 `spawn`/`complete`/`replan`/`specialize` 的 action 格式说明
**And** prompt 模板中 `spawn` 格式：`{"action": "spawn", "tool": "intent text", "data": {"agent": "name"}}`
**And** prompt 模板中 `complete` 格式：`{"action": "complete", "tool": "", "data": {"result": "..."}}`
**And** prompt 模板中 `specialize` 格式：`{"action": "specialize", "tool": "skill-name", "data": {}}`

**Given** `agents/types.go` 中 `AgentManifest` 结构体
**When** 将 `Reasoning string` 替换为 `Planning *bool`
**Then** 字段定义为 `Planning *bool \`yaml:"planning,omitempty"\``
**And** `nil` 表示未设置（等价于 `true`），`*true` 显式启用，`*false` 显式禁用

**Given** `agents/loader.go` 中 reasoning 验证逻辑
**When** 替换为 planning 字段处理
**Then** 删除原有 `reasoning` 字段验证（"must be empty, linear, or ooda"）
**And** `Planning` 字段无需验证（`*bool` 天然只能是 nil/true/false）
**And** `agents/testdata/invalid-reasoning/` 目录可以删除或改为 planning 相关测试

**Given** planning 配置为 true（默认）
**When** Spawn 方法构建 system prompt
**Then** 在 `toolProtocol` 之后额外注入 `planProtocol` 模板：

```
To plan before executing (for complex multi-step tasks):
{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "..."}}

Use planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.
```

**Given** planning 配置为 false
**When** Spawn 方法构建 system prompt
**Then** 仅注入 `toolProtocol`，不注入 `planProtocol`

**Given** `reasonStep` 中 LLM 返回 `ActionPlan`
**When** 解析并处理 plan action
**Then** 以 `RoleAssistant` 将 plan 内容写入上下文（格式：`[Plan]\n{steps JSON}`）
**And** 不使用 `RoleUser`（架构评审要求：plan 是 LLM 自己生成的输出）
**And** 继续下一步循环，LLM 可见 plan 内容并按计划执行

**Given** planning 为 false 且 LLM 仍返回 `ActionPlan`
**When** 解析 action
**Then** 按 `ActionText` 处理——将 plan 内容作为最终输出文本
**And** 不静默忽略，不按 replan 处理（架构评审要求）

**Given** `reasonStep` 中 LLM 返回 `ActionSpawn`
**When** 解析 spawn action
**Then** 从 `action.ToolData` 解析可选的 `{"agent": "name", "model": "model"}` 参数
**And** 通过 `k.agentLoader` 加载 agent（如指定）
**And** 调用 `k.Spawn(intent, agentInfo, childOpts)` 创建子进程
**And** 等待子进程完成，结果以 tool message 写入上下文
**And** 支持 TraceID/SpanID 传播（从原 `oodaActSpawn` 迁移）

**Given** `reasonStep` 中 LLM 返回 `ActionComplete`
**When** 解析 complete action
**Then** 设置 `proc.Result` 为 complete 的 result 内容
**And** 调用 `k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})`
**And** 循环退出

**Given** `reasonStep` 中 LLM 返回 `ActionReplan`
**When** 解析 replan action
**Then** 以 `RoleAssistant` 将 replan 原因写入上下文（格式：`[Replan] {reason}`）
**And** 继续下一步循环

**Given** `lib/agents/stem/agent.yaml` 中 `reasoning: ooda`
**When** 更新为新配置格式
**Then** 删除 `reasoning: ooda` 行
**And** 可选添加 `planning: true`（或直接删除此行，默认即为 true）

**Given** 统一循环中 `parseAction` 函数
**When** 解析 LLM JSON 响应
**Then** 根据 `action` 字段值分派到对应 ActionType
**And** 无法解析为 JSON 时按 `ActionText` 处理（兼容纯文本最终答案）

**Given** 所有 ActionType 处理完成
**When** 运行 `go build ./cmd/rnix/`
**Then** 编译成功
**And** 运行 `go vet ./...` 无警告

**Technical Notes:**
- 修改文件：`kernel/kernel.go`（ActionType 常量 + toolProtocol/planProtocol 模板 + reasonStep 中 6 个新 action 分支 + parseAction 扩展）
- 修改文件：`agents/types.go`（Reasoning→Planning）、`agents/loader.go`（验证逻辑）
- 修改文件：`lib/agents/stem/agent.yaml`（配置格式更新）
- parseAction 的 JSON 解析复用现有 `parsedAction` 结构体，扩展字段
- spawn 子进程的等待逻辑从 `oodaActSpawn` 迁移，保留 TraceID 传播和 parent context 取消检查
- plan 写入上下文的 role 必须是 RoleAssistant（不是 RoleUser），这是与 OODA replan 实现的关键差异

---

## Story 26.3a: VFS Flags 自动降级

As a 平台构建者,
I want tool_call 的 VFS Open 操作根据 payload 自动选择 flags（读取用 O_RDONLY，写入用 O_RDWR）,
So that `/dev/fs` 读取操作不再报 permission denied。

**FRs:** FR116（部分）

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中 tool_call 的 `vfs.Open` 调用（原 kernel.go:1256 硬编码 `vfs.O_RDWR`）
**When** 修复 VFS flags 自动降级
**Then** 对 tool_call action 的 Open 操作根据 payload 内容选择 flags：
- `len(action.ToolData) == 0` 或 `string(action.ToolData) == "{}"`：使用 `vfs.O_RDONLY`
- 其他情况：使用 `vfs.O_RDWR`
**And** `/dev/fs/path/to/file` 读取操作（payload 为空或 `{}`）不再报 `permission denied`
**And** `/dev/fs/path/to/file` 写入操作（payload 含 `content`）使用 `O_RDWR` 正常工作

**Given** `/dev/fs/path` 读取操作的 flags 降级测试
**When** 使用 `O_RDONLY` 打开 hostfs 设备
**Then** 读取成功，不触发 `drivers/fs/hostfs.go:82` 的 flags 检查错误

**Technical Notes:**
- 修改文件：`kernel/kernel.go`（VFS Open flags 逻辑）
- flags 降级判断：`isEmpty := len(action.ToolData) == 0 || string(action.ToolData) == "{}"`——两种空 payload 用 `O_RDONLY`，其余 `O_RDWR`

---

## Story 26.3b: 工具错误注入上下文

As a 平台构建者,
I want 所有 tool_call 的 Open/Write/Read 错误以 tool message 格式注入 LLM 上下文,
So that LLM 可感知错误并在下一步调整策略。

**FRs:** FR116

**Acceptance Criteria:**

**Given** `reasonStep` 中任何 tool_call 的 Open/Write/Read 失败
**When** 错误发生
**Then** 所有错误路径调用 `k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg)`
**And** 错误以 `role: "tool"` 格式注入 LLM 上下文
**And** LLM 在下一步可感知错误并调整策略
**And** 这与当前 linear 模式 `kernel.go:1262-1271` 的正确实现保持一致

**Technical Notes:**
- 修改文件：`kernel/kernel.go`（错误注入路径）
- 错误注入路径确保与现有 linear 模式一致（kernel.go:1262-1271 是正确模板）

---

## Story 26.3c: 熔断机制

As a 平台构建者,
I want 连续 3 次 tool_call/spawn 失败时自动终止进程,
So that 智能体不会陷入无限错误循环。

**FRs:** FR115

**Acceptance Criteria:**

**Given** `reasonStep` 中新增 `consecutiveToolErrors int` 计数器
**When** tool_call 执行成功
**Then** `consecutiveToolErrors` 重置为 0

**Given** tool_call 或 spawn 执行失败
**When** `consecutiveToolErrors` 递增
**Then** 计数器 +1
**And** spawn 失败同样计入（架构评审要求：子进程创建失败也是资源性错误）
**And** plan/replan/specialize 失败**不计入**（可恢复的逻辑错误）

**Given** `consecutiveToolErrors >= 3`
**When** 达到熔断阈值
**Then** 调用 `k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})`
**And** 进程正确转入 Zombie 状态
**And** 产生 `ReasonStep` 事件，包含 `action: "circuit_breaker"` 和 `consecutive_errors: 3`

**Given** 使用 mock LLM 和 mock VFS 设备的单元测试
**When** 模拟连续 3 次 tool_call 失败
**Then** 进程触发熔断退出
**And** exit code 为 1，reason 包含 "circuit_breaker"

**Given** 使用 mock LLM 的单元测试
**When** 模拟 2 次失败后 1 次成功
**Then** 计数器重置，进程继续正常运行

**Given** 统一循环单步框架开销测试
**When** 使用 mock LLM 即时返回运行性能基准测试
**Then** 单步纯框架代码开销（不含 LLM 调用时间）≤ 50ms（NFR44 重写版）
**And** 通过 `go test -race` 无数据竞争

**Technical Notes:**
- 修改文件：`kernel/kernel.go`（consecutiveToolErrors 变量 + 熔断逻辑）
- 熔断计数范围：tool_call 失败 + spawn 失败；plan/replan/specialize 失败不计入
- NFR44 性能指标从 200ms（OODA 框架开销）调整为 50ms（统一循环单步开销）

---

## Story 26.4: Specialize 能力迁移

As a 平台构建者,
I want 在统一推理循环中保留 Stem Cell 的动态 Skill 加载能力（specialize action）,
So that 智能体在执行过程中可以按需加载新能力，保持渐进式特化和分化记忆功能。

**FRs:** FR120, FR121

**Acceptance Criteria:**

**Given** `reasonStep` 中 LLM 返回 `ActionSpecialize`
**When** 解析 specialize action
**Then** 从 `action.ToolPath` 获取 skill 名称（格式：`{"action": "specialize", "tool": "skill-name"}`）
**And** 执行以下步骤（从原 `oodaActSpecialize` 迁移）：

1. 检查 `k.skillLoader` 是否存在
2. 加锁检查 skill 是否已加载（`slices.Contains(proc.Skills, skillName)`）
3. 调用 `k.skillLoader(skillName)` 加载 skill（在锁外执行，I/O 可能慢）
4. 重新加锁，TOCTOU 双重检查防止并发重复加载
5. 追加 `proc.Skills` 和 `proc.AllowedDevices`
6. 通过 `k.ctxMgr.AppendMessage` 以 `RoleUser` 注入 skill body（格式：`[Dynamic Skill Loaded: name]\n{body}`）
7. 产生 `StemSpecialize` 事件
8. 更新 DiffMemory 记录

**Given** specialize 成功加载 skill 且进程有 lineage
**When** 加载完成后
**Then** 调用 `proc.lineage.Record(LineageEvent{Phase: "progressive", Skills: []string{skillName}, Trigger: reason})`
**And** 事件中 `FromMemory` 标记为 false（渐进式特化不来自记忆）

**Given** specialize 指定已加载的 skill
**When** TOCTOU 检查命中
**Then** 返回提示 `skill "name" already loaded`，以 tool message 注入上下文
**And** 不重复加载

**Given** specialize 指定不存在的 skill
**When** `k.skillLoader` 返回错误
**Then** 错误信息以 tool message 注入上下文：`specialize error: skill "name" load failed: ...`
**And** 不导致进程崩溃，LLM 在下一步可感知错误
**And** 此错误**不计入**熔断计数器（specialize 失败是可恢复的逻辑错误）

**Given** `k.ctxMgr.AppendMessage` 调用失败
**When** 上下文已被释放或异常
**Then** 输出警告日志（不静默吞掉错误）
**And** 继续循环不终止进程

**Given** `cmd/rnix/lineage.go` 中 `"ooda-specialize"` 引用
**When** 更新引用
**Then** 改为 `"specialize"`

**Given** `kernel/stem_integration_test.go` 中 OODA 相关断言
**When** 更新测试
**Then** 删除 `reasoning: "ooda"` 相关测试设置
**And** 删除 `IsOODA()` 断言
**And** specialize 测试改为直接测试 `ActionSpecialize` 在 `reasonStep` 中的处理

**Given** `kernel/diffmemory_integration_test.go` 中 ~30 处 OODA 引用
**When** 重写测试
**Then** 所有 specialize 相关测试改为统一循环上下文
**And** DiffMemory 的 Record/Lookup 逻辑测试保持不变（核心逻辑未改）

**Given** `kernel/lineage_integration_test.go` 中 ~15 处 OODA 引用
**When** 重写测试
**Then** lineage 记录测试改为统一循环触发 specialize 的场景
**And** LineageEvent 的 Phase/Skills/Trigger/FromMemory 字段测试保持不变

**Given** `kernel/diffmemory_test.go` 中 OODA 相关注释
**When** 更新注释
**Then** 移除 OODA 措辞

**Given** 多个 goroutine 并发触发 specialize
**When** 同时对 DiffMemory 和 Lineage 读写
**Then** 通过现有 `sync.RWMutex` 保证线程安全（锁设计未变）
**And** 通过 `go test -race` 无数据竞争

**Technical Notes:**
- 修改文件：`kernel/kernel.go`（reasonStep 中 ActionSpecialize 分支——从 ooda.go:461-531 迁移逻辑）
- 修改文件：`cmd/rnix/lineage.go`（ooda-specialize → specialize）
- 修改文件：`kernel/stem_integration_test.go`、`kernel/diffmemory_test.go`、`kernel/diffmemory_integration_test.go`、`kernel/lineage_integration_test.go`
- specialize 的 TOCTOU 防护必须保留（先检查 → 加载 → 再检查 under lock）
- DiffMemory 和 Lineage 的核心代码不改——只改调用入口从 ooda.go 迁到 kernel.go 的 reasonStep

---

## Story 26.5: 测试矩阵、Loader 测试重写与文档更新

As a 平台构建者,
I want 完整的统一推理循环测试覆盖和文档更新,
So that 架构变更有充分的测试保障，所有文档与代码保持一致。

**FRs:** FR112-FR118（全覆盖验证）

**Acceptance Criteria:**

### 测试矩阵

**Given** 统一推理循环的 9 个核心场景
**When** 编写单元测试
**Then** 覆盖以下测试矩阵：

| # | 场景 | 验证点 |
|---|------|--------|
| 1 | LLM 返回 `tool_call` | 直接执行，结果以 tool message 注入上下文 |
| 2 | LLM 返回 `plan`（planning=true） | Plan 以 RoleAssistant 写入上下文，下一步 LLM 按 plan 执行 |
| 3 | LLM 返回 `plan`（planning=false） | 按 text 处理，plan 内容作为最终输出 |
| 4 | LLM 返回 `complete` | 正常退出 code=0 |
| 5 | LLM 返回 `spawn` | 创建子进程，等待完成，结果写入上下文 |
| 6 | LLM 返回 `specialize` | 动态加载 skill，body 注入上下文 |
| 7 | 连续 3 次 tool/spawn 失败 | 熔断退出 code=1 |
| 8 | tool 错误 | 错误以 role:"tool" 注入上下文 |
| 9 | /dev/fs 读取 | flags 自动降级为 O_RDONLY |

### Loader 测试重写

**Given** `agents/loader_reasoning_test.go` 存在
**When** 重写为 planning 字段测试
**Then** 测试以下场景：
- `planning: true` → `*true`
- `planning: false` → `*false`
- `planning` 未设置 → `nil`（等价于 true）
- 删除原有的 `reasoning: "linear"/"ooda"/invalid` 测试

**Given** `agents/testdata/invalid-reasoning/` 目录
**When** 决定处理方式
**Then** 删除或重命名为 planning 相关测试 fixture

### 文档更新

**Given** `_bmad-output/planning-artifacts/prd/functional-requirements.md`
**When** 更新 OODA 章节
**Then** 将 "OODA Autonomous Decision（OODA 自主决策，Phase 3）" 重写为 "Unified Reasoning Loop（统一推理循环，Phase 3）"
**And** FR112-FR117 重写为统一循环版本（按 Sprint Change Proposal §4.1 的 PRD-1 变更）
**And** FR118 中 "OODA 循环" 措辞改为 "统一推理循环"

**Given** `_bmad-output/planning-artifacts/prd/non-functional-requirements.md`
**When** 更新 NFR44
**Then** 从 "OODA 单轮循环框架开销 ≤200ms" 改为 "统一推理循环单步框架开销 ≤50ms"

**Given** `_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md`
**When** 更新 Phase 3 描述
**Then** "OODA" 引用改为 "统一推理循环"

**Given** `_bmad-output/planning-artifacts/prd/index.md`
**When** 更新目录
**Then** "OODA Autonomous Decision" 改为 "Unified Reasoning Loop"

**Given** `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md`
**When** 新增 Decision 23
**Then** 添加统一推理循环架构决策段落（按 Sprint Change Proposal §4.2 的 ARCH-1 内容）

**Given** `_bmad-output/planning-artifacts/architecture/architecture-validation-results.md`
**When** 更新验证结果
**Then** OODA 相关条目更新为统一推理循环

**Given** `_bmad-output/project-context.md` §推理循环模式
**When** 重写段落
**Then** 删除双模式描述（12 行），替换为统一推理循环描述（按 Sprint Change Proposal §4.2 的 ARCH-2 内容）

**Given** 项目 `CLAUDE.md`
**When** 更新架构描述
**Then** 推理循环相关描述与新架构一致

**Given** `_bmad-output/planning-artifacts/epics/epic-list.md` 和 `index.md`
**When** 新增 Epic 26 引用
**Then** 添加 Epic 26 条目和链接

**Given** 所有变更完成
**When** 运行 `make all`
**Then** lint + vet + test + build 全部通过
**And** 所有 20 个 Go 包测试通过（`-race` 检测）

**Technical Notes:**
- 新建文件：统一循环测试（可在现有 `kernel/kernel_test.go` 中扩展或新建 `kernel/unified_reasoning_test.go`）
- 修改文件：`agents/loader_reasoning_test.go`（重写）
- 修改文件：PRD 4 个子文件、Architecture 2 个子文件、project-context.md、CLAUDE.md、epic-list.md、index.md
- 测试使用 mock LLM（即时返回预设 JSON）和 mock VFS 设备
- 文档更新遵循 Sprint Change Proposal §4 中定义的具体变更内容
