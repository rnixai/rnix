---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-22'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-8-dashboard-security-anomaly-panel.md
  - ipc/protocol.go (ImmuneStatusResponse, AlertWire)
  - cmd/rnix/dashboard.go
  - cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go (pattern reference)
---

# ATDD Checklist: Story 27.8 — Dashboard Security Anomaly Panel

## Step 1: Preflight & Context

- **Detected Stack**: `backend` (Go project, `go.mod` present)
- **Prerequisites**:
  - [x] Story approved with clear acceptance criteria (7 ACs)
  - [x] Test framework: `go test` with race detection (`*_test.go` pattern)
  - [x] Development environment available
- **Story File**: `_bmad-output/implementation-artifacts/27-8-dashboard-security-anomaly-panel.md`
- **Existing Patterns**: `atdd_27_7_dashboard_intent_tree_test.go` (同类 dashboard 窗格 ATDD)
- **TEA Config**: No Playwright/Pact/MCP (pure backend)

## Step 2: Generation Mode

- **Mode**: AI Generation
- **Reason**: Backend project with clear acceptance criteria, standard unit test patterns (Go `testing` package)

## Step 3: Test Strategy

### AC → Test Scenario Mapping

| AC | Test Scenario | Level | Priority | Test Function |
|----|--------------|-------|----------|---------------|
| AC-1 | paneSecurity = 5 | Unit | P0 | `TestATDD_27_8_AC1_PaneSecurityConstant` |
| AC-1 | Tab cycles 6 panes | Unit | P0 | `TestATDD_27_8_AC1_TabCycles6Panes` |
| AC-1 | Security pane border highlight | Unit | P0 | `TestATDD_27_8_AC1_SecurityPaneBorderHighlight` |
| AC-1 | Status bar help text | Unit | P1 | `TestATDD_27_8_AC1_StatusBarSecurityHelp` |
| AC-2 | Model has security fields | Unit | P0 | `TestATDD_27_8_AC2_ModelHasSecurityFields` |
| AC-2 | immuneStatusMsg updates model | Unit | P0 | `TestATDD_27_8_AC2_ImmuneStatusMsgUpdatesModel` |
| AC-2 | immuneStatusMsg error handling | Unit | P0 | `TestATDD_27_8_AC2_ImmuneStatusMsgError` |
| AC-2 | Cursor clamped after refresh | Unit | P0 | `TestATDD_27_8_AC2_CursorClampedAfterRefresh` |
| AC-3 | Alerts sorted by Deviation desc | Unit | P0 | `TestATDD_27_8_AC3_AlertsSortedByDeviation` |
| AC-3 | Alert type color mapping | Unit | P0 | `TestATDD_27_8_AC3_AlertTypeColor` |
| AC-3 | Render shows alert details | Unit | P0 | `TestATDD_27_8_AC3_RenderSecurityPane_AlertDetails` |
| AC-3 | Render with empty alerts | Unit | P0 | `TestATDD_27_8_AC3_RenderSecurityPane_EmptyAlerts` |
| AC-3 | sortAlertsByDeviation helper | Unit | P0 | `TestATDD_27_8_AC3_SortAlertsByDeviation` |
| AC-4 | j/k moves securityCursor | Unit | P0 | `TestATDD_27_8_AC4_JK_MovesSecurityCursor` |
| AC-4 | Enter links to process | Unit | P0 | `TestATDD_27_8_AC4_Enter_LinksToProcess` |
| AC-4 | Enter on reaped process | Unit | P0 | `TestATDD_27_8_AC4_Enter_ProcessGone_ShowsMessage` |
| AC-4 | Cursor bounds check | Unit | P0 | `TestATDD_27_8_AC4_CursorBounds` |
| AC-5 | OK status green message | Unit | P1 | `TestATDD_27_8_AC5_OKStatus_GreenMessage` |
| AC-5 | securityStatusColor mapping | Unit | P0 | `TestATDD_27_8_AC5_SecurityStatusColor` |
| AC-5 | Warning shows alert count | Unit | P1 | `TestATDD_27_8_AC5_WarningStatus_ShowsAlertCount` |
| AC-5 | OK status uptime + threats | Unit | P1 | `TestATDD_27_8_AC5_OKStatus_UptimeAndThreats` |
| AC-5 | formatUptimeShort helper | Unit | P1 | `TestATDD_27_8_AC5_FormatUptimeShort` |
| AC-6 | Suspended PIDs shown | Unit | P1 | `TestATDD_27_8_AC6_SuspendedPIDs_Shown` |
| AC-6 | No suspended section when empty | Unit | P1 | `TestATDD_27_8_AC6_NoSuspendedSection_WhenEmpty` |
| AC-7 | Daemon not running fallback | Unit | P1 | `TestATDD_27_8_AC7_DaemonNotRunning_ShowsFallback` |
| AC-7 | Not running + navigation safe | Unit | P1 | `TestATDD_27_8_AC7_DaemonNotRunning_NavigationSafe` |
| AC-7 | Nil immuneStatus renders | Unit | P1 | `TestATDD_27_8_AC7_NilImmuneStatus_Renders` |

### Priority Distribution

| Priority | Count |
|----------|-------|
| P0 | 16 |
| P1 | 11 |
| **Total** | **27** |

## Step 4: Test Generation (TDD Red Phase)

### Generated Test File

| File | Tests | TDD Phase |
|------|-------|-----------|
| `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` | 27 | RED |

### Functions/Types Referenced (Not Yet Implemented)

These are the production code symbols that the tests reference and will cause **compile errors** until implemented:

| Symbol | Type | AC |
|--------|------|-----|
| `paneSecurity` | const (paneType = 5) | AC-1 |
| `immuneStatus` | field on dashboardModel | AC-2 |
| `immuneErr` | field on dashboardModel | AC-2 |
| `securityAlerts` | field on dashboardModel | AC-2,3,4 |
| `securityCursor` | field on dashboardModel | AC-2,4 |
| `immuneStatusMsg` | struct type | AC-2 |
| `renderSecurityPane()` | method on dashboardModel | AC-1,3,5,6,7 |
| `sortAlertsByDeviation()` | function | AC-3 |
| `alertTypeColor()` | function | AC-3 |
| `securityStatusColor()` | function | AC-5 |
| `formatUptimeShort()` | function | AC-5 |

## Step 5: Validation & Completion

### Validation Checklist

- [x] All 7 ACs covered by at least one test
- [x] Tests will fail (compile error) before implementation — TDD red phase
- [x] No placeholder assertions (`expect(true).toBe(true)` equivalent)
- [x] Realistic test data (actual alert types, deviations, PIDs)
- [x] Edge cases covered: nil immuneStatus, empty alerts, cursor bounds, reaped process
- [x] Pattern matches existing ATDD tests (27-7 as template)
- [x] Test file follows naming convention: `atdd_27_8_*_test.go`

### Key Risks / Assumptions

1. **Tab modulo change**: `% 5` → `% 6` will affect all existing Tab cycling tests (27-7 ATDD test needs update)
2. **AlertWire.PID is uint64**: Tests use `types.PID(alert.PID)` cast pattern
3. **immuneStatus nil guard**: Tests exercise nil immuneStatus path (AC-7.3)
4. **securityCursor overflow**: Tests verify clamp-after-refresh (AC-2.4)

### Next Steps (TDD Green Phase)

1. Implement `paneSecurity`, model fields, `immuneStatusMsg`, render + helper functions in `dashboard.go`
2. Update Tab `% 5` → `% 6`
3. Update Story 27-7 ATDD Tab test from 5→6 panes
4. Run `go test -race ./cmd/rnix/...` → verify all 27 tests PASS
5. Run `make all` for full CI check
