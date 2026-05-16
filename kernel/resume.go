package kernel

import (
	"bufio"
	gocontext "context"
	"encoding/json"
	"fmt"
	"log"
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
func (k *KernelImpl) Resume(uuid string) (*ResumeResult, error) {
	start := time.Now()

	// --- Validation phase (no state mutations) ---

	if uuid == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("empty UUID"), types.ErrInvalid)
	}

	// P5: Validate UUID format to prevent path traversal
	if !isValidUUIDFormat(uuid) {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("invalid UUID format: %s", uuid), types.ErrInvalid)
	}

	// P4: Serialize concurrent Resume calls
	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	// 1. Locate checkpoint directory
	baseDir := k.stepDataDir
	if baseDir == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("step data directory not configured"), types.ErrInternal)
	}
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)

	// 2. Read and validate checkpoint
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

	cp := cpData.ProcState

	// P7: Pre-flight check — reject resume when already at max step
	startStep := cpData.LastStep + 1
	if cp.MaxSteps > 0 && startStep > cp.MaxSteps {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("checkpoint at max step %d/%d — nothing to resume", cpData.LastStep, cp.MaxSteps), types.ErrInvalid)
	}

	// 3. Check if old process exists (P2: don't remove yet — validate first for AC#10)
	var oldProc *Process
	if found, ok := k.GetProcessByUUID(uuid); ok {
		oldState := found.GetState()
		if oldState != types.StateSuspended {
			return nil, NewSyscallError("Resume", found.PID, "",
				fmt.Errorf("process with UUID %s is in %s state, not suspended", uuid, oldState), types.ErrInvalid)
		}
		oldProc = found
	}

	// 4. Validate LLM provider BEFORE any mutations (AC#10: keep Suspended on failure)
	llmDevice, _, resolveErr := k.resolveLLMDevice(nil, cp.Provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", cp.Provider, resolveErr), types.ErrDriver)
	}

	// --- Mutation phase (all validations passed) ---

	// P2+P3: Remove old Suspended process AFTER validation; free its CtxID
	if oldProc != nil {
		if oldProc.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(oldProc.CtxID)
		}
		k.procTable.Delete(oldProc.PID)
		if queue, ok := k.msgQueues.LoadAndDelete(oldProc.PID); ok {
			queue.close()
		}
	}

	// 5. Clean procHistory to avoid UUID conflict (daemon restart scenario)
	k.procHistory.RemoveByUUID(uuid)

	// 6. Create new Process
	proc := NewProcess(0, cp.Intent, cp.Skills)
	proc.UUID = uuid // Preserve original UUID
	proc.Provider = cp.Provider
	proc.Model = cp.Model
	proc.AllowedDevices = append([]string(nil), cp.AllowedDevices...)
	proc.MaxSteps = cp.MaxSteps
	proc.mu.Lock()
	proc.TokensUsed = cp.UsedTokens
	proc.Budget = ProcessBudget{
		MaxTokens: cp.MaxTokens,
		MaxCost:   cp.MaxCost,
		UsedCost:  cp.UsedCost,
	}
	proc.mu.Unlock()

	// 7. Allocate context and deserialize snapshot
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

	// P8: stepsDir and checkpointErrCh are set by reasonStep — no need to set here

	// 8. StepTimeout: default 5 minutes (agent manifest not available during resume)
	proc.mu.Lock()
	proc.StepTimeout = 5 * time.Minute
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	// 9. Env snapshot comparison — warn on differences (non-fatal)
	if len(cp.EnvSnapshot) > 0 {
		k.compareEnvSnapshot(proc, cp.EnvSnapshot)
	}

	// 10. Open LLM device
	proc.PrimaryDevice = llmDevice
	llmFD, openErr := k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
	if openErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, llmDevice,
			fmt.Errorf("LLM device open failed: %w", openErr), types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil

	// Set up stream handler
	k.setupDriverStreamHandler(proc, llmFD)

	// P1: Cancel the context that NewProcess() created before overwriting
	proc.cancel()
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = gctx

	// 11. Register process in table
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	// 12. Restore SuspendReason for resume-time compact decision
	proc.mu.Lock()
	proc.SuspendReason = cp.SuspendReason
	proc.mu.Unlock()

	// 13. Launch reasoning goroutine from checkpoint step + 1
	opts := SpawnOpts{
		Model:     cp.Model,
		StartStep: startStep,
	}

	k.emitEvent(proc, "Resume", map[string]any{
		"uuid":       uuid,
		"from_step":  startStep,
		"provider":   cp.Provider,
		"model":      cp.Model,
		"checkpoint": cpData.LastStep,
	}, proc.PID, nil, time.Since(start))

	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		_ = proc.Start() // Created → Running

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

		k.reasonStep(proc, llmFD, opts)
	})

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent, proc.Provider, proc.Model, proc.UUID)
	}

	log.Printf("[kernel] resume uuid=%s new_pid=%d from_step=%d (took %v)", uuid, proc.PID, startStep, time.Since(start))

	return &ResumeResult{
		PID:             proc.PID,
		UUID:            uuid,
		ResumedFromStep: startStep,
	}, nil
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
			// History-based resume
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

	// Prefer checkpoint path if checkpoint.json exists
	cpPath := filepath.Join(stepsDir, "checkpoint.json")
	if _, err := os.Stat(cpPath); err == nil {
		return k.resumeFromCheckpoint(uuid, opts, start)
	}

	return k.resumeFromHistory(uuid, opts, start)
}

// resumeFromCheckpoint handles the checkpoint-based resume path (original 30-4 logic).
func (k *KernelImpl) resumeFromCheckpoint(uuid string, opts ResumeOpts, start time.Time) (*ResumeResult, error) {
	baseDir := k.stepDataDir
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)

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
		if oldProc.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(oldProc.CtxID)
		}
		k.procTable.Delete(oldProc.PID)
		if queue, ok := k.msgQueues.LoadAndDelete(oldProc.PID); ok {
			queue.close()
		}
	}

	k.procHistory.RemoveByUUID(uuid)

	proc := NewProcess(0, cp.Intent, cp.Skills)
	if opts.Fork {
		proc.OriginUUID = uuid
	} else {
		proc.UUID = uuid
	}
	proc.Provider = cp.Provider
	proc.Model = cp.Model
	proc.AllowedDevices = append([]string(nil), cp.AllowedDevices...)
	proc.MaxSteps = cp.MaxSteps
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

	proc.cancel()
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

	// Read steps.jsonl to determine last step and rebuild messages
	stepsPath := filepath.Join(stepsDir, "steps.jsonl")
	lastStep, messages, err := k.parseStepsJSONL(stepsPath)
	if err != nil {
		return nil, err
	}

	startStep := lastStep + 1
	provider := diskInfo.Provider
	if provider == "" {
		provider = "claude"
	}

	llmDevice, _, resolveErr := k.resolveLLMDevice(nil, provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", provider, resolveErr), types.ErrDriver)
	}

	// Remove old Dead/Zombie process from procTable if present
	if found, ok := k.GetProcessByUUID(uuid); ok {
		if found.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(found.CtxID)
		}
		k.procTable.Delete(found.PID)
		if queue, ok := k.msgQueues.LoadAndDelete(found.PID); ok {
			queue.close()
		}
	}

	k.procHistory.RemoveByUUID(uuid)

	// Create new process
	proc := NewProcess(0, diskInfo.Intent, diskInfo.Skills)
	if opts.Fork {
		proc.OriginUUID = uuid
	} else {
		proc.UUID = uuid
	}
	proc.Provider = provider
	proc.Model = diskInfo.Model
	proc.AllowedDevices = append([]string(nil), diskInfo.AllowedDevices...)
	proc.MaxSteps = diskInfo.MaxSteps
	proc.mu.Lock()
	proc.TokensUsed = diskInfo.TokensUsed
	proc.mu.Unlock()

	// Allocate context and rebuild from steps
	resumeCtxSize := DefaultCtxSize
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

	proc.cancel()
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = gctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	// Determine if context_full exit — trigger compact after start
	isContextFull := strings.Contains(strings.ToLower(diskInfo.Result), "context_full")

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
				log.Printf("[kernel] pid=%d resume(history) compact failed: %v", proc.PID, compactErr)
				k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full: resume compact failed", Err: compactErr})
				return
			}
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
func (k *KernelImpl) parseStepsJSONL(path string) (int, []rnixctx.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("steps.jsonl not found"), types.ErrNotFound)
		}
		return 0, nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("open steps.jsonl: %w", err), types.ErrInternal)
	}
	defer f.Close()

	var lastStep int
	var messages []rnixctx.Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record stepRecordPartial
		if err := json.Unmarshal(line, &record); err != nil {
			return 0, nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("parse steps.jsonl line: %w", err), types.ErrInternal)
		}
		if record.Step > lastStep {
			lastStep = record.Step
		}
		// Extract messages from the record's Messages field
		if len(record.Messages) > 0 {
			var msgs []rnixctx.Message
			if err := json.Unmarshal(record.Messages, &msgs); err == nil {
				messages = append(messages, msgs...)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("scan steps.jsonl: %w", err), types.ErrInternal)
	}

	return lastStep, messages, nil
}

// stepRecordPartial is a minimal struct for parsing steps.jsonl during resume.
type stepRecordPartial struct {
	Step     int             `json:"step"`
	Messages json.RawMessage `json:"messages"`
}
