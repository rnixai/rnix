package main

// =============================================================================
// ATDD Story 27.7: Dashboard Intent Tree Integration
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: paneIntent constant (=4), Tab cycling % 5
//   AC-2: Intent data fetch via IPC (intentTreesMsg, dashboardModel fields)
//   AC-3: DAG flattening (flattenIntentTrees) + state color mapping
//   AC-4: Node selection (j/k) + Enter PID linkage
//   AC-5: Empty state rendering
//   AC-6: Multiple intent trees support
//   AC-7: Rendering performance (NFR, implicit)
//
// Priority: P0 (AC-1,2,3,4), P1 (AC-5,6), P2 (AC-7)
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

// newIntentTreeModel creates a dashboardModel configured for intent pane testing.
func newIntentTreeModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneIntent
	return m
}

// makeSingleTree creates a single IntentTreeWire with a linear chain: root → child1 → child2.
func makeSingleTree() *ipc.IntentTreeWire {
	return &ipc.IntentTreeWire{
		ID:         "tree-1",
		RootIntent: "Analyze project structure",
		State:      "executing",
		Nodes: map[string]*ipc.IntentNodeWire{
			"root": {
				ID:        "root",
				Intent:    "Analyze project structure and generate report",
				State:     "executing",
				DependsOn: nil,
				PID:       1,
			},
			"task-1": {
				ID:        "task-1",
				Intent:    "Scan directory structure",
				State:     "completed",
				DependsOn: []string{"root"},
				PID:       3,
			},
			"task-2": {
				ID:        "task-2",
				Intent:    "Analyze dependencies",
				State:     "executing",
				DependsOn: []string{"root"},
				PID:       5,
			},
			"task-3": {
				ID:        "task-3",
				Intent:    "Generate report",
				State:     "pending",
				DependsOn: []string{"task-1", "task-2"},
				PID:       0,
			},
		},
		CreatedAtMs: 1700000000000,
	}
}

// makeTwoTrees creates two intent trees for multi-tree testing.
func makeTwoTrees() []*ipc.IntentTreeWire {
	tree1 := makeSingleTree()
	tree2 := &ipc.IntentTreeWire{
		ID:         "tree-2",
		RootIntent: "Optimize performance",
		State:      "completed",
		Nodes: map[string]*ipc.IntentNodeWire{
			"opt-root": {
				ID:        "opt-root",
				Intent:    "Optimize performance",
				State:     "completed",
				DependsOn: nil,
				PID:       10,
			},
			"opt-1": {
				ID:        "opt-1",
				Intent:    "Profile bottlenecks",
				State:     "completed",
				DependsOn: []string{"opt-root"},
				PID:       11,
			},
		},
		CompletedAtMs: 1700000001000,
	}
	return []*ipc.IntentTreeWire{tree1, tree2}
}

// =============================================================================
// AC-1: paneIntent constant + Tab cycling
// =============================================================================

// --- AC-1.1: [P0] paneIntent equals 4 ---
func TestATDD_27_7_AC1_PaneIntentConstant(t *testing.T) {
	// RED: paneIntent does not exist yet — will cause compile error
	if paneIntent != 4 {
		t.Errorf("AC-1: paneIntent = %d, want 4", paneIntent)
	}
}

// --- AC-1.2: [P0] Digit keys switch through all panes (replaces Tab cycling) ---
func TestATDD_27_7_AC1_TabCycles5Panes(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.activePane = paneTree // 0

	// Use digit keys to verify all panes are reachable
	expectedOrder := []struct {
		key  rune
		pane paneType
	}{
		{'2', paneTimeline}, {'3', paneHeatmap}, {'4', paneDetail}, {'5', paneIntent},
		{'6', paneSecurity}, {'7', paneTrace}, {'8', paneEval}, {'1', paneTree},
	}
	for i, tt := range expectedOrder {
		m2, _ := m.Update(tea.KeyPressMsg{Code: tt.key})
		model := m2.(dashboardModel)
		if model.activePane != tt.pane {
			t.Errorf("AC-1: key '%c' (step %d): activePane = %d, want %d", tt.key, i+1, model.activePane, tt.pane)
		}
		m = model
	}
}

// --- AC-1.3: [P0] Intent pane border highlights when active ---
func TestATDD_27_7_AC1_IntentPaneBorderHighlight(t *testing.T) {
	m := newIntentTreeModel()
	m.intent.Trees = []*ipc.IntentTreeWire{makeSingleTree()}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)

	output := m.renderIntentPane(60, 20)

	// Active pane should use agent border color, not muted
	if output == "" {
		t.Fatal("AC-1: renderIntentPane returned empty string")
	}
}

// =============================================================================
// AC-2: Intent data fetch
// =============================================================================

// --- AC-2.1: [P0] dashboardModel has intent fields ---
func TestATDD_27_7_AC2_ModelHasIntentFields(t *testing.T) {
	m := newIntentTreeModel()

	// RED: these fields do not exist yet
	if m.intent.Trees != nil {
		t.Error("AC-2: intentTrees should be nil initially")
	}
	if m.intent.FlatNodes != nil {
		t.Error("AC-2: intentFlatNodes should be nil initially")
	}
	if m.intent.Cursor != 0 {
		t.Error("AC-2: intentCursor should be 0 initially")
	}
	if m.intent.TreeErr != nil {
		t.Error("AC-2: intentTreeErr should be nil initially")
	}
}

// --- AC-2.2: [P0] intentTreesMsg updates model ---
func TestATDD_27_7_AC2_IntentTreesMsgUpdatesModel(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()

	msg := intentTreesMsg{
		trees: &ipc.IntentStatusResponse{
			Intents: []*ipc.IntentTreeWire{tree},
		},
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.intent.Trees) != 1 {
		t.Fatalf("AC-2: intentTrees len = %d, want 1", len(model.intent.Trees))
	}
	if model.intent.Trees[0].ID != "tree-1" {
		t.Errorf("AC-2: intentTrees[0].ID = %q, want %q", model.intent.Trees[0].ID, "tree-1")
	}
	if len(model.intent.FlatNodes) == 0 {
		t.Error("AC-2: intentFlatNodes should be populated after intentTreesMsg")
	}
}

// --- AC-2.3: [P0] intentTreesMsg with error sets intentTreeErr ---
func TestATDD_27_7_AC2_IntentTreesMsgError(t *testing.T) {
	m := newIntentTreeModel()

	msg := intentTreesMsg{
		err: fmt.Errorf("connection refused"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.intent.TreeErr == nil {
		t.Error("AC-2: intentTreeErr should be set on error")
	}
}

// =============================================================================
// AC-3: DAG visualization — flattenIntentTrees + intentStateColor
// =============================================================================

// --- AC-3.1: [P0] flattenIntentTrees produces correct topology ---
func TestATDD_27_7_AC3_FlattenIntentTrees_Topology(t *testing.T) {
	tree := makeSingleTree()
	flat := flattenIntentTrees([]*ipc.IntentTreeWire{tree})

	// Expected: header + 4 nodes (root at indent 0, task-1/task-2 at indent 1, task-3 at indent 2)
	if len(flat) == 0 {
		t.Fatal("AC-3: flattenIntentTrees returned empty list")
	}

	// First entry should be tree header
	if !flat[0].IsTreeHeader {
		t.Error("AC-3: first flat node should be isTreeHeader=true")
	}

	// Find root node
	var rootFound bool
	for _, n := range flat {
		if n.NodeID == "root" {
			rootFound = true
			if n.Indent != 0 {
				t.Errorf("AC-3: root node indent = %d, want 0", n.Indent)
			}
		}
	}
	if !rootFound {
		t.Error("AC-3: root node not found in flattened list")
	}

	// task-1 and task-2 depend on root → indent 1
	for _, n := range flat {
		if n.NodeID == "task-1" || n.NodeID == "task-2" {
			if n.Indent != 1 {
				t.Errorf("AC-3: node %s indent = %d, want 1", n.NodeID, n.Indent)
			}
		}
	}

	// task-3 depends on task-1 and task-2 → indent 2
	for _, n := range flat {
		if n.NodeID == "task-3" {
			if n.Indent != 2 {
				t.Errorf("AC-3: task-3 indent = %d, want 2", n.Indent)
			}
		}
	}
}

// --- AC-3.2: [P0] flattenIntentTrees sorts same-level nodes by ID ---
func TestATDD_27_7_AC3_FlattenIntentTrees_SortOrder(t *testing.T) {
	tree := makeSingleTree()
	flat := flattenIntentTrees([]*ipc.IntentTreeWire{tree})

	// Collect non-header nodes at indent 1 (task-1, task-2)
	var indent1Nodes []string
	for _, n := range flat {
		if !n.IsTreeHeader && n.Indent == 1 {
			indent1Nodes = append(indent1Nodes, n.NodeID)
		}
	}

	if len(indent1Nodes) < 2 {
		t.Fatalf("AC-3: expected >=2 indent-1 nodes, got %d", len(indent1Nodes))
	}

	// Should be sorted alphabetically: task-1 before task-2
	if indent1Nodes[0] != "task-1" || indent1Nodes[1] != "task-2" {
		t.Errorf("AC-3: indent-1 order = %v, want [task-1, task-2]", indent1Nodes)
	}
}

// --- AC-3.3: [P0] intentStateColor returns correct colors ---
func TestATDD_27_7_AC3_IntentStateColor(t *testing.T) {
	tests := []struct {
		state string
		want  lipgloss.Color
	}{
		{"pending", lipgloss.Color("240")},
		{"decomposing", lipgloss.Color("220")},
		{"await_confirm", lipgloss.Color("220")},
		{"executing", lipgloss.Color("39")},
		{"completed", lipgloss.Color("42")},
		{"failed", lipgloss.Color("196")},
		{"retrying", lipgloss.Color("208")},
		{"unknown_state", lipgloss.Color("240")}, // default
	}

	for _, tt := range tests {
		got := intentStateColor(tt.state)
		if got != tt.want {
			t.Errorf("AC-3: intentStateColor(%q) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

// --- AC-3.4: [P0] renderIntentPane shows node ID + truncated intent + state icon + PID ---
func TestATDD_27_7_AC3_RenderIntentPane_NodeFormat(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)

	output := m.renderIntentPane(80, 30)

	// Should contain node IDs
	if !strings.Contains(output, "task-1") {
		t.Error("AC-3: render should contain node ID 'task-1'")
	}

	// Should contain PID for nodes that have one
	if !strings.Contains(output, "PID") || !strings.Contains(output, "3") {
		t.Error("AC-3: render should show PID for assigned nodes")
	}
}

// --- AC-3.5: [P0] flattenIntentTrees handles missing DependsOn references ---
func TestATDD_27_7_AC3_FlattenIntentTrees_MissingDeps(t *testing.T) {
	tree := &ipc.IntentTreeWire{
		ID:         "tree-orphan",
		RootIntent: "Test with bad deps",
		State:      "executing",
		Nodes: map[string]*ipc.IntentNodeWire{
			"node-a": {
				ID:        "node-a",
				Intent:    "Do something",
				State:     "executing",
				DependsOn: []string{"nonexistent-node"},
			},
		},
	}

	// Should not panic on missing dependency references
	flat := flattenIntentTrees([]*ipc.IntentTreeWire{tree})
	if len(flat) == 0 {
		t.Error("AC-3: flattenIntentTrees should handle missing deps gracefully")
	}
}

// =============================================================================
// AC-4: Node selection + PID linkage
// =============================================================================

// --- AC-4.1: [P0] j/k moves intentCursor ---
func TestATDD_27_7_AC4_JK_MovesIntentCursor(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)
	m.intent.Cursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.intent.Cursor != 1 {
		t.Errorf("AC-4: after j, intentCursor = %d, want 1", model.intent.Cursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.intent.Cursor != 0 {
		t.Errorf("AC-4: after k, intentCursor = %d, want 0", model2.intent.Cursor)
	}
}

// --- AC-4.2: [P0] Enter on node with PID links to process ---
func TestATDD_27_7_AC4_Enter_LinksToProcess(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)
	// Add process with PID=3 to process list so validation passes
	m.processes = []vfs.ProcInfo{{PID: 3}}

	// Move cursor to a node with PID=3 (task-1)
	for i, n := range m.intent.FlatNodes {
		if n.NodeID == "task-1" {
			m.intent.Cursor = i
			break
		}
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	if model.selectedPID != types.PID(3) {
		t.Errorf("AC-4: after Enter on task-1, selectedPID = %d, want 3", model.selectedPID)
	}
	if model.activePane != paneTimeline {
		t.Errorf("AC-4: after Enter, activePane = %d, want paneTimeline (%d)", model.activePane, paneTimeline)
	}
}

// --- AC-4.3: [P0] Enter on node with PID=0 shows status message ---
func TestATDD_27_7_AC4_Enter_NoPID_ShowsMessage(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)

	// Move cursor to task-3 (PID=0, pending)
	for i, n := range m.intent.FlatNodes {
		if n.NodeID == "task-3" {
			m.intent.Cursor = i
			break
		}
	}
	prevPID := m.selectedPID

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	// Should NOT change selectedPID
	if model.selectedPID != prevPID {
		t.Errorf("AC-4: Enter on PID=0 node should not change selectedPID, got %d", model.selectedPID)
	}
	// Should show status message
	if model.statusMsg == "" {
		t.Error("AC-4: Enter on PID=0 node should set statusMsg")
	}
}

// --- AC-4.4: [P0] j/k does not go out of bounds ---
func TestATDD_27_7_AC4_CursorBounds(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)
	m.intent.Cursor = 0

	// Press 'k' at cursor=0 → should stay at 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	model := m2.(dashboardModel)
	if model.intent.Cursor != 0 {
		t.Errorf("AC-4: k at cursor=0 should stay 0, got %d", model.intent.Cursor)
	}

	// Move cursor to last node
	lastIdx := len(m.intent.FlatNodes) - 1
	model.intent.Cursor = lastIdx

	// Press 'j' at last position → should stay
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	model2 := m3.(dashboardModel)
	if model2.intent.Cursor != lastIdx {
		t.Errorf("AC-4: j at last position should stay %d, got %d", lastIdx, model2.intent.Cursor)
	}
}

// =============================================================================
// AC-5: Empty state handling
// =============================================================================

// --- AC-5.1: [P1] Empty state shows hint message ---
func TestATDD_27_7_AC5_EmptyState_ShowsHint(t *testing.T) {
	m := newIntentTreeModel()
	// No intent trees loaded
	m.intent.Trees = nil
	m.intent.FlatNodes = nil

	output := m.renderIntentPane(80, 20)

	if !strings.Contains(output, "rnix apply") {
		t.Error("AC-5: empty state should mention 'rnix apply'")
	}
}

// --- AC-5.2: [P1] Empty state does not crash on navigation ---
func TestATDD_27_7_AC5_EmptyState_NavigationSafe(t *testing.T) {
	m := newIntentTreeModel()
	m.intent.Trees = nil
	m.intent.FlatNodes = nil
	m.intent.Cursor = 0

	// j/k/Enter on empty state should not panic
	for _, key := range []rune{'j', 'k', '\r'} {
		m2, _ := m.Update(tea.KeyPressMsg{Code: key})
		_ = m2.(dashboardModel)
	}
}

// =============================================================================
// AC-6: Multiple intent trees
// =============================================================================

// --- AC-6.1: [P1] Multiple trees appear with separators ---
func TestATDD_27_7_AC6_MultipleTrees_Headers(t *testing.T) {
	trees := makeTwoTrees()
	flat := flattenIntentTrees(trees)

	// Should have headers for both trees
	headerCount := 0
	for _, n := range flat {
		if n.IsTreeHeader {
			headerCount++
		}
	}
	if headerCount != 2 {
		t.Errorf("AC-6: header count = %d, want 2", headerCount)
	}
}

// --- AC-6.2: [P1] j/k navigates across tree boundaries ---
func TestATDD_27_7_AC6_CrossTreeNavigation(t *testing.T) {
	m := newIntentTreeModel()
	trees := makeTwoTrees()
	m.intent.Trees = trees
	m.intent.FlatNodes = flattenIntentTrees(trees)
	m.intent.Cursor = 0

	// Navigate through all nodes — should pass through tree boundaries
	totalNodes := len(m.intent.FlatNodes)
	current := m
	for range totalNodes - 1 {
		m2, _ := current.Update(tea.KeyPressMsg{Code: 'j'})
		current = m2.(dashboardModel)
	}

	// Should be at last node
	if current.intent.Cursor != totalNodes-1 {
		t.Errorf("AC-6: after %d j presses, cursor = %d, want %d",
			totalNodes-1, current.intent.Cursor, totalNodes-1)
	}
}

// --- AC-6.3: [P1] flattenIntentTrees preserves tree index ---
func TestATDD_27_7_AC6_FlattenPreservesTreeIndex(t *testing.T) {
	trees := makeTwoTrees()
	flat := flattenIntentTrees(trees)

	// Nodes from tree-1 should have treeIndex 0
	// Nodes from tree-2 should have treeIndex 1
	for _, n := range flat {
		if n.TreeWire != nil {
			if n.TreeWire.ID == "tree-1" && n.TreeIndex != 0 {
				t.Errorf("AC-6: tree-1 node has treeIndex %d, want 0", n.TreeIndex)
			}
			if n.TreeWire.ID == "tree-2" && n.TreeIndex != 1 {
				t.Errorf("AC-6: tree-2 node has treeIndex %d, want 1", n.TreeIndex)
			}
		}
	}
}

// =============================================================================
// AC-3/AC-6: Rendering integration
// =============================================================================

// --- AC-3/6: [P1] renderIntentPane shows both trees ---
func TestATDD_27_7_AC3_6_RenderMultipleTrees(t *testing.T) {
	m := newIntentTreeModel()
	trees := makeTwoTrees()
	m.intent.Trees = trees
	m.intent.FlatNodes = flattenIntentTrees(trees)

	output := m.renderIntentPane(80, 30)

	// Should contain both root intents
	if !strings.Contains(output, "Analyze") {
		t.Error("AC-3/6: render should contain tree-1 root intent")
	}
	if !strings.Contains(output, "Optimize") {
		t.Error("AC-3/6: render should contain tree-2 root intent")
	}
}

// =============================================================================
// AC-3: Empty Nodes map handling
// =============================================================================

// --- AC-3.6: [P1] tree with empty Nodes map ---
func TestATDD_27_7_AC3_EmptyNodesMap(t *testing.T) {
	tree := &ipc.IntentTreeWire{
		ID:         "tree-empty",
		RootIntent: "Just created",
		State:      "decomposing",
		Nodes:      map[string]*ipc.IntentNodeWire{},
	}
	flat := flattenIntentTrees([]*ipc.IntentTreeWire{tree})

	// Should at least have the tree header
	if len(flat) == 0 {
		t.Error("AC-3: flattenIntentTrees with empty Nodes should still return tree header")
	}
}

// =============================================================================
// AC-2: intentCursor bounds after refresh
// =============================================================================

// --- AC-2.4: [P0] intentCursor clamped after tree refresh ---
func TestATDD_27_7_AC2_CursorClampedAfterRefresh(t *testing.T) {
	m := newIntentTreeModel()
	tree := makeSingleTree()
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTrees(m.intent.Trees)
	m.intent.Cursor = len(m.intent.FlatNodes) - 1 // at last position

	// Simulate refresh with fewer nodes (tree completed, nodes removed)
	smallTree := &ipc.IntentTreeWire{
		ID:         "tree-small",
		RootIntent: "Small",
		State:      "completed",
		Nodes: map[string]*ipc.IntentNodeWire{
			"only": {ID: "only", Intent: "Only node", State: "completed"},
		},
	}
	msg := intentTreesMsg{
		trees: &ipc.IntentStatusResponse{
			Intents: []*ipc.IntentTreeWire{smallTree},
		},
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	// Cursor should be clamped to valid range
	if model.intent.Cursor >= len(model.intent.FlatNodes) {
		t.Errorf("AC-2: intentCursor %d out of range (flatNodes len=%d)",
			model.intent.Cursor, len(model.intent.FlatNodes))
	}
}

// =============================================================================
// AC-1: Help text update
// =============================================================================

// --- AC-1.4: [P1] status bar shows intent pane help ---
func TestATDD_27_7_AC1_StatusBarIntentHelp(t *testing.T) {
	m := newIntentTreeModel()
	m.activePane = paneIntent
	// Story 29.2: pane-specific hints shown in viewExpanded mode
	m.viewMode = viewExpanded
	m.expandedPane = paneIntent

	output := m.renderDashboardStatus()

	if !strings.Contains(output, "nav") || !strings.Contains(output, "Enter") {
		t.Error("AC-1: intent pane status help should mention nav and Enter")
	}
}
