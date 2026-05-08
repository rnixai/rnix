package kernel

import (
	gocontext "context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ResumeResult holds the output of a successful Resume call.
type ResumeResult struct {
	PID             types.PID
	UUID            string
	ResumedFromStep int
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

	// 12. Launch reasoning goroutine from checkpoint step + 1
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
