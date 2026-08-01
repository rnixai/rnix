package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// applyCompactTimeout resolves the EXPLICIT compaction timeout for a freshly
// spawned process (Story 69.3 AC5): opts > agent manifest compact_timeout >
// leave the field zero so resolveCompactTimeout (kernel/driver_timeout.go)
// fills the derived value, or — if every lookup misses — effectiveCompactTimeout()
// falls back to DefaultCompactTimeout (the 30s floor). Shape deliberately
// mirrors the StepTimeout block in spawn.go, including "a ParseDuration
// failure is ignored rather than fatal".
//
// ⚠️ Unlike StepTimeout, a configured 0 does NOT disable anything:
// effectiveCompactTimeout() maps 0 → floor. A zero field is the only way to
// reach the default, so a manifest "0" is intentionally a no-op instead of an
// instant-expiry timeout (which would make compaction permanently unavailable).
//
// Semantic layering (Story 71.3 AC3): compactTimeout is the OUTER wall-clock
// budget (gocontext.WithTimeout around the whole compact call, compact.go:841).
// The driver's per-request timeout (timeout_sec / llm.DefaultTimeout) serves as
// the INNER idle timeout inside the driver (NewIdleTimer, 8 drivers share it).
// The two are nested, not competing: removing the outer bound would let a
// response that keeps emitting bytes but never ends run forever (IdleTimer
// resets on every event); removing the inner is a whole-driver-surface change
// far beyond this story. The ×compactTimeoutMultiplier (= 4) is the conversion
// between the two layers — codex's authoritative rationale
// (COMPACT_REQUEST_TIMEOUT_IDLE_MULTIPLIER, client.rs:156-157): "/responses/
// compact is unary, so the timeout covers the full response rather than one
// idle period between stream events."
//
// Story 71.3 AC4 — the 69.3 ruling "the 30s default is deliberately unchanged"
// is OVERRULED by post-69.1/69.4 data: with the cache prefix fixed (CtxReclaim
// events present, hit rate holding 99%+ in long stretches), compact STILL
// saturated the 30s ceiling — p90 = 120.1s, 308/342 = 90.1% of all compactions
// timed out. The payload-heaviest call class (pre_tokens p50 = 53,301, max =
// 160,202) was configured with the globally SHORTEST timeout, 1/10 of the
// driver family default. The default is now derived (driverTimeout × 4); this
// function handles only the explicit-config half.
func applyCompactTimeout(proc *Process, agent *agents.AgentInfo, opts SpawnOpts) {
	if opts.CompactTimeout > 0 {
		proc.CompactTimeout = opts.CompactTimeout
		proc.compactTimeoutExplicit = true
		return
	}
	if agent == nil || agent.Manifest.CompactTimeout == "" {
		return
	}
	d, err := time.ParseDuration(agent.Manifest.CompactTimeout)
	if err != nil {
		log.Printf("[kernel] agent %q has invalid compact_timeout %q: %v (using default %v)",
			agent.Manifest.Name, agent.Manifest.CompactTimeout, err, DefaultCompactTimeout)
		return
	}
	if d > 0 {
		proc.CompactTimeout = d
		proc.compactTimeoutExplicit = true
	} else {
		log.Printf("[kernel] agent %q has non-positive compact_timeout %q: %v (using default %v)",
			agent.Manifest.Name, agent.Manifest.CompactTimeout, d, DefaultCompactTimeout)
	}
}

// mechanicalFallbackName is the value of the `fallback` event key that marks a
// deterministic, LLM-free reclamation. Consumers distinguish a degraded
// compaction from a successful one purely by this key's PRESENCE — the success
// paths must never carry it (Story 69.3 AC7).
const mechanicalFallbackName = "mechanical_prune"

// mechanicalFallbackResult reports what runMechanicalFallback reclaimed.
type mechanicalFallbackResult struct {
	Pruned        int
	TokensFreed   int
	DroppedRounds int
	SlotsFreed    int
	// ReleasableTokens is what the cold-zone scan found it could actually free,
	// reported whether or not the floor admitted the rewrite. Paired with
	// ClearAtLeast it is the difference between "nothing was leaked" and "plenty
	// was leaked but it could not reach the landing point" (Story 71.2 AC2's
	// honest-insufficiency requirement).
	ReleasableTokens int
	// ClearAtLeast echoes the floor this run was given, so an event consumer can
	// read the decision without re-deriving it.
	ClearAtLeast int
}

// fallbackTarget carries what a mechanical reclamation is aiming for. The two
// fields are on DIFFERENT axes and neither substitutes for the other — which is
// precisely why the old `needSlots int` parameter had nowhere to put a token
// target (Story 71.1 left a TODO here rather than invent one).
//
//	NeedSlots    → slot axis, consumed by DropOldestRounds. Only a caller under
//	               genuine slot pressure sets it, and since Story 71.1 removed the
//	               slot ceiling that means the resume/precompact paths alone.
//	ClearAtLeast → token axis, consumed by PruneToolResults as a minimum-release
//	               floor. Derived from the landing point (Story 71.2 AC1/AC2).
type fallbackTarget struct {
	NeedSlots    int
	ClearAtLeast int
}

// clearAtLeastForLanding converts a usage snapshot into the token floor that
// would put this process at its landing point (Story 71.2 AC1/AC2 — the same
// arithmetic seen from its two ends).
//
//	landingPct   = threshold / compactHysteresisRatio      // 80 / 1.5 = 53.3%
//	targetTokens = limit × landingPct / 100
//	clearAtLeast = max(used − targetTokens, 0)
//
// This is cc-src's `clear_at_least = trigger − keep` shape (apiMicrocompact.ts).
// That field itself is not portable — it is an Anthropic SERVER-side
// context_management parameter with no local consumer — but the arithmetic
// behind it is, and it is why AC1's floor and AC2's landing point are one
// computation rather than two gates that would shadow each other.
//
// Returns 0 (= no floor) when usage is already at or below the landing point:
// there is nothing to demand, and a caller must not be blocked from a prune it
// happens to want anyway.
func (p *Process) clearAtLeastForLanding(usage rnixctx.TokenStats) int {
	limit := usage.Limit
	if limit <= 0 {
		// No capacity scale to land against. Demanding a floor off a fabricated
		// denominator would be the third un-validated threshold in this
		// subsystem; decline to set one instead.
		return 0
	}
	targetTokens := int(float64(limit) * p.effectiveCompactLandingPct() / 100)
	return max(usage.Used-targetTokens, 0)
}

// clearAtLeastNow reads current usage and derives the landing floor from it. Used
// by the call sites that do not already hold a usage snapshot.
//
// 🔴 Called from the kernel side, OUTSIDE the reclamation primitives: TokenUsage
// takes ctx.mu and runs Sections.Build(), both forbidden inside
// PruneToolResults' write lock (prune.go lock contract). A failed read yields 0,
// i.e. no floor — degrade toward reclaiming, never toward refusing.
func (k *KernelImpl) clearAtLeastNow(proc *Process) int {
	usage, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		return 0
	}
	return proc.clearAtLeastForLanding(usage)
}

// runMechanicalFallback is the deterministic, LLM-free reclamation used whenever
// a compaction attempt fails (Story 69.3 AC3 / AC4 / AC6). It is the reason a
// failed compaction no longer equals a dead process: the incident saw 27
// consecutive compaction failures and 0 successes, with no path that did not
// itself require the LLM.
//
// Two primitives, two axes — deliberately not merged (Story 69.3 decision 1):
//   - PruneToolResults reclaims TOKENS by rewriting cold leaked tool results in
//     place. It cannot free slots: slot usage is literally len(ctx.Messages).
//   - DropOldestRounds reclaims SLOTS by dropping whole API rounds. Only called
//     when the caller is actually short on slots (NeedSlots > 0), because
//     dropping history costs more than clearing stale payloads.
//
// Story 71.2 AC1/AC2 gave the token half a TARGET: target.ClearAtLeast is a
// minimum-release floor, so the prune is all-or-nothing against the landing
// point instead of dribbling out whatever it finds. Reclaiming a little, every
// step, is strictly worse than reclaiming nothing — each rewrite invalidates the
// provider's cached prompt prefix while leaving usage above the trigger, which
// is the measured 88%-vs-3% cache collapse this story exists to stop.
//
// A floor of 0 means "reclaim whatever you find", which is what the resume path
// wants (see reclaimForResume).
//
// Both preserve the tool_use ↔ tool_result pairing invariant; violating it makes
// anthropic reject the next request outright (its driver builds tool_result
// blocks straight from ToolCallID with no repair).
//
// Errors from either primitive are logged, not propagated: this is a
// best-effort last line of defence and the caller decides what to do with the
// (possibly zero) reclamation reported back.
func (k *KernelImpl) runMechanicalFallback(proc *Process, target fallbackTarget) mechanicalFallbackResult {
	out := mechanicalFallbackResult{ClearAtLeast: target.ClearAtLeast}

	pruneOpts := rnixctx.PruneOpts{ClearAtLeast: target.ClearAtLeast}
	if pruneRes, err := k.ctxMgr.PruneToolResults(proc.CtxID, pruneOpts); err != nil {
		log.Printf("[kernel] pid=%d mechanical prune failed: %v", proc.PID, err)
	} else if pruneRes != nil {
		out.Pruned = pruneRes.Pruned
		out.TokensFreed = pruneRes.TokensFreed
		out.ReleasableTokens = pruneRes.ReleasableTokens
	}

	if target.NeedSlots > 0 {
		if dropRes, err := k.ctxMgr.DropOldestRounds(proc.CtxID, rnixctx.DropOpts{NeedSlots: target.NeedSlots}); err != nil {
			log.Printf("[kernel] pid=%d mechanical round drop failed: %v", proc.PID, err)
		} else if dropRes != nil {
			out.DroppedRounds = dropRes.DroppedRounds
			out.SlotsFreed = dropRes.SlotsFreed
			out.TokensFreed += dropRes.TokensFreed
		}
	}

	return out
}

// addFallbackArgs stamps a mechanical-fallback reclamation onto an event's args.
//
// AC7 (provenance): the `fallback` key is the sole discriminator, so it goes on
// degraded events ONLY. Never fill restored_items here — mechanical reclamation
// performs no post-compact restore — and never present these post_tokens as an
// LLM compaction result.
func addFallbackArgs(args map[string]any, res mechanicalFallbackResult) {
	args["fallback"] = mechanicalFallbackName
	args["pruned"] = res.Pruned
	args["tokens_freed"] = res.TokensFreed
	args["dropped_rounds"] = res.DroppedRounds
	args["slots_freed"] = res.SlotsFreed
	args["fallback_freed"] = res.TokensFreed + res.SlotsFreed
	// Story 71.2 AC1/AC2: the floor asked for and what the scan could actually
	// have released. Together they separate "there was nothing to reclaim" from
	// "there was plenty, but not enough to reach the landing point, so the batch
	// was declined rather than spent on a cache invalidation".
	args["clear_at_least"] = res.ClearAtLeast
	args["releasable_tokens"] = res.ReleasableTokens
}

// resumeFallbackHeadroom is the number of message slots a revived process needs
// before its first reasonStep can do anything useful (1 assistant + at least a
// couple of tool results).
const resumeFallbackHeadroom = 3

// reclaimForResume runs the mechanical fallback on behalf of a resume path and
// reports whether the process now has room to run (Story 69.3 AC6 / F1).
//
// Both resume paths used to call finishProcess(ExitContextFull) the moment the
// resume-time compaction failed — killing the process outright. That is a
// different code path from autoCompactIfNeeded (which is best-effort and never
// kills), and it is the real terminus of the incident's
// 2391→2392→2393→2394 chain: every revival immediately compacted, timed out
// after 30s, and died. Killing is now the LAST resort, reached only when the
// deterministic reclamation also came up empty.
//
// ⚠️ Since Story 71.1 (AC4) this returns true unconditionally when no slot
// ceiling is configured: AvailableSlots yields the unlimitedSlots sentinel, so
// both the needSlots computation and the final `avail >= resumeFallbackHeadroom`
// comparison are decided in advance. The two callers' finishProcess(
// ExitContextFull) fallbacks are consequently UNREACHABLE.
//
// Those fallbacks must stay exactly as they are. Story 71.4 AC6 requires that
// the kill-on-resume behaviour does not come back, and whoever reads that dead
// branch later needs this note to know its unreachability came from removing the
// slot ceiling here, not from anything 71.4 did.
//
// 🔴 EXEMPT from Story 71.2's minimum-release floor (AC1 / F5), and the only
// exemption in the tree. Everywhere else "reclaim in bulk or not at all" is the
// right trade because the alternative is another cache-prefix invalidation for a
// couple of tokens. Here the alternative is killing the process: both callers
// terminate it outright when this comes back short. A floor is a refusal to act,
// and the last line of defence does not get to refuse.
func (k *KernelImpl) reclaimForResume(proc *Process) (mechanicalFallbackResult, bool) {
	needSlots := resumeFallbackHeadroom
	if avail, err := k.ctxMgr.AvailableSlots(proc.CtxID); err == nil && avail < resumeFallbackHeadroom {
		needSlots = resumeFallbackHeadroom - avail
	} else if err == nil {
		// Already has headroom; still prune tokens, but do not drop history.
		needSlots = 0
	}

	res := k.runMechanicalFallback(proc, fallbackTarget{NeedSlots: needSlots})

	avail, err := k.ctxMgr.AvailableSlots(proc.CtxID)
	if err != nil {
		return res, false
	}
	return res, avail >= resumeFallbackHeadroom
}

// slotCeilingUnloadThreshold is the slot-usage percentage above which
// unloadForResume reclaims. It is a plain constant because the process field it
// used to read (SlotCompactThreshold) had NO writer anywhere in the tree — the
// field and its "0 = use default" accessor advertised a knob that was in fact
// hardcoded 80.0, so Story 71.1 deleted the pretence and kept the number (Epic 67
// "命名诚实化" precedent).
//
// Only reachable when a caller passes a positive ctxSize, i.e. when an operator
// explicitly configured a slot ceiling. Both production resume paths pass 0 since
// Story 71.1.
const slotCeilingUnloadThreshold = 80.0

// unloadForResume proactively reclaims context BEFORE a revived process starts
// reasoning (Story 69.3 AC5/AC6 preventive half). A snapshot restored right at
// its slot ceiling hits preCompactForToolCalls on step one, which is how a
// resumed process replays the very failure it was suspended for.
//
// ⚠️ STRUCTURALLY NO-OP IN PRODUCTION since Story 71.1 (AC4): the pressure it
// measures is slot pressure, and there is no slot ceiling any more — both resume
// paths therefore pass ctxSize 0 and return at the first line. It is kept, rather
// than deleted, because the ceiling survives as an operator escape hatch at FRESH
// SPAWN (SpawnOpts.CtxSize / agent.yaml ctx_size). It does NOT protect such a
// process on revival: Context.Deserialize forces MaxSize to 0 unconditionally
// (AC6-④), so a configured ceiling vanishes on the first resume (recorded in
// deferred-work). Do NOT read its existence as evidence that resumes are being
// trimmed today; the equivalent token-axis protection is reclaimLeakedIfNeeded
// plus autoCompactIfNeeded on step one.
//
// Deliberately NOT gated on proc.CompactionDisabled: disabling routine
// compaction must not mean "prefer to hang". This is fault handling, not
// routine reclamation.
func (k *KernelImpl) unloadForResume(proc *Process, ctxSize int, label string) {
	if ctxSize <= 0 {
		return
	}
	used, _, err := k.ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		return
	}
	threshold := slotCeilingUnloadThreshold
	if float64(used)/float64(ctxSize)*100 <= threshold {
		return
	}

	target := int(float64(ctxSize) * threshold / 100)
	res := k.runMechanicalFallback(proc, fallbackTarget{
		NeedSlots: max(used-target, resumeFallbackHeadroom),
		// Preventive, not last-resort: the process has not started yet and
		// nothing kills it if this reclaims nothing, so it takes the same
		// bulk-or-nothing floor as every other call site. (Contrast
		// reclaimForResume, which is exempt because its callers terminate.)
		ClearAtLeast: k.clearAtLeastNow(proc),
	})
	postUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	log.Printf("[kernel] %s uuid=%s: pre-start unload at %d/%d slots (>%.0f%%): pruned=%d dropped_rounds=%d tokens_freed=%d → %d/%d slots",
		label, proc.UUID, used, ctxSize, threshold, res.Pruned, res.DroppedRounds, res.TokensFreed, postUsed, ctxSize)
}

// reclaimLeakedIfNeeded proactively clears stale tool outputs out of the cold
// zone BEFORE the compaction threshold is reached (Story 69.4). It is the
// consumer for the "consider pruning unused tool outputs" suggestion that
// debug/ctx_profile.go has been generating with nothing acting on it.
//
// TWO GATES, both mandatory. The cost being rationed is not cpu — the leaked
// predicate is a role compare plus a len(). It is the PROVIDER PROMPT CACHE
// PREFIX: drivers/llm/anthropic.go's applyCacheControlToMessageHistory puts the
// cache breakpoint after the message history, so the cold zone sits inside the
// cached prefix and rewriting any byte of it invalidates the whole thing. An
// ungated per-step reclamation would invalidate the cache on every single step
// (the cold-zone boundary marches right as messages accumulate, so fresh
// entries keep falling in), which is the same failure mode Story 69.1 removed
// on the system-prompt axis — 15k-token full recomputes and a 7.4s → 48.7s
// latency blow-up.
//
//	watermark gate — fire only once usage is within striking distance of the
//	  compact threshold, so low-pressure steps leave the context byte-identical.
//	  Derived as a RATIO of the threshold, never a subtraction: threshold-20 goes
//	  negative for any configured threshold ≤ 20 and then fires every step (same
//	  倒挂 trap Story 69.1's tier boundary formula avoids).
//	yield gate — enforced inside PruneToolResults via MinTokens, so the scan and
//	  the rewrite decision happen under ONE write lock. Splitting them into a
//	  read-only scan plus a rewrite would be a TOCTOU window against the real
//	  concurrent writers (IPC handleCompact / gdb AppendMessage / fork_continue).
//
// Honest accounting (story F2): PruneToolResults frees exactly 0 message slots
// by design (slot usage is literally len(ctx.Messages)), so this function does
// NOT relieve slot pressure and must not be described as preventing a hang. What
// it buys is a smaller request body every subsequent step, and a deferred token
// threshold.
//
// No cooldown timer, deliberately (story F4): the placeholder is 37 bytes versus
// a 1000-byte LeakedThreshold, so a rewritten entry can never match the
// predicate again, and the yield gate then requires a fresh batch to accumulate.
// The hysteresis is structural — no "last reclaimed step" process field needed.
func (k *KernelImpl) reclaimLeakedIfNeeded(proc *Process, step int) {
	// AC5: routine reclamation, so it honours the same flag that suppresses the
	// `frc` system-prompt section (kernel/sections.go). Deliberately ASYMMETRIC
	// with Story 69.3: runMechanicalFallback / unloadForResume / reclaimForResume
	// stay ungated because "disabling routine compaction must not mean 'prefer to
	// hang'" — those are fault handling. This one is routine, so it stops here.
	if proc.CompactionDisabled {
		return
	}

	// The yield gate scales with the RECLAIMABLE pool, not total usage. The system
	// prompt can never be reclaimed (it is not a cold-zone tool result), so
	// counting it in the denominator would raise the gate above what the entire
	// message history can supply on a system-prompt-heavy agent — silently
	// disabling reclamation for exactly the large-prefix agents this story
	// protects. Read it with no lock held: GetFinalSystemPrompt takes proc.mu, and
	// we deliberately avoid introducing a compactMu → proc.mu ordering.
	sysPromptTokens := rnixctx.EstimateTokens(proc.GetFinalSystemPrompt())

	// Same mutual exclusion as autoCompactIfNeeded: a manual IPC compact must not
	// interleave with an in-place rewrite of the same message slice.
	if !proc.compactMu.TryLock() {
		return
	}
	defer proc.compactMu.Unlock()

	start := time.Now()

	// One TokenUsage call covers both the token total and (since Story 69.2) the
	// slot fields, read under the SAME read lock. Story 71.1 retired the slot
	// TRIGGER, so only usage.Percentage gates this now; the slot fields survive
	// purely as observability (ipc/protocol.go ContextStatsWire).
	usage, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		return
	}

	tokenWatermark := proc.effectiveCompactThreshold() * proactiveReclaimWatermarkRatio

	if usage.Percentage <= tokenWatermark {
		return
	}

	// Trigger classification keeps the _watermark suffix so an event consumer can
	// never confuse a proactive reclamation trigger with a compaction one.
	//
	// Story 71.1 AC3: the value域 lost `slot_watermark` (and with it the `both`
	// degeneracy) along with the slot trigger axis. Token pressure is now the only
	// way to reach this line, so the label is a constant rather than a
	// classification — kept as a named variable because the event contract has
	// always carried a `trigger` key and consumers read it.
	trigger := "token_watermark"

	// Yield gate: an absolute floor for small contexts, scaled by the reclaimable
	// pool for large ones. usage.Used includes the system prompt, which can never
	// be reclaimed, so subtract it before scaling — otherwise a large system
	// prompt raises the gate above what the message history can ever supply.
	// Evaluated inside the primitive so the decision is atomic with the scan that
	// informs it.
	reclaimable := max(usage.Used-sysPromptTokens, 0)
	minTokens := max(minReclaimTokens, reclaimable*minReclaimRatioPct/100)

	res, err := k.ctxMgr.PruneToolResults(proc.CtxID, rnixctx.PruneOpts{MinTokens: minTokens})
	if err != nil {
		log.Printf("[kernel] pid=%d step=%d proactive reclaim failed: %v", proc.PID, step, err)
		return
	}
	if res.Pruned == 0 {
		// Nothing worth a cache invalidation. Emit no EVENT rather than spamming
		// the event stream on every step above the watermark — AC6 keeps this at
		// debug level precisely so the event stream does not become unreadable.
		// (A daemon-log line is still written; that is expected, not silence.)
		log.Printf("[kernel] pid=%d step=%d proactive reclaim declined (%s): %s min_tokens=%d",
			proc.PID, step, trigger, res, minTokens)
		return
	}

	args := map[string]any{
		"step":             step,
		"trigger":          trigger,
		"pruned":           res.Pruned,
		"tokens_freed":     res.TokensFreed,
		"candidate_tokens": res.CandidateTokens,
		"pre_tokens":       usage.Used,
		"pre_pct":          usage.Percentage,
	}

	// post_* comes from a re-read so it reflects reality rather than
	// pre-tokens minus an estimate. Error shape reuses the Story 69.3 convention
	// (post_tokens_err instead of a fabricated number).
	postUsage, postErr := k.ctxMgr.TokenUsage(proc.CtxID)
	if postErr != nil {
		args["post_tokens_err"] = postErr.Error()
	} else {
		args["post_tokens"] = postUsage.Used
		args["post_pct"] = postUsage.Percentage
	}

	k.emitEvent(proc, "CtxReclaim", args, nil, nil, time.Since(start))

	// Log the same figures the event carries. On a post-read failure, report the
	// error instead of the zero-value TokenStats — printing "N→0" would make it
	// look like the context was emptied, contradicting the post_tokens_err marker
	// an operator correlates against (observability provenance).
	if postErr != nil {
		log.Printf("[kernel] pid=%d step=%d proactive reclaim (%s): pruned=%d tokens %d→? (post-read failed: %v) candidates=%d",
			proc.PID, step, trigger, res.Pruned, usage.Used, postErr, res.CandidateTokens)
	} else {
		log.Printf("[kernel] pid=%d step=%d proactive reclaim (%s): pruned=%d tokens %d→%d (%.1f%%→%.1f%%) candidates=%d",
			proc.PID, step, trigger, res.Pruned, usage.Used, postUsage.Used,
			usage.Percentage, postUsage.Percentage, res.CandidateTokens)
	}
}

// autoCompactIfNeeded checks if context token usage exceeds the compact
// threshold and triggers automatic compaction if so. Best-effort: failures are
// logged but do not terminate the process.
//
// Story 71.1 AC3 retired the second, slot-based trigger. Slots量的是 STRUCTURE
// (message count); tokens量的是 CAPACITY (volume). The two have no stable
// conversion rate — 205 slots measured anywhere from 36.7k to 146.2k real tokens
// (4.0x spread) — so a slot threshold is a量纲 error, not a mis-calibration, and
// no threshold value could fix it. Because the slot axis fired at ~36k tokens it
// always preceded the token axis, which is why the incident's 47 compactions were
// ALL labelled slot_threshold and the freshly-connected token scale (Story 69.2)
// never once got to speak.
func (k *KernelImpl) autoCompactIfNeeded(proc *Process, step int) {
	if proc.CompactionDisabled {
		return
	}
	// Prevent concurrent compact (auto + manual IPC)
	if !proc.compactMu.TryLock() {
		return
	}
	defer proc.compactMu.Unlock()

	// Slot figures come from this SAME TokenUsage snapshot rather than a second
	// SlotUsage() call: two calls take two locks and could mix two different
	// context states into one event (the reason reclaimLeakedIfNeeded named this
	// function as its counter-example). They are observability only now.
	usage, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		return
	}
	slotUsed, slotMax := usage.SlotUsed, usage.SlotMax

	if usage.Percentage <= proc.effectiveCompactThreshold() {
		return
	}

	// Token pressure is the only remaining path to this line, so `trigger` is a
	// constant. It stays a named variable because the Compact event contract has
	// always carried the key and CompactOpts propagates it into context/compact.go.
	trigger := "token_threshold"

	compactStart := time.Now()

	// Build CompactOpts using shared helpers
	readFileState := k.SnapshotReadFileState(proc)
	activeSkills := k.BuildActiveSkills(proc)

	// Get active plan from the most recent plan action in context
	activePlan := k.extractActivePlan(proc.CtxID)

	opts := rnixctx.CompactOpts{
		LLMCall:       k.BuildCompactLLMCall(proc),
		Trigger:       trigger,
		ReadFileState: readFileState,
		ActiveSkills:  activeSkills,
		ActivePlan:    activePlan,
	}

	// --- Story 71.2 AC3: pre-flight gate, BEFORE the LLM request goes out ---
	//
	// Ported from qwen's MIN_COMPRESSION_FRACTION (chatCompressionService.ts),
	// with its "preserved portion" mapped onto rnix's structure. qwen keeps the
	// newest 30% of history and compresses the rest, so it can ask "is the part
	// I would compress at least 5% of the whole?". rnix compresses ALL messages
	// and then RESTORES files / skills / plan, so the restore payload is what
	// plays the part of the preserved portion:
	//
	//	compressible = reclaimable − what the restore puts straight back
	//
	// The failure this stops is the one that actually happens: a process whose
	// context is mostly restore payload sits above the threshold, compacts,
	// lands back on the same restore payload, and compacts again next step —
	// each attempt a 30s timeout risk and a full-history request body, for a
	// context that cannot get smaller. AC2's landing-point arithmetic has the
	// same physical floor (files 50k + skills 25k + an untruncated plan), so
	// this gate is where that floor is detected rather than repeatedly paid for.
	//
	// 🔴 The denominator is the reclaimable pool, not usage.Used: the system
	// prompt can never be compacted away, so counting it would raise the bar
	// above what the message history can ever supply on a large-prompt agent —
	// the same trap reclaimLeakedIfNeeded's yield gate documents.
	//
	// Read the system prompt with no context lock held (GetFinalSystemPrompt
	// takes proc.mu) — same ordering rule as reclaimLeakedIfNeeded.
	//
	// Deliberately conservative: the summary the LLM will produce is NOT counted
	// as surviving, because its size cannot be known before the call. That
	// overstates `compressible`, i.e. it biases toward attempting the compaction.
	// A gate that guesses high and blocks a useful compaction would trade a
	// recoverable 30s failure for an unrecoverable context overflow.
	sysPromptTokens := rnixctx.EstimateTokens(proc.GetFinalSystemPrompt())
	reclaimable := max(usage.Used-sysPromptTokens, 0)
	restoreTokens := rnixctx.EstimateRestoreTokens(opts)
	compressible := max(reclaimable-restoreTokens, 0)

	// 🔴 FAIL OPEN, never closed. `reclaimable` is a subtraction across two
	// sources — usage.Used counts the context's own system prompt, while
	// sysPromptTokens reads the kernel-side proc field — and production keeps
	// them equal only because spawn writes both. Any divergence drives the
	// difference to 0, and treating that as "nothing to compact" would silently
	// disable compaction for the life of the process: a permanently wedged
	// context, reached without a single error being raised.
	//
	// So a non-positive pool means "cannot judge", and the compaction proceeds.
	// The gate exists to avoid a recoverable 30s timeout; blocking a needed
	// compaction risks an unrecoverable overflow. Those are not symmetric, and
	// the tie goes to attempting.
	if reclaimable > 0 && compressible*100 < reclaimable*minCompactionFractionPct {
		// NOOP, and specifically NOT an error (Story 71.2 AC3): the five
		// Compact() callers each handle errors differently, and
		// autoCompactIfNeeded's handler is the mechanical fallback — so
		// signalling "not worth compacting" as a failure would trigger the very
		// reclamation this gate just declined. qwen's counterpart is likewise a
		// CompressionStatus.NOOP enum, not a thrown error.
		//
		// No event either: this is a routine early exit like the threshold check
		// above, and emitting one per step above the threshold would flood the
		// event stream (the reason Story 69.4's declined reclamation logs at
		// daemon level only). The Compact event contract is reserved for
		// attempts. Story 71.4 may promote this to a first-class observable when
		// it builds the started/completed event pairing.
		log.Printf("[kernel] pid=%d step=%d auto-compact declined (%s): compressible=%d of reclaimable=%d (<%d%%), restore_floor=%d, token=%.1f%%",
			proc.PID, step, trigger, compressible, reclaimable, minCompactionFractionPct, restoreTokens, usage.Percentage)
		return
	}

	log.Printf("[kernel] pid=%d step=%d auto-compact triggered (%s): token=%.1f%%, slots=%d/%d, compressible=%d/%d",
		proc.PID, step, trigger, usage.Percentage, slotUsed, slotMax, compressible, reclaimable)

	result, err := k.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		// Story 69.3 AC3 — the LLM compaction failed, so fall back to the
		// deterministic reclamation instead of giving up. Before this, the
		// failure branch only logged: the incident's 27 consecutive failures
		// left the context untouched every single time, so the very next step
		// hit the same wall.
		//
		// Story 71.2 AC1/AC2 replaced the retired slot target (slotMax ×
		// SlotCompactThreshold, dead since Story 71.1 removed the ceiling) with
		// the TOKEN-axis one: a floor equal to the release needed to land at
		// threshold/compactHysteresisRatio. `usage` is still valid here — a
		// failed Compact() mutates nothing — so no second read is needed.
		//
		// NeedSlots stays 0: DropOldestRounds is the SLOT-axis primitive and
		// there is no slot ceiling to be short of. Converting a token shortfall
		// into a slot count would need a tokens-per-message rate, and Story 71.1
		// measured that at a 4.0x spread — inventing one here would re-introduce
		// exactly the量纲 error this epic removed. When the prune alone cannot
		// reach the landing point, the shortfall is REPORTED (see
		// landing_reached below), not padded out by dropping history.
		clearAtLeast := proc.clearAtLeastForLanding(usage)
		fallback := k.runMechanicalFallback(proc, fallbackTarget{ClearAtLeast: clearAtLeast})

		// pre_tokens must be sourced locally here: on the failure path `result`
		// is nil, so result.PreTokens (used by the success path below) would
		// panic. usage.Used was captured before the attempt.
		postUsage, postUsageErr := k.ctxMgr.TokenUsage(proc.CtxID)
		postSlotUsed, _, postSlotErr := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":          step,
			"trigger":       trigger,
			"error":         err.Error(),
			"compact_error": err.Error(),
			"pre_tokens":    usage.Used,
			"pre_slots":     slotUsed,
		}
		if postUsageErr != nil {
			args["post_tokens_err"] = postUsageErr.Error()
		} else {
			args["post_tokens"] = postUsage.Used
		}
		if postSlotErr != nil {
			args["post_slots_err"] = postSlotErr.Error()
		} else {
			args["post_slots"] = postSlotUsed
		}
		addFallbackArgs(args, fallback)

		// Story 71.2 AC2 — report the landing point honestly. The restore budget
		// (files 50k + skills 25k + an UNTRUNCATED plan) puts a hard floor under
		// how low a context can go, so a large plan can make the target
		// physically unreachable. When that happens the event says so; it must
		// never be papered over, and the reclamation must never break round
		// boundaries to manufacture a number ("insufficiency is reported, not
		// raised", context/prune.go).
		args["landing_target_pct"] = proc.effectiveCompactLandingPct()
		if postUsageErr == nil {
			args["post_pct"] = postUsage.Percentage
			args["landing_reached"] = postUsage.Percentage <= proc.effectiveCompactLandingPct()
		}
		k.emitEvent(proc, "Compact", args, nil, err, time.Since(compactStart))

		log.Printf("[kernel] pid=%d compact failed (%v); mechanical fallback: pruned=%d tokens %d→%d, dropped_rounds=%d slots %d→%d, clear_at_least=%d releasable=%d landing_target=%.1f%%",
			proc.PID, err, fallback.Pruned, usage.Used, postUsage.Used,
			fallback.DroppedRounds, slotUsed, postSlotUsed,
			clearAtLeast, fallback.ReleasableTokens, proc.effectiveCompactLandingPct())

		// Best-effort by contract: never terminate the process from here, even
		// when the fallback reclaimed nothing.
		return
	}

	// Clear ReadFileState after successful compact
	k.ClearReadFileState(proc)

	postSlotUsed, postSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	k.emitEvent(proc, "Compact", map[string]any{
		"step":           step,
		"trigger":        trigger,
		"pre_tokens":     result.PreTokens,
		"post_tokens":    result.PostTokens,
		"pre_slots":      slotUsed,
		"post_slots":     postSlotUsed,
		"restored_items": result.ItemsRestored,
		"duration_ms":    float64(result.Duration.Microseconds()) / 1000.0,
	}, nil, nil, result.Duration)

	log.Printf("[kernel] pid=%d compact done in %dms: %d→%d tokens, slots %d/%d→%d/%d, restored %d items",
		proc.PID, result.Duration.Milliseconds(),
		result.PreTokens, result.PostTokens,
		slotUsed, slotMax, postSlotUsed, postSlotMax,
		len(result.ItemsRestored))
}

// preCompactForToolCalls checks if there are enough message slots for the
// upcoming AppendAssistantWithToolCalls (1 assistant + N tool results) and
// triggers a compact if not. Best-effort: TryLock failure means another compact
// is running so we skip and let the caller try the append directly.
//
// ⚠️ STRUCTURALLY NO-OP IN PRODUCTION since Story 71.1 (AC4/F3). With no slot
// ceiling AvailableSlots returns the unlimitedSlots sentinel, so
// `available >= required` is always true and this function returns nil on its
// second line — everything below (the compaction, the mechanical fallback, the
// post-compact top-up) is unreachable. The whole function is a SLOT-pressure
// remedy; the atomic-admission guarantee it was protecting lives in
// context.AppendAssistantWithToolCalls and is untouched.
//
// Kept rather than deleted for two reasons: an explicit ctx_size escape hatch
// still reaches it, and Story 71.2 rewrites the reclamation arithmetic on the
// token axis and will decide then whether any of this survives. Registered in
// deferred-work.md — do not read its existence as evidence that it still runs.
func (k *KernelImpl) preCompactForToolCalls(proc *Process, toolCallCount int, step int) error {
	required := 1 + toolCallCount
	available, err := k.ctxMgr.AvailableSlots(proc.CtxID)
	if err != nil || available >= required {
		return nil
	}

	if !proc.compactMu.TryLock() {
		return nil
	}
	defer proc.compactMu.Unlock()

	preSlotUsed, preSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	log.Printf("[kernel] pid=%d step=%d precompact: need %d slots, have %d",
		proc.PID, step, required, available)

	compactStart := time.Now()
	opts := rnixctx.CompactOpts{
		LLMCall:       k.BuildCompactLLMCall(proc),
		Trigger:       "precompact",
		ReadFileState: k.SnapshotReadFileState(proc),
		ActiveSkills:  k.BuildActiveSkills(proc),
		ActivePlan:    k.extractActivePlan(proc.CtxID),
	}

	result, err := k.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		// Story 69.3 AC4 — precompact pressure is PURELY about slots (see the
		// `required` computation above: tokens are never consulted), so the
		// token-axis prune cannot rescue this path. DropOldestRounds is what
		// moves AvailableSlots, which is exactly why the two primitives are
		// separate.
		//
		// Returning an error here is not the end of the world by itself
		// (tool_exec.go only logs it), but the very next
		// AppendAssistantWithToolCalls then returns ErrContextFull and the
		// process self-suspends. Reclaiming mechanically first turns that into a
		// continuable step.
		// AC4 pressure is PURELY about slots, so NeedSlots carries the target;
		// the token floor rides along because a slot-starved context is usually
		// token-heavy too, and Story 71.2 exempts only reclaimForResume from it.
		fallback := k.runMechanicalFallback(proc, fallbackTarget{
			NeedSlots:    required - available,
			ClearAtLeast: k.clearAtLeastNow(proc),
		})
		postAvail, _ := k.ctxMgr.AvailableSlots(proc.CtxID)
		postSlotUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":          step,
			"trigger":       "precompact",
			"error":         err.Error(),
			"compact_error": err.Error(),
			"pre_slots":     preSlotUsed,
			"post_slots":    postSlotUsed,
		}
		addFallbackArgs(args, fallback)
		k.emitEvent(proc, "Compact", args, nil, err, time.Since(compactStart))

		if postAvail >= required {
			log.Printf("[kernel] pid=%d precompact compact failed (%v) but mechanical fallback freed %d slots: need %d, have %d — continuing",
				proc.PID, err, fallback.SlotsFreed, required, postAvail)
			return nil
		}
		return fmt.Errorf("precompact failed: %w", err)
	}

	k.ClearReadFileState(proc)
	postSlotUsed, postSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	k.emitEvent(proc, "Compact", map[string]any{
		"step":           step,
		"trigger":        "precompact",
		"pre_tokens":     result.PreTokens,
		"post_tokens":    result.PostTokens,
		"pre_slots":      preSlotUsed,
		"post_slots":     postSlotUsed,
		"restored_items": result.ItemsRestored,
		"duration_ms":    float64(result.Duration.Microseconds()) / 1000.0,
	}, nil, nil, result.Duration)

	log.Printf("[kernel] pid=%d precompact done in %dms: %d→%d tokens, slots %d/%d→%d/%d, restored %d items",
		proc.PID, result.Duration.Milliseconds(),
		result.PreTokens, result.PostTokens,
		preSlotUsed, preSlotMax, postSlotUsed, postSlotMax,
		len(result.ItemsRestored))

	available, _ = k.ctxMgr.AvailableSlots(proc.CtxID)
	if available < required {
		// Story 69.3 AC4 — the compaction SUCCEEDED yet left too few slots (a
		// small context can compact down to boundary+summary and still not fit
		// 1 assistant + N tool results). Top up mechanically before declaring
		// insufficiency, same reasoning as the failure branch above.
		fallback := k.runMechanicalFallback(proc, fallbackTarget{
			NeedSlots:    required - available,
			ClearAtLeast: k.clearAtLeastNow(proc),
		})
		postAvail, _ := k.ctxMgr.AvailableSlots(proc.CtxID)
		postSlotUsed2, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":       step,
			"trigger":    "precompact",
			"reason":     "post_compact_insufficient",
			"pre_slots":  postSlotUsed,
			"post_slots": postSlotUsed2,
		}
		addFallbackArgs(args, fallback)
		k.emitEvent(proc, "Compact", args, nil, nil, 0)

		if postAvail >= required {
			log.Printf("[kernel] pid=%d precompact still short after compact; mechanical fallback freed %d slots: need %d, have %d — continuing",
				proc.PID, fallback.SlotsFreed, required, postAvail)
			return nil
		}
		return fmt.Errorf("precompact freed space but still insufficient: need %d, have %d", required, postAvail)
	}
	return nil
}

// BuildCompactLLMCall creates the LLMCall callback for compact operations.
// Exported for use by IPC server's handleCompact.
func (k *KernelImpl) BuildCompactLLMCall(proc *Process) func(string, []rnixctx.Message) (string, error) {
	return func(sysPrompt string, messages []rnixctx.Message) (string, error) {
		timeout := proc.effectiveCompactTimeout()
		compactCtx, cancel := gocontext.WithTimeout(proc.ctx, timeout)
		defer cancel()

		// Open LLM device
		var fd types.FD
		var err error
		if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
			provider := strings.TrimPrefix(proc.PrimaryDevice, "/dev/llm/")
			fileAny, openErr := proc.ProjectConfig.LLMFileOpener(provider, int(vfs.O_RDWR))
			if openErr == nil {
				if vfsFile, ok := fileAny.(vfs.VFSFile); ok {
					fd = k.vfs.RegisterFD(proc.PID, vfsFile)
				} else {
					fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
				}
			} else {
				fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
			}
		} else {
			fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
		}
		if err != nil {
			return "", fmt.Errorf("compact: open LLM device failed: %w", err)
		}
		defer func() { _ = k.vfs.Close(proc.PID, fd) }()

		// Build LLM request — no tools, simplified system prompt
		req := llmRequest{
			Intent:       "compact",
			SystemPrompt: sysPrompt,
			Model:        proc.Model,
			Messages:     messages,
			// No Tools — compact prompt explicitly prohibits tool use
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			return "", fmt.Errorf("compact: marshal request failed: %w", err)
		}

		// Write request — use compactCtx for timeout propagation
		if err := k.vfs.Write(compactCtx, proc.PID, fd, reqJSON); err != nil {
			if compactCtx.Err() != nil {
				if compactCtx.Err() == gocontext.DeadlineExceeded {
					return "", fmt.Errorf("compact: LLM call timed out after %v", timeout)
				}
				return "", fmt.Errorf("compact: LLM call cancelled: %w", compactCtx.Err())
			}
			return "", fmt.Errorf("compact: LLM write failed: %w", err)
		}

		// Read response
		respData, err := k.vfs.Read(proc.PID, fd, 1<<20)
		if err != nil {
			if compactCtx.Err() != nil {
				if compactCtx.Err() == gocontext.DeadlineExceeded {
					return "", fmt.Errorf("compact: LLM call timed out after %v", timeout)
				}
				return "", fmt.Errorf("compact: LLM call cancelled: %w", compactCtx.Err())
			}
			return "", fmt.Errorf("compact: LLM read failed: %w", err)
		}

		// Parse response to extract content
		var resp llmResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			return "", fmt.Errorf("compact: unmarshal response failed: %w", err)
		}

		return resp.Content, nil
	}
}

// extractActivePlan scans the context for the most recent plan action content.
// Uses BuildPrompt to safely access messages without direct lock access.
func (k *KernelImpl) extractActivePlan(cid types.CtxID) string {
	prompt, err := k.ctxMgr.BuildPrompt(cid)
	if err != nil {
		return ""
	}

	// Scan messages in reverse to find the most recent plan
	for i := range slices.Backward(prompt.Messages) {
		msg := prompt.Messages[i]
		if msg.Role == rnixctx.RoleAssistant && strings.HasPrefix(msg.Content, "[Plan]") {
			return msg.Content
		}
	}
	return ""
}

// ExtractActivePlan is the exported variant for IPC server use.
func (k *KernelImpl) ExtractActivePlan(cid types.CtxID) string {
	return k.extractActivePlan(cid)
}

// SnapshotReadFileState returns a copy of the process's ReadFileState under lock.
func (k *KernelImpl) SnapshotReadFileState(proc *Process) map[string]rnixctx.ReadFileEntry {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.ReadFileState) == 0 {
		return nil
	}
	snapshot := make(map[string]rnixctx.ReadFileEntry, len(proc.ReadFileState))
	maps.Copy(snapshot, proc.ReadFileState)
	return snapshot
}

// ClearReadFileState nils out the process's ReadFileState under lock.
func (k *KernelImpl) ClearReadFileState(proc *Process) {
	proc.mu.Lock()
	proc.ReadFileState = nil
	proc.mu.Unlock()
}

// BuildActiveSkills constructs SkillEntry list from the process's loaded skills.
func (k *KernelImpl) BuildActiveSkills(proc *Process) []rnixctx.SkillEntry {
	proc.mu.Lock()
	loadedSkills := make([]string, len(proc.Skills))
	copy(loadedSkills, proc.Skills)
	proc.mu.Unlock()

	var activeSkills []rnixctx.SkillEntry
	if k.skillLoader != nil {
		for _, name := range loadedSkills {
			if skill, err := k.skillLoader(name); err == nil {
				activeSkills = append(activeSkills, rnixctx.SkillEntry{
					Name:    name,
					Content: skill.Body,
				})
			}
		}
	}
	return activeSkills
}

// trackReadFile records a file read into the process's ReadFileState for
// post-compact restore (Story 31.2 AC#5).
func (k *KernelImpl) trackReadFile(proc *Process, path string, content string, mtime time.Time) {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.ReadFileState == nil {
		proc.ReadFileState = make(map[string]rnixctx.ReadFileEntry)
	}
	proc.ReadFileState[path] = rnixctx.ReadFileEntry{
		Content:   content,
		Timestamp: time.Now(),
		Mtime:     mtime,
	}
}
