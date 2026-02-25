package kernel

import (
	"fmt"
	"time"

	"github.com/gonewx/crux/internal/types"
)

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

	// Emit exit event BEFORE closing DebugChan (writing to closed channel panics)
	k.emitEvent(proc, "Wait", map[string]any{
		"pid":    pid,
		"action": "completed",
	}, exit, nil, time.Since(start))

	// Resource release sequence (strict order per architecture doc):
	// 1. cancel() — ensure context cancelled (idempotent)
	proc.Cancel()

	// 2. wg.Wait() — wait for goroutine to complete (internal defer executes CloseAll)
	proc.wg.Wait()

	// 3. close(DebugChan) — close debug event channel (nil out under lock first to prevent races)
	proc.mu.Lock()
	ch := proc.DebugChan
	proc.DebugChan = nil
	proc.mu.Unlock()
	close(ch)

	// 4. CtxFree(CtxID) — release context space
	_ = k.ctxMgr.CtxFree(proc.CtxID)

	// 5. Reap() — Zombie → Dead state transition
	_ = proc.Reap()

	// 6. RemoveProcess(pid) — remove from process table
	k.RemoveProcess(pid)

	return exit, nil
}
