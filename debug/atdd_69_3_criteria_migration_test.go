package debug

import (
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
)

// Story 69.3 Task 1 — migration equivalence guard.
//
// The classification criteria (leakedThreshold / activeWindowSize /
// warmWindowSize) moved to the context package so the pruning primitives and
// this analyzer share one definition. These tests re-implement the PRE-migration
// algorithm locally and assert the migrated code agrees for every shape we care
// about. They are green-guards: they must stay green forever. If someone
// "improves" a window formula in context/prune.go, this file goes red.

// preMigrationActiveWindowSize is the verbatim pre-migration implementation.
func preMigrationActiveWindowSize(n int) int {
	const minActiveWindow = 4
	adaptive := n / 5
	if adaptive > minActiveWindow {
		return adaptive
	}
	return minActiveWindow
}

// preMigrationWarmWindowSize is the verbatim pre-migration implementation.
func preMigrationWarmWindowSize(n int) int {
	const minWarmWindow = 6
	adaptive := n * 3 / 10
	if adaptive > minWarmWindow {
		return adaptive
	}
	return minWarmWindow
}

func TestCtxProfile_MigratedWindowFunctionsEquivalent(t *testing.T) {
	for n := range 400 {
		if got, want := rnixctx.ActiveWindowSize(n), preMigrationActiveWindowSize(n); got != want {
			t.Fatalf("ActiveWindowSize(%d) = %d, want %d (migration must be byte-equivalent)", n, got, want)
		}
		if got, want := rnixctx.WarmWindowSize(n), preMigrationWarmWindowSize(n); got != want {
			t.Fatalf("WarmWindowSize(%d) = %d, want %d (migration must be byte-equivalent)", n, got, want)
		}

		// ColdZoneEnd must equal the pre-migration warmStart computation.
		activeStart := max(0, n-preMigrationActiveWindowSize(n))
		wantCold := max(0, activeStart-preMigrationWarmWindowSize(n))
		if got := rnixctx.ColdZoneEnd(n); got != wantCold {
			t.Fatalf("ColdZoneEnd(%d) = %d, want %d", n, got, wantCold)
		}
	}
}

func TestCtxProfile_MigratedLeakedPredicateEquivalent(t *testing.T) {
	const preMigrationLeakedThreshold = 1000
	if rnixctx.LeakedThreshold != preMigrationLeakedThreshold {
		t.Fatalf("LeakedThreshold = %d, want %d", rnixctx.LeakedThreshold, preMigrationLeakedThreshold)
	}

	cases := []CtxMessage{
		{Role: "tool", Content: strings.Repeat("x", 1001)},
		{Role: "tool", Content: strings.Repeat("x", 1000)}, // boundary: NOT leaked (strict >)
		{Role: "tool", Content: strings.Repeat("x", 999)},
		{Role: "tool", Content: ""},
		{Role: "user", Content: strings.Repeat("x", 5000)},
		{Role: "assistant", Content: strings.Repeat("x", 5000)},
		{Role: "system", Content: strings.Repeat("x", 5000)},
		{Role: "", Content: strings.Repeat("x", 5000)},
	}
	for i, msg := range cases {
		want := msg.Role == "tool" && len(msg.Content) > preMigrationLeakedThreshold
		if got := isLeaked(msg); got != want {
			t.Errorf("case %d (role=%q len=%d): isLeaked = %v, want %v", i, msg.Role, len(msg.Content), got, want)
		}
	}
}

// preMigrationClassify is the verbatim pre-migration classifyMessages body,
// kept as an independent oracle.
func preMigrationClassify(data *ContextData, sysTokens, totalTokens int) ClassificationResult {
	n := len(data.Messages)
	var result ClassificationResult

	activeStart := max(0, n-preMigrationActiveWindowSize(n))
	warmStart := max(0, activeStart-preMigrationWarmWindowSize(n))

	activeTokens := sysTokens
	activeMsgs := 0
	for i := activeStart; i < n; i++ {
		activeTokens += estimateTokens(data.Messages[i].Content)
		activeMsgs++
	}
	if data.SystemPrompt != "" {
		activeMsgs++
	}
	result.Active = ClassBucket{Tokens: activeTokens, Messages: activeMsgs}

	warmTokens := 0
	warmMsgs := 0
	for i := warmStart; i < activeStart; i++ {
		warmTokens += estimateTokens(data.Messages[i].Content)
		warmMsgs++
	}
	result.Warm = ClassBucket{Tokens: warmTokens, Messages: warmMsgs}

	coldTokens := 0
	coldMsgs := 0
	leakedTokens := 0
	leakedMsgs := 0
	for i := range warmStart {
		msg := data.Messages[i]
		tok := estimateTokens(msg.Content)
		if msg.Role == "tool" && len(msg.Content) > 1000 {
			leakedTokens += tok
			leakedMsgs++
		} else {
			coldTokens += tok
			coldMsgs++
		}
	}
	result.Cold = ClassBucket{Tokens: coldTokens, Messages: coldMsgs}
	result.Leaked = ClassBucket{Tokens: leakedTokens, Messages: leakedMsgs}

	if totalTokens > 0 {
		result.Active.Pct = roundPct(float64(result.Active.Tokens) / float64(totalTokens) * 100)
		result.Warm.Pct = roundPct(float64(result.Warm.Tokens) / float64(totalTokens) * 100)
		result.Cold.Pct = roundPct(float64(result.Cold.Tokens) / float64(totalTokens) * 100)
		result.Leaked.Pct = roundPct(float64(result.Leaked.Tokens) / float64(totalTokens) * 100)
	}
	return result
}

func buildMigrationFixture(n int) *ContextData {
	data := &ContextData{SystemPrompt: "you are a test agent"}
	for i := range n {
		switch i % 4 {
		case 0:
			data.Messages = append(data.Messages, CtxMessage{Role: "user", Content: "please do step"})
		case 1:
			data.Messages = append(data.Messages, CtxMessage{Role: "assistant", Content: "calling read_file"})
		case 2:
			// Big tool result → leaked when in the cold zone.
			data.Messages = append(data.Messages, CtxMessage{
				Role:       "tool",
				Content:    "read_file result " + strings.Repeat("payload ", 400),
				ToolCallID: "call-x",
			})
		case 3:
			// Small tool result → cold, never leaked.
			data.Messages = append(data.Messages, CtxMessage{Role: "tool", Content: "ok", ToolCallID: "call-y"})
		}
	}
	return data
}

func TestCtxProfile_MigratedCriteriaAnalyzeEquivalent(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 9, 10, 20, 41, 60, 137, 256} {
		data := buildMigrationFixture(n)

		sysTokens := estimateTokens(data.SystemPrompt)
		totalTokens := sysTokens
		for _, msg := range data.Messages {
			totalTokens += estimateTokens(msg.Content)
		}

		want := preMigrationClassify(data, sysTokens, totalTokens)
		got := AnalyzeContext(data, 1, 1, 0, 0)

		if got.TotalTokens != totalTokens {
			t.Errorf("n=%d: TotalTokens = %d, want %d", n, got.TotalTokens, totalTokens)
		}
		if got.Classification != want {
			t.Errorf("n=%d: Classification mismatch\n got: %+v\nwant: %+v", n, got.Classification, want)
		}

		// Suggestions must be byte-equivalent too (AC8: public behaviour unchanged).
		oracle := &CtxProfileResult{
			TotalTokens:    totalTokens,
			Classification: want,
			TopConsumers:   findTopConsumers(data, totalTokens, topConsumersN),
		}
		wantSug := generateSuggestions(oracle)
		if len(got.Suggestions) != len(wantSug) {
			t.Fatalf("n=%d: suggestions len = %d, want %d (%v vs %v)", n, len(got.Suggestions), len(wantSug), got.Suggestions, wantSug)
		}
		for i := range wantSug {
			if got.Suggestions[i] != wantSug[i] {
				t.Errorf("n=%d: suggestion[%d] = %q, want %q", n, i, got.Suggestions[i], wantSug[i])
			}
		}
	}
}
