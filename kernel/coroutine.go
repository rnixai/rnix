package kernel

import (
	"fmt"
	"sync"
	"time"

	"github.com/gonewx/crux/internal/types"
)

type coroutineState int

const (
	coReady coroutineState = iota
	coRunning
	coSuspended
	coDone
)

// Coroutine represents a cooperatively scheduled execution unit within a process.
type Coroutine struct {
	CoID      types.CoID
	ParentPID types.PID
	State     coroutineState
	yieldCh   chan any      // coroutine -> caller (yield value)
	resumeCh  chan struct{} // caller -> coroutine (resume signal)
	result    any
	consumed  bool // true after caller has read the yield value but hasn't resumed yet
	mu        sync.Mutex
}

// SpawnCoroutine creates a new coroutine within the parent process.
func (k *KernelImpl) SpawnCoroutine(parentPID types.PID, fn CoroutineFunc) (types.CoID, error) {
	start := time.Now()

	proc, ok := k.GetProcess(parentPID)
	if !ok {
		return 0, NewSyscallError("SpawnCoroutine", parentPID, "",
			fmt.Errorf("parent process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state != types.StateRunning {
		return 0, NewSyscallError("SpawnCoroutine", parentPID, "",
			fmt.Errorf("parent process %d is %s, expected running", parentPID, state), types.ErrInvalid)
	}

	coID := types.CoID(proc.coIDCounter.Add(1))
	co := &Coroutine{
		CoID:      coID,
		ParentPID: parentPID,
		State:     coReady,
		yieldCh:   make(chan any),
		resumeCh:  make(chan struct{}),
	}

	proc.AddCoroutine(co)

	// Launch coroutine goroutine
	go func() {
		co.mu.Lock()
		co.State = coRunning
		co.mu.Unlock()

		// yield function passed to CoroutineFunc
		yield := func(value any) {
			co.mu.Lock()
			co.State = coSuspended
			co.mu.Unlock()

			co.yieldCh <- value // blocks until caller reads (ResumeCoroutine)
			<-co.resumeCh       // blocks until caller sends resume signal

			co.mu.Lock()
			co.State = coRunning
			co.mu.Unlock()
		}

		result := fn(yield)

		co.mu.Lock()
		co.result = result
		co.State = coDone
		co.mu.Unlock()

		close(co.yieldCh) // signal completion
	}()

	k.emitEvent(proc, "SpawnCoroutine", map[string]any{
		"parent_pid": parentPID,
		"co_id":      coID,
	}, coID, nil, time.Since(start))

	return coID, nil
}

// Yield is called externally to yield a value from a coroutine.
// Note: In practice, the coroutine yields via the yield function passed to CoroutineFunc.
// This method is provided for the ConcurrencyManager interface.
func (k *KernelImpl) Yield(parentPID types.PID, coID types.CoID, value any) error {
	start := time.Now()

	proc, ok := k.GetProcess(parentPID)
	if !ok {
		return NewSyscallError("Yield", parentPID, "",
			fmt.Errorf("parent process not found"), types.ErrNotFound)
	}

	co, ok := proc.GetCoroutine(coID)
	if !ok {
		return NewSyscallError("Yield", parentPID, "",
			fmt.Errorf("coroutine %d not found", coID), types.ErrNotFound)
	}

	co.mu.Lock()
	if co.State != coRunning {
		coState := co.State
		co.mu.Unlock()
		return NewSyscallError("Yield", parentPID, "",
			fmt.Errorf("coroutine %d is not running (state=%d)", coID, coState), types.ErrInvalid)
	}
	co.State = coSuspended
	co.mu.Unlock()

	co.yieldCh <- value

	k.emitEvent(proc, "Yield", map[string]any{
		"parent_pid": parentPID,
		"co_id":      coID,
	}, value, nil, time.Since(start))

	return nil
}

// ResumeCoroutine resumes a suspended coroutine and returns the next yielded value or final result.
//
// Protocol:
//   - First call after SpawnCoroutine: reads the initial yield value from yieldCh
//   - Subsequent calls: sends resume signal first (unblocking the coroutine), then reads next yield/completion
//   - When coroutine completes: detects closed yieldCh and returns the final result
func (k *KernelImpl) ResumeCoroutine(parentPID types.PID, coID types.CoID) (any, error) {
	start := time.Now()

	proc, ok := k.GetProcess(parentPID)
	if !ok {
		return nil, NewSyscallError("ResumeCoroutine", parentPID, "",
			fmt.Errorf("parent process not found"), types.ErrNotFound)
	}

	co, ok := proc.GetCoroutine(coID)
	if !ok {
		return nil, NewSyscallError("ResumeCoroutine", parentPID, "",
			fmt.Errorf("coroutine %d not found", coID), types.ErrNotFound)
	}

	co.mu.Lock()
	coState := co.State
	consumed := co.consumed
	co.mu.Unlock()

	if coState == coDone {
		return nil, NewSyscallError("ResumeCoroutine", parentPID, "",
			fmt.Errorf("coroutine %d is already done", coID), types.ErrInvalid)
	}

	if coState != coSuspended {
		return nil, NewSyscallError("ResumeCoroutine", parentPID, "",
			fmt.Errorf("coroutine %d is not suspended (state=%d)", coID, coState), types.ErrInvalid)
	}

	// If the previous yield value was already consumed, resume the coroutine first
	// so it can run to the next yield point or completion.
	if consumed {
		co.resumeCh <- struct{}{} // unblock coroutine's <-resumeCh

		// Wait for next yield or completion
		value, ok := <-co.yieldCh
		if !ok {
			// Channel closed = coroutine completed
			co.mu.Lock()
			result := co.result
			co.mu.Unlock()

			proc.RemoveCoroutine(coID)

			k.emitEvent(proc, "ResumeCoroutine", map[string]any{
				"parent_pid": parentPID,
				"co_id":      coID,
				"action":     "completed",
			}, result, nil, time.Since(start))

			return result, nil
		}

		// Got next yield value
		co.mu.Lock()
		co.consumed = true
		co.mu.Unlock()

		k.emitEvent(proc, "ResumeCoroutine", map[string]any{
			"parent_pid": parentPID,
			"co_id":      coID,
			"action":     "yielded",
		}, value, nil, time.Since(start))

		return value, nil
	}

	// First time: just read the yield value (coroutine is blocked at yieldCh send)
	value, ok := <-co.yieldCh
	if !ok {
		// Shouldn't happen if state was Suspended, but handle gracefully
		co.mu.Lock()
		result := co.result
		co.mu.Unlock()

		proc.RemoveCoroutine(coID)

		k.emitEvent(proc, "ResumeCoroutine", map[string]any{
			"parent_pid": parentPID,
			"co_id":      coID,
			"action":     "completed",
		}, result, nil, time.Since(start))

		return result, nil
	}

	// Mark as consumed
	co.mu.Lock()
	co.consumed = true
	co.mu.Unlock()

	k.emitEvent(proc, "ResumeCoroutine", map[string]any{
		"parent_pid": parentPID,
		"co_id":      coID,
		"action":     "yielded",
	}, value, nil, time.Since(start))

	return value, nil
}
