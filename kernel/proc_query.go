package kernel

import (
	"cmp"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
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
	reconciled := 0
	for _, baseDir := range AllBaseDirs(k.dataDir) {
		// Full disk scan, not the 1000-entry window (Story 64.1 review D2):
		// ListResumable reads the disk directly, so stale entries beyond the
		// window must also be normalized. merged (ring cap 1000, fed in
		// CreatedAt order) still keeps only the most recent entries in memory.
		infos, err := loadAllProcInfos(baseDir)
		if err != nil {
			log.Printf("[history] warn: load %s: %v", baseDir, err)
			continue
		}
		for _, info := range infos {
			// Story 64.1: normalize a non-terminal snapshot (created/running/
			// zombie) left by a force-killed daemon to Dead, and write the cure
			// back so it is permanent. best-effort — a writeback failure only
			// warns and never blocks startup; merged always receives the
			// normalized (in-memory) info so the view is correct even if the
			// disk write lost a race (裁决 5).
			info, changed := k.reconcileStaleHistoryEntry(baseDir, info)
			if changed {
				reconciled++
				if serr := SaveProcInfo(baseDir, info); serr != nil {
					log.Printf("[history] warn: reconcile writeback %s: %v", info.UUID, serr)
				}
			}
			// Story 64.2: Upsert (not Add) so a same UUID appearing across
			// baseDirs (projects/* + global fallback, e.g. data-migration
			// leftovers) collapses to a single snapshot; the later-scanned
			// baseDir wins, EXCEPT a dead snapshot is never replaced by a
			// non-terminal one (Upsert terminal guard). Within one baseDir
			// 每 UUID 至多一目录，无重复。
			merged.Upsert(info)
		}
		if removed, rerr := LoadGcRemovedUUIDs(baseDir); rerr == nil {
			merged.SeedRemovedUUIDs(removed)
		}
	}
	if reconciled > 0 {
		log.Printf("[history] reconciled %d stale non-terminal entries to dead", reconciled)
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

// KillAttribution answers "who killed this process" for every Kill event
// emitted along a termination path (Story 66.3).
//
// It travels as an explicit parameter down Kill → deliverSignal →
// defaultSignalAction → twoPhaseShutdown rather than living on Process:
// concurrent kills would otherwise race for a single field and the audit
// record would depend on which caller won the shutdownStarted CAS.
type KillAttribution struct {
	// Origin is who asked. Empty is normalized to KillOriginUnknown.
	Origin types.KillOrigin
	// Requester identifies the concrete asker — `rnix[41234]` for CLI clients,
	// a subsystem name (`immune`, `intent-reconciler`) for in-daemon callers.
	Requester string
	// RootPID / RootOrigin are set only on cascade descendants, recording the
	// process whose termination triggered this one and that request's origin.
	RootPID    types.PID
	RootOrigin types.KillOrigin
	// Escalation records that this kill is a system-internal upgrade of an
	// earlier request (currently only "grace_timeout"). Origin still names the
	// original requester — an escalation is a means, not a second killer.
	Escalation string
}

// withEscalation returns a copy of attr tagged as a system-internal upgrade.
func (a KillAttribution) withEscalation(reason string) KillAttribution {
	a.Escalation = reason
	return a
}

// killAttrArgs renders the attribution into event args. origin and requester
// are always present (AC5: legacy paths land "unknown", never a missing field);
// the cascade and escalation fields appear only when set.
func killAttrArgs(attr KillAttribution) map[string]any {
	origin := attr.Origin
	if origin == "" {
		origin = types.KillOriginUnknown
	}
	args := map[string]any{
		"origin":    origin.String(),
		"requester": attr.Requester,
	}
	if attr.RootPID != 0 {
		args["root_pid"] = attr.RootPID
	}
	if attr.RootOrigin != "" {
		args["root_origin"] = attr.RootOrigin.String()
	}
	if attr.Escalation != "" {
		args["escalation"] = attr.Escalation
	}
	return args
}

// killEventArgs builds the common Kill event args (attribution + pid + signal).
// Each emit point gets a fresh map — emitEvent hands the map off to consumers.
func killEventArgs(pid types.PID, signal types.Signal, attr KillAttribution) map[string]any {
	args := killAttrArgs(attr)
	args["pid"] = pid
	args["signal"] = signal.String()
	return args
}

// Kill sends a signal to the target process, without attribution.
// Callers that know who requested the termination should use KillWithOrigin;
// this delegate keeps the ProcessManager interface and every legacy call site
// compiling, at the cost of an "unknown" origin in the audit trail.
func (k *KernelImpl) Kill(pid types.PID, signal types.Signal) error {
	return k.KillWithOrigin(pid, signal, KillAttribution{Origin: types.KillOriginUnknown})
}

// KillWithOrigin sends a signal to the target process and records who asked.
// Every Kill event on the path (entry / noop / noop_suspended / final action /
// killed_suspended) carries the attribution.
func (k *KernelImpl) KillWithOrigin(pid types.PID, signal types.Signal, attr KillAttribution) error {
	start := time.Now()

	if !signal.Valid() {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("invalid signal %d", signal), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}

	k.emitEvent(proc, "Kill", killEventArgs(pid, signal, attr), nil, nil, 0)

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		// "Who is killing an already-dead process" is audit information too.
		args := killEventArgs(pid, signal, attr)
		args["action"] = "noop"
		k.emitEvent(proc, "Kill", args, nil, nil, time.Since(start))
		return nil
	}

	// Suspended process: no running goroutine, handle signal directly
	if state == types.StateSuspended {
		if signal.IsTermination() {
			return k.killSuspendedProcess(proc, signal, "Kill", start, attr)
		}
		// Non-termination signals on suspended process: noop
		args := killEventArgs(pid, signal, attr)
		args["action"] = "noop_suspended"
		k.emitEvent(proc, "Kill", args, nil, nil, time.Since(start))
		return nil
	}

	action, extraArgs, deliverErr := k.deliverSignal(proc, signal, attr)

	args := killEventArgs(pid, signal, attr)
	args["action"] = action
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
		// Story 66.6: synthesize the live token preview = authoritative TokensUsed
		// + the current step's mid-stream accumulation. For a completed / dead
		// process StreamTokensUsed is 0 (cleared at the step boundary or folded in
		// by finishProcess), so this equals TokensUsed — every downstream
		// consumer (ps/top/dashboard/compose) heals with zero changes.
		TokensUsed:      proc.TokensUsed + proc.StreamTokensUsed,
		UsageProvenance: proc.UsageProvenance,
		ToolCallCount:   proc.ToolCallCount,
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
		ResultPartial:   proc.ResultPartial,
		AllowedDevices:  append([]string(nil), proc.AllowedDevices...),
		DeniedDevices:   append([]string(nil), proc.DeniedDevices...),
		AllowedTools:    append([]string(nil), proc.AllowedTools...),
		Provider:        proc.Provider,
		Model:           proc.Model,
		ReasoningEffort: proc.ReasoningEffort,
		PrimaryDevice:   proc.PrimaryDevice,
		ContextWindow:   proc.ContextWindow,
		LastHeartbeat:   proc.LastHeartbeat,
		StepTimeout:     proc.StepTimeout,
		SuspendReason:   proc.SuspendReason,
		ResumeAt:        proc.ResumeAt,
		CompactLatched:  proc.CompactLatched,
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
	// Story 71.3 AC5 — expose the compact timeout to the disk layer ONLY when it
	// was explicitly configured (opts/manifest). Derived values stay 0 here so
	// procInfoToDisk omits them and resume re-derives from current providers.yaml.
	if proc.compactTimeoutExplicit {
		info.CompactTimeout = proc.CompactTimeout
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
			// Story 66.6: live token preview + provenance + tool-call liveness
			// (see GetProcInfo above for the synthesis rationale).
			TokensUsed:      proc.TokensUsed + proc.StreamTokensUsed,
			UsageProvenance: proc.UsageProvenance,
			ToolCallCount:   proc.ToolCallCount,
			LastInputTokens: proc.LastInputTokens,
			ContextBudget:   proc.ContextBudget,
			MaxTokens:       proc.Budget.MaxTokens,
			MaxCost:         proc.Budget.MaxCost,
			UsedCost:        proc.Budget.UsedCost,
			CreatedAt:       proc.CreatedAt,
			DeadAt:          proc.DeadAt,
			CtxID:           proc.CtxID,
			Result:          proc.Result,
			ResultPartial:   proc.ResultPartial,
			AllowedDevices:  append([]string(nil), proc.AllowedDevices...),
			DeniedDevices:   append([]string(nil), proc.DeniedDevices...),
			AllowedTools:    append([]string(nil), proc.AllowedTools...),
			Provider:        proc.Provider,
			Model:           proc.Model,
			ReasoningEffort: proc.ReasoningEffort,
			PrimaryDevice:   proc.PrimaryDevice,
			ContextWindow:   proc.ContextWindow,
			LastHeartbeat:   proc.LastHeartbeat,
			StepTimeout:     proc.StepTimeout,
			SuspendReason:   proc.SuspendReason,
			ResumeAt:        proc.ResumeAt,
			CompactLatched:  proc.CompactLatched,
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
		// Story 71.3 AC5 — explicit-only compact timeout (see GetProcInfo).
		if proc.compactTimeoutExplicit {
			infos[len(infos)-1].CompactTimeout = proc.CompactTimeout
		}
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
//   - 同一非空 UUID 在结果中至多出现一次。
//   - active 无条件优先于 historical：active 占用的 UUID 不被任何 historical 条目
//     替换（active 内部理论上不会有同 UUID，仍去重防御未来 resume/fork 不变量退化）。
//   - historical 内部同 UUID 多条时（如 64-1 之前的 Add 泄漏路径）按比较式去重
//     （Story 64.2 裁决 2）：① 新条目为终态(Dead)且旧条目非终态 → 替换（终态胜
//     非终态，编码状态机不变量"Dead→任何状态非法"）；② 新旧同级（同终态或同非终态）
//     → 替换（last-wins：以 FIFO 写入序近似新旧——单一来源内成立；跨 baseDir 拼接
//     时非全局时序，此时终态误判由规则①③兜底）；③ 新条目非终态且旧条目终态 →
//     跳过。规则①③不依赖写入序，构成不依赖裁决 1 Upsert 的独立防线。
//   - 空 UUID 不进 seen 集合，按原条目逐一保留（向后兼容老进程）。
//   - 替换/保留的 historical 条目 PID 一律清零（reap 后 PID 失效）。
func (k *KernelImpl) ListAllProcs() []vfs.ProcInfo {
	active := k.ListProcs()
	historical := k.procHistory.List()

	seen := make(map[string]bool, len(active)+len(historical))
	// histIdx maps a historical UUID to its slot in result, so a later same-UUID
	// entry can compare against and replace the earlier one in place (裁决 2).
	// active-occupied UUIDs never enter histIdx — active wins unconditionally.
	histIdx := make(map[string]int, len(historical))
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
		p.PID = 0 // historical process — PID no longer valid after reap
		if p.UUID == "" {
			// empty UUID: not deduped (backward compat with pre-UUID procs).
			result = append(result, p)
			continue
		}
		if seen[p.UUID] {
			// active already occupies this UUID → active wins, drop historical.
			continue
		}
		if idx, ok := histIdx[p.UUID]; ok {
			// Same UUID earlier in historical — compare terminal-ness / recency.
			if shouldReplaceHistoryEntry(result[idx], p) {
				result[idx] = p
			}
			continue
		}
		histIdx[p.UUID] = len(result)
		result = append(result, p)
	}

	// Comparator chain: CreatedAt → UUID → PID. The input order is random
	// (active comes from SyncMap.Range) — so CreatedAt ties MUST be broken
	// deterministically, or the result order (and the IPC pagination windows
	// sliced from it) drifts between calls, making dashboard rows jump every
	// tick. UUID breaks ties for all real processes; PID covers legacy entries
	// with empty UUIDs. The residual full-tie case (historical entries with
	// PID zeroed above, empty UUID, equal CreatedAt) is closed by the STABLE
	// sort: procHistory.List() preserves FIFO insertion order, and active
	// processes always have unique PIDs, so equal elements keep a fixed
	// relative order across calls.
	slices.SortStableFunc(result, func(a, b vfs.ProcInfo) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		if c := strings.Compare(a.UUID, b.UUID); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
	return result
}

// isTerminalHistoryState reports whether a history snapshot's state is terminal.
// Only Dead is terminal for history dedup purposes (Story 64.2 裁决 2). Zombie is
// deliberately NOT treated as terminal: history entries are almost never Zombie
// (reap落史时已 Dead、64-1 装载归一化为 dead)，即便出现也应让位于 dead 条目。
// kernel-local helper — do NOT reference dashboard's StateRank (依赖方向反转).
func isTerminalHistoryState(s types.ProcessState) bool {
	return s == types.StateDead
}

// shouldReplaceHistoryEntry decides whether a later same-UUID historical entry
// (cand) should replace an earlier one (cur) in ListAllProcs dedup (Story 64.2
// 裁决 2):
//   - cand terminal, cur non-terminal → replace (终态胜非终态).
//   - cand non-terminal, cur terminal → keep cur (终态不被非终态取代).
//   - same terminal-ness → replace (last-wins: FIFO-later = written later = newer).
func shouldReplaceHistoryEntry(cur, cand vfs.ProcInfo) bool {
	curTerminal := isTerminalHistoryState(cur.State)
	candTerminal := isTerminalHistoryState(cand.State)
	if candTerminal != curTerminal {
		return candTerminal
	}
	return true
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
		"step":         step,
		"action":       "budget_warning",
		"input_tokens": inputTokens,
		"budget":       budget,
		"usage_pct":    usagePct,
		"alert_level":  level,
	}, nil, nil, 0)
}
