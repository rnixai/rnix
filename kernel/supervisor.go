package kernel

import (
	"fmt"
	"sync"
	"time"

	gocontext "context"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
)

// RestartStrategy defines how a Supervisor responds to child failures.
type RestartStrategy string

const (
	OneForOne  RestartStrategy = "one_for_one"
	OneForAll  RestartStrategy = "one_for_all"
	RestForOne RestartStrategy = "rest_for_one"
)

// ChildRestart defines the restart policy for individual children.
type ChildRestart string

const (
	RestartPermanent ChildRestart = "permanent"
	RestartTransient ChildRestart = "transient"
	RestartTemporary ChildRestart = "temporary"
)

// ChildSpec describes a child process to be supervised.
type ChildSpec struct {
	Name          string
	Intent        string
	Agent         *agents.AgentInfo
	Model         string
	Provider      string
	ContextBudget int
	Restart       ChildRestart
}

// SupervisorSpec configures a Supervisor's behavior.
type SupervisorSpec struct {
	Strategy    RestartStrategy
	MaxRestarts int
	MaxWindow   time.Duration
	Children    []ChildSpec
}

// childExit carries information about a child process exiting.
type childExit struct {
	index int
	pid   types.PID // identifies which generation of child sent this event
	exit  ExitStatus
}

// supervisedChild tracks a running child within the Supervisor.
type supervisedChild struct {
	spec  ChildSpec
	pid   types.PID
	index int
	alive bool
}

// Supervisor manages a set of child processes with automatic restart.
type Supervisor struct {
	proc         *Process
	spec         SupervisorSpec
	kernel       *KernelImpl
	children     []*supervisedChild
	exitCh       chan childExit
	restartTimes []time.Time
	mu           sync.Mutex
}

// newSupervisor creates a new Supervisor instance.
func newSupervisor(proc *Process, spec SupervisorSpec, k *KernelImpl) *Supervisor {
	return &Supervisor{
		proc:     proc,
		spec:     spec,
		kernel:   k,
		children: make([]*supervisedChild, len(spec.Children)),
		exitCh:   make(chan childExit, len(spec.Children)),
	}
}

// SupervisorManager defines the interface for spawning Supervisor processes.
type SupervisorManager interface {
	SpawnSupervisor(spec SupervisorSpec) (types.PID, error)
}

// Compile-time interface compliance check.
var _ SupervisorManager = (*KernelImpl)(nil)

// SpawnSupervisor creates a Supervisor process that manages child processes.
func (k *KernelImpl) SpawnSupervisor(spec SupervisorSpec) (types.PID, error) {
	start := time.Now()

	if len(spec.Children) == 0 {
		return 0, NewSyscallError("SpawnSupervisor", 0, "",
			fmt.Errorf("supervisor must have at least one child spec"), types.ErrInvalid)
	}
	if spec.MaxRestarts <= 0 {
		spec.MaxRestarts = 3
	}
	if spec.MaxWindow <= 0 {
		spec.MaxWindow = 60 * time.Second
	}
	for i, cs := range spec.Children {
		if cs.Restart == "" {
			spec.Children[i].Restart = RestartPermanent
		}
	}

	proc := NewProcess(0, "supervisor:"+string(spec.Strategy), nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = ctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	sup := newSupervisor(proc, spec, k)

	k.emitEvent(proc, "SpawnSupervisor", map[string]any{
		"strategy":     string(spec.Strategy),
		"max_restarts": spec.MaxRestarts,
		"max_window":   spec.MaxWindow.String(),
		"children":     len(spec.Children),
	}, proc.PID, nil, time.Since(start))

	proc.wg.Go(func() {
		_ = proc.Start()
		sup.run()
	})

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent)
	}

	return proc.PID, nil
}

// run is the Supervisor's main loop: starts children, then monitors.
func (s *Supervisor) run() {
	// Phase 1: Start all children in order
	for i, childSpec := range s.spec.Children {
		pid, err := s.startChild(i, childSpec)
		if err != nil {
			s.shutdownStarted(i)
			s.kernel.finishProcess(s.proc, ExitStatus{
				Code:   1,
				Reason: fmt.Sprintf("child %q start failed: %v", childSpec.Name, err),
				Err:    err,
			})
			return
		}
		s.children[i] = &supervisedChild{
			spec:  childSpec,
			pid:   pid,
			index: i,
			alive: true,
		}
	}

	// Phase 2: Monitor loop
	for {
		select {
		case ce := <-s.exitCh:
			if s.proc.GetState() != types.StateRunning {
				return
			}
			s.handleChildExit(ce.index, ce.pid, ce.exit)
			if s.proc.GetState() != types.StateRunning {
				return
			}
			if s.allDone() {
				s.kernel.finishProcess(s.proc, ExitStatus{
					Code: 0, Reason: "all children completed",
				})
				return
			}
		case <-s.proc.ctx.Done():
			s.shutdownAll()
			s.kernel.finishProcess(s.proc, ExitStatus{
				Code: 0, Reason: "supervisor cancelled",
			})
			return
		}
	}
}

// startChild spawns a single child process and starts a monitor goroutine.
func (s *Supervisor) startChild(idx int, spec ChildSpec) (types.PID, error) {
	opts := SpawnOpts{
		ParentPID:     s.proc.PID,
		Model:         spec.Model,
		Provider:      spec.Provider,
		ContextBudget: spec.ContextBudget,
	}
	pid, err := s.kernel.Spawn(spec.Intent, spec.Agent, opts)
	if err != nil {
		return 0, err
	}

	s.kernel.emitEvent(s.proc, "SupervisorStartChild", map[string]any{
		"child_name":  spec.Name,
		"child_pid":   pid,
		"child_index": idx,
	}, pid, nil, 0)

	childProc, _ := s.kernel.GetProcess(pid)
	go func(index int, childPID types.PID, done <-chan ExitStatus) {
		exit := <-done
		s.exitCh <- childExit{index: index, pid: childPID, exit: exit}
	}(idx, pid, childProc.Done)

	return pid, nil
}

// stopChild kills and reaps a single child process.
// Uses wg.Wait() instead of <-Done to avoid races with the monitor goroutine
// that also reads from Done.
func (s *Supervisor) stopChild(idx int) {
	child := s.children[idx]
	if child == nil || !child.alive {
		return
	}
	child.alive = false

	childProc, ok := s.kernel.GetProcess(child.pid)
	if !ok {
		return
	}

	state := childProc.GetState()
	if state != types.StateZombie && state != types.StateDead {
		_ = s.kernel.Kill(child.pid, types.SIGKILL)
	}

	// Wait for the process goroutine to finish (with timeout).
	done := make(chan struct{})
	go func() {
		childProc.wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}

	s.kernel.Reap(child.pid)
}

// shutdownAll stops all alive children in reverse startup order.
func (s *Supervisor) shutdownAll() {
	for i := len(s.children) - 1; i >= 0; i-- {
		s.stopChild(i)
	}
}

// shutdownStarted rolls back children that were already started during Phase 1.
func (s *Supervisor) shutdownStarted(failedIdx int) {
	for i := failedIdx - 1; i >= 0; i-- {
		s.stopChild(i)
	}
}

// allDone returns true if no children are alive and none need restarting.
func (s *Supervisor) allDone() bool {
	for _, child := range s.children {
		if child != nil && child.alive {
			return false
		}
	}
	return true
}

// handleChildExit processes a child exit event.
// The pid parameter filters stale events from pre-restart monitor goroutines:
// after one_for_all/rest_for_one restart, old monitor goroutines may still send
// exit events with the same index but a different (old) pid.
func (s *Supervisor) handleChildExit(idx int, pid types.PID, exit ExitStatus) {
	child := s.children[idx]
	if child == nil || !child.alive {
		return
	}
	// Filter stale events: if the PID doesn't match the current child at this
	// index, this event came from a pre-restart monitor goroutine — ignore it.
	if child.pid != pid {
		return
	}
	child.alive = false

	s.kernel.emitEvent(s.proc, "SupervisorChildExit", map[string]any{
		"child_name":  child.spec.Name,
		"child_pid":   child.pid,
		"child_index": idx,
		"exit_code":   exit.Code,
		"exit_reason": exit.Reason,
	}, nil, exit.Err, 0)

	if !s.shouldRestart(child.spec, exit) {
		s.kernel.Reap(child.pid)
		return
	}

	s.recordRestart()
	if s.exceedsRestartLimit() {
		s.kernel.emitEvent(s.proc, "SupervisorShutdown", map[string]any{
			"reason":       "max_restarts_exceeded",
			"max_restarts": s.spec.MaxRestarts,
			"max_window":   s.spec.MaxWindow.String(),
		}, nil, nil, 0)
		s.shutdownAll()
		s.kernel.finishProcess(s.proc, ExitStatus{
			Code:   1,
			Reason: "max_restarts_exceeded",
			Err: fmt.Errorf("supervisor restart limit exceeded: %d restarts in %s",
				s.spec.MaxRestarts, s.spec.MaxWindow),
		})
		return
	}

	s.kernel.emitEvent(s.proc, "SupervisorRestart", map[string]any{
		"strategy":      string(s.spec.Strategy),
		"child_name":    child.spec.Name,
		"child_index":   idx,
		"restart_count": len(s.restartTimes),
	}, nil, nil, 0)

	var err error
	switch s.spec.Strategy {
	case OneForOne:
		err = s.restartOneForOne(idx)
	case OneForAll:
		err = s.restartOneForAll(idx)
	case RestForOne:
		err = s.restartRestForOne(idx)
	}

	if err != nil {
		s.kernel.finishProcess(s.proc, ExitStatus{
			Code:   1,
			Reason: fmt.Sprintf("restart failed: %v", err),
			Err:    err,
		})
	}
}

// shouldRestart determines if a child should be restarted based on its restart policy.
func (s *Supervisor) shouldRestart(spec ChildSpec, exit ExitStatus) bool {
	switch spec.Restart {
	case RestartPermanent:
		return true
	case RestartTransient:
		return exit.Code != 0
	case RestartTemporary:
		return false
	default:
		return true
	}
}

// restartOneForOne restarts only the crashed child.
func (s *Supervisor) restartOneForOne(idx int) error {
	child := s.children[idx]
	s.kernel.Reap(child.pid)

	pid, err := s.startChild(idx, child.spec)
	if err != nil {
		return err
	}
	s.children[idx] = &supervisedChild{
		spec:  child.spec,
		pid:   pid,
		index: idx,
		alive: true,
	}
	return nil
}

// restartOneForAll stops all children in reverse order, then restarts all.
func (s *Supervisor) restartOneForAll(crashedIdx int) error {
	// Stop all alive children in reverse order (crashed one is already dead)
	for i := len(s.children) - 1; i >= 0; i-- {
		if i != crashedIdx {
			s.stopChild(i)
		}
	}
	// Reap the crashed child
	s.kernel.Reap(s.children[crashedIdx].pid)

	// Restart all children in order
	for i, child := range s.children {
		pid, err := s.startChild(i, child.spec)
		if err != nil {
			s.shutdownStarted(i)
			return err
		}
		s.children[i] = &supervisedChild{
			spec:  child.spec,
			pid:   pid,
			index: i,
			alive: true,
		}
	}
	return nil
}

// restartRestForOne stops children after the crashed one in reverse order,
// then restarts the crashed one and all subsequent children.
func (s *Supervisor) restartRestForOne(crashedIdx int) error {
	// Stop children after crashedIdx in reverse order
	for i := len(s.children) - 1; i > crashedIdx; i-- {
		s.stopChild(i)
	}
	// Reap the crashed child
	s.kernel.Reap(s.children[crashedIdx].pid)

	// Restart crashedIdx and all subsequent children
	for i := crashedIdx; i < len(s.children); i++ {
		child := s.children[i]
		pid, err := s.startChild(i, child.spec)
		if err != nil {
			s.shutdownStarted(i)
			return err
		}
		s.children[i] = &supervisedChild{
			spec:  child.spec,
			pid:   pid,
			index: i,
			alive: true,
		}
	}
	return nil
}

// recordRestart appends the current time to the restart history.
func (s *Supervisor) recordRestart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restartTimes = append(s.restartTimes, time.Now())
}

// exceedsRestartLimit checks if the number of restarts within the sliding window
// exceeds MaxRestarts. Prunes expired entries to prevent unbounded growth.
func (s *Supervisor) exceedsRestartLimit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-s.spec.MaxWindow)
	valid := s.restartTimes[:0]
	for _, t := range s.restartTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	s.restartTimes = valid
	return len(valid) >= s.spec.MaxRestarts
}
