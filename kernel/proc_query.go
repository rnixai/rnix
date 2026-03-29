package kernel

import (
	"fmt"
	"slices"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// AddProcess registers a process in the kernel's process table.
func (k *KernelImpl) AddProcess(p *Process) {
	k.procTable.Store(p.PID, p)
}

// GetProcess retrieves a process by PID.
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool) {
	return k.procTable.Load(pid)
}

// GetProcessByUUID finds a process by UUID in the process table.
func (k *KernelImpl) GetProcessByUUID(uuid string) (*Process, bool) {
	if uuid == "" {
		return nil, false
	}
	var found *Process
	k.procTable.Range(func(pid types.PID, proc *Process) bool {
		if proc.UUID == uuid {
			found = proc
			return false
		}
		return true
	})
	return found, found != nil
}

// RemoveProcess removes a process from the process table.
func (k *KernelImpl) RemoveProcess(pid types.PID) {
	k.procTable.Delete(pid)
}

// FindHistoryByPID returns the most recent history snapshot for a reaped process, or nil.
func (k *KernelImpl) FindHistoryByPID(pid types.PID) *vfs.ProcInfo {
	return k.procHistory.FindByPID(pid)
}

// FindHistoryByUUID returns the most recent history snapshot for the given UUID, or nil.
func (k *KernelImpl) FindHistoryByUUID(uuid string) *vfs.ProcInfo {
	return k.procHistory.FindByUUID(uuid)
}

// LoadHistory loads process history from disk into the in-memory ring buffer.
func (k *KernelImpl) LoadHistory() error {
	if k.stepDataDir == "" {
		return nil
	}
	history, err := LoadProcHistory(k.stepDataDir, 1000)
	if err != nil {
		return err
	}
	k.procHistory = history
	return nil
}

// RegisterBudgetPool associates a BudgetPool with a process group.
func (k *KernelImpl) RegisterBudgetPool(groupID types.PGID, pool *BudgetPool) {
	k.budgetPools.Store(groupID, pool)
}

// UnregisterBudgetPool removes a BudgetPool association.
func (k *KernelImpl) UnregisterBudgetPool(groupID types.PGID) {
	k.budgetPools.Delete(groupID)
}

// GetBudgetStatus returns a snapshot of the budget pool for the given group.
func (k *KernelImpl) GetBudgetStatus(groupID types.PGID) (*BudgetPoolStatus, error) {
	pool, ok := k.budgetPools.Load(groupID)
	if !ok {
		return nil, fmt.Errorf("no budget pool for group %d", groupID)
	}
	status := pool.GetStatus()
	return &status, nil
}

// RecordSLAResult appends an SLA evaluation result for a compose group (Story 21.2).
func (k *KernelImpl) RecordSLAResult(groupID types.PGID, result *SLAResult) {
	k.slaResultsMu.Lock()
	defer k.slaResultsMu.Unlock()
	existing, ok := k.slaResults.Load(groupID)
	if !ok {
		existing = []*SLAResult{}
	}
	existing = append(existing, result)
	k.slaResults.Store(groupID, existing)
}

// GetSLAResults returns the SLA evaluation results for a compose group (Story 21.2).
func (k *KernelImpl) GetSLAResults(groupID types.PGID) ([]*SLAResult, error) {
	results, ok := k.slaResults.Load(groupID)
	if !ok {
		return nil, fmt.Errorf("no SLA results for group %d", groupID)
	}
	return results, nil
}

// GetLineage returns the lineage events for the given PID.
func (k *KernelImpl) GetLineage(pid types.PID) ([]LineageEvent, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, NewSyscallError("GetLineage", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}
	lineage := proc.GetLineage()
	if lineage == nil {
		return nil, nil
	}
	return lineage.Events(), nil
}

// Kill sends a signal to the target process.
func (k *KernelImpl) Kill(pid types.PID, signal types.Signal) error {
	start := time.Now()

	if !signal.Valid() {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("invalid signal %d", signal), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}

	k.emitEvent(proc, "Kill", map[string]any{
		"pid":    pid,
		"signal": signal.String(),
	}, nil, nil, 0)

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		k.emitEvent(proc, "Kill", map[string]any{
			"pid":    pid,
			"signal": signal.String(),
			"action": "noop",
		}, nil, nil, time.Since(start))
		return nil
	}

	action := k.deliverSignal(proc, signal)

	k.emitEvent(proc, "Kill", map[string]any{
		"pid":    pid,
		"signal": signal.String(),
		"action": action,
	}, nil, nil, time.Since(start))

	return nil
}

// ListProcesses returns all processes currently in the process table.
func (k *KernelImpl) ListProcesses() []*Process {
	var procs []*Process
	k.procTable.Range(func(_ types.PID, p *Process) bool {
		procs = append(procs, p)
		return true
	})
	return procs
}

// GetSpanID returns the SpanID for the given process, if it has one.
func (k *KernelImpl) GetSpanID(pid types.PID) (types.SpanID, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return "", false
	}
	proc.mu.Lock()
	spanID := proc.SpanID
	proc.mu.Unlock()
	return spanID, spanID != ""
}

// GetProcInfo returns a snapshot of process information for the given PID.
func (k *KernelImpl) GetProcInfo(pid types.PID) (*vfs.ProcInfo, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, &vfs.VFSError{
			Op:     "GetProcInfo",
			Device: "/proc",
			Err:    fmt.Errorf("process %d not found", pid),
			Code:   types.ErrNotFound,
		}
	}

	proc.mu.Lock()
	info := &vfs.ProcInfo{
		PID:            proc.PID,
		UUID:           proc.UUID,
		PPID:           proc.PPID,
		State:          proc.State,
		Intent:         proc.Intent,
		Skills:         append([]string(nil), proc.Skills...),
		TokensUsed:     proc.TokensUsed,
		ContextBudget:  proc.ContextBudget,
		MaxSteps:       proc.MaxSteps,
		CreatedAt:      proc.CreatedAt,
		DeadAt:         proc.DeadAt,
		CtxID:          proc.CtxID,
		Result:         proc.Result,
		AllowedDevices: append([]string(nil), proc.AllowedDevices...),
		Provider:       proc.Provider,
		Model:          proc.Model,
	}
	proc.mu.Unlock()
	return info, nil
}

// GetTokenHistory returns a copy of the token usage history for a process.
func (k *KernelImpl) GetTokenHistory(pid types.PID) ([]types.TokenSnapshot, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, NewSyscallError("GetTokenHistory", pid, "", fmt.Errorf("process %d not found", pid), types.ErrNotFound)
	}
	history := proc.GetTokenHistory()
	if history == nil {
		return []types.TokenSnapshot{}, nil
	}
	return history, nil
}

// ListProcs returns snapshots of all processes in the process table.
func (k *KernelImpl) ListProcs() []vfs.ProcInfo {
	var infos []vfs.ProcInfo
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		proc.mu.Lock()
		infos = append(infos, vfs.ProcInfo{
			PID:            proc.PID,
			UUID:           proc.UUID,
			PPID:           proc.PPID,
			State:          proc.State,
			Intent:         proc.Intent,
			Skills:         append([]string(nil), proc.Skills...),
			TokensUsed:     proc.TokensUsed,
			ContextBudget:  proc.ContextBudget,
			CreatedAt:      proc.CreatedAt,
			DeadAt:         proc.DeadAt,
			CtxID:          proc.CtxID,
			Result:         proc.Result,
			AllowedDevices: append([]string(nil), proc.AllowedDevices...),
			Provider:       proc.Provider,
			Model:          proc.Model,
		})
		proc.mu.Unlock()
		return true
	})
	return infos
}

// ListAllProcs returns the union of active processes and historical processes.
func (k *KernelImpl) ListAllProcs() []vfs.ProcInfo {
	active := k.ListProcs()
	historical := k.procHistory.List()

	seen := make(map[string]bool, len(active))
	result := make([]vfs.ProcInfo, 0, len(active)+len(historical))
	for _, p := range active {
		seen[p.UUID] = true
		result = append(result, p)
	}
	for _, p := range historical {
		if !seen[p.UUID] {
			result = append(result, p)
		}
	}

	slices.SortFunc(result, func(a, b vfs.ProcInfo) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return result
}

// checkBudgetWarning emits warning/critical log and event when context budget
// is nearing exhaustion.
func (k *KernelImpl) checkBudgetWarning(proc *Process, step, tokens, budget int) {
	if budget <= 0 || tokens >= budget {
		return
	}
	remainPct := float64(budget-tokens) / float64(budget) * 100
	if remainPct >= 20 {
		return
	}
	avgRate := float64(tokens) / float64(step)
	estRemain := 0
	if avgRate > 0 {
		estRemain = int(float64(budget-tokens) / avgRate)
	}
	level := "warning"
	if remainPct < 10 {
		level = "critical"
	}
	k.emitLog(proc, step, types.LogWarning,
		fmt.Sprintf("Budget %s: %d/%d (%.0f%% remaining, ~%d steps left)",
			level, tokens, budget, remainPct, estRemain), "")
	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":          step,
		"action":        "budget_warning",
		"tokens":        tokens,
		"budget":        budget,
		"remaining_pct": remainPct,
		"est_remaining": estRemain,
		"alert_level":   level,
	}, nil, nil, 0)
}
