package kernel

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 69.3 — compact failure now falls back to a deterministic, LLM-free
// reclamation instead of giving up (AC3), precompact tops up slots instead of
// declaring insufficiency (AC4), and both resume kill points reclaim before
// terminating (AC6 / F1).

// failingCompactLLMFactory returns a device whose Write always fails, i.e. the
// compaction LLM call cannot succeed. Mirrors the incident, where all 27
// attempts died on the 30s timeout.
func failingCompactLLMFactory(failure error) vfs.VFSFileFactory {
	return func(string, vfs.OpenFlag, string) (vfs.VFSFile, error) {
		return &mockLLMFile{writeErr: failure}, nil
	}
}

// setupFallbackKernel builds a kernel whose LLM device always fails, with a
// buffered DebugChan so emitted events can be inspected.
func setupFallbackKernel(t *testing.T, maxSize int) (*KernelImpl, *rnixctx.Manager, *Process, types.CtxID) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", failingCompactLLMFactory(errors.New("compact: LLM call timed out after 30s")))
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(maxSize)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	proc := NewProcess(0, "fallback test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{}
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	return k, ctxMgr, proc, cid
}

// drainCompactEvents collects the Compact events emitted so far.
func drainCompactEvents(t *testing.T, proc *Process) []types.SyscallEvent {
	t.Helper()
	var out []types.SyscallEvent
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "Compact" {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// fillLeakyContext writes `rounds` API rounds of user → assistant(tool_calls) →
// tool(big result) straight into the context.
func fillLeakyContext(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID, rounds int) {
	t.Helper()
	ctxObj, err := ctxMgr.GetContext(cid)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	payload := strings.Repeat("tool payload ", 250) // > LeakedThreshold
	for i := range rounds {
		id := "call-" + strings.Repeat("x", i+1)
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "please run step"); err != nil {
			t.Fatalf("AppendMessage user: %v", err)
		}
		if err := ctxMgr.AppendAssistantWithToolCalls(cid, "running", "", nil,
			[]rnixctx.ToolCall{{ID: id, Name: "Bash"}}); err != nil {
			t.Fatalf("AppendAssistantWithToolCalls: %v", err)
		}
		if err := ctxMgr.AppendToolResult(cid, id, payload); err != nil {
			t.Fatalf("AppendToolResult: %v", err)
		}
	}
	_ = ctxObj
}

// assertKernelPairing checks the tool_use ↔ tool_result invariant on a live
// context. This is the story's most important guard (AC9 ④).
func assertKernelPairing(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID) {
	t.Helper()
	prompt, err := ctxMgr.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	present := make(map[string]bool)
	for _, msg := range prompt.Messages {
		if msg.Role == rnixctx.RoleTool && msg.ToolCallID != "" {
			present[msg.ToolCallID] = true
		}
	}
	for i, msg := range prompt.Messages {
		if msg.Role != rnixctx.RoleAssistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if !present[tc.ID] {
				t.Fatalf("pairing broken: assistant msg[%d] tool_call %q has no tool result", i, tc.ID)
			}
		}
	}
}

// --- AC3: autoCompactIfNeeded falls back instead of giving up ---

func TestATDD_69_3_AC3_CompactFailureFallsBackMechanically(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 12)
	// Story 71.1 AC3: the slot trigger is gone, so the compaction must be driven
	// over the TOKEN threshold instead (this fixture used to rely on 36/40 slots).
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	beforeTokens, _ := ctxMgr.TokenUsage(cid)
	_ = drainCompactEvents(t, proc) // clear anything prior

	k.autoCompactIfNeeded(proc, 7)

	// The process must still be alive: this path is best-effort by contract.
	if state := proc.GetState(); state != types.StateRunning {
		t.Errorf("process state = %s, want Running (compact fallback must never terminate)", state)
	}
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit != nil {
		t.Errorf("process exited with %+v — the fallback path must not terminate the process", exit)
	}

	afterTokens, _ := ctxMgr.TokenUsage(cid)
	if afterTokens.Used >= beforeTokens.Used {
		t.Errorf("tokens did not drop: %d → %d (mechanical fallback reclaimed nothing)", beforeTokens.Used, afterTokens.Used)
	}

	events := drainCompactEvents(t, proc)
	if len(events) == 0 {
		t.Fatal("no Compact event emitted on the failure path")
	}
	ev := events[len(events)-1]
	if ev.Args["fallback"] != mechanicalFallbackName {
		t.Errorf("event fallback = %v, want %q", ev.Args["fallback"], mechanicalFallbackName)
	}
	if ev.Args["compact_error"] == nil {
		t.Error("event missing compact_error (the original LLM failure must be recorded)")
	}
	// AC3: pre_tokens must come from the pre-attempt usage snapshot, not from a
	// nil CompactResult.
	if pre, ok := ev.Args["pre_tokens"].(int); !ok || pre != beforeTokens.Used {
		t.Errorf("pre_tokens = %v, want %d (must be sourced from usage.Used)", ev.Args["pre_tokens"], beforeTokens.Used)
	}
	if post, ok := ev.Args["post_tokens"].(int); !ok || post != afterTokens.Used {
		t.Errorf("post_tokens = %v, want %d", ev.Args["post_tokens"], afterTokens.Used)
	}
	// AC7: never fake a post-compact restore on a mechanical path.
	if _, present := ev.Args["restored_items"]; present {
		t.Error("degraded event must not carry restored_items (mechanical reclamation restores nothing)")
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_3_AC7_SuccessPathHasNoFallbackKey pins the discriminator: a
// successful LLM compaction must not carry the fallback key, since consumers
// tell the two apart by presence alone.
func TestATDD_69_3_AC7_SuccessPathHasNoFallbackKey(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 12) // healthy mock LLM
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillContext(t, ctxMgr, cid, 11)
	raiseTokenWatermark(t, ctxMgr, cid, 90) // Story 71.1 AC3: token axis is the only trigger

	k.autoCompactIfNeeded(proc, 3)

	events := drainCompactEvents(t, proc)
	if len(events) == 0 {
		t.Fatal("no Compact event emitted")
	}
	for i, ev := range events {
		if ev.Args["phase"] == "started" {
			continue // Story 71.4 AC1: the started half of the pair carries no outcome fields
		}
		if _, present := ev.Args["fallback"]; present {
			t.Errorf("event[%d]: success path must not carry a fallback key, got %v", i, ev.Args["fallback"])
		}
		if ev.Args["restored_items"] == nil {
			t.Errorf("event[%d]: success path should still report restored_items", i)
		}
	}
}

// TestATDD_69_3_AC3_FallbackWithNothingToReclaimStillEmits: when the fallback
// itself frees nothing the event must still be emitted and say so, and the
// process must still survive.
func TestATDD_69_3_AC3_FallbackWithNothingToReclaimStillEmits(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 10)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// All-user context: nothing leaked to prune, and grouping degenerates to a
	// single group so no rounds are droppable either (F4).
	for range 9 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "post-compact restore entry"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90) // Story 71.1 AC3: token axis is the only trigger
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 4)

	if state := proc.GetState(); state != types.StateRunning {
		t.Errorf("process state = %s, want Running", state)
	}

	events := drainCompactEvents(t, proc)
	if len(events) == 0 {
		t.Fatal("event must still be emitted when the fallback reclaims nothing")
	}
	ev := events[len(events)-1]
	if ev.Args["fallback"] != mechanicalFallbackName {
		t.Errorf("fallback = %v, want %q", ev.Args["fallback"], mechanicalFallbackName)
	}
	if freed, ok := ev.Args["fallback_freed"].(int); !ok || freed != 0 {
		t.Errorf("fallback_freed = %v, want 0 (zero reclamation must be visible, not silent)", ev.Args["fallback_freed"])
	}
}

// --- AC4: precompact tops up slots mechanically ---

func TestATDD_69_3_AC4_PrecompactFallbackFreesSlots(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 13 rounds = 39 of 40 slots: an append needing 1+2 slots cannot fit.
	fillLeakyContext(t, ctxMgr, cid, 13)

	avail, _ := ctxMgr.AvailableSlots(cid)
	if avail >= 3 {
		t.Fatalf("fixture must start slot-starved, have %d available", avail)
	}

	err := k.preCompactForToolCalls(proc, 2, 5)
	if err != nil {
		t.Fatalf("preCompactForToolCalls returned error %v — mechanical fallback should have freed slots", err)
	}

	postAvail, _ := ctxMgr.AvailableSlots(cid)
	if postAvail < 3 {
		t.Errorf("available slots = %d, want >= 3", postAvail)
	}

	// The append that used to trip ErrContextFull must now succeed.
	if err := ctxMgr.AppendAssistantWithToolCalls(cid, "after fallback", "", nil,
		[]rnixctx.ToolCall{{ID: "post-1", Name: "Bash"}, {ID: "post-2", Name: "Bash"}}); err != nil {
		t.Fatalf("append after fallback failed: %v", err)
	}
	if err := ctxMgr.AppendToolResult(cid, "post-1", "ok"); err != nil {
		t.Fatalf("AppendToolResult post-1: %v", err)
	}
	if err := ctxMgr.AppendToolResult(cid, "post-2", "ok"); err != nil {
		t.Fatalf("AppendToolResult post-2: %v", err)
	}

	events := drainCompactEvents(t, proc)
	if len(events) == 0 {
		t.Fatal("no Compact event emitted from precompact")
	}
	ev := events[len(events)-1]
	if ev.Args["trigger"] != "precompact" {
		t.Errorf("trigger = %v, want precompact", ev.Args["trigger"])
	}
	if ev.Args["fallback"] != mechanicalFallbackName {
		t.Errorf("fallback = %v, want %q", ev.Args["fallback"], mechanicalFallbackName)
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_3_AC4_ExecuteToolCallsSurvivesWithFallback drives the real
// tool-execution path: with a failing LLM and a slot-starved context the
// process used to self-suspend with context_full. It must now keep going.
func TestATDD_69_3_AC4_ExecuteToolCallsSurvivesWithFallback(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	k.AddProcess(proc)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 13)

	resp := llmResponse{
		Content:   "calling tools",
		ToolCalls: []llmToolCall{{ID: "a", Name: "noop"}, {ID: "b", Name: "noop"}},
	}
	var consec errFingerprintCounter
	prompt := &rnixctx.PromptResult{}
	_, cont := k.executeToolCalls(proc, resp, 6, time.Now(), &consec, prompt, "")

	proc.mu.Lock()
	exit := proc.Exit
	reason := proc.SuspendReason
	proc.mu.Unlock()

	if reason == "context_full" {
		t.Error("process suspended with context_full despite the mechanical fallback")
	}
	if exit != nil && exit.Code == ExitContextFull {
		t.Errorf("process exited ExitContextFull: %+v", exit)
	}
	if !cont {
		// Continuation may legitimately stop for unrelated reasons (unknown
		// tool), but never because the context was full.
		if state := proc.GetState(); state == types.StateSuspended && reason == "context_full" {
			t.Error("loop stopped on context_full")
		}
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// --- AC6 / F1: resume paths reclaim before killing ---

// TestATDD_69_3_AC6_F1_ReclaimForResumeGivesHeadroom covers the shared helper
// both resume kill points now call: a context restored at its ceiling must come
// back with room to run rather than being declared unrecoverable.
func TestATDD_69_3_AC6_F1_ReclaimForResumeGivesHeadroom(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	fillLeakyContext(t, ctxMgr, cid, 13) // 39/40 slots

	avail, _ := ctxMgr.AvailableSlots(cid)
	if avail >= resumeFallbackHeadroom {
		t.Fatalf("fixture must start without headroom, have %d", avail)
	}

	res, ok := k.reclaimForResume(proc)
	if !ok {
		t.Fatalf("reclaimForResume reported no headroom; result %+v", res)
	}
	postAvail, _ := ctxMgr.AvailableSlots(cid)
	if postAvail < resumeFallbackHeadroom {
		t.Errorf("available slots = %d, want >= %d", postAvail, resumeFallbackHeadroom)
	}
	if res.TokensFreed <= 0 {
		t.Errorf("TokensFreed = %d, want > 0", res.TokensFreed)
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_3_AC6_F1_UnrecoverableContextStillReportsFailure: the kill path
// must remain reachable — a context that genuinely cannot be reclaimed still
// reports "no headroom" so the caller can terminate.
func TestATDD_69_3_AC6_F1_UnrecoverableContextStillReportsFailure(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 4)
	// All-user, ceiling-full: nothing leaked, single group, no headroom to find.
	for range 4 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "summary line"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	if _, ok := k.reclaimForResume(proc); ok {
		t.Error("reclaimForResume claimed headroom on an unreclaimable context")
	}
}

// TestATDD_69_3_AC6_UnloadForResumeFreesRoom covers the preventive half: a
// snapshot restored above the slot threshold is unloaded before the first step.
func TestATDD_69_3_AC6_UnloadForResumeFreesRoom(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	fillLeakyContext(t, ctxMgr, cid, 13) // 39/40 = 97.5% > 80% threshold

	k.unloadForResume(proc, 40, "test")

	used, _, _ := ctxMgr.SlotUsage(cid)
	if used >= 39 {
		t.Errorf("slot usage still %d/40, want meaningful reclamation from 39", used)
	}
	avail, _ := ctxMgr.AvailableSlots(cid)
	if avail <= 0 {
		t.Errorf("available slots = %d, want > 0 after preventive unload", avail)
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_3_AC6_UnloadForResumeBelowThresholdIsNoop: a comfortably-sized
// snapshot must not be touched.
func TestATDD_69_3_AC6_UnloadForResumeBelowThresholdIsNoop(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 100)
	fillLeakyContext(t, ctxMgr, cid, 5) // 15/100 slots

	beforeUsed, _, _ := ctxMgr.SlotUsage(cid)
	beforeTokens, _ := ctxMgr.TokenUsage(cid)

	k.unloadForResume(proc, 100, "test")

	afterUsed, _, _ := ctxMgr.SlotUsage(cid)
	afterTokens, _ := ctxMgr.TokenUsage(cid)
	if afterUsed != beforeUsed {
		t.Errorf("slots changed %d → %d on a below-threshold context", beforeUsed, afterUsed)
	}
	if afterTokens.Used != beforeTokens.Used {
		t.Errorf("tokens changed %d → %d on a below-threshold context", beforeTokens.Used, afterTokens.Used)
	}
}

func TestATDD_69_3_AC6_UnloadForResumeZeroSizeIsNoop(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	fillLeakyContext(t, ctxMgr, cid, 13)
	before, _, _ := ctxMgr.SlotUsage(cid)

	k.unloadForResume(proc, 0, "test")

	after, _, _ := ctxMgr.SlotUsage(cid)
	if after != before {
		t.Errorf("slots changed %d → %d with ctxSize=0", before, after)
	}
}

// --- AC8 red line: CompactionDisabled interaction ---

// TestATDD_69_3_AC8_CompactionDisabledSkipsAutoCompact documents the deliberate
// split: autoCompactIfNeeded returns early when compaction is disabled (so the
// fallback is never reached there), while the precompact and resume paths run
// the fallback regardless — turning compaction off must not mean "prefer to
// hang".
func TestATDD_69_3_AC8_CompactionDisabledSkipsAutoCompact(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 40)
	proc.CompactionDisabled = true
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 12)

	beforeTokens, _ := ctxMgr.TokenUsage(cid)
	k.autoCompactIfNeeded(proc, 3)
	afterTokens, _ := ctxMgr.TokenUsage(cid)

	if afterTokens.Used != beforeTokens.Used {
		t.Errorf("CompactionDisabled must short-circuit before the fallback: tokens %d → %d", beforeTokens.Used, afterTokens.Used)
	}

	// Precompact deliberately does NOT honour the flag.
	if err := k.preCompactForToolCalls(proc, 2, 4); err != nil {
		t.Fatalf("precompact must still reclaim when compaction is disabled: %v", err)
	}
	postAvail, _ := ctxMgr.AvailableSlots(cid)
	if postAvail < 3 {
		t.Errorf("available slots = %d, want >= 3 (fault handling ignores CompactionDisabled)", postAvail)
	}
}

// TestATDD_69_3_AC1_PlaceholderIsDistinctFromDriverStub guards decision 2: the
// context-side placeholder must not be confused with the driver-side protocol
// stub, so an investigator can tell who cleared the payload.
func TestATDD_69_3_AC1_PlaceholderIsDistinctFromDriverStub(t *testing.T) {
	const driverStub = "[Tool result unavailable: dropped due to context buffer limit]"
	if rnixctx.DefaultPrunePlaceholder == driverStub {
		t.Error("context placeholder must differ from drivers/llm's toolResultUnavailableStub")
	}
	if !strings.Contains(rnixctx.DefaultPrunePlaceholder, "cleared") {
		t.Errorf("placeholder %q should say the content was cleared, matching the frc section's promise",
			rnixctx.DefaultPrunePlaceholder)
	}
}

// TestATDD_69_3_AC7_EventArgsSerializable: events go through NDJSON to
// events.jsonl, so every arg the fallback adds must marshal.
func TestATDD_69_3_AC7_EventArgsSerializable(t *testing.T) {
	args := map[string]any{"step": 1, "trigger": "precompact"}
	addFallbackArgs(args, mechanicalFallbackResult{Pruned: 2, TokensFreed: 300, DroppedRounds: 1, SlotsFreed: 3})
	blob, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("event args must marshal: %v", err)
	}
	for _, key := range []string{"fallback", "pruned", "tokens_freed", "dropped_rounds", "slots_freed"} {
		if !strings.Contains(string(blob), `"`+key+`"`) {
			t.Errorf("marshalled args missing %q: %s", key, blob)
		}
	}
	if !strings.Contains(string(blob), "fallback_freed") {
		t.Error("fallback_freed must always be present on fallback events")
	}
	if freed, ok := args["fallback_freed"].(int); !ok || freed != 303 {
		t.Errorf("fallback_freed = %v, want 303 (TokensFreed+SlotsFreed)", args["fallback_freed"])
	}
}
