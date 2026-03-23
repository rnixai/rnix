package main

// =============================================================================
// ATDD Story 27.9: Dashboard Distributed Tracing Integration
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: paneTrace constant (=6), Tab cycling % 7
//   AC-2: IPC trace data methods (MethodTraceList, MethodTraceTree, Wire types)
//   AC-3: Trace list rendering (sorted by time desc, truncated TraceID, details)
//   AC-4: Span tree expansion (flattenSpanTree, tree connectors, status coloring)
//   AC-5: Span node linkage (Enter → selectedPID + Timeline, process-gone guard)
//   AC-6: Empty state handling (no traces, IPC error, safe navigation)
//
// Priority: P0 (AC-1,2,3,4,5), P1 (AC-6)
// Test Level: Unit (dashboard model + rendering)

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// --- helpers ---

// newTraceModel creates a dashboardModel configured for trace pane testing.
func newTraceModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneTrace
	return m
}

// makeTraceSummaries creates a set of test trace summaries.
func makeTraceSummaries() []ipc.TraceSummaryWire {
	return []ipc.TraceSummaryWire{
		{
			TraceID:         "a1b2c3d4e5f6g7h8i9j0k1l2",
			SpanCount:       5,
			StartTimeMs:     1700000005000,
			TotalDurationMs: 2300,
			RootSpanName:    "pipeline",
		},
		{
			TraceID:         "f7e8d9c0b1a2z3y4x5w6v7u8",
			SpanCount:       3,
			StartTimeMs:     1700000003000,
			TotalDurationMs: 1100,
			RootSpanName:    "workflow",
		},
		{
			TraceID:         "12345678abcdef01234567ab",
			SpanCount:       8,
			StartTimeMs:     1700000001000,
			TotalDurationMs: 5700,
			RootSpanName:    "compose",
		},
	}
}

// makeSpanTree creates a test SpanTreeWire with a 3-level hierarchy.
func makeSpanTree() *ipc.SpanTreeWire {
	return &ipc.SpanTreeWire{
		TraceID: "a1b2c3d4e5f6g7h8i9j0k1l2",
		Root: &ipc.SpanNodeWire{
			SpanID:     "span-root",
			PID:        1,
			Name:       "pipeline",
			DurationMs: 2300,
			TokensUsed: 500,
			Status:     "ok",
			Children: []ipc.SpanNodeWire{
				{
					SpanID:       "span-2",
					ParentSpanID: "span-root",
					PID:          2,
					Name:         "researcher",
					DurationMs:   1200,
					TokensUsed:   380,
					Status:       "ok",
					Children: []ipc.SpanNodeWire{
						{
							SpanID:       "span-4",
							ParentSpanID: "span-2",
							PID:          4,
							Name:         "sub-task",
							DurationMs:   300,
							TokensUsed:   100,
							Status:       "ok",
						},
					},
				},
				{
					SpanID:       "span-3",
					ParentSpanID: "span-root",
					PID:          3,
					Name:         "writer",
					DurationMs:   800,
					TokensUsed:   400,
					Status:       "error",
				},
			},
		},
		Metadata: ipc.TraceMetaWire{
			TotalSpans:      5,
			TotalTokens:     1280,
			TotalDurationMs: 2300,
			ErrorCount:      1,
		},
	}
}

// =============================================================================
// AC-1: paneTrace constant + Tab cycling
// =============================================================================

// --- AC-1.1: [P0] paneTrace equals 6 ---
func TestATDD_27_9_AC1_PaneTraceConstant(t *testing.T) {
	// RED: paneTrace does not exist yet — will cause compile error
	if paneTrace != 6 {
		t.Errorf("AC-1: paneTrace = %d, want 6", paneTrace)
	}
}

// --- AC-1.2: [P0] Tab cycles through 7 panes ---
func TestATDD_27_9_AC1_TabCycles7Panes(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.activePane = paneTree // 0

	// Press Tab 8 times and verify full cycle back to paneTree (updated for 8 panes after Story 27-10)
	expectedOrder := []paneType{
		paneTimeline, paneHeatmap, paneDetail, paneIntent,
		paneSecurity, paneTrace, paneEval, paneTree,
	}
	for i, expected := range expectedOrder {
		m2, _ := m.Update(tea.KeyPressMsg{Code: '\t'})
		model := m2.(dashboardModel)
		if model.activePane != expected {
			t.Errorf("AC-1: Tab press %d: activePane = %d, want %d", i+1, model.activePane, expected)
		}
		m = model
	}
}

// --- AC-1.3: [P0] Trace pane border highlights when active ---
func TestATDD_27_9_AC1_TracePaneBorderHighlight(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()

	output := m.renderTracePane(60, 20)

	if output == "" {
		t.Fatal("AC-1: renderTracePane returned empty string")
	}
}

// --- AC-1.4: [P1] Status bar shows trace pane help (list mode) ---
func TestATDD_27_9_AC1_StatusBarTraceHelp_ListMode(t *testing.T) {
	m := newTraceModel()
	m.activePane = paneTrace
	m.traceViewMode = 0 // list mode
	// Story 29.2: pane-specific hints shown in viewExpanded mode
	m.viewMode = viewExpanded
	m.expandedPane = paneTrace

	output := m.renderDashboardStatus()

	if !strings.Contains(output, "nav") || !strings.Contains(output, "Enter") {
		t.Error("AC-1: trace pane list mode status help should mention nav and Enter")
	}
}

// --- AC-1.5: [P1] Status bar shows trace pane help (tree mode) ---
func TestATDD_27_9_AC1_StatusBarTraceHelp_TreeMode(t *testing.T) {
	m := newTraceModel()
	m.activePane = paneTrace
	m.traceViewMode = 1 // tree mode
	// Story 29.2: pane-specific hints shown in viewExpanded mode
	m.viewMode = viewExpanded
	m.expandedPane = paneTrace

	output := m.renderDashboardStatus()

	if !strings.Contains(output, "Esc") || !strings.Contains(output, "jump") {
		t.Error("AC-1: trace pane tree mode status help should mention Esc and jump")
	}
}

// =============================================================================
// AC-2: IPC trace data methods
// =============================================================================

// --- AC-2.1: [P0] MethodTraceList and MethodTraceTree constants exist ---
func TestATDD_27_9_AC2_MethodConstants(t *testing.T) {
	// RED: these constants do not exist yet
	if ipc.MethodTraceList != "trace_list" {
		t.Errorf("AC-2: MethodTraceList = %q, want %q", ipc.MethodTraceList, "trace_list")
	}
	if ipc.MethodTraceTree != "trace_tree" {
		t.Errorf("AC-2: MethodTraceTree = %q, want %q", ipc.MethodTraceTree, "trace_tree")
	}
}

// --- AC-2.2: [P0] TraceSummaryWire has required fields ---
func TestATDD_27_9_AC2_TraceSummaryWireFields(t *testing.T) {
	s := ipc.TraceSummaryWire{
		TraceID:         "abc123",
		SpanCount:       5,
		StartTimeMs:     1700000000000,
		TotalDurationMs: 2300,
		RootSpanName:    "pipeline",
	}

	if s.TraceID != "abc123" {
		t.Errorf("AC-2: TraceID = %q, want %q", s.TraceID, "abc123")
	}
	if s.SpanCount != 5 {
		t.Errorf("AC-2: SpanCount = %d, want 5", s.SpanCount)
	}
	if s.StartTimeMs != 1700000000000 {
		t.Errorf("AC-2: StartTimeMs = %d, want 1700000000000", s.StartTimeMs)
	}
	if s.TotalDurationMs != 2300 {
		t.Errorf("AC-2: TotalDurationMs = %d, want 2300", s.TotalDurationMs)
	}
	if s.RootSpanName != "pipeline" {
		t.Errorf("AC-2: RootSpanName = %q, want %q", s.RootSpanName, "pipeline")
	}
}

// --- AC-2.3: [P0] SpanTreeWire has required fields ---
func TestATDD_27_9_AC2_SpanTreeWireFields(t *testing.T) {
	tree := makeSpanTree()

	if tree.TraceID != "a1b2c3d4e5f6g7h8i9j0k1l2" {
		t.Errorf("AC-2: TraceID = %q", tree.TraceID)
	}
	if tree.Root == nil {
		t.Fatal("AC-2: Root should not be nil")
	}
	if tree.Root.SpanID != "span-root" {
		t.Errorf("AC-2: Root.SpanID = %q, want %q", tree.Root.SpanID, "span-root")
	}
	if tree.Metadata.TotalSpans != 5 {
		t.Errorf("AC-2: Metadata.TotalSpans = %d, want 5", tree.Metadata.TotalSpans)
	}
	if tree.Metadata.ErrorCount != 1 {
		t.Errorf("AC-2: Metadata.ErrorCount = %d, want 1", tree.Metadata.ErrorCount)
	}
}

// --- AC-2.4: [P0] SpanNodeWire recursive structure ---
func TestATDD_27_9_AC2_SpanNodeWireRecursive(t *testing.T) {
	tree := makeSpanTree()

	// Root should have 2 children
	if len(tree.Root.Children) != 2 {
		t.Fatalf("AC-2: Root.Children len = %d, want 2", len(tree.Root.Children))
	}

	// First child (researcher) should have 1 grandchild (sub-task)
	researcher := tree.Root.Children[0]
	if researcher.Name != "researcher" {
		t.Errorf("AC-2: first child Name = %q, want %q", researcher.Name, "researcher")
	}
	if len(researcher.Children) != 1 {
		t.Fatalf("AC-2: researcher.Children len = %d, want 1", len(researcher.Children))
	}
	if researcher.Children[0].Name != "sub-task" {
		t.Errorf("AC-2: grandchild Name = %q, want %q", researcher.Children[0].Name, "sub-task")
	}

	// Second child (writer) should have status "error"
	writer := tree.Root.Children[1]
	if writer.Status != "error" {
		t.Errorf("AC-2: writer Status = %q, want %q", writer.Status, "error")
	}
}

// =============================================================================
// AC-3: Trace list rendering
// =============================================================================

// --- AC-3.1: [P0] dashboardModel has trace fields ---
func TestATDD_27_9_AC3_ModelHasTraceFields(t *testing.T) {
	m := newTraceModel()

	// RED: these fields do not exist yet
	if m.traceSummaries != nil {
		t.Error("AC-3: traceSummaries should be nil initially")
	}
	if m.traceErr != nil {
		t.Error("AC-3: traceErr should be nil initially")
	}
	if m.traceCursor != 0 {
		t.Error("AC-3: traceCursor should be 0 initially")
	}
	if m.traceViewMode != 0 {
		t.Error("AC-3: traceViewMode should be 0 (list) initially")
	}
	if m.selectedTraceID != "" {
		t.Error("AC-3: selectedTraceID should be empty initially")
	}
	if m.selectedSpanTree != nil {
		t.Error("AC-3: selectedSpanTree should be nil initially")
	}
}

// --- AC-3.2: [P0] traceListMsg updates model ---
func TestATDD_27_9_AC3_TraceListMsgUpdatesModel(t *testing.T) {
	m := newTraceModel()
	summaries := makeTraceSummaries()

	msg := traceListMsg{
		summaries: summaries,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.traceSummaries) != 3 {
		t.Fatalf("AC-3: traceSummaries len = %d, want 3", len(model.traceSummaries))
	}
	if model.traceErr != nil {
		t.Errorf("AC-3: traceErr should be nil on success, got %v", model.traceErr)
	}
}

// --- AC-3.3: [P0] traceListMsg with error sets traceErr ---
func TestATDD_27_9_AC3_TraceListMsgError(t *testing.T) {
	m := newTraceModel()

	msg := traceListMsg{
		err: fmt.Errorf("connection refused"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.traceErr == nil {
		t.Error("AC-3: traceErr should be set on error")
	}
}

// --- AC-3.4: [P0] Trace list sorted by StartTimeMs descending (newest first) ---
func TestATDD_27_9_AC3_TraceListSortedByTimeDesc(t *testing.T) {
	m := newTraceModel()
	summaries := makeTraceSummaries()

	msg := traceListMsg{summaries: summaries}
	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.traceSummaries) < 2 {
		t.Fatal("AC-3: need at least 2 summaries for sort test")
	}

	// Verify descending order by StartTimeMs
	for i := 1; i < len(model.traceSummaries); i++ {
		if model.traceSummaries[i].StartTimeMs > model.traceSummaries[i-1].StartTimeMs {
			t.Errorf("AC-3: traces not sorted by StartTimeMs desc: [%d]=%d > [%d]=%d",
				i, model.traceSummaries[i].StartTimeMs,
				i-1, model.traceSummaries[i-1].StartTimeMs)
		}
	}

	// The newest trace (StartTimeMs=1700000005000) should be first
	if model.traceSummaries[0].StartTimeMs != 1700000005000 {
		t.Errorf("AC-3: first trace StartTimeMs = %d, want 1700000005000",
			model.traceSummaries[0].StartTimeMs)
	}
}

// --- AC-3.5: [P0] renderTracePane shows trace details ---
func TestATDD_27_9_AC3_RenderTracePane_Details(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	m.traceViewMode = 0 // list mode

	output := m.renderTracePane(80, 30)

	// Should contain truncated TraceID (first 16 chars)
	if !strings.Contains(output, "a1b2c3d4e5f6g7h8") {
		t.Error("AC-3: render should contain truncated TraceID (16 chars)")
	}

	// Should contain root span name
	if !strings.Contains(output, "pipeline") {
		t.Error("AC-3: render should contain root span name 'pipeline'")
	}

	// Should contain span count
	if !strings.Contains(output, "5") {
		t.Error("AC-3: render should contain span count")
	}
}

// --- AC-3.6: [P0] traceCursor clamped after list refresh ---
func TestATDD_27_9_AC3_CursorClampedAfterRefresh(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	m.traceCursor = 2 // at last position

	// Simulate refresh with fewer traces (only 1 trace now)
	msg := traceListMsg{
		summaries: []ipc.TraceSummaryWire{
			{TraceID: "single-trace", SpanCount: 1, StartTimeMs: 1700000000000,
				TotalDurationMs: 500, RootSpanName: "solo"},
		},
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.traceCursor >= len(model.traceSummaries) {
		t.Errorf("AC-3: traceCursor %d out of range (summaries len=%d)",
			model.traceCursor, len(model.traceSummaries))
	}
}

// =============================================================================
// AC-4: Span tree expansion & waterfall
// =============================================================================

// --- AC-4.1: [P0] traceTreeMsg updates model with span tree ---
func TestATDD_27_9_AC4_TraceTreeMsgUpdatesModel(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	tree := makeSpanTree()

	msg := traceTreeMsg{
		traceID: "a1b2c3d4e5f6g7h8i9j0k1l2",
		tree:    tree,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.selectedSpanTree == nil {
		t.Fatal("AC-4: selectedSpanTree should be set after traceTreeMsg")
	}
	if model.selectedTraceID != "a1b2c3d4e5f6g7h8i9j0k1l2" {
		t.Errorf("AC-4: selectedTraceID = %q", model.selectedTraceID)
	}
	if model.traceViewMode != 1 {
		t.Errorf("AC-4: traceViewMode = %d, want 1 (tree)", model.traceViewMode)
	}
	if len(model.spanFlatNodes) == 0 {
		t.Error("AC-4: spanFlatNodes should be populated after traceTreeMsg")
	}
}

// --- AC-4.2: [P0] traceTreeMsg with error sets traceErr ---
func TestATDD_27_9_AC4_TraceTreeMsgError(t *testing.T) {
	m := newTraceModel()

	msg := traceTreeMsg{
		traceID: "some-trace",
		err:     fmt.Errorf("trace not found"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.traceErr == nil {
		t.Error("AC-4: traceErr should be set on error")
	}
	// Should NOT switch to tree mode on error
	if model.traceViewMode != 0 {
		t.Errorf("AC-4: traceViewMode should stay 0 on error, got %d", model.traceViewMode)
	}
}

// --- AC-4.3: [P0] flattenSpanTree produces correct flat list ---
func TestATDD_27_9_AC4_FlattenSpanTree(t *testing.T) {
	tree := makeSpanTree()
	flat := flattenSpanTree(tree)

	// Should have 4 nodes: root + researcher + sub-task + writer
	if len(flat) != 4 {
		t.Fatalf("AC-4: flattenSpanTree len = %d, want 4", len(flat))
	}

	// First node should be root
	if flat[0].name != "pipeline" {
		t.Errorf("AC-4: first node name = %q, want %q", flat[0].name, "pipeline")
	}
	if !flat[0].isRoot {
		t.Error("AC-4: first node should be isRoot=true")
	}
	if flat[0].depth != 0 {
		t.Errorf("AC-4: root depth = %d, want 0", flat[0].depth)
	}

	// Second node should be researcher (depth 1)
	if flat[1].name != "researcher" {
		t.Errorf("AC-4: second node name = %q, want %q", flat[1].name, "researcher")
	}
	if flat[1].depth != 1 {
		t.Errorf("AC-4: researcher depth = %d, want 1", flat[1].depth)
	}

	// Third node should be sub-task (depth 2)
	if flat[2].name != "sub-task" {
		t.Errorf("AC-4: third node name = %q, want %q", flat[2].name, "sub-task")
	}
	if flat[2].depth != 2 {
		t.Errorf("AC-4: sub-task depth = %d, want 2", flat[2].depth)
	}

	// Fourth node should be writer (depth 1)
	if flat[3].name != "writer" {
		t.Errorf("AC-4: fourth node name = %q, want %q", flat[3].name, "writer")
	}
	if flat[3].depth != 1 {
		t.Errorf("AC-4: writer depth = %d, want 1", flat[3].depth)
	}
}

// --- AC-4.4: [P0] flattenSpanTree preserves PID and status ---
func TestATDD_27_9_AC4_FlattenSpanTree_Fields(t *testing.T) {
	tree := makeSpanTree()
	flat := flattenSpanTree(tree)

	if len(flat) < 4 {
		t.Fatal("AC-4: need at least 4 nodes")
	}

	// Root: PID=1, status=ok
	if flat[0].pid != types.PID(1) {
		t.Errorf("AC-4: root PID = %d, want 1", flat[0].pid)
	}
	if flat[0].status != "ok" {
		t.Errorf("AC-4: root status = %q, want %q", flat[0].status, "ok")
	}

	// Writer: PID=3, status=error
	var writerNode *spanFlatNode
	for i := range flat {
		if flat[i].name == "writer" {
			writerNode = &flat[i]
			break
		}
	}
	if writerNode == nil {
		t.Fatal("AC-4: writer node not found")
	}
	if writerNode.pid != types.PID(3) {
		t.Errorf("AC-4: writer PID = %d, want 3", writerNode.pid)
	}
	if writerNode.status != "error" {
		t.Errorf("AC-4: writer status = %q, want %q", writerNode.status, "error")
	}
}

// --- AC-4.5: [P0] flattenSpanTree with nil tree returns nil ---
func TestATDD_27_9_AC4_FlattenSpanTree_NilTree(t *testing.T) {
	flat := flattenSpanTree(nil)
	if flat != nil {
		t.Errorf("AC-4: flattenSpanTree(nil) should return nil, got len=%d", len(flat))
	}
}

// --- AC-4.6: [P0] flattenSpanTree with nil root returns nil ---
func TestATDD_27_9_AC4_FlattenSpanTree_NilRoot(t *testing.T) {
	tree := &ipc.SpanTreeWire{
		TraceID: "empty",
		Root:    nil,
	}
	flat := flattenSpanTree(tree)
	if flat != nil {
		t.Errorf("AC-4: flattenSpanTree with nil Root should return nil, got len=%d", len(flat))
	}
}

// --- AC-4.7: [P0] spanStatusColor returns correct colors ---
func TestATDD_27_9_AC4_SpanStatusColor(t *testing.T) {
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		{"ok", lipgloss.Color("42")},       // green
		{"error", lipgloss.Color("196")},    // red
		{"timeout", lipgloss.Color("208")},  // orange
		{"unknown", lipgloss.Color("240")},  // gray default
	}

	for _, tt := range tests {
		got := spanStatusColor(tt.status)
		if got != tt.want {
			t.Errorf("AC-4: spanStatusColor(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// --- AC-4.8: [P0] Span tree rendering shows tree connectors ---
func TestATDD_27_9_AC4_RenderTracePane_TreeMode(t *testing.T) {
	m := newTraceModel()
	tree := makeSpanTree()
	m.traceViewMode = 1 // tree mode
	m.selectedTraceID = tree.TraceID
	m.selectedSpanTree = tree
	m.spanFlatNodes = flattenSpanTree(tree)

	output := m.renderTracePane(80, 30)

	// Should contain span names
	if !strings.Contains(output, "pipeline") {
		t.Error("AC-4: tree render should contain root span name 'pipeline'")
	}
	if !strings.Contains(output, "researcher") {
		t.Error("AC-4: tree render should contain child span name 'researcher'")
	}
	if !strings.Contains(output, "writer") {
		t.Error("AC-4: tree render should contain child span name 'writer'")
	}

	// Should contain PID values
	if !strings.Contains(output, "PID") || !strings.Contains(output, "1") {
		t.Error("AC-4: tree render should show PID for spans")
	}
}

// --- AC-4.9: [P0] Enter in list mode triggers tree expansion ---
func TestATDD_27_9_AC4_Enter_ListToTree(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	m.traceViewMode = 0 // list mode
	m.traceCursor = 0

	m2, cmd := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	// Should set selectedTraceID from the selected trace
	if model.selectedTraceID != m.traceSummaries[0].TraceID {
		t.Errorf("AC-4: selectedTraceID = %q, want %q",
			model.selectedTraceID, m.traceSummaries[0].TraceID)
	}

	// Should produce a command (fetchTraceTreeCmd)
	if cmd == nil {
		t.Error("AC-4: Enter in list mode should produce a tea.Cmd (fetchTraceTreeCmd)")
	}
}

// --- AC-4.10: [P0] Escape in tree mode returns to list ---
func TestATDD_27_9_AC4_Escape_TreeToList(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	m.traceViewMode = 1 // tree mode
	m.selectedSpanTree = makeSpanTree()
	m.spanFlatNodes = flattenSpanTree(m.selectedSpanTree)

	m2, _ := m.Update(tea.KeyPressMsg{Code: 27}) // Escape
	model := m2.(dashboardModel)

	if model.traceViewMode != 0 {
		t.Errorf("AC-4: after Escape, traceViewMode = %d, want 0 (list)", model.traceViewMode)
	}
}

// =============================================================================
// AC-5: Span node linkage
// =============================================================================

// --- AC-5.1: [P0] Enter on span links to process ---
func TestATDD_27_9_AC5_Enter_LinksToProcess(t *testing.T) {
	m := newTraceModel()
	tree := makeSpanTree()
	m.traceViewMode = 1 // tree mode
	m.selectedSpanTree = tree
	m.spanFlatNodes = flattenSpanTree(tree)
	// Add processes so PID validation passes
	m.processes = []vfs.ProcInfo{{PID: 1}, {PID: 2}, {PID: 3}, {PID: 4}}
	// Select the researcher span (PID=2, index 1)
	m.spanCursor = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	if model.selectedPID != types.PID(2) {
		t.Errorf("AC-5: after Enter, selectedPID = %d, want 2", model.selectedPID)
	}
	if model.activePane != paneTimeline {
		t.Errorf("AC-5: after Enter, activePane = %d, want paneTimeline (%d)",
			model.activePane, paneTimeline)
	}
}

// --- AC-5.2: [P0] Enter on span where process is gone shows status message ---
func TestATDD_27_9_AC5_Enter_ProcessGone_ShowsMessage(t *testing.T) {
	m := newTraceModel()
	tree := makeSpanTree()
	m.traceViewMode = 1 // tree mode
	m.selectedSpanTree = tree
	m.spanFlatNodes = flattenSpanTree(tree)
	// Only PID 1 exists; PID 2 (researcher, index 1) is gone
	m.processes = []vfs.ProcInfo{{PID: 1}}
	m.spanCursor = 1 // researcher span (PID=2)
	prevPID := m.selectedPID

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	// Should NOT change selectedPID
	if model.selectedPID != prevPID {
		t.Errorf("AC-5: Enter on reaped process should not change selectedPID, got %d", model.selectedPID)
	}
	// Should show status message about process not existing
	if model.statusMsg == "" {
		t.Error("AC-5: Enter on reaped process should set statusMsg")
	}
}

// --- AC-5.3: [P0] j/k navigates span tree cursor ---
func TestATDD_27_9_AC5_JK_MovesSpanCursor(t *testing.T) {
	m := newTraceModel()
	tree := makeSpanTree()
	m.traceViewMode = 1
	m.selectedSpanTree = tree
	m.spanFlatNodes = flattenSpanTree(tree)
	m.spanCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.spanCursor != 1 {
		t.Errorf("AC-5: after j, spanCursor = %d, want 1", model.spanCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.spanCursor != 0 {
		t.Errorf("AC-5: after k, spanCursor = %d, want 0", model2.spanCursor)
	}
}

// --- AC-5.4: [P0] j/k does not go out of bounds in span tree ---
func TestATDD_27_9_AC5_SpanCursorBounds(t *testing.T) {
	m := newTraceModel()
	tree := makeSpanTree()
	m.traceViewMode = 1
	m.selectedSpanTree = tree
	m.spanFlatNodes = flattenSpanTree(tree)
	m.spanCursor = 0

	// Press 'k' at cursor=0 → should stay at 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	model := m2.(dashboardModel)
	if model.spanCursor != 0 {
		t.Errorf("AC-5: k at cursor=0 should stay 0, got %d", model.spanCursor)
	}

	// Move cursor to last node
	lastIdx := len(m.spanFlatNodes) - 1
	model.spanCursor = lastIdx

	// Press 'j' at last position → should stay
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	model2 := m3.(dashboardModel)
	if model2.spanCursor != lastIdx {
		t.Errorf("AC-5: j at last position should stay %d, got %d", lastIdx, model2.spanCursor)
	}
}

// --- AC-5.5: [P0] j/k in list mode moves traceCursor ---
func TestATDD_27_9_AC5_JK_MovesTraceCursor(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = makeTraceSummaries()
	m.traceViewMode = 0 // list mode
	m.traceCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.traceCursor != 1 {
		t.Errorf("AC-5: after j (list), traceCursor = %d, want 1", model.traceCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.traceCursor != 0 {
		t.Errorf("AC-5: after k (list), traceCursor = %d, want 0", model2.traceCursor)
	}
}

// =============================================================================
// AC-6: Empty state handling
// =============================================================================

// --- AC-6.1: [P1] No traces shows hint message ---
func TestATDD_27_9_AC6_EmptyState_ShowsHint(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = nil
	m.traceViewMode = 0

	output := m.renderTracePane(80, 20)

	if !strings.Contains(output, "compose") || !strings.Contains(output, "rnix") {
		t.Error("AC-6: empty state should mention 'rnix compose'")
	}
}

// --- AC-6.2: [P1] TraceList IPC error shows error without crash ---
func TestATDD_27_9_AC6_ErrorState_ShowsError(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = nil
	m.traceErr = fmt.Errorf("daemon not reachable")
	m.traceViewMode = 0

	output := m.renderTracePane(80, 20)

	if output == "" {
		t.Fatal("AC-6: renderTracePane with error should not return empty")
	}
}

// --- AC-6.3: [P1] Empty state navigation safe ---
func TestATDD_27_9_AC6_EmptyState_NavigationSafe(t *testing.T) {
	m := newTraceModel()
	m.traceSummaries = nil
	m.spanFlatNodes = nil
	m.traceCursor = 0
	m.spanCursor = 0

	// j/k/Enter/Escape on empty state should not panic
	for _, key := range []rune{'j', 'k', '\r', 27} {
		m2, _ := m.Update(tea.KeyPressMsg{Code: key})
		_ = m2.(dashboardModel)
	}
}

// --- AC-6.4: [P1] Nil selectedSpanTree renders gracefully in tree mode ---
func TestATDD_27_9_AC6_NilSpanTree_Renders(t *testing.T) {
	m := newTraceModel()
	m.traceViewMode = 1 // tree mode but no tree loaded
	m.selectedSpanTree = nil
	m.spanFlatNodes = nil

	output := m.renderTracePane(80, 20)

	if output == "" {
		t.Fatal("AC-6: renderTracePane with nil spanTree in tree mode should not return empty")
	}
}

// =============================================================================
// Scroll offset tests
// =============================================================================

// --- AC-3.7: [P1] traceAdjustScroll keeps cursor visible (list mode) ---
func TestATDD_27_9_TraceAdjustScroll(t *testing.T) {
	m := newTraceModel()
	m.height = 20
	m.traceScrollOffset = 0
	m.traceCursor = 10
	m.traceSummaries = make([]ipc.TraceSummaryWire, 20)

	traceAdjustScroll(&m)

	if m.traceScrollOffset == 0 {
		t.Error("scroll offset should have adjusted for cursor=10")
	}
}

// --- AC-4.11: [P1] spanAdjustScroll keeps cursor visible (tree mode) ---
func TestATDD_27_9_SpanAdjustScroll(t *testing.T) {
	m := newTraceModel()
	m.height = 20
	m.spanScrollOffset = 0
	m.spanCursor = 10
	m.spanFlatNodes = make([]spanFlatNode, 20)

	spanAdjustScroll(&m)

	if m.spanScrollOffset == 0 {
		t.Error("span scroll offset should have adjusted for cursor=10")
	}
}

// =============================================================================
// Cross-pane interaction tests
// =============================================================================

// --- AC-4.12: [P1] Tab away from Trace and back preserves view mode ---
func TestATDD_27_9_TabPreservesViewMode(t *testing.T) {
	m := newTraceModel()
	m.traceViewMode = 1 // tree mode
	m.selectedSpanTree = makeSpanTree()
	m.spanFlatNodes = flattenSpanTree(m.selectedSpanTree)

	// Tab away to next pane
	m2, _ := m.Update(tea.KeyPressMsg{Code: '\t'})
	model := m2.(dashboardModel)

	// Tab back to Trace pane (7 more tabs to cycle around for 8 panes)
	for range 7 {
		m3, _ := model.Update(tea.KeyPressMsg{Code: '\t'})
		model = m3.(dashboardModel)
	}

	if model.activePane != paneTrace {
		t.Errorf("AC-4: after cycling tabs, activePane = %d, want paneTrace (%d)",
			model.activePane, paneTrace)
	}
	// View mode should be preserved (tree mode=1)
	if model.traceViewMode != 1 {
		t.Errorf("AC-4: traceViewMode should be preserved after tab cycle, got %d", model.traceViewMode)
	}
}

// Ensure unused imports are consumed (build guard).
var (
	_ = fmt.Sprintf
	_ = strings.Contains
	_ = lipgloss.Color("")
	_ = types.PID(0)
	_ = ipc.TraceSummaryWire{}
	_ = vfs.ProcInfo{}
)
