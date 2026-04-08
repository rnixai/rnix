package kernel

import (
	"time"
)

// twoPhaseShutdown implements the two-phase SIGTERM shutdown protocol.
// Phase 1 (proc.Cancel) is already done synchronously by the caller.
// This goroutine handles phase 2: wait for exit or escalate to force-kill.
//
// The shutdownStarted atomic.Bool on Process prevents duplicate invocations.
func (k *KernelImpl) twoPhaseShutdown(proc *Process, gracePeriod time.Duration) {
	start := time.Now()

	k.emitEvent(proc, "Shutdown", map[string]any{
		"phase":           "sigterm_sent",
		"grace_period_ms": gracePeriod.Milliseconds(),
	}, nil, nil, 0)

	// Wait for process to exit or timeout
	timer := time.NewTimer(gracePeriod)
	defer timer.Stop()

	select {
	case <-proc.Done:
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

		// Force cancel again (idempotent) — the reasonStep loop should have
		// already exited via ctx.Done(), but if cleanup is stuck, this ensures
		// the goroutine terminates.
		proc.Cancel()
	}
}
