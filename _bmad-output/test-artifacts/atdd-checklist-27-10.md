---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-22'
storyId: '27-10'
storyTitle: 'Dashboard Multi-Agent Evaluation View'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/27-10-dashboard-multi-agent-evaluation-view.md
  - cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
---

# ATDD Checklist — Story 27.10: Dashboard Multi-Agent Evaluation View

## Test File

`cmd/rnix/atdd_27_10_dashboard_eval_panel_test.go`

## TDD Phase: RED

All tests reference types and methods that do not yet exist (paneEval, evalSubView, evalReputationMsg, renderEvalPane, etc.). Tests will fail at compile time until implementation is complete.

## Test Summary

| ID | AC | Priority | Test Function | Description |
|----|-----|----------|--------------|-------------|
| 27.10-UNIT-001 | AC-1 | P0 | TestATDD_27_10_AC1_PaneEvalConstant | paneEval = 7 |
| 27.10-UNIT-002 | AC-1 | P0 | TestATDD_27_10_AC1_TabCycles8Panes | Tab cycles 8 panes |
| 27.10-UNIT-003 | AC-1 | P0 | TestATDD_27_10_AC1_EvalPaneRenders | Eval pane renders |
| 27.10-UNIT-004 | AC-1 | P1 | TestATDD_27_10_AC1_StatusBarEvalHelp | Status bar help text |
| 27.10-UNIT-005 | AC-2 | P0 | TestATDD_27_10_AC2_ModelHasEvalFields | Model eval fields exist |
| 27.10-UNIT-006 | AC-2 | P0 | TestATDD_27_10_AC2_ReputationMsgUpdatesModel | Reputation msg updates model |
| 27.10-UNIT-007 | AC-2 | P0 | TestATDD_27_10_AC2_ReputationMsgError | Reputation msg error handling |
| 27.10-UNIT-008 | AC-2 | P0 | TestATDD_27_10_AC2_ReputationSortedByScoreDesc | Sorted by score desc |
| 27.10-UNIT-009 | AC-2 | P0 | TestATDD_27_10_AC2_RenderEvalPane_ReputationDetails | Reputation rendering |
| 27.10-UNIT-010 | AC-2 | P0 | TestATDD_27_10_AC2_CursorClampedAfterRefresh | Cursor bounds after refresh |
| 27.10-UNIT-011 | AC-2 | P0 | TestATDD_27_10_AC2_JK_MovesRepCursor | j/k cursor navigation |
| 27.10-UNIT-012 | AC-2 | P0 | TestATDD_27_10_AC2_RepCursorBounds | Cursor bounds checking |
| 27.10-UNIT-013 | AC-3 | P0 | TestATDD_27_10_AC3_Key1_SelectsReputation | Key 1 → reputation |
| 27.10-UNIT-014 | AC-3 | P0 | TestATDD_27_10_AC3_Key2_SelectsTopology | Key 2 → topology |
| 27.10-UNIT-015 | AC-3 | P0 | TestATDD_27_10_AC3_Key3_SelectsSynergy | Key 3 → synergy |
| 27.10-UNIT-016 | AC-3 | P0 | TestATDD_27_10_AC3_SubViewKeysOnlyInEvalPane | Keys guarded to eval pane |
| 27.10-UNIT-017 | AC-3 | P0 | TestATDD_27_10_AC3_SubViewPreservedAfterTabCycle | Sub-view preserved after tab |
| 27.10-UNIT-018 | AC-4 | P0 | TestATDD_27_10_AC4_TopologyMsgUpdatesModel | Topology msg updates model |
| 27.10-UNIT-019 | AC-4 | P0 | TestATDD_27_10_AC4_TopologyMsgError | Topology msg error |
| 27.10-UNIT-020 | AC-4 | P0 | TestATDD_27_10_AC4_RenderTopology_Nodes | Topology renders nodes |
| 27.10-UNIT-021 | AC-4 | P0 | TestATDD_27_10_AC4_RenderTopology_Edges | Topology renders edges |
| 27.10-UNIT-022 | AC-4 | P0 | TestATDD_27_10_AC4_JK_MovesTopoCursor | j/k topology cursor |
| 27.10-UNIT-023 | AC-5 | P0 | TestATDD_27_10_AC5_SynergyMsgUpdatesModel | Synergy msg updates model |
| 27.10-UNIT-024 | AC-5 | P0 | TestATDD_27_10_AC5_SynergyMsgError | Synergy msg error |
| 27.10-UNIT-025 | AC-5 | P0 | TestATDD_27_10_AC5_RenderSynergy_Combos | Synergy renders combos |
| 27.10-UNIT-026 | AC-5 | P0 | TestATDD_27_10_AC5_JK_MovesSynCursor | j/k synergy cursor |
| 27.10-UNIT-027 | AC-5 | P0 | TestATDD_27_10_AC5_SynCursorBounds | Synergy cursor bounds |
| 27.10-UNIT-028 | AC-6 | P1 | TestATDD_27_10_AC6_EmptyReputation_ShowsHint | Empty reputation hint |
| 27.10-UNIT-029 | AC-6 | P1 | TestATDD_27_10_AC6_EmptyReputationLoaded_ShowsHint | Loaded but empty rep |
| 27.10-UNIT-030 | AC-6 | P1 | TestATDD_27_10_AC6_EmptyTopology_ShowsHint | Empty topology hint |
| 27.10-UNIT-031 | AC-6 | P1 | TestATDD_27_10_AC6_EmptySynergy_ShowsHint | Empty synergy hint |
| 27.10-UNIT-032 | AC-6 | P1 | TestATDD_27_10_AC6_IPCError_ShowsError | IPC error rendering |
| 27.10-UNIT-033 | AC-6 | P1 | TestATDD_27_10_AC6_EmptyState_NavigationSafe | Safe navigation on empty |
| 27.10-UNIT-034 | AC-6 | P1 | TestATDD_27_10_AC6_TopologyError_Renders | Topology error rendering |
| 27.10-UNIT-035 | AC-6 | P1 | TestATDD_27_10_AC6_SynergyError_Renders | Synergy error rendering |
| 27.10-UNIT-036 | AC-2 | P1 | TestATDD_27_10_RepAdjustScroll | Reputation scroll adjust |
| 27.10-UNIT-037 | AC-4 | P1 | TestATDD_27_10_TopoAdjustScroll | Topology scroll adjust |
| 27.10-UNIT-038 | AC-5 | P1 | TestATDD_27_10_SynAdjustScroll | Synergy scroll adjust |
| 27.10-UNIT-039 | AC-3 | P0 | TestATDD_27_10_VKeyGuard | v/V/p key guard |

## Coverage Matrix

| AC | P0 Tests | P1 Tests | Total |
|----|----------|----------|-------|
| AC-1 (paneEval + Tab) | 3 | 1 | 4 |
| AC-2 (Reputation) | 6 | 2 | 8 |
| AC-3 (Sub-view switch) | 6 | 0 | 6 |
| AC-4 (Topology) | 4 | 1 | 5 |
| AC-5 (Synergy) | 5 | 1 | 6 |
| AC-6 (Empty states) | 0 | 8 | 8 |
| **Total** | **24** | **13** | **37** |

## Dependencies

- Story 27.3-27.9 (Dashboard pane patterns) — completed
- Epic 21 (Reputation system) — ReputationSummary, ComboSummary types available
- Epic 22 (Collaboration topology) — TopologyNode, CooperationEdge types available
- IPC methods (reputation_status, topology_query, synergy_list) — all implemented

## Key Types Referenced (RED — to be added)

### New Types (in dashboard.go)
- `paneEval paneType = 7`
- `evalReputationMsg { summaries []kernel.ReputationSummary; err error }`
- `evalTopologyMsg { topology *ipc.TopologyQueryResponse; err error }`
- `evalSynergyMsg { combos []kernel.ComboSummary; err error }`

### New Fields (in dashboardModel)
- `evalSubView int` (0=reputation, 1=topology, 2=synergy)
- `evalReputations []kernel.ReputationSummary`
- `evalRepErr error`
- `evalRepCursor int`
- `evalRepScrollOffset int`
- `evalTopology *ipc.TopologyQueryResponse`
- `evalTopoErr error`
- `evalTopoCursor int`
- `evalTopoScrollOffset int`
- `evalSynergies []kernel.ComboSummary`
- `evalSynErr error`
- `evalSynCursor int`
- `evalSynScrollOffset int`

### New Functions
- `renderEvalPane(width, height int) string`
- `fetchReputationCmd() tea.Cmd`
- `fetchTopologyCmd() tea.Cmd`
- `fetchSynergyCmd() tea.Cmd`
- `evalRepAdjustScroll(m *dashboardModel)`
- `evalTopoAdjustScroll(m *dashboardModel)`
- `evalSynAdjustScroll(m *dashboardModel)`
