package main

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/ipc"
)

// newAggNavModel builds a fixture whose step count exceeds the aggregation
// threshold (>100), so the Timeline renders in 50-step chunk mode. A single
// SPAWN sys event is appended at the tail to exercise the unified↔step-only
// index-space conversion (spec-timeline-agg-nav-fix Finding 8).
//
// Layout: 120 step events (steps 1-120) + 1 trailing sys event.
//   step-only ordinals: 0..119  → unified indices 0..119
//   sys event          : unified index 120 (no StepEntry)
func newAggNavModel(t *testing.T) dashboardModel {
	t.Helper()
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneTimeline

	const nSteps = 120
	entries := make([]stepEntry, nSteps)
	for i := range entries {
		entries[i] = stepEntry{Summary: ipc.StepSummaryWire{
			Step:     i + 1,
			Action:   "tool_call",
			Summary:  "call",
			ToolPath: "/dev/shell",
		}}
	}
	m.timeline.StepEntries = entries

	now := time.Now()
	for i := range entries {
		m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
			Type:      EventStep,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			PID:       1,
			Summary:   entries[i].Summary.Summary,
			StepEntry: &m.timeline.StepEntries[i],
		})
	}
	// Trailing sys event (no StepEntry) — must not shift step-only ordinals.
	m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
		Type:      EventSpawn,
		Timestamp: now.Add(time.Duration(nSteps) * time.Second),
		PID:       1,
		Summary:   "spawn child",
	})

	m.timeline.StepCursor = 0
	m.timeline.StepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.timeline.ExpandedAggGroups = make(map[int]bool)
	m.timeline.ExpandedChunkGroups = make(map[int]bool)
	return m
}

// --- Index-space conversion helpers (Finding 8) ---

func TestUnifiedCursorToStepOrd_SkipsSysEvents(t *testing.T) {
	filtered := []UnifiedEvent{
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 0
		{Type: EventSpawn},                            // sys — no ord
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 1
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 2
	}
	// cursor on the second StepEntry (unified idx 2) → ordinal 1
	if got := unifiedCursorToStepOrd(filtered, 2); got != 1 {
		t.Errorf("unifiedCursorToStepOrd(2) = %d, want 1", got)
	}
	// cursor on trailing StepEntry (unified idx 3) → ordinal 2
	if got := unifiedCursorToStepOrd(filtered, 3); got != 2 {
		t.Errorf("unifiedCursorToStepOrd(3) = %d, want 2", got)
	}
	// cursor on the sys event (unified idx 1) → projects to preceding step ord 1
	if got := unifiedCursorToStepOrd(filtered, 1); got != 1 {
		t.Errorf("unifiedCursorToStepOrd(1 sys) = %d, want 1", got)
	}
}

func TestStepOrdToUnifiedCursor_RoundTrip(t *testing.T) {
	filtered := []UnifiedEvent{
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 0 → unified 0
		{Type: EventSpawn},                            // sys
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 1 → unified 2
		{Type: EventStep, StepEntry: &stepEntry{}},   // ord 2 → unified 3
	}
	if got := stepOrdToUnifiedCursor(filtered, 1); got != 2 {
		t.Errorf("stepOrdToUnifiedCursor(1) = %d, want 2", got)
	}
	// round-trip: ord → unified → ord
	for ord := range 3 {
		u := stepOrdToUnifiedCursor(filtered, ord)
		if back := unifiedCursorToStepOrd(filtered, u); back != ord {
			t.Errorf("round-trip ord %d → unified %d → ord %d", ord, u, back)
		}
	}
	// empty filtered → 0
	if got := stepOrdToUnifiedCursor(nil, 5); got != 0 {
		t.Errorf("stepOrdToUnifiedCursor(nil) = %d, want 0", got)
	}
}

// --- Collapsed chunk-group navigation (Matrix: 折叠组间 j/k) ---

func TestAggNav_CollapsedJumpsByChunk(t *testing.T) {
	m := newAggNavModel(t)
	// cursor at step ord 0 (chunk group 0).
	m.timeline.StepCursor = 0

	m = m.handleTimelineKey("j")
	// Expect jump to chunk group 1 start = step ord 50 → unified idx 50.
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 50 {
		t.Fatalf("j from group 0 collapsed: step ord = %d, want 50", got)
	}

	m = m.handleTimelineKey("j")
	// group 2 start = step ord 100.
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 100 {
		t.Fatalf("j from group 1 collapsed: step ord = %d, want 100", got)
	}

	// k back to group 1.
	m = m.handleTimelineKey("k")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 50 {
		t.Fatalf("k from group 2 collapsed: step ord = %d, want 50", got)
	}
}

func TestAggNav_CollapsedClampAtEnds(t *testing.T) {
	m := newAggNavModel(t)
	// At group 0, k should clamp at ordinal 0 (no underflow).
	m.timeline.StepCursor = 0
	m = m.handleTimelineKey("k")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 0 {
		t.Fatalf("k at first group: step ord = %d, want 0 (clamp)", got)
	}

	// Jump to last group, j should clamp at last step ordinal (119).
	m.timeline.StepCursor = stepOrdToUnifiedCursor(m.filteredUnifiedEvents(), 100)
	m = m.handleTimelineKey("j")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 119 {
		t.Fatalf("j at last group: step ord = %d, want 119 (clamp)", got)
	}
}

// --- Expanded chunk-group navigation (Matrix: 展开组内 j/k) ---

func TestAggNav_ExpandedMovesByRow(t *testing.T) {
	m := newAggNavModel(t)
	// Expand chunk group 0 (groupIdx key — the fixed namespace).
	m.timeline.ExpandedChunkGroups[0] = true
	m.timeline.StepCursor = 0

	m = m.handleTimelineKey("j")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 1 {
		t.Fatalf("j inside expanded group: step ord = %d, want 1 (row step)", got)
	}
	m = m.handleTimelineKey("j")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 2 {
		t.Fatalf("j inside expanded group: step ord = %d, want 2", got)
	}
	m = m.handleTimelineKey("k")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 1 {
		t.Fatalf("k inside expanded group: step ord = %d, want 1", got)
	}
}

// --- g / G (Matrix: g/G) ---

func TestAggNav_HomeEnd(t *testing.T) {
	m := newAggNavModel(t)
	m.timeline.StepCursor = stepOrdToUnifiedCursor(m.filteredUnifiedEvents(), 60)

	m = m.handleTimelineKey("G")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 119 {
		t.Fatalf("G: step ord = %d, want 119 (last)", got)
	}
	m = m.handleTimelineKey("g")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 0 {
		t.Fatalf("g: step ord = %d, want 0 (first)", got)
	}
	if m.timeline.StepScrollTop != 0 {
		t.Errorf("g should reset StepScrollTop to 0, got %d", m.timeline.StepScrollTop)
	}
}

// --- Enter writes the chunk-group namespace, not the ToolPath namespace ---
// (Matrix: 双键无污染 — regression guard for Finding 7).

func TestAggNav_EnterTogglesChunkNamespace(t *testing.T) {
	m := newAggNavModel(t)
	m.timeline.StepCursor = 0

	m = dispatchEnter(m)

	if !m.timeline.ExpandedChunkGroups[0] {
		t.Errorf("Enter in aggregation mode should set ExpandedChunkGroups[0]=true")
	}
	// The ToolPath step-number namespace must remain untouched.
	if len(m.timeline.ExpandedAggGroups) != 0 {
		t.Errorf("Enter must not write ExpandedAggGroups (ToolPath namespace), got %v", m.timeline.ExpandedAggGroups)
	}
}

// --- Non-navigation keys still fall through in aggregation mode ---

func TestAggNav_NonNavKeyFallsThrough(t *testing.T) {
	m := newAggNavModel(t)
	// 'f' enters filter mode — must not be swallowed by the aggregation guard.
	m = m.handleTimelineKey("f")
	if !m.timeline.StepFilterMode {
		t.Errorf("'f' should enter StepFilterMode even in aggregation mode")
	}
}

// --- Expanded-group boundary is symmetric (regression guard for review 缺陷 2) ---
//
// 展开 group 0（ord 0-49），cursor 在组末 ord 49 按 j 跨入折叠 group 1（ord 50）；
// 此时按 k 必须逐行退回展开组的 ord 49（而非跳回组 0 顶部 ord 0）。修复前 k 因
// 「当前组折叠」误走折叠分支 (1-1)*50=0，跳过 ord 1-49。
func TestAggNav_ExpandedBoundarySymmetric(t *testing.T) {
	m := newAggNavModel(t)
	m.timeline.ExpandedChunkGroups[0] = true
	// cursor at last row of expanded group 0.
	m.timeline.StepCursor = stepOrdToUnifiedCursor(m.filteredUnifiedEvents(), 49)

	m = m.handleTimelineKey("j")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 50 {
		t.Fatalf("j from expanded group tail: step ord = %d, want 50 (next collapsed group start)", got)
	}
	// k must return to ord 49 (the row we left), not jump to group 0 top.
	m = m.handleTimelineKey("k")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 49 {
		t.Fatalf("k back into expanded group: step ord = %d, want 49 (symmetric), not 0", got)
	}
}

// --- pgup/pgdown move by visible units, not group×AggGroupSize (review 缺陷 1) ---
//
// 修复前 pgdown = (group+pageSize)*50，pageSize≈30 → 跳 1500 step 被 clamp 钉到末尾，
// 中间组无法翻页到达。修复后翻页 = 连续前进 pageSize 个可见单元。全折叠视图每组 1 单元，
// pgdown 应前进恰好 pageSize 个组（受 stepCount 上界 clamp）。
func TestAggNav_PageDownMovesByVisibleUnits(t *testing.T) {
	m := newAggNavModel(t)
	m.timeline.StepCursor = 0
	// pageSize = max(dashboardVisibleLines()-4, 1); with height 40 it is well >3,
	// so pgdown over 3 collapsed groups (0→1→2) should land within bounds, not pinned.
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	m = m.handleTimelineKey("pgdown")
	got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor)
	// fixture has 3 groups (0-49, 50-99, 100-119). pageSize ≥ 3 → clamps to last step 119.
	// The key assertion: pgdown actually moves forward (not stuck at 0) and stays in bounds.
	if got <= 0 || got > 119 {
		t.Fatalf("pgdown: step ord = %d, want forward movement within [1,119]", got)
	}
	if pageSize >= 3 && got != 119 {
		t.Fatalf("pgdown with pageSize %d over 3 groups: step ord = %d, want 119 (clamped last)", pageSize, got)
	}
	// pgup back to first.
	m = m.handleTimelineKey("pgup")
	if got := unifiedCursorToStepOrd(m.filteredUnifiedEvents(), m.timeline.StepCursor); got != 0 {
		t.Fatalf("pgup from last: step ord = %d, want 0 (clamped first)", got)
	}
}

// --- Sanity: fixture actually triggers aggregation ---

func TestAggNav_FixtureTriggersAggregation(t *testing.T) {
	m := newAggNavModel(t)
	if got := len(m.filteredStepEntries()); got <= 100 {
		t.Fatalf("fixture step count = %d, want >100 to trigger aggregation", got)
	}
	// AggGroupSize invariant the navigation math depends on.
	if timeline.AggGroupSize != 50 {
		t.Fatalf("AggGroupSize = %d, want 50", timeline.AggGroupSize)
	}
}
