package kernel

import (
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 69.4 — proactive reclamation of leaked tool results.
//
// The consumer this story finally provides for debug/ctx_profile.go's
// long-standing "consider pruning unused tool outputs" suggestion: the analyzer
// has been reporting leaked payloads for a while with nothing acting on them.
//
// Honest scope (story F2): the incident that motivated Epic 69 was SLOT-axis
// driven (23 consecutive slot_threshold triggers, token usage stuck around 14%
// of a 200k limit), and PruneToolResults frees exactly 0 slots by design. So
// these tests assert what this story actually buys — a smaller request body and
// a deferred token threshold — and never that it prevents a hang.
//
// The single most important test here is AC7 (byte-identity below the
// watermark). Rewriting cold-zone content invalidates the provider's cached
// prompt prefix, so an ungated per-step reclamation would replay the very
// failure mode Story 69.1 fixed, just on the message axis instead of the system
// prompt axis.

// setupReclaimKernel builds a kernel with a buffered DebugChan for event
// inspection. The LLM device is never actually called by
// reclaimLeakedIfNeeded — reclamation is mechanical — but the process needs a
// device configured to look realistic.
func setupReclaimKernel(t *testing.T, maxSize int) (*KernelImpl, *rnixctx.Manager, *Process, types.CtxID) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(maxSize)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	proc := NewProcess(0, "reclaim test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{}
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	return k, ctxMgr, proc, cid
}

// drainReclaimEvents collects the CtxReclaim events emitted so far.
func drainReclaimEvents(t *testing.T, proc *Process) []types.SyscallEvent {
	t.Helper()
	var out []types.SyscallEvent
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "CtxReclaim" {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// snapshotKernelMessages copies the live context messages through BuildPrompt,
// which is the same view the driver would serialize.
func snapshotKernelMessages(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID) []rnixctx.Message {
	t.Helper()
	prompt, err := ctxMgr.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	out := make([]rnixctx.Message, len(prompt.Messages))
	copy(out, prompt.Messages)
	return out
}

// projectToDebug maps context.Message onto debug's narrow CtxMessage schema.
// The projection is lossless for the classification criteria: IsLeakedToolResult
// looks at Role and Content only.
func projectToDebug(msgs []rnixctx.Message) *debug.ContextData {
	data := &debug.ContextData{Messages: make([]debug.CtxMessage, 0, len(msgs))}
	for _, m := range msgs {
		data.Messages = append(data.Messages, debug.CtxMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		})
	}
	return data
}

func hasPruningSuggestion(suggestions []string) bool {
	for _, s := range suggestions {
		if strings.Contains(s, "consider pruning unused tool outputs") {
			return true
		}
	}
	return false
}

// --- AC3: reproduce the measured incident baseline ---

// buildIncidentFixture reconstructs the shape measured in the real incident's
// ctx-profile.json:
//
//	total_tokens 28316, leaked {tokens 8651, messages 7, pct 30.6},
//	active {10281, 17}, warm {6608, 24}, cold {2776, 35}
//
// active.messages counts the system prompt as one entry (classifyMessages does
// activeMsgs++ for a non-empty SystemPrompt), so the real message count is
// 16 + 24 + 35 + 7 = 82. That number matters arithmetically:
//
//	ColdZoneEnd(82) = 82 - max(82/5,4) - max(82*3/10,6) = 82 - 16 - 24 = 42
//
// and cold(35) + leaked(7) = 42 exactly. The 7 leaked entries must therefore sit
// inside [0,42) or the reclamation selects none of them and the test fails
// vacuously with "reclaimed 0".
//
// Layout: 27 rounds of user → assistant(tool_calls) → tool (81 messages) plus a
// trailing user = 82. The first 14 rounds fill the cold zone exactly (42
// messages); every second one of those carries a leaked-size payload, giving 7
// leaked entries at indices 2, 8, 14, 20, 26, 32, 38 and 35 cold non-leaked
// entries. The remaining 13 rounds hold leaked-SIZE payloads on purpose: they
// live in the warm/active zone and must survive untouched (AC4).
func buildIncidentFixture(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID) {
	t.Helper()

	const (
		totalMessages = 82
		coldEnd       = 42
		coldRounds    = coldEnd / 3 // 14
		leakedCount   = 7
	)
	if got := rnixctx.ColdZoneEnd(totalMessages); got != coldEnd {
		t.Fatalf("fixture arithmetic broke: ColdZoneEnd(%d) = %d, want %d", totalMessages, got, coldEnd)
	}

	// ~8651 tokens across 7 entries ≈ 1236 tokens each ≈ 4320 ASCII bytes.
	leakedPayload := strings.Repeat("tool output payload ", 216) // 4320 bytes > LeakedThreshold
	// Cold non-leaked filler: below LeakedThreshold so it stays classified cold.
	shortPayload := strings.Repeat("ok ", 30) // 90 bytes

	appendRound := func(round int, payload string) {
		id := "call-" + strings.Repeat("x", round+1)
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "step "+id); err != nil {
			t.Fatalf("AppendMessage user (round %d): %v", round, err)
		}
		if err := ctxMgr.AppendAssistantWithToolCalls(cid, "running", "", nil,
			[]rnixctx.ToolCall{{ID: id, Name: "Bash"}}); err != nil {
			t.Fatalf("AppendAssistantWithToolCalls (round %d): %v", round, err)
		}
		if err := ctxMgr.AppendToolResult(cid, id, payload); err != nil {
			t.Fatalf("AppendToolResult (round %d): %v", round, err)
		}
	}

	for round := range coldRounds {
		payload := shortPayload
		if round%2 == 0 {
			payload = leakedPayload
		}
		appendRound(round, payload)
	}
	for round := coldRounds; round < (totalMessages-1)/3; round++ {
		appendRound(round, leakedPayload)
	}
	if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "what next?"); err != nil {
		t.Fatalf("AppendMessage trailing user: %v", err)
	}

	msgs := snapshotKernelMessages(t, ctxMgr, cid)
	if len(msgs) != totalMessages {
		t.Fatalf("fixture built %d messages, want %d", len(msgs), totalMessages)
	}
	if got := countLeakedInColdZone(msgs); got != leakedCount {
		t.Fatalf("fixture holds %d leaked entries in the cold zone, want %d", got, leakedCount)
	}
}

// countLeakedInColdZone counts leaked tool results inside the cold zone, i.e.
// exactly the set the reclamation is allowed to touch.
func countLeakedInColdZone(msgs []rnixctx.Message) int {
	coldEnd := rnixctx.ColdZoneEnd(len(msgs))
	n := 0
	for i := range coldEnd {
		if rnixctx.IsLeakedToolResult(msgs[i]) {
			n++
		}
	}
	return n
}

// raiseTokenWatermark lowers the context's token limit so the fixture's usage
// lands above the reclamation watermark (0.75 × 80% = 60%) without needing a
// synthetic 100k-token fixture. This is the same knob Story 69.2 wired up:
// ctx.TokenLimit ← proc.ContextBudget ← context_window × 9/10.
func raiseTokenWatermark(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID, pctWanted float64) {
	t.Helper()
	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	limit := int(float64(usage.Used) / pctWanted * 100)
	if limit <= 0 {
		t.Fatalf("cannot derive a token limit from used=%d", usage.Used)
	}
	if err := ctxMgr.SetTokenLimit(cid, limit); err != nil {
		t.Fatalf("SetTokenLimit: %v", err)
	}
}

// TestATDD_69_4_AC3_IncidentBaselineReclaimed replays the measured incident
// context and asserts the analyzer's own verdict improves: the leaked share
// drops and the "consider pruning unused tool outputs" suggestion — generated
// since Story 34 with nothing consuming it — disappears.
func TestATDD_69_4_AC3_IncidentBaselineReclaimed(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	buildIncidentFixture(t, ctxMgr, cid)
	raiseTokenWatermark(t, ctxMgr, cid, 70) // above the 60% watermark

	before := snapshotKernelMessages(t, ctxMgr, cid)
	beforeProfile := debug.AnalyzeContext(projectToDebug(before), proc.PID, cid, 0, 0)
	if beforeProfile.Classification.Leaked.Messages != 7 {
		t.Fatalf("baseline leaked messages = %d, want 7 (fixture must match the measured incident)",
			beforeProfile.Classification.Leaked.Messages)
	}
	if !hasPruningSuggestion(beforeProfile.Suggestions) {
		t.Fatal("baseline must carry the pruning suggestion — otherwise this test cannot show it being consumed")
	}
	beforeUsage, _ := ctxMgr.TokenUsage(cid)

	k.reclaimLeakedIfNeeded(proc, 12)

	after := snapshotKernelMessages(t, ctxMgr, cid)
	afterProfile := debug.AnalyzeContext(projectToDebug(after), proc.PID, cid, 0, 0)

	if afterProfile.Classification.Leaked.Pct >= beforeProfile.Classification.Leaked.Pct {
		t.Errorf("leaked pct did not drop: %.1f%% → %.1f%%",
			beforeProfile.Classification.Leaked.Pct, afterProfile.Classification.Leaked.Pct)
	}
	if hasPruningSuggestion(afterProfile.Suggestions) {
		t.Errorf("pruning suggestion still present after reclamation: %v", afterProfile.Suggestions)
	}

	afterUsage, _ := ctxMgr.TokenUsage(cid)
	if afterUsage.Used >= beforeUsage.Used {
		t.Errorf("token usage did not drop: %d → %d", beforeUsage.Used, afterUsage.Used)
	}

	// Entry count must be untouched: this story reclaims payload, never slots.
	if len(after) != len(before) {
		t.Errorf("message count changed %d → %d (prune must preserve entries)", len(before), len(after))
	}
	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_4_AC4_ReclaimedSetEqualsAnalyzerLeakedSet is the green-guard
// against a dev quietly introducing a second threshold or window formula: the
// messages actually rewritten must be exactly the ones debug.AnalyzeContext
// reports as leaked on the same message slice, entry for entry.
func TestATDD_69_4_AC4_ReclaimedSetEqualsAnalyzerLeakedSet(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	buildIncidentFixture(t, ctxMgr, cid)
	raiseTokenWatermark(t, ctxMgr, cid, 70)

	before := snapshotKernelMessages(t, ctxMgr, cid)

	// The analyzer's leaked set, by index.
	wantLeaked := make(map[int]bool)
	coldEnd := rnixctx.ColdZoneEnd(len(before))
	for i := range coldEnd {
		if rnixctx.IsLeakedToolResult(before[i]) {
			wantLeaked[i] = true
		}
	}
	profile := debug.AnalyzeContext(projectToDebug(before), proc.PID, cid, 0, 0)
	if len(wantLeaked) != profile.Classification.Leaked.Messages {
		t.Fatalf("oracle disagreement: index scan found %d leaked, AnalyzeContext reports %d",
			len(wantLeaked), profile.Classification.Leaked.Messages)
	}

	k.reclaimLeakedIfNeeded(proc, 5)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	gotRewritten := make(map[int]bool)
	for i := range after {
		if after[i].Content != before[i].Content {
			gotRewritten[i] = true
		}
	}

	if len(gotRewritten) == 0 {
		t.Fatal("nothing was rewritten — the set-equality assertion would pass vacuously")
	}
	for i := range wantLeaked {
		if !gotRewritten[i] {
			t.Errorf("msg[%d] is analyzer-leaked but was NOT reclaimed", i)
		}
	}
	for i := range gotRewritten {
		if !wantLeaked[i] {
			t.Errorf("msg[%d] was reclaimed but the analyzer does not classify it as leaked (a second criterion crept in?)", i)
		}
		if after[i].Content != rnixctx.DefaultPrunePlaceholder {
			t.Errorf("msg[%d] rewritten to %q, want the shared placeholder", i, after[i].Content)
		}
		if after[i].Role != before[i].Role || after[i].ToolCallID != before[i].ToolCallID {
			t.Errorf("msg[%d]: Role/ToolCallID must be preserved verbatim", i)
		}
	}
	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_4_AC7_LowWatermarkLeavesBytesIdentical is this story's first red
// line, in executable form. Below the watermark the reclamation must not rewrite
// a single byte, because every cold-zone rewrite invalidates the provider's
// cached prompt prefix (drivers/llm/anthropic.go
// applyCacheControlToMessageHistory puts the cache breakpoint after the history,
// so the cold zone sits inside the cached prefix).
//
// N consecutive calls, because the failure mode being guarded against is
// per-step reclamation: the cold-zone boundary marches right as messages
// accumulate, so an ungated implementation invalidates the cache on every single
// step — exactly the cost profile Story 69.1 was written to remove.
func TestATDD_69_4_AC7_LowWatermarkLeavesBytesIdentical(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 4096)
	buildIncidentFixture(t, ctxMgr, cid)

	// Leave TokenLimit at its default (200k): the fixture's ~28k usage is ~14%,
	// far below the 60% watermark. (Story 71.1 retired the slot watermark, so the
	// token axis is the only gate left to stay below.)
	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	watermark := proc.effectiveCompactThreshold() * proactiveReclaimWatermarkRatio
	if usage.Percentage > watermark {
		t.Fatalf("precondition: token usage %.1f%% must sit below the %.1f%% watermark", usage.Percentage, watermark)
	}
	// And there IS something reclaimable — otherwise the assertion is vacuous.
	if countLeakedInColdZone(snapshotKernelMessages(t, ctxMgr, cid)) == 0 {
		t.Fatal("precondition: fixture must hold reclaimable payload, else byte-identity proves nothing")
	}

	before := snapshotKernelMessages(t, ctxMgr, cid)
	for step := 1; step <= 12; step++ {
		k.reclaimLeakedIfNeeded(proc, step)
	}
	after := snapshotKernelMessages(t, ctxMgr, cid)

	if len(after) != len(before) {
		t.Fatalf("message count changed %d → %d below the watermark", len(before), len(after))
	}
	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] content changed below the watermark (%d → %d bytes) — this breaks the provider prompt cache prefix on every step",
				i, len(before[i].Content), len(after[i].Content))
		}
	}
	if events := drainReclaimEvents(t, proc); len(events) != 0 {
		t.Errorf("emitted %d CtxReclaim events below the watermark, want 0 (Pruned==0 must stay silent)", len(events))
	}
}

// TestATDD_69_4_AC1_YieldGateDeclinesSmallBatches covers the second gate: even
// above the watermark, a batch too small to be worth a cache invalidation must
// leave the bytes alone.
func TestATDD_69_4_AC1_YieldGateDeclinesSmallBatches(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)

	// 20 rounds whose cold-zone tool results are just barely leaked-sized, then
	// one enormous non-tool message so that usage is high but the reclaimable
	// share is a rounding error against it.
	payload := strings.Repeat("x", rnixctx.LeakedThreshold+1)
	for round := range 20 {
		id := "small-" + strings.Repeat("s", round+1)
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "step"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		if err := ctxMgr.AppendAssistantWithToolCalls(cid, "running", "", nil,
			[]rnixctx.ToolCall{{ID: id, Name: "Bash"}}); err != nil {
			t.Fatalf("AppendAssistantWithToolCalls: %v", err)
		}
		if err := ctxMgr.AppendToolResult(cid, id, payload); err != nil {
			t.Fatalf("AppendToolResult: %v", err)
		}
	}
	// A single huge user message in the active zone dwarfs the cold-zone payload,
	// so candidates fall below used × minReclaimRatioPct / 100.
	if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("bulk ", 200_000)); err != nil {
		t.Fatalf("AppendMessage bulk: %v", err)
	}
	raiseTokenWatermark(t, ctxMgr, cid, 70)

	msgs := snapshotKernelMessages(t, ctxMgr, cid)
	if countLeakedInColdZone(msgs) == 0 {
		t.Fatal("precondition: need reclaimable cold-zone payload for the gate to decline")
	}
	usage, _ := ctxMgr.TokenUsage(cid)
	candidateTokens := 0
	coldEnd := rnixctx.ColdZoneEnd(len(msgs))
	for i := range coldEnd {
		if rnixctx.IsLeakedToolResult(msgs[i]) {
			candidateTokens += rnixctx.EstimateMessageTokens(msgs[i])
		}
	}
	gate := max(minReclaimTokens, usage.Used*minReclaimRatioPct/100)
	if candidateTokens >= gate {
		t.Fatalf("precondition: candidates (%d) must fall below the yield gate (%d) for this case to test anything",
			candidateTokens, gate)
	}

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.reclaimLeakedIfNeeded(proc, 7)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] rewritten despite the batch being below the yield gate", i)
		}
	}
	if events := drainReclaimEvents(t, proc); len(events) != 0 {
		t.Errorf("emitted %d CtxReclaim events for a declined batch, want 0", len(events))
	}
}

// Story 71.1 AC3 retired the slot watermark, so the former
// TestATDD_69_4_AC1_SlotWatermarkAloneTriggers ("token low, slots high → fires
// with trigger=slot_watermark") no longer describes intended behaviour. Its
// replacement asserts the OPPOSITE and lives in
// atdd_71_1_token_axis_slot_decoupling_test.go
// (TestATDD_71_1_AC7_HighMessageCountLowTokensDoesNotReclaim), so the scenario
// stays covered rather than silently disappearing.

// TestATDD_69_4_AC6_EventShape pins the observability contract. The event name
// is deliberately NOT "Compact": mixing proactive reclamation into the compact
// counter would corrupt exactly the kind of post-incident arithmetic that
// produced "27 compactions, 0 successes". Known cost, accepted: the dashboard's
// sysEvents pane hard-filters on Syscall == "Compact", so CtxReclaim is visible
// via `rnix strace` and events.jsonl but not there.
func TestATDD_69_4_AC6_EventShape(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	buildIncidentFixture(t, ctxMgr, cid)
	raiseTokenWatermark(t, ctxMgr, cid, 70)

	k.reclaimLeakedIfNeeded(proc, 42)

	events := drainReclaimEvents(t, proc)
	if len(events) != 1 {
		t.Fatalf("emitted %d CtxReclaim events, want exactly 1", len(events))
	}
	args := events[0].Args

	for _, key := range []string{
		"step", "trigger", "pruned", "tokens_freed", "candidate_tokens",
		"pre_tokens", "post_tokens", "pre_pct", "post_pct",
	} {
		if _, ok := args[key]; !ok {
			t.Errorf("CtxReclaim args missing key %q (have %v)", key, args)
		}
	}

	if args["step"] != 42 {
		t.Errorf("step = %v, want 42", args["step"])
	}
	if args["trigger"] != "token_watermark" {
		t.Errorf("trigger = %v, want token_watermark", args["trigger"])
	}
	if pruned, ok := args["pruned"].(int); !ok || pruned != 7 {
		t.Errorf("pruned = %v, want 7", args["pruned"])
	}
	if freed, ok := args["tokens_freed"].(int); !ok || freed <= 0 {
		t.Errorf("tokens_freed = %v, want > 0", args["tokens_freed"])
	}
	if cand, ok := args["candidate_tokens"].(int); !ok || cand <= 0 {
		t.Errorf("candidate_tokens = %v, want > 0", args["candidate_tokens"])
	}
	pre, preOK := args["pre_tokens"].(int)
	post, postOK := args["post_tokens"].(int)
	if !preOK || !postOK || post >= pre {
		t.Errorf("expected post_tokens < pre_tokens, got pre=%v post=%v", args["pre_tokens"], args["post_tokens"])
	}
	prePct, preP := args["pre_pct"].(float64)
	postPct, postP := args["post_pct"].(float64)
	if !preP || !postP || postPct >= prePct {
		t.Errorf("expected post_pct < pre_pct, got pre=%v post=%v", args["pre_pct"], args["post_pct"])
	}
	// The percentages must use TokenUsage's own denominator (Story 69.2:
	// ctx.TokenLimit ← ContextBudget ← context_window × 9/10), not a home-made one.
	usage, _ := ctxMgr.TokenUsage(cid)
	if diff := postPct - usage.Percentage; diff > 0.01 || diff < -0.01 {
		t.Errorf("post_pct = %.4f but TokenUsage reports %.4f — the denominator must be shared", postPct, usage.Percentage)
	}
}

// --- AC5: FRC semantics, both directions ---

// TestATDD_69_4_AC5_CompactionDisabledSkipsReclaim is the forward direction:
// proactive reclamation is routine reclamation, so it must honour the same flag
// that suppresses the `frc` system-prompt section (kernel/sections.go: "if
// proc.CompactionDisabled { return \"\" }"). Reclaiming payload the prompt never
// warned the model about would break the promise that section carries.
func TestATDD_69_4_AC5_CompactionDisabledSkipsReclaim(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	proc.CompactionDisabled = true
	buildIncidentFixture(t, ctxMgr, cid)
	raiseTokenWatermark(t, ctxMgr, cid, 95) // far above every gate

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.reclaimLeakedIfNeeded(proc, 3)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] rewritten with CompactionDisabled=true", i)
		}
	}
	if events := drainReclaimEvents(t, proc); len(events) != 0 {
		t.Errorf("emitted %d CtxReclaim events with CompactionDisabled=true, want 0", len(events))
	}
}

// TestATDD_69_4_AC5_CompactionDisabledStillAllowsFallback is the reverse
// direction, and the one a dev is most likely to break by "consistently" gating
// everything: Story 69.3's fault-handling paths deliberately ignore the flag
// (kernel/compact.go: "disabling routine compaction must not mean 'prefer to
// hang'. This is fault handling, not routine reclamation"). Turning routine
// reclamation off must not disarm the last line of defence.
func TestATDD_69_4_AC5_CompactionDisabledStillAllowsFallback(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	proc.CompactionDisabled = true
	buildIncidentFixture(t, ctxMgr, cid)

	beforeUsage, _ := ctxMgr.TokenUsage(cid)
	// Story 71.2: an empty target is "no slot need, no release floor", i.e. the
	// pre-71.2 call shape. The floor is a separate axis from the flag under test.
	res := k.runMechanicalFallback(proc, fallbackTarget{})
	afterUsage, _ := ctxMgr.TokenUsage(cid)

	if res.Pruned == 0 {
		t.Error("mechanical fallback reclaimed nothing with CompactionDisabled=true — fault handling must not be gated on the flag")
	}
	if afterUsage.Used >= beforeUsage.Used {
		t.Errorf("fallback did not reduce tokens: %d → %d", beforeUsage.Used, afterUsage.Used)
	}
	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_4_AC1_ReclaimIsIdempotentAcrossSteps proves the hysteresis is
// structural rather than timer-based (story F4): the placeholder is 37 bytes,
// far under LeakedThreshold (1000), so a rewritten entry can never match the
// predicate again. Combined with the yield gate needing a fresh 20% to
// accumulate, no "last reclaimed step" process field is needed.
func TestATDD_69_4_AC1_ReclaimIsIdempotentAcrossSteps(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 256)
	buildIncidentFixture(t, ctxMgr, cid)
	raiseTokenWatermark(t, ctxMgr, cid, 95)

	k.reclaimLeakedIfNeeded(proc, 1)
	first := drainReclaimEvents(t, proc)
	if len(first) != 1 {
		t.Fatalf("first call emitted %d events, want 1 — nothing to be idempotent about otherwise", len(first))
	}
	afterFirst := snapshotKernelMessages(t, ctxMgr, cid)

	for step := 2; step <= 6; step++ {
		k.reclaimLeakedIfNeeded(proc, step)
	}
	afterRest := snapshotKernelMessages(t, ctxMgr, cid)

	for i := range afterFirst {
		if afterFirst[i].Content != afterRest[i].Content {
			t.Fatalf("msg[%d] changed on a repeat sweep — reclamation is not idempotent", i)
		}
	}
	if events := drainReclaimEvents(t, proc); len(events) != 0 {
		t.Errorf("repeat sweeps emitted %d events, want 0 (Pruned==0 stays silent)", len(events))
	}
	if len(rnixctx.DefaultPrunePlaceholder) >= rnixctx.LeakedThreshold {
		t.Errorf("placeholder is %d bytes, which is not < LeakedThreshold (%d) — the hysteresis argument collapses",
			len(rnixctx.DefaultPrunePlaceholder), rnixctx.LeakedThreshold)
	}
}

// TestATDD_69_4_AC1_ReclaimDefersCompaction pins the reason the call site sits
// immediately BEFORE autoCompactIfNeeded rather than after it: the reclamation
// lowers TokenUsage, and the compaction check then re-reads that lowered figure
// and declines to fire. Deferring an LLM compaction (a full-history replacement
// plus a model call) is the larger half of this story's payoff.
func TestATDD_69_4_AC1_ReclaimDefersCompaction(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 4096)
	buildIncidentFixture(t, ctxMgr, cid)

	// Put usage just above the compact threshold, so without reclamation the
	// very next autoCompactIfNeeded would fire.
	raiseTokenWatermark(t, ctxMgr, cid, 85)
	before, _ := ctxMgr.TokenUsage(cid)
	if before.Percentage <= proc.effectiveCompactThreshold() {
		t.Fatalf("precondition: usage %.1f%% must exceed the compact threshold %.1f%%",
			before.Percentage, proc.effectiveCompactThreshold())
	}

	k.reclaimLeakedIfNeeded(proc, 11)

	after, _ := ctxMgr.TokenUsage(cid)
	if after.Percentage > proc.effectiveCompactThreshold() {
		t.Errorf("usage still above the compact threshold after reclamation: %.1f%% (was %.1f%%) — no compaction was deferred",
			after.Percentage, before.Percentage)
	}

	// And prove it really is deferred: autoCompactIfNeeded now finds nothing to
	// do, so it emits no Compact event.
	k.autoCompactIfNeeded(proc, 11)
	if events := drainCompactEvents(t, proc); len(events) != 0 {
		t.Errorf("autoCompactIfNeeded still fired after reclamation (%d Compact events)", len(events))
	}
	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_69_4_AC1_WatermarkRatioIsProportional guards the derivation shape.
// A subtractive watermark (threshold - 20) goes negative for any configured
// threshold ≤ 20 and then fires on every step, which is the cost profile this
// story exists to avoid.
func TestATDD_69_4_AC1_WatermarkRatioIsProportional(t *testing.T) {
	for _, threshold := range []float64{1, 5, 15, 20, 50, 80, 100} {
		proc := NewProcess(0, "watermark", nil)
		proc.CompactThreshold = threshold
		got := proc.effectiveCompactThreshold() * proactiveReclaimWatermarkRatio
		if got <= 0 {
			t.Errorf("threshold=%.0f yields watermark %.2f, must stay positive", threshold, got)
		}
		if got >= proc.effectiveCompactThreshold() {
			t.Errorf("threshold=%.0f yields watermark %.2f, must sit strictly below the compact threshold", threshold, got)
		}
	}
}

// TestATDD_69_4_AC1_YieldGateConstantsArePinned locks the two yield-gate
// constants as literals rather than via the constants themselves. Every gate
// test above computes its expected threshold FROM minReclaimTokens /
// minReclaimRatioPct, which is tautological: editing either constant would still
// pass. These literals pin the behavioral intent — a ~one-leaked-result floor
// and a 20%-of-the-reclaimable-pool ratio — so an accidental edit fails here.
func TestATDD_69_4_AC1_YieldGateConstantsArePinned(t *testing.T) {
	if minReclaimTokens != 1000 {
		t.Errorf("minReclaimTokens = %d, want 1000 (the ~one-leaked-result floor)", minReclaimTokens)
	}
	if minReclaimRatioPct != 20 {
		t.Errorf("minReclaimRatioPct = %d, want 20 (the defer-a-compaction-by-several-steps point)", minReclaimRatioPct)
	}
}

// The former TestATDD_69_4_AC6_BothAxesTrigger covered the "both" trigger
// classification. Story 71.1 AC3 removed the slot watermark, so "both" is no
// longer reachable in either classifier (kernel/compact.go) — a test asserting it
// would pin behaviour that cannot occur. The surviving single-axis label is
// covered by TestATDD_69_4_AC6_EventShape.

