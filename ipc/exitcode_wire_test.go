package ipc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// isProcessFailedMirror replays internal/ui.IsProcessFailed semantics here to
// avoid an import cycle: internal/ui directly imports ipc (see
// internal/ui/intent.go), so ipc cannot reach back into internal/ui.
//
// Keep this in sync with internal/ui/symbols.go::IsProcessFailed. The mirror's
// drift risk is bounded by internal/ui/symbols_test.go covering the same
// semantics directly on the real function.
func isProcessFailedMirror(exitCode int, exitCodeSet bool, result string) bool {
	if exitCodeSet {
		return exitCode != 0
	}
	if result == "" {
		return true
	}
	lower := strings.ToLower(result)
	if lower == "interrupted" || lower == "cli_disconnected" {
		return false
	}
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "timeout")
}

// TestProcInfoWire_ExitCode_RoundTrip pins the contract: ExitCode and ExitCodeSet
// must survive ProcInfo → ProcInfoWire → JSON → ProcInfoWire → ProcInfo.
//
// Without these fields on the wire, the daemon's authoritative exit signal is
// dropped and the dashboard falls back to scanning Result for "error"/"fail"/
// "timeout" — which falsely flags successful code-review reports that mention
// "AbortError" or similar.
func TestProcInfoWire_ExitCode_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   vfs.ProcInfo
	}{
		{
			name: "Success with ExitCodeSet=true, ExitCode=0",
			in: vfs.ProcInfo{
				PID:         6,
				UUID:        "test-success",
				State:       types.StateDead,
				ExitCode:    0,
				ExitCodeSet: true,
				Result:      "AbortError was handled gracefully",
			},
		},
		{
			name: "Failure with ExitCodeSet=true, ExitCode=1",
			in: vfs.ProcInfo{
				PID:         7,
				UUID:        "test-fail",
				State:       types.StateDead,
				ExitCode:    1,
				ExitCodeSet: true,
				Result:      "llm write failed",
			},
		},
		{
			name: "Legacy with ExitCodeSet=false",
			in: vfs.ProcInfo{
				PID:         8,
				UUID:        "test-legacy",
				State:       types.StateDead,
				ExitCode:    0,
				ExitCodeSet: false,
				Result:      "completed",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := ProcInfoToWire(tc.in)
			if wire.ExitCode != tc.in.ExitCode {
				t.Errorf("wire ExitCode: got %d, want %d", wire.ExitCode, tc.in.ExitCode)
			}
			if wire.ExitCodeSet != tc.in.ExitCodeSet {
				t.Errorf("wire ExitCodeSet: got %v, want %v", wire.ExitCodeSet, tc.in.ExitCodeSet)
			}

			raw, err := json.Marshal(wire)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			var decoded ProcInfoWire
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			out := WireToProcInfo(decoded)
			if out.ExitCode != tc.in.ExitCode {
				t.Errorf("round-trip ExitCode: got %d, want %d", out.ExitCode, tc.in.ExitCode)
			}
			if out.ExitCodeSet != tc.in.ExitCodeSet {
				t.Errorf("round-trip ExitCodeSet: got %v, want %v", out.ExitCodeSet, tc.in.ExitCodeSet)
			}
		})
	}
}

// TestProcInfoWire_ExitCode_DashboardFailureSemantics is the regression test
// for the user-reported bug: a grandchild process that completed successfully
// (ExitCodeSet=true, ExitCode=0) but whose Result contains "Error" must NOT
// be rendered as failed.
//
// Before the fix, ProcInfoWire dropped ExitCode/ExitCodeSet, so the dashboard
// downstream call IsProcessFailed(0, false, result) walked the fallback text
// heuristic and matched on "AbortError" → false positive.
func TestProcInfoWire_ExitCode_DashboardFailureSemantics(t *testing.T) {
	original := vfs.ProcInfo{
		PID:         6,
		UUID:        "blind-hunter",
		State:       types.StateDead,
		ExitCode:    0,
		ExitCodeSet: true,
		Result:      "Here is the code review. Issues: AbortError detection missing DOMException existence check",
	}

	if !strings.Contains(strings.ToLower(original.Result), "error") {
		t.Fatalf("test setup: Result must contain 'error' to exercise the regression path")
	}

	wire := ProcInfoToWire(original)
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded ProcInfoWire
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	restored := WireToProcInfo(decoded)

	if isProcessFailedMirror(restored.ExitCode, restored.ExitCodeSet, restored.Result) {
		t.Errorf("after wire round-trip, successful process with Result containing 'Error' "+
			"was rendered as failed (ExitCode=%d, ExitCodeSet=%v) — wire still drops the "+
			"authoritative exit signal", restored.ExitCode, restored.ExitCodeSet)
	}
}
