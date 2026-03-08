---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/16-2-three-assertion-types.md'
  - 'agtest/types.go'
  - 'agtest/validator.go'
  - 'agtest/validator_test.go'
---

# ATDD Checklist - Epic 16, Story 2: 三种断言类型

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with testify, `-race` detection
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 16-2 approved with 3 clear acceptance criteria (AC #1-3)
- Story 16-1 completed — AssertConfig、OutputAssert、SyscallAssert、QualityAssert 已定义于 `agtest/types.go`
- Test framework configured: Go `testing` + testify across project
- Development environment available

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/16-2-three-assertion-types.md`
- **Acceptance Criteria:** 3 ACs covering 推理断言 (output)、syscall 断言、质量断言 (quality judge)
- **Affected Components:** `agtest/eval.go` (new), `agtest/eval_test.go` (new), `agtest/validator.go` (extend), `agtest/validator_test.go` (extend)
- **Dependencies:** Story 16-1 (types.go AssertConfig 已存在)

### Framework & Existing Patterns
- Existing types in `agtest/types.go` — AssertConfig, OutputAssert, SyscallAssert, QualityAssert
- Existing validator in `agtest/validator.go` — Validate for TestCaseSpec
- Existing testdata patterns in `agtest/testdata/`
- Test pattern: Go table-driven tests, testify assert/require, `t.Helper()`

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (assertion evaluation, validator extension)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | 推理断言 assert_output: contains — 输出包含指定内容，不满足则失败 | Unit (agtest/eval) | P0 |
| AC#2 | syscall 断言 assert_syscalls: includes — 序列包含指定调用，不满足则失败 | Unit (agtest/eval) | P0 |
| AC#3 | 质量断言 assert_quality — 轻量模型评估输出质量，不满足则失败并附评估原因 | Unit (agtest/eval) | P0 |
| All | 断言配置校验扩展 — output/syscalls/quality 空配置校验 | Unit (agtest/validator) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests (eval) | 19 | EvalOutput, EvalSyscalls, EvalQuality, EvalAssertions, 边界与组合 |
| Unit Tests (validator) | 5 | 断言配置校验扩展 |
| **Total** | **24** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — agtest/eval_test.go

**File:** `agtest/eval_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestEvalOutput_ContainsAll_Pass` | #1 | P0 | contains 列表全部匹配时通过 |
| 2 | `TestEvalOutput_ContainsMissing_Fail` | #1 | P0 | contains 缺失时失败，Message 清晰 |
| 3 | `TestEvalOutput_NotContainsFound_Fail` | #1 | P0 | not_contains 命中时失败 |
| 4 | `TestEvalOutput_Mixed` | #1 | P0 | contains 与 not_contains 混合场景 |
| 5 | `TestEvalOutput_NilAssert` | #1 | P1 | assert 为 nil 时返回空切片 |
| 6 | `TestEvalSyscalls_IncludesAll_Pass` | #2 | P0 | includes 全部存在时通过 |
| 7 | `TestEvalSyscalls_IncludesMissing_Fail` | #2 | P0 | includes 缺失时失败 |
| 8 | `TestEvalSyscalls_ExcludesFound_Fail` | #2 | P0 | excludes 命中时失败 |
| 9 | `TestEvalSyscalls_Partial` | #2 | P0 | 部分 includes 场景 |
| 10 | `TestEvalSyscalls_NilAssert` | #2 | P1 | assert 为 nil 时返回空切片 |
| 11 | `TestEvalQuality_Pass` | #3 | P0 | MockQualityJudge 返回 Passed=true |
| 12 | `TestEvalQuality_Fail` | #3 | P0 | MockQualityJudge 返回 Passed=false，Message 含 Reason |
| 13 | `TestEvalQuality_JudgeError` | #3 | P0 | MockQualityJudge 返回 error |
| 14 | `TestEvalAssertions_NilAssert` | all | P0 | assert 为 nil 时返回空 |
| 15 | `TestEvalAssertions_OutputOnly` | #1 | P0 | 仅 output 断言 |
| 16 | `TestEvalAssertions_SyscallsOnly` | #2 | P0 | 仅 syscalls 断言 |
| 17 | `TestEvalAssertions_QualityOnly` | #3 | P0 | 仅 quality 断言 |
| 18 | `TestEvalAssertions_AllThree` | all | P0 | 三种断言全部启用 |
| 19 | `TestEvalAssertions_NilJudgeWithQuality` | #3 | P0 | judge 为 nil 但有 quality 断言时返回错误 |

### Unit Tests — agtest/validator_test.go (extensions)

**File:** `agtest/validator_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 20 | `TestValidate_AssertOutputEmptyBoth_Fail` | all | P0 | output 断言 contains/not_contains 同时为空 |
| 21 | `TestValidate_AssertSyscallsEmptyBoth_Fail` | all | P0 | syscalls 断言 includes/excludes 同时为空 |
| 22 | `TestValidate_AssertQualityEmptyCriteria_Fail` | all | P0 | quality 断言 criteria 为空 |
| 23 | `TestValidate_ValidAssert_Pass` | all | P0 | 有效断言配置通过校验 |
| 24 | `TestValidate_AssertMixed_Pass` | all | P1 | 多种有效断言组合通过 |

---

## Fixtures & Helpers

### Test Data Files

**位置:** `agtest/testdata/`

| File | Purpose |
|------|---------|
| `assert-output-only.yaml` | 仅包含 output 断言的有效用例 |
| `assert-invalid-empty.yaml` | 断言配置无效（空 contains/not_contains 等） |

### Test Helpers

**位置:** `agtest/eval_test.go` 内部

- `MockQualityJudge` — 实现 QualityJudge 接口，可配置 Passed、Reason、Error
- 表驱动测试辅助：`assertResultPass(t, results)` / `assertResultFail(t, results, expectedMsg)`

---

## Implementation Checklist

### Phase 1: 断言结果类型 (Tests 14, 18)

- [ ] 在 `agtest/eval.go` 中定义 AssertionResult、TestResult、QualityResult
- [ ] Run: `go build ./agtest/`
- [ ] ✅ 类型编译通过

### Phase 2: EvalOutput (AC #1, Tests 1-5)

- [ ] 实现 `EvalOutput(output string, assert *OutputAssert) []AssertionResult`
- [ ] Run: `go test -race ./agtest/ -run TestEvalOutput`
- [ ] ✅ Tests 1-5 pass

### Phase 3: EvalSyscalls (AC #2, Tests 6-10)

- [ ] 实现 `EvalSyscalls(syscalls []string, assert *SyscallAssert) []AssertionResult`
- [ ] Run: `go test -race ./agtest/ -run TestEvalSyscalls`
- [ ] ✅ Tests 6-10 pass

### Phase 4: EvalQuality + QualityJudge (AC #3, Tests 11-13)

- [ ] 定义 `QualityJudge` 接口
- [ ] 实现 `MockQualityJudge` 用于测试
- [ ] 实现 `EvalQuality(output string, assert *QualityAssert, judge QualityJudge) ([]AssertionResult, error)`
- [ ] Run: `go test -race ./agtest/ -run TestEvalQuality`
- [ ] ✅ Tests 11-13 pass

### Phase 5: EvalAssertions (Tests 14-19)

- [ ] 实现 `EvalAssertions(output string, syscalls []string, assert *AssertConfig, judge QualityJudge) ([]AssertionResult, error)`
- [ ] Run: `go test -race ./agtest/ -run TestEvalAssertions`
- [ ] ✅ Tests 14-19 pass

### Phase 6: 断言配置校验扩展 (Tests 20-24)

- [ ] 扩展 `agtest/validator.go` — Validate 增加 output/syscalls/quality 断言配置校验
- [ ] 创建 `assert-output-only.yaml`、`assert-invalid-empty.yaml` fixture
- [ ] Run: `go test -race ./agtest/ -run TestValidate`
- [ ] ✅ Tests 20-24 pass

### Phase 7: 全量回归

- [ ] Run: `go test -race ./...`
- [ ] ✅ 全项目通过

---

## Running Tests

```bash
# Run all tests for story 16-2 (affected packages)
go test -race -v ./agtest/

# Run only eval tests
go test -race -v ./agtest/ -run TestEval

# Run only output assertion tests
go test -race -v ./agtest/ -run TestEvalOutput

# Run only syscall assertion tests
go test -race -v ./agtest/ -run TestEvalSyscalls

# Run only quality assertion tests
go test -race -v ./agtest/ -run TestEvalQuality

# Run validator tests (including new assertion validation)
go test -race -v ./agtest/ -run TestValidate

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 24 tests designed and specified
- ✅ Test strategy mapped to all 3 acceptance criteria
- ✅ Implementation checklist created with phased approach
- ✅ Tests designed to fail before implementation (eval.go, extended validator don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (EvalOutput, EvalSyscalls, EvalQuality, EvalAssertions, extended Validate)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (result types) → Types compile
2. Implement Phase 2 (EvalOutput) → Tests 1-5 pass
3. Implement Phase 3 (EvalSyscalls) → Tests 6-10 pass
4. Implement Phase 4 (EvalQuality + QualityJudge) → Tests 11-13 pass
5. Implement Phase 5 (EvalAssertions) → Tests 14-19 pass
6. Implement Phase 6 (validator extension) → Tests 20-24 pass
7. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, Story 16-1 types available)
- [x] Test strategy maps to all 3 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] Implementation checklist covers all 7 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- 本 story 依赖 Story 16-1 的 AssertConfig、OutputAssert、SyscallAssert、QualityAssert 类型
- QualityJudge 为接口，便于测试时注入 MockQualityJudge；生产实现将在后续 story 中接入轻量模型
- syscall 断言中的 syscalls 参数为字符串切片（如 "Read", "Write"），实际调用名需与实现约定一致
- 断言配置校验：output 断言若存在则 contains/not_contains 至少其一非空；syscalls 同理；quality 的 criteria 非空

---

**Generated by BMad TEA Agent** - 2026-03-08
