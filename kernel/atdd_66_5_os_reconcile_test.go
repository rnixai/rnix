package kernel

import (
	"context"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
)

// Story 66.5 — daemon OS process reconcile loop tests.
//
// The classify/warn/reap logic is exercised through the injectable
// scan/kill/owned seams (no real /proc needed); a single //go:build linux
// integration case in atdd_66_5_os_reconcile_linux_test.go proves the real
// scanner reads RNIX_PROC_UUID from /proc/<pid>/environ.

// fakeScanner returns a scripted list of osCliProc per successive round.
type fakeScanner struct {
	rounds [][]osCliProc
	idx    int
}

func (f *fakeScanner) scan() []osCliProc {
	if f.idx >= len(f.rounds) {
		return nil
	}
	out := f.rounds[f.idx]
	f.idx++
	return out
}

// recordingKiller records the OS pids handed to it.
type recordingKiller struct {
	killed []int
}

func (r *recordingKiller) kill(_ context.Context, osPid int) { r.killed = append(r.killed, osPid) }

func newReconciler(sc *fakeScanner, ki *recordingKiller, owned func(string) bool) *osReconciler {
	return &osReconciler{
		scan:       sc.scan,
		kill:       ki.kill,
		owned:      owned,
		candidates: map[int]string{},
	}
}

// TestOSReconcile_OwnedRunningExempt: a UUID owned by a live proc is never
// reaped, even across multiple rounds.
func TestOSReconcile_OwnedRunningExempt(t *testing.T) {
	sc := &fakeScanner{rounds: [][]osCliProc{
		{{OSPid: 100, UUID: "u-owned", Argv: "claude --print"}},
		{{OSPid: 100, UUID: "u-owned", Argv: "claude --print"}},
	}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(uuid string) bool { return uuid == "u-owned" })

	r.reconcileOnce(context.Background())
	r.reconcileOnce(context.Background())

	if len(ki.killed) != 0 {
		t.Fatalf("owned proc must never be reaped, got kills=%v", ki.killed)
	}
	if len(r.candidates) != 0 {
		t.Fatalf("owned proc must not be a candidate, got %v", r.candidates)
	}
}

// TestOSReconcile_TwoRoundConfirmation: an orphan is warned (not killed) on
// round one and reaped only on round two.
func TestOSReconcile_TwoRoundConfirmation(t *testing.T) {
	orphan := osCliProc{OSPid: 200, UUID: "u-orphan", Argv: "claude --print sa-step-runner"}
	sc := &fakeScanner{rounds: [][]osCliProc{{orphan}, {orphan}}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(string) bool { return false })

	r.reconcileOnce(context.Background())
	if len(ki.killed) != 0 {
		t.Fatalf("round one must not kill (warn only), got %v", ki.killed)
	}
	if _, ok := r.candidates[200]; !ok {
		t.Fatalf("round one must register candidate, got %v", r.candidates)
	}

	r.reconcileOnce(context.Background())
	if len(ki.killed) != 1 || ki.killed[0] != 200 {
		t.Fatalf("round two must reap os_pid=200, got %v", ki.killed)
	}
}

// TestOSReconcile_NotInTableIsOrphan: a UUID with no owner at all is treated as
// orphan (reaped on the second round).
func TestOSReconcile_NotInTableIsOrphan(t *testing.T) {
	ghost := osCliProc{OSPid: 300, UUID: "u-ghost"}
	sc := &fakeScanner{rounds: [][]osCliProc{{ghost}, {ghost}}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(string) bool { return false })

	r.reconcileOnce(context.Background())
	r.reconcileOnce(context.Background())

	if len(ki.killed) != 1 || ki.killed[0] != 300 {
		t.Fatalf("unowned ghost must be reaped on round two, got %v", ki.killed)
	}
}

// TestOSReconcile_VanishedCandidateNotKilled: a first-round candidate whose OS
// pid disappears before round two is dropped, not killed.
func TestOSReconcile_VanishedCandidateNotKilled(t *testing.T) {
	orphan := osCliProc{OSPid: 400, UUID: "u-gone"}
	sc := &fakeScanner{rounds: [][]osCliProc{
		{orphan}, // round 1: candidate registered
		{},       // round 2: process gone from scan
	}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(string) bool { return false })

	r.reconcileOnce(context.Background())
	r.reconcileOnce(context.Background())

	if len(ki.killed) != 0 {
		t.Fatalf("vanished candidate must not be killed, got %v", ki.killed)
	}
	if len(r.candidates) != 0 {
		t.Fatalf("vanished candidate must be cleared, got %v", r.candidates)
	}
}

// TestOSReconcile_EmptyUUIDSkipped guards against reaping entries with a blank
// marker (defensive — scanner filters these, but reconcileOnce must too).
func TestOSReconcile_EmptyUUIDSkipped(t *testing.T) {
	sc := &fakeScanner{rounds: [][]osCliProc{
		{{OSPid: 500, UUID: ""}},
		{{OSPid: 500, UUID: ""}},
	}}
	ki := &recordingKiller{}
	r := newReconciler(sc, ki, func(string) bool { return false })

	r.reconcileOnce(context.Background())
	r.reconcileOnce(context.Background())

	if len(ki.killed) != 0 {
		t.Fatalf("blank-UUID entry must be skipped, got %v", ki.killed)
	}
}

// TestUUIDOwnedByLiveProc_StateMatrix验证拍板 5: only Created/Running own a CLI
// child; Suspended/Zombie/Dead and unknown UUIDs are orphan surfaces.
func TestUUIDOwnedByLiveProc_StateMatrix(t *testing.T) {
	k := NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)

	mk := func(state types.ProcessState) string {
		p := NewProcess(0, "intent", nil)
		p.State = state
		k.AddProcess(p)
		return p.UUID
	}

	cases := []struct {
		name  string
		state types.ProcessState
		owned bool
	}{
		{"created", types.StateCreated, true},
		{"running", types.StateRunning, true},
		{"suspended", types.StateSuspended, false}, // 拍板 5: not exempt
		{"zombie", types.StateZombie, false},
		{"dead", types.StateDead, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uuid := mk(c.state)
			if got := k.uuidOwnedByLiveProc(uuid); got != c.owned {
				t.Fatalf("state=%s: owned=%v, want %v", c.name, got, c.owned)
			}
		})
	}

	if k.uuidOwnedByLiveProc("no-such-uuid") {
		t.Fatal("unknown UUID must not be owned")
	}
	if k.uuidOwnedByLiveProc("") {
		t.Fatal("empty UUID must not be owned")
	}
}

// TestOSReconcile_RunLoopFirstScanImmediate proves run() fires an immediate
// first scan and honors ctx cancellation without waiting a full interval.
func TestOSReconcile_RunLoopFirstScanImmediate(t *testing.T) {
	var mu sync.Mutex
	scanned := 0
	r := &osReconciler{
		scan: func() []osCliProc {
			mu.Lock()
			scanned++
			mu.Unlock()
			return nil
		},
		kill:       func(context.Context, int) {},
		owned:      func(string) bool { return false },
		interval:   time.Hour, // long; only the immediate first scan should run
		candidates: map[int]string{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.run(ctx); close(done) }()

	// Give the immediate first scan a moment, then cancel.
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return scanned >= 1 }, 2*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not exit on ctx cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	if scanned != 1 {
		t.Fatalf("expected exactly 1 immediate scan (interval=1h), got %d", scanned)
	}
}

// waitFor polls cond until true or the deadline elapses.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
