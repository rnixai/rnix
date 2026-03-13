---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/21-2-contract-sla-and-evaluation.md'
  - 'kernel/budget_pool_test.go'
  - 'kernel/atdd_21_1_budget_pool_test.go'
  - 'compose/atdd_21_1_budget_pool_test.go'
  - 'compose/engine_test.go'
  - 'compose/types.go'
  - 'compose/engine.go'
  - 'agents/types.go'
  - 'ipc/protocol.go'
  - 'kernel/kernel.go'
---

# ATDD Checklist - Epic 21, Story 2: 合约 SLA 与评估

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

为智能体协作引入合约 SLA（Service Level Agreement）机制。通过在 agent.yaml 或 compose.yaml 中定义 max_tokens、max_duration_ms、output_format 等约束，系统在智能体执行完成后自动评估 SLA 合规性，并将结果记录到声誉分数文件。无 SLA 定义时完全向后兼容。

**As a** 应用开发者
**I want** 通过合约 SLA 约束智能体协作的输入格式、输出质量、token 消耗和超时
**So that** 智能体之间的协作有明确的质量保证

---

## Acceptance Criteria

1. **AC1: SLA 定义与解析** - agent.yaml/compose.yaml 中的 SLA 被正确解析为结构化类型，缺省字段使用合理默认值
2. **AC2: SLA 自动评估** - 智能体完成后对每项约束逐一检查通过/失败，生成 SLAResult
3. **AC3: 评估结果记录到声誉分数** - SLA 评估结果记录到 Agent 模板的声誉分数文件
4. **AC4: Compose 引擎集成 SLA 评估** - Engine 自动触发 SLA 评估并附加到 ScheduleResult，可通过 IPC 查询
5. **AC5: 无 SLA 定义时向后兼容** - 未定义 SLA 时行为与现有完全一致

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_21_2_sla_test.go (16 tests)

**File:** `kernel/atdd_21_2_sla_test.go`

- **Test:** `TestSLASpec_IsEmpty` (21.2-UNIT-001)
  - **Status:** RED - SLASpec 类型不存在
  - **Verifies:** AC1/AC5 - 空 SLASpec 返回 true
  - **Priority:** P0

- **Test:** `TestSLASpec_IsEmpty_WithConstraints` (21.2-UNIT-002)
  - **Status:** RED - SLASpec 类型不存在
  - **Verifies:** AC1 - 有约束时返回 false（4 个子测试）
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_AllPassed` (21.2-UNIT-003)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 所有约束都在限制内时全部通过
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_TokenExceeded` (21.2-UNIT-004)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - token 超出限制时失败
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_DurationExceeded` (21.2-UNIT-005)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 时间超出限制时失败
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_OutputFormatJSON_Valid` (21.2-UNIT-006)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 有效 JSON 输出通过检查
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_OutputFormatJSON_Invalid` (21.2-UNIT-007)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 非 JSON 输出未通过检查
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_OutputFormatMarkdown_Valid` (21.2-UNIT-008)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 有效 markdown 输出通过检查
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_OutputFormatMarkdown_Invalid` (21.2-UNIT-009)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 非 markdown 输出未通过检查
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_NoConstraints` (21.2-UNIT-010)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC5 - 无约束时通过，检查列表为空
  - **Priority:** P0

- **Test:** `TestSLASpec_Evaluate_MultipleFailures` (21.2-UNIT-011)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 多项同时失败
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_TokenExactBoundary` (21.2-UNIT-012)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 边界 - tokens == limit 时通过（<=）
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_DurationExactBoundary` (21.2-UNIT-013)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 边界 - duration == limit 时通过（<=）
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_RecordsTimestamp` (21.2-UNIT-014)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2/AC3 - EvaluatedAt 时间戳正确记录
  - **Priority:** P2

- **Test:** `TestSLASpec_Evaluate_CheckResultValues` (21.2-UNIT-015)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC2 - 检查结果包含 Actual 和 Limit 值
  - **Priority:** P1

- **Test:** `TestSLASpec_Evaluate_SkipsZeroConstraints` (21.2-UNIT-016)
  - **Status:** RED - Evaluate 方法不存在
  - **Verifies:** AC5 - 零值约束被跳过不检查
  - **Priority:** P2

### Unit Tests - kernel/atdd_21_2_reputation_test.go (7 tests)

**File:** `kernel/atdd_21_2_reputation_test.go`

- **Test:** `TestReputationStore_RecordResult` (21.2-UNIT-017)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 写入后读取结果一致
  - **Priority:** P0

- **Test:** `TestReputationStore_RecordResult_Multiple` (21.2-UNIT-018)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 多次追加正确保序
  - **Priority:** P0

- **Test:** `TestReputationStore_GetHistory_NoFile` (21.2-UNIT-019)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3/AC5 - 文件不存在返回空切片
  - **Priority:** P0

- **Test:** `TestReputationStore_RecordResult_CreateDir` (21.2-UNIT-020)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 自动创建目录
  - **Priority:** P1

- **Test:** `TestReputationStore_ConcurrentWrite` (21.2-UNIT-021)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 多 goroutine 并发写入安全
  - **Priority:** P1

- **Test:** `TestReputationStore_AgentIsolation` (21.2-UNIT-022)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 不同 Agent 的声誉数据隔离
  - **Priority:** P1

- **Test:** `TestReputationStore_JSONLinesFormat` (21.2-UNIT-023)
  - **Status:** RED - ReputationStore 类型不存在
  - **Verifies:** AC3 - 文件使用 JSON Lines 格式
  - **Priority:** P2

### Kernel Integration Tests - kernel/atdd_21_2_kernel_sla_test.go (3 tests)

**File:** `kernel/atdd_21_2_kernel_sla_test.go`

- **Test:** `TestKernel_RecordSLAResult` (21.2-KINT-001)
  - **Status:** RED - RecordSLAResult 方法不存在
  - **Verifies:** AC4 - Kernel 注册和查询 SLA 结果
  - **Priority:** P0

- **Test:** `TestKernel_GetSLAResults_NoGroup` (21.2-KINT-002)
  - **Status:** RED - GetSLAResults 方法不存在
  - **Verifies:** AC4 - 未知 group 返回错误
  - **Priority:** P0

- **Test:** `TestKernel_RecordSLAResult_Multiple` (21.2-KINT-003)
  - **Status:** RED - RecordSLAResult 方法不存在
  - **Verifies:** AC4 - 同一 group 多个 SLA 结果
  - **Priority:** P1

### Compose Integration Tests - compose/atdd_21_2_sla_test.go (10 tests)

**File:** `compose/atdd_21_2_sla_test.go`

- **Test:** `TestParseBytes_WithSLA` (21.2-INT-001)
  - **Status:** RED - AgentSpec.SLA 字段不存在
  - **Verifies:** AC1 - 解析 compose.yaml 中的 SLA 字段
  - **Priority:** P0

- **Test:** `TestParseBytes_WithoutSLA` (21.2-INT-002)
  - **Status:** RED - AgentSpec.SLA 字段不存在
  - **Verifies:** AC5 - 无 SLA 字段时 nil
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithSLA_AllPassed` (21.2-INT-003)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC2/AC4 - SLA 全部通过
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithSLA_TokenExceeded` (21.2-INT-004)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC2/AC4 - token 超出 SLA
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithoutSLA` (21.2-INT-005)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC5 - 无 SLA 时行为不变
  - **Priority:** P0

- **Test:** `TestEngine_Execute_WithSLA_RecordReputation` (21.2-INT-006)
  - **Status:** RED - NewEngineWithReputation 构造器不存在
  - **Verifies:** AC3/AC4 - 声誉记录被持久化
  - **Priority:** P1

- **Test:** `TestEngine_Execute_WithSLA_NilReputationStore` (21.2-INT-007)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC4/AC5 - 无 ReputationStore 不 panic
  - **Priority:** P1

- **Test:** `TestEngine_Execute_MultipleAgents_EachSLA` (21.2-INT-008)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC2/AC4 - 多智能体各自 SLA 评估
  - **Priority:** P0

- **Test:** `TestEngine_Execute_SLAUsesActualDuration` (21.2-INT-009)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC2 - SLA 使用实际执行时间
  - **Priority:** P1

- **Test:** `TestEngine_Execute_SLAChecksOutputFormat` (21.2-INT-010)
  - **Status:** RED - ScheduleResult.SLAResult 字段不存在
  - **Verifies:** AC2 - SLA 使用进程输出检查格式
  - **Priority:** P1

---

## Implementation Checklist

### Task 1: SLA 数据模型 (kernel/sla.go)

**Tests to make pass:** 21.2-UNIT-001 ~ 21.2-UNIT-016

- [ ] 新建 `kernel/sla.go`
- [ ] 定义 `SLASpec` 结构体（MaxTokens, MaxDurationMs, OutputFormat）
- [ ] 定义 `SLACheckResult` 结构体（Name, Passed, Actual, Limit）
- [ ] 定义 `SLAResult` 结构体（AgentName, Passed, Checks, EvaluatedAt, TokensUsed, DurationMs）
- [ ] 实现 `SLASpec.IsEmpty()` - 所有字段为零值时返回 true
- [ ] 实现 `SLASpec.Evaluate()` - 逐项检查 max_tokens/max_duration_ms/output_format
- [ ] Run: `go test -race -run "TestSLASpec" ./kernel/...`

**Estimated Effort:** 1-2 hours

### Task 2: 声誉持久化 (kernel/reputation.go)

**Tests to make pass:** 21.2-UNIT-017 ~ 21.2-UNIT-023

- [ ] 新建 `kernel/reputation.go`
- [ ] 定义 `ReputationRecord` 结构体
- [ ] 定义 `ReputationStore` 结构体（mu sync.Mutex, baseDir string）
- [ ] 实现 `NewReputationStore(baseDir)`
- [ ] 实现 `RecordResult()` - JSON Lines 追加写入
- [ ] 实现 `GetHistory()` - 读取 JSON Lines 文件
- [ ] Run: `go test -race -run "TestReputationStore" ./kernel/...`

**Estimated Effort:** 1-2 hours

### Task 3: 配置扩展 (compose/types.go, agents/types.go)

**Tests to make pass:** 21.2-INT-001, 21.2-INT-002

- [ ] `compose/types.go`: AgentSpec 添加 `SLA *kernel.SLASpec`
- [ ] `compose/types.go`: ScheduleResult 添加 `SLAResult *kernel.SLAResult`
- [ ] `agents/types.go`: AgentManifest 添加 `SLA *kernel.SLASpec`
- [ ] Run: `go test -race -run "TestParseBytes_With" ./compose/...`

**Estimated Effort:** 0.5 hours

### Task 4: Compose 引擎集成 (compose/engine.go)

**Tests to make pass:** 21.2-INT-003 ~ 21.2-INT-010

- [ ] Engine 添加 `reputationStore *kernel.ReputationStore` 字段
- [ ] 新增 `NewEngineWithReputation()` 构造器
- [ ] `executeNode` 完成后调用 `SLASpec.Evaluate()`
- [ ] 将 SLAResult 附加到 ScheduleResult
- [ ] 如果 reputationStore 非 nil，调用 `RecordResult()`
- [ ] Run: `go test -race -run "TestEngine_Execute.*SLA" ./compose/...`

**Estimated Effort:** 1-2 hours

### Task 5: Kernel SLA 结果管理 (kernel/kernel.go)

**Tests to make pass:** 21.2-KINT-001 ~ 21.2-KINT-003

- [ ] kernel.go 添加 `slaResults *xsync.SyncMap[types.PGID, []*SLAResult]` 字段
- [ ] 实现 `RecordSLAResult(groupID, result)` 方法
- [ ] 实现 `GetSLAResults(groupID)` 方法
- [ ] Run: `go test -race -run "TestKernel_.*SLA" ./kernel/...`

**Estimated Effort:** 0.5-1 hour

### Task 6: IPC SLA 查询 (ipc/protocol.go, server.go, client.go)

**Note:** IPC 层测试未包含在 ATDD 中，将在 dev story 中添加

- [ ] `ipc/protocol.go`: 新增 MethodSLAStatus 及请求/响应类型
- [ ] `ipc/server.go`: dispatch 注册 handleSLAStatus
- [ ] `ipc/client.go`: 新增 SLAStatus() 方法
- [ ] Run: `go test -race ./ipc/...`

**Estimated Effort:** 1 hour

---

## Running Tests

```bash
# Run all SLA unit tests
go test -race -run "TestSLASpec|TestReputationStore" ./kernel/...

# Run kernel SLA integration tests
go test -race -run "TestKernel_.*SLA" ./kernel/...

# Run compose SLA integration tests
go test -race -run "TestEngine_Execute.*SLA|TestParseBytes.*SLA" ./compose/...

# Run all Story 21.2 tests
go test -race -run "TestSLASpec|TestReputationStore|TestKernel_.*SLA|TestEngine_Execute.*SLA|TestParseBytes.*SLA" ./kernel/... ./compose/...

# Run all tests in affected packages
go test -race ./kernel/... ./compose/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 36 tests written and designed to fail (types/methods do not exist)
- Tests cover all 5 acceptance criteria
- Tests follow existing project patterns (kernel/budget_pool_test.go, compose/engine_test.go)
- Test naming convention: `TestSLASpec_*`, `TestReputationStore_*`, `TestKernel_*SLA*`, `TestEngine_Execute_*SLA*`

**Verification:**

- Tests will fail to compile until `kernel/sla.go`, `kernel/reputation.go`, `compose/types.go`, `compose/engine.go` are updated
- Failure is due to missing types/methods, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**Recommended implementation order:**

1. Task 1: SLA 数据模型（纯类型 + 方法，无外部依赖）
2. Task 2: 声誉持久化（仅依赖 os/json/sync）
3. Task 3: 配置扩展（添加字段，最小改动）
4. Task 4: Compose 引擎集成（依赖 Task 1-3）
5. Task 5: Kernel SLA 结果管理
6. Task 6: IPC SLA 查询

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- 验证 SLA 评估不影响现有 BudgetPool 行为
- 确认无 SLA 时的向后兼容性
- 检查 ReputationStore 文件格式的一致性

---

## Acceptance Criteria Coverage Matrix

| AC | 测试覆盖 | 测试数 |
|----|---------|--------|
| AC1: SLA 定义与解析 | UNIT-001~002, INT-001~002 | 4 |
| AC2: SLA 自动评估 | UNIT-003~016, INT-003~004, INT-008~010 | 19 |
| AC3: 评估结果记录到声誉分数 | UNIT-014, UNIT-017~023, INT-006 | 9 |
| AC4: Compose 引擎集成 | KINT-001~003, INT-003~010 | 11 |
| AC5: 向后兼容 | UNIT-001, UNIT-010, UNIT-016, INT-002, INT-005, INT-007 | 6 |

---

## Knowledge Base References Applied

- **test-levels-framework.md** - 测试级别选择：纯 backend Go 项目使用 Unit + Integration
- **data-factories.md** - 测试数据构造模式（使用 table-driven tests 替代工厂）
- **test-quality.md** - 测试质量原则（Given-When-Then 注释、单一断言、确定性）
- **test-healing-patterns.md** - 测试修复模式参考
- **test-priorities-matrix.md** - P0-P2 优先级分配

---

## Notes

- SLA 评估是后置验证，不是运行时约束（不中断正在运行的智能体）
- 复用 Story 21.1 的 ScheduleResult 扩展模式
- ReputationStore 使用 JSON Lines 文件格式，与 debug 包的 trace 数据存储模式一致
- IPC 层测试将在 dev story 阶段添加（遵循 IPC 4 步标准流程）
- 测试文件命名遵循项目惯例：`atdd_21_2_*.go`

---

**Generated by BMad TEA Agent** - 2026-03-13
