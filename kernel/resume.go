package kernel

import (
	"bufio"
	gocontext "context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ResumeResult holds the output of a successful Resume call.
type ResumeResult struct {
	PID             types.PID
	UUID            string
	ResumedFromStep int
}

// ResumeOpts holds options for the extended Resume call.
type ResumeOpts struct {
	Fork bool // true = new UUID + origin_uuid tracking; false = inherit original UUID

	// FromStep — Story 42.3 stub: truncates history replay to the first FromStep
	// records, then continues reasoning from step FromStep+1. 0 = no truncation.
	// Negative values are rejected at the resume entry. Effective only on the
	// history path; checkpoint path rejects (FromStep>0 + FromStep<cpData.LastStep).
	// Behavior wired in dev-story; ATDD red-phase: field exists, logic pending.
	FromStep int
}

// cleanupOldProcessAndHistory removes the old in-memory Process and procHistory
// entry that share the resume UUID. It is a no-op when fork=true (preserving
// the original lineage for the forked branch). Called by both resumeFromCheckpoint
// and resumeFromHistory after all new-process allocations succeed.
func (k *KernelImpl) cleanupOldProcessAndHistory(oldProc *Process, uuid string, fork bool) {
	if fork {
		return
	}
	if oldProc != nil {
		if oldProc.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(oldProc.CtxID)
		}
		k.procTable.Delete(oldProc.PID)
		if queue, ok := k.msgQueues.LoadAndDelete(oldProc.PID); ok {
			queue.close()
		}
	}
	k.procHistory.RemoveByUUID(uuid)
}

// checkpointExists reports whether a checkpoint.json file is present on disk
// for the given UUID. Used by ResumeWithOpts to apply the spec L114-117
// checkpoint > history preference uniformly across all routing branches.
func checkpointExists(baseDir, uuid string) bool {
	if baseDir == "" || uuid == "" {
		return false
	}
	cpPath := filepath.Join(baseDir, "data", "steps", uuid, "checkpoint.json")
	_, err := os.Stat(cpPath)
	return err == nil
}

// isValidUUIDFormat validates that a UUID string contains only safe characters
// (alphanumeric and dashes) to prevent path traversal attacks.
func isValidUUIDFormat(uuid string) bool {
	if len(uuid) < 1 || len(uuid) > 64 {
		return false
	}
	for _, c := range uuid {
		isAlnum := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isAlnum && c != '-' {
			return false
		}
	}
	return true
}

// Resume restores a previously suspended or interrupted process from its checkpoint.
// It reads the checkpoint from disk, creates a new Process with a new PID,
// restores all state, and resumes the reasoning loop from the next step.
// The original UUID is preserved; the PID is newly allocated.
//
// Resume is a thin wrapper over ResumeWithOpts(uuid, ResumeOpts{}) — kept for
// backward compatibility with existing callers (30-4 tests, IPC client). All new
// code paths should call ResumeWithOpts directly. The previous standalone
// implementation has been removed; ResumeWithOpts → resumeFromCheckpoint is
// the single source of truth.
func (k *KernelImpl) Resume(uuid string) (*ResumeResult, error) {
	return k.ResumeWithOpts(uuid, ResumeOpts{})
}

// compareEnvSnapshot compares checkpoint env snapshot with current environment
// and emits warning events for any differences.
func (k *KernelImpl) compareEnvSnapshot(proc *Process, snapshot map[string]string) {
	var diffs []string
	for key, oldVal := range snapshot {
		newVal := os.Getenv(key)
		if newVal != oldVal {
			if newVal == "" {
				diffs = append(diffs, fmt.Sprintf("%s: removed (was set)", key))
			} else {
				diffs = append(diffs, fmt.Sprintf("%s: changed", key))
			}
		}
	}
	if len(diffs) > 0 {
		k.emitEvent(proc, "ResumeEnvDrift", map[string]any{
			"changed_vars": diffs,
			"count":        len(diffs),
		}, nil, nil, 0)
		log.Printf("[kernel] resume uuid=%s env drift: %d vars changed", proc.UUID, len(diffs))
	}
}

// ResumeWithOpts is the extended Resume that supports fork mode and history-based resume.
// It routes to the appropriate path based on process state:
//   - Suspended in procTable → original checkpoint-based path (30-4)
//   - Dead/Zombie in procTable → ResumeFromHistory
//   - Running in procTable → rejected
//   - Not in procTable → disk lookup → ResumeFromHistory
func (k *KernelImpl) ResumeWithOpts(uuid string, opts ResumeOpts) (*ResumeResult, error) {
	start := time.Now()

	if uuid == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("empty UUID"), types.ErrInvalid)
	}
	if !isValidUUIDFormat(uuid) {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("invalid UUID format: %s", uuid), types.ErrInvalid)
	}

	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	baseDir := k.stepDataDir
	if baseDir == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("step data directory not configured"), types.ErrInternal)
	}

	// Route based on process state in procTable
	if found, ok := k.GetProcessByUUID(uuid); ok {
		state := found.GetState()
		switch state {
		case types.StateSuspended:
			// Original 30-4 checkpoint path (delegate to existing Resume logic)
			return k.resumeFromCheckpoint(uuid, opts, start)
		case types.StateRunning:
			return nil, NewSyscallError("Resume", found.PID, "",
				fmt.Errorf("process with UUID %s is in Running state, cannot resume", uuid), types.ErrInvalid)
		case types.StateDead, types.StateZombie:
			// Prefer checkpoint.json if present (spec L114-117: checkpoint > history).
			// Falls back to history-based resume when no checkpoint was written.
			if checkpointExists(baseDir, uuid) {
				return k.resumeFromCheckpoint(uuid, opts, start)
			}
			return k.resumeFromHistory(uuid, opts, start)
		default:
			return nil, NewSyscallError("Resume", found.PID, "",
				fmt.Errorf("process with UUID %s is in %s state", uuid, state), types.ErrInvalid)
		}
	}

	// Not in procTable — check disk
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if _, err := os.Stat(stepsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("no data found for UUID %s", uuid), types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("stat steps dir: %w", err), types.ErrInternal)
	}

	// Prefer checkpoint path if checkpoint.json exists (shared with the
	// Dead/Zombie branch above via the checkpointExists helper).
	if checkpointExists(baseDir, uuid) {
		return k.resumeFromCheckpoint(uuid, opts, start)
	}

	return k.resumeFromHistory(uuid, opts, start)
}

// resumeFromCheckpoint handles the checkpoint-based resume path (original 30-4 logic).
func (k *KernelImpl) resumeFromCheckpoint(uuid string, opts ResumeOpts, start time.Time) (*ResumeResult, error) {
	baseDir := k.stepDataDir
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)

	// Story 42.3 — reject negative FromStep on this path too (defensive symmetry).
	if opts.FromStep < 0 {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("from_step %d invalid (must be >= 0)", opts.FromStep), types.ErrInvalid)
	}

	cpData, err := ReadCheckpointPublic(stepsDir)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "checkpoint read") {
			return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint not found for UUID %s", uuid), types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint corrupted: %w", err), types.ErrInvalid)
	}

	if cpData.UUID != uuid {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint UUID mismatch: got %s, want %s", cpData.UUID, uuid), types.ErrInvalid)
	}

	// Story 42.3 — checkpoint + --from-step conflict.
	// checkpoint.json is a complete ContextSnapshot at cpData.LastStep; we cannot
	// partially deserialize it back to an arbitrary earlier step. Reject when the
	// caller asks for a truncation that would precede the checkpoint anchor.
	if opts.FromStep > 0 && opts.FromStep < cpData.LastStep {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("from_step %d requires history path; checkpoint at step %d (full snapshot, no partial replay)",
				opts.FromStep, cpData.LastStep), types.ErrInvalid)
	}

	cp := cpData.ProcState
	startStep := cpData.LastStep + 1
	if cp.MaxSteps > 0 && startStep > cp.MaxSteps {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("checkpoint at max step %d/%d — nothing to resume", cpData.LastStep, cp.MaxSteps), types.ErrInvalid)
	}

	// Remove old process from procTable if Suspended
	var oldProc *Process
	if found, ok := k.GetProcessByUUID(uuid); ok {
		oldProc = found
	}

	llmDevice, _, resolveErr := k.resolveLLMDevice(nil, cp.Provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", cp.Provider, resolveErr), types.ErrDriver)
	}

	if oldProc != nil {
		// Defer cleanup of old process to AFTER all allocations succeed
		// (see post-Open block). This avoids destroying the original on early failure.
		// fork=true skips cleanup entirely so original keeps running.
		_ = oldProc
	}

	proc := NewProcess(0, cp.Intent, cp.Skills)
	if opts.Fork {
		// Fork mode: NewProcess already generated a fresh UUIDv7; assert it
		// differs from the origin to make the intent explicit (defensive against
		// future NewProcess refactors).
		if proc.UUID == "" || proc.UUID == uuid {
			return nil, NewSyscallError("Resume", proc.PID, "",
				fmt.Errorf("fork: new UUID generation produced empty or duplicate UUID %q", proc.UUID), types.ErrInternal)
		}
		proc.OriginUUID = uuid
	} else {
		// Non-fork: inherit original UUID
		proc.UUID = uuid
	}
	proc.Provider = cp.Provider
	proc.Model = cp.Model
	proc.AllowedDevices = append([]string(nil), cp.AllowedDevices...)
	proc.MaxSteps = cp.MaxSteps
	proc.ResumedFromStep = startStep
	proc.mu.Lock()
	proc.TokensUsed = cp.UsedTokens
	proc.Budget = ProcessBudget{MaxTokens: cp.MaxTokens, MaxCost: cp.MaxCost, UsedCost: cp.UsedCost}
	proc.mu.Unlock()

	resumeCtxSize := DefaultCtxSize
	if cp.CtxSize > 0 {
		resumeCtxSize = cp.CtxSize
	}
	proc.CtxSize = resumeCtxSize
	cid, err := k.ctxMgr.CtxAlloc(resumeCtxSize)
	if err != nil {
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context alloc: %w", err), types.ErrInternal)
	}
	proc.CtxID = cid

	ctx, getErr := k.ctxMgr.GetContext(cid)
	if getErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context get: %w", getErr), types.ErrInternal)
	}
	if err := ctx.Deserialize(cpData.ContextSnapshot); err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context deserialize: %w", err), types.ErrInternal)
	}

	proc.mu.Lock()
	proc.StepTimeout = 5 * time.Minute
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	if len(cp.EnvSnapshot) > 0 {
		k.compareEnvSnapshot(proc, cp.EnvSnapshot)
	}

	proc.PrimaryDevice = llmDevice
	llmFD, openErr := k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
	if openErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, llmDevice,
			fmt.Errorf("LLM device open failed: %w", openErr), types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	// All allocations and Open succeeded — now safe to clean up old process and
	// procHistory entry (skip entirely when forking, so original keeps running).
	k.cleanupOldProcessAndHistory(oldProc, uuid, opts.Fork)

	if proc.cancel != nil {
		proc.cancel() // release ctx created by NewProcess (no listeners yet, but be defensive)
	}
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = gctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	proc.mu.Lock()
	proc.SuspendReason = cp.SuspendReason
	proc.mu.Unlock()

	spawnOpts := SpawnOpts{Model: cp.Model, StartStep: startStep}

	k.emitEvent(proc, "Resume", map[string]any{
		"uuid":       uuid,
		"from_step":  startStep,
		"provider":   cp.Provider,
		"model":      cp.Model,
		"checkpoint": cpData.LastStep,
		"fork":       opts.Fork,
	}, proc.PID, nil, time.Since(start))

	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		_ = proc.Start()

		if cp.SuspendReason == "context_full" {
			proc.compactMu.Lock()
			compactOpts := rnixctx.CompactOpts{
				LLMCall:       k.BuildCompactLLMCall(proc),
				Trigger:       "context_full_resume",
				ReadFileState: k.SnapshotReadFileState(proc),
				ActiveSkills:  k.BuildActiveSkills(proc),
				ActivePlan:    k.extractActivePlan(proc.CtxID),
			}
			_, compactErr := k.ctxMgr.Compact(proc.CtxID, compactOpts)
			proc.compactMu.Unlock()
			if compactErr != nil {
				log.Printf("[kernel] pid=%d resume compact failed: %v", proc.PID, compactErr)
				k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full: resume compact failed", Err: compactErr})
				return
			}
			k.ClearReadFileState(proc)
			proc.mu.Lock()
			proc.SuspendReason = ""
			proc.mu.Unlock()
		}

		k.reasonStep(proc, llmFD, spawnOpts)
	})

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent, proc.Provider, proc.Model, proc.UUID)
	}

	log.Printf("[kernel] resume(checkpoint) uuid=%s new_pid=%d from_step=%d fork=%v (took %v)",
		uuid, proc.PID, startStep, opts.Fork, time.Since(start))

	return &ResumeResult{
		PID:             proc.PID,
		UUID:            proc.UUID,
		ResumedFromStep: startStep,
	}, nil
}

// resumeFromHistory handles resume for Dead/Zombie processes using disk data
// (steps.jsonl + process-meta.json + proc-info.json).
func (k *KernelImpl) resumeFromHistory(uuid string, opts ResumeOpts, start time.Time) (*ResumeResult, error) {
	baseDir := k.stepDataDir
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)

	// Story 42.3 — reject negative FromStep up-front (AC#2). Zero is valid (=
	// "no truncation"); positive values get range-checked after we read steps.jsonl.
	if opts.FromStep < 0 {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("from_step %d invalid (must be >= 0)", opts.FromStep), types.ErrInvalid)
	}

	// Read proc-info.json for provider/model/skills/intent
	procInfoPath := filepath.Join(stepsDir, "proc-info.json")
	procInfoData, err := os.ReadFile(procInfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("no data found for UUID %s: proc-info.json missing", uuid), types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("read proc-info.json: %w", err), types.ErrInternal)
	}

	var diskInfo procInfoDisk
	if err := json.Unmarshal(procInfoData, &diskInfo); err != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("parse proc-info.json: %w", err), types.ErrInternal)
	}

	// Read process-meta.json for system_prompt
	metaPath := filepath.Join(stepsDir, "process-meta.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("process-meta.json missing for UUID %s", uuid), types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("read process-meta.json: %w", err), types.ErrInternal)
	}

	var meta struct {
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("parse process-meta.json: %w", err), types.ErrInternal)
	}
	if meta.SystemPrompt == "" {
		// Without the system prompt the LLM has no role/capability context.
		// Reject the resume so the caller knows the snapshot is incomplete
		// rather than silently launching a "blank-prompt" agent.
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("system_prompt missing in process-meta.json for UUID %s", uuid), types.ErrInvalid)
	}

	// Read steps.jsonl to determine last step and rebuild messages.
	// Story 42.3: pass FromStep to apply truncation when > 0; totalSteps is
	// returned for out-of-range validation below.
	stepsPath := filepath.Join(stepsDir, "steps.jsonl")
	lastStep, messages, totalSteps, err := k.parseStepsJSONL(stepsPath, opts.FromStep)
	if err != nil {
		return nil, err
	}

	// Story 42.3 — out-of-range check (AC#2).
	if opts.FromStep > 0 && opts.FromStep > totalSteps {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("from_step %d exceeds total steps (actual: %d)", opts.FromStep, totalSteps),
			types.ErrInvalid)
	}

	// startStep semantics:
	//   - opts.FromStep > 0: truncate at FromStep, continue from FromStep+1.
	//   - opts.FromStep == 0: default → continue from lastStep+1.
	var startStep int
	if opts.FromStep > 0 {
		startStep = opts.FromStep + 1
	} else {
		startStep = lastStep + 1
	}
	if diskInfo.Provider == "" {
		// Don't silently fall back to "claude" — the original process may have
		// been running on cursor/openai/etc and the behavior/cost/model contract
		// would diverge silently.
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("provider missing in proc-info.json for UUID %s", uuid), types.ErrInvalid)
	}
	provider := diskInfo.Provider

	llmDevice, _, resolveErr := k.resolveLLMDevice(nil, provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", provider, resolveErr), types.ErrDriver)
	}

	// Locate old Dead/Zombie process if present (cleanup deferred until all
	// allocations succeed; skipped entirely when forking so original is preserved).
	var oldProc *Process
	if found, ok := k.GetProcessByUUID(uuid); ok {
		oldProc = found
	}

	// Create new process
	proc := NewProcess(0, diskInfo.Intent, diskInfo.Skills)
	if opts.Fork {
		// Fork mode: NewProcess already generated a fresh UUIDv7; assert it
		// differs from the origin to make the intent explicit. The UUIDv7
		// time-ordered generator makes a collision practically impossible,
		// but defensive checking guards against future refactors of NewProcess.
		if proc.UUID == "" || proc.UUID == uuid {
			return nil, NewSyscallError("Resume", proc.PID, "",
				fmt.Errorf("fork: new UUID generation produced empty or duplicate UUID %q", proc.UUID), types.ErrInternal)
		}
		proc.OriginUUID = uuid
	} else {
		// Non-fork: inherit original UUID
		proc.UUID = uuid
	}
	proc.Provider = provider
	proc.Model = diskInfo.Model
	proc.AllowedDevices = append([]string(nil), diskInfo.AllowedDevices...)
	proc.MaxSteps = diskInfo.MaxSteps
	proc.ResumedFromStep = startStep
	// Restore additional state captured on disk so the resumed process is not a
	// stripped-down shadow of the original (Edge Case Hunter Finding #5 & #10).
	proc.ParentUUID = diskInfo.ParentUUID
	proc.ContextBudget = diskInfo.ContextBudget
	proc.ContextWindow = diskInfo.ContextWindow
	proc.ComposeNode = diskInfo.ComposeNode
	proc.ComposeDeps = append([]string(nil), diskInfo.ComposeDeps...)
	proc.PipelineIndex = diskInfo.PipelineIndex
	proc.PipelineTotal = diskInfo.PipelineTotal
	proc.mu.Lock()
	proc.TokensUsed = diskInfo.TokensUsed
	proc.mu.Unlock()

	// Rebuild toolMap + nativeToolDefs from VFS DeviceRegistry + AllowedDevices.
	// Without this, resume processes have nil tool definitions and cannot perform
	// any tool calls (LLM gets req.Tools = nil). Mirrors observe.go:682-690.
	vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
	metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled, proc.DeferredSkills)
	allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
	allDefs = append(allDefs, vfsDefs...)
	allDefs = append(allDefs, metaDefs...)
	proc.mu.Lock()
	proc.nativeToolDefs = allDefs
	proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
	maps.Copy(proc.toolMap, vfsMap)
	maps.Copy(proc.toolMap, metaMap)
	proc.mu.Unlock()
	// Rehydrate SkillBodies / SkillDirs from the skillLoader (Edge Case Hunter
	// Finding #15). Without these, BuildPrompt degrades to skill names only and
	// claude-cli bundle symlink creation fails.
	if k.skillLoader != nil && len(proc.Skills) > 0 {
		bodies := make(map[string]string, len(proc.Skills))
		dirs := make(map[string]string, len(proc.Skills))
		for _, skillName := range proc.Skills {
			info, err := k.skillLoader(skillName)
			if err != nil || info == nil {
				log.Printf("[resume] skill %q not loadable: %v (continuing with degraded prompt)", skillName, err)
				continue
			}
			if info.Body != "" {
				body := info.Body
				if info.Dir != "" {
					body = "Base directory for this skill: " + info.Dir + "\n\n" + body
				}
				bodies[info.Manifest.Name] = body
				if info.Dir != "" {
					dirs[info.Manifest.Name] = info.Dir
				}
			}
		}
		proc.mu.Lock()
		proc.SkillBodies = bodies
		proc.SkillDirs = dirs
		proc.mu.Unlock()
	}
	for _, dev := range proc.AllowedDevices {
		if strings.HasPrefix(dev, "/dev/mcp/") || strings.HasPrefix(dev, "/mnt/mcp/") {
			proc.mcpDevicePaths = append(proc.mcpDevicePaths, dev)
		}
	}

	// Allocate context and rebuild from steps. Prefer the CtxSize saved on the
	// original process; only fall back to DefaultCtxSize when the disk snapshot
	// predates this field (or was zero) — otherwise N-step processes resumed
	// into a 256-slot context would silently lose messages past slot 256.
	resumeCtxSize := diskInfo.CtxSize
	if resumeCtxSize <= 0 {
		resumeCtxSize = DefaultCtxSize
	}
	proc.CtxSize = resumeCtxSize
	cid, allocErr := k.ctxMgr.CtxAlloc(resumeCtxSize)
	if allocErr != nil {
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context alloc: %w", allocErr), types.ErrInternal)
	}
	proc.CtxID = cid

	ctx, getErr := k.ctxMgr.GetContext(cid)
	if getErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context get: %w", getErr), types.ErrInternal)
	}

	// Rebuild context via Deserialize (same mechanism as checkpoint path)
	snapshot := struct {
		SystemPrompt string             `json:"system_prompt"`
		Messages     []rnixctx.Message  `json:"messages"`
		MaxSize      int                `json:"max_size"`
	}{
		SystemPrompt: meta.SystemPrompt,
		Messages:     messages,
		MaxSize:      resumeCtxSize,
	}
	snapJSON, _ := json.Marshal(snapshot)
	if err := ctx.Deserialize(snapJSON); err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context rebuild: %w", err), types.ErrInternal)
	}

	proc.mu.Lock()
	proc.StepTimeout = 5 * time.Minute
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	proc.PrimaryDevice = llmDevice
	llmFD, openErr := k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
	if openErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, llmDevice,
			fmt.Errorf("LLM device open failed: %w", openErr), types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	// All allocations and Open succeeded — now safe to clean up old process and
	// procHistory entry (skip entirely when forking, so original keeps running).
	k.cleanupOldProcessAndHistory(oldProc, uuid, opts.Fork)

	if proc.cancel != nil {
		proc.cancel() // release ctx created by NewProcess (no listeners yet, but be defensive)
	}
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = gctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	// Story 42.3 — synchronously persist the resumed process's proc-info.json
	// so downstream tooling (Dashboard lineage queries, test fixtures) can
	// observe origin_uuid / resumed_from_step before the first checkpoint
	// or reap. Best-effort: failure logged but not fatal — the reap path
	// will write a fresh snapshot on exit.
	if procInfo, piErr := k.GetProcInfo(proc.PID); piErr == nil && procInfo != nil {
		if saveErr := SaveProcInfo(baseDir, *procInfo); saveErr != nil {
			log.Printf("[resume] SaveProcInfo(new uuid=%s) failed: %v", proc.UUID, saveErr)
		}
	}

	// Determine if context_full exit — trigger compact after start.
	// Read ExitReason field (written by reap from proc.Exit.Reason); fall back
	// to legacy Result field for proc-info.json written before this field existed.
	exitReason := diskInfo.ExitReason
	if exitReason == "" {
		exitReason = diskInfo.Result
	}
	isContextFull := strings.Contains(strings.ToLower(exitReason), "context_full")

	spawnOpts := SpawnOpts{Model: diskInfo.Model, StartStep: startStep}

	k.emitEvent(proc, "ResumeFromHistory", map[string]any{
		"uuid":      uuid,
		"from_step": startStep,
		"provider":  provider,
		"model":     diskInfo.Model,
		"fork":      opts.Fork,
		"last_step": lastStep,
	}, proc.PID, nil, time.Since(start))

	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		_ = proc.Start()

		if isContextFull {
			compactStart := time.Now()
			proc.compactMu.Lock()
			compactOpts := rnixctx.CompactOpts{
				LLMCall:       k.BuildCompactLLMCall(proc),
				Trigger:       "context_full_resume",
				ReadFileState: k.SnapshotReadFileState(proc),
				ActiveSkills:  k.BuildActiveSkills(proc),
				ActivePlan:    k.extractActivePlan(proc.CtxID),
			}
			compactResult, compactErr := k.ctxMgr.Compact(proc.CtxID, compactOpts)
			proc.compactMu.Unlock()
			if compactErr != nil {
				log.Printf("[kernel] pid=%d resume(history) compact failed: %v", proc.PID, compactErr)
				k.emitEvent(proc, "Compact", map[string]any{
					"step":    startStep,
					"trigger": "context_full_resume",
					"error":   compactErr.Error(),
				}, nil, compactErr, time.Since(compactStart))
				k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full: resume compact failed", Err: compactErr})
				return
			}
			k.emitEvent(proc, "Compact", map[string]any{
				"step":           startStep,
				"trigger":        "context_full_resume",
				"pre_tokens":     compactResult.PreTokens,
				"post_tokens":    compactResult.PostTokens,
				"restored_items": compactResult.ItemsRestored,
				"duration_ms":    float64(compactResult.Duration.Microseconds()) / 1000.0,
			}, nil, nil, compactResult.Duration)
			k.ClearReadFileState(proc)
		}

		k.reasonStep(proc, llmFD, spawnOpts)
	})

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent, proc.Provider, proc.Model, proc.UUID)
	}

	log.Printf("[kernel] resume(history) uuid=%s new_pid=%d from_step=%d fork=%v (took %v)",
		uuid, proc.PID, startStep, opts.Fork, time.Since(start))

	return &ResumeResult{
		PID:             proc.PID,
		UUID:            proc.UUID,
		ResumedFromStep: startStep,
	}, nil
}

// parseStepsJSONL reads steps.jsonl and extracts the last step number and messages.
// Messages semantics: each StepRecord.Messages field already contains the cumulative
// message history at the time of that LLM call (see observe.go writeStepRecord).
// Therefore we use the LAST record's messages, NOT the concatenation of all records
// (which would yield O(N^2) duplicates).
//
// Story 42.3 — maxStep parameter:
//   - maxStep == 0  → no truncation (legacy behavior preserved)
//   - maxStep  > 0  → only records with `step <= maxStep` are considered when
//     building the resumed-from-step boundary; the returned `lastStep` is capped
//     at `maxStep` and `messages` is sourced from the highest step <= maxStep.
//
// Returns (lastStep, messages, totalSteps, err) where totalSteps is the highest
// step number observed across the entire file (independent of maxStep) — callers
// use it for out-of-range validation in ResumeWithOpts.
func (k *KernelImpl) parseStepsJSONL(path string, maxStep int) (int, []rnixctx.Message, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, 0, NewSyscallError("Resume", 0, "",
				fmt.Errorf("steps.jsonl not found"), types.ErrNotFound)
		}
		return 0, nil, 0, NewSyscallError("Resume", 0, "",
			fmt.Errorf("open steps.jsonl: %w", err), types.ErrInternal)
	}
	defer f.Close()

	var lastStep int           // highest step considered for messages (bounded by maxStep)
	var totalSteps int         // highest step across the entire file (for range validation)
	var lastMessagesRaw json.RawMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record stepRecordPartial
		if err := json.Unmarshal(line, &record); err != nil {
			return 0, nil, 0, NewSyscallError("Resume", 0, "",
				fmt.Errorf("parse steps.jsonl line: %w", err), types.ErrInternal)
		}
		if record.Step > totalSteps {
			totalSteps = record.Step
		}
		// Apply maxStep truncation. maxStep == 0 means "no truncation".
		if maxStep > 0 && record.Step > maxStep {
			continue
		}
		// Track the highest record within the [0..maxStep] window.
		if record.Step >= lastStep {
			lastStep = record.Step
			if len(record.Messages) > 0 {
				lastMessagesRaw = append(lastMessagesRaw[:0], record.Messages...)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, 0, NewSyscallError("Resume", 0, "",
			fmt.Errorf("scan steps.jsonl: %w", err), types.ErrInternal)
	}

	var messages []rnixctx.Message
	if len(lastMessagesRaw) > 0 {
		if err := json.Unmarshal(lastMessagesRaw, &messages); err != nil {
			// Legacy/corrupt schema: log loud, return empty messages so caller can decide.
			// Don't fail the resume — system_prompt + most recent steps may be enough.
			log.Printf("[resume] parseStepsJSONL: messages unmarshal failed at step %d: %v", lastStep, err)
		}
	}
	return lastStep, messages, totalSteps, nil
}

// stepRecordPartial is a minimal struct for parsing steps.jsonl during resume.
type stepRecordPartial struct {
	Step     int             `json:"step"`
	Messages json.RawMessage `json:"messages"`
}
