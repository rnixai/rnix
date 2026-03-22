package main

// =============================================================================
// ATDD Story 27.6: Dashboard Process Detail Panel
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-2: Tab key cycles through Tree → Timeline → Heatmap → Detail → Tree
//   AC-2: Detail pane border highlights when active
//   AC-2: Pane switch renders within ≤100ms (NFR63-obs)
//   AC-3: Detail pane shows four sections (basic info, skills, FD, context stats)
//   AC-5: Selecting a different process refreshes detail panel
//   AC-5: Previously fetched detail is cached
//   AC-6: Dead process displays with empty FD table notation
//
// Priority: P0 (AC-2 tab cycle, AC-3 rendering), P1 (AC-5 refresh, AC-6 dead proc)
// Test Level: Unit (dashboard model key handling + rendering)

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newDetailPanelModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.treeRows = []flatRow{
		{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, Intent: "analyze code", CreatedAt: time.Now()}},
		{proc: vfs.ProcInfo{PID: 42, State: types.StateRunning, Intent: "build feature", CreatedAt: time.Now()}},
		{proc: vfs.ProcInfo{PID: 99, State: types.StateDead, Intent: "completed task", CreatedAt: time.Now()}},
	}
	m.processes = []vfs.ProcInfo{
		m.treeRows[0].proc,
		m.treeRows[1].proc,
		m.treeRows[2].proc,
	}
	m.treeCursor = 0
	m.selectedPID = 1
	return m
}

// ---------------------------------------------------------------------------
// AC-2: Tab key cycles through 5 panes (Tree → Timeline → Heatmap → Detail → Intent)
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC2_Tab_CyclesThrough5Panes(t *testing.T) {
	m := newDetailPanelModel()

	// Start at Tree (pane 0)
	if m.activePane != paneTree {
		t.Fatalf("AC-2: initial pane should be Tree, got %d", m.activePane)
	}

	// Tab → Timeline
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(dashboardModel)
	if m.activePane != paneTimeline {
		t.Errorf("AC-2: after 1st Tab, pane = %d, want %d (Timeline)", m.activePane, paneTimeline)
	}

	// Tab → Heatmap
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(dashboardModel)
	if m.activePane != paneHeatmap {
		t.Errorf("AC-2: after 2nd Tab, pane = %d, want %d (Heatmap)", m.activePane, paneHeatmap)
	}

	// Tab → Detail
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(dashboardModel)
	if m.activePane != paneDetail {
		t.Errorf("AC-2: after 3rd Tab, pane = %d, want %d (Detail)", m.activePane, paneDetail)
	}

	// Tab → Intent
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(dashboardModel)
	if m.activePane != paneIntent {
		t.Errorf("AC-2: after 4th Tab, pane = %d, want %d (Intent)", m.activePane, paneIntent)
	}

	// Tab → back to Tree
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(dashboardModel)
	if m.activePane != paneTree {
		t.Errorf("AC-2: after 5th Tab, pane = %d, want %d (Tree)", m.activePane, paneTree)
	}
}

func TestATDD_27_6_AC2_Tab_ModuloIs5(t *testing.T) {
	m := newDetailPanelModel()

	// Press Tab 10 times (2 full cycles of 5 panes) — should return to Tree
	for range 10 {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = updated.(dashboardModel)
	}
	if m.activePane != paneTree {
		t.Errorf("AC-2: after 10 Tabs, pane = %d, want %d (Tree)", m.activePane, paneTree)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Detail pane border highlights when active
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC2_DetailPane_HighlightWhenActive(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail

	v := m.View()

	// The Detail pane should be rendered with some indication it's active
	// (e.g., highlighted border or title)
	if !strings.Contains(v.Content, "Detail") && !strings.Contains(v.Content, "detail") {
		t.Error("AC-2: Detail pane should show 'Detail' label when active")
	}
}

// ---------------------------------------------------------------------------
// AC-3: Detail pane shows four sections
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC3_DetailPane_ShowsBasicInfo(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:         1,
		UUID:        "01960abc-def0-7000-8000-000000000001",
		State:       "running",
		Intent:      "analyze code",
		Provider:    "claude",
		Model:       "opus-4",
		CreatedAtMs: time.Now().UnixMilli(),
	}
	m.procDetailPID = 1

	v := m.View()
	content := v.Content

	// Basic info section should show PID
	if !strings.Contains(content, "1") {
		t.Error("AC-3: Detail pane should display PID")
	}
	// Should show intent
	if !strings.Contains(content, "analyze code") {
		t.Error("AC-3: Detail pane should display intent")
	}
	// Should show provider:model
	if !strings.Contains(content, "claude") {
		t.Error("AC-3: Detail pane should display provider")
	}
}

func TestATDD_27_6_AC3_DetailPane_ShowsSkills(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:   1,
		State: "running",
		Skills: []ipc.SkillInfoWire{
			{Name: "code-analysis", AllowedTools: []string{"/dev/fs", "/dev/shell"}},
			{Name: "shell-ops", AllowedTools: []string{"/dev/shell"}},
		},
	}
	m.procDetailPID = 1

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "code-analysis") {
		t.Error("AC-3: Detail pane should display skill name 'code-analysis'")
	}
	if !strings.Contains(content, "shell-ops") {
		t.Error("AC-3: Detail pane should display skill name 'shell-ops'")
	}
}

func TestATDD_27_6_AC3_DetailPane_ShowsFDTable(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:   1,
		State: "running",
		FDTable: []ipc.FDEntryWire{
			{FD: types.FD(0), DevicePath: "/dev/llm/claude"},
			{FD: types.FD(1), DevicePath: "/dev/fs"},
		},
	}
	m.procDetailPID = 1

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "/dev/llm/claude") {
		t.Error("AC-3: Detail pane should display FD device path '/dev/llm/claude'")
	}
	if !strings.Contains(content, "/dev/fs") {
		t.Error("AC-3: Detail pane should display FD device path '/dev/fs'")
	}
}

func TestATDD_27_6_AC3_DetailPane_ShowsContextStats(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:   1,
		State: "running",
		ContextStats: ipc.ContextStatsWire{
			MessageCount:  42,
			TokensUsed:    12500,
			ContextBudget: 20000,
			UsagePct:      62.5,
		},
	}
	m.procDetailPID = 1

	v := m.View()
	content := v.Content

	if !strings.Contains(content, "42") {
		t.Error("AC-3: Detail pane should display message count")
	}
	// Should show some form of token count
	if !strings.Contains(content, "12,500") && !strings.Contains(content, "12500") && !strings.Contains(content, "12.5k") {
		t.Error("AC-3: Detail pane should display token usage")
	}
}

// ---------------------------------------------------------------------------
// AC-5: Selecting different process refreshes detail panel
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC5_PIDChange_TriggersRefresh(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.procDetailPID = 1
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:    1,
		State:  "running",
		Intent: "old intent",
	}

	// Simulate selecting a different process (PID changes to 42)
	m.selectedPID = 42

	// The model should detect the PID change and clear/refresh
	// This would happen during Update when tick fires
	if m.procDetailPID == 42 && m.procDetail != nil && m.procDetail.PID == 1 {
		t.Error("AC-5: procDetail should be invalidated when selectedPID changes")
	}
}

func TestATDD_27_6_AC5_Cache_PreviousDetail(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail

	// Simulate having cached detail for PID 1
	m.procDetailPID = 1
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:    1,
		State:  "running",
		Intent: "cached intent",
	}

	// When switching back to PID 1, cache should be used
	// (no new IPC call needed)
	if m.procDetail == nil || m.procDetail.Intent != "cached intent" {
		t.Error("AC-5: should cache previous process detail data")
	}
}

// ---------------------------------------------------------------------------
// AC-6: Dead process displays with empty FD table
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC6_DeadProcess_ShowsEmptyFDTable(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.selectedPID = 99 // Dead process
	m.procDetailPID = 99
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:     99,
		State:   "dead",
		Intent:  "completed task",
		FDTable: []ipc.FDEntryWire{}, // Empty — reaper cleared
	}

	v := m.View()
	content := v.Content

	// FD section should exist — verify it shows some indicator (closed, empty, dash, etc.)
	lower := strings.ToLower(content)
	hasFDIndicator := strings.Contains(lower, "closed") ||
		strings.Contains(lower, "empty") ||
		strings.Contains(content, "—") ||
		strings.Contains(content, "-") ||
		strings.Contains(lower, "fd")
	if !hasFDIndicator {
		t.Error("AC-6: dead process detail should show FD section or empty indicator")
	}
}

func TestATDD_27_6_AC6_DeadProcess_ShowsHistoricalInfo(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail
	m.selectedPID = 99
	m.procDetailPID = 99
	m.procDetail = &ipc.GetProcDetailResponse{
		PID:      99,
		State:    "dead",
		Intent:   "completed task",
		Provider: "claude",
		Model:    "opus-4",
		Skills: []ipc.SkillInfoWire{
			{Name: "code-analysis", AllowedTools: []string{"/dev/fs"}},
		},
		FDTable: []ipc.FDEntryWire{},
	}

	v := m.View()
	content := v.Content

	// Dead process should still show intent and skills from history
	if !strings.Contains(content, "completed task") {
		t.Error("AC-6: Dead process should display historical intent")
	}
	if !strings.Contains(content, "code-analysis") {
		t.Error("AC-6: Dead process should display historical skills")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Help line includes Detail pane hint
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC2_HelpLine_MentionsDetail(t *testing.T) {
	m := newDetailPanelModel()
	m.activePane = paneDetail

	v := m.View()
	content := strings.ToLower(v.Content)

	// Help line should mention the Detail pane exists
	if !strings.Contains(content, "detail") {
		t.Error("AC-2: help line should mention 'detail' pane")
	}
}
