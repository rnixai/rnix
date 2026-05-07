package main

// =============================================================================
// ATDD Story 27.10: Dashboard Multi-Agent Evaluation View
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: paneEval constant (=7), Tab cycling % 8
//   AC-2: Reputation leaderboard rendering (sorted by score, trend coloring)
//   AC-3: Sub-view switching (1/2/3 keys for reputation/topology/synergy)
//   AC-4: Collaboration topology view (nodes, edges, reinforced paths)
//   AC-5: Capability overlap matrix view (combo summaries, recommended)
//   AC-6: Empty state handling (no data, IPC error, safe navigation)
//
// Priority: P0 (AC-1,2,3,4,5), P1 (AC-6)
// Test Level: Unit (dashboard model + rendering)

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// --- helpers ---

// newEvalModel creates a dashboardModel configured for eval pane testing.
func newEvalModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneEval
	return m
}

// makeReputationSummaries creates test reputation data.
func makeReputationSummaries() []kernel.ReputationSummary {
	return []kernel.ReputationSummary{
		{
			AgentName:     "code-reviewer",
			Score:         0.85,
			SuccessRate:   0.85,
			AvgTokens:     5000,
			AvgDurationMs: 30000,
			TotalRecords:  10,
			RecentTrend:   "improving",
		},
		{
			AgentName:     "code-fixer",
			Score:         0.72,
			SuccessRate:   0.70,
			AvgTokens:     6000,
			AvgDurationMs: 35000,
			TotalRecords:  8,
			RecentTrend:   "improving",
		},
		{
			AgentName:     "summarizer",
			Score:         0.60,
			SuccessRate:   0.60,
			AvgTokens:     3000,
			AvgDurationMs: 20000,
			TotalRecords:  5,
			RecentTrend:   "stable",
		},
		{
			AgentName:     "translator",
			Score:         0.45,
			SuccessRate:   0.40,
			AvgTokens:     8000,
			AvgDurationMs: 50000,
			TotalRecords:  3,
			RecentTrend:   "declining",
		},
	}
}

// makeTopologyData creates test collaboration topology data.
func makeTopologyData() *ipc.TopologyQueryResponse {
	return &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "code-reviewer", ReputationScore: 0.85, Connections: 3},
			{Agent: "code-fixer", ReputationScore: 0.72, Connections: 2},
			{Agent: "summarizer", ReputationScore: 0.60, Connections: 1},
		},
		Edges: []kernel.CooperationEdge{
			{From: "code-reviewer", To: "code-fixer", SpawnCount: 8, MsgCount: 5, Total: 13, Reinforced: true},
			{From: "code-reviewer", To: "summarizer", SpawnCount: 3, MsgCount: 2, Total: 5, Reinforced: true},
			{From: "code-fixer", To: "translator", SpawnCount: 1, MsgCount: 1, Total: 2, Reinforced: false},
		},
		ReinforcedPaths: []kernel.CooperationEdge{
			{From: "code-reviewer", To: "code-fixer", SpawnCount: 8, MsgCount: 5, Total: 13, Reinforced: true},
			{From: "code-reviewer", To: "summarizer", SpawnCount: 3, MsgCount: 2, Total: 5, Reinforced: true},
		},
	}
}

// makeSynergyCombos creates test synergy combo summaries.
func makeSynergyCombos() []kernel.ComboSummary {
	return []kernel.ComboSummary{
		{
			ComboKey:         kernel.SynergyComboKey("review,fix"),
			Skills:           []string{"review", "fix"},
			SuccessRate:      0.92,
			AvgTokens:        4500,
			TotalExecutions:  12,
			AvgSoloRate:      0.80,
			TokenImprovement: 0.12,
			Recommended:      true,
		},
		{
			ComboKey:         kernel.SynergyComboKey("review,summarize"),
			Skills:           []string{"review", "summarize"},
			SuccessRate:      0.80,
			AvgTokens:        3000,
			TotalExecutions:  8,
			AvgSoloRate:      0.75,
			TokenImprovement: 0.05,
			Recommended:      true,
		},
		{
			ComboKey:         kernel.SynergyComboKey("fix,translate"),
			Skills:           []string{"fix", "translate"},
			SuccessRate:      0.55,
			AvgTokens:        7000,
			TotalExecutions:  4,
			AvgSoloRate:      0.60,
			TokenImprovement: -0.05,
			Recommended:      false,
		},
	}
}

// =============================================================================
// AC-1: paneEval constant + Tab cycling
// =============================================================================

// --- AC-1.1: [P0] paneEval equals 7 ---
func TestATDD_27_10_AC1_PaneEvalConstant(t *testing.T) {
	// RED: paneEval does not exist yet — will cause compile error
	if paneEval != 7 {
		t.Errorf("AC-1: paneEval = %d, want 7", paneEval)
	}
}

// --- AC-1.2: [P0] Digit keys switch through all panes (replaces Tab cycling) ---
func TestATDD_27_10_AC1_TabCycles8Panes(t *testing.T) {
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

// --- AC-1.3: [P0] Eval pane renders when active ---
func TestATDD_27_10_AC1_EvalPaneRenders(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()

	output := m.renderEvalPane(60, 20)

	if output == "" {
		t.Fatal("AC-1: renderEvalPane returned empty string")
	}
}

// --- AC-1.4: [P1] Status bar shows eval pane help ---
func TestATDD_27_10_AC1_StatusBarEvalHelp(t *testing.T) {
	m := newEvalModel()
	m.activePane = paneEval
	// Story 29.2: pane-specific hints shown in viewExpanded mode
	m.viewMode = viewExpanded
	m.expandedPane = paneEval

	output := m.renderDashboardStatus()

	if !strings.Contains(output, "view") {
		t.Error("AC-1: eval pane status help should mention view")
	}
}

// =============================================================================
// AC-2: Reputation leaderboard rendering
// =============================================================================

// --- AC-2.1: [P0] dashboardModel has eval fields ---
func TestATDD_27_10_AC2_ModelHasEvalFields(t *testing.T) {
	m := newEvalModel()

	// RED: these fields do not exist yet
	if m.eval.SubView != 0 {
		t.Error("AC-2: evalSubView should be 0 (reputation) initially")
	}
	if m.eval.Reputations != nil {
		t.Error("AC-2: evalReputations should be nil initially")
	}
	if m.eval.RepErr != nil {
		t.Error("AC-2: evalRepErr should be nil initially")
	}
	if m.eval.RepCursor != 0 {
		t.Error("AC-2: evalRepCursor should be 0 initially")
	}
	if m.eval.RepScrollOffset != 0 {
		t.Error("AC-2: evalRepScrollOffset should be 0 initially")
	}
}

// --- AC-2.2: [P0] evalReputationMsg updates model ---
func TestATDD_27_10_AC2_ReputationMsgUpdatesModel(t *testing.T) {
	m := newEvalModel()
	summaries := makeReputationSummaries()

	msg := evalReputationMsg{
		Summaries: summaries,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.eval.Reputations) != 4 {
		t.Fatalf("AC-2: evalReputations len = %d, want 4", len(model.eval.Reputations))
	}
	if model.eval.RepErr != nil {
		t.Errorf("AC-2: evalRepErr should be nil on success, got %v", model.eval.RepErr)
	}
}

// --- AC-2.3: [P0] evalReputationMsg with error sets evalRepErr ---
func TestATDD_27_10_AC2_ReputationMsgError(t *testing.T) {
	m := newEvalModel()

	msg := evalReputationMsg{
		Err: fmt.Errorf("connection refused"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.eval.RepErr == nil {
		t.Error("AC-2: evalRepErr should be set on error")
	}
}

// --- AC-2.4: [P0] Reputation table sorted by score descending ---
func TestATDD_27_10_AC2_ReputationSortedByScoreDesc(t *testing.T) {
	m := newEvalModel()
	summaries := makeReputationSummaries()

	msg := evalReputationMsg{Summaries: summaries}
	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.eval.Reputations) < 2 {
		t.Fatal("AC-2: need at least 2 summaries for sort test")
	}

	// Verify descending order by Score
	for i := 1; i < len(model.eval.Reputations); i++ {
		if model.eval.Reputations[i].Score > model.eval.Reputations[i-1].Score {
			t.Errorf("AC-2: reputations not sorted by Score desc: [%d]=%.2f > [%d]=%.2f",
				i, model.eval.Reputations[i].Score,
				i-1, model.eval.Reputations[i-1].Score)
		}
	}

	// Highest score agent should be first
	if model.eval.Reputations[0].AgentName != "code-reviewer" {
		t.Errorf("AC-2: first reputation agent = %q, want %q",
			model.eval.Reputations[0].AgentName, "code-reviewer")
	}
}

// --- AC-2.5: [P0] renderEvalPane shows reputation details ---
func TestATDD_27_10_AC2_RenderEvalPane_ReputationDetails(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()
	m.eval.SubView = 0 // reputation view

	output := m.renderEvalPane(80, 30)

	// Should contain agent name
	if !strings.Contains(output, "code-reviewer") {
		t.Error("AC-2: render should contain agent name 'code-reviewer'")
	}

	// Should contain score
	if !strings.Contains(output, "0.85") {
		t.Error("AC-2: render should contain score '0.85'")
	}

	// Should contain success rate
	if !strings.Contains(output, "85") {
		t.Error("AC-2: render should contain success rate percentage")
	}
}

// --- AC-2.6: [P0] evalRepCursor clamped after refresh ---
func TestATDD_27_10_AC2_CursorClampedAfterRefresh(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()
	m.eval.RepCursor = 3 // at last position

	// Simulate refresh with fewer entries
	msg := evalReputationMsg{
		Summaries: []kernel.ReputationSummary{
			{AgentName: "solo-agent", Score: 0.50, SuccessRate: 0.50,
				AvgTokens: 1000, AvgDurationMs: 10000, TotalRecords: 1, RecentTrend: "stable"},
		},
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.eval.RepCursor >= len(model.eval.Reputations) {
		t.Errorf("AC-2: evalRepCursor %d out of range (evalReputations len=%d)",
			model.eval.RepCursor, len(model.eval.Reputations))
	}
}

// --- AC-2.7: [P0] j/k navigates evalRepCursor ---
func TestATDD_27_10_AC2_JK_MovesRepCursor(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()
	m.eval.SubView = 0
	m.eval.RepCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.eval.RepCursor != 1 {
		t.Errorf("AC-2: after j, evalRepCursor = %d, want 1", model.eval.RepCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.eval.RepCursor != 0 {
		t.Errorf("AC-2: after k, evalRepCursor = %d, want 0", model2.eval.RepCursor)
	}
}

// --- AC-2.8: [P0] j/k bounds checking in reputation view ---
func TestATDD_27_10_AC2_RepCursorBounds(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()
	m.eval.SubView = 0
	m.eval.RepCursor = 0

	// Press 'k' at cursor=0 → should stay at 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	model := m2.(dashboardModel)
	if model.eval.RepCursor != 0 {
		t.Errorf("AC-2: k at cursor=0 should stay 0, got %d", model.eval.RepCursor)
	}

	// Move cursor to last position
	lastIdx := len(m.eval.Reputations) - 1
	model.eval.RepCursor = lastIdx

	// Press 'j' at last position → should stay
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	model2 := m3.(dashboardModel)
	if model2.eval.RepCursor != lastIdx {
		t.Errorf("AC-2: j at last position should stay %d, got %d", lastIdx, model2.eval.RepCursor)
	}
}

// =============================================================================
// AC-3: Sub-view switching (1/2/3 keys)
// =============================================================================

// --- AC-3.1: [P0] Key '!' (Shift+1) selects reputation sub-view ---
func TestATDD_27_10_AC3_Key1_SelectsReputation(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 1 // currently on topology
	// Story 29.2: shifted digit keys !/@ /# pass to eval only in viewExpanded + paneEval
	m.viewMode = viewExpanded
	m.expandedPane = paneEval

	m2, _ := m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	model := m2.(dashboardModel)

	if model.eval.SubView != 0 {
		t.Errorf("AC-3: after pressing '!', evalSubView = %d, want 0 (reputation)", model.eval.SubView)
	}
}

// --- AC-3.2: [P0] Key '@' (Shift+2) selects topology sub-view ---
func TestATDD_27_10_AC3_Key2_SelectsTopology(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 0 // currently on reputation
	// Story 29.2: shifted digit keys !/@ /# pass to eval only in viewExpanded + paneEval
	m.viewMode = viewExpanded
	m.expandedPane = paneEval

	m2, _ := m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	model := m2.(dashboardModel)

	if model.eval.SubView != 1 {
		t.Errorf("AC-3: after pressing '@', evalSubView = %d, want 1 (topology)", model.eval.SubView)
	}
}

// --- AC-3.3: [P0] Key '#' (Shift+3) selects synergy sub-view ---
func TestATDD_27_10_AC3_Key3_SelectsSynergy(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 0 // currently on reputation
	// Story 29.2: shifted digit keys !/@ /# pass to eval only in viewExpanded + paneEval
	m.viewMode = viewExpanded
	m.expandedPane = paneEval

	m2, _ := m.Update(tea.KeyPressMsg{Code: '#', Text: "#"})
	model := m2.(dashboardModel)

	if model.eval.SubView != 2 {
		t.Errorf("AC-3: after pressing '#', evalSubView = %d, want 2 (synergy)", model.eval.SubView)
	}
}

// --- AC-3.4: [P0] digit keys in non-eval pane jump to pane, not eval sub-view ---
func TestATDD_27_10_AC3_SubViewKeysOnlyInEvalPane(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.activePane = paneTree // not eval pane

	// Pressing '2' in tree pane should NOT set evalSubView — it should jump to pane
	m2, _ := m.Update(tea.KeyPressMsg{Code: '2'})
	model := m2.(dashboardModel)

	if model.eval.SubView != 0 {
		t.Errorf("AC-3: pressing '2' in paneTree should not change evalSubView, got %d", model.eval.SubView)
	}
}

// --- AC-3.5: [P0] Sub-view state preserved after Tab cycle ---
func TestATDD_27_10_AC3_SubViewPreservedAfterTabCycle(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 2 // synergy sub-view

	// Switch to another pane via digit key
	m2, _ := m.Update(tea.KeyPressMsg{Code: '2'}) // Timeline
	model := m2.(dashboardModel)

	// Switch back to Eval pane via digit key
	m3, _ := model.Update(tea.KeyPressMsg{Code: '8'}) // Eval
	model = m3.(dashboardModel)

	if model.activePane != paneEval {
		t.Errorf("AC-3: after switching back, activePane = %d, want paneEval (%d)",
			model.activePane, paneEval)
	}
	// Sub-view should be preserved
	if model.eval.SubView != 2 {
		t.Errorf("AC-3: evalSubView should be preserved after pane switch, got %d", model.eval.SubView)
	}
}

// =============================================================================
// AC-4: Collaboration topology view
// =============================================================================

// --- AC-4.1: [P0] evalTopologyMsg updates model ---
func TestATDD_27_10_AC4_TopologyMsgUpdatesModel(t *testing.T) {
	m := newEvalModel()
	topo := makeTopologyData()

	msg := evalTopologyMsg{
		Topology: topo,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.eval.Topology == nil {
		t.Fatal("AC-4: evalTopology should be set after evalTopologyMsg")
	}
	if len(model.eval.Topology.Nodes) != 3 {
		t.Errorf("AC-4: evalTopology.Nodes len = %d, want 3", len(model.eval.Topology.Nodes))
	}
	if len(model.eval.Topology.Edges) != 3 {
		t.Errorf("AC-4: evalTopology.Edges len = %d, want 3", len(model.eval.Topology.Edges))
	}
}

// --- AC-4.2: [P0] evalTopologyMsg with error sets evalTopoErr ---
func TestATDD_27_10_AC4_TopologyMsgError(t *testing.T) {
	m := newEvalModel()

	msg := evalTopologyMsg{
		Err: fmt.Errorf("topology service unavailable"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.eval.TopoErr == nil {
		t.Error("AC-4: evalTopoErr should be set on error")
	}
}

// --- AC-4.3: [P0] Topology view renders nodes ---
func TestATDD_27_10_AC4_RenderTopology_Nodes(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 1 // topology
	m.eval.Topology = makeTopologyData()

	output := m.renderEvalPane(80, 30)

	// Should contain node agent names
	if !strings.Contains(output, "code-reviewer") {
		t.Error("AC-4: topology render should contain node 'code-reviewer'")
	}
	if !strings.Contains(output, "code-fixer") {
		t.Error("AC-4: topology render should contain node 'code-fixer'")
	}
}

// --- AC-4.4: [P0] Topology view renders edges ---
func TestATDD_27_10_AC4_RenderTopology_Edges(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 1
	m.eval.Topology = makeTopologyData()

	output := m.renderEvalPane(80, 30)

	// Should contain edge relationship
	if !strings.Contains(output, "code-reviewer") || !strings.Contains(output, "code-fixer") {
		t.Error("AC-4: topology render should show edge from code-reviewer to code-fixer")
	}

	// Should contain spawn count for the main edge
	if !strings.Contains(output, "13") {
		t.Error("AC-4: topology render should show total count 13 for reviewer→fixer edge")
	}
}

// --- AC-4.5: [P0] j/k navigates topology cursor ---
func TestATDD_27_10_AC4_JK_MovesTopoCursor(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 1
	m.eval.Topology = makeTopologyData()
	m.eval.TopoCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.eval.TopoCursor != 1 {
		t.Errorf("AC-4: after j, evalTopoCursor = %d, want 1", model.eval.TopoCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.eval.TopoCursor != 0 {
		t.Errorf("AC-4: after k, evalTopoCursor = %d, want 0", model2.eval.TopoCursor)
	}
}

// =============================================================================
// AC-5: Capability overlap matrix view
// =============================================================================

// --- AC-5.1: [P0] evalSynergyMsg updates model ---
func TestATDD_27_10_AC5_SynergyMsgUpdatesModel(t *testing.T) {
	m := newEvalModel()
	combos := makeSynergyCombos()

	msg := evalSynergyMsg{
		Combos: combos,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.eval.Synergies) != 3 {
		t.Fatalf("AC-5: evalSynergies len = %d, want 3", len(model.eval.Synergies))
	}
	if model.eval.SynErr != nil {
		t.Errorf("AC-5: evalSynErr should be nil on success, got %v", model.eval.SynErr)
	}
}

// --- AC-5.2: [P0] evalSynergyMsg with error sets evalSynErr ---
func TestATDD_27_10_AC5_SynergyMsgError(t *testing.T) {
	m := newEvalModel()

	msg := evalSynergyMsg{
		Err: fmt.Errorf("synergy data unavailable"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.eval.SynErr == nil {
		t.Error("AC-5: evalSynErr should be set on error")
	}
}

// --- AC-5.3: [P0] Synergy matrix renders combo summaries ---
func TestATDD_27_10_AC5_RenderSynergy_Combos(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 2 // synergy
	m.eval.Synergies = makeSynergyCombos()

	output := m.renderEvalPane(80, 30)

	// Should contain skill combo
	if !strings.Contains(output, "review") || !strings.Contains(output, "fix") {
		t.Error("AC-5: synergy render should contain skill combo 'review,fix'")
	}

	// Should contain success rate
	if !strings.Contains(output, "92") {
		t.Error("AC-5: synergy render should contain success rate '92'")
	}

	// Should contain execution count
	if !strings.Contains(output, "12") {
		t.Error("AC-5: synergy render should contain execution count '12'")
	}
}

// --- AC-5.4: [P0] j/k navigates synergy cursor ---
func TestATDD_27_10_AC5_JK_MovesSynCursor(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 2
	m.eval.Synergies = makeSynergyCombos()
	m.eval.SynCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.eval.SynCursor != 1 {
		t.Errorf("AC-5: after j, evalSynCursor = %d, want 1", model.eval.SynCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.eval.SynCursor != 0 {
		t.Errorf("AC-5: after k, evalSynCursor = %d, want 0", model2.eval.SynCursor)
	}
}

// --- AC-5.5: [P0] synergy cursor bounds checking ---
func TestATDD_27_10_AC5_SynCursorBounds(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 2
	m.eval.Synergies = makeSynergyCombos()
	m.eval.SynCursor = 0

	// Press 'k' at cursor=0 → should stay at 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	model := m2.(dashboardModel)
	if model.eval.SynCursor != 0 {
		t.Errorf("AC-5: k at cursor=0 should stay 0, got %d", model.eval.SynCursor)
	}

	// Move cursor to last position
	lastIdx := len(m.eval.Synergies) - 1
	model.eval.SynCursor = lastIdx

	// Press 'j' at last position → should stay
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	model2 := m3.(dashboardModel)
	if model2.eval.SynCursor != lastIdx {
		t.Errorf("AC-5: j at last position should stay %d, got %d", lastIdx, model2.eval.SynCursor)
	}
}

// =============================================================================
// AC-6: Empty state handling
// =============================================================================

// --- AC-6.1: [P1] No reputation data shows hint ---
func TestATDD_27_10_AC6_EmptyReputation_ShowsHint(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = nil
	m.eval.SubView = 0

	output := m.renderEvalPane(80, 20)

	if !strings.Contains(output, "rnix spawn") && !strings.Contains(output, "compose") {
		t.Error("AC-6: empty reputation state should mention 'rnix spawn' or 'compose'")
	}
}

// --- AC-6.2: [P1] Empty reputation with loaded but zero entries ---
func TestATDD_27_10_AC6_EmptyReputationLoaded_ShowsHint(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = []kernel.ReputationSummary{} // loaded but empty
	m.eval.SubView = 0

	output := m.renderEvalPane(80, 20)

	if !strings.Contains(output, "rnix spawn") && !strings.Contains(output, "compose") {
		t.Error("AC-6: empty loaded reputation should show hint")
	}
}

// --- AC-6.3: [P1] No topology data shows hint ---
func TestATDD_27_10_AC6_EmptyTopology_ShowsHint(t *testing.T) {
	m := newEvalModel()
	m.eval.Topology = nil
	m.eval.SubView = 1

	output := m.renderEvalPane(80, 20)

	if !strings.Contains(output, "协作拓扑") && !strings.Contains(output, "编排") {
		t.Error("AC-6: empty topology state should mention collaboration topology or orchestration")
	}
}

// --- AC-6.4: [P1] No synergy data shows hint ---
func TestATDD_27_10_AC6_EmptySynergy_ShowsHint(t *testing.T) {
	m := newEvalModel()
	m.eval.Synergies = nil
	m.eval.SubView = 2

	output := m.renderEvalPane(80, 20)

	if !strings.Contains(output, "Skill") || !strings.Contains(output, "Agent") {
		t.Error("AC-6: empty synergy state should mention skills or agents")
	}
}

// --- AC-6.5: [P1] IPC error shows error without crash ---
func TestATDD_27_10_AC6_IPCError_ShowsError(t *testing.T) {
	m := newEvalModel()
	m.eval.RepErr = fmt.Errorf("daemon not reachable")
	m.eval.SubView = 0

	output := m.renderEvalPane(80, 20)

	if output == "" {
		t.Fatal("AC-6: renderEvalPane with error should not return empty")
	}
}

// --- AC-6.6: [P1] Empty state navigation safe ---
func TestATDD_27_10_AC6_EmptyState_NavigationSafe(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = nil
	m.eval.Topology = nil
	m.eval.Synergies = nil
	m.eval.RepCursor = 0
	m.eval.TopoCursor = 0
	m.eval.SynCursor = 0

	// j/k/1/2/3 on empty state should not panic
	for _, key := range []rune{'j', 'k', '1', '2', '3'} {
		m2, _ := m.Update(tea.KeyPressMsg{Code: key})
		_ = m2.(dashboardModel)
	}
}

// --- AC-6.7: [P1] Topology error state renders gracefully ---
func TestATDD_27_10_AC6_TopologyError_Renders(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 1
	m.eval.Topology = nil
	m.eval.TopoErr = fmt.Errorf("topology unavailable")

	output := m.renderEvalPane(80, 20)

	if output == "" {
		t.Fatal("AC-6: renderEvalPane with topology error should not return empty")
	}
}

// --- AC-6.8: [P1] Synergy error state renders gracefully ---
func TestATDD_27_10_AC6_SynergyError_Renders(t *testing.T) {
	m := newEvalModel()
	m.eval.SubView = 2
	m.eval.Synergies = nil
	m.eval.SynErr = fmt.Errorf("synergy unavailable")

	output := m.renderEvalPane(80, 20)

	if output == "" {
		t.Fatal("AC-6: renderEvalPane with synergy error should not return empty")
	}
}

// =============================================================================
// Scroll offset tests
// =============================================================================

// --- AC-2.9: [P1] evalRepAdjustScroll keeps cursor visible ---
func TestATDD_27_10_RepAdjustScroll(t *testing.T) {
	m := newEvalModel()
	m.height = 20
	m.eval.RepScrollOffset = 0
	m.eval.RepCursor = 10
	m.eval.Reputations = make([]kernel.ReputationSummary, 20)

	evalRepAdjustScroll(&m)

	if m.eval.RepScrollOffset == 0 {
		t.Error("scroll offset should have adjusted for cursor=10")
	}
}

// --- AC-4.6: [P1] evalTopoAdjustScroll keeps cursor visible ---
func TestATDD_27_10_TopoAdjustScroll(t *testing.T) {
	m := newEvalModel()
	m.height = 20
	m.eval.TopoScrollOffset = 0
	m.eval.TopoCursor = 10
	m.eval.Topology = &ipc.TopologyQueryResponse{
		Nodes: make([]kernel.TopologyNode, 5),
		Edges: make([]kernel.CooperationEdge, 15),
	}

	evalTopoAdjustScroll(&m)

	if m.eval.TopoScrollOffset == 0 {
		t.Error("topo scroll offset should have adjusted for cursor=10")
	}
}

// --- AC-5.6: [P1] evalSynAdjustScroll keeps cursor visible ---
func TestATDD_27_10_SynAdjustScroll(t *testing.T) {
	m := newEvalModel()
	m.height = 20
	m.eval.SynScrollOffset = 0
	m.eval.SynCursor = 10
	m.eval.Synergies = make([]kernel.ComboSummary, 20)

	evalSynAdjustScroll(&m)

	if m.eval.SynScrollOffset == 0 {
		t.Error("synergy scroll offset should have adjusted for cursor=10")
	}
}

// =============================================================================
// Cross-pane interaction tests
// =============================================================================

// --- AC-3.6: [P0] v/V/p keys should NOT trigger in Eval pane ---
func TestATDD_27_10_VKeyGuard(t *testing.T) {
	m := newEvalModel()
	m.eval.Reputations = makeReputationSummaries()

	// 'v' key should not trigger step-detail expansion in eval pane
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'v'})
	model := m2.(dashboardModel)

	// Model should remain in eval pane without changing to timeline mode
	if model.activePane != paneEval {
		t.Errorf("AC-3: v key in eval pane should not change activePane, got %d", model.activePane)
	}
}

// Ensure unused imports are consumed (build guard).
var (
	_ = fmt.Sprintf
	_ = strings.Contains
	_ = ipc.TopologyQueryResponse{}
	_ = kernel.ReputationSummary{}
	_ = kernel.ComboSummary{}
	_ = kernel.TopologyNode{}
	_ = kernel.CooperationEdge{}
)
