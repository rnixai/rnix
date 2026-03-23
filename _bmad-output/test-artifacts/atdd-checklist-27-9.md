---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04c-aggregate'
lastStep: 'step-04c-aggregate'
lastSaved: '2026-03-22'
storyId: '27-9'
storyFile: '_bmad-output/implementation-artifacts/27-9-dashboard-distributed-tracing-integration.md'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - '_bmad-output/implementation-artifacts/27-9-dashboard-distributed-tracing-integration.md'
  - 'cmd/rnix/atdd_27_8_dashboard_security_panel_test.go'
  - 'cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go'
  - 'cmd/rnix/dashboard.go'
  - 'ipc/protocol.go'
---

# ATDD Checklist: Story 27.9 — Dashboard 分布式追踪集成

## TDD Red Phase (Current)

All tests designed to **FAIL** until implementation exists (compile errors expected).

### Test File

- `cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go` — 37 tests (RED)

## Acceptance Criteria Coverage

### AC-1: 新增追踪窗格（paneTrace）— 5 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC1_PaneTraceConstant | P0 | paneTrace == 6 |
| TestATDD_27_9_AC1_TabCycles7Panes | P0 | Tab cycles through 7 panes (Tree→Timeline→Heatmap→Detail→Intent→Security→Trace→Tree) |
| TestATDD_27_9_AC1_TracePaneBorderHighlight | P0 | renderTracePane returns non-empty output |
| TestATDD_27_9_AC1_StatusBarTraceHelp_ListMode | P1 | Status bar shows Navigate/Enter in list mode |
| TestATDD_27_9_AC1_StatusBarTraceHelp_TreeMode | P1 | Status bar shows Esc/Process in tree mode |

### AC-2: IPC 追踪数据方法 — 4 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC2_MethodConstants | P0 | MethodTraceList=="trace_list", MethodTraceTree=="trace_tree" |
| TestATDD_27_9_AC2_TraceSummaryWireFields | P0 | TraceSummaryWire struct fields (TraceID, SpanCount, StartTimeMs, TotalDurationMs, RootSpanName) |
| TestATDD_27_9_AC2_SpanTreeWireFields | P0 | SpanTreeWire struct fields (Root, TraceID, Metadata) |
| TestATDD_27_9_AC2_SpanNodeWireRecursive | P0 | SpanNodeWire recursive Children structure |

### AC-3: 追踪列表渲染 — 7 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC3_ModelHasTraceFields | P0 | dashboardModel trace fields exist (traceSummaries, traceErr, traceCursor, traceViewMode, selectedTraceID, selectedSpanTree) |
| TestATDD_27_9_AC3_TraceListMsgUpdatesModel | P0 | traceListMsg updates traceSummaries |
| TestATDD_27_9_AC3_TraceListMsgError | P0 | traceListMsg error sets traceErr |
| TestATDD_27_9_AC3_TraceListSortedByTimeDesc | P0 | Trace list sorted by StartTimeMs descending (newest first) |
| TestATDD_27_9_AC3_RenderTracePane_Details | P0 | Render shows truncated TraceID (16 chars) + root span name + span count |
| TestATDD_27_9_AC3_CursorClampedAfterRefresh | P0 | traceCursor clamped within range after list refresh |
| TestATDD_27_9_TraceAdjustScroll | P1 | Scroll offset adjusts to keep cursor visible |

### AC-4: Span 树展开与瀑布图 — 12 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC4_TraceTreeMsgUpdatesModel | P0 | traceTreeMsg sets selectedSpanTree, traceViewMode=1, populates spanFlatNodes |
| TestATDD_27_9_AC4_TraceTreeMsgError | P0 | traceTreeMsg error keeps traceViewMode=0 |
| TestATDD_27_9_AC4_FlattenSpanTree | P0 | flattenSpanTree produces correct 5-node DFS with correct depths |
| TestATDD_27_9_AC4_FlattenSpanTree_Fields | P0 | flattenSpanTree preserves PID and status on each node |
| TestATDD_27_9_AC4_FlattenSpanTree_NilTree | P0 | flattenSpanTree(nil) returns nil |
| TestATDD_27_9_AC4_FlattenSpanTree_NilRoot | P0 | flattenSpanTree with nil Root returns nil |
| TestATDD_27_9_AC4_SpanStatusColor | P0 | spanStatusColor: ok=green(42), error=red(196), timeout=orange(208), default=gray(240) |
| TestATDD_27_9_AC4_RenderTracePane_TreeMode | P0 | Tree mode render shows span names + PID |
| TestATDD_27_9_AC4_Enter_ListToTree | P0 | Enter in list mode sets selectedTraceID and produces fetchTraceTreeCmd |
| TestATDD_27_9_AC4_Escape_TreeToList | P0 | Escape in tree mode resets traceViewMode to 0 |
| TestATDD_27_9_SpanAdjustScroll | P1 | Span scroll offset adjusts to keep cursor visible |
| TestATDD_27_9_TabPreservesViewMode | P1 | Tab cycle preserves traceViewMode |

### AC-5: Span 节点联动 — 5 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC5_Enter_LinksToProcess | P0 | Enter on span sets selectedPID and switches to paneTimeline |
| TestATDD_27_9_AC5_Enter_ProcessGone_ShowsMessage | P0 | Enter on reaped process shows statusMsg, no PID change |
| TestATDD_27_9_AC5_JK_MovesSpanCursor | P0 | j/k navigates spanCursor in tree mode |
| TestATDD_27_9_AC5_SpanCursorBounds | P0 | j/k does not go out of bounds |
| TestATDD_27_9_AC5_JK_MovesTraceCursor | P0 | j/k navigates traceCursor in list mode |

### AC-6: 空状态处理 — 4 tests

| Test | Priority | Description |
|------|----------|-------------|
| TestATDD_27_9_AC6_EmptyState_ShowsHint | P1 | Empty state mentions "rnix compose" |
| TestATDD_27_9_AC6_ErrorState_ShowsError | P1 | Error state renders without crash |
| TestATDD_27_9_AC6_EmptyState_NavigationSafe | P1 | j/k/Enter/Escape on empty state does not panic |
| TestATDD_27_9_AC6_NilSpanTree_Renders | P1 | Nil spanTree in tree mode renders gracefully |

## Summary

| Metric | Value |
|--------|-------|
| Total Tests | 37 |
| P0 Tests | 28 |
| P1 Tests | 9 |
| AC Coverage | AC-1 through AC-6 (100%) |
| Test Level | Unit (dashboard model + rendering) |
| TDD Phase | RED (compile errors expected) |

## Next Steps (TDD Green Phase)

After implementing the feature:

1. Run `go test -race -run TestATDD_27_9 ./cmd/rnix/...`
2. Verify all 37 tests PASS (green phase)
3. If any tests fail:
   - Either fix implementation (feature bug)
   - Or fix test (test bug)
4. Update 27-7 and 27-8 Tab cycling tests (6→7 panes)
5. Commit passing tests

## Implementation Guidance

### New types to create (ipc/protocol.go)
- `MethodTraceList Method = "trace_list"`
- `MethodTraceTree Method = "trace_tree"`
- `TraceSummaryWire`, `TraceListResponse`, `TraceTreeRequest`, `TraceTreeResponse`
- `SpanTreeWire`, `SpanNodeWire`, `TraceMetaWire`

### New functions to create (cmd/rnix/dashboard.go)
- `paneTrace paneType = 6` (iota)
- `traceListMsg`, `traceTreeMsg` message types
- `fetchTraceListCmd()`, `fetchTraceTreeCmd(traceID string)` tea.Cmd
- `renderTracePane(width, height int) string`
- `flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode`
- `spanStatusColor(status string) lipgloss.Color`
- `traceAdjustScroll(m *dashboardModel)`, `spanAdjustScroll(m *dashboardModel)`
- Tab modulo: `% 6` → `% 7`
- Help text update for Trace pane (list + tree modes)

### New handlers (ipc/server.go)
- `handleTraceList` — via `debug.SpanReader.ListTraces()`
- `handleTraceTree` — via `debug.SpanReader.ReadSpans()` + `debug.BuildSpanTree()`

### New client methods (ipc/client.go)
- `TraceList() ([]TraceSummaryWire, error)`
- `TraceTree(traceID string) (*SpanTreeWire, error)`
