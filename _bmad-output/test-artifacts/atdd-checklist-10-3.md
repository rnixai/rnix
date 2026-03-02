---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-04c-aggregate'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-02'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/10-3-token-budget-management.md'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/kernel_test.go'
  - 'compose/types.go'
  - 'compose/engine.go'
  - 'compose/engine_test.go'
  - 'ipc/protocol.go'
  - 'ipc/protocol_test.go'
  - 'vfs/proc.go'
  - 'vfs/proc_test.go'
  - 'cmd/crux/top.go'
  - 'cmd/crux/top_test.go'
  - 'agents/types.go'
---

# ATDD 清单 - Epic 10, Story 10.3: Token 预算管理

**日期:** 2026-03-02
**作者:** Decker
**主要测试级别:** Unit + Integration (Go backend)
**堆栈类型:** backend (Go 1.26)

---

## 故事摘要

为智能体设置 token 预算上限，累计消耗达到预算时系统自动终止推理，并通过 IPC 和 crux top 展示预算使用情况及警告。

**As a** 用户
**I want** 为智能体设置 token 预算上限，超限时系统自动终止推理
**So that** 我可以控制 LLM 调用的成本

---

## 验收标准

1. **AC1: Agent 级 Token 预算执行** — 累计消耗达到 `context_budget` 时系统终止推理，进程转 Zombie，ExitStatus `{Code: 2, Reason: "budget_exceeded"}`，emitLog 发送预算超限通知
2. **AC2: Compose 覆盖预算** — Compose 中的 `context_budget` 覆盖 Agent 配置，优先级：Compose > Agent > 默认(0=无限制)
3. **AC3: crux top 预算警告** — 预算已设且剩余 < 10% 时 TOKENS 列显示 WarningStyle，格式 `已用/预算`
4. **AC4: 无预算时无变化** — `context_budget: 0` 或未设置时行为与现有完全一致，crux top TOKENS 列维持纯数字格式
5. **AC5: IPC 传递预算信息** — ProcInfo 包含 `ContextBudget` 字段，客户端可用于判断警告阈值

---

## 测试策略

| AC | 描述 | 测试级别 | 优先级 | 测试 ID 范围 |
|----|------|---------|--------|-------------|
| AC1 | Agent 级预算执行 | Unit | P0 | 10.3-UNIT-001~009, 10.3-UNIT-011~013 |
| AC2 | Compose 覆盖预算 | Unit | P0/P1 | 10.3-UNIT-003~004, 10.3-UNIT-020~022 |
| AC3 | crux top 预算警告 | Unit | P1/P2 | 10.3-UNIT-040~044 |
| AC4 | 无预算无变化 | Unit + Integration | P0 | 10.3-UNIT-002, 10.3-UNIT-012, 10.3-INT-001 |
| AC5 | IPC 传递预算 | Unit | P1/P2 | 10.3-UNIT-010, 10.3-UNIT-030~034, 10.3-UNIT-050~053 |

---

## 失败测试清单 (RED Phase)

### 内核预算检查测试 (16 tests)

**文件:** `kernel/budget_test.go` (新建)

- **10.3-UNIT-001:** `TestBudgetEnforcement_TerminatesAtBudget` — budget=2500, LLM 每次返回 1000 token，第 3 步触发终止
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 累计达预算时终止推理

- **10.3-UNIT-002:** `TestBudgetEnforcement_ZeroBudgetNoLimit` — budget=0 时正常完成
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC4 — 无预算不限制

- **10.3-UNIT-003:** `TestBudgetPriority_OptsOverridesAgent` — SpawnOpts=3000 覆盖 Agent=1000
  - **状态:** RED — SpawnOpts.ContextBudget / Process.ContextBudget 字段不存在
  - **验证:** AC2 — 预算优先级

- **10.3-UNIT-004:** `TestBudgetPriority_AgentManifestWhenOptsZero` — opts 未设时使用 Agent=4096
  - **状态:** RED — Process.ContextBudget 字段不存在
  - **验证:** AC2 — Agent manifest 作为默认预算

- **10.3-UNIT-005:** `TestBudgetExceeded_ExitCode2` — 超限返回 ExitStatus{Code: 2}
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 退出码 2 表示受控终止

- **10.3-UNIT-006:** `TestBudgetExceeded_EmitsLog` — 超限时 emitLog [output] 类别
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 日志通知

- **10.3-UNIT-007:** `TestBudgetExceeded_EmitsEvent` — 超限时 emitEvent action=budget_exceeded
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 事件记录

- **10.3-UNIT-008:** `TestBudgetEnforcement_ExactBoundary` — tokens==budget 时触发终止 (>=)
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 边界条件

- **10.3-UNIT-009:** `TestBudgetEnforcement_NegativeTreatedAsZero` — 负预算视为 0
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC4 — 边界安全

- **10.3-UNIT-010:** `TestGetProcInfo_IncludesContextBudget` — GetProcInfo 返回 ContextBudget
  - **状态:** RED — Process.ContextBudget / ProcInfo.ContextBudget 字段不存在
  - **验证:** AC5 — ProcInfo 扩展

- **10.3-UNIT-011:** `TestBudgetEnforcement_MultiStep_CumulativeCheck` — 多步累计检查 >=
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 累计 token 检查

- **10.3-UNIT-012:** `TestBudgetEnforcement_DefaultNoLimit` — 默认 SpawnOpts 无限制
  - **状态:** RED — Process.ContextBudget 字段不存在
  - **验证:** AC4 — 默认行为

- **10.3-UNIT-013:** `TestBudgetEnforcement_PreventsActionAfterExceeded` — 超限后阻止 action 执行
  - **状态:** RED — SpawnOpts.ContextBudget 字段不存在
  - **验证:** AC1 — 预算检查在 parseAction 之前

- **10.3-INT-001:** `TestBudget_BackwardCompatibility` — 现有 Spawn 无 budget 字段正常工作
  - **状态:** RED — 编译通过但验证预算字段为 0
  - **验证:** AC4 — 向后兼容

- **10.3-UNIT-014:** `TestAgentContextBudget_ExistingHelper` — testAgentInfo ContextBudget=4096
  - **状态:** RED — 取决于 testAgentInfo 是否包含 ContextBudget
  - **验证:** AC2 — 测试基础设施

- **10.3-INT-002:** `TestSpawn_WithAgent_UsesBudgetFromManifest` — Spawn 时读取 agent manifest budget
  - **状态:** RED — Process.ContextBudget 字段不存在
  - **验证:** AC2 — manifest 预算传递

### Compose 预算传递测试 (3 tests)

**文件:** `compose/engine_test.go` (追加)

- **10.3-UNIT-020:** `TestEngine_Execute_ContextBudgetPassthrough` — AgentSpec.ContextBudget=5000 传递到 ComposeSpawnOpts
  - **状态:** RED — AgentSpec.ContextBudget 字段不存在
  - **验证:** AC2 — Compose 预算传递

- **10.3-UNIT-021:** `TestEngine_Execute_NoBudgetPassesZero` — 无 budget 时传 0
  - **状态:** RED — ComposeSpawnOpts.ContextBudget 字段不存在
  - **验证:** AC2 — 默认值传递

- **10.3-UNIT-022:** `TestEngine_Execute_MultipleBudgets` — 多 agent 不同 budget
  - **状态:** RED — AgentSpec.ContextBudget 字段不存在
  - **验证:** AC2 — 多 agent 场景

### IPC 协议 ContextBudget 测试 (5 tests)

**文件:** `ipc/protocol_test.go` (追加)

- **10.3-UNIT-030:** `TestProcInfoWire_ContextBudget_RoundTrip` — ProcInfoWire ContextBudget 往返转换
  - **状态:** RED — ProcInfoWire.ContextBudget / ProcInfo.ContextBudget 字段不存在
  - **验证:** AC5 — IPC 传递

- **10.3-UNIT-031:** `TestProcInfoWire_ContextBudget_OmitEmpty` — budget=0 时 JSON 中不出现
  - **状态:** RED — ProcInfoWire.ContextBudget 字段不存在
  - **验证:** AC5 — omitempty 行为

- **10.3-UNIT-032:** `TestProcInfoWire_ContextBudget_PresentWhenSet` — budget>0 时 JSON 中出现
  - **状态:** RED — ProcInfoWire.ContextBudget 字段不存在
  - **验证:** AC5 — 序列化正确性

- **10.3-UNIT-033:** `TestSpawnRequest_ContextBudget_RoundTrip` — SpawnRequest ContextBudget 往返
  - **状态:** RED — SpawnRequest.ContextBudget 字段不存在
  - **验证:** AC2/AC5 — IPC Spawn 传递

- **10.3-UNIT-034:** `TestSpawnRequest_ContextBudget_OmitEmpty` — budget=0 时 omit
  - **状态:** RED — SpawnRequest.ContextBudget 字段不存在
  - **验证:** AC5 — omitempty 行为

### crux top 预算渲染测试 (5 tests)

**文件:** `cmd/crux/top_test.go` (追加)

- **10.3-UNIT-040:** `TestTopSummaryLine_WithBudgetInfo` — summary 在有 budget 进程时正常渲染
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC3 — 基本渲染

- **10.3-UNIT-041:** `TestTopDetailView_ShowsBudget` — 详情视图显示 budget 值
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC3 — 详情视图

- **10.3-UNIT-042:** `TestTopDetailView_NoBudgetOmitsBudgetLine` — budget=0 时不显示 budget 行
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC4 — 无预算时省略

- **10.3-UNIT-043:** `TestTopView_WarningStyleHighUsage` — 90% 使用率时的警告渲染
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC3 — WarningStyle 着色

- **10.3-UNIT-044:** `TestTopView_PlainTokensNoBudget` — 无 budget 时纯数字显示
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC4 — 纯数字格式

### VFS /proc 预算字段测试 (4 tests)

**文件:** `vfs/proc_test.go` (追加)

- **10.3-UNIT-050:** `TestProcFS_Status_IncludesContextBudget` — statusJSON 包含 context_budget
  - **状态:** RED — ProcInfo.ContextBudget / statusJSON.ContextBudget 字段不存在
  - **验证:** AC5 — /proc 暴露预算

- **10.3-UNIT-051:** `TestProcFS_Status_OmitsContextBudgetWhenZero` — budget=0 时 JSON 中不出现
  - **状态:** RED — statusJSON.ContextBudget 字段不存在
  - **验证:** AC5 — omitempty 行为

- **10.3-UNIT-052:** `TestProcInfo_ContextBudget_FieldExists` — ProcInfo.ContextBudget 字段存在
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC5 — 字段定义

- **10.3-UNIT-053:** `TestProcFS_ListProcs_ContextBudget` — ListProcs 返回各进程 ContextBudget
  - **状态:** RED — ProcInfo.ContextBudget 字段不存在
  - **验证:** AC5 — 列表查询

---

## Mock 需求

### sequenceLLMFile (kernel/budget_test.go)

按顺序返回预设 LLM 响应，每个响应携带固定 `TokensUsed`，用于精确控制预算触发时机。

### mockToolFile (kernel/budget_test.go)

简单工具 mock，返回固定字节数据，用于模拟 tool call 执行。

### mockKernelSpawner (compose/engine_test.go)

记录 Spawn 调用参数（含 ComposeSpawnOpts.ContextBudget），验证预算从 AgentSpec 正确传递。

### mockProcessInfoProvider (vfs/proc_test.go)

提供带 ContextBudget 字段的 ProcInfo，验证 /proc 文件系统正确序列化预算信息。

---

## 实现清单

### Phase 1: 核心字段添加 (使 UNIT-010, UNIT-050~053 编译通过)

- [ ] `kernel/process.go`：Process 添加 `ContextBudget int` 字段
- [ ] `kernel/kernel.go`：SpawnOpts 添加 `ContextBudget int` 字段
- [ ] `vfs/proc.go`：ProcInfo 添加 `ContextBudget int` 字段
- [ ] `vfs/proc.go`：statusJSON 添加 `ContextBudget int \`json:"context_budget,omitempty"\``
- [ ] `vfs/proc.go`：toStatusJSON 中赋值 ContextBudget
- [ ] 运行测试: `go test ./vfs/ -run 'ContextBudget'`
- [ ] 预计工作量: 0.5h

### Phase 2: 预算优先级与赋值 (使 UNIT-002~004, UNIT-012, INT-001~002, UNIT-014 通过)

- [ ] `kernel/kernel.go`：Spawn 方法中实现 opts > agent manifest > 0 优先级
- [ ] `kernel/kernel.go`：`proc.ContextBudget = opts.ContextBudget`
- [ ] `kernel/kernel.go`：负预算值处理 `if opts.ContextBudget < 0 { opts.ContextBudget = 0 }`
- [ ] `kernel/kernel.go`：GetProcInfo 中传递 ContextBudget
- [ ] 运行测试: `go test ./kernel/ -run 'Budget'`
- [ ] 预计工作量: 1h

### Phase 3: 预算检查核心逻辑 (使 UNIT-001, UNIT-005~009, UNIT-011, UNIT-013 通过)

- [ ] `kernel/kernel.go`：reasonStep 中 token 累加后添加 `budget > 0 && tokens >= budget` 检查
- [ ] `kernel/kernel.go`：检查通过时调用 emitLog、emitEvent、finishProcess
- [ ] `kernel/kernel.go`：ExitStatus{Code: 2, Reason: "budget_exceeded"}
- [ ] 运行测试: `go test ./kernel/ -run 'Budget' -v`
- [ ] 预计工作量: 1.5h

### Phase 4: Compose 预算传递 (使 UNIT-020~022 通过)

- [ ] `compose/types.go`：AgentSpec 添加 `ContextBudget int \`yaml:"context_budget,omitempty"\``
- [ ] `compose/types.go`：ComposeSpawnOpts 添加 `ContextBudget int`
- [ ] `compose/engine.go`：spawnAgent 中 `opts.ContextBudget = agentSpec.ContextBudget`
- [ ] 运行测试: `go test ./compose/ -run 'ContextBudget'`
- [ ] 预计工作量: 0.5h

### Phase 5: IPC 协议扩展 (使 UNIT-030~034 通过)

- [ ] `ipc/protocol.go`：SpawnRequest 添加 `ContextBudget int \`json:"context_budget,omitempty"\``
- [ ] `ipc/protocol.go`：ProcInfoWire 添加 `ContextBudget int \`json:"context_budget,omitempty"\``
- [ ] `ipc/protocol.go`：ProcInfoToWire 和 WireToProcInfo 转换 ContextBudget
- [ ] `ipc/server.go`：handleSpawn 解析 ContextBudget 传入 kernel.SpawnOpts
- [ ] `cmd/crux/compose.go`：ipcKernelSpawner.Spawn 传递 ContextBudget 到 SpawnRequest
- [ ] 运行测试: `go test ./ipc/ -run 'ContextBudget'`
- [ ] 预计工作量: 1h

### Phase 6: crux top 预算渲染 (使 UNIT-040~044 通过)

- [ ] `cmd/crux/top.go`：TOKENS 列渲染 — budget>0 时格式 `已用/预算`
- [ ] `cmd/crux/top.go`：WarningStyle 渲染 — usage >= 90% 时黄色
- [ ] `cmd/crux/top.go`：topDetailView — budget>0 时增加 Budget 行
- [ ] `cmd/crux/top.go`：TOKENS 列宽从 8 调整到 12
- [ ] 运行测试: `go test ./cmd/crux/ -run 'Budget\|Warning\|PlainTokens'`
- [ ] 预计工作量: 1h

---

## 运行测试

```bash
# 运行 Story 10.3 所有失败测试
go test ./kernel/ ./compose/ ./ipc/ ./vfs/ ./cmd/crux/ -run 'Budget|ContextBudget|Warning' -v

# 运行特定文件
go test ./kernel/ -run 'Budget' -v

# 运行 Compose 预算测试
go test ./compose/ -run 'ContextBudget' -v

# 运行 IPC 预算测试
go test ./ipc/ -run 'ContextBudget' -v

# 运行 VFS 预算测试
go test ./vfs/ -run 'ContextBudget' -v

# 运行 crux top 预算测试
go test ./cmd/crux/ -run 'Budget|Warning|PlainTokens' -v

# 运行全量测试（含回归）
go test ./...

# 运行测试并生成覆盖率
go test ./kernel/ ./compose/ ./ipc/ ./vfs/ ./cmd/crux/ -coverprofile=coverage.out -run 'Budget|ContextBudget|Warning'
go tool cover -html=coverage.out
```

---

## Red-Green-Refactor 工作流

### RED Phase (完成) ✅

**TEA Agent 完成内容:**

- ✅ 33 个测试编写完成（全部失败）
- ✅ Mock 结构定义（sequenceLLMFile, mockToolFile 复用现有）
- ✅ 实现清单按优先级和依赖排列
- ✅ 所有 5 个 AC 均有对应测试覆盖

**验证:**

- 所有测试因缺少 `ContextBudget` 字段而编译失败
- 失败原因明确且可操作
- 测试因缺少实现而失败，而非测试本身有 bug

---

### GREEN Phase (DEV Team - 下一步)

1. **按 Phase 1→6 顺序** 逐步实现
2. **每个 Phase 完成后** 运行对应测试验证 GREEN
3. **每个测试通过后** 在清单中标记 ✅
4. **全部 33 个测试通过** 后进入 REFACTOR

---

### REFACTOR Phase (DEV Team - 全部测试通过后)

1. 提取预算检查为独立方法（如 `checkBudget(proc) bool`）
2. 统一 ExitStatus 常量（Code=0/1/2）
3. 确认 TOKENS 列宽足够（覆盖 `99,999/100,000` 场景）
4. 确认 WarningStyle 不影响 cursor 高亮逻辑

---

## 知识库引用

- **test-quality.md** — Given-When-Then、单一断言、确定性、隔离性
- **test-levels-framework.md** — 测试级别选择（Unit vs Integration）
- **data-factories.md** — 工厂模式用于测试数据生成
- **test-healing-patterns.md** — 测试自愈模式，避免脆弱测试

---

## 备注

- Go 项目的 RED phase 表现为编译失败（引用不存在的字段），而非运行时断言失败
- `sequenceLLMFile` 复用 `kernel/kernel_test.go` 中已有的 mock 模式
- `agents/types.go` 中 `AgentManifest.ContextBudget` 字段已存在，无需修改
- ExitStatus.Code=2 为新增退出码，与现有 0（正常）和 1（错误）并列
- 负预算值视为 0（无限制），作为防御性编程处理

---

**Generated by BMad TEA Agent** — 2026-03-02
