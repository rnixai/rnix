package kernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.3 — AC#1: procInfoDisk persistence of suspend_reason / paused_at /
// is_paused.
//
// Story spec (44-3 §AC1):
//   - procInfoDisk gains three new fields with omitempty JSON tags.
//   - procInfoToDisk maps vfs.ProcInfo.{SuspendReason, PausedAt, IsPaused}
//     onto the disk struct; PausedAt is RFC3339Nano-formatted, zero values
//     omitted.
//   - procInfoFromDisk reverses the mapping; PausedAt parses with
//     time.Parse(time.RFC3339Nano, ...) and is left zero on parse error.
//   - SuspendReason field is intentionally untyped string — Epic 45 解耦
//     红线 means the persistence layer does not bind to any specific enum
//     value. Test examples use "user_paused" / "cli_disconnected" / "" /
//     "reason_for_test_only", NEVER "heartbeat_timeout".
//
// RED phase signal: the procInfoDisk type does not yet expose the three
// fields → the test file fails to compile against the current source tree.
// This is the same "compile-fail is RED" pattern 44.1 / 44.2 use.
// =============================================================================

// TestATDD_44_3_010_ProcInfoDisk_Roundtrip_PreservesSuspendFields
//
// AC#1 round-trip: construct vfs.ProcInfo with state=Suspended,
// SuspendReason="user_paused", PausedAt set, IsPaused=true → toDisk →
// fromDisk → fields equal. Mirrors the existing roundtrip pattern from
// procInfoToDisk's PausedTotal handling.
func TestATDD_44_3_010_ProcInfoDisk_Roundtrip_PreservesSuspendFields(t *testing.T) {
	pausedAt := staticTime(t, 0)
	original := vfs.ProcInfo{
		PID:           42,
		UUID:          uuidForTest("acroundt"),
		State:         types.StateSuspended,
		Intent:        "round-trip test",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		TokensUsed:    100,
		CreatedAt:     staticTime(t, -time.Hour),
		CtxID:         7,
		SuspendReason: "user_paused",
		PausedAt:      pausedAt,
		PausedTotal:   2 * time.Second,
		IsPaused:      true,
	}

	d := procInfoToDisk(original)

	// AC#1 explicit field assertions: these line-by-line lookups force the
	// compiler to resolve the post-Task-1 field identifiers; pre-Task-1 the
	// file does not compile because procInfoDisk has no such fields.
	if d.SuspendReason != "user_paused" {
		t.Errorf("d.SuspendReason = %q, want %q", d.SuspendReason, "user_paused")
	}
	if d.PausedAt == "" {
		t.Error("d.PausedAt = \"\", want non-empty RFC3339Nano string")
	} else if _, perr := time.Parse(time.RFC3339Nano, d.PausedAt); perr != nil {
		t.Errorf("d.PausedAt = %q is not RFC3339Nano: %v", d.PausedAt, perr)
	}
	if !d.IsPaused {
		t.Error("d.IsPaused = false, want true (round-trip from in-memory IsPaused=true)")
	}

	// fromDisk reversal — every new field must come back equal modulo
	// timestamp precision (RFC3339Nano allows ns precision).
	got := procInfoFromDisk(d)
	if got.SuspendReason != original.SuspendReason {
		t.Errorf("got.SuspendReason = %q, want %q", got.SuspendReason, original.SuspendReason)
	}
	if !got.PausedAt.Equal(original.PausedAt) {
		t.Errorf("got.PausedAt = %v, want %v", got.PausedAt, original.PausedAt)
	}
	if got.IsPaused != original.IsPaused {
		t.Errorf("got.IsPaused = %v, want %v", got.IsPaused, original.IsPaused)
	}
}

// TestATDD_44_3_011_ProcInfoDisk_OmitemptyEmptyReason
//
// AC#1 omitempty: when SuspendReason is empty string and IsPaused is false,
// the marshalled JSON must NOT contain "suspend_reason" / "is_paused" /
// "paused_at" keys at all. Historical proc-info.json (pre-44.3) had no such
// fields; omitempty preserves that so the disk format is forwards/backwards
// compatible (Story 44.3 §"Epic 42 关联：磁盘是真源").
func TestATDD_44_3_011_ProcInfoDisk_OmitemptyEmptyReason(t *testing.T) {
	original := vfs.ProcInfo{
		PID:       1,
		UUID:      uuidForTest("omitem"),
		State:     types.StateDead, // historical no-pause path
		Intent:    "omitempty test",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -2*time.Hour),
		CtxID:     1,
		// SuspendReason, PausedAt, IsPaused intentionally left zero.
	}

	d := procInfoToDisk(original)
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal procInfoDisk: %v", err)
	}
	body := string(data)
	for _, forbidden := range []string{`"suspend_reason"`, `"paused_at"`, `"is_paused"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("JSON contains %s but field was zero: %s", forbidden, body)
		}
	}
}

// TestATDD_44_3_012_ProcInfoDisk_PausedAtRFC3339NanoFormat
//
// AC#1 timestamp format: PausedAt is serialised as RFC3339Nano (matching the
// existing CreatedAt / DeadAt convention) so downstream tooling can parse
// it uniformly. Round-tripping through fromDisk yields a time.Time with
// nanosecond precision intact (within the time package's RFC3339Nano
// guarantees).
//
// Also exercises the "reason value matrix" mandated by §Dev Notes Epic 45
// red line — these are the only allowed example values for SuspendReason
// in 44.3 ATDD tests. "heartbeat_timeout" is intentionally absent.
func TestATDD_44_3_012_ProcInfoDisk_PausedAtRFC3339NanoFormat(t *testing.T) {
	cases := []struct {
		name   string
		reason string // must be drawn from the §"Epic 45 解耦红线" allowed set
	}{
		{"user_paused (44.1 main path)", "user_paused"},
		{"cli_disconnected (Epic 43 preserved enum)", "cli_disconnected"},
		{"empty (historical proc-info.json)", ""},
		{"custom placeholder (forward compatibility)", "reason_for_test_only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := staticTime(t, 17*time.Minute+345*time.Nanosecond)
			info := vfs.ProcInfo{
				PID:           99,
				UUID:          uuidForTest("ts" + tc.name[:1]),
				State:         types.StateSuspended,
				Intent:        "rfc3339 test",
				Provider:      "claude",
				Model:         "claude-opus-4-7",
				CreatedAt:     staticTime(t, -time.Hour),
				CtxID:         1,
				SuspendReason: tc.reason,
				PausedAt:      ts,
				IsPaused:      tc.reason != "",
			}
			d := procInfoToDisk(info)
			if d.PausedAt == "" {
				t.Fatal("PausedAt serialized to empty; expected RFC3339Nano string")
			}
			parsed, err := time.Parse(time.RFC3339Nano, d.PausedAt)
			if err != nil {
				t.Fatalf("PausedAt %q is not RFC3339Nano: %v", d.PausedAt, err)
			}
			if !parsed.Equal(ts) {
				t.Errorf("PausedAt parse-equal: parsed=%v, want=%v", parsed, ts)
			}
			// reversal via procInfoFromDisk must preserve the reason exactly,
			// proving the persistence layer transparently passes any string
			// without enum validation (red line: future-proof against Epic 45
			// removing "heartbeat_timeout" and Epic N adding new reasons).
			got := procInfoFromDisk(d)
			if got.SuspendReason != tc.reason {
				t.Errorf("round-trip SuspendReason: got %q, want %q", got.SuspendReason, tc.reason)
			}
		})
	}
}
