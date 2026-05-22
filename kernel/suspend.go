package kernel

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// suspendProcess performs the shared suspend cleanup logic:
// closes all VFS FDs, drains checkpoint channel, transitions to Suspended.
// Called by both Suspend (external) and selfSuspend (internal).
func (k *KernelImpl) suspendProcess(proc *Process, reason string, exitCode int) error {
	// Extract FD references under lock, then close outside lock to prevent
	// deadlock if test timeout panic triggers defer → GetState → proc.mu.
	proc.mu.Lock()
	proc.SuspendReason = reason
	fds := make([]vfs.VFSFile, 0, len(proc.FDTable))
	for fd, f := range proc.FDTable {
		fds = append(fds, f)
		delete(proc.FDTable, fd)
	}
	proc.mu.Unlock()

	// Close FDs outside lock
	for _, f := range fds {
		if f != nil {
			f.Close()
		}
	}

	// Drain checkpoint error channel (Story 30.2)
	if proc.checkpointErrCh != nil {
		select {
		case <-proc.checkpointErrCh:
		default:
		}
	}

	// State transition Running → Suspended
	if err := proc.Suspend(); err != nil {
		return NewSyscallError("Suspend", proc.PID, "", err, types.ErrInternal)
	}

	// Emit suspend event
	k.emitEvent(proc, "Suspend", map[string]any{
		"pid":    proc.PID,
		"reason": reason,
	}, nil, nil, 0)

	// Set exit status so callers can inspect it.
	exit := ExitStatus{Code: exitCode, Reason: "suspended: " + reason}
	proc.mu.Lock()
	proc.Exit = &exit
	proc.mu.Unlock()

	// Story 44.3 AC#2 — persist proc-info.json synchronously so the disk
	// snapshot reflects state=suspended + suspend_reason before the next
	// daemon restart. Best-effort: a write failure must NOT block the state
	// transition (44.1 dashboard p / Ctrl+C hot path). Mirrors the
	// reapSuspendedProcess SaveProcInfo block.
	if info, ierr := k.GetProcInfo(proc.PID); ierr == nil && info != nil {
		if perr := SaveProcInfo(k.stepDataDir, *info); perr != nil {
			log.Printf("[suspend] proc-info.json write error pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, perr)
		}
	}

	// Notify callbacks
	if k.callbacks != nil {
		k.callbacks.OnComplete(proc.PID, "", exit)
	}

	return nil
}

// notifySuspendDone writes to proc.Done AFTER the calling goroutine has finished
// all its work in the reasoning loop. This must be called at the top level of the
// goroutine exit path (not from within suspendProcess) to avoid a race where
// Wait() → reapProcess → wg.Wait() deadlocks with the still-running goroutine.
func notifySuspendDone(proc *Process) {
	exit := ExitStatus{Code: ExitSuspended, Reason: "suspended"}
	proc.mu.Lock()
	if proc.Exit != nil {
		exit = *proc.Exit
	}
	proc.mu.Unlock()
	select {
	case proc.Done <- exit:
	default:
	}
}

// Suspend externally suspends a Running process.
// Marks the suspend request flag, cancels context, waits for the reasoning
// goroutine to exit, then performs shared cleanup.
func (k *KernelImpl) Suspend(pid types.PID) error {
	start := time.Now()

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("Suspend", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state != types.StateRunning {
		return NewSyscallError("Suspend", pid, "", fmt.Errorf("cannot suspend %s process", state), types.ErrInvalid)
	}

	// Mark suspend requested (reasonStep checks this to skip finishProcess)
	proc.suspendRequested.Store(true)

	// Pre-set exit status so defer's notifySuspendDone sends the correct reason
	exit := ExitStatus{Code: ExitSuspended, Reason: "suspended: user_suspended"}
	proc.mu.Lock()
	proc.SuspendReason = "user_suspended"
	proc.Exit = &exit
	proc.mu.Unlock()

	// Cancel context to exit the reasoning loop
	proc.Cancel()

	// Wait for reasoning goroutine to exit (defer fires notifySuspendDone with correct Exit)
	proc.wg.Wait()

	if err := k.suspendProcess(proc, "user_suspended", ExitSuspended); err != nil {
		return err
	}

	log.Printf("[kernel] pid=%d suspended (took %v)", pid, time.Since(start))
	return nil
}

// selfSuspend is called from within the reasoning loop goroutine when the
// process decides to suspend itself (e.g., loop detection, budget exhaustion).
// Unlike Suspend(), it does NOT call Cancel+Wait since the goroutine is the caller.
// Done notification is deferred to reasonStep's exit path via notifySuspendDone.
func (k *KernelImpl) selfSuspend(proc *Process, reason string, exitCode int) error {
	proc.suspendRequested.Store(true)
	return k.suspendProcess(proc, reason, exitCode)
}

// reapSuspendedProcess cleans up a Suspended process that has been killed.
// Simpler than reapProcess — no goroutine to wait, FDs already closed by Suspend.
// Checkpoint files are preserved for potential future inspection.
func (k *KernelImpl) reapSuspendedProcess(proc *Process) {
	proc.reapOnce.Do(func() {
		// Close debug/log channels
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

		// Close message queue
		if queue, ok := k.msgQueues.LoadAndDelete(proc.PID); ok {
			queue.close()
		}

		// Clean up groups and signal state
		k.removeFromAllGroups(proc.PID, proc)
		proc.ClearSignalState()
		proc.ClearThreads()
		proc.ClearCoroutines()

		// Free context
		if k.ctxMgr != nil && proc.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(proc.CtxID)
		}

		// Close step/event writers
		proc.mu.Lock()
		sw := proc.stepWriter
		proc.stepWriter = nil
		ew := proc.eventWriter
		proc.eventWriter = nil
		proc.mu.Unlock()
		if ew != nil {
			_ = ew.Close()
		}
		if sw != nil {
			_ = sw.Close()
		}

		// Persist proc-info for history
		baseDir := k.stepDataDir
		if baseDir == "" && proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
			baseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
		}
		if info, err := k.GetProcInfo(proc.PID); err == nil {
			if err := SaveProcInfo(baseDir, *info); err != nil {
				log.Printf("[reaper] proc-info.json write error pid=%d: %v", proc.PID, err)
			}
		}
	})
}

// killSuspendedProcess transitions a Suspended process to Dead, notifies waiters,
// and reaps resources. Shared by Kill() and Signal() for suspended process handling.
func (k *KernelImpl) killSuspendedProcess(proc *Process, sig types.Signal, syscall string, start time.Time) error {
	if err := proc.Transition(types.StateDead); err != nil {
		return NewSyscallError(syscall, proc.PID, "", err, types.ErrInternal)
	}
	exit := ExitStatus{Code: 1, Reason: fmt.Sprintf("%s while suspended", sig.String())}
	proc.mu.Lock()
	proc.Exit = &exit
	proc.DeadAt = time.Now()
	// Story 44.2 review P1: wake any ScriptExecutor parked on ResumedCh().
	// Without this close, a SIGKILL on a Suspended script-runner leaves the
	// executor blocked forever on `<-resumedCh` (scriptCtx is also not
	// cancelled on this path), leaking the handleExecScript goroutine + FDs.
	oldCh := proc.resumedCh
	oldClose := proc.resumedClose
	proc.mu.Unlock()
	oldClose.Do(func() { close(oldCh) })
	k.emitEvent(proc, syscall, map[string]any{
		"pid":    proc.PID,
		"signal": sig.String(),
		"action": "killed_suspended",
	}, nil, nil, time.Since(start))
	// Notify waiters so Wait() does not hang
	select {
	case proc.Done <- exit:
	default:
	}
	proc.closeTerminated() // unblock twoPhaseShutdown if it's watching this process
	k.reapSuspendedProcess(proc)
	return nil
}
