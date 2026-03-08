---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/15-5-context-growth-prediction-and-alert.md'
  - '_bmad-output/implementation-artifacts/15-4-context-usage-analysis.md'
  - '_bmad/tea/config.yaml'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'debug/ctx_profile.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/rnix/top.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 15, Story 5: 上下文增长预测与告警 (ctx-growth)

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with `go test -race`
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 15-5 approved with 5 clear acceptance criteria (AC #1-5)
- Story 15-4 completed: CtxProfileResult、AnalyzeContext、FormatCtxProfile、IPC MethodCtxProfile 模式
- Story 15-3 completed: BlameResult MarshalJSON snake_case + _ms 后缀模式
- kernel/process.go: logHistory 环形缓冲区模式可直接参照
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/15-5-context-growth-prediction-and-alert.md`
- **Acceptance Criteria:** 5 ACs covering growth prediction, budget alert, CLI query, error handling, JSON output
- **Affected Components:** `kernel/` (process.go + kernel.go), `debug/` (new ctx_growth.go), `cmd/rnix/` (new ctx_growth.go + top.go), `ipc/` (protocol, server, client), `internal/types/` (LogWarning)
- **Dependencies:** Process.TokensUsed, Process.ContextBudget, emitLog/emitEvent, IPC 非流式请求模式

### Framework & Existing Patterns
- Existing test patterns in `kernel/process_test.go` (logHistory 环形缓冲区测试)
- Existing test patterns in `debug/ctx_profile_test.go` (table-driven, makeContextData helpers)
- Existing CLI test patterns in `cmd/rnix/ctx_profile_test.go` (Cobra 执行、setupTestServer)
- Existing IPC test patterns in `ipc/server_test.go` (setupTestServer, AddProcess)
- Test pattern: Go table-driven tests, `t.TempDir()`, `t.Helper()`, `-race` detector

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (kernel/process, debug/ctx_growth, ipc handleCtxGrowth, cmd/rnix ctx-growth CLI)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | 基于历史增长速率预测何时耗尽预算 | Unit (kernel process + debug) | P0 |
| AC#2 | 剩余 < 20% 时发出告警，显示消耗/总额、百分比、预估剩余步数 | Unit (kernel) + Integration (top) | P0 |
| AC#3 | `rnix ctx-growth <pid>` + Running 进程 → 展示增长趋势、预测和告警 | IPC (server) + Integration (CLI) | P0 |
| AC#4 | 无效 PID 或非 Running → 友好错误信息 | IPC (server) + Integration (CLI) | P0 |
| AC#5 | `--json` → JSON 格式输出 | Unit (debug MarshalJSON) + Integration (CLI) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests (kernel/process_test.go) | ~4 | TokenSnapshot 环形缓冲区、GetTokenHistory |
| Unit Tests (debug/ctx_growth_test.go) | ~10 | PredictGrowth、FormatGrowthPrediction、MarshalJSON |
| Unit Tests (kernel/kernel_test.go) | ~2 | Budget warning 发射、LogWarning 类别 |
| IPC Handler Tests (ipc/server_test.go) | ~3 | handleCtxGrowth：有效 PID、无效 PID、错误状态 |
| CLI Integration Tests (cmd/rnix/ctx_growth_test.go) | ~4 | 正常输出、JSON、错误处理、daemon 不可用 |
| **Total** | **~23** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — kernel/process_test.go

**File:** `kernel/process_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestProcess_TokenHistory_Empty` | #1 | P0 | 空 history：GetTokenHistory 返回空切片 |
| 2 | `TestProcess_TokenHistory_AppendAndRetrieve` | #1 | P0 | 添加 3 个 snapshot：顺序正确，Step/Tokens/DeltaMs 准确 |
| 3 | `TestProcess_TokenHistory_RingBufferOverflow` | #1 | P0 | 添加 60 个 snapshot（超 50 上限）：返回最近 50 个，顺序正确 |
| 4 | `TestProcess_TokenHistory_ConcurrentSafety` | #1 | P0 | 多 goroutine 并发 appendTokenSnapshot 不 panic（-race） |

### Unit Tests — debug/ctx_growth_test.go

**File:** `debug/ctx_growth_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 5 | `TestPredictGrowth_NoBudget` | #1 | P0 | 无 budget (=0)：AlertNone，EstRemaining=0，PredictExhaust=false |
| 6 | `TestPredictGrowth_WithHistory` | #1 | P0 | 5 步 history + budget → 正确计算 AvgRate、RecentRate、EstRemaining |
| 7 | `TestPredictGrowth_AlertWarning` | #2 | P0 | 剩余 15%（< 20%）：AlertWarning |
| 8 | `TestPredictGrowth_AlertCritical` | #2 | P0 | 剩余 8%（< 10%）：AlertCritical |
| 9 | `TestPredictGrowth_EmptyHistory` | #1 | P0 | 空 history：EstRemaining=0，仅使用 AvgRate |
| 10 | `TestPredictGrowth_SingleStep` | #1 | P0 | 单步 history：AvgRate == RecentRate |
| 11 | `TestPredictGrowth_PredictExhaust` | #1 | P0 | 高消耗率 + 低预算 → PredictExhaust=true |
| 12 | `TestFormatGrowthPrediction_Normal` | #3 | P0 | 输出含 "Growth Trend"、"Prediction"、"Budget" 段落 |
| 13 | `TestFormatGrowthPrediction_NoBudget` | #3 | P0 | 无 budget → 省略 Prediction 和 Budget 段落 |
| 14 | `TestFormatGrowthPrediction_AlertWarning` | #2 | P0 | AlertWarning → 显示 "⚠ WARNING" |
| 15 | `TestGrowthPrediction_MarshalJSON` | #5 | P0 | snake_case、浮点一位小数、history 空数组而非 null |

### Unit Tests — kernel budget warning

**File:** `kernel/kernel_test.go` (或在现有 reasonStep 相关测试附近)

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 16 | `TestReasonStep_BudgetWarning_EmitsLogWarning` | #2 | P0 | 剩余 < 20% → LogChan 收到 LogWarning 类别的条目 |
| 17 | `TestReasonStep_TokenHistory_RecordedPerStep` | #1 | P0 | 每步后 TokenHistory 有新增条目 |

### IPC Handler Tests — ipc/server_test.go

**File:** `ipc/server_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 18 | `TestServer_CtxGrowth_ValidPID_Running` | #3 | P0 | 有效 PID + Running → OK，result 含 GrowthPrediction 数据 |
| 19 | `TestServer_CtxGrowth_InvalidPID` | #4 | P0 | 无效 PID → NOT_FOUND |
| 20 | `TestServer_CtxGrowth_WrongState` | #4 | P0 | 非 Running 状态 → INVALID |

### CLI Integration Tests — cmd/rnix/ctx_growth_test.go

**File:** `cmd/rnix/ctx_growth_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 21 | `TestCtxGrowthCmd_ValidPID` | #3 | P0 | `ctx-growth <valid-pid>` → 输出含 "Prediction" |
| 22 | `TestCtxGrowthCmd_InvalidPID` | #4 | P0 | `ctx-growth <invalid-pid>` → 友好错误信息 |
| 23 | `TestCtxGrowthCmd_JSON` | #5 | P0 | `ctx-growth <pid> --json` → JSON 输出（JSONResponse 包装） |
| 24 | `TestCtxGrowthCmd_DaemonUnavailable` | #4 | P0 | daemon 不可用时输出明确错误 |

---

## Fixtures & Helpers

### Token History Test Helpers

**位置:** `kernel/process_test.go` 内部

- 使用 `kernel.NewProcess` 创建进程，手动调用 `appendTokenSnapshot` 填充历史
- 并发测试使用 `sync.WaitGroup` + 多 goroutine

### Growth Prediction Test Helpers

**位置:** `debug/ctx_growth_test.go` 内部

- `makeTokenSnapshots(pairs ...int) []kernel.TokenSnapshot` — 从 (step, tokens) 对创建 TokenSnapshot 切片
- `makeGrowthPrediction(tokensUsed, budget, step, maxSteps int, history []kernel.TokenSnapshot) *GrowthPrediction` — 快捷构造

### IPC Test Helpers

**位置:** `ipc/server_test.go` 内部

- 复用 `setupTestServer`
- 创建带 tokenHistory 的进程：`proc.AppendTokenSnapshot(step, tokens)` 填充历史数据
- `kern.AddProcess(proc)` 注册进程

### CLI Test Helpers

**位置:** `cmd/rnix/ctx_growth_test.go` 内部

- 复用 `setupCtxGrowthTestServer` 模式（参考 ctx_profile_test.go）
- Cobra 命令执行模式（SetOut、SetArgs、Execute）
- `ipc.SocketPathOverride` 指向测试 socket

---

## Mock Requirements

### 无外部服务 Mock

本 Story 不涉及外部 LLM 或网络服务。Token 历史通过 Process 内部环形缓冲区维护。

### Test 进程

- 使用 `kernel.NewProcess` 创建测试进程
- 手动设置 TokensUsed、ContextBudget、State
- 手动调用 `appendTokenSnapshot` 填充历史数据
- 使用 `kern.AddProcess(proc)` 注册进程

---

## Implementation Checklist

### Phase 1: Token 历史追踪基础 (Tests 1-4)

- [ ] 在 `kernel/process.go` 定义 `TokenSnapshot{Step, Tokens, DeltaMs}`
- [ ] 在 `Process` 结构体添加 `tokenHistory []TokenSnapshot`、`tokenHistIdx int`、`tokenHistLen int`
- [ ] 实现 `appendTokenSnapshot(step, tokens int)` 环形缓冲区方法（参照 logHistory 模式）
- [ ] 实现 `GetTokenHistory() []TokenSnapshot`（加锁返回副本）
- [ ] Run: `go test -race ./kernel/ -run TestProcess_TokenHistory`
- [ ] ✅ Tests 1-4 pass

### Phase 2: 增长预测引擎 (Tests 5-11)

- [ ] 创建 `debug/ctx_growth.go`，定义 GrowthPrediction、AlertLevel、TokenSnapshot 类型
- [ ] 实现 `PredictGrowth` 函数：AvgRate、RecentRate、EstRemaining、AlertLevel 计算
- [ ] 处理边界：无 budget、空 history、单步、高消耗率
- [ ] Run: `go test -race ./debug/ -run TestPredictGrowth`
- [ ] ✅ Tests 5-11 pass

### Phase 3: 格式化输出 (Tests 12-14)

- [ ] 实现 `FormatGrowthPrediction(p *GrowthPrediction) string`
- [ ] 段落：Growth Trend、Prediction、Budget bar
- [ ] 无 budget 省略 Prediction/Budget，AlertWarning 显示 ⚠
- [ ] Run: `go test -race ./debug/ -run TestFormatGrowthPrediction`
- [ ] ✅ Tests 12-14 pass

### Phase 4: MarshalJSON (Test 15)

- [ ] 为 GrowthPrediction 实现 MarshalJSON（snake_case、浮点一位小数）
- [ ] Run: `go test -race ./debug/ -run TestGrowthPrediction_MarshalJSON`
- [ ] ✅ Test 15 pass

### Phase 5: Budget Warning + Token History in reasonStep (Tests 16-17)

- [ ] 在 `internal/types/types.go` 新增 `LogWarning LogCategory = "warning"`
- [ ] 在 `kernel/kernel.go` reasonStep 中 token 更新后调用 `appendTokenSnapshot`
- [ ] 在 budget_exceeded 检查之前插入 `remainPct < 20` 告警逻辑
- [ ] 使用 `emitLog(proc, step, types.LogWarning, ...)` 发射告警
- [ ] Run: `go test -race ./kernel/ -run "TestReasonStep_Budget|TestReasonStep_Token"`
- [ ] ✅ Tests 16-17 pass

### Phase 6: IPC 协议与 Handler (Tests 18-20)

- [ ] 在 `ipc/protocol.go` 新增 MethodCtxGrowth、CtxGrowthRequest
- [ ] 在 `ipc/server.go` handleConn 增加 case MethodCtxGrowth
- [ ] 实现 `handleCtxGrowth`：GetProcInfo → GetTokenHistory → PredictGrowth → response
- [ ] 在 `ipc/client.go` 实现 `CtxGrowth(pid) (*debug.GrowthPrediction, error)`
- [ ] Run: `go test -race ./ipc/ -run TestServer_CtxGrowth`
- [ ] ✅ Tests 18-20 pass

### Phase 7: CLI ctx-growth 命令 (Tests 21-24)

- [ ] 在 `cmd/rnix/ctx_growth.go` 定义 ctxGrowthCmd
- [ ] 实现 `runCtxGrowth(cmd, args) error`
- [ ] 在 `cmd/rnix/main.go` init 中 `rootCmd.AddCommand(ctxGrowthCmd)`
- [ ] 支持 --json、daemon 不可用友好错误
- [ ] Run: `go test -race ./cmd/rnix/ -run TestCtxGrowthCmd`
- [ ] ✅ Tests 21-24 pass

### Phase 8: rnix top 集成 (AC #2)

- [ ] 修改 `cmd/rnix/top.go` 告警阈值 90% → 80%
- [ ] Run: `go test -race ./cmd/rnix/ -run TestTop` (现有 top 测试仍通过)

---

## Running Tests

```bash
# Run all tests for story 15-5 (affected packages)
go test -race -v ./kernel/ ./debug/ ./ipc/ ./cmd/rnix/ ./internal/types/ -run "TestProcess_TokenHistory|TestPredictGrowth|TestFormatGrowthPrediction|TestGrowthPrediction_MarshalJSON|TestReasonStep_Budget|TestReasonStep_Token|TestServer_CtxGrowth|TestCtxGrowthCmd"

# Run token history unit tests
go test -race -v ./kernel/ -run TestProcess_TokenHistory

# Run growth prediction unit tests
go test -race -v ./debug/ -run "TestPredictGrowth|TestFormatGrowthPrediction|TestGrowthPrediction_MarshalJSON"

# Run budget warning tests
go test -race -v ./kernel/ -run "TestReasonStep_Budget|TestReasonStep_Token"

# Run IPC handler tests
go test -race -v ./ipc/ -run TestServer_CtxGrowth

# Run CLI integration tests
go test -race -v ./cmd/rnix/ -run TestCtxGrowthCmd

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 24 tests designed and specified
- ✅ Test strategy mapped to all 5 acceptance criteria
- ✅ Implementation checklist created with 8-phase approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (kernel.TokenSnapshot, debug.GrowthPrediction, debug.PredictGrowth, debug.FormatGrowthPrediction, MethodCtxGrowth, handleCtxGrowth, ctxGrowthCmd, types.LogWarning, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (token history) → Tests 1-4 pass
2. Implement Phase 2 (prediction engine) → Tests 5-11 pass
3. Implement Phase 3 (format) → Tests 12-14 pass
4. Implement Phase 4 (MarshalJSON) → Test 15 pass
5. Implement Phase 5 (budget warning + history in reasonStep) → Tests 16-17 pass
6. Implement Phase 6 (IPC) → Tests 18-20 pass
7. Implement Phase 7 (CLI) → Tests 21-24 pass
8. Implement Phase 8 (top integration) → Existing top tests pass
9. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, 15-4 patterns available, test framework configured)
- [x] Test strategy maps to all 5 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] Implementation checklist covers all 9 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- ctx-growth 是顶级命令（rootCmd.AddCommand），非 trace 子命令
- TokenSnapshot 环形缓冲区参照 logHistory 模式，max 50 条
- PredictGrowth 使用双速率模型：AvgRate (全局) + RecentRate (最近 5 步移动均值)
- AlertLevel: none (>= 20%), warning (10-20%), critical (< 10%)
- LogWarning 新增到 internal/types，不影响现有 LogCategory 使用
- Budget warning 在 reasonStep 中 budget_exceeded 检查之前发射
- rnix top 告警阈值从 90% 降至 80%（对齐 AC#2 的 20% remaining 需求）
- IPC handleCtxGrowth 需 kern.GetTokenHistory(pid) 获取历史数据

---

**Generated by BMad TEA Agent** - 2026-03-08
