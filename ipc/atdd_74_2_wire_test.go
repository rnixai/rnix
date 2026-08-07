package ipc

// Story 74.2 — the process-level four-way cumulative token spend must survive
// the ProcInfo ↔ wire roundtrip so `rnix ps` / JSON IPC consumers see it.
// ProcInfoWire has no ipc/wire mirror (create-story 修正 1 / AI-73-6), so
// TestWireDrift does not cover it — this is its round-trip / omitempty /
// legacy-zero guard, following the 66-6 TestProcInfoWire_* pattern.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestProcInfoWire_TokenSpend_RoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:                         71,
		UUID:                        "test-uuid-74-2",
		State:                       types.StateDead,
		Intent:                      "74.2 wire",
		CreatedAt:                   time.Now(),
		TokensUsed:                  785,
		CumInputTokens:              600,
		CumCachedInputTokens:        300,
		CumCacheCreationInputTokens: 120,
		CumOutputTokens:             180,
	}
	w := ProcInfoToWire(info)
	if w.CumInputTokens != 600 || w.CumCachedInputTokens != 300 ||
		w.CumCacheCreationInputTokens != 120 || w.CumOutputTokens != 180 {
		t.Fatalf("wire = %d/%d/%d/%d want 600/300/120/180",
			w.CumInputTokens, w.CumCachedInputTokens, w.CumCacheCreationInputTokens, w.CumOutputTokens)
	}
	// AC3 test-1: the round-trip must pass through real JSON — a mistyped
	// cum_*_tokens json tag would otherwise silently zero the fields for
	// `rnix ps` / JSON IPC consumers with every test still green.
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var w2 ProcInfoWire
	if err := json.Unmarshal(b, &w2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	back := WireToProcInfo(w2)
	if back.CumInputTokens != 600 || back.CumCachedInputTokens != 300 ||
		back.CumCacheCreationInputTokens != 120 || back.CumOutputTokens != 180 {
		t.Errorf("roundtrip = %d/%d/%d/%d want 600/300/120/180",
			back.CumInputTokens, back.CumCachedInputTokens, back.CumCacheCreationInputTokens, back.CumOutputTokens)
	}
}

func TestProcInfoWire_TokenSpend_OmitEmpty(t *testing.T) {
	// Fresh / legacy processes with no completed step must keep the wire clean.
	w := ProcInfoToWire(vfs.ProcInfo{PID: 1, CreatedAt: time.Now()})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"cum_input_tokens", "cum_cached_input_tokens", "cum_cache_creation_input_tokens", "cum_output_tokens"} {
		if strings.Contains(string(b), key) {
			t.Errorf("zero %s should be omitted, got: %s", key, b)
		}
	}
}

func TestProcInfoWire_TokenSpend_LegacyWireZero(t *testing.T) {
	// A pre-74.2 daemon's wire JSON has no cum_*_tokens keys → reads back 0.
	// state is the numeric ProcessState enum on the wire (see 73-4 legacy
	// fixture shape).
	legacy := `{"pid":5,"uuid":"legacy","state":2,"intent":"y","tokens_used":10,"created_at_ms":1786000000000}`
	var w ProcInfoWire
	if err := json.Unmarshal([]byte(legacy), &w); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	back := WireToProcInfo(w)
	if back.CumInputTokens != 0 || back.CumCachedInputTokens != 0 ||
		back.CumCacheCreationInputTokens != 0 || back.CumOutputTokens != 0 {
		t.Errorf("legacy wire → cumulatives %d/%d/%d/%d, want 0/0/0/0",
			back.CumInputTokens, back.CumCachedInputTokens, back.CumCacheCreationInputTokens, back.CumOutputTokens)
	}
}
