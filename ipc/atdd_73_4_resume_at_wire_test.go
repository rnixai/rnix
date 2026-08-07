package ipc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 73.4 — AC3 (wire field) / AC5 (quota-suspended visibility).
//
// ProcInfoWire has no ipc/wire mirror (Story 71.4 verified: no TestWireDrift
// guard covers it), so the round-trip guard is self-built in the
// TestProcInfoWire_* pattern of atdd_66_6_wire_test.go / budget_wire_test.go /
// exitcode_wire_test.go. ResumeAtMs is the wire projection of
// vfs.ProcInfo.ResumeAt (Story 73.3): the quota-window reset instant a
// quota-suspended process waits for. Zero value = not quota-suspended or no
// server-declared wait (manual resume only) — omitempty keeps legacy wire
// bytes unchanged.

func TestProcInfoWire_ResumeAt_RoundTrip(t *testing.T) {
	resumeAt := time.Date(2026, 8, 9, 12, 30, 0, 0, time.UTC)
	info := vfs.ProcInfo{
		PID:           42,
		UUID:          "test-uuid-73-4",
		State:         types.StateSuspended,
		SuspendReason: "quota_exhausted",
		ResumeAt:      resumeAt,
		CreatedAt:     time.Now(),
	}
	w := ProcInfoToWire(info)
	if w.ResumeAtMs != resumeAt.UnixMilli() {
		t.Errorf("wire.ResumeAtMs = %d, want %d", w.ResumeAtMs, resumeAt.UnixMilli())
	}
	if w.SuspendReason != "quota_exhausted" {
		t.Errorf("wire.SuspendReason = %q, want quota_exhausted", w.SuspendReason)
	}
	back := WireToProcInfo(w)
	if !back.ResumeAt.Equal(resumeAt) {
		t.Errorf("roundtrip ResumeAt = %v, want %v", back.ResumeAt, resumeAt)
	}
	if back.SuspendReason != "quota_exhausted" {
		t.Errorf("roundtrip SuspendReason = %q, want quota_exhausted", back.SuspendReason)
	}
}

func TestProcInfoWire_ResumeAt_OmitEmpty(t *testing.T) {
	// A quota-suspended process WITHOUT a server-declared wait (manual resume
	// only, Story 73.3 fast path) must omit the field — legacy consumers read 0.
	w := ProcInfoToWire(vfs.ProcInfo{
		PID:           7,
		State:         types.StateSuspended,
		SuspendReason: "quota_exhausted",
		CreatedAt:     time.Now(),
	})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "resume_at_ms") {
		t.Errorf("zero resume_at_ms should be omitted, got: %s", b)
	}
	// And a plain Running process carries neither field (zero-value safety,
	// AC6 — existing rendering is unaffected).
	running := ProcInfoToWire(vfs.ProcInfo{PID: 8, State: types.StateRunning, CreatedAt: time.Now()})
	rb, err := json.Marshal(running)
	if err != nil {
		t.Fatalf("marshal running: %v", err)
	}
	if strings.Contains(string(rb), "resume_at_ms") {
		t.Errorf("Running process should omit resume_at_ms, got: %s", rb)
	}
}

func TestProcInfoWire_ResumeAt_LegacyWireZero(t *testing.T) {
	// A legacy wire (no resume_at_ms field, pre-73.4 daemon) decodes with a
	// zero ResumeAt — the zero-value safety that keeps dashboard / ps
	// rendering unchanged.
	legacy := `{"pid":9,"uuid":"legacy","state":4,"suspend_reason":"quota_exhausted","created_at_ms":1786000000000}`
	var w ProcInfoWire
	if err := json.Unmarshal([]byte(legacy), &w); err != nil {
		t.Fatalf("unmarshal legacy wire: %v", err)
	}
	back := WireToProcInfo(w)
	if !back.ResumeAt.IsZero() {
		t.Errorf("legacy wire ResumeAt = %v, want zero", back.ResumeAt)
	}
	if back.SuspendReason != "quota_exhausted" {
		t.Errorf("legacy wire SuspendReason = %q, want quota_exhausted", back.SuspendReason)
	}
}
