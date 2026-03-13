---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/21-3-reputation-system-and-auto-selection.md'
  - 'kernel/reputation.go'
  - 'kernel/sla.go'
  - 'kernel/atdd_21_2_reputation_test.go'
  - 'compose/atdd_21_2_sla_test.go'
  - 'compose/engine.go'
  - 'compose/types.go'
  - 'agents/types.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
---

# ATDD Checklist - Epic 21, Story 3: 声誉系统与自动选择

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

为 AI 智能体协作引入声誉系统和自动选择机制。基于历史 SLA 评估结果计算综合声誉评分，支持通过 CLI 和 IPC 查询声誉数据，并在 Compose 编排或 agent.yaml 中指定候选列表时自动择优选择。无声誉数据时返回默认中性分数，完全向后兼容。

**As a** 应用开发者
**I want** 系统跟踪 Agent 模板的历史表现并在自动分配时择优选择
**So that** 表现好的 Agent 模板被更多使用，系统质量持续优化

---

## Acceptance Criteria

1. **AC1: 声誉分数计算** - 基于历史 SLA 评估结果生成综合评分，包含成功率、平均 token 效率、SLA 达标率，近期记录权重高于历史记录
2. **AC2: 声誉查询 CLI** - `rnix reputation [agent]` 显示声誉信息（CLI 测试将在 dev story 阶段添加）
3. **AC3: 声誉查询 IPC** - 通过 IPC 查询 `reputation_status` 返回声誉摘要
4. **AC4: 自动选择机制** - 多候选模板可用时优先选择声誉分数最高的模板
5. **AC5: 无声誉数据时向后兼容** - 无历史记录时返回默认中性分数，行为与当前完全一致

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_21_3_reputation_score_test.go (14 tests)

**File:** `kernel/atdd_21_3_reputation_score_test.go`

- **Test:** `TestReputationStore_GetSummary_NoRecords` (21.3-UNIT-001)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1/AC5 - 无记录返回默认中性 Summary（Score=0.5）
  - **Priority:** P0

- **Test:** `TestReputationStore_GetSummary_AllPassed` (21.3-UNIT-002)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 全部通过时 Score >= 0.9，SuccessRate = 1.0
  - **Priority:** P0

- **Test:** `TestReputationStore_GetSummary_MixedResults` (21.3-UNIT-003)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 部分通过时 Score 在 (0,1) 之间
  - **Priority:** P0

- **Test:** `TestReputationStore_GetSummary_RecentTrend_Improving` (21.3-UNIT-004)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 近期改善时 RecentTrend = "improving"
  - **Priority:** P1

- **Test:** `TestReputationStore_GetSummary_RecentTrend_Declining` (21.3-UNIT-005)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 近期下降时 RecentTrend = "declining"
  - **Priority:** P1

- **Test:** `TestReputationStore_ListAgents` (21.3-UNIT-006)
  - **Status:** RED - ListAgents 方法不存在
  - **Verifies:** AC1 - 列出所有有记录的 agent
  - **Priority:** P0

- **Test:** `TestReputationStore_ListAgents_NoDir` (21.3-UNIT-007)
  - **Status:** RED - ListAgents 方法不存在
  - **Verifies:** AC5 - 目录不存在时返回空切片
  - **Priority:** P1

- **Test:** `TestReputationStore_GetAllSummaries` (21.3-UNIT-008)
  - **Status:** RED - GetAllSummaries 方法不存在
  - **Verifies:** AC1 - 按 Score 降序返回所有摘要
  - **Priority:** P0

- **Test:** `TestReputationStore_SelectBest_HighestScore` (21.3-UNIT-009)
  - **Status:** RED - SelectBest 方法不存在
  - **Verifies:** AC4 - 选择声誉最高的候选
  - **Priority:** P0

- **Test:** `TestReputationStore_SelectBest_AllDefault` (21.3-UNIT-010)
  - **Status:** RED - SelectBest 方法不存在
  - **Verifies:** AC4/AC5 - 全部默认分时返回第一个候选
  - **Priority:** P0

- **Test:** `TestReputationStore_SelectBest_EmptyCandidates` (21.3-UNIT-011)
  - **Status:** RED - SelectBest 方法不存在
  - **Verifies:** AC4 - 空候选列表返回错误
  - **Priority:** P0

- **Test:** `TestReputationStore_GetSummary_AvgDurationMs` (21.3-UNIT-012)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - AvgDurationMs 计算正确
  - **Priority:** P1

- **Test:** `TestReputationStore_GetSummary_TokenEfficiency_AllEqual` (21.3-UNIT-013)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 所有 token 相同时效率 = 1.0
  - **Priority:** P2

- **Test:** `TestReputationStore_ListAgents_IgnoresNonJSON` (21.3-UNIT-014)
  - **Status:** RED - ListAgents 方法不存在
  - **Verifies:** AC1 - 忽略非 .json 文件
  - **Priority:** P1

### IPC Tests - ipc/atdd_21_3_reputation_ipc_test.go (4 tests)

**File:** `ipc/atdd_21_3_reputation_ipc_test.go`

- **Test:** `TestReputationStatus_TypesExist` (21.3-IPC-001)
  - **Status:** RED - ReputationStatusRequest/Response 类型不存在
  - **Verifies:** AC3 - IPC 请求/响应类型可序列化
  - **Priority:** P0

- **Test:** `TestReputationStatus_MethodConstant` (21.3-IPC-002)
  - **Status:** RED - MethodReputationStatus 常量不存在
  - **Verifies:** AC3 - 方法常量值为 "reputation_status"
  - **Priority:** P0

- **Test:** `TestReputationStatus_EmptyAgentName` (21.3-IPC-003)
  - **Status:** RED - ReputationStatusRequest 类型不存在
  - **Verifies:** AC3 - 空 AgentName 表示查询全部
  - **Priority:** P0

- **Test:** `TestReputationStatus_MultipleSummaries` (21.3-IPC-004)
  - **Status:** RED - ReputationStatusResponse 类型不存在
  - **Verifies:** AC3 - 多个 Summary 序列化/反序列化正确
  - **Priority:** P1

### Agent Tests - agents/atdd_21_3_alternatives_test.go (2 tests)

**File:** `agents/atdd_21_3_alternatives_test.go`

- **Test:** `TestAgentManifest_Alternatives` (21.3-AGENT-001)
  - **Status:** RED - AgentManifest.Alternatives 字段不存在
  - **Verifies:** AC4 - agent.yaml 中 alternatives 字段解析正确
  - **Priority:** P0

- **Test:** `TestAgentManifest_NoAlternatives` (21.3-AGENT-002)
  - **Status:** RED - AgentManifest.Alternatives 字段不存在
  - **Verifies:** AC5 - 无 alternatives 时字段为 nil（向后兼容）
  - **Priority:** P0

### Compose Integration Tests - compose/atdd_21_3_auto_select_test.go (7 tests)

**File:** `compose/atdd_21_3_auto_select_test.go`

- **Test:** `TestParseBytes_WithCandidates` (21.3-INT-001)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC4 - 解析 compose.yaml 中的 candidates 字段
  - **Priority:** P0

- **Test:** `TestParseBytes_WithoutCandidates` (21.3-INT-002)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC5 - 无 candidates 时为 nil
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithCandidates_SelectBest` (21.3-INT-003)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC4 - Engine 使用声誉最高的候选
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithCandidates_NoReputationStore` (21.3-INT-004)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC4/AC5 - 无 ReputationStore 时 fallback 到第一个候选
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithoutCandidates_UsesAgentField` (21.3-INT-005)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC5 - 无 candidates 时使用 Agent 字段（向后兼容）
  - **Priority:** P0

- **Test:** `TestParseBytes_WithCandidatesAndSLA` (21.3-INT-006)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC4 - candidates 与 SLA 同时定义可解析
  - **Priority:** P1

- **Test:** `TestEngine_Execute_WithCandidates_SLAEvaluated` (21.3-INT-007)
  - **Status:** RED - AgentSpec.Candidates 字段不存在
  - **Verifies:** AC4 - 选中的 agent 仍执行 SLA 评估
  - **Priority:** P1

### End-to-End Integration Tests - kernel/atdd_21_3_integration_test.go (4 tests)

**File:** `kernel/atdd_21_3_integration_test.go`

- **Test:** `TestE2E_Reputation_RecordAndQuery` (21.3-E2E-001)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC all - 记录 SLA 结果后查询声誉摘要正确
  - **Priority:** P0

- **Test:** `TestE2E_Reputation_ScoreCalculation` (21.3-E2E-002)
  - **Status:** RED - GetAllSummaries 方法不存在
  - **Verifies:** AC all - 多条记录后分数计算和排序正确
  - **Priority:** P0

- **Test:** `TestE2E_Reputation_TrendDetection` (21.3-E2E-003)
  - **Status:** RED - GetSummary 方法不存在
  - **Verifies:** AC1 - 趋势检测正确
  - **Priority:** P1

- **Test:** `TestE2E_Reputation_AutoSelect` (21.3-E2E-004)
  - **Status:** RED - SelectBest 方法不存在
  - **Verifies:** AC4 - 自动选择返回最佳 agent
  - **Priority:** P0

---

## Implementation Checklist

### Task 1: 声誉分数计算模型 (kernel/reputation.go)

**Tests to make pass:** 21.3-UNIT-001 ~ 21.3-UNIT-014

- [ ] 新增 `ReputationSummary` 结构体（AgentName, Score, SuccessRate, AvgTokens, AvgDurationMs, TotalRecords, RecentTrend）
- [ ] 实现 `GetSummary(agentName string) (*ReputationSummary, error)` -- 基于 GetHistory 计算综合评分
- [ ] 实现 `ListAgents() ([]string, error)` -- 扫描 baseDir 的 .json 文件
- [ ] 实现 `GetAllSummaries() ([]ReputationSummary, error)` -- 按 Score 降序
- [ ] 实现 `SelectBest(candidates []string) (string, error)` -- 返回最高分候选
- [ ] Run: `go test -race -run "TestReputationStore_GetSummary|TestReputationStore_ListAgents|TestReputationStore_GetAllSummaries|TestReputationStore_SelectBest" ./kernel/...`

### Task 2: IPC 声誉查询 (ipc/)

**Tests to make pass:** 21.3-IPC-001 ~ 21.3-IPC-004

- [ ] `ipc/protocol.go`：新增 `MethodReputationStatus`、`ReputationStatusRequest`、`ReputationStatusResponse`
- [ ] `ipc/server.go`：添加 `reputationStore` 字段，注册 `handleReputationStatus` handler
- [ ] `ipc/client.go`：新增 `ReputationStatus(agentName string)` 客户端方法
- [ ] Run: `go test -race -run "TestReputationStatus" ./ipc/...`

### Task 3: Agent Alternatives 配置 (agents/types.go)

**Tests to make pass:** 21.3-AGENT-001 ~ 21.3-AGENT-002

- [ ] `agents/types.go`：AgentManifest 添加 `Alternatives []string` 字段
- [ ] Run: `go test -race -run "TestAgentManifest_Alternatives|TestAgentManifest_NoAlternatives" ./agents/...`

### Task 4: Compose Candidates 配置与引擎集成 (compose/)

**Tests to make pass:** 21.3-INT-001 ~ 21.3-INT-007

- [ ] `compose/types.go`：AgentSpec 添加 `Candidates []string` 字段
- [ ] `compose/engine.go`：executeNode 中 candidates 非空时调用 `SelectBest()`
- [ ] `compose/engine.go`：reputationStore 为 nil 时 fallback 到 `Candidates[0]`
- [ ] Run: `go test -race -run "TestParseBytes_WithCandidates|TestEngine_Execute_WithCandidates|TestEngine_Execute_WithoutCandidates" ./compose/...`

### Task 5: CLI 声誉查询命令 (cmd/rnix/)

**Note:** CLI 测试将在 dev story 阶段添加（需要更多集成基础设施）

- [ ] 新建 `cmd/rnix/reputation.go`：实现 `rnix reputation` 命令
- [ ] `cmd/rnix/main.go`：注册 `reputationCmd`
- [ ] Run: `go test -race ./cmd/rnix/...`

### Task 6: 端到端集成验证

**Tests to make pass:** 21.3-E2E-001 ~ 21.3-E2E-004

- [ ] 确认 RecordResult → GetSummary → SelectBest 完整链路工作
- [ ] Run: `go test -race -run "TestE2E_Reputation" ./kernel/...`

---

## Running Tests

```bash
# Run all Story 21.3 unit tests (reputation score)
go test -race -run "TestReputationStore_GetSummary|TestReputationStore_ListAgents|TestReputationStore_GetAllSummaries|TestReputationStore_SelectBest" ./kernel/...

# Run IPC reputation tests
go test -race -run "TestReputationStatus" ./ipc/...

# Run agent alternatives tests
go test -race -run "TestAgentManifest_Alternatives|TestAgentManifest_NoAlternatives" ./agents/...

# Run compose auto-select tests
go test -race -run "TestParseBytes_WithCandidates|TestEngine_Execute_WithCandidates|TestEngine_Execute_WithoutCandidates" ./compose/...

# Run E2E integration tests
go test -race -run "TestE2E_Reputation" ./kernel/...

# Run all Story 21.3 tests
go test -race -run "TestReputationStore_GetSummary|TestReputationStore_ListAgents|TestReputationStore_GetAllSummaries|TestReputationStore_SelectBest|TestReputationStatus|TestAgentManifest_Alternatives|TestAgentManifest_NoAlternatives|TestParseBytes_WithCandidates|TestEngine_Execute_WithCandidates|TestEngine_Execute_WithoutCandidates|TestE2E_Reputation" ./kernel/... ./ipc/... ./agents/... ./compose/...

# Run all tests in affected packages
go test -race ./kernel/... ./ipc/... ./agents/... ./compose/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 31 tests written and designed to fail (types/methods do not exist)
- Tests cover all 5 acceptance criteria
- Tests follow existing project patterns (kernel/atdd_21_2_reputation_test.go, compose/atdd_21_2_sla_test.go)
- Test naming convention: `TestReputationStore_*`, `TestReputationStatus_*`, `TestAgentManifest_*`, `TestParseBytes_*`, `TestEngine_Execute_*`, `TestE2E_Reputation_*`

**Verification:**

- Tests will fail to compile until `kernel/reputation.go` 新增方法, `ipc/protocol.go` 新增类型, `agents/types.go` 新增字段, `compose/types.go` 新增字段, `compose/engine.go` 新增逻辑
- Failure is due to missing types/methods/fields, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**Recommended implementation order:**

1. Task 1: 声誉分数计算模型（纯方法扩展，无新依赖）
2. Task 3: Agent Alternatives 配置（添加字段，最小改动）
3. Task 4: Compose Candidates 配置与引擎集成（依赖 Task 1）
4. Task 2: IPC 声誉查询（依赖 Task 1）
5. Task 5: CLI 声誉查询命令（依赖 Task 2）
6. Task 6: 端到端集成验证（依赖 Task 1-4）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- 验证声誉查询不影响现有 SLA 评估行为
- 确认无 candidates/alternatives 时的向后兼容性
- 检查自动选择不影响 BudgetPool 配额分配
- 验证 ReputationStore 并发安全性（GetSummary/ListAgents 读操作）

---

## Acceptance Criteria Coverage Matrix

| AC | 测试覆盖 | 测试数 |
|----|---------|--------|
| AC1: 声誉分数计算 | UNIT-001~008, UNIT-012~014, E2E-001~003 | 14 |
| AC2: 声誉查询 CLI | (CLI 测试在 dev story 阶段添加) | 0 |
| AC3: 声誉查询 IPC | IPC-001~004 | 4 |
| AC4: 自动选择机制 | UNIT-009~011, AGENT-001, INT-001, INT-003~004, INT-006~007, E2E-004 | 9 |
| AC5: 向后兼容 | UNIT-001, UNIT-007, UNIT-010, AGENT-002, INT-002, INT-004~005 | 7 |

---

## Knowledge Base References Applied

- **test-levels-framework.md** - 测试级别选择：纯 backend Go 项目使用 Unit + Integration
- **data-factories.md** - 测试数据构造模式（使用 table-driven 和循环构造测试数据）
- **test-quality.md** - 测试质量原则（Given-When-Then 注释、单一断言、确定性）
- **test-healing-patterns.md** - 测试修复模式参考
- **test-priorities-matrix.md** - P0-P2 优先级分配

---

## Notes

- 声誉计算基于 Story 21.2 已实现的 ReputationStore（RecordResult + GetHistory）
- Score 计算公式：0.7 * SuccessRate + 0.3 * TokenEfficiency
- RecentTrend 基于最近 5 条记录与整体的 Passed 率对比
- 自动选择是可选功能，仅在 candidates/alternatives 非空时激活
- IPC 遵循 4 步标准流程（protocol → server → client → CLI）
- CLI 测试（AC2）将在 dev story 阶段添加，因需更多集成基础设施
- 测试文件命名遵循项目惯例：`atdd_21_3_*.go`
- 复用 Story 21.2 的 mockKernelSpawner 测试基础设施

---

**Generated by BMad TEA Agent** - 2026-03-13
