package kernel

import (
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// twoPhaseShutdown implements the two-phase SIGTERM shutdown protocol.
// Phase 1 (proc.Cancel) is already done synchronously by the caller.
// This goroutine handles phase 2: wait for exit or escalate to force-kill.
//
// The shutdownStarted atomic.Bool on Process prevents duplicate invocations.
//
// attr is the attribution of the SIGTERM that started this shutdown. The
// SIGKILL escalation inherits it verbatim and only adds escalation=grace_timeout
// (Story 66.3 AC4): the answer to "who killed this" is the original requester,
// not the kernel timer that carried out the request.
func (k *KernelImpl) twoPhaseShutdown(proc *Process, gracePeriod time.Duration, attr KillAttribution) {
	start := time.Now()

	k.emitEvent(proc, "Shutdown", map[string]any{
		"phase":           "sigterm_sent",
		"grace_period_ms": gracePeriod.Milliseconds(),
		"elapsed_ms":      int64(0),
	}, nil, nil, 0)

	// Wait for process to exit or timeout.
	// Use proc.terminated (closed-channel broadcast) instead of proc.Done
	// so we don't race with kern.Wait(), which is the sole consumer of proc.Done.
	timer := time.NewTimer(gracePeriod)
	defer timer.Stop()

	select {
	case <-proc.terminated:
		// Process exited within grace period
		k.emitEvent(proc, "Shutdown", map[string]any{
			"phase":           "grace_completed",
			"grace_period_ms": gracePeriod.Milliseconds(),
			"elapsed_ms":      time.Since(start).Milliseconds(),
		}, nil, nil, time.Since(start))

	case <-timer.C:
		// Grace period expired — process is stuck, force kill
		k.emitEvent(proc, "Shutdown", map[string]any{
			"phase":           "grace_timeout",
			"grace_period_ms": gracePeriod.Milliseconds(),
			"elapsed_ms":      time.Since(start).Milliseconds(),
		}, nil, nil, time.Since(start))

		// Escalate to SIGKILL — force-terminate the process, keeping the
		// original requester as the attributed origin.
		_ = k.KillWithOrigin(proc.PID, types.SIGKILL, attr.withEscalation("grace_timeout"))
	}
}
