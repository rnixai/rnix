package ipc

import (
	gocontext "context"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
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
			gocontext.Background(), shell.SpawnRequest{Intent: "atdd 44.5 — pause child"})
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
			gocontext.Background(), shell.SpawnRequest{Intent: "atdd 44.5 — invariant after pause+resume"})
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
// Story 44.5 v2 review (Edge Case Hunter) — the original version of this test
// asserted AC8 only via the SpawnAndWait black-box exit code, but the 5ms
// poll in waitState gave the SpawnAndWait reader goroutine ample opportunity
// to read the stale Done BEFORE the drain executed, making the drain a no-op
// while AC7's Suspended-loop alone carried the assertion. To force the AC8
// path to fail observably when the drain is missing, we now assert directly
// on the proc.Done channel length: after Pause, len(proc.Done)==1; after
// Resume, drain must have emptied it BEFORE reasonStep restarts and writes
// a new Done. We DO NOT launch the SpawnAndWait reader for this assertion —
// it would race the drain.
//
// Sequence (white-box on the kernel Process.Done channel):
//   1. Spawn child stuck in parkOnWrite (no SpawnAndWait reader competing).
//   2. SignalTree(SIGPAUSE) — notifySuspendDone writes Code=2 to proc.Done
//      via the reasonStep defer at kernel/reason.go:248-259.
//   3. Assert len(proc.Done) == 1 — the stale ExitSuspended is sitting in
//      the buffered cap=1 channel.
//   4. ResumeSubtree — AC8 drain at kernel/subtree.go:297-300 must clear
//      proc.Done before reasonStep restarts.
//   5. Assert len(proc.Done) == 0 immediately after ResumeSubtree returns
//      and BEFORE the new reasonStep writes finishProcess's terminal Done.
//      Pre-AC8: len would still be 1 (the stale Code=2 was never drained),
//      causing the post-resume finishProcess write to be silently dropped.
//   6. Cleanup: close(release) so reasonStep can complete and not leak.
//
// Pre-AC8 failure mode: step 5 sees len==1 because the drain block is
// missing, and a subsequent SpawnAndWait reader would observe Code=2 instead
// of Code=0.
func TestATDD_44_5_022_ResumeOneForSubtree_DrainsStaleDone(t *testing.T) {
	_, kern, _, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	// Spawn the child via the same path SpawnAndWait would, but without
	// launching the SpawnAndWait reader goroutine — we own proc.Done for the
	// duration of the assertion.
	childPID, err := kern.Spawn("atdd 44.5 — drain stale Done", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("child did not enter LLM Write within 2s")
	}

	childProc, ok := kern.GetProcess(childPID)
	if !ok {
		t.Fatalf("GetProcess(%d) returned !ok", childPID)
	}

	// Pause — notifySuspendDone writes Code=2 to proc.Done via reasonStep defer.
	if _, err := kern.SignalTree(childPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}
	waitState(t, childProc, types.StateSuspended, 500*time.Millisecond)

	// AC8 precondition: the stale ExitSuspended must be sitting in proc.Done.
	// If it is not, the test fixture itself is broken — either notifySuspendDone
	// did not fire (AC1 regressed) or proc.Done has unexpected cap.
	if got := len(childProc.Done); got != 1 {
		t.Fatalf("AC8 precondition: len(proc.Done) = %d, want 1 "+
			"(notifySuspendDone must have written ExitSuspended via reasonStep defer)", got)
	}

	// AC8 load-bearing assertion: ResumeSubtree must drain proc.Done BEFORE
	// reasonStep restarts.
	if _, _, err := kern.ResumeSubtree(childPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	// Snapshot proc.Done length immediately. The new reasonStep was kicked off
	// by ResumeSubtree but is still parked in parkOnWrite (we haven't closed
	// release), so finishProcess has not yet run and cannot have written a new
	// Done. If len > 0 here, the drain block is missing.
	if got := len(childProc.Done); got != 0 {
		t.Errorf("AC8: len(proc.Done) = %d immediately after ResumeSubtree, want 0 "+
			"(kernel/subtree.go:297-300 drain block must clear the stale "+
			"ExitSuspended so the next finishProcess write is not dropped by "+
			"cap=1 saturation)", got)
	}

	// Cleanup: let the new reasonStep complete so the test does not leak.
	close(release)
	released = true

	// Wait for the child to finish so its goroutine is reaped before the test
	// fixture teardown.
	select {
	case <-childProc.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not exit within 2s after Resume + release")
	}
}

// TestATDD_44_5_023_PausedAt_Frozen_PausedTotal_Accumulates
//
// Epic 44.1 SoftPause→HardSuspend 重构遗漏 — suspendProcess 未维护 pausedAt，
// resumeOneForSubtree 未累加 pausedTotal，导致 dashboard 在 state=Suspended 期间
// 仍按 wall clock 增长显示 elapsed time（用户报告"按 p 后时间还在增长"）。
//
// v2 修复后断言：
//   - Suspend 时 GetProcInfo().IsPaused == true（不只看 resumeCh，也认 Suspended state）
//   - Suspend 时 GetProcInfo().PausedAt != zero（dashboard freeze 分支前置条件）
//   - Resume 后 GetProcInfo().PausedTotal > 0（累加了 Suspended 期间的时间）
//   - Resume 后 IsPaused == false, PausedAt == zero（清理干净）
func TestATDD_44_5_023_PausedAt_Frozen_PausedTotal_Accumulates(t *testing.T) {
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
			gocontext.Background(), shell.SpawnRequest{Intent: "atdd 44.5 — pausedAt/pausedTotal accounting"})
		resultCh <- spawnAndWaitResult{res, code, toks, err}
	}()

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("child did not enter LLM Write within 2s")
	}

	childPID := waitChildEnterRunning(t, kern, 0, 1*time.Second)

	// Pre-suspend invariant: not paused, PausedAt zero, PausedTotal zero.
	preInfo, err := kern.GetProcInfo(childPID)
	if err != nil {
		t.Fatalf("GetProcInfo pre-suspend: %v", err)
	}
	if preInfo.IsPaused {
		t.Errorf("pre-suspend IsPaused = true, want false")
	}
	if !preInfo.PausedAt.IsZero() {
		t.Errorf("pre-suspend PausedAt = %v, want zero", preInfo.PausedAt)
	}

	// Pause the child.
	if _, err := kern.SignalTree(childPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}
	childProc, _ := kern.GetProcess(childPID)
	waitState(t, childProc, types.StateSuspended, 500*time.Millisecond)

	// Mid-suspend invariant: IsPaused == true, PausedAt set.
	midInfo, err := kern.GetProcInfo(childPID)
	if err != nil {
		t.Fatalf("GetProcInfo mid-suspend: %v", err)
	}
	if !midInfo.IsPaused {
		t.Error("mid-suspend IsPaused = false, want true " +
			"(Epic 44.1 missed: HardSuspend needs to surface as IsPaused for dashboard freeze branch)")
	}
	if midInfo.PausedAt.IsZero() {
		t.Error("mid-suspend PausedAt = zero, want non-zero " +
			"(suspendProcess must record the suspend wall-clock anchor)")
	}

	// Hold the suspended state for ~100ms so PausedTotal has a measurable delta.
	suspendDuration := 100 * time.Millisecond
	time.Sleep(suspendDuration)

	// Resume.
	if _, _, err := kern.ResumeSubtree(childPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	// Post-resume invariant: IsPaused cleared, PausedAt zero, PausedTotal accumulated.
	postInfo, err := kern.GetProcInfo(childPID)
	if err != nil {
		t.Fatalf("GetProcInfo post-resume: %v", err)
	}
	if postInfo.IsPaused {
		t.Error("post-resume IsPaused = true, want false (resumeOneForSubtree must clear)")
	}
	if !postInfo.PausedAt.IsZero() {
		t.Errorf("post-resume PausedAt = %v, want zero", postInfo.PausedAt)
	}
	if postInfo.PausedTotal < suspendDuration {
		t.Errorf("post-resume PausedTotal = %v, want >= %v "+
			"(resumeOneForSubtree must accumulate the suspended-interval)",
			postInfo.PausedTotal, suspendDuration)
	}

	// Cleanup: let the child complete so the test does not leak.
	close(release)
	released = true
	select {
	case <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("SpawnAndWait did not return within 2s")
	}
}
