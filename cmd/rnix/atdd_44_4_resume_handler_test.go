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
// ATDD 44.4 — AC#3: dashboard `r` (resumeHandler) Suspended branch switches
// from single-process resume (resumeProcessCmd → resumeResultMsg) to subtree
// resume (resumeSubtreeCmd → client.ResumeSubtree). This is the direct fix for
// Decker's "父进程橙色恢复后整树停滞" bug.
//
// RED expression (behavioural, command-identity):
//   - Suspended: current resumeHandler returns resumeProcessCmd, whose deferred
//     message is resumeResultMsg. AC#3 requires resumeSubtreeCmd, whose message
//     must NOT be a resumeResultMsg. Invoking the cmd against a bogus socket and
//     asserting "not resumeResultMsg" fails today → RED, passes once Task 3.2/3.4
//     land.
//   - Dead/Zombie: must KEEP resumeProcessCmd (Epic 42 UUID 续跑) → its message
//     IS resumeResultMsg. Green guard so the fix doesn't bleed into the
//     UUID-resume path.
//   - Running (not paused): "Nothing to resume" + nil cmd (unchanged).
//
// withBogusSocket / pauseTestModel are shared with the AC#2 pause-handler test
// in the same package.
// =============================================================================

func resumeTestModel(state types.ProcessState, isPaused bool) dashboardModel {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault
	m.connected = true
	m.selectedPID = types.PID(200)
	m.selectedUUID = "uuid-44-4-resume"
	m.processes = []vfs.ProcInfo{{
		PID:      types.PID(200),
		UUID:     "uuid-44-4-resume",
		State:    state,
		IsPaused: isPaused,
		Intent:   "resume handler fixture",
	}}
	return m
}

func TestATDD_44_4_030_ResumeHandler_Suspended_ProducesSubtreeCmd(t *testing.T) {
	withBogusSocket(t)
	m := resumeTestModel(types.StateSuspended, false)

	consumed, _, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume on a Suspended selected process")
	}
	if cmd == nil {
		t.Fatalf("resumeHandler(Suspended) should return a non-nil resume cmd")
	}
	msg := cmd()
	if _, isSingleProc := msg.(resumeResultMsg); isSingleProc {
		t.Errorf("resumeHandler(Suspended) returned resumeResultMsg (single-process "+
			"resumeProcessCmd — Decker bug root cause); AC#3 requires a subtree resume "+
			"cmd (client.ResumeSubtree). got %T", msg)
	}
}

func TestATDD_44_4_031_ResumeHandler_Dead_KeepsUUIDResume(t *testing.T) {
	withBogusSocket(t)
	m := resumeTestModel(types.StateDead, false)

	consumed, ctx, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume on a Dead selected process")
	}
	if cmd == nil {
		t.Fatalf("resumeHandler(Dead) should return a non-nil resume cmd (Epic 42 UUID resume)")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Resuming UUID") {
		t.Errorf("resumeHandler(Dead) statusMsg = %q, want it to contain \"Resuming UUID\"", got)
	}
	if _, ok := cmd().(resumeResultMsg); !ok {
		t.Errorf("resumeHandler(Dead) must KEEP single-process resumeProcessCmd "+
			"(resumeResultMsg) for Epic 42 UUID 续跑; subtree resume is for Suspended only")
	}
}

func TestATDD_44_4_032_ResumeHandler_Zombie_KeepsUUIDResume(t *testing.T) {
	withBogusSocket(t)
	m := resumeTestModel(types.StateZombie, false)

	consumed, _, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume on a Zombie selected process")
	}
	if cmd == nil {
		t.Fatalf("resumeHandler(Zombie) should return a non-nil resume cmd")
	}
	if _, ok := cmd().(resumeResultMsg); !ok {
		t.Errorf("resumeHandler(Zombie) must keep single-process resumeProcessCmd (resumeResultMsg)")
	}
}

func TestATDD_44_4_033_ResumeHandler_RunningUnpaused_NothingToResume(t *testing.T) {
	m := resumeTestModel(types.StateRunning, false)

	consumed, ctx, cmd := resumeHandler(tea.KeyPressMsg{}, ui.KeyContext(m))
	if !consumed {
		t.Fatalf("resumeHandler should consume on a Running selected process")
	}
	if cmd != nil {
		t.Errorf("resumeHandler(Running, not paused) should return nil cmd; got non-nil")
	}
	if got := ctx.(dashboardModel).statusMsg; !strings.Contains(got, "Nothing to resume") {
		t.Errorf("resumeHandler(Running) statusMsg = %q, want it to contain \"Nothing to resume\"", got)
	}
}
