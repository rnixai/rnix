package context

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
)

// --- Classification criteria (Story 69.3 Task 1) ---
//
// These constants and window functions were migrated verbatim from
// debug/ctx_profile.go so the pruning primitives below and the `rnix ctx
// profile` analyzer share ONE definition of "which messages are cold /
// leaked". The direction is forced: debug already imports this package (for
// EstimateTokens), so the criteria must live here and be referenced from
// debug — the reverse is a compile-time import cycle.
//
// The algorithm is byte-equivalent to the pre-migration debug implementation
// on purpose. Do NOT "improve" the window formulas here: debug's
// AnalyzeContext output (leaked counts, suggestions text) is a public
// observability surface with equivalence tests guarding it.

const (
	// MinActiveWindow is the floor for the active (most recent) message window.
	MinActiveWindow = 4
	// MinWarmWindow is the floor for the warm (recently used) message window.
	MinWarmWindow = 6
	// LeakedThreshold is the byte length above which a cold-zone tool result
	// counts as leaked context.
	LeakedThreshold = 1000
)

// ActiveWindowSize returns the size of the active message window for a context
// holding n messages: 20% of the messages, with MinActiveWindow as the floor.
func ActiveWindowSize(n int) int {
	adaptive := n / 5 // 20% of messages
	if adaptive > MinActiveWindow {
		return adaptive
	}
	return MinActiveWindow
}

// WarmWindowSize returns the size of the warm message window for a context
// holding n messages: 30% of the messages, with MinWarmWindow as the floor.
func WarmWindowSize(n int) int {
	adaptive := n * 3 / 10 // 30% of messages
	if adaptive > MinWarmWindow {
		return adaptive
	}
	return MinWarmWindow
}

// ColdZoneEnd returns the exclusive upper bound of the cold zone for a context
// holding n messages, i.e. the warmStart index. Messages in [0, ColdZoneEnd(n))
// are cold; everything from there on is warm or active. Equivalent to
// debug's classifyMessages boundary computation.
func ColdZoneEnd(n int) int {
	activeStart := max(0, n-ActiveWindowSize(n))
	return max(0, activeStart-WarmWindowSize(n))
}

// IsLeakedToolResult reports whether msg is a tool result large enough to count
// as leaked context. The caller is responsible for restricting the check to the
// cold zone (see ColdZoneEnd) — this predicate deliberately does not know the
// message's position.
func IsLeakedToolResult(msg Message) bool {
	return msg.Role == RoleTool && len(msg.Content) > LeakedThreshold
}

// --- Mechanical reclamation primitives (Story 69.3 AC1 / AC2) ---

// DefaultPrunePlaceholder replaces the content of a pruned tool result. It is
// deliberately NOT the same string as drivers/llm's toolResultUnavailableStub:
// that one is a driver-side protocol repair for results that were already
// lost, this one records a context-side reclamation. Keeping them distinct
// means an incident investigator can tell WHO cleared the content.
//
// The wording matches the promise the `frc` system-prompt section already
// makes to the LLM ("Old device results may be automatically cleared from
// context to free up space").
const DefaultPrunePlaceholder = "[tool output cleared to free context]"

// PruneOpts configures PruneToolResults.
type PruneOpts struct {
	// TargetTokens stops the sweep once at least this many tokens have been
	// freed. 0 = prune every leaked tool result in the cold zone.
	TargetTokens int
	// MinTokens is an admission gate on the WHOLE batch: if the cold zone offers
	// fewer than this many reclaimable tokens, nothing is rewritten at all and
	// Pruned comes back 0. 0 = no gate = Story 69.3 behaviour, which every
	// mechanical-fallback call site relies on (they all pass PruneOpts{}).
	MinTokens int
	// Placeholder overrides the replacement content. Empty = DefaultPrunePlaceholder.
	Placeholder string
}

// PruneResult reports what PruneToolResults reclaimed.
type PruneResult struct {
	// Pruned is the number of tool-result messages whose content was replaced.
	Pruned int
	// TokensFreed is the estimated token reduction across those messages.
	TokensFreed int
	// CandidateTokens is the total reclaimable tokens the cold-zone scan found,
	// whether or not the rewrite went ahead. It is what lets an operator tell
	// "nothing was leaked" apart from "plenty was leaked but it did not clear
	// MinTokens" — two outcomes that both report Pruned == 0.
	CandidateTokens int
	// SlotsFreed is ALWAYS 0. This is a design fact, not a defect: pruning
	// rewrites Content in place, so len(ctx.Messages) — which is exactly what
	// SlotUsage / AvailableSlots report — cannot change. Slot reclamation is
	// the job of DropOldestRounds.
	SlotsFreed int
}

// PruneToolResults mechanically reclaims tokens by replacing the content of
// leaked tool results in the cold zone with a short placeholder. It never calls
// an LLM, so it is the deterministic fallback for a failed compaction.
//
// The message ENTRIES are preserved — only Content changes, while Role and
// ToolCallID are kept verbatim. Deleting a tool entry would orphan the
// assistant tool_use it pairs with, and the anthropic driver has no repair for
// that (drivers/llm/anthropic.go builds tool_result blocks straight from
// ToolCallID); the provider answers HTTP 400. Slot pressure therefore has its
// own primitive (DropOldestRounds) which drops whole API rounds.
//
// Idempotent: an entry already holding the placeholder is skipped, so repeated
// calls do not inflate Pruned / TokensFreed.
//
// MinTokens (Story 69.4 AC2) makes the sweep all-or-nothing below a caller-set
// bar. The cost it guards against is NOT cpu: the predicate is a role compare
// plus a len(). It is the PROMPT CACHE PREFIX. Rewriting a cold-zone message
// mutates bytes that sit before the provider's cache breakpoint
// (drivers/llm/anthropic.go applyCacheControlToMessageHistory), so every
// rewrite invalidates the cached prefix and the next request is billed and
// timed as a full recompute. A proactive caller must therefore reclaim rarely
// and in bulk; MinTokens is how it declines a rewrite that would not pay for
// the invalidation it causes.
//
// MinTokens == 0 means NO GATE and is byte-for-byte the pre-69.4 behaviour.
// This matters: all three Story 69.3 fallback paths pass PruneOpts{}, and
// reading zero as "apply a built-in default" would make the last line of
// defence refuse to reclaim exactly when it is needed.
//
// MinTokens and TargetTokens are orthogonal and may be combined: the former is
// an admission gate on the batch, the latter an early stop once enough has been
// freed.
//
// Lock contract (Story 69.1 red line): the whole rewrite happens under
// ctx.mu.Lock() and this function MUST NOT call Sections.Build() or any Manager
// method that re-acquires ctx.mu (TokenUsage / SlotUsage / BuildPrompt).
// Recursive acquisition with a writer queued is a deterministic deadlock.
// Token accounting uses the pure EstimateMessageTokens, which takes no locks.
// Both passes below stay inside the one write lock on purpose — scanning under
// one acquisition and rewriting under another is a TOCTOU window against the
// real concurrent writers (IPC handleCompact / gdb AppendMessage /
// fork_continue), which is also why there is no separate read-only scan method.
func (m *Manager) PruneToolResults(cid types.CtxID, opts PruneOpts) (*PruneResult, error) {
	ctx, err := m.getContext("PruneToolResults", cid)
	if err != nil {
		return nil, err
	}

	placeholder := opts.Placeholder
	if placeholder == "" {
		placeholder = DefaultPrunePlaceholder
	}

	result := &PruneResult{}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	// Pass 1 — collect candidates and total what they are worth. Runs even when
	// MinTokens is 0 so CandidateTokens is always reported honestly.
	coldEnd := ColdZoneEnd(len(ctx.Messages))
	var candidates []int
	for i := range coldEnd {
		msg := ctx.Messages[i]
		if !IsLeakedToolResult(msg) {
			continue
		}
		// Idempotence: exact equality, not HasPrefix — a genuine tool output
		// that happens to start with the placeholder text must still be pruned.
		if msg.Content == placeholder {
			continue
		}
		candidates = append(candidates, i)
		result.CandidateTokens += EstimateMessageTokens(msg)
	}

	// Admission gate: not worth a cache-prefix invalidation, so do not touch a
	// single byte. CandidateTokens still reports what was on the table.
	if opts.MinTokens > 0 && result.CandidateTokens < opts.MinTokens {
		return result, nil
	}

	// Pass 2 — rewrite.
	for _, i := range candidates {
		before := EstimateMessageTokens(ctx.Messages[i])
		ctx.Messages[i].Content = placeholder
		after := EstimateMessageTokens(ctx.Messages[i])

		result.Pruned++
		if freed := before - after; freed > 0 {
			result.TokensFreed += freed
		}

		if opts.TargetTokens > 0 && result.TokensFreed >= opts.TargetTokens {
			break
		}
	}

	// Explicit: in-place content rewrite cannot move the slot needle.
	result.SlotsFreed = 0
	return result, nil
}

// DropOpts configures DropOldestRounds.
type DropOpts struct {
	// NeedSlots is how many message slots the caller wants back. The sweep
	// stops as soon as it has freed that many (or has run out of droppable
	// rounds).
	NeedSlots int
	// KeepRounds is the number of most-recent API rounds to preserve. 0 = use
	// the built-in floor (see the implementation): at least the last round,
	// and never fewer than minMessagesForCompact messages in total.
	KeepRounds int
}

// DropResult reports what DropOldestRounds reclaimed.
type DropResult struct {
	DroppedMessages int
	DroppedRounds   int
	SlotsFreed      int
	TokensFreed     int
}

// DropOldestRounds mechanically reclaims message SLOTS by discarding the oldest
// API rounds whole. This is the only lawful shape of slot reclamation: slot
// usage is literally len(ctx.Messages), so nothing short of removing entries
// moves it, and removing a partial round (dropping a tool result while its
// assistant tool_use stays) breaks the pairing invariant that
// PruneToolResults exists to protect.
//
// Grouping reuses groupMessagesByAPIRound / flattenGroups so the round boundary
// definition stays single-sourced with the compact PTL retry path.
//
// GROUP SHAPE (matters for what "whole round" means here): grouping splits on
// RoleAssistant, so group 0 is whatever precedes the first assistant (typically
// a single leading user message) and every later group is
// assistant + its tool results + any trailing user messages. That is precisely
// why dropping leading groups is pairing-safe: an assistant message and the
// tool results answering its tool_calls always live in the SAME group, so they
// leave together. It also means the first drop may free just one slot (the
// stray user prefix) — that is a whole group, not a partial round.
//
// KNOWN DEGENERATION (deferred-work.md:305): immediately after a successful
// Compact every message is RoleUser (boundary marker, summary and restore
// entries all are), while grouping only splits on RoleAssistant — so the whole
// context collapses into ONE group and this primitive reclaims nothing. That is
// ACCEPTABLE and must not be "fixed" here: at that point tokens have just been
// compressed anyway, and slot pressure comes from not-yet-compacted history
// where roles are varied. Changing the grouping semantics would also perturb
// compactWithPTLRetry's retry behaviour.
//
// Insufficiency is reported, not raised: when SlotsFreed < NeedSlots the error
// is still nil. "Reclaimed what I could, still not enough" is a normal outcome
// the caller must decide on, and padding it out by breaking round boundaries is
// forbidden.
//
// Lock contract is identical to PruneToolResults: write lock held throughout,
// no Sections.Build(), no re-entrant Manager calls.
func (m *Manager) DropOldestRounds(cid types.CtxID, opts DropOpts) (*DropResult, error) {
	ctx, err := m.getContext("DropOldestRounds", cid)
	if err != nil {
		return nil, err
	}

	result := &DropResult{}
	if opts.NeedSlots <= 0 {
		return result, nil
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	groups := groupMessagesByAPIRound(ctx.Messages)
	if len(groups) <= 1 {
		// Nothing droppable: empty context, or a single round we must keep.
		return result, nil
	}

	keepRounds := max(opts.KeepRounds, 1)

	total := len(ctx.Messages)
	dropUpTo := 0 // number of leading groups to discard
	freed := 0
	tokensFreed := 0

	for k := 0; k < len(groups)-keepRounds; k++ {
		groupSize := len(groups[k])
		// Floor: never shrink below minMessagesForCompact messages.
		if total-freed-groupSize < minMessagesForCompact {
			break
		}
		for _, msg := range groups[k] {
			tokensFreed += EstimateMessageTokens(msg)
		}
		freed += groupSize
		dropUpTo = k + 1
		if freed >= opts.NeedSlots {
			break
		}
	}

	if dropUpTo == 0 {
		return result, nil
	}

	ctx.Messages = flattenGroups(groups[dropUpTo:])

	// Provider safety: Anthropic requires messages[0].role == "user". Dropping
	// group 0 (the user prefix before the first assistant) leaves the context
	// headed by RoleAssistant → HTTP 400. Prepend a minimal user marker so the
	// invariant holds regardless of which groups were dropped. The marker costs
	// one slot, so adjust the reported reclamation accordingly.
	if len(ctx.Messages) > 0 && ctx.Messages[0].Role != RoleUser {
		ctx.Messages = append([]Message{{Role: RoleUser, Content: "[earlier conversation history dropped to free context]"}}, ctx.Messages...)
		freed--
	}

	if ctx.Sections != nil {
		ctx.Sections.Invalidate()
	}

	result.DroppedRounds = dropUpTo
	result.DroppedMessages = freed
	result.SlotsFreed = freed
	result.TokensFreed = tokensFreed
	return result, nil
}

// String renders a PruneResult for log lines.
func (r *PruneResult) String() string {
	return fmt.Sprintf("pruned=%d tokens_freed=%d candidates=%d", r.Pruned, r.TokensFreed, r.CandidateTokens)
}

// String renders a DropResult for log lines.
func (r *DropResult) String() string {
	return fmt.Sprintf("dropped_rounds=%d dropped_messages=%d tokens_freed=%d", r.DroppedRounds, r.DroppedMessages, r.TokensFreed)
}
