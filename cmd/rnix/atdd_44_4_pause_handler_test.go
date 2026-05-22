package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.4 — AC#2: dashboard `p` (pauseHandler) routes to PauseSubtree and
// the IsPaused dead-branch is removed.
//
// Two RED expressions, both behavioural (no compile-fail — pauseHandler
// already exists):
//
//  1. Suspended-state statusMsg: current pauseHandler hits the
//     `State != Running && != Created` guard and emits
//     "Cannot pause: process is suspended"; AC#2 wants the dedicated
//     "Already suspended — press r to resume". Asserting the new string fails
//     against the current message → RED.
//
//  2. Running-state command identity: current pauseHandler returns
//     pauseTreeCmd(SIGPAUSE) whose deferred message is pauseToggleMsg. AC#2
//     replaces it with pauseTreeSubtreeCmd (client.PauseSubtree), which must
//     emit a NON-pauseToggleMsg. We invoke the returned cmd against a bogus
//     socket (no daemon touched) and assert the message is NOT a pauseToggleMsg
//     → RED today, GREEN once dev-story Task 2.2 lands the new cmd.
//
// `ipc.SocketPathOverride` is restored via t.Cleanup; these tests do not run
// in parallel so the global override is safe.
// =============================================================================

// withBogusSocket points ipc.Dial at a non-existent unix socket so any cmd that
// dials the daemon fails fast (ENOENT) without contacting a real daemon, while
// still returning its concrete tea.Msg type for identity assertions.
func withBogusSocket(t *testing.T) {
	t.Helper()
	prev := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/nonexistent/rnix-atdd-44-4.sock"
	t.Cleanup(func() { ipc.SocketPathOverride = prev })
}

// pauseTestModel builds a connected dashboardModel with a single selected
// process in the given state.
func pauseTestModel(state types.ProcessState, isPaused bool) dashboardModel {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault
	m.connected = true
	m.selectedPID = types.PID(100)
	m.selectedUUID = "uuid-44-4-pause"
	m.processes = []vfs.ProcInfo{{
		PID:      types.PID(100),
		UUID:     "uuid-44-4-pause",
		State:    state,
		IsPaused: isPaused,
		Intent:   "pause handler fixture",
	}}
	return m
}

func TestATDD_44_4_020_PauseHandler_Running_ProducesSubtreeCmd(t *testing.T) {
	withBogusSocket(t)
	m := pauseTestModel(types.StateRunning, false)

	consumed, _, cmd := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume on a Running selected process")
	}
	if cmd == nil {
		t.Fatalf("pauseHandler(Running) should return a non-nil pause cmd")
	}
	msg := cmd()
	if _, isLegacy := msg.(pauseToggleMsg); isLegacy {
		t.Errorf("pauseHandler(Running) returned pauseToggleMsg (legacy SignalTree path); "+
			"AC#2 requires a subtree pause cmd (client.PauseSubtree). got %T", msg)
	}
}

func TestATDD_44_4_021_PauseHandler_Created_CanPause(t *testing.T) {
	withBogusSocket(t)
	m := pauseTestModel(types.StateCreated, false)

	consumed, _, cmd := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume on a Created selected process")
	}
	if cmd == nil {
		t.Errorf("pauseHandler(Created) should return a non-nil pause cmd (Created is pausable)")
	}
}

func TestATDD_44_4_022_PauseHandler_Suspended_GivesResumeHint(t *testing.T) {
	m := pauseTestModel(types.StateSuspended, false)

	consumed, ctx, cmd := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume on a Suspended selected process")
	}
	if cmd != nil {
		t.Errorf("pauseHandler(Suspended) should NOT issue a pause cmd; got non-nil")
	}
	got := ctx.(dashboardModel).statusMsg
	if !strings.Contains(got, "Already suspended") {
		t.Errorf("pauseHandler(Suspended) statusMsg = %q, want it to contain "+
			"\"Already suspended\" (AC#2 dedicated hint, not the generic \"Cannot pause\")", got)
	}
}

func TestATDD_44_4_023_PauseHandler_Dead_CannotPause(t *testing.T) {
	m := pauseTestModel(types.StateDead, false)

	consumed, ctx, cmd := pauseHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("pauseHandler should consume on a Dead selected process")
	}
	if cmd != nil {
		t.Errorf("pauseHandler(Dead) should NOT issue a pause cmd; got non-nil")
	}
	got := ctx.(dashboardModel).statusMsg
	if !strings.Contains(got, "Cannot pause") || !strings.Contains(got, "dead") {
		t.Errorf("pauseHandler(Dead) statusMsg = %q, want it to mention \"Cannot pause\" + \"dead\"", got)
	}
}
