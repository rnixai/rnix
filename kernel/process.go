package kernel

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
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
	PID            types.PID
	PPID           types.PID
	State          types.ProcessState // guarded by mu
	Intent         string             // immutable after creation
	Skills         []string
	Children       []types.PID
	FDTable        map[types.FD]vfs.VFSFile // per architecture doc; VFS manages actual FD state internally
	DebugChan      chan types.SyscallEvent
	LogChan        chan types.LogEntry
	Done           chan ExitStatus
	CreatedAt      time.Time
	DeadAt         time.Time   // set by reapProcess, used for TTL cleanup
	Exit           *ExitStatus // non-nil in Zombie/Dead
	CtxID          types.CtxID // context allocated by Spawn
	Result         string      // final output from reasoning
	TokensUsed     int         // cumulative token consumption
	ContextBudget  int         // 0 = no limit; >0 = terminate when TokensUsed >= ContextBudget
	AllowedDevices []string    // nil/empty = all devices allowed; non-empty = whitelist only
	MCPMounts      []string    // MCP mount paths auto-mounted by Spawn
	HasToolError   bool        // true if any tool call failed (mu protected)

	// Log history ring buffer (mu protected)
	logHistory []types.LogEntry
	logHistIdx int // ring buffer write position
	logHistLen int // current valid entry count

	groups []types.PGID // guarded by mu, process group memberships

	// Signal system (mu protected)
	sigHandlers    map[types.Signal]SignalHandler
	blockedSignals map[types.Signal]struct{}
	pendingSignals map[types.Signal]struct{}
	resumeCh       chan struct{} // nil=not paused; non-nil=paused, close to resume

	// Thread system (mu protected for threads map, atomic for counter)
	threads    map[types.TID]*Thread
	tidCounter atomic.Uint64

	// Coroutine system (mu protected for coroutines map, atomic for counter)
	coroutines  map[types.CoID]*Coroutine
	coIDCounter atomic.Uint64

	// GDB breakpoint system (mu protected)
	breakpoints []*Breakpoint
	gdbPauseCh  chan struct{} // nil=not paused; non-nil=paused, close to resume
	gdbStepMode StepMode     // current single-step mode (StepNone by default)

	mu       sync.Mutex
	cancel   context.CancelFunc
	ctx      context.Context
	wg       sync.WaitGroup
	reapOnce sync.Once // ensures reap executes at most once
}

// NewProcess creates a new process in the Created state with a unique PID.
func NewProcess(ppid types.PID, intent string, skills []string) *Process {
	ctx, cancel := context.WithCancel(context.Background())
	return &Process{
		PID:       nextPID(),
		PPID:      ppid,
		State:     types.StateCreated,
		Intent:    intent,
		Skills:    skills,
		Children:  []types.PID{},
		FDTable:   make(map[types.FD]vfs.VFSFile),
		DebugChan: make(chan types.SyscallEvent, 256),
		LogChan:   make(chan types.LogEntry, 256),
		Done:      make(chan ExitStatus, 1),
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
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
// This is the Rnix equivalent of Unix getpid(2). Since PID is immutable after
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

// IsCancelled returns true if the process context has been cancelled.
func (p *Process) IsCancelled() bool {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		return false
	}
	return ctx.Err() != nil
}

// CancelledCh returns a channel that is closed when the process context is cancelled.
// Returns nil if the process has no context.
func (p *Process) CancelledCh() <-chan struct{} {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		return nil
	}
	return ctx.Done()
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

// --- Signal disposition (atomic check under single lock) ---

// signalDisposition describes what should happen when a signal is delivered.
type signalDisposition int

const (
	dispBlocked signalDisposition = iota // signal is blocked, added to pending
	dispHandler                          // custom handler registered
	dispDefault                          // no handler, use default behavior
)

// resolveSignalDisposition atomically determines the disposition for a signal
// under a single lock hold. This prevents TOCTOU races between IsBlocked/AddPending
// and between GetHandler checks. SIGKILL always returns dispDefault (cannot be
// blocked or handled).
func (p *Process) resolveSignalDisposition(sig types.Signal) (signalDisposition, SignalHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if blocked (SIGKILL can never be blocked due to SigBlock validation)
	if p.blockedSignals != nil {
		if _, ok := p.blockedSignals[sig]; ok {
			if p.pendingSignals == nil {
				p.pendingSignals = make(map[types.Signal]struct{})
			}
			p.pendingSignals[sig] = struct{}{}
			return dispBlocked, nil
		}
	}

	// SIGKILL cannot have custom handler — force-kill semantics
	if !sig.Blockable() {
		return dispDefault, nil
	}

	// Check custom handler
	if p.sigHandlers != nil {
		if h, ok := p.sigHandlers[sig]; ok {
			return dispHandler, h
		}
	}

	return dispDefault, nil
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

const logHistoryCap = 256

// AppendLogHistory adds a log entry to the ring buffer. Caller must hold p.mu.
func (p *Process) AppendLogHistory(entry types.LogEntry) {
	if p.logHistory == nil {
		p.logHistory = make([]types.LogEntry, logHistoryCap)
	}
	p.logHistory[p.logHistIdx] = entry
	p.logHistIdx = (p.logHistIdx + 1) % logHistoryCap
	if p.logHistLen < logHistoryCap {
		p.logHistLen++
	}
}

// GetLogHistory returns a time-ordered copy of the log history. Caller must hold p.mu.
func (p *Process) GetLogHistory() []types.LogEntry {
	if p.logHistLen == 0 {
		return nil
	}
	result := make([]types.LogEntry, p.logHistLen)
	if p.logHistLen < logHistoryCap {
		copy(result, p.logHistory[:p.logHistLen])
	} else {
		start := p.logHistIdx // oldest entry
		n := copy(result, p.logHistory[start:])
		copy(result[n:], p.logHistory[:start])
	}
	return result
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

// --- Thread management methods (all thread-safe via mu) ---

// AddThread registers a thread in the process's thread table.
func (p *Process) AddThread(t *Thread) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads == nil {
		p.threads = make(map[types.TID]*Thread)
	}
	p.threads[t.TID] = t
}

// GetThread retrieves a thread by TID from the process's thread table.
func (p *Process) GetThread(tid types.TID) (*Thread, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads == nil {
		return nil, false
	}
	t, ok := p.threads[tid]
	return t, ok
}

// RemoveThread removes a thread from the process's thread table.
func (p *Process) RemoveThread(tid types.TID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads != nil {
		delete(p.threads, tid)
	}
}

// ClearThreads cancels all threads and waits for them to finish.
// Used during process reap.
func (p *Process) ClearThreads() {
	p.mu.Lock()
	threads := make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		threads = append(threads, t)
	}
	p.threads = nil
	p.mu.Unlock()

	for _, t := range threads {
		t.cancel()
		<-t.Done
	}
}

// --- Coroutine management methods (all thread-safe via mu) ---

// AddCoroutine registers a coroutine in the process's coroutine table.
func (p *Process) AddCoroutine(c *Coroutine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines == nil {
		p.coroutines = make(map[types.CoID]*Coroutine)
	}
	p.coroutines[c.CoID] = c
}

// GetCoroutine retrieves a coroutine by CoID from the process's coroutine table.
func (p *Process) GetCoroutine(coID types.CoID) (*Coroutine, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines == nil {
		return nil, false
	}
	c, ok := p.coroutines[coID]
	return c, ok
}

// RemoveCoroutine removes a coroutine from the process's coroutine table.
func (p *Process) RemoveCoroutine(coID types.CoID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines != nil {
		delete(p.coroutines, coID)
	}
}

// ClearCoroutines cleans up all coroutines by closing their resume channels.
// Used during process reap.
//
// Coroutine goroutines can be blocked at two points:
//  1. co.yieldCh <- value (inside yield closure, waiting for caller to read)
//  2. <-co.resumeCh (inside yield closure, waiting for caller to resume)
//
// Closing resumeCh unblocks case 2. For case 1, the goroutine will panic
// on send to closed channel if we close yieldCh, so we use a drain goroutine
// to consume from yieldCh, which unblocks the sender, then the coroutine
// proceeds to <-resumeCh which returns zero value (closed channel).
func (p *Process) ClearCoroutines() {
	p.mu.Lock()
	coroutines := make([]*Coroutine, 0, len(p.coroutines))
	for _, c := range p.coroutines {
		coroutines = append(coroutines, c)
	}
	p.coroutines = nil
	p.mu.Unlock()

	for _, c := range coroutines {
		c.mu.Lock()
		if c.State != coDone {
			// Close resumeCh to unblock coroutines waiting at <-co.resumeCh.
			// This also causes yield closure to return (resume receives zero value),
			// then the coroutine continues and eventually completes or blocks at
			// the next yieldCh send, which the drain goroutine handles.
			close(c.resumeCh)

			// Drain yieldCh to unblock coroutines blocked at co.yieldCh <- value.
			// The goroutine exits when yieldCh is closed (coroutine completes).
			go func(ch chan any) {
				for range ch {
				}
			}(c.yieldCh)
		}
		c.mu.Unlock()
	}
}
