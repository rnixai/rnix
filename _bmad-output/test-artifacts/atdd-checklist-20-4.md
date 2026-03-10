---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-10'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/20-4-progressive-specialization-and-differentiation-memory.md'
  - 'kernel/kernel.go'
  - 'kernel/ooda.go'
  - 'kernel/stem.go'
  - 'kernel/process.go'
  - 'kernel/kernel_test.go'
  - 'kernel/ooda_test.go'
  - 'kernel/ooda_reasoning_test.go'
  - 'kernel/stem_integration_test.go'
  - 'context/context.go'
---

# ATDD Checklist - Epic 20, Story 20-4: 渐进式特化与分化记忆

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

分化后的智能体可以在执行过程中动态加载额外 Skill，并记忆分化路径，使智能体能力按需扩展，且相似任务可快速复用上次分化结果。

**As a** 平台构建者
**I want** 分化后的智能体可以在执行过程中动态加载额外 Skill，并记忆分化路径
**So that** 智能体能力可以按需扩展，且相似任务可快速复用上次分化结果

---

## Acceptance Criteria

1. **AC#1**: 已分化智能体检测到能力缺口时，动态加载额外 Skill 进一步特化，不中断执行
2. **AC#2**: 分化路径被记录为"表观遗传"记忆：哪些 Skill 被加载、加载顺序、触发意图
3. **AC#3**: 下次相似意图时，系统优先复用上次记录的分化路径，加速分化过程

---

## Test Strategy

### Stack Detection
- **Detected Stack**: `backend` (Go 1.26, `go.mod` present, no `package.json`)
- **Test Framework**: Go `testing` package with `-race` flag
- **No E2E/Browser tests** needed

### Test Levels
| Level | Count | Purpose |
|-------|-------|---------|
| Unit | 13 | DiffMemory Record/Lookup/normalize/eviction/concurrency |
| Integration | 10 | Spawn+DiffMemory, OODA specialize, E2E lifecycle |
| **Total** | **23** | |

### Priority Matrix
| Priority | Tests | Description |
|----------|-------|-------------|
| P0 | 6 | 核心分化记忆：RecordAndLookup, NormalizedIntent, EvictionPolicy, Spawn RecordAndReuse |
| P1 | 10 | OODA Specialize 全场景：LoadSkill, AlreadyLoaded, SkillNotFound, UpdatesDevices, InjectsBody, RecordsToDiffMemory |
| P2 | 5 | 边界情况：EmptyIntent, EmptySkillList, ConcurrentAccess, FallbackToMatch |
| P3 | 2 | 端到端集成：ProgressiveSpecialization, NormalizedIntentReuse |

---

## Generation Mode

- **Mode**: AI Generation (backend Go project, no browser recording needed)
- **Execution**: Sequential (Go test files created directly)

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/diffmemory_test.go (13 tests)

**File:** `kernel/diffmemory_test.go` (226 lines)

- **[P0] TestDiffMemory_RecordAndLookup**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#2 - 记录分化路径后可精确查找

- **[P0] TestDiffMemory_NormalizedIntent**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#3 - 意图重排后匹配同一条目 ("analyze code" == "code analyze")

- **[P1] TestDiffMemory_NormalizedIntent_CaseInsensitive**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#3 - 大小写不敏感匹配

- **[P1] TestDiffMemory_UpdateExisting_SameSkills**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#2 - 重复记录更新 HitCount 和 Timestamp

- **[P1] TestDiffMemory_UpdateExisting_DifferentSkills**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#2 - skill 列表不同时替换为新列表

- **[P0] TestDiffMemory_EvictionPolicy**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** AC#2 - 超过 maxSize 时淘汰低频旧条目

- **[P0] TestDiffMemory_LookupNotFound**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** 未记录意图返回 false

- **[P2] TestDiffMemory_LookupNotFound_AfterEviction**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** 淘汰后查找返回 false

- **[P2] TestDiffMemory_ConcurrentAccess**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** 100 goroutine 并发读写安全 (-race)

- **[P2] TestDiffMemory_EmptyIntent**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** 空意图边界处理

- **[P2] TestDiffMemory_EmptySkillList**
  - **Status:** RED - `undefined: NewDiffMemory`
  - **Verifies:** 空 skill 列表边界处理

- **[P0] TestNormalizeIntent_TokenSort**
  - **Status:** RED - `undefined: normalizeIntent`
  - **Verifies:** AC#3 - token 排序签名一致性

- **[P1] TestNormalizeIntent_Deduplication**
  - **Status:** RED - `undefined: normalizeIntent`
  - **Verifies:** token 去重

### Integration Tests - kernel/diffmemory_integration_test.go (10 tests)

**File:** `kernel/diffmemory_integration_test.go` (480 lines)

- **[P0] TestSpawn_StemAgentDifferentiationMemory_RecordAndReuse**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#2,#3 - 首次 spawn 记录，第二次复用

- **[P2] TestSpawn_StemAgentDifferentiationMemory_FallbackToMatch**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#3 - 无记忆时降级为关键词匹配

- **[P1] TestSpawn_StemAgentDifferentiationMemory_EventFromMemory**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#3 - 记忆命中时 stem matcher 不被调用

- **[P1] TestOODA_Specialize_LoadSkill**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** AC#1 - OODA decide 返回 specialize，成功加载 skill

- **[P1] TestOODA_Specialize_AlreadyLoaded**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** AC#1 - 尝试加载已有 skill 不重复

- **[P1] TestOODA_Specialize_SkillNotFound**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** AC#1 - 加载不存在 skill 优雅处理

- **[P1] TestOODA_Specialize_UpdatesAllowedDevices**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** AC#1 - 新 skill 工具权限被追加到 AllowedDevices

- **[P1] TestOODA_Specialize_InjectsBody**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** AC#1 - skill body 注入上下文

- **[P1] TestOODA_Specialize_RecordsToDiffMemory**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#1,#2 - 动态特化更新分化记忆

- **[P0] TestOODADecision_SpecializeType**
  - **Status:** RED - `undefined: OODASpecialize`
  - **Verifies:** OODASpecialize 常量 = "specialize"

### E2E Integration Tests (in diffmemory_integration_test.go, 3 tests)

- **[P3] TestE2E_StemDifferentiation_ProgressiveSpecialization**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#1,#2 - stem 初始分化 + OODA 动态 specialize + 记忆全链路

- **[P0] TestE2E_StemDifferentiation_MemoryReuse**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#3 - 两次相同意图 spawn，第二次使用记忆路径

- **[P3] TestE2E_StemDifferentiation_NormalizedIntentReuse**
  - **Status:** RED - `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`
  - **Verifies:** AC#3 - "analyze code" 和 "code analyze" 命中同一记忆条目

---

## Implementation Checklist

### Test: TestDiffMemory_* (Unit tests, 13 tests)

**File:** `kernel/diffmemory_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `kernel/diffmemory.go`，实现 `DiffMemoryEntry` 和 `DiffMemory` 结构体
- [ ] 实现 `NewDiffMemory(maxSize int) *DiffMemory` 构造函数
- [ ] 实现 `Record(intent string, skills []string)` 方法（含 LRU 淘汰）
- [ ] 实现 `Lookup(intent string) ([]string, bool)` 方法（含 HitCount 更新）
- [ ] 实现 `normalizeIntent(intent string) string`（复用 `tokenize()` + 排序）
- [ ] 运行测试: `go test -race -run TestDiffMemory ./kernel/...`
- [ ] 运行测试: `go test -race -run TestNormalizeIntent ./kernel/...`

**Estimated Effort:** 2 hours

---

### Test: TestSpawn_StemAgentDifferentiationMemory_* (3 tests)

**File:** `kernel/diffmemory_integration_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `KernelImpl` 新增 `diffMemory *DiffMemory` 字段
- [ ] 新增 `SetDiffMemory(m *DiffMemory)` setter
- [ ] 修改 `Spawn` 中 stem 分化代码块：在 `stemMatcher.Match` 前插入 `diffMemory.Lookup`
- [ ] 在 stem 分化代码块末尾添加 `diffMemory.Record` 调用
- [ ] 更新 `StemDifferentiate` 事件 args 添加 `from_memory` 字段
- [ ] 运行测试: `go test -race -run TestSpawn_StemAgentDifferentiationMemory ./kernel/...`

**Estimated Effort:** 1.5 hours

---

### Test: TestOODA_Specialize_* (6 tests)

**File:** `kernel/diffmemory_integration_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/ooda.go` 新增 `OODASpecialize OODAActionType = "specialize"` 常量
- [ ] 更新 `oodaDecidePromptTemplate` 添加 specialize 选项描述
- [ ] 在 `oodaAct` switch 添加 `case OODASpecialize` 分支
- [ ] 实现 `oodaActSpecialize(proc *Process, decision *OODADecision) string`
  - 检查 skill 是否已加载（避免重复）
  - 调用 `k.skillLoader` 加载 skill
  - 更新 `proc.Skills` 和 `proc.AllowedDevices`（mu 保护）
  - 通过 `ctxMgr.AppendMessage` 注入 skill body
  - 记录到 DiffMemory
  - 发出 `StemSpecialize` 事件
- [ ] 运行测试: `go test -race -run TestOODA_Specialize ./kernel/...`

**Estimated Effort:** 2.5 hours

---

### Test: TestE2E_StemDifferentiation_* (3 tests)

**File:** `kernel/diffmemory_integration_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/rnix/main.go` daemon 启动中注入 DiffMemory
- [ ] 确保 stem 分化 + OODA specialize + DiffMemory 全链路连通
- [ ] 运行测试: `go test -race -run TestE2E_StemDifferentiation ./kernel/...`

**Estimated Effort:** 1 hour

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run 'TestDiffMemory|TestNormalizeIntent|TestOODADecision_SpecializeType|TestSpawn_StemAgentDifferentiationMemory|TestOODA_Specialize|TestE2E_StemDifferentiation' ./kernel/...

# Run unit tests only
go test -race -run 'TestDiffMemory|TestNormalizeIntent' ./kernel/...

# Run integration tests only
go test -race -run 'TestSpawn_StemAgentDifferentiationMemory|TestOODA_Specialize|TestE2E_StemDifferentiation' ./kernel/...

# Run with verbose output
go test -race -v -run 'TestDiffMemory|TestNormalizeIntent|TestOODADecision_SpecializeType|TestSpawn_StemAgentDifferentiationMemory|TestOODA_Specialize|TestE2E_StemDifferentiation' ./kernel/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 23 tests written and failing (compilation errors: undefined types/functions)
- Test files created: `kernel/diffmemory_test.go`, `kernel/diffmemory_integration_test.go`
- Tests cover all 3 acceptance criteria across unit and integration levels
- No data factories/fixtures needed (Go testing with mocks)

**Verification:**

- All tests fail with `undefined: NewDiffMemory`, `k.SetDiffMemory undefined`, `undefined: OODASpecialize`, `undefined: normalizeIntent`
- Failures are due to missing implementation, not test bugs
- Other packages (`ipc`, `vfs`, `internal`, `context`, `skills`) still pass

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with Task 1** (DiffMemory unit tests) -- create `kernel/diffmemory.go`
2. **Then Task 2** (Spawn integration) -- modify `kernel/kernel.go`
3. **Then Task 3** (OODA specialize) -- modify `kernel/ooda.go`
4. **Finally Task 4** (E2E + daemon injection) -- modify `cmd/rnix/main.go`

**Key Principles:**

- One test group at a time (unit -> integration -> E2E)
- Minimal implementation to pass each group
- Run tests frequently with `-race` flag

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- Extract common test helpers if patterns emerge
- Ensure DiffMemory LRU performance is acceptable
- Verify concurrent access with stress tests

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run 'TestDiffMemory|TestNormalizeIntent|TestOODADecision_SpecializeType|TestSpawn_StemAgentDifferentiationMemory|TestOODA_Specialize|TestE2E_StemDifferentiation' ./kernel/...`

**Results:**

```
# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/diffmemory_integration_test.go:49:8: undefined: NewDiffMemory
kernel/diffmemory_integration_test.go:50:4: k.SetDiffMemory undefined (type *KernelImpl has no field or method SetDiffMemory)
kernel/diffmemory_test.go:32:8: undefined: NewDiffMemory
...
FAIL	github.com/rnixai/rnix/kernel [build failed]
```

**Summary:**

- Total tests: 23
- Passing: 0 (expected)
- Failing: 23 (expected - compilation errors)
- Status: RED phase verified

**Expected Failure Messages:**
- `undefined: NewDiffMemory` -- DiffMemory 结构体未创建
- `k.SetDiffMemory undefined` -- KernelImpl setter 未添加
- `undefined: OODASpecialize` -- OODA action 常量未定义
- `undefined: normalizeIntent` -- 意图规范化函数未创建

---

## Knowledge Base References Applied

This ATDD workflow consulted the following knowledge fragments:

- **test-quality.md** -- 测试设计原则（Given-When-Then, 原子测试, 确定性, 隔离性）
- **test-levels-framework.md** -- 测试层级选择框架（Unit vs Integration for backend）
- **test-priorities-matrix.md** -- P0-P3 优先级矩阵
- **data-factories.md** -- Go 测试中的 mock 模式参考（无需 faker，使用 mock 函数注入）

---

## Notes

- Go 后端项目使用编译错误作为 RED phase 验证（类型/函数不存在 = 测试失败）
- DiffMemory 测试复用 `kernel/stem_integration_test.go` 和 `kernel/ooda_reasoning_test.go` 的 mock 模式
- 并发安全测试通过 `-race` flag 验证，100 goroutine 并发读写
- `normalizeIntent` 测试直接调用该函数（同包内访问），验证与 `tokenize()` 的一致性

---

**Generated by BMad TEA Agent** - 2026-03-10
