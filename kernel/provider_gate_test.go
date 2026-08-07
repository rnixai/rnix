package kernel

import (
	gocontext "context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Story 73.5 — unit tests for the per-provider concurrency gate
// (D9: TestProviderGate_* naming; clock via the sleepFunc seam from
// retry_backoff.go, ms-level waits only).

// gateLimitProc builds a minimal process with a live ctx + DebugChan event
// sink, plus a kernel whose global concurrency-limit closure returns the
// given limit (level ② of the three-level chain — project-level is nil here,
// so the closure is authoritative).
func gateLimitProc(t *testing.T, limit int, provider string) (*KernelImpl, *Process, *[]types.SyscallEvent, *sync.Mutex) {
	t.Helper()
	k := &KernelImpl{}
	k.SetProviderConcurrencyLimitFunc(func(string) int { return limit })
	proc := NewProcess(0, "73.5 gate unit", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	proc.Provider = provider
	proc.DebugChan = make(chan types.SyscallEvent, 256)
	var mu sync.Mutex
	var evs []types.SyscallEvent
	go func() {
		for ev := range proc.DebugChan {
			mu.Lock()
			evs = append(evs, ev)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		cancel()
		close(proc.DebugChan)
	})
	return k, proc, &evs, &mu
}

// installBlockSleep replaces sleepFunc with one that never fires, parking any
// gate waiter deterministically. Restored via t.Cleanup.
func installBlockSleep(t *testing.T) {
	t.Helper()
	prev := sleepFunc
	sleepFunc = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	t.Cleanup(func() { sleepFunc = prev })
}

// manualSleep is a sleepFunc whose channels fire only when releaseAll is
// called — the deterministic barrier the queued-count test needs.
type manualSleep struct {
	mu      sync.Mutex
	pending []chan time.Time
}

func (m *manualSleep) after(time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	m.mu.Lock()
	m.pending = append(m.pending, ch)
	m.mu.Unlock()
	return ch
}

func (m *manualSleep) releaseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.pending {
		select {
		case ch <- time.Time{}:
		default:
		}
	}
}

func installManualSleep(t *testing.T, m *manualSleep) {
	t.Helper()
	prev := sleepFunc
	sleepFunc = m.after
	t.Cleanup(func() { sleepFunc = prev })
}

// gateWaiters reads the entry's waiter count under the gate lock.
func gateWaiters(g *providerGate, provider string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.entries[provider]; e != nil {
		return e.waiters
	}
	return 0
}

// gateEvents filters the captured event sink for a gate syscall name.
func gateEvents(evs *[]types.SyscallEvent, mu *sync.Mutex, syscall string) []types.SyscallEvent {
	mu.Lock()
	defer mu.Unlock()
	var out []types.SyscallEvent
	for _, ev := range *evs {
		if ev.Syscall == syscall {
			out = append(out, ev)
		}
	}
	return out
}

// pollUntil polls cond until it holds or the deadline expires (test-local;
// kernel tests are sequential, so a tiny real-time poll is safe).
func pollUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// waitAcquireResult asserts an in-flight acquire resolves within the timeout.
func waitAcquireResult(t *testing.T, ch chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		t.Fatal("acquire did not resolve in time")
		return nil
	}
}

// assertBlocked asserts an in-flight acquire stays unresolved (still queued).
func assertBlocked(t *testing.T, ch chan error, window time.Duration) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("acquire unexpectedly resolved: %v", err)
	case <-time.After(window):
	}
}

// --- AC1: concurrency cap — limit 2, third acquire queues ---

func TestProviderGate_ConcurrencyCap_ThirdQueuesUntilRelease(t *testing.T) {
	installBlockSleep(t) // park the queued waiter
	k, pa, _, _ := gateLimitProc(t, 2, "a")
	ctx := pa.ctx

	if err := k.acquireProviderGate(ctx, pa, "a"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := k.acquireProviderGate(ctx, pa, "a"); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	_, pb, _, _ := gateLimitProc(t, 2, "a")
	third := make(chan error, 1)
	go func() { third <- k.acquireProviderGate(ctx, pb, "a") }()
	assertBlocked(t, third, 50*time.Millisecond)

	k.gate().release("a")
	if err := waitAcquireResult(t, third, 2*time.Second); err != nil {
		t.Fatalf("third acquire after release: %v", err)
	}
}

// --- AC1: separate buckets per provider ---

func TestProviderGate_SeparateBuckets(t *testing.T) {
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	// A different provider has its own bucket — must not queue behind "a".
	_, pb, _, _ := gateLimitProc(t, 1, "b")
	if err := k.acquireProviderGate(pb.ctx, pb, "b"); err != nil {
		t.Fatalf("acquire b must not block on a's bucket: %v", err)
	}
	k.gate().release("a")
	k.gate().release("b")
}

// --- AC1: release symmetry — a leaked slot blocks the next acquire ---

func TestProviderGate_ReleaseSymmetry_LeakBlocks(t *testing.T) {
	installBlockSleep(t)
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// WITHOUT releasing, a second acquire must stay blocked (defer guarantee:
	// a leaked slot deadlocks the same provider's later requests).
	_, pb, _, _ := gateLimitProc(t, 1, "a")
	second := make(chan error, 1)
	go func() { second <- k.acquireProviderGate(pb.ctx, pb, "a") }()
	assertBlocked(t, second, 50*time.Millisecond)

	// Release once → the queued acquire passes.
	k.gate().release("a")
	if err := waitAcquireResult(t, second, 2*time.Second); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}

	// Over-release is dropped, not fatal — the bucket keeps working.
	k.gate().release("a")
	k.gate().release("a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire after over-release: %v", err)
	}
	k.gate().release("a")
}

// --- AC6: ctx cancel unblocks a waiter with a cancelled gate error ---

func TestProviderGate_CancelUnblocksWaiter(t *testing.T) {
	installBlockSleep(t)
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, pb, _, _ := gateLimitProc(t, 1, "a")
	waiter := make(chan error, 1)
	go func() { waiter <- k.acquireProviderGate(pb.ctx, pb, "a") }()
	assertBlocked(t, waiter, 50*time.Millisecond)

	pb.cancel()
	err := waitAcquireResult(t, waiter, 2*time.Second)
	var ge *gateError
	if !errors.As(err, &ge) {
		t.Fatalf("want *gateError, got %T: %v", err, err)
	}
	if ge.kind != gateErrCancelled {
		t.Errorf("kind = %v, want gateErrCancelled", ge.kind)
	}
	if !strings.Contains(ge.Error(), "gate cancelled: a") {
		t.Errorf("error text = %q, want cancelled marker", ge.Error())
	}
}

// --- AC6: hard cap — timeout fires (seam-shrunk) with the gate text ---

func TestProviderGate_Timeout(t *testing.T) {
	// Default TestMain sleepFunc fires immediately → the 60s budget elapses
	// in microseconds (D9: never a real 60s wait).
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, pb, _, _ := gateLimitProc(t, 1, "a")
	err := k.acquireProviderGate(pb.ctx, pb, "a")
	var ge *gateError
	if !errors.As(err, &ge) {
		t.Fatalf("want *gateError, got %T: %v", err, err)
	}
	if ge.kind != gateErrTimeout {
		t.Errorf("kind = %v, want gateErrTimeout", ge.kind)
	}
	if ge.Error() != "provider concurrency gate timeout: a" {
		t.Errorf("error text = %q", ge.Error())
	}
}

// --- AC6/D4: heartbeat refreshed during the wait (time-jump assertion) ---

func TestProviderGate_HeartbeatRefreshedDuringWait(t *testing.T) {
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, pb, _, _ := gateLimitProc(t, 1, "a")
	pb.LastHeartbeat = time.Now().Add(-time.Minute) // stale before the wait
	before := time.Now()
	_ = k.acquireProviderGate(pb.ctx, pb, "a") // times out under the seam
	if hb := pb.LastHeartbeatSnapshot(); !hb.After(before) {
		t.Errorf("LastHeartbeat %v not refreshed during the wait (before=%v)", hb, before)
	}
}

// --- AC7: wait + timeout events, zero noise on the fast path ---

func TestProviderGate_WaitAndTimeoutEvents(t *testing.T) {
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, pb, evs, evMu := gateLimitProc(t, 1, "a")
	_ = k.acquireProviderGate(pb.ctx, pb, "a") // times out under the seam

	// Events land in the sink goroutine asynchronously — poll.
	pollUntil(t, "wait + timeout events drained", func() bool {
		return len(gateEvents(evs, evMu, "provider_gate_wait")) == 1 &&
			len(gateEvents(evs, evMu, "provider_gate_timeout")) == 1
	})
	waits := gateEvents(evs, evMu, "provider_gate_wait")
	if len(waits) != 1 {
		t.Fatalf("provider_gate_wait events = %d, want 1", len(waits))
	}
	w := waits[0].Args
	if w["provider"] != "a" {
		t.Errorf("wait event provider = %v, want a", w["provider"])
	}
	if w["limit"] != 1 {
		t.Errorf("wait event limit = %v, want 1", w["limit"])
	}
	if w["queued"] != 1 {
		t.Errorf("wait event queued = %v, want 1 (single waiter includes itself)", w["queued"])
	}
	if _, ok := w["wait_ms"]; !ok {
		t.Error("wait event missing wait_ms")
	}

	timeouts := gateEvents(evs, evMu, "provider_gate_timeout")
	if len(timeouts) != 1 {
		t.Fatalf("provider_gate_timeout events = %d, want 1", len(timeouts))
	}
	to := timeouts[0].Args
	if to["provider"] != "a" || to["limit"] != 1 {
		t.Errorf("timeout event provider/limit = %v/%v, want a/1", to["provider"], to["limit"])
	}
	if _, ok := to["wait_ms"]; !ok {
		t.Error("timeout event missing wait_ms")
	}
}

// --- AC7: queued counts every waiter including self (fixed at 2) ---

func TestProviderGate_QueuedCountsAllWaiters(t *testing.T) {
	ms := &manualSleep{}
	installManualSleep(t, ms)
	k, pa, _, _ := gateLimitProc(t, 1, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, pb, evsB, evMuB := gateLimitProc(t, 1, "a")
	_, pc, evsC, evMuC := gateLimitProc(t, 1, "a")

	waiterB := make(chan error, 1)
	waiterC := make(chan error, 1)
	go func() { waiterB <- k.acquireProviderGate(pb.ctx, pb, "a") }()
	pollUntil(t, "waiter B parked", func() bool { return gateWaiters(k.gate(), "a") == 1 })
	go func() { waiterC <- k.acquireProviderGate(pc.ctx, pc, "a") }()
	pollUntil(t, "waiter C parked", func() bool { return gateWaiters(k.gate(), "a") == 2 })

	// One chunk fires for each waiter → both emit provider_gate_wait while
	// the other is still queued: queued must be 2 (definition: waiters
	// including the emitter), asserted fixed.
	ms.releaseAll()
	pollUntil(t, "both wait events emitted", func() bool {
		return len(gateEvents(evsB, evMuB, "provider_gate_wait")) == 1 &&
			len(gateEvents(evsC, evMuC, "provider_gate_wait")) == 1
	})
	for name, pair := range map[string][2]any{
		"B": {evsB, evMuB},
		"C": {evsC, evMuC},
	} {
		evs := pair[0].(*[]types.SyscallEvent)
		evMu := pair[1].(*sync.Mutex)
		for _, ev := range gateEvents(evs, evMu, "provider_gate_wait") {
			if q, _ := ev.Args["queued"].(int); q != 2 {
				t.Errorf("waiter %s queued = %v, want 2 (two waiters, including self)", name, ev.Args["queued"])
			}
		}
	}
	// Cleanup: cancel both parked waiters so the test cannot leak goroutines.
	pb.cancel()
	pc.cancel()
	_ = waitAcquireResult(t, waiterB, 2*time.Second)
	_ = waitAcquireResult(t, waiterC, 2*time.Second)
}

// --- AC7: healthy fast acquire emits nothing ---

func TestProviderGate_ZeroNoiseOnFastAcquire(t *testing.T) {
	k, pa, evs, evMu := gateLimitProc(t, 2, "a")
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := k.acquireProviderGate(pa.ctx, pa, "a"); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	k.gate().release("a")
	k.gate().release("a")
	if got := gateEvents(evs, evMu, "provider_gate_wait"); len(got) != 0 {
		t.Errorf("provider_gate_wait on fast path: %d events, want 0", len(got))
	}
	if got := gateEvents(evs, evMu, "provider_gate_timeout"); len(got) != 0 {
		t.Errorf("provider_gate_timeout on fast path: %d events, want 0", len(got))
	}
}

// --- AC2: API surface pinned — no rate/window/priority parameters ---

func TestProviderGate_APISurfacePinned(t *testing.T) {
	// Compile-time pin: acquireProviderGate keeps the (ctx, proc, provider)
	// signature — no rate, window, or priority parameters. If a
	// generalization param is ever added, this call stops compiling (AC2:
	// 编译期即钉住, review 比对 API 面).
	k, pa, _, _ := gateLimitProc(t, 4, "pinned")
	if err := k.acquireProviderGate(pa.ctx, pa, "pinned"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	k.gate().release("pinned")
}

// --- D1/D4 constants pinned ---

func TestProviderGate_Constants(t *testing.T) {
	if defaultMaxConcurrency != 4 {
		t.Errorf("defaultMaxConcurrency = %d, want 4 (D1 — epic red line: never 1)", defaultMaxConcurrency)
	}
	if gateAcquireTimeout != maxInProcessWait {
		t.Errorf("gateAcquireTimeout = %v, want maxInProcessWait %v (D4 — reuse, no new constant)", gateAcquireTimeout, maxInProcessWait)
	}
	if gateWaitEmitThreshold != time.Second {
		t.Errorf("gateWaitEmitThreshold = %v, want 1s (D7)", gateWaitEmitThreshold)
	}
}

// --- AC1: empty provider is a no-op (nothing to gate) ---

func TestProviderGate_EmptyProviderNoOp(t *testing.T) {
	k, pa, _, _ := gateLimitProc(t, 1, "")
	if err := k.acquireProviderGate(pa.ctx, pa, ""); err != nil {
		t.Fatalf("empty provider must not block: %v", err)
	}
}
