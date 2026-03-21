package main

// =============================================================================
// ATDD Story 27.4: watch 三级详细度 + prompt 查看
// TDD RED PHASE — All tests designed to FAIL until BubbleTea TUI is implemented
// =============================================================================
//
// Test Strategy:
//   AC-1:  watch 升级为 BubbleTea TUI (type existence, tea.Model interface)
//   AC-2:  Level 1 步骤列表 (step rendering, cursor navigation)
//   AC-3:  v 键展开 Level 2 详情 (RawResponse, ToolInput, ToolResult, tokens)
//   AC-4:  V 键展开 Level 3 调试详情 (MessageCount, TokenCount, first user msg)
//   AC-5:  错误步骤自动展开 (HasError → auto-expand level 2)
//   AC-6:  慢步骤自动展开 (DurationMs>1000 → auto-expand level 2)
//   AC-7:  p 键进入 prompt 翻页模式 (Pager state, SystemPrompt+Messages+Tools)
//   AC-8:  Pager 模式交互 (q/Esc back, j/k/↑/↓ scroll, PgUp/PgDn, g/G)
//   AC-9:  v 键折叠 (Expanded → Normal)
//   AC-10: q 键退出 (tea.Quit from Normal/Expanded)
//   AC-11: 步骤详情缓存 (detailCache hit/miss)
//
// Priority: P0 (core observation infrastructure)
// Test Level: Unit (watchModel state machine) + Integration (view rendering)

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestWatchModel() watchModel {
	profile := ui.TerminalProfile{IsUnicode: true, ColorLevel: 3}
	m := newWatchModel(42, nil, nil, profile)
	m.width = 120
	m.height = 40
	m.detailCache = make(map[int]*ipc.GetStepDetailResponse)
	return m
}

func newTestWatchModelWithSteps(steps []watchStepInfo) watchModel {
	m := newTestWatchModel()
	m.steps = steps
	return m
}

func sampleSteps() []watchStepInfo {
	return []watchStepInfo{
		{step: 1, action: "tool_call", summary: "/dev/fs read", durationMs: 200, hasError: false},
		{step: 2, action: "tool_call", summary: "/dev/shell make build", durationMs: 1500, hasError: false},
		{step: 3, action: "tool_call", summary: "/dev/fs write", durationMs: 100, hasError: true},
		{step: 4, action: "plan", summary: "Created plan", durationMs: 800, hasError: false},
	}
}

func sampleStepDetail() *ipc.GetStepDetailResponse {
	return &ipc.GetStepDetailResponse{
		SystemPrompt:   "You are a helpful agent with filesystem access.",
		Tools:          []ipc.ToolDefWire{{Name: "read_file", Description: "Read a file"}},
		Step:           2,
		Messages:       []ipc.MessageWire{{Role: "user", Content: "分析 main.go 文件的结构和依赖关系"}, {Role: "assistant", Content: "I'll analyze main.go..."}},
		MessageCount:   12,
		TokenCount:     8450,
		RawResponse:    `{"role":"assistant","content":"I'll run the build command to check for errors..."}`,
		Action:         "tool_call",
		Summary:        "/dev/shell make build",
		ToolPath:       "/dev/shell",
		ToolInput:      `{"command": "make build"}`,
		ToolResult:     "Build succeeded with 0 errors",
		RequestTokens:  1234,
		ResponseTokens: 567,
	}
}

func progressPayloadJSON(event string, step int, hasError bool, durationMs float64) json.RawMessage {
	pp := ipc.ProgressPayload{
		Event:      event,
		PID:        42,
		Step:       step,
		HasError:   hasError,
		DurationMs: durationMs,
		Action:     "tool_call",
		Summary:    "test summary",
	}
	data, _ := json.Marshal(pp)
	return data
}

// ---------------------------------------------------------------------------
// AC-1: watch 升级为 BubbleTea TUI
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC1_WatchModelImplementsTeaModel(t *testing.T) {
	var _ tea.Model = watchModel{}
}

func TestATDD_27_4_AC1_WatchStateEnum(t *testing.T) {
	if watchStateNormal != 0 {
		t.Errorf("AC-1: watchStateNormal = %d, want 0", watchStateNormal)
	}
	if watchStateExpanded != 1 {
		t.Errorf("AC-1: watchStateExpanded = %d, want 1", watchStateExpanded)
	}
	if watchStatePager != 2 {
		t.Errorf("AC-1: watchStatePager = %d, want 2", watchStatePager)
	}
}

func TestATDD_27_4_AC1_NewWatchModel_InitializesFields(t *testing.T) {
	profile := ui.TerminalProfile{IsUnicode: true, ColorLevel: 3}
	m := newWatchModel(42, nil, nil, profile)

	if m.pid != 42 {
		t.Errorf("AC-1: pid = %d, want 42", m.pid)
	}
	if m.state != watchStateNormal {
		t.Errorf("AC-1: initial state = %d, want watchStateNormal(0)", m.state)
	}
	if m.cursor != 0 {
		t.Errorf("AC-1: initial cursor = %d, want 0", m.cursor)
	}
	if m.detailCache == nil {
		t.Error("AC-1: detailCache should be initialized (non-nil)")
	}
	if m.profile.IsUnicode != true {
		t.Error("AC-1: profile.IsUnicode should be true")
	}
}

func TestATDD_27_4_AC1_Init_ReturnsNonNilCmd(t *testing.T) {
	m := newTestWatchModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("AC-1: Init() should return a non-nil command to start watch stream")
	}
}

func TestATDD_27_4_AC1_ViewUsesAltScreen(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	v := m.View()
	if !v.AltScreen {
		t.Error("AC-1: View should set AltScreen = true")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Level 1 步骤列表（保持现有行为）
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC2_ViewRendersStepList(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	v := m.View()

	content := v.Content
	if !strings.Contains(content, "[step 1]") {
		t.Error("AC-2: view should contain [step 1]")
	}
	if !strings.Contains(content, "[step 2]") {
		t.Error("AC-2: view should contain [step 2]")
	}
	if !strings.Contains(content, "tool_call") {
		t.Error("AC-2: view should contain action type")
	}
	if !strings.Contains(content, "/dev/fs") {
		t.Error("AC-2: view should contain step summary")
	}
}

func TestATDD_27_4_AC2_CursorNavigation_JDown(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(watchModel)
	if um.cursor != 1 {
		t.Errorf("AC-2: j should move cursor down: expected 1, got %d", um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorNavigation_KUp(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(watchModel)
	if um.cursor != 1 {
		t.Errorf("AC-2: k should move cursor up: expected 1, got %d", um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorNavigation_ArrowDown(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	um := updated.(watchModel)
	if um.cursor != 1 {
		t.Errorf("AC-2: ↓ should move cursor down: expected 1, got %d", um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorNavigation_ArrowUp(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	um := updated.(watchModel)
	if um.cursor != 1 {
		t.Errorf("AC-2: ↑ should move cursor up: expected 1, got %d", um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorBoundsBottom(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = len(m.steps) - 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(watchModel)
	if um.cursor != len(m.steps)-1 {
		t.Errorf("AC-2: j at bottom should stay at %d, got %d", len(m.steps)-1, um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorBoundsTop(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(watchModel)
	if um.cursor != 0 {
		t.Errorf("AC-2: k at top should stay at 0, got %d", um.cursor)
	}
}

func TestATDD_27_4_AC2_CursorHighlight(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.cursor = 1
	v := m.View()

	if !strings.Contains(v.Content, "▸") {
		t.Error("AC-2: view should contain ▸ cursor indicator for selected step")
	}
}

func TestATDD_27_4_AC2_ViewShowsPIDAndModel(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.pid = 42
	v := m.View()

	if !strings.Contains(v.Content, "42") {
		t.Error("AC-2: view should display PID")
	}
}

// ---------------------------------------------------------------------------
// AC-3: v 键展开 Level 2 详情
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC3_VKey_NormalToExpanded(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v'})
	um := updated.(watchModel)

	if um.state != watchStateExpanded {
		t.Errorf("AC-3: v from Normal should go to Expanded, got state %d", um.state)
	}
	if um.expandLevel != 2 {
		t.Errorf("AC-3: v should set expandLevel=2, got %d", um.expandLevel)
	}
}

func TestATDD_27_4_AC3_Level2_ShowsRawResponse(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "I'll run the build command") {
		t.Error("AC-3: Level 2 view should show RawResponse content")
	}
}

func TestATDD_27_4_AC3_Level2_ShowsToolInputOutput(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "make build") {
		t.Error("AC-3: Level 2 view should show ToolInput")
	}
	if !strings.Contains(v.Content, "Build succeeded") {
		t.Error("AC-3: Level 2 view should show ToolResult")
	}
}

func TestATDD_27_4_AC3_Level2_ShowsTokens(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "1234") {
		t.Error("AC-3: Level 2 view should show RequestTokens (1234)")
	}
	if !strings.Contains(v.Content, "567") {
		t.Error("AC-3: Level 2 view should show ResponseTokens (567)")
	}
}

func TestATDD_27_4_AC3_VKey_TriggersDetailFetch(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 0
	m.detailCache = map[int]*ipc.GetStepDetailResponse{}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v'})

	if cmd == nil {
		t.Error("AC-3: v on uncached step should return a non-nil cmd to fetch detail")
	}
}

func TestATDD_27_4_AC3_Level2_TreeLinePrefix(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "┊") {
		t.Error("AC-3: Level 2 expanded lines should use ┊ tree prefix (unicode mode)")
	}
}

// ---------------------------------------------------------------------------
// AC-4: V 键展开 Level 3 调试详情
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC4_ShiftV_Level2ToLevel3(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'V', ShiftedCode: 'V', Mod: tea.ModShift})
	um := updated.(watchModel)

	if um.state != watchStateExpanded {
		t.Errorf("AC-4: V should stay in Expanded state, got %d", um.state)
	}
	if um.expandLevel != 3 {
		t.Errorf("AC-4: V should set expandLevel=3, got %d", um.expandLevel)
	}
}

func TestATDD_27_4_AC4_Level3_ShowsMessageCount(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "12") {
		t.Error("AC-4: Level 3 view should show MessageCount (12)")
	}
}

func TestATDD_27_4_AC4_Level3_ShowsTokenCount(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "8450") {
		t.Error("AC-4: Level 3 view should show TokenCount (8450)")
	}
}

func TestATDD_27_4_AC4_Level3_ShowsFirstUserMessage(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "分析 main.go") {
		t.Error("AC-4: Level 3 view should show first user message preview")
	}
}

func TestATDD_27_4_AC4_ShiftV_Level3BackToLevel2(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'V', ShiftedCode: 'V', Mod: tea.ModShift})
	um := updated.(watchModel)

	if um.expandLevel != 2 {
		t.Errorf("AC-4: V from Level 3 should toggle back to Level 2, got %d", um.expandLevel)
	}
}

func TestATDD_27_4_AC4_Level3_DebugSeparator(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if !strings.Contains(v.Content, "Debug") {
		t.Error("AC-4: Level 3 should show Debug separator")
	}
}

// ---------------------------------------------------------------------------
// AC-5: 错误步骤自动展开
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC5_ErrorStepAutoExpand(t *testing.T) {
	m := newTestWatchModelWithSteps(nil)
	m.state = watchStateNormal

	ev := ipc.StreamEvent{
		Type:    ipc.StreamProgress,
		Payload: progressPayloadJSON("step_complete", 1, true, 200),
	}
	updated, cmd := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if um.state != watchStateExpanded {
		t.Errorf("AC-5: error step should auto-expand, got state %d", um.state)
	}
	if um.expandLevel != 2 {
		t.Errorf("AC-5: error step should auto-expand to level 2, got %d", um.expandLevel)
	}
	if cmd == nil {
		t.Error("AC-5: auto-expand should trigger fetchDetailCmd")
	}
}

// ---------------------------------------------------------------------------
// AC-6: 慢步骤自动展开
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC6_SlowStepAutoExpand(t *testing.T) {
	m := newTestWatchModelWithSteps(nil)
	m.state = watchStateNormal

	ev := ipc.StreamEvent{
		Type:    ipc.StreamProgress,
		Payload: progressPayloadJSON("step_complete", 1, false, 1500),
	}
	updated, cmd := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if um.state != watchStateExpanded {
		t.Errorf("AC-6: slow step (1500ms) should auto-expand, got state %d", um.state)
	}
	if um.expandLevel != 2 {
		t.Errorf("AC-6: slow step should auto-expand to level 2, got %d", um.expandLevel)
	}
	if cmd == nil {
		t.Error("AC-6: auto-expand should trigger fetchDetailCmd")
	}
}

func TestATDD_27_4_AC6_FastStepNoAutoExpand(t *testing.T) {
	m := newTestWatchModelWithSteps(nil)
	m.state = watchStateNormal

	ev := ipc.StreamEvent{
		Type:    ipc.StreamProgress,
		Payload: progressPayloadJSON("step_complete", 1, false, 500),
	}
	updated, _ := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-6: fast step (500ms) should NOT auto-expand, got state %d", um.state)
	}
}

// ---------------------------------------------------------------------------
// AC-7: p 键进入 prompt 翻页模式
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC7_PKey_EntersPager(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	um := updated.(watchModel)

	if um.state != watchStatePager {
		t.Errorf("AC-7: p should enter Pager state, got %d", um.state)
	}
}

func TestATDD_27_4_AC7_PagerShowsSystemPrompt(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.cursor = 1
	detail := sampleStepDetail()
	m.detailCache = map[int]*ipc.GetStepDetailResponse{2: detail}

	m.pagerLines = formatPromptForPager(detail)
	m.pagerOffset = 0

	v := m.View()
	if !strings.Contains(v.Content, "System Prompt") {
		t.Error("AC-7: Pager should show [System Prompt] header")
	}
	if !strings.Contains(v.Content, "helpful agent") {
		t.Error("AC-7: Pager should show system prompt content")
	}
}

func TestATDD_27_4_AC7_PagerShowsMessages(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	detail := sampleStepDetail()
	m.detailCache = map[int]*ipc.GetStepDetailResponse{2: detail}

	m.pagerLines = formatPromptForPager(detail)
	m.pagerOffset = 0
	m.height = 200

	v := m.View()
	if !strings.Contains(v.Content, "Messages") {
		t.Error("AC-7: Pager should show [Messages] header")
	}
	if !strings.Contains(v.Content, "[user]") {
		t.Error("AC-7: Pager should show message roles")
	}
}

func TestATDD_27_4_AC7_PagerShowsTools(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	detail := sampleStepDetail()
	m.detailCache = map[int]*ipc.GetStepDetailResponse{2: detail}

	m.pagerLines = formatPromptForPager(detail)
	m.pagerOffset = 0
	m.height = 200

	v := m.View()
	if !strings.Contains(v.Content, "Tools") {
		t.Error("AC-7: Pager should show [Tools] header")
	}
	if !strings.Contains(v.Content, "read_file") {
		t.Error("AC-7: Pager should show tool names")
	}
}

func TestATDD_27_4_AC7_PKey_TriggersDetailFetch_WhenUncached(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 0
	m.detailCache = map[int]*ipc.GetStepDetailResponse{}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'p'})
	if cmd == nil {
		t.Error("AC-7: p on uncached step should return fetchDetailCmd")
	}
}

// ---------------------------------------------------------------------------
// AC-8: Pager 模式交互
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC8_PagerQuit_QKey(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-8: q in Pager should return to Normal, got state %d", um.state)
	}
}

func TestATDD_27_4_AC8_PagerQuit_EscKey(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-8: Esc in Pager should return to Normal, got state %d", um.state)
	}
}

func TestATDD_27_4_AC8_PagerScroll_JDown(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	m.pagerOffset = 0
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(watchModel)

	if um.pagerOffset != 1 {
		t.Errorf("AC-8: j should scroll down by 1, got offset %d", um.pagerOffset)
	}
}

func TestATDD_27_4_AC8_PagerScroll_KUp(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	m.pagerOffset = 5
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(watchModel)

	if um.pagerOffset != 4 {
		t.Errorf("AC-8: k should scroll up by 1, got offset %d", um.pagerOffset)
	}
}

func TestATDD_27_4_AC8_PagerScroll_GTop(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	m.pagerOffset = 50
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'g'})
	um := updated.(watchModel)

	if um.pagerOffset != 0 {
		t.Errorf("AC-8: g should jump to top (offset 0), got %d", um.pagerOffset)
	}
}

func TestATDD_27_4_AC8_PagerScroll_GBottom(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	m.pagerOffset = 0
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'G', ShiftedCode: 'G', Mod: tea.ModShift})
	um := updated.(watchModel)

	maxOffset := max(len(m.pagerLines)-(m.height-4), 0)
	if um.pagerOffset != maxOffset {
		t.Errorf("AC-8: G should jump to bottom (offset %d), got %d", maxOffset, um.pagerOffset)
	}
}

func TestATDD_27_4_AC8_PagerScroll_BoundsTop(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	m.pagerOffset = 0
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(watchModel)

	if um.pagerOffset != 0 {
		t.Errorf("AC-8: k at top should stay at 0, got %d", um.pagerOffset)
	}
}

func TestATDD_27_4_AC8_PagerShowsLinePosition(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = make([]string, 100)
	for i := range m.pagerLines {
		m.pagerLines[i] = "line content"
	}
	m.pagerOffset = 0
	m.height = 30

	v := m.View()
	if !strings.Contains(v.Content, "1/") {
		t.Error("AC-8: Pager should show line position indicator")
	}
}

func TestATDD_27_4_AC8_PagerHelpBar(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager
	m.pagerLines = []string{"test"}
	m.pagerOffset = 0

	v := m.View()
	if !strings.Contains(v.Content, "Back") || !strings.Contains(v.Content, "Scroll") {
		t.Error("AC-8: Pager should show help bar with navigation hints")
	}
}

// ---------------------------------------------------------------------------
// AC-9: v 键折叠
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC9_VKey_ExpandedToNormal(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v'})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-9: v from Expanded should collapse to Normal, got state %d", um.state)
	}
}

func TestATDD_27_4_AC9_VKey_Level3CollapsesToNormal(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 3
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v'})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-9: v from Level 3 should collapse to Normal, got state %d", um.state)
	}
}

// ---------------------------------------------------------------------------
// AC-10: q 键退出
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC10_QKey_QuitsFromNormal(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("AC-10: q from Normal should return tea.Quit cmd")
	}
}

func TestATDD_27_4_AC10_CtrlC_Quits(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("AC-10: Ctrl+C should return tea.Quit cmd")
	}
}

func TestATDD_27_4_AC10_QKey_QuitsFromExpanded(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("AC-10: q from Expanded should return tea.Quit cmd")
	}
}

func TestATDD_27_4_AC10_QKey_InPager_ReturnsToNormal(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStatePager

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	um := updated.(watchModel)

	if um.state != watchStateNormal {
		t.Errorf("AC-10: q in Pager should return to Normal (not quit), got state %d", um.state)
	}
	// q in pager should NOT quit the program, just exit pager
	_ = cmd
}

// ---------------------------------------------------------------------------
// AC-11: 步骤详情缓存
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC11_CacheHit_NoFetch(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'v'})
	um := updated.(watchModel)

	if um.state != watchStateExpanded {
		t.Errorf("AC-11: cached v should expand immediately, got state %d", um.state)
	}
	// Cache hit should NOT issue a fetch command
	if cmd != nil {
		t.Error("AC-11: cache hit should not return a fetch command (cmd should be nil)")
	}
}

func TestATDD_27_4_AC11_CacheMiss_TriggersFetch(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v'})

	if cmd == nil {
		t.Error("AC-11: cache miss should return a fetchDetailCmd")
	}
}

func TestATDD_27_4_AC11_DetailMsgPopulatesCache(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.detailCache = map[int]*ipc.GetStepDetailResponse{}

	detail := sampleStepDetail()
	updated, _ := m.Update(watchDetailMsg{step: 2, detail: detail, err: nil})
	um := updated.(watchModel)

	cached, ok := um.detailCache[2]
	if !ok {
		t.Fatal("AC-11: watchDetailMsg should populate detailCache for step 2")
	}
	if cached.SystemPrompt != detail.SystemPrompt {
		t.Errorf("AC-11: cached SystemPrompt mismatch: got %q", cached.SystemPrompt)
	}
}

func TestATDD_27_4_AC11_DetailMsgError_NoCacheEntry(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.detailCache = map[int]*ipc.GetStepDetailResponse{}

	updated, _ := m.Update(watchDetailMsg{step: 2, detail: nil, err: nil})
	um := updated.(watchModel)

	if _, ok := um.detailCache[2]; ok {
		t.Error("AC-11: nil detail should not be cached")
	}
}

// ---------------------------------------------------------------------------
// Integration: watchEventMsg step processing
// ---------------------------------------------------------------------------

func TestATDD_27_4_INT_StepEvent_AddsToStepList(t *testing.T) {
	m := newTestWatchModelWithSteps(nil)

	ev := ipc.StreamEvent{
		Type:    ipc.StreamProgress,
		Payload: progressPayloadJSON("step_complete", 1, false, 200),
	}
	updated, _ := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if len(um.steps) != 1 {
		t.Fatalf("INT: step_complete event should add to steps, got %d", len(um.steps))
	}
	if um.steps[0].step != 1 {
		t.Errorf("INT: added step should be step 1, got %d", um.steps[0].step)
	}
}

func TestATDD_27_4_INT_StepEvent_CursorFollowsLatest(t *testing.T) {
	m := newTestWatchModelWithSteps(nil)

	for i := 1; i <= 3; i++ {
		ev := ipc.StreamEvent{
			Type:    ipc.StreamProgress,
			Payload: progressPayloadJSON("step_complete", i, false, 200),
		}
		updated, _ := m.Update(watchEventMsg{event: ev})
		m = updated.(watchModel)
	}

	if m.cursor != len(m.steps)-1 {
		t.Errorf("INT: cursor should follow latest step, expected %d, got %d", len(m.steps)-1, m.cursor)
	}
}

func TestATDD_27_4_INT_ThinkingIndicator(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())

	ev := ipc.StreamEvent{
		Type:    ipc.StreamProgress,
		Payload: progressPayloadJSON("step", 5, false, 0),
	}
	updated, _ := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if um.thinkingStep != 5 {
		t.Errorf("INT: step event should set thinkingStep=5, got %d", um.thinkingStep)
	}

	v := um.View()
	if !strings.Contains(v.Content, "thinking") {
		t.Error("INT: view should show thinking indicator for in-progress step")
	}
}

func TestATDD_27_4_INT_CompleteEvent_SetsCompleted(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())

	pp := ipc.ProgressPayload{
		Event:    "complete",
		PID:      42,
		ExitCode: 0,
	}
	data, _ := json.Marshal(pp)
	ev := ipc.StreamEvent{
		Type:    ipc.StreamComplete,
		Payload: data,
	}

	updated, _ := m.Update(watchEventMsg{event: ev})
	um := updated.(watchModel)

	if !um.completed {
		t.Error("INT: complete event should set completed=true")
	}
	if um.exitCode != 0 {
		t.Errorf("INT: exitCode should be 0, got %d", um.exitCode)
	}
}

func TestATDD_27_4_INT_WindowSizeMsg(t *testing.T) {
	m := newTestWatchModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	um := updated.(watchModel)

	if um.width != 200 {
		t.Errorf("INT: width should be 200, got %d", um.width)
	}
	if um.height != 50 {
		t.Errorf("INT: height should be 50, got %d", um.height)
	}
}

// ---------------------------------------------------------------------------
// formatPromptForPager helper (tested directly)
// ---------------------------------------------------------------------------

func TestATDD_27_4_FormatPromptForPager_Structure(t *testing.T) {
	detail := sampleStepDetail()
	lines := formatPromptForPager(detail)

	if len(lines) == 0 {
		t.Fatal("formatPromptForPager should return non-empty lines")
	}

	content := strings.Join(lines, "\n")
	if !strings.Contains(content, "[System Prompt]") {
		t.Error("prompt format should contain [System Prompt] header")
	}
	if !strings.Contains(content, "[Messages") {
		t.Error("prompt format should contain [Messages] header")
	}
	if !strings.Contains(content, "[Tools") {
		t.Error("prompt format should contain [Tools] header")
	}
}

func TestATDD_27_4_FormatPromptForPager_MessageRoles(t *testing.T) {
	detail := sampleStepDetail()
	lines := formatPromptForPager(detail)
	content := strings.Join(lines, "\n")

	if !strings.Contains(content, "[user]") {
		t.Error("prompt format should show [user] role prefix")
	}
	if !strings.Contains(content, "[assistant]") {
		t.Error("prompt format should show [assistant] role prefix")
	}
}

func TestATDD_27_4_FormatPromptForPager_ToolDefs(t *testing.T) {
	detail := sampleStepDetail()
	lines := formatPromptForPager(detail)
	content := strings.Join(lines, "\n")

	if !strings.Contains(content, "read_file") {
		t.Error("prompt format should show tool name")
	}
	if !strings.Contains(content, "Read a file") {
		t.Error("prompt format should show tool description")
	}
}

// ---------------------------------------------------------------------------
// ASCII mode compatibility
// ---------------------------------------------------------------------------

func TestATDD_27_4_ASCIIMode_TreeLine(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.profile = ui.TerminalProfile{IsUnicode: false, ColorLevel: 0}
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	if strings.Contains(v.Content, "┊") {
		t.Error("ASCII mode should NOT use unicode tree characters")
	}
}

// ---------------------------------------------------------------------------
// Help bar rendering per state
// ---------------------------------------------------------------------------

func TestATDD_27_4_HelpBar_NormalState(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal

	v := m.View()
	content := v.Content
	if !strings.Contains(content, "Expand") || !strings.Contains(content, "[v]") {
		t.Error("Normal state help bar should show [v] Expand")
	}
	if !strings.Contains(content, "Prompt") || !strings.Contains(content, "[p]") {
		t.Error("Normal state help bar should show [p] Prompt")
	}
	if !strings.Contains(content, "Quit") || !strings.Contains(content, "[q]") {
		t.Error("Normal state help bar should show [q] Quit")
	}
}

func TestATDD_27_4_HelpBar_ExpandedState(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded
	m.expandLevel = 2
	m.cursor = 1
	m.detailCache = map[int]*ipc.GetStepDetailResponse{
		2: sampleStepDetail(),
	}

	v := m.View()
	content := v.Content
	if !strings.Contains(content, "Collapse") || !strings.Contains(content, "[v]") {
		t.Error("Expanded state help bar should show [v] Collapse")
	}
	if !strings.Contains(content, "Debug") || !strings.Contains(content, "[V]") {
		t.Error("Expanded state help bar should show [V] Debug")
	}
}
