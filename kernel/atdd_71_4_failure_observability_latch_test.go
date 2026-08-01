package kernel

import (
	gocontext "context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 71.4 — failure observability (started/completed pairing, failure_kind
// classification) and the qwen-style one-shot latch (set on automatic failure,
// cleared on success, never persisted, process keeps running).
//
// 🔴 AC2 classification tests MUST drive a REAL timeout through
// BuildCompactLLMCall (§11): slowMockLLMFactory blocks in Write so compactCtx's
// deadline fires and the sentinel-wrapping branch runs. setupFallbackKernel's
// fake error (errors.New("compact: LLM call timed out after 30s")) never
// traverses BuildCompactLLMCall, carries no sentinel, and is correctly
// classified "other" — asserting "timeout" against it would only pass with the
// string matching this story removes.

// setupSlowCompactKernel builds a kernel whose LLM device blocks in Write for
// delay, producing real compactCtx timeouts. proc.CompactTimeout is set short so
// the timeout fires quickly. DebugChan is ready for event assertions.
func setupSlowCompactKernel(t *testing.T, delay, compactTimeout time.Duration) (*KernelImpl, *rnixctx.Manager, *Process, types.CtxID) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/slow", slowMockLLMFactory(delay))
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(0)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc := NewProcess(0, "slow compact", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/slow"
	proc.CompactTimeout = compactTimeout
	proc.toolMap = map[string]toolMapping{}
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	return k, ctxMgr, proc, cid
}

// countingSlowMockLLMFile wraps slowMockLLMFile and counts Write invocations, so
// a test can prove a latched second attempt never reached the LLM device.
type countingSlowMockLLMFile struct {
	slowMockLLMFile
	writes *atomic.Int32
}

func (f *countingSlowMockLLMFile) Write(ctx gocontext.Context, data []byte) error {
	f.writes.Add(1)
	return f.slowMockLLMFile.Write(ctx, data)
}

func countingSlowFactory(delay time.Duration, counter *atomic.Int32) vfs.VFSFileFactory {
	return func(string, vfs.OpenFlag, string) (vfs.VFSFile, error) {
		resp, _ := json.Marshal(llmResponse{Content: "<summary>\nSlow summary.\n</summary>"})
		return &countingSlowMockLLMFile{
			slowMockLLMFile: slowMockLLMFile{delay: delay, readData: resp},
			writes:          counter,
		}, nil
	}
}

// drainCompactLatchEvents collects the CompactLatch events emitted so far,
// leaving any other events in the channel.
func drainCompactLatchEvents(t *testing.T, proc *Process) []types.SyscallEvent {
	t.Helper()
	var out []types.SyscallEvent
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "CompactLatch" {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// =============================================================================
// AC2 — failure classification at the point of origin
// =============================================================================

// TestATDD_71_4_AC2_FailureKindTimeout drives a REAL timeout through
// BuildCompactLLMCall (§11) and asserts the structured classification.
//
// 🔴 Mutation teeth: replacing classifyCompactFailure's sentinel check with
// errors.Is(err, gocontext.DeadlineExceeded) turns this test red — the timeout
// branch's error carries ErrCompactTimeout but DeadlineExceeded is NOT on its
// Unwrap chain (the whole reason the sentinels exist, F2).
func TestATDD_71_4_AC2_FailureKindTimeout(t *testing.T) {
	k, ctxMgr, proc, cid := setupSlowCompactKernel(t, 5*time.Second, 100*time.Millisecond)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 1)

	ev := lastCompactEvent(t, proc)
	if ev == nil {
		t.Fatal("no Compact event on the failure path")
	}
	if kind := ev.Args["failure_kind"]; kind != "timeout" {
		t.Errorf("failure_kind = %v, want timeout (real compactCtx deadline through BuildCompactLLMCall)", kind)
	}
	// Provenance: the full error text survives alongside the classification.
	if ce, _ := ev.Args["compact_error"].(string); !strings.Contains(ce, "timed out") {
		t.Errorf("compact_error = %q, want the original timeout text retained", ce)
	}
}

// TestATDD_71_4_AC2_FailureKindCancelled cancels proc.ctx mid-call so
// compactCtx.Err() == Canceled, exercising the cancelled branch.
func TestATDD_71_4_AC2_FailureKindCancelled(t *testing.T) {
	// CompactTimeout large enough that the explicit Cancel (after 50ms) wins the
	// race against the deadline — the failure must be Canceled, not DeadlineExceeded.
	k, ctxMgr, proc, cid := setupSlowCompactKernel(t, 10*time.Second, 5*time.Second)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	go func() {
		time.Sleep(50 * time.Millisecond)
		proc.Cancel() // simulates Suspend()/Kill/shutdown cancelling proc.ctx
	}()

	k.autoCompactIfNeeded(proc, 1)

	ev := lastCompactEvent(t, proc)
	if ev == nil {
		t.Fatal("no Compact event on the failure path")
	}
	if kind := ev.Args["failure_kind"]; kind != "cancelled" {
		t.Errorf("failure_kind = %v, want cancelled", kind)
	}
}

// TestATDD_71_4_AC2_FailureKindOther_FakeTimeout pins §11's guard: the
// fake-timeout error injected by setupFallbackKernel carries no sentinel (it
// never traverses BuildCompactLLMCall), so it must classify as "other" — NOT
// "timeout". Making it say "timeout" would require the string matching AC2
// removes.
func TestATDD_71_4_AC2_FailureKindOther_FakeTimeout(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 1)

	ev := lastCompactEvent(t, proc)
	if ev == nil {
		t.Fatal("no Compact event on the failure path")
	}
	if kind := ev.Args["failure_kind"]; kind != "other" {
		t.Errorf("failure_kind = %v, want other — the harness's fake timeout error carries no "+
			"sentinel and must not be labelled timeout", kind)
	}
}

// TestATDD_71_4_AC2_ClassifyCompactFailure pins the classifier directly,
// including the Unwrap-chain reach through context/compact.go's
// "compact LLM call failed: %w" wrapper.
func TestATDD_71_4_AC2_ClassifyCompactFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"timeout sentinel", ErrCompactTimeout, "timeout"},
		{"cancelled sentinel", ErrCompactCancelled, "cancelled"},
		{"wrapped timeout", gocontext.DeadlineExceeded, "other"}, // bare DeadlineExceeded is NOT the sentinel
		{"plain error", gocontext.Canceled, "other"},
		{"nil", nil, "other"},
	}
	for _, tc := range cases {
		if got := classifyCompactFailure(tc.err); got != tc.want {
			t.Errorf("%s: classifyCompactFailure = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// =============================================================================
// AC1 — started/completed pairing
// =============================================================================

// TestATDD_71_4_AC1_FailureEmitsStartedCompletedPair: one failed automatic
// compaction emits exactly one started and one completed Compact event, sharing
// the correlating `step` key.
func TestATDD_71_4_AC1_FailureEmitsStartedCompletedPair(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 7)

	events := drainCompactEvents(t, proc)
	if len(events) != 2 {
		t.Fatalf("Compact events = %d, want 2 (started + completed); got %v", len(events), events)
	}
	started, completed := events[0], events[1]

	if phase := started.Args["phase"]; phase != "started" {
		t.Errorf("first event phase = %v, want started", phase)
	}
	if started.Args["step"] != 7 {
		t.Errorf("started step = %v, want 7", started.Args["step"])
	}
	if _, ok := started.Args["pre_tokens"].(int); !ok {
		t.Error("started event missing pre_tokens")
	}
	// The started half is a marker, not an outcome: no error, no fallback.
	if _, present := started.Args["compact_error"]; present {
		t.Error("started event must not carry compact_error")
	}

	if _, present := completed.Args["phase"]; present {
		t.Errorf("completed event must not carry a phase key (existing shape preserved), got %v",
			completed.Args["phase"])
	}
	if completed.Args["step"] != 7 {
		t.Errorf("completed step = %v, want 7 — the pair correlates on step", completed.Args["step"])
	}
	if completed.Args["compact_error"] == nil {
		t.Error("completed failure event missing compact_error")
	}
	if completed.Args["failure_kind"] == nil {
		t.Error("completed failure event missing failure_kind")
	}
}

// TestATDD_71_4_AC1_SuccessEmitsStartedCompletedPair: the healthy path also
// pairs, and the completed half keeps its pre-existing fields intact (AC1-②).
func TestATDD_71_4_AC1_SuccessEmitsStartedCompletedPair(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0)
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 3)

	events := drainCompactEvents(t, proc)
	if len(events) != 2 {
		t.Fatalf("Compact events = %d, want 2 (started + completed)", len(events))
	}
	if phase := events[0].Args["phase"]; phase != "started" {
		t.Errorf("first event phase = %v, want started", phase)
	}
	completed := events[1]
	// AC1-② red line: the existing success fields survive untouched.
	for _, key := range []string{"pre_tokens", "post_tokens", "pre_slots", "post_slots", "restored_items", "duration_ms"} {
		if _, ok := completed.Args[key]; !ok {
			t.Errorf("completed success event missing pre-existing field %q", key)
		}
	}
	if _, present := completed.Args["failure_kind"]; present {
		t.Error("success event must not carry failure_kind (avoid the failure_kind:none anti-semantics)")
	}
}

// TestATDD_71_4_AC1_GateNoopEmitsNoStartedEvent (AC6-⑤): the 71.2 pre-flight
// gate is an early exit, not an attempt — it must not leave an orphan started
// event behind (the exact noise the pairing semantics forbids).
func TestATDD_71_4_AC1_GateNoopEmitsNoStartedEvent(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0) // healthy LLM: only the gate can stop it
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	for range 6 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "short"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	// Restore-dominated: extractActivePlan picks this up, Compact restores it
	// untruncated, so the gate declines (same fixture as 71.2's GateDeclines test).
	plan := "[Plan]\n" + strings.Repeat("plan step detail ", 2000)
	if err := ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, plan); err != nil {
		t.Fatalf("AppendMessage plan: %v", err)
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 1)

	if events := drainCompactEvents(t, proc); len(events) != 0 {
		t.Errorf("gate NOOP emitted %d Compact events, want 0 — a declined compaction is an early "+
			"exit, and an orphan started event is precisely the pairing noise AC1 forbids", len(events))
	}
}

// =============================================================================
// AC3 + AC5 — the latch
// =============================================================================

// TestATDD_71_4_AC3_LatchStopsSecondAttempt: after one automatic failure the
// latch is set, and the next autoCompactIfNeeded never reaches the LLM device
// (mock Write count stays 1) nor emits any event.
//
// 🔴 Fixture design is load-bearing against a vacuous pass: the context is
// irreducible prose the mechanical fallback CANNOT reclaim (no leaked tool
// payload, no slot ceiling to drop rounds against), so usage stays above the
// threshold after the first failure. Were the fallback able to free room, the
// second attempt would early-return at the threshold check — and "writes stays
// 1" would hold with the latch removed entirely.
func TestATDD_71_4_AC3_LatchStopsSecondAttempt(t *testing.T) {
	var writes atomic.Int32
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/counting", countingSlowFactory(5*time.Second, &writes))
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, _ := ctxMgr.CtxAlloc(0)
	proc := NewProcess(0, "latch", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/counting"
	proc.CompactTimeout = 100 * time.Millisecond
	proc.toolMap = map[string]toolMapping{}
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Irreducible prose: RoleUser messages the token prune cannot touch, so the
	// first failure's fallback reclaims nothing and usage stays over threshold.
	for range 9 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("irreducible narrative ", 300)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	beforeTokens, _ := ctxMgr.TokenUsage(cid)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 1)

	if writes.Load() != 1 {
		t.Fatalf("first attempt: LLM writes = %d, want 1", writes.Load())
	}
	if !proc.GetCompactLatched() {
		t.Fatal("latch not set after the first automatic failure")
	}
	// Precondition guard: the fallback must NOT have dropped usage below the
	// threshold, or the second attempt's short-circuit would be unattributable.
	afterTokens, _ := ctxMgr.TokenUsage(cid)
	if afterTokens.Percentage <= proc.effectiveCompactThreshold() {
		t.Fatalf("fixture invalid: usage fell to %.1f%% after the first failure — the second "+
			"attempt would early-return at the threshold check regardless of the latch",
			afterTokens.Percentage)
	}
	_ = beforeTokens
	_ = drainCompactEvents(t, proc)
	_ = drainCompactLatchEvents(t, proc)

	// Second attempt: short-circuited at the latch, before any token computation.
	k.autoCompactIfNeeded(proc, 2)

	if writes.Load() != 1 {
		t.Errorf("latched second attempt reached the LLM: writes = %d, want still 1", writes.Load())
	}
	if events := drainCompactEvents(t, proc); len(events) != 0 {
		t.Errorf("latched second attempt emitted %d Compact events, want 0", len(events))
	}
}

// TestATDD_71_4_AC5_LatchKeepsProcessRunning: the latch disables automatic
// compaction, it does NOT terminate the process — the 69.3 best-effort contract
// holds (the "best-effort by contract" comment in the failure branch stays
// load-bearing).
func TestATDD_71_4_AC5_LatchKeepsProcessRunning(t *testing.T) {
	k, ctxMgr, proc, cid := setupSlowCompactKernel(t, 5*time.Second, 100*time.Millisecond)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	k.autoCompactIfNeeded(proc, 1)

	if !proc.GetCompactLatched() {
		t.Fatal("precondition: latch must be set")
	}
	if state := proc.GetState(); state != types.StateRunning {
		t.Errorf("process state = %s, want Running — the latch must not terminate (AC5)", state)
	}
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit != nil {
		t.Errorf("process exited with %+v — latching is not fail-stop", exit)
	}
}

// TestATDD_71_4_AC3_ManualPathUnaffectedByLatch (F4): the manual path calls
// ctxMgr.Compact directly (ipc/server_observe.go:566), never
// autoCompactIfNeeded — the latch is structurally invisible to it. No `force`
// parameter needed or added.
func TestATDD_71_4_AC3_ManualPathUnaffectedByLatch(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0)
	_ = k
	fillLeakyContext(t, ctxMgr, cid, 4)
	proc.SetCompactLatched(true)

	called := 0
	_, err := ctxMgr.Compact(cid, rnixctx.CompactOpts{
		LLMCall: func(string, []rnixctx.Message) (string, error) {
			called++
			return "<summary>manual ok</summary>", nil
		},
		Trigger: "manual",
	})
	if err != nil {
		t.Fatalf("manual Compact must succeed despite the latch: %v", err)
	}
	if called != 1 {
		t.Errorf("LLM called %d times, want 1 — the latch must not gate the manual path", called)
	}
}

// TestATDD_71_4_AC3_ClearCompactLatch covers the exported clearer the manual IPC
// success path calls. Idempotent when not latched.
func TestATDD_71_4_AC3_ClearCompactLatch(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "clear latch", nil)

	proc.SetCompactLatched(true)
	k.ClearCompactLatch(proc)
	if proc.GetCompactLatched() {
		t.Error("ClearCompactLatch did not clear a set latch")
	}
	// Idempotent: clearing an already-clear latch is a no-op, not a panic.
	k.ClearCompactLatch(proc)
	if proc.GetCompactLatched() {
		t.Error("latch re-set by a redundant clear")
	}
}

// TestATDD_71_4_AC3_LatchNotPersistedToDisk (AC3-③ / F7): the latch is on
// vfs.ProcInfo (visible) but NOT on procInfoDisk — daemon restart / resume must
// start unlatched. This is the guard the story demands: ProcInfoWire has no
// TestWireDrift, so nothing else would catch the field being smuggled onto the
// disk schema.
func TestATDD_71_4_AC3_LatchNotPersistedToDisk(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "latch disk", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	proc.SetCompactLatched(true)

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	if !info.CompactLatched {
		t.Fatal("precondition: latch must be visible in the in-memory ProcInfo")
	}

	disk := procInfoToDisk(*info)
	blob, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal procInfoDisk: %v", err)
	}
	if strings.Contains(string(blob), "compact_latched") {
		t.Error("procInfoDisk carries compact_latched — the latch must NOT survive resume (AC3-③): " +
			"resume re-derives the compact timeout, so the failure's cause may have changed, and a " +
			"stale latch would silently disable a P0 mechanism until manual intervention")
	}
	if restored := procInfoFromDisk(disk); restored.CompactLatched {
		t.Error("disk round-trip restored the latch — a resumed process must start unlatched")
	}
}

// =============================================================================
// AC4 — the three observability channels
// =============================================================================

// TestATDD_71_4_AC4_LatchSetEmitsEventOnce: latching is a state transition with
// its own CompactLatch event (step + failure_kind), emitted exactly once — a
// second failure while already latched never reaches the set branch.
func TestATDD_71_4_AC4_LatchSetEmitsEventOnce(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 5)

	latches := drainCompactLatchEvents(t, proc)
	if len(latches) != 1 {
		t.Fatalf("CompactLatch events = %d, want 1 on the first failure", len(latches))
	}
	if latches[0].Args["step"] != 5 {
		t.Errorf("latch event step = %v, want 5", latches[0].Args["step"])
	}
	if latches[0].Args["failure_kind"] != "other" {
		// setupFallbackKernel's fake error classifies as other (§11).
		t.Errorf("latch event failure_kind = %v, want other", latches[0].Args["failure_kind"])
	}

	// Second attempt: short-circuited at the latch — no new latch event.
	k.autoCompactIfNeeded(proc, 6)
	if latches := drainCompactLatchEvents(t, proc); len(latches) != 0 {
		t.Errorf("latched second attempt emitted %d CompactLatch events, want 0 (set is idempotent)",
			len(latches))
	}
}

// TestATDD_71_4_AC4_InvariantHoldsWhenLatched is F1's permanent guardrail: the
// latch is visible on ProcInfo while SuspendReason stays empty, so the Story
// 44.5 invariant still validates. A future implementation that smuggles the
// latch into SuspendReason goes red here immediately.
func TestATDD_71_4_AC4_InvariantHoldsWhenLatched(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "latch invariant", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	proc.SetCompactLatched(true)

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	if !info.CompactLatched {
		t.Error("CompactLatched not projected onto ProcInfo (AC4-② process-level visibility)")
	}
	if info.SuspendReason != "" {
		t.Errorf("SuspendReason = %q, want empty — the latch must NOT reuse SuspendReason (F1: "+
			"invariant.go forbids it on a Running process)", info.SuspendReason)
	}
	if err := ValidateProcInfoInvariant(info); err != nil {
		t.Errorf("invariant violated for a latched Running process: %v", err)
	}
}
