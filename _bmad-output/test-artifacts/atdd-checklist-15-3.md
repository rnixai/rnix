---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/15-3-trace-blame-root-cause-analysis.md'
  - '_bmad-output/implementation-artifacts/15-2-distributed-tracing-view.md'
  - '_bmad-output/implementation-artifacts/15-1-trace-id-generation-and-span-recording.md'
  - '_bmad/tea/config.yaml'
  - 'debug/trace.go'
  - 'debug/trace_view.go'
  - 'debug/trace_view_test.go'
  - 'cmd/rnix/trace.go'
  - 'cmd/rnix/trace_test.go'
---

# ATDD Checklist - Epic 15, Story 3: Trace Blame 根因定位

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
- Story 15-3 approved with 5 clear acceptance criteria (AC #1-5)
- Story 15-1 completed: SpanReader, SpanWriter, Span types, SpanRecorder all available
- Story 15-2 completed: BuildSpanTree, SpanTree, SpanNode, FormatTraceTree, traceCmd all available
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/15-3-trace-blame-root-cause-analysis.md`
- **Acceptance Criteria:** 5 ACs covering blame analysis, error chains, JSON output, error handling, no-error scenario
- **Affected Components:** `debug/` (new trace_blame.go), `cmd/rnix/` (modify trace.go)
- **Dependencies:** Builds on Story 15-1 SpanReader/Span types and Story 15-2 SpanTree/BuildSpanTree

### Framework & Existing Patterns
- Existing test patterns in `debug/trace_view_test.go` (BuildSpanTree, FormatTraceTree tests)
- Existing CLI test patterns in `cmd/rnix/trace_test.go`
- Test pattern: Go table-driven tests, `t.TempDir()` for filesystem, `t.Helper()`, `-race` detector
- Span helper pattern: `makeSpan()` and `makeSpanTree()` helpers from trace_view_test.go

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (debug/trace_blame, cmd/rnix/trace CLI blame subcommand)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | `rnix trace blame <trace-id>` 自动分析：耗时最长节点、token 消耗最大节点、错误关键路径 | Unit (debug/trace_blame) + Integration (cmd/rnix/trace) | P0 |
| AC#2 | 包含错误节点时显示错误传播链路（根因到最终失败的完整路径） | Unit (debug/trace_blame) | P0 |
| AC#3 | `--json` 模式输出 JSON 格式 blame 结果 | Integration (cmd/rnix/trace) | P0 |
| AC#4 | 不存在的 Trace ID 返回友好错误信息 | Integration (cmd/rnix/trace) | P0 |
| AC#5 | 全 OK Trace 仍输出耗时和 token 分析，错误链路段为空 | Unit (debug/trace_blame) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests | ~16 | AnalyzeTrace、findCriticalPath、findDurationHotspots、findTokenHotspots、findErrorChains、FormatBlameResult |
| Integration Tests | ~4 | CLI blame 子命令：正常输出、错误处理、JSON 输出 |
| **Total** | **~20** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — debug/trace_blame_test.go

**File:** `debug/trace_blame_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestAnalyzeTrace_LinearChain` | #1 | P0 | 3 节点线性链 A→B→C：关键路径 = 全链路，hotspots 排名正确 |
| 2 | `TestAnalyzeTrace_BranchTree` | #1 | P0 | 分支树 root→[child1, child2]：关键路径选择耗时更长的分支 |
| 3 | `TestAnalyzeTrace_AllOK` | #5 | P0 | 全 OK 场景：ErrorChains 为空，Duration/Token hotspots 正常 |
| 4 | `TestAnalyzeTrace_AllError` | #1, #2 | P0 | 全错误场景：多条 ErrorChain 验证 |
| 5 | `TestAnalyzeTrace_MixedErrors` | #2 | P0 | 部分节点 ERROR：RootCause 标记正确，错误链路完整 |
| 6 | `TestAnalyzeTrace_TokenIndependentOfDuration` | #1 | P0 | Token hotspots 排名独立于 Duration 排名 |
| 7 | `TestAnalyzeTrace_Summary` | #1 | P0 | BlameSummary 统计正确：TotalSpans、ErrorSpans、CriticalPathPct |
| 8 | `TestFindCriticalPath_DeepTree` | #1 | P0 | 4 层深度嵌套树：正确找到最长路径 |
| 9 | `TestFindCriticalPath_SingleNode` | #1 | P1 | 单节点树：关键路径只有根节点 |
| 10 | `TestFindErrorChains_SingleErrorLeaf` | #2 | P0 | 单个错误叶节点：一条 ErrorChain，RootCause 是叶节点 |
| 11 | `TestFindErrorChains_MiddleAndLeafError` | #2 | P0 | 中间节点和叶节点都出错：RootCause 是路径中最先出错的节点 |
| 12 | `TestFindErrorChains_MultipleIndependentErrors` | #2 | P0 | 多条独立错误链：返回多个 ErrorChain |
| 13 | `TestFindErrorChains_TimeoutStatus` | #2 | P1 | TIMEOUT 状态节点也纳入错误链 |
| 14 | `TestFormatBlameResult_WithErrors` | #1, #2 | P0 | 输出含 "Critical Path"、"Duration Hotspots"、"Token Hotspots"、"Error Chains" 段落 |
| 15 | `TestFormatBlameResult_NoErrors` | #5 | P0 | 无错误时无 "Error Chains" 段落 |
| 16 | `TestFormatBlameResult_Ranking` | #1 | P0 | 排名格式 #1/#2/#3 正确，百分比计算正确 |

### Integration Tests — cmd/rnix/trace_test.go

**File:** `cmd/rnix/trace_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 17 | `TestBlameCmd_ValidTraceID` | #1, #2 | P0 | `trace blame <valid-id>` → 输出含 "Critical Path" 和 "Duration Hotspots" |
| 18 | `TestBlameCmd_InvalidTraceID` | #4 | P0 | `trace blame <invalid-id>` → 错误信息 |
| 19 | `TestBlameCmd_JSON` | #3 | P0 | `trace blame <valid-id> --json` → JSON 输出（JSONResponse 包装） |
| 20 | `TestBlameCmd_AllOK` | #5 | P0 | 全 OK Trace → 无 "Error Chains" 段落，仍有 hotspots |

---

## Fixtures & Helpers

### Span Test Helpers

**位置:** `debug/trace_blame_test.go` 内部

- `makeBlameSpan(traceID, spanID, parentSpanID, name string, pid types.PID, startOffset, duration time.Duration, tokens int, status SpanStatus) *Span` — 创建测试用 Span，以 baseTime + startOffset 为 StartTime
- `buildBlameTree(spans ...*Span) *SpanTree` — 便捷构建 SpanTree（调用 BuildSpanTree）

### CLI Test Helpers

**位置:** `cmd/rnix/trace_test.go` 内部

- 复用 15-2 已有的 `setupTraceTestDir` helper（在 t.TempDir 下创建 `.rnix/traces/<trace-id>/spans.jsonl` 结构）
- 复用 15-2 已有的 Cobra 命令执行模式

---

## Mock Requirements

### 无外部服务 Mock

本 Story 不涉及外部服务或 IPC 通信。所有操作都是本地文件系统读取 + 内存分析。

### Test 文件系统

使用 `t.TempDir()` 创建临时 `.rnix/traces/` 目录结构，写入测试 Span JSONL 数据。

---

## Implementation Checklist

### Phase 1: Blame 数据类型 (Tests 1, 3, 7)

- [ ] 在 `debug/trace_blame.go` 中定义 BlameResult、BlameEntry、ErrorChain、BlameSummary 类型
- [ ] 定义 AnalyzeTrace 函数签名
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeTrace` (should compile but tests fail)
- [ ] ✅ Types compile correctly

### Phase 2: 关键路径分析 (Tests 1, 2, 8, 9)

- [ ] 实现 `findCriticalPath(tree *SpanTree) ([]*SpanNode, time.Duration)`
- [ ] DFS 遍历，累加每条路径 Duration，返回最长路径
- [ ] Run: `go test -race ./debug/ -run TestFindCriticalPath`
- [ ] ✅ Tests 8-9 pass

### Phase 3: Hotspot 分析 (Tests 1, 2, 6)

- [ ] 实现 `findDurationHotspots(tree *SpanTree, topN int) []*BlameEntry`
- [ ] 实现 `findTokenHotspots(tree *SpanTree, topN int) []*BlameEntry`
- [ ] 百分比计算：Span 值 / Trace 总量 × 100
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeTrace`
- [ ] ✅ Tests 1, 2, 6 pass

### Phase 4: 错误链分析 (Tests 4, 5, 10-13)

- [ ] 实现 `findErrorChains(tree *SpanTree) []ErrorChain`
- [ ] 查找 ERROR/TIMEOUT 叶节点，回溯到根构建路径
- [ ] 最先出错的 Span 标记为 RootCause
- [ ] Run: `go test -race ./debug/ -run TestFindErrorChains`
- [ ] ✅ Tests 10-13 pass

### Phase 5: AnalyzeTrace 集成 + Summary (Tests 3, 4, 5, 7)

- [ ] 实现 `AnalyzeTrace(tree *SpanTree) *BlameResult` 主函数
- [ ] 调用 findCriticalPath + findDurationHotspots + findTokenHotspots + findErrorChains
- [ ] 构建 BlameSummary
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeTrace`
- [ ] ✅ Tests 1-7 pass

### Phase 6: 格式化输出 (Tests 14-16)

- [ ] 实现 `FormatBlameResult(result *BlameResult) string`
- [ ] 段落：Critical Path（→ 箭头）、Duration Hotspots（#N 排名）、Token Hotspots、Error Chains（✗/↑ 符号）
- [ ] 无错误时省略 Error Chains
- [ ] Run: `go test -race ./debug/ -run TestFormatBlameResult`
- [ ] ✅ Tests 14-16 pass

### Phase 7: JSON 序列化

- [ ] 为 BlameResult 实现 MarshalJSON（snake_case、时间用 _ms 后缀）
- [ ] 验证 JSON 输出结构正确

### Phase 8: CLI blame 子命令 (Tests 17-20)

- [ ] 在 `cmd/rnix/trace.go` 新增 blameCmd 子命令
- [ ] 实现 `runBlame(cmd *cobra.Command, args []string) error`
- [ ] 在 `init()` 中注册 `traceCmd.AddCommand(blameCmd)`
- [ ] 支持 --json 全局标志
- [ ] Run: `go test -race ./cmd/rnix/ -run TestBlameCmd`
- [ ] ✅ Tests 17-20 pass

---

## Running Tests

```bash
# Run all tests for story 15-3 (affected packages)
go test -race -v ./debug/ ./cmd/rnix/ -run "TestAnalyzeTrace|TestFindCriticalPath|TestFindErrorChains|TestFormatBlameResult|TestBlameCmd"

# Run blame analysis tests
go test -race -v ./debug/ -run "TestAnalyzeTrace|TestFindCriticalPath|TestFindErrorChains"

# Run blame format tests
go test -race -v ./debug/ -run TestFormatBlameResult

# Run CLI blame integration tests
go test -race -v ./cmd/rnix/ -run TestBlameCmd

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 20 tests designed and specified
- ✅ Test strategy mapped to all 5 acceptance criteria
- ✅ Implementation checklist created with 8-phase approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (debug.BlameResult, debug.AnalyzeTrace, debug.FormatBlameResult, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (types) → Types compile
2. Implement Phase 2 (critical path) → Tests 8-9 pass
3. Implement Phase 3 (hotspots) → Tests 1, 2, 6 pass
4. Implement Phase 4 (error chains) → Tests 10-13 pass
5. Implement Phase 5 (AnalyzeTrace) → Tests 1-7 pass
6. Implement Phase 6 (formatting) → Tests 14-16 pass
7. Implement Phase 7 (JSON) → Serialization correct
8. Implement Phase 8 (CLI) → Tests 17-20 pass
9. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, 15-1 and 15-2 complete, test framework configured)
- [x] Test strategy maps to all 5 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] No concurrency concerns in this story (all file reads + in-memory analysis, no shared state)
- [x] Implementation checklist covers all 5 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- 本 story 基于 15-2 的 BuildSpanTree 和 SpanTree.Walk，无需重新测试树构建功能
- BlameResult 结构为后续可视化仪表盘（Epic 17）的分析基础，需确保 JSON 序列化通用性
- FormatBlameResult 使用纯文本符号（→、✗、↑、#），不使用 lipgloss 样式（放在 debug 包中）
- blame 命令是 trace 的 Cobra 子命令，使用 `traceCmd.AddCommand(blameCmd)` 注册
- CLI 测试复用 15-2 已有的 setupTraceTestDir helper 和 Cobra 执行模式
- 关键路径分析使用 DFS，对小规模 Span 树（通常 < 100 节点）性能足够

---

**Generated by BMad TEA Agent** - 2026-03-08
