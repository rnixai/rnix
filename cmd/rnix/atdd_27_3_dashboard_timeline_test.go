package main

// =============================================================================
// ATDD Story 27.3: Dashboard Timeline Three-Level Detail
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: Level 1 default step summary rendering
//   AC-2: Level 2 expand via v key — parameters, return value, tokens
//   AC-3: Level 3 debug detail via V key — prompt summary
//   AC-4: Collapse back to Level 1 via v key
//   AC-5: Auto-expand on error/slow steps
//   AC-7: spawn --dashboard flag
//
// Priority: P0 (AC-1,2,4), P1 (AC-3,5), P2 (AC-7)
// Test Level: Unit (dashboard model + rendering)

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newStepTimelineModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.stepTimelineMode = true

	m.stepEntries = []stepEntry{
		{
			summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "/dev/fs → read main.go", HasError: false, DurationMs: 218.0},
			level:   levelSummary,
		},
		{
			summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", Summary: "/dev/shell → go build", HasError: true, DurationMs: 1200.0},
			level:   levelSummary,
		},
		{
			summary: ipc.StepSummaryWire{Step: 3, Action: "complete", Summary: "任务完成", HasError: false, DurationMs: 45.0},
			level:   levelSummary,
		},
	}
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	return m
}

// ---------------------------------------------------------------------------
// AC-1: Level 1 默认步骤摘要 — one-line summary per step
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC1_Level1_StepSummary_Rendered(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	if !strings.Contains(output, "Step 1") {
		t.Errorf("AC-1: Level 1 should show 'Step 1', got: %s", output)
	}
	if !strings.Contains(output, "tool_call") {
		t.Errorf("AC-1: Level 1 should show action type 'tool_call', got: %s", output)
	}
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC-1: Level 1 should show target '/dev/fs', got: %s", output)
	}
}

func TestATDD_27_3_AC1_Level1_ShowsDuration(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	if !strings.Contains(output, "218") {
		t.Errorf("AC-1: Level 1 should show duration '218ms', got: %s", output)
	}
}

func TestATDD_27_3_AC1_Level1_ShowsErrorMarker(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	if !strings.Contains(output, "Step 2") {
		t.Errorf("AC-1: Level 1 should show 'Step 2'")
	}
}

func TestATDD_27_3_AC1_Level1_AllSteps(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	for _, step := range []string{"Step 1", "Step 2", "Step 3"} {
		if !strings.Contains(output, step) {
			t.Errorf("AC-1: Level 1 should show %q", step)
		}
	}
}

func TestATDD_27_3_AC1_Level1_StepTotal(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	if !strings.Contains(output, "/3") {
		t.Errorf("AC-1: Level 1 should show step total '/3', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Level 2 展开 — v key expands to show params, result, tokens
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC2_VKey_ExpandsToLevel2(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:           1,
		Action:         "tool_call",
		ToolInput:      `{"path":"main.go"}`,
		ToolResult:     "package main\nfunc main() {}",
		RequestTokens:  2340,
		ResponseTokens: 156,
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 118}) // 'v' = rune 118

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelExpanded {
		t.Errorf("AC-2: after v key, step level = %d, want %d (levelExpanded)", model.stepEntries[0].level, levelExpanded)
	}
}

func TestATDD_27_3_AC2_Level2_ShowsInput(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelExpanded
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:           1,
		Action:         "tool_call",
		ToolInput:      `{"path":"main.go"}`,
		ToolResult:     "package main\nfunc main() {}",
		RequestTokens:  2340,
		ResponseTokens: 156,
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "Input") {
		t.Errorf("AC-2: Level 2 should show 'Input' label, got: %s", output)
	}
}

func TestATDD_27_3_AC2_Level2_ShowsTokens(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelExpanded
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:           1,
		Action:         "tool_call",
		ToolInput:      `{"path":"main.go"}`,
		ToolResult:     "package main",
		RequestTokens:  2340,
		ResponseTokens: 156,
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "2340") {
		t.Errorf("AC-2: Level 2 should show request tokens '2340', got: %s", output)
	}
}

func TestATDD_27_3_AC2_Level2_ShowsError(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 1
	m.stepEntries[1].level = levelExpanded
	m.stepDetailCache[2] = &ipc.GetStepDetailResponse{
		Step:           2,
		Action:         "tool_call",
		ToolInput:      "go build -o rnix",
		ToolResult:     "",
		ToolError:      "exit status 1",
		RequestTokens:  1000,
		ResponseTokens: 200,
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "exit status 1") {
		t.Errorf("AC-2: Level 2 should show ToolError, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-3: Level 3 调试级详情 — V key (shift+v) shows prompt summary
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC3_ShiftVKey_ExpandsToLevel3(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelExpanded
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:         1,
		Action:       "tool_call",
		MessageCount: 23,
		TokenCount:   12500,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "请帮我编译项目并修复所有错误"}},
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 86}) // 'V' = Shift+v = rune 86

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelDebug {
		t.Errorf("AC-3: after V key, step level = %d, want %d (levelDebug)", model.stepEntries[0].level, levelDebug)
	}
}

func TestATDD_27_3_AC3_Level3_ShowsMessageCount(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelDebug
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:         1,
		Action:       "tool_call",
		MessageCount: 23,
		TokenCount:   12500,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "请帮我编译项目并修复所有错误"}},
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "23") {
		t.Errorf("AC-3: Level 3 should show message count '23', got: %s", output)
	}
}

func TestATDD_27_3_AC3_Level3_ShowsTokenCount(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelDebug
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:         1,
		Action:       "tool_call",
		MessageCount: 23,
		TokenCount:   12500,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "请帮我编译项目"}},
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "12.5k") && !strings.Contains(output, "12500") {
		t.Errorf("AC-3: Level 3 should show token count, got: %s", output)
	}
}

func TestATDD_27_3_AC3_Level3_ShowsFirstMessagePreview(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelDebug
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:         1,
		Action:       "tool_call",
		MessageCount: 5,
		TokenCount:   3000,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "请帮我编译项目并修复所有错误"}},
	}

	output := m.renderTimelinePane(100, 30)

	if !strings.Contains(output, "请帮我编译") {
		t.Errorf("AC-3: Level 3 should show first user message preview, got: %s", output)
	}
}

func TestATDD_27_3_AC3_ShiftV_FromLevel1_GoesToLevel3(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelSummary
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:         1,
		MessageCount: 5,
		TokenCount:   3000,
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 86}) // 'V'

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelDebug {
		t.Errorf("AC-3: V from Level 1 should go to Level 3, got level %d", model.stepEntries[0].level)
	}
}

// ---------------------------------------------------------------------------
// AC-4: 折叠回 Level 1 — v key from expanded state
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC4_VKey_FromLevel2_CollapseToLevel1(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelExpanded

	m2, _ := m.Update(tea.KeyPressMsg{Code: 118}) // 'v'

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelSummary {
		t.Errorf("AC-4: v from Level 2 should collapse to Level 1, got level %d", model.stepEntries[0].level)
	}
}

func TestATDD_27_3_AC4_VKey_FromLevel3_CollapseToLevel1(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelDebug

	m2, _ := m.Update(tea.KeyPressMsg{Code: 118}) // 'v'

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelSummary {
		t.Errorf("AC-4: v from Level 3 should collapse to Level 1, got level %d", model.stepEntries[0].level)
	}
}

func TestATDD_27_3_AC4_Collapse_PerStep(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelExpanded
	m.stepEntries[1].level = levelExpanded

	m2, _ := m.Update(tea.KeyPressMsg{Code: 118}) // 'v' — collapses only cursor step

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelSummary {
		t.Errorf("AC-4: cursor step should collapse, got level %d", model.stepEntries[0].level)
	}
	if model.stepEntries[1].level != levelExpanded {
		t.Errorf("AC-4: non-cursor step should remain expanded, got level %d", model.stepEntries[1].level)
	}
}

// ---------------------------------------------------------------------------
// AC-5: 自动展开出错/慢步骤
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC5_AutoExpand_Error(t *testing.T) {
	m := newStepTimelineModel()
	m.stepEntries = nil // start clean

	newStep := ipc.StepSummaryWire{
		Step:       1,
		Action:     "tool_call",
		Summary:    "/dev/shell → go build",
		HasError:   true,
		DurationMs: 500.0,
	}

	m = m.applyNewSteps([]ipc.StepSummaryWire{newStep})

	if len(m.stepEntries) != 1 {
		t.Fatalf("AC-5: expected 1 step entry, got %d", len(m.stepEntries))
	}
	if m.stepEntries[0].level != levelExpanded {
		t.Errorf("AC-5: error step should auto-expand to Level 2, got level %d", m.stepEntries[0].level)
	}
	if !m.stepEntries[0].autoExpand {
		t.Errorf("AC-5: autoExpand should be true for error step")
	}
}

func TestATDD_27_3_AC5_AutoExpand_SlowStep(t *testing.T) {
	m := newStepTimelineModel()
	m.stepEntries = nil

	newStep := ipc.StepSummaryWire{
		Step:       1,
		Action:     "tool_call",
		Summary:    "/dev/shell → go build",
		HasError:   false,
		DurationMs: 1500.0, // > 1s threshold
	}

	m = m.applyNewSteps([]ipc.StepSummaryWire{newStep})

	if len(m.stepEntries) != 1 {
		t.Fatalf("AC-5: expected 1 step entry, got %d", len(m.stepEntries))
	}
	if m.stepEntries[0].level != levelExpanded {
		t.Errorf("AC-5: slow step (>1s) should auto-expand to Level 2, got level %d", m.stepEntries[0].level)
	}
}

func TestATDD_27_3_AC5_NoAutoExpand_NormalStep(t *testing.T) {
	m := newStepTimelineModel()
	m.stepEntries = nil

	newStep := ipc.StepSummaryWire{
		Step:       1,
		Action:     "tool_call",
		Summary:    "/dev/fs → read config.yaml",
		HasError:   false,
		DurationMs: 200.0, // normal duration
	}

	m = m.applyNewSteps([]ipc.StepSummaryWire{newStep})

	if len(m.stepEntries) != 1 {
		t.Fatalf("AC-5: expected 1 step entry, got %d", len(m.stepEntries))
	}
	if m.stepEntries[0].level != levelSummary {
		t.Errorf("AC-5: normal step should stay at Level 1, got level %d", m.stepEntries[0].level)
	}
}

// ---------------------------------------------------------------------------
// AC-7: spawn --dashboard flag
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC7_SpawnDashboard_FlagExists(t *testing.T) {
	flag := rootCmd.Flags().Lookup("dashboard")
	if flag == nil {
		t.Fatal("AC-7: --dashboard flag not found on root command")
	}
	if flag.DefValue != "false" {
		t.Errorf("AC-7: --dashboard default = %q, want %q", flag.DefValue, "false")
	}
}

// ---------------------------------------------------------------------------
// Step cursor navigation — j/k keys
// ---------------------------------------------------------------------------

func TestATDD_27_3_StepCursor_JKey_MovesDown(t *testing.T) {
	m := newStepTimelineModel()
	m.activePane = paneTimeline
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 106}) // 'j'

	model := m2.(dashboardModel)
	if model.stepCursor != 1 {
		t.Errorf("j key should move cursor to 1, got %d", model.stepCursor)
	}
}

func TestATDD_27_3_StepCursor_KKey_MovesUp(t *testing.T) {
	m := newStepTimelineModel()
	m.activePane = paneTimeline
	m.stepCursor = 2

	m2, _ := m.Update(tea.KeyPressMsg{Code: 107}) // 'k'

	model := m2.(dashboardModel)
	if model.stepCursor != 1 {
		t.Errorf("k key should move cursor to 1, got %d", model.stepCursor)
	}
}

// ---------------------------------------------------------------------------
// Step timeline mode flag
// ---------------------------------------------------------------------------

func TestATDD_27_3_StepTimelineMode_Default(t *testing.T) {
	m := newDashboardModel(nil)
	if !m.stepTimelineMode {
		t.Errorf("stepTimelineMode should default to true (step view is default)")
	}
}

// ---------------------------------------------------------------------------
// V key from Level 3 → Level 2 (downgrade)
// ---------------------------------------------------------------------------

func TestATDD_27_3_ShiftV_FromLevel3_GoesToLevel2(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.stepEntries[0].level = levelDebug

	m2, _ := m.Update(tea.KeyPressMsg{Code: 86}) // 'V'

	model := m2.(dashboardModel)
	if model.stepEntries[0].level != levelExpanded {
		t.Errorf("V from Level 3 should downgrade to Level 2, got level %d", model.stepEntries[0].level)
	}
}
