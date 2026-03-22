---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-22'
storyId: '27-5'
storyFile: '_bmad-output/implementation-artifacts/27-5-top-to-dashboard-navigation.md'
testFile: 'cmd/rnix/atdd_27_5_top_dashboard_nav_test.go'
detectedStack: backend
generationMode: ai-generation
tddPhase: RED
inputDocuments:
  - _bmad-output/implementation-artifacts/27-5-top-to-dashboard-navigation.md
  - cmd/rnix/top.go
  - cmd/rnix/dashboard.go
  - cmd/rnix/atdd_27_3_dashboard_timeline_test.go
  - cmd/rnix/atdd_27_4_dashboard_prompt_view_test.go
---

# ATDD Checklist — Story 27.5: top→dashboard Navigation

## Preflight

| Item | Status |
|------|--------|
| Stack type | `backend` (Go 1.26, go.mod) |
| Test framework | Go `testing` + BubbleTea v2 model unit tests |
| Story approved | Yes (ready-for-dev) |
| AC count | 7 acceptance criteria |
| Test config | `*_test.go` in `cmd/rnix/` package |

## Test Strategy

| AC | Scenario | Level | Priority | Test Function(s) |
|----|----------|-------|----------|-------------------|
| AC-1 | Enter → launchDashboardPID + quit | Unit | P0 | `TestATDD_27_5_AC1_Enter_SetsLaunchDashboardPID`, `_ReturnsQuitCmd`, `_DoesNotSetDetailPID` |
| AC-2 | --pid auto-focuses treeCursor | Unit | P0 | `TestATDD_27_5_AC2_InitialPIDFocus_PositionsCursor`, `_SetsSelectedPID`, `_ClearsAfterApply` |
| AC-3 | --pid not found → warning | Unit | P0 | `TestATDD_27_5_AC3_InitialPIDFocus_NotFound_StatusMsg`, `_CursorDefault`, `_ClearsFlag` |
| AC-4 | Enter on Dead process | Unit | P1 | `TestATDD_27_5_AC4_Enter_DeadProcess_SetsLaunchDashboardPID`, `_ReturnsQuitCmd` |
| AC-5 | q exits dashboard | — | P2 | Inherent BubbleTea behavior, no explicit test |
| AC-6 | Help line text | Unit | P1 | `TestATDD_27_5_AC6_HelpLine_ContainsDashboard`, `_NoDetailsLabel`, `_DetailView_HelpLine_NoEnterDashboard` |
| AC-7 | Detail view Enter no jump | Unit | P0 | `TestATDD_27_5_AC7_DetailView_Enter_NoDashboardJump`, `_ReturnsNilCmd`, `_StaysInDetail` |

## TDD Red Phase Results

**Run date:** 2026-03-22
**Total tests:** 16
**Failures:** 12 (expected — logic not implemented)
**Passes:** 4 (negative validations)

### Failed (12) — Implementation required

| Test | Failure Reason |
|------|---------------|
| AC1_Enter_SetsLaunchDashboardPID | launchDashboardPID = 0, want 42 |
| AC1_Enter_ReturnsQuitCmd | cmd = nil, want non-nil |
| AC1_Enter_DoesNotSetDetailPID | detailPID = 42, want 0 |
| AC2_InitialPIDFocus_PositionsCursor | treeCursor = 0, want 1 |
| AC2_InitialPIDFocus_SetsSelectedPID | selectedPID = 1, want 42 |
| AC2_InitialPIDFocus_ClearsAfterApply | initialPIDFocus = 42, want 0 |
| AC3_InitialPIDFocus_NotFound_StatusMsg | statusMsg = "", want "999 not found" |
| AC3_InitialPIDFocus_NotFound_ClearsFlag | initialPIDFocus = 999, want 0 |
| AC4_Enter_DeadProcess_SetsLaunchDashboardPID | launchDashboardPID = 0, want 99 |
| AC4_Enter_DeadProcess_ReturnsQuitCmd | cmd = nil, want non-nil |
| AC6_HelpLine_ContainsDashboard | "[Enter] Details" not "dashboard" |
| AC6_HelpLine_NoDetailsLabel | old "[Enter] Details" still present |

### Passed (4) — Negative validations (correct before implementation)

| Test | Reason |
|------|--------|
| AC3_InitialPIDFocus_NotFound_CursorDefault | treeCursor stays at 0 (no focus logic) |
| AC6_DetailView_HelpLine_NoEnterDashboard | Detail view has no Enter/dashboard text |
| AC7_DetailView_Enter_NoDashboardJump | launchDashboardPID stays 0 in detail view |
| AC7_DetailView_Enter_ReturnsNilCmd | Enter returns nil in detail view |
| AC7_DetailView_StaysInDetail | detailPID preserved in detail view |

## Stub Changes (minimal, for test compilation)

| File | Change |
|------|--------|
| `cmd/rnix/top.go` | Added `launchDashboardPID types.PID` field to `topModel` struct |
| `cmd/rnix/dashboard.go` | Added `initialPIDFocus types.PID` field to `dashboardModel` struct |

## Regression Check

Existing tests verified — no regressions from field additions:
- `TestTopModel_*` — all pass
- `TestATDD_27_3_*` — all pass
- `TestATDD_27_4_*` — all pass

## Implementation Notes for GREEN Phase

1. **Top handleKey**: Change `"enter"` case to set `launchDashboardPID` and return `tea.Quit` (instead of setting `detailPID`)
2. **Dashboard dashboardTick**: Add `initialPIDFocus` processing **before** client reconnect logic so tests with pre-populated treeRows work
3. **Top View**: Update help line from `[Enter] Details` to `[Enter: dashboard | K: kill | q: quit | ↑↓/jk: navigate]`
4. **runTop**: After `p.Run()`, check `launchDashboardPID > 0` and exec dashboard via `syscall.Exec`
5. **runDashboard**: Set `initialPIDFocus` alongside `selectedPID` when `--pid` flag is provided
6. **Existing test update**: `TestTopModel_EnterDetail` will need updating (Enter no longer opens detail view)
