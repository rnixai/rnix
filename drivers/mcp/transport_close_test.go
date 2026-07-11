//go:build unix

package mcp

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 48.2 — MCP transport Close 优雅关闭与进程组清理
//
// Story: _bmad-output/implementation-artifacts/48-2-mcp-transport-graceful-shutdown.md
// Phase:  TDD RED — every test (except 008 NeverConnected, 已 baseline OK) starts
//         with t.Skip until the corresponding Task lands. Dev-story removes the
//         Skip line, then the assertions drive GREEN.
//
// Coverage:
//   AC1 → TestATDD_48_2_001_Close_KillsSubprocessTree_Graceful   (Task 1 + 2.3 + 4.1)
//   AC3 → TestATDD_48_2_003_Close_Idempotent                     (Task 2.3 §1-2 closed flag)
//   AC4 → TestATDD_48_2_004_Close_TimeoutFallbackToSIGKILL       (Task 2.3 §8-9 + 2.5 sentinel)
//   AC5 → TestATDD_48_2_005_Connect_FailureCleansSubprocessTree  (Task 2.4 closeProcess helper)
//   AC8 → TestATDD_48_2_008_Close_NeverConnected_NoSignal        (zero-overhead guarantee)
//
// Design notes:
//   - We assert behaviour, not internal symbols, so this file compiles against
//     the current baseline (16f86bc). Activation happens by removing t.Skip.
//   - The Pdeathsig configuration check (AC6) lives in
//     transport_pgrp_linux_test.go / transport_pgrp_other_unix_test.go so the
//     build tags align with the prod files Task 1 introduces.
//   - SIGTERM-ignore fixture is a Go program in testdata/mcp_ignore_sigterm —
//     `bash -c 'trap "" TERM'` is unreliable across PID namespaces (Story §易错点 #10).
// =============================================================================

// mockMCPServerWithChildren extends the existing mockMCPServer (transport_test.go)
// by spawning two sleep children **before** the initialize handshake. The shell
// is the process group leader once Story 48.2 Task 4.1 wires
// applyProcessGroupIsolation(cmd) — both sleeps inherit the same pgid, giving
// us a 3-process tree (bash + 2× sleep) that mimics the playwright "node +
// 2× chromium" topology Investigation Finding 2 documented.
const mockMCPServerWithChildren = `
sleep 100 &
sleep 100 &
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}'
read notif
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  echo "{\"jsonrpc\":\"2.0\",\"id\":${id},\"result\":{}}"
done
`

// processGroupAlive returns true if the process group identified by pgid is
// still receiving signals (i.e., at least one member is alive). Uses
// syscall.Kill(-pgid, 0) per Story 48.2 ATDD spec: ESRCH ⇒ group gone.
//
// Note: signal 0 is a no-op delivery probe; it returns errno only.
//
// Caller-supplied t lets us distinguish "group alive" / "group gone" from
// "indeterminate" errors (EPERM in restricted namespaces, PID-reused into a
// process the test user cannot signal). Earlier this helper folded EPERM into
// "alive", which produces silent false-greens/reds; now indeterminate errnos
// fail the test loud (code review F7, 2026-05-28).
func processGroupAlive(t *testing.T, pgid int) bool {
	t.Helper()
	err := syscall.Kill(-pgid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	t.Fatalf("processGroupAlive(%d): unexpected errno %v — refusing to interpret as alive/dead", pgid, err)
	return false // unreachable
}

// -----------------------------------------------------------------------------
// AC1: agent kill 触发 finishProcess → Unmount → 进程组内全部后代在 5s 内终止
// -----------------------------------------------------------------------------
//
// Given an MCP transport whose backing shell fixture has 2 sleep children
// (so cmd.Process.Pid is the pgid leader of 3 processes), when transport.Close
// is invoked, then:
//  1. Close returns nil within graceful budget (<200ms expected because the
//     shell honors SIGTERM and the sleeps inherit it via the process group).
//  2. The entire process group is gone — syscall.Kill(-pgid, 0) → ESRCH.
//  3. cmd.Wait completed (no goroutine leak from the internal done channel).
//
// Pre-implementation behaviour (baseline 16f86bc):
//   - Close does cmd.Process.Kill() (SIGKILL on the shell only) + Wait
//   - The 2 sleep children are NOT in a process group with bash (no Setpgid),
//     so they survive the parent's SIGKILL and become orphans reparented to
//     init. ps --ppid <bashPid> returns nothing, but `ps -ef | grep -c "sleep 100"`
//     still shows 2 entries. This is exactly the Investigation Finding 2 bug.
//
// Activating this test (remove t.Skip) will surface that gap.
func TestATDD_48_2_001_Close_KillsSubprocessTree_Graceful(t *testing.T) {
	transport := NewStdioTransport(TransportConfig{
		Command:       "bash",
		Args:          []string{"-c", mockMCPServerWithChildren},
		TimeoutMillis: 3000,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Capture pgid BEFORE Close — once Close returns, the leader may be gone.
	if transport.cmd == nil || transport.cmd.Process == nil {
		t.Fatal("transport.cmd unexpectedly nil after successful Connect")
	}
	pgid, err := syscall.Getpgid(transport.cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid(%d) failed — Task 4.1 (applyProcessGroupIsolation) not wired? err=%v",
			transport.cmd.Process.Pid, err)
	}
	if pgid != transport.cmd.Process.Pid {
		// When Setpgid: true is applied, the child becomes its own group leader,
		// so pgid == pid. A mismatch means Setpgid was not honored.
		t.Fatalf("expected pgid==pid (Setpgid leader) but got pgid=%d pid=%d",
			pgid, transport.cmd.Process.Pid)
	}

	// Give the shell ~150ms to fork the 2 background sleeps before we Close
	// — without this, Close might fire before the children exist and the
	// "process tree" assertion becomes trivially true.
	time.Sleep(150 * time.Millisecond)

	if !processGroupAlive(t, pgid) {
		t.Fatalf("process group %d unexpectedly dead before Close — fixture failed to start", pgid)
	}

	start := time.Now()
	closeErr := transport.Close()
	elapsed := time.Since(start)

	if closeErr != nil {
		t.Fatalf("graceful Close returned non-nil err: %v (expected nil for cooperative fixture)", closeErr)
	}

	// AC1 NFR: graceful path should return well under the 5s timeout. A
	// generous 2s ceiling absorbs cgroup / scheduler jitter on overloaded CI.
	if elapsed >= 2*time.Second {
		t.Errorf("graceful Close took %v ≥ 2s — Wait goroutine likely not joining promptly", elapsed)
	}

	// Allow a brief grace window for the kernel to deliver SIGTERM through the
	// pgid to the sleep children before observing ESRCH.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processGroupAlive(t, pgid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if processGroupAlive(t, pgid) {
		t.Errorf("process group %d still alive 2s after Close — entire tree not cleaned up", pgid)
	}
}

// -----------------------------------------------------------------------------
// AC3: Close 幂等 — 多次调用不 panic 不报错
// -----------------------------------------------------------------------------
//
// Pre-implementation behaviour (baseline 16f86bc):
//   - 2nd Close calls cmd.Process.Kill() on a finished process (ESRCH from
//     OS but Go swallows it) then cmd.Wait() — which panics:
//     "exec: Wait was already called"
//   - This is exactly the bug Story §易错点 #5 names: dev-story MUST gate via
//     `closed` flag or sync.Once.
func TestATDD_48_2_003_Close_Idempotent(t *testing.T) {
	transport := NewStdioTransport(TransportConfig{
		Command:       "bash",
		Args:          []string{"-c", mockMCPServer},
		TimeoutMillis: 3000,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Three sequential Close calls. Baseline's 2nd Close panics on cmd.Wait
	// already-called — recover() inside subtest converts panic to test fail.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Close panicked on subsequent call: %v (closed flag missing?)", r)
		}
	}()

	for i := 1; i <= 3; i++ {
		// Each Close must return promptly — no 5s graceful wait re-trigger.
		start := time.Now()
		err := transport.Close()
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("Close call #%d returned non-nil err: %v", i, err)
		}
		if i > 1 && elapsed >= 500*time.Millisecond {
			t.Errorf("Close call #%d took %v ≥ 500ms — second+ Close should short-circuit", i, elapsed)
		}
	}
}

// -----------------------------------------------------------------------------
// AC4: MCP server 对 SIGTERM 无响应 → 5s 超时后 SIGKILL 强杀 + ErrForceKilled
// -----------------------------------------------------------------------------
//
// Uses testdata/mcp_ignore_sigterm/main.go fixture (a Go program that masks
// SIGTERM via signal.Notify + discard). The fixture is compiled into t.TempDir()
// so the test is hermetic — no dependency on global GOBIN.
//
// Pre-implementation behaviour (baseline 16f86bc):
//   - Close calls cmd.Process.Kill() (SIGKILL, not blockable) — so even on
//     baseline the process dies. But there's NO 5s SIGTERM-first attempt, so
//     well-behaved servers also lose their cleanup window. This test asserts
//     the 5s SIGTERM-first contract.
func TestATDD_48_2_004_Close_TimeoutFallbackToSIGKILL(t *testing.T) {
	// The SIGTERM-immune fixture forces Close to sleep through the ENTIRE
	// escalation window, so inject a short one. The escalation CONTRACT
	// (SIGTERM → full graceful wait → SIGKILL + ErrForceKilled sentinel) is
	// asserted unchanged below with proportionally scaled bounds; the
	// production 5s value lives in the gracefulShutdownTimeout default (NFR-
	// 48-S2, pinned by TestTimeoutDefaultsUnchanged). Not t.Parallel: the
	// injection would race with concurrent Closes — t.Setenv (panics under
	// t.Parallel) enforces that mechanically.
	t.Setenv("RNIX_TEST_TIMEOUT_INJECTION", "1")
	origGraceful := gracefulShutdownTimeout
	gracefulShutdownTimeout = 500 * time.Millisecond
	t.Cleanup(func() { gracefulShutdownTimeout = origGraceful })

	fixtureSrc, err := filepath.Abs("testdata/mcp_ignore_sigterm")
	if err != nil {
		t.Fatalf("resolve fixture src dir: %v", err)
	}
	fixtureBin := filepath.Join(t.TempDir(), "mcp_ignore_sigterm")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", fixtureBin, ".")
	build.Dir = fixtureSrc
	if buildOut, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build fixture failed: %v\n%s", err, buildOut)
	}

	transport := NewStdioTransport(TransportConfig{
		Command:       fixtureBin,
		TimeoutMillis: 10000,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	start := time.Now()
	closeErr := transport.Close()
	elapsed := time.Since(start)

	// Story NFR-48-S2 contract, scaled to the injected 500ms window (production
	// default is 5s): lower bound (98% of window) confirms SIGTERM truly
	// waited the full window; upper bound 3× the window keeps absolute slack
	// (~1s) for timer lateness + SIGKILL reap under loaded -race runners while
	// still catching a stalled Wait join by an order of magnitude.
	if elapsed < 490*time.Millisecond {
		t.Errorf("Close returned in %v — expected ≥ 490ms graceful wait before SIGKILL escalation", elapsed)
	}
	if elapsed > 3*gracefulShutdownTimeout {
		t.Errorf("Close took %v > %v — SIGKILL Wait join is stalling", elapsed, 3*gracefulShutdownTimeout)
	}

	// Sentinel: dev-story (Task 2.5 / 3.2) decided to express this as
	// *types.DriverError{Code: types.ErrForceKilled}. Compare against the
	// constant rather than a hard-coded literal so the assertion tracks any
	// future rename (code review F5 renamed "force_killed" → "FORCE_KILLED" for
	// consistency with the rest of the ErrCode set).
	if closeErr == nil {
		t.Fatal("Close returned nil err on SIGKILL escalation path — sentinel missing")
	}
	var drvErr *types.DriverError
	if !errors.As(closeErr, &drvErr) {
		t.Fatalf("Close err not *types.DriverError: %T (%v)", closeErr, closeErr)
	}
	if drvErr.Code != types.ErrForceKilled {
		t.Errorf("DriverError.Code = %q, want %q (ErrForceKilled)", drvErr.Code, types.ErrForceKilled)
	}
}

// -----------------------------------------------------------------------------
// AC5: Connect 失败回滚路径 — 不残留孤儿子进程
// -----------------------------------------------------------------------------
//
// fixture: bash that spawns a background sleep then writes non-JSON to stdout
// so initialize parsing fails. Story §易错点 #10 reminder: this works because
// the test relies on entire-process-group cleanup, not on bash trap behavior.
//
// Pre-implementation behaviour (baseline 16f86bc):
//   - Connect's failure path calls cmd.Process.Kill() (line 100) + cmd.Wait()
//     — only kills the shell. The background `sleep 100 &` survives.
const mockConnectFailureWithChildren = `
sleep 100 &
echo "not-json-rpc"
exit 0
`

func TestATDD_48_2_005_Connect_FailureCleansSubprocessTree(t *testing.T) {
	// Two waits dominate this test, so shorten both without touching the
	// behavior under test (GROUP CLEANUP after a failed Connect):
	//   - the initialize handshake budget derives from THIS ctx deadline
	//     (handshakeTimeout) and runs to the full deadline because the fixture's
	//     non-JSON line is ignored, not rejected — 2s (down from 5s) still
	//     leaves cold-start headroom for exec'ing bash on a loaded -race
	//     runner (a late start would Fatal on lastChildPID==0);
	//   - the failure rollback reuses the graceful escalation window while the
	//     orphaned background child still holds the pipes — inject a short one
	//     (production 5s). Not t.Parallel — enforced by t.Setenv (panics under
	//     t.Parallel).
	t.Setenv("RNIX_TEST_TIMEOUT_INJECTION", "1")
	origGraceful := gracefulShutdownTimeout
	gracefulShutdownTimeout = 300 * time.Millisecond
	t.Cleanup(func() { gracefulShutdownTimeout = origGraceful })

	transport := NewStdioTransport(TransportConfig{
		Command:       "bash",
		Args:          []string{"-c", mockConnectFailureWithChildren},
		TimeoutMillis: 3000,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Capture the child PID via the transport's lastChildPID atomic counter,
	// which is set inside Connect after cmd.Start (before initialize). Reading
	// it after Connect returns gives a deterministic, race-free snapshot of
	// the PID that was launched and then cleaned up — exactly what we need
	// to verify the process group is gone.
	err := transport.Connect(ctx)
	if err == nil {
		t.Fatal("Connect should fail on non-JSON-RPC response")
	}
	if !strings.Contains(err.Error(), "initialize") && !strings.Contains(err.Error(), "parse") {
		t.Logf("Connect err (informational): %v", err)
	}

	pid := int(transport.lastChildPID.Load())
	if pid == 0 {
		t.Fatal("lastChildPID not recorded — Connect must atomically capture pid after cmd.Start")
	}

	// After Connect returns, the cmd field is cleared (line 102-104) but pgid
	// is whatever Setpgid gave us, equal to the captured pid.
	pgid := pid

	// Allow up to 1s for cleanup to propagate.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if !processGroupAlive(t, pgid) {
			return // pass
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("process group %d still alive 1s after Connect failure — orphan sleep child not cleaned",
		pgid)
}

// -----------------------------------------------------------------------------
// AC8: 未 Connect 的 transport.Close 立即 nil 不发任何信号
// -----------------------------------------------------------------------------
//
// This test is NOT t.Skip'd — baseline 16f86bc already handles `cmd == nil`
// (line 161-163 in transport.go). It anchors the zero-overhead invariant
// during Task 2.3's Close rewrite so a regression is caught immediately.
func TestATDD_48_2_008_Close_NeverConnected_NoSignal(t *testing.T) {
	transport := NewStdioTransport(TransportConfig{
		Command:       "/bin/true", // never invoked
		TimeoutMillis: 3000,
	})

	start := time.Now()
	err := transport.Close()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Close on never-Connected transport returned err: %v", err)
	}
	// Must return promptly — no SIGTERM / 5s wait / SIGKILL machinery should
	// fire when there's no cmd to signal.
	if elapsed >= 100*time.Millisecond {
		t.Errorf("Close took %v ≥ 100ms — Task 2.3 may be entering graceful wait without cmd",
			elapsed)
	}

	// Sanity: a second Close on the never-connected transport is still nil.
	if err := transport.Close(); err != nil {
		t.Errorf("second Close on never-Connected transport returned err: %v", err)
	}
}

// TestTimeoutDefaultsUnchanged pins the production defaults of the injectable
// timeout vars. Since gracefulShutdownTimeout (NFR-48-S2's 5s contract) and
// l2PingTimeout became test-injectable, nothing else asserts their values —
// this guard catches both a silently changed default and an injecting test
// that forgot its t.Cleanup restore (which would poison every later sequential
// test in the package). Not t.Parallel: it must observe un-injected values.
func TestTimeoutDefaultsUnchanged(t *testing.T) {
	if gracefulShutdownTimeout != 5*time.Second {
		t.Errorf("gracefulShutdownTimeout = %v, want 5s (NFR-48-S2 production default; injected value leaked?)", gracefulShutdownTimeout)
	}
	if l2PingTimeout != 2*time.Second {
		t.Errorf("l2PingTimeout = %v, want 2s (Story 48.5 production default; injected value leaked?)", l2PingTimeout)
	}
}
