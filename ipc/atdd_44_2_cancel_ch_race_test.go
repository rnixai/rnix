package ipc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.2 — AC#1.b: CancelledCh-watcher race-window defence.
//
// Story 44.2 §AC1.b: in the new design `handleExecScript`'s
// `<-scriptProc.CancelledCh()` goroutine must check `IsSuspendRequested()`
// before calling `scriptCancel`. If suspendRequested is true, the cancel
// originated from `suspendOneForSubtree` (which sets `suspendRequested=
// true` BEFORE `proc.Cancel()` — kernel/subtree.go:238 → :250) — the
// watcher must NOT cancel scriptCtx; otherwise the ScriptExecutor errors
// out instead of waiting at the statement boundary.
//
// The watcher's new body (from §AC1.b):
//
//   go func() {
//       select {
//       case <-s.done:
//           scriptCancel(nil)
//       case <-scriptProc.CancelledCh():
//           if !scriptProc.IsSuspendRequested() {
//               scriptCancel(nil)
//           }
//       case <-scriptCtx.Done():
//       }
//   }()
//
// RED phase: the test references `watchScriptProcCancellation` (or any
// extracted helper that dev-story creates per §AC1.b). If dev-story
// inlines the logic, this test still validates the OBSERVABLE invariant:
// "given a Suspend-driven Cancel, scriptCtx must remain alive". We use a
// minimal façade `runCancelWatcherForTest` that dev-story is expected to
// add (or this test is rewritten to call the inlined version).
// =============================================================================

// runCancelWatcherForTest is the testable extraction of the CancelledCh
// watcher in handleExecScript (44.2 §AC1.b). It must exist on the ipc
// package as a package-level helper so unit tests can drive the race
// window without spinning up a full handleExecScript / conn pair.
//
// Contract (§AC1.b):
//   - Exits when ANY of {s.done, scriptProc.CancelledCh(), scriptCtx.Done()}
//     fires.
//   - Calls scriptCancel(nil) ONLY when s.done fires OR when CancelledCh
//     fires AND IsSuspendRequested() == false.
//   - On CancelledCh + IsSuspendRequested() == true: does NOT cancel.
//
// dev-story Task 4.1 must surface this helper (or the watcher inlined in
// handleExecScript with the same observable behaviour); this test pins
// the contract.
//
// During RED phase this symbol does not exist — compile fails, which is
// the intended signal.

// TestATDD_44_2_040_CancelWatcher_SuspendDrivenCancel_DoesNotCancelCtx
//
// Race window the most carefully described in §AC1.b:
//   suspendOneForSubtree does Store(true) → Cancel() in that order.
//   The watcher runs at some point AFTER both. By the time CancelledCh
//   wakes the watcher, IsSuspendRequested() is true → must NOT cancel
//   scriptCtx.
func TestATDD_44_2_040_CancelWatcher_SuspendDrivenCancel_DoesNotCancelCtx(t *testing.T) {
	proc := newRunningScriptProc(t)

	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	cancelCount := atomic.Int32{}
	wrappedCancel := func(err error) {
		cancelCount.Add(1)
		scriptCancel(err)
	}

	done := make(chan struct{})
	watcherDone := make(chan struct{})

	go func() {
		defer close(watcherDone)
		runCancelWatcherForTest(done, scriptCtx, proc, wrappedCancel)
	}()

	// Emulate suspendOneForSubtree's ordering:
	//   1. set suspendRequested = true
	//   2. cancel proc.ctx
	// (kernel/subtree.go:238 → :250)
	proc.SetSuspendReason("user_paused")
	// We do not have direct access to suspendRequested.Store — but
	// Suspend() emulates the post-state. Use Suspend's flag side-effect
	// via reflection? Simpler: the watcher's contract is observation-
	// based — we test it by transitioning the process into Suspended
	// (which sets the flag through the same Store(true)+Cancel sequence
	// that subtree.go uses) inline:
	if err := simulateSuspendDrivenCancel(proc); err != nil {
		t.Fatalf("simulateSuspendDrivenCancel: %v", err)
	}

	// Give the watcher 100ms to (incorrectly) cancel — it should NOT.
	time.Sleep(100 * time.Millisecond)
	if cancelCount.Load() != 0 {
		t.Errorf("scriptCancel called %d times; want 0 (CancelledCh + IsSuspendRequested must not cancel)", cancelCount.Load())
	}
	if err := scriptCtx.Err(); err != nil {
		t.Errorf("scriptCtx.Err() = %v, want nil (ctx must stay alive across suspend-driven cancel)", err)
	}

	// Cleanup: close done to tear down the watcher.
	close(done)
	select {
	case <-watcherDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watcher did not exit after s.done close")
	}
}

// TestATDD_44_2_041_CancelWatcher_KillDrivenCancel_DoesCancelCtx
//
// Counterpart: a "real" termination path (SIGKILL/SIGTERM) does NOT set
// suspendRequested before cancelling the process ctx — so the watcher
// MUST cancel scriptCtx, returning the executor and letting finalize
// run.
func TestATDD_44_2_041_CancelWatcher_KillDrivenCancel_DoesCancelCtx(t *testing.T) {
	proc := newRunningScriptProc(t)

	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	var (
		mu     sync.Mutex
		called bool
	)
	wrappedCancel := func(err error) {
		mu.Lock()
		called = true
		mu.Unlock()
		scriptCancel(err)
	}

	done := make(chan struct{})
	go runCancelWatcherForTest(done, scriptCtx, proc, wrappedCancel)

	// Plain Cancel() (no suspendRequested.Store(true)) — emulates SIGKILL
	// /SIGTERM dispatch which never touches the suspend flag.
	proc.Cancel()

	// Within 200ms the watcher must have called scriptCancel.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := called
		mu.Unlock()
		if got {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := called
	mu.Unlock()
	if !got {
		t.Error("scriptCancel was NOT called on kill-driven Cancel; want cancellation propagated so executor unwinds")
	}
	if err := scriptCtx.Err(); err == nil {
		t.Error("scriptCtx still alive after kill-driven Cancel; want it cancelled")
	}

	close(done)
}

// TestATDD_44_2_042_CancelWatcher_DaemonShutdownCancelsCtx
//
// `<-s.done` (daemon shutdown) ALWAYS cancels scriptCtx with nil cause.
// AC#5 fourth bullet collapses errDaemonShutdown into a plain nil cause.
func TestATDD_44_2_042_CancelWatcher_DaemonShutdownCancelsCtx(t *testing.T) {
	proc := newRunningScriptProc(t)

	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	gotCancel := make(chan error, 1)
	wrappedCancel := func(err error) {
		select {
		case gotCancel <- err:
		default:
		}
		scriptCancel(err)
	}

	done := make(chan struct{})
	go runCancelWatcherForTest(done, scriptCtx, proc, wrappedCancel)

	close(done)
	select {
	case err := <-gotCancel:
		if err != nil {
			t.Errorf("scriptCancel cause = %v, want nil on daemon shutdown (sentinel deleted)", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("scriptCancel was not called on s.done close")
	}
}

// --- shims used by the tests above ---

// simulateSuspendDrivenCancel mirrors kernel/subtree.go:238-251 exactly:
//
//	proc.suspendRequested.Store(true)   // line 238
//	proc.Cancel()                       // line 250
//
// The Process API does not export suspendRequested directly; the only
// public path that sets it is `Suspend()` via `suspendOneForSubtree`. The
// latter is a kernel internal — for unit-testing the watcher in ipc/ we
// use the cheapest equivalent: `SetSuspendReason("user_paused") +
// Suspend()` sets state to Suspended (which is itself observable via
// IsSuspendRequested() returning... no, actually IsSuspendRequested is
// the atomic flag, not derived from state).
//
// dev-story Task 1.x is expected to keep `IsSuspendRequested` reflecting
// the atomic. If the future implementation requires an explicit flag
// flip outside Suspend(), update this helper accordingly.
//
// Returns an error so the test surfaces failures clearly.
func simulateSuspendDrivenCancel(proc *kernel.Process) error {
	// Emulate suspendOneForSubtree:
	//   1. (would be) proc.suspendRequested.Store(true) — there is no
	//      public setter, but kernel.SuspendSubtree's effect is observable
	//      via Suspend() leaving suspendRequested=true (process.go:174,
	//      Suspend currently only flips state without setting the flag;
	//      Task 1 leaves it alone, and the real production path
	//      `suspendOneForSubtree` does the Store explicitly).
	//   2. proc.Cancel().
	//
	// Since this is a unit test and we can't call the kernel-internal
	// helper, dev-story must EITHER export `Process.RequestSuspend()` (a
	// lightweight setter for the atomic) OR this test must move to
	// kernel/_test.go space. The cleanest is the former — and 44.2
	// Task 1.1 already adds an explicit setter for resumedCh anyway.
	//
	// For now we call Suspend (which transitions state) and then Cancel.
	// dev-story may need to insert `proc.RequestSuspend()` between them
	// — this test will fail with a clear "IsSuspendRequested returned
	// false; want true" message if the production helper doesn't set
	// the flag.
	proc.SetSuspendReason("user_paused")
	if err := proc.Suspend(); err != nil {
		return err
	}
	// Ensure the flag is observable. If Suspend() does not set it (it
	// currently does not — that is done by suspendOneForSubtree), the
	// test will fail at the assertion site; dev-story must expose a
	// setter or refactor Suspend.
	if !proc.IsSuspendRequested() {
		// Best-effort: try the kernel-level subtree helper via a setter
		// that 44.2 must add. Reference the not-yet-existing method
		// `RequestSuspend()` so the compile error makes the dependency
		// explicit to dev-story.
		proc.RequestSuspend()
	}
	proc.Cancel()
	return nil
}
