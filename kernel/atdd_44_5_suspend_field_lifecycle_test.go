package kernel

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.5 — Field lifecycle invariants around pause / fail-exit.
//
// This file covers AC2 (suspendOneForSubtree must not pre-fill
// ResumedFromStep) and AC3 (finishProcess must clear SuspendReason on failed
// exit). Each test exercises a kernel-internal seam directly, using the
// makeRunningProc44_1 fixture from Story 44.1's helpers — no IPC server, no
// LLM mock, no reasonStep goroutine. The simpler the fixture, the sharper the
// RED signal.
//
// Why kernel-level and not e2e: AC2 and AC3 are field-state invariants on
// fundamental kernel primitives. The e2e path (atdd_44_5_suspend_during_llm_
// write_test.go) covers them implicitly through observable behaviour, but a
// dedicated unit test pins the specification to a single line of production
// code so a regression-causing edit produces a precise failure message.
// =============================================================================

// TestATDD_44_5_020_SuspendOneForSubtree_DoesNotPreFillResumedFromStep
//
// AC2 boundary case: when LastCompletedStep == 0 the existing pre-fill code
// block at subtree.go:249-255 already short-circuits via `if last > 0`, so
// this test is naturally green even before Story 44.5 is applied. It exists
// as a green-guard: when Story 44.5 deletes the pre-fill block entirely
// (Task 5.1), this case must keep returning ResumedFromStep == 0 — otherwise
// the deletion introduced a regression elsewhere.
func TestATDD_44_5_020_SuspendOneForSubtree_DoesNotPreFillResumedFromStep(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "atdd 44.5 — fresh process pause")

	if err := k.suspendOneForSubtree(proc, "user_paused"); err != nil {
		t.Fatalf("suspendOneForSubtree: %v", err)
	}

	if proc.ResumedFromStep != 0 {
		t.Errorf("after pause on fresh process: proc.ResumedFromStep = %d, want 0 "+
			"(LastCompletedStep was 0; pause must not invent a resume cursor)",
			proc.ResumedFromStep)
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("proc.State = %s, want Suspended", got)
	}
}

// TestATDD_44_5_021_SuspendOneForSubtree_DoesNotTouchResumedFromStepEvenWithCompletedSteps
//
// AC2 main RED. With LastCompletedStep > 0 the existing subtree.go:249-255
// block triggers and pre-fills proc.ResumedFromStep = LastCompletedStep + 1.
// Story 44.5 Task 5.1 deletes that block — afterwards the pre-fill must not
// happen on the pause leg. ResumedFromStep stays at its allocation default
// (0) and resumeOneForSubtree's existing LastCompletedStep+1 fallback at
// subtree.go:343-348 takes over on the real resume.
//
// RED-phase failure (pre-fix): proc.ResumedFromStep == 6 after suspend.
func TestATDD_44_5_021_SuspendOneForSubtree_DoesNotTouchResumedFromStepEvenWithCompletedSteps(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "atdd 44.5 — pause after 5 completed steps")

	// Pre-set the "we've reasoned 5 steps already" state that the EchoMatrix
	// repro hit. The atomic.Int64 storage mirrors how reasonStep updates it.
	proc.LastCompletedStep.Store(5)

	if err := k.suspendOneForSubtree(proc, "user_paused"); err != nil {
		t.Fatalf("suspendOneForSubtree: %v", err)
	}

	if proc.ResumedFromStep != 0 {
		t.Errorf("after pause with LastCompletedStep=5: proc.ResumedFromStep = %d, want 0 "+
			"(Story 44.5 AC2 — pause must not pre-fill the resume cursor; "+
			"resume.go computes it on the resume leg via the existing "+
			"subtree.go:343-348 fallback)",
			proc.ResumedFromStep)
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("proc.State = %s, want Suspended", got)
	}
}

// TestATDD_44_5_030_FinishProcessFailed_ClearsSuspendReason
//
// AC3 main RED. When a failure path (exit.Code != 0) wins the race against a
// concurrent suspend that has already written SuspendReason = "user_paused",
// finishProcess must clear the stale SuspendReason so the persisted ProcInfo
// does not show the contradiction (state=dead, suspend_reason=user_paused,
// exit_reason="llm write failed") seen in the EchoMatrix repro on PID 2/4/6.
//
// The test simulates the race by manually calling SetSuspendReason before
// finishProcess — this is exactly the field state suspendProcess:20 leaves
// behind when its own state transition fails after reasonStep has already
// killed the process via attemptFallback.
//
// RED-phase failure (pre-fix): finishProcess does not touch SuspendReason on
// any path, so the assertion below sees "user_paused" instead of "".
func TestATDD_44_5_030_FinishProcessFailed_ClearsSuspendReason(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "atdd 44.5 — finishProcess(failed) clears suspend reason")

	// Mirror the race: suspendProcess:20 has already stamped SuspendReason
	// before suspendProcess's own Suspend() transition is doomed to fail
	// (because reasonStep raced it to Dead).
	proc.SetSuspendReason("user_paused")

	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm write failed"})

	if got := proc.GetSuspendReason(); got != "" {
		t.Errorf("after finishProcess(failed): proc.SuspendReason = %q, want %q "+
			"(Story 44.5 AC3 — failed exit must clear stale SuspendReason "+
			"so persisted ProcInfo does not contradict itself)",
			got, "")
	}
}

// TestATDD_44_5_031_FinishProcessSuccess_DoesNotTouchSuspendReason
//
// AC3 boundary guard. The cleanup must trigger ONLY on failed exits
// (exit.Code != 0); a successful exit must leave SuspendReason untouched.
// The success path is not a known SuspendReason write site today, but this
// test pins the spec so a "fix" that unconditionally clears the field
// surfaces here as a regression.
//
// This case is a green-guard from day one: finishProcess does not touch the
// field today, so SuspendReason stays at the pre-set value regardless of the
// exit code. Story 44.5 AC3 implementation must preserve that on the success
// branch.
func TestATDD_44_5_031_FinishProcessSuccess_DoesNotTouchSuspendReason(t *testing.T) {
	k := newSubtreeKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "atdd 44.5 — finishProcess(success) preserves suspend reason")

	// The "budget_exhausted" reason is intentionally chosen because it has no
	// realistic relationship to a success exit (a normal complete would not
	// race with a budget-exhausted suspend) — the test asserts the cleanup
	// stays scoped to the failed branch, nothing more.
	proc.SetSuspendReason("budget_exhausted")

	k.finishProcess(proc, ExitStatus{Code: 0, Reason: "complete"})

	if got := proc.GetSuspendReason(); got != "budget_exhausted" {
		t.Errorf("after finishProcess(success): proc.SuspendReason = %q, "+
			"want %q (Story 44.5 AC3 — cleanup must be scoped to exit.Code != 0)",
			got, "budget_exhausted")
	}
}
