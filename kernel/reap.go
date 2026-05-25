package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel/memory"
)

// DeadProcessTTL is how long a Dead process stays in the procTable before removal.
const DeadProcessTTL = 60 * time.Second

// reapProcess performs the complete resource release sequence for a zombie process.
// It is idempotent — proc.reapOnce ensures only one caller executes the cleanup,
// even if both Wait and the reaper goroutine try concurrently.
func (k *KernelImpl) reapProcess(proc *Process) {
	proc.reapOnce.Do(func() {
		proc.mu.Lock()
		traceID := proc.TraceID
		exit := proc.Exit
		proc.mu.Unlock()

		if traceID != "" && k.spanRecorder != nil {
			status := debug.SpanOK
			if exit != nil && exit.Err != nil {
				status = debug.SpanERROR
			}
			var se *SyscallError
			var de *types.DriverError
			if exit != nil && exit.Err != nil &&
				((errors.As(exit.Err, &se) && se.Code == types.ErrTimeout) ||
					(errors.As(exit.Err, &de) && de.Code == types.ErrTimeout) ||
					strings.Contains(strings.ToLower(exit.Reason), "timeout")) {
				status = debug.SpanTIMEOUT
			}
			k.spanRecorder.EndSpan(proc.PID, status)
		}

		// Auto-stop recording if active (Story 14.1)
		if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
			_ = k.recordMgr.StopRecording(proc.PID)
		}

		// Handle orphan children before removing parent from table
		k.handleOrphanChildren(proc)

		// Resource release sequence (strict order per architecture doc):
		// 1. cancel() — ensure context cancelled (idempotent)
		proc.Cancel()

		// 2. wg.Wait() — wait for goroutine to complete (internal defer executes CloseAll)
		proc.wg.Wait()

		// 2.5 Write process-meta.json and close StepWriter + EventWriter (Story 27.1 AC-8)
		proc.mu.Lock()
		sw := proc.stepWriter
		proc.stepWriter = nil
		ew := proc.eventWriter
		proc.eventWriter = nil
		fsp := proc.FinalSystemPrompt
		toolDefs := proc.nativeToolDefs
		proc.mu.Unlock()
		if ew != nil {
			if err := ew.Close(); err != nil {
				log.Printf("[event_writer] close error pid=%d: %v", proc.PID, err)
			}
		}
		if sw != nil {
			// Write process-meta.json to the same directory
			metaDir := filepath.Dir(sw.file.Name())
			meta := struct {
				PID          types.PID `json:"pid"`
				SystemPrompt string    `json:"system_prompt"`
				ToolDefs     []any     `json:"tool_defs"`
			}{
				PID:          proc.PID,
				SystemPrompt: fsp,
			}
			if toolDefs != nil {
				meta.ToolDefs = make([]any, len(toolDefs))
				for i, td := range toolDefs {
					meta.ToolDefs[i] = td
				}
			}
			if metaJSON, err := json.Marshal(meta); err == nil {
				if err := os.WriteFile(filepath.Join(metaDir, "process-meta.json"), metaJSON, 0o644); err != nil {
					log.Printf("[step_writer] process-meta.json write error pid=%d: %v", proc.PID, err)
				}
			} else {
				log.Printf("[step_writer] process-meta.json marshal error pid=%d: %v", proc.PID, err)
			}
			if err := sw.Close(); err != nil {
				log.Printf("[step_writer] close error pid=%d: %v", proc.PID, err)
			}
		}

		// 2.6 Drain checkpoint error channel (Story 30.2)
		// Don't close the channel — a goroutine may still be writing.
		// It uses default-send so it won't block.
		if proc.checkpointErrCh != nil {
			select {
			case <-proc.checkpointErrCh:
			default:
			}
		}

		// 2.7 Clean up scratchpad directory if it exists and is empty
		proc.mu.Lock()
		scratchDir := proc.scratchDir
		proc.mu.Unlock()
		if scratchDir != "" {
			_ = os.Remove(scratchDir) // only removes if empty; non-empty preserved for user
		}

		// 3. close(DebugChan) — nil out under lock first to prevent races with emitEvent
		proc.mu.Lock()
		ch := proc.DebugChan
		proc.DebugChan = nil
		lch := proc.LogChan
		proc.LogChan = nil
		proc.mu.Unlock()
		if ch != nil {
			close(ch)
		}
		if lch != nil {
			close(lch)
		}

		// 4. msgQueue.close() — close message queue, unblock pending Recv (Story 6.1)
		if queue, ok := k.msgQueues.LoadAndDelete(proc.PID); ok {
			queue.close()
		}

		// 5. IPC persist cleanup — on normal exit, remove persisted messages
		if proc.IPCPersist && exit != nil && exit.Code == 0 {
			baseDir := k.resolveBaseDir(proc)
			if baseDir != "" {
				_ = removePersistedMessages(baseDir, proc.UUID)
			}
		}

		// 6. removeFromAllGroups — clean up process group memberships (Story 6.3)
		k.removeFromAllGroups(proc.PID, proc)

		// 7. ClearSignalState — clean up signal handlers/blocked/pending/resume (Story 6.4)
		proc.ClearSignalState()

		// 7. ClearThreads — cancel all threads and wait for completion (Story 6.5)
		proc.ClearThreads()

		// 8. ClearCoroutines — clean up all coroutines (Story 6.5)
		proc.ClearCoroutines()

		// Resolve base directory for persistence (used by steps 8.5 and 12)
		baseDir := k.stepDataDir
		if baseDir == "" && proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
			baseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
		}

		// 8.5 Snapshot context profile before freeing (for dead process heatmap)
		if k.ctxMgr != nil && proc.CtxID > 0 {
			k.saveCtxProfile(proc, baseDir)
		}

		// 9. CtxFree(CtxID) — release context space
		_ = k.ctxMgr.CtxFree(proc.CtxID)

		// 10. Reap() — Zombie → Dead state transition
		_ = proc.Reap()

		// 11. Set DeadAt timestamp (TTL cleanup will remove from procTable later)
		proc.mu.Lock()
		proc.DeadAt = time.Now()
		proc.mu.Unlock()

		// 12. Persist proc-info.json for history recovery after daemon restart
		if info, err := k.GetProcInfo(proc.PID); err == nil {
			if err := SaveProcInfo(baseDir, *info); err != nil {
				log.Printf("[reaper] proc-info.json write error pid=%d: %v", proc.PID, err)
			}
		}
	})

	// 13. Writeback async knowledge extraction (Story 35.3, Architecture Decision 36)
	// Runs AFTER reapOnce.Do completes — stepsDir and proc-info.json are finalized.
	// Uses writebackOnce to ensure only one invocation per process even if reapProcess
	// is called multiple times (from both Wait and reaper goroutine).
	if k.writebackWorker != nil {
		proc.writebackOnce.Do(func() {
			proc.mu.Lock()
			exit := proc.Exit
			proc.mu.Unlock()
			if exit == nil {
				return
			}
			stepsDir := k.resolveStepsDir(proc)
			if stepsDir == "" {
				return
			}
			if memory.ShouldExtract(k.writebackWorker.Config(), exit.Code, exit.Reason, stepsDir) {
				k.writebackWorker.Submit(memory.NewWritebackJob(proc.UUID, stepsDir, exit.Code, exit.Reason))
			}
		})
	}
}

// handleOrphanChildren processes a dying parent's children:
// - Running children are reparented to PID 0 (init/kernel)
// - Zombie children are pushed to reapCh for auto-reap
func (k *KernelImpl) handleOrphanChildren(parent *Process) {
	children := parent.GetChildren()
	for _, childPID := range children {
		child, ok := k.GetProcess(childPID)
		if !ok {
			continue // child already removed
		}

		state := child.GetState()
		switch state {
		case types.StateRunning, types.StateCreated, types.StateSuspended:
			// Reparent to init (PID 0)
			child.mu.Lock()
			child.PPID = 0
			child.mu.Unlock()

			// Emit reparent SyscallEvent
			k.emitEvent(child, "Reparent", map[string]any{
				"child_pid": childPID,
				"old_ppid":  parent.PID,
				"new_ppid":  types.PID(0),
			}, nil, nil, 0)

		case types.StateZombie:
			// Orphan zombie → push to reapCh for auto-reap
			select {
			case k.reapCh <- childPID:
			default:
				// Fallback when reapCh is full or the reaper has already exited.
				// Track via reaperWg so Shutdown waits for this goroutine before
				// returning — otherwise reapProcess may still be writing
				// process-meta.json after t.TempDir() cleanup runs.
				k.reaperWg.Go(func() {
					k.reapProcess(child)
				})
			}
		}
		// StateDead: already cleaned up, ignore
	}
}

// Wait blocks until the target process enters Zombie state, then performs the
// complete resource release sequence and returns the ExitStatus.
// Returns *SyscallError with ErrNotFound if the PID does not exist.
func (k *KernelImpl) Wait(pid types.PID) (ExitStatus, error) {
	start := time.Now()

	proc, ok := k.GetProcess(pid)
	if !ok {
		return ExitStatus{}, NewSyscallError("Wait", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}

	// Emit entry event
	k.emitEvent(proc, "Wait", map[string]any{
		"pid": pid,
	}, nil, nil, 0)

	// Block until process completes (finishProcess writes to Done channel)
	exit := <-proc.Done

	// Emit exit event BEFORE reapProcess closes DebugChan
	k.emitEvent(proc, "Wait", map[string]any{
		"pid":    pid,
		"action": "completed",
	}, exit, nil, time.Since(start))

	// Shared reap logic (idempotent via reapOnce)
	k.reapProcess(proc)

	return exit, nil
}

// WaitChildInReason is the cancellable + heartbeat-aware variant of Wait used
// by the parent's reasonStep when handling ActionSpawn. Unlike Wait it:
//
//   - selects on parent.ctx.Done() so a SuspendSubtree / Kill on the parent can
//     unwind the reasonStep goroutine instead of deadlocking proc.wg.Wait()
//     (which suspendOneForSubtree depends on for state transitions)
//   - periodically calls parent.TouchHeartbeat() so the heartbeat monitor does
//     not flag the parent as STALLED while it is legitimately waiting on a
//     long-running child (the heartbeat monitor's `primaryDevice == ""` skip
//     only covers script-runners, not reasoning parents)
//
// Returns (childExit, cancelled). When cancelled is true the caller MUST
// return from reasonStep without appending tool results — the loop's
// top-of-iteration ctx check then drives the clean suspend/exit path.
//
// On normal child completion this performs the same emit + reapProcess as
// Wait; on cancellation the child is left in its current state for the
// SuspendSubtree walker (or the reaper) to clean up.
func (k *KernelImpl) WaitChildInReason(parent *Process, childPID types.PID) (ExitStatus, bool) {
	start := time.Now()

	child, ok := k.GetProcess(childPID)
	if !ok {
		return ExitStatus{}, false
	}

	k.emitEvent(child, "Wait", map[string]any{
		"pid": childPID,
	}, nil, nil, 0)

	// Heartbeat refresh cadence: third of StepTimeout, capped at 30s. The
	// cap matters when StepTimeout is large (10m+) — refresh every 30s is
	// still cheap (single mutex + time.Now) and keeps the LastHeartbeat-vs-
	// StepTimeout gap well below the threshold even under skewed clocks.
	hbInterval := parent.StepTimeout / 3
	if hbInterval <= 0 || hbInterval > 30*time.Second {
		hbInterval = 30 * time.Second
	}
	ticker := time.NewTicker(hbInterval)
	defer ticker.Stop()

	parentCtxDone := parent.CancelledCh()

	for {
		select {
		case exit := <-child.Done:
			k.emitEvent(child, "Wait", map[string]any{
				"pid":    childPID,
				"action": "completed",
			}, exit, nil, time.Since(start))
			k.reapProcess(child)
			return exit, false

		case <-parentCtxDone:
			k.emitEvent(child, "Wait", map[string]any{
				"pid":    childPID,
				"action": "parent_cancelled",
			}, nil, nil, time.Since(start))
			return ExitStatus{}, true

		case <-ticker.C:
			parent.TouchHeartbeat()
		}
	}
}

// Reap triggers cleanup of a zombie process by PID.
// Safe to call even if the process has already been reaped (idempotent via reapOnce).
// This is used by the IPC server to reap top-level processes (PPID=0) after
// spawn streaming completes, since no CLI Wait() call exists in daemon mode.
func (k *KernelImpl) Reap(pid types.PID) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return
	}
	if proc.GetState() == types.StateZombie {
		k.reapProcess(proc)
	}
}

// startReaper launches the background goroutine that auto-reaps orphan zombies
// and periodically cleans up expired Dead processes.
func (k *KernelImpl) startReaper() {
	// Start writeback worker if available (Story 35.3)
	if k.writebackWorker != nil {
		k.writebackWorker.Start()
	}

	k.deadTicker = time.NewTicker(10 * time.Second)
	k.reaperWg.Go(func() {
		for {
			select {
			case pid := <-k.reapCh:
				proc, ok := k.GetProcess(pid)
				if !ok {
					continue // already reaped by Wait
				}
				if proc.GetState() == types.StateZombie {
					k.reapProcess(proc)
				}
			case <-k.deadTicker.C:
				k.cleanupExpiredDead(DeadProcessTTL)
			case <-k.stopCh:
				k.deadTicker.Stop()
				// Drain remaining PIDs before exiting
				for {
					select {
					case pid := <-k.reapCh:
						if proc, ok := k.GetProcess(pid); ok && proc.GetState() == types.StateZombie {
							k.reapProcess(proc)
						}
					default:
						return
					}
				}
			}
		}
	})
}

// cleanupExpiredDead removes Dead processes whose DeadAt exceeds the TTL.
func (k *KernelImpl) cleanupExpiredDead(ttl time.Duration) {
	var toRemove []types.PID
	k.procTable.Range(func(pid types.PID, proc *Process) bool {
		proc.mu.Lock()
		state := proc.State
		deadAt := proc.DeadAt
		proc.mu.Unlock()
		if state == types.StateDead && !deadAt.IsZero() && time.Since(deadAt) > ttl {
			toRemove = append(toRemove, pid)
		}
		return true
	})
	for _, pid := range toRemove {
		if info, err := k.GetProcInfo(pid); err == nil {
			k.procHistory.Add(*info)
			// Best-effort persist (safety net if reapProcess didn't write it)
			baseDir := k.stepDataDir
			if baseDir == "" {
				if proc, ok := k.GetProcess(pid); ok && proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
					baseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
				}
			}
			if err := SaveProcInfo(baseDir, *info); err != nil {
				log.Printf("[reaper] proc-info.json write error pid=%d: %v", pid, err)
			}
		}
		k.RemoveProcess(pid)
	}
}

// resolveStepsDir returns the .rnix/data/steps/<uuid>/ directory for a process.
func (k *KernelImpl) resolveStepsDir(proc *Process) string {
	baseDir := k.stepDataDir
	if baseDir == "" && proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
		baseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
	}
	if baseDir == "" || proc.UUID == "" {
		return ""
	}
	return filepath.Join(baseDir, "data", "steps", proc.UUID)
}

// saveCtxProfile snapshots the context profile to disk before CtxFree.
// Best-effort: errors are logged but do not halt reaping.
func (k *KernelImpl) saveCtxProfile(proc *Process, baseDir string) {
	if baseDir == "" || proc.UUID == "" {
		return
	}
	rawCtx, err := k.ctxMgr.CtxRead(proc.CtxID, 0, 0)
	if err != nil {
		return
	}
	var ctxData debug.ContextData
	if err := json.Unmarshal(rawCtx, &ctxData); err != nil {
		return
	}
	proc.mu.Lock()
	tokensUsed := proc.TokensUsed
	contextBudget := proc.ContextBudget
	proc.mu.Unlock()

	result := debug.AnalyzeContext(&ctxData, proc.PID, proc.CtxID, tokensUsed, contextBudget)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Join(baseDir, "data", "steps", proc.UUID)
	_ = os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "ctx-profile.json"), data, 0o644); err != nil {
		log.Printf("[reaper] ctx-profile.json write error pid=%d: %v", proc.PID, err)
	}
}

// Shutdown stops the reaper goroutine, unmounts all MCP servers,
// closes all active recordings, and waits for exit.
// Safe to call multiple times — only the first call closes stopCh.
func (k *KernelImpl) Shutdown() {
	k.shutdownOnce.Do(func() {
		// Stop writeback worker first — drain pending jobs (Story 35.3)
		if k.writebackWorker != nil {
			k.writebackWorker.Stop()
		}
		// Stop heartbeat monitor before reaper (Story 30.6)
		if k.heartbeatMonitor != nil {
			k.heartbeatMonitor.Stop()
		}
		// Close all active recordings before stopping reaper
		if k.recordMgr != nil {
			k.recordMgr.CloseAll()
		}
		// Unmount all MCP servers before stopping reaper
		if k.mountMgr != nil {
			_ = k.mountMgr.UnmountAll()
		}
		if k.deadTicker != nil {
			k.deadTicker.Stop()
		}
		close(k.stopCh)
	})
	k.reaperWg.Wait()

	// Drain all process goroutines (reasoning loops + async checkpoint writers).
	// This prevents file-I/O races when tests use t.TempDir() as the step data
	// directory: without this, a checkpoint goroutine spawned by asyncWriteCheckpoint
	// may still be writing files when Go's TempDir cleanup tries to remove the dir.
	// proc.Cancel() is idempotent — safe to call even if already cancelled.
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		proc.Cancel()
		proc.wg.Wait()
		return true
	})

	// Persist all remaining processes that weren't reaped before shutdown.
	// This catches Running/Created processes that were cancelled above but whose
	// reapProcess never ran because the reaper was already stopped.
	k.procTable.Range(func(pid types.PID, proc *Process) bool {
		baseDir := k.stepDataDir
		if baseDir == "" && proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
			baseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
		}
		if baseDir == "" {
			return true
		}
		info, err := k.GetProcInfo(pid)
		if err != nil {
			return true
		}
		// Best-effort persist: SaveProcInfo skips if file already exists and is fresh.
		if err := SaveProcInfo(baseDir, *info); err != nil {
			log.Printf("[shutdown] proc-info.json write error pid=%d: %v", pid, err)
		}
		return true
	})
}
