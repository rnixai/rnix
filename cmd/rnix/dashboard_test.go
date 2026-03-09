package main

import (
	"bytes"
	"fmt"
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

// ============================================================
// ATDD RED PHASE — Story 17.2: 追踪时间线窗格
// All tests assert EXPECTED behavior. They FAIL because the
// functions/fields do not exist yet (stubs not implemented).
// ============================================================

// --- timeline test helpers ---

func mockTimelineEvents() []ipc.SyscallEventWire {
	return []ipc.SyscallEventWire{
		{TimestampMs: 100, PID: 2, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}, DurationMs: 1.5},
		{TimestampMs: 200, PID: 2, Syscall: "Spawn", Args: map[string]any{"intent": "build"}, DurationMs: 5.0},
		{TimestampMs: 300, PID: 2, Syscall: "Send", Args: map[string]any{"target_pid": 3}, DurationMs: 0.5},
		{TimestampMs: 400, PID: 2, Syscall: "CtxAlloc", Args: map[string]any{"size": 64}, DurationMs: 0.2},
		{TimestampMs: 500, PID: 2, Syscall: "Read", Args: map[string]any{"fd": 3}, Error: "EOF", DurationMs: 0.1},
	}
}

func newTestTimelineDashboardModel() dashboardModel {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.activePane = paneTimeline
	events := mockTimelineEvents()
	for _, ev := range events {
		m.timelineEvents = append(m.timelineEvents, timelineEvent{
			wire:     ev,
			category: classifySyscall(ev),
		})
	}
	m.timelineFilters = defaultTimelineFilters()
	return m
}

// --- 17.2-UNIT-001: [P0] classifySyscall — LLM event (AC1) ---

func TestClassifySyscall_LLM(t *testing.T) {
	ev := ipc.SyscallEventWire{
		Syscall: "Open",
		Args:    map[string]any{"path": "/dev/llm/claude"},
	}
	cat := classifySyscall(ev)
	if cat != catLLM {
		t.Errorf("Open(/dev/llm/claude) should be catLLM, got %d", cat)
	}

	ev2 := ipc.SyscallEventWire{
		Syscall: "Write",
		Args:    map[string]any{"tool": "/dev/llm/claude"},
	}
	cat2 := classifySyscall(ev2)
	if cat2 != catLLM {
		t.Errorf("Write(tool=/dev/llm/claude) should be catLLM, got %d", cat2)
	}
}

// --- 17.2-UNIT-002: [P0] classifySyscall — IPC event (AC1) ---

func TestClassifySyscall_IPC(t *testing.T) {
	for _, syscall := range []string{"Send", "Recv", "Pipe", "Signal", "SigBlock", "SigUnblock", "JoinGroup", "LeaveGroup", "GetProcGroup", "SignalGroup"} {
		ev := ipc.SyscallEventWire{Syscall: syscall}
		cat := classifySyscall(ev)
		if cat != catIPC {
			t.Errorf("%s should be catIPC, got %d", syscall, cat)
		}
	}
}

// --- 17.2-UNIT-003: [P0] classifySyscall — Tool event (AC1) ---

func TestClassifySyscall_Tool(t *testing.T) {
	for _, syscall := range []string{"Spawn", "Kill", "Wait", "Reparent", "SpawnThread", "JoinThread", "SpawnSupervisor", "SpawnCoroutine", "Yield", "ResumeCoroutine"} {
		ev := ipc.SyscallEventWire{Syscall: syscall}
		cat := classifySyscall(ev)
		if cat != catTool {
			t.Errorf("%s should be catTool, got %d", syscall, cat)
		}
	}

	evShell := ipc.SyscallEventWire{
		Syscall: "Open",
		Args:    map[string]any{"path": "/dev/shell/bash"},
	}
	if classifySyscall(evShell) != catTool {
		t.Error("Open(/dev/shell/bash) should be catTool")
	}

	evFs := ipc.SyscallEventWire{
		Syscall: "Read",
		Args:    map[string]any{"path": "/dev/fs/workspace"},
	}
	if classifySyscall(evFs) != catTool {
		t.Error("Read(/dev/fs/workspace) should be catTool")
	}
}

// --- 17.2-UNIT-004: [P0] classifySyscall — VFS event (AC1) ---

func TestClassifySyscall_VFS(t *testing.T) {
	for _, syscall := range []string{"Open", "Read", "Write", "Close", "Mount", "Unmount", "CtxAlloc", "CtxRead", "CtxWrite", "ReasonStep"} {
		ev := ipc.SyscallEventWire{Syscall: syscall}
		cat := classifySyscall(ev)
		if cat != catVFS {
			t.Errorf("%s (no special path) should be catVFS, got %d", syscall, cat)
		}
	}
}

// --- 17.2-UNIT-005: [P0] classifySyscall — error takes priority (AC1) ---

func TestClassifySyscall_ErrorPriority(t *testing.T) {
	ev := ipc.SyscallEventWire{
		Syscall: "Open",
		Args:    map[string]any{"path": "/dev/llm/claude"},
		Error:   "connection refused",
	}
	cat := classifySyscall(ev)
	if cat != catError {
		t.Errorf("event with error should be catError regardless of syscall, got %d", cat)
	}
}

// --- 17.2-UNIT-006: [P1] categoryColor returns correct color values (AC1) ---

func TestCategoryColor(t *testing.T) {
	tests := []struct {
		cat   eventCategory
		color string
	}{
		{catLLM, ui.ColorAgent},
		{catTool, ui.ColorSuccess},
		{catIPC, colorIPC},
		{catVFS, ui.ColorWarning},
		{catError, ui.ColorError},
	}
	for _, tt := range tests {
		got := categoryColor(tt.cat)
		if got != tt.color {
			t.Errorf("categoryColor(%d) = %q, want %q", tt.cat, got, tt.color)
		}
	}
}

// --- 17.2-UNIT-007: [P0] timelineEventMsg appends to timelineEvents (AC1) ---

func TestDashboardModel_TimelineEventAppend(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.timelineFilters = defaultTimelineFilters()

	ev := ipc.SyscallEventWire{
		TimestampMs: 100,
		PID:         2,
		Syscall:     "Open",
		Args:        map[string]any{"path": "/dev/llm/claude"},
	}

	updated, _ := m.Update(timelineEventMsg{event: ev})
	um := updated.(dashboardModel)

	if len(um.timelineEvents) != 1 {
		t.Fatalf("expected 1 timeline event, got %d", len(um.timelineEvents))
	}
	if um.timelineEvents[0].category != catLLM {
		t.Errorf("expected catLLM, got %d", um.timelineEvents[0].category)
	}
}

// --- 17.2-UNIT-008: [P0] timelineEvents FIFO eviction at 1000 (AC1) ---

func TestDashboardModel_TimelineEventsFIFO(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.timelineFilters = defaultTimelineFilters()

	for i := 0; i < maxTimelineEvents+50; i++ {
		ev := ipc.SyscallEventWire{
			TimestampMs: int64(i),
			PID:         2,
			Syscall:     "Read",
		}
		updated, _ := m.Update(timelineEventMsg{event: ev})
		m = updated.(dashboardModel)
	}

	if len(m.timelineEvents) != maxTimelineEvents {
		t.Fatalf("expected %d events after FIFO eviction, got %d", maxTimelineEvents, len(m.timelineEvents))
	}
	if m.timelineEvents[0].wire.TimestampMs != 50 {
		t.Errorf("oldest event should be timestamp 50 after eviction, got %d", m.timelineEvents[0].wire.TimestampMs)
	}
}

// --- 17.2-UNIT-009: [P0] timeline renders empty state text (AC1) ---

func TestDashboardModel_TimelineRenderEmpty(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 0
	m.activePane = paneTimeline
	m.timelineFilters = defaultTimelineFilters()

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "Select an agent") {
		t.Error("timeline with no PID should show 'Select an agent' prompt")
	}
	if !strings.Contains(content, "Timeline") {
		t.Error("timeline pane should still show 'Timeline' title")
	}
}

// --- 17.2-UNIT-010: [P0] timeline renders events without 'Coming Soon' (AC1) ---

func TestDashboardModel_TimelineRenderEvents(t *testing.T) {
	m := newTestTimelineDashboardModel()
	v := m.View()
	content := v.Content

	if !strings.Contains(content, "5 events") {
		t.Error("timeline with events should show event count")
	}
	if !strings.Contains(content, "Timeline") {
		t.Error("timeline pane should show 'Timeline' title")
	}
	if strings.Contains(content, "Select an agent") {
		t.Error("timeline with events should not show 'Select an agent'")
	}
}

// --- 17.2-UNIT-011: [P0] +/- adjusts zoomLevel (AC2) ---

func TestDashboardModel_TimelineZoom(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.timelineZoomLevel = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: '+'})
	um := updated.(dashboardModel)
	if um.timelineZoomLevel != 3 {
		t.Errorf("+ should increase zoomLevel: expected 3, got %d", um.timelineZoomLevel)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '-'})
	um = updated.(dashboardModel)
	if um.timelineZoomLevel != 2 {
		t.Errorf("- should decrease zoomLevel: expected 2, got %d", um.timelineZoomLevel)
	}

	um.timelineZoomLevel = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: '-'})
	um = updated.(dashboardModel)
	if um.timelineZoomLevel != 0 {
		t.Error("zoomLevel should not go below 0")
	}

	um.timelineZoomLevel = 5
	updated, _ = um.Update(tea.KeyPressMsg{Code: '+'})
	um = updated.(dashboardModel)
	if um.timelineZoomLevel != 5 {
		t.Error("zoomLevel should not go above 5")
	}
}

// --- 17.2-UNIT-012: [P0] 1-4 toggles category filter (AC2) ---

func TestDashboardModel_TimelineFilter(t *testing.T) {
	m := newTestTimelineDashboardModel()

	if !m.timelineFilters[catLLM] {
		t.Fatal("LLM filter should be true by default")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	um := updated.(dashboardModel)
	if um.timelineFilters[catLLM] {
		t.Error("pressing '1' should toggle LLM filter to false")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '1'})
	um = updated.(dashboardModel)
	if !um.timelineFilters[catLLM] {
		t.Error("pressing '1' again should toggle LLM filter back to true")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '2'})
	um = updated.(dashboardModel)
	if um.timelineFilters[catTool] {
		t.Error("pressing '2' should toggle Tool filter to false")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '3'})
	um = updated.(dashboardModel)
	if um.timelineFilters[catIPC] {
		t.Error("pressing '3' should toggle IPC filter to false")
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: '4'})
	um = updated.(dashboardModel)
	if um.timelineFilters[catVFS] {
		t.Error("pressing '4' should toggle VFS filter to false")
	}
}

// --- 17.2-UNIT-013: [P0] h/l scrolls timeline viewStart (AC2) ---

func TestDashboardModel_TimelineScroll(t *testing.T) {
	m := newTestTimelineDashboardModel()
	m.timelineViewStart = 500

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	um := updated.(dashboardModel)
	if um.timelineViewStart <= 500 {
		t.Errorf("l should scroll right (increase viewStart), got %d", um.timelineViewStart)
	}

	prevStart := um.timelineViewStart
	updated, _ = um.Update(tea.KeyPressMsg{Code: 'h'})
	um = updated.(dashboardModel)
	if um.timelineViewStart >= prevStart {
		t.Errorf("h should scroll left (decrease viewStart), got %d", um.timelineViewStart)
	}
}

// --- 17.2-UNIT-014: [P0] PID change clears timeline events (AC1) ---

func TestDashboardModel_TimelinePIDChange(t *testing.T) {
	m := newTestTimelineDashboardModel()
	if len(m.timelineEvents) == 0 {
		t.Fatal("pre-condition: model should have timeline events")
	}

	prevPID := m.selectedPID
	m.selectedPID = 999
	m.timelineAttachedPID = prevPID

	m = m.handleTimelinePIDChange()

	if len(m.timelineEvents) != 0 {
		t.Errorf("PID change should clear timelineEvents, got %d", len(m.timelineEvents))
	}
	if m.timelineAttachedPID != 999 {
		t.Errorf("timelineAttachedPID should update to new PID, got %d", m.timelineAttachedPID)
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

	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	um = updated.(dashboardModel)
	if um.heatmapCursor != 0 {
		t.Errorf("up arrow should move heatmapCursor up: expected 0, got %d", um.heatmapCursor)
	}

	um.heatmapCursor = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	um = updated.(dashboardModel)
	if um.heatmapCursor != 0 {
		t.Errorf("up arrow at top should stay at 0, got %d", um.heatmapCursor)
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

// --- 17.3-UNIT-011: [P1] Tab switches to paneHeatmap (AC1) ---

func TestDashboardModel_TabToHeatmap(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTimeline

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um := updated.(dashboardModel)
	if um.activePane != paneHeatmap {
		t.Errorf("Tab from timeline should switch to paneHeatmap, got %d", um.activePane)
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
	m2, _ := m.handlePIDChange()

	if len(m2.timelineEvents) != 0 {
		t.Errorf("handlePIDChange should clear timelineEvents, got %d", len(m2.timelineEvents))
	}
	if m2.heatmapProfile != nil {
		t.Error("handlePIDChange should clear heatmapProfile")
	}
	if len(m2.heatmapSegments) != 0 {
		t.Errorf("handlePIDChange should clear heatmapSegments, got %d", len(m2.heatmapSegments))
	}
	if m2.timelineAttachedPID != 999 {
		t.Errorf("timelineAttachedPID should be 999, got %d", m2.timelineAttachedPID)
	}
	if m2.heatmapPID != 999 {
		t.Errorf("heatmapPID should be 999, got %d", m2.heatmapPID)
	}
}

// --- 17.4-UNIT-004: [P0] Global kill — k in timeline triggers confirmKill (AC2) ---

func TestDashboardModel_GlobalKillConfirmTimeline(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.activePane = paneTimeline
	m.selectedPID = 2
	m.connected = true

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if !um.confirmKill {
		t.Error("k in timeline pane with selectedPID > 0 should trigger confirmKill")
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
	m.recording = make(map[types.PID]string)

	updated, _ := m.Update(recordToggleMsg{pid: 2, recordID: "rec-001"})
	um := updated.(dashboardModel)

	if um.recording[2] != "rec-001" {
		t.Errorf("recording[2] should be 'rec-001', got %q", um.recording[2])
	}
	if um.statusMsg == "" {
		t.Error("start recording should set statusMsg")
	}
}

// --- 17.4-UNIT-010: [P0] recordToggleMsg — stop recording clears map (AC2) ---

func TestRecordToggleMsg_Stop(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.recording = map[types.PID]string{2: "rec-001"}

	updated, _ := m.Update(recordToggleMsg{pid: 2, stopped: true, eventCount: 42})
	um := updated.(dashboardModel)

	if _, exists := um.recording[2]; exists {
		t.Error("stop recording should remove PID from recording map")
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
	m.recording = make(map[types.PID]string)

	updated, _ := m.Update(recordToggleMsg{pid: 2, err: fmt.Errorf("connection refused")})
	um := updated.(dashboardModel)

	if um.statusMsg == "" {
		t.Error("recordToggleMsg with error should set statusMsg")
	}
	if !strings.Contains(um.statusMsg, "connection refused") {
		t.Errorf("statusMsg should contain error, got %q", um.statusMsg)
	}
}

// --- 17.4-UNIT-012: [P1] Status bar shows ●REC when recording (AC2) ---

func TestDashboardModel_StatusBarRecording(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.recording = map[types.PID]string{2: "rec-001"}

	v := m.View()
	if !strings.Contains(v.Content, "●REC") {
		t.Error("status bar should show '●REC' when selected process is recording")
	}
}

// --- 17.4-UNIT-013: [P1] Status bar shows operation key hints (AC2) ---

func TestDashboardModel_StatusBarOperationKeys(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2

	v := m.View()
	content := v.Content

	for _, hint := range []string{"k:Kill", "a:GDB", "l:Log", "r:Record"} {
		if !strings.Contains(content, hint) {
			t.Errorf("status bar should contain '%s'", hint)
		}
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
	m.recording = map[types.PID]string{2: "rec-001"}

	v := m.View()

	lines := strings.Split(v.Content, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "PID 2") && strings.Contains(line, "●") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tree pane should show '●' indicator on the line for recording PID 2")
	}
}

func TestDashboardModel_HeatmapRefreshTick(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newDashboardModel(nil)
	m.connected = false
	m.heatmapTickCount = 0

	for i := 0; i < 5; i++ {
		updated, _ := m.Update(tickMsg(time.Now()))
		m = updated.(dashboardModel)
	}

	if m.heatmapTickCount != 5 {
		t.Errorf("after 5 ticks, heatmapTickCount should be 5, got %d", m.heatmapTickCount)
	}
}
