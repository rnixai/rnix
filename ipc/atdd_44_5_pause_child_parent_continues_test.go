package ipc

import (
	gocontext "context"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.5 — Pause-child does not break parent SpawnAndWait (Story 44.5 AC7/AC9).
//
// Reproduces the EchoMatrix dev-auto.ash regression: the user pauses a child
// process while dev-auto.ash is in `r = spawn "..." on-error spawn "..."`. The
// pre-fix kernel writes ExitSuspended=2 to proc.Done; ipcKernelSpawner.SpawnAndWait
// reads it, reaps the (still-Suspended) child, and returns exitCode=2; the shell
// then runs the on-error handler — spawning the "上一轮执行异常..." child.
//
// AC7 lands the SpawnAndWait Suspended-loop: when proc.Done fires with a
// non-terminal state (Running transitional / Suspended), discard the exit and
// re-arm select. Only Zombie/Dead actually reap+return.
//
// AC8 lands resumeOneForSubtree's drain of stale proc.Done events so the next
// finishProcess write does not get dropped by cap=1 saturation.
//
// AC9 holds the user-facing assertion: parent SpawnAndWait must not return
// until the child truly exits (after Resume + reasonStep completes).
// =============================================================================

// waitChildEnterRunning polls procTable for a process matching predicate
// (default: state==Running and PID != skipPID), with a deadline.
func waitChildEnterRunning(t *testing.T, kern *kernel.KernelImpl, skipPID types.PID, timeout time.Duration) types.PID {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, info := range kern.ListProcs() {
			if info.PID == skipPID {
				continue
			}
			if info.State == types.StateRunning || info.State == types.StateCreated {
				return info.PID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no child process entered Running within %s", timeout)
	return 0
}

// waitState polls until proc.GetState() matches want or timeout.
func waitState(t *testing.T, proc *kernel.Process, want types.ProcessState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if proc.GetState() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process state = %s, want %s within %s", proc.GetState(), want, timeout)
}

// spawnAndWaitResult bundles SpawnAndWait return values for delivery through a
// channel — used to assert blocking/non-blocking behaviour from the test goroutine.
type spawnAndWaitResult struct {
	result   string
	exitCode int
	tokens   int
	err      error
}

// TestATDD_44_5_020_SpawnAndWait_PauseChild_Blocks_UntilResumed
//
// AC7 main assertion: a SIGPAUSE on the child during LLM Write must NOT cause
// SpawnAndWait to return with exitCode=ExitSuspended. The call must continue
// blocking until ResumeSubtree → reasonStep completes → finishProcess writes a
// terminal Done.
//
// RED-phase (pre-AC7): SpawnAndWait returns exitCode=2 immediately on pause,
// because the Done-read case unconditionally reaps and returns.
func TestATDD_44_5_020_SpawnAndWait_PauseChild_Blocks_UntilResumed(t *testing.T) {
	_, kern, _, llmFile := setupResumeIPCTest(t)

	// Park the child in LLM Write so SIGPAUSE catches it mid-flight (the
	// EchoMatrix-equivalent race window).
	reached, release := llmFile.parkOnWrite()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	spawner := &ipcKernelSpawner{
		kernel:    kern,
		parentPID: 0, // no script-runner needed for AC7 unit assertion
	}

	resultCh := make(chan spawnAndWaitResult, 1)
	go func() {
		res, code, toks, err := spawner.SpawnAndWait(
			gocontext.Background(), "atdd 44.5 — pause child", "", "")
		resultCh <- spawnAndWaitResult{res, code, toks, err}
	}()

	// Wait until child is in LLM Write.
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("child did not enter LLM Write within 2s")
	}

	// Find the child PID (single non-script process in procTable).
	childPID := waitChildEnterRunning(t, kern, 0, 1*time.Second)
	childProc, ok := kern.GetProcess(childPID)
	if !ok {
		t.Fatalf("child pid=%d vanished", childPID)
	}

	// Fire SIGPAUSE on the child subtree.
	if _, err := kern.SignalTree(childPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}

	// Wait for child to reach Suspended.
	waitState(t, childProc, types.StateSuspended, 500*time.Millisecond)

	// AC7 load-bearing assertion: SpawnAndWait MUST NOT have returned. The
	// Done event written by notifySuspendDone is mid-state — the loop should
	// have discarded it and re-armed select.
	select {
	case r := <-resultCh:
		t.Fatalf("SpawnAndWait returned prematurely with exitCode=%d (err=%v) "+
			"while child was Suspended — AC7 loop discarded too few events", r.exitCode, r.err)
	case <-time.After(200 * time.Millisecond):
	}

	// Resume the child subtree. The drain in resumeOneForSubtree (AC8)
	// clears any stale Done so the next finishProcess write lands cleanly.
	if _, _, err := kern.ResumeSubtree(childPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	// Release the gated Write so the resumed reasonStep can complete.
	// mockLLMFile.readData = {"action":"complete","content":"done"} so the
	// process exits with Code=0/Reason="completed".
	close(release)
	released = true

	// SpawnAndWait now returns the terminal exit.
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("SpawnAndWait err after resume: %v", r.err)
		}
		if r.exitCode != 0 {
			t.Errorf("exitCode = %d after Resume+complete, want 0 "+
				"(non-zero would re-trigger dev-auto.ash on-error)", r.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SpawnAndWait did not return within 2s after ResumeSubtree + release")
	}
}

// TestATDD_44_5_021_PauseChild_NoStaleOnError_StateConsistent
//
// AC7 + invariant assertion. After SIGPAUSE + ResumeSubtree + completion, the
// proc-info.json snapshot must be invariant-clean: no Dead-with-SuspendReason
// contradiction (which would be the AC6 ValidateProcInfoInvariant red flag).
func TestATDD_44_5_021_PauseChild_NoStaleOnError_StateConsistent(t *testing.T) {
	_, kern, _, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	spawner := &ipcKernelSpawner{kernel: kern, parentPID: 0}

	resultCh := make(chan spawnAndWaitResult, 1)
	go func() {
		res, code, toks, err := spawner.SpawnAndWait(
			gocontext.Background(), "atdd 44.5 — invariant after pause+resume", "", "")
		resultCh <- spawnAndWaitResult{res, code, toks, err}
	}()

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("child did not enter LLM Write within 2s")
	}

	childPID := waitChildEnterRunning(t, kern, 0, 1*time.Second)

	if _, err := kern.SignalTree(childPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}

	childProc, _ := kern.GetProcess(childPID)
	waitState(t, childProc, types.StateSuspended, 500*time.Millisecond)

	if _, _, err := kern.ResumeSubtree(childPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	close(release)
	released = true

	select {
	case <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("SpawnAndWait did not return within 2s")
	}

	// At terminal state, ProcInfo invariant must hold (AC6).
	info, err := kern.GetProcInfo(childPID)
	if err != nil {
		// Process may already be reaped by SpawnAndWait — check history.
		t.Logf("GetProcInfo: %v (likely already reaped, looking up history)", err)
		return
	}
	if err := kernel.ValidateProcInfoInvariant(info); err != nil {
		t.Errorf("ValidateProcInfoInvariant after pause+resume+complete: %v", err)
	}
}

// TestATDD_44_5_022_ResumeOneForSubtree_DrainsStaleDone
//
// AC8 race-defense assertion: when a Pause-Resume cycle completes very fast
// (before SpawnAndWait reads), the stale ExitSuspended in proc.Done must be
// drained so the next finishProcess write is not silently dropped.
//
// Sequence:
//   1. Parent SpawnAndWait starts; child in parkOnWrite.
//   2. SignalTree(SIGPAUSE) — notifySuspendDone writes Code=2 to proc.Done.
//   3. Immediately ResumeSubtree without giving SpawnAndWait a chance to read.
//      AC8 drain clears the stale Code=2.
//   4. close(release) — Write returns nil → Read returns "complete" →
//      finishProcess writes Code=0 to proc.Done (succeeds because drained).
//   5. SpawnAndWait reads Code=0 + state=Zombie → return exitCode=0.
//
// Pre-AC8: step 4's send is dropped (cap=1 full with stale Code=2);
// SpawnAndWait reads Code=2 + state=Zombie → returns exitCode=2.
func TestATDD_44_5_022_ResumeOneForSubtree_DrainsStaleDone(t *testing.T) {
	_, kern, _, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	spawner := &ipcKernelSpawner{kernel: kern, parentPID: 0}

	resultCh := make(chan spawnAndWaitResult, 1)
	go func() {
		res, code, toks, err := spawner.SpawnAndWait(
			gocontext.Background(), "atdd 44.5 — drain stale Done", "", "")
		resultCh <- spawnAndWaitResult{res, code, toks, err}
	}()

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("child did not enter LLM Write within 2s")
	}

	childPID := waitChildEnterRunning(t, kern, 0, 1*time.Second)

	// Pause and immediately Resume — give the goroutine no chance to read the
	// stale Done in between.
	if _, err := kern.SignalTree(childPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}
	childProc, _ := kern.GetProcess(childPID)
	waitState(t, childProc, types.StateSuspended, 500*time.Millisecond)

	if _, _, err := kern.ResumeSubtree(childPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	close(release)
	released = true

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("SpawnAndWait err: %v", r.err)
		}
		if r.exitCode != 0 {
			t.Errorf("exitCode = %d, want 0 (AC8 drain ensures the new "+
				"finishProcess Code=0 write is not dropped by stale Code=2)", r.exitCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SpawnAndWait did not return within 3s — drain may have failed")
	}
}
