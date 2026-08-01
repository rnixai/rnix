package context

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// Story 69.3 — mechanical reclamation primitives (AC1 PruneToolResults token
// axis / AC2 DropOldestRounds slot axis).
//
// assertToolCallPairing is the most important guard in this story (AC9 ④): for
// every assistant message carrying tool_calls, each tc.ID must still have a
// matching `tool` entry afterwards. Either primitive breaking this produces a
// protocol-illegal context that anthropic rejects with HTTP 400 (its driver
// builds tool_result blocks straight from ToolCallID with no repair path).
func assertToolCallPairing(t *testing.T, msgs []Message) {
	t.Helper()
	present := make(map[string]bool)
	for _, msg := range msgs {
		if msg.Role == RoleTool && msg.ToolCallID != "" {
			present[msg.ToolCallID] = true
		}
	}
	for i, msg := range msgs {
		if msg.Role != RoleAssistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if !present[tc.ID] {
				t.Fatalf("pairing broken: assistant msg[%d] has tool_call id=%q with no matching tool result entry", i, tc.ID)
			}
		}
	}
}

// bigToolResult returns a tool payload long enough to be classified as leaked.
func bigToolResult() string {
	return strings.Repeat("payload ", 300) // 2400 bytes > LeakedThreshold
}

// buildPruneFixture creates a context of n API rounds. Each round is
// user → assistant(tool_calls) → tool(big result), so the pairing invariant is
// exercised and cold-zone tool results are leaked-classified.
func buildPruneFixture(t *testing.T, m *Manager, rounds int, maxSize int) types.CtxID {
	t.Helper()
	cid, err := m.CtxAlloc(maxSize)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	ctx, err := m.GetContext(cid)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	// Write directly: AppendAssistantWithToolCalls enforces atomic admission and
	// we deliberately want a densely-packed fixture.
	ctx.mu.Lock()
	for i := range rounds {
		id := "call-" + string(rune('a'+i%26)) + strings.Repeat("z", i/26)
		ctx.Messages = append(ctx.Messages,
			Message{Role: RoleUser, Content: "please run step"},
			Message{Role: RoleAssistant, Content: "running", ToolCalls: []ToolCall{{ID: id, Name: "Bash"}}},
			Message{Role: RoleTool, Content: bigToolResult(), ToolCallID: id},
		)
	}
	ctx.mu.Unlock()
	return cid
}

func snapshotMessages(t *testing.T, m *Manager, cid types.CtxID) []Message {
	t.Helper()
	ctx, err := m.GetContext(cid)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	out := make([]Message, len(ctx.Messages))
	copy(out, ctx.Messages)
	return out
}

// --- AC1: PruneToolResults ---

func TestPruneToolResults_FreesTokensAndKeepsEntries(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	before := snapshotMessages(t, m, cid)
	beforeTokens, err := m.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}

	res, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}

	if res.Pruned == 0 {
		t.Fatal("expected at least one pruned tool result in the cold zone, got 0")
	}
	if res.TokensFreed <= 0 {
		t.Fatalf("TokensFreed = %d, want > 0", res.TokensFreed)
	}
	// AC1: SlotsFreed is a design-fact zero.
	if res.SlotsFreed != 0 {
		t.Errorf("SlotsFreed = %d, want 0 (in-place rewrite cannot free slots)", res.SlotsFreed)
	}

	after := snapshotMessages(t, m, cid)

	// Entry count unchanged — only Content was rewritten.
	if len(after) != len(before) {
		t.Fatalf("message count changed: %d → %d (prune must preserve entries)", len(before), len(after))
	}

	afterTokens, err := m.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if afterTokens.Used >= beforeTokens.Used {
		t.Errorf("tokens did not drop: %d → %d", beforeTokens.Used, afterTokens.Used)
	}

	// Cold-zone leaked entries now hold the placeholder; Role / ToolCallID intact.
	coldEnd := ColdZoneEnd(len(after))
	placeholders := 0
	for i := range coldEnd {
		if after[i].Content != DefaultPrunePlaceholder {
			continue
		}
		placeholders++
		if after[i].Role != RoleTool {
			t.Errorf("msg[%d]: Role = %q, want %q", i, after[i].Role, RoleTool)
		}
		if after[i].ToolCallID != before[i].ToolCallID {
			t.Errorf("msg[%d]: ToolCallID changed %q → %q", i, before[i].ToolCallID, after[i].ToolCallID)
		}
	}
	if placeholders != res.Pruned {
		t.Errorf("placeholder count = %d, want %d (== Pruned)", placeholders, res.Pruned)
	}

	// Warm / active zone untouched.
	for i := coldEnd; i < len(after); i++ {
		if after[i].Content != before[i].Content {
			t.Errorf("msg[%d] outside cold zone was modified", i)
		}
	}

	assertToolCallPairing(t, after)
}

func TestPruneToolResults_Idempotent(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	first, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("first PruneToolResults: %v", err)
	}
	// Guard against a vacuous pass: the second call may only be zero because
	// the first one actually did work.
	if first.Pruned == 0 || first.TokensFreed == 0 {
		t.Fatalf("first pass must reclaim something, got %+v", first)
	}

	second, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("second PruneToolResults: %v", err)
	}
	if second.Pruned != 0 {
		t.Errorf("second pass Pruned = %d, want 0 (idempotent)", second.Pruned)
	}
	if second.TokensFreed != 0 {
		t.Errorf("second pass TokensFreed = %d, want 0 (idempotent)", second.TokensFreed)
	}

	assertToolCallPairing(t, snapshotMessages(t, m, cid))
}

func TestPruneToolResults_TargetTokensStopsEarly(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 30, 256)

	full, err := m.PruneToolResults(cid, PruneOpts{TargetTokens: 1})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if full.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1 (TargetTokens=1 satisfied by first entry)", full.Pruned)
	}
}

func TestPruneToolResults_SkipsShortAndNonToolMessages(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(256)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	ctx, _ := m.GetContext(cid)
	ctx.mu.Lock()
	for range 30 {
		ctx.Messages = append(ctx.Messages,
			Message{Role: RoleUser, Content: strings.Repeat("u", 5000)},
			Message{Role: RoleAssistant, Content: strings.Repeat("a", 5000)},
			Message{Role: RoleTool, Content: "short ok", ToolCallID: "t"},
		)
	}
	ctx.mu.Unlock()

	res, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if res.Pruned != 0 {
		t.Errorf("Pruned = %d, want 0 (no leaked tool results present)", res.Pruned)
	}
}

func TestPruneToolResults_EmptyContext(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(256)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	res, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults on empty context: %v", err)
	}
	if res.Pruned != 0 || res.TokensFreed != 0 {
		t.Errorf("expected zero reclamation on empty context, got %+v", res)
	}
}

func TestPruneToolResults_CustomPlaceholder(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	const custom = "[cleared by test]"
	res, err := m.PruneToolResults(cid, PruneOpts{Placeholder: custom})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if res.Pruned == 0 {
		t.Fatal("expected pruning to happen")
	}
	found := false
	for _, msg := range snapshotMessages(t, m, cid) {
		if msg.Content == custom {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom placeholder not applied")
	}
}

func TestPruneToolResults_UnknownContext(t *testing.T) {
	m := NewManager()
	if _, err := m.PruneToolResults(types.CtxID(9999), PruneOpts{}); err == nil {
		t.Error("expected error for unknown CtxID")
	}
}

// --- AC2: DropOldestRounds ---

func TestDropOldestRounds_FreesSlots(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 64)

	beforeUsed, _, err := m.SlotUsage(cid)
	if err != nil {
		t.Fatalf("SlotUsage: %v", err)
	}

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 6})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.SlotsFreed < 6 {
		t.Fatalf("SlotsFreed = %d, want >= 6", res.SlotsFreed)
	}
	if res.DroppedRounds == 0 {
		t.Error("DroppedRounds = 0, want > 0")
	}
	if res.TokensFreed <= 0 {
		t.Errorf("TokensFreed = %d, want > 0", res.TokensFreed)
	}
	if res.DroppedMessages != res.SlotsFreed {
		t.Errorf("DroppedMessages (%d) != SlotsFreed (%d)", res.DroppedMessages, res.SlotsFreed)
	}

	afterUsed, _, _ := m.SlotUsage(cid)
	if afterUsed != beforeUsed-res.SlotsFreed {
		t.Errorf("slot usage %d → %d, expected drop of %d", beforeUsed, afterUsed, res.SlotsFreed)
	}

	// Whole rounds only: pairing must survive.
	assertToolCallPairing(t, snapshotMessages(t, m, cid))
}

func TestDropOldestRounds_DropsWholeGroupsOnly(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 10, 64)

	// Ask for 1 slot. groupMessagesByAPIRound splits on RoleAssistant, so
	// group 0 is the leading user message alone and later groups are
	// assistant + its tool results. Whatever gets dropped must be a whole
	// group — never a shaved-off piece of one.
	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 1})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.DroppedRounds != 1 {
		t.Errorf("DroppedRounds = %d, want 1", res.DroppedRounds)
	}

	// Story 71.2 AC6-②: this exact input is the net-negative shape. group 0 is a
	// lone leading user message, so dropping it frees one slot — and the marker
	// prepended to keep messages[0].role == "user" immediately takes it back.
	// SlotsFreed must therefore be 0, not 1. Reporting "1 round dropped" without
	// this assertion let the case look like a reclamation while the context had
	// actually grown by the marker's 16 tokens.
	if res.SlotsFreed != 0 {
		t.Errorf("SlotsFreed = %d, want 0: the one slot freed by dropping group 0 is spent again on "+
			"the leading-user marker, so this drop nets nothing", res.SlotsFreed)
	}
	if res.TokensFreed >= 0 {
		t.Errorf("TokensFreed = %d, want negative: the marker (%d tokens) costs more than the short "+
			"user message it replaced, and the report must not hide that",
			res.TokensFreed, EstimateTokens(droppedHistoryMarker))
	}

	after := snapshotMessages(t, m, cid)
	assertToolCallPairing(t, after)

	// An assistant may now lead the context (its tool results follow it), but a
	// LEADING tool result would mean its assistant was dropped without it.
	if len(after) > 0 && after[0].Role == RoleTool {
		t.Error("context now starts with an orphan tool result — a partial group was dropped")
	}
}

// TestDropOldestRounds_NeverOrphansToolResults sweeps every NeedSlots value and
// asserts the invariant that matters most: no dropped assistant may leave its
// tool results behind, and no surviving assistant may lose its tool results.
func TestDropOldestRounds_NeverOrphansToolResults(t *testing.T) {
	for need := 1; need <= 30; need++ {
		m := NewManager()
		cid := buildPruneFixture(t, m, 10, 64)

		if _, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: need}); err != nil {
			t.Fatalf("need=%d: DropOldestRounds: %v", need, err)
		}
		after := snapshotMessages(t, m, cid)
		assertToolCallPairing(t, after)

		if len(after) > 0 && after[0].Role == RoleTool {
			t.Errorf("need=%d: context leads with an orphan tool result", need)
		}
	}
}

func TestDropOldestRounds_InsufficientReturnsNoError(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 3, 64)

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 1000})
	if err != nil {
		t.Fatalf("insufficient reclamation must not be an error, got: %v", err)
	}
	if res.SlotsFreed >= 1000 {
		t.Fatalf("SlotsFreed = %d — fixture cannot possibly yield that", res.SlotsFreed)
	}

	after := snapshotMessages(t, m, cid)
	if len(after) < minMessagesForCompact {
		t.Errorf("remaining messages = %d, must not fall below minMessagesForCompact (%d)", len(after), minMessagesForCompact)
	}
	assertToolCallPairing(t, after)
}

func TestDropOldestRounds_KeepsLastRound(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 5, 64)

	if _, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 1000}); err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	after := snapshotMessages(t, m, cid)
	if len(after) == 0 {
		t.Fatal("context was emptied — losing the current task state is worse than suspending")
	}
	// The surviving tail must be the newest round.
	last := after[len(after)-1]
	if last.Role != RoleTool {
		t.Errorf("tail role = %q, want the newest round's tool result", last.Role)
	}
}

func TestDropOldestRounds_KeepRoundsHonoured(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 10, 64)

	// The fixture's 10 user/assistant/tool rounds yield 11 groups: a leading
	// user-only group plus 10 assistant-anchored ones. KeepRounds=4 must leave
	// exactly the newest 4 groups standing.
	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 1000, KeepRounds: 4})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.DroppedRounds != 7 {
		t.Errorf("DroppedRounds = %d, want 7 (11 groups, keep 4)", res.DroppedRounds)
	}
	after := snapshotMessages(t, m, cid)
	// 4 surviving groups (2+3+3+3 = 11 messages) plus the P1 user marker
	// prepended because the first surviving group starts with RoleAssistant.
	if len(after) != 12 {
		t.Errorf("remaining messages = %d, want 12 (4 groups + user marker)", len(after))
	}
	if after[0].Role != RoleUser {
		t.Errorf("first message role = %q, want user (provider-safety marker)", after[0].Role)
	}
	assertToolCallPairing(t, after)
}

func TestDropOldestRounds_ZeroNeedIsNoop(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 10, 64)
	before := snapshotMessages(t, m, cid)

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 0})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.SlotsFreed != 0 || res.DroppedRounds != 0 {
		t.Errorf("NeedSlots=0 must be a no-op, got %+v", res)
	}
	if len(snapshotMessages(t, m, cid)) != len(before) {
		t.Error("messages changed on a no-op call")
	}
}

func TestDropOldestRounds_EmptyAndSingleRound(t *testing.T) {
	m := NewManager()

	empty, err := m.CtxAlloc(64)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	res, err := m.DropOldestRounds(empty, DropOpts{NeedSlots: 5})
	if err != nil {
		t.Fatalf("empty context must not error: %v", err)
	}
	if res.SlotsFreed != 0 {
		t.Errorf("SlotsFreed = %d on empty context, want 0", res.SlotsFreed)
	}

	// A one-round fixture is user + assistant + tool = 2 groups. The floor
	// (keep the newest group, never go below minMessagesForCompact) allows the
	// leading user-only group to go, and nothing more.
	single := buildPruneFixture(t, m, 1, 64)
	if _, err := m.DropOldestRounds(single, DropOpts{NeedSlots: 5}); err != nil {
		t.Fatalf("single-round context must not error: %v", err)
	}
	after := snapshotMessages(t, m, single)
	if len(after) < minMessagesForCompact {
		t.Errorf("remaining messages = %d, must not fall below minMessagesForCompact (%d)", len(after), minMessagesForCompact)
	}
	if after[len(after)-1].Role != RoleTool || after[1].Role != RoleAssistant {
		t.Errorf("newest group must survive intact after user marker, got roles %q…%q", after[1].Role, after[len(after)-1].Role)
	}
	assertToolCallPairing(t, after)
}

// TestDropOldestRounds_TrueSingleGroupIsNoop covers the genuinely
// indivisible case: one assistant-anchored group and nothing before it.
func TestDropOldestRounds_TrueSingleGroupIsNoop(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(64)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	ctx, _ := m.GetContext(cid)
	ctx.mu.Lock()
	ctx.Messages = append(ctx.Messages,
		Message{Role: RoleAssistant, Content: "running", ToolCalls: []ToolCall{{ID: "only", Name: "Bash"}}},
		Message{Role: RoleTool, Content: bigToolResult(), ToolCallID: "only"},
	)
	ctx.mu.Unlock()

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 5})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.SlotsFreed != 0 {
		t.Errorf("SlotsFreed = %d, want 0 (single group is indivisible)", res.SlotsFreed)
	}
	if len(snapshotMessages(t, m, cid)) != 2 {
		t.Error("the only group must be preserved")
	}
}

// TestDropOldestRounds_AllUserDegeneration documents F4: after a successful
// Compact every message is RoleUser, grouping collapses into one group and this
// primitive reclaims nothing. This is the accepted behaviour, asserted so a
// future reader does not "fix" the grouping algorithm (which would perturb
// compactWithPTLRetry).
func TestDropOldestRounds_AllUserDegeneration(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(64)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	ctx, _ := m.GetContext(cid)
	ctx.mu.Lock()
	for range 20 {
		ctx.Messages = append(ctx.Messages, Message{Role: RoleUser, Content: "post-compact restore entry"})
	}
	ctx.mu.Unlock()

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 10})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}
	if res.SlotsFreed != 0 {
		t.Errorf("SlotsFreed = %d — F4 documents zero reclamation on all-user contexts", res.SlotsFreed)
	}
}

func TestDropOldestRounds_UnknownContext(t *testing.T) {
	m := NewManager()
	if _, err := m.DropOldestRounds(types.CtxID(9999), DropOpts{NeedSlots: 1}); err == nil {
		t.Error("expected error for unknown CtxID")
	}
}

// --- AC8 red line: Compact semantics unchanged by the new primitives ---

func TestPrimitives_DoNotTouchCompactSemantics(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 10, 64)

	if _, err := m.PruneToolResults(cid, PruneOpts{}); err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if _, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 3}); err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}

	// Compact still performs its wholesale replacement afterwards.
	res, err := m.Compact(cid, CompactOpts{
		Trigger: "manual",
		LLMCall: func(string, []Message) (string, error) {
			return "<summary>done</summary>", nil
		},
	})
	if err != nil {
		t.Fatalf("Compact after primitives: %v", err)
	}
	after := snapshotMessages(t, m, cid)
	if len(after) != 2 {
		t.Errorf("post-compact messages = %d, want 2 (boundary + summary)", len(after))
	}
	if res.PostTokens <= 0 {
		t.Errorf("PostTokens = %d, want > 0", res.PostTokens)
	}
}
