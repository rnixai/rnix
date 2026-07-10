package kernel

import (
	"fmt"
	"log"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// suspendProcess performs the shared suspend cleanup logic:
// closes all VFS FDs, drains checkpoint channel, transitions to Suspended.
// Called by both Suspend (external) and selfSuspend (internal).
//
// Story 44.5 v2 review Decision 1 — SuspendReason is written AFTER the
// state transition succeeds. The previous "write reason before transition"
// pattern leaked SuspendReason onto Zombie/Dead processes when finishProcess
// won a race against SIGPAUSE (the happy-exit race: reasonStep completed
// normally with exit.Code=0 just as SIGPAUSE arrived; AC3's
// `exit.Code != 0 → clear SuspendReason` did not fire because the exit was
// a successful one, leaving SuspendReason=user_paused on a Zombie/Dead
// snapshot that violated the AC6 invariant matrix). Writing after the
// transition guarantees: transition succeeded → state=Suspended +
// SuspendReason=reason (consistent); transition failed → state unchanged +
// SuspendReason untouched (no leak).
func (k *KernelImpl) suspendProcess(proc *Process, reason string, exitCode int) error {
	// Extract FD references under lock, then close outside lock to prevent
	// deadlock if test timeout panic triggers defer → GetState → proc.mu.
	proc.mu.Lock()
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

	// Story 44.5 v2 — record the suspend wall-clock anchor so dashboard
	// elapsed-time rendering can freeze at PausedAt instead of advancing with
	// wall clock. Mirror of SoftPause Pause() (kernel/process.go:735) which
	// was the SoftPause field maintainer; Epic 44.1's HardSuspend rewrite
	// switched the SIGPAUSE entry from Pause() to Suspend() but did NOT
	// migrate the pausedAt write — leaving Dashboard tree render.go's
	// `IsPaused && !PausedAt.IsZero()` freeze branch unreachable.
	//
	// Decision 1 — write SuspendReason here, AFTER the transition succeeded,
	// not before (see function docstring).
	proc.mu.Lock()
	proc.SuspendReason = reason
	proc.pausedAt = time.Now()
	proc.mu.Unlock()

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
	baseDir := k.ResolveStepBaseDir(proc)
	if info, ierr := k.GetProcInfo(proc.PID); ierr == nil && info != nil {
		if perr := SaveProcInfo(baseDir, *info); perr != nil {
			log.Printf("[suspend] proc-info.json write error pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, perr)
		}
	}

	// Epic 44 follow-up — also persist process-meta.json on the suspend leg.
	// Previously this file was written only by reapProcess, which never runs
	// on the Suspended path. The omission left LoadSuspendedFromDisk +
	// rehydrateRuntimeStateFromDisk without the system_prompt they need to
	// revive the placeholder after a daemon restart — dashboard `r` would
	// silently skip the placeholder ("process-meta.json missing"). Same
	// best-effort contract as SaveProcInfo above: log on failure, do not
	// block the state transition.
	writeProcessMetaBestEffort(baseDir, proc, "suspend")

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

	// Pre-set Exit so the reasonStep defer's notifySuspendDone sends the
	// correct exit reason on the Done channel. SuspendReason is NOT written
	// here — Decision 1 moved that write into suspendProcess (after the
	// state transition succeeds) so a happy-exit race (reasonStep already
	// past finishProcess when SIGPAUSE arrives) cannot leak SuspendReason
	// onto a Zombie/Dead snapshot. See suspendProcess docstring.
	exit := ExitStatus{Code: ExitSuspended, Reason: "suspended: user_suspended"}
	proc.mu.Lock()
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
		baseDir := k.ResolveStepBaseDir(proc)
		if info, err := k.GetProcInfo(proc.PID); err == nil {
			if err := SaveProcInfo(baseDir, *info); err != nil {
				log.Printf("[reaper] proc-info.json write error pid=%d: %v", proc.PID, err)
			}
		}
	})
}

// killSuspendedProcess transitions a Suspended process to Dead, notifies waiters,
// and reaps resources. Shared by Kill() and Signal() for suspended process handling.
// attr attributes the killed_suspended event (Signal() passes unknown).
func (k *KernelImpl) killSuspendedProcess(proc *Process, sig types.Signal, syscall string, start time.Time, attr KillAttribution) error {
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
	killedArgs := killEventArgs(proc.PID, sig, attr)
	killedArgs["action"] = "killed_suspended"
	k.emitEvent(proc, syscall, killedArgs, nil, nil, time.Since(start))
	// Notify waiters so Wait() does not hang
	select {
	case proc.Done <- exit:
	default:
	}
	proc.closeTerminated() // unblock twoPhaseShutdown if it's watching this process
	k.reapSuspendedProcess(proc)
	return nil
}
