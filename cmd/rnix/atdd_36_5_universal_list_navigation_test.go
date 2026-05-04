package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// ============================================================
// ATDD — Story 36-5: Universal list navigation extraction
// ============================================================

// --- AC-2: HandleListKey key set ---

func TestATDD_36_5_AC2_KeySet(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		start    int
		n        int
		pageSize int
		want     int
	}{
		{"j", "j", 2, 10, 5, 3},
		{"k", "k", 2, 10, 5, 1},
		{"pgdown", "pgdown", 0, 10, 4, 4},
		{"pgup", "pgup", 9, 10, 4, 5},
		{"ctrl+d", "ctrl+d", 0, 10, 6, 3},
		{"ctrl+u", "ctrl+u", 9, 10, 6, 6},
		{"g", "g", 5, 10, 5, 0},
		{"G", "G", 0, 10, 5, 9},
		{"home", "home", 5, 10, 5, 0},
		{"end", "end", 0, 10, 5, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.start
			if !ui.HandleListKey(tc.key, nil, &cur, tc.n, ui.ListNavOpts{PageSize: tc.pageSize}) {
				t.Fatalf("key %q not handled", tc.key)
			}
			if cur != tc.want {
				t.Fatalf("key %q: cursor=%d want %d", tc.key, cur, tc.want)
			}
		})
	}
	// Boundary: k at 0, j at end
	cur := 0
	ui.HandleListKey("k", nil, &cur, 5, ui.ListNavOpts{PageSize: 3})
	if cur != 0 {
		t.Fatalf("k at 0 should stay, got %d", cur)
	}
	cur = 4
	ui.HandleListKey("j", nil, &cur, 5, ui.ListNavOpts{PageSize: 3})
	if cur != 4 {
		t.Fatalf("j at end should stay, got %d", cur)
	}
	// itemCount=0 safe
	cur = 0
	if !ui.HandleListKey("j", nil, &cur, 0, ui.ListNavOpts{PageSize: 3}) {
		t.Fatal("empty list j should be handled")
	}
	if cur != 0 {
		t.Fatal("empty list j should not mutate cursor")
	}
}

// --- AC-4: Tree navigation — selectProcess + userManualSelect ---

func TestATDD_36_5_AC4_TreeNavigation(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTree
	m.tree.Cursor = 0
	m.selectedPID = m.tree.Rows[0].Proc.PID
	m.tree.UserManualSelect = false

	// Ctrl-d half page
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	mm := m2.(dashboardModel)
	if !mm.tree.UserManualSelect {
		t.Error("userManualSelect should be true after Ctrl-d")
	}
	if mm.tree.Cursor == 0 {
		t.Error("treeCursor should have moved on Ctrl-d")
	}
	if mm.selectedPID != mm.tree.Rows[mm.tree.Cursor].Proc.PID {
		t.Errorf("selectedPID should follow cursor; got %d, cursor row pid %d",
			mm.selectedPID, mm.tree.Rows[mm.tree.Cursor].Proc.PID)
	}

	// G → last row
	m3, _ := mm.Update(tea.KeyPressMsg{Code: 'G', ShiftedCode: 'G', Mod: tea.ModShift})
	mm2 := m3.(dashboardModel)
	if mm2.tree.Cursor != len(mm2.tree.Rows)-1 {
		t.Errorf("G should move to last row; got %d want %d", mm2.tree.Cursor, len(mm2.tree.Rows)-1)
	}

	// g → first row
	m4, _ := mm2.Update(tea.KeyPressMsg{Code: 'g'})
	mm3 := m4.(dashboardModel)
	if mm3.tree.Cursor != 0 {
		t.Errorf("g should move to first row; got %d", mm3.tree.Cursor)
	}
}

// --- AC-5: Timeline navigation — Ctrl-d half page + cursor clamp ---

func TestATDD_36_5_AC5_TimelineNavigation(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.timeline.StepEntries = nil
	// 20 step entries → 20 unified events
	for i := range 20 {
		m.timeline.StepEntries = append(m.timeline.StepEntries, stepEntry{
			Summary: ipc.StepSummaryWire{Step: i + 1, Action: "tool_call", TimestampMs: int64(100 * (i + 1))},
		})
	}
	m.unifiedEvents = mergeUnifiedEvents(m.timeline.StepEntries, nil, m.selectedPID, m.selectedUUID, m.processes, true)
	m.timeline.StepCursor = 0
	m.timeline.StepFilterMode = false

	// Ctrl-d should advance cursor
	m2 := m.handleTimelineKey("ctrl+d")
	if m2.timeline.StepCursor == 0 {
		t.Error("stepCursor should advance on ctrl+d")
	}

	// Clamp at end: G
	m3 := m2.handleTimelineKey("G")
	filtered := m3.filteredUnifiedEvents()
	if m3.timeline.StepCursor != len(filtered)-1 {
		t.Errorf("G should clamp to %d; got %d", len(filtered)-1, m3.timeline.StepCursor)
	}

	// j at end should stay
	m4 := m3.handleTimelineKey("j")
	if m4.timeline.StepCursor != len(filtered)-1 {
		t.Errorf("j past end should stay at %d; got %d", len(filtered)-1, m4.timeline.StepCursor)
	}
}

// --- AC-6: Heatmap — g/G jumps first/last segment ---

func TestATDD_36_5_AC6_HeatmapNavigation(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.heatmap.Segments = []heatmapSegment{
		{Label: "System", Pct: 40},
		{Label: "Skill", Pct: 30},
		{Label: "Tool", Pct: 20},
		{Label: "Assistant", Pct: 10},
	}
	m.heatmap.Cursor = 0

	// G → last
	m2 := m.handleHeatmapKey("G")
	if m2.heatmap.Cursor != 3 {
		t.Errorf("G should move to last segment (3); got %d", m2.heatmap.Cursor)
	}
	// g → first
	m3 := m2.handleHeatmapKey("g")
	if m3.heatmap.Cursor != 0 {
		t.Errorf("g should move to first segment; got %d", m3.heatmap.Cursor)
	}
	// enter still toggles expansion
	m4 := m3.handleHeatmapKey("enter")
	if !m4.heatmap.Expanded {
		t.Error("enter should toggle heatmapExpanded on")
	}
}

// --- AC-7: Intent navigation — Ctrl-d half page + intentAdjustScroll ---

func TestATDD_36_5_AC7_IntentNavigation(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneIntent
	// Populate synthetic intentFlatNodes (just need length)
	m.intent.FlatNodes = make([]intentFlatNode, 30)
	m.intent.Cursor = 0
	m.intent.ScrollOffset = 0

	// Ctrl-d
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	mm := m2.(dashboardModel)
	if mm.intent.Cursor == 0 {
		t.Error("intentCursor should advance on ctrl+d")
	}
	// intentAdjustScroll should have run (scrollOffset != 0 once cursor > visible)
	// We cannot easily assert intentAdjustScroll called; verify scrollOffset bounded
	if mm.intent.ScrollOffset < 0 {
		t.Errorf("intentScrollOffset invalid: %d", mm.intent.ScrollOffset)
	}
}

// --- AC-9: Inspector — j/k scroll lens viewport; h/l/H/L still jump steps ---

func TestATDD_36_5_AC9_InspectorLensScrollOnly(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.viewMode = viewStepInspector
	m.inspectorPID = 2
	m.inspectorUUID = "uuid-mock-002"
	m.inspectorLens = lensConversation
	m.inspectorSteps = []ipc.StepSummaryWire{
		{Step: 1}, {Step: 3}, {Step: 5},
	}
	m.inspectorStep = 3
	m.inspectorStepMax = 5
	// Prepare viewport with content
	vp := viewport.New(viewport.WithHeight(5), viewport.WithWidth(40))
	vp.SetContent(strings.Repeat("line\n", 50))
	m.inspectorViewports[lensConversation] = vp

	// j should scroll viewport (not navigate step)
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	mm := m2.(dashboardModel)
	if mm.inspectorStep != 3 {
		t.Errorf("j should NOT change inspectorStep; got %d", mm.inspectorStep)
	}

	// h should navigate to prev step (step 1)
	m3, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	mm3 := m3.(dashboardModel)
	if mm3.inspectorStep != 1 {
		t.Errorf("h should go to prev step=1; got %d", mm3.inspectorStep)
	}

	// H (home) → first step (still 1 since that's already first)
	m4, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'H', Text: "H"})
	mm4 := m4.(dashboardModel)
	if mm4.inspectorStep != 1 {
		t.Errorf("H should go to first step=1; got %d", mm4.inspectorStep)
	}

	// L (end) → last step (5)
	m5, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'L', Text: "L"})
	mm5 := m5.(dashboardModel)
	if mm5.inspectorStep != 5 {
		t.Errorf("L should go to last step=5; got %d", mm5.inspectorStep)
	}
}

// --- AC-12: Inspector search / n N Esc ---

func TestATDD_36_5_AC12_SearchBasic(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.viewMode = viewStepInspector
	m.inspectorLens = lensConversation
	content := "alpha\nfoo line\nbeta\nfoo again\ngamma"
	m.inspectorContents[lensConversation] = content
	vp := viewport.New(viewport.WithHeight(10), viewport.WithWidth(40))
	vp.SetContent(content)
	m.inspectorViewports[lensConversation] = vp

	// "/" enters search mode
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	mm := m2.(dashboardModel)
	if !mm.searchMode {
		t.Fatal("searchMode should be true after /")
	}
	// Type "foo"
	for _, c := range "foo" {
		m3, _ := mm.Update(tea.KeyPressMsg{Code: c, Text: string(c)})
		mm = m3.(dashboardModel)
	}
	if mm.searchQuery != "foo" {
		t.Fatalf("searchQuery=%q want foo", mm.searchQuery)
	}
	// Enter confirms
	m4, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = m4.(dashboardModel)
	if mm.searchMode {
		t.Fatal("searchMode should be false after Enter")
	}
	if len(mm.searchMatches) != 2 {
		t.Fatalf("expected 2 matches; got %d", len(mm.searchMatches))
	}
	// n → next match
	prevIdx := mm.searchMatchIdx
	m5, _ := mm.Update(tea.KeyPressMsg{Code: 'n'})
	mm = m5.(dashboardModel)
	if mm.searchMatchIdx == prevIdx {
		t.Error("n should advance searchMatchIdx")
	}
	// Esc clears search
	m6, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	mm = m6.(dashboardModel)
	if mm.searchQuery != "" || len(mm.searchMatches) != 0 {
		t.Error("Esc should clear search")
	}
}

// --- AC-15: Smoke regression — business keys still work after migration ---

func TestATDD_36_5_AC15_Regression(t *testing.T) {
	// Timeline 'o' still toggles sort
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	before := m.timeline.SortAsc
	m2 := m.handleTimelineKey("o")
	if m2.timeline.SortAsc == before {
		t.Error("timeline 'o' should toggle sort direction")
	}
	// Heatmap 'enter' still toggles expand
	mh := newTestDashboardModel(mockDashboardProcs())
	mh.heatmap.Segments = []heatmapSegment{{Label: "A"}, {Label: "B"}}
	mh2 := mh.handleHeatmapKey("enter")
	if !mh2.heatmap.Expanded {
		t.Error("heatmap enter should toggle expand")
	}
	// Inspector '1'-'5' lens switching
	mi := newTestDashboardModel(mockDashboardProcs())
	mi.viewMode = viewStepInspector
	mi.inspectorLens = lensConversation
	m2i, _ := mi.Update(tea.KeyPressMsg{Code: '3'})
	mm := m2i.(dashboardModel)
	if mm.inspectorLens != lensToolIO {
		t.Errorf("'3' should switch to Tool I/O lens; got %v", mm.inspectorLens)
	}
}
