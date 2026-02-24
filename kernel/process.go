package kernel

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// pidCounter is the global PID allocator. PIDs start at 1 and monotonically
// increase. They are never recycled.
var pidCounter atomic.Uint64

// nextPID returns the next unique PID.
func nextPID() types.PID {
	return types.PID(pidCounter.Add(1))
}

// ExitStatus records how a process terminated.
type ExitStatus struct {
	Code   int    // 0 = normal, non-zero = abnormal
	Reason string // human-readable reason
	Err    error  // underlying error, if any
}

// Process represents an agent process.
type Process struct {
	PID        types.PID
	PPID       types.PID
	State      types.ProcessState        // guarded by mu
	Intent     string                    // immutable after creation
	Skills     []string
	Children   []types.PID
	FDTable    map[types.FD]vfs.VFSFile  // per architecture doc; VFS manages actual FD state internally
	DebugChan  chan types.SyscallEvent
	Done       chan ExitStatus
	CreatedAt  time.Time
	Exit       *ExitStatus               // non-nil in Zombie/Dead
	CtxID      types.CtxID               // context allocated by Spawn
	Result     string                    // final output from reasoning
	TokensUsed int                       // cumulative token consumption

	mu     sync.Mutex
	cancel context.CancelFunc
	ctx    context.Context
	wg     sync.WaitGroup
}

// NewProcess creates a new process in the Created state with a unique PID.
func NewProcess(ppid types.PID, intent string, skills []string) *Process {
	return &Process{
		PID:       nextPID(),
		PPID:      ppid,
		State:     types.StateCreated,
		Intent:    intent,
		Skills:    skills,
		Children:  []types.PID{},
		FDTable:   make(map[types.FD]vfs.VFSFile),
		DebugChan: make(chan types.SyscallEvent, 256),
		Done:      make(chan ExitStatus, 1),
		CreatedAt: time.Now(),
	}
}

// validTransitions defines the legal state transitions.
var validTransitions = map[types.ProcessState][]types.ProcessState{
	types.StateCreated: {types.StateRunning},
	types.StateRunning: {types.StateZombie},
	types.StateZombie:  {types.StateDead},
	// StateDead has no valid transitions
}

// GetState returns the current process state in a thread-safe manner.
func (p *Process) GetState() types.ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State
}

// transitionLocked attempts to move the process to the target state.
// Caller must hold p.mu.
func (p *Process) transitionLocked(target types.ProcessState) error {
	if slices.Contains(validTransitions[p.State], target) {
		p.State = target
		return nil
	}

	return NewSyscallError(
		"transition",
		p.PID,
		"",
		fmt.Errorf("illegal transition: %d → %d", p.State, target),
		types.ErrInternal,
	)
}

// Transition attempts to move the process to the target state.
// Returns *SyscallError if the transition is illegal.
func (p *Process) Transition(target types.ProcessState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.transitionLocked(target)
}

// Start transitions the process from Created to Running.
func (p *Process) Start() error {
	return p.Transition(types.StateRunning)
}

// Terminate transitions the process from Running to Zombie and records the exit status.
func (p *Process) Terminate(exit ExitStatus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.transitionLocked(types.StateZombie); err != nil {
		return err
	}
	p.Exit = &exit
	return nil
}

// Reap transitions the process from Zombie to Dead.
func (p *Process) Reap() error {
	return p.Transition(types.StateDead)
}
