package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// Story 48.5 AC3 — backoff reconnecter.
//
// When L2 readiness detects a dead/unresponsive child (Story 48.5 §健康检查时序),
// the transport re-execs the MCP server subprocess with a bounded backoff
// schedule. This is opportunistic recovery driven from the tool-call boundary —
// NOT a background ticker (Winston 决策 3). The mechanism lives entirely in
// drivers/mcp; kernel only observes the resulting ReconnectCount() delta.

// Default production backoff: 3 attempts at 1s / 2s / 4s (Story 48.5 AC3). Tests
// inject a millisecond schedule via TransportConfig.ReconnectPolicy so exhaustion
// resolves in ~7ms instead of 7s (Story §测试策略 "reconnect 退避加速").
const defaultReconnectMaxRetries = 3

var defaultReconnectBackoff = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// ReconnectPolicy configures the backoff reconnect schedule. The zero value is
// valid and resolves to the production defaults via effectivePolicy.
type ReconnectPolicy struct {
	// MaxRetries is the number of re-exec attempts before giving up (→
	// BackoffExhausted). ≤0 → defaultReconnectMaxRetries.
	MaxRetries int
	// Backoff holds the per-attempt wait before each attempt. Shorter than
	// MaxRetries → the last entry repeats; empty → defaultReconnectBackoff.
	Backoff []time.Duration
}

// effectivePolicy resolves zero-value fields to production defaults.
func (t *StdioTransport) effectivePolicy() ReconnectPolicy {
	p := t.config.ReconnectPolicy
	if p.MaxRetries <= 0 {
		p.MaxRetries = defaultReconnectMaxRetries
	}
	if len(p.Backoff) == 0 {
		p.Backoff = defaultReconnectBackoff
	}
	return p
}

// backoffFor returns the wait before the given (0-based) attempt. Past the end
// of the schedule it repeats the last entry.
func (p ReconnectPolicy) backoffFor(attempt int) time.Duration {
	if len(p.Backoff) == 0 {
		return 0
	}
	if attempt < len(p.Backoff) {
		return p.Backoff[attempt]
	}
	return p.Backoff[len(p.Backoff)-1]
}

// reconnect drives a full backoff reconnect cycle, acquiring t.mu itself. This
// is the entry point ATDD drives directly (and the public-ish surface). The
// inline L2 trigger path calls reconnectLocked (it already holds t.mu).
func (t *StdioTransport) reconnect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reconnectLocked(ctx)
}

// reconnectLocked performs the backoff reconnect. Caller MUST hold t.mu and the
// function returns with t.mu held (Story 48.5 §易错点 4: it RELEASES t.mu only
// around the backoff sleep so concurrent Call doesn't block on the 1+2+4=7s
// schedule, re-acquiring before touching t.cmd/t.stdin/t.stdout/connected).
//
// Outcome:
//   - nil  → Status=Connected, child re-exec'd, initialize + tools/list redone,
//     ReconnectCount incremented.
//   - err  → Status=BackoffExhausted (or ctx error), all attempts failed.
func (t *StdioTransport) reconnectLocked(ctx context.Context) error {
	policy := t.effectivePolicy()
	t.status.Store(int32(vfs.MCPStatusReconnecting))

	// Tear down the unresponsive child up front (before the first backoff
	// sleep that releases t.mu). This sets connected=false and drops the
	// stale stdin/stdout/reader, so a concurrent Call that grabs t.mu during a
	// backoff window fails fast on the `!t.connected` guard instead of issuing
	// a request on a child we are about to SIGKILL (use-after-teardown)
	// ([Review][Patch] P3). teardown is idempotent, so the per-attempt teardown
	// below still handles a failed start.
	t.teardownCurrentProcessLocked()

	var lastErr error
	for attempt := 0; attempt < policy.MaxRetries; attempt++ {
		// Backoff BEFORE the attempt (1s → 2s → 4s). Release t.mu while sleeping.
		if d := policy.backoffFor(attempt); d > 0 {
			t.mu.Unlock()
			timer := time.NewTimer(d)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.mu.Lock()
				t.status.Store(int32(vfs.MCPStatusBackoffExhausted))
				return fmt.Errorf("reconnect aborted: %w", ctx.Err())
			}
			t.mu.Lock()
		}

		// Tear down whatever is left of the previous attempt, then re-exec.
		t.teardownCurrentProcessLocked()
		if err := t.startProcessLocked(ctx); err != nil {
			lastErr = err
			t.teardownCurrentProcessLocked()
			continue
		}
		// Re-run tools/list (+ resources/list) so the cache reflects the fresh
		// server. A hung child that completes initialize but stalls here counts
		// as a failed attempt (Story §健康检查时序 — covers mockMCPHangAfterInit).
		if err := t.refreshToolsLocked(ctx); err != nil {
			lastErr = err
			t.teardownCurrentProcessLocked()
			continue
		}

		// Success.
		t.connected = true
		t.status.Store(int32(vfs.MCPStatusConnected))
		t.reconnectCount.Add(1)
		now := nowFunc()
		t.lastCall.Store(now.UnixNano())
		t.lastCheck.Store(now.UnixNano())
		return nil
	}

	t.status.Store(int32(vfs.MCPStatusBackoffExhausted))
	if lastErr == nil {
		lastErr = fmt.Errorf("reconnect exhausted after %d attempts", policy.MaxRetries)
	}
	return lastErr
}
