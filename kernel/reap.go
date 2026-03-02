package kernel

import (
	"fmt"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// reapProcess performs the complete resource release sequence for a zombie process.
// It is idempotent — proc.reapOnce ensures only one caller executes the cleanup,
// even if both Wait and the reaper goroutine try concurrently.
func (k *KernelImpl) reapProcess(proc *Process) {
	proc.reapOnce.Do(func() {
		// Handle orphan children before removing parent from table
		k.handleOrphanChildren(proc)

		// Resource release sequence (strict order per architecture doc):
		// 1. cancel() — ensure context cancelled (idempotent)
		proc.Cancel()

		// 2. wg.Wait() — wait for goroutine to complete (internal defer executes CloseAll)
		proc.wg.Wait()

		// 3. close(DebugChan) — nil out under lock first to prevent races with emitEvent
		proc.mu.Lock()
		ch := proc.DebugChan
		proc.DebugChan = nil
		proc.mu.Unlock()
		if ch != nil {
			close(ch)
		}

		// 4. msgQueue.close() — close message queue, unblock pending Recv (Story 6.1)
		if queue, ok := k.msgQueues.LoadAndDelete(proc.PID); ok {
			queue.close()
		}

		// 5. removeFromAllGroups — clean up process group memberships (Story 6.3)
		k.removeFromAllGroups(proc.PID, proc)

		// 6. ClearSignalState — clean up signal handlers/blocked/pending/resume (Story 6.4)
		proc.ClearSignalState()

		// 7. ClearThreads — cancel all threads and wait for completion (Story 6.5)
		proc.ClearThreads()

		// 8. ClearCoroutines — clean up all coroutines (Story 6.5)
		proc.ClearCoroutines()

		// 9. CtxFree(CtxID) — release context space
		_ = k.ctxMgr.CtxFree(proc.CtxID)

		// 10. Reap() — Zombie → Dead state transition
		_ = proc.Reap()

		// 11. RemoveProcess(pid) — remove from process table
		k.RemoveProcess(proc.PID)
	})
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
		case types.StateRunning, types.StateCreated:
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
				// reapCh full, fall back to async reap
				go k.reapProcess(child)
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

// startReaper launches the background goroutine that auto-reaps orphan zombies.
func (k *KernelImpl) startReaper() {
	k.reaperWg.Add(1)
	go func() {
		defer k.reaperWg.Done()
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
			case <-k.stopCh:
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
	}()
}

// Shutdown stops the reaper goroutine, unmounts all MCP servers, and waits for exit.
// Safe to call multiple times — only the first call closes stopCh.
func (k *KernelImpl) Shutdown() {
	k.shutdownOnce.Do(func() {
		// Unmount all MCP servers before stopping reaper
		if k.mountMgr != nil {
			_ = k.mountMgr.UnmountAll()
		}
		close(k.stopCh)
	})
	k.reaperWg.Wait()
}
