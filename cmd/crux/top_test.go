package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// ============================================================
// ATDD RED PHASE — Story 10.1: crux top 实时监控 TUI
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

	if !strings.Contains(summary, "2") {
		t.Errorf("summary should contain active count (2 running), got %q", summary)
	}
	if !strings.Contains(summary, "crux top") {
		t.Errorf("summary should contain 'crux top' branding, got %q", summary)
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
	detail := topDetailView(proc)

	if !strings.Contains(detail, "running") {
		t.Errorf("detail should contain state, got %q", detail)
	}
	if !strings.Contains(detail, "分析代码库中的安全漏洞") {
		t.Errorf("detail should contain intent, got %q", detail)
	}
	if !strings.Contains(detail, "code-analysis") {
		t.Errorf("detail should contain skills, got %q", detail)
	}
}

func TestTopDetailView_ShowsDevices(t *testing.T) {
	proc := vfs.ProcInfo{
		PID:            1,
		State:          types.StateRunning,
		AllowedDevices: []string{"/dev/llm/claude", "/dev/fs"},
	}
	detail := topDetailView(proc)

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
	detail := topDetailView(proc)
	if detail == "" {
		t.Error("detail view should render even with nil skills")
	}
}

// --- 10.1-UNIT-011 to 10.1-UNIT-020: Bubbletea-dependent tests ---
// The following tests require bubbletea v2 dependency (Task 1 of story).
// Test scenarios documented here; implementations added when dependency is available.
//
// 10.1-UNIT-011: Init() returns tick command
// 10.1-UNIT-012: Update(tickMsg) triggers process data refresh
// 10.1-UNIT-013: Update(K) triggers kill on selected PID
// 10.1-UNIT-014: Cursor navigation j/k moves up/down
// 10.1-UNIT-015: Kill on empty process list is no-op
// 10.1-UNIT-018: Update(Esc) returns to list view from detail
// 10.1-UNIT-019: Update(q) returns tea.Quit
// 10.1-UNIT-020: Update(ctrl+c) returns tea.Quit

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

// --- 10.1-INT-002: crux top without daemon exits gracefully ---

func TestRunTop_NoDaemon(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	err := runTop(nil, nil)
	if err != nil {
		t.Fatalf("runTop should handle daemon absence gracefully, got error: %v", err)
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
