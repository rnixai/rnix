//go:build unix

package mcp

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.5 AC1/AC2/AC6 — transport L1 liveness + L2 readiness + fast-path
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — gated `//go:build atdd_48_5_red && unix`. The unix tag scopes
//        out Windows (syscall.Kill(pid,0) is unix-only; Windows isChildAlive is
//        a deferred degraded impl per Story §易错点 7). Run:
//            go test -tags=atdd_48_5_red -race ./drivers/mcp/
//        Pre-impl: compile fails on undefined types.ErrDeviceDisconnected /
//        transport.Status() / mcp.nowFunc — that IS the red signal.
//
// Reuses mockMCPServer (transport_test.go, same package). Adds mockMCPHangAfterInit
// for the "child alive but unresponsive" L2-timeout case.
//
// Injection points (dev-story Tasks 2-3, 5):
//   types.ErrDeviceDisconnected ErrCode
//   var nowFunc = time.Now                       // package-level, injectable
//   (*StdioTransport).Status() vfs.MCPStatus
//   (*StdioTransport).Alive() bool
//   (*StdioTransport).LastCheck() time.Time
//   (*StdioTransport).ReconnectCount() int
// =============================================================================

// mockMCPHangAfterInit completes the initialize handshake then goes silent —
// the child stays ALIVE (so L1 passes) but never answers a subsequent request,
// forcing the L2 `ping` to hit its 2s timeout. `read x` blocks on stdin EOF;
// `sleep 60` keeps the PID alive past the ping deadline.
const mockMCPHangAfterInit = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"hang","version":"1.0.0"}}}'
read notif
sleep 60
`

func connectMock(t *testing.T, script string) *StdioTransport {
	t.Helper()
	tr := NewStdioTransport(TransportConfig{
		Command:       "bash",
		Args:          []string{"-c", script},
		TimeoutMillis: 3000,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return tr
}

// swapNowFunc installs a fixed/jumpable clock and restores it on cleanup.
func swapNowFunc(t *testing.T, fn func() time.Time) {
	t.Helper()
	old := nowFunc
	nowFunc = fn
	t.Cleanup(func() { nowFunc = old })
}

// -----------------------------------------------------------------------------
// _010: L1 — child killed → Call returns ErrDeviceDisconnected FAST (< 2s),
//
//	NOT after the 30s mcpCallTimeout stdio scan. (AC1)
//
// -----------------------------------------------------------------------------
func TestATDD_48_5_010_L1_ChildDead_FastFailDeviceDisconnected(t *testing.T) {
	tr := connectMock(t, mockMCPServer)
	defer tr.Close()

	// Kill the child out from under the transport (simulates server crash).
	pid := tr.lastChildPID.Load()
	if pid <= 0 {
		t.Fatalf("lastChildPID not set: %d", pid)
	}
	if err := syscall.Kill(int(pid), syscall.SIGKILL); err != nil {
		t.Fatalf("kill child %d: %v", pid, err)
	}
	// Give the kernel a beat to reap the PID so syscall.Kill(pid,0) reports ESRCH.
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	start := time.Now()
	_, err := tr.Call(ctx, "tools/call", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Call succeeded against a dead child; want ErrDeviceDisconnected")
	}
	// Must be the L1 sentinel, NOT a generic stdio read error.
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrDeviceDisconnected {
		t.Fatalf("error = %v (code mismatch); want *types.DriverError{Code: ErrDeviceDisconnected}", err)
	}
	// Fast-fail: L1 is a local syscall, must NOT wait on the 30s scan.
	if elapsed > 2*time.Second {
		t.Errorf("Call took %v — L1 did not short-circuit before the 30s stdio scan", elapsed)
	}
	if tr.Status() == vfs.MCPStatusConnected {
		t.Errorf("Status still Connected after L1 detected dead child")
	}
}

// -----------------------------------------------------------------------------
// _011: L1 healthy fast-path — child alive + ≤30s idle → Call succeeds, only
//
//	the ≤1ms L1 check runs, no reconnect. (AC1 happy + AC6)
//
// -----------------------------------------------------------------------------
func TestATDD_48_5_011_L1_HealthyFastPath(t *testing.T) {
	// Fixed clock: every Call sees the same "now" → idle delta is 0 (≤30s),
	// so L2 must be skipped entirely.
	fixed := time.Unix(1_700_000_000, 0)
	swapNowFunc(t, func() time.Time { return fixed })

	tr := connectMock(t, mockMCPServer)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for i := range 3 {
		if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
			t.Fatalf("healthy Call #%d failed: %v", i, err)
		}
	}
	if !tr.Alive() {
		t.Error("Alive() = false on a healthy connected transport")
	}
	if tr.Status() != vfs.MCPStatusConnected {
		t.Errorf("Status = %v, want Connected on healthy fast-path", tr.Status())
	}
	if tr.ReconnectCount() != 0 {
		t.Errorf("ReconnectCount = %d, want 0 (no reconnect on healthy path)", tr.ReconnectCount())
	}
}

// -----------------------------------------------------------------------------
// _012: L2 — > 30s idle triggers a ping before the real Call; ping success
//
//	advances LastCheck and the original Call still completes. (AC2)
//
// -----------------------------------------------------------------------------
func TestATDD_48_5_012_L2_IdleTriggersPing(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	clock := base
	swapNowFunc(t, func() time.Time { return clock })

	tr := connectMock(t, mockMCPServer)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First call establishes lastCall at `base`.
	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("first Call: %v", err)
	}
	before := tr.LastCheck()

	// Jump the clock 31s → next Call must run the L2 ping (mockMCPServer answers
	// any request, so ping succeeds) and refresh LastCheck.
	clock = base.Add(31 * time.Second)
	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("post-idle Call: %v", err)
	}

	after := tr.LastCheck()
	if !after.After(before) {
		t.Errorf("LastCheck not advanced after >30s idle: before=%v after=%v (L2 ping skipped)", before, after)
	}
	if tr.Status() != vfs.MCPStatusConnected {
		t.Errorf("Status = %v, want Connected after successful L2 ping", tr.Status())
	}
}

// -----------------------------------------------------------------------------
// _013: L2 ping failure — child alive but unresponsive, > 30s idle → ping
//
//	times out → Status leaves Connected (Error/Reconnecting/Backoff). (AC2)
//
// -----------------------------------------------------------------------------
func TestATDD_48_5_013_L2_PingTimeout_LeavesConnected(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	clock := base
	swapNowFunc(t, func() time.Time { return clock })

	// The hung child forces BOTH the L2 ping and the reconnect's tools/list
	// refresh to run to their full timeouts, so shorten both: inject a short
	// ping window (production 2s; same injection pattern as swapNowFunc) and a
	// small TimeoutMillis (bounds the tools/list wait). The behavior under
	// test — ping timeout ⇒ status leaves Connected — is unchanged. Only the
	// ping window is injected (NOT gracefulShutdownTimeout): the hang fixture
	// honors SIGTERM, so teardown never waits the escalation window. Note the
	// restore t.Cleanup runs AFTER the deferred tr.Close() below (defer before
	// Cleanup) — safe today because Close never reads l2PingTimeout. Not
	// t.Parallel — enforced by t.Setenv (panics under t.Parallel).
	t.Setenv("RNIX_TEST_TIMEOUT_INJECTION", "1")
	origPing := l2PingTimeout
	l2PingTimeout = 200 * time.Millisecond
	t.Cleanup(func() { l2PingTimeout = origPing })

	// Cap reconnect retries to a single fast attempt so a failed reconnect
	// resolves quickly (the hung child cannot be revived by a re-exec of the
	// same hang script, so this lands in a non-Connected terminal state).
	tr := NewStdioTransport(TransportConfig{
		Command:         "bash",
		Args:            []string{"-c", mockMCPHangAfterInit},
		TimeoutMillis:   500,
		ReconnectPolicy: ReconnectPolicy{MaxRetries: 1, Backoff: []time.Duration{1 * time.Millisecond}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer tr.Close()

	// Mark a successful baseline, then jump 31s so the next Call runs L2.
	clock = base.Add(31 * time.Second)
	_, _ = tr.Call(ctx, "tools/call", nil) // expected to fail (ping timeout → reconnect of hung server fails)

	if tr.Status() == vfs.MCPStatusConnected {
		t.Errorf("Status still Connected after L2 ping timeout against an unresponsive child")
	}
}

// -----------------------------------------------------------------------------
// _014: fast-path — ≤30s idle skips L2 entirely (zero ping overhead). (AC6)
//
//	Asserted by: a server that would HANG on a second request still lets
//	≤30s calls through, because L2 (ping) never fires. We prove the ping
//	was skipped by confirming LastCheck did NOT advance.
//
// -----------------------------------------------------------------------------
func TestATDD_48_5_014_L2_WithinThreshold_Skipped(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	clock := base
	swapNowFunc(t, func() time.Time { return clock })

	tr := connectMock(t, mockMCPServer)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("first Call: %v", err)
	}
	lc1 := tr.LastCheck()

	// Advance only 10s (< 30s threshold) → L2 must be skipped.
	clock = base.Add(10 * time.Second)
	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("within-threshold Call: %v", err)
	}
	lc2 := tr.LastCheck()

	if !lc2.Equal(lc1) {
		t.Errorf("LastCheck advanced (%v → %v) within 30s — L2 ping fired when it should be skipped (AC6 fast-path violated)", lc1, lc2)
	}
}
