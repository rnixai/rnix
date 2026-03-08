---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/16-3-batch-test-run-and-report.md'
  - 'agtest/types.go'
  - 'agtest/eval.go'
  - 'agtest/parser.go'
  - 'cmd/rnix/agtest.go'
  - 'ipc/protocol.go'
  - 'ipc/client.go'
---

# ATDD Checklist - Epic 16, Story 3: 批量测试运行与报告

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
- Story 16-3 approved with 2 clear acceptance criteria (AC #1-2)
- Story 16-1 completed — TestSuiteSpec, TestCaseSpec, ParseFile, ParseDir
- Story 16-2 completed — EvalAssertions, TestResult, AssertionResult, QualityJudge, MockQualityJudge
- Test framework configured: Go `testing` + testify across project
- IPC infrastructure available: SpawnAndWatch, StreamEvent, SyscallEventWire

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/16-3-batch-test-run-and-report.md`
- **Acceptance Criteria:** 2 ACs — 批量运行+结果报告 (AC#1), 失败用例详细信息 (AC#2)
- **Affected Components:** `agtest/runner.go` (new), `agtest/judge.go` (new), `cmd/rnix/agtest.go` (extend)
- **Dependencies:** Story 16-1 (parser), Story 16-2 (eval), ipc (SpawnAndWatch)

### Framework & Existing Patterns
- Existing types in `agtest/types.go` — TestCaseSpec, AgentConfig
- Existing eval in `agtest/eval.go` — EvalAssertions, TestResult, QualityJudge, MockQualityJudge
- Existing CLI in `cmd/rnix/agtest.go` — agtestCmd, runAgtest, agtestDryRunOutput
- IPC patterns in `cmd/rnix/main.go` — SpawnAndWatch callback, ProgressPayload parsing
- IPC protocol in `ipc/protocol.go` — SpawnRequest, StreamEvent, SyscallEventWire, ProgressPayload

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (runner, judge, CLI report)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | 批量运行所有测试用例，输出结果报告（通过/失败/跳过 + 失败原因），框架开销 ≤ 500ms | Unit (agtest/runner, cmd/rnix/agtest) | P0 |
| AC#2 | 失败用例报告包含断言类型、期望值、实际值和差异说明 | Unit (agtest/runner, cmd/rnix/agtest) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests (runner) | 14 | Runner.RunSuite, runCase, syscall collection, timeout, status determination |
| Unit Tests (judge) | 4 | LLMQualityJudge JSON parsing and fallback |
| Unit Tests (CLI) | 5 | CLI report output, JSON mode, --timeout flag |
| **Total** | **23** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — agtest/runner_test.go

**File:** `agtest/runner_test.go`

| # | Test ID | Test Name | AC | Priority | Verifies |
|---|---------|-----------|----|----|----------|
| 1 | 16.3-UNIT-001 | `TestRunner_RunSuite_AllPassed` | #1 | P0 | 全部测试通过时 SuiteResult.Passed == Total |
| 2 | 16.3-UNIT-002 | `TestRunner_RunSuite_MixedResults` | #1,#2 | P0 | 混合通过/失败时正确聚合 |
| 3 | 16.3-UNIT-003 | `TestRunner_RunCase_SpawnError` | #1 | P0 | spawn 错误返回 StatusError + Error 信息 |
| 4 | 16.3-UNIT-004 | `TestRunner_RunCase_NoAssert_ExitZero` | #1 | P0 | 无断言 + ExitCode==0 → StatusPassed |
| 5 | 16.3-UNIT-005 | `TestRunner_RunCase_NoAssert_ExitNonZero` | #1 | P0 | 无断言 + ExitCode!=0 → StatusError |
| 6 | 16.3-UNIT-006 | `TestRunner_RunCase_OutputAssert_Pass` | #1 | P0 | output 断言全部匹配 → StatusPassed |
| 7 | 16.3-UNIT-007 | `TestRunner_RunCase_OutputAssert_Fail` | #2 | P0 | output 断言失败 → StatusFailed + 详细信息 |
| 8 | 16.3-UNIT-008 | `TestRunner_RunCase_SyscallAssert_Pass` | #1 | P0 | syscall 断言匹配 → StatusPassed |
| 9 | 16.3-UNIT-009 | `TestRunner_RunCase_SyscallAssert_Fail` | #2 | P0 | syscall 断言失败 → StatusFailed + 详细信息 |
| 10 | 16.3-UNIT-010 | `TestRunner_RunCase_SyscallCollection` | #1 | P0 | 从 StreamSyscallEvent 正确收集 syscall 名称 |
| 11 | 16.3-UNIT-011 | `TestRunner_RunCase_Timeout` | #1 | P1 | 测试用例超时处理 |
| 12 | 16.3-UNIT-012 | `TestRunner_RunCase_TimeoutFromSpec` | #1 | P1 | tc.Timeout 优先于 Runner.Timeout |
| 13 | 16.3-UNIT-013 | `TestSuiteResult_Aggregation` | #1 | P0 | SuiteResult 正确聚合 Total/Passed/Failed/Errors |
| 14 | 16.3-UNIT-014 | `TestCaseStatus_Constants` | #1 | P1 | CaseStatus 常量值正确 |

### Unit Tests — agtest/judge_test.go

**File:** `agtest/judge_test.go`

| # | Test ID | Test Name | AC | Priority | Verifies |
|---|---------|-----------|----|----|----------|
| 15 | 16.3-UNIT-015 | `TestParseQualityResponse_Valid` | #2 | P0 | 有效 JSON 响应正确解析 |
| 16 | 16.3-UNIT-016 | `TestParseQualityResponse_InvalidJSON` | #2 | P0 | 无效 JSON 返回 fallback 结果 |
| 17 | 16.3-UNIT-017 | `TestParseQualityResponse_FallbackPassedTrue` | #2 | P1 | 非标准 JSON 但包含 "passed": true 的 fallback |
| 18 | 16.3-UNIT-018 | `TestLLMQualityJudge_Interface` | #2 | P1 | LLMQualityJudge 满足 QualityJudge 接口 |

### Unit Tests — cmd/rnix/agtest_test.go (extensions)

**File:** `cmd/rnix/agtest_test.go`

| # | Test ID | Test Name | AC | Priority | Verifies |
|---|---------|-----------|----|----|----------|
| 19 | 16.3-CLI-001 | `TestAgtest_TextReport_AllPassed` | #1 | P0 | 纯文本报告格式正确（✓/✗ 符号、统计行） |
| 20 | 16.3-CLI-002 | `TestAgtest_TextReport_WithFailures` | #2 | P0 | 失败用例报告包含断言详情 |
| 21 | 16.3-CLI-003 | `TestAgtest_JSONReport` | #1 | P0 | JSON 报告格式正确 |
| 22 | 16.3-CLI-004 | `TestAgtest_TimeoutFlag` | #1 | P1 | --timeout flag 正确解析和传递 |
| 23 | 16.3-CLI-005 | `TestAgtest_NoDaemon_Error` | #1 | P0 | daemon 不可用时友好报错 |

---

## Fixtures & Helpers

### Test Data Files

**位置:** `agtest/testdata/`

| File | Purpose |
|------|---------|
| `valid-single.yaml` | 已有：有效的单个测试用例 |
| `valid-suite.yaml` | 已有：有效的测试套件 |

### Test Helpers

**位置:** `agtest/runner_test.go` 内部

- `MockSpawnClient` — 实现 SpawnClient 接口，可配置返回的 PID、ProgressPayload、error、StreamEvent 序列
- `mockStreamEvents(syscalls ...string)` — 生成包含 syscall_event 的 StreamEvent 切片
- `assertCaseStatus(t, got CaseResult, wantStatus CaseStatus)` — 验证 CaseResult 状态

### Mock Strategy

Runner 测试通过 `SpawnClient` 接口 mock IPC client：

```go
type SpawnClient interface {
    SpawnAndWatch(req ipc.SpawnRequest, onEvent func(ipc.StreamEvent)) (types.PID, *ipc.ProgressPayload, error)
    Close() error
}
```

`MockSpawnClient` 实现此接口，按测试场景配置：
- 返回成功（PID + Result）
- 返回失败（error）
- 注入 syscall 事件到 onEvent 回调
- 模拟超时

---

## Implementation Checklist

### Phase 1: Runner 核心类型 (Tests 13, 14)

- [ ] 在 `agtest/runner.go` 中定义 CaseStatus、CaseResult、SuiteResult、SpawnClient 接口、Runner
- [ ] Run: `go build ./agtest/`
- [ ] ✅ 类型编译通过

### Phase 2: Runner.runCase 基本流程 (AC #1, Tests 3-9)

- [ ] 实现 `Runner.runCase(ctx, tc *TestCaseSpec) CaseResult`
- [ ] SpawnRequest 字段映射：Intent, Agent.Name, Agent.Model, Agent.ContextBudget, Timeout
- [ ] EvalAssertions 调用和状态判定
- [ ] Run: `go test -race ./agtest/ -run TestRunner_RunCase`
- [ ] ✅ Tests 3-9 pass

### Phase 3: Syscall 事件收集 (AC #1, Test 10)

- [ ] 在 onEvent 回调中过滤 StreamSyscallEvent，提取 Syscall 名称
- [ ] Run: `go test -race ./agtest/ -run TestRunner_RunCase_SyscallCollection`
- [ ] ✅ Test 10 pass

### Phase 4: 超时处理 (Tests 11, 12)

- [ ] context.WithTimeout 传播 tc.Timeout 或 Runner.Timeout
- [ ] SpawnRequest.TimeoutMs 设置
- [ ] Run: `go test -race ./agtest/ -run TestRunner_RunCase_Timeout`
- [ ] ✅ Tests 11-12 pass

### Phase 5: RunSuite 聚合 (AC #1, Tests 1, 2)

- [ ] 实现 `Runner.RunSuite(ctx, suite *TestSuiteSpec) *SuiteResult`
- [ ] 顺序执行所有 TestCaseSpec，聚合 CaseResult
- [ ] Run: `go test -race ./agtest/ -run TestRunner_RunSuite`
- [ ] ✅ Tests 1-2 pass

### Phase 6: LLMQualityJudge (AC #2, Tests 15-18)

- [ ] 在 `agtest/judge.go` 实现 LLMQualityJudge 和 parseQualityResponse
- [ ] Run: `go test -race ./agtest/ -run TestParseQualityResponse`
- [ ] Run: `go test -race ./agtest/ -run TestLLMQualityJudge`
- [ ] ✅ Tests 15-18 pass

### Phase 7: CLI 集成与报告 (AC #1, #2, Tests 19-23)

- [ ] 修改 `cmd/rnix/agtest.go` — 非 dry-run 时连接 daemon 执行测试
- [ ] 实现 agtestReport (纯文本) 和 agtestJSONReport (JSON)
- [ ] 添加 --timeout flag
- [ ] Run: `go test -race ./cmd/rnix/ -run TestAgtest`
- [ ] ✅ Tests 19-23 pass

### Phase 8: 全量回归

- [ ] Run: `go test -race ./...`
- [ ] ✅ 全项目通过

---

## Running Tests

```bash
# Run all tests for story 16-3 (affected packages)
go test -race -v ./agtest/ ./cmd/rnix/

# Run only runner tests
go test -race -v ./agtest/ -run TestRunner

# Run only judge tests
go test -race -v ./agtest/ -run TestParseQualityResponse

# Run only CLI agtest tests
go test -race -v ./cmd/rnix/ -run TestAgtest

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 23 tests designed and specified
- ✅ Test strategy mapped to all 2 acceptance criteria
- ✅ Implementation checklist created with phased approach
- ✅ Tests designed to fail before implementation (runner.go, judge.go don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (Runner, CaseResult, SuiteResult, SpawnClient, LLMQualityJudge, parseQualityResponse)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (core types) → Types compile
2. Implement Phase 2 (runCase basic) → Tests 3-9 pass
3. Implement Phase 3 (syscall collection) → Test 10 pass
4. Implement Phase 4 (timeout) → Tests 11-12 pass
5. Implement Phase 5 (RunSuite) → Tests 1-2 pass
6. Implement Phase 6 (LLMQualityJudge) → Tests 15-18 pass
7. Implement Phase 7 (CLI integration) → Tests 19-23 pass
8. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (Story 16-1 parser, Story 16-2 eval available)
- [x] Test strategy maps to all 2 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] Implementation checklist covers all tasks from story
- [x] Artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- Runner 通过 SpawnClient 接口抽象 IPC 调用，便于 mock 测试
- syscall 事件从 SpawnAndWatch 的 StreamEvent 流中收集，Type == StreamSyscallEvent
- LLMQualityJudge 的 JSON 解析需要 fallback 逻辑（LLM 输出可能不是严格 JSON）
- CLI 报告需要区分纯文本和 JSON 两种输出格式
- --timeout flag 默认 60000ms，传递到 Runner.Timeout
- NFR35 验证：框架开销（解析+校验+Runner初始化+断言评估）应 < 500ms，不含 LLM 调用

---

**Generated by BMad TEA Agent** - 2026-03-08
