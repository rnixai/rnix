package kernel

import (
	"math/rand/v2"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
)

// Story 73.2 — rate-limit backoff decision and wait (the DISPOSITION side of
// the short-wait layer; parsing lives in drivers/llm/retryafter.go, the
// long-wait suspension is Story 73.3's).
//
// Layering (AC7): drivers parse headers/bodies into the *RateLimitError wait
// fields at construction time (D8); the kernel only READS them through
// llm.RateLimitWaitOf, arbitrates against its own hard caps, and does the
// actual waiting. Drivers never sleep; the kernel never parses error text.

const (
	// maxInProcessWait bounds a single in-process backoff wait (D3). 60s
	// covers the measured 6-second throttle with 10x headroom, stays far
	// below cc-src's 6-hour WINDOW wait (not comparable — that is quota
	// suspension territory, Story 73.3), and sits inside the reference band
	// (2x gemini-cli's maxDelayMs=30000).
	maxInProcessWait = 60 * time.Second

	// maxRateLimitRetries bounds how many times the rate-limit family may
	// retry (D5). The cap is deliberately SEPARATE from
	// consecutiveTransientRetries: socket/EOF/timeout retries keep their
	// pre-73.2 `< 2` budget verbatim, and the two counters never consume
	// each other. Bounding the COUNT as well as each wait is what closes the
	// NFR1 hole — capping the single wait alone would still let a provider
	// returning 59s on every attempt hold the process indefinitely.
	maxRateLimitRetries = 3

	// rateLimitBackoffBase is the local exponential backoff base (opencode
	// RETRY_INITIAL_DELAY=2000), used only when the server gave no wait
	// instruction at all.
	rateLimitBackoffBase = 2 * time.Second

	// rateLimitBackoffMaxDelay caps the LOCAL exponential growth (gemini
	// maxDelayMs=30000). Server-declared waits deliberately BYPASS this cap
	// (cc-src withRetry.ts:530-548 shape) — they are bounded only by
	// maxInProcessWait.
	rateLimitBackoffMaxDelay = 30 * time.Second

	// heartbeatRefreshInterval is the chunk size for refreshing
	// proc.LastHeartbeat during a backoff wait (D6). The heartbeat monitor
	// compares time.Since(LastHeartbeat) against StepTimeout with no
	// knowledge of step boundaries, so an un-refreshed 60s wait would look
	// like a stall on any process with StepTimeout< 60s. 10s is far below
	// any configured StepTimeout in this repo. Same technique as cc-src
	// slicing long sleeps into 30s heartbeat chunks (withRetry.ts:500).
	heartbeatRefreshInterval = 10 * time.Second
)

// sleepFunc is the wait seam (AC9): a package-level var so tests can replace
// time.After with an immediate channel and exercise multi-minute backoffs in
// milliseconds. Tests MUST restore the original via t.Cleanup (see
// overrideSleepFunc); kernel tests run concurrently and the variable is
// shared. Shape mirrors drivers/mcp's nowFunc precedent.
var sleepFunc = func(d time.Duration) <-chan time.Time { return time.After(d) }

// jitterFunc is the randomness seam (AC9): injectable so the one-sided jitter
// property can be asserted deterministically instead of via sampling.
// math/rand/v2's global source is concurrency-safe and auto-seeded (Go 1.20+);
// jitter is de-correlation, not a security property — never crypto/rand.
var jitterFunc = rand.Float64

// localRateLimitBackoff computes the local exponential backoff for the Nth
// rate-limit retry attempt (1-based): base × 2^(attempt-1), capped at
// rateLimitBackoffMaxDelay. Shift overflow saturates to the cap.
func localRateLimitBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 30 {
		return rateLimitBackoffMaxDelay
	}
	d := rateLimitBackoffBase << shift
	if d <= 0 || d > rateLimitBackoffMaxDelay {
		return rateLimitBackoffMaxDelay
	}
	return d
}

// applyRateLimitJitter adds ONE-SIDED jitter: delay × jitterFunc() × 0.25,
// only ever added, never subtracted (cc-src +0~25%, hermes +0~50% — gemini's
// ±30% is the wrong direction here: shortening a server-declared wait means
// retrying while the provider is still throttling). Jitter applies to
// server-declared values too: processes hitting the same Retry-After would
// otherwise stampede the provider at the same millisecond (complementary to
// Story73.5's per-provider gate).
func applyRateLimitJitter(d time.Duration) time.Duration {
	return d + time.Duration(jitterFunc()*0.25*float64(d))
}

// resolveRateLimitDelay arbitrates the PRE-JITTER base delay for a
// rate-limit retry (AC4 three-level precedence):
//
//  1. server-declared duration (RetryAfter from headers or body) — bypasses
//     the local maxDelay cap, bounded only by maxInProcessWait at the caller;
//  2. server-declared reset instant (ResetAt, terminal quota windows) —
//     converted with time.Until, same bypass;
//  3. local exponential backoff (no server instruction).
//
// The caller applies one-sided jitter afterwards and clamps the result to
// maxInProcessWait — comparing the PRE-jitter value here is deliberate: a
// server-declared 59s wait must stay retryable (it is inside the cap), even
// though jitter alone could push it past 60s (test 9-②'s malicious-provider
// bound depends on this).
//
// The returned source is "header"/"body"/"backoff" — provenance of who named
// the wait, orthogonal to the rate_limit_kind dimension (D9) — and
// serverStated reports whether levels 1-2 produced the value (drives the
// retry_after_ms / reset_at event fields).
//
// retryAfter and resetAt are the RAW server-declared fields from the SAME
// extraction that decided the delay (review 2026-08-03: the caller's event
// fields used to come from a second, independent RateLimitWaitOf traversal —
// today identical, but a future cap/unit change on either path would silently
// desync what the event reports from what the process actually waited).
func resolveRateLimitDelay(err error, attempt int) (baseDelay time.Duration, source string, serverStated bool, retryAfter time.Duration, resetAt time.Time) {
	retryAfter, resetAt, waitSource, ok := llm.RateLimitWaitOf(err)
	switch {
	case ok && retryAfter > 0:
		return retryAfter, waitSource, true, retryAfter, resetAt
	case ok && !resetAt.IsZero():
		if d := time.Until(resetAt); d > 0 {
			return d, waitSource, true, retryAfter, resetAt
		}
		// A reset instant already in the past carries no instruction; fall
		// through to local backoff rather than treating it as "wait zero".
	}
	return localRateLimitBackoff(attempt), "backoff", false, 0, time.Time{}
}

// backoffWaitInterruptible waits d in heartbeat-sized chunks, refreshing
// proc.LastHeartbeat every chunk (D6 — keeps the wait invisible to stall
// detection; StepTimeout semantics are NOT touched). It returns true when
// proc.ctx is cancelled mid-wait: a SIGTERM/SIGKILL arriving during the wait
// never passes the pre-retry cancel check (reason.go's Story 66.2 guard runs
// BEFORE this wait), so the select here is the only defence. The caller must
// route the true case through handleInterruptedWrite — the SAME exit path as
// a cancel during the write itself — so one SIGTERM cannot produce two
// different exit_reason values depending on timing (AC6). Bare time.Sleep is
// forbidden for exactly this reason.
func (k *KernelImpl) backoffWaitInterruptible(proc *Process, d time.Duration) (interrupted bool) {
	for remaining := d; remaining > 0; {
		chunk := min(remaining, heartbeatRefreshInterval)
		select {
		case <-sleepFunc(chunk):
			remaining -= chunk
			proc.TouchHeartbeat()
		case <-proc.ctx.Done():
			return true
		}
	}
	return false
}
