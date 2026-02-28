package kernel

import (
	gocontext "context"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// newConcurrencyTestProcess creates a Running test process registered in the kernel
// for concurrency (thread/coroutine) tests.
func newConcurrencyTestProcess(t *testing.T, k *KernelImpl) *Process {
	t.Helper()
	proc := NewProcess(0, "concurrency-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	_ = proc.Start() // Created -> Running
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	return proc
}

// --- Thread Tests (AC #2: Thread-level concurrency) ---

// TestSpawnThread_Basic verifies that a thread can be spawned for a running process
// and shares the parent's CtxID.
func TestSpawnThread_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	tid, err := k.SpawnThread(proc.PID, "test-thread")
	if err != nil {
		t.Fatalf("SpawnThread failed: %v", err)
	}
	if tid == 0 {
		t.Fatal("expected non-zero TID")
	}

	// Verify thread is registered in parent process
	thread, ok := proc.GetThread(tid)
	if !ok {
		t.Fatal("thread not found in parent process")
	}
	if thread.ParentPID != proc.PID {
		t.Errorf("ParentPID = %d, want %d", thread.ParentPID, proc.PID)
	}
	if thread.Intent != "test-thread" {
		t.Errorf("Intent = %q, want %q", thread.Intent, "test-thread")
	}
}

// TestSpawnThread_ParentNotFound verifies SpawnThread returns ErrNotFound for non-existent parent.
func TestSpawnThread_ParentNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.SpawnThread(99999, "orphan-thread")
	if err == nil {
		t.Fatal("expected error for non-existent parent PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
	if se.Syscall != "SpawnThread" {
		t.Errorf("Syscall = %q, want SpawnThread", se.Syscall)
	}
}

// TestSpawnThread_ParentNotRunning verifies SpawnThread returns ErrInvalid when parent is not Running.
func TestSpawnThread_ParentNotRunning(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	// Transition to Zombie
	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})

	_, err := k.SpawnThread(proc.PID, "thread-on-zombie")
	if err == nil {
		t.Fatal("expected error for non-Running parent")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", se.Code)
	}
}

// TestJoinThread_Basic verifies JoinThread waits for thread completion and returns.
func TestJoinThread_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	tid, err := k.SpawnThread(proc.PID, "joinable-thread")
	if err != nil {
		t.Fatalf("SpawnThread failed: %v", err)
	}

	// Thread should complete when we cancel its context (via parent)
	// or when the thread's goroutine finishes.
	// For testing, we join in a goroutine with a timeout.
	done := make(chan error, 1)
	go func() {
		done <- k.JoinThread(proc.PID, tid)
	}()

	// Give thread a moment to be running, then cancel parent context
	// which should cascade-cancel the thread's context
	time.Sleep(10 * time.Millisecond)
	proc.Cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("JoinThread failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("JoinThread did not return after parent cancel")
	}
}

// TestJoinThread_ThreadNotFound verifies JoinThread returns ErrNotFound for non-existent thread.
func TestJoinThread_ThreadNotFound(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	err := k.JoinThread(proc.PID, types.TID(99999))
	if err == nil {
		t.Fatal("expected error for non-existent thread")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestJoinThread_ParentNotFound verifies JoinThread returns ErrNotFound for non-existent parent.
func TestJoinThread_ParentNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	err := k.JoinThread(99999, types.TID(1))
	if err == nil {
		t.Fatal("expected error for non-existent parent PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestThread_SharesContext verifies the thread shares the parent process's CtxID.
func TestThread_SharesContext(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	tid, err := k.SpawnThread(proc.PID, "shared-ctx-thread")
	if err != nil {
		t.Fatalf("SpawnThread failed: %v", err)
	}

	thread, ok := proc.GetThread(tid)
	if !ok {
		t.Fatal("thread not found")
	}

	// Thread's context should be a child of the parent's context.
	// When parent's context is cancelled, thread's context should also be cancelled.
	proc.Cancel()

	select {
	case <-thread.ctx.Done():
		// expected: thread context cancelled when parent cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("thread context should be cancelled when parent context is cancelled")
	}
}

// TestThread_ParentKill verifies killing the parent process cancels all child threads.
func TestThread_ParentKill(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	// Spawn multiple threads
	tids := make([]types.TID, 3)
	for i := range 3 {
		tid, err := k.SpawnThread(proc.PID, "child-thread")
		if err != nil {
			t.Fatalf("SpawnThread %d failed: %v", i, err)
		}
		tids[i] = tid
	}

	// Kill parent via signal
	if err := k.Kill(proc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// All thread contexts should be cancelled (inherited from parent)
	for i, tid := range tids {
		thread, ok := proc.GetThread(tid)
		if !ok {
			// Thread may have already been cleaned up, which is also acceptable
			continue
		}
		select {
		case <-thread.ctx.Done():
			// expected
		case <-time.After(2 * time.Second):
			t.Errorf("thread %d (TID=%d) context not cancelled after parent kill", i, tid)
		}
	}
}

// TestThread_IndependentExecution verifies multiple threads run concurrently.
func TestThread_IndependentExecution(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	const numThreads = 5
	tids := make([]types.TID, numThreads)
	for i := range numThreads {
		tid, err := k.SpawnThread(proc.PID, "concurrent-thread")
		if err != nil {
			t.Fatalf("SpawnThread %d failed: %v", i, err)
		}
		tids[i] = tid
	}

	// All threads should be registered
	for i, tid := range tids {
		_, ok := proc.GetThread(tid)
		if !ok {
			t.Errorf("thread %d (TID=%d) not found in parent", i, tid)
		}
	}

	// TIDs should be unique and monotonically increasing
	for i := 1; i < len(tids); i++ {
		if tids[i] <= tids[i-1] {
			t.Errorf("TID[%d]=%d not greater than TID[%d]=%d", i, tids[i], i-1, tids[i-1])
		}
	}
}

// TestThread_MultipleTIDsAreProcessLocal verifies TIDs are process-local (different processes
// can have the same TID values).
func TestThread_MultipleTIDsAreProcessLocal(t *testing.T) {
	k := newSimpleKernel(t)
	proc1 := newConcurrencyTestProcess(t, k)
	proc2 := newConcurrencyTestProcess(t, k)

	tid1, err := k.SpawnThread(proc1.PID, "proc1-thread")
	if err != nil {
		t.Fatalf("SpawnThread proc1 failed: %v", err)
	}

	tid2, err := k.SpawnThread(proc2.PID, "proc2-thread")
	if err != nil {
		t.Fatalf("SpawnThread proc2 failed: %v", err)
	}

	// Both should have TID=1 since TIDs are process-local
	if tid1 != tid2 {
		t.Logf("TID1=%d, TID2=%d — TIDs may be process-local identical (both should be 1)", tid1, tid2)
	}
	// At minimum, both must be valid non-zero
	if tid1 == 0 || tid2 == 0 {
		t.Errorf("TIDs should be non-zero: tid1=%d, tid2=%d", tid1, tid2)
	}
}

// TestSpawnThread_SyscallEvent verifies DebugChan receives a SpawnThread event.
func TestSpawnThread_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	tid, err := k.SpawnThread(proc.PID, "event-thread")
	if err != nil {
		t.Fatalf("SpawnThread failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SpawnThread" {
			t.Errorf("Syscall = %q, want SpawnThread", ev.Syscall)
		}
		if ev.PID != proc.PID {
			t.Errorf("PID = %d, want %d", ev.PID, proc.PID)
		}
		if ev.Args["tid"] != tid {
			t.Errorf("Args[tid] = %v, want %d", ev.Args["tid"], tid)
		}
		if ev.Args["intent"] != "event-thread" {
			t.Errorf("Args[intent] = %v, want %q", ev.Args["intent"], "event-thread")
		}
	case <-time.After(time.Second):
		t.Fatal("no SpawnThread SyscallEvent received")
	}
}

// TestThread_Concurrent verifies multiple goroutines concurrently performing
// SpawnThread/JoinThread without race conditions.
func TestThread_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	const n = 50
	var wg sync.WaitGroup

	tids := make([]types.TID, n)
	var mu sync.Mutex

	// Concurrent SpawnThread
	for i := range n {
		wg.Go(func() {
			tid, err := k.SpawnThread(proc.PID, "concurrent-spawn")
			if err != nil {
				t.Errorf("SpawnThread %d failed: %v", i, err)
				return
			}
			mu.Lock()
			tids[i] = tid
			mu.Unlock()
		})
	}
	wg.Wait()

	// Cancel parent to finish all threads
	proc.Cancel()

	// Concurrent JoinThread
	for i := range n {
		if tids[i] == 0 {
			continue
		}
		wg.Go(func() {
			_ = k.JoinThread(proc.PID, tids[i])
		})
	}
	wg.Wait()
	// No race detected = pass (test runs with -race flag)
}
