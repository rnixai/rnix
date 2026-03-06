package kernel

import (
	gocontext "context"
	"sync"
	"testing"
	"time"

	"github.com/usecrux/crux/internal/types"
)

// newSignalTestProcess creates a Running test process registered in the kernel.
func newSignalTestProcess(t *testing.T, k *KernelImpl) *Process {
	t.Helper()
	proc := NewProcess(0, "signal-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	_ = proc.Start() // Created → Running
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	return proc
}

// TestSignal_Basic verifies SIGTERM terminates the process context.
func TestSignal_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	select {
	case <-proc.ctx.Done():
		// expected: context cancelled
	default:
		t.Fatal("expected context to be cancelled after SIGTERM")
	}
}

// TestSignal_SIGINT verifies SIGINT terminates the process context.
func TestSignal_SIGINT(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	if err := k.Signal(proc.PID, types.SIGINT); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	select {
	case <-proc.ctx.Done():
		// expected
	default:
		t.Fatal("expected context to be cancelled after SIGINT")
	}
}

// TestSignal_InvalidSignal verifies invalid signal returns ErrInvalid.
func TestSignal_InvalidSignal(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	err := k.Signal(proc.PID, types.Signal(99))
	if err == nil {
		t.Fatal("expected error for invalid signal")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", se.Code)
	}
}

// TestSignal_ProcessNotFound verifies non-existent PID returns ErrNotFound.
func TestSignal_ProcessNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	err := k.Signal(99999, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestSignal_ZombieProcess verifies signaling Zombie/Dead returns ErrNotFound.
func TestSignal_ZombieProcess(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})

	err := k.Signal(proc.PID, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for zombie process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestSignal_SIGPAUSE verifies SIGPAUSE puts process into paused state.
func TestSignal_SIGPAUSE(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal SIGPAUSE failed: %v", err)
	}

	if !proc.IsPaused() {
		t.Fatal("expected process to be paused after SIGPAUSE")
	}
}

// TestSignal_SIGRESUME verifies SIGRESUME resumes a paused process.
func TestSignal_SIGRESUME(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// First pause
	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("SIGPAUSE failed: %v", err)
	}
	if !proc.IsPaused() {
		t.Fatal("expected paused")
	}

	// Then resume
	if err := k.Signal(proc.PID, types.SIGRESUME); err != nil {
		t.Fatalf("SIGRESUME failed: %v", err)
	}
	if proc.IsPaused() {
		t.Fatal("expected not paused after SIGRESUME")
	}
}

// TestSignal_ResumeNotPaused verifies SIGRESUME on non-paused process is noop.
func TestSignal_ResumeNotPaused(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Not paused — SIGRESUME should be noop
	if err := k.Signal(proc.PID, types.SIGRESUME); err != nil {
		t.Fatalf("SIGRESUME failed: %v", err)
	}
	if proc.IsPaused() {
		t.Fatal("should not be paused")
	}
	// Context should still be alive
	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled")
	default:
	}
}

// TestSigBlock_Basic verifies blocked signal goes to pending.
func TestSigBlock_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Block SIGTERM
	if err := k.SigBlock(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("SigBlock failed: %v", err)
	}

	// Send SIGTERM — should go to pending, not terminate
	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	// Context should NOT be cancelled
	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled when signal is blocked")
	default:
	}

	// Verify pending
	if !proc.HasPending(types.SIGTERM) {
		t.Fatal("SIGTERM should be in pending set")
	}
}

// TestSigBlock_SIGKILL_Rejected verifies SIGKILL cannot be blocked.
func TestSigBlock_SIGKILL_Rejected(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	err := k.SigBlock(proc.PID, types.SIGKILL)
	if err == nil {
		t.Fatal("expected error blocking SIGKILL")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", se.Code)
	}
}

// TestSigUnblock_TriggersPending verifies unblock delivers pending signal.
func TestSigUnblock_TriggersPending(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Block SIGTERM, send it (goes to pending), then unblock
	if err := k.SigBlock(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("SigBlock failed: %v", err)
	}
	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	// Context still alive
	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled yet")
	default:
	}

	// Unblock — pending SIGTERM should be delivered
	if err := k.SigUnblock(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("SigUnblock failed: %v", err)
	}

	// Now context should be cancelled
	select {
	case <-proc.ctx.Done():
		// expected
	default:
		t.Fatal("expected context cancelled after unblocking pending SIGTERM")
	}
}

// TestSigUnblock_NoPending verifies unblock with no pending is noop.
func TestSigUnblock_NoPending(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Block and then unblock without sending signal
	if err := k.SigBlock(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("SigBlock failed: %v", err)
	}
	if err := k.SigUnblock(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("SigUnblock failed: %v", err)
	}

	// Context should still be alive
	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled")
	default:
	}
}

// TestSignalHandler_Custom verifies custom handler is called instead of default.
func TestSignalHandler_Custom(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	var handlerCalled bool
	var handlerSig types.Signal
	proc.SetHandler(types.SIGTERM, func(sig types.Signal) {
		handlerCalled = true
		handlerSig = sig
	})

	if err := k.Signal(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	if !handlerCalled {
		t.Fatal("custom handler should have been called")
	}
	if handlerSig != types.SIGTERM {
		t.Errorf("handler received %v, want SIGTERM", handlerSig)
	}

	// Context should NOT be cancelled (handler overrides default behavior)
	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled when custom handler is registered")
	default:
	}
}

// TestSignalHandler_Override verifies handler overrides default termination.
func TestSignalHandler_Override(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	callCount := 0
	proc.SetHandler(types.SIGINT, func(_ types.Signal) {
		callCount++
	})

	// Send SIGINT twice — handler should be called both times, process not killed
	_ = k.Signal(proc.PID, types.SIGINT)
	_ = k.Signal(proc.PID, types.SIGINT)

	if callCount != 2 {
		t.Errorf("handler call count = %d, want 2", callCount)
	}

	select {
	case <-proc.ctx.Done():
		t.Fatal("context should not be cancelled")
	default:
	}
}

// TestKill_DelegatesToSignal verifies Kill still works (backward compatibility).
func TestKill_DelegatesToSignal(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	if err := k.Kill(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	select {
	case <-proc.ctx.Done():
		// expected
	default:
		t.Fatal("expected context cancelled after Kill")
	}
}

// TestKill_WithSIGPAUSE verifies Kill(pid, SIGPAUSE) pauses the process.
func TestKill_WithSIGPAUSE(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	if err := k.Kill(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("Kill with SIGPAUSE failed: %v", err)
	}

	if !proc.IsPaused() {
		t.Fatal("expected process to be paused after Kill(SIGPAUSE)")
	}
}

// TestSignalGroup_WithNewSignals verifies SignalGroup + SIGPAUSE pauses all members.
func TestSignalGroup_WithNewSignals(t *testing.T) {
	k := newSimpleKernel(t)

	procs := make([]*Process, 3)
	for i := range procs {
		procs[i] = newSignalTestProcess(t, k)
		if err := k.JoinGroup(procs[i].PID, 100); err != nil {
			t.Fatalf("JoinGroup failed: %v", err)
		}
	}

	if err := k.SignalGroup(100, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalGroup failed: %v", err)
	}

	for i, proc := range procs {
		if !proc.IsPaused() {
			t.Errorf("proc[%d] should be paused", i)
		}
	}
}

// TestReapProcess_CleanupSignalState verifies signal state is cleaned on reap.
func TestReapProcess_CleanupSignalState(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Set up signal state
	proc.SetHandler(types.SIGTERM, func(_ types.Signal) {})
	proc.BlockSignal(types.SIGINT)
	proc.AddPending(types.SIGINT)

	// Terminate and reap
	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})
	k.reapProcess(proc)

	// Verify cleanup
	if _, ok := proc.GetHandler(types.SIGTERM); ok {
		t.Error("signal handlers should be cleared after reap")
	}
	if proc.IsBlocked(types.SIGINT) {
		t.Error("blocked signals should be cleared after reap")
	}
	if proc.HasPending(types.SIGINT) {
		t.Error("pending signals should be cleared after reap")
	}
}

// TestReapProcess_ResumeBeforeCleanup verifies paused process is resumed before reap.
func TestReapProcess_ResumeBeforeCleanup(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Pause the process
	proc.Pause()
	if !proc.IsPaused() {
		t.Fatal("expected paused")
	}

	// Terminate and reap
	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})
	k.reapProcess(proc)

	// Verify not paused after reap
	if proc.IsPaused() {
		t.Error("process should not be paused after reap")
	}
}

// TestSignal_Concurrent verifies 100 goroutines performing Signal/SigBlock/SigUnblock without race.
func TestSignal_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	const n = 100
	var wg sync.WaitGroup

	for range n {
		wg.Go(func() {
			// Mix of operations
			_ = k.SigBlock(proc.PID, types.SIGTERM)
			_ = k.Signal(proc.PID, types.SIGPAUSE)
			_ = k.SigUnblock(proc.PID, types.SIGTERM)
			_ = k.Signal(proc.PID, types.SIGRESUME)
		})
	}

	wg.Wait()
	// No race detected = pass (test runs with -race flag)
}

// TestSignal_SyscallEvent verifies DebugChan receives Signal/SigBlock/SigUnblock events.
func TestSignal_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Signal event
	_ = k.Signal(proc.PID, types.SIGPAUSE)

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "Signal" {
			t.Errorf("Syscall = %q, want Signal", ev.Syscall)
		}
		if ev.Args["signal"] != "SIGPAUSE" {
			t.Errorf("signal = %v, want SIGPAUSE", ev.Args["signal"])
		}
		if ev.Args["action"] != "paused" {
			t.Errorf("action = %v, want paused", ev.Args["action"])
		}
	case <-time.After(time.Second):
		t.Fatal("no Signal event received")
	}

	// SigBlock event
	_ = k.SigBlock(proc.PID, types.SIGTERM)

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SigBlock" {
			t.Errorf("Syscall = %q, want SigBlock", ev.Syscall)
		}
	case <-time.After(time.Second):
		t.Fatal("no SigBlock event received")
	}

	// SigUnblock event
	_ = k.SigUnblock(proc.PID, types.SIGTERM)

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SigUnblock" {
			t.Errorf("Syscall = %q, want SigUnblock", ev.Syscall)
		}
	case <-time.After(time.Second):
		t.Fatal("no SigUnblock event received")
	}
}

// TestSignal_PauseResumeIntegration verifies pause blocks WaitIfPaused and resume unblocks it.
func TestSignal_PauseResumeIntegration(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Pause
	if err := k.Signal(proc.PID, types.SIGPAUSE); err != nil {
		t.Fatalf("SIGPAUSE failed: %v", err)
	}

	ch := proc.WaitIfPaused()
	if ch == nil {
		t.Fatal("expected non-nil channel from WaitIfPaused")
	}

	// Verify channel blocks
	select {
	case <-ch:
		t.Fatal("channel should be blocking")
	default:
	}

	// Resume via signal
	resumed := make(chan struct{})
	go func() {
		<-ch
		close(resumed)
	}()

	time.Sleep(10 * time.Millisecond)
	if err := k.Signal(proc.PID, types.SIGRESUME); err != nil {
		t.Fatalf("SIGRESUME failed: %v", err)
	}

	select {
	case <-resumed:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("WaitIfPaused did not unblock after SIGRESUME")
	}

	// Verify not paused anymore
	if proc.IsPaused() {
		t.Fatal("should not be paused after resume")
	}
}

// --- Review-driven test cases (Code Review 6.4) ---

// TestSignal_SIGKILL_IgnoresHandler verifies SIGKILL cannot be intercepted by custom handler.
func TestSignal_SIGKILL_IgnoresHandler(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	handlerCalled := false
	proc.SetHandler(types.SIGKILL, func(_ types.Signal) {
		handlerCalled = true
	})

	if err := k.Signal(proc.PID, types.SIGKILL); err != nil {
		t.Fatalf("Signal SIGKILL failed: %v", err)
	}

	if handlerCalled {
		t.Fatal("SIGKILL handler should NOT be called — force-kill semantics")
	}

	// Context must be cancelled (default termination behavior)
	select {
	case <-proc.ctx.Done():
		// expected
	default:
		t.Fatal("SIGKILL should terminate process even with handler registered")
	}
}

// TestSigBlock_ZombieProcess verifies SigBlock on Zombie process returns ErrNotFound.
func TestSigBlock_ZombieProcess(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})

	err := k.SigBlock(proc.PID, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for SigBlock on zombie process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestSigUnblock_ZombieProcess verifies SigUnblock on Zombie process returns ErrNotFound.
func TestSigUnblock_ZombieProcess(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newSignalTestProcess(t, k)

	// Block and add pending before terminating
	_ = k.SigBlock(proc.PID, types.SIGTERM)
	_ = k.Signal(proc.PID, types.SIGTERM)

	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})

	err := k.SigUnblock(proc.PID, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for SigUnblock on zombie process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}
