package kernel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Thread represents a lightweight execution unit that shares its parent process's context space.
type Thread struct {
	TID       types.TID
	ParentPID types.PID
	Intent    string
	State     types.ProcessState // reuses Created/Running/Zombie/Dead
	Done      chan struct{}      // closed when thread finishes
	Result    string
	Err       error

	mu     sync.Mutex
	cancel context.CancelFunc
	ctx    context.Context
}

// Start transitions the thread to Running state.
func (t *Thread) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.State = types.StateRunning
}

// Finish marks the thread as completed with the given result and error.
func (t *Thread) Finish(result string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Result = result
	t.Err = err
	t.State = types.StateDead
}

// SpawnThread creates a new thread within the parent process and launches a goroutine.
func (k *KernelImpl) SpawnThread(parentPID types.PID, intent string) (types.TID, error) {
	start := time.Now()

	proc, ok := k.GetProcess(parentPID)
	if !ok {
		return 0, NewSyscallError("SpawnThread", parentPID, "",
			fmt.Errorf("parent process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state != types.StateRunning {
		return 0, NewSyscallError("SpawnThread", parentPID, "",
			fmt.Errorf("parent process %d is %s, expected running", parentPID, state), types.ErrInvalid)
	}

	// Allocate TID (process-local)
	tid := types.TID(proc.tidCounter.Add(1))

	// Create thread with inherited context
	ctx, cancel := context.WithCancel(proc.ctx)
	thread := &Thread{
		TID:       tid,
		ParentPID: parentPID,
		Intent:    intent,
		State:     types.StateCreated,
		Done:      make(chan struct{}),
		cancel:    cancel,
		ctx:       ctx,
	}

	// Register thread
	proc.AddThread(thread)

	// Launch thread goroutine
	go func() {
		thread.Start()

		defer func() {
			thread.Finish("", nil)
			close(thread.Done)
		}()

		// Wait for context cancellation (thread lifecycle managed externally)
		<-ctx.Done()
	}()

	k.emitEvent(proc, "SpawnThread", map[string]any{
		"parent_pid": parentPID,
		"tid":        tid,
		"intent":     intent,
	}, tid, nil, time.Since(start))

	return tid, nil
}

// JoinThread waits for the specified thread to complete and cleans up its resources.
func (k *KernelImpl) JoinThread(parentPID types.PID, tid types.TID) error {
	start := time.Now()

	proc, ok := k.GetProcess(parentPID)
	if !ok {
		return NewSyscallError("JoinThread", parentPID, "",
			fmt.Errorf("parent process not found"), types.ErrNotFound)
	}

	thread, ok := proc.GetThread(tid)
	if !ok {
		return NewSyscallError("JoinThread", parentPID, "",
			fmt.Errorf("thread %d not found", tid), types.ErrNotFound)
	}

	// Wait for thread to finish
	<-thread.Done

	// Clean up
	proc.RemoveThread(tid)

	k.emitEvent(proc, "JoinThread", map[string]any{
		"parent_pid": parentPID,
		"tid":        tid,
		"action":     "completed",
	}, nil, nil, time.Since(start))

	return nil
}
