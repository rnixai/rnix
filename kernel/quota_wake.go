package kernel

import (
	"fmt"
	"log"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
)

// Story 73.3 — quota-window suspension and window-recovery wake (the LONG-wait
// disposition layer; classification is 73.1, Retry-After parsing + short-wait
// backoff is 73.2, wire visibility is 73.4, per-provider concurrency gating is
// 73.5).
//
// A rate-limit failure whose server-declared wait exceeds maxInProcessWait —
// or a terminal quota failure with NO server wait evidence at all — suspends
// the process instead of killing it, recording the window reset instant in
// ResumeAt. A daemon-side scanner then resumes the process once the instant
// arrives, so a 95-hour quota window neither aborts the epic nor keeps a
// process asleep in the daemon's arms for the whole window.

const (
	// quotaWakeScanInterval is the period of the daemon-side wake scanner
	// (D2). A process wakes within [0, quotaWakeScanInterval] of its reset
	// instant — imperceptible against hour-scale windows.
	quotaWakeScanInterval = 60 * time.Second

	// quotaWakeRetryBackoff is how far a FAILED wake attempt pushes ResumeAt
	// forward (D3). The scanner ticks every quotaWakeScanInterval; without the
	// push, a persistent failure (device unmounted, rehydrate corrupt) would
	// be retried on every tick as a hot loop. No failure cap — windows are
	// hour-scale, a 5-minute-interval retry is negligible, and a permanently
	// broken device ends in an operator kill; the suspended state itself stays
	// observable (NFR3).
	quotaWakeRetryBackoff = 5 * time.Minute
)

// SuspendReasonQuotaExhausted is the canonical SuspendReason value for a
// process suspended because its rate-limit / quota window is spent. Defined
// here (not resume.go) because the quota wake scanner — the primary consumer —
// lives in this file; the single source of truth is shared at compile time by
// reason.go (which stamps it) and quota_wake.go (which scans for it), in the
// style of SuspendReasonCLIDisconnected.
const SuspendReasonQuotaExhausted = "quota_exhausted"

// quotaSuspendProcess performs the quota suspend exit from inside reasonStep:
// it records the wake instant, emits the quota_suspend observability event,
// and hands off to selfSuspend. Called for BOTH suspension exits (D5 fast path
// and D6 over-cap), which share this helper but differ in event fields.
//
// resumeAt is the recorded window reset instant (zero on the D5 fast path,
// where there is no server wait evidence to anchor a wake). resumeAtSource
// records provenance for the resume_at_source event field ("reset_at" for a
// server absolute instant, "retry_after" for now+server-duration). requiredWait
// is the server-declared wait that exceeded the cap; it is ZERO on the fast
// path, which has no required wait to report — the event then omits both
// required_wait_ms and limit_ms.
//
// selfSuspend is used (not Suspend) because we are inside the reasoning loop
// goroutine; the Done notification is deferred to reasonStep's exit path via
// notifySuspendDone, exactly like the context_full precedent.
func (k *KernelImpl) quotaSuspendProcess(proc *Process, step int, kind llm.RateLimitKind,
	resumeAt time.Time, resumeAtSource string, requiredWait time.Duration, stepStart time.Time) {

	// Set ResumeAt BEFORE selfSuspend: suspendProcess persists proc-info.json
	// synchronously (via GetProcInfo), so the wake instant must already be on
	// the process for the snapshot to carry it. GetProcInfo reads it under
	// proc.mu; SetResumeAt takes the same lock.
	proc.SetResumeAt(resumeAt)

	args := map[string]any{
		"step":            step,
		"action":          "quota_suspend",
		"rate_limit_kind": kind.String(),
		"provider":        proc.Provider,
	}
	if !resumeAt.IsZero() {
		args["resume_at"] = resumeAt.Format(time.RFC3339)
	}
	if resumeAtSource != "" {
		args["resume_at_source"] = resumeAtSource
	}
	if requiredWait > 0 {
		// Over-cap exit only — the REQUIRED wait (not a waited duration) and
		// the in-process limit it exceeded. The D5 fast path omits both.
		args["required_wait_ms"] = requiredWait.Milliseconds()
		args["limit_ms"] = maxInProcessWait.Milliseconds()
	}
	k.emitEvent(proc, "ReasonStep", args, nil, nil, time.Since(stepStart))

	if requiredWait > 0 {
		log.Printf("[kernel] pid=%d step=%d quota suspend: kind=%s required wait %s exceeds in-process limit %s — suspending until %s (device=%s)",
			proc.PID, step, kind, requiredWait, maxInProcessWait, formatResumeAt(resumeAt), proc.PrimaryDevice)
	} else {
		log.Printf("[kernel] pid=%d step=%d quota suspend: kind=%s no server wait evidence — suspending with no wake instant (manual resume only, device=%s)",
			proc.PID, step, kind, proc.PrimaryDevice)
	}

	// The suspension itself. selfSuspend → suspendProcess transitions the state
	// and persists; on the (rare) failure we fall back to terminate so the
	// process is never left Running-but-undriven. Mirrors the context_full
	// precedent (reason.go) verbatim in shape.
	if err := k.selfSuspend(proc, SuspendReasonQuotaExhausted, ExitSuspended); err != nil {
		log.Printf("[kernel] pid=%d quota suspend failed: %v, falling back to terminate", proc.PID, err)
		// Story 73.3 review P5 — ResumeAt was stamped above for the snapshot
		// that never happened; clear it before the terminal leg so the dead
		// proc-info.json does not carry a wake instant.
		proc.SetResumeAt(time.Time{})
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "quota_exhausted + suspend failed", Err: err})
	}
	// The caller (reasonStep) returns immediately after this; the reasonStep
	// defer observes IsSuspendRequested and runs notifySuspendDone.
}

// formatResumeAt renders a wake instant for log lines, or a placeholder when
// the fast path recorded none.
func formatResumeAt(t time.Time) string {
	if t.IsZero() {
		return "<none>"
	}
	return t.Format(time.RFC3339)
}

// deriveQuotaResumeAt derives the D6 over-cap suspension's wake instant from
// the PRE-jitter server values resolveRateLimitDelay returned — the SAME
// extraction that decided the delay (a second traversal could silently desync
// the recorded instant from the arbitration, the 73.2 review lesson).
//
// resetAt wins when it is a FUTURE server absolute instant: it does not drift
// across daemon restarts the way a now+duration derivation would. A resetAt
// already in the past carries no future instruction — the same rule
// resolveRateLimitDelay's level 2 applies — so the derivation falls back to
// now+retryAfter there. Both-zero returns "no wake instant" (manual resume
// only); in practice unreachable from the over-cap branch, because
// baseDelay > maxInProcessWait can only arise from a server-declared wait.
func deriveQuotaResumeAt(retryAfter time.Duration, resetAt time.Time) (time.Time, string) {
	if !resetAt.IsZero() && resetAt.After(time.Now()) {
		return resetAt, "reset_at"
	}
	if retryAfter > 0 {
		return time.Now().Add(retryAfter), "retry_after"
	}
	return time.Time{}, ""
}

// startQuotaWakeScanner launches the daemon-side ticker that wakes
// quota-suspended processes when their recorded reset instant arrives (D2).
// It mirrors startReaper's deadTicker shape exactly: the goroutine rides
// reaperWg and shares stopCh, so Shutdown waits for it to exit before
// returning — which is what keeps a test's t.TempDir() cleanup from racing the
// scanner's file I/O. Called from NewKernel beside startReaper.
func (k *KernelImpl) startQuotaWakeScanner() {
	k.reaperWg.Go(func() {
		ticker := time.NewTicker(quotaWakeScanInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				k.scanQuotaWakeups(now)
			case <-k.stopCh:
				return
			}
		}
	})
}

// scanQuotaWakeups walks the process table once and wakes every quota-suspended
// process whose recorded reset instant has arrived. It is deliberately split
// from the ticker so tests can drive it directly with a controlled `now`
// instead of waiting on real time (no new seam needed — 73.2's TestMain already
// swaps sleepFunc for the wait path, which this scanner does not use).
//
// The scan is lock-free collection followed by per-process wake under resumeMu;
// the re-check inside wakeQuotaProcess is what makes that safe against races
// with manual resume / kill / a concurrent scan.
func (k *KernelImpl) scanQuotaWakeups(now time.Time) {
	var due []*Process
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		if isQuotaWakeCandidate(proc, now) {
			due = append(due, proc)
		}
		return true
	})
	for _, proc := range due {
		k.wakeQuotaProcess(proc, now)
	}
}

// isQuotaWakeCandidate reports whether proc is a quota-suspended process whose
// wake instant has arrived. It is the collection predicate AND (re-invoked under
// resumeMu) the re-check condition, so a process that was concurrently resumed,
// killed, or already woken falls out of the set before any state change.
//
// A zero ResumeAt never matches: the D5 fast path suspends without a wake
// instant precisely because it does not know when the window recovers, so only
// a manual resume may revive it.
func isQuotaWakeCandidate(proc *Process, now time.Time) bool {
	if proc.GetState() != types.StateSuspended {
		return false
	}
	if proc.GetSuspendReason() != SuspendReasonQuotaExhausted {
		return false
	}
	resumeAt := proc.GetResumeAt()
	if resumeAt.IsZero() {
		return false
	}
	return !now.Before(resumeAt)
}

// wakeQuotaProcess resumes one due quota-suspended process under resumeMu, the
// same mutual-exclusion lock ResumeSubtree / ResumeWithOpts hold (Epic 44.1
// red line — the scanner must not race a manual resume's state transition).
//
// It re-checks the full candidate condition after taking the lock, then drives
// resumeOneForSubtree — the SAME in-place wake path SIGRESUME / dashboard R use,
// which preserves the process object's identity so a supervising parent's
// monitor goroutine keeps watching the right Done channel (D4's premise).
func (k *KernelImpl) wakeQuotaProcess(proc *Process, now time.Time) {
	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	// Re-check under the lock: between the lock-free collection and now, the
	// process may have been manually resumed, killed, or woken by a concurrent
	// scan. Silently skip in that case — there is nothing to wake.
	if !isQuotaWakeCandidate(proc, now) {
		return
	}
	// Membership re-check: a non-fork manual resume that raced our collection
	// deletes the OLD placeholder from procTable (cleanupOldProcessAndHistory)
	// WITHOUT transitioning it — the detached object still reads
	// Suspended/quota/due, so the state re-check above passes for it. Only
	// table membership separates the live process from the husk; waking the
	// husk would run reasonStep on a freed (possibly already reused) CtxID
	// and double-run the agent beside the manual resume's new process.
	if _, ok := k.GetProcess(proc.PID); !ok {
		return
	}

	resumeAt := proc.GetResumeAt()
	k.emitEvent(proc, "Resume", map[string]any{
		"pid":       proc.PID,
		"action":    "quota_window_wake",
		"resume_at": resumeAt.Format(time.RFC3339),
	}, nil, nil, 0)
	log.Printf("[kernel] pid=%d quota window wake: reset instant %s reached, resuming", proc.PID, resumeAt.Format(time.RFC3339))

	// D3 — a wake attempt must NEVER kill the process. The probe keeps the
	// common failure (device unmounted) side-effect-free: postpone while still
	// Suspended instead of paying an Unsuspend→rollback cycle. Reopen failures
	// the probe cannot see (project opener errors, registered-device Open
	// errors) are caught by resumeOneForSubtreeQuotaWake's rollback branch,
	// which restores the Suspended shape instead of finishing the process
	// (the terminal reopen-failure semantics stay reserved for manual resume).
	if err := k.probeQuotaWakeReopen(proc); err != nil {
		k.postponeQuotaWake(proc, now, err)
		return
	}

	if err := k.resumeOneForSubtreeQuotaWake(proc); err != nil {
		// D3 — resume failed (device unmounted, rehydrate corrupt, ...). Do NOT
		// lift the suspension: re-check the state first (a concurrent manual
		// resume or kill may have already moved it out of quota-Suspended, in
		// which case we must not push a stale process's wake instant), then push
		// ResumeAt forward by the retry backoff and record the failure.
		if proc.GetState() != types.StateSuspended ||
			proc.GetSuspendReason() != SuspendReasonQuotaExhausted {
			return
		}
		k.postponeQuotaWake(proc, now, err)
	}
}

// rollbackQuotaWakeResume restores the Suspended shape after the scanner's
// Unsuspend, so a failed wake attempt leaves the process exactly where it
// started (D3: an automated wake attempt must never kill). Running→Suspended
// is a legal transition; the fields re-stamped are the ones suspendProcess
// would have written — SuspendReason, the pausedAt accounting start, and the
// Exit payload notifySuspendDone would have delivered (the invariant matrix's
// Suspended row requires a non-empty SuspendReason and a "suspended: …"
// ExitReason). The reasonStep goroutine is NOT running yet at the rollback
// point (reopen fails before it starts), so there is no wg to wait on and no
// Done write is owed: a parent waiting on this child was already waiting on a
// Suspended child, and a supervising parent's re-armed monitor (D4) keeps
// watching for the next terminal or suspend Done, exactly as pre-attempt.
// ResumeAt stays cleared here — postponeQuotaWake stamps the postponed
// instant and persists it right after.
func (k *KernelImpl) rollbackQuotaWakeResume(proc *Process, cause error) {
	if err := proc.Suspend(); err != nil {
		// Concurrently raced to a terminal state (killed while we were
		// reopening) — nothing to roll back onto; leave the terminal shape.
		log.Printf("[kernel] pid=%d quota wake rollback skipped (state changed): %v", proc.PID, err)
		return
	}
	proc.mu.Lock()
	proc.SuspendReason = SuspendReasonQuotaExhausted
	proc.pausedAt = time.Now()
	proc.Exit = &ExitStatus{Code: ExitSuspended, Reason: "suspended: " + SuspendReasonQuotaExhausted}
	proc.mu.Unlock()
	log.Printf("[kernel] pid=%d quota wake reopen failed: %v — rolled back to Suspended (D3 never-kill)", proc.PID, cause)
}

// probeQuotaWakeReopen verifies — side-effect-free — that the LLM device a
// quota wake would reopen is still available, mirroring
// openLLMDeviceForResume's resolution order (project LLMFileOpener first,
// global registry second). A project opener is trusted without probing
// (openLLMDeviceForResume falls back to the global VFS when it fails);
// otherwise the device must be registered.
func (k *KernelImpl) probeQuotaWakeReopen(proc *Process) error {
	if proc.PrimaryDevice == "" {
		// Script-runner / fixture shape — resumeOneForSubtree returns without
		// reopening anything.
		return nil
	}
	if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
		return nil
	}
	if _, err := k.vfs.Stat(proc.PrimaryDevice); err != nil {
		return fmt.Errorf("llm device %q no longer registered: %w", proc.PrimaryDevice, err)
	}
	return nil
}

// postponeQuotaWake pushes the wake instant back by quotaWakeRetryBackoff and
// records the quota_wake_failed observability channel. Callers must hold
// resumeMu and have verified the process is still quota-Suspended.
func (k *KernelImpl) postponeQuotaWake(proc *Process, now time.Time, cause error) {
	// Postpone from the FAILED ATTEMPT's time, not from the (possibly
	// long-stale — the daemon may have been down past the reset instant)
	// recorded wake instant. Anchoring at `now` is what guarantees the full
	// quotaWakeRetryBackoff gap between attempts in every case; resumeAt.Add
	// would leave a stale instant still due and retry on the very next tick —
	// the hot loop D3 exists to prevent.
	newResumeAt := now.Add(quotaWakeRetryBackoff)
	proc.SetResumeAt(newResumeAt)
	// Persist the postponed instant: a daemon restart inside the backoff
	// window must not reload the stale due ResumeAt from disk and retry
	// immediately on the first post-restart scan (the anchoring at `now`
	// above only guarantees the gap while the memory state survives).
	// Best-effort, same shape as resumeOneForSubtree's 44.3 persist.
	if info, ierr := k.GetProcInfo(proc.PID); ierr == nil && info != nil {
		if perr := SaveProcInfo(k.ResolveStepBaseDir(proc), *info); perr != nil {
			log.Printf("[quota_wake] proc-info.json write error pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, perr)
		}
	}
	k.emitEvent(proc, "Resume", map[string]any{
		"pid":              proc.PID,
		"action":           "quota_wake_failed",
		"error":            cause.Error(),
		"resume_at":        newResumeAt.Format(time.RFC3339),
		"retry_backoff_ms": quotaWakeRetryBackoff.Milliseconds(),
	}, nil, cause, 0)
	log.Printf("[kernel] pid=%d quota wake failed: %v — still suspended, next retry at %s",
		proc.PID, cause, newResumeAt.Format(time.RFC3339))
}
