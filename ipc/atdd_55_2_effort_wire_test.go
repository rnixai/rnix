package ipc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 55.2 — ReasoningEffort must survive the ProcInfo ↔ wire roundtrip so the
// dashboard (which reads ProcInfoWire via list_procs / get_proc_detail) shows it.

func TestProcInfoWire_ReasoningEffortRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:             1,
		UUID:            "test-uuid-effort",
		State:           types.StateRunning,
		Intent:          "test",
		CreatedAt:       time.Now(),
		Provider:        "openai",
		Model:           "gpt-5.1",
		ReasoningEffort: "high",
	}
	wire := ProcInfoToWire(info)
	if wire.ReasoningEffort != "high" {
		t.Errorf("wire.ReasoningEffort = %q, want 'high'", wire.ReasoningEffort)
	}
	back := WireToProcInfo(wire)
	if back.ReasoningEffort != "high" {
		t.Errorf("roundtrip ReasoningEffort = %q, want 'high'", back.ReasoningEffort)
	}
}

func TestProcInfoWire_ReasoningEffort_OmitEmpty(t *testing.T) {
	// Empty effort (the common case) must be omitted from JSON so legacy
	// consumers and non-effort providers stay clean.
	wire := ProcInfoToWire(vfs.ProcInfo{PID: 1, CreatedAt: time.Now()})
	b, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "reasoning_effort") {
		t.Errorf("empty effort should be omitted from JSON, got: %s", b)
	}
}
