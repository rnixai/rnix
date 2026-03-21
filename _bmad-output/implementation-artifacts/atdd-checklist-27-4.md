---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation']
lastStep: 'step-02-generation'
lastSaved: '2026-03-21'
storyId: '27-4'
storyTitle: 'watch 三级详细度 + prompt 查看'
detectedStack: 'backend'
testFramework: 'go test (stdlib)'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-4-watch-three-level-detail-and-prompt-view.md
  - cmd/rnix/watch.go
  - cmd/rnix/top.go (BubbleTea pattern reference)
  - cmd/rnix/top_test.go (test pattern reference)
  - ipc/atdd_27_2_getstepdetail_test.go (ATDD pattern reference)
  - ipc/protocol.go (ProgressPayload, GetStepDetailResponse, MessageWire, ToolDefWire)
  - ipc/client.go (GetStepDetail, WatchProcess)
---

# ATDD Checklist — Story 27.4: watch 三级详细度 + prompt 查看

## Test File

`cmd/rnix/atdd_27_4_watch_tui_test.go`

## RED Phase Status: ✅ Confirmed

- **Total tests:** 64
- **FAIL:** 56
- **PASS:** 8 (type existence, enum values, zero-value boundary passthrough)
- **Compile:** ✅ `go build` and `go vet` clean

## Stub File Changes

`cmd/rnix/watch.go` — Added minimal BubbleTea type stubs:
- `watchState` enum (Normal, Expanded, Pager)
- `watchStepInfo` struct
- `watchEventMsg`, `watchDoneMsg`, `watchDetailMsg` message types
- `watchModel` struct with all required fields
- `newWatchModel()` stub (returns zero-value)
- `Init()`, `Update()`, `View()` stubs (no-op)
- `formatPromptForPager()` stub (returns nil)

Existing functions preserved: `runWatch`, `readQuitKey`, `renderWatchEvent`, `watchSuccessIcon`, `watchErrorIcon`, `watchFormatDuration`

## AC Coverage Matrix

| AC | Description | Tests | Count |
|----|-------------|-------|-------|
| AC-1 | BubbleTea TUI (tea.Model, state enum, Init, AltScreen) | `AC1_*` | 5 |
| AC-2 | Level 1 步骤列表 (render, cursor j/k/↑/↓, highlight, PID) | `AC2_*` | 9 |
| AC-3 | v 键展开 Level 2 (state transition, RawResponse, ToolInput/Result, tokens, tree line, fetch) | `AC3_*` | 6 |
| AC-4 | V 键展开 Level 3 (L2→L3, MessageCount, TokenCount, first user msg, L3→L2 toggle, Debug separator) | `AC4_*` | 6 |
| AC-5 | 错误步骤自动展开 (HasError → auto Level 2 + fetch) | `AC5_*` | 1 |
| AC-6 | 慢步骤自动展开 (DurationMs>1000 → auto Level 2; fast no-expand) | `AC6_*` | 2 |
| AC-7 | p 键进入 Pager (state transition, SystemPrompt, Messages, Tools, uncached fetch) | `AC7_*` | 5 |
| AC-8 | Pager 交互 (q/Esc back, j/k scroll, g/G top/bottom, bounds, position, help bar) | `AC8_*` | 8 |
| AC-9 | v 键折叠 (Expanded→Normal, Level 3→Normal) | `AC9_*` | 2 |
| AC-10 | q 键退出 (q Normal/Expanded→Quit, Ctrl+C→Quit, q Pager→Normal) | `AC10_*` | 4 |
| AC-11 | 步骤详情缓存 (hit no-fetch, miss triggers fetch, DetailMsg populates, nil no-cache) | `AC11_*` | 4 |
| INT | Integration (step event adds/cursor follows, thinking indicator, complete event, window size) | `INT_*` | 5 |
| Helper | formatPromptForPager (structure, roles, tool defs) | `FormatPromptForPager_*` | 3 |
| Compat | ASCII mode tree line, help bar per state | `ASCIIMode_*`, `HelpBar_*` | 3 |

**Total: 64 tests across 11 ACs + integration + helpers**

## Implementation Notes

- Tests use `newTestWatchModel()` and `newTestWatchModelWithSteps()` helpers
- BubbleTea v2 `tea.KeyPressMsg` used with `.Code` and `.Mod` fields
- `watchEventMsg` wraps `ipc.StreamEvent` for BubbleTea message passing
- `watchDetailMsg` wraps GetStepDetail response for async fetch results
- `formatPromptForPager` is a pure function tested independently
- Tests validate view output via `v.Content` string matching (same pattern as top_test.go)

## Next Step

Implement `watchModel` methods in `watch.go` to turn RED → GREEN:
1. `newWatchModel` — initialize all fields
2. `Init` — start watch stream via channel bridge
3. `Update` — handle KeyPressMsg, watchEventMsg, watchDetailMsg, WindowSizeMsg
4. `View` — render Normal/Expanded/Pager states
5. `formatPromptForPager` — format SystemPrompt + Messages + Tools
