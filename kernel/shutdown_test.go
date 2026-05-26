package kernel

import (
	gocontext "context"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// newShutdownTestProcess creates a Running process with a simulated goroutine
// that exits when ctx is cancelled.
func newShutdownTestProcess(t *testing.T, k *KernelImpl, gracePeriod time.Duration) *Process {
	t.Helper()
	proc := NewProcess(0, "shutdown-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	proc.GracePeriod = gracePeriod
	_ = proc.Start()
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	return proc
}

// TestTwoPhaseShutdown_GraceExit verifies that SIGTERM triggers two-phase shutdown
// and cancels the process context (phase 1).
func TestTwoPhaseShutdown_GraceExit(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newShutdownTestProcess(t, k, 5*time.Second)

	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal SIGTERM failed: %v", err)
	}

	// Context should be cancelled immediately (phase 1)
	select {
	case <-proc.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled within 1s after SIGTERM")
	}

	// shutdownStarted should be set
	if !proc.shutdownStarted.Load() {
		t.Error("shutdownStarted should be true after SIGTERM")
	}
}

// TestTwoPhaseShutdown_SIGKILL_Immediate verifies SIGKILL bypasses grace period
// and cancels immediately.
func TestTwoPhaseShutdown_SIGKILL_Immediate(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newShutdownTestProcess(t, k, 5*time.Second)

	start := time.Now()
	if err := k.Kill(proc.PID, types.SIGKILL); err != nil {
		t.Fatalf("Kill SIGKILL failed: %v", err)
	}

	select {
	case <-proc.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled immediately on SIGKILL")
	}

	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("SIGKILL took %v, expected < 500ms", elapsed)
	}

	// shutdownStarted should NOT be set for SIGKILL (it bypasses CAS)
	// SIGKILL uses Kill() path, not signal handler
}

// TestTwoPhaseShutdown_Idempotent verifies that sending SIGTERM twice does not panic
// and CAS guard prevents duplicate shutdown goroutines.
func TestTwoPhaseShutdown_Idempotent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newShutdownTestProcess(t, k, 2*time.Second)

	// First SIGTERM
	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("first SIGTERM failed: %v", err)
	}

	// Second SIGTERM should not error or panic
	_ = k.Signal(proc.PID, types.SIGTERM)

	// Context cancelled
	select {
	case <-proc.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after SIGTERM")
	}

	// Only one shutdown goroutine should run (CAS prevents duplicate)
	if !proc.shutdownStarted.Load() {
		t.Error("shutdownStarted should be true")
	}
}

// TestTwoPhaseShutdown_CustomGracePeriod verifies that effectiveGracePeriod
// returns the custom value when set.
func TestTwoPhaseShutdown_CustomGracePeriod(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newShutdownTestProcess(t, k, 100*time.Millisecond)

	if got := proc.effectiveGracePeriod(); got != 100*time.Millisecond {
		t.Errorf("effectiveGracePeriod() = %v, want 100ms", got)
	}

	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal SIGTERM failed: %v", err)
	}

	select {
	case <-proc.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after SIGTERM")
	}
}

// TestShutdown_RunningProcsTransitionToSuspended is the regression test for
// the EchoMatrix dashboard symptom captured on 2026-05-26: pausing a child
// then restarting the daemon left the root process showing ✗ (zombie +
// exit_code=1) because the shutdown drain loop did a bare proc.Cancel()
// without setting proc.suspendRequested. reasonStep's defer
// (kernel/reason.go:248-259) then saw state=Running with
// suspendRequested=false and routed to "unexpected exit" — even though the
// user only paused a child, never killed the root.
//
// The Shutdown drain loop now stamps suspendRequested before Cancel and
// transitions Running → Suspended with reason="daemon_shutdown" before
// SaveProcInfo, so the disk snapshot is what LoadSuspended needs on the
// next daemon startup to rehydrate the process as resumable.
func TestShutdown_RunningProcsTransitionToSuspended(t *testing.T) {
	k := newSimpleKernel(t)

	// Skip the t.Cleanup Shutdown — we drive Shutdown explicitly below.
	proc := NewProcess(0, "shutdown-suspend-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	if got := proc.GetState(); got != types.StateRunning {
		t.Fatalf("precondition: proc state = %s, want Running", got)
	}

	// First Shutdown drains the process; t.Cleanup's second Shutdown is a no-op.
	k.Shutdown()

	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("after Shutdown: proc state = %s, want Suspended (was Running)", got)
	}
	if !proc.suspendRequested.Load() {
		t.Error("after Shutdown: suspendRequested should be true so reasonStep defer takes the notifySuspendDone path")
	}

	proc.mu.Lock()
	suspendReason := proc.SuspendReason
	exit := proc.Exit
	proc.mu.Unlock()

	if suspendReason != "daemon_shutdown" {
		t.Errorf("SuspendReason = %q, want %q", suspendReason, "daemon_shutdown")
	}
	if exit == nil {
		t.Fatal("Exit is nil after Shutdown")
	}
	if exit.Code != ExitSuspended {
		t.Errorf("Exit.Code = %d, want ExitSuspended (%d) — anything else (esp. 1) shows up red in dashboard", exit.Code, ExitSuspended)
	}
}

// TestEffectiveGracePeriod verifies the method returns the default when unset.
func TestEffectiveGracePeriod(t *testing.T) {
	proc := NewProcess(0, "grace-test", nil)

	// Default (zero) → DefaultGracePeriod
	if got := proc.effectiveGracePeriod(); got != DefaultGracePeriod {
		t.Errorf("effectiveGracePeriod() = %v, want %v", got, DefaultGracePeriod)
	}

	// Custom
	proc.GracePeriod = 42 * time.Second
	if got := proc.effectiveGracePeriod(); got != 42*time.Second {
		t.Errorf("effectiveGracePeriod() = %v, want 42s", got)
	}
}

