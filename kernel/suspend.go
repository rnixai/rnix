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
func (k *KernelImpl) suspendProcess(proc *Process, reason string) error {
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
	exit := ExitStatus{Code: 2, Reason: "suspended: " + reason}
	proc.mu.Lock()
	proc.Exit = &exit
	proc.mu.Unlock()

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
	exit := ExitStatus{Code: 2, Reason: "suspended"}
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

	// Cancel context to exit the reasoning loop
	proc.Cancel()

	// Wait for reasoning goroutine to exit
	proc.wg.Wait()

	if err := k.suspendProcess(proc, "user_suspended"); err != nil {
		return err
	}

	// For external suspend, goroutine is already done — safe to notify Done.
	notifySuspendDone(proc)

	log.Printf("[kernel] pid=%d suspended (took %v)", pid, time.Since(start))
	return nil
}

// selfSuspend is called from within the reasoning loop goroutine when the
// process decides to suspend itself (e.g., loop detection, budget exhaustion).
// Unlike Suspend(), it does NOT call Cancel+Wait since the goroutine is the caller.
// Done notification is deferred to reasonStep's exit path via notifySuspendDone.
func (k *KernelImpl) selfSuspend(proc *Process, reason string) error {
	proc.suspendRequested.Store(true)
	return k.suspendProcess(proc, reason)
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
