package kernel

import (
	"fmt"
	"sync"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// ProcGroupManager manages process groups and batch signaling.
type ProcGroupManager interface {
	JoinGroup(pid types.PID, groupID types.PGID) error
	LeaveGroup(pid types.PID, groupID types.PGID) error
	GetProcGroup(groupID types.PGID) ([]types.PID, error)
	SignalGroup(groupID types.PGID, signal types.Signal) error
}

// Compile-time interface compliance check.
var _ ProcGroupManager = (*KernelImpl)(nil)

// ProcGroup is the internal representation of a process group.
type ProcGroup struct {
	mu      sync.RWMutex
	id      types.PGID
	members map[types.PID]struct{}
}

func newProcGroup(id types.PGID) *ProcGroup {
	return &ProcGroup{
		id:      id,
		members: make(map[types.PID]struct{}),
	}
}

func (g *ProcGroup) Add(pid types.PID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.members[pid] = struct{}{}
}

func (g *ProcGroup) Remove(pid types.PID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, existed := g.members[pid]
	delete(g.members, pid)
	return existed
}

func (g *ProcGroup) Members() []types.PID {
	g.mu.RLock()
	defer g.mu.RUnlock()
	pids := make([]types.PID, 0, len(g.members))
	for pid := range g.members {
		pids = append(pids, pid)
	}
	return pids
}

func (g *ProcGroup) Size() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.members)
}

func (g *ProcGroup) Contains(pid types.PID) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.members[pid]
	return ok
}

// JoinGroup adds a process to a process group. The group is auto-created if it doesn't exist.
func (k *KernelImpl) JoinGroup(pid types.PID, groupID types.PGID) error {
	start := time.Now()

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("JoinGroup", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}
	if state := proc.GetState(); state == types.StateZombie || state == types.StateDead {
		return NewSyscallError("JoinGroup", pid, "",
			fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
	}

	group, _ := k.procGroups.LoadOrStore(groupID, newProcGroup(groupID))
	group.Add(pid)
	proc.AddGroup(groupID)

	k.emitEvent(proc, "JoinGroup", map[string]any{
		"pid":      pid,
		"group_id": groupID,
	}, nil, nil, time.Since(start))

	return nil
}

// LeaveGroup removes a process from a process group. The group is auto-destroyed if empty.
func (k *KernelImpl) LeaveGroup(pid types.PID, groupID types.PGID) error {
	start := time.Now()

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("LeaveGroup", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	group, ok := k.procGroups.Load(groupID)
	if !ok {
		return NewSyscallError("LeaveGroup", pid, "",
			fmt.Errorf("group %d not found", groupID), types.ErrNotFound)
	}

	if !group.Remove(pid) {
		return NewSyscallError("LeaveGroup", pid, "",
			fmt.Errorf("process %d not in group %d", pid, groupID), types.ErrNotFound)
	}

	proc.RemoveGroup(groupID)

	if group.Size() == 0 {
		k.procGroups.Delete(groupID)
	}

	k.emitEvent(proc, "LeaveGroup", map[string]any{
		"pid":      pid,
		"group_id": groupID,
	}, nil, nil, time.Since(start))

	return nil
}

// GetProcGroup returns the list of PIDs in a process group.
func (k *KernelImpl) GetProcGroup(groupID types.PGID) ([]types.PID, error) {
	start := time.Now()

	group, ok := k.procGroups.Load(groupID)
	if !ok {
		return nil, NewSyscallError("GetProcGroup", 0, "",
			fmt.Errorf("group %d not found", groupID), types.ErrNotFound)
	}

	members := group.Members()

	// Emit event using the first member if available
	if len(members) > 0 {
		if proc, ok := k.GetProcess(members[0]); ok {
			k.emitEvent(proc, "GetProcGroup", map[string]any{
				"group_id":     groupID,
				"member_count": len(members),
			}, nil, nil, time.Since(start))
		}
	}

	return members, nil
}

// SignalGroup sends a signal to all processes in a group.
func (k *KernelImpl) SignalGroup(groupID types.PGID, signal types.Signal) error {
	start := time.Now()

	if !signal.Valid() {
		return NewSyscallError("SignalGroup", 0, "",
			fmt.Errorf("invalid signal: %d", signal), types.ErrInvalid)
	}

	group, ok := k.procGroups.Load(groupID)
	if !ok {
		return NewSyscallError("SignalGroup", 0, "",
			fmt.Errorf("group %d not found", groupID), types.ErrNotFound)
	}

	members := group.Members()
	if len(members) == 0 {
		return NewSyscallError("SignalGroup", 0, "",
			fmt.Errorf("group %d is empty", groupID), types.ErrNotFound)
	}

	successCount := 0
	for _, pid := range members {
		err := k.Kill(pid, signal)
		if err == nil {
			successCount++
		}
	}

	if proc, ok := k.GetProcess(members[0]); ok {
		k.emitEvent(proc, "SignalGroup", map[string]any{
			"group_id":      groupID,
			"signal":        signal,
			"member_count":  len(members),
			"success_count": successCount,
		}, nil, nil, time.Since(start))
	}

	return nil
}

// removeFromAllGroups removes a process from all groups it belongs to.
// Groups that become empty are automatically destroyed.
func (k *KernelImpl) removeFromAllGroups(pid types.PID, proc *Process) {
	for _, pgid := range proc.GetGroups() {
		if group, ok := k.procGroups.Load(pgid); ok {
			group.Remove(pid)
			if group.Size() == 0 {
				k.procGroups.Delete(pgid)
			}
		}
	}
}
