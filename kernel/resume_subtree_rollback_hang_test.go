package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// TestResumeSubtree_FDReopenFailure_WaitChildInReason_DoesNotHang locks the fix
// for deferred-work #9 (originally ECH#1 of spec-fix-pause-child-reaped-as-dead):
// when resumeOneForSubtree's LLM-device FD reopen fails, the rollback must finish
// the child to a TERMINAL state (Running→Zombie + terminal Done via finishProcess)
// instead of re-Suspending it via suspendProcess.
//
// Root cause: suspendProcess never writes proc.Done — only reasonStep's exit
// defer → notifySuspendDone does, and no reasonStep goroutine is running during
// the resume rollback (the rollback sits before setupDriverStreamHandler). A
// parent blocked in WaitChildInReason keys off child.GetState(); a re-Suspended
// child yields no terminal Done, so the parent's reasonStep hangs forever. With
// the fix, finishProcess transitions the child to Zombie and writes a terminal
// Done(Code=1), so the parent reaps + returns instead of hanging.
//
// Pre-fix this test hits the 2s WaitChildInReason deadline (hang); post-fix it
// returns Code=1 promptly. Mirrors the ATDD 44.6 fixture (the sibling suite that
// fixed the receive side of this same dual-load Done channel).
func TestResumeSubtree_FDReopenFailure_WaitChildInReason_DoesNotHang(t *testing.T) {
	k := newSubtreeKernel(t)

	parent := makeRunningProc44_1(t, k, 0, "parent")
	child := makeRunningProc44_1(t, k, parent.PID, "child")
	// Give the child a PrimaryDevice the test kernel's VFS has NOT registered, so
	// resumeOneForSubtree's openLLMDeviceForResume fails and drives the rollback
	// branch under test (an empty PrimaryDevice would take the script-runner
	// early-return path instead).
	child.PrimaryDevice = "/dev/llm/nonexistent"

	resultCh := runWaitChildInReason(k, parent, child.PID)

	// Suspend the child subtree and feed the mid-state suspend Done exactly as
	// production notifySuspendDone would. The parent's WaitChildInReason consumes
	// it and continues (does not return) — mirror of ATDD 44.6 030.
	if _, err := k.SuspendSubtree(child.PID); err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	simulateNotifySuspendDone(t, child)
	time.Sleep(50 * time.Millisecond) // let the parent consume the mid-state Done + continue

	// ResumeSubtree → resumeOneForSubtree → openLLMDeviceForResume fails →
	// rollback. resumeSubtreeLocked swallows the per-node error into `skipped`
	// (no top-level error), but the rollback's finishProcess has already written
	// the terminal Done that unblocks the parent.
	_, skipped, err := k.ResumeSubtree(child.PID)
	if err != nil {
		t.Fatalf("ResumeSubtree top-level error: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (child FD reopen failed in rollback)", skipped)
	}

	// Core regression assertion: WaitChildInReason must return promptly with the
	// terminal Code=1, NOT hang. Pre-fix (suspendProcess rollback) this select
	// hits the 2s deadline because no terminal Done is ever written.
	select {
	case got := <-resultCh:
		if got.cancelled {
			t.Errorf("cancelled = true, want false (terminal exit from rollback)")
		}
		if got.exit.Code != 1 {
			t.Errorf("exit.Code = %d, want 1 (resume_failed terminal Done)", got.exit.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitChildInReason hung after FD-reopen-failure rollback — deferred-work #9 regression (rollback must write a terminal Done, not re-Suspend)")
	}

	// The parent reaped the child → Dead. Honours the Resume philosophy: data is
	// preserved on disk so the user can still `rnix resume <uuid>` it.
	assertProcState44_1(t, child, types.StateDead, "child after rollback finish + parent reap")
}
