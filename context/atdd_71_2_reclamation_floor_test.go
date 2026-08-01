package context

import (
	"strings"
	"testing"
)

// Story 71.2 — the context-package half: the minimum-release floor (AC1), the
// compaction request's input trimming (AC4) and the drop marker's token
// accounting (AC6-②).
//
// Fixtures reuse buildPruneFixture / bigToolResult / assertToolCallPairing /
// coldZoneCandidateTokens / assertMessageBytesEqual from the Story 69.3 and 69.4
// files on purpose — a second fixture would be a second place the cold-zone
// arithmetic has to stay correct.

// =============================================================================
// AC1 — ClearAtLeast: a floor on what is ACTUALLY released
// =============================================================================

// TestATDD_71_2_AC1_ClearAtLeastGateBothDirections asserts both halves in ONE
// test. Split apart, the "below the floor changes nothing" half would pass
// vacuously against an implementation that never rewrites anything at all — the
// trap Story 69.4's gate test documents. The admitted half is what gives the
// refused half teeth.
func TestATDD_71_2_AC1_ClearAtLeastGateBothDirections(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	before := snapshotMessages(t, m, cid)
	candidate, candidateCount := coldZoneCandidateTokens(before)
	if candidateCount == 0 {
		t.Fatalf("fixture precondition: cold zone holds no leaked tool results (coldEnd=%d, msgs=%d)",
			ColdZoneEnd(len(before)), len(before))
	}

	// --- half 1: an unreachable floor must not cost a single byte ---
	res, err := m.PruneToolResults(cid, PruneOpts{ClearAtLeast: candidate * 10})
	if err != nil {
		t.Fatalf("PruneToolResults (below floor): %v", err)
	}
	if res.Pruned != 0 {
		t.Errorf("below floor: Pruned = %d, want 0", res.Pruned)
	}
	if res.TokensFreed != 0 {
		t.Errorf("below floor: TokensFreed = %d, want 0", res.TokensFreed)
	}
	// Honest reporting while declining: an operator must be able to tell "there
	// was nothing here" from "there was plenty, just not enough".
	if res.ReleasableTokens <= 0 {
		t.Errorf("below floor: ReleasableTokens = %d, want > 0 (a declined batch must still report what it saw)",
			res.ReleasableTokens)
	}
	assertMessageBytesEqual(t, before, snapshotMessages(t, m, cid), "declined by ClearAtLeast")

	// --- half 2: a floor the batch can meet must go ahead ---
	releasable := res.ReleasableTokens
	res, err = m.PruneToolResults(cid, PruneOpts{ClearAtLeast: releasable})
	if err != nil {
		t.Fatalf("PruneToolResults (at floor): %v", err)
	}
	if res.Pruned != candidateCount {
		t.Errorf("at floor: Pruned = %d, want %d", res.Pruned, candidateCount)
	}
	if res.TokensFreed < releasable {
		t.Errorf("at floor: TokensFreed = %d, want >= the promised %d — the floor is a GUARANTEE, "+
			"so admitting a batch that then under-delivers is the F4 bug in a new place",
			res.TokensFreed, releasable)
	}
	assertToolCallPairing(t, snapshotMessages(t, m, cid))
}

// TestATDD_71_2_AC1_ReleasableIsStrictlyBelowCandidate pins the asymmetry that
// forced ClearAtLeast to be a separate field (story F4). A pruned entry keeps
// the placeholder and its ToolCallID, so what a batch RELEASES is always less
// than what its candidates WEIGH. Judging a release floor against
// CandidateTokens would therefore promise tokens it cannot deliver.
func TestATDD_71_2_AC1_ReleasableIsStrictlyBelowCandidate(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	res, err := m.PruneToolResults(cid, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if res.Pruned == 0 {
		t.Fatal("fixture precondition: nothing pruned, so there is no accounting to compare")
	}
	if res.ReleasableTokens >= res.CandidateTokens {
		t.Errorf("ReleasableTokens (%d) >= CandidateTokens (%d): the placeholder and ToolCallID "+
			"residue must make the release strictly smaller, otherwise ClearAtLeast could just "+
			"reuse MinTokens", res.ReleasableTokens, res.CandidateTokens)
	}
	// On an admitted batch with no TargetTokens early stop, the prediction must
	// match what actually happened — a predicted floor nobody honours is worse
	// than no floor.
	if res.ReleasableTokens != res.TokensFreed {
		t.Errorf("ReleasableTokens (%d) != TokensFreed (%d): the pre-flight prediction and the "+
			"realised reclamation must agree", res.ReleasableTokens, res.TokensFreed)
	}
}

// TestATDD_71_2_AC1_ZeroClearAtLeastMatchesPre712 is the regression red line, the
// twin of Story 69.4's MinTokens equivalent: PruneOpts{} must stay byte-for-byte
// the pre-71.2 behaviour, because reclaimForResume — the last hop before a
// process is killed — deliberately passes no floor.
func TestATDD_71_2_AC1_ZeroClearAtLeastMatchesPre712(t *testing.T) {
	zero := NewManager()
	cidZero := buildPruneFixture(t, zero, 20, 256)
	explicitMgr := NewManager()
	cidExplicit := buildPruneFixture(t, explicitMgr, 20, 256)

	legacy, err := zero.PruneToolResults(cidZero, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults{}: %v", err)
	}
	explicit, err := explicitMgr.PruneToolResults(cidExplicit, PruneOpts{ClearAtLeast: 0})
	if err != nil {
		t.Fatalf("PruneToolResults{ClearAtLeast:0}: %v", err)
	}

	if legacy.Pruned != explicit.Pruned || legacy.TokensFreed != explicit.TokensFreed {
		t.Errorf("zero ClearAtLeast diverged from the omitted field: %+v vs %+v", legacy, explicit)
	}
	if legacy.Pruned == 0 {
		t.Error("Pruned = 0: a zero floor must mean NO FLOOR, never 'apply a built-in default' — " +
			"that reading would disarm the last line of defence exactly when it is needed")
	}
	assertMessageBytesEqual(t,
		snapshotMessages(t, zero, cidZero),
		snapshotMessages(t, explicitMgr, cidExplicit),
		"zero vs omitted ClearAtLeast")
}

// TestATDD_71_2_AC1_MinTokensAndClearAtLeastAreIndependent proves the two gates
// measure different quantities rather than one shadowing the other: a floor set
// between them admits under MinTokens' reading and refuses under
// ClearAtLeast's.
func TestATDD_71_2_AC1_MinTokensAndClearAtLeastAreIndependent(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	before := snapshotMessages(t, m, cid)
	probe, err := NewManager(), error(nil)
	cidProbe := buildPruneFixture(t, probe, 20, 256)
	scan, err := probe.PruneToolResults(cidProbe, PruneOpts{ClearAtLeast: 1 << 30}) // declines, but reports
	if err != nil {
		t.Fatalf("probe scan: %v", err)
	}
	if scan.CandidateTokens <= scan.ReleasableTokens {
		t.Fatalf("fixture precondition: need CandidateTokens (%d) > ReleasableTokens (%d) for a gap to sit in",
			scan.CandidateTokens, scan.ReleasableTokens)
	}

	// A demand inside the gap: satisfiable as a candidate WEIGHT, unsatisfiable
	// as an actual RELEASE.
	inGap := scan.ReleasableTokens + 1

	// MinTokens reading: candidates weigh more than this, so it admits.
	admitted, err := m.PruneToolResults(cid, PruneOpts{MinTokens: inGap})
	if err != nil {
		t.Fatalf("PruneToolResults(MinTokens in gap): %v", err)
	}
	if admitted.Pruned == 0 {
		t.Errorf("MinTokens=%d refused, but candidates weigh %d — the gate must judge WEIGHT",
			inGap, scan.CandidateTokens)
	}

	// ClearAtLeast reading, same number, fresh context: it must refuse, because
	// the release cannot reach it.
	strict := NewManager()
	cidStrict := buildPruneFixture(t, strict, 20, 256)
	beforeStrict := snapshotMessages(t, strict, cidStrict)
	refused, err := strict.PruneToolResults(cidStrict, PruneOpts{ClearAtLeast: inGap})
	if err != nil {
		t.Fatalf("PruneToolResults(ClearAtLeast in gap): %v", err)
	}
	if refused.Pruned != 0 {
		t.Errorf("ClearAtLeast=%d admitted a batch that can only release %d — a release floor that "+
			"under-delivers is exactly the F4 defect", inGap, refused.ReleasableTokens)
	}
	assertMessageBytesEqual(t, beforeStrict, snapshotMessages(t, strict, cidStrict), "refused by ClearAtLeast in gap")
	_ = before
}

// =============================================================================
// AC4 — the compaction REQUEST is trimmed; the stored context is not
// =============================================================================

// TestATDD_71_2_AC4_CompactRequestTrimsToolResults is the story's mandatory new
// coverage (🔴): every one of context/compact_test.go's 18 cases mocks LLMCall
// while asserting only the system prompt and the call count, so the request BODY
// has never been observed. Without this the trimming could be deleted wholesale
// and the suite would stay green.
func TestATDD_71_2_AC4_CompactRequestTrimsToolResults(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 8, 256)

	before := snapshotMessages(t, m, cid)
	bigPayload := bigToolResult()

	var seen []Message
	llm := func(_ string, messages []Message) (string, error) {
		seen = make([]Message, len(messages))
		copy(seen, messages)
		return "<summary>ok</summary>", nil
	}

	if _, err := m.Compact(cid, CompactOpts{LLMCall: llm, Trigger: "manual"}); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("LLMCall never observed the request")
	}

	trimmed := 0
	for i, msg := range seen {
		if msg.Role != RoleTool {
			continue
		}
		if msg.Content == bigPayload {
			t.Errorf("request msg[%d]: full %d-byte tool payload reached the summariser untrimmed",
				i, len(msg.Content))
			continue
		}
		if EstimateTokens(msg.Content) > compactToolResultTokenLimit+EstimateTokens(
			FormatTruncationNotice(0, compactToolResultTokenLimit, "")) {
			t.Errorf("request msg[%d]: %d tokens exceeds the %d-token cap plus its notice",
				i, EstimateTokens(msg.Content), compactToolResultTokenLimit)
		}
		if !strings.Contains(msg.Content, "Truncated") {
			t.Errorf("request msg[%d]: trimmed without a notice — an investigator reading a captured "+
				"request could not tell a trimmed payload from a short one", i)
		}
		trimmed++
	}
	if trimmed == 0 {
		t.Fatal("no tool result was trimmed: fixture must carry oversized payloads or this proves nothing")
	}

	// The trim must not have reached the stored context. Compact replaces the
	// history wholesale, so the check is on the snapshot taken beforehand: those
	// Message values share their backing array with what the trim walked.
	for i := range before {
		if before[i].Role == RoleTool && before[i].Content != bigPayload {
			t.Fatalf("stored msg[%d] was mutated: the trim must act on the request copy only, "+
				"never on ctx.Messages", i)
		}
	}
}

// TestATDD_71_2_AC4_TrimSkipsPrunePlaceholder pins the layering against Story
// 69.4: the persistent erasure wins, and re-truncating its 37-byte marker would
// bolt a misleading "original N tokens" notice onto content this layer never saw.
func TestATDD_71_2_AC4_TrimSkipsPrunePlaceholder(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, Content: "running", ToolCalls: []ToolCall{{ID: "a", Name: "Bash"}}},
		{Role: RoleTool, Content: DefaultPrunePlaceholder, ToolCallID: "a"},
		{Role: RoleTool, Content: bigToolResult(), ToolCallID: "b"},
	}

	shrinkToolResultsForCompact(messages)

	if messages[2].Content != DefaultPrunePlaceholder {
		t.Errorf("already-pruned entry was rewritten to %q — Story 69.4's persistent layer must win",
			messages[2].Content)
	}
	if messages[3].Content == bigToolResult() {
		t.Error("oversized entry untouched: the skip must be narrow, not a blanket bypass")
	}
}

// TestATDD_71_2_AC4_TrimNeverTouchesSharedSlices is the red line from
// context.go:644. The request snapshot is SHALLOW: ToolCalls and
// ReasoningBlocks are slice headers sharing a backing array with ctx.Messages,
// so writing through either corrupts the live context after RUnlock, from
// another goroutine.
func TestATDD_71_2_AC4_TrimNeverTouchesSharedSlices(t *testing.T) {
	bigInput := map[string]any{"script": strings.Repeat("echo hello; ", 500)}
	shared := []ToolCall{{ID: "call-1", Name: "Bash", Input: bigInput}}
	blocks := []ReasoningBlock{{Type: "thinking", Thinking: strings.Repeat("deliberating ", 500)}}

	messages := []Message{
		{Role: RoleAssistant, Content: "running", ToolCalls: shared, ReasoningBlocks: blocks},
		{Role: RoleTool, Content: bigToolResult(), ToolCallID: "call-1"},
	}
	wantInput := strings.Repeat("echo hello; ", 500)
	wantThinking := strings.Repeat("deliberating ", 500)

	shrinkToolResultsForCompact(messages)

	if got := shared[0].Input["script"]; got != wantInput {
		t.Errorf("ToolCalls[0].Input was mutated through the shallow snapshot — this silently corrupts "+
			"the real context (context.go:644 red line); got %d bytes, want %d",
			len(got.(string)), len(wantInput))
	}
	if blocks[0].Thinking != wantThinking {
		t.Error("ReasoningBlocks[0].Thinking was mutated through the shallow snapshot")
	}
	if messages[1].Content == bigToolResult() {
		t.Error("the tool result was not trimmed, so this test proves nothing about what was spared")
	}
}

// TestATDD_71_2_AC4_TrimLimitPinned guards the constant. A behavioural threshold
// with no test is a number anyone may quietly change (Story 69.4 precedent).
func TestATDD_71_2_AC4_TrimLimitPinned(t *testing.T) {
	if compactToolResultTokenLimit != 600 {
		t.Errorf("compactToolResultTokenLimit = %d, want 600", compactToolResultTokenLimit)
	}
	// It must stay above LeakedThreshold's token equivalent, or the trim and the
	// prune primitive would disagree about which payloads count as large.
	if leakedTokens := EstimateTokens(strings.Repeat("x", LeakedThreshold)); compactToolResultTokenLimit <= leakedTokens {
		t.Errorf("trim cap (%d tokens) must exceed LeakedThreshold's ~%d tokens, otherwise an entry too "+
			"small to be prunable would still be trimmed", compactToolResultTokenLimit, leakedTokens)
	}
}

// TestATDD_71_2_AC4_PreTokensReportsUntrimmedSize: the trim must not be able to
// flatter the compaction's own report. PreTokens is read off ctx before the
// trim, so it keeps describing the real context.
func TestATDD_71_2_AC4_PreTokensReportsUntrimmedSize(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 8, 256)

	msgs := snapshotMessages(t, m, cid)
	want := 0
	for _, msg := range msgs {
		want += EstimateMessageTokens(msg)
	}

	res, err := m.Compact(cid, CompactOpts{
		LLMCall: func(string, []Message) (string, error) { return "<summary>ok</summary>", nil },
	})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.PreTokens != want {
		t.Errorf("PreTokens = %d, want %d (the untrimmed size). Reporting the trimmed request size "+
			"would understate the compaction's own achievement", res.PreTokens, want)
	}
}

// =============================================================================
// AC6-② — the drop marker is no longer free
// =============================================================================

// TestATDD_71_2_AC6_DropMarkerCostIsAccounted covers the exact net-negative
// shape the story names: groups[0] is a lone leading user message, NeedSlots is
// 1, and groups[1] opens with an assistant. One slot leaves, the marker arrives,
// and the old accounting reported a reclamation while the context had GROWN.
func TestATDD_71_2_AC6_DropMarkerCostIsAccounted(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 10, 64)

	before := snapshotMessages(t, m, cid)
	beforeTokens := 0
	for _, msg := range before {
		beforeTokens += EstimateMessageTokens(msg)
	}

	res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: 1})
	if err != nil {
		t.Fatalf("DropOldestRounds: %v", err)
	}

	after := snapshotMessages(t, m, cid)
	afterTokens := 0
	for _, msg := range after {
		afterTokens += EstimateMessageTokens(msg)
	}

	// The reported reclamation must equal the measured one, sign included.
	if want := beforeTokens - afterTokens; res.TokensFreed != want {
		t.Errorf("TokensFreed = %d, but the context actually changed by %d tokens (%d → %d). "+
			"The marker costs real tokens and must be charged against the report — a figure whose "+
			"SIGN disagrees with reality is worse than no figure",
			res.TokensFreed, want, beforeTokens, afterTokens)
	}
	if res.SlotsFreed != len(before)-len(after) {
		t.Errorf("SlotsFreed = %d, but message count moved %d → %d",
			res.SlotsFreed, len(before), len(after))
	}
	assertToolCallPairing(t, after)
}

// TestATDD_71_2_AC6_DropReportsNetTokensAcrossNeedSlots sweeps the whole range
// so no single NeedSlots value can hide a mis-charged marker.
func TestATDD_71_2_AC6_DropReportsNetTokensAcrossNeedSlots(t *testing.T) {
	for need := 1; need <= 12; need++ {
		m := NewManager()
		cid := buildPruneFixture(t, m, 10, 64)

		before := snapshotMessages(t, m, cid)
		beforeTokens := 0
		for _, msg := range before {
			beforeTokens += EstimateMessageTokens(msg)
		}

		res, err := m.DropOldestRounds(cid, DropOpts{NeedSlots: need})
		if err != nil {
			t.Fatalf("need=%d: DropOldestRounds: %v", need, err)
		}

		after := snapshotMessages(t, m, cid)
		afterTokens := 0
		for _, msg := range after {
			afterTokens += EstimateMessageTokens(msg)
		}
		if want := beforeTokens - afterTokens; res.TokensFreed != want {
			t.Errorf("need=%d: TokensFreed = %d, want %d (net of the marker)", need, res.TokensFreed, want)
		}
		if want := len(before) - len(after); res.SlotsFreed != want {
			t.Errorf("need=%d: SlotsFreed = %d, want %d", need, res.SlotsFreed, want)
		}
	}
}
