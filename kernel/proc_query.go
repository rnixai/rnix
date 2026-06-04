package kernel

import (
	"fmt"
	"log"
	"maps"
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
//
// Diagnostic note: a full Range scan (rather than early-return on first match)
// is performed to detect the pathological case where two procTable entries
// share the same UUID. If a duplicate is observed, a [resume-trace] warning is
// emitted so the resume race that produced the duplicate is auditable in
// daemon stderr; the first match is still returned to preserve historical
// behaviour. Hot-path scans are bounded by procTable size (typically < 100).
func (k *KernelImpl) GetProcessByUUID(uuid string) (*Process, bool) {
	if uuid == "" {
		return nil, false
	}
	var found *Process
	var dup *Process
	k.procTable.Range(func(pid types.PID, proc *Process) bool {
		if proc.UUID == uuid {
			if found == nil {
				found = proc
			} else if dup == nil {
				dup = proc
			}
		}
		return true
	})
	if dup != nil {
		log.Printf("[resume-trace] WARN GetProcessByUUID duplicate uuid=%s pid_a=%d state_a=%s pid_b=%d state_b=%s",
			uuid, found.PID, found.GetState(), dup.PID, dup.GetState())
	}
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

// LoadHistory loads process history from all per-project data directories
// into the in-memory ring buffer.
func (k *KernelImpl) LoadHistory() error {
	if k.dataDir == "" {
		return nil
	}
	merged := NewProcessHistory(1000)
	for _, baseDir := range AllBaseDirs(k.dataDir) {
		h, err := LoadProcHistory(baseDir, 1000)
		if err != nil {
			log.Printf("[history] warn: load %s: %v", baseDir, err)
			continue
		}
		for _, info := range h.List() {
			merged.Add(info)
		}
		if removed, rerr := LoadGcRemovedUUIDs(baseDir); rerr == nil {
			merged.SeedRemovedUUIDs(removed)
		}
	}
	k.procHistory = merged
	return nil
}

// ListResumable returns proc-info snapshots that can be resumed. Epic 42 fix:
// resumability is decoupled from daemon crashes — ANY process with on-disk
// history (steps.jsonl + proc-info.json) can be resumed unless it is currently
// running in the process table (which would amount to resuming itself).
//
// Filter:
//   - Include: disk entries whose UUID is NOT live, or whose live process is
//     in Dead/Zombie/Suspended state (still resumable from the user's POV).
//   - Exclude: disk entries whose live process is in Running state (the live
//     process IS the resumed session; no parallel resume permitted).
func (k *KernelImpl) ListResumable() ([]vfs.ProcInfo, error) {
	var candidates []vfs.ProcInfo
	for _, baseDir := range AllBaseDirs(k.dataDir) {
		c, err := ListResumable(baseDir)
		if err != nil {
			log.Printf("[resumable] warn: scan %s: %v", baseDir, err)
			continue
		}
		candidates = append(candidates, c...)
	}
	out := make([]vfs.ProcInfo, 0, len(candidates))
	for _, c := range candidates {
		if live, ok := k.GetProcessByUUID(c.UUID); ok {
			if live.GetState() == types.StateRunning {
				continue // would amount to resuming a process that is still running
			}
		}
		out = append(out, c)
	}
	return out, nil
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

	// Suspended process: no running goroutine, handle signal directly
	if state == types.StateSuspended {
		if signal.IsTermination() {
			return k.killSuspendedProcess(proc, signal, "Kill", start)
		}
		// Non-termination signals on suspended process: noop
		k.emitEvent(proc, "Kill", map[string]any{
			"pid":    pid,
			"signal": signal.String(),
			"action": "noop_suspended",
		}, nil, nil, time.Since(start))
		return nil
	}

	action, extraArgs, deliverErr := k.deliverSignal(proc, signal)

	args := map[string]any{
		"pid":    pid,
		"signal": signal.String(),
		"action": action,
	}
	maps.Copy(args, extraArgs)
	k.emitEvent(proc, "Kill", args, nil, deliverErr, time.Since(start))

	if deliverErr != nil {
		return NewSyscallError("Kill", pid, "", deliverErr, types.ErrInternal)
	}
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

// buildMCPMountSnapshots pairs proc.MCPMounts (path slice) with proc.mcpConfigs
// (vfs.MCPConfig slice) into [{Path, Config}] tuples for persistence.
//
// MUST be called with proc.mu held — both source slices are mu-protected
// (kernel/spawn.go:646-650 writes them under lock, and rehydrate writes them
// under lock too). Callers in this file (GetProcInfo / ListProcs) invoke this
// helper inline inside their proc.mu critical section, so the contract is
// satisfied by construction; Story 48.1 code-review P6 verified both sites.
//
// Pairing strategy: the path format is `/mnt/mcp/<pid>-<server>`; we extract
// the suffix after the FIRST `-` and match it to mcpCfg.ServerName. PIDs are
// purely numeric, so the first dash is always the pid/server boundary even
// when the server name itself contains dashes ("deepwiki-test"). This is
// resilient to PID renumbering between snapshots (rehydrate may write a path
// using the original PID while the in-memory proc carries a new one) and to
// slice-order drift.
//
// Skipped paths log a warning: a non-empty MCPMounts with no matching
// mcpConfig means the two slices have drifted out of sync — exactly the
// kind of silent-drop that masks the original Investigation Finding 9 this
// story was meant to eliminate (Story 48.1 code-review B10).
func buildMCPMountSnapshots(proc *Process) []vfs.MCPMountSnapshot {
	if len(proc.MCPMounts) == 0 {
		return nil
	}
	if len(proc.mcpConfigs) == 0 {
		log.Printf("[proc_query] uuid=%s: MCPMounts non-empty (%d paths) but mcpConfigs empty — snapshot will be empty, mounts will not survive resume",
			proc.UUID, len(proc.MCPMounts))
		return nil
	}
	cfgByName := make(map[string]vfs.MCPConfig, len(proc.mcpConfigs))
	for _, cfg := range proc.mcpConfigs {
		cfgByName[cfg.ServerName] = cfg
	}
	out := make([]vfs.MCPMountSnapshot, 0, len(proc.MCPMounts))
	for _, path := range proc.MCPMounts {
		// extract `<server>` from `/mnt/mcp/<pid>-<server>`
		serverName := mcpServerNameFromPath(path)
		cfg, ok := cfgByName[serverName]
		if !ok {
			log.Printf("[proc_query] uuid=%s: MCPMount path %q has no matching mcpConfig (server=%q) — skipping; resume will lose this mount",
				proc.UUID, path, serverName)
			continue
		}
		out = append(out, vfs.MCPMountSnapshot{Path: path, Config: cfg})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mcpServerNameFromPath extracts the server name suffix from a canonical
// `/mnt/mcp/<pid>-<server>` mount path. Returns "" if the path doesn't match.
// Splits on the FIRST `-`, which is the pid/server boundary as long as PIDs
// stay purely numeric (server names may legitimately contain dashes like
// "deepwiki-test"). Story 48.1 code-review P9.
func mcpServerNameFromPath(path string) string {
	const prefix = "/mnt/mcp/"
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return ""
	}
	tail := path[len(prefix):]
	for i := 0; i < len(tail); i++ {
		if tail[i] == '-' {
			return tail[i+1:]
		}
	}
	return ""
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
	exitReason := ""
	exitCode := 0
	exitCodeSet := false
	if proc.Exit != nil {
		exitReason = proc.Exit.Reason
		exitCode = proc.Exit.Code
		exitCodeSet = true
	}
	info := &vfs.ProcInfo{
		PID:             proc.PID,
		UUID:            proc.UUID,
		OriginUUID:      proc.OriginUUID,
		ResumedFromStep: proc.ResumedFromStep,
		ExitReason:      exitReason,
		CtxSize:         proc.CtxSize,
		PPID:            proc.PPID,
		ParentUUID:      proc.ParentUUID,
		State:           proc.State,
		Intent:          proc.Intent,
		Skills:          append([]string(nil), proc.Skills...),
		TokensUsed:      proc.TokensUsed,
		LastInputTokens: proc.LastInputTokens,
		ContextBudget:   proc.ContextBudget,
		MaxTokens:       proc.Budget.MaxTokens,
		MaxCost:         proc.Budget.MaxCost,
		UsedCost:        proc.Budget.UsedCost,
		MaxSteps:        proc.MaxSteps,
		CreatedAt:       proc.CreatedAt,
		DeadAt:          proc.DeadAt,
		CtxID:           proc.CtxID,
		Result:          proc.Result,
		AllowedDevices:  append([]string(nil), proc.AllowedDevices...),
		Provider:        proc.Provider,
		Model:           proc.Model,
		PrimaryDevice:   proc.PrimaryDevice,
		ContextWindow:   proc.ContextWindow,
		LastHeartbeat:   proc.LastHeartbeat,
		StepTimeout:     proc.StepTimeout,
		SuspendReason:   proc.SuspendReason,
		IsPaused:        proc.resumeCh != nil || proc.State == types.StateSuspended,
		PausedAt:        proc.pausedAt,
		PausedTotal:     proc.pausedTotal,
		ComposeNode:     proc.ComposeNode,
		ComposeDeps:     append([]string(nil), proc.ComposeDeps...),
		PipelineIndex:   proc.PipelineIndex,
		PipelineTotal:   proc.PipelineTotal,
		ExitCode:        exitCode,
		ExitCodeSet:     exitCodeSet,
		MCPMounts:       buildMCPMountSnapshots(proc),
		DriverMeta:      proc.DriverMeta,
		FeatureProfile:  proc.FeatureFlags.ProfileName,
	}
	if proc.ProjectConfig != nil {
		info.ProjectDir = proc.ProjectConfig.ProjectDir
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
		exitReason := ""
		exitCode := 0
		exitCodeSet := false
		if proc.Exit != nil {
			exitReason = proc.Exit.Reason
			exitCode = proc.Exit.Code
			exitCodeSet = true
		}
		infos = append(infos, vfs.ProcInfo{
			PID:             proc.PID,
			UUID:            proc.UUID,
			OriginUUID:      proc.OriginUUID,
			ResumedFromStep: proc.ResumedFromStep,
			ExitReason:      exitReason,
			CtxSize:         proc.CtxSize,
			PPID:            proc.PPID,
			ParentUUID:      proc.ParentUUID,
			State:           proc.State,
			Intent:          proc.Intent,
			Skills:          append([]string(nil), proc.Skills...),
			TokensUsed:      proc.TokensUsed,
			LastInputTokens: proc.LastInputTokens,
			ContextBudget:   proc.ContextBudget,
			MaxTokens:       proc.Budget.MaxTokens,
			MaxCost:         proc.Budget.MaxCost,
			UsedCost:        proc.Budget.UsedCost,
			CreatedAt:       proc.CreatedAt,
			DeadAt:          proc.DeadAt,
			CtxID:           proc.CtxID,
			Result:          proc.Result,
			AllowedDevices:  append([]string(nil), proc.AllowedDevices...),
			Provider:        proc.Provider,
			Model:           proc.Model,
			PrimaryDevice:   proc.PrimaryDevice,
			ContextWindow:   proc.ContextWindow,
			LastHeartbeat:   proc.LastHeartbeat,
			StepTimeout:     proc.StepTimeout,
			SuspendReason:   proc.SuspendReason,
			IsPaused:        proc.resumeCh != nil || proc.State == types.StateSuspended,
			PausedAt:        proc.pausedAt,
			PausedTotal:     proc.pausedTotal,
			MaxSteps:        proc.MaxSteps,
			ComposeNode:     proc.ComposeNode,
			ComposeDeps:     append([]string(nil), proc.ComposeDeps...),
			PipelineIndex:   proc.PipelineIndex,
			PipelineTotal:   proc.PipelineTotal,
			ExitCode:        exitCode,
			ExitCodeSet:     exitCodeSet,
			MCPMounts:       buildMCPMountSnapshots(proc),
			DriverMeta:      proc.DriverMeta,
		})
		// Stamp ProjectDir on the just-appended entry. proc.ProjectConfig is
		// pointer-safe to read under proc.mu (set once at Spawn, immutable
		// afterwards — see kernel/spawn.go:71). Empty when the process was
		// spawned without a project context (test fixtures, global-only runs).
		if proc.ProjectConfig != nil {
			infos[len(infos)-1].ProjectDir = proc.ProjectConfig.ProjectDir
		}
		proc.mu.Unlock()
		return true
	})
	return infos
}

// ListAllProcs returns the union of active processes and historical processes.
//
// Dedup rules (defense against pathological inputs):
//   - 同一非空 UUID 在结果中至多出现一次（active 优先于 historical）。
//   - active 内部理论上不会有同 UUID（procTable 按 PID 索引、UUID 唯一），但仍做
//     去重防御以应对未来 resume / fork 路径的不变量退化。
//   - historical 内部允许同 UUID 多条记录（reap / cleanupExpiredDead 多次 Add 同
//     UUID 时），此处按"先到先留"原则保留一条。
//   - 空 UUID 不进 seen 集合，按原条目逐一保留（向后兼容老进程）。
func (k *KernelImpl) ListAllProcs() []vfs.ProcInfo {
	active := k.ListProcs()
	historical := k.procHistory.List()

	seen := make(map[string]bool, len(active)+len(historical))
	result := make([]vfs.ProcInfo, 0, len(active)+len(historical))
	for _, p := range active {
		if p.UUID != "" {
			if seen[p.UUID] {
				continue
			}
			seen[p.UUID] = true
		}
		result = append(result, p)
	}
	for _, p := range historical {
		if p.UUID != "" {
			if seen[p.UUID] {
				continue
			}
			seen[p.UUID] = true
		}
		p.PID = 0 // historical process — PID no longer valid after reap
		result = append(result, p)
	}

	slices.SortFunc(result, func(a, b vfs.ProcInfo) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return result
}

// checkBudgetWarning emits warning/critical log when per-step input tokens
// are approaching the context budget (context window guard).
func (k *KernelImpl) checkBudgetWarning(proc *Process, step, inputTokens, budget int) {
	if budget <= 0 || inputTokens >= budget {
		return
	}
	usagePct := float64(inputTokens) / float64(budget) * 100
	if usagePct < 80 {
		return
	}
	level := "warning"
	if usagePct >= 90 {
		level = "critical"
	}
	k.emitLog(proc, step, types.LogWarning,
		fmt.Sprintf("Context %s: %d/%d input tokens (%.0f%% of budget)",
			level, inputTokens, budget, usagePct), "")
	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":        step,
		"action":      "budget_warning",
		"input_tokens": inputTokens,
		"budget":      budget,
		"usage_pct":   usagePct,
		"alert_level": level,
	}, nil, nil, 0)
}
