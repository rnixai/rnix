package kernel

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestProcInfoExitCodeRoundTrip pins方案 B 持久化契约: ProcInfo.ExitCode and
// ExitCodeSet must survive procInfoToDisk → JSON → procInfoFromDisk without
// being lost or coerced. Three scenarios:
//
//  1. Authoritative success (ExitCodeSet=true, ExitCode=0): on-disk record
//     keeps ExitCodeSet=true so dashboard skips the text heuristic.
//  2. Authoritative failure (ExitCodeSet=true, ExitCode=1): both fields
//     persist.
//  3. Legacy (ExitCodeSet=false, ExitCode=0): both fields stay at zero values
//     and are omitted from the JSON so old proc-info.json files keep
//     deserializing into ExitCodeSet=false (fallback to text heuristic).
func TestProcInfoExitCodeRoundTrip(t *testing.T) {
	cases := []struct {
		name            string
		exitCode        int
		exitCodeSet     bool
		wantJSONHasCode bool
		wantJSONHasSet  bool
	}{
		{"authoritative success", 0, true, false /* omitempty drops 0 */, true},
		{"authoritative failure", 1, true, true, true},
		{"legacy unrecorded", 0, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := vfs.ProcInfo{
				PID:         types.PID(42),
				UUID:        "uuid-42",
				State:       types.StateDead,
				ExitCode:    tc.exitCode,
				ExitCodeSet: tc.exitCodeSet,
				Result:      "found error in archive.go",
			}

			data, err := json.Marshal(procInfoToDisk(orig))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("unmarshal raw: %v", err)
			}
			_, hasCode := raw["exit_code"]
			_, hasSet := raw["exit_code_set"]
			if hasCode != tc.wantJSONHasCode {
				t.Errorf("exit_code present=%v, want %v (json=%s)", hasCode, tc.wantJSONHasCode, string(data))
			}
			if hasSet != tc.wantJSONHasSet {
				t.Errorf("exit_code_set present=%v, want %v (json=%s)", hasSet, tc.wantJSONHasSet, string(data))
			}

			var d procInfoDisk
			if err := json.Unmarshal(data, &d); err != nil {
				t.Fatalf("unmarshal disk: %v", err)
			}
			got := procInfoFromDisk(d)
			if got.ExitCode != tc.exitCode {
				t.Errorf("ExitCode round-trip = %d, want %d", got.ExitCode, tc.exitCode)
			}
			if got.ExitCodeSet != tc.exitCodeSet {
				t.Errorf("ExitCodeSet round-trip = %v, want %v", got.ExitCodeSet, tc.exitCodeSet)
			}
		})
	}
}

// TestProcInfoExitCodeBackwardCompat verifies that a legacy proc-info.json
// without exit_code / exit_code_set fields deserializes into ExitCodeSet=false
// — the trigger for IsProcessFailed's text-heuristic fallback. This is the
// migration contract: old data must keep working without backfill.
func TestProcInfoExitCodeBackwardCompat(t *testing.T) {
	legacyJSON := []byte(`{
		"pid": 1,
		"uuid": "legacy-uuid",
		"state": "dead",
		"result": "operation error"
	}`)
	var d procInfoDisk
	if err := json.Unmarshal(legacyJSON, &d); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	info := procInfoFromDisk(d)
	if info.ExitCodeSet {
		t.Errorf("legacy proc-info.json must produce ExitCodeSet=false, got true")
	}
	if info.ExitCode != 0 {
		t.Errorf("legacy proc-info.json must produce ExitCode=0, got %d", info.ExitCode)
	}
}
