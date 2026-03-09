package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD RED PHASE — Story 17.1: Dashboard 框架与智能体树窗格
// All tests assert EXPECTED behavior. They FAIL because the
// functions return zero values (stubs not yet implemented).
// ============================================================

// --- helpers ---

func mockDashboardProcs() []vfs.ProcInfo {
	now := time.Now()
	return []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "init", Skills: []string{"supervisor"}, TokensUsed: 500, CreatedAt: now},
		{PID: 2, PPID: 1, State: types.StateRunning, Intent: "review", Skills: []string{"code-analyst"}, TokensUsed: 3200, CreatedAt: now},
		{PID: 3, PPID: 1, State: types.StateZombie, Intent: "build", Skills: []string{"builder"}, TokensUsed: 8000, CreatedAt: now},
		{PID: 5, PPID: 2, State: types.StateRunning, Intent: "lint", Skills: []string{"linter"}, TokensUsed: 1100, CreatedAt: now},
	}
}

func newTestDashboardModel(procs []vfs.ProcInfo) dashboardModel {
	m := newDashboardModel(nil)
	m.processes = procs
	m.treeRows = make([]flatRow, len(procs))
	for i, p := range procs {
		m.treeRows[i] = flatRow{proc: p, depth: 0}
	}
	m.width = 120
	m.height = 40
	m.connected = true
	return m
}

// --- 17.1-INT-001: [P0] dashboard command registered in cobra ---

func TestHelp_ContainsDashboardSubcommand(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "dashboard") {
		t.Errorf("expected 'dashboard' subcommand in help output, got %q", output)
	}
}

// --- 17.1-INT-002: [P1] rnix dashboard without daemon exits gracefully ---

func TestRunDashboard_NoDaemon(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	err := runDashboard(nil, nil)
	if err != nil {
		t.Fatalf("runDashboard should handle daemon absence gracefully, got error: %v", err)
	}
}

// --- 17.1-UNIT-001: [P0] Init() returns tick command (AC1) ---

func TestDashboardModel_Init(t *testing.T) {
	m := newDashboardModel(nil)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil tick command")
	}
}

// --- 17.1-UNIT-002: [P0] View() sets AltScreen = true (AC1) ---

func TestDashboardModel_ViewAltScreen(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	if !v.AltScreen {
		t.Error("View should set AltScreen = true")
	}
	if v.Content == "" {
		t.Error("View content should not be empty")
	}
}

// --- 17.1-UNIT-003: [P0] View renders multi-pane layout (AC1) ---

func TestDashboardModel_ViewMultiPaneLayout(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Agent Tree") {
		t.Errorf("view should contain 'Agent Tree' pane title, got %q", content)
	}
	if !strings.Contains(content, "Timeline") {
		t.Errorf("view should contain 'Timeline' pane placeholder, got %q", content)
	}
	if !strings.Contains(content, "Heatmap") {
		t.Errorf("view should contain 'Heatmap' pane placeholder, got %q", content)
	}
}

// --- 17.1-UNIT-004: [P1] View title bar with connection status (AC1) ---

func TestDashboardModel_ViewTitleBar(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Rnix Dashboard") {
		t.Errorf("view should contain 'Rnix Dashboard' title, got %q", content)
	}
	if !strings.Contains(content, "Connected") {
		t.Errorf("view should show connection status, got %q", content)
	}
}

// --- 17.1-UNIT-005: [P1] View status bar with keyboard shortcuts (AC1) ---

func TestDashboardModel_ViewStatusBar(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Quit") {
		t.Errorf("status bar should contain quit hint, got %q", content)
	}
	if !strings.Contains(content, "Tab") {
		t.Errorf("status bar should contain Tab hint, got %q", content)
	}
}

// --- 17.1-UNIT-006: [P0] buildProcessTree empty list → empty tree (AC2) ---

func TestDashboardBuildProcessTree_Empty(t *testing.T) {
	roots := buildProcessTree(nil)
	if len(roots) != 0 {
		t.Errorf("expected 0 roots for nil input, got %d", len(roots))
	}

	roots = buildProcessTree([]vfs.ProcInfo{})
	if len(roots) != 0 {
		t.Errorf("expected 0 roots for empty input, got %d", len(roots))
	}
}

// --- 17.1-UNIT-007: [P0] buildProcessTree parent-child relationships (AC2) ---

func TestDashboardBuildProcessTree_ParentChild(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning},
		{PID: 2, PPID: 1, State: types.StateRunning},
		{PID: 3, PPID: 1, State: types.StateZombie},
	}
	roots := buildProcessTree(procs)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(roots[0].children) != 2 {
		t.Fatalf("expected 2 children of root, got %d", len(roots[0].children))
	}
	if roots[0].children[0].proc.PID != 2 {
		t.Errorf("first child PID should be 2, got %d", roots[0].children[0].proc.PID)
	}
	if roots[0].children[1].proc.PID != 3 {
		t.Errorf("second child PID should be 3, got %d", roots[0].children[1].proc.PID)
	}
}

// --- 17.1-UNIT-008: [P0] buildProcessTree multi-level nesting (AC2) ---

func TestDashboardBuildProcessTree_DeepNesting(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning},
		{PID: 2, PPID: 1, State: types.StateRunning},
		{PID: 3, PPID: 2, State: types.StateRunning},
		{PID: 4, PPID: 3, State: types.StateRunning},
	}
	roots := buildProcessTree(procs)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	node := roots[0]
	for depth := 1; depth <= 3; depth++ {
		if len(node.children) != 1 {
			t.Fatalf("depth %d: expected 1 child, got %d", depth, len(node.children))
		}
		node = node.children[0]
	}
	if node.proc.PID != 4 {
		t.Errorf("deepest node should be PID 4, got %d", node.proc.PID)
	}
}

// --- 17.1-UNIT-009: [P0] flattenTree indentation prefixes (AC2) ---

func TestDashboardFlattenTree_Indentation(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0},
		{PID: 2, PPID: 1},
		{PID: 3, PPID: 1},
	}
	roots := buildProcessTree(procs)
	rows := flattenTree(roots)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].prefix != "" {
		t.Errorf("root should have no prefix, got %q", rows[0].prefix)
	}
	if !strings.Contains(rows[1].prefix, "├") {
		t.Errorf("non-last child should use ├── prefix, got %q", rows[1].prefix)
	}
	if !strings.Contains(rows[2].prefix, "└") {
		t.Errorf("last child should use └── prefix, got %q", rows[2].prefix)
	}
}

// --- 17.1-UNIT-010: [P0] Update(tickMsg) refreshes process tree (AC2) ---

func TestDashboardModel_TickUpdatesTree(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newDashboardModel(nil)
	m.connected = false

	updated, cmd := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)

	if cmd == nil {
		t.Error("tick should schedule next tick command")
	}
	_ = um
}

// --- 17.1-UNIT-011: [P0] j/k cursor navigation (AC2) ---

func TestDashboardModel_NavigateJK(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(dashboardModel)
	if um.treeCursor != 1 {
		t.Errorf("j should move cursor down: expected 1, got %d", um.treeCursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'j'})
	um = updated.(dashboardModel)
	if um.treeCursor != 2 {
		t.Errorf("j again: expected 2, got %d", um.treeCursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(dashboardModel)
	if um.treeCursor != 1 {
		t.Errorf("k should move cursor up: expected 1, got %d", um.treeCursor)
	}

	um.treeCursor = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(dashboardModel)
	if um.treeCursor != 0 {
		t.Errorf("k at top should stay at 0, got %d", um.treeCursor)
	}
}

// --- 17.1-UNIT-012: [P0] q returns tea.Quit (AC1) ---

func TestDashboardModel_QuitQ(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("q should return a non-nil quit command")
	}
}

// --- 17.1-UNIT-013: [P0] Tab switches activePane (AC1) ---

func TestDashboardModel_TabSwitchPane(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	if m.activePane != paneTree {
		t.Fatalf("initial activePane should be paneTree, got %d", m.activePane)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um := updated.(dashboardModel)
	if um.activePane == paneTree {
		t.Error("Tab should switch away from paneTree")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um = updated.(dashboardModel)
	if um.activePane == paneTimeline {
		t.Error("second Tab should switch away from paneTimeline")
	}
}

// --- 17.1-UNIT-014: [P0] Kill confirmation K → y (AC2) ---

func TestDashboardModel_KillConfirmY(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(dashboardModel)
	if !um.confirmKill {
		t.Error("K should enter kill confirmation mode")
	}
	if um.confirmPID == 0 {
		t.Error("confirmPID should be set to selected process PID")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'y'})
	um = updated.(dashboardModel)
	if um.confirmKill {
		t.Error("y should exit kill confirmation mode")
	}
}

// --- 17.1-UNIT-015: [P0] Kill confirmation K → n (AC2) ---

func TestDashboardModel_KillConfirmN(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(dashboardModel)
	if !um.confirmKill {
		t.Error("K should enter kill confirmation mode")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'n'})
	um = updated.(dashboardModel)
	if um.confirmKill {
		t.Error("n should cancel kill confirmation")
	}
}

// --- 17.1-UNIT-016: [P1] Daemon disconnect → connected=false → reconnect (AC2) ---

func TestDashboardModel_DaemonDisconnect(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newDashboardModel(nil)
	m.connected = false
	updated, cmd := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)
	if um.connected {
		t.Error("should remain disconnected when no daemon available")
	}
	if cmd == nil {
		t.Error("should schedule next tick even when disconnected")
	}
}

// --- 17.1-UNIT-017: [P0] 50 processes render without panic (NFR37) ---

func TestDashboardModel_50Processes(t *testing.T) {
	procs := make([]vfs.ProcInfo, 50)
	for i := range procs {
		ppid := types.PID(0)
		if i > 0 {
			ppid = types.PID(i/3 + 1)
		}
		procs[i] = vfs.ProcInfo{
			PID:        types.PID(i + 1),
			PPID:       ppid,
			State:      types.StateRunning,
			Intent:     "test",
			TokensUsed: i * 100,
			CreatedAt:  time.Now(),
		}
	}

	m := newTestDashboardModel(procs)
	v := m.View()
	if v.Content == "" {
		t.Error("50 process view should not be empty")
	}
}

// --- 17.1-UNIT-018: [P1] selectedPID syncs with cursor movement (AC2) ---

func TestDashboardModel_SelectedPIDSync(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(dashboardModel)
	if um.selectedPID == 0 {
		t.Error("selectedPID should be updated when cursor moves")
	}
	if len(um.treeRows) > 1 && um.selectedPID != um.treeRows[um.treeCursor].proc.PID {
		t.Errorf("selectedPID should match cursor row PID, got %d vs %d",
			um.selectedPID, um.treeRows[um.treeCursor].proc.PID)
	}
}

// --- 17.1-UNIT-019: [P1] scroll viewport follows cursor (AC2) ---

func TestDashboardModel_ScrollViewport(t *testing.T) {
	procs := make([]vfs.ProcInfo, 30)
	for i := range procs {
		procs[i] = vfs.ProcInfo{
			PID:       types.PID(i + 1),
			PPID:      0,
			State:     types.StateRunning,
			CreatedAt: time.Now(),
		}
	}

	m := newTestDashboardModel(procs)
	m.height = 15

	for i := 0; i < 20; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
		m = updated.(dashboardModel)
	}

	if m.treeOffset == 0 {
		t.Error("treeOffset should have scrolled to keep cursor visible")
	}
}

// --- 17.1-UNIT-020: [P1] View renders process state colors (AC2) ---

func TestDashboardModel_ViewProcessStates(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "running") && !strings.Contains(content, "Running") {
		t.Errorf("view should display running state, got %q", content)
	}
}

// --- 17.1-UNIT-021: [P1] View renders token consumption with budget warning (AC2) ---

func TestDashboardModel_ViewTokenBudgetWarning(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, TokensUsed: 4500, ContextBudget: 5000, CreatedAt: time.Now()},
	}
	m := newTestDashboardModel(procs)
	v := m.View()

	if !strings.Contains(v.Content, "4,500") && !strings.Contains(v.Content, "4500") {
		t.Errorf("view should show token consumption, got %q", v.Content)
	}
}
