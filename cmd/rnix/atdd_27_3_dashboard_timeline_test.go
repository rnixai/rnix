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
	"time"

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
	m.activePane = paneTimeline

	m.stepEntries = []stepEntry{
		{
			summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "/dev/fs → read main.go", HasError: false, DurationMs: 218.0, TokenCount: 150},
			level:   levelSummary,
		},
		{
			summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", Summary: "/dev/shell → go build", HasError: true, DurationMs: 1200.0, TokenCount: 280},
			level:   levelSummary,
		},
		{
			summary: ipc.StepSummaryWire{Step: 3, Action: "complete", Summary: "任务完成", HasError: false, DurationMs: 45.0, TokenCount: 50},
			level:   levelSummary,
		},
	}
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	// Populate unifiedEvents from stepEntries (pointers allow level mutations to propagate).
	now := time.Now()
	for i := range m.stepEntries {
		m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
			Type:      EventStep,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			PID:       1,
			Summary:   m.stepEntries[i].summary.Summary,
			StepEntry: &m.stepEntries[i],
		})
	}
	return m
}

// ---------------------------------------------------------------------------
// AC-1: Level 1 默认步骤摘要 — one-line summary per step
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC1_Level1_StepSummary_Rendered(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	// Summary is the primary content — must be prominently displayed
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC-1: Level 1 should show summary '/dev/fs', got: %s", output)
	}
	// Action abbreviation visible in suffix
	if !strings.Contains(output, "tool") {
		t.Errorf("AC-1: Level 1 should show action abbreviation 'tool', got: %s", output)
	}
	// Step number visible in suffix
	if !strings.Contains(output, " 1 ") && !strings.Contains(output, " 1\n") {
		t.Errorf("AC-1: Level 1 should show step number '1', got: %s", output)
	}
}

func TestATDD_27_3_AC1_Level1_ShowsDuration(t *testing.T) {
	m := newStepTimelineModel()
	m.width = 120 // wide enough to show duration

	output := m.renderTimelinePane(120, 20)

	if !strings.Contains(output, "218") {
		t.Errorf("AC-1: Level 1 should show duration '218ms', got: %s", output)
	}
}

func TestATDD_27_3_AC1_Level1_ShowsErrorMarker(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	// Error step summary should be visible with error marker
	if !strings.Contains(output, "/dev/shell") {
		t.Errorf("AC-1: Level 1 should show error step summary '/dev/shell'")
	}
	if !strings.Contains(output, "✗") {
		t.Errorf("AC-1: Level 1 should show error marker '✗'")
	}
}

func TestATDD_27_3_AC1_Level1_AllSteps(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	// Each step's summary content must be visible
	for _, summary := range []string{"/dev/fs → read main.go", "/dev/shell → go build", "任务完成"} {
		if !strings.Contains(output, summary) {
			t.Errorf("AC-1: Level 1 should show summary %q", summary)
		}
	}
}

func TestATDD_27_3_AC1_Level1_StepTotal(t *testing.T) {
	m := newStepTimelineModel()

	output := m.renderTimelinePane(100, 20)

	if !strings.Contains(output, "3 steps") {
		t.Errorf("AC-1: Level 1 should show step total '3 steps', got: %s", output)
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
	// Slow steps no longer auto-expand (layout-first redesign: only HasError triggers expand).
	// Duration is indicated by yellow color badge in Level 1 view.
	if m.stepEntries[0].level != levelSummary {
		t.Errorf("AC-5: slow step should stay at Level 1 (summary), got level %d", m.stepEntries[0].level)
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
	m.width = 120
	m.height = 40
	m.selectedPID = 1
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "text", Summary: "hello"}},
	}
	m.unifiedEvents = []UnifiedEvent{
		{Type: EventStep, PID: 1, Summary: "hello", StepEntry: &m.stepEntries[0]},
	}
	output := m.renderTimelinePane(100, 20)
	if !strings.Contains(output, "hello") {
		t.Errorf("Timeline should show step summary 'hello', got: %s", output)
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

// ---------------------------------------------------------------------------
// PID switch: timeline resets and shows new process steps
// ---------------------------------------------------------------------------

func TestTimeline_PIDSwitch_ClearsOldSteps(t *testing.T) {
	m := newStepTimelineModel() // PID=1, 3 steps
	m.selectedUUID = "uuid-1"
	m.timelineAttachedUUID = "uuid-1"

	// Verify PID 1 has steps
	if len(m.stepEntries) != 3 {
		t.Fatalf("setup: expected 3 steps, got %d", len(m.stepEntries))
	}

	// Switch to different process
	m.selectedPID = 2
	m.selectedUUID = "uuid-2"
	m = m.handleTimelinePIDChange()

	if len(m.stepEntries) != 0 {
		t.Errorf("after UUID change, stepEntries should be empty, got %d", len(m.stepEntries))
	}
	if m.lastFetchedStep != 0 {
		t.Errorf("after UUID change, lastFetchedStep should be 0, got %d", m.lastFetchedStep)
	}
}

func TestTimeline_PIDSwitch_IgnoresStaleStepListMsg(t *testing.T) {
	m := newStepTimelineModel() // PID=1, 3 steps
	m.selectedUUID = "uuid-1"
	m.timelineAttachedUUID = "uuid-1"
	m.selectedPID = 2
	m.selectedUUID = "uuid-2"
	m = m.handleTimelinePIDChange()

	// Stale msg from uuid-1 should be discarded
	staleMsg := stepListMsg{
		uuid: "uuid-1",
		pid:  1,
		steps: []ipc.StepSummaryWire{
			{Step: 4, Action: "tool_call", Summary: "stale-step"},
		},
	}
	m2, _ := m.Update(staleMsg)
	m = m2.(dashboardModel)

	if len(m.stepEntries) != 0 {
		t.Errorf("stale uuid-1 msg should be discarded, got %d steps", len(m.stepEntries))
	}

	// Valid msg from uuid-2 should be applied
	validMsg := stepListMsg{
		uuid: "uuid-2",
		pid:  2,
		steps: []ipc.StepSummaryWire{
			{Step: 1, Action: "plan", Summary: "PID-2-step-1"},
			{Step: 2, Action: "tool_call", Summary: "PID-2-step-2"},
		},
	}
	m3, _ := m.Update(validMsg)
	m = m3.(dashboardModel)

	if len(m.stepEntries) != 2 {
		t.Errorf("uuid-2 msg should be applied, expected 2 steps, got %d", len(m.stepEntries))
	}
}

// ---------------------------------------------------------------------------
// Expand Dedup Tests (spec-timeline-expand-dedup)
// ---------------------------------------------------------------------------

func newExpandDedupModel() dashboardModel {
	m := newStepTimelineModel()
	// Add detail cache for step 1 (tool_call with ToolPath already shown as summary)
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:           1,
		ToolPath:       "/dev/fs → read main.go",
		ToolInput:      "path: main.go",
		ToolResult:     "package main\nfunc main() {}",
		ToolDurationMs: 218.0,
		RequestTokens:  100,
		ResponseTokens: 50,
		TokenCount:     150,
	}
	// Add detail cache for step 2 (error)
	m.stepDetailCache[2] = &ipc.GetStepDetailResponse{
		Step:           2,
		ToolPath:       "/dev/shell → go build",
		ToolInput:      "go build -o rnix ./cmd/rnix/",
		ToolError:      "exit status 1: undefined: ProcessManager",
		ToolDurationMs: 1200.0,
		RequestTokens:  200,
		ResponseTokens: 80,
		TokenCount:     280,
	}
	// Add detail cache for step 3 (complete)
	m.stepDetailCache[3] = &ipc.GetStepDetailResponse{
		Step:           3,
		RawResponse:    "任务完成，输出已生成。",
		RequestTokens:  30,
		ResponseTokens: 20,
		TokenCount:     50,
	}
	return m
}

// AC-1: Path dedup — when Level 1 already shows ToolPath, Level 2 should not repeat it
func TestExpandDedup_AC1_PathDedupWhenSummaryIsToolPath(t *testing.T) {
	m := newExpandDedupModel()
	m.stepEntries[0].level = levelExpanded // expand step 1

	output := m.renderTimelinePane(100, 30)

	// Level 1 should show the ToolPath as summary
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC-1: Level 1 should show ToolPath summary, got: %s", output)
	}
	// Level 2 should NOT contain a separate "Path" line since Level 1 already shows it
	// Count "Path" occurrences — should be 0 for this step (only other steps might show it)
	lines := strings.Split(output, "\n")
	pathCount := 0
	for _, line := range lines {
		if strings.Contains(line, "Path") && strings.Contains(line, "┊") {
			pathCount++
		}
	}
	if pathCount != 0 {
		t.Errorf("AC-1: Level 2 should not show Path when Level 1 already displays ToolPath, found %d Path lines", pathCount)
	}
}

// AC-2: Dur. dedup — Level 2 should never show Duration (Level 1 already has it)
func TestExpandDedup_AC2_DurationRemovedFromExpanded(t *testing.T) {
	m := newExpandDedupModel()
	m.stepEntries[0].level = levelExpanded

	output := m.renderTimelinePane(100, 30)

	// "Dur." should not appear anywhere in the expanded view
	if strings.Contains(output, "Dur.") {
		t.Errorf("AC-2: Level 2 should not show 'Dur.' (Level 1 already has duration), got: %s", output)
	}
}

// AC-3: Token conditional — when req+resp == TokenCount, skip Token line
func TestExpandDedup_AC3_TokenSkippedWhenMatchesTotal(t *testing.T) {
	m := newExpandDedupModel()
	m.stepEntries[0].level = levelExpanded // step 1: req=100, resp=50, total=150 → match

	output := m.renderTimelinePane(100, 30)

	// "Token" in the ┊ section should not appear since req+resp == TokenCount
	lines := strings.Split(output, "\n")
	tokenLineFound := false
	for _, line := range lines {
		if strings.Contains(line, "┊") && strings.Contains(line, "Token") {
			tokenLineFound = true
			break
		}
	}
	if tokenLineFound {
		t.Errorf("AC-3: Token breakdown should be hidden when req+resp matches total, got: %s", output)
	}
}

// AC-3b: Token shown when req+resp != TokenCount
func TestExpandDedup_AC3b_TokenShownWhenMismatch(t *testing.T) {
	m := newExpandDedupModel()
	// Step 2 entry has TokenCount: 280, detail req+resp=280 matches
	// Set entry TokenCount to different value to trigger mismatch
	m.stepEntries[1].summary.TokenCount = 500 // entry says 500 total
	m.stepDetailCache[2].RequestTokens = 200
	m.stepDetailCache[2].ResponseTokens = 80 // 200+80=280 != 500
	m.stepEntries[1].level = levelExpanded

	output := m.renderTimelinePane(100, 30)

	lines := strings.Split(output, "\n")
	tokenLineFound := false
	for _, line := range lines {
		if strings.Contains(line, "┊") && strings.Contains(line, "Token") {
			tokenLineFound = true
			break
		}
	}
	if !tokenLineFound {
		t.Errorf("AC-3b: Token breakdown should be shown when req+resp != TokenCount, got: %s", output)
	}
}

// AC-4: Header shows stage statistics on wide screens
func TestExpandDedup_AC4_HeaderStageStats(t *testing.T) {
	m := newExpandDedupModel()
	m.width = 120

	output := m.renderTimelinePane(120, 20)

	// Header should contain action counts
	if !strings.Contains(output, "tool:") {
		t.Errorf("AC-4: Header should show 'tool:' count on wide screens, got: %s", output)
	}
	if !strings.Contains(output, "done:") {
		t.Errorf("AC-4: Header should show 'done:' count on wide screens, got: %s", output)
	}
}

// AC-5: Header shows scroll position
func TestExpandDedup_AC5_HeaderScrollPosition(t *testing.T) {
	m := newExpandDedupModel()
	m.width = 100
	m.stepCursor = 1

	output := m.renderTimelinePane(100, 20)

	// Header should show position like "2/3"
	if !strings.Contains(output, "2/3") {
		t.Errorf("AC-5: Header should show scroll position '2/3', got: %s", output)
	}
}

// AC-6: e key expands all visible steps
func TestExpandDedup_AC6_ExpandAllWithE(t *testing.T) {
	m := newExpandDedupModel()
	// All at levelSummary
	for i := range m.stepEntries {
		m.stepEntries[i].level = levelSummary
	}

	m = m.handleTimelineKey("e")

	for i, entry := range m.stepEntries {
		if entry.level < levelExpanded {
			t.Errorf("AC-6: step %d should be expanded after 'e' key, got level %d", i+1, entry.level)
		}
	}
}

// AC-7: C key collapses all steps (Story 36-4: E repurposed to ErrorsOnly;
// collapse-all is now on C. Preserved the original intent by testing C.)
func TestExpandDedup_AC7_CollapseAllWithShiftE(t *testing.T) {
	m := newExpandDedupModel()
	// Expand some
	m.stepEntries[0].level = levelExpanded
	m.stepEntries[1].level = levelDebug

	m = m.handleTimelineKey("C")

	for i, entry := range m.stepEntries {
		if entry.level != levelSummary {
			t.Errorf("AC-7: step %d should be at levelSummary after 'C' key, got level %d", i+1, entry.level)
		}
	}
}

// AC-8: n key jumps to next error
func TestExpandDedup_AC8_JumpToNextError(t *testing.T) {
	m := newExpandDedupModel()
	m.stepCursor = 0 // at step 1 (no error)

	m = m.handleTimelineKey("n")

	// Should jump to step 2 (has error), which is index 1 in filtered
	if m.stepCursor != 1 {
		t.Errorf("AC-8: 'n' should jump to next error (index 1), got cursor %d", m.stepCursor)
	}
}

// AC-9: N key jumps to previous error
func TestExpandDedup_AC9_JumpToPrevError(t *testing.T) {
	m := newExpandDedupModel()
	m.stepCursor = 2 // at step 3 (no error)

	m = m.handleTimelineKey("N")

	// Should jump to step 2 (has error), which is index 1
	if m.stepCursor != 1 {
		t.Errorf("AC-9: 'N' should jump to previous error (index 1), got cursor %d", m.stepCursor)
	}
}

// AC-10: Filter bar labels match action abbreviations
func TestExpandDedup_AC10_FilterLabelsMatchAbbreviations(t *testing.T) {
	m := newExpandDedupModel()
	m.stepFilterMode = true

	output := m.renderStepFilterBar(120)

	// Labels should use actionAbbrev values
	if !strings.Contains(output, "tool") {
		t.Errorf("AC-10: filter should show 'tool' (not 'tool_call'), got: %s", output)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("AC-10: filter should show 'done' (not 'complete'), got: %s", output)
	}
	if !strings.Contains(output, "spec") {
		t.Errorf("AC-10: filter should show 'spec' (not 'specialize'), got: %s", output)
	}
	if strings.Contains(output, "tool_call") {
		t.Errorf("AC-10: filter should NOT show 'tool_call', got: %s", output)
	}
	if strings.Contains(output, "specialize") {
		t.Errorf("AC-10: filter should NOT show 'specialize', got: %s", output)
	}
}

// AC-11: Path shown when Level 1 Summary is NOT ToolPath
func TestExpandDedup_AC11_PathShownWhenSummaryDiffers(t *testing.T) {
	m := newExpandDedupModel()
	// Step 1 has Summary "/dev/fs → read main.go" (long, >= 8 chars), so Level 1 shows Summary, not ToolPath
	// But ToolPath in detail is the same as Summary... Let's use step 2 instead
	m.stepEntries[1].level = levelExpanded
	m.stepDetailCache[2].ToolPath = "/dev/shell → go build -o rnix ./cmd/rnix/"

	output := m.renderTimelinePane(100, 30)

	// Step 2 summary is "/dev/shell → go build" which is >= 8 chars
	// So Level 1 shows Summary, Level 2 should show the different ToolPath
	lines := strings.Split(output, "\n")
	pathFound := false
	for _, line := range lines {
		if strings.Contains(line, "┊") && strings.Contains(line, "Path") {
			pathFound = true
			break
		}
	}
	if !pathFound {
		t.Errorf("AC-11: Level 2 should show Path when it differs from Level 1 summary, got: %s", output)
	}
}

// AC-6: Error inline preview — Level 1 shows ToolError first line from cached detail
func TestExpandDedup_AC6_ErrorInlinePreview(t *testing.T) {
	m := newExpandDedupModel()
	m.width = 120

	output := m.renderTimelinePane(120, 30)

	// Step 2 has HasError=true and detail cached with ToolError
	// Level 1 should show error preview text after ✗
	if !strings.Contains(output, "exit status 1") {
		t.Errorf("AC-6: Level 1 error step should show ToolError preview 'exit status 1', got: %s", output)
	}
}

// AC-6b: Error preview NOT shown when detail is not cached
func TestExpandDedup_AC6b_ErrorPreviewHiddenWithoutCache(t *testing.T) {
	m := newExpandDedupModel()
	m.width = 120
	// Clear detail cache for step 2
	delete(m.stepDetailCache, 2)

	output := m.renderTimelinePane(120, 30)

	// Should still have ✗ but no error preview text
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "go build") && strings.Contains(line, "exit status") {
			t.Errorf("AC-6b: error preview should not show without cache, got: %s", line)
		}
	}
}

// AC-1b: Path dedup when Summary is short and replaced by ToolPath in Level 1
func TestExpandDedup_AC1b_PathDedupShortSummary(t *testing.T) {
	m := newExpandDedupModel()
	// Set step 1 summary to short text so Level 1 uses ToolPath
	m.stepEntries[0].summary.Summary = "read"
	m.stepEntries[0].summary.ToolPath = "/dev/fs → read main.go"
	m.stepDetailCache[1].ToolPath = "/dev/fs → read main.go"
	m.stepEntries[0].level = levelExpanded

	output := m.renderTimelinePane(100, 30)

	// Level 1 should show ToolPath (since Summary < 8)
	// Level 2 should NOT show Path (same as Level 1 display)
	lines := strings.Split(output, "\n")
	pathCount := 0
	for _, line := range lines {
		if strings.Contains(line, "┊") && strings.Contains(line, "Path") {
			pathCount++
		}
	}
	if pathCount != 0 {
		t.Errorf("AC-1b: Level 2 should not show Path when Level 1 uses ToolPath (short summary), found %d Path lines", pathCount)
	}
}

// Story 36-4: C (collapse-all) applies to ALL step entries, not just filtered —
// expand-mode changes are intentionally global so filter changes do not leak
// stale expanded state. The original E-only-filtered test is superseded by this.
func TestExpandDedup_E_CollapsesOnlyFiltered(t *testing.T) {
	m := newExpandDedupModel()
	// Expand all
	for i := range m.stepEntries {
		m.stepEntries[i].level = levelExpanded
	}
	// Filter to only show tool_call (indices 0, 1)
	m.stepFilters = map[string]bool{
		"tool_call": true, "plan": false, "text": false,
		"complete": false, "spawn": false, "replan": false, "specialize": false,
	}

	m = m.handleTimelineKey("C")

	// Story 36-4: all entries collapse (including step 2 outside the filter)
	for i, e := range m.stepEntries {
		if e.level != levelSummary {
			t.Errorf("Story 36-4: step %d should collapse globally, got level %d", i+1, e.level)
		}
	}
	if m.expandMode != expandModeCollapsed {
		t.Errorf("expected expandMode=Collapsed after C, got %d", m.expandMode)
	}
}
