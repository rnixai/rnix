package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.1 — Signal-dispatch tests for the unified pause/resume statemachine.
//
// Covers:
//   - AC#1: SIGPAUSE on a process propagates Suspended to its entire subtree,
//           leaves siblings and ancestors untouched, and is idempotent on
//           already-suspended targets (event name `noop_already_suspended`).
//   - AC#2: SIGRESUME on a Suspended process restores the subtree to Running,
//           restarts reasonStep for non-script processes, skips Dead/Failed.
//   - AC#5: Suspended-state signal branch no longer silent-noops on SIGRESUME;
//           SIGPAUSE on Suspended emits the renamed event.
//   - AC#6: defaultSignalAction routes SIGPAUSE/SIGRESUME through the state
//           machine (k.SuspendSubtree / k.ResumeSubtree) rather than the
//           SoftPause proc.Pause()/proc.Resume() path.
//
// These tests intentionally bypass the public Signal API in some places to
// exercise the internal contracts (event payloads, dispatch routing). The
// observable end-to-end behaviour is asserted through k.Signal.
// =============================================================================

// --- AC#1: SIGPAUSE on P2 puts P2 and its descendants Suspended ---

func TestATDD_44_1_001_SignalSIGPAUSE_SubtreePropagates(t *testing.T) {
	k := newSubtreeKernel(t)

	p1 := makeRunningProc44_1(t, k, 0, "P1 root")
	p2 := makeRunningProc44_1(t, k, p1.PID, "P2 child")
	p3 := makeRunningProc44_1(t, k, p2.PID, "P3 grandchild")

	if err := k.Signal(p2.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE on P2: %v", err)
	}

	assertProcState44_1(t, p2, types.StateSuspended, "P2 must enter Suspended")
	assertProcState44_1(t, p3, types.StateSuspended, "P3 (descendant) must propagate to Suspended")
}

// --- AC#1: SIGPAUSE on P2 must not affect ancestor P1 ---

func TestATDD_44_1_002_SignalSIGPAUSE_DoesNotAffectAncestor(t *testing.T) {
	k := newSubtreeKernel(t)

	p1 := makeRunningProc44_1(t, k, 0, "P1 root")
	p2 := makeRunningProc44_1(t, k, p1.PID, "P2 child")

	if err := k.Signal(p2.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE on P2: %v", err)
	}

	assertProcState44_1(t, p2, types.StateSuspended, "P2")
	if got := p1.GetState(); got != types.StateRunning {
		t.Errorf("P1 (ancestor) state = %s, want Running (no upward propagation)", got)
	}
}

// --- AC#1: SIGPAUSE on P2 must not affect a sibling P0 ---
//
// AC1 wording: "P0 = P1 的兄弟" — P0 and P1 are top-level siblings (both have
// PPID=0). SIGPAUSE on P2 (child of P1) must not propagate to P0's branch
// (Story 44.1 code review F29).

func TestATDD_44_1_003_SignalSIGPAUSE_DoesNotAffectSibling(t *testing.T) {
	k := newSubtreeKernel(t)

	p0 := makeRunningProc44_1(t, k, 0, "P0 sibling of P1")
	p1 := makeRunningProc44_1(t, k, 0, "P1 root")
	p2 := makeRunningProc44_1(t, k, p1.PID, "P2 child of P1")

	if err := k.Signal(p2.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE on P2: %v", err)
	}

	assertProcState44_1(t, p2, types.StateSuspended, "P2 target")
	if got := p0.GetState(); got != types.StateRunning {
		t.Errorf("P0 (P1's sibling) state = %s, want Running (no sibling propagation)", got)
	}
	if got := p1.GetState(); got != types.StateRunning {
		t.Errorf("P1 (ancestor) state = %s, want Running (no upward propagation)", got)
	}
}

// --- AC#1 / AC#5: SIGPAUSE on already-Suspended process is no-op ---

func TestATDD_44_1_004_SignalSIGPAUSE_OnSuspended_IsNoOp(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeSuspendedProc44_1(t, k, 0, "already suspended", "user_paused")

	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE on suspended: %v", err)
	}

	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("state = %s, want Suspended (no double-suspend)", got)
	}
}

// --- AC#5: SIGPAUSE on Suspended emits the renamed event `noop_already_suspended` ---

func TestATDD_44_1_005_SignalSIGPAUSE_NoopEventNameIsAlreadySuspended(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeSuspendedProc44_1(t, k, 0, "already suspended", "user_paused")

	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	events := drainDebugChan44_1(t, proc, 4, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Signal", map[string]any{"action": "noop_already_suspended"}); ev == nil {
		t.Errorf("expected Signal event action=noop_already_suspended; got events=%+v", events)
	}
}

// --- AC#1: Suspend event reason is the canonical `user_paused` ---

func TestATDD_44_1_006_SuspendEvent_ReasonIsUserPaused(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "target")

	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE: %v", err)
	}

	events := drainDebugChan44_1(t, proc, 8, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Suspend", map[string]any{"reason": "user_paused"}); ev == nil {
		t.Errorf("expected Suspend event reason=user_paused; got events=%+v", events)
	}
}

// --- AC#2: SIGRESUME on P2 restores P2 and P3 to Running ---

func TestATDD_44_1_010_SignalSIGRESUME_SubtreeRestoresRunning(t *testing.T) {
	k := newSubtreeKernel(t)

	p1 := makeRunningProc44_1(t, k, 0, "P1 untouched root")
	p2 := makeSuspendedProc44_1(t, k, p1.PID, "P2 child", "user_paused")
	p3 := makeSuspendedProc44_1(t, k, p2.PID, "P3 grandchild", "user_paused")

	if err := k.Signal(p2.PID, types.SIGRESUME); err != nil {
		t.Fatalf("Signal SIGRESUME on P2: %v", err)
	}

	assertProcState44_1(t, p2, types.StateRunning, "P2")
	assertProcState44_1(t, p3, types.StateRunning, "P3")
	if got := p1.GetState(); got != types.StateRunning {
		t.Errorf("P1 was Running and must stay Running; got %s", got)
	}
}

// --- AC#2: SIGRESUME on Suspended target restores it to Running ---
//
// Renamed from the misleading "RestartsReasonStep" — the fixture has no
// PrimaryDevice and therefore exercises the script-runner branch of
// resumeOneForSubtree (state transition only, no goroutine launch). The
// reasonStep-restart code path is exercised end-to-end by the project's
// integration smoke (see Story 44.1 Tasks 7.3 dev-notes) and by the broader
// resume tests in resume_parent_linkage_test.go; covering it via unit test
// would require standing up a fake LLM device, which the rest of this
// package's tests intentionally avoid (Story 44.1 code review F19).

func TestATDD_44_1_011_SignalSIGRESUME_RestoresRunningState(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeSuspendedProc44_1(t, k, 0, "needs restart", "user_paused")

	if err := k.Signal(proc.PID, types.SIGRESUME); err != nil {
		t.Fatalf("Signal SIGRESUME: %v", err)
	}

	assertProcState44_1(t, proc, types.StateRunning, "post-resume")
	if proc.IsSuspendRequested() {
		t.Error("IsSuspendRequested must be false after SIGRESUME")
	}
}

// --- AC#5 / AC#2: SIGRESUME on Suspended triggers ResumeSubtree ---

func TestATDD_44_1_040_SignalSIGRESUME_OnSuspended_TriggersResumeSubtree(t *testing.T) {
	k := newSubtreeKernel(t)

	root := makeSuspendedProc44_1(t, k, 0, "subtree root", "user_paused")
	child := makeSuspendedProc44_1(t, k, root.PID, "subtree child", "user_paused")

	if err := k.Signal(root.PID, types.SIGRESUME); err != nil {
		t.Fatalf("Signal SIGRESUME on Suspended: %v", err)
	}

	assertProcState44_1(t, root, types.StateRunning, "root after SIGRESUME")
	assertProcState44_1(t, child, types.StateRunning, "child must also resume (subtree semantics)")

	events := drainDebugChan44_1(t, root, 8, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Signal", map[string]any{"action": "resumed_subtree"}); ev == nil {
		t.Errorf("expected Signal event action=resumed_subtree on suspended target; got events=%+v", events)
	}
}

// --- AC#5: SIGPAUSE on Suspended emits noop_already_suspended (also exercised
// in TestATDD_44_1_005); included here so AC#5 has a dedicated case independent
// of AC#1 wording. ---

func TestATDD_44_1_041_SignalSIGPAUSE_OnSuspended_EmitsNoopAlreadySuspended(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeSuspendedProc44_1(t, k, 0, "already paused", "user_paused")

	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE: %v", err)
	}

	events := drainDebugChan44_1(t, proc, 4, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Signal", map[string]any{"action": "noop_already_suspended"}); ev == nil {
		t.Errorf("expected Signal action=noop_already_suspended; got events=%+v", events)
	}
}

// --- AC#5: SIGRESUME on Dead returns ErrNotFound ---

func TestATDD_44_1_042_SignalSIGRESUME_OnDead_ReturnsErrNotFound(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeDeadProc44_1(t, k, 0, "dead")

	err := k.Signal(proc.PID, types.SIGRESUME)
	if err == nil {
		t.Fatal("expected error for SIGRESUME on Dead process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T (%v)", err, err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("err.Code = %v, want ErrNotFound", se.Code)
	}
}

// --- AC#5: SIGRESUME on Zombie returns ErrNotFound ---

func TestATDD_44_1_043_SignalSIGRESUME_OnZombie_ReturnsErrNotFound(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "soon-to-be-zombie")
	if err := proc.Terminate(ExitStatus{Code: 0, Reason: "done"}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	err := k.Signal(proc.PID, types.SIGRESUME)
	if err == nil {
		t.Fatal("expected error for SIGRESUME on Zombie process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T (%v)", err, err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("err.Code = %v, want ErrNotFound", se.Code)
	}
}

// --- AC#6: defaultSignalAction SIGPAUSE routes through SuspendSubtree ---
//
// We assert routing by observing that a SIGPAUSE on a process whose subtree
// contains living descendants suspends the descendants too. The legacy
// SoftPause path (proc.Pause()) is purely local — it cannot put a descendant
// into Suspended. Therefore, "descendant became Suspended" proves routing
// reached the state-machine path.

func TestATDD_44_1_050_DefaultSignalAction_SIGPAUSE_CallsSuspendSubtree(t *testing.T) {
	k := newSubtreeKernel(t)
	parent := makeRunningProc44_1(t, k, 0, "parent")
	child := makeRunningProc44_1(t, k, parent.PID, "child")

	if err := k.Signal(parent.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE: %v", err)
	}

	if got := child.GetState(); got != types.StateSuspended {
		t.Errorf("child state = %s, want Suspended — proves default action delegated to SuspendSubtree", got)
	}
}

// --- AC#6: defaultSignalAction SIGRESUME routes through ResumeSubtree ---
//
// Symmetric to TestATDD_44_1_050: the legacy proc.Resume() is local; observing
// a descendant resumed by signalling the parent proves the dispatcher walks
// the subtree.

func TestATDD_44_1_051_DefaultSignalAction_SIGRESUME_CallsResumeSubtree(t *testing.T) {
	k := newSubtreeKernel(t)
	parent := makeRunningProc44_1(t, k, 0, "parent then suspend")
	child := makeRunningProc44_1(t, k, parent.PID, "child then suspend")

	// Use the new path to put both into Suspended (we are not testing
	// SoftPause/HardSuspend here — that's covered elsewhere). Suspend each
	// directly via the proven k.Suspend syscall.
	if err := k.Suspend(parent.PID); err != nil {
		t.Fatalf("Suspend parent: %v", err)
	}
	if err := k.Suspend(child.PID); err != nil {
		t.Fatalf("Suspend child: %v", err)
	}

	if err := k.Signal(parent.PID, types.SIGRESUME); err != nil {
		t.Fatalf("Signal SIGRESUME: %v", err)
	}

	assertProcState44_1(t, parent, types.StateRunning, "parent")
	if got := child.GetState(); got != types.StateRunning {
		t.Errorf("child state = %s, want Running — proves SIGRESUME delegated to ResumeSubtree", got)
	}
}
