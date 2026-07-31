package context

import (
	"strings"
	"testing"
)

// Story 69.4 — the yield gate inside PruneToolResults (AC2).
//
// Why the gate lives in the primitive rather than in a read-only ScanLeaked
// helper the kernel could call first (story decision 3): concurrent writers to
// the same context genuinely exist (ipc/server_observe.go handleCompact,
// server_gdb.go AppendMessage, server_record.go fork_continue), so scanning
// under one lock and rewriting under another is a TOCTOU window. One method,
// one lock acquisition, one atomic decision.
//
// The fixtures reuse buildPruneFixture / bigToolResult / assertToolCallPairing
// from atdd_69_3_prune_test.go on purpose — a second fixture would be a second
// place where the cold-zone arithmetic has to stay correct.

// assertMessageBytesEqual is Story 69.4's central guard in its context-package
// form: it proves a call left the message payloads byte-identical. The kernel
// side (AC7) uses the same idea to prove that below the watermark the proactive
// reclamation never touches the prompt cache prefix.
func assertMessageBytesEqual(t *testing.T, want, got []Message, label string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: message count %d → %d", label, len(want), len(got))
	}
	for i := range want {
		if want[i].Content != got[i].Content {
			t.Fatalf("%s: msg[%d] content changed (%d → %d bytes)", label, i, len(want[i].Content), len(got[i].Content))
		}
		if want[i].Role != got[i].Role || want[i].ToolCallID != got[i].ToolCallID {
			t.Fatalf("%s: msg[%d] role/tool_call_id changed", label, i)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// coldZoneCandidateTokens recomputes, independently of the implementation, how
// many tokens the cold zone currently offers up for reclamation. Used as the
// oracle for CandidateTokens.
func coldZoneCandidateTokens(msgs []Message) (tokens int, count int) {
	coldEnd := ColdZoneEnd(len(msgs))
	for i := range coldEnd {
		if !IsLeakedToolResult(msgs[i]) {
			continue
		}
		if msgs[i].Content == DefaultPrunePlaceholder {
			continue
		}
		tokens += EstimateMessageTokens(msgs[i])
		count++
	}
	return tokens, count
}

// TestPruneToolResults_MinTokensGate asserts BOTH directions in one test case.
//
// ⚠️ Splitting these into two tests would make the "below threshold rewrites
// nothing" half pass vacuously against a stub that never rewrites anything at
// all — the exact trap Story 69.3's idempotence case documented. The
// above-threshold assertion that follows is what gives the first half teeth.
func TestPruneToolResults_MinTokensGate(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 20, 256)

	before := snapshotMessages(t, m, cid)
	candidate, candidateCount := coldZoneCandidateTokens(before)
	if candidateCount == 0 {
		t.Fatalf("fixture precondition: cold zone holds no leaked tool results (coldEnd=%d, msgs=%d)",
			ColdZoneEnd(len(before)), len(before))
	}

	// --- half 1: below the gate, not one byte may change ---
	res, err := m.PruneToolResults(cid, PruneOpts{MinTokens: candidate + 1})
	if err != nil {
		t.Fatalf("PruneToolResults (below gate): %v", err)
	}
	if res.Pruned != 0 {
		t.Errorf("below gate: Pruned = %d, want 0", res.Pruned)
	}
	if res.TokensFreed != 0 {
		t.Errorf("below gate: TokensFreed = %d, want 0", res.TokensFreed)
	}
	if res.CandidateTokens != candidate {
		t.Errorf("below gate: CandidateTokens = %d, want %d (must report what it saw even when it declined)",
			res.CandidateTokens, candidate)
	}
	afterDeclined := snapshotMessages(t, m, cid)
	assertMessageBytesEqual(t, before, afterDeclined, "declined prune")

	// --- half 2: at the gate, the rewrite must actually happen ---
	res, err = m.PruneToolResults(cid, PruneOpts{MinTokens: candidate})
	if err != nil {
		t.Fatalf("PruneToolResults (at gate): %v", err)
	}
	if res.Pruned != candidateCount {
		t.Errorf("at gate: Pruned = %d, want %d", res.Pruned, candidateCount)
	}
	if res.TokensFreed <= 0 {
		t.Errorf("at gate: TokensFreed = %d, want > 0", res.TokensFreed)
	}
	if res.CandidateTokens != candidate {
		t.Errorf("at gate: CandidateTokens = %d, want %d", res.CandidateTokens, candidate)
	}

	assertToolCallPairing(t, snapshotMessages(t, m, cid))
}

// TestPruneToolResults_MinTokensZeroMatches693 pins the regression red line
// (AC8): the three Story 69.3 fallback paths all pass rnixctx.PruneOpts{}, so a
// zero MinTokens must mean "no gate", never "apply a built-in default gate".
// Reading zero as a default would make the mechanical fallback refuse to
// reclaim at the exact moment it is the last line of defence — the same class
// of one-line production failure as Story 69.3's CompactTimeout==0 trap.
func TestPruneToolResults_MinTokensZeroMatches693(t *testing.T) {
	withGate := NewManager()
	cidGate := buildPruneFixture(t, withGate, 20, 256)
	zeroOpts := NewManager()
	cidZero := buildPruneFixture(t, zeroOpts, 20, 256)

	// Story 69.3's exact call shape.
	legacy, err := zeroOpts.PruneToolResults(cidZero, PruneOpts{})
	if err != nil {
		t.Fatalf("PruneToolResults{}: %v", err)
	}
	explicit, err := withGate.PruneToolResults(cidGate, PruneOpts{MinTokens: 0})
	if err != nil {
		t.Fatalf("PruneToolResults{MinTokens:0}: %v", err)
	}

	if legacy.Pruned == 0 {
		t.Fatal("PruneOpts{} reclaimed nothing — the Story 69.3 fallback paths would be dead")
	}
	if legacy.Pruned != explicit.Pruned || legacy.TokensFreed != explicit.TokensFreed {
		t.Errorf("MinTokens:0 diverged from PruneOpts{}: %+v vs %+v", explicit, legacy)
	}

	// And the resulting message bytes must match too, not just the counters.
	assertMessageBytesEqual(t,
		snapshotMessages(t, zeroOpts, cidZero),
		snapshotMessages(t, withGate, cidGate),
		"MinTokens:0 vs PruneOpts{}")

	assertToolCallPairing(t, snapshotMessages(t, zeroOpts, cidZero))
}

// TestPruneToolResults_CandidateTokensDistinguishesEmptyFromDeclined covers the
// observability reason CandidateTokens exists: "there was nothing leaked" and
// "there was plenty but it did not clear the bar" both report Pruned == 0, and
// an operator reading the event must be able to tell them apart.
func TestPruneToolResults_CandidateTokensDistinguishesEmptyFromDeclined(t *testing.T) {
	// (a) genuinely nothing to reclaim
	empty := NewManager()
	cidEmpty, err := empty.CtxAlloc(256)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	res, err := empty.PruneToolResults(cidEmpty, PruneOpts{MinTokens: 1000})
	if err != nil {
		t.Fatalf("PruneToolResults on empty context: %v", err)
	}
	if res.Pruned != 0 || res.CandidateTokens != 0 {
		t.Errorf("empty context: got %+v, want Pruned=0 CandidateTokens=0", res)
	}

	// (b) plenty available, gate declined
	loaded := NewManager()
	cidLoaded := buildPruneFixture(t, loaded, 20, 256)
	candidate, _ := coldZoneCandidateTokens(snapshotMessages(t, loaded, cidLoaded))
	res, err = loaded.PruneToolResults(cidLoaded, PruneOpts{MinTokens: candidate * 10})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if res.Pruned != 0 {
		t.Errorf("declined: Pruned = %d, want 0", res.Pruned)
	}
	if res.CandidateTokens <= 0 {
		t.Errorf("declined: CandidateTokens = %d, want > 0 — otherwise this outcome is indistinguishable from (a)",
			res.CandidateTokens)
	}

	assertToolCallPairing(t, snapshotMessages(t, loaded, cidLoaded))
}

// TestPruneToolResults_MinTokensAndTargetTokensCoexist pins that the two knobs
// are orthogonal: MinTokens is an admission gate on the whole batch ("do not
// bother unless it is worth a cache-prefix invalidation"), TargetTokens is an
// early stop once enough has been freed.
func TestPruneToolResults_MinTokensAndTargetTokensCoexist(t *testing.T) {
	m := NewManager()
	cid := buildPruneFixture(t, m, 30, 256)
	candidate, candidateCount := coldZoneCandidateTokens(snapshotMessages(t, m, cid))
	if candidateCount < 2 {
		t.Fatalf("fixture precondition: need >= 2 candidates, got %d", candidateCount)
	}

	res, err := m.PruneToolResults(cid, PruneOpts{MinTokens: candidate, TargetTokens: 1})
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	// Gate passed (candidate total clears MinTokens) but TargetTokens=1 is
	// satisfied by the first rewrite, so the sweep stops there.
	if res.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1 (TargetTokens must still stop the sweep early)", res.Pruned)
	}
	// CandidateTokens reports the whole scanned pool, not just what was used.
	if res.CandidateTokens != candidate {
		t.Errorf("CandidateTokens = %d, want %d (full scanned pool)", res.CandidateTokens, candidate)
	}

	assertToolCallPairing(t, snapshotMessages(t, m, cid))
}

// TestPruneResultString_ReportsCandidates keeps the log line honest: a
// "pruned=0" line that does not say how much was on the table is unreadable
// during an incident.
func TestPruneResultString_ReportsCandidates(t *testing.T) {
	r := &PruneResult{Pruned: 0, TokensFreed: 0, CandidateTokens: 4321}
	got := r.String()
	if !containsAll(got, "pruned=0", "tokens_freed=0", "candidates=4321") {
		t.Errorf("PruneResult.String() = %q, want it to carry pruned / tokens_freed / candidates", got)
	}
}
