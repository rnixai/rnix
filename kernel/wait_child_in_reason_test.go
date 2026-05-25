package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// newWaitChildTestKernel builds a real kernel with the minimal subsystems
// reapProcess touches (msgQueues, ctxMgr, procGroups, etc). The same fixture
// other reap-aware kernel tests use (see checkpoint_test.go).
func newWaitChildTestKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	k.SetStepDataDir(t.TempDir())
	t.Cleanup(func() { k.Shutdown() })
	return k
}

// newWaitTestProcess builds a Process that looks Running enough for
// WaitChildInReason — has a context, Done channel, StepTimeout, and a context
// slot so CtxFree on reap doesn't panic.
func newWaitTestProcess(t *testing.T, k *KernelImpl, intent string, stepTimeout time.Duration) *Process {
	t.Helper()
	proc := NewProcess(0, intent, nil)
	ctxID, err := k.ctxMgr.CtxAlloc(2048)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = stepTimeout
	proc.LastHeartbeat = time.Now()
	proc.CtxID = ctxID
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)
	return proc
}

// TestWaitChildInReason_NormalCompletion verifies the happy path: when the
// child writes its terminal Exit to Done, WaitChildInReason returns the exit
// with cancelled=false and reaps the child.
func TestWaitChildInReason_NormalCompletion(t *testing.T) {
	k := newWaitChildTestKernel(t)
	parent := newWaitTestProcess(t, k, "parent", 500*time.Millisecond)
	child := newWaitTestProcess(t, k, "child", 500*time.Millisecond)

	exitWant := ExitStatus{Code: 0, Reason: "completed"}

	type result struct {
		exit      ExitStatus
		cancelled bool
	}
	resultCh := make(chan result, 1)

	go func() {
		ex, c := k.WaitChildInReason(parent, child.PID)
		resultCh <- result{exit: ex, cancelled: c}
	}()

	time.Sleep(20 * time.Millisecond)
	child.Done <- exitWant

	select {
	case r := <-resultCh:
		if r.cancelled {
			t.Fatalf("cancelled=true on normal completion, want false")
		}
		if r.exit.Code != exitWant.Code || r.exit.Reason != exitWant.Reason {
			t.Errorf("exit = %+v, want %+v", r.exit, exitWant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitChildInReason did not return after child.Done write")
	}
}

// TestWaitChildInReason_ParentCancellation verifies the load-bearing pause-fix
// invariant: cancelling the parent's ctx (the path SuspendSubtree takes via
// proc.Cancel()) must unblock WaitChildInReason with cancelled=true. Without
// this, suspendOneForSubtree's proc.wg.Wait() would deadlock against a child
// that takes minutes to finish.
func TestWaitChildInReason_ParentCancellation(t *testing.T) {
	k := newWaitChildTestKernel(t)
	parent := newWaitTestProcess(t, k, "parent", 500*time.Millisecond)
	child := newWaitTestProcess(t, k, "child", 500*time.Millisecond)

	type result struct {
		exit      ExitStatus
		cancelled bool
	}
	resultCh := make(chan result, 1)

	start := time.Now()
	go func() {
		ex, c := k.WaitChildInReason(parent, child.PID)
		resultCh <- result{exit: ex, cancelled: c}
	}()

	time.Sleep(20 * time.Millisecond)
	parent.Cancel()

	select {
	case r := <-resultCh:
		if !r.cancelled {
			t.Fatalf("cancelled=false after parent.Cancel(), want true")
		}
		elapsed := time.Since(start)
		if elapsed > 200*time.Millisecond {
			t.Errorf("WaitChildInReason took %v to react to parent cancel, want <200ms", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitChildInReason did not return after parent.Cancel() — pause would deadlock")
	}
}

// TestWaitChildInReason_HeartbeatRefresh verifies the STALL-fix invariant:
// while parent waits on a long-running child, parent.LastHeartbeat must be
// touched periodically so heartbeat_monitor does not escalate stall recovery
// on a parent that is doing nothing wrong.
func TestWaitChildInReason_HeartbeatRefresh(t *testing.T) {
	k := newWaitChildTestKernel(t)
	// StepTimeout=300ms → heartbeat interval = 100ms (StepTimeout/3, capped at 30s)
	parent := newWaitTestProcess(t, k, "parent", 300*time.Millisecond)
	child := newWaitTestProcess(t, k, "child", 300*time.Millisecond)

	parent.mu.Lock()
	parent.LastHeartbeat = time.Now().Add(-1 * time.Hour)
	staleHB := parent.LastHeartbeat
	parent.mu.Unlock()

	done := make(chan struct{})
	go func() {
		k.WaitChildInReason(parent, child.PID)
		close(done)
	}()

	// Wait long enough for at least 2 ticks (200ms) plus buffer.
	time.Sleep(280 * time.Millisecond)

	parent.mu.Lock()
	refreshedHB := parent.LastHeartbeat
	parent.mu.Unlock()

	// Stop the wait so the goroutine exits cleanly.
	parent.Cancel()
	<-done

	if !refreshedHB.After(staleHB) {
		t.Fatalf("parent.LastHeartbeat not refreshed: still %v (was %v) — STALL would fire", refreshedHB, staleHB)
	}
	gap := time.Since(refreshedHB)
	if gap > 200*time.Millisecond {
		t.Errorf("heartbeat gap = %v after refresh, want <200ms", gap)
	}
}

// TestWaitChildInReason_MissingChildReturnsImmediately covers the defensive
// branch when the child PID is no longer in procTable (e.g. raced reap).
func TestWaitChildInReason_MissingChildReturnsImmediately(t *testing.T) {
	k := newWaitChildTestKernel(t)
	parent := newWaitTestProcess(t, k, "parent", 500*time.Millisecond)

	done := make(chan struct{})
	go func() {
		_, cancelled := k.WaitChildInReason(parent, types.PID(99999))
		if cancelled {
			t.Errorf("cancelled=true on missing child, want false")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("WaitChildInReason hung on missing child")
	}
}
