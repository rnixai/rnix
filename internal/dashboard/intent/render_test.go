// Package intent — render_test.go (Story 38-5 PR11 Step 4(c))
//
// 验证 helpers + Render() 行为契约（与 cmd/rnix.renderIntentPane 1:1 等价）：
//   - StateColor / StateIcon / IsTreeTerminal / FlattenTrees /
//     FlattenTreesWithCollapse / PruneCollapse / AdjustScroll 各 helper 行为；
//   - Render() 4 个关键路径：empty / cursor highlight / collapsed indicator /
//     "(分解中...)" empty Nodes prompt。
//
// **Story 38-4 P1 行为契约显式测试**：terminal 树默认折叠 + RootIntent stable
// across reorder + PruneCollapse 清除 stale entries。
package intent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/ipc"
)

// --- Helpers: StateColor ---

func TestStateColor_KnownStates(t *testing.T) {
	cases := map[string]lipgloss.Color{
		"pending":       lipgloss.Color("240"),
		"decomposing":   lipgloss.Color("220"),
		"await_confirm": lipgloss.Color("220"),
		"executing":     lipgloss.Color("39"),
		"completed":     lipgloss.Color("42"),
		"failed":        lipgloss.Color("196"),
		"retrying":      lipgloss.Color("208"),
	}
	for state, want := range cases {
		got := StateColor(state)
		if got != want {
			t.Errorf("StateColor(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestStateColor_UnknownStateFallback(t *testing.T) {
	got := StateColor("unknown-future-state")
	want := lipgloss.Color("240")
	if got != want {
		t.Errorf("StateColor(unknown) = %v, want gray (240) fallback", got)
	}
}

// --- Helpers: StateIcon ---

func TestStateIcon_AllStatesNonEmpty(t *testing.T) {
	for _, state := range []string{"completed", "executing", "pending", "failed", "retrying", "await_confirm", "decomposing", "unknown"} {
		got := StateIcon(state)
		if got == "" {
			t.Errorf("StateIcon(%q) returned empty string", state)
		}
	}
}

// --- Helpers: IsTreeTerminal ---

func TestIsTreeTerminal_TerminalStates(t *testing.T) {
	for _, state := range []string{"completed", "failed"} {
		if !IsTreeTerminal(state) {
			t.Errorf("IsTreeTerminal(%q) = false, want true", state)
		}
	}
}

func TestIsTreeTerminal_NonTerminalStates(t *testing.T) {
	for _, state := range []string{"pending", "decomposing", "executing", "retrying", "await_confirm", ""} {
		if IsTreeTerminal(state) {
			t.Errorf("IsTreeTerminal(%q) = true, want false", state)
		}
	}
}

// --- Helpers: FlattenTrees / FlattenTreesWithCollapse ---

func makeTree(rootIntent, state string, nodes map[string]*ipc.IntentNodeWire) *ipc.IntentTreeWire {
	return &ipc.IntentTreeWire{RootIntent: rootIntent, State: state, Nodes: nodes}
}

func TestFlattenTrees_EmptyInput(t *testing.T) {
	got := FlattenTrees(nil)
	if got != nil {
		t.Errorf("FlattenTrees(nil) = %v, want nil", got)
	}
}

func TestFlattenTrees_SingleTreeHeaderOnly(t *testing.T) {
	tree := makeTree("test-intent", "executing", nil)
	flat := FlattenTrees([]*ipc.IntentTreeWire{tree})
	if len(flat) != 1 {
		t.Fatalf("expected 1 flat node (header only), got %d", len(flat))
	}
	if !flat[0].IsTreeHeader {
		t.Errorf("expected first node to be tree header")
	}
	if flat[0].TreeWire != tree {
		t.Errorf("expected TreeWire to point at original tree")
	}
}

func TestFlattenTrees_NodesIncluded(t *testing.T) {
	nodes := map[string]*ipc.IntentNodeWire{
		"n1": {ID: "n1", Intent: "step 1", State: "completed"},
		"n2": {ID: "n2", Intent: "step 2", State: "executing", DependsOn: []string{"n1"}},
	}
	tree := makeTree("root", "executing", nodes)
	flat := FlattenTrees([]*ipc.IntentTreeWire{tree})
	if len(flat) != 3 {
		t.Fatalf("expected 3 flat nodes (1 header + 2 children), got %d", len(flat))
	}
	if !flat[0].IsTreeHeader {
		t.Errorf("expected first flat to be header")
	}
	// n1 has no deps → indent 0; n2 depends on n1 → indent 1
	var sawN1, sawN2 bool
	for _, n := range flat[1:] {
		switch n.NodeID {
		case "n1":
			sawN1 = true
			if n.Indent != 0 {
				t.Errorf("n1 indent = %d, want 0", n.Indent)
			}
		case "n2":
			sawN2 = true
			if n.Indent != 1 {
				t.Errorf("n2 indent = %d, want 1", n.Indent)
			}
		}
	}
	if !sawN1 || !sawN2 {
		t.Errorf("expected to see both n1 and n2 in flat output")
	}
}

func TestFlattenTrees_TerminalTreeCollapsed(t *testing.T) {
	// Story 38-4: terminal trees stay collapsed (header only, no nodes shown)
	nodes := map[string]*ipc.IntentNodeWire{"n1": {ID: "n1"}}
	tree := makeTree("done", "completed", nodes)
	flat := FlattenTrees([]*ipc.IntentTreeWire{tree})
	if len(flat) != 1 {
		t.Errorf("expected 1 flat node (terminal collapsed), got %d", len(flat))
	}
	if !flat[0].IsCollapsed {
		t.Errorf("expected terminal tree header to be IsCollapsed=true")
	}
}

func TestFlattenTrees_ActiveBeforeTerminalSort(t *testing.T) {
	// AC-2: active trees first, completed/failed at bottom
	t1 := makeTree("done", "completed", nil)
	t2 := makeTree("running", "executing", nil)
	flat := FlattenTrees([]*ipc.IntentTreeWire{t1, t2})
	// Both are header-only (terminal collapsed; running has no nodes anyway)
	if len(flat) != 2 {
		t.Fatalf("expected 2 flat nodes, got %d", len(flat))
	}
	if flat[0].TreeWire.State != "executing" {
		t.Errorf("expected first tree to be active 'executing', got %q", flat[0].TreeWire.State)
	}
	if flat[1].TreeWire.State != "completed" {
		t.Errorf("expected second tree to be terminal 'completed', got %q", flat[1].TreeWire.State)
	}
}

func TestFlattenTreesWithCollapse_UserToggleHidesActiveTree(t *testing.T) {
	// Story 38-4 P1: user-toggled collapse on non-terminal tree
	nodes := map[string]*ipc.IntentNodeWire{"n1": {ID: "n1"}}
	tree := makeTree("active-1", "executing", nodes)
	collapsed := map[string]bool{"active-1": true}
	flat := FlattenTreesWithCollapse([]*ipc.IntentTreeWire{tree}, collapsed)
	if len(flat) != 1 {
		t.Errorf("expected 1 flat node when active tree user-collapsed, got %d", len(flat))
	}
	if !flat[0].IsCollapsed {
		t.Errorf("expected IsCollapsed=true for user-toggled active tree")
	}
}

func TestFlattenTreesWithCollapse_StableRootIntentKey(t *testing.T) {
	// Story 38-4 P1: collapse map keyed by RootIntent (stable across reorder)
	nodes1 := map[string]*ipc.IntentNodeWire{"a": {ID: "a"}}
	nodes2 := map[string]*ipc.IntentNodeWire{"b": {ID: "b"}}
	t1 := makeTree("alpha", "executing", nodes1)
	t2 := makeTree("beta", "executing", nodes2)
	collapsed := map[string]bool{"beta": true}

	// Original order: alpha (visible nodes), beta (collapsed)
	flat := FlattenTreesWithCollapse([]*ipc.IntentTreeWire{t1, t2}, collapsed)
	// alpha header + alpha.a + beta header (collapsed) = 3 entries
	if len(flat) != 3 {
		t.Errorf("expected 3 flat nodes, got %d", len(flat))
	}

	// Reorder input: beta first, alpha second — collapse should still apply to beta only
	flat2 := FlattenTreesWithCollapse([]*ipc.IntentTreeWire{t2, t1}, collapsed)
	if len(flat2) != 3 {
		t.Errorf("expected 3 flat nodes after reorder, got %d", len(flat2))
	}
	// Find beta in flat2; verify still collapsed
	var betaCollapsed bool
	for _, n := range flat2 {
		if n.IsTreeHeader && n.TreeWire.RootIntent == "beta" {
			betaCollapsed = n.IsCollapsed
		}
	}
	if !betaCollapsed {
		t.Errorf("38-4 P1: beta should remain collapsed after reorder (RootIntent stable key)")
	}
}

// --- Helpers: PruneCollapse ---

func TestPruneCollapse_RemovesStaleEntries(t *testing.T) {
	// Story 38-4 P4: PruneCollapse drops keys not in current tree list
	collapsed := map[string]bool{"alive": true, "stale": true, "another-stale": true}
	live := []*ipc.IntentTreeWire{{RootIntent: "alive"}}
	got := PruneCollapse(collapsed, live)
	if _, ok := got["alive"]; !ok {
		t.Errorf("expected 'alive' to remain in map")
	}
	if _, ok := got["stale"]; ok {
		t.Errorf("expected 'stale' to be pruned")
	}
	if _, ok := got["another-stale"]; ok {
		t.Errorf("expected 'another-stale' to be pruned")
	}
}

func TestPruneCollapse_EmptyMapPassthrough(t *testing.T) {
	got := PruneCollapse(nil, nil)
	if got != nil {
		t.Errorf("PruneCollapse(nil, nil) should return nil, got %v", got)
	}
	empty := map[string]bool{}
	got = PruneCollapse(empty, []*ipc.IntentTreeWire{{RootIntent: "x"}})
	if len(got) != 0 {
		t.Errorf("PruneCollapse(empty map) should return empty, got %v", got)
	}
}

// --- Helpers: AdjustScroll ---

func TestAdjustScroll_NilStateNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AdjustScroll(nil) panicked: %v", r)
		}
	}()
	AdjustScroll(nil, 10)
}

func TestAdjustScroll_CursorBeforeOffset(t *testing.T) {
	state := &IntentState{Cursor: 2, ScrollOffset: 5}
	AdjustScroll(state, 10)
	if state.ScrollOffset != 2 {
		t.Errorf("expected ScrollOffset to drop to Cursor (2), got %d", state.ScrollOffset)
	}
}

func TestAdjustScroll_CursorBeyondVisible(t *testing.T) {
	state := &IntentState{Cursor: 12, ScrollOffset: 0}
	AdjustScroll(state, 5)
	// visibleLines=5 → if cursor >= offset+5 → offset = cursor-5+1 = 8
	if state.ScrollOffset != 8 {
		t.Errorf("expected ScrollOffset = 8 (cursor 12 - visible 5 + 1), got %d", state.ScrollOffset)
	}
}

func TestAdjustScroll_ZeroVisibleLinesClamped(t *testing.T) {
	state := &IntentState{Cursor: 5, ScrollOffset: 0}
	AdjustScroll(state, 0)
	// visibleLines clamped to 1 → cursor 5 >= 0+1 → offset = 5
	if state.ScrollOffset != 5 {
		t.Errorf("expected ScrollOffset = 5 with visibleLines clamped to 1, got %d", state.ScrollOffset)
	}
}

// --- Render ---

func TestRender_EmptyFlatNodes(t *testing.T) {
	state := IntentState{}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "Intent Tree") {
		t.Errorf("expected header 'Intent Tree' in empty render, got %q", got)
	}
	if !strings.Contains(got, "No intent decomposition tasks") {
		t.Errorf("expected empty-state prompt in render, got %q", got)
	}
}

func TestRender_SingleTreeHeader(t *testing.T) {
	tree := makeTree("test-intent", "executing", nil)
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{tree}),
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "test-intent") {
		t.Errorf("expected RootIntent in render, got %q", got)
	}
	if !strings.Contains(got, "executing") {
		t.Errorf("expected state in render, got %q", got)
	}
}

func TestRender_EmptyNodesMapShowsDecomposing(t *testing.T) {
	// Fix #8: Empty Nodes map shows "(分解中...)"
	tree := makeTree("planning", "executing", map[string]*ipc.IntentNodeWire{}) // explicit empty
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{tree}),
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "(decomposing...)") {
		t.Errorf("expected '(decomposing...)' for empty Nodes map, got %q", got)
	}
}

func TestRender_NodeRowFormat(t *testing.T) {
	nodes := map[string]*ipc.IntentNodeWire{
		"n1": {ID: "n1", Intent: "do thing", State: "executing", PID: 42},
	}
	tree := makeTree("root", "executing", nodes)
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{tree}),
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "n1") {
		t.Errorf("expected NodeID in render, got %q", got)
	}
	if !strings.Contains(got, "do thing") {
		t.Errorf("expected node intent in render, got %q", got)
	}
	if !strings.Contains(got, "(PID:42)") {
		t.Errorf("expected (PID:42) in render, got %q", got)
	}
}

func TestRender_TerminalTreeCollapsedArrow(t *testing.T) {
	// Story 38-4: terminal tree has ▷ collapsed arrow (not ▶)
	tree := makeTree("done", "completed", map[string]*ipc.IntentNodeWire{"x": {ID: "x"}})
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{tree}),
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "▷") {
		t.Errorf("expected collapsed arrow ▷ for terminal tree, got %q", got)
	}
}

func TestRender_TwoTreesSeparator(t *testing.T) {
	// AC-6: separator "  ───" between trees (skip first)
	t1 := makeTree("first", "executing", nil)
	t2 := makeTree("second", "executing", nil)
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{t1, t2},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{t1, t2}),
	}
	got := Render(state, RenderContext{}, 30)
	if !strings.Contains(got, "───") {
		t.Errorf("expected separator '───' between trees, got %q", got)
	}
}

func TestRender_CursorIndicator(t *testing.T) {
	tree := makeTree("test", "executing", map[string]*ipc.IntentNodeWire{"n1": {ID: "n1", State: "pending"}})
	flat := FlattenTrees([]*ipc.IntentTreeWire{tree})
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: flat,
		Cursor:    1, // point at n1, not the header
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "▸") {
		t.Errorf("expected cursor indicator '▸' at row 1, got %q", got)
	}
}

func TestRender_SmallInnerHClamps(t *testing.T) {
	// innerH < 1 should be clamped (no panic, header always rendered)
	tree := makeTree("test", "executing", nil)
	state := IntentState{
		Trees:     []*ipc.IntentTreeWire{tree},
		FlatNodes: FlattenTrees([]*ipc.IntentTreeWire{tree}),
	}
	got := Render(state, RenderContext{}, 0)
	if !strings.Contains(got, "Intent Tree") {
		t.Errorf("expected 'Intent Tree' header even with innerH=0, got %q", got)
	}
}
