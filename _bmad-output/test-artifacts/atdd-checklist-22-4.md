---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/22-4-capability-migration-and-similarity-matrix.md'
  - 'kernel/immune.go'
  - 'kernel/immune_test.go'
  - 'kernel/atdd_22_3_security_status_test.go'
  - 'kernel/reputation.go'
  - 'kernel/supervisor.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/rnix/immune.go'
  - 'cmd/rnix/immune_test.go'
---

# ATDD Checklist - Epic 22, Story 4: 能力迁移与相似度矩阵

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统在智能体异常退出且 Supervisor 重启失败时，自动将任务迁移到相似能力的智能体，基于 Skill 重叠度和协作历史的相似度矩阵进行最佳替代选择。

**As a** 平台构建者
**I want** 系统在智能体异常退出且 Supervisor 重启失败时，自动将任务迁移到相似能力的智能体
**So that** 任务不会因单个智能体故障而丢失

---

## Acceptance Criteria

1. **AC1: 能力相似度矩阵计算** - 基于 Skill 重叠度（Jaccard 系数）计算任意两个 Agent 之间的相似度分数（0.0~1.0）
2. **AC2: 历史协作记录纳入相似度** - 协作历史作为加权因子纳入相似度分数（Skill 重叠 70% + 协作历史 30%）
3. **AC3: Supervisor 重启失败触发能力迁移** - 系统尝试将失败子进程的任务上下文迁移到最佳替代 Agent
4. **AC4: 最佳替代 Agent 选择** - 选择相似度最高且声誉分数最高的 Agent；若无可用替代（相似度 < 0.3），迁移放弃
5. **AC5: 迁移性能约束** - 总迁移时间 <= 10s（NFR48）
6. **AC6: IPC 查询接口** - 通过 IPC 查询相似度，返回指定 Agent 的相似 Agent 列表
7. **AC7: CLI 展示** - `rnix immune similarity [agent-name]` 显示能力相似度排行榜

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_22_4_capability_migration_test.go (25 tests)

**File:** `kernel/atdd_22_4_capability_migration_test.go`

- **Test:** `TestSimilarityMatrix_Compute_BasicSkillOverlap` (22.4-UNIT-001)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 两个 Agent 共享部分 Skill 时 Jaccard 系数正确
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_Compute_NoOverlap` (22.4-UNIT-002)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 完全不同的 Skill 集合，Score=0
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_Compute_IdenticalSkills` (22.4-UNIT-003)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 完全相同的 Skill，SkillScore=1.0
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_Compute_WithCoopHistory` (22.4-UNIT-004)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC2 - 协作历史纳入综合分
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_GetSimilar_SortedByScore` (22.4-UNIT-005)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 结果按 Score 降序排列
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_GetSimilar_MinScoreFilter` (22.4-UNIT-006)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - minScore 过滤正确
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_Compute_EmptyInput` (22.4-UNIT-007)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 空输入返回空矩阵
  - **Priority:** P1

- **Test:** `TestSimilarityMatrix_Get_Symmetric` (22.4-UNIT-008)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 相似度查询对称
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_Compute_NoSelfSimilarity` (22.4-UNIT-009)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 不存储自身相似度
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_UpdateSimilarityMatrix` (22.4-UNIT-010)
  - **Status:** RED - ImmuneDaemon.UpdateSimilarityMatrix 方法不存在
  - **Verifies:** AC1 - 矩阵更新后可查询
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperation` (22.4-UNIT-011)
  - **Status:** RED - ImmuneDaemon.RecordCooperation 方法不存在
  - **Verifies:** AC2 - 协作记录正确影响相似度
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetSimilarAgents_NilDaemon` (22.4-UNIT-012)
  - **Status:** RED - ImmuneDaemon.GetSimilarAgents 方法不存在
  - **Verifies:** AC4 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetSimilarity_NilDaemon` (22.4-UNIT-013)
  - **Status:** RED - ImmuneDaemon.GetSimilarity 方法不存在
  - **Verifies:** AC1 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_AttemptMigration_Success` (22.4-UNIT-014)
  - **Status:** RED - ImmuneDaemon.AttemptMigration 方法不存在
  - **Verifies:** AC3, AC4 - 成功迁移到最佳替代
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_AttemptMigration_NoCandidate` (22.4-UNIT-015)
  - **Status:** RED - ImmuneDaemon.AttemptMigration 方法不存在
  - **Verifies:** AC4 - 无候选时 Success=false
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_AttemptMigration_BelowThreshold` (22.4-UNIT-016)
  - **Status:** RED - ImmuneDaemon.AttemptMigration 方法不存在
  - **Verifies:** AC4 - 相似度低于阈值时迁移放弃
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_AttemptMigration_ReputationWeighted` (22.4-UNIT-017)
  - **Status:** RED - ImmuneDaemon.AttemptMigration 方法不存在
  - **Verifies:** AC4 - 声誉分数影响选择
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_AttemptMigration_NilDaemon` (22.4-UNIT-018)
  - **Status:** RED - ImmuneDaemon.AttemptMigration 方法不存在
  - **Verifies:** AC3 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestSimilarityMatrix_ConcurrentAccess` (22.4-UNIT-019)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 并发读取不竞态
  - **Priority:** P1

- **Test:** `TestMinMigrationSimilarity_Value` (22.4-UNIT-020)
  - **Status:** RED - MinMigrationSimilarity 常量不存在
  - **Verifies:** AC4 - 最小迁移相似度阈值为 0.3
  - **Priority:** P0

- **Test:** `TestCapabilitySimilarity_Fields` (22.4-UNIT-021)
  - **Status:** RED - CapabilitySimilarity 类型不存在
  - **Verifies:** AC1 - 结构体字段完整
  - **Priority:** P0

- **Test:** `TestMigrationResult_Fields` (22.4-UNIT-022)
  - **Status:** RED - MigrationResult 类型不存在
  - **Verifies:** AC3 - 结构体字段完整
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperation_Bidirectional` (22.4-UNIT-023)
  - **Status:** RED - ImmuneDaemon.RecordCooperation 方法不存在
  - **Verifies:** AC2 - 协作记录双向（A->B 和 B->A）
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_UpdateSimilarityMatrix_NilDaemon` (22.4-UNIT-024)
  - **Status:** RED - ImmuneDaemon.UpdateSimilarityMatrix 方法不存在
  - **Verifies:** AC1 - nil daemon 安全
  - **Priority:** P1

- **Test:** `TestSimilarityMatrix_Compute_SingleAgent` (22.4-UNIT-025)
  - **Status:** RED - SimilarityMatrix 类型不存在
  - **Verifies:** AC1 - 单 Agent 时矩阵为空
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_22_4_similarity_ipc_test.go (6 tests)

**File:** `ipc/atdd_22_4_similarity_ipc_test.go`

- **Test:** `TestMethodSimilarityQuery_Constant` (22.4-IPC-001)
  - **Status:** RED - MethodSimilarityQuery 常量不存在
  - **Verifies:** AC6 - Method 常量定义
  - **Priority:** P0

- **Test:** `TestSimilarityQueryRequest_Serialization` (22.4-IPC-002)
  - **Status:** RED - SimilarityQueryRequest 类型不存在
  - **Verifies:** AC6 - 请求 JSON 序列化/反序列化
  - **Priority:** P0

- **Test:** `TestSimilarityQueryResponse_Serialization` (22.4-IPC-003)
  - **Status:** RED - SimilarityQueryResponse 类型不存在
  - **Verifies:** AC6 - 响应 JSON 序列化/反序列化
  - **Priority:** P0

- **Test:** `TestSimilarityQueryRequest_JSONFieldNames` (22.4-IPC-004)
  - **Status:** RED - SimilarityQueryRequest 类型不存在
  - **Verifies:** AC6 - JSON 字段使用 snake_case
  - **Priority:** P0

- **Test:** `TestSimilarityQueryResponse_EmptySimilarities` (22.4-IPC-005)
  - **Status:** RED - SimilarityQueryResponse 类型不存在
  - **Verifies:** AC6 - 空数组序列化为 [] 而非 null
  - **Priority:** P1

- **Test:** `TestSimilarityQueryResponse_BackwardCompatible` (22.4-IPC-006)
  - **Status:** RED - SimilarityQueryResponse 类型不存在
  - **Verifies:** AC6 - 旧版 JSON 反序列化兼容
  - **Priority:** P0

### CLI Tests - cmd/rnix/atdd_22_4_similarity_cmd_test.go (6 tests)

**File:** `cmd/rnix/atdd_22_4_similarity_cmd_test.go`

- **Test:** `TestRunImmuneSimilarity_TextOutput` (22.4-CLI-001)
  - **Status:** RED - formatSimilarityText 函数不存在
  - **Verifies:** AC7 - 文本格式显示相似度排行榜
  - **Priority:** P0

- **Test:** `TestRunImmuneSimilarity_JSONOutput` (22.4-CLI-002)
  - **Status:** RED - immuneSimilarityCmd 不存在（编译失败）
  - **Verifies:** AC7 - JSON 格式输出
  - **Priority:** P0

- **Test:** `TestRunImmuneSimilarity_NoDaemon` (22.4-CLI-003)
  - **Status:** RED - immuneSimilarityCmd 不存在（编译失败）
  - **Verifies:** AC7 - 无 daemon 时错误提示
  - **Priority:** P0

- **Test:** `TestImmuneSimilarityCmd_Registered` (22.4-CLI-004)
  - **Status:** RED - immuneSimilarityCmd 不存在（编译失败）
  - **Verifies:** AC7 - similarity 子命令注册在 immune 下
  - **Priority:** P0

- **Test:** `TestRunImmuneSimilarity_TextOutput_SortedByScore` (22.4-CLI-005)
  - **Status:** RED - formatSimilarityText 函数不存在
  - **Verifies:** AC7 - 输出按 Score 降序排列
  - **Priority:** P1

- **Test:** `TestRunImmuneSimilarity_TextOutput_Empty` (22.4-CLI-006)
  - **Status:** RED - formatSimilarityText 函数不存在
  - **Verifies:** AC7 - 无相似 Agent 时的输出
  - **Priority:** P1

---

## Implementation Checklist

### Task 1: 能力相似度矩阵核心数据结构 (kernel/immune.go)

**Tests to make pass:** 22.4-UNIT-020, 22.4-UNIT-021, 22.4-UNIT-022

- [ ] 新增 `CapabilitySimilarity` 结构体（AgentA, AgentB, SkillScore, CoopScore, Score）
- [ ] 新增 `MigrationResult` 结构体（OriginalPID, OriginalAgent, TargetAgent, NewPID, Similarity, DurationMs, Success, Reason）
- [ ] 新增 `MinMigrationSimilarity = 0.3` 常量
- [ ] Run: `go test -race -run "TestMinMigrationSimilarity_Value|TestCapabilitySimilarity_Fields|TestMigrationResult_Fields" ./kernel/...`

### Task 2: SimilarityMatrix 核心算法 (kernel/immune.go)

**Tests to make pass:** 22.4-UNIT-001 ~ 22.4-UNIT-009, 22.4-UNIT-019, 22.4-UNIT-025

- [ ] 新增 `SimilarityMatrix` 结构体：`mu sync.RWMutex` + `entries map[string]map[string]*CapabilitySimilarity`
- [ ] `NewSimilarityMatrix()` 构造函数
- [ ] `Compute(agents map[string][]string, coopHistory map[string]map[string]int)` — Jaccard 系数 + 协作归一化
- [ ] `Get(agentA, agentB string) *CapabilitySimilarity` — 查询
- [ ] `GetSimilar(agentName string, minScore float64) []CapabilitySimilarity` — 过滤+降序排序
- [ ] 自身相似度不存储（跳过 agentA == agentB）
- [ ] 对称性：A-B 和 B-A 存储相同结果
- [ ] Run: `go test -race -run "TestSimilarityMatrix" ./kernel/...`

### Task 3: ImmuneDaemon 集成相似度矩阵 (kernel/immune.go)

**Tests to make pass:** 22.4-UNIT-010, 22.4-UNIT-011, 22.4-UNIT-012, 22.4-UNIT-013, 22.4-UNIT-023, 22.4-UNIT-024

- [ ] `ImmuneDaemon` 新增 `similarity *SimilarityMatrix` 和 `coopHistory map[string]map[string]int` 字段
- [ ] `UpdateSimilarityMatrix(agents map[string][]string)` — 使用 coopHistory 重新计算矩阵
- [ ] `RecordCooperation(agentA, agentB string)` — 双向记录协作事件
- [ ] `GetSimilarity(agentA, agentB string) *CapabilitySimilarity` — 委托查询
- [ ] `GetSimilarAgents(agentName string, minScore float64) []CapabilitySimilarity` — 委托查询
- [ ] 所有方法 nil daemon 安全
- [ ] Run: `go test -race -run "TestImmuneDaemon_UpdateSimilarityMatrix|TestImmuneDaemon_RecordCooperation|TestImmuneDaemon_GetSimilar|TestImmuneDaemon_GetSimilarity" ./kernel/...`

### Task 4: 能力迁移引擎 (kernel/immune.go)

**Tests to make pass:** 22.4-UNIT-014, 22.4-UNIT-015, 22.4-UNIT-016, 22.4-UNIT-017, 22.4-UNIT-018

- [ ] 新增 `MigrateFunc func(intent string, agentName string, contextMessages []string) (types.PID, error)` 类型
- [ ] `SetMigrateFunc(fn MigrateFunc)` — 注入迁移执行函数
- [ ] `SetReputationStore(rs *ReputationStore)` — 注入声誉存储
- [ ] `AttemptMigration(pid types.PID, agentTemplate string, intent string, contextMsgs []string) *MigrationResult`
  - 从 SimilarityMatrix 获取 GetSimilarAgents(agentTemplate, MinMigrationSimilarity)
  - 结合 ReputationStore.GetSummary() 排序候选（similarity * 0.6 + reputation * 0.4）
  - 调用 migrateFunc 在最佳候选上 Spawn 新进程
  - nil daemon 返回 nil
- [ ] Run: `go test -race -run "TestImmuneDaemon_AttemptMigration" ./kernel/...`

### Task 5: IPC 协议扩展 (ipc/protocol.go)

**Tests to make pass:** 22.4-IPC-001 ~ 22.4-IPC-006

- [ ] 新增 `MethodSimilarityQuery Method = "similarity_query"` 常量
- [ ] 新增 `SimilarityQueryRequest` 结构体（AgentName, MinScore）
- [ ] 新增 `SimilarityQueryResponse` 结构体（Agent, Similarities）
- [ ] Run: `go test -race -run "TestMethodSimilarityQuery|TestSimilarityQueryRequest|TestSimilarityQueryResponse" ./ipc/...`

### Task 6: IPC Server handler (ipc/server.go)

**Tests to make pass:** (集成测试 - 需 daemon 运行)

- [ ] 注册 `MethodSimilarityQuery` → `handleSimilarityQuery` handler
- [ ] `handleSimilarityQuery` 调用 `immuneDaemon.GetSimilarAgents()`

### Task 7: IPC Client 方法 (ipc/client.go)

- [ ] 新增 `Client.SimilarityQuery(agentName string, minScore float64) (*SimilarityQueryResponse, error)`

### Task 8: CLI 命令实现 (cmd/rnix/immune.go)

**Tests to make pass:** 22.4-CLI-001 ~ 22.4-CLI-006

- [ ] 新增 `immuneSimilarityCmd` — `rnix immune similarity [agent-name]`
- [ ] 新增 `formatSimilarityText(w io.Writer, resp *SimilarityQueryResponse)` 格式化函数
- [ ] 文本输出：表头 AGENT / SIMILARITY / SKILL OVERLAP + 按 Score 降序
- [ ] JSON 输出：`JSONResponse{OK: true, Data: resp}`
- [ ] 在 `init()` 中注册 `immuneCmd.AddCommand(immuneSimilarityCmd)`
- [ ] Run: `go test -race -run "TestRunImmuneSimilarity|TestImmuneSimilarityCmd" ./cmd/rnix/...`

---

## Test Summary

| Category | Test File | Count | Priority |
|----------|----------|-------|----------|
| Unit (kernel) | `kernel/atdd_22_4_capability_migration_test.go` | 25 | 17 P0, 8 P1 |
| IPC | `ipc/atdd_22_4_similarity_ipc_test.go` | 6 | 5 P0, 1 P1 |
| CLI | `cmd/rnix/atdd_22_4_similarity_cmd_test.go` | 6 | 4 P0, 2 P1 |
| **Total** | | **37** | **26 P0, 11 P1** |

---

## AC Coverage Matrix

| AC | Description | Tests |
|----|------------|-------|
| AC1 | 能力相似度矩阵计算 | 22.4-UNIT-001, 002, 003, 005, 006, 007, 008, 009, 010, 013, 019, 020, 021, 024, 025 |
| AC2 | 历史协作记录纳入相似度 | 22.4-UNIT-004, 011, 023 |
| AC3 | Supervisor 重启失败触发能力迁移 | 22.4-UNIT-014, 018, 022 |
| AC4 | 最佳替代 Agent 选择 | 22.4-UNIT-012, 014, 015, 016, 017, 020 |
| AC5 | 迁移性能约束 | (NFR 约束，由 MigrationResult.DurationMs 字段支持运行时验证) |
| AC6 | IPC 查询接口 | 22.4-IPC-001, 002, 003, 004, 005, 006 |
| AC7 | CLI 展示 | 22.4-CLI-001, 002, 003, 004, 005, 006 |

---

## Key Risks & Assumptions

1. **Jaccard 系数为纯集合计算**：不考虑 Skill 权重，所有 Skill 等权。测试通过精确数学验证。
2. **协作历史为纯内存**：daemon 重启后从零积累。不引入持久化。
3. **MigrateFunc 通过注入解耦**：ImmuneDaemon 不直接依赖 KernelImpl.Spawn，通过函数注入保持依赖方向。测试使用 mock 函数。
4. **ReputationStore 复用 21.3**：直接调用 GetSummary，不修改声誉系统。
5. **MinMigrationSimilarity = 0.3**：低于此阈值的候选不考虑。测试验证边界。
6. **向后兼容**：SimilarityQueryResponse 新增类型不影响旧客户端（IPC-006 验证）。
7. **AC5 性能约束**：测试不直接验证 10s 限制（单元测试环境不稳定），但 MigrationResult.DurationMs 字段支持运行时监控。

## Next Step

推荐执行 `dev-story` 工作流实现 Story 22.4，按 Implementation Checklist 中的 Task 顺序依次将测试从 RED 变为 GREEN。
