// Package detail — atdd_74_2_token_spend_test.go
//
// Story 74.2: Detail pane Token Spend section (FR6). Renders the process-level
// four-way cumulative token spend as a dedicated section between §4 Context
// stats and §5 Stall. NFR1: §4 Context stats untouched. NFR5: all-zero
// cumulatives → section omitted entirely (legacy/history processes).
//
// AC coverage:
//   - AC4-1a: all-zero cumulatives → output contains no "Token Spend"
//   - AC4-1b: non-zero cumulatives → "Token Spend" + four rows formatted via
//     timeline.FormatTokenCount ("Input: 1.2k tok", "Cache Create: 120 tok")
package detail

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// tokenSpendState builds a minimal DetailState whose Detail.PID/UUID match
// SelectedPID/SelectedUUID so Render()'s Loading-guard does not short-circuit.
func tokenSpendState(pid types.PID, uuid string) DetailState {
	return DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      pid,
			UUID:     uuid,
			State:    "dead",
			Provider: "claude",
			Model:    "sonnet",
		},
	}
}

func TestATDD_74_2_AC4_001_AllZeroOmitsSection(t *testing.T) {
	out := Render(tokenSpendState(42, "u-742-zero"), RenderContext{
		SelectedPID:  42,
		SelectedUUID: "u-742-zero",
	}, 80)
	if strings.Contains(out, "Token Spend") {
		t.Errorf("NFR5 FAIL: all-zero cumulatives must omit the Token Spend section, got:\n%s", out)
	}
	// §4 Context stats must still render (NFR1 untouched).
	if !strings.Contains(out, "Context") {
		t.Errorf("NFR1 FAIL: §4 Context stats missing from output:\n%s", out)
	}
}

func TestATDD_74_2_AC4_002_NonZeroRendersFourRows(t *testing.T) {
	st := tokenSpendState(43, "u-742-spend")
	st.Detail.CumInputTokens = 1200
	st.Detail.CumCachedInputTokens = 500
	st.Detail.CumCacheCreationInputTokens = 120
	st.Detail.CumOutputTokens = 340

	out := Render(st, RenderContext{
		SelectedPID:  43,
		SelectedUUID: "u-742-spend",
	}, 80)

	for _, want := range []string{
		"Token Spend",
		"Input: 1.2k tok",
		"Cached: 500 tok",
		"Cache Create: 120 tok",
		"Output: 340 tok",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("AC4 FAIL: missing %q in output:\n%s", want, out)
		}
	}
	// NFR1: §4 Context stats row still present (占用量与累计花费量纲分离).
	if !strings.Contains(out, "msgs") {
		t.Errorf("NFR1 FAIL: §4 Context stats row missing:\n%s", out)
	}
}
