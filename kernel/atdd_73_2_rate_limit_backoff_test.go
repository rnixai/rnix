package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gocontext "context"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 73.2 — kernel-side: backoff decision and wait (AC4/AC5/AC6/AC8).
//
// AC9 clock seam: the kernel package's TestMain (below) replaces sleepFunc
// with an immediate channel for ALL kernel tests so multi-second backoffs
// stay millisecond-fast. Tests that need to observe the wait itself install
// a recording variant via installSleepRecorder, which saves/restores around
// the test.

// TestMain installs the AC9 sleep seam for the whole kernel package:
// production backoffs are real multi-second waits, and several standing
// suites (73.1's retry-event tests among them) drive processes through this
// path. Without the seam they would burn the exponential 2s/4s/8s waits in
// real time; the immediate channel keeps them fast while the chunk loop,
// heartbeat refreshes, and event emissions all still execute for real.
func TestMain(m *testing.M) {
	sleepFunc = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
	os.Exit(m.Run())
}

// recordingSleep captures the chunk durations the wait loop feeds sleepFunc.
// block=true returns channels that never fire — cancellation tests use it to
// park the process inside the wait deterministically.
type recordingSleep struct {
	mu     sync.Mutex
	chunks []time.Duration
	block  bool
}

func (r *recordingSleep) after(d time.Duration) <-chan time.Time {
	r.mu.Lock()
	r.chunks = append(r.chunks, d)
	block := r.block
	r.mu.Unlock()
	ch := make(chan time.Time, 1)
	if !block {
		ch <- time.Time{}
	}
	return ch
}

func (r *recordingSleep) snapshot() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, len(r.chunks))
	copy(out, r.chunks)
	return out
}

// installSleepRecorder swaps sleepFunc for a recorder and restores the
// previous seam (the TestMain immediate variant) on cleanup. jitter is pinned
// to zero unless the caller wants a specific value — deterministic chunk
// arithmetic beats sampling (AC9).
func installSleepRecorder(t *testing.T, block bool, jitter float64) *recordingSleep {
	t.Helper()
	rec := &recordingSleep{block: block}
	prevSleep, prevJitter := sleepFunc, jitterFunc
	sleepFunc, jitterFunc = rec.after, func() float64 { return jitter }
	t.Cleanup(func() { sleepFunc, jitterFunc = prevSleep, prevJitter })
	return rec
}

// writeErrSequenceLLM fails Writes with errs[0..len-1] in order, then
// succeeds — the shape multi-retry scenarios need (73.1's rateLimitRetryLLM
// only carries a single error).
type writeErrSequenceLLM struct {
	errs  []error
	calls int
}

func (f *writeErrSequenceLLM) Write(_ gocontext.Context, _ []byte) error {
	if f.calls < len(f.errs) {
		err := f.errs[f.calls]
		f.calls++
		return err
	}
	f.calls++
	return nil
}
func (f *writeErrSequenceLLM) Read(_ int) ([]byte, error) { return makeLLMResponse("done", 5), nil }
func (f *writeErrSequenceLLM) Close() error               { return nil }
func (f *writeErrSequenceLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/qwen"}, nil
}
func (f *writeErrSequenceLLM) SupportsToolCalling() bool { return true }

// runBackoffProcess spawns one process on the given LLM file and returns the
// process, its exit status, and every ReasonStep event's args in order.
func runBackoffProcess(t *testing.T, llmFile vfs.VFSFile, intent string) (*Process, ExitStatus, []map[string]any) {
	t.Helper()
	k, baseDir := newFailureRawKernel(t, llmFile, nil, "claude", "")
	pid, err := k.Spawn(intent, nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	var steps []map[string]any
	for _, ev := range evs {
		if ev.Syscall == "ReasonStep" {
			steps = append(steps, ev.Args)
		}
	}
	return proc, exit, steps
}

func filterAction(steps []map[string]any, action string) []map[string]any {
	var out []map[string]any
	for _, s := range steps {
		if s["action"] == action {
			out = append(out, s)
		}
	}
	return out
}

// argMillis reads an integer-millisecond event field tolerantly: the disk
// round-trip (events.jsonl → json.Unmarshal into map[string]any) yields
// float64 for numbers, while in-memory emits carry int64.
func argMillis(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	}
	return 0, false
}

// withWait builds a rate-limit error whose wait fields are already filled —
// the exact shape drivers produce after 73.2 wiring (D8: kernel reads, never
// writes).
func withWait(kind llm.RateLimitKind, msg string, retryAfter time.Duration, source string) error {
	return llm.NewLLMError("qwen", 429, llm.NewRateLimitErrorWithWait(kind, msg, retryAfter, time.Time{}, source))
}

// --- AC5: the double cap (test 9) ---

// TestATDD_73_2_AC5_GiveupBeyondCap ①: a required wait above
// maxInProcessWait is neither waited out nor retried. Story 73.3 updated
// this test to match the disposition it introduced: the 73.2 give-up
// fall-through (rate_limit_giveup → attemptFallback → death) is replaced by
// a quota SUSPENSION with the wake instant recorded (quota_suspend →
// selfSuspend + ResumeAt). The over-cap exit is keyed on the wait DURATION,
// not the kind — hence one throttle and one quota case, both suspending.
func TestATDD_73_2_AC5_GiveupBeyondCap(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "61s required wait",
			err:  withWait(llm.KindThrottle, "try again after 61 seconds", 61*time.Second, "header"),
		},
		{
			// The captured production value: 343831 seconds.
			name: "captured 95.5h quota window",
			err:  withWait(llm.KindQuota, kernelQuotaBody, 343831*time.Second, "header"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := installSleepRecorder(t, false, 0)
			primary := &writeErrSequenceLLM{errs: []error{tc.err, tc.err, tc.err, tc.err}}
			proc, exit, steps := runBackoffProcess(t, primary, "73.2 AC5 giveup")

			suspends := filterAction(steps, "quota_suspend")
			if len(suspends) != 1 {
				t.Fatalf("quota_suspend events = %d, want exactly 1 (steps=%v)", len(suspends), steps)
			}
			g := suspends[0]
			if lm, ok := argMillis(g["limit_ms"]); !ok || lm != maxInProcessWait.Milliseconds() {
				t.Errorf("limit_ms = %v, want %d", g["limit_ms"], maxInProcessWait.Milliseconds())
			}
			if wm, ok := argMillis(g["required_wait_ms"]); !ok || time.Duration(wm)*time.Millisecond <= maxInProcessWait {
				t.Errorf("required_wait_ms = %v, want a required wait above the cap", g["required_wait_ms"])
			}
			if _, hasKind := g["rate_limit_kind"]; !hasKind {
				t.Error("quota_suspend event must carry rate_limit_kind")
			}
			if _, hasResumeAt := g["resume_at"]; !hasResumeAt {
				t.Error("quota_suspend event must carry resume_at — the wake instant the scanner waits for")
			}
			if giveups := filterAction(steps, "rate_limit_giveup"); len(giveups) != 0 {
				t.Errorf("rate_limit_giveup events = %d, want 0 — the 73.2 give-up shape is replaced by quota_suspend", len(giveups))
			}
			if retries := filterAction(steps, "transient_retry"); len(retries) != 0 {
				t.Errorf("transient_retry events = %d, want 0 — over-cap means no retry", len(retries))
			}
			if n := len(rec.snapshot()); n != 0 {
				t.Errorf("sleepFunc called %d times, want 0 — the over-cap path must not wait at all", n)
			}
			// Story 73.3: the over-cap exit SUSPENDS — it no longer kills.
			if exit.Code != ExitSuspended {
				t.Errorf("exit.Code = %d, want %d (ExitSuspended) — over-cap is a suspension, not a death", exit.Code, ExitSuspended)
			}
			if st := proc.GetState(); st != types.StateSuspended {
				t.Errorf("state = %s, want %s", st, types.StateSuspended)
			}
			if r := proc.GetSuspendReason(); r != SuspendReasonQuotaExhausted {
				t.Errorf("SuspendReason = %q, want %q", r, SuspendReasonQuotaExhausted)
			}
			if proc.GetResumeAt().IsZero() {
				t.Error("ResumeAt zero, want the derived wake instant")
			}
		})
	}
}

// TestATDD_73_2_AC5_RetryCountCap ②: the NFR1 anti-pattern — a provider
// returning 59s on EVERY attempt must stop after maxRateLimitRetries, not
// hold the process indefinitely. jitter is pinned to 1.0 (maximum) on
// purpose: 59s × 1.25 = 73.75s would cross the cap without the post-jitter
// clamp, falsely promoting an in-cap wait into give-up territory.
func TestATDD_73_2_AC5_RetryCountCap(t *testing.T) {
	installSleepRecorder(t, false, 1.0)
	err := withWait(llm.KindThrottle, "try again after 59 seconds", 59*time.Second, "header")
	errs := make([]error, 8) // far more failures than any budget
	for i := range errs {
		errs[i] = err
	}
	primary := &writeErrSequenceLLM{errs: errs}
	_, exit, steps := runBackoffProcess(t, primary, "73.2 AC5 count cap")

	retries := filterAction(steps, "transient_retry")
	if len(retries) != maxRateLimitRetries {
		t.Fatalf("transient_retry events = %d, want exactly %d (maxRateLimitRetries)", len(retries), maxRateLimitRetries)
	}
	if giveups := filterAction(steps, "rate_limit_giveup"); len(giveups) != 0 {
		t.Errorf("rate_limit_giveup events = %d, want 0 — 59s is INSIDE the cap; jitter must not leak it past", len(giveups))
	}
	// Every waited duration is the jitter result clamped to the cap.
	for i, r := range retries {
		if wm, ok := argMillis(r["wait_ms"]); !ok || wm != maxInProcessWait.Milliseconds() {
			t.Errorf("retry %d wait_ms = %v, want %d (clamped)", i, r["wait_ms"], maxInProcessWait.Milliseconds())
		}
		if ram, ok := argMillis(r["retry_after_ms"]); !ok || ram != 59000 {
			t.Errorf("retry %d retry_after_ms = %v, want 59000 (server value unmodified)", i, r["retry_after_ms"])
		}
	}
	if exit.Code == 0 {
		t.Error("exit code 0, want failure after budget exhaustion")
	}
}

// --- AC5 / D5: counter separation (test 10) ---

// TestATDD_73_2_AC5_CounterSeparation: the rate-limit family and the legacy
// socket/EOF/timeout path keep INDEPENDENT counters that never consume each
// other's budget; the non-rate-limit `< 2` threshold stays verbatim.
func TestATDD_73_2_AC5_CounterSeparation(t *testing.T) {
	installSleepRecorder(t, false, 0)
	socketErr := llm.NewLLMError("qwen", 0, fmt.Errorf("socket hang up: %w", llm.ErrTransient))
	rlErr := withWait(llm.KindThrottle, "try again after 3 seconds", 3*time.Second, "header")

	t.Run("socket budget does not feed the rate-limit budget", func(t *testing.T) {
		// Two socket failures exhaust the legacy `< 2` budget; a subsequent
		// rate limit must still get its own full budget and recover.
		primary := &writeErrSequenceLLM{errs: []error{socketErr, socketErr, rlErr}}
		_, exit, steps := runBackoffProcess(t, primary, "73.2 D5 separation A")
		if exit.Code != 0 {
			t.Fatalf("exit = %+v, want success — the rate-limit retry after two socket retries must still land", exit)
		}
		retries := filterAction(steps, "transient_retry")
		if len(retries) != 3 {
			t.Fatalf("transient_retry events = %d, want 3 (2 socket + 1 rate-limit)", len(retries))
		}
		for i, r := range retries[:2] {
			if _, hasWait := r["wait_ms"]; hasWait {
				t.Errorf("socket retry %d carries wait_ms — the legacy path must stay zero-delay and field-less", i)
			}
		}
		if _, hasWait := retries[2]["wait_ms"]; !hasWait {
			t.Error("rate-limit retry lacks wait_ms")
		}
	})

	t.Run("rate-limit budget does not feed the socket budget", func(t *testing.T) {
		// Three rate-limit failures exhaust maxRateLimitRetries; a subsequent
		// socket failure must still get the legacy budget and recover.
		primary := &writeErrSequenceLLM{errs: []error{rlErr, rlErr, rlErr, socketErr}}
		_, exit, steps := runBackoffProcess(t, primary, "73.2 D5 separation B")
		if exit.Code != 0 {
			t.Fatalf("exit = %+v, want success — the socket retry after an exhausted rate-limit budget must still land", exit)
		}
		retries := filterAction(steps, "transient_retry")
		if len(retries) != maxRateLimitRetries+1 {
			t.Fatalf("transient_retry events = %d, want %d", len(retries), maxRateLimitRetries+1)
		}
		last := retries[len(retries)-1]
		if _, hasWait := last["wait_ms"]; hasWait {
			t.Error("the trailing socket retry carries wait_ms — wrong branch")
		}
	})

	t.Run("non-rate-limit threshold stays < 2 verbatim", func(t *testing.T) {
		// Three socket failures: retries 1 and 2 land, the third failure
		// exhausts the budget (2 < 2 is false) and dies — exactly pre-73.2.
		primary := &writeErrSequenceLLM{errs: []error{socketErr, socketErr, socketErr}}
		_, exit, steps := runBackoffProcess(t, primary, "73.2 D5 legacy threshold")
		if exit.Code == 0 {
			t.Fatal("exit code 0, want failure after the legacy2-retry budget")
		}
		if retries := filterAction(steps, "transient_retry"); len(retries) != 2 {
			t.Fatalf("transient_retry events = %d, want 2 (legacy `< 2` budget unchanged)", len(retries))
		}
	})
}

// --- AC6: interruptibility and heartbeat (tests 11 & 12) ---

// TestATDD_73_2_AC6_CancelInterruptsWait: a cancel arriving mid-wait exits
// immediately via the SAME interrupted path as a cancel during the write —
// one SIGTERM, one exit_reason, regardless of timing.
func TestATDD_73_2_AC6_CancelInterruptsWait(t *testing.T) {
	rec := installSleepRecorder(t, true, 0) // block forever inside the wait
	rlErr := withWait(llm.KindThrottle, "try again after 30 seconds", 30*time.Second, "header")
	primary := &writeErrSequenceLLM{errs: []error{rlErr, rlErr, rlErr, rlErr}}

	k, baseDir := newFailureRawKernel(t, primary, nil, "claude", "")
	pid, err := k.Spawn("73.2 AC6 cancel mid-wait", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	// Wait until the process is parked inside the chunked wait.
	deadline := time.Now().Add(3 * time.Second)
	for len(rec.snapshot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("process never entered the backoff wait")
		}
		time.Sleep(2 * time.Millisecond)
	}

	start := time.Now()
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	exit := waitDone(t, proc)

	// ① immediate return — a 30s wait with a blocked sleep can only exit
	// this fast by responding to the cancel.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("exit took %v after kill — the wait did not respond to cancellation", elapsed)
	}
	// ②+③ the interrupted path, with the canonical exit_reason — no new
	// variant born from the wait.
	if exit.Reason != "interrupted" {
		t.Fatalf("exit.Reason = %q, want %q (the handleInterruptedWrite path)", exit.Reason, "interrupted")
	}
	if exit.Code != 1 {
		t.Errorf("exit.Code = %d, want 1", exit.Code)
	}

	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	var sawInterrupted bool
	for _, ev := range evs {
		if ev.Syscall == "ReasonStep" && ev.Args["action"] == "interrupted" {
			sawInterrupted = true
		}
	}
	if !sawInterrupted {
		t.Error("no interrupted event emitted")
	}
}

// TestATDD_73_2_SuspendDuringBackoffWait (review 2026-08-03): SIGPAUSE
// cancels proc.ctx exactly like SIGTERM (suspendOneForSubtree sets
// suspendRequested, then Cancel), and the chunked wait is a NEW cancel window
// the Story 44.5 AC1 guard at the Write-error path never covered. The wait's
// interrupt exit must distinguish: a pause hands off to suspendProcess (the
// process lives on, Suspended); only a kill routes through
// handleInterruptedWrite. Without the guard the process died "interrupted"
// and the waiting suspendOneForSubtree got an illegal-transition error from
// suspendProcess on the already-Dead process.
func TestATDD_73_2_SuspendDuringBackoffWait(t *testing.T) {
	rec := installSleepRecorder(t, true, 0) // block forever inside the wait
	rlErr := withWait(llm.KindThrottle, "try again after 30 seconds", 30*time.Second, "header")
	primary := &writeErrSequenceLLM{errs: []error{rlErr, rlErr, rlErr, rlErr}}

	k, baseDir := newFailureRawKernel(t, primary, nil, "claude", "")
	pid, err := k.Spawn("73.2 suspend mid-wait", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	// Wait until the process is parked inside the chunked wait.
	deadline := time.Now().Add(3 * time.Second)
	for len(rec.snapshot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("process never entered the backoff wait")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// SIGPAUSE mid-wait must succeed — a kill-instead-of-suspend regression
	// surfaces here as "suspend_failed" (suspendProcess rejecting the
	// already-Dead process).
	if err := k.Signal(pid, types.SIGPAUSE); err != nil {
		t.Fatalf("Signal(SIGPAUSE) mid-wait: %v — the suspend hand-off failed", err)
	}
	if st := proc.GetState(); st != types.StateSuspended {
		t.Fatalf("state after SIGPAUSE mid-wait = %s, want %s", st, types.StateSuspended)
	}

	// The interrupted exit path must NOT have run: no interrupted event, no
	// exit status. (handleInterruptedWrite emits action=interrupted before
	// finishProcess; the suspend hand-off emits neither.)
	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.Syscall == "ReasonStep" && ev.Args["action"] == "interrupted" {
			t.Fatal("interrupted event emitted — SIGPAUSE mid-wait took the kill path, not the suspend hand-off")
		}
	}
}

// TestATDD_73_2_AC6_HeartbeatRefreshedDuringWait (D6): a 60s wait runs as
// heartbeatRefreshInterval chunks and refreshes LastHeartbeat on each —
// without this, every process with StepTimeout < 60s would register a false
// stall during backoff. The server-declared 60s value also proves the bypass
// of the 30s LOCAL cap (only the 60s hard cap bounds server waits).
func TestATDD_73_2_AC6_HeartbeatRefreshedDuringWait(t *testing.T) {
	rec := installSleepRecorder(t, false, 0) // zero jitter → exactly 60s
	rlErr := withWait(llm.KindThrottle, "try again after 60 seconds", 60*time.Second, "header")
	primary := &writeErrSequenceLLM{errs: []error{rlErr}}

	before := time.Now()
	proc, exit, _ := runBackoffProcess(t, primary, "73.2 D6 heartbeat")
	if exit.Code != 0 {
		t.Fatalf("exit = %+v, want success after the single backoff", exit)
	}

	chunks := rec.snapshot()
	wantChunks := int(maxInProcessWait / heartbeatRefreshInterval) // 60s / 10s
	if len(chunks) < wantChunks {
		t.Fatalf("chunks = %d (%v), want >= %d — the wait must be split into heartbeat-sized pieces", len(chunks), chunks, wantChunks)
	}
	var total time.Duration
	for _, c := range chunks {
		if c > heartbeatRefreshInterval {
			t.Errorf("chunk %v exceeds heartbeatRefreshInterval %v", c, heartbeatRefreshInterval)
		}
		total += c
	}
	if total != maxInProcessWait {
		t.Errorf("chunk total = %v, want %v", total, maxInProcessWait)
	}
	// Every chunk iteration calls TouchHeartbeat; after the wait the
	// heartbeat must be fresh (the whole run is microseconds under the seam,
	// so any staleness would mean the refresh never happened).
	if since := time.Since(proc.LastHeartbeatSnapshot()); since > heartbeatRefreshInterval {
		t.Errorf("LastHeartbeat is %v stale after the wait — refreshes did not run", since)
	}
	if hb := proc.LastHeartbeatSnapshot(); hb.Before(before) {
		t.Errorf("LastHeartbeat %v predates the spawn %v", hb, before)
	}
}

// --- AC8: event shape (test 13) ---

// TestATDD_73_2_AC8_EventFields: the four new fields ride the existing
// transient_retry event on the rate-limit path only; the socket path stays
// field-less (anti-semantic-placeholder discipline).
func TestATDD_73_2_AC8_EventFields(t *testing.T) {
	installSleepRecorder(t, false, 0)
	socketErr := llm.NewLLMError("qwen", 0, fmt.Errorf("socket hang up: %w", llm.ErrTransient))
	rlErr := withWait(llm.KindThrottle, "try again after 6 seconds", 6*time.Second, "header")
	primary := &writeErrSequenceLLM{errs: []error{socketErr, rlErr}}
	_, exit, steps := runBackoffProcess(t, primary, "73.2 AC8 events")
	if exit.Code != 0 {
		t.Fatalf("exit = %+v, want success", exit)
	}

	retries := filterAction(steps, "transient_retry")
	if len(retries) != 2 {
		t.Fatalf("transient_retry events = %d, want 2", len(retries))
	}

	// Socket retry: legacy four fields, none of the new ones.
	sock := retries[0]
	for _, field := range []string{"step", "action", "attempt", "reason"} {
		if _, ok := sock[field]; !ok {
			t.Errorf("socket retry missing legacy field %q", field)
		}
	}
	for _, field := range []string{"wait_ms", "wait_source", "retry_after_ms", "reset_at", "rate_limit_kind"} {
		if v, present := sock[field]; present {
			t.Errorf("socket retry carries %q=%v — the new fields are rate-limit-only", field, v)
		}
	}

	// Rate-limit retry: legacy + 73.1's kind + the four new fields.
	rl := retries[1]
	for _, field := range []string{"step", "action", "attempt", "reason", "rate_limit_kind", "wait_ms", "wait_source"} {
		if _, ok := rl[field]; !ok {
			t.Errorf("rate-limit retry missing field %q", field)
		}
	}
	waitMs, _ := argMillis(rl["wait_ms"])
	retryAfterMs, hasRA := argMillis(rl["retry_after_ms"])
	if !hasRA || retryAfterMs != 6000 {
		t.Errorf("retry_after_ms = %v (present=%v), want 6000", rl["retry_after_ms"], hasRA)
	}
	if waitMs < retryAfterMs {
		t.Errorf("wait_ms %d < retry_after_ms %d — jitter is one-sided, the wait can never undershoot the server value", waitMs, retryAfterMs)
	}
	if rl["wait_source"] != "header" {
		t.Errorf("wait_source = %v, want header", rl["wait_source"])
	}
	if rl["rate_limit_kind"] != "throttle" {
		t.Errorf("rate_limit_kind = %v, want throttle", rl["rate_limit_kind"])
	}
}

// TestATDD_73_2_AC8_BackoffSourceAndResetAt covers level-3 backoff
// (wait_source "backoff", no retry_after_ms) and the reset_at rendering for
// body-parsed quota windows.
func TestATDD_73_2_AC8_BackoffSourceAndResetAt(t *testing.T) {
	installSleepRecorder(t, false, 0)

	t.Run("no server instruction → backoff source", func(t *testing.T) {
		// Plain constructor: CLI-shaped rate limit with no wait fields.
		rlErr := llm.NewLLMError("claude", 429, llm.NewRateLimitError(llm.KindThrottle, ""))
		primary := &writeErrSequenceLLM{errs: []error{rlErr}}
		_, exit, steps := runBackoffProcess(t, primary, "73.2 AC8 backoff source")
		if exit.Code != 0 {
			t.Fatalf("exit = %+v, want success", exit)
		}
		retries := filterAction(steps, "transient_retry")
		if len(retries) != 1 {
			t.Fatalf("transient_retry events = %d, want 1", len(retries))
		}
		r := retries[0]
		if r["wait_source"] != "backoff" {
			t.Errorf("wait_source = %v, want backoff", r["wait_source"])
		}
		if _, hasRA := r["retry_after_ms"]; hasRA {
			t.Error("retry_after_ms present on a local-backoff retry — server fields are omitted when absent")
		}
		if wm, ok := argMillis(r["wait_ms"]); !ok || wm != rateLimitBackoffBase.Milliseconds() {
			t.Errorf("wait_ms = %v, want %d (attempt 1 = base)", r["wait_ms"], rateLimitBackoffBase.Milliseconds())
		}
	})

	t.Run("quota reset instant → reset_at field", func(t *testing.T) {
		resetAt := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
		rlErr := llm.NewLLMError("qwen", 429,
			llm.NewRateLimitErrorWithWait(llm.KindQuota, kernelQuotaBody, 0, resetAt, "body"))
		primary := &writeErrSequenceLLM{errs: []error{rlErr}}
		_, exit, steps := runBackoffProcess(t, primary, "73.2 AC8 reset_at")
		if exit.Code != 0 {
			t.Fatalf("exit = %+v, want success", exit)
		}
		retries := filterAction(steps, "transient_retry")
		if len(retries) != 1 {
			t.Fatalf("transient_retry events = %d, want 1", len(retries))
		}
		if got := retries[0]["reset_at"]; got != resetAt.Format(time.RFC3339) {
			t.Errorf("reset_at = %v, want %v", got, resetAt.Format(time.RFC3339))
		}
		if retries[0]["wait_source"] != "body" {
			t.Errorf("wait_source = %v, want body", retries[0]["wait_source"])
		}
	})
}

// --- AC4: jitter and local backoff arithmetic (test 8) ---

func TestATDD_73_2_AC4_JitterOneSided(t *testing.T) {
	base := 4 * time.Second
	origJitter := jitterFunc
	t.Cleanup(func() { jitterFunc = origJitter })
	for _, j := range []float64{0, 1} {
		jitterFunc = func() float64 { return j }
		got := applyRateLimitJitter(base)
		if got < base {
			t.Errorf("jitter=%v: applyRateLimitJitter(%v) = %v — jitter must never shorten the wait", j, base, got)
		}
		want := base + time.Duration(j*0.25*float64(base))
		if got != want {
			t.Errorf("jitter=%v: got %v, want %v", j, got, want)
		}
	}
	jitterFunc = func() float64 { return 0.5 }
	if got, want := applyRateLimitJitter(base), base+base/8; got != want {
		t.Errorf("jitter=0.5: got %v, want %v", got, want)
	}
}

func TestATDD_73_2_AC4_LocalBackoffSequence(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := localRateLimitBackoff(i + 1); got != w {
			t.Errorf("localRateLimitBackoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	// Saturation: absurd attempt counts clamp to maxDelay, never overflow.
	if got := localRateLimitBackoff(500); got != rateLimitBackoffMaxDelay {
		t.Errorf("localRateLimitBackoff(500) = %v, want %v", got, rateLimitBackoffMaxDelay)
	}
}

// --- AC7: layering counter-proof (test 14) ---

// TestATDD_73_2_AC7_DriversNeverWait asserts the AC7 layering invariant
// structurally: no file in drivers/llm sleeps or imports the kernel — waits
// travel on the error value, decisions live in the kernel.
func TestATDD_73_2_AC7_DriversNeverWait(t *testing.T) {
	files, err := filepath.Glob("../drivers/llm/*.go")
	if err != nil || len(files) == 0 {
		// Fallback for unusual working directories (go test's cwd is the
		// package directory, so the form above is the canonical one).
		files, err = filepath.Glob("../../drivers/llm/*.go")
		if err != nil || len(files) == 0 {
			t.Skip("cannot locate drivers/llm sources from the test working directory")
		}
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(data)
		if strings.Contains(src, "time.Sleep(") {
			t.Errorf("%s contains time.Sleep — drivers must never wait; backoff lives in the kernel (AC7)", f)
		}
		if strings.Contains(src, `"github.com/rnixai/rnix/kernel"`) {
			t.Errorf("%s imports kernel — forbidden dependency direction (AC7)", f)
		}
	}
}
