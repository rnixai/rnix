---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/21-5-skill-combination-matrix.md'
  - 'kernel/reputation.go'
  - 'compose/engine.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/rnix/reputation.go'
  - 'kernel/sla.go'
  - 'compose/types.go'
---

# ATDD Checklist - Epic 21, Story 5: Skill 组合矩阵

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统维护 Skill 组合矩阵记录历史表现，可通过 `rnix synergy list` 命令查看有效组合。包括组合执行记录、统计汇总、对比分析、推荐标记、JSON 输出和空数据处理。

**As a** 平台构建者
**I want** 系统维护 Skill 组合矩阵记录历史表现，可通过命令查看有效组合
**So that** 我可以了解哪些 Skill 组合效果最好

---

## Acceptance Criteria

1. **AC1: Synergy 组合执行记录** - 智能体加载多个 Skill 并完成执行后，SLA 评估完成时将组合及结果记录到组合矩阵存储
2. **AC2: `rnix synergy list` 命令** - 展示已知的有效 Skill 组合，包含成功率、平均 token 消耗、使用频次，按推荐度排序
3. **AC3: 组合 vs 单独表现对比** - 每条组合显示组合成功率 vs 各 Skill 单独平均成功率、token 效率提升百分比
4. **AC4: 推荐组合标记** - 组合成功率比各 Skill 单独平均成功率高出 10% 以上时标记为推荐
5. **AC5: JSON 输出支持** - `rnix synergy list --json` 输出符合 JSONResponse[T] 格式
6. **AC6: 空数据优雅处理** - 无历史数据时显示友好消息，不报错、不 panic
7. **AC7: 向后兼容** - 不影响现有声誉系统功能和数据格式

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_21_5_synergy_matrix_test.go (17 tests)

**File:** `kernel/atdd_21_5_synergy_matrix_test.go`

- **Test:** `TestNewComboKey_SortedDeterministic` (21.5-UNIT-001)
  - **Status:** RED - NewComboKey 函数不存在
  - **Verifies:** AC1 - {B,A} 和 {A,B} 生成相同 key（排序确定性）
  - **Priority:** P0

- **Test:** `TestNewComboKey_SingleSkill` (21.5-UNIT-002)
  - **Status:** RED - NewComboKey 函数不存在
  - **Verifies:** AC1 - 单 Skill 也能正常生成 key
  - **Priority:** P0

- **Test:** `TestNewComboKey_Empty` (21.5-UNIT-003)
  - **Status:** RED - NewComboKey 函数不存在
  - **Verifies:** AC1 - 空列表返回空字符串
  - **Priority:** P1

- **Test:** `TestSynergyMatrix_RecordAndRead` (21.5-UNIT-004)
  - **Status:** RED - NewSynergyMatrix, SynergyRecord 不存在
  - **Verifies:** AC1 - 写入 3 条记录，读回全部
  - **Priority:** P0

- **Test:** `TestSynergyMatrix_EmptyFile` (21.5-UNIT-005)
  - **Status:** RED - NewSynergyMatrix 不存在
  - **Verifies:** AC6 - 文件不存在时返回空切片
  - **Priority:** P0

- **Test:** `TestSynergyMatrix_ConcurrentWrites` (21.5-UNIT-006)
  - **Status:** RED - NewSynergyMatrix, SynergyRecord 不存在
  - **Verifies:** AC1 - 多 goroutine 并发写入不丢数据
  - **Priority:** P1

- **Test:** `TestGetComboSummaries_BasicStats` (21.5-UNIT-007)
  - **Status:** RED - ComboSummary, GetComboSummaries 不存在
  - **Verifies:** AC2/AC3 - 成功率、平均 token、执行次数计算
  - **Priority:** P0

- **Test:** `TestGetComboSummaries_Recommended` (21.5-UNIT-008)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC4 - 组合优于单 Skill 10% 以上标记推荐
  - **Priority:** P0

- **Test:** `TestGetComboSummaries_NotRecommended` (21.5-UNIT-009)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC4 - 差距不足 10% 不标记推荐
  - **Priority:** P0

- **Test:** `TestGetComboSummaries_NoSoloData` (21.5-UNIT-010)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC4 - 无单 Skill 数据时 AvgSoloRate=0, Recommended=false
  - **Priority:** P1

- **Test:** `TestGetComboSummaries_SortOrder` (21.5-UNIT-011)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC2 - 推荐在前、成功率降序
  - **Priority:** P0

- **Test:** `TestGetComboSummaries_Empty` (21.5-UNIT-012)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC6 - 空数据返回空切片
  - **Priority:** P0

- **Test:** `TestSynergyRecord_JSONSerialization` (21.5-UNIT-013)
  - **Status:** RED - SynergyRecord 不存在
  - **Verifies:** AC5 - JSON 字段 snake_case 格式
  - **Priority:** P1

- **Test:** `TestComboSummary_JSONSerialization` (21.5-UNIT-014)
  - **Status:** RED - ComboSummary 不存在
  - **Verifies:** AC5 - JSON 字段 snake_case 格式
  - **Priority:** P1

- **Test:** `TestSynergyMatrix_FilePersistence` (21.5-UNIT-015)
  - **Status:** RED - NewSynergyMatrix 不存在
  - **Verifies:** AC7 - 数据跨实例持久化（模拟 daemon 重启）
  - **Priority:** P1

- **Test:** `TestSynergyMatrix_FilePathInReputationDir` (21.5-UNIT-016)
  - **Status:** RED - NewSynergyMatrix 不存在
  - **Verifies:** AC7 - 文件存放在 reputation 目录下
  - **Priority:** P1

- **Test:** `TestGetComboSummaries_TokenImprovement` (21.5-UNIT-017)
  - **Status:** RED - GetComboSummaries 不存在
  - **Verifies:** AC3 - token 效率提升百分比计算
  - **Priority:** P1

### Integration Tests - compose/atdd_21_5_synergy_engine_test.go (2 tests)

**File:** `compose/atdd_21_5_synergy_engine_test.go`

- **Test:** `TestEngine_SetSynergyMatrix` (21.5-INT-001)
  - **Status:** RED - Engine.SetSynergyMatrix 方法不存在
  - **Verifies:** AC1 - Compose 引擎 setter 正确设置 SynergyMatrix
  - **Priority:** P0

- **Test:** `TestEngine_SetSynergyMatrix_Nil` (21.5-INT-002)
  - **Status:** RED - Engine.SetSynergyMatrix 方法不存在
  - **Verifies:** AC7 - nil SynergyMatrix 不 panic
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_21_5_synergy_ipc_test.go (5 tests)

**File:** `ipc/atdd_21_5_synergy_ipc_test.go`

- **Test:** `TestMethodSynergyList_Constant` (21.5-IPC-001)
  - **Status:** RED - MethodSynergyList 常量不存在
  - **Verifies:** AC2 - 常量值为 "synergy_list"
  - **Priority:** P0

- **Test:** `TestSynergyListRequest_TypeExists` (21.5-IPC-002)
  - **Status:** RED - SynergyListRequest 类型不存在
  - **Verifies:** AC2 - 请求类型编译通过
  - **Priority:** P0

- **Test:** `TestSynergyListResponse_CombosField` (21.5-IPC-003)
  - **Status:** RED - SynergyListResponse, ComboSummary 不存在
  - **Verifies:** AC2/AC5 - 响应包含 combos 字段
  - **Priority:** P0

- **Test:** `TestSynergyListResponse_EmptyCombos` (21.5-IPC-004)
  - **Status:** RED - SynergyListResponse 不存在
  - **Verifies:** AC6 - 空 combos 序列化为 [] 而非 null
  - **Priority:** P0

- **Test:** `TestClient_SynergyList_MethodExists` (21.5-IPC-005)
  - **Status:** RED - Client.SynergyList 方法不存在
  - **Verifies:** AC2 - 客户端方法签名正确
  - **Priority:** P1

### CLI Tests - cmd/rnix/atdd_21_5_synergy_cmd_test.go (4 tests)

**File:** `cmd/rnix/atdd_21_5_synergy_cmd_test.go`

- **Test:** `TestRunSynergyList_NoData` (21.5-CLI-001)
  - **Status:** RED - synergyCmd 不存在
  - **Verifies:** AC6 - 无数据时显示 "No synergy combination data available."
  - **Priority:** P0

- **Test:** `TestRunSynergyList_JSON` (21.5-CLI-002)
  - **Status:** RED - synergyCmd 不存在
  - **Verifies:** AC5 - JSON 模式输出正确格式
  - **Priority:** P0

- **Test:** `TestSynergyCmd_Registered` (21.5-CLI-003)
  - **Status:** RED - synergyCmd 不存在
  - **Verifies:** AC2 - synergy 命令注册 + list 子命令
  - **Priority:** P1

- **Test:** `TestRunSynergyList_TableColumns` (21.5-CLI-004)
  - **Status:** RED - synergyListCmd 不存在
  - **Verifies:** AC2 - 表格模式正确列
  - **Priority:** P1

---

## Implementation Checklist

### Task 1: SynergyRecord 数据类型 (kernel/synergy_matrix.go)

**Tests to make pass:** 21.5-UNIT-001 ~ 21.5-UNIT-003, 21.5-UNIT-013

- [ ] 定义 `SynergyComboKey` 类型
- [ ] 实现 `NewComboKey(skills []string) SynergyComboKey` 函数
- [ ] 定义 `SynergyRecord` 结构体（含 JSON tags）
- [ ] Run: `go test -race -run "TestNewComboKey|TestSynergyRecord_JSON" ./kernel/...`

### Task 2: SynergyMatrix 存储引擎 (kernel/synergy_matrix.go)

**Tests to make pass:** 21.5-UNIT-004 ~ 21.5-UNIT-006, 21.5-UNIT-015, 21.5-UNIT-016

- [ ] 实现 `NewSynergyMatrix(reputationDir string) *SynergyMatrix`
- [ ] 实现 `RecordCombo(record SynergyRecord) error`（JSON Lines 追加写入）
- [ ] 实现 `GetAllRecords() ([]SynergyRecord, error)`（逐行读取）
- [ ] Run: `go test -race -run "TestSynergyMatrix_RecordAndRead|TestSynergyMatrix_EmptyFile|TestSynergyMatrix_ConcurrentWrites|TestSynergyMatrix_FilePersistence|TestSynergyMatrix_FilePathInReputationDir" ./kernel/...`

### Task 3: ComboSummary 统计计算 (kernel/synergy_matrix.go)

**Tests to make pass:** 21.5-UNIT-007 ~ 21.5-UNIT-012, 21.5-UNIT-014, 21.5-UNIT-017

- [ ] 定义 `ComboSummary` 结构体（含 JSON tags）
- [ ] 实现 `GetComboSummaries() ([]ComboSummary, error)`
  - 按 ComboKey 分组聚合
  - 计算成功率、平均 token、执行次数
  - 计算 AvgSoloRate（各参与 Skill 的 solo 成功率平均值）
  - 计算 TokenImprovement
  - 判定 Recommended（成功率 > AvgSoloRate + 0.10）
  - 排序：recommended 在前，成功率降序
- [ ] Run: `go test -race -run "TestGetComboSummaries|TestComboSummary_JSON" ./kernel/...`

### Task 4: Compose 引擎集成 (compose/engine.go)

**Tests to make pass:** 21.5-INT-001 ~ 21.5-INT-002

- [ ] Engine 结构体新增 `synergyMatrix *kernel.SynergyMatrix` 字段
- [ ] 新增 `SetSynergyMatrix(m *kernel.SynergyMatrix)` setter
- [ ] executeNode 中 SLA 评估后追加 synergy 记录
- [ ] Run: `go test -race -run "TestEngine_SetSynergyMatrix" ./compose/...`

### Task 5: IPC 协议扩展 (ipc/)

**Tests to make pass:** 21.5-IPC-001 ~ 21.5-IPC-005

- [ ] protocol.go: 新增 `MethodSynergyList` 常量和请求/响应类型
- [ ] server.go: 新增 `handleSynergyList` handler + dispatch 注册
- [ ] client.go: 新增 `SynergyList() (*SynergyListResponse, error)`
- [ ] Run: `go test -race -run "TestMethodSynergyList|TestSynergyListRequest|TestSynergyListResponse|TestClient_SynergyList" ./ipc/...`

### Task 6: CLI 命令 (cmd/rnix/synergy.go)

**Tests to make pass:** 21.5-CLI-001 ~ 21.5-CLI-004

- [ ] 新建 `cmd/rnix/synergy.go`
- [ ] 实现 synergy 命令组 + synergy list 子命令
- [ ] 终端表格输出和 JSON 输出
- [ ] 空数据友好消息
- [ ] Run: `go test -race -run "TestRunSynergyList|TestSynergyCmd_Registered" ./cmd/rnix/...`

### Task 7: Daemon 初始化集成 (cmd/rnix/main.go)

- [ ] runDaemon 中创建 SynergyMatrix 实例
- [ ] 注入到 Engine 和 Server
- [ ] 验证 daemon 启动不受影响

---

## Running Tests

```bash
# Run all Story 21.5 kernel unit tests
go test -race -run "TestNewComboKey|TestSynergyMatrix|TestGetComboSummaries|TestSynergyRecord_JSON|TestComboSummary_JSON" ./kernel/...

# Run all Story 21.5 compose integration tests
go test -race -run "TestEngine_SetSynergyMatrix" ./compose/...

# Run all Story 21.5 IPC tests
go test -race -run "TestMethodSynergyList|TestSynergyListRequest|TestSynergyListResponse|TestClient_SynergyList" ./ipc/...

# Run all Story 21.5 CLI tests
go test -race -run "TestRunSynergyList|TestSynergyCmd_Registered" ./cmd/rnix/...

# Run all Story 21.5 tests
go test -race -run "TestNewComboKey|TestSynergyMatrix|TestGetComboSummaries|TestSynergyRecord_JSON|TestComboSummary_JSON|TestEngine_SetSynergyMatrix|TestMethodSynergyList|TestSynergyListRequest|TestSynergyListResponse|TestClient_SynergyList|TestRunSynergyList|TestSynergyCmd_Registered" ./kernel/... ./compose/... ./ipc/... ./cmd/rnix/...

# Run all tests in affected packages
go test -race ./kernel/... ./compose/... ./ipc/... ./cmd/rnix/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 28 tests written and designed to fail (types/methods do not exist)
- Tests cover all 7 acceptance criteria
- Tests follow existing project patterns (kernel/reputation.go, compose/engine.go, ipc/protocol.go, cmd/rnix/reputation.go)
- Test naming convention: `TestNewComboKey_*`, `TestSynergyMatrix_*`, `TestGetComboSummaries_*`, `TestEngine_SetSynergyMatrix*`, `TestMethodSynergyList_*`, `TestSynergyListResponse_*`, `TestRunSynergyList_*`

**Verification:**

- Tests will fail to compile until:
  - `kernel/synergy_matrix.go` 新建 (SynergyComboKey, NewComboKey, SynergyRecord, SynergyMatrix, ComboSummary, GetComboSummaries)
  - `compose/engine.go` 添加 SetSynergyMatrix 方法
  - `ipc/protocol.go` 添加 MethodSynergyList 常量和类型
  - `ipc/client.go` 添加 SynergyList 方法
  - `cmd/rnix/synergy.go` 新建 (synergyCmd, synergyListCmd)
- Failure is due to missing types/methods/fields, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**Recommended implementation order:**

1. Task 1: SynergyRecord 数据类型（最小改动，仅添加类型和函数）
2. Task 2: SynergyMatrix 存储引擎（新建文件，复用 ReputationStore 的 JSON Lines 模式）
3. Task 3: ComboSummary 统计计算（纯函数，依赖 Task 1 和 Task 2）
4. Task 4: Compose 引擎集成（修改 engine.go，依赖 Task 2）
5. Task 5: IPC 协议扩展（protocol → server → client，依赖 Task 3）
6. Task 6: CLI 命令（新建 synergy.go，依赖 Task 5）
7. Task 7: Daemon 初始化集成（修改 main.go，依赖 Task 2/4/5）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- 验证 SynergyMatrix 文件不影响 ReputationStore 现有数据
- 确认 daemon 重启后 SynergyMatrix 数据不丢失
- 确认并发写入场景下数据一致性
- 验证 JSON 输出格式符合 JSONResponse 标准
- 确认空数据场景所有路径正常

---

## Acceptance Criteria Coverage Matrix

| AC | 测试覆盖 | 测试数 |
|----|---------|--------|
| AC1: Synergy 组合执行记录 | UNIT-001~006, UNIT-015~016, INT-001~002 | 10 |
| AC2: `rnix synergy list` 命令 | UNIT-007, UNIT-011, IPC-001~003, CLI-001, CLI-003~004 | 7 |
| AC3: 组合 vs 单独表现对比 | UNIT-007, UNIT-014, UNIT-017 | 3 |
| AC4: 推荐组合标记 | UNIT-008~010 | 3 |
| AC5: JSON 输出支持 | UNIT-013~014, IPC-003, CLI-002 | 4 |
| AC6: 空数据优雅处理 | UNIT-005, UNIT-012, IPC-004, CLI-001 | 4 |
| AC7: 向后兼容 | UNIT-015~016, INT-002 | 3 |

---

## Knowledge Base References Applied

- **test-levels-framework.md** - 测试级别选择：纯 backend Go 项目使用 Unit + Integration
- **data-factories.md** - 测试数据构造模式（使用 struct literal 和 table-driven 构造测试数据）
- **test-quality.md** - 测试质量原则（Given-When-Then 注释、确定性、隔离性）
- **test-healing-patterns.md** - 测试修复模式参考
- **test-priorities-matrix.md** - P0-P1 优先级分配

---

## Notes

- 所有 17 个 kernel 测试在同一文件中，覆盖 SynergyComboKey、SynergyRecord、SynergyMatrix、ComboSummary 四种类型
- Compose 集成测试 2 个：仅验证 setter 和 nil 保护（实际记录由 kernel 测试覆盖）
- IPC 测试 5 个：覆盖常量、请求/响应类型、客户端方法签名
- CLI 测试 4 个：覆盖空数据消息、JSON 输出、命令注册、表格列
- 所有测试使用 `t.TempDir()` 创建隔离的临时目录，无文件系统残留
- 并发测试 `TestSynergyMatrix_ConcurrentWrites` 使用 10 goroutine x 5 条记录
- 文件持久化测试模拟 daemon 重启场景（两个 SynergyMatrix 实例共用目录）
- 文件命名遵循项目惯例：`atdd_21_5_*.go`
- 复用 21.4 测试模式：编译失败即 RED 状态

---

**Generated by BMad TEA Agent** - 2026-03-14
