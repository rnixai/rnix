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
	AllowedDevices []string              // nil/empty = all devices allowed; non-empty = whitelist only

	groups []types.PGID               // guarded by mu, process group memberships

	// Signal system (mu protected)
	sigHandlers    map[types.Signal]SignalHandler
	blockedSignals map[types.Signal]struct{}
	pendingSignals map[types.Signal]struct{}
	resumeCh       chan struct{} // nil=not paused; non-nil=paused, close to resume

	mu       sync.Mutex
	cancel   context.CancelFunc
	ctx      context.Context
	wg       sync.WaitGroup
	reapOnce sync.Once // ensures reap executes at most once
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

// GetPID returns the process's own PID.
// This is the Crux equivalent of Unix getpid(2). Since PID is immutable after
// creation, no locking is required.
func (p *Process) GetPID() types.PID {
	return p.PID
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

// Cancel cancels the process context, signaling the reasoning goroutine to stop.
func (p *Process) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
}

// AddChild appends a child PID to the Children slice (thread-safe).
func (p *Process) AddChild(pid types.PID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Children = append(p.Children, pid)
}

// RemoveChild removes a child PID from the Children slice (thread-safe).
func (p *Process) RemoveChild(pid types.PID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.Children {
		if c == pid {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			return
		}
	}
}

// GetChildren returns a copy of the Children slice (thread-safe).
func (p *Process) GetChildren() []types.PID {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]types.PID, len(p.Children))
	copy(result, p.Children)
	return result
}

// AddGroup adds a process group ID to the process's group list (idempotent, thread-safe).
func (p *Process) AddGroup(pgid types.PGID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, g := range p.groups {
		if g == pgid {
			return
		}
	}
	p.groups = append(p.groups, pgid)
}

// RemoveGroup removes a process group ID from the process's group list (thread-safe).
func (p *Process) RemoveGroup(pgid types.PGID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, g := range p.groups {
		if g == pgid {
			p.groups = append(p.groups[:i], p.groups[i+1:]...)
			return
		}
	}
}

// GetGroups returns a copy of the process's group membership list (thread-safe).
func (p *Process) GetGroups() []types.PGID {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]types.PGID, len(p.groups))
	copy(result, p.groups)
	return result
}

// --- Signal state methods (all thread-safe via mu) ---

// SetHandler registers a custom signal handler for the given signal.
func (p *Process) SetHandler(sig types.Signal, handler SignalHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sigHandlers == nil {
		p.sigHandlers = make(map[types.Signal]SignalHandler)
	}
	p.sigHandlers[sig] = handler
}

// GetHandler returns the custom handler for the given signal, if any.
func (p *Process) GetHandler(sig types.Signal) (SignalHandler, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sigHandlers == nil {
		return nil, false
	}
	h, ok := p.sigHandlers[sig]
	return h, ok
}

// BlockSignal adds the signal to the blocked set.
func (p *Process) BlockSignal(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals == nil {
		p.blockedSignals = make(map[types.Signal]struct{})
	}
	p.blockedSignals[sig] = struct{}{}
}

// UnblockSignal removes the signal from the blocked set and returns whether
// there was a pending signal of this type.
func (p *Process) UnblockSignal(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals != nil {
		delete(p.blockedSignals, sig)
	}
	_, hasPending := p.pendingSignals[sig]
	return hasPending
}

// IsBlocked reports whether the signal is in the blocked set.
func (p *Process) IsBlocked(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals == nil {
		return false
	}
	_, ok := p.blockedSignals[sig]
	return ok
}

// AddPending adds the signal to the pending set.
func (p *Process) AddPending(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals == nil {
		p.pendingSignals = make(map[types.Signal]struct{})
	}
	p.pendingSignals[sig] = struct{}{}
}

// HasPending reports whether the signal is in the pending set.
func (p *Process) HasPending(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals == nil {
		return false
	}
	_, ok := p.pendingSignals[sig]
	return ok
}

// ClearPending removes the signal from the pending set.
func (p *Process) ClearPending(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals != nil {
		delete(p.pendingSignals, sig)
	}
}

// Pause creates a resumeCh channel, putting the process into paused state.
// Idempotent — if already paused, this is a no-op.
func (p *Process) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resumeCh == nil {
		p.resumeCh = make(chan struct{})
	}
}

// Resume closes the resumeCh channel, unblocking any goroutine waiting on WaitIfPaused.
// Idempotent — if not paused, this is a no-op.
func (p *Process) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resumeCh != nil {
		close(p.resumeCh)
		p.resumeCh = nil
	}
}

// WaitIfPaused returns the resume channel if the process is paused.
// Returns nil if not paused (caller should skip select).
func (p *Process) WaitIfPaused() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumeCh
}

// IsPaused reports whether the process is currently paused.
func (p *Process) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumeCh != nil
}

// ClearSignalState cleans up all signal state (handlers, blocked, pending, resume channel).
// Used during process reap.
func (p *Process) ClearSignalState() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sigHandlers = nil
	p.blockedSignals = nil
	p.pendingSignals = nil
	if p.resumeCh != nil {
		close(p.resumeCh)
		p.resumeCh = nil
	}
}
