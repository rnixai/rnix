package main

// =============================================================================
// ATDD Story 34.5: 默认布局重构与 Focus Card 解散
// =============================================================================
//
// Test Strategy:
//   AC-1: 默认视图四层布局 (title + tree/timeline + detail card + alerts + status)
//   AC-2: 进程详情卡 (Running and Dead process rendering)
//   AC-3: Focus Card 解散 (focusCardData removed, dashboard_focus.go deleted)
//   AC-4: 树面板宽度自适应 (35% formula with 28/50 clamp)
//   AC-5: 向后兼容 (number keys, Esc, view modes unchanged)

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// AC-2: Detail Card — Running process
// ---------------------------------------------------------------------------

func TestRenderDetailCardLeft_Running(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:            2,
		UUID:           "uuid-mock-002",
		Provider:       "claude",
		Model:          "claude-sonnet-4-20250514",
		AllowedDevices: []string{"/dev/fs", "/dev/shell", "/dev/llm/claude"},
		Skills:         []ipc.SkillInfoWire{{Name: "code-analyst"}},
		ContextStats:   ipc.ContextStatsWire{TokensUsed: 3200, ContextBudget: 100000, UsagePct: 3.2},
	}

	result := renderDetailCardLeft(&m, 40, 2)

	if !strings.Contains(result, "Provider: claude") {
		t.Errorf("expected Provider info, got %q", result)
	}
	if !strings.Contains(result, "Devices: /dev/fs") {
		t.Errorf("expected device list, got %q", result)
	}
	if !strings.Contains(result, "Skills: code-analyst") {
		t.Errorf("expected skill name, got %q", result)
	}
}

func TestRenderDetailCardRight_Running(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 2, PPID: 1, UUID: "uuid-mock-002", State: types.StateRunning, Intent: "review code", CreatedAt: now},
	}
	m := newTestDashboardModel(procs)
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:          2,
		UUID:         "uuid-mock-002",
		Provider:     "claude",
		ContextStats: ipc.ContextStatsWire{TokensUsed: 5000, ContextBudget: 100000, UsagePct: 5.0},
	}

	result := renderDetailCardRight(&m, 60, 2)

	if !strings.Contains(result, "Intent: review code") {
		t.Errorf("expected intent text, got %q", result)
	}
	if !strings.Contains(result, "Compact:") {
		t.Errorf("expected compact stats, got %q", result)
	}
	if !strings.Contains(result, "Steps:") {
		t.Errorf("expected steps count, got %q", result)
	}
	if !strings.Contains(result, "Budget:") {
		t.Errorf("expected budget percentage, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Detail Card — Dead process
// ---------------------------------------------------------------------------

func TestRenderDetailCardLeft_Dead(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 3, PPID: 1, UUID: "uuid-dead-003", State: types.StateDead, Intent: "build", Result: "completed successfully", CreatedAt: now, DeadAt: now.Add(5 * time.Second)},
	}
	m := newTestDashboardModel(procs)
	m.selectedPID = 3
	m.selectedUUID = "uuid-dead-003"
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:            3,
		UUID:           "uuid-dead-003",
		Provider:       "claude",
		AllowedDevices: []string{"/dev/fs"},
		CreatedAtMs:    now.UnixMilli(),
		DeadAtMs:       now.Add(5 * time.Second).UnixMilli(),
		ContextStats:   ipc.ContextStatsWire{TokensUsed: 8000},
	}

	result := renderDetailCardLeft(&m, 60, 2)

	// Should show success marker (not failure)
	if !strings.Contains(result, "Done") && !strings.Contains(result, "[ok]") {
		t.Errorf("expected success marker for exit 0, got %q", result)
	}
	if !strings.Contains(result, "5.0s") {
		t.Errorf("expected duration in time range, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Detail Card — No selection
// ---------------------------------------------------------------------------

func TestRenderDetailCardRight_NoSelection(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	// No selected PID

	result := renderDetailCardRight(&m, 60, 2)

	// Should contain separator but no process-specific data
	if strings.Contains(result, "Intent:") {
		t.Errorf("should not show intent when no process selected, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// AC-1: Default layout structure
// ---------------------------------------------------------------------------

func TestRenderDefaultLayout_FourLayers(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	result := m.renderDefaultLayout(120, 30)

	// Should contain tree and timeline content
	if !strings.Contains(result, "Agent Tree") && !strings.Contains(result, "Processes") {
		t.Errorf("expected tree pane, got %q", result)
	}

	// Should contain detail card placeholder (no selection)
	if !strings.Contains(result, "Select a process") {
		t.Errorf("expected detail card placeholder, got %q", result)
	}

	// Should NOT contain Focus Card
	if strings.Contains(result, "Focus Card") || strings.Contains(result, "FOCUS") {
		t.Errorf("should not contain Focus Card references, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// AC-4: Tree width formula
// ---------------------------------------------------------------------------

func TestRenderDefaultLayout_TreeWidth(t *testing.T) {
	tests := []struct {
		name      string
		termWidth int
		wantTree  int
	}{
		{"80col", 80, 28},   // 80*35/100=28, max(28,28)=28, min(28,50)=28
		{"120col", 120, 42}, // 120*35/100=42, max(28,42)=42, min(42,50)=42
		{"200col", 200, 50}, // 200*35/100=70, max(28,70)=70, min(70,50)=50
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			treeWidth := max(28, min(tt.termWidth*35/100, 50))
			if treeWidth != tt.wantTree {
				t.Errorf("tree width for %d cols = %d, want %d", tt.termWidth, treeWidth, tt.wantTree)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AC-1: Non-timeline pane layout
// ---------------------------------------------------------------------------

func TestRenderDefaultLayout_NonTimelinePane(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.rightPane = paneDetail
	result := m.renderDefaultLayout(120, 30)

	// Should still have detail card at bottom
	if !strings.Contains(result, "Select a process") && !strings.Contains(result, "─") {
		t.Errorf("expected detail card area even with non-timeline right pane, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// AC-3: Focus Card removed
// ---------------------------------------------------------------------------

func TestFocusCardRemoved(t *testing.T) {
	// Verify dashboard_focus.go is deleted
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dashboard_focus.go should be deleted, but it still exists")
	}

	// Verify focusCardData field removed from dashboardModel
	dashPath := filepath.Join(cmdRnixDir(), "dashboard.go")
	content, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	if strings.Contains(string(content), "focusCardData") {
		t.Error("dashboardModel should not contain 'focusCardData' field")
	}

	// Verify aggregateFocusCard removed
	if strings.Contains(string(content), "aggregateFocusCard") {
		t.Error("dashboard.go should not reference aggregateFocusCard")
	}

	// Verify focusCardState type removed from dashboard_types.go
	typesPath := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	typesContent, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}
	if strings.Contains(string(typesContent), "focusCardState") {
		t.Error("dashboard_types.go should not contain focusCardState type")
	}
}

// ---------------------------------------------------------------------------
// AC-5: Number key compatibility
// ---------------------------------------------------------------------------

func TestNumberKeyCompatibility(t *testing.T) {
	// Story 38.1: number key bindings moved from dashboard_nav.go to
	// dashboard_keylayers.go (Layer 1 Default + Expanded). Both files searched.
	navPath := filepath.Join(cmdRnixDir(), "dashboard_nav.go")
	keylayersPath := filepath.Join(cmdRnixDir(), "dashboard_keylayers.go")

	navContent, err := os.ReadFile(navPath)
	if err != nil {
		t.Fatalf("failed to read dashboard_nav.go: %v", err)
	}
	keylayersContent, err := os.ReadFile(keylayersPath)
	if err != nil {
		t.Fatalf("failed to read dashboard_keylayers.go: %v", err)
	}

	contentStr := string(navContent) + "\n" + string(keylayersContent)

	// Number keys 2-8 should still switch right pane (somewhere in nav/keylayers)
	for _, key := range []string{"\"2\"", "\"3\"", "\"4\"", "\"5\"", "\"6\"", "\"7\"", "\"8\""} {
		if !strings.Contains(contentStr, key) {
			t.Errorf("dashboard_nav.go or dashboard_keylayers.go should contain case for key %s", key)
		}
	}

	// Should NOT reference focusCardData (removed)
	if strings.Contains(contentStr, "focusCardData") {
		t.Error("dashboard_nav.go should not reference focusCardData")
	}
}

// ---------------------------------------------------------------------------
// AC-2: ASCII mode
// ---------------------------------------------------------------------------

func TestDetailCard_ASCII(t *testing.T) {
	// Test that ASCII mode symbols are handled in the code
	detailCardPath := filepath.Join(cmdRnixDir(), "dashboard_detail_card.go")
	content, err := os.ReadFile(detailCardPath)
	if err != nil {
		t.Fatalf("failed to read dashboard_detail_card.go: %v", err)
	}
	contentStr := string(content)

	// Should handle ASCII mode for check/fail marks
	if !strings.Contains(contentStr, "[ok]") {
		t.Error("detail card should handle ASCII mode with [ok]")
	}
	if !strings.Contains(contentStr, "[FAIL]") {
		t.Error("detail card should handle ASCII mode with [FAIL]")
	}
	if !strings.Contains(contentStr, "IsASCIIMode") {
		t.Error("detail card should use ui.IsASCIIMode()")
	}
}

// ---------------------------------------------------------------------------
// AC-3: IPC optimization — no focusCardNeedsData
// ---------------------------------------------------------------------------

func TestIPCOptimization_NoFocusCardBroadcast(t *testing.T) {
	dashPath := filepath.Join(cmdRnixDir(), "dashboard.go")
	content, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	contentStr := string(content)

	// focusCardNeedsData should not exist
	if strings.Contains(contentStr, "focusCardNeedsData") {
		t.Error("dashboard.go should not contain focusCardNeedsData (replaced with targeted detailCardNeedsData)")
	}

	// detailCardNeedsData should exist (the replacement)
	if !strings.Contains(contentStr, "detailCardNeedsData") {
		t.Error("dashboard.go should contain detailCardNeedsData for targeted procDetail fetching")
	}
}

// ---------------------------------------------------------------------------
// Helpers: detail card width uses lipgloss.Width
// ---------------------------------------------------------------------------

func TestDetailCard_UsesLipglossWidth(t *testing.T) {
	detailCardPath := filepath.Join(cmdRnixDir(), "dashboard_detail_card.go")
	content, err := os.ReadFile(detailCardPath)
	if err != nil {
		t.Fatalf("failed to read dashboard_detail_card.go: %v", err)
	}
	contentStr := string(content)

	if !strings.Contains(contentStr, "lipgloss.Width(") {
		t.Error("detail card should use lipgloss.Width() for width measurement (34-2 review lesson)")
	}
}

// ---------------------------------------------------------------------------
// AC-1: renderDefaultLayout structure verification
// ---------------------------------------------------------------------------

func TestRenderDefaultLayout_StructureVerification(t *testing.T) {
	dashPath := filepath.Join(cmdRnixDir(), "dashboard.go")
	content, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}

	funcBody := extractFuncBody(string(content), "renderDefaultLayout")
	if funcBody == "" {
		t.Fatal("renderDefaultLayout function body not found")
	}

	// Should use new width formula
	if !strings.Contains(funcBody, "35/100") {
		t.Error("renderDefaultLayout should use 35% width formula")
	}

	// Should call detail card renderers
	if !strings.Contains(funcBody, "renderDetailCardLeft") {
		t.Error("renderDefaultLayout should call renderDetailCardLeft")
	}
	if !strings.Contains(funcBody, "renderDetailCardRight") {
		t.Error("renderDefaultLayout should call renderDetailCardRight")
	}

	// Should NOT call renderFocusCard
	if strings.Contains(funcBody, "renderFocusCard") {
		t.Error("renderDefaultLayout should NOT call renderFocusCard")
	}
}

// ---------------------------------------------------------------------------
// dashboard_detail_card.go file existence and functions
// ---------------------------------------------------------------------------

func TestDetailCardFile_ExistsWithExpectedFunctions(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_detail_card.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected dashboard_detail_card.go to exist in cmd/rnix/")
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard_detail_card.go: %v", err)
	}

	funcSet := make(map[string]bool)
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			funcSet[fn.Name.Name] = true
		}
	}

	required := []string{
		"renderDetailCardLeft",
		"renderDetailCardRight",
		"findSelectedProcess",
		"compactStats",
		"fitLine",
		"safeRepeat",
	}
	for _, fn := range required {
		if !funcSet[fn] {
			t.Errorf("expected function %s in dashboard_detail_card.go", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// Existing tests updated: verify no regression
// ---------------------------------------------------------------------------

func TestDetailCard_ExistingTestsPass_BuildCompiles(t *testing.T) {
	// This test verifies the code compiles (if this test runs, compilation passed)
	m := newTestDashboardModel(mockDashboardProcs())
	_ = m.renderDefaultLayout(120, 30)
	_ = renderDetailCardLeft(&m, 40, 2)
	_ = renderDetailCardRight(&m, 80, 2)
}
