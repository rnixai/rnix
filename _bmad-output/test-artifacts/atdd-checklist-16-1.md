---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/16-1-declarative-test-case-definition.md'
  - 'compose/parser.go'
  - 'compose/parser_test.go'
  - 'compose/types.go'
  - 'agents/loader.go'
  - 'agents/types.go'
  - 'cmd/rnix/trace.go'
---

# ATDD Checklist - Epic 16, Story 1: 声明式测试用例定义

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with `go test -race`
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 16-1 approved with 2 clear acceptance criteria (AC #1-2)
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages
- Development environment available

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/16-1-declarative-test-case-definition.md`
- **Acceptance Criteria:** 2 ACs covering YAML parsing/loading and validation with line numbers
- **Affected Components:** `agtest/` (new package), `cmd/rnix/` (new agtest.go)
- **Dependencies:** No dependency on previous epics' runtime — pure parsing/validation library

### Framework & Existing Patterns
- Existing YAML parsing in `compose/parser.go` (ParseFile/ParseBytes/validate pattern)
- Existing YAML parsing in `agents/loader.go` (AgentManifest unmarshal)
- Existing testdata patterns in `compose/testdata/`, `agents/testdata/`
- CLI command patterns in `cmd/rnix/trace.go`, `cmd/rnix/ctx_profile.go`
- Test pattern: Go table-driven tests, `t.TempDir()` for filesystem, `t.Helper()`

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (YAML parsing, validation, CLI registration)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | YAML 文件包含 intent、agent 配置和断言，系统可解析并加载 | Unit (agtest/parser) + CLI (cmd/rnix/agtest) | P0 |
| AC#2 | 缺少必填字段时报告具体校验错误和行号 | Unit (agtest/validator) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests | ~22 | TestCaseSpec 解析、TestSuiteSpec 解析、格式检测、校验错误、行号、边界情况 |
| CLI Tests | ~3 | agtest 命令注册、--dry-run 模式、错误输出 |
| **Total** | **~25** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — agtest/parser_test.go

**File:** `agtest/parser_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestParseBytes_SingleTestCase` | #1 | P0 | 解析单个测试用例 YAML，包含 intent、agent.name |
| 2 | `TestParseBytes_SingleTestCase_AllFields` | #1 | P0 | 解析包含所有可选字段的单个测试用例（model、skills、context_budget、timeout、assert） |
| 3 | `TestParseBytes_TestSuite` | #1 | P0 | 解析包含 tests 数组的测试套件 YAML |
| 4 | `TestParseBytes_TestSuite_Multiple` | #1 | P0 | 解析包含多个测试用例的 suite |
| 5 | `TestParseBytes_AutoDetect_SingleToSuite` | #1 | P0 | 单个测试用例自动包装为 TestSuiteSpec（Tests 长度 1） |
| 6 | `TestParseBytes_InvalidYAML` | #1 | P0 | 无效 YAML 语法返回解析错误 |
| 7 | `TestParseBytes_EmptyInput` | #1 | P0 | 空输入返回错误 |
| 8 | `TestParseFile_ValidFile` | #1 | P0 | 从文件路径解析有效测试用例 |
| 9 | `TestParseFile_NotFound` | #1 | P0 | 文件不存在返回错误 |
| 10 | `TestParseDir_MultipleFiles` | #1 | P0 | 从目录扫描并合并多个 YAML 文件 |
| 11 | `TestParseDir_EmptyDir` | #1 | P1 | 空目录返回错误 |
| 12 | `TestParseDir_IgnoresNonYAML` | #1 | P1 | 目录中的非 .yaml 文件被忽略 |

### Unit Tests — agtest/validator_test.go

**File:** `agtest/validator_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 13 | `TestValidate_ValidSpec` | #1 | P0 | 有效 spec 校验通过（无错误） |
| 14 | `TestValidate_MissingIntent` | #2 | P0 | 缺少 intent 返回 ValidationError |
| 15 | `TestValidate_EmptyIntent` | #2 | P0 | 空字符串 intent 返回 ValidationError |
| 16 | `TestValidate_MissingAgentName` | #2 | P0 | 缺少 agent.name 返回 ValidationError |
| 17 | `TestValidate_EmptyAgentName` | #2 | P0 | 空字符串 agent.name 返回 ValidationError |
| 18 | `TestValidate_InvalidVersion` | #2 | P0 | version 不为 "1.0" 返回 ValidationError |
| 19 | `TestValidate_MissingVersion` | #2 | P0 | 缺少 version 返回 ValidationError |
| 20 | `TestValidate_MultipleErrors` | #2 | P0 | 同时缺少 intent 和 agent.name 时收集两个错误 |
| 21 | `TestValidate_WithLineNumbers` | #2 | P0 | 校验错误包含正确的行号信息 |
| 22 | `TestValidate_SuiteMultipleTests` | #2 | P0 | suite 中多个 test 各自独立校验 |
| 23 | `TestValidationError_ErrorString` | #2 | P1 | ValidationError.Error() 格式化输出正确 |
| 24 | `TestValidationErrors_ErrorString` | #2 | P1 | ValidationErrors.Error() 聚合输出正确 |

### CLI Tests — cmd/rnix/agtest_test.go

**File:** `cmd/rnix/agtest_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 25 | `TestAgtestCommand_Registered` | #1 | P0 | agtest 命令在 rootCmd 中注册 |
| 26 | `TestAgtestCommand_DryRun_ValidFile` | #1 | P0 | --dry-run 模式解析有效文件并输出摘要 |
| 27 | `TestAgtestCommand_DryRun_InvalidFile` | #2 | P0 | --dry-run 模式处理校验错误 |

---

## Fixtures & Helpers

### Test Data Files

**位置:** `agtest/testdata/`

| File | Purpose |
|------|---------|
| `valid-single.yaml` | 有效的单个测试用例（intent + agent.name） |
| `valid-suite.yaml` | 有效的测试套件（多用例） |
| `valid-full.yaml` | 包含所有可选字段的完整测试用例 |
| `missing-intent.yaml` | 缺少 intent 字段 |
| `missing-agent.yaml` | 缺少 agent.name 字段 |
| `missing-version.yaml` | 缺少 version 字段 |
| `invalid-version.yaml` | version 不为 "1.0" |
| `invalid-syntax.yaml` | 无效 YAML 语法 |
| `empty.yaml` | 空文件 |
| `multi-errors.yaml` | 同时缺少 intent 和 agent.name |

### Test Helpers

**位置:** `agtest/parser_test.go` 内部

- `writeTestYAML(t, dir, name, content)` — 在临时目录创建测试 YAML 文件
- `assertValidationError(t, errs, field, expectedMsg)` — 断言 ValidationErrors 中包含指定字段的错误

---

## Implementation Checklist

### Phase 1: 类型定义 (Tests 1-2, 13)

- [ ] 创建 `agtest/types.go` — TestCaseSpec、AgentConfig、AssertConfig、TestSuiteSpec
- [ ] Run: `go build ./agtest/`
- [ ] ✅ 类型编译通过

### Phase 2: 解析器 (Tests 1-12)

- [ ] 创建 `agtest/parser.go` — ParseFile、ParseBytes、ParseDir、格式检测
- [ ] 创建 `agtest/testdata/` fixture 文件
- [ ] Run: `go test -race ./agtest/ -run TestParse`
- [ ] ✅ Tests 1-12 pass

### Phase 3: 校验器 (Tests 13-24)

- [ ] 创建 `agtest/validator.go` — ValidationError、ValidationErrors、Validate
- [ ] 实现行号提取（goccy/go-yaml AST）
- [ ] Run: `go test -race ./agtest/ -run TestValidate`
- [ ] ✅ Tests 13-24 pass

### Phase 4: CLI (Tests 25-27)

- [ ] 创建 `cmd/rnix/agtest.go` — agtest 命令注册 + --dry-run
- [ ] Run: `go test -race ./cmd/rnix/ -run TestAgtest`
- [ ] ✅ Tests 25-27 pass

### Phase 5: 全量回归

- [ ] Run: `go test -race ./...`
- [ ] ✅ 全项目通过

---

## Running Tests

```bash
# Run all tests for story 16-1 (affected packages)
go test -race -v ./agtest/ ./cmd/rnix/

# Run only parser tests
go test -race -v ./agtest/ -run TestParse

# Run only validator tests
go test -race -v ./agtest/ -run TestValidate

# Run CLI tests
go test -race -v ./cmd/rnix/ -run TestAgtest

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 27 tests designed and specified
- ✅ Test strategy mapped to acceptance criteria
- ✅ Implementation checklist created with phased approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (agtest.ParseFile, agtest.Validate, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (types) → Types compile
2. Implement Phase 2 (parser) → Tests 1-12 pass
3. Implement Phase 3 (validator) → Tests 13-24 pass
4. Implement Phase 4 (CLI) → Tests 25-27 pass
5. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, test framework configured)
- [x] Test strategy maps to all 2 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] Implementation checklist covers all 6 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- 本 story 是 Epic 16 的第一层，后续 story (16-2 断言类型, 16-3 批量运行) 依赖本 story 的 ParseFile 和 TestCaseSpec
- agtest 包不依赖 kernel、debug 或其他运行时包 — 纯 YAML 解析和校验
- goccy/go-yaml 的 AST 行号获取需要在实现时验证具体 API（Path vs Token Position）
- CLI --dry-run 模式是展示解析结果的"安全"方式，不需要 daemon 运行

---

**Generated by BMad TEA Agent** - 2026-03-08
