package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD — Story 36-2: Agent Tree Sort Fix & New Process Highlight
// ============================================================

// --- AC-1: Time sort logic fix ---

func TestSortTreeNodes_TimeSortDesc_CreatedAtIsPrimary(t *testing.T) {
	// Two siblings with different CreatedAt but same State — descending should
	// place the newer one first regardless of state ranking.
	older := &treeNode{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-10 * time.Second)}}
	newer := &treeNode{proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, CreatedAt: time.Now()}}
	nodes := []*treeNode{older, newer}

	sortTreeNodes(nodes, treeSortTime, false) // desc
	if nodes[0].proc.PID != 2 {
		t.Errorf("expected newest PID=2 first in desc time sort, got PID=%d", nodes[0].proc.PID)
	}
}

func TestSortTreeNodes_TimeSortAsc_OldestFirst(t *testing.T) {
	older := &treeNode{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-10 * time.Second)}}
	newer := &treeNode{proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, CreatedAt: time.Now()}}
	nodes := []*treeNode{newer, older}

	sortTreeNodes(nodes, treeSortTime, true) // asc
	if nodes[0].proc.PID != 1 {
		t.Errorf("expected oldest PID=1 first in asc time sort, got PID=%d", nodes[0].proc.PID)
	}
}

func TestSortTreeNodes_TimeSortDesc_DifferentStateSameTime(t *testing.T) {
	// Same CreatedAt, different State — stateRank is secondary key.
	// In desc mode, cmp is a>b, so higher stateRank sorts first.
	// stateRank: Running=0, Created=1 → desc cmp(0,1)=false → Created before Running.
	ts := time.Now()
	created := &treeNode{proc: vfs.ProcInfo{PID: 1, State: types.StateCreated, CreatedAt: ts}}
	running := &treeNode{proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, CreatedAt: ts}}
	nodes := []*treeNode{created, running}

	sortTreeNodes(nodes, treeSortTime, false) // desc
	if nodes[0].proc.PID == nodes[1].proc.PID {
		t.Fatal("nodes should have different PIDs after sort")
	}
	if nodes[0].proc.State != types.StateCreated {
		t.Errorf("desc sort same-time: expected Created first, got first=%v second=%v",
			nodes[0].proc.State, nodes[1].proc.State)
	}
}

func TestSortTreeNodes_TimeSortDesc_CreatedAtOverridesState(t *testing.T) {
	// Core bug fix test: a newer Created process should sort before an older Running
	// process in time-descending mode, even though Running has a better stateRank.
	olderRunning := &treeNode{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-10 * time.Second)}}
	newerCreated := &treeNode{proc: vfs.ProcInfo{PID: 2, State: types.StateCreated, CreatedAt: time.Now()}}
	nodes := []*treeNode{olderRunning, newerCreated}

	sortTreeNodes(nodes, treeSortTime, false) // desc
	if nodes[0].proc.PID != 2 {
		t.Errorf("time desc sort should put newer PID=2 first regardless of state, got PID=%d", nodes[0].proc.PID)
	}
}

// --- AC-2: Sort direction toggle key ---

func TestTreeSortDirectionToggle_OKey(t *testing.T) {
	m := newDashboardModel(nil)
	m.processes = []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	m.treeRows = []flatRow{{proc: m.processes[0]}}

	// Default is desc (treeSortAsc = false)
	if m.treeSortAsc {
		t.Fatal("default treeSortAsc should be false")
	}

	// Simulate pressing "o" — which now triggers the same code as "S"/"shift+S"
	m.treeSortAsc = !m.treeSortAsc
	if !m.treeSortAsc {
		t.Error("after toggle, treeSortAsc should be true")
	}

	m.treeSortAsc = !m.treeSortAsc
	if m.treeSortAsc {
		t.Error("after second toggle, treeSortAsc should be false again")
	}
}

// --- AC-3: Header direction labels ---

func TestTreeSortDirLabels_TimeDesc(t *testing.T) {
	label := treeSortDirLabels[treeSortTime][0] // desc
	if !strings.Contains(label, "新→旧") {
		t.Errorf("time desc label should be '新→旧', got %q", label)
	}
}

func TestTreeSortDirLabels_TimeAsc(t *testing.T) {
	label := treeSortDirLabels[treeSortTime][1] // asc
	if !strings.Contains(label, "旧→新") {
		t.Errorf("time asc label should be '旧→新', got %q", label)
	}
}

func TestTreeSortDirLabelsASCII_TimeDesc(t *testing.T) {
	label := treeSortDirLabelsASCII[treeSortTime][0]
	if !strings.Contains(label, "新->旧") {
		t.Errorf("time desc ASCII label should be '新->旧', got %q", label)
	}
}

func TestTreeSortDirLabels_PIDDesc(t *testing.T) {
	label := treeSortDirLabels[treeSortPID][0]
	if !strings.Contains(label, "大→小") {
		t.Errorf("PID desc label should be '大→小', got %q", label)
	}
}

func TestHeaderRendering_IncludesDirLabel(t *testing.T) {
	m := newDashboardModel(nil)
	m.treeSortMode = treeSortTime
	m.treeSortAsc = false
	m.processes = []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
	m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)

	rendered := m.renderDashboardTreePane(80, 20)
	if !strings.Contains(rendered, "新→旧") && !strings.Contains(rendered, "新->旧") {
		t.Errorf("tree header should contain direction label, got:\n%s", rendered)
	}
}

func TestHeaderRendering_AscIncludesDirLabel(t *testing.T) {
	m := newDashboardModel(nil)
	m.treeSortMode = treeSortTime
	m.treeSortAsc = true
	m.processes = []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
	m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)

	rendered := m.renderDashboardTreePane(80, 20)
	if !strings.Contains(rendered, "旧→新") && !strings.Contains(rendered, "旧->新") {
		t.Errorf("tree header should contain asc direction label, got:\n%s", rendered)
	}
}

// --- AC-4/AC-5: New process highlight ---

func TestNewProcessHighlight_WithinWindow(t *testing.T) {
	m := newDashboardModel(nil)
	m.processFirstSeenAt = map[types.PID]time.Time{
		1: time.Now(), // just appeared
	}
	m.processes = []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now(), Intent: "test intent"},
	}
	roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
	m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)

	rendered := m.renderDashboardTreePane(80, 20)
	// Should contain sparkle emoji or * (ASCII) depending on mode
	if !strings.Contains(rendered, "✨") && !strings.Contains(rendered, "*") {
		t.Errorf("new process should have highlight prefix, got:\n%s", rendered)
	}
}

func TestNewProcessHighlight_ExpiredWindow(t *testing.T) {
	m := newDashboardModel(nil)
	m.processFirstSeenAt = map[types.PID]time.Time{
		1: time.Now().Add(-2 * time.Second), // 2s ago — past the 1.5s window
	}
	m.processes = []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-2 * time.Second), Intent: "test intent"},
	}
	roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
	m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)

	rendered := m.renderDashboardTreePane(80, 20)
	if strings.Contains(rendered, "✨") {
		t.Errorf("expired highlight should not show sparkle, got:\n%s", rendered)
	}
}

// --- AC-6: Help panel ---

func TestHelpPanel_ContainsTreeSection(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	rendered := m.renderHelpOverlay()
	if !strings.Contains(rendered, "Tree") {
		t.Error("help panel should contain Tree section")
	}
	if !strings.Contains(rendered, "Cycle sort mode") {
		t.Error("help panel should mention 'Cycle sort mode' for s key")
	}
	if !strings.Contains(rendered, "Toggle sort dir") {
		t.Error("help panel should mention 'Toggle sort dir' for o key")
	}
}

// --- AC-7: Regression tests for other sort modes ---

func TestSortTreeNodes_StateSortUnchanged(t *testing.T) {
	running := &treeNode{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now()}}
	dead := &treeNode{proc: vfs.ProcInfo{PID: 2, State: types.StateDead, CreatedAt: time.Now()}}
	nodes := []*treeNode{dead, running}

	sortTreeNodes(nodes, treeSortState, false) // desc
	// stateRank: Running=0, Dead=4. Desc: higher value first → Dead first.
	if nodes[0].proc.State != types.StateDead {
		t.Errorf("state desc sort should put dead first (higher rank value), got %v", nodes[0].proc.State)
	}
}

func TestSortTreeNodes_PIDSortUnchanged(t *testing.T) {
	p1 := &treeNode{proc: vfs.ProcInfo{PID: 1, CreatedAt: time.Now()}}
	p2 := &treeNode{proc: vfs.ProcInfo{PID: 2, CreatedAt: time.Now()}}
	nodes := []*treeNode{p2, p1}

	sortTreeNodes(nodes, treeSortPID, true) // asc
	if nodes[0].proc.PID != 1 {
		t.Errorf("PID asc sort should put PID=1 first, got PID=%d", nodes[0].proc.PID)
	}
}

// --- Highlight color constant ---

func TestColorHighlightDefined(t *testing.T) {
	if ui.ColorHighlight != "#FFFACD" {
		t.Errorf("ColorHighlight should be #FFFACD, got %s", ui.ColorHighlight)
	}
}
