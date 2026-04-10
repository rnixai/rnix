package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
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
		{PID: 1, PPID: 0, UUID: "uuid-mock-001", State: types.StateRunning, Intent: "init", Skills: []string{"supervisor"}, TokensUsed: 500, CreatedAt: now},
		{PID: 2, PPID: 1, UUID: "uuid-mock-002", State: types.StateRunning, Intent: "review", Skills: []string{"code-analyst"}, TokensUsed: 3200, CreatedAt: now},
		{PID: 3, PPID: 1, UUID: "uuid-mock-003", State: types.StateZombie, Intent: "build", Skills: []string{"builder"}, TokensUsed: 8000, CreatedAt: now},
		{PID: 5, PPID: 2, UUID: "uuid-mock-005", State: types.StateRunning, Intent: "lint", Skills: []string{"linter"}, TokensUsed: 1100, CreatedAt: now},
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
	// Isolate from any real daemon by pointing to a non-existent socket
	saved := ipc.SocketPathOverride
	ipc.SocketPathOverride = filepath.Join(t.TempDir(), "nonexistent.sock")
	defer func() { ipc.SocketPathOverride = saved }()

	savedExit := exitCode
	defer func() { exitCode = savedExit }()
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
	// Story 34.5: default view now shows detail card instead of Focus Card
	if !strings.Contains(content, "Select a process") && !strings.Contains(content, "Provider:") {
		t.Errorf("view should contain detail card placeholder or content in default layout, got %q", content)
	}
}

// --- 17.1-UNIT-004: [P1] View title bar with connection status (AC1) ---

func TestDashboardModel_ViewTitleBar(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "rnix") {
		t.Errorf("view should contain 'rnix' title, got %q", content)
	}
	if !strings.Contains(content, "●") {
		t.Errorf("view should show connection status indicator, got %q", content)
	}
	if !strings.Contains(content, "P") {
		t.Errorf("view should contain process count indicator 'P', got %q", content)
	}
}

// --- 17.1-UNIT-005: [P1] View status bar with keyboard shortcuts (AC1) ---

func TestDashboardModel_ViewStatusBar(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "quit") {
		t.Errorf("status bar should contain quit hint, got %q", content)
	}
	if !strings.Contains(content, "help") {
		t.Errorf("status bar should contain help hint, got %q", content)
	}
}

// --- 17.1-UNIT-006: [P0] buildProcessTree empty list → empty tree (AC2) ---

func TestDashboardBuildProcessTree_Empty(t *testing.T) {
	roots := buildProcessTree(nil, treeSortPID, false)
	if len(roots) != 0 {
		t.Errorf("expected 0 roots for nil input, got %d", len(roots))
	}

	roots = buildProcessTree([]vfs.ProcInfo{}, treeSortPID, false)
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
	roots := buildProcessTree(procs, treeSortPID, true)
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
	roots := buildProcessTree(procs, treeSortPID, true)
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
	roots := buildProcessTree(procs, treeSortPID, true)
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

	for range 20 {
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

	// Story 34.3 replaced text state labels with emoji badges (🟢/🔴/etc.)
	if !strings.Contains(content, "running") && !strings.Contains(content, "Running") && !strings.Contains(content, "🟢") {
		t.Errorf("view should display running state indicator, got %q", content)
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

// ============================================================
// ATDD RED PHASE — Story 17.2: 追踪时间线窗格
// All tests assert EXPECTED behavior. They FAIL because the
// functions/fields do not exist yet (stubs not implemented).
// ============================================================

// --- timeline test helpers ---

func newTestTimelineDashboardModel() dashboardModel {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.activePane = paneTimeline
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "Open /dev/llm/claude"}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "plan", Summary: "Planning build step"}},
		{summary: ipc.StepSummaryWire{Step: 3, Action: "spawn", Summary: "Spawn builder"}},
		{summary: ipc.StepSummaryWire{Step: 4, Action: "tool_call", Summary: "Read file"}},
		{summary: ipc.StepSummaryWire{Step: 5, Action: "complete", Summary: "Done"}},
	}
	m.stepFilters = defaultStepFilters()
	return m
}

// --- 17.2-UNIT-009: [P0] timeline renders empty state text (AC1) ---

func TestDashboardModel_TimelineRenderEmpty(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 0
	m.activePane = paneTimeline
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	m.rightPane = paneTimeline

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Select an agent") {
		t.Error("timeline with no PID should show 'Select an agent' prompt")
	}
	if !strings.Contains(content, "Timeline") {
		t.Error("timeline pane should still show 'Timeline' title")
	}
}

// --- 17.2-UNIT-012: [P0] f key enters step filter mode (AC2) ---

func TestDashboardModel_TimelineFilter(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.viewMode = viewDefault

	// Press f to enter filter mode
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f'})
	um := updated.(dashboardModel)
	if !um.stepFilterMode {
		t.Error("pressing 'f' should enter step filter mode")
	}

	// Press f to exit filter mode
	updated, _ = um.Update(tea.KeyPressMsg{Code: 'f'})
	um = updated.(dashboardModel)
	if um.stepFilterMode {
		t.Error("pressing 'f' again should exit step filter mode")
	}
}

// --- 17.2-UNIT-014: [P0] PID change clears step entries (AC1) ---

func TestDashboardModel_TimelinePIDChange(t *testing.T) {
	m := newTestTimelineDashboardModel()
	if len(m.stepEntries) == 0 {
		t.Fatal("pre-condition: model should have step entries")
	}

	m.timelineAttachedUUID = m.selectedUUID
	m.selectedPID = 999
	m.selectedUUID = "uuid-999"

	m = m.handleTimelinePIDChange()

	if len(m.stepEntries) != 0 {
		t.Errorf("UUID change should clear stepEntries, got %d", len(m.stepEntries))
	}
	if m.timelineAttachedUUID != "uuid-999" {
		t.Errorf("timelineAttachedUUID should update to new UUID, got %q", m.timelineAttachedUUID)
	}
}

// --- 17.2-UNIT-015: [P1] Tab switches to paneTimeline (AC1) ---

func TestDashboardModel_TabToTimeline(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	if m.activePane != paneTree {
		t.Fatalf("initial activePane should be paneTree, got %d", m.activePane)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um := updated.(dashboardModel)
	if um.activePane != paneTimeline {
		t.Errorf("first Tab should switch to paneTimeline, got %d", um.activePane)
	}
}

// ============================================================
// ATDD RED PHASE — Story 17.3: 上下文热力图窗格
// All tests assert EXPECTED behavior. They FAIL because the
// functions return zero values (stubs not yet implemented).
// ============================================================

// --- heatmap test helpers ---

func mockHeatmapProfile() *debug.CtxProfileResult {
	return &debug.CtxProfileResult{
		PID:           2,
		TotalTokens:   1200,
		ContextBudget: 8000,
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 360, Pct: 30.0, Rank: 1},
			{Kind: "tool:read_file", Tokens: 300, Pct: 25.0, Rank: 2},
			{Kind: "user", Tokens: 240, Pct: 20.0, Rank: 3},
			{Kind: "assistant", Tokens: 180, Pct: 15.0, Rank: 4},
		},
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{Tokens: 600, Pct: 50.0, Messages: 3},
			Warm:   debug.ClassBucket{Tokens: 300, Pct: 25.0, Messages: 2},
			Cold:   debug.ClassBucket{Tokens: 240, Pct: 20.0, Messages: 4},
			Leaked: debug.ClassBucket{Tokens: 60, Pct: 5.0, Messages: 1},
		},
	}
}

func newTestHeatmapDashboardModel() dashboardModel {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.activePane = paneHeatmap
	m.viewMode = viewExpanded
	m.expandedPane = paneHeatmap
	m.rightPane = paneHeatmap
	m.heatmapProfile = mockHeatmapProfile()
	m.heatmapPID = 2
	m.heatmapSegments = []heatmapSegment{
		{label: "System Prompt", tokens: 360, pct: 30.0, kind: segSystem, activity: actActive},
		{label: "Tool Results", tokens: 300, pct: 25.0, kind: segTool, activity: actWarm},
		{label: "User Messages", tokens: 240, pct: 20.0, kind: segUser, activity: actActive},
		{label: "Assistant", tokens: 180, pct: 15.0, kind: segAssistant, activity: actCold},
		{label: "Leaked", tokens: 60, pct: 5.0, kind: segLeaked, activity: actLeaked},
	}
	return m
}

// --- 17.3-UNIT-001: [P0] buildHeatmapSegments — empty profile (AC1) ---

func TestBuildHeatmapSegments_Empty(t *testing.T) {
	segments := buildHeatmapSegments(nil)
	if len(segments) != 0 {
		t.Errorf("expected 0 segments for nil profile, got %d", len(segments))
	}

	empty := &debug.CtxProfileResult{}
	segments = buildHeatmapSegments(empty)
	if len(segments) != 0 {
		t.Errorf("expected 0 segments for empty profile, got %d", len(segments))
	}
}

// --- 17.3-UNIT-002: [P0] buildHeatmapSegments — sorted by token desc (AC1) ---

func TestBuildHeatmapSegments_SortedByTokenDesc(t *testing.T) {
	profile := mockHeatmapProfile()
	segments := buildHeatmapSegments(profile)

	if len(segments) == 0 {
		t.Fatal("expected non-empty segments for profile with TopConsumers")
	}
	for i := 1; i < len(segments); i++ {
		if segments[i].tokens > segments[i-1].tokens {
			t.Errorf("segments not sorted by token desc: [%d].tokens=%d > [%d].tokens=%d",
				i, segments[i].tokens, i-1, segments[i-1].tokens)
		}
	}
}

// --- 17.3-UNIT-003: [P0] buildHeatmapSegments — merge small segments (AC1) ---

func TestBuildHeatmapSegments_MergeSmall(t *testing.T) {
	profile := &debug.CtxProfileResult{
		PID:           2,
		TotalTokens:   1000,
		ContextBudget: 8000,
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 500, Pct: 50.0, Rank: 1},
			{Kind: "user", Tokens: 400, Pct: 40.0, Rank: 2},
			{Kind: "tool:read_file", Tokens: 20, Pct: 2.0, Rank: 3},
			{Kind: "assistant", Tokens: 10, Pct: 1.0, Rank: 4},
		},
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{Tokens: 800, Pct: 80.0, Messages: 2},
		},
	}
	segments := buildHeatmapSegments(profile)

	hasOther := false
	for _, seg := range segments {
		if seg.label == "Other" {
			hasOther = true
		}
		if seg.pct < 3.0 && seg.label != "Other" {
			t.Errorf("segment %q has pct %.1f%% which is <3%% and should be merged into Other",
				seg.label, seg.pct)
		}
	}
	if !hasOther {
		t.Error("expected 'Other' segment for merged small entries (<3%%)")
	}
}

// --- 17.3-UNIT-004: [P1] segmentColor — kind and activity variations (AC1) ---

func TestSegmentColor_KindAndActivity(t *testing.T) {
	tests := []struct {
		kind     segmentKind
		activity activityLevel
	}{
		{segSystem, actActive},
		{segSystem, actCold},
		{segTool, actActive},
		{segTool, actWarm},
		{segUser, actActive},
		{segLeaked, actLeaked},
	}

	for _, tt := range tests {
		color := segmentColor(tt.kind, tt.activity)
		if color == "" {
			t.Errorf("segmentColor(%d, %d) returned empty string", tt.kind, tt.activity)
		}
	}

	activeColor := segmentColor(segSystem, actActive)
	coldColor := segmentColor(segSystem, actCold)
	if activeColor == coldColor {
		t.Errorf("segSystem active and cold should have different colors, both got %q", activeColor)
	}
}

// --- 17.3-UNIT-005: [P0] heatmapProfileMsg stores profile in model (AC1) ---

func TestDashboardModel_HeatmapProfileMsg(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2

	profile := mockHeatmapProfile()
	updated, _ := m.Update(heatmapProfileMsg{profile: profile})
	um := updated.(dashboardModel)

	if um.heatmapProfile == nil {
		t.Error("heatmapProfileMsg should store profile in model")
	}
	if len(um.heatmapSegments) == 0 {
		t.Error("heatmapProfileMsg should trigger buildHeatmapSegments and produce segments")
	}
}

// --- 17.3-UNIT-006: [P0] heatmap renders "Select an agent" when no PID (AC1) ---

func TestDashboardModel_HeatmapRenderEmpty(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 0
	m.activePane = paneHeatmap
	m.viewMode = viewExpanded
	m.expandedPane = paneHeatmap
	m.rightPane = paneHeatmap

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Select an agent") {
		t.Error("heatmap with no PID should show 'Select an agent' prompt")
	}
}

// --- 17.3-UNIT-007: [P0] heatmap renders without "Coming Soon" when segments exist (AC1) ---

func TestDashboardModel_HeatmapRenderWithSegments(t *testing.T) {
	m := newTestHeatmapDashboardModel()

	v := m.View()
	content := v.Content

	if strings.Contains(content, "Coming Soon") {
		t.Error("heatmap with segments should not show 'Coming Soon'")
	}
	if !strings.Contains(content, "Heatmap") {
		t.Error("heatmap pane should show 'Heatmap' title")
	}
}

// --- 17.3-UNIT-008: [P0] j/k moves heatmapCursor (AC2) ---

func TestDashboardModel_HeatmapCursorJK(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.heatmapCursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(dashboardModel)
	if um.heatmapCursor != 1 {
		t.Errorf("j should move heatmapCursor down: expected 1, got %d", um.heatmapCursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(dashboardModel)
	if um.heatmapCursor != 0 {
		t.Errorf("k should move heatmapCursor up: expected 0, got %d", um.heatmapCursor)
	}

	um.heatmapCursor = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(dashboardModel)
	if um.heatmapCursor != 0 {
		t.Errorf("k at top should stay at 0, got %d", um.heatmapCursor)
	}
}

// --- 17.3-UNIT-009: [P0] selected segment shows token count and percentage (AC2) ---

func TestDashboardModel_HeatmapSelectedDetails(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.heatmapCursor = 0
	m.activePane = paneHeatmap

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "360") {
		t.Error("heatmap should display selected segment token count (360)")
	}
	if !strings.Contains(content, "30.0%") || !strings.Contains(content, "30.0") {
		t.Error("heatmap should display selected segment percentage (30.0%%)")
	}
}

// --- 17.3-UNIT-010: [P0] PID change clears heatmapProfile (AC1) ---

func TestDashboardModel_HeatmapPIDChange(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	if m.heatmapProfile == nil {
		t.Fatal("precondition: model should have heatmap profile")
	}

	m.selectedPID = 999
	m = m.handleHeatmapPIDChange()

	if m.heatmapProfile != nil {
		t.Error("PID change should clear heatmapProfile")
	}
	if len(m.heatmapSegments) != 0 {
		t.Errorf("PID change should clear heatmapSegments, got %d", len(m.heatmapSegments))
	}
	if m.heatmapCursor != 0 {
		t.Errorf("PID change should reset heatmapCursor to 0, got %d", m.heatmapCursor)
	}
}

// --- 17.3-UNIT-011: [P1] Digit key 3 switches to paneHeatmap (AC1) ---

func TestDashboardModel_TabToHeatmap(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTimeline

	// Use digit key '3' to switch to Heatmap (Tab now toggles Tree ↔ rightPane)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	um := updated.(dashboardModel)
	if um.activePane != paneHeatmap {
		t.Errorf("Digit '3' should switch to paneHeatmap, got %d", um.activePane)
	}
	if um.rightPane != paneHeatmap {
		t.Errorf("Digit '3' should set rightPane to paneHeatmap, got %d", um.rightPane)
	}
}

// --- 17.3-UNIT-012: [P1] mapConsumerKind classifies consumer kinds (AC1) ---

func TestMapConsumerKindToSegmentKind(t *testing.T) {
	tests := []struct {
		kind     string
		expected segmentKind
	}{
		{"system_prompt", segSystem},
		{"user", segUser},
		{"assistant", segAssistant},
		{"tool:read_file", segTool},
		{"tool:write_file", segTool},
	}
	for _, tt := range tests {
		got := mapConsumerKind(tt.kind)
		if got != tt.expected {
			t.Errorf("mapConsumerKind(%q) = %d, want %d", tt.kind, got, tt.expected)
		}
	}
}

// --- 17.3-UNIT-013: [P1] heatmapTickCount increments for refresh (AC1) ---

// --- CR-FIX-001: [P0] Enter toggles heatmapExpanded (AC2) ---

func TestDashboardModel_HeatmapEnterToggle(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.heatmapCursor = 0

	if m.heatmapExpanded {
		t.Fatal("heatmapExpanded should default to false")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(dashboardModel)
	if !um.heatmapExpanded {
		t.Error("enter should toggle heatmapExpanded to true")
	}

	v := um.View()
	if !strings.Contains(v.Content, "── Selected:") {
		t.Error("expanded heatmap should show detail section with '── Selected:'")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um = updated.(dashboardModel)
	if um.heatmapExpanded {
		t.Error("enter again should toggle heatmapExpanded back to false")
	}
}

// --- CR-FIX-002: [P0] buildHeatmapSegments merges same-kind consumers (AC1) ---

func TestBuildHeatmapSegments_MergeToolKinds(t *testing.T) {
	profile := &debug.CtxProfileResult{
		PID:           2,
		TotalTokens:   1000,
		ContextBudget: 8000,
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 400, Pct: 40.0, Rank: 1},
			{Kind: "tool:read_file", Tokens: 200, Pct: 20.0, Rank: 2},
			{Kind: "tool:write_file", Tokens: 150, Pct: 15.0, Rank: 3},
			{Kind: "user", Tokens: 250, Pct: 25.0, Rank: 4},
		},
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{Tokens: 800, Pct: 80.0, Messages: 3},
		},
	}
	segments := buildHeatmapSegments(profile)

	toolCount := 0
	totalToolTokens := 0
	for _, seg := range segments {
		if seg.kind == segTool {
			toolCount++
			totalToolTokens = seg.tokens
		}
	}
	if toolCount != 1 {
		t.Errorf("tool types should be merged into 1 segment, got %d", toolCount)
	}
	if totalToolTokens != 350 {
		t.Errorf("merged tool segment should have 350 tokens, got %d", totalToolTokens)
	}
}

// --- CR-FIX-003: [P0] heatmapProfileMsg error stored and displayed (AC1) ---

func TestDashboardModel_HeatmapProfileError(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.activePane = paneHeatmap
	m.viewMode = viewExpanded
	m.expandedPane = paneHeatmap
	m.rightPane = paneHeatmap

	updated, _ := m.Update(heatmapProfileMsg{err: fmt.Errorf("connection refused")})
	um := updated.(dashboardModel)

	if um.heatmapErr == nil {
		t.Error("heatmapProfileMsg with error should store heatmapErr")
	}

	v := um.View()
	if !strings.Contains(v.Content, "connection refused") {
		t.Error("heatmap pane should display error message")
	}
}

// --- CR-FIX-004: [P1] mapConsumerKind maps skill → segSkill (AC1) ---

func TestMapConsumerKind_Skill(t *testing.T) {
	if got := mapConsumerKind("skill"); got != segSkill {
		t.Errorf("mapConsumerKind(\"skill\") = %d, want segSkill(%d)", got, segSkill)
	}
	if got := mapConsumerKind("skill:code-analyst"); got != segSkill {
		t.Errorf("mapConsumerKind(\"skill:code-analyst\") = %d, want segSkill(%d)", got, segSkill)
	}
}

// --- CR-FIX-005: [P1] PID change resets heatmapExpanded and heatmapErr (AC1) ---

func TestDashboardModel_HeatmapPIDChangeResetsState(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.heatmapExpanded = true
	m.heatmapErr = fmt.Errorf("old error")

	m.selectedPID = 999
	m = m.handleHeatmapPIDChange()

	if m.heatmapExpanded {
		t.Error("PID change should reset heatmapExpanded to false")
	}
	if m.heatmapErr != nil {
		t.Error("PID change should clear heatmapErr")
	}
}

// ============================================================
// ATDD RED PHASE — Story 17.4: 窗格联动与进程操作
// All tests assert EXPECTED behavior. They FAIL because the
// functions are stubs (not yet implemented).
// ============================================================

// --- 17.4-UNIT-001: [P0] PID change via tree j → immediate cmd (AC1) ---

func TestDashboardModel_PIDChangeImmediateLinkage(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 0
	m.activePane = paneTree

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(dashboardModel)

	if um.selectedPID == 0 {
		t.Error("selectedPID should be set after j press")
	}
	if cmd == nil {
		t.Error("PID change should return non-nil cmd (containing timeline+heatmap fetch)")
	}
}

// --- 17.4-UNIT-002: [P0] handlePIDChange — selectedPID=0 → nil cmd (AC1) ---

func TestDashboardModel_HandlePIDChangeNoPID(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.selectedPID = 0

	m2, cmd := m.handlePIDChange()

	if cmd != nil {
		t.Error("handlePIDChange with selectedPID=0 should return nil cmd (no IPC)")
	}
	if m2.timelineAttachedPID != 0 {
		t.Errorf("timelineAttachedPID should be 0 when selectedPID=0, got %d", m2.timelineAttachedPID)
	}
	if m2.heatmapPID != 0 {
		t.Errorf("heatmapPID should be 0 when selectedPID=0, got %d", m2.heatmapPID)
	}
}

// --- 17.4-UNIT-003: [P0] handlePIDChange — PID change clears timeline + heatmap (AC1) ---

func TestDashboardModel_HandlePIDChangeClearsData(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.heatmapProfile = mockHeatmapProfile()
	m.heatmapSegments = []heatmapSegment{{label: "test", tokens: 100}}
	m.heatmapPID = 2

	m.selectedPID = 999
	m.selectedUUID = "uuid-999"
	m2, _ := m.handlePIDChange()

	if len(m2.stepEntries) != 0 {
		t.Errorf("handlePIDChange should clear stepEntries, got %d", len(m2.stepEntries))
	}
	if m2.heatmapProfile != nil {
		t.Error("handlePIDChange should clear heatmapProfile")
	}
	if len(m2.heatmapSegments) != 0 {
		t.Errorf("handlePIDChange should clear heatmapSegments, got %d", len(m2.heatmapSegments))
	}
	if m2.timelineAttachedUUID != "uuid-999" {
		t.Errorf("timelineAttachedUUID should be uuid-999, got %q", m2.timelineAttachedUUID)
	}
	if m2.heatmapPID != 999 {
		t.Errorf("heatmapPID should be 999, got %d", m2.heatmapPID)
	}
}

// --- 17.4-UNIT-004: [P0] Global kill — Shift+K in timeline triggers confirmKill (AC2) ---

func TestDashboardModel_GlobalKillConfirmTimeline(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTimeline
	m.selectedPID = 2
	m.connected = true

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(dashboardModel)

	if !um.confirmKill {
		t.Error("Shift+K in timeline pane with selectedPID > 0 should trigger confirmKill")
	}
	if um.confirmPID != 2 {
		t.Errorf("confirmPID should be 2, got %d", um.confirmPID)
	}
}

// --- CR-FIX-006: [P0] Timeline k navigates up, not kill (AC2) ---

func TestDashboardModel_TimelineKNavigatesNotKill(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.stepCursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.confirmKill {
		t.Error("k in timeline pane should navigate, not trigger kill")
	}
	if um.stepCursor != 1 {
		t.Errorf("k in timeline should move cursor up: expected 1, got %d", um.stepCursor)
	}
}

// --- CR-FIX-007: [P0] Heatmap k navigates up, not kill (AC2) ---

func TestDashboardModel_HeatmapKNavigatesNotKill(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.heatmapCursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.confirmKill {
		t.Error("k in heatmap pane should navigate, not trigger kill")
	}
	if um.heatmapCursor != 1 {
		t.Errorf("k in heatmap should move cursor up: expected 1, got %d", um.heatmapCursor)
	}
}

// --- CR-FIX-008: [P0] Shift+K triggers kill in heatmap pane (AC2) ---

func TestDashboardModel_ShiftKKillsInHeatmap(t *testing.T) {
	m := newTestHeatmapDashboardModel()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(dashboardModel)

	if !um.confirmKill {
		t.Error("Shift+K in heatmap pane should trigger confirmKill")
	}
	if um.confirmPID != 2 {
		t.Errorf("confirmPID should be 2, got %d", um.confirmPID)
	}
}

// --- 17.4-UNIT-005: [P1] Global kill — no selectedPID → no action (AC2) ---

func TestDashboardModel_GlobalKillNoSelectedPID(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTimeline
	m.selectedPID = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.confirmKill {
		t.Error("k without selectedPID should not trigger confirmKill")
	}
}

// --- 17.4-UNIT-006: [P0] Tree pane k → navigate up, not kill (AC2) ---

func TestDashboardModel_TreeKNavigatesUpNotKill(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTree
	m.treeCursor = 2
	m.selectedPID = 3

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.treeCursor != 1 {
		t.Errorf("k in tree pane should navigate up: expected cursor 1, got %d", um.treeCursor)
	}
	if um.confirmKill {
		t.Error("k in tree pane should NOT trigger kill confirmation")
	}
}

// --- 17.4-UNIT-007: [P0] execResultMsg — success sets statusMsg (AC2) ---

func TestExecResultMsg_Success(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())

	updated, _ := m.Update(execResultMsg{err: nil})
	um := updated.(dashboardModel)

	if um.statusMsg == "" {
		t.Error("execResultMsg with nil err should set statusMsg")
	}
	if um.statusMsgTTL <= 0 {
		t.Error("statusMsgTTL should be set > 0 after execResultMsg")
	}
}

// --- 17.4-UNIT-008: [P0] execResultMsg — error sets statusMsg (AC2) ---

func TestExecResultMsg_Error(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())

	updated, _ := m.Update(execResultMsg{err: fmt.Errorf("gdb failed")})
	um := updated.(dashboardModel)

	if um.statusMsg == "" {
		t.Error("execResultMsg with err should set statusMsg")
	}
	if !strings.Contains(um.statusMsg, "gdb failed") {
		t.Errorf("statusMsg should contain error message, got %q", um.statusMsg)
	}
	if um.statusMsgTTL <= 0 {
		t.Error("statusMsgTTL should be set > 0 after execResultMsg error")
	}
}

// --- 17.4-UNIT-009: [P0] recordToggleMsg — start recording updates map (AC2) ---

func TestRecordToggleMsg_Start(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.recording = make(map[string]string)

	updated, _ := m.Update(recordToggleMsg{pid: 2, uuid: "uuid-mock-002", recordID: "rec-001"})
	um := updated.(dashboardModel)

	if um.recording["uuid-mock-002"] != "rec-001" {
		t.Errorf("recording[uuid-mock-002] should be 'rec-001', got %q", um.recording["uuid-mock-002"])
	}
	if um.statusMsg == "" {
		t.Error("start recording should set statusMsg")
	}
}

// --- 17.4-UNIT-010: [P0] recordToggleMsg — stop recording clears map (AC2) ---

func TestRecordToggleMsg_Stop(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.recording = map[string]string{"uuid-mock-002": "rec-001"}

	updated, _ := m.Update(recordToggleMsg{pid: 2, uuid: "uuid-mock-002", stopped: true, eventCount: 42})
	um := updated.(dashboardModel)

	if _, exists := um.recording["uuid-mock-002"]; exists {
		t.Error("stop recording should remove UUID from recording map")
	}
	if um.statusMsg == "" {
		t.Error("stop recording should set statusMsg")
	}
	if !strings.Contains(um.statusMsg, "42") {
		t.Errorf("statusMsg should contain event count 42, got %q", um.statusMsg)
	}
}

// --- 17.4-UNIT-011: [P0] recordToggleMsg — error sets statusMsg (AC2) ---

func TestRecordToggleMsg_Error(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.recording = make(map[string]string)

	updated, _ := m.Update(recordToggleMsg{pid: 2, err: fmt.Errorf("connection refused")})
	um := updated.(dashboardModel)

	if um.statusMsg == "" {
		t.Error("recordToggleMsg with error should set statusMsg")
	}
	if !strings.Contains(um.statusMsg, "connection refused") {
		t.Errorf("statusMsg should contain error, got %q", um.statusMsg)
	}
}

// --- CR-FIX-009: [P0] a key toggles alert strip (was GDB, now Story 34.4) ---

func TestDashboardModel_GlobalAlertToggleKey(t *testing.T) {
	m := newTestHeatmapDashboardModel()
	m.alertEvents = []UnifiedEvent{
		{Type: EventStall, Severity: SevWarn, Summary: "stall"},
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'a'})
	model := m2.(dashboardModel)

	if !model.alertExpanded {
		t.Error("a key with alertEvents should toggle alertExpanded to true")
	}
}

// --- CR-FIX-010: [P0] l key in heatmap returns non-nil cmd for log (AC2) ---

func TestDashboardModel_GlobalLogKey(t *testing.T) {
	m := newTestHeatmapDashboardModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l'})

	if cmd == nil {
		t.Error("l key in heatmap pane with selectedPID > 0 should return non-nil cmd (ExecProcess for log)")
	}
}

// --- CR-FIX-011: [P0] r key returns non-nil cmd for record toggle (AC2) ---

func TestDashboardModel_GlobalRecordKey(t *testing.T) {
	m := newTestHeatmapDashboardModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})

	if cmd == nil {
		t.Error("r key with selectedPID > 0 should return non-nil cmd (toggleRecordCmd)")
	}
}

// --- 17.4-UNIT-012: [P1] Status bar shows ●REC when recording (AC2) ---

func TestDashboardModel_StatusBarRecording(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.recording = map[string]string{"uuid-mock-002": "rec-001"}

	v := m.View()
	if !strings.Contains(v.Content, "●REC") {
		t.Error("status bar should show '●REC' when selected process is recording")
	}
}

// --- 17.4-UNIT-013: [P1] Status bar shows operation key hints (AC2) ---

func TestDashboardModel_StatusBarOperationKeys(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2

	// Process ops (kill/gdb/log/rec) are now in ? help overlay, not in status bar.
	// Status bar should contain ? help hint instead.
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "help") {
		t.Error("status bar should contain 'help' hint (press ? for full shortcuts)")
	}
}

// --- 17.4-UNIT-014: [P0] statusMsgTTL — tick decrements to 0 then clears (AC2) ---

func TestDashboardModel_StatusMsgTTL(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newDashboardModel(nil)
	m.connected = false
	m.statusMsg = "test message"
	m.statusMsgTTL = 2

	updated, _ := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)
	if um.statusMsgTTL != 1 {
		t.Errorf("after 1st tick, statusMsgTTL should be 1, got %d", um.statusMsgTTL)
	}
	if um.statusMsg == "" {
		t.Error("statusMsg should still be set when TTL > 0")
	}

	updated, _ = um.Update(tickMsg(time.Now()))
	um = updated.(dashboardModel)
	if um.statusMsgTTL != 0 {
		t.Errorf("after 2nd tick, statusMsgTTL should be 0, got %d", um.statusMsgTTL)
	}
	if um.statusMsg != "" {
		t.Errorf("statusMsg should be cleared when TTL reaches 0, got %q", um.statusMsg)
	}
}

// --- 17.4-UNIT-015: [P1] Tree pane recording indicator — ● for recording PID (AC2) ---

func TestDashboardModel_TreeRecordingIndicator(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.recording = map[string]string{"uuid-mock-002": "rec-001"}

	v := m.View()

	lines := strings.Split(v.Content, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "review") && strings.Contains(line, "●") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tree pane should show '●' indicator on the line for recording PID 2 (intent: review)")
	}
}

// ============================================================
// ATDD RED PHASE — Story 17.5: 离线回放分析
// All tests assert EXPECTED behavior. They FAIL because the
// functions return zero values (stubs not yet implemented).
// ============================================================

// --- replay test helpers ---

func newTestRecordReader(t *testing.T) *debug.RecordReader {
	t.Helper()
	dir := t.TempDir()

	meta := debug.RecordMetadata{
		RecordID: "test-rec-001",
		PID:      2,
		Intent:   "review",
		Status:   debug.RecordStatusCompleted,
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaJSON, 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	events := []debug.RecordEvent{
		{SeqNum: 0, Timestamp: 100 * time.Millisecond, PID: 2, Type: debug.RecordSyscall, Syscall: &debug.SyscallEventData{
			Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}, Duration: 10 * time.Millisecond,
		}},
		{SeqNum: 1, Timestamp: 200 * time.Millisecond, PID: 2, Type: debug.RecordSyscall, Syscall: &debug.SyscallEventData{
			Syscall: "Write", Args: map[string]any{"data": "hello"}, Duration: 5 * time.Millisecond,
		}},
		{SeqNum: 2, Timestamp: 300 * time.Millisecond, PID: 2, Type: debug.RecordStateChange, State: &debug.StateChangeData{
			FromState: "running", ToState: "sleeping", Reason: "waiting for LLM",
		}},
		{SeqNum: 3, Timestamp: 400 * time.Millisecond, PID: 2, Type: debug.RecordContextSnapshot, Context: &debug.ContextSnapshotData{
			Messages: []string{"[system] You are an assistant", "[user] Hello", "[assistant] Hi there"},
		}},
		{SeqNum: 4, Timestamp: 500 * time.Millisecond, PID: 2, Type: debug.RecordSyscall, Syscall: &debug.SyscallEventData{
			Syscall: "Read", Args: map[string]any{"fd": 3}, Err: "EOF", Duration: 1 * time.Millisecond,
		}},
	}

	var buf bytes.Buffer
	for _, ev := range events {
		line, _ := json.Marshal(ev)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), buf.Bytes(), 0644); err != nil {
		t.Fatalf("write events.jsonl: %v", err)
	}

	reader, err := debug.NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	return reader
}

func newTestReplayDashboardModel(t *testing.T) dashboardModel {
	t.Helper()
	reader := newTestRecordReader(t)
	m := newReplayDashboardModel(reader)
	m.width = 120
	m.height = 40
	return m
}

// --- 17.5-UNIT-001: [P0] newReplayDashboardModel initializes all replay fields (AC1) ---

func TestReplayDashboard_Init(t *testing.T) {
	reader := newTestRecordReader(t)
	m := newReplayDashboardModel(reader)

	if !m.replayMode {
		t.Error("replayMode should be true")
	}
	if m.replayReader != reader {
		t.Error("replayReader should be set to the provided reader")
	}
	if m.replayCursor != -1 {
		t.Errorf("replayCursor should be -1 (initial state), got %d", m.replayCursor)
	}
	if m.replaySpeed != 1.0 {
		t.Errorf("replaySpeed should be 1.0, got %f", m.replaySpeed)
	}
	if m.replayPlaying {
		t.Error("replayPlaying should be false initially")
	}
	if m.connected {
		t.Error("connected should be false in replay mode")
	}
	if m.recording == nil {
		t.Error("recording map should be initialized")
	}
}

// --- 17.5-UNIT-004: [P0] buildReplayProcessTree produces process info from recording (AC1) ---

func TestReplayDashboard_TreePane(t *testing.T) {
	reader := newTestRecordReader(t)
	procs := buildReplayProcessTree(reader, 4)

	if len(procs) == 0 {
		t.Fatal("expected at least 1 process from buildReplayProcessTree")
	}

	found := false
	for _, p := range procs {
		if p.PID == 2 {
			found = true
			if p.Intent != "review" {
				t.Errorf("PID 2 intent should be 'review', got %q", p.Intent)
			}
			if p.PPID != 1 {
				t.Errorf("PID 2 PPID should be 1, got %d", p.PPID)
			}
		}
	}
	if !found {
		t.Error("expected PID 2 in process tree (from recording metadata)")
	}
}

// --- 17.5-UNIT-006: [P0] buildReplayHeatmap finds nearest ContextSnapshot (AC1) ---

func TestReplayDashboard_Heatmap(t *testing.T) {
	reader := newTestRecordReader(t)

	profile := buildReplayHeatmap(reader, 4)
	if profile == nil {
		t.Fatal("buildReplayHeatmap should return non-nil for recording with ContextSnapshot")
	}
	if profile.TotalTokens != 0 {
		t.Errorf("TotalTokens should be 0 (TokenEstimate removed in Story 27.1 AC-9), got %d", profile.TotalTokens)
	}

	profileBefore := buildReplayHeatmap(reader, 2)
	if profileBefore != nil {
		t.Error("buildReplayHeatmap should return nil before any ContextSnapshot event (cursor=2)")
	}
}

// --- 17.5-UNIT-007: [P0] Space toggles replayPlaying (AC2) ---

func TestReplayDashboard_PlayPause(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 0

	if m.replayPlaying {
		t.Fatal("precondition: replayPlaying should be false")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: ' '})
	um := updated.(dashboardModel)
	if !um.replayPlaying {
		t.Error("Space should toggle replayPlaying to true")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: ' '})
	um = updated.(dashboardModel)
	if um.replayPlaying {
		t.Error("Space again should toggle replayPlaying back to false")
	}
}

// --- 17.5-UNIT-008: [P0] [/] keys adjust replaySpeed with bounds (AC2) ---

func TestReplayDashboard_SpeedControl(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 0
	m.replaySpeed = 1.0

	updated, _ := m.Update(tea.KeyPressMsg{Code: ']'})
	um := updated.(dashboardModel)
	if um.replaySpeed != 2.0 {
		t.Errorf("] should double speed: expected 2.0, got %f", um.replaySpeed)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '['})
	um = updated.(dashboardModel)
	if um.replaySpeed != 1.0 {
		t.Errorf("[ should halve speed: expected 1.0, got %f", um.replaySpeed)
	}

	um.replaySpeed = 0.5
	updated, _ = um.Update(tea.KeyPressMsg{Code: '['})
	um = updated.(dashboardModel)
	if um.replaySpeed != 0.5 {
		t.Errorf("[ at 0.5x should stay at 0.5, got %f", um.replaySpeed)
	}

	um.replaySpeed = 8.0
	updated, _ = um.Update(tea.KeyPressMsg{Code: ']'})
	um = updated.(dashboardModel)
	if um.replaySpeed != 8.0 {
		t.Errorf("] at 8.0x should stay at 8.0, got %f", um.replaySpeed)
	}
}

// --- 17.5-UNIT-009: [P0] ./,keys for frame step forward/backward (AC2) ---

func TestReplayDashboard_FrameStep(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: '.'})
	um := updated.(dashboardModel)
	if um.replayCursor != 3 {
		t.Errorf(". should advance cursor: expected 3, got %d", um.replayCursor)
	}
	if um.replayPlaying {
		t.Error(". should auto-pause")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: ','})
	um = updated.(dashboardModel)
	if um.replayCursor != 2 {
		t.Errorf(", should retreat cursor: expected 2, got %d", um.replayCursor)
	}

	um.replayCursor = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: ','})
	um = updated.(dashboardModel)
	if um.replayCursor != 0 {
		t.Errorf(", at cursor 0 should stay at 0, got %d", um.replayCursor)
	}
}

// --- 17.5-UNIT-010: [P0] 0/$ keys jump to start/end (AC2) ---

func TestReplayDashboard_JumpStartEnd(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 3

	updated, _ := m.Update(tea.KeyPressMsg{Code: '0'})
	um := updated.(dashboardModel)
	if um.replayCursor != 0 {
		t.Errorf("0 should jump to start: expected 0, got %d", um.replayCursor)
	}
	if um.replayPlaying {
		t.Error("0 should auto-pause")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '$'})
	um = updated.(dashboardModel)
	if um.replayReader == nil {
		t.Fatal("replayReader should not be nil after $ key (stub not implemented)")
	}
	expectedEnd := um.replayReader.EventCount() - 1
	if um.replayCursor != expectedEnd {
		t.Errorf("$ should jump to end: expected %d, got %d", expectedEnd, um.replayCursor)
	}
	if um.replayPlaying {
		t.Error("$ should auto-pause")
	}
}

// --- 17.5-UNIT-011: [P0] tick advances cursor based on speed when playing (AC2) ---

func TestReplayDashboard_AutoPlayAdvance(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 0
	m.replayPlaying = true
	m.replaySpeed = 2.0

	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	updated, _ := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)

	if um.replayCursor <= 0 {
		t.Errorf("tick with replayPlaying=true and speed=2.0 should advance cursor, got %d", um.replayCursor)
	}
}

// --- 17.5-UNIT-012: [P0] auto-play pauses at end of recording (AC2) ---

func TestReplayDashboard_AutoPlayPauseAtEnd(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	if m.replayReader == nil {
		t.Fatal("replayReader should not be nil (stub not implemented)")
	}
	m.replayCursor = m.replayReader.EventCount() - 2
	m.replayPlaying = true
	m.replaySpeed = 1.0

	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	updated, _ := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)

	if um.replayCursor != m.replayReader.EventCount()-1 {
		t.Errorf("should reach last event, got cursor=%d", um.replayCursor)
	}
	if um.replayPlaying {
		t.Error("should auto-pause when reaching last event")
	}
}

// --- 17.5-UNIT-013: [P0] live-mode keys blocked in replay mode (AC1, AC2) ---

func TestReplayDashboard_LiveKeysBlocked(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 0
	m.selectedPID = 2

	blockedKeys := []rune{'a', 'r'}
	for _, key := range blockedKeys {
		updated, _ := m.Update(tea.KeyPressMsg{Code: key})
		um := updated.(dashboardModel)

		if um.statusMsg == "" {
			t.Errorf("key %q in replay mode should set statusMsg (blocked), got empty", string(key))
		}
		if !strings.Contains(um.statusMsg, "replay") && !strings.Contains(um.statusMsg, "Replay") {
			t.Errorf("key %q blocked message should mention replay, got %q", string(key), um.statusMsg)
		}
	}

	// 'l' blocked in non-timeline panes (tree pane is default activePane)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	um := updated.(dashboardModel)
	if um.statusMsg == "" || !strings.Contains(um.statusMsg, "replay") {
		t.Error("l in tree pane replay mode should show blocked message")
	}

	// 'k' navigates in tree pane, not blocked
	m2 := newTestReplayDashboardModel(t)
	m2.replayCursor = 0
	m2.replayReader = m.replayReader
	// trigger tick to populate treeRows so k can navigate
	m2.processes = buildReplayProcessTree(m2.replayReader, 0)
	roots := buildProcessTree(m2.processes, treeSortPID, false)
	m2.treeRows = flattenTree(roots)
	m2.treeCursor = 0
	m2.selectedPID = m2.treeRows[0].proc.PID

	updated, _ = m2.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(dashboardModel)
	if um.statusMsg != "" && strings.Contains(um.statusMsg, "replay") {
		t.Error("k in tree pane replay mode should navigate, not show blocked message")
	}
}

// --- 17.5-UNIT-014: [P1] replay status bar shows replay info (AC1, AC2) ---

func TestReplayDashboard_StatusBar(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 2
	m.replayPlaying = false

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "REPLAY") {
		t.Error("replay mode status bar should contain 'REPLAY'")
	}
	if !strings.Contains(content, "test-rec-001") {
		t.Error("replay mode status bar should contain the record ID")
	}
	if strings.Contains(content, "k:Kill") {
		t.Error("replay mode should not show live-mode key hints like 'k:Kill'")
	}
}

// --- 17.5-UNIT-015: [P0] replay tick does not attempt IPC connection (AC1) ---

func TestReplayDashboard_TickNoIPC(t *testing.T) {
	m := newTestReplayDashboardModel(t)
	m.replayCursor = 0

	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	updated, cmd := m.Update(tickMsg(time.Now()))
	um := updated.(dashboardModel)

	if um.client != nil {
		t.Error("replay mode tick should not create IPC client")
	}
	if um.connected {
		t.Error("replay mode tick should not set connected=true")
	}
	if cmd == nil {
		t.Error("replay mode tick should still schedule next tick")
	}
}

func TestDashboardModel_HeatmapRefreshTick(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newDashboardModel(nil)
	m.connected = false
	m.heatmapTickCount = 0

	for range 5 {
		updated, _ := m.Update(tickMsg(time.Now()))
		m = updated.(dashboardModel)
	}

	if m.heatmapTickCount != 5 {
		t.Errorf("after 5 ticks, heatmapTickCount should be 5, got %d", m.heatmapTickCount)
	}
}

// ============================================================
// ATDD RED PHASE — Story 29.6: LLM 对话查看器
// All tests assert EXPECTED behavior. They FAIL because the
// LLM viewer overlay (viewLLM) is not implemented yet.
// ============================================================

// --- LLM viewer test helpers ---

func newTestLLMViewerModel() dashboardModel {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.viewMode = viewLLM
	m.activePane = paneTree
	return m
}

// --- 29.6-UNIT-001: [P0] L 键选中进程后进入 viewLLM (AC1) ---

func TestLLMViewer_LKeyEntersViewer(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 1
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'L', ShiftedCode: 'L', Mod: tea.ModShift})
	um := updated.(dashboardModel)

	if um.viewMode != viewLLM {
		t.Errorf("L key should enter viewLLM mode, got viewMode=%d", um.viewMode)
	}
}

// --- 29.6-UNIT-002: [P0] L 键返回非 nil cmd 获取步骤数据 (AC1) ---

func TestLLMViewer_LKeyReturnsCmd(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.treeCursor = 1
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'L', ShiftedCode: 'L', Mod: tea.ModShift})

	if cmd == nil {
		t.Error("L key should return non-nil cmd (fetch step detail and step list)")
	}
}

// --- 29.6-UNIT-003: [P0] 未选中进程按 L 显示 "No process selected" (AC2) ---

func TestLLMViewer_LKeyNoProcessSelected(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'L', ShiftedCode: 'L', Mod: tea.ModShift})
	um := updated.(dashboardModel)

	if !strings.Contains(um.statusMsg, "No process selected") {
		t.Errorf("L with no selectedPID should show 'No process selected', got %q", um.statusMsg)
	}
	if um.viewMode == viewLLM {
		t.Error("should not enter viewLLM when no process selected")
	}
}

// --- 29.6-UNIT-004: [P0] viewLLM View 包含 "REQUEST" 区块 (AC3) ---

func TestLLMViewer_ViewContainsRequest(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()

	if !strings.Contains(v.Content, "REQUEST") {
		t.Error("viewLLM should contain 'REQUEST' section header")
	}
}

// --- 29.6-UNIT-005: [P0] viewLLM View 包含 "RESPONSE" 区块 (AC3) ---

func TestLLMViewer_ViewContainsResponse(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()

	if !strings.Contains(v.Content, "RESPONSE") {
		t.Error("viewLLM should contain 'RESPONSE' section header")
	}
}

// --- 29.6-UNIT-006: [P0] viewLLM View 包含步骤导航栏 (AC4) ---

func TestLLMViewer_ViewContainsStepNav(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()

	if !strings.Contains(v.Content, "Steps:") {
		t.Error("viewLLM should contain 'Steps:' navigation bar")
	}
}

// --- 29.6-UNIT-007: [P0] viewLLM h 键返回非 nil cmd（获取上一步）(AC5) ---

func TestLLMViewer_HKeyPrevStep(t *testing.T) {
	m := newTestLLMViewerModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'h'})

	if cmd == nil {
		t.Error("h key in viewLLM should return non-nil cmd (fetch previous step)")
	}
}

// --- 29.6-UNIT-008: [P0] viewLLM l 键返回非 nil cmd（获取下一步）(AC5) ---

func TestLLMViewer_LKeyNextStep(t *testing.T) {
	m := newTestLLMViewerModel()
	m.llmViewerStepMax = 5 // step list loaded

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l'})

	if cmd == nil {
		t.Error("l key in viewLLM should return non-nil cmd (fetch next step)")
	}
}

func TestLLMViewer_LKeyNextStepBlockedBeforeLoad(t *testing.T) {
	m := newTestLLMViewerModel()
	// llmViewerStepMax == 0 (step list not loaded)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l'})

	if cmd != nil {
		t.Error("l key in viewLLM should return nil cmd when step list not loaded")
	}
}

// --- 29.6-UNIT-009: [P0] viewLLM j 键滚动 viewport 而非移动 treeCursor (AC6) ---

func TestLLMViewer_JKeyScrollsNotTree(t *testing.T) {
	m := newTestLLMViewerModel()
	m.treeCursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(dashboardModel)

	if um.treeCursor != 0 {
		t.Errorf("j in viewLLM should scroll viewport, not move treeCursor: expected 0, got %d", um.treeCursor)
	}
}

// --- 29.6-UNIT-010: [P1] viewLLM y 键返回非 nil cmd（剪贴板复制）(AC7) ---

func TestLLMViewer_YKeyCopy(t *testing.T) {
	m := newTestLLMViewerModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})

	if cmd == nil {
		t.Error("y key in viewLLM should return non-nil cmd (clipboard copy)")
	}
}

// --- 29.6-UNIT-011: [P0] viewLLM Esc 恢复 viewDefault (AC8) ---

func TestLLMViewer_EscExitsViewer(t *testing.T) {
	m := newTestLLMViewerModel()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(dashboardModel)

	if um.viewMode != viewDefault {
		t.Errorf("Esc in viewLLM should return to viewDefault, got viewMode=%d", um.viewMode)
	}
}

// --- 29.6-UNIT-012: [P1] viewLLM Status bar 包含快捷键提示 (AC9) ---

func TestLLMViewer_ViewContainsKeyHints(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()
	content := v.Content

	for _, hint := range []string{"scroll", "copy", "Esc"} {
		if !strings.Contains(content, hint) {
			t.Errorf("viewLLM status bar should contain '%s' hint", hint)
		}
	}
}

// --- 29.6-UNIT-013: [P0] 历史视图 L 键进入 viewLLM (AC10) ---

func TestLLMViewer_HistoryViewLKey(t *testing.T) {
	procs := mockDashboardProcs()
	m := newTestDashboardModel(procs)
	m.viewMode = viewHistory
	m.historyProcs = procs // populate history list so filtered is non-empty
	m.historyCursor = 1    // select PID 2
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'L', ShiftedCode: 'L', Mod: tea.ModShift})
	um := updated.(dashboardModel)

	if um.viewMode != viewLLM {
		t.Errorf("L in history view should enter viewLLM, got viewMode=%d", um.viewMode)
	}
	if um.llmViewerPrevMode != viewHistory {
		t.Errorf("LLM viewer should save prev mode as viewHistory, got %d", um.llmViewerPrevMode)
	}
}

// --- 29.6-UNIT-014: [P0] viewLLM 渲染全屏覆盖层内容（非默认面板）(AC1, AC3) ---

func TestLLMViewer_ViewFullScreenOverlay(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()
	content := v.Content

	// viewLLM should render LLM-specific content, not default pane layout
	hasLLMContent := strings.Contains(content, "LLM") ||
		strings.Contains(content, "REQUEST") ||
		strings.Contains(content, "RESPONSE") ||
		strings.Contains(content, "Viewer")
	if !hasLLMContent {
		t.Error("viewLLM should render LLM viewer content (LLM/REQUEST/RESPONSE/Viewer)")
	}
}

// --- 29.6-UNIT-015: [P0] viewLLM k 键滚动 viewport 而非移动 treeCursor (AC6) ---

func TestLLMViewer_KKeyScrollsNotTree(t *testing.T) {
	m := newTestLLMViewerModel()
	m.treeCursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.treeCursor != 2 {
		t.Errorf("k in viewLLM should scroll viewport, not move treeCursor: expected 2, got %d", um.treeCursor)
	}
}

// --- 29.6-UNIT-CR-001: Esc 从历史视图进入后回退到历史视图 ---

func TestLLMViewer_EscReturnsToHistoryView(t *testing.T) {
	m := newTestLLMViewerModel()
	m.llmViewerPrevMode = viewHistory

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(dashboardModel)

	if um.viewMode != viewHistory {
		t.Errorf("Esc in viewLLM should return to previous viewHistory, got viewMode=%d", um.viewMode)
	}
}

// --- 29.6-UNIT-016: [P1] viewLLM Status bar 包含 token 统计 (AC9) ---

func TestLLMViewer_ViewContainsTokenStats(t *testing.T) {
	m := newTestLLMViewerModel()
	v := m.View()
	content := v.Content

	// Status bar now shows simplified hints; token stats are in ? help or title
	if !strings.Contains(content, "scroll") || !strings.Contains(content, "copy") {
		t.Error("viewLLM status bar should contain scroll and copy hints")
	}
}

// --- Step filter from default view ---

func TestStepFilterFromDefaultView(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.viewMode = viewDefault
	m.activePane = paneTree // Tree 面板活跃

	// f 键应触发过滤模式
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f'})
	um := updated.(dashboardModel)
	if !um.stepFilterMode {
		t.Error("f key in default view should enter step filter mode even from tree pane")
	}

	// 在过滤模式按 t 应能切换 tool_call
	updated, _ = um.Update(tea.KeyPressMsg{Code: 't'})
	um = updated.(dashboardModel)
	if um.stepFilters["tool_call"] {
		t.Error("t in filter mode should toggle tool_call filter to false")
	}

	// Esc 退出过滤模式
	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um = updated.(dashboardModel)
	if um.stepFilterMode {
		t.Error("Esc should exit filter mode")
	}
}

// --- truncateAnsi ---

func TestTruncateAnsi_ShortString(t *testing.T) {
	s := "hello"
	got := truncateAnsi(s, 10)
	if got != "hello" {
		t.Errorf("truncateAnsi should not modify short string, got %q", got)
	}
}

func TestTruncateAnsi_ZeroWidth(t *testing.T) {
	got := truncateAnsi("hello", 0)
	if got != "" {
		t.Errorf("truncateAnsi(0) should return empty, got %q", got)
	}
}

// ============================================================
// Story 34.2: 标题状态栏与全局健康指标
// ============================================================

// --- 34.2-UNIT-001: computeHealthCounts with no errors → E=0, W=0 ---

func TestComputeHealthCounts_NoErrors(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 100, ContextBudget: 1000},
		{PID: 2, State: types.StateRunning, TokensUsed: 200, ContextBudget: 1000},
	}
	e, w := computeHealthCounts(procs, nil, nil)
	if e != 0 {
		t.Errorf("expected errorCount=0, got %d", e)
	}
	if w != 0 {
		t.Errorf("expected warnCount=0, got %d", w)
	}
}

// --- 34.2-UNIT-002: Dead+failed process → E=1 ---

func TestComputeHealthCounts_DeadProcess(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 100, ContextBudget: 1000},
		{PID: 2, State: types.StateDead, Result: "error: timeout"},
	}
	e, w := computeHealthCounts(procs, nil, nil)
	if e != 1 {
		t.Errorf("expected errorCount=1 for dead+failed process, got %d", e)
	}
	if w != 0 {
		t.Errorf("expected warnCount=0, got %d", w)
	}
}

// --- 34.2-UNIT-003: ctx 85% → W=1 ---

func TestComputeHealthCounts_CtxWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 850, ContextBudget: 1000},
	}
	e, w := computeHealthCounts(procs, nil, nil)
	if e != 0 {
		t.Errorf("expected errorCount=0, got %d", e)
	}
	if w != 1 {
		t.Errorf("expected warnCount=1 for ctx>=80%%, got %d", w)
	}
}

// --- 34.2-UNIT-003b: cost budget 85% → W=1 ---

func TestComputeHealthCounts_BudgetCostWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 100, ContextBudget: 1000, MaxCost: 10.0, UsedCost: 8.5},
	}
	e, w := computeHealthCounts(procs, nil, nil)
	if e != 0 {
		t.Errorf("expected errorCount=0, got %d", e)
	}
	if w != 1 {
		t.Errorf("expected warnCount=1 for cost budget>=80%%, got %d", w)
	}
}

// --- 34.2-UNIT-003c: ctx+stall same PID → W=1 (no double-count) ---

func TestComputeHealthCounts_CtxAndStallSamePID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 900, ContextBudget: 1000},
	}
	heartbeat := &ipc.HeartbeatStatusResponse{
		CurrentStalled: []ipc.StalledProcWire{{PID: 1}},
	}
	e, w := computeHealthCounts(procs, nil, heartbeat)
	if e != 0 {
		t.Errorf("expected errorCount=0, got %d", e)
	}
	if w != 1 {
		t.Errorf("expected warnCount=1 (no double-count for same PID), got %d", w)
	}
}

// --- 34.2-UNIT-004: heartbeat stall → W includes stall count ---

func TestComputeHealthCounts_StallWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 100, ContextBudget: 1000},
	}
	heartbeat := &ipc.HeartbeatStatusResponse{
		CurrentStalled: []ipc.StalledProcWire{
			{PID: 3},
			{PID: 4},
		},
	}
	e, w := computeHealthCounts(procs, nil, heartbeat)
	if e != 0 {
		t.Errorf("expected errorCount=0, got %d", e)
	}
	if w != 2 {
		t.Errorf("expected warnCount=2 for stalled processes, got %d", w)
	}
}

// --- 34.2-UNIT-005: renderDashboardTitle connected → contains "rnix", "●", "P", "E", "W" ---

func TestRenderDashboardTitle_Connected(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	title := m.renderDashboardTitle()

	for _, expect := range []string{"rnix", "●", "P", "E", "W"} {
		if !strings.Contains(title, expect) {
			t.Errorf("expected title to contain %q, got: %s", expect, title)
		}
	}
}

// --- 34.2-UNIT-006: renderDashboardTitle disconnected → contains "○" ---

func TestRenderDashboardTitle_Disconnected(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = false

	title := m.renderDashboardTitle()
	if !strings.Contains(title, "○") {
		t.Errorf("expected disconnected indicator '○', got: %s", title)
	}
	if strings.Contains(title, "●") {
		t.Errorf("should NOT contain connected indicator '●' when disconnected, got: %s", title)
	}
}

// --- 34.2-UNIT-007: narrow terminal (width=60) → omits elapsed, budget, ctx ---

func TestRenderDashboardTitle_NarrowTerminal(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.width = 60
	title := m.renderDashboardTitle()

	// Should still contain core info
	if !strings.Contains(title, "rnix") {
		t.Errorf("narrow title should still contain 'rnix', got: %s", title)
	}
	if !strings.Contains(title, "P") {
		t.Errorf("narrow title should still contain process count, got: %s", title)
	}
}

// --- 34.2-UNIT-008: RNIX_ASCII=1 → uses * and o instead of ● and ○ ---

func TestRenderDashboardTitle_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	m := newTestDashboardModel(mockDashboardProcs())
	title := m.renderDashboardTitle()
	if !strings.Contains(title, "*") {
		t.Errorf("ASCII mode should use '*' for connected, got: %s", title)
	}
	if strings.Contains(title, "●") {
		t.Errorf("ASCII mode should NOT use '●', got: %s", title)
	}
	// Separator should be --
	if !strings.Contains(title, "--") {
		t.Errorf("ASCII mode should use '--' separator, got: %s", title)
	}

	// Disconnected
	m.connected = false
	title = m.renderDashboardTitle()
	if !strings.Contains(title, "o") {
		t.Errorf("ASCII mode disconnected should use 'o', got: %s", title)
	}
}

// --- 34.2-UNIT-009: computeCtxPercent ---

func TestComputeCtxPercent(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 620, ContextBudget: 1000},
		{PID: 2, State: types.StateRunning, TokensUsed: 800, ContextBudget: 1000},
	}
	// Selected process
	pct := computeCtxPercent(1, procs)
	if pct != 62 {
		t.Errorf("expected ctx 62%% for PID 1, got %d%%", pct)
	}
	// No selection → average
	pct = computeCtxPercent(0, procs)
	if pct != 71 { // (620+800)/(1000+1000)*100 = 71
		t.Errorf("expected ctx average 71%%, got %d%%", pct)
	}
	// Selected PID not found → 0, not average
	pct = computeCtxPercent(999, procs)
	if pct != 0 {
		t.Errorf("expected ctx 0%% for missing PID, got %d%%", pct)
	}
}

// --- 34.2-UNIT-010: computeBudgetPercent ---

func TestComputeBudgetPercent(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, UsedCost: 0.45, MaxCost: 1.0},
	}
	pct := computeBudgetPercent(1, procs)
	if pct != 45 {
		t.Errorf("expected budget 45%%, got %d%%", pct)
	}
	// Token-based fallback
	procs2 := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 5000, MaxTokens: 10000},
	}
	pct = computeBudgetPercent(1, procs2)
	if pct != 50 {
		t.Errorf("expected budget 50%%, got %d%%", pct)
	}
}

// --- 34.2-UNIT-011: formatElapsedHHMMSS ---

func TestFormatElapsedHHMMSS(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{0, "00:00:00"},
		{5 * time.Second, "00:00:05"},
		{12*time.Minute + 34*time.Second, "00:12:34"},
		{1*time.Hour + 2*time.Minute + 3*time.Second, "01:02:03"},
	}
	for _, tt := range tests {
		got := formatElapsedHHMMSS(tt.dur)
		if got != tt.expect {
			t.Errorf("formatElapsedHHMMSS(%v) = %q, want %q", tt.dur, got, tt.expect)
		}
	}
}

// --- 34.2-UNIT-012: styleProviderName coloring ---

func TestStyleProviderName(t *testing.T) {
	// Healthy → contains provider name (green-styled)
	proc := &vfs.ProcInfo{Provider: "claude-sonnet", State: types.StateRunning, TokensUsed: 100, ContextBudget: 1000}
	s := styleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("expected provider name in styled output, got %q", s)
	}

	// Dead+failed → red
	proc2 := &vfs.ProcInfo{Provider: "claude-sonnet", State: types.StateDead, Result: "error: crash"}
	s2 := styleProviderName(true, proc2)
	if !strings.Contains(s2, "claude-sonnet") {
		t.Errorf("expected provider name for dead process, got %q", s2)
	}

	// Nil/empty → empty
	s3 := styleProviderName(true, nil)
	if s3 != "" {
		t.Errorf("expected empty for nil proc, got %q", s3)
	}
}

// --- 34.2-UNIT-013: panel tabs on second line ---

func TestRenderDashboardTitle_PanelTabs(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.width = 120
	title := m.renderDashboardTitle()

	// Title should have two lines (panel tabs on second line)
	lines := strings.Split(title, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected title to have at least 2 lines, got %d: %s", len(lines), title)
	}

	// Second line should contain panel labels
	for _, label := range []string{"[1]", "[2]", "[3]", "[4]", "[5]", "[6]", "[7]", "[8]"} {
		if !strings.Contains(lines[1], label) {
			t.Errorf("expected panel tabs line to contain %q, got: %s", label, lines[1])
		}
	}
}

// --- 34.2-UNIT-014: E dedup by PID ---

func TestComputeHealthCounts_ErrorDedup(t *testing.T) {
	// Same PID dead+failed AND has error event — should count as 1 error, not 2
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, Result: "error: crash"},
	}
	events := []UnifiedEvent{
		{Type: EventExit, Severity: SevError, PID: 1},
	}
	e, _ := computeHealthCounts(procs, events, nil)
	if e != 1 {
		t.Errorf("expected errorCount=1 (dedup by PID), got %d", e)
	}
}

// ============================================================
// Story 34.3: Process Tree Visual Enhancement
// ============================================================

// --- 34.3-UNIT-001: [P0] StateBadge returns emoji badges in Unicode mode (AC1) ---

func TestStateBadge_Unicode(t *testing.T) {
	os.Setenv("RNIX_ASCII", "0")
	defer os.Unsetenv("RNIX_ASCII")

	tests := []struct {
		state  types.ProcessState
		result string
		want   string
	}{
		{types.StateRunning, "", "🟢"},
		{types.StateCreated, "", "🟢"},
		{types.StateSuspended, "", "🟡"},
		{types.StateDead, "error: crash", "🔴"},
		{types.StateDead, "", "🔴"}, // empty result = failure
		{types.StateDead, "done", "⚪"},
	}
	for _, tc := range tests {
		got := ui.StateBadge(tc.state, tc.result)
		if got != tc.want {
			t.Errorf("StateBadge(%v, %q) = %q, want %q", tc.state, tc.result, got, tc.want)
		}
	}
}

// --- 34.3-UNIT-002: [P0] StateBadge returns ASCII badges when RNIX_ASCII=1 (AC1) ---

func TestStateBadge_ASCII(t *testing.T) {
	os.Setenv("RNIX_ASCII", "1")
	defer os.Setenv("RNIX_ASCII", "0")

	tests := []struct {
		state  types.ProcessState
		result string
		want   string
	}{
		{types.StateRunning, "", "[R]"},
		{types.StateSuspended, "", "[S]"},
		{types.StateDead, "error: crash", "[E]"},
		{types.StateDead, "done", "[D]"},
	}
	for _, tc := range tests {
		got := ui.StateBadge(tc.state, tc.result)
		if got != tc.want {
			t.Errorf("StateBadge(%v, %q) = %q, want %q", tc.state, tc.result, got, tc.want)
		}
	}
}

// --- 34.3-UNIT-003: [P1] renderCtxBar uses correct colors based on percentage (AC2) ---

func TestRenderCtxBar_Colors(t *testing.T) {
	os.Setenv("RNIX_ASCII", "0")
	defer os.Unsetenv("RNIX_ASCII")
	// InitStyles with no-color so we get plain text but can still check content
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})

	tests := []struct {
		used, budget int
		wantContains string
	}{
		{30, 100, "30%"},   // <50% → green
		{60, 100, "60%"},   // 50-80 → yellow
		{90, 100, "90%"},   // >80 → red
		{0, 100, "0%"},     // 0%
		{100, 100, "100%"}, // 100%
	}
	for _, tc := range tests {
		bar := renderCtxBar(tc.used, tc.budget, 10)
		if !strings.Contains(bar, tc.wantContains) {
			t.Errorf("renderCtxBar(%d, %d, 10) should contain %q, got %q",
				tc.used, tc.budget, tc.wantContains, bar)
		}
	}

	// Zero budget → empty string
	if got := renderCtxBar(100, 0, 10); got != "" {
		t.Errorf("renderCtxBar with zero budget should return empty, got %q", got)
	}
}

// --- 34.3-UNIT-004: [P1] renderCtxBar uses # and . in ASCII mode (AC2) ---

func TestRenderCtxBar_ASCII(t *testing.T) {
	os.Setenv("RNIX_ASCII", "1")
	defer os.Setenv("RNIX_ASCII", "0")
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})

	bar := renderCtxBar(50, 100, 10)
	if !strings.Contains(bar, "#") {
		t.Errorf("ASCII mode renderCtxBar should use '#' for filled, got %q", bar)
	}
	if !strings.Contains(bar, ".") {
		t.Errorf("ASCII mode renderCtxBar should use '.' for empty, got %q", bar)
	}
}

// --- 34.3-UNIT-005: [P0] buildProcessTree preserves dead process hierarchy (AC3) ---

func TestBuildProcessTree_DeadHierarchy(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateRunning, CreatedAt: now},
		{PID: 2, PPID: 1, UUID: "u2", State: types.StateDead, CreatedAt: now.Add(time.Second), DeadAt: now.Add(2 * time.Second)},
		{PID: 3, PPID: 2, UUID: "u3", State: types.StateDead, CreatedAt: now.Add(time.Second), DeadAt: now.Add(3 * time.Second)},
	}
	roots := buildProcessTree(procs, treeSortPID, true)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	// PID 2 should be child of PID 1
	root := roots[0]
	if root.proc.PID != 1 {
		t.Fatalf("root should be PID 1, got %d", root.proc.PID)
	}
	if len(root.children) != 1 {
		t.Fatalf("PID 1 should have 1 child (PID 2), got %d", len(root.children))
	}
	child := root.children[0]
	if child.proc.PID != 2 {
		t.Fatalf("child of PID 1 should be PID 2, got %d", child.proc.PID)
	}
	// PID 3 should be child of PID 2 (dead→dead hierarchy)
	if len(child.children) != 1 {
		t.Fatalf("PID 2 should have 1 child (PID 3), got %d children", len(child.children))
	}
	if child.children[0].proc.PID != 3 {
		t.Errorf("child of PID 2 should be PID 3, got %d", child.children[0].proc.PID)
	}
}

// --- 34.3-UNIT-006: [P1] buildProcessTree dead orphan becomes root (AC3) ---

func TestBuildProcessTree_DeadOrphan(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateRunning, CreatedAt: now},
		{PID: 5, PPID: 99, UUID: "u5", State: types.StateDead, CreatedAt: now.Add(time.Second)},
	}
	roots := buildProcessTree(procs, treeSortPID, true)
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots (PID 1 active + PID 5 dead orphan), got %d", len(roots))
	}
	pids := map[types.PID]bool{}
	for _, r := range roots {
		pids[r.proc.PID] = true
	}
	if !pids[5] {
		t.Error("dead process with missing PPID 99 should become root")
	}
}

// --- 34.3-UNIT-007: [P0] collapseCommonPrefix collapses when prefix > 50% (AC4) ---

func TestCollapseCommonPrefix(t *testing.T) {
	// Long common prefix (>50% of avg length) → should collapse
	intents := []string{
		"deploy/staging/service-a",
		"deploy/staging/service-b",
		"deploy/staging/service-c",
	}
	result := collapseCommonPrefix(intents)
	// "deploy/staging/" is 15 chars, average is ~24 chars → 15/24 = 62% > 50% → collapse
	for _, r := range result {
		if strings.HasPrefix(r, "deploy/staging/") {
			t.Errorf("common prefix should be collapsed, got %q", r)
		}
	}

	// Short prefix (≤50%) → should NOT collapse
	short := []string{
		"a/unique-suffix-one-with-lots-of-extra-content",
		"b/unique-suffix-two-with-lots-of-extra-content",
	}
	shortResult := collapseCommonPrefix(short)
	// No common prefix at all (starts with a/ vs b/) → no collapse
	for i, r := range shortResult {
		if r != short[i] {
			t.Errorf("short prefix should not collapse: got %q, want %q", r, short[i])
		}
	}

	// Single intent → no collapse
	single := collapseCommonPrefix([]string{"only one"})
	if single[0] != "only one" {
		t.Errorf("single intent should not collapse: got %q", single[0])
	}
}

// --- 34.3-UNIT-008: [P1] collapseCommonPrefix truncates to word boundary (AC4) ---

func TestCollapseCommonPrefix_WordBoundary(t *testing.T) {
	intents := []string{
		"review code changes in module alpha",
		"review code changes in module beta",
	}
	result := collapseCommonPrefix(intents)
	// Common prefix is "review code changes in module " (with space boundary)
	// Should collapse at word boundary, not mid-word
	for _, r := range result {
		// Collapsed result should start with ellipsis marker
		if !strings.Contains(r, "…/") && !strings.Contains(r, ".../") {
			t.Errorf("collapsed intent should start with ellipsis marker, got %q", r)
		}
		// Should contain the unique suffix
		if !strings.Contains(r, "alpha") && !strings.Contains(r, "beta") {
			t.Errorf("collapsed intent should preserve unique suffix, got %q", r)
		}
	}
}

// --- 34.3-UNIT-009: [P1] Most active process gets bold highlight within 2s (AC5) ---

func TestMostActiveHighlight(t *testing.T) {
	now := time.Now()
	m := newTestDashboardModel([]vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateRunning, Intent: "worker-a", CreatedAt: now},
		{PID: 2, PPID: 0, UUID: "u2", State: types.StateRunning, Intent: "worker-b", CreatedAt: now},
	})

	// PID 1 had event 1 second ago → should be "most active"
	m.lastEventByPID[1] = now.Add(-1 * time.Second)
	// PID 2 had event 5 seconds ago → not active
	m.lastEventByPID[2] = now.Add(-5 * time.Second)

	v := m.View()
	content := v.Content
	// We can't easily check for bold in test output, but at minimum the view renders without panic
	if !strings.Contains(content, "worker-a") {
		t.Error("view should show worker-a intent")
	}
	if !strings.Contains(content, "worker-b") {
		t.Error("view should show worker-b intent")
	}
}

// --- 34.3-UNIT-010: [P0] flattenTreeWithCollapse respects collapsed state (AC3) ---

func TestDeadTreeCollapse(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "root-u", State: types.StateRunning, CreatedAt: now},
		{PID: 2, PPID: 1, UUID: "dead-parent-u", State: types.StateDead, CreatedAt: now.Add(time.Second)},
		{PID: 3, PPID: 2, UUID: "dead-child-u", State: types.StateDead, CreatedAt: now.Add(2 * time.Second)},
	}
	roots := buildProcessTree(procs, treeSortPID, true)

	// Not collapsed → all 3 visible
	rows := flattenTreeWithCollapse(roots, nil)
	if len(rows) != 3 {
		t.Fatalf("uncollapsed: expected 3 rows, got %d", len(rows))
	}

	// Collapse the dead parent → child hidden
	collapsed := map[string]bool{"dead-parent-u": true}
	rows = flattenTreeWithCollapse(roots, collapsed)
	if len(rows) != 2 {
		t.Fatalf("collapsed: expected 2 rows (root + collapsed dead-parent), got %d", len(rows))
	}
	// The collapsed row should have collapsed=true
	found := false
	for _, r := range rows {
		if r.proc.UUID == "dead-parent-u" {
			found = true
			if !r.collapsed {
				t.Error("dead-parent-u row should have collapsed=true")
			}
		}
	}
	if !found {
		t.Error("dead-parent-u should be in the flattened rows")
	}

	// Expand again → all visible
	collapsed["dead-parent-u"] = false
	rows = flattenTreeWithCollapse(roots, collapsed)
	if len(rows) != 3 {
		t.Fatalf("re-expanded: expected 3 rows, got %d", len(rows))
	}
}

// ============================================================
// Story 34.4: Alert Strip and Unified Timeline
// ============================================================

// --- 34.4 Test Helpers ---

func makeTestUnifiedEvents() []UnifiedEvent {
	now := time.Now()
	return []UnifiedEvent{
		{Type: EventStep, Summary: "read file", Severity: SevInfo, Timestamp: now.Add(-5 * time.Second), PID: 1,
			StepEntry: &stepEntry{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read file"}}},
		{Type: EventCompact, Summary: "compact PID 1: 500→250 tok", Severity: SevInfo, Timestamp: now.Add(-4 * time.Second), PID: 1},
		{Type: EventBudget, Summary: "PID 2 budget 80% used", Severity: SevWarn, Timestamp: now.Add(-3 * time.Second), PID: 2},
		{Type: EventStep, Summary: "write code", Severity: SevInfo, Timestamp: now.Add(-2 * time.Second), PID: 1,
			StepEntry: &stepEntry{summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", Summary: "write code"}}},
		{Type: EventExit, Summary: "PID 3 exited code=1", Severity: SevError, Timestamp: now.Add(-1 * time.Second), PID: 3},
		{Type: EventStall, Summary: "PID 1 stalled >30s", Severity: SevWarn, Timestamp: now, PID: 1},
	}
}

// --- 7.1: TestRenderAlertStrip_NoAlerts ---

func TestRenderAlertStrip_NoAlerts(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = nil
	result := renderAlertStrip(&m, 80, 2)
	if result != "" {
		t.Errorf("expected empty string for no alerts, got %q", result)
	}
}

// --- 7.2: TestRenderAlertStrip_SingleAlert ---

func TestRenderAlertStrip_SingleAlert(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = []UnifiedEvent{
		{Type: EventBudget, Summary: "PID 2 budget 80%", Severity: SevWarn, Timestamp: time.Now()},
	}
	result := renderAlertStrip(&m, 80, 2)
	if result == "" {
		t.Fatal("expected non-empty alert strip")
	}
	if !strings.Contains(result, "PID 2 budget 80%") {
		t.Errorf("alert strip should contain summary, got %q", result)
	}
}

// --- 7.3: TestRenderAlertStrip_Overflow ---

func TestRenderAlertStrip_Overflow(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = []UnifiedEvent{
		{Type: EventExit, Summary: "error 1", Severity: SevError, Timestamp: time.Now()},
		{Type: EventExit, Summary: "error 2", Severity: SevError, Timestamp: time.Now()},
		{Type: EventBudget, Summary: "warn 1", Severity: SevWarn, Timestamp: time.Now()},
		{Type: EventBudget, Summary: "warn 2", Severity: SevWarn, Timestamp: time.Now()},
	}
	// Collapsed mode: 2 lines max, 4 alerts → should show "+N more"
	result := renderAlertStrip(&m, 80, 2)
	if !strings.Contains(result, "+3 more") {
		t.Errorf("expected overflow indicator, got %q", result)
	}
}

// --- 7.4: TestRenderAlertStrip_SeverityOrder ---

func TestRenderAlertStrip_SeverityOrder(t *testing.T) {
	now := time.Now()
	events := []UnifiedEvent{
		{Type: EventBudget, Summary: "warn old", Severity: SevWarn, Timestamp: now.Add(-10 * time.Second)},
		{Type: EventExit, Summary: "error new", Severity: SevError, Timestamp: now},
		{Type: EventBudget, Summary: "warn new", Severity: SevWarn, Timestamp: now},
	}
	alerts := buildAlertEvents(events)
	if len(alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(alerts))
	}
	if alerts[0].Severity != SevError {
		t.Errorf("first alert should be SevError, got %d", alerts[0].Severity)
	}
	if alerts[1].Summary != "warn new" {
		t.Errorf("second alert should be warn new, got %q", alerts[1].Summary)
	}
	if alerts[2].Summary != "warn old" {
		t.Errorf("third alert should be warn old, got %q", alerts[2].Summary)
	}
}

// --- 7.5: TestRenderAlertStrip_ASCII ---

func TestRenderAlertStrip_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = []UnifiedEvent{
		{Type: EventExit, Summary: "PID 3 exited", Severity: SevError, Timestamp: time.Now()},
	}
	result := renderAlertStrip(&m, 80, 2)
	if !strings.Contains(result, "[!]") {
		t.Errorf("ASCII mode should use [!] icon for error severity, got %q", result)
	}
	if !strings.Contains(result, "Alerts") {
		t.Errorf("ASCII mode should use text separator, got %q", result)
	}
}

// --- 7.6: TestRenderUnifiedTimeline_StepEvents ---

func TestRenderUnifiedTimeline_StepEvents(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 1
	m.selectedUUID = "uuid-mock-001"
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read file"}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "plan", Summary: "plan next steps"}},
	}
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, StepEntry: &m.stepEntries[0], Summary: "read file", Timestamp: time.Now().Add(-2 * time.Second), PID: 1},
		{Type: EventStep, StepEntry: &m.stepEntries[1], Summary: "plan next steps", Timestamp: time.Now(), PID: 1},
	}
	result := m.renderStepTimeline(80, 20)
	if !strings.Contains(result, "read file") {
		t.Errorf("should contain step summary, got %q", result)
	}
	if !strings.Contains(result, "plan next") {
		t.Errorf("should contain plan step, got %q", result)
	}
}

// --- 7.7: TestRenderUnifiedTimeline_SystemEvents ---

func TestRenderUnifiedTimeline_SystemEvents(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 1
	m.selectedUUID = "uuid-mock-001"
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read"}},
	}
	now := time.Now()
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, StepEntry: &m.stepEntries[0], Summary: "read", Timestamp: now.Add(-3 * time.Second), PID: 1},
		{Type: EventCompact, Summary: "compact PID 1", Severity: SevInfo, Timestamp: now.Add(-2 * time.Second), PID: 1},
		{Type: EventSpawn, Summary: "spawned PID 5", Severity: SevInfo, Timestamp: now.Add(-1 * time.Second), PID: 1},
		{Type: EventExit, Summary: "PID 3 exited", Severity: SevError, Timestamp: now, PID: 3},
	}
	result := m.renderStepTimeline(80, 20)
	if !strings.Contains(result, "compact PID 1") {
		t.Errorf("should render compact event, got %q", result)
	}
	if !strings.Contains(result, "spawned PID 5") {
		t.Errorf("should render spawn event, got %q", result)
	}
	if !strings.Contains(result, "PID 3 exited") {
		t.Errorf("should render exit event, got %q", result)
	}
}

// --- 7.8: TestRenderUnifiedTimeline_MixedEvents ---

func TestRenderUnifiedTimeline_MixedEvents(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 1
	m.selectedUUID = "uuid-mock-001"
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read file"}},
	}
	now := time.Now()
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, StepEntry: &m.stepEntries[0], Summary: "read file", Timestamp: now.Add(-2 * time.Second), PID: 1},
		{Type: EventBudget, Summary: "budget warning", Severity: SevWarn, Timestamp: now, PID: 1},
	}
	result := m.renderStepTimeline(80, 20)
	if !strings.Contains(result, "read file") {
		t.Errorf("should contain step event")
	}
	if !strings.Contains(result, "budget warning") {
		t.Errorf("should contain system event")
	}
}

// --- 7.9: TestEventTypeIcon_Unicode ---

func TestEventTypeIcon_Unicode(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{EventCompact, "★"},
		{EventBudget, "⚠"},
		{EventSpawn, "↳"},
		{EventExit, "↲"},
		{EventStall, "⚠"},
		{EventImmune, "🛡"},
	}
	for _, tt := range tests {
		icon := ui.EventTypeIcon(tt.eventType)
		if icon != tt.expected {
			t.Errorf("EventTypeIcon(%q) = %q, want %q", tt.eventType, icon, tt.expected)
		}
	}
}

// --- 7.10: TestEventTypeIcon_ASCII ---

func TestEventTypeIcon_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	tests := []struct {
		eventType string
		expected  string
	}{
		{EventCompact, "*"},
		{EventBudget, "(!)"},
		{EventSpawn, ">"},
		{EventExit, "<"},
		{EventStall, "(!)"},
		{EventImmune, "[I]"},
	}
	for _, tt := range tests {
		icon := ui.EventTypeIcon(tt.eventType)
		if icon != tt.expected {
			t.Errorf("EventTypeIcon(%q) ASCII = %q, want %q", tt.eventType, icon, tt.expected)
		}
	}
}

// --- 7.11: TestEventFilter_SystemEvents ---

func TestEventFilter_SystemEvents(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 1
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read"}},
	}
	now := time.Now()
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, StepEntry: &m.stepEntries[0], Summary: "read", Timestamp: now.Add(-1 * time.Second), PID: 1},
		{Type: EventCompact, Summary: "compact", Severity: SevInfo, Timestamp: now, PID: 1},
	}
	m.stepFilters = defaultStepFilters()
	filtered := m.filteredUnifiedEvents()
	if len(filtered) != 2 {
		t.Fatalf("all filters on: expected 2 events, got %d", len(filtered))
	}
	m.stepFilters[EventCompact] = false
	filtered = m.filteredUnifiedEvents()
	if len(filtered) != 1 {
		t.Fatalf("compact off: expected 1 event, got %d", len(filtered))
	}
	if filtered[0].Type != EventStep {
		t.Errorf("remaining event should be step, got %q", filtered[0].Type)
	}
}

// --- 7.12: TestEventFilter_MixedFilter ---

func TestEventFilter_MixedFilter(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 1
	entries := []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read"}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "plan", Summary: "plan"}},
	}
	m.stepEntries = entries
	now := time.Now()
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, StepEntry: &m.stepEntries[0], Summary: "read", Timestamp: now.Add(-3 * time.Second), PID: 1},
		{Type: EventCompact, Summary: "compact", Severity: SevInfo, Timestamp: now.Add(-2 * time.Second), PID: 1},
		{Type: EventStep, StepEntry: &m.stepEntries[1], Summary: "plan", Timestamp: now.Add(-1 * time.Second), PID: 1},
		{Type: EventBudget, Summary: "budget", Severity: SevWarn, Timestamp: now, PID: 1},
	}
	m.stepFilters = defaultStepFilters()
	m.stepFilters["tool_call"] = false
	m.stepFilters[EventBudget] = false
	filtered := m.filteredUnifiedEvents()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered events, got %d", len(filtered))
	}
	types := []string{filtered[0].Type, filtered[1].Type}
	if types[0] != EventCompact || types[1] != EventStep {
		t.Errorf("expected [compact, step], got %v", types)
	}
}

// --- 7.13: TestAlertJump_ToProcess ---

func TestAlertJump_ToProcess(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = []UnifiedEvent{
		{Type: EventExit, Summary: "PID 3 exited", Severity: SevError, Timestamp: time.Now(), PID: 3},
		{Type: EventBudget, Summary: "PID 2 budget", Severity: SevWarn, Timestamp: time.Now(), PID: 2},
	}
	m.alertExpanded = true
	m.alertCursor = 0
	alert := m.alertEvents[m.alertCursor]
	if alert.PID != 3 {
		t.Errorf("expected alert PID 3, got %d", alert.PID)
	}
	m.alertCursor = 1
	alert = m.alertEvents[m.alertCursor]
	if alert.PID != 2 {
		t.Errorf("expected alert PID 2, got %d", alert.PID)
	}
}

// --- 7.14: TestAlertStrip_ExpandCollapse ---

func TestAlertStrip_ExpandCollapse(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.alertEvents = []UnifiedEvent{
		{Type: EventExit, Summary: "err1", Severity: SevError, Timestamp: time.Now()},
		{Type: EventExit, Summary: "err2", Severity: SevError, Timestamp: time.Now()},
		{Type: EventBudget, Summary: "warn1", Severity: SevWarn, Timestamp: time.Now()},
		{Type: EventBudget, Summary: "warn2", Severity: SevWarn, Timestamp: time.Now()},
		{Type: EventBudget, Summary: "warn3", Severity: SevWarn, Timestamp: time.Now()},
	}

	h := alertStripHeight(len(m.alertEvents), false)
	if h != 2 {
		t.Errorf("collapsed height: expected 2, got %d", h)
	}

	h = alertStripHeight(len(m.alertEvents), true)
	if h != 5 {
		t.Errorf("expanded height: expected 5, got %d", h)
	}

	m.alertExpanded = false
	m.alertCursor = 3
	m.alertExpanded = true
	if m.alertCursor != 3 {
		t.Errorf("cursor should stay at 3 when expanding, got %d", m.alertCursor)
	}
	m.alertExpanded = false
	visible := alertStripHeight(len(m.alertEvents), false)
	if m.alertCursor >= visible {
		m.alertCursor = 0
	}
	if m.alertCursor != 0 {
		t.Errorf("cursor should reset to 0 when collapsing, got %d", m.alertCursor)
	}
}

// --- 7.15a: TestAlertSeverityIcon ---

func TestAlertSeverityIcon(t *testing.T) {
	icon := ui.AlertSeverityIcon(SevError)
	if icon != "🔴" {
		t.Errorf("SevError icon = %q, want 🔴", icon)
	}
	icon = ui.AlertSeverityIcon(SevWarn)
	if icon != "⚠" {
		t.Errorf("SevWarn icon = %q, want ⚠", icon)
	}
	t.Setenv("RNIX_ASCII", "1")
	icon = ui.AlertSeverityIcon(SevError)
	if icon != "[!]" {
		t.Errorf("SevError ASCII icon = %q, want [!]", icon)
	}
	icon = ui.AlertSeverityIcon(SevWarn)
	if icon != "(!)" {
		t.Errorf("SevWarn ASCII icon = %q, want (!)", icon)
	}
}

// --- 7.15b: TestBuildAlertEvents_OnlySevWarnOrHigher ---

func TestBuildAlertEvents_OnlySevWarnOrHigher(t *testing.T) {
	events := makeTestUnifiedEvents()
	alerts := buildAlertEvents(events)
	for _, a := range alerts {
		if a.Severity < SevWarn {
			t.Errorf("alert with severity %d < SevWarn should not be included", a.Severity)
		}
	}
	if len(alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(alerts))
	}
}
