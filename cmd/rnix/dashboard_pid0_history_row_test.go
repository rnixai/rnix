package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Fix: pauseHandler/resumeHandler PID=0 history-row sentinel
// (spec-resume-project-provider-fix.md, 目标 2).
//
// A process that failed to resume (or was reaped) appears in the dashboard as a
// history row with PID=0 but a valid UUID. The old `selectedPID == 0` sentinel
// treated such a row as "nothing selected" and emitted the misleading
// "Select a process first". After aligning the sentinel to hasProcessSelected()
// (= selectedPID>0 || selectedUUID!=""), a PID=0+UUID row is a valid selection:
//   - resumeHandler routes it to single-process UUID resume (resumeProcessCmd),
//     because a PID-keyed subtree resume is impossible with PID=0.
//   - pauseHandler falls through to the state-specific hint (e.g. "Cannot pause:
//     process is dead") instead of "Select a process first".
//   - A genuinely empty selection (PID=0 AND UUID="") still gets the original
//     "Select a process first".
//
// Shares withBogusSocket / newDashboardModel / resumeResultMsg with the AC#2/#3
// handler tests in the same package.
// =============================================================================

// historyRowModel builds a connected dashboardModel whose selection is a PID=0
// history row identified solely by UUID (mirrors a resume-failed / reaped proc).
func historyRowModel(state types.ProcessState) dashboardModel {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault
	m.connected = true
	m.selectedPID = types.PID(0)
	m.selectedUUID = "uuid-hist-row"
	m.processes = []vfs.ProcInfo{{
		PID:    types.PID(0),
		UUID:   "uuid-hist-row",
		State:  state,
		Intent: "pid0 history-row fixture",
	}}
	return m
}

// emptySelectionModel builds a connected dashboardModel with no selection at
// all (PID=0 AND UUID="").
func emptySelectionModel() dashboardModel {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault
	m.connected = true
	m.selectedPID = types.PID(0)
	m.selectedUUID = ""
	return m
}

func TestResumeHandler_PID0HistoryRow_Suspended_ResumesByUUID(t *testing.T) {
	withBogusSocket(t)
	m := historyRowModel(types.StateSuspended)

	consumed, ctx, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume a PID=0 history row")
	}
	if cmd == nil {
		t.Fatalf("resumeHandler(PID=0 Suspended) should return a non-nil resume cmd")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Resuming UUID") {
		t.Errorf("statusMsg = %q, want it to contain \"Resuming UUID\" "+
			"(not the misleading \"Select a process first\")", got)
	}
	// PID=0 cannot drive a PID-keyed subtree resume → must be single-process
	// UUID resume (resumeProcessCmd → resumeResultMsg), NOT resumeSubtreeCmd.
	if _, ok := cmd().(resumeResultMsg); !ok {
		t.Errorf("resumeHandler(PID=0 Suspended) must use single-process resumeProcessCmd (resumeResultMsg)")
	}
}

func TestResumeHandler_PID0HistoryRow_Dead_ResumesByUUID(t *testing.T) {
	withBogusSocket(t)
	m := historyRowModel(types.StateDead)

	consumed, ctx, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume a PID=0 Dead history row")
	}
	if cmd == nil {
		t.Fatalf("resumeHandler(PID=0 Dead) should return a non-nil resume cmd")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Resuming UUID") {
		t.Errorf("statusMsg = %q, want it to contain \"Resuming UUID\"", got)
	}
	if _, ok := cmd().(resumeResultMsg); !ok {
		t.Errorf("resumeHandler(PID=0 Dead) must use single-process resumeProcessCmd (resumeResultMsg)")
	}
}

func TestResumeHandler_EmptySelection_StillSelectFirst(t *testing.T) {
	m := emptySelectionModel()

	consumed, ctx, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume even with no selection")
	}
	if cmd != nil {
		t.Errorf("resumeHandler(no selection) should return nil cmd; got non-nil")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Select a process first") {
		t.Errorf("statusMsg = %q, want it to contain \"Select a process first\"", got)
	}
}

func TestPauseHandler_PID0HistoryRow_Dead_AccurateHint(t *testing.T) {
	m := historyRowModel(types.StateDead)

	consumed, ctx, cmd := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume a PID=0 Dead history row")
	}
	if cmd != nil {
		t.Errorf("pauseHandler(PID=0 Dead) should NOT issue a pause cmd; got non-nil")
	}
	got := ctx.(dashboardModel).statusMsg
	if strings.Contains(got, "Select a process first") {
		t.Errorf("pauseHandler(PID=0 Dead) must NOT emit the misleading "+
			"\"Select a process first\"; got %q", got)
	}
	if !strings.Contains(got, "Cannot pause") || !strings.Contains(got, "dead") {
		t.Errorf("statusMsg = %q, want it to mention \"Cannot pause\" + \"dead\"", got)
	}
}

func TestPauseHandler_EmptySelection_StillSelectFirst(t *testing.T) {
	m := emptySelectionModel()

	consumed, ctx, _ := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume even with no selection")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Select a process first") {
		t.Errorf("statusMsg = %q, want it to contain \"Select a process first\"", got)
	}
}
