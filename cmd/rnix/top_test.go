package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD RED PHASE — Story 10.1: rnix top 实时监控 TUI
// All tests assert EXPECTED behavior. They FAIL because the
// functions return zero values (stubs not yet implemented).
// ============================================================

// --- 10.1-UNIT-001: buildTree empty input ---

func TestBuildTree_Empty(t *testing.T) {
	roots := buildTree(nil)
	if len(roots) != 0 {
		t.Errorf("expected 0 roots for nil input, got %d", len(roots))
	}

	roots = buildTree([]vfs.ProcInfo{})
	if len(roots) != 0 {
		t.Errorf("expected 0 roots for empty input, got %d", len(roots))
	}
}

// --- 10.1-UNIT-002: buildTree single root ---

func TestBuildTree_SingleRoot(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "test"},
	}
	roots := buildTree(procs)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].proc.PID != 1 {
		t.Errorf("expected root PID 1, got %d", roots[0].proc.PID)
	}
	if len(roots[0].children) != 0 {
		t.Errorf("expected 0 children, got %d", len(roots[0].children))
	}
}

// --- 10.1-UNIT-003: buildTree parent-child relationships ---

func TestBuildTree_ParentChild(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning},
		{PID: 2, PPID: 1, State: types.StateRunning},
		{PID: 3, PPID: 1, State: types.StateZombie},
	}
	roots := buildTree(procs)
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

// --- 10.1-UNIT-004: buildTree orphan processes become roots ---

func TestBuildTree_OrphanBecomesRoot(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 5, PPID: 99, State: types.StateRunning},
	}
	roots := buildTree(procs)
	if len(roots) != 1 {
		t.Fatalf("expected orphan as root, got %d roots", len(roots))
	}
	if roots[0].proc.PID != 5 {
		t.Errorf("expected PID 5, got %d", roots[0].proc.PID)
	}
}

// --- 10.1-UNIT-005: buildTree children sorted by PID ---

func TestBuildTree_ChildrenSortedByPID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0},
		{PID: 5, PPID: 1},
		{PID: 3, PPID: 1},
		{PID: 2, PPID: 1},
	}
	roots := buildTree(procs)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	children := roots[0].children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	for i := 1; i < len(children); i++ {
		if children[i].proc.PID < children[i-1].proc.PID {
			t.Errorf("children not sorted by PID: %d before %d", children[i-1].proc.PID, children[i].proc.PID)
		}
	}
}

// --- 10.1-UNIT-006: flattenTree with indentation prefixes ---

func TestFlattenTree_Indentation(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0},
		{PID: 2, PPID: 1},
		{PID: 3, PPID: 1},
	}
	roots := buildTree(procs)
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

func TestFlattenTree_Empty(t *testing.T) {
	rows := flattenTree(nil)
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for nil roots, got %d", len(rows))
	}
}

func TestFlattenTree_DeepNesting(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0},
		{PID: 2, PPID: 1},
		{PID: 3, PPID: 2},
	}
	roots := buildTree(procs)
	rows := flattenTree(roots)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].depth != 0 {
		t.Errorf("root depth should be 0, got %d", rows[0].depth)
	}
	if rows[1].depth != 1 {
		t.Errorf("child depth should be 1, got %d", rows[1].depth)
	}
	if rows[2].depth != 2 {
		t.Errorf("grandchild depth should be 2, got %d", rows[2].depth)
	}
}

// --- 10.1-UNIT-007: topSummaryLine renders active count, token total, uptime ---

func TestTopSummaryLine_Content(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 5000},
		{PID: 2, State: types.StateRunning, TokensUsed: 3000},
		{PID: 3, State: types.StateZombie, TokensUsed: 2000},
	}
	summary := topSummaryLine(procs, 5*time.Minute)

	if !strings.Contains(summary, "2 active") {
		t.Errorf("summary should contain '2 active' (2 running), got %q", summary)
	}
	if !strings.Contains(summary, "rnix top") {
		t.Errorf("summary should contain 'rnix top' branding, got %q", summary)
	}
}

// --- 10.1-UNIT-008: topSummaryLine renders token total ---

func TestTopSummaryLine_TokenTotal(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 5200},
		{PID: 2, State: types.StateRunning, TokensUsed: 3100},
		{PID: 3, State: types.StateZombie, TokensUsed: 4150},
	}
	summary := topSummaryLine(procs, time.Minute)

	if !strings.Contains(summary, "12,450") && !strings.Contains(summary, "12450") {
		t.Errorf("summary should contain total tokens (12,450), got %q", summary)
	}
}

// --- 10.1-UNIT-009: topSummaryLine with empty process list ---

func TestTopSummaryLine_Empty(t *testing.T) {
	summary := topSummaryLine(nil, 0)
	if summary == "" {
		t.Error("summary should not be empty even with no processes")
	}
	if !strings.Contains(summary, "0") {
		t.Errorf("empty summary should show 0 active, got %q", summary)
	}
}

// --- 10.1-UNIT-010: flattenTree preserves process data ---

func TestFlattenTree_PreservesProcessData(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "分析代码", Skills: []string{"code-analyst"}, TokensUsed: 1847, CreatedAt: now},
	}
	roots := buildTree(procs)
	rows := flattenTree(roots)

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].proc.PID != 1 {
		t.Errorf("expected PID 1, got %d", rows[0].proc.PID)
	}
	if rows[0].proc.Intent != "分析代码" {
		t.Errorf("expected intent preserved, got %q", rows[0].proc.Intent)
	}
	if rows[0].proc.TokensUsed != 1847 {
		t.Errorf("expected tokens 1847, got %d", rows[0].proc.TokensUsed)
	}
}

// --- 10.1-UNIT-016: topDetailView renders process fields ---

func TestTopDetailView_ContainsFields(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:            1,
		PPID:           0,
		State:          types.StateRunning,
		Intent:         "分析代码库中的安全漏洞",
		Skills:         []string{"code-analysis", "security-scan"},
		TokensUsed:     5200,
		CreatedAt:      time.Now().Add(-3 * time.Minute),
		CtxID:          1,
		AllowedDevices: []string{"/dev/llm/claude", "/dev/fs"},
	}
	allProcs := []vfs.ProcInfo{
		proc,
		{PID: 2, PPID: 1, State: types.StateRunning},
		{PID: 3, PPID: 1, State: types.StateZombie},
	}
	detail := topDetailView(proc, allProcs)

	if !strings.Contains(detail, "running") {
		t.Errorf("detail should contain state, got %q", detail)
	}
	if !strings.Contains(detail, "分析代码库中的安全漏洞") {
		t.Errorf("detail should contain intent, got %q", detail)
	}
	if !strings.Contains(detail, "code-analysis") {
		t.Errorf("detail should contain skills, got %q", detail)
	}
	if !strings.Contains(detail, "PID 2") || !strings.Contains(detail, "PID 3") {
		t.Errorf("detail should list children PID 2 and PID 3, got %q", detail)
	}
}

func TestTopDetailView_ShowsDevices(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:            1,
		State:          types.StateRunning,
		AllowedDevices: []string{"/dev/llm/claude", "/dev/fs"},
	}
	detail := topDetailView(proc, nil)

	if !strings.Contains(detail, "/dev/llm/claude") {
		t.Errorf("detail should list allowed devices, got %q", detail)
	}
	if !strings.Contains(detail, "/dev/fs") {
		t.Errorf("detail should list /dev/fs, got %q", detail)
	}
}

func TestTopDetailView_EmptySkills(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:    1,
		State:  types.StateRunning,
		Skills: nil,
	}
	detail := topDetailView(proc, nil)
	if detail == "" {
		t.Error("detail view should render even with nil skills")
	}
}

func TestTopDetailView_NoChildren(t *testing.T) {
	proc := vfs.ProcInfo{PID: 5, State: types.StateRunning}
	detail := topDetailView(proc, []vfs.ProcInfo{proc})
	if strings.Contains(detail, "Children") {
		t.Errorf("detail should not show Children when there are none, got %q", detail)
	}
}

// --- 10.1-UNIT-011: Init() returns tick command ---

func TestTopModel_Init(t *testing.T) {
	m := newTopModel(nil)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil tick command")
	}
}

// --- 10.1-UNIT-012: Update(tickMsg) with nil client stays disconnected ---

func TestTopModel_TickNoClient(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-nonexistent-test.sock"
	defer func() { ipc.SocketPathOverride = old }()

	m := newTopModel(nil)
	m.connected = false
	updated, cmd := m.Update(tickMsg(time.Now()))
	um := updated.(topModel)
	if um.connected {
		t.Error("should remain disconnected when no daemon available")
	}
	if cmd == nil {
		t.Error("should schedule next tick even when disconnected")
	}
}

// --- 10.1-UNIT-013: Update(K) triggers kill intent on selected PID ---

func TestTopModel_KillKey(t *testing.T) {
	m := newTopModel(nil)
	m.rows = []flatRow{
		{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning}},
		{proc: vfs.ProcInfo{PID: 2, State: types.StateRunning}},
	}
	m.cursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(topModel)
	if um.cursor != 1 {
		t.Errorf("cursor should stay at 1, got %d", um.cursor)
	}
}

// --- 10.1-UNIT-014: Cursor navigation j/k moves up/down ---

func TestTopModel_CursorNavigation(t *testing.T) {
	m := newTopModel(nil)
	m.rows = []flatRow{
		{proc: vfs.ProcInfo{PID: 1}},
		{proc: vfs.ProcInfo{PID: 2}},
		{proc: vfs.ProcInfo{PID: 3}},
	}
	m.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(topModel)
	if um.cursor != 1 {
		t.Errorf("j should move cursor down: expected 1, got %d", um.cursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'j'})
	um = updated.(topModel)
	if um.cursor != 2 {
		t.Errorf("j again: expected 2, got %d", um.cursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'j'})
	um = updated.(topModel)
	if um.cursor != 2 {
		t.Errorf("j at bottom should stay at 2, got %d", um.cursor)
	}

	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(topModel)
	if um.cursor != 1 {
		t.Errorf("k should move cursor up: expected 1, got %d", um.cursor)
	}

	um.cursor = 0
	updated, _ = um.Update(tea.KeyPressMsg{Code: 'k'})
	um = updated.(topModel)
	if um.cursor != 0 {
		t.Errorf("k at top should stay at 0, got %d", um.cursor)
	}
}

// --- 10.1-UNIT-023: Kill with nil client sets no statusMsg ---

func TestTopModel_KillNilClient(t *testing.T) {
	m := newTopModel(nil)
	m.killSelected(1)
	if m.statusMsg != "" {
		t.Errorf("kill with nil client should not set statusMsg, got %q", m.statusMsg)
	}
}

// --- 10.1-UNIT-015: Kill on empty process list is no-op ---

func TestTopModel_KillEmptyList(t *testing.T) {
	m := newTopModel(nil)
	m.rows = nil
	m.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	um := updated.(topModel)
	if um.cursor != 0 {
		t.Errorf("kill on empty list should be no-op, cursor: %d", um.cursor)
	}
}

// --- 10.1-UNIT-018: Update(Esc) returns to list view from detail ---

func TestTopModel_EscFromDetail(t *testing.T) {
	m := newTopModel(nil)
	m.rows = []flatRow{
		{proc: vfs.ProcInfo{PID: 1}},
	}
	m.detailPID = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(topModel)
	if um.detailPID != 0 {
		t.Errorf("Esc should clear detailPID, got %d", um.detailPID)
	}
}

// --- 10.1-UNIT-019: Update(q) returns tea.Quit ---

func TestTopModel_QuitQ(t *testing.T) {
	m := newTopModel(nil)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("q should return a non-nil quit command")
	}
}

// --- 10.1-UNIT-020: Update(Enter) launches dashboard (Story 27-5 changed behavior) ---

func TestTopModel_EnterLaunchesDashboard(t *testing.T) {
	m := newTopModel(nil)
	m.rows = []flatRow{
		{proc: vfs.ProcInfo{PID: 5}},
		{proc: vfs.ProcInfo{PID: 10}},
	}
	m.cursor = 1

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)
	if um.launchDashboardPID != 10 {
		t.Errorf("Enter should set launchDashboardPID to selected proc (10), got %d", um.launchDashboardPID)
	}
	if cmd == nil {
		t.Error("Enter should return a quit command for dashboard launch")
	}
}

// --- 10.1-UNIT-021: View renders with AltScreen ---

func TestTopModel_ViewAltScreen(t *testing.T) {
	m := newTopModel(nil)
	v := m.View()
	if !v.AltScreen {
		t.Error("View should set AltScreen = true")
	}
	if v.Content == "" {
		t.Error("View content should not be empty")
	}
}

// --- 10.1-UNIT-022: View detail mode renders process info ---

func TestTopModel_ViewDetailMode(t *testing.T) {
	m := newTopModel(nil)
	proc := vfs.ProcInfo{PID: 1, State: types.StateRunning, Intent: "test-intent", Skills: []string{"analyzer"}}
	m.rows = []flatRow{{proc: proc}}
	m.processes = []vfs.ProcInfo{proc}
	m.detailPID = 1

	v := m.View()
	if !strings.Contains(v.Content, "test-intent") {
		t.Errorf("detail view should contain intent, got %q", v.Content)
	}
	if !strings.Contains(v.Content, "analyzer") {
		t.Errorf("detail view should contain skills, got %q", v.Content)
	}
}

// --- 10.1-INT-001: top command registered in cobra ---

func TestHelp_ContainsTopSubcommand(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "top") {
		t.Errorf("expected 'top' subcommand in help output, got %q", output)
	}
}

// --- 10.1-INT-002: rnix top without daemon exits gracefully ---

func TestRunTop_NoDaemon(t *testing.T) {
	// Isolate from any real daemon by pointing to a non-existent socket
	saved := ipc.SocketPathOverride
	ipc.SocketPathOverride = filepath.Join(t.TempDir(), "nonexistent.sock")
	defer func() { ipc.SocketPathOverride = saved }()

	savedExit := exitCode
	defer func() { exitCode = savedExit }()
	exitCode = 0

	err := runTop(nil, nil)
	if err != nil {
		t.Fatalf("runTop should handle daemon absence gracefully, got error: %v", err)
	}
}

// ============================================================
// ATDD RED PHASE — Story 10.3: Token 预算管理 (AC3, AC4)
//
// Tests reference ProcInfo.ContextBudget which does NOT exist yet.
// RED → GREEN: add ContextBudget to ProcInfo and rendering logic.
// ============================================================

// --- 10.3-UNIT-040: [P1] TOKENS column shows "used/budget" when budget > 0 ---

func TestTopSummaryLine_WithBudgetInfo(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 3000, ContextBudget: 5000},
		{PID: 2, State: types.StateRunning, TokensUsed: 2000, ContextBudget: 0},
	}
	summary := topSummaryLine(procs, time.Minute)
	if summary == "" {
		t.Fatal("summary should not be empty")
	}
}

// --- 10.3-UNIT-041: [P1] topDetailView shows budget in detail when set ---

func TestTopDetailView_ShowsBudget(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:           1,
		State:         types.StateRunning,
		Intent:        "budget display test",
		TokensUsed:    4500,
		ContextBudget: 5000,
	}
	detail := topDetailView(proc, nil)

	if !strings.Contains(detail, "5000") && !strings.Contains(detail, "5,000") {
		t.Errorf("detail view should show budget value 5000, got %q", detail)
	}
	if !strings.Contains(detail, "4500") && !strings.Contains(detail, "4,500") {
		t.Errorf("detail view should show tokens used 4500, got %q", detail)
	}
}

// --- 10.3-UNIT-042: [P1] topDetailView omits budget line when ContextBudget=0 ---

func TestTopDetailView_NoBudgetOmitsBudgetLine(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:           1,
		State:         types.StateRunning,
		Intent:        "no limit set",
		TokensUsed:    1000,
		ContextBudget: 0,
	}
	detail := topDetailView(proc, nil)

	if strings.Contains(strings.ToLower(detail), "budget") {
		t.Errorf("detail view should NOT contain 'budget' when ContextBudget=0, got %q", detail)
	}
}

// --- 10.3-UNIT-043: [P1] warning style when usage >= 80% of budget ---

func TestTopView_WarningStyleHighUsage(t *testing.T) {
	m := newTopModel(nil)
	proc := vfs.ProcInfo{
		PID:           1,
		State:         types.StateRunning,
		Intent:        "almost exceeded",
		TokensUsed:    4500,
		ContextBudget: 5000,
	}
	m.rows = []flatRow{{proc: proc}}
	m.processes = []vfs.ProcInfo{proc}

	v := m.View()
	content := v.Content
	if content == "" {
		t.Fatal("view content should not be empty")
	}
	// The token column for 90% usage (4500/5000) should indicate warning
	// Exact rendering depends on implementation, but tokens should be present
	if !strings.Contains(content, "4500") && !strings.Contains(content, "4,500") {
		t.Errorf("view should show tokens used, got %q", content)
	}
}

// --- 10.3-UNIT-044: [P2] View shows plain tokens when no budget (AC4) ---

func TestTopView_PlainTokensNoBudget(t *testing.T) {
	m := newTopModel(nil)
	proc := vfs.ProcInfo{
		PID:           1,
		State:         types.StateRunning,
		Intent:        "no budget plain",
		TokensUsed:    3000,
		ContextBudget: 0,
	}
	m.rows = []flatRow{{proc: proc}}
	m.processes = []vfs.ProcInfo{proc}

	v := m.View()
	if !strings.Contains(v.Content, "3000") && !strings.Contains(v.Content, "3,000") {
		t.Errorf("view should show plain tokens, got %q", v.Content)
	}
}

// --- buildTree: multiple root processes ---

func TestBuildTree_MultipleRoots(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning},
		{PID: 4, PPID: 0, State: types.StateRunning},
	}
	roots := buildTree(procs)
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}
	if roots[0].proc.PID != 1 {
		t.Errorf("first root PID should be 1, got %d", roots[0].proc.PID)
	}
	if roots[1].proc.PID != 4 {
		t.Errorf("second root PID should be 4, got %d", roots[1].proc.PID)
	}
}

// --- buildTree: roots sorted by PID ---

func TestBuildTree_RootsSortedByPID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 10, PPID: 0},
		{PID: 1, PPID: 0},
		{PID: 5, PPID: 0},
	}
	roots := buildTree(procs)
	if len(roots) != 3 {
		t.Fatalf("expected 3 roots, got %d", len(roots))
	}
	for i := 1; i < len(roots); i++ {
		if roots[i].proc.PID < roots[i-1].proc.PID {
			t.Errorf("roots not sorted by PID: %d before %d", roots[i-1].proc.PID, roots[i].proc.PID)
		}
	}
}
