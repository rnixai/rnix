package main

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.4 — AC#4: status bar shows an explicit "press r to resume subtree"
// hint when the selected process is Suspended (incl. daemon-restart
// placeholders), and stays quiet otherwise.
//
// RED expression (behavioural): renderDashboardStatus currently renders only
// the generic default-view hints ("j/k nav · s/S sort · z expand · p pause ·
// f filter · ? help") with no "resume" affordance for a Suspended selection.
// Asserting the rendered output contains "resume" fails today → RED; passes
// once dev-story Task 4.1 adds the Suspended inline hint.
//
// Note: renderDashboardStatus early-returns when statusMsg != "", so the
// fixtures keep statusMsg empty to exercise the hint branch.
// =============================================================================

func statusHintModel(state types.ProcessState) dashboardModel {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault
	m.connected = true
	m.statusMsg = ""
	m.confirmKill = false
	m.replayMode = false
	m.selectedPID = types.PID(300)
	m.selectedUUID = "uuid-44-4-status"
	m.processes = []vfs.ProcInfo{{
		PID:    types.PID(300),
		UUID:   "uuid-44-4-status",
		State:  state,
		Intent: "status hint fixture",
	}}
	return m
}

func TestATDD_44_4_040_StatusBar_Suspended_ShowsResumeHint(t *testing.T) {
	m := statusHintModel(types.StateSuspended)

	out := strings.ToLower(m.renderDashboardStatus())
	if !strings.Contains(out, "resume") {
		t.Errorf("renderDashboardStatus() for a Suspended selection = %q, want it to contain "+
			"a \"resume\" hint (AC#4 \"press r to resume subtree\")", out)
	}
}

func TestATDD_44_4_041_StatusBar_Running_NoResumeHint(t *testing.T) {
	m := statusHintModel(types.StateRunning)

	out := strings.ToLower(m.renderDashboardStatus())
	if strings.Contains(out, "resume") {
		t.Errorf("renderDashboardStatus() for a Running selection = %q, must NOT contain a "+
			"\"resume\" hint (AC#4: hint is Suspended-only, avoid noise)", out)
	}
}
