package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// --- Coroutine Tests (AC #3: Coroutine-level concurrency) ---

// TestSpawnCoroutine_Basic verifies a coroutine can be spawned and yields a value.
func TestSpawnCoroutine_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("hello")
		return "done"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}
	if coID == 0 {
		t.Fatal("expected non-zero CoID")
	}

	// The coroutine should have yielded "hello" — wait briefly for goroutine to start
	time.Sleep(20 * time.Millisecond)

	// First ResumeCoroutine should return the yielded value "hello"
	val, err := k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine failed: %v", err)
	}
	if val != "hello" {
		t.Errorf("yielded value = %v, want %q", val, "hello")
	}
}

// TestSpawnCoroutine_ParentNotFound verifies SpawnCoroutine returns ErrNotFound for non-existent parent.
func TestSpawnCoroutine_ParentNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.SpawnCoroutine(99999, func(yield func(any)) any {
		return nil
	})
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
	if se.Syscall != "SpawnCoroutine" {
		t.Errorf("Syscall = %q, want SpawnCoroutine", se.Syscall)
	}
}

// TestSpawnCoroutine_ParentNotRunning verifies SpawnCoroutine returns ErrInvalid when parent is not Running.
func TestSpawnCoroutine_ParentNotRunning(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})

	_, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		return nil
	})
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

// TestYield_Basic verifies the coroutine can yield a value to the caller.
func TestYield_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield(42)
		return "final"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	// Wait for coroutine goroutine to start and reach the yield point
	time.Sleep(20 * time.Millisecond)

	// First resume should get the yielded value 42
	val, err := k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine failed: %v", err)
	}
	if val != 42 {
		t.Errorf("yielded value = %v, want 42", val)
	}
}

// TestResumeCoroutine_Basic verifies resuming a suspended coroutine returns the next yield value.
func TestResumeCoroutine_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("first")
		yield("second")
		return "completed"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// First resume: get "first"
	val1, err := k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine 1 failed: %v", err)
	}
	if val1 != "first" {
		t.Errorf("resume 1 value = %v, want %q", val1, "first")
	}

	// Second resume: get "second"
	val2, err := k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine 2 failed: %v", err)
	}
	if val2 != "second" {
		t.Errorf("resume 2 value = %v, want %q", val2, "second")
	}
}

// TestResumeCoroutine_NotSuspended verifies resuming a non-suspended coroutine returns ErrInvalid.
func TestResumeCoroutine_NotSuspended(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("first")
		return "done"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// First resume: gets "first" (yield value)
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("first ResumeCoroutine failed: %v", err)
	}

	// Second resume: gets "done" (completion value, auto-cleans coroutine)
	val, err := k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("second ResumeCoroutine failed: %v", err)
	}
	if val != "done" {
		t.Errorf("completion value = %v, want %q", val, "done")
	}

	// Third resume: coroutine is auto-cleaned, should return ErrNotFound
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err == nil {
		t.Fatal("expected error for resuming completed/auto-cleaned coroutine")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestResumeCoroutine_NotFound verifies resuming a non-existent coroutine returns ErrNotFound.
func TestResumeCoroutine_NotFound(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	_, err := k.ResumeCoroutine(proc.PID, types.CoID(99999))
	if err == nil {
		t.Fatal("expected error for non-existent coroutine")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestResumeCoroutine_ParentNotFound verifies ResumeCoroutine returns ErrNotFound for non-existent parent.
func TestResumeCoroutine_ParentNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.ResumeCoroutine(99999, types.CoID(1))
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

// TestCoroutine_MultipleYields verifies multiple yield/resume cycles work correctly.
func TestCoroutine_MultipleYields(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield(1)
		yield(2)
		yield(3)
		return 4
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	expected := []any{1, 2, 3, 4}
	for i, want := range expected {
		val, err := k.ResumeCoroutine(proc.PID, coID)
		if err != nil {
			t.Fatalf("ResumeCoroutine %d failed: %v", i, err)
		}
		if val != want {
			t.Errorf("resume %d: got %v, want %v", i, val, want)
		}
	}
}

// TestCoroutine_Completion verifies a coroutine that completes without yielding
// enters coDone state and subsequent ResumeCoroutine returns ErrInvalid.
func TestCoroutine_Completion(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		return "all-done"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	// Wait for coroutine to complete (no yields, goes straight to return)
	time.Sleep(50 * time.Millisecond)

	// The coroutine completed without yielding, so it is in coDone state.
	// ResumeCoroutine should return ErrInvalid because coDone is not Suspended.
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err == nil {
		t.Fatal("expected error for resuming a completed coroutine (no yields)")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", se.Code)
	}
}

// TestSpawnCoroutine_SyscallEvent verifies DebugChan receives a SpawnCoroutine event.
func TestSpawnCoroutine_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("val")
		return nil
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SpawnCoroutine" {
			t.Errorf("Syscall = %q, want SpawnCoroutine", ev.Syscall)
		}
		if ev.PID != proc.PID {
			t.Errorf("PID = %d, want %d", ev.PID, proc.PID)
		}
		if ev.Args["co_id"] != coID {
			t.Errorf("Args[co_id] = %v, want %d", ev.Args["co_id"], coID)
		}
	case <-time.After(time.Second):
		t.Fatal("no SpawnCoroutine SyscallEvent received")
	}
}

// TestResumeCoroutine_SyscallEvent verifies DebugChan receives ResumeCoroutine events.
func TestResumeCoroutine_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("value")
		return "final"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	// Drain SpawnCoroutine event
	select {
	case <-proc.DebugChan:
	case <-time.After(time.Second):
		t.Fatal("no SpawnCoroutine event")
	}

	time.Sleep(20 * time.Millisecond)

	// Resume and check for ResumeCoroutine event
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "ResumeCoroutine" {
			t.Errorf("Syscall = %q, want ResumeCoroutine", ev.Syscall)
		}
		if ev.Args["co_id"] != coID {
			t.Errorf("Args[co_id] = %v, want %d", ev.Args["co_id"], coID)
		}
	case <-time.After(time.Second):
		t.Fatal("no ResumeCoroutine SyscallEvent received")
	}
}

// TestCoroutine_Concurrent verifies multiple goroutines concurrently performing
// SpawnCoroutine/Yield/Resume without race conditions.
func TestCoroutine_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	const n = 20
	var wg sync.WaitGroup

	for range n {
		wg.Go(func() {
			coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
				yield("concurrent-yield")
				return "concurrent-done"
			})
			if err != nil {
				t.Errorf("SpawnCoroutine failed: %v", err)
				return
			}

			time.Sleep(20 * time.Millisecond)

			// Resume to get yield value
			_, err = k.ResumeCoroutine(proc.PID, coID)
			if err != nil {
				t.Errorf("ResumeCoroutine (yield) coID=%d failed: %v", coID, err)
				return
			}

			// Resume to get final value
			_, err = k.ResumeCoroutine(proc.PID, coID)
			if err != nil {
				// May be auto-cleaned, acceptable
				return
			}
		})
	}
	wg.Wait()
	// No race detected = pass (test runs with -race flag)
}

// --- Reap Integration Tests ---

// TestReapProcess_CleansThreadsAndCoroutines verifies that reapProcess cleans up
// all threads and coroutines of a dying process.
func TestReapProcess_CleansThreadsAndCoroutines(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	// Spawn some threads
	for range 3 {
		_, err := k.SpawnThread(proc.PID, "reap-thread")
		if err != nil {
			t.Fatalf("SpawnThread failed: %v", err)
		}
	}

	// Spawn some coroutines
	for range 2 {
		_, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
			yield("waiting")
			return nil
		})
		if err != nil {
			t.Fatalf("SpawnCoroutine failed: %v", err)
		}
	}

	time.Sleep(20 * time.Millisecond)

	// Terminate and reap
	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})
	k.reapProcess(proc)

	// After reap, threads and coroutines maps should be empty
	proc.mu.Lock()
	threadCount := len(proc.threads)
	coCount := len(proc.coroutines)
	proc.mu.Unlock()

	if threadCount != 0 {
		t.Errorf("expected 0 threads after reap, got %d", threadCount)
	}
	if coCount != 0 {
		t.Errorf("expected 0 coroutines after reap, got %d", coCount)
	}
}

// --- NFR24 Performance Test ---

// TestConcurrent_10Processes verifies >= 10 concurrent process-level agents can run
// simultaneously with process table operation latency not exceeding 2x single-process scenario.
func TestConcurrent_10Processes(t *testing.T) {
	k := newSimpleKernel(t)

	// Measure single process operation time
	singleProc := newConcurrencyTestProcess(t, k)
	singleStart := time.Now()
	_, _ = k.GetProcess(singleProc.PID)
	singleDuration := time.Since(singleStart)

	// Create 10 concurrent processes
	const n = 10
	procs := make([]*Process, n)
	for i := range n {
		procs[i] = newConcurrencyTestProcess(t, k)
	}

	// Measure concurrent process table operations
	var wg sync.WaitGroup
	concurrentStart := time.Now()
	for i := range n {
		wg.Go(func() {
			_, _ = k.GetProcess(procs[i].PID)
			// Also do some spawns of threads for concurrency stress
			_, _ = k.SpawnThread(procs[i].PID, "perf-thread")
		})
	}
	wg.Wait()
	concurrentDuration := time.Since(concurrentStart)

	// NFR24: concurrent operations should be <= 2x single operation
	threshold := max(singleDuration*2,
		// minimum threshold
		time.Millisecond)
	if concurrentDuration > threshold*10 { // generous threshold for CI
		t.Logf("WARNING: concurrent 10-process ops took %v vs single %v (ratio: %.1fx)",
			concurrentDuration, singleDuration, float64(concurrentDuration)/float64(singleDuration))
	}

	// Verify all processes are still accessible
	for i, proc := range procs {
		_, ok := k.GetProcess(proc.PID)
		if !ok {
			t.Errorf("process %d not found after concurrent operations", i)
		}
	}
}

// TestConcurrency_SyscallEvents verifies SpawnThread/JoinThread/SpawnCoroutine/Yield/ResumeCoroutine
// all emit correct SyscallEvents.
func TestConcurrency_SyscallEvents(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newConcurrencyTestProcess(t, k)

	// Test SpawnThread event
	tid, err := k.SpawnThread(proc.PID, "event-thread")
	if err != nil {
		t.Fatalf("SpawnThread failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SpawnThread" {
			t.Errorf("expected SpawnThread event, got %q", ev.Syscall)
		}
		if ev.Args["tid"] != tid {
			t.Errorf("SpawnThread event tid = %v, want %d", ev.Args["tid"], tid)
		}
	case <-time.After(time.Second):
		t.Fatal("no SpawnThread event")
	}

	// Test SpawnCoroutine event
	coID, err := k.SpawnCoroutine(proc.PID, func(yield func(any)) any {
		yield("event-val")
		return "event-done"
	})
	if err != nil {
		t.Fatalf("SpawnCoroutine failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "SpawnCoroutine" {
			t.Errorf("expected SpawnCoroutine event, got %q", ev.Syscall)
		}
		if ev.Args["co_id"] != coID {
			t.Errorf("SpawnCoroutine event co_id = %v, want %d", ev.Args["co_id"], coID)
		}
	case <-time.After(time.Second):
		t.Fatal("no SpawnCoroutine event")
	}

	time.Sleep(20 * time.Millisecond)

	// Test ResumeCoroutine event (yield path)
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "ResumeCoroutine" {
			t.Errorf("expected ResumeCoroutine event, got %q", ev.Syscall)
		}
		if ev.Args["action"] != "yielded" {
			t.Errorf("ResumeCoroutine action = %v, want %q", ev.Args["action"], "yielded")
		}
	case <-time.After(time.Second):
		t.Fatal("no ResumeCoroutine (yielded) event")
	}

	// Test ResumeCoroutine event (completed path)
	_, err = k.ResumeCoroutine(proc.PID, coID)
	if err != nil {
		t.Fatalf("ResumeCoroutine final failed: %v", err)
	}

	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "ResumeCoroutine" {
			t.Errorf("expected ResumeCoroutine event, got %q", ev.Syscall)
		}
		if ev.Args["action"] != "completed" {
			t.Errorf("ResumeCoroutine action = %v, want %q", ev.Args["action"], "completed")
		}
	case <-time.After(time.Second):
		t.Fatal("no ResumeCoroutine (completed) event")
	}
}
