package kernel

import (
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
)

// Story 71.2 — the kernel-side half: the landing point (AC2), the pre-flight
// gate (AC3) and the cache-protection invariant (AC5).
//
// Helpers are reused rather than re-invented: setupFallbackKernel /
// fillLeakyContext / assertKernelPairing (atdd_69_3), buildIncidentFixture /
// raiseTokenWatermark / snapshotKernelMessages / setupReclaimKernel (atdd_69_4),
// readCompactTrigger (atdd_69_2).
//
// ⚠️ fillTo / fillContext are NOT used for reclamation outcomes here: they build
// contexts of one-byte "x" messages with zero reclaimable tool payload, so any
// assertion about how much was freed would be vacuous against them.

// lastCompactEvent returns the most recent Compact event, or nil.
func lastCompactEvent(t *testing.T, proc *Process) *types.SyscallEvent {
	t.Helper()
	events := drainCompactEvents(t, proc)
	if len(events) == 0 {
		return nil
	}
	return &events[len(events)-1]
}

// =============================================================================
// AC2 — the landing point sits well below the trigger
// =============================================================================

// TestATDD_71_2_AC2_HysteresisRatioPinned guards the constant and, more
// importantly, its SHAPE. A subtraction would look equivalent at the default
// threshold and invert for any threshold at or below the subtrahend — the倒挂
// trap proactiveReclaimWatermarkRatio already documents.
func TestATDD_71_2_AC2_HysteresisRatioPinned(t *testing.T) {
	if compactHysteresisRatio != 1.5 {
		t.Errorf("compactHysteresisRatio = %v, want 1.5", compactHysteresisRatio)
	}

	proc := NewProcess(0, "landing", nil)
	if got, want := proc.effectiveCompactLandingPct(), DefaultCompactThreshold/1.5; got != want {
		t.Errorf("landing pct = %v, want %v", got, want)
	}

	// The shape check: at a low configured threshold the landing point must stay
	// positive and below the trigger. `threshold - 26.7` would be negative here.
	for _, threshold := range []float64{80, 50, 26, 10, 1} {
		p := NewProcess(0, "landing", nil)
		p.CompactThreshold = threshold
		landing := p.effectiveCompactLandingPct()
		if landing <= 0 {
			t.Errorf("threshold %.0f%%: landing pct %.2f%% must stay positive — a non-positive target "+
				"is unreachable and would make every step demand an impossible reclamation",
				threshold, landing)
		}
		if landing >= threshold {
			t.Errorf("threshold %.0f%%: landing pct %.2f%% must sit BELOW the trigger, else the next "+
				"append immediately re-crosses it (the measured 1:1 oscillation)", threshold, landing)
		}
	}
}

// TestATDD_71_2_AC2_ClearAtLeastDerivesFromLanding checks the arithmetic that
// unifies AC1 and AC2: the floor is exactly the release needed to reach the
// landing point.
func TestATDD_71_2_AC2_ClearAtLeastDerivesFromLanding(t *testing.T) {
	proc := NewProcess(0, "landing", nil)

	// 90% of a 1000-token limit, landing at 53.33% → target 533, floor 900-533.
	usage := rnixctx.TokenStats{Used: 900, Limit: 1000, Percentage: 90}
	if got, want := proc.clearAtLeastForLanding(usage), 900-533; got != want {
		t.Errorf("clearAtLeastForLanding = %d, want %d", got, want)
	}

	// Already at or below the landing point: nothing to demand. A floor here
	// would block a prune the caller may legitimately want anyway.
	below := rnixctx.TokenStats{Used: 400, Limit: 1000, Percentage: 40}
	if got := proc.clearAtLeastForLanding(below); got != 0 {
		t.Errorf("below the landing point: floor = %d, want 0", got)
	}

	// No capacity scale → no fabricated denominator, hence no floor.
	noLimit := rnixctx.TokenStats{Used: 900, Limit: 0}
	if got := proc.clearAtLeastForLanding(noLimit); got != 0 {
		t.Errorf("with no token limit: floor = %d, want 0 (a floor off a fabricated denominator "+
			"would be the third un-validated threshold in this subsystem)", got)
	}
}

// TestATDD_71_2_AC2_FallbackDemandsTheLandingFloor is the behavioural half: when
// a compaction fails, the mechanical fallback must be asked for the landing
// release rather than for "whatever you can find".
func TestATDD_71_2_AC2_FallbackDemandsTheLandingFloor(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14) // real, reclaimable tool payload
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	beforeUsage, _ := ctxMgr.TokenUsage(cid)
	_ = drainCompactEvents(t, proc)

	k.autoCompactIfNeeded(proc, 7)

	ev := lastCompactEvent(t, proc)
	if ev == nil {
		t.Fatal("no Compact event on the failure path")
	}

	floor, ok := ev.Args["clear_at_least"].(int)
	if !ok {
		t.Fatalf("clear_at_least missing from the event: %v", ev.Args)
	}
	want := proc.clearAtLeastForLanding(beforeUsage)
	if floor != want {
		t.Errorf("clear_at_least = %d, want %d (the release that reaches the landing point)", floor, want)
	}
	if floor <= 0 {
		t.Fatal("floor = 0: at 90% usage the fallback must carry a real demand, or this proves nothing")
	}

	// The report must state the landing target and whether it was reached.
	if _, ok := ev.Args["landing_target_pct"]; !ok {
		t.Error("event missing landing_target_pct — the target must be visible to an operator")
	}
	if _, ok := ev.Args["landing_reached"]; !ok {
		t.Error("event missing landing_reached — AC2 requires an unreached target be reported, not hidden")
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_71_2_AC2_UnreachableLandingIsReportedNotFaked pins the honesty
// requirement. The post-compact restore budget (files 50k + skills 25k + an
// UNTRUNCATED plan) puts a physical floor under how small a context can get, so
// the landing point is sometimes unreachable. When it is, the event must say so
// — and the reclamation must not break round boundaries to manufacture a number.
func TestATDD_71_2_AC2_UnreachableLandingIsReportedNotFaked(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Mostly non-tool bulk: the token prune cannot touch it, so the landing point
	// is out of reach no matter how willing the reclamation is.
	for range 6 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("irreducible narrative ", 400)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	fillLeakyContext(t, ctxMgr, cid, 2)
	raiseTokenWatermark(t, ctxMgr, cid, 95)
	_ = drainCompactEvents(t, proc)

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.autoCompactIfNeeded(proc, 3)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	ev := lastCompactEvent(t, proc)
	if ev == nil {
		t.Fatal("no Compact event emitted")
	}
	reached, ok := ev.Args["landing_reached"].(bool)
	if !ok {
		t.Fatalf("landing_reached missing or not a bool: %v", ev.Args["landing_reached"])
	}
	if reached {
		t.Skip("fixture reached its landing point; the honesty path needs an unreachable one")
	}

	// Reported as unreached — and nothing was destroyed trying to force it.
	if len(after) != len(before) {
		t.Errorf("message count changed %d → %d: an unreachable target must be REPORTED, never chased "+
			"by dropping history the caller did not ask to lose", len(before), len(after))
	}
	assertKernelPairing(t, ctxMgr, cid)

	// The process survives regardless: best-effort by contract.
	if state := proc.GetState(); state != types.StateRunning {
		t.Errorf("process state = %s, want Running", state)
	}
}

// TestATDD_71_2_AC1_ReclaimForResumeIsExemptFromTheFloor is the story's F5 red
// line, and the ONLY exemption in the tree.
//
// Everywhere else "reclaim in bulk or not at all" is right: the alternative is
// another cache-prefix invalidation bought for a handful of tokens. Here the
// alternative is death — both callers run finishProcess(ExitContextFull) when
// this reports no headroom (resume.go:774/1148). A floor is a refusal to act,
// and the last line of defence does not get to refuse.
//
// The fixture supplies far less than the landing point would demand, so a floor
// applied here would reclaim exactly nothing.
func TestATDD_71_2_AC1_ReclaimForResumeIsExemptFromTheFloor(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)

	// ⚠️ Ordering is load-bearing: the prune only touches the COLD zone, i.e.
	// indices [0, ColdZoneEnd(n)), so the leaked rounds must be the OLDEST
	// messages. Appending them after the bulk puts them in the warm/active zone,
	// where the primitive correctly ignores them — and the test would then be
	// asserting the exemption against a context that has nothing reclaimable,
	// proving nothing about the floor.
	fillLeakyContext(t, ctxMgr, cid, 3)
	// Bulk the prune cannot touch keeps usage high, so a landing-point floor
	// would demand far more than those few leaked entries can release.
	for range 12 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("irreducible narrative ", 400)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	raiseTokenWatermark(t, ctxMgr, cid, 95)

	if countLeakedInColdZone(snapshotKernelMessages(t, ctxMgr, cid)) == 0 {
		t.Fatal("precondition: the cold zone must hold reclaimable payload, or 'reclaimed nothing' " +
			"would be correct behaviour rather than the floor refusing")
	}

	usage, _ := ctxMgr.TokenUsage(cid)
	floor := proc.clearAtLeastForLanding(usage)
	if floor <= 0 {
		t.Fatal("precondition: the fixture must be over its landing point, else there is no floor to be exempt from")
	}

	before, _ := ctxMgr.TokenUsage(cid)
	res, _ := k.reclaimForResume(proc)
	after, _ := ctxMgr.TokenUsage(cid)

	if res.Pruned == 0 {
		t.Errorf("reclaimForResume reclaimed nothing: it must be EXEMPT from the %d-token landing "+
			"floor. Its callers kill the process when it comes back empty, so applying a "+
			"bulk-or-nothing gate here converts a survivable resume into a dead process", floor)
	}
	if after.Used >= before.Used {
		t.Errorf("token usage did not drop: %d → %d", before.Used, after.Used)
	}
	if res.ClearAtLeast != 0 {
		t.Errorf("ClearAtLeast = %d, want 0 on the resume path", res.ClearAtLeast)
	}
	assertKernelPairing(t, ctxMgr, cid)
}

// =============================================================================
// AC3 — the pre-flight gate: no request when there is nothing to win
// =============================================================================

// TestATDD_71_2_AC3_GateDeclinesWhenRestoreDominates is the gate's positive
// case. A context whose bulk is an untruncated plan cannot shrink: the
// compaction would summarise everything and then restore the plan verbatim.
// Sending that request risks a 30s timeout to achieve nothing.
func TestATDD_71_2_AC3_GateDeclinesWhenRestoreDominates(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0) // healthy LLM: only the gate can stop it
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	for range 6 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "short"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	// extractActivePlan picks this up, and Compact restores plans untruncated.
	plan := "[Plan]\n" + strings.Repeat("plan step detail ", 2000)
	if err := ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, plan); err != nil {
		t.Fatalf("AppendMessage plan: %v", err)
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	usage, _ := ctxMgr.TokenUsage(cid)
	if usage.Percentage <= proc.effectiveCompactThreshold() {
		t.Fatalf("precondition: usage %.1f%% must exceed the threshold, or the gate is not what "+
			"stopped the compaction", usage.Percentage)
	}

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.autoCompactIfNeeded(proc, 1)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	if len(after) != len(before) {
		t.Errorf("message count changed %d → %d: the gate must not send the request", len(before), len(after))
	}
	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] rewritten: a declined compaction must leave the context byte-identical", i)
		}
	}
	if trigger := readCompactTrigger(t, proc); trigger != "" {
		t.Errorf("Compact event emitted with trigger=%q, want none — a NOOP is an early exit, "+
			"not an attempt, and per-step events above the threshold would flood the stream", trigger)
	}
}

// TestATDD_71_2_AC3_GateAdmitsWhenThereIsRealPayload is the negative case, and
// the reason the test above is not vacuous: with genuinely compressible history
// the request must go out.
func TestATDD_71_2_AC3_GateAdmitsWhenThereIsRealPayload(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0)
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	before, _, _ := ctxMgr.SlotUsage(cid)
	k.autoCompactIfNeeded(proc, 1)

	if trigger := readCompactTrigger(t, proc); trigger != "token_threshold" {
		t.Errorf("trigger = %q, want token_threshold — the gate must not block a compaction that "+
			"has real work to do", trigger)
	}
	after, _, _ := ctxMgr.SlotUsage(cid)
	if after >= before {
		t.Errorf("messages %d → %d: the compaction did not run", before, after)
	}
}

// TestATDD_71_2_AC3_GateAdmitsPlainTextContext pins the gate's NUMERATOR, which
// is the one question this story had to settle by owner decision.
//
// The gate measures what a compaction would REMOVE: `reclaimable − restore
// budget`. The tempting alternative is "how much prunable tool payload is
// there", because the mechanical fallback works on exactly that set — but the two
// are different operations. PruneToolResults clears tool-result bodies; an LLM
// compaction summarises EVERY message and restores files/skills/plan. A hundred
// rounds of plain prose hold no prunable payload whatsoever and still compact
// down to one summary.
//
// Under the tool-payload reading this fixture would be refused forever: usage is
// past the threshold, no compaction is ever attempted, the history keeps growing,
// and the process dies on a provider prompt-too-long that nothing in rnix can
// recover from. That failure is strictly worse than the oscillation this story
// exists to fix, and — the reason this test is written explicitly rather than
// left implicit — it is INVISIBLE to any fixture that carries tool payload.
func TestATDD_71_2_AC3_GateAdmitsPlainTextContext(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0)
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	// Prose only: zero tool results, hence zero prunable payload.
	for range 12 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("plain narrative prose ", 200)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	msgs := snapshotKernelMessages(t, ctxMgr, cid)
	if countLeakedInColdZone(msgs) != 0 {
		t.Fatalf("fixture precondition: must hold NO prunable tool payload, found %d entries",
			countLeakedInColdZone(msgs))
	}

	before, _, _ := ctxMgr.SlotUsage(cid)
	k.autoCompactIfNeeded(proc, 1)
	after, _, _ := ctxMgr.SlotUsage(cid)

	if trigger := readCompactTrigger(t, proc); trigger != "token_threshold" {
		t.Errorf("trigger = %q, want token_threshold. A context of pure prose has no prunable tool "+
			"payload, but it is entirely compressible — the gate must measure what the COMPACTION "+
			"removes, not what the PRUNE could reclaim. Refusing here means the context can never "+
			"shrink again and the process dies on a provider prompt-too-long", trigger)
	}
	if after >= before {
		t.Errorf("messages %d → %d: the compaction did not run on a fully compressible context",
			before, after)
	}
}

// TestATDD_71_2_AC3_GateDenominatorExcludesSystemPrompt pins the易错点. Copying
// qwen's "share of the whole history" verbatim would count the system prompt in
// the denominator. The prompt can never be compacted away, so counting it
// inflates `reclaimable` and MASKS the restore dominance this gate exists to
// catch: a context whose message pool is mostly an untruncated plan would then
// look "mostly compressible" (the huge prompt dwarfs the restore), the gate
// would admit, and the process would oscillate — compact, land back on the same
// plan, compact again — exactly the failure the gate prevents.
//
// The fixture is GateDeclinesWhenRestoreDominates plus a system prompt that
// dwarfs the history, injected into BOTH the context (so usage.Used carries it)
// and the proc field (so the gate's sysPromptTokens subtracts the same amount).
// The denominator must subtract it, leaving reclaimable = the message pool, so
// the restore dominance stays visible and the gate still declines.
//
// Mutation teeth: if the denominator counted total usage, reclaimable would
// balloon to sysPrompt+pool, compressible/reclaimable would climb past 5%, and
// the gate would ADMIT. The precondition below asserts the prompt is large
// enough for exactly that, so the decline assertion goes red under the mutation.
// (The earlier version of this test set the prompt only on the proc field, never
// in the context, so usage.Used stayed small, reclaimable hit 0, and the gate
// failed OPEN to an admit — the denominator branch never ran under reclaimable>0
// and the test passed even with the denominator mutated to total usage.)
func TestATDD_71_2_AC3_GateDenominatorExcludesSystemPrompt(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0) // healthy LLM: only the gate can stop it
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	// A system prompt dwarfing the history — the shape that breaks a total-usage
	// denominator. Injected into the context so usage.Used carries it, and into
	// the proc field so the gate's sysPromptTokens subtracts the same amount.
	bigPrompt := strings.Repeat("system prompt bulk ", 4000)
	if err := ctxMgr.SetSystemPrompt(cid, bigPrompt); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	proc.FinalSystemPrompt = bigPrompt

	// Restore-dominated message pool: an untruncated plan is the bulk.
	for range 6 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "short"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	// extractActivePlan picks this up, and Compact restores plans untruncated.
	plan := "[Plan]\n" + strings.Repeat("plan step detail ", 2000)
	if err := ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, plan); err != nil {
		t.Fatalf("AppendMessage plan: %v", err)
	}
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	usage, _ := ctxMgr.TokenUsage(cid)
	if usage.Percentage <= proc.effectiveCompactThreshold() {
		t.Fatalf("precondition: usage %.1f%% must exceed the threshold, or the gate is not what "+
			"stopped the compaction", usage.Percentage)
	}

	// Discriminative precondition: the prompt must be large enough that, were it
	// counted in the denominator, compressible/reclaimable would clear the 5% bar
	// and the gate would ADMIT. sysPrompt >= pool/19 puts the total-usage fraction
	// above 5% (admit) while the reclaimable-pool fraction stays ~0 (decline).
	// Without this margin the test cannot tell the two denominators apart.
	sysTokens := rnixctx.EstimateTokens(bigPrompt)
	pool := max(usage.Used-sysTokens, 0)
	if pool <= 0 {
		t.Fatalf("precondition: reclaimable pool must be > 0 so the gate's fraction branch actually "+
			"runs (usage.Used=%d, sysTokens=%d)", usage.Used, sysTokens)
	}
	if sysTokens < pool/19 {
		t.Fatalf("precondition: system prompt (%d tokens) must be >= pool/19 (%d) so a total-usage "+
			"denominator would admit — otherwise this test cannot discriminate the denominator",
			sysTokens, pool/19)
	}

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.autoCompactIfNeeded(proc, 1)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	if len(after) != len(before) {
		t.Errorf("message count changed %d → %d: counting the system prompt in the denominator would "+
			"inflate reclaimable, mask the restore dominance, and admit this useless compaction. The "+
			"denominator must be the RECLAIMABLE pool (usage minus the system prompt)", len(before), len(after))
	}
	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] rewritten: the gate must decline a restore-dominated context even under a "+
				"large system prompt", i)
		}
	}
	if trigger := readCompactTrigger(t, proc); trigger != "" {
		t.Errorf("Compact event emitted with trigger=%q, want none — the gate must decline", trigger)
	}
}

// TestATDD_71_2_AC3_GateConstantPinned guards the threshold, per the Story 69.4
// precedent that behavioural constants get pinned.
func TestATDD_71_2_AC3_GateConstantPinned(t *testing.T) {
	if minCompactionFractionPct != 5 {
		t.Errorf("minCompactionFractionPct = %d, want 5 (qwen's MIN_COMPRESSION_FRACTION = 0.05)",
			minCompactionFractionPct)
	}
}

// TestATDD_71_2_AC3_CompactionDisabledStaysFirst is the panic guard 🔴. The flag
// check must remain autoCompactIfNeeded's first statement:
// atdd_52_2_kernel_conditional_injection_test.go calls it on a bare
// &KernelImpl{} whose ctxMgr is nil, so any gate placed ahead of the flag that
// touches ctxMgr panics.
func TestATDD_71_2_AC3_CompactionDisabledStaysFirst(t *testing.T) {
	k := &KernelImpl{} // no ctxMgr, exactly like the Story 52.2 fixture
	proc := NewProcess(0, "disabled", nil)
	proc.CompactionDisabled = true

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked with CompactionDisabled=true: %v — the flag check must stay the FIRST "+
				"statement, ahead of anything that reads ctxMgr", r)
		}
	}()
	k.autoCompactIfNeeded(proc, 1)
}

// TestATDD_71_2_AC3_ManualAndResumePathsBypassTheGate: the gate lives in
// autoCompactIfNeeded, not in Compact(), so the paths that must always run —
// manual IPC and the two resume paths — reach the LLM regardless. This is qwen's
// `force` semantics, obtained structurally rather than through a flag.
func TestATDD_71_2_AC3_ManualAndResumePathsBypassTheGate(t *testing.T) {
	m := rnixctx.NewManager()
	cid, err := m.CtxAlloc(0)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	// The very shape autoCompactIfNeeded would decline: tiny, nothing to win.
	for range 4 {
		if err := m.AppendMessage(cid, rnixctx.RoleUser, "short"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	called := 0
	llm := func(string, []rnixctx.Message) (string, error) {
		called++
		return "<summary>ok</summary>", nil
	}
	if _, err := m.Compact(cid, rnixctx.CompactOpts{LLMCall: llm, Trigger: "manual"}); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if called != 1 {
		t.Errorf("LLM called %d times, want 1 — putting the gate inside Compact() would break manual "+
			"IPC and both resume paths, and would also reject context/compact_test.go's 18 small fixtures",
			called)
	}
}

// =============================================================================
// AC5 — reclaim rarely and in bulk (the cache-protection invariant)
// =============================================================================

// TestATDD_71_2_AC5_ColdZoneRewritesAreSparse turns context/prune.go's comment
// promise — "a proactive caller must reclaim rarely and in bulk" — into a
// measured invariant.
//
// Every cold-zone rewrite invalidates the provider's cached prompt prefix
// (anthropic's cache breakpoint sits after the message history). The incident
// measured the cost: among steps with <70% cache hit rate, 88% immediately
// followed a fallback, against 3% in the control group; one process's hit rate
// stuck at 34.5% for 37 consecutive steps after its first fallback.
//
// 🔴 Fixture design, both halves load-bearing — an easier one passes vacuously:
//
//  1. Usage must stay ABOVE the threshold for the whole run, so the fallback is
//     reached on every step. A context that lands below the trigger after one
//     reclamation is trivially sparse: nothing fires again, and the test reports
//     "sparse" against an implementation with no floor at all. Irreducible
//     non-tool bulk (the prune cannot touch RoleUser prose) holds usage up, which
//     is precisely the incident's shape — 93.6% of its compactions hit the
//     mechanical fallback.
//  2. Fresh tool payload must arrive each step. On a static context the
//     placeholder (37 bytes, far under LeakedThreshold) means a cleared entry can
//     never match the predicate again, so step 2 onwards find nothing no matter
//     what any gate does. A running process appends a round per step and the cold
//     zone boundary marches right, so new leaked entries keep dropping into it.
//     That treadmill is the thing the floor has to stop.
//
// Measured against this fixture: 0 rewrites with the floor, 10 of 20 without it.
func TestATDD_71_2_AC5_ColdZoneRewritesAreSparse(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Irreducible bulk: RoleUser prose is invisible to the token prune, so usage
	// stays over the threshold and every step reaches the fallback.
	for range 10 {
		if err := ctxMgr.AppendMessage(cid, rnixctx.RoleUser, strings.Repeat("irreducible narrative ", 300)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	fillLeakyContext(t, ctxMgr, cid, 4)
	raiseTokenWatermark(t, ctxMgr, cid, 95)

	const steps = 20
	rewriteSteps := 0
	previous := snapshotKernelMessages(t, ctxMgr, cid)

	for step := range steps {
		// One more round of real work, exactly as a running process would do.
		fillLeakyContext(t, ctxMgr, cid, 1)

		k.autoCompactIfNeeded(proc, step)
		current := snapshotKernelMessages(t, ctxMgr, cid)

		// Compare only the prefix both snapshots share: the appended round is
		// growth, not a rewrite, and counting it would make every step look like
		// a cache invalidation.
		rewritten := false
		for i := 0; i < len(previous) && i < len(current); i++ {
			if previous[i].Content != current[i].Content {
				rewritten = true
				break
			}
		}
		if len(current) < len(previous) {
			rewritten = true // history was dropped: also a prefix invalidation
		}
		if rewritten {
			rewriteSteps++
		}
		previous = current
	}

	// Every step sits above the threshold, so an unfloored reclamation rewrites
	// the cold zone as fast as payload arrives. ⌈N/4⌉ still allows genuine
	// bulk reclamations while rejecting the per-step treadmill.
	if limit := (steps + 3) / 4; rewriteSteps > limit {
		t.Errorf("cold zone rewritten on %d of %d steps (limit %d), with usage above the threshold "+
			"throughout. Reclaiming a little on every step is strictly worse than reclaiming nothing: "+
			"each rewrite invalidates the provider's cached prefix while leaving usage above the "+
			"trigger — the measured 88%%-vs-3%% cache collapse", rewriteSteps, steps, limit)
	}

	assertKernelPairing(t, ctxMgr, cid)
}

// TestATDD_71_2_AC5_RepeatedFallbacksConverge is the complementary shape: the
// second and later reclamations must not keep nibbling. Once the cold zone is
// cleared the placeholder (37 bytes) can never match the leaked predicate again,
// so the hysteresis is structural — no cooldown timer, no lastCompactStep field
// (Story 71.2 F1 forbids both).
func TestATDD_71_2_AC5_RepeatedFallbacksConverge(t *testing.T) {
	k, ctxMgr, proc, cid := setupFallbackKernel(t, 0)
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fillLeakyContext(t, ctxMgr, cid, 14)
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	k.autoCompactIfNeeded(proc, 1)
	afterFirst := snapshotKernelMessages(t, ctxMgr, cid)

	for step := 2; step <= 5; step++ {
		k.autoCompactIfNeeded(proc, step)
	}
	afterRest := snapshotKernelMessages(t, ctxMgr, cid)

	if len(afterFirst) != len(afterRest) {
		t.Fatalf("message count moved %d → %d across the follow-up steps", len(afterFirst), len(afterRest))
	}
	for i := range afterFirst {
		if afterFirst[i].Content != afterRest[i].Content {
			t.Errorf("msg[%d] rewritten again on a follow-up step: once the cold zone is cleared there "+
				"is nothing left worth a cache invalidation, and repeat rewrites are the oscillation "+
				"this story removes", i)
			break
		}
	}
}
