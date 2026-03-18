---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-18'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/26-2-planning-config-and-agent-adaptation.md'
---

# ATDD Checklist - Epic 26, Story 2: ActionType 扩展与统一 Prompt 模板

**Date:** 2026-03-18
**Primary Test Level:** Unit/Integration (Go backend)
**Story Type:** FEATURE — 扩展 ActionType、统一 Prompt 模板、Planning 配置注入、parseAction/reasonStep 扩展

---

## Story Summary

**As a** 平台构建者
**I want** 统一推理循环支持 7 种 action 类型（text/tool_call/plan/spawn/complete/replan/specialize），LLM 每步自主选择行为
**So that** 智能体能力不受限于预设模式，由 LLM 根据任务复杂度智能决策

本 Story 在 26-1（OODA 删除）基础上，扩展 ActionType 常量至 7 种，重命名 `linearToolProtocol` 为 `toolProtocol` 并新增 `planProtocol`，将 `AgentManifest.Reasoning string` 替换为 `Planning *bool`，扩展 `parseAction` 和 `reasonStep` switch 分支以处理 plan/spawn/complete/replan/specialize action。

---

## Acceptance Criteria

| AC | 描述 |
|----|------|
| **AC-1** | ActionType 常量扩展至 7 种（text/tool_call/plan/spawn/complete/replan/specialize） |
| **AC-2** | `linearToolProtocol` 重命名为 `toolProtocol`，新增 spawn/complete/replan/specialize 格式说明 |
| **AC-3** | `planProtocol` 常量定义，包含 plan action 格式说明 |
| **AC-4** | `AgentManifest.Reasoning string` → `Planning *bool`（nil=true, *true=enabled, *false=disabled） |
| **AC-5** | Loader reasoning 验证逻辑删除（`Planning *bool` 无需运行时验证） |
| **AC-6** | Planning 配置注入——planning=true(default) 追加 `planProtocol`，false 时仅注入 `toolProtocol` |
| **AC-7** | `parseAction` 扩展：解析 plan/spawn/complete/replan/specialize ActionType |
| **AC-8** | ActionPlan 处理——planning=true 写 RoleAssistant 到 context；planning=false 按 ActionText 处理 |
| **AC-9** | ActionSpawn 处理——解析 agent/model，spawn 子进程，等待完成，写结果 |
| **AC-10** | ActionComplete 处理——设置 proc.Result，finishProcess(code=0) |
| **AC-11** | ActionReplan 处理——以 RoleAssistant 写 replan 原因到 context |
| **AC-12** | stem agent 配置——无 planning 字段（nil=true，默认启用 planning） |
| **AC-13** | 编译和静态分析通过：`go build ./cmd/rnix/`、`go vet ./...` |

---

## Failing Tests Created (RED Phase)

### Group 1: 常量与类型验证 (AC-1, AC-2, AC-3)

**File:** `kernel/atdd_26_2_action_types_test.go`

- RED **Test:** TestActionType_AllSevenConstantsDefined
  - **Status:** RED — `ActionPlan`/`ActionComplete`/`ActionReplan`/`ActionSpecialize` 常量不存在
  - **Verifies:** AC#1 — ActionType 扩展至 7 种
  - **Method:** 验证 `ActionText`、`ActionToolCall`、`ActionPlan`、`ActionSpawn`、`ActionComplete`、`ActionReplan`、`ActionSpecialize` 常量值均正确
  - **Priority:** P0

- RED **Test:** TestActionType_ConstantValues
  - **Status:** RED — 新常量字符串值不匹配
  - **Verifies:** AC#1 — 每个 ActionType 常量的字符串值与协议一致
  - **Method:** `assertEqual(ActionPlan, ActionType("plan"))` 等
  - **Priority:** P0

- RED **Test:** TestToolProtocol_Exists
  - **Status:** RED — `toolProtocol` 常量不存在（当前为 `linearToolProtocol`）
  - **Verifies:** AC#2 — 常量已重命名
  - **Method:** 引用 `toolProtocol` 常量，编译通过即验证
  - **Priority:** P0

- RED **Test:** TestToolProtocol_ContainsAllActionFormats
  - **Status:** RED — `toolProtocol` 缺少 spawn/complete/replan/specialize 格式说明
  - **Verifies:** AC#2 — toolProtocol 包含所有 action 格式
  - **Method:** `strings.Contains(toolProtocol, "spawn")` 等子串检查
  - **Priority:** P0

- RED **Test:** TestPlanProtocol_Exists
  - **Status:** RED — `planProtocol` 常量不存在
  - **Verifies:** AC#3 — planProtocol 常量已定义
  - **Method:** 引用 `planProtocol` 常量，编译通过即验证
  - **Priority:** P0

- RED **Test:** TestPlanProtocol_ContainsPlanFormat
  - **Status:** RED — `planProtocol` 内容不包含 plan action JSON 格式
  - **Verifies:** AC#3 — planProtocol 包含 plan action 格式和 steps/reason 字段说明
  - **Method:** `strings.Contains(planProtocol, `"action": "plan"`)` 等子串检查
  - **Priority:** P0

---

### Group 2: AgentManifest 与 Loader (AC-4, AC-5)

**File:** `agents/atdd_26_2_planning_field_test.go`

- RED **Test:** TestAgentManifest_HasPlanningField
  - **Status:** RED — `AgentManifest` 结构体无 `Planning *bool` 字段（当前为 `Reasoning string`）
  - **Verifies:** AC#4 — Planning 字段替换 Reasoning
  - **Method:** 反射检查 `AgentManifest` 包含 `Planning` 字段且类型为 `*bool`
  - **Priority:** P0

- RED **Test:** TestAgentManifest_NoReasoningField
  - **Status:** RED — `AgentManifest` 仍包含 `Reasoning` 字段
  - **Verifies:** AC#4 — Reasoning 字段已删除
  - **Method:** 反射检查 `AgentManifest` 不包含 `Reasoning` 字段
  - **Priority:** P0

- RED **Test:** TestAgentManifest_PlanningYAMLParsing
  - **Status:** RED — YAML 解析 `planning: true/false` 无法映射到 `Planning *bool`
  - **Verifies:** AC#4 — YAML 序列化正确映射
  - **Method:** 解析含 `planning: true`、`planning: false`、无 `planning` 字段的 YAML，验证 `*bool` 值
  - **Priority:** P0

- RED **Test:** TestLoader_NoPlanningValidationError
  - **Status:** RED — 加载含 `reasoning: bogus` 的 agent.yaml 仍然报错
  - **Verifies:** AC#5 — reasoning 验证逻辑已删除
  - **Method:** YAML 中只有 `planning` 字段有意义，未知字段被忽略
  - **Priority:** P1

- RED **Test:** TestLoader_PlanningDefaultNil
  - **Status:** RED — 加载不含 `planning` 的 agent.yaml 时 `Planning` 不为 nil
  - **Verifies:** AC#4, AC#5 — 默认值 nil 等价于 true
  - **Method:** 加载 mock-agent（无 planning 字段），验证 `manifest.Planning == nil`
  - **Priority:** P0

---

### Group 3: parseAction 扩展 (AC-7)

**File:** `kernel/atdd_26_2_parse_action_test.go`

- RED **Test:** TestParseAction_PlanJSON
  - **Status:** RED — parseAction 不识别 `"action": "plan"` 并按 ActionText 回退
  - **Verifies:** AC#7 — plan action 正确解析
  - **Method:** 输入 `{"action":"plan","tool":"","data":{"steps":["s1"],"reason":"r"}}`，验证 `action.Type == ActionPlan` 且 `action.ToolData` 包含 steps
  - **Priority:** P0

- RED **Test:** TestParseAction_SpawnJSON
  - **Status:** RED — parseAction 不识别 `"action": "spawn"` 结构（仅旧逻辑处理 spawn 常量但无分支）
  - **Verifies:** AC#7 — spawn action 正确解析
  - **Method:** 输入 `{"action":"spawn","tool":"analyze code","data":{"agent":"a","model":"m"}}`，验证 `Type == ActionSpawn`、`ToolPath == "analyze code"`、`ToolData` 含 agent/model
  - **Priority:** P0

- RED **Test:** TestParseAction_CompleteJSON
  - **Status:** RED — parseAction 不识别 `"action": "complete"`
  - **Verifies:** AC#7 — complete action 正确解析
  - **Method:** 输入 `{"action":"complete","tool":"","data":{"result":"done"}}`，验证 `Type == ActionComplete`、`ToolData` 含 result
  - **Priority:** P0

- RED **Test:** TestParseAction_ReplanJSON
  - **Status:** RED — parseAction 不识别 `"action": "replan"`
  - **Verifies:** AC#7 — replan action 正确解析
  - **Method:** 输入 `{"action":"replan","tool":"","data":{"reason":"failed"}}`，验证 `Type == ActionReplan`、`ToolData` 含 reason
  - **Priority:** P0

- RED **Test:** TestParseAction_SpecializeJSON
  - **Status:** RED — parseAction 不识别 `"action": "specialize"`
  - **Verifies:** AC#7 — specialize action 正确解析
  - **Method:** 输入 `{"action":"specialize","tool":"code-analyst","data":{}}`，验证 `Type == ActionSpecialize`、`ToolPath == "code-analyst"`
  - **Priority:** P0

- RED **Test:** TestParseAction_ToolCallStillWorks
  - **Status:** GREEN（回归保护） — 现有 tool_call 解析不受影响
  - **Verifies:** AC#7 — tool_call 兼容性
  - **Method:** 输入 tool_call JSON，验证 `Type == ActionToolCall`
  - **Priority:** P0

- RED **Test:** TestParseAction_PlainTextFallback
  - **Status:** GREEN（回归保护） — 非 JSON 输入按 ActionText 处理
  - **Verifies:** AC#7 — 纯文本回退
  - **Method:** 输入 "hello world"，验证 `Type == ActionText`
  - **Priority:** P0

- RED **Test:** TestParseAction_DataFieldIsRawMessage
  - **Status:** RED — 当前 Data 类型为 `map[string]any`，需改为 `json.RawMessage`
  - **Verifies:** AC#7 — Data 延迟解析
  - **Method:** 验证 `action.ToolData` 为有效 JSON 字节切片，可独立 Unmarshal
  - **Priority:** P1

- RED **Test:** TestParseAction_UnknownActionFallsBackToText
  - **Status:** RED — 解析 `{"action":"unknown_action"}` 的行为需验证
  - **Verifies:** AC#7 — 未知 action 按 ActionText 回退
  - **Method:** 输入 `{"action":"unknown","tool":"x"}`，验证 `Type == ActionText`
  - **Priority:** P1

---

### Group 4: Planning 配置注入 (AC-6)

**File:** `kernel/atdd_26_2_planning_config_test.go`

- RED **Test:** TestProcess_HasPlanningEnabledField
  - **Status:** RED — Process 结构体无 `PlanningEnabled` 字段
  - **Verifies:** AC#6 — Process 包含 PlanningEnabled bool
  - **Method:** 反射检查 Process 结构体包含 `PlanningEnabled` 字段且类型为 `bool`
  - **Priority:** P0

- RED **Test:** TestNewProcess_PlanningEnabledDefaultsTrue
  - **Status:** RED — NewProcess 不设置 PlanningEnabled
  - **Verifies:** AC#6 — 默认启用 planning
  - **Method:** 创建 NewProcess，验证 `proc.PlanningEnabled == true`
  - **Priority:** P0

- RED **Test:** TestSpawn_PlanningTrueFromManifest
  - **Status:** RED — Spawn 不读取 `manifest.Planning`
  - **Verifies:** AC#6 — 从 AgentManifest Planning=true 传播到 proc.PlanningEnabled=true
  - **Method:** 创建含 `Planning: boolPtr(true)` 的 AgentInfo，Spawn 后验证 proc.PlanningEnabled == true
  - **Priority:** P0

- RED **Test:** TestSpawn_PlanningFalseFromManifest
  - **Status:** RED — Spawn 不读取 `manifest.Planning`
  - **Verifies:** AC#6 — Planning=false 传播为 PlanningEnabled=false
  - **Method:** 创建含 `Planning: boolPtr(false)` 的 AgentInfo，Spawn 后验证 proc.PlanningEnabled == false
  - **Priority:** P0

- RED **Test:** TestSpawn_PlanningNilDefaultsTrue
  - **Status:** RED — Spawn 不读取 `manifest.Planning`
  - **Verifies:** AC#6 — Planning=nil 等价于 true
  - **Method:** 创建含 `Planning: nil` 的 AgentInfo，Spawn 后验证 proc.PlanningEnabled == true
  - **Priority:** P0

---

### Group 5: reasonStep Action Handling (AC-8, AC-9, AC-10, AC-11)

**File:** `kernel/atdd_26_2_reason_actions_test.go`

- RED **Test:** TestReasonStep_ActionPlan_WritesToContext
  - **Status:** RED — reasonStep switch 无 ActionPlan case
  - **Verifies:** AC#8 — plan 以 RoleAssistant 写入 context，格式 `[Plan]\n{steps JSON}`
  - **Method:** Mock LLM 返回 plan JSON，验证 ctxMgr.AppendMessage 被调用，role=RoleAssistant，内容以 `[Plan]` 开头
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionPlan_ContinuesLoop
  - **Status:** RED — reasonStep switch 无 ActionPlan case
  - **Verifies:** AC#8 — plan 后继续下一步循环（非终止）
  - **Method:** Mock LLM 先返回 plan 再返回 complete，验证两步都执行
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionPlan_PlanningDisabled_TreatsAsText
  - **Status:** RED — reasonStep switch 无 ActionPlan case
  - **Verifies:** AC#8 — planning=false 时 plan 按 ActionText 处理
  - **Method:** proc.PlanningEnabled=false，Mock LLM 返回 plan JSON，验证按文本输出处理（proc.Result 被设置）
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionComplete_SetsResult
  - **Status:** RED — reasonStep switch 无 ActionComplete case
  - **Verifies:** AC#10 — complete 设置 proc.Result 并 finishProcess(code=0)
  - **Method:** Mock LLM 返回 complete JSON，验证 proc.Result 包含 data.result 内容
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionComplete_ExitCodeZero
  - **Status:** RED — reasonStep switch 无 ActionComplete case
  - **Verifies:** AC#10 — complete 以 exit code 0 结束
  - **Method:** Mock LLM 返回 complete JSON，等待 proc.Done，验证 exit.Code == 0
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionReplan_WritesToContext
  - **Status:** RED — reasonStep switch 无 ActionReplan case
  - **Verifies:** AC#11 — replan 以 RoleAssistant 写入 context，格式 `[Replan] {reason}`
  - **Method:** Mock LLM 返回 replan JSON，验证 ctxMgr.AppendMessage 被调用，内容以 `[Replan]` 开头
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionReplan_ContinuesLoop
  - **Status:** RED — reasonStep switch 无 ActionReplan case
  - **Verifies:** AC#11 — replan 后继续下一步循环
  - **Method:** Mock LLM 先返回 replan 再返回 complete，验证两步都执行
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionSpawn_SpawnsChild
  - **Status:** RED — reasonStep switch 无 ActionSpawn 处理分支
  - **Verifies:** AC#9 — spawn 创建子进程
  - **Method:** Mock LLM 返回 spawn JSON（含 agent/model），验证 k.Spawn 被调用
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionSpawn_WaitsForChild
  - **Status:** RED — reasonStep switch 无 ActionSpawn 处理分支
  - **Verifies:** AC#9 — spawn 等待子进程完成并写结果到 context
  - **Method:** Mock LLM 返回 spawn JSON，模拟子进程完成，验证 AppendToolResult 被调用
  - **Priority:** P0

- RED **Test:** TestReasonStep_ActionSpawn_TraceIDPropagation
  - **Status:** RED — reasonStep switch 无 ActionSpawn 处理分支
  - **Verifies:** AC#9 — TraceID/SpanID 传播到子进程
  - **Method:** 设置 proc.TraceID/SpanID，Mock spawn，验证 childOpts 包含 TraceID 和 ParentSpanID
  - **Priority:** P1

- RED **Test:** TestReasonStep_ActionSpawn_ParentCancellation
  - **Status:** RED — reasonStep switch 无 ActionSpawn 处理分支
  - **Verifies:** AC#9 — 父进程取消时终止等待
  - **Method:** Mock spawn，取消 parent context，验证 finishProcess 被调用
  - **Priority:** P1

- RED **Test:** TestReasonStep_ActionSpecialize_Stub
  - **Status:** RED — reasonStep switch 无 ActionSpecialize case
  - **Verifies:** AC#7 — specialize 占位处理（返回 "not yet implemented" 并 continue）
  - **Method:** Mock LLM 返回 specialize JSON，验证 AppendToolResult 含 "not yet implemented"
  - **Priority:** P1

---

### Group 6: stem agent 配置 (AC-12)

**File:** `agents/atdd_26_2_stem_config_test.go`

- RED **Test:** TestStemAgent_PlanningDefaultEnabled
  - **Status:** GREEN（回归保护） — stem agent.yaml 无 planning 字段，默认 nil=true
  - **Verifies:** AC#12 — stem agent planning 默认启用
  - **Method:** 加载 stem agent，验证 `manifest.Planning == nil`（nil 等价于 true）
  - **Priority:** P1

---

### Group 7: Build 验证 (AC-13)

**File:** 脚本/CI 或 `make all` 验证

- RED **Test:** BuildVerify_GoBuildSucceeds
  - **Status:** RED — `go build ./cmd/rnix/` 可能因新常量/类型引用失败
  - **Verifies:** AC#13 — 编译成功
  - **Priority:** P0

- RED **Test:** BuildVerify_GoVetClean
  - **Status:** RED — `go vet ./...` 有警告
  - **Verifies:** AC#13 — 静态分析无警告
  - **Priority:** P0

- RED **Test:** BuildVerify_KernelTestsPass
  - **Status:** RED — `go test -race ./kernel/...` 失败
  - **Verifies:** AC#13 — kernel 包测试通过（回归）
  - **Priority:** P0

- RED **Test:** BuildVerify_AgentsTestsPass
  - **Status:** RED — `go test -race ./agents/...` 失败
  - **Verifies:** AC#13 — agents 包测试通过（回归）
  - **Priority:** P0

- RED **Test:** BuildVerify_MakeAllPass
  - **Status:** RED — `make all` 失败
  - **Verifies:** AC#13 — lint + vet + test + build 全通过
  - **Priority:** P0

---

## AC <-> Test 覆盖矩阵

| AC | 描述 | 测试 |
|----|------|------|
| AC-1 | ActionType 常量扩展 | TestActionType_AllSevenConstantsDefined, TestActionType_ConstantValues |
| AC-2 | toolProtocol 重命名扩展 | TestToolProtocol_Exists, TestToolProtocol_ContainsAllActionFormats |
| AC-3 | planProtocol 定义 | TestPlanProtocol_Exists, TestPlanProtocol_ContainsPlanFormat |
| AC-4 | Reasoning → Planning | TestAgentManifest_HasPlanningField, TestAgentManifest_NoReasoningField, TestAgentManifest_PlanningYAMLParsing, TestLoader_PlanningDefaultNil |
| AC-5 | Loader 验证删除 | TestLoader_NoPlanningValidationError, TestLoader_PlanningDefaultNil |
| AC-6 | Planning 配置注入 | TestProcess_HasPlanningEnabledField, TestNewProcess_PlanningEnabledDefaultsTrue, TestSpawn_PlanningTrueFromManifest, TestSpawn_PlanningFalseFromManifest, TestSpawn_PlanningNilDefaultsTrue |
| AC-7 | parseAction 扩展 | TestParseAction_PlanJSON, TestParseAction_SpawnJSON, TestParseAction_CompleteJSON, TestParseAction_ReplanJSON, TestParseAction_SpecializeJSON, TestParseAction_ToolCallStillWorks, TestParseAction_PlainTextFallback, TestParseAction_DataFieldIsRawMessage, TestParseAction_UnknownActionFallsBackToText |
| AC-8 | ActionPlan 处理 | TestReasonStep_ActionPlan_WritesToContext, TestReasonStep_ActionPlan_ContinuesLoop, TestReasonStep_ActionPlan_PlanningDisabled_TreatsAsText |
| AC-9 | ActionSpawn 处理 | TestReasonStep_ActionSpawn_SpawnsChild, TestReasonStep_ActionSpawn_WaitsForChild, TestReasonStep_ActionSpawn_TraceIDPropagation, TestReasonStep_ActionSpawn_ParentCancellation |
| AC-10 | ActionComplete 处理 | TestReasonStep_ActionComplete_SetsResult, TestReasonStep_ActionComplete_ExitCodeZero |
| AC-11 | ActionReplan 处理 | TestReasonStep_ActionReplan_WritesToContext, TestReasonStep_ActionReplan_ContinuesLoop |
| AC-12 | stem agent 配置 | TestStemAgent_PlanningDefaultEnabled |
| AC-13 | 编译和静态分析 | BuildVerify_GoBuildSucceeds, BuildVerify_GoVetClean, BuildVerify_KernelTestsPass, BuildVerify_AgentsTestsPass, BuildVerify_MakeAllPass |

---

## 测试策略说明（Go Backend）

- **常量/类型验证**：直接引用新常量（`ActionPlan`、`toolProtocol`、`planProtocol`）——编译通过即验证存在性；字符串值通过 `assertEqual` 精确检查。
- **反射检查**：使用 `reflect.TypeOf` 验证结构体字段（`AgentManifest.Planning *bool`、`Process.PlanningEnabled bool`）的存在性和类型，以及旧字段（`Reasoning`）的删除。
- **parseAction 单元测试**：构造 `llmResponse{Content: jsonString}` 直接调用 `parseAction`，验证返回的 `ReasonAction` 的 Type/ToolPath/ToolData/Content 字段。`parseAction` 是纯函数，无外部依赖。
- **reasonStep 集成测试**：需要 Mock LLM（通过 VFS `/dev/llm` 设备）和 Mock CtxManager。模式：创建 Kernel + Process，注册 mock LLM 设备返回预设 JSON 响应，启动 reasonStep，验证 context 写入和进程状态。复用现有 `kernel_test.go` 的 mock 基础设施。
- **Planning 配置传播测试**：端到端验证路径 `agent.yaml → AgentManifest.Planning → proc.PlanningEnabled → sysPrompt 注入`。需 mock agentLoader。
- **YAML 解析测试**：使用 `yaml.Unmarshal` 直接测试 `AgentManifest` 解析，覆盖 `planning: true`、`planning: false`、无 `planning` 字段三种情况。
- **构建验证**：`go build`、`go vet`、`go test -race`、`make all` 作为最终验收。

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `kernel/kernel.go` | 待修改 | ActionType 常量扩展 (AC-1)、toolProtocol/planProtocol (AC-2/3)、Planning 注入 (AC-6)、parseAction 扩展 (AC-7)、reasonStep switch 扩展 (AC-8/9/10/11) |
| `kernel/process.go` | 待修改 | 新增 `PlanningEnabled bool` 字段 (AC-6) |
| `agents/types.go` | 待修改 | `Reasoning string` → `Planning *bool` (AC-4) |
| `agents/loader.go` | 待修改 | 删除 reasoning 验证 switch (AC-5) |
| `agents/loader_reasoning_test.go` | 待重写 | Reasoning 测试 → Planning 测试 (AC-4/5) |
| `kernel/atdd_26_2_action_types_test.go` | 待创建 | 常量/类型验证测试 (AC-1/2/3) |
| `kernel/atdd_26_2_parse_action_test.go` | 待创建 | parseAction 扩展测试 (AC-7) |
| `kernel/atdd_26_2_planning_config_test.go` | 待创建 | Planning 配置注入测试 (AC-6) |
| `kernel/atdd_26_2_reason_actions_test.go` | 待创建 | reasonStep action handling 测试 (AC-8/9/10/11) |
| `agents/atdd_26_2_planning_field_test.go` | 待创建 | AgentManifest Planning 字段测试 (AC-4/5) |
| `agents/atdd_26_2_stem_config_test.go` | 待创建 | stem agent 配置测试 (AC-12) |

---

## 测试优先级分布

| 优先级 | 数量 | 测试 |
|--------|------|------|
| P0 | 29 | 常量验证 (6)、Manifest/Loader (4)、parseAction (7)、Planning config (5)、reasonStep actions (7)、Build 验证 (5) |
| P1 | 8 | Loader 验证删除 (1)、parseAction 边界 (2)、Spawn trace/cancel (2)、Specialize stub (1)、stem config (1)、Build 附加 (0) |

---

## 下一步

1. 按 AC 优先级创建 RED 测试文件（先写测试，编译应失败或测试应 FAIL）
2. 按 Story Tasks 执行顺序实现代码变更
3. 逐步验证测试从 RED → GREEN
4. 运行 `make all` 确保全部通过
5. 所有 37 个测试由 RED 变为 GREEN
