---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation']
lastStep: 'step-02-generation'
lastSaved: '2026-03-22'
storyId: '27-4'
storyTitle: 'Dashboard Prompt View'
detectedStack: 'backend'
inputDocuments:
  - '_bmad-output/implementation-artifacts/27-4-dashboard-prompt-view.md'
  - 'cmd/rnix/dashboard.go'
  - 'cmd/rnix/atdd_27_3_dashboard_timeline_test.go'
  - 'ipc/protocol.go'
---

# ATDD Checklist — Story 27.4: Dashboard Prompt View

## Preflight Summary

| Item | Value |
|------|-------|
| Stack | Go backend (BubbleTea TUI) |
| Test Framework | `go test` with race detection |
| Test File | `cmd/rnix/atdd_27_4_dashboard_prompt_view_test.go` |
| Stubs Added | `cmd/rnix/dashboard.go` — fields, types, empty functions |
| Pattern Reference | `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` |

## Test Coverage Matrix

| AC | Description | Tests | Status |
|----|-------------|-------|--------|
| AC-1 | p key enters Prompt Pager | 8 tests | RED (5 FAIL, 2 FAIL, 1 PASS trivial) |
| AC-2 | Pager scrolling | 5 tests | RED (3 FAIL, 1 SKIP, 1 PASS trivial) |
| AC-3 | q key returns to Dashboard | 5 tests | RED (3 FAIL, 2 PASS trivial) |
| AC-4 | Offline viewing | N/A | Server-side, not dashboard unit testable |
| AC-5 | Prompt content formatting | 10 tests | RED (all 10 FAIL) |
| AC-6 | Cache reuse | 3 tests | RED (1 FAIL, 2 PASS trivial) |
| AC-7 | No step → p key no-op | 2 tests | RED (PASS trivial — guards) |
| Extra | PID change, Window resize, fetchingDetail guard, mode guard | 5 tests | RED (2 FAIL, 3 PASS trivial) |

**Total: 38 tests — 25 FAIL, 1 SKIP, 12 PASS (trivially)**

## RED Phase Results

```
FAIL: TestATDD_27_4_AC1_PKey_EntersPromptPager_CacheHit
FAIL: TestATDD_27_4_AC1_PKey_SetsPromptStep
FAIL: TestATDD_27_4_AC1_PKey_SetsPromptContent
FAIL: TestATDD_27_4_AC1_PKey_CacheMiss_ReturnsCmd
FAIL: TestATDD_27_4_AC1_PKey_CacheMiss_SetsFetchingDetail
FAIL: TestATDD_27_4_AC1_PromptPagerMsg_EntersPager
FAIL: TestATDD_27_4_AC1_PromptPagerMsg_CachesDetail
FAIL: TestATDD_27_4_AC1_PromptPagerMsg_ClearsFetchingDetail
PASS: TestATDD_27_4_AC1_PromptPagerMsg_Error_NoPager (trivial)
SKIP: TestATDD_27_4_AC2_PagerMode_KeysForwardToViewport
PASS: TestATDD_27_4_AC2_PagerMode_KKey_StaysInPager (trivial)
FAIL: TestATDD_27_4_AC3_QKey_ExitsPager
PASS: TestATDD_27_4_AC3_QKey_PreservesStepCursor (trivial)
PASS: TestATDD_27_4_AC3_QKey_PreservesActivePane (trivial)
FAIL: TestATDD_27_4_AC3_QKey_DoesNotQuitDashboard
FAIL: TestATDD_27_4_AC3_EscapeKey_ExitsPager
FAIL: TestATDD_27_4_AC5_FormatPromptContent_SystemPromptSection
FAIL: TestATDD_27_4_AC5_FormatPromptContent_MessagesSection
FAIL: TestATDD_27_4_AC5_FormatPromptContent_ToolsSection
FAIL: TestATDD_27_4_AC5_FormatPromptContent_SectionSeparators
FAIL: TestATDD_27_4_AC5_FormatPromptContent_MessageCount
FAIL: TestATDD_27_4_AC5_FormatPromptContent_ToolCount
FAIL: TestATDD_27_4_AC5_FormatPromptContent_EmptySystemPrompt
FAIL: TestATDD_27_4_AC5_FormatPromptContent_NoTools
FAIL: TestATDD_27_4_AC5_FormatPromptContent_ToolRoleMessage
PASS: TestATDD_27_4_AC6_CacheHit_NoFetchCmd (trivial)
FAIL: TestATDD_27_4_AC6_CacheHit_ImmediatePager
PASS: TestATDD_27_4_AC6_CacheHit_NoFetchingDetailFlag (trivial)
PASS: TestATDD_27_4_AC7_NoSteps_PKey_Noop (trivial)
PASS: TestATDD_27_4_AC7_EmptyStepEntries_PKey_Silent (trivial)
FAIL: TestATDD_27_4_Extra_PIDChange_ExitsPager
FAIL: TestATDD_27_4_Extra_View_PagerMode_OverridesDashboard
FAIL: TestATDD_27_4_AC2_PagerMode_RenderShowsContent
FAIL: TestATDD_27_4_AC2_PagerMode_ShowsHelpBar
FAIL: TestATDD_27_4_AC2_PagerMode_ShowsTitleBar
PASS: TestATDD_27_4_Extra_WindowResize_InPagerMode (trivial)
PASS: TestATDD_27_4_Extra_PKey_WhileFetching_Noop (trivial)
PASS: TestATDD_27_4_Extra_PKey_NotInStepTimelineMode_Noop (trivial)
```

## Stubs Added to dashboard.go

### New type

```go
type promptPagerMsg struct {
    step   int
    detail *ipc.GetStepDetailResponse
    err    error
}
```

### New fields on dashboardModel

```go
// Prompt pager fields (Story 27-4)
promptPager   bool
promptContent string
promptStep    int
```

### Empty function signatures

```go
func formatPromptContent(detail *ipc.GetStepDetailResponse, step int) string
func (m *dashboardModel) enterPromptPager(detail *ipc.GetStepDetailResponse, step int)
func (m dashboardModel) renderPromptPager() string
func fetchStepDetailForPagerCmd(pid types.PID, step int) tea.Cmd
```

## Implementation Guidance

To turn GREEN, the dev should implement in this order:

1. **formatPromptContent** — standalone function, pure logic, 10 tests depend on it
2. **enterPromptPager** — sets fields + calls formatPromptContent
3. **p key handler** in `dashboardKey` — cache hit/miss paths, guards
4. **promptPagerMsg handler** in `Update()` — async completion
5. **q/Escape key interception** at top of `dashboardKey`
6. **renderPromptPager** — viewport + title/help bars
7. **renderDashboard override** — `if m.promptPager { return m.renderPromptPager() }`
8. **PID change cleanup** — add `promptPager = false` in `handleTimelinePIDChange`

## Existing Tests Verification

27-3 ATDD tests: **all PASS** (no regressions)
