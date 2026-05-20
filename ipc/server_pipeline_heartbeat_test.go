package ipc

import (
	"context"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// HB-1 (Epic 43, Story 43.1) — these tests verify that the lifecycle-level
// heartbeat keeper actually keeps script-runner processes alive in the
// HeartbeatMonitor's eyes, and exits cleanly when its context is cancelled.

// AC#1 — LastHeartbeat is updated repeatedly while ctx is alive.
// AC#4 — keeper exits within ~100ms of ctx cancel.
func TestKeepScriptRunnerHeartbeat_TouchesUntilCtxCancel(t *testing.T) {
	proc := newRunningScriptProc(t)

	// Seed an initial heartbeat (Spawn does this in production; tests don't
	// go through Spawn) so we can measure forward movement instead of just
	// "field is non-zero".
	proc.TouchHeartbeat()
	initial := proc.LastHeartbeatSnapshot()
	if initial.IsZero() {
		t.Fatal("TouchHeartbeat did not seed LastHeartbeat")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		keepScriptRunnerHeartbeat(ctx, proc, 20*time.Millisecond)
		close(done)
	}()

	// Allow ~5 ticks at 20ms cadence.
	time.Sleep(120 * time.Millisecond)

	current := proc.LastHeartbeatSnapshot()
	if !current.After(initial) {
		t.Fatalf("LastHeartbeat not advanced: initial=%v current=%v", initial, current)
	}
	// Loose gap budget — GC pauses on shared CI runners can easily blow past
	// the 20ms tick cadence. Forward-progress is the load-bearing assertion;
	// this gap check is a sanity floor, not a precision measurement.
	if gap := time.Since(current); gap > 150*time.Millisecond {
		t.Errorf("heartbeat gap %v exceeds tolerance after recent tick", gap)
	}

	cancel()
	select {
	case <-done:
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("keepScriptRunnerHeartbeat did not exit within 100ms after ctx cancel (AC#4)")
	}
}

// AC#3 — repeated TouchHeartbeat under proc.mu is safe even when the keeper
// runs concurrently with another writer (mimicking SpawnAndWait's own ticker).
// Verified under -race.
func TestKeepScriptRunnerHeartbeat_SafeWithConcurrentTouch(t *testing.T) {
	proc := newRunningScriptProc(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		keepScriptRunnerHeartbeat(ctx, proc, 5*time.Millisecond)
		close(done)
	}()

	// Hammer TouchHeartbeat from this goroutine concurrently — must not race
	// or panic.
	deadline := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(deadline) {
		proc.TouchHeartbeat()
	}

	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("keeper did not exit within 100ms after cancel (AC#4)")
	}

	if proc.LastHeartbeatSnapshot().IsZero() {
		t.Fatal("LastHeartbeat should be non-zero after the test ran")
	}
}

// AC#4 — keeper exits even if ctx is already done at start (no first tick).
func TestKeepScriptRunnerHeartbeat_ExitsIfCtxAlreadyDone(t *testing.T) {
	proc := newRunningScriptProc(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done

	done := make(chan struct{})
	go func() {
		keepScriptRunnerHeartbeat(ctx, proc, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// pass — must not block on the ticker
	case <-time.After(100 * time.Millisecond):
		t.Fatal("keeper did not exit immediately when ctx was pre-cancelled (AC#4)")
	}
}

// Smoke check that the production constant matches the documented cadence
// (10s). If we ever tune this, update the Story 43.1 AC#1 tolerance.
func TestScriptRunnerHeartbeatInterval_IsTenSeconds(t *testing.T) {
	if scriptRunnerHeartbeatInterval != 10*time.Second {
		t.Errorf("scriptRunnerHeartbeatInterval = %v, expected 10s", scriptRunnerHeartbeatInterval)
	}
}

// Invariant: the keeper cadence must be strictly smaller than the default
// StepTimeout that Spawn assigns (kernel/spawn.go:355). If anyone tightens
// StepTimeout below this ratio, the keeper stops protecting script-runners
// from STALL detection silently. Pinning the ratio here makes such a regression
// a test failure rather than a production false-positive.
func TestScriptRunnerHeartbeatInterval_LessThanDefaultStepTimeout(t *testing.T) {
	const defaultStepTimeout = 5 * time.Minute // mirrors kernel/spawn.go:355
	if scriptRunnerHeartbeatInterval >= defaultStepTimeout {
		t.Fatalf("interval %v must be < default StepTimeout %v", scriptRunnerHeartbeatInterval, defaultStepTimeout)
	}
	// Want at least 10× safety margin so a stalled tick doesn't immediately
	// trip the monitor; spec Dev Notes documents 30× as the design target.
	if defaultStepTimeout/scriptRunnerHeartbeatInterval < 10 {
		t.Errorf("safety margin %v×: want ≥10×", defaultStepTimeout/scriptRunnerHeartbeatInterval)
	}
}

// =============================================================================
// Integration: drive the keeper against a real Spawn'd SkipReasonLoop process
// (the production handleExecScript wiring), not just the helper in isolation.
// These tests close the spec Task 2.1 / 2.2 gap by routing through kernel.Spawn,
// so a regression in keeper wiring (wrong proc target, wrong interval, missing
// goroutine) surfaces here rather than in production.
// =============================================================================

// newSkipReasonLoopProc spawns a real SkipReasonLoop process the same way
// handleExecScript does (kernel/spawn.go:466 skips provider/reasonStep setup),
// then returns the live *kernel.Process pointer. No VFS/LLM/ctxMgr is required
// because SkipReasonLoop bypasses all of that machinery.
func newSkipReasonLoopProc(t *testing.T) (*kernel.KernelImpl, *kernel.Process, types.PID) {
	t.Helper()
	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { kern.Shutdown() })

	pid, err := kern.Spawn("run: integration.ash", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn(SkipReasonLoop): %v", err)
	}
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("GetProcess(%d): process vanished after Spawn", pid)
	}
	return kern, proc, pid
}

// AC#1 — across a multi-tick "statement gap" the keeper advances LastHeartbeat
// past Spawn's seed value. Mirrors spec Task 2.1's "sleep between SpawnAndWait"
// scenario with a sleep at test cadence.
func TestHandleExecScript_HeartbeatAlive_DuringStmtGap(t *testing.T) {
	_, proc, _ := newSkipReasonLoopProc(t)

	initial := proc.LastHeartbeatSnapshot()
	if initial.IsZero() {
		t.Fatal("Spawn did not seed LastHeartbeat (precondition broken)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		keepScriptRunnerHeartbeat(ctx, proc, 20*time.Millisecond)
		close(done)
	}()

	// Simulate a "statement gap" — the script-runner is doing nothing the
	// kernel can see, but the keeper should still TouchHeartbeat.
	time.Sleep(120 * time.Millisecond)

	if hb := proc.LastHeartbeatSnapshot(); !hb.After(initial) {
		t.Fatalf("LastHeartbeat stayed at Spawn seed during stmt gap: initial=%v current=%v", initial, hb)
	}

	cancel()
	<-done
}

// AC#4 — when scriptCtx is cancelled (CLI disconnect / kill / shutdown), the
// keeper exits promptly so cleanup paths can Reap/Suspend without racing it.
// Mirrors spec Task 2.2.
func TestHandleExecScript_HeartbeatGoroutineExits_OnCtxCancel(t *testing.T) {
	_, proc, _ := newSkipReasonLoopProc(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		keepScriptRunnerHeartbeat(ctx, proc, 20*time.Millisecond)
		close(done)
	}()

	// Give the keeper time to enter its select loop, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("keeper did not exit within 100ms after scriptCtx cancel (AC#4)")
	}
}

// AC#2 — with the keeper running, HeartbeatMonitor.scan must not mark the
// script-runner as stalled even when its StepTimeout is shorter than several
// keeper ticks would normally take to accumulate. Wires a real HeartbeatMonitor
// to a real Spawn'd proc to close the AC#2 verification gap that the unit
// tests above leave open.
func TestHandleExecScript_KeeperPreventsHeartbeatMonitorStall(t *testing.T) {
	kern, proc, _ := newSkipReasonLoopProc(t)

	// Tighten StepTimeout aggressively so the test runs in subsecond time;
	// production uses 5min default + 10s interval, which is the same shape but
	// 6 orders of magnitude slower.
	proc.StepTimeout = 80 * time.Millisecond

	monitor := kernel.NewHeartbeatMonitor(kern, 30*time.Millisecond)
	monitor.Start()
	defer monitor.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keeperDone := make(chan struct{})
	go func() {
		// Keep at well under StepTimeout so heartbeat stays fresh across scans.
		keepScriptRunnerHeartbeat(ctx, proc, 20*time.Millisecond)
		close(keeperDone)
	}()

	// Allow enough wall-time for multiple monitor scan() iterations to run.
	// Each scan reads proc.LastHeartbeat under proc.mu; with the keeper at
	// 20ms and the monitor at 30ms, ~6 scans fit in 200ms.
	time.Sleep(200 * time.Millisecond)

	status := monitor.Status()
	if len(status.CurrentStalled) != 0 {
		t.Errorf("HeartbeatMonitor flagged keeper-protected proc as stalled: %+v", status.CurrentStalled)
	}
	if status.TotalStalledDetected != 0 {
		t.Errorf("TotalStalledDetected = %d, want 0 (AC#2: no STALL false positives)", status.TotalStalledDetected)
	}

	cancel()
	<-keeperDone
}
