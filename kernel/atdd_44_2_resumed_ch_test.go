package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.2 — Task 1: Process.ResumedCh()
//
// Drives Story 44.2 AC#3 / AC#4 requirements for a per-process "resumed"
// channel that ScriptExecutor.SuspendController can wait on. RED phase:
// `Process.ResumedCh()` does not yet exist — every test below fails to
// compile until Task 1.1-1.5 lands.
//
// Spec (from 44.2 §AC3 / §Tasks/Task 1):
//
//   - NewProcess() initialises an internal `resumedCh chan struct{}` that
//     stays open while the process is NOT in a Suspend cycle (semantics:
//     "the channel is closed iff the most recent Suspend has been undone").
//
//   - On Suspend() the channel stays untouched (the value already returned
//     by a prior ResumedCh() must NOT close — readers will block on the
//     same chan).
//
//   - On Unsuspend(): after the Transition(Running) succeeds AND after
//     suspendRequested.Store(false), the prior chan is closed (one-shot,
//     guarded by sync.Once) and a NEW unclosed chan is installed for the
//     next Suspend cycle.
//
//   - ResumedCh() returns the CURRENT chan under proc.mu — across multiple
//     Suspend-Resume cycles, callers must re-fetch.
//
//   - Concurrent multiple readers (parallel block worker goroutines) are
//     supported — all observe the same close.
// =============================================================================

// TestATDD_44_2_010_ResumedCh_NotifiesOnUnsuspend
//
// Story 44.2 AC#3 / AC#4: while Suspended, ResumedCh() returns an open
// channel; after Unsuspend the channel must close so the script executor's
// `select { case <-ch: }` wakes up.
func TestATDD_44_2_010_ResumedCh_NotifiesOnUnsuspend(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := proc.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	ch := proc.ResumedCh()
	if ch == nil {
		t.Fatal("ResumedCh() returned nil; want an open channel during Suspend cycle")
	}
	select {
	case <-ch:
		t.Fatal("ResumedCh() chan should NOT be closed while Suspended; got close before Unsuspend")
	default:
	}

	if err := proc.Unsuspend(); err != nil {
		t.Fatalf("Unsuspend: %v", err)
	}

	select {
	case <-ch:
		// expected: chan closed after Unsuspend
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ResumedCh() chan was not closed within 100ms after Unsuspend")
	}
}

// TestATDD_44_2_011_ResumedCh_MultipleSuspendResumeCycles
//
// Story 44.2 §Dev Notes "ResumedCh 多次 Suspend-Resume 循环的并发安全":
// each Suspend cycle must hand out a FRESH channel; the closed channel
// from cycle N must not leak into cycle N+1.
func TestATDD_44_2_011_ResumedCh_MultipleSuspendResumeCycles(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for cycle := 1; cycle <= 3; cycle++ {
		if err := proc.Suspend(); err != nil {
			t.Fatalf("cycle %d: Suspend: %v", cycle, err)
		}
		ch := proc.ResumedCh()
		if ch == nil {
			t.Fatalf("cycle %d: ResumedCh() returned nil", cycle)
		}
		select {
		case <-ch:
			t.Fatalf("cycle %d: chan must be open at start of Suspend cycle", cycle)
		default:
		}

		if err := proc.Unsuspend(); err != nil {
			t.Fatalf("cycle %d: Unsuspend: %v", cycle, err)
		}
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("cycle %d: chan not closed after Unsuspend", cycle)
		}

		// Next cycle's ResumedCh must return a DIFFERENT chan instance.
		if cycle < 3 {
			if err := proc.Suspend(); err != nil {
				t.Fatalf("cycle %d: pre-next Suspend: %v", cycle, err)
			}
			next := proc.ResumedCh()
			if next == ch {
				t.Fatalf("cycle %d: ResumedCh() returned the same closed chan — must rotate per Suspend cycle", cycle)
			}
			select {
			case <-next:
				t.Fatalf("cycle %d: freshly rotated chan should be open", cycle)
			default:
			}
			ch = next
			// The Unsuspend at the top of the next loop iteration will close
			// this one — but we Unsuspend here so the loop's Suspend can run.
			if err := proc.Unsuspend(); err != nil {
				t.Fatalf("cycle %d: cleanup Unsuspend: %v", cycle, err)
			}
		}
	}
}

// TestATDD_44_2_012_ResumedCh_ConcurrentReaders
//
// Story 44.2 §AC3 SuspendController doc: "implementations must be safe for
// concurrent ResumedCh() calls (multiple goroutines may observe the same
// suspend, e.g. parallel block)". All parallel readers must wake on a
// single Unsuspend.
func TestATDD_44_2_012_ResumedCh_ConcurrentReaders(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := proc.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	const readers = 5
	var wg sync.WaitGroup
	wg.Add(readers)
	woke := make(chan int, readers)
	for i := 0; i < readers; i++ {
		go func(id int) {
			defer wg.Done()
			ch := proc.ResumedCh()
			select {
			case <-ch:
				woke <- id
			case <-time.After(500 * time.Millisecond):
			}
		}(i)
	}

	// give the readers a head start
	time.Sleep(20 * time.Millisecond)
	if err := proc.Unsuspend(); err != nil {
		t.Fatalf("Unsuspend: %v", err)
	}

	wg.Wait()
	close(woke)
	count := 0
	for range woke {
		count++
	}
	if count != readers {
		t.Errorf("woken readers = %d, want %d (all concurrent ResumedCh() readers must observe the close)", count, readers)
	}
}

// TestATDD_44_2_013_ResumedCh_FollowsRunningStateAfterUnsuspend
//
// Order invariant from §Dev Notes "ResumedCh 多次 Suspend-Resume 循环的并发安全"
// item 4: "新的 ResumedCh 安装 + 旧 chan close" must happen AFTER
// suspendRequested.Store(false) so a waiter that wakes and re-checks
// IsSuspendRequested sees false (no infinite loop).
func TestATDD_44_2_013_ResumedCh_FollowsRunningStateAfterUnsuspend(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := proc.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	ch := proc.ResumedCh()

	done := make(chan struct {
		suspendRequested bool
		state            types.ProcessState
	}, 1)
	go func() {
		<-ch
		// At the instant we wake, the production contract is:
		//   suspendRequested == false  AND  state == Running
		done <- struct {
			suspendRequested bool
			state            types.ProcessState
		}{proc.IsSuspendRequested(), proc.GetState()}
	}()

	time.Sleep(10 * time.Millisecond)
	if err := proc.Unsuspend(); err != nil {
		t.Fatalf("Unsuspend: %v", err)
	}

	select {
	case got := <-done:
		if got.suspendRequested {
			t.Error("after ResumedCh close, IsSuspendRequested() must be false — got true (ordering violation)")
		}
		if got.state != types.StateRunning {
			t.Errorf("after ResumedCh close, state = %s, want Running", got.state)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waiter did not wake within 500ms of Unsuspend")
	}
}

// TestATDD_44_2_014_ResumedCh_NoDoubleClose
//
// Defence against double-close panic if dev-story's Unsuspend somehow runs
// twice on the same cycle (e.g. retry path). The sync.Once guard in Task 1.4
// must hold — calling Unsuspend twice should not panic.
func TestATDD_44_2_014_ResumedCh_NoDoubleClose(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := proc.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	if err := proc.Unsuspend(); err != nil {
		t.Fatalf("first Unsuspend: %v", err)
	}
	// Second Unsuspend should fail (state == Running already) but MUST NOT
	// panic — the production sync.Once guard is the safety net.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second Unsuspend panicked (double-close of resumedCh): %v", r)
		}
	}()
	_ = proc.Unsuspend() // expected to return illegal-transition error, not panic
}
