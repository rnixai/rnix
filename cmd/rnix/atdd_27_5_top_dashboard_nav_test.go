package main

// =============================================================================
// ATDD Story 27.5: top→dashboard Navigation
// =============================================================================
//
// Test Strategy:
//   AC-1: Enter in list view → launchDashboardPID set + tea.Quit
//   AC-2: dashboard --pid auto-focuses treeCursor to target PID
//   AC-3: --pid for non-existent PID → statusMsg warning
//   AC-4: Enter on Dead process → still sets launchDashboardPID
//   AC-6: Help line contains "dashboard" hint (not "Details")
//   AC-7: Enter in detail view → no dashboard jump
//
// AC-5 (q exits dashboard) is inherent BubbleTea behavior, not unit-testable.
//
// Priority: P0 (AC-1,2,3,7), P1 (AC-4,6), P2 (AC-5)
// Test Level: Unit (top model key handling + dashboard model tick/focus)

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTopNavModel() topModel {
	m := newTopModel(nil)
	m.width = 120
	m.height = 40
	m.rows = []flatRow{
		{Proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now()}},
		{Proc: vfs.ProcInfo{PID: 42, State: types.StateRunning, CreatedAt: time.Now()}},
		{Proc: vfs.ProcInfo{PID: 99, State: types.StateDead, CreatedAt: time.Now()}},
	}
	m.processes = []vfs.ProcInfo{
		m.rows[0].Proc,
		m.rows[1].Proc,
		m.rows[2].Proc,
	}
	m.cursor = 0
	return m
}

func newDashboardNavModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.tree.Rows = []flatRow{
		{Proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now()}},
		{Proc: vfs.ProcInfo{PID: 42, State: types.StateRunning, CreatedAt: time.Now()}},
		{Proc: vfs.ProcInfo{PID: 99, State: types.StateRunning, CreatedAt: time.Now()}},
	}
	m.processes = []vfs.ProcInfo{
		m.tree.Rows[0].Proc,
		m.tree.Rows[1].Proc,
		m.tree.Rows[2].Proc,
	}
	m.tree.Cursor = 0
	m.selectedPID = 1
	return m
}

// ---------------------------------------------------------------------------
// AC-1: Enter in list view → launchDashboardPID set + quit
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC1_Enter_SetsLaunchDashboardPID(t *testing.T) {
	m := newTopNavModel()
	m.cursor = 1 // PID 42

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)

	if um.launchDashboardPID != 42 {
		t.Errorf("AC-1: launchDashboardPID = %d, want 42", um.launchDashboardPID)
	}
}

func TestATDD_27_5_AC1_Enter_ReturnsQuitCmd(t *testing.T) {
	m := newTopNavModel()
	m.cursor = 0

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Error("AC-1: Enter should return a non-nil quit command")
	}
}

func TestATDD_27_5_AC1_Enter_DoesNotSetDetailPID(t *testing.T) {
	m := newTopNavModel()
	m.cursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)

	if um.detailPID != 0 {
		t.Errorf("AC-1: detailPID should remain 0 after Enter (dashboard jump), got %d", um.detailPID)
	}
}

// ---------------------------------------------------------------------------
// AC-2: dashboard --pid auto-focuses treeCursor
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC2_InitialPIDFocus_PositionsCursor(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 42

	m.applyInitialPIDFocus()

	if m.tree.Cursor != 1 {
		t.Errorf("AC-2: treeCursor = %d, want 1 (PID 42 row)", m.tree.Cursor)
	}
}

func TestATDD_27_5_AC2_InitialPIDFocus_SetsSelectedPID(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 42

	m.applyInitialPIDFocus()
	if m.tree.Cursor < len(m.tree.Rows) {
		m.selectedPID = m.tree.Rows[m.tree.Cursor].Proc.PID
	}

	if m.selectedPID != 42 {
		t.Errorf("AC-2: selectedPID = %d, want 42", m.selectedPID)
	}
}

func TestATDD_27_5_AC2_InitialPIDFocus_ClearsAfterApply(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 42

	m.applyInitialPIDFocus()

	if m.initialPIDFocus != 0 {
		t.Errorf("AC-2: initialPIDFocus should be cleared to 0, got %d", m.initialPIDFocus)
	}
}

// ---------------------------------------------------------------------------
// AC-3: --pid for non-existent PID → statusMsg warning
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC3_InitialPIDFocus_NotFound_StatusMsg(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 999

	m.applyInitialPIDFocus()

	if !strings.Contains(m.statusMsg, "999") {
		t.Errorf("AC-3: statusMsg should mention PID 999, got %q", m.statusMsg)
	}
	if !strings.Contains(strings.ToLower(m.statusMsg), "not found") {
		t.Errorf("AC-3: statusMsg should say 'not found', got %q", m.statusMsg)
	}
}

func TestATDD_27_5_AC3_InitialPIDFocus_NotFound_CursorDefault(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 999

	m.applyInitialPIDFocus()

	if m.tree.Cursor != 0 {
		t.Errorf("AC-3: treeCursor should stay at 0, got %d", m.tree.Cursor)
	}
}

func TestATDD_27_5_AC3_InitialPIDFocus_NotFound_ClearsFlag(t *testing.T) {
	m := newDashboardNavModel()
	m.initialPIDFocus = 999

	m.applyInitialPIDFocus()

	if m.initialPIDFocus != 0 {
		t.Errorf("AC-3: initialPIDFocus should be cleared, got %d", m.initialPIDFocus)
	}
}

// ---------------------------------------------------------------------------
// AC-4: Enter on Dead process → still launches dashboard
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC4_Enter_DeadProcess_SetsLaunchDashboardPID(t *testing.T) {
	m := newTopNavModel()
	m.cursor = 2 // PID 99, Dead state

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)

	if um.launchDashboardPID != 99 {
		t.Errorf("AC-4: launchDashboardPID = %d, want 99 (dead process)", um.launchDashboardPID)
	}
}

func TestATDD_27_5_AC4_Enter_DeadProcess_ReturnsQuitCmd(t *testing.T) {
	m := newTopNavModel()
	m.cursor = 2 // Dead process

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Error("AC-4: Enter on dead process should still return quit command")
	}
}

// ---------------------------------------------------------------------------
// AC-6: Help line text includes "dashboard" hint
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC6_HelpLine_ContainsDashboard(t *testing.T) {
	m := newTopNavModel()

	v := m.View()

	if !strings.Contains(strings.ToLower(v.Content), "dashboard") {
		t.Errorf("AC-6: help line should mention 'dashboard', got: %s", v.Content)
	}
}

func TestATDD_27_5_AC6_HelpLine_NoDetailsLabel(t *testing.T) {
	m := newTopNavModel()

	v := m.View()

	if strings.Contains(v.Content, "[Enter] Details") {
		t.Error("AC-6: help line should not use old '[Enter] Details' text")
	}
}

func TestATDD_27_5_AC6_DetailView_HelpLine_NoEnterDashboard(t *testing.T) {
	m := newTopNavModel()
	m.detailPID = 1

	v := m.View()

	if strings.Contains(strings.ToLower(v.Content), "enter") && strings.Contains(strings.ToLower(v.Content), "dashboard") {
		t.Error("AC-6: detail view help should not show Enter→dashboard hint")
	}
}

// ---------------------------------------------------------------------------
// AC-7: Enter in detail view → no dashboard jump
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC7_DetailView_Enter_NoDashboardJump(t *testing.T) {
	m := newTopNavModel()
	m.detailPID = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)

	if um.launchDashboardPID != 0 {
		t.Errorf("AC-7: launchDashboardPID should be 0 in detail view, got %d", um.launchDashboardPID)
	}
}

func TestATDD_27_5_AC7_DetailView_Enter_ReturnsNilCmd(t *testing.T) {
	m := newTopNavModel()
	m.detailPID = 1

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd != nil {
		t.Error("AC-7: Enter in detail view should not return a command (no quit)")
	}
}

func TestATDD_27_5_AC7_DetailView_StaysInDetail(t *testing.T) {
	m := newTopNavModel()
	m.detailPID = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(topModel)

	if um.detailPID != 1 {
		t.Errorf("AC-7: detailPID should stay at 1 in detail view, got %d", um.detailPID)
	}
}
