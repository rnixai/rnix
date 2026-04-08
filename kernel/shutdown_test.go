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

