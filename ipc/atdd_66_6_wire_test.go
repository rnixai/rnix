package ipc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 66.6 — UsageProvenance + ToolCallCount must survive the ProcInfo ↔ wire
// roundtrip so `rnix ps` / dashboard (list_procs) render the ACTIVE column and
// the estimated-usage `~` guard. ProcInfoWire has no ipc/wire mirror, so
// TestWireDrift does not cover it — this is its round-trip / omitempty guard.

func TestProcInfoWire_UsageProvenanceToolCount_RoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:             7,
		UUID:            "test-uuid-66-6",
		State:           types.StateRunning,
		Intent:          "long step",
		CreatedAt:       time.Now(),
		TokensUsed:      1234,
		UsageProvenance: vfs.UsageProvenanceCLIStream,
		ToolCallCount:   594,
	}
	w := ProcInfoToWire(info)
	if w.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("wire.UsageProvenance = %q, want cli_stream", w.UsageProvenance)
	}
	if w.ToolCallCount != 594 {
		t.Errorf("wire.ToolCallCount = %d, want 594", w.ToolCallCount)
	}
	back := WireToProcInfo(w)
	if back.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("roundtrip UsageProvenance = %q, want cli_stream", back.UsageProvenance)
	}
	if back.ToolCallCount != 594 {
		t.Errorf("roundtrip ToolCallCount = %d, want 594", back.ToolCallCount)
	}
}

func TestProcInfoWire_UsageProvenanceToolCount_OmitEmpty(t *testing.T) {
	// Legacy / fresh processes with no usage label and no tool calls must keep
	// the wire clean so a pre-66.6 client reads back "" / 0 unchanged.
	w := ProcInfoToWire(vfs.ProcInfo{PID: 1, CreatedAt: time.Now()})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "usage_provenance") {
		t.Errorf("empty usage_provenance should be omitted, got: %s", b)
	}
	if strings.Contains(string(b), "tool_call_count") {
		t.Errorf("zero tool_call_count should be omitted, got: %s", b)
	}
}
