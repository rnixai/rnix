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

	for i := range maxTimelineEvents + 50 {
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
	m.timelineEventCursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(dashboardModel)

	if um.confirmKill {
		t.Error("k in timeline pane should navigate, not trigger kill")
	}
	if um.timelineEventCursor != 1 {
		t.Errorf("k in timeline should move cursor up: expected 1, got %d", um.timelineEventCursor)
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

// --- CR-FIX-009: [P0] a key returns non-nil cmd for GDB (AC2) ---

func TestDashboardModel_GlobalGDBKey(t *testing.T) {
	m := newTestHeatmapDashboardModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'a'})

	if cmd == nil {
		t.Error("a key with selectedPID > 0 should return non-nil cmd (ExecProcess for gdb)")
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
	if m.timelineFilters == nil {
		t.Error("timelineFilters should be initialized (defaultTimelineFilters)")
	}
	if m.recording == nil {
		t.Error("recording map should be initialized")
	}
}

// --- 17.5-UNIT-002: [P0] recordEventToWire converts syscall events correctly (AC1) ---

func TestRecordEventToWire(t *testing.T) {
	ev := debug.RecordEvent{
		SeqNum:    0,
		Timestamp: 100 * time.Millisecond,
		PID:       2,
		Type:      debug.RecordSyscall,
		Syscall: &debug.SyscallEventData{
			Syscall:  "Open",
			Args:     map[string]any{"path": "/dev/llm/claude"},
			Result:   "fd=3",
			Err:      "",
			Duration: 10 * time.Millisecond,
		},
	}

	wire := recordEventToWire(ev)

	if wire.TimestampMs != 100 {
		t.Errorf("TimestampMs should be 100, got %d", wire.TimestampMs)
	}
	if wire.PID != 2 {
		t.Errorf("PID should be 2, got %d", wire.PID)
	}
	if wire.Syscall != "Open" {
		t.Errorf("Syscall should be 'Open', got %q", wire.Syscall)
	}
	if wire.DurationMs != 10.0 {
		t.Errorf("DurationMs should be 10.0, got %f", wire.DurationMs)
	}
}

// --- 17.5-UNIT-003: [P1] recordEventToWire returns zero for non-syscall events (AC1) ---

func TestRecordEventToWire_NonSyscall(t *testing.T) {
	ev := debug.RecordEvent{
		SeqNum:    2,
		Timestamp: 300 * time.Millisecond,
		PID:       2,
		Type:      debug.RecordStateChange,
		State: &debug.StateChangeData{
			FromState: "running",
			ToState:   "sleeping",
		},
	}

	wire := recordEventToWire(ev)

	if wire.Syscall != "" {
		t.Errorf("non-syscall event should produce zero-value wire, got Syscall=%q", wire.Syscall)
	}
	if wire.PID != 0 {
		t.Errorf("non-syscall event should produce zero-value wire, got PID=%d", wire.PID)
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

// --- 17.5-UNIT-005: [P0] loadReplayTimeline loads events up to cursor (AC1) ---

func TestReplayDashboard_Timeline(t *testing.T) {
	reader := newTestRecordReader(t)

	eventsAtCursor2 := loadReplayTimeline(reader, 2)
	syscallCount := 0
	for _, ev := range eventsAtCursor2 {
		if ev.wire.Syscall != "" {
			syscallCount++
		}
	}
	if syscallCount != 2 {
		t.Errorf("cursor=2: expected 2 syscall events (SeqNum 0,1), got %d", syscallCount)
	}

	eventsAtMinus1 := loadReplayTimeline(reader, -1)
	if len(eventsAtMinus1) != 0 {
		t.Errorf("cursor=-1: expected 0 events, got %d", len(eventsAtMinus1))
	}

	eventsAtEnd := loadReplayTimeline(reader, 4)
	endSyscallCount := 0
	for _, ev := range eventsAtEnd {
		if ev.wire.Syscall != "" {
			endSyscallCount++
		}
	}
	if endSyscallCount != 3 {
		t.Errorf("cursor=4: expected 3 syscall events (SeqNum 0,1,4), got %d", endSyscallCount)
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
	roots := buildProcessTree(m2.processes)
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
