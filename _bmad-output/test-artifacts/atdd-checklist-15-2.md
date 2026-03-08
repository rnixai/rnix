---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/15-2-distributed-tracing-view.md'
  - '_bmad-output/implementation-artifacts/15-1-trace-id-generation-and-span-recording.md'
  - '_bmad/tea/config.yaml'
  - 'debug/trace.go'
  - 'debug/trace_test.go'
  - 'cmd/rnix/replay.go'
  - 'cmd/rnix/main.go'
  - 'internal/ui/styles.go'
  - 'internal/ui/trace.go'
---

# ATDD Checklist - Epic 15, Story 2: 分布式追踪视图

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
- Story 15-2 approved with 5 clear acceptance criteria (AC #1-5)
- Story 15-1 completed: SpanReader, SpanWriter, Span types, SpanRecorder all available
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/15-2-distributed-tracing-view.md`
- **Acceptance Criteria:** 5 ACs covering Span tree view, parent-child display, trace listing, error handling, JSON output
- **Affected Components:** `debug/` (new trace_view.go), `cmd/rnix/` (new trace.go)
- **Dependencies:** Builds on Story 15-1 SpanReader/Span types

### Framework & Existing Patterns
- Existing test patterns in `debug/*_test.go` (trace, record, replay, snapshot_diff, fork tests)
- Existing CLI test patterns in `cmd/rnix/replay_test.go`
- Test pattern: Go table-driven tests, `t.TempDir()` for filesystem, `t.Helper()`, `-race` detector
- Formatting test pattern: string comparison with expected output (reference: `debug/event_test.go`)

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (debug/trace_view, cmd/rnix/trace CLI)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | `rnix trace <trace-id>` 展示 Span 树状视图，含时序、持续时间、token 消耗 | Unit (debug/trace_view) + Integration (cmd/rnix/trace) | P0 |
| AC#2 | Span parent-child 关系清晰展示（树形缩进） | Unit (debug/trace_view) | P0 |
| AC#3 | `rnix trace` 无参数列出所有可用 Trace ID | Unit (debug/trace) + Integration (cmd/rnix/trace) | P0 |
| AC#4 | 不存在的 Trace ID 返回友好错误信息 | Integration (cmd/rnix/trace) | P0 |
| AC#5 | `--json` 模式输出 JSON 格式 Span 树 | Integration (cmd/rnix/trace) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests | ~18 | SpanTree 构建、FormatTraceTree、FormatTraceList、ListTraces |
| Integration Tests | ~8 | CLI 命令：列表、树输出、错误处理、JSON 输出 |
| **Total** | **~26** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — debug/trace_view_test.go

**File:** `debug/trace_view_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestBuildSpanTree_SingleRoot` | #1 | P0 | 单根节点场景，Root 正确、Children 为空 |
| 2 | `TestBuildSpanTree_TwoLevels` | #1, #2 | P0 | 根节点 + 2 个子节点，Children 正确链接 |
| 3 | `TestBuildSpanTree_ThreeLevels` | #2 | P0 | 3 层嵌套树，孙节点正确挂载 |
| 4 | `TestBuildSpanTree_ChildrenSortedByStartTime` | #1 | P0 | 子节点按 StartTime 升序排列 |
| 5 | `TestBuildSpanTree_EmptySpans` | #1 | P1 | 空 Span 列表返回 nil 树 |
| 6 | `TestBuildSpanTree_MultipleRoots` | #1 | P1 | 多个 ParentSpanID 为空的 Span，取最早的作为根，其余挂载到根下 |
| 7 | `TestBuildSpanTree_Metadata` | #1 | P0 | TraceMetadata 统计正确：TotalSpans、TotalTokens、TotalDuration、ErrorCount |
| 8 | `TestSpanTree_Walk` | #2 | P0 | Walk 深度优先遍历，depth 参数正确 |
| 9 | `TestFormatTraceTree_SingleSpan` | #1 | P0 | 单 Span 树输出包含 Trace ID、span 数、duration、tokens |
| 10 | `TestFormatTraceTree_ParentChild` | #2 | P0 | 父子 Span 的树形连接线（├─、└─）正确 |
| 11 | `TestFormatTraceTree_ThreeLevels` | #2 | P0 | 3 层嵌套的缩进和连接线正确（│ 延续线） |
| 12 | `TestFormatTraceTree_ErrorSpan` | #1 | P0 | 错误状态 Span 标记为 "error" |
| 13 | `TestFormatTraceTree_TimeoutSpan` | #1 | P0 | 超时状态 Span 标记为 "timeout" |
| 14 | `TestFormatTraceTree_Verbose` | #1 | P1 | verbose 模式显示 SpanID、SyscallCount、StartTime |
| 15 | `TestFormatTraceList_Empty` | #3 | P1 | 空列表输出 "No traces found" 或空字符串 |
| 16 | `TestFormatTraceList_Multiple` | #3 | P0 | 多个 Trace 的列表输出包含表头、TraceID、Spans、Duration、Root |

### Unit Tests — debug/trace_test.go (SpanReader.ListTraces 扩展)

**File:** `debug/trace_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 17 | `TestSpanReader_ListTraces_Empty` | #3 | P0 | 空目录返回空列表 |
| 18 | `TestSpanReader_ListTraces_Single` | #3 | P0 | 单个 Trace 目录返回 1 个 TraceSummary |
| 19 | `TestSpanReader_ListTraces_Multiple` | #3 | P0 | 多个 Trace 目录，按 StartTime 倒序 |
| 20 | `TestSpanReader_ListTraces_CorruptFile` | #3 | P1 | 损坏的 spans.jsonl 跳过该 Trace，不报错 |

### Integration Tests — cmd/rnix/trace_test.go

**File:** `cmd/rnix/trace_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 21 | `TestTraceCmd_NoArgs_ListTraces` | #3 | P0 | 无参数时列出 Trace 列表 |
| 22 | `TestTraceCmd_ValidTraceID_TreeOutput` | #1, #2 | P0 | 有效 TraceID → 树状视图输出 |
| 23 | `TestTraceCmd_InvalidTraceID_Error` | #4 | P0 | 不存在的 TraceID → 友好错误信息 |
| 24 | `TestTraceCmd_ValidTraceID_JSON` | #5 | P0 | --json 模式 → JSON 输出（JSONResponse 包装） |
| 25 | `TestTraceCmd_NoArgs_EmptyDir` | #3 | P0 | 无 Trace 时输出 "No traces found" |
| 26 | `TestTraceCmd_NoArgs_JSON` | #3, #5 | P1 | 无参数 + --json → JSON 列表输出 |

---

## Fixtures & Helpers

### Span Test Helpers

**位置:** `debug/trace_view_test.go` 内部

- `makeSpan(traceID, spanID, parentSpanID, name string, pid types.PID, startOffset, duration time.Duration, tokens int, status SpanStatus) *Span` — 创建测试用 Span，以 baseTime + startOffset 为 StartTime
- `makeSpanTree(spans ...*Span) *SpanTree` — 便捷构建 SpanTree（调用 BuildSpanTree）

### CLI Test Helpers

**位置:** `cmd/rnix/trace_test.go` 内部

- `setupTraceTestDir(t *testing.T, traces map[types.TraceID][]*debug.Span) string` — 在 t.TempDir 下创建 `.rnix/traces/<trace-id>/spans.jsonl` 结构，写入 Span 数据
- 复用 `cmd/rnix/replay_test.go` 的 Cobra 命令执行模式

---

## Mock Requirements

### 无外部服务 Mock

本 Story 不涉及外部服务或 IPC 通信。所有操作都是本地文件系统读取。

### Test 文件系统

使用 `t.TempDir()` 创建临时 `.rnix/traces/` 目录结构，写入测试 Span JSONL 数据。

---

## Implementation Checklist

### Phase 1: SpanReader.ListTraces (Tests 17-20)

- [ ] 在 `debug/trace.go` 的 SpanReader 上新增 `ListTraces() ([]TraceSummary, error)` 方法
- [ ] 定义 `TraceSummary` 结构体（debug/trace_view.go 或 debug/trace.go）
- [ ] 实现目录扫描和 spans.jsonl 首行解析逻辑
- [ ] Run: `go test -race ./debug/ -run TestSpanReader_ListTraces`
- [ ] ✅ Tests 17-20 pass

### Phase 2: SpanTree 构建 (Tests 1-8)

- [ ] 在 `debug/trace_view.go` 定义 SpanTree、SpanNode、TraceMetadata 类型
- [ ] 实现 `BuildSpanTree(spans []*Span) *SpanTree`
- [ ] 实现 `SpanTree.Walk(fn func(node *SpanNode, depth int))`
- [ ] Run: `go test -race ./debug/ -run TestBuildSpanTree`
- [ ] Run: `go test -race ./debug/ -run TestSpanTree_Walk`
- [ ] ✅ Tests 1-8 pass

### Phase 3: 格式化输出 (Tests 9-16)

- [ ] 实现 `FormatTraceTree(tree *SpanTree, verbose bool) string`
- [ ] 实现 `FormatTraceList(summaries []TraceSummary) string`
- [ ] 使用 box-drawing 字符（├─、└─、│）绘制树形连接线
- [ ] 支持 verbose 模式额外信息
- [ ] Run: `go test -race ./debug/ -run TestFormatTrace`
- [ ] ✅ Tests 9-16 pass

### Phase 4: CLI 命令 (Tests 21-26)

- [ ] 创建 `cmd/rnix/trace.go`，定义 `traceCmd` Cobra 命令
- [ ] 实现 `runTrace(cmd *cobra.Command, args []string) error`
- [ ] 在 `init()` 中注册 `rootCmd.AddCommand(traceCmd)`
- [ ] 无参数 → ListTraces + FormatTraceList
- [ ] 有参数 → ReadSpans + BuildSpanTree + FormatTraceTree
- [ ] 支持 --json 和 --verbose 标志
- [ ] 不存在的 TraceID → 友好错误信息
- [ ] Run: `go test -race ./cmd/rnix/ -run TestTraceCmd`
- [ ] ✅ Tests 21-26 pass

---

## Running Tests

```bash
# Run all tests for story 15-2 (affected packages)
go test -race -v ./debug/ ./cmd/rnix/ -run "TestBuildSpanTree|TestSpanTree_Walk|TestFormatTrace|TestSpanReader_ListTraces|TestTraceCmd"

# Run SpanTree build tests
go test -race -v ./debug/ -run TestBuildSpanTree

# Run format tests
go test -race -v ./debug/ -run TestFormatTrace

# Run ListTraces tests
go test -race -v ./debug/ -run TestSpanReader_ListTraces

# Run CLI integration tests
go test -race -v ./cmd/rnix/ -run TestTraceCmd

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 26 tests designed and specified
- ✅ Test strategy mapped to all 5 acceptance criteria
- ✅ Implementation checklist created with 4-phase approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (debug.SpanTree, debug.BuildSpanTree, debug.FormatTraceTree, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (ListTraces) → Tests 17-20 pass
2. Implement Phase 2 (SpanTree build) → Tests 1-8 pass
3. Implement Phase 3 (formatting) → Tests 9-16 pass
4. Implement Phase 4 (CLI) → Tests 21-26 pass
5. Run full suite: `go test -race ./...` → All 19+ packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, 15-1 complete, test framework configured)
- [x] Test strategy maps to all 5 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] No concurrency concerns in this story (all file reads, no shared state)
- [x] Implementation checklist covers all 6 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- 本 story 基于 15-1 的 SpanReader.ReadSpans 和 Span 类型，无需重新测试这些基础功能
- SpanTree/SpanNode 结构为后续 15-3（trace blame）的分析基础，需确保 Walk 方法通用性
- FormatTraceTree 不使用 lipgloss 样式（放在 debug 包中），颜色输出由 CLI 层控制（或通过简单的 ANSI 转义）
- CLI 测试使用 t.TempDir() 创建临时目录结构，不依赖真实的 .rnix/traces/ 数据
- trace 命令是纯本地操作，不需要 daemon，测试模式与 replay_test.go 类似

---

**Generated by BMad TEA Agent** - 2026-03-08
