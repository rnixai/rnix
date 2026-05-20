package ipc

import (
	"context"
	"testing"
	"time"
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
	if gap := time.Since(current); gap > 50*time.Millisecond {
		t.Errorf("heartbeat gap %v exceeds tolerance after recent tick", gap)
	}

	cancel()
	select {
	case <-done:
		// pass
	case <-time.After(200 * time.Millisecond):
		t.Fatal("keepScriptRunnerHeartbeat did not exit within 200ms after ctx cancel")
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
	case <-time.After(200 * time.Millisecond):
		t.Fatal("keeper did not exit after cancel")
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
	case <-time.After(200 * time.Millisecond):
		t.Fatal("keeper did not exit immediately when ctx was pre-cancelled")
	}
}

// Smoke check that the production constant matches the documented cadence
// (10s). If we ever tune this, update the Story 43.1 AC#1 tolerance.
func TestScriptRunnerHeartbeatInterval_IsTenSeconds(t *testing.T) {
	if scriptRunnerHeartbeatInterval != 10*time.Second {
		t.Errorf("scriptRunnerHeartbeatInterval = %v, expected 10s", scriptRunnerHeartbeatInterval)
	}
}
