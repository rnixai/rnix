package kernel

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Story 66.5 — QA-generated E2E/edge tests (bmad-qa-generate-e2e-tests).
//
// The dev-story ATDD suite (atdd_66_5_os_reconcile_test.go) proves each AC in
// isolation with a CONSTANT ownership function (owned ≡ true or owned ≡ false).
// These tests fill the orthogonal gap the逐-AC suite cannot reach: the two-round
// confirmation's behavior when a UUID's ownership CHANGES BETWEEN ROUNDS — the
// exact races 拍板 7 exists to defuse (spawn mid-flight, twoPhaseShutdown grace
// window, resume recovery). They reuse the fakeScanner / recordingKiller /
// newReconciler / waitFor seams defined in the ATDD file (same package).

// TestOSReconcile_OwnedThenOrphan_RestartsConfirmation covers a process owned on
// round one that becomes an orphan on round two (e.g. its rnix proc was just
// reaped). It must NOT be reaped merely because it has existed across two OS
// scans — the two-round confirmation restarts from the moment ownership is lost,
// so reaping happens on the SECOND orphaned round (overall round three). This is
// the core防误杀 guarantee for the twoPhaseShutdown-grace / spawn-mid-flight race.
func TestOSReconcile_OwnedThenOrphan_RestartsConfirmation(t *testing.T) {
	p := osCliProc{OSPid: 800, UUID: "u-transient", Argv: "claude --print"}
	sc := &fakeScanner{rounds: [][]osCliProc{{p}, {p}, {p}}}
	ki := &recordingKiller{}

	owned := true // round one: a live Running proc owns this UUID
	r := newReconciler(sc, ki, func(string) bool { return owned })

	// Round 1 — owned ⇒ exempt, never registered as a candidate.
	r.reconcileOnce()
	if len(ki.killed) != 0 || len(r.candidates) != 0 {
		t.Fatalf("owned round must not kill or register candidate: killed=%v candidates=%v", ki.killed, r.candidates)
	}

	// Round 2 — owner is gone; this is the FIRST orphaned sighting ⇒ warn only.
	// The fact it survived one OS scan as an owned proc must NOT count toward
	// confirmation.
	owned = false
	r.reconcileOnce()
	if len(ki.killed) != 0 {
		t.Fatalf("first orphaned round must warn, not kill (prior owned scan must not count), got kills=%v", ki.killed)
	}
	if _, ok := r.candidates[800]; !ok {
		t.Fatalf("first orphaned round must register candidate, got %v", r.candidates)
	}

	// Round 3 — still orphaned & alive ⇒ reap.
	r.reconcileOnce()
	if len(ki.killed) != 1 || ki.killed[0] != 800 {
		t.Fatalf("second orphaned round must reap os_pid=800, got %v", ki.killed)
	}
}

// TestOSReconcile_CandidateThenOwned_NotKilled covers the reverse race: a
// process warned as an orphan candidate on round one recovers an owner on round
// two (e.g. resume drives its rnix proc back to Running). Ownership is evaluated
// BEFORE the candidate lookup in reconcileOnce, so it must be exempted and its
// stale candidate entry cleared — never reaped.
func TestOSReconcile_CandidateThenOwned_NotKilled(t *testing.T) {
	p := osCliProc{OSPid: 801, UUID: "u-recovers"}
	sc := &fakeScanner{rounds: [][]osCliProc{{p}, {p}}}
	ki := &recordingKiller{}

	owned := false // round one: orphan
	r := newReconciler(sc, ki, func(string) bool { return owned })

	r.reconcileOnce()
	if _, ok := r.candidates[801]; !ok {
		t.Fatalf("round one must register orphan candidate, got %v", r.candidates)
	}

	// Round 2 — owner recovered ⇒ exempt despite being a prior candidate.
	owned = true
	r.reconcileOnce()
	if len(ki.killed) != 0 {
		t.Fatalf("recovered candidate must NOT be reaped, got kills=%v", ki.killed)
	}
	if len(r.candidates) != 0 {
		t.Fatalf("recovered candidate must be cleared from the candidate table, got %v", r.candidates)
	}
}

// TestOSReconcile_MultipleOrphans_IndependentConfirmation proves the candidate
// table tracks several distinct orphans independently: two orphans (distinct
// UUIDs/pids — e.g. a leader and a reparented subagent) are each warned on round
// one and both reaped on round two.
func TestOSReconcile_MultipleOrphans_IndependentConfirmation(t *testing.T) {
	p1 := osCliProc{OSPid: 810, UUID: "u-leader", Argv: "claude --print"}
	p2 := osCliProc{OSPid: 811, UUID: "u-subagent", Argv: "claude --print sa-step-runner"}
	sc := &fakeScanner{rounds: [][]osCliProc{{p1, p2}, {p1, p2}}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(string) bool { return false })

	r.reconcileOnce()
	if len(ki.killed) != 0 {
		t.Fatalf("round one must warn all, kill none, got %v", ki.killed)
	}
	if len(r.candidates) != 2 {
		t.Fatalf("round one must register both candidates, got %v", r.candidates)
	}

	r.reconcileOnce()
	killedSet := map[int]bool{}
	for _, pid := range ki.killed {
		killedSet[pid] = true
	}
	if !killedSet[810] || !killedSet[811] {
		t.Fatalf("round two must reap both orphans 810 & 811, got %v", ki.killed)
	}
}

// TestOSReconcile_RunLoop_TwoRoundReapEndToEnd wires the reconcileOnce two-round
// logic through the real run() loop (immediate first scan + ticker), proving an
// orphan present across two ticks is reaped with NO manual reconcileOnce calls.
// Complements TestOSReconcile_RunLoopFirstScanImmediate, which only covers the
// single-scan + ctx-cancel path (interval=1h).
func TestOSReconcile_RunLoop_TwoRoundReapEndToEnd(t *testing.T) {
	orphan := osCliProc{OSPid: 820, UUID: "u-run-e2e"}

	var mu sync.Mutex
	var killed []int
	r := &osReconciler{
		scan:       func() []osCliProc { return []osCliProc{orphan} },
		kill:       func(pid int) { mu.Lock(); killed = append(killed, pid); mu.Unlock() },
		owned:      func(string) bool { return false },
		interval:   20 * time.Millisecond, // short: the 2nd tick reaps quickly
		candidates: map[int]string{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.run(ctx); close(done) }()

	// Round 1 (immediate) warns; round 2 (first tick) reaps.
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(killed) >= 1 }, 3*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not exit on ctx cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, pid := range killed {
		if pid == 820 {
			found = true
		}
	}
	if !found {
		t.Fatalf("run loop must reap orphan os_pid=820 across two ticks, got %v", killed)
	}
}
