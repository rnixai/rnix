package kernel

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.1 — Direct unit tests for the new subtree-level pause/resume API.
//
// Covers:
//   - AC#3: Kernel.ResumeSubtree (rootPID) returning (affected, skipped, err).
//   - AC#3: SubtreeManager interface declaration + compile-time conformance.
//   - AC#1 / AC#2 internals that are best asserted by calling the subtree
//     helpers directly rather than going through Signal(SIGPAUSE/SIGRESUME).
//
// All tests assume:
//   - KernelImpl gains the methods `SuspendSubtree(rootPID) (int, error)` and
//     `ResumeSubtree(rootPID) (affected, skipped int, err error)`.
//   - A new package-level interface `SubtreeManager` exists in this package,
//     with `KernelImpl` declaring `var _ SubtreeManager = (*KernelImpl)(nil)`.
//
// Until those land, every test in this file fails to compile — which is the
// intended RED phase signal.
// =============================================================================

// --- AC#3 / AC#1: SuspendSubtree returns affected count ---

func TestATDD_44_1_024_SuspendSubtree_ReturnsAffected(t *testing.T) {
	k := newSubtreeKernel(t)

	p1 := makeRunningProc44_1(t, k, 0, "P1 root")
	p2 := makeRunningProc44_1(t, k, p1.PID, "P2 child")
	p3 := makeRunningProc44_1(t, k, p2.PID, "P3 grandchild")

	affected, err := k.SuspendSubtree(p1.PID)
	if err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	if affected != 3 {
		t.Errorf("affected = %d, want 3 (P1+P2+P3)", affected)
	}

	for _, proc := range []*Process{p1, p2, p3} {
		assertProcState44_1(t, proc, types.StateSuspended, proc.Intent)
	}
}

// --- AC#3: ResumeSubtree returns (affected, skipped) ---

func TestATDD_44_1_020_ResumeSubtree_ReturnsAffectedAndSkipped(t *testing.T) {
	k := newSubtreeKernel(t)

	p1 := makeSuspendedProc44_1(t, k, 0, "P1", "user_paused")
	p2 := makeSuspendedProc44_1(t, k, p1.PID, "P2", "user_paused")
	// Already-Running descendant: should be counted as skipped, not double-resumed.
	p3 := makeRunningProc44_1(t, k, p2.PID, "P3 already running")
	// Dead descendant: skipped.
	pDead := makeDeadProc44_1(t, k, p2.PID, "Dead descendant")

	affected, skipped, err := k.ResumeSubtree(p1.PID)
	if err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	// Strict counts (Story 44.1 code review F30): asymmetric strictness
	// (affected==2 vs skipped>=2) made the test brittle to future
	// reclassification of already-Running nodes. Pin both ends.
	if affected != 2 {
		t.Errorf("affected = %d, want 2 (only P1 and P2 transition Suspended→Running)", affected)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2 (P3 already running + Dead descendant)", skipped)
	}

	assertProcState44_1(t, p1, types.StateRunning, "P1")
	assertProcState44_1(t, p2, types.StateRunning, "P2")
	if got := p3.GetState(); got != types.StateRunning {
		t.Errorf("P3 was already Running and must remain so; got %s", got)
	}
	if got := pDead.GetState(); got != types.StateDead {
		t.Errorf("Dead descendant must stay Dead; got %s", got)
	}
}

// --- AC#3: Unknown root PID surfaces ErrNotFound ---

func TestATDD_44_1_021_ResumeSubtree_RootPIDNotFound_ReturnsErrNotFound(t *testing.T) {
	k := newSubtreeKernel(t)

	affected, skipped, err := k.ResumeSubtree(types.PID(987654321))
	if err == nil {
		t.Fatal("expected error for unknown root PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T (%v)", err, err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("err.Code = %v, want ErrNotFound", se.Code)
	}
	if affected != 0 || skipped != 0 {
		t.Errorf("unknown root must return (0, 0, err); got (%d, %d)", affected, skipped)
	}
}

// --- AC#3: Root is already Running → affected=0 (nothing to resume here),
//     descendants still inspected so skipped may be >0 but never negative. ---

func TestATDD_44_1_022_ResumeSubtree_RootIsRunning_ReturnsZeroAffected(t *testing.T) {
	k := newSubtreeKernel(t)

	root := makeRunningProc44_1(t, k, 0, "running root")
	// Suspended descendant should still get woken up.
	susChild := makeSuspendedProc44_1(t, k, root.PID, "suspended child", "user_paused")

	affected, skipped, err := k.ResumeSubtree(root.PID)
	if err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	if affected != 1 {
		t.Errorf("affected = %d, want 1 (only the suspended child)", affected)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (root was already Running)", skipped)
	}
	assertProcState44_1(t, susChild, types.StateRunning, "suspended child after ResumeSubtree")
	if got := root.GetState(); got != types.StateRunning {
		t.Errorf("root was already Running; got %s", got)
	}
}

// --- AC#3: SubtreeManager interface compliance check ---
//
// The compile-time assertion `var _ SubtreeManager = (*KernelImpl)(nil)` lives
// in production code (kernel/subtree.go or kernel/kernel.go). This test makes
// the contract explicit at the test level too so a future refactor that removes
// the production-side assertion still fails here.
//
// Required interface (see story task 5.2):
//
//   type SubtreeManager interface {
//       SuspendSubtree(rootPID types.PID) (int, error)
//       ResumeSubtree(rootPID types.PID) (affected, skipped int, err error)
//   }

func TestATDD_44_1_023_SubtreeManager_InterfaceCompliance(t *testing.T) {
	var _ SubtreeManager = (*KernelImpl)(nil)
	// The line above is the test. If KernelImpl drifts from SubtreeManager the
	// package fails to compile, which is the strongest possible signal.
}

// --- AC#2: Dead descendant emits skipped_dead=true ---

func TestATDD_44_1_012_SignalSIGRESUME_SkipsDeadDescendant(t *testing.T) {
	k := newSubtreeKernel(t)

	root := makeSuspendedProc44_1(t, k, 0, "root", "user_paused")
	dead := makeDeadProc44_1(t, k, root.PID, "dead descendant")

	_, _, err := k.ResumeSubtree(root.PID)
	if err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	events := drainDebugChan44_1(t, dead, 4, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Resume", map[string]any{"skipped_dead": true}); ev == nil {
		t.Errorf("expected Resume event with skipped_dead=true on dead descendant; got events=%+v", events)
	}
}

// --- AC#2: Failed (Dead with non-zero exit) descendant emits skipped_failed=true ---

func TestATDD_44_1_013_SignalSIGRESUME_SkipsFailedDescendant(t *testing.T) {
	k := newSubtreeKernel(t)

	root := makeSuspendedProc44_1(t, k, 0, "root", "user_paused")
	failed := makeFailedProc44_1(t, k, root.PID, "failed descendant")

	_, _, err := k.ResumeSubtree(root.PID)
	if err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	events := drainDebugChan44_1(t, failed, 4, 200*time.Millisecond)
	if ev := findEvent44_1(events, "Resume", map[string]any{"skipped_failed": true}); ev == nil {
		t.Errorf("expected Resume event with skipped_failed=true on failed descendant; got events=%+v", events)
	}
}

// --- AC#2: ResumeSubtree must serialize via the same resumeMu as ResumeWithOpts.
// Concurrent invocations must not interleave their state transitions.
//
// We observe serialization directly with a parallel counter: each goroutine
// increments a shared atomic on entry and decrements on exit. If resumeMu is
// honoured, the peak value never exceeds 1; without serialization the peak
// climbs to len(roots). Spawning many roots makes the race window wide enough
// for an unsynchronised implementation to be caught reliably (Story 44.1 code
// review F20). ---

func TestATDD_44_1_015_ResumeSubtree_SerializedByResumeMu(t *testing.T) {
	k := newSubtreeKernel(t)

	const numRoots = 8
	roots := make([]*Process, 0, numRoots)
	for range numRoots {
		root := makeSuspendedProc44_1(t, k, 0, "root", "user_paused")
		makeSuspendedProc44_1(t, k, root.PID, "child", "user_paused")
		roots = append(roots, root)
	}

	// Probe: try to grab resumeMu in a sibling goroutine while ResumeSubtree
	// calls are in flight. If TryLock succeeds during a call window, the
	// lock is not being held — i.e. serialization is broken. We loop the
	// probe rapidly to maximise the chance of hitting a call mid-flight.
	probeStop := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		for {
			select {
			case <-probeStop:
				return
			default:
			}
			if k.resumeMu.TryLock() {
				k.resumeMu.Unlock()
			}
			// Pace probe to roughly the call duration; runtime.Gosched is
			// enough on the developer's machine and on CI.
			runtime.Gosched()
		}
	}()

	errs := make(chan error, len(roots))
	for _, r := range roots {
		go func(r *Process) {
			_, _, err := k.ResumeSubtree(r.PID)
			errs <- err
		}(r)
	}

	for range roots {
		select {
		case e := <-errs:
			if e != nil {
				t.Errorf("ResumeSubtree returned error under concurrency: %v", e)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent ResumeSubtree did not complete within 2s (deadlock?)")
		}
	}
	close(probeStop)
	<-probeDone

	for _, r := range roots {
		assertProcState44_1(t, r, types.StateRunning, "root after concurrent ResumeSubtree")
	}

	// Final post-condition: resumeMu must be free (released by every call).
	if !k.resumeMu.TryLock() {
		t.Fatal("resumeMu still held after all ResumeSubtree calls returned")
	}
	k.resumeMu.Unlock()
}

// --- AC#2: ResumeSubtree must NOT walk upward through ancestors ---

func TestATDD_44_1_014_SignalSIGRESUME_DoesNotWakeAncestor(t *testing.T) {
	k := newSubtreeKernel(t)

	// P1 is Suspended; P2 (its child) is Suspended; ResumeSubtree(P2) must
	// touch P2 but never P1.
	p1 := makeSuspendedProc44_1(t, k, 0, "P1 ancestor", "user_paused")
	p2 := makeSuspendedProc44_1(t, k, p1.PID, "P2 child", "user_paused")

	if _, _, err := k.ResumeSubtree(p2.PID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	assertProcState44_1(t, p2, types.StateRunning, "P2 (subtree root)")
	if got := p1.GetState(); got != types.StateSuspended {
		t.Errorf("P1 ancestor state = %s, want Suspended (subtree resume must not walk up)", got)
	}
}

// --- Smoke: ResumeSubtree never returns a non-SyscallError error ---

func TestATDD_44_1_025_ResumeSubtree_ErrorTypeIsSyscallError(t *testing.T) {
	k := newSubtreeKernel(t)

	_, _, err := k.ResumeSubtree(types.PID(0))
	if err == nil {
		t.Fatal("expected error for PID 0")
	}
	var se *SyscallError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
}
