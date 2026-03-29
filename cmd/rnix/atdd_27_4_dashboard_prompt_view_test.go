package main

// =============================================================================
// ATDD Story 27.4: Dashboard Prompt View (Prompt Pager)
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: P key enters Prompt Pager (with GetStepDetail fetch or cache hit)
//   AC-2: Pager scrolling (j/k, PgUp/PgDn, Home/End via viewport)
//   AC-3: q key returns to Dashboard (preserves stepCursor & activePane)
//   AC-5: Prompt content formatting (System/Messages/Tools sections)
//   AC-6: Cache reuse (no IPC call when cache exists)
//   AC-7: No step → P key is no-op
//   Extra: PID change exits pager, Escape exits pager, WindowResize in pager
//
// Note: AC-4 (offline viewing) is an IPC/server-side concern — not testable
// at the dashboard model unit level. The dashboard always calls GetStepDetail
// the same way regardless of online/offline; the server decides the data source.
//
// Priority: P0 (AC-1,3,5,6,7), P1 (AC-2), P2 (extra)
// Test Level: Unit (dashboard model + rendering)

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newPromptPagerModel() dashboardModel {
	m := newStepTimelineModel() // reuse 27-3 helper
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		SystemPrompt: "You are a code analyst agent. Analyze code quality.",
		Step:         1,
		Action:       "tool_call",
		Messages: []ipc.MessageWire{
			{Role: "system", Content: "You are a code analyst agent."},
			{Role: "user", Content: "请帮我分析这段代码的性能问题"},
			{Role: "assistant", Content: "我来分析一下。首先让我读取文件..."},
			{Role: "assistant", Content: "代码分析完成。主要问题是..."},
		},
		Tools: []ipc.ToolDefWire{
			{Name: "read_file", Description: "Read a file from the virtual filesystem"},
			{Name: "write_file", Description: "Write content to a file"},
			{Name: "shell_exec", Description: "Execute a shell command"},
		},
		MessageCount:   4,
		TokenCount:     12500,
		RequestTokens:  2340,
		ResponseTokens: 156,
	}
	return m
}

// ---------------------------------------------------------------------------
// AC-1: P 键进入 Prompt Pager
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC1_PKey_EntersPromptPager_CacheHit(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("AC-1: after p key with cache hit, promptPager should be true")
	}
}

func TestATDD_27_4_AC1_PKey_SetsPromptStep(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.promptStep != 1 {
		t.Errorf("AC-1: promptStep = %d, want 1", model.promptStep)
	}
}

func TestATDD_27_4_AC1_PKey_SetsPromptContent(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.promptContent == "" {
		t.Error("AC-1: promptContent should be non-empty after entering pager")
	}
}

func TestATDD_27_4_AC1_PKey_CacheMiss_ReturnsCmd(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	// No cache entry for step 1

	_, cmd := m.Update(tea.KeyPressMsg{Code: 80})

	if cmd == nil {
		t.Error("AC-1: cache miss should return a fetch Cmd, got nil")
	}
}

func TestATDD_27_4_AC1_PKey_CacheMiss_SetsFetchingDetail(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if !model.fetchingDetail {
		t.Error("AC-1: cache miss should set fetchingDetail = true")
	}
}

func TestATDD_27_4_AC1_PromptPagerMsg_EntersPager(t *testing.T) {
	m := newStepTimelineModel()
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "You are an agent.",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hello"}},
		MessageCount: 1,
		TokenCount:   500,
	}

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, detail: detail})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("AC-1: promptPagerMsg should set promptPager = true")
	}
}

func TestATDD_27_4_AC1_PromptPagerMsg_CachesDetail(t *testing.T) {
	m := newStepTimelineModel()
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "You are an agent.",
		Step:         1,
		MessageCount: 1,
	}

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, detail: detail})

	model := m2.(dashboardModel)
	if model.stepDetailCache[1] == nil {
		t.Error("AC-1: promptPagerMsg should cache the detail response")
	}
}

func TestATDD_27_4_AC1_PromptPagerMsg_ClearsFetchingDetail(t *testing.T) {
	m := newStepTimelineModel()
	m.fetchingDetail = true
	detail := &ipc.GetStepDetailResponse{Step: 1}

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, detail: detail})

	model := m2.(dashboardModel)
	if model.fetchingDetail {
		t.Error("AC-1: promptPagerMsg should clear fetchingDetail")
	}
}

func TestATDD_27_4_AC1_PromptPagerMsg_Error_NoPager(t *testing.T) {
	m := newStepTimelineModel()
	m.fetchingDetail = true

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, err: fmt.Errorf("dial error")})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("AC-1: promptPagerMsg with error should NOT enter pager")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Pager 滚动 (viewport handles j/k/PgUp/PgDn/Home/End)
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC2_PagerMode_KeysForwardToViewport(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})
	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Skip("AC-2: pager not entered, skipping scroll test")
	}

	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	_ = m3.(dashboardModel)
	// Viewport scrolling is handled by bubbles/viewport — we verify that
	// j/k in pager mode do NOT exit the pager or trigger other dashboard keys
	m3model := m3.(dashboardModel)
	if !m3model.promptPager {
		t.Error("AC-2: j key in pager mode should NOT exit pager")
	}
}

func TestATDD_27_4_AC2_PagerMode_KKey_StaysInPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("AC-2: k key in pager mode should NOT exit pager")
	}
}

// ---------------------------------------------------------------------------
// AC-3: q 键返回 Dashboard
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC3_QKey_ExitsPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'q'})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("AC-3: q key in pager mode should set promptPager = false")
	}
}

func TestATDD_27_4_AC3_QKey_PreservesStepCursor(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1
	m.stepCursor = 2

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'q'})

	model := m2.(dashboardModel)
	if model.stepCursor != 2 {
		t.Errorf("AC-3: q should preserve stepCursor, got %d want 2", model.stepCursor)
	}
}

func TestATDD_27_4_AC3_QKey_PreservesActivePane(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1
	m.activePane = paneTimeline

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'q'})

	model := m2.(dashboardModel)
	if model.activePane != paneTimeline {
		t.Errorf("AC-3: q should preserve activePane, got %d want %d", model.activePane, paneTimeline)
	}
}

func TestATDD_27_4_AC3_QKey_DoesNotQuitDashboard(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})

	// In non-pager mode, q returns tea.Quit. In pager mode, it should return nil.
	if cmd != nil {
		t.Error("AC-3: q in pager should NOT return tea.Quit cmd")
	}
}

func TestATDD_27_4_AC3_EscapeKey_ExitsPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "test content"
	m.promptStep = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: rune(tea.KeyEscape)})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("AC-3: Escape key in pager mode should exit pager")
	}
}

// ---------------------------------------------------------------------------
// AC-5: Prompt 内容格式化渲染
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC5_FormatPromptContent_SystemPromptSection(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "You are a code analyst agent. Analyze code quality.",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hello"}},
		MessageCount: 1,
		TokenCount:   500,
	}

	content := formatPromptContent(detail, 1, promptTabSystem)

	if !strings.Contains(content, "System Prompt") {
		t.Error("AC-5: should contain 'System Prompt' section header")
	}
	if !strings.Contains(content, "You are a code analyst agent") {
		t.Error("AC-5: should contain the full system prompt text")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_MessagesSection(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys prompt",
		Step:         1,
		Messages: []ipc.MessageWire{
			{Role: "user", Content: "请帮我分析这段代码"},
			{Role: "assistant", Content: "我来看看..."},
		},
		MessageCount: 2,
		TokenCount:   1000,
	}

	content := formatPromptContent(detail, 1, promptTabMessages)

	if !strings.Contains(content, "user") {
		t.Error("AC-5: should show 'user' role label")
	}
	if !strings.Contains(content, "assistant") {
		t.Error("AC-5: should show 'assistant' role label")
	}
	if !strings.Contains(content, "请帮我分析这段代码") {
		t.Error("AC-5: should show user message content")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_ToolsSection(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Tools: []ipc.ToolDefWire{
			{Name: "read_file", Description: "Read a file from the virtual filesystem"},
			{Name: "shell_exec", Description: "Execute a shell command"},
		},
		MessageCount: 1,
		TokenCount:   500,
	}

	content := formatPromptContent(detail, 1, promptTabTools)

	if !strings.Contains(content, "Tools") {
		t.Error("AC-5: should contain 'Tools' section header")
	}
	if !strings.Contains(content, "read_file") {
		t.Error("AC-5: should show tool name 'read_file'")
	}
	if !strings.Contains(content, "shell_exec") {
		t.Error("AC-5: should show tool name 'shell_exec'")
	}
	if !strings.Contains(content, "Read a file") {
		t.Error("AC-5: should show tool description")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_SectionSeparators(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys prompt",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
		Tools:        []ipc.ToolDefWire{{Name: "t1", Description: "desc"}},
		MessageCount: 2,
		TokenCount:   100,
	}

	// System tab has section header
	sysContent := formatPromptContent(detail, 1, promptTabSystem)
	if !strings.Contains(sysContent, "═══") {
		t.Error("AC-5: System tab should have section separator (═══)")
	}

	// Messages tab has dividers between messages
	msgContent := formatPromptContent(detail, 1, promptTabMessages)
	if !strings.Contains(msgContent, "─") {
		t.Error("AC-5: Messages tab should have dividers (─) between messages")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_MessageCount(t *testing.T) {
	// Message count is shown in the Prompt Viewer title bar, not in tab content.
	// Verify that messages tab renders all messages.
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hi"}},
		MessageCount: 23,
		TokenCount:   12500,
	}

	content := formatPromptContent(detail, 1, promptTabMessages)

	if !strings.Contains(content, "hi") {
		t.Error("AC-5: Messages tab should contain message content")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_ToolCount(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Tools: []ipc.ToolDefWire{
			{Name: "t1"}, {Name: "t2"}, {Name: "t3"}, {Name: "t4"}, {Name: "t5"},
		},
		MessageCount: 1,
		TokenCount:   500,
	}

	content := formatPromptContent(detail, 1, promptTabTools)

	if !strings.Contains(content, "5") {
		t.Error("AC-5: Tools section should show tool count '5'")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_EmptySystemPrompt(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hi"}},
		MessageCount: 1,
		TokenCount:   100,
	}

	content := formatPromptContent(detail, 1, promptTabSystem)

	if !strings.Contains(content, "System Prompt") {
		t.Error("AC-5: should still show System Prompt section even when empty")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_NoTools(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Messages:     []ipc.MessageWire{{Role: "user", Content: "hi"}},
		Tools:        nil,
		MessageCount: 1,
		TokenCount:   100,
	}

	content := formatPromptContent(detail, 1, promptTabTools)

	if !strings.Contains(content, "No tool information available") {
		t.Error("AC-5: Tools section should show 'No tool information available' when no tools and no action")
	}
}

func TestATDD_27_4_AC5_FormatPromptContent_ToolRoleMessage(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Messages: []ipc.MessageWire{
			{Role: "user", Content: "read the file"},
			{Role: "assistant", Content: "calling tool", ToolCalls: []ipc.ToolCallWire{{ID: "tc1", Name: "read_file"}}},
			{Role: "tool", Content: "file contents here", ToolCallID: "tc1"},
		},
		MessageCount: 3,
		TokenCount:   2000,
	}

	content := formatPromptContent(detail, 1, promptTabMessages)

	if !strings.Contains(content, "tool") {
		t.Error("AC-5: should show 'tool' role for tool result messages")
	}
}

// ---------------------------------------------------------------------------
// AC-6: 缓存复用
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC6_CacheHit_NoFetchCmd(t *testing.T) {
	m := newPromptPagerModel() // cache pre-filled for step 1
	m.stepCursor = 0

	_, cmd := m.Update(tea.KeyPressMsg{Code: 80})

	if cmd != nil {
		t.Error("AC-6: cache hit should NOT return a fetch Cmd")
	}
}

func TestATDD_27_4_AC6_CacheHit_ImmediatePager(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("AC-6: cache hit should immediately enter pager (no async wait)")
	}
}

func TestATDD_27_4_AC6_CacheHit_NoFetchingDetailFlag(t *testing.T) {
	m := newPromptPagerModel()
	m.stepCursor = 0
	m.fetchingDetail = false

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.fetchingDetail {
		t.Error("AC-6: cache hit should not set fetchingDetail")
	}
}

// ---------------------------------------------------------------------------
// AC-7: 无步骤时 p 键无效
// ---------------------------------------------------------------------------

func TestATDD_27_4_AC7_NoSteps_PKey_Noop(t *testing.T) {
	m := newStepTimelineModel()
	m.stepEntries = nil // empty

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("AC-7: p key with no steps should NOT enter pager")
	}
	if cmd != nil {
		t.Error("AC-7: p key with no steps should return nil cmd")
	}
}

func TestATDD_27_4_AC7_EmptyStepEntries_PKey_Silent(t *testing.T) {
	m := newStepTimelineModel()
	m.stepEntries = []stepEntry{} // empty slice

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("AC-7: p key with empty stepEntries should NOT enter pager")
	}
}

// ---------------------------------------------------------------------------
// Extra: PID 切换退出 pager
// ---------------------------------------------------------------------------

func TestATDD_27_4_Extra_PIDChange_ExitsPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "old content"
	m.promptStep = 1
	m.timelineAttachedUUID = "uuid-1"
	m.selectedPID = 2
	m.selectedUUID = "uuid-2" // force process change

	m2 := m.handleTimelinePIDChange()

	if m2.promptPager {
		t.Error("Extra: PID change should reset promptPager = false")
	}
}

// ---------------------------------------------------------------------------
// Extra: View() 渲染 — pager 模式覆盖三窗格布局
// ---------------------------------------------------------------------------

func TestATDD_27_4_Extra_View_PagerMode_OverridesDashboard(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "═══ System Prompt ═══\nYou are an agent."
	m.promptStep = 1
	m.width = 120
	m.height = 40

	output := m.renderDashboard()

	if strings.Contains(output, "Rnix Dashboard") {
		t.Error("Extra: pager mode View() should NOT show dashboard title")
	}
}

func TestATDD_27_4_AC2_PagerMode_RenderShowsContent(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "═══ System Prompt ═══\nYou are an agent."
	m.promptStep = 1
	m.width = 120
	m.height = 40

	output := m.renderPromptPager()

	if output == "" {
		t.Error("AC-2: renderPromptPager should return non-empty content")
	}
}

func TestATDD_27_4_AC2_PagerMode_ShowsHelpBar(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "some content"
	m.promptStep = 1
	m.width = 120
	m.height = 40

	output := m.renderPromptPager()

	if !strings.Contains(output, "q") || !strings.Contains(output, "back") {
		t.Error("AC-2: pager should show help bar with q:back")
	}
}

func TestATDD_27_4_AC2_PagerMode_ShowsTitleBar(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "some content"
	m.promptStep = 1
	m.selectedPID = 42
	m.width = 120
	m.height = 40

	output := m.renderPromptPager()

	if !strings.Contains(output, "Prompt View") {
		t.Error("AC-2: pager title bar should contain 'Prompt View'")
	}
}

// ---------------------------------------------------------------------------
// Extra: WindowResize 同步 viewport 尺寸
// ---------------------------------------------------------------------------

func TestATDD_27_4_Extra_WindowResize_InPagerMode(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "content"
	m.promptStep = 1
	m.width = 80
	m.height = 24

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	model := m2.(dashboardModel)
	if model.width != 120 || model.height != 40 {
		t.Errorf("Extra: WindowResize should update dimensions, got %dx%d", model.width, model.height)
	}
}

// ---------------------------------------------------------------------------
// Extra: fetchingDetail 互斥 — p key while already fetching
// ---------------------------------------------------------------------------

func TestATDD_27_4_Extra_PKey_WhileFetching_Noop(t *testing.T) {
	m := newStepTimelineModel()
	m.stepCursor = 0
	m.fetchingDetail = true // already fetching

	_, cmd := m.Update(tea.KeyPressMsg{Code: 80})

	if cmd != nil {
		t.Error("Extra: p key while fetchingDetail=true should NOT issue another fetch")
	}
}

// ---------------------------------------------------------------------------
// Extra: p 键仅在 Timeline 面板下工作（Syscall 模式已移除，此测试已过时）
// ---------------------------------------------------------------------------

func TestATDD_27_4_Extra_PKey_NotInStepTimelineMode_Noop(t *testing.T) {
	// With Syscall mode removed, step timeline is always active.
	// This test validates that p does nothing when no steps are loaded.
	m := newPromptPagerModel()
	m.stepEntries = nil // no steps

	m2, _ := m.Update(tea.KeyPressMsg{Code: 80})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("Extra: p key with no steps should NOT enter pager")
	}
}

// ---------------------------------------------------------------------------
// CR Fix: PID mismatch discards stale promptPagerMsg
// ---------------------------------------------------------------------------

func TestATDD_27_4_CR_PromptPagerMsg_PIDMismatch_Discarded(t *testing.T) {
	m := newStepTimelineModel()
	m.selectedPID = 2
	m.fetchingDetail = true
	detail := &ipc.GetStepDetailResponse{Step: 1, SystemPrompt: "stale"}

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, detail: detail})

	model := m2.(dashboardModel)
	if model.promptPager {
		t.Error("CR: promptPagerMsg with mismatched PID should NOT enter pager")
	}
	if model.stepDetailCache[1] != nil {
		t.Error("CR: promptPagerMsg with mismatched PID should NOT cache detail")
	}
}

// ---------------------------------------------------------------------------
// CR Fix: IPC error shows statusMsg
// ---------------------------------------------------------------------------

func TestATDD_27_4_CR_PromptPagerMsg_Error_ShowsStatusMsg(t *testing.T) {
	m := newStepTimelineModel()
	m.fetchingDetail = true

	m2, _ := m.Update(promptPagerMsg{pid: 1, step: 1, err: fmt.Errorf("connection refused")})

	model := m2.(dashboardModel)
	if model.statusMsg == "" {
		t.Error("CR: promptPagerMsg with error should set statusMsg")
	}
	if !strings.Contains(model.statusMsg, "prompt load") {
		t.Errorf("CR: statusMsg should mention prompt load, got %q", model.statusMsg)
	}
}

// ---------------------------------------------------------------------------
// CR Fix: ctrl+c in pager exits program
// ---------------------------------------------------------------------------

func TestATDD_27_4_CR_CtrlC_InPager_Quits(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "content"
	m.promptStep = 1

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if cmd == nil {
		t.Error("CR: ctrl+c in pager should return a Cmd (tea.Quit)")
	}
}

// ---------------------------------------------------------------------------
// CR Fix: Home/End keys in pager
// ---------------------------------------------------------------------------

func TestATDD_27_4_CR_HomeKey_StaysInPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "content"
	m.promptStep = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: rune(tea.KeyHome)})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("CR: Home key in pager should NOT exit pager")
	}
}

func TestATDD_27_4_CR_EndKey_StaysInPager(t *testing.T) {
	m := newPromptPagerModel()
	m.promptPager = true
	m.promptContent = "content"
	m.promptStep = 1

	m2, _ := m.Update(tea.KeyPressMsg{Code: rune(tea.KeyEnd)})

	model := m2.(dashboardModel)
	if !model.promptPager {
		t.Error("CR: End key in pager should NOT exit pager")
	}
}

// ---------------------------------------------------------------------------
// CR Fix: Tool role shows tool name instead of ToolCallID
// ---------------------------------------------------------------------------

func TestATDD_27_4_CR_FormatRoleTag_ToolName(t *testing.T) {
	toolCallNames := map[string]string{"tc1": "read_file"}
	msg := ipc.MessageWire{Role: "tool", ToolCallID: "tc1"}
	tag := formatRoleTag(msg, toolCallNames)
	if !strings.Contains(tag, "read_file") {
		t.Errorf("CR: tool role tag should contain tool name 'read_file', got %q", tag)
	}
}

func TestATDD_27_4_CR_FormatRoleTag_ToolFallbackToID(t *testing.T) {
	toolCallNames := map[string]string{}
	msg := ipc.MessageWire{Role: "tool", ToolCallID: "tc1"}
	tag := formatRoleTag(msg, toolCallNames)
	if !strings.Contains(tag, "tc1") {
		t.Errorf("CR: tool role tag should fallback to ToolCallID 'tc1', got %q", tag)
	}
}

func TestATDD_27_4_CR_FormatPromptContent_ToolNameResolved(t *testing.T) {
	detail := &ipc.GetStepDetailResponse{
		SystemPrompt: "sys",
		Step:         1,
		Messages: []ipc.MessageWire{
			{Role: "assistant", Content: "calling tool", ToolCalls: []ipc.ToolCallWire{{ID: "tc1", Name: "read_file"}}},
			{Role: "tool", Content: "file contents", ToolCallID: "tc1"},
		},
		MessageCount: 2,
		TokenCount:   1000,
	}

	content := formatPromptContent(detail, 1, promptTabMessages)

	if !strings.Contains(content, "read_file") {
		t.Error("CR: formatted prompt should show tool name 'read_file' in tool role tag")
	}
}
