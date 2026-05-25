package ui

import "testing"

// TestIsFailedResult_InterruptedNotFailed pins the carve-out added for the
// "script parent suspended on CLI disconnect" fix. Result strings that mark
// CLI interruption must NOT render as failure even though the parent's exit
// code is 1 — the script is resumable from Dashboard.
func TestIsFailedResult_InterruptedNotFailed(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantOK bool // true means NOT a failed result
	}{
		{"empty is failed", "", false},
		{"explicit error", "context deadline exceeded: error", false},
		{"failure word", "operation failed", false},
		{"timeout", "wait timeout", false},
		{"interrupted lowercase", "interrupted", true},
		{"interrupted mixed case", "Interrupted", true},
		{"cli_disconnected", "cli_disconnected", true},
		{"cli_disconnected upper", "CLI_DISCONNECTED", true},
		{"success result", "ok", true},
		{"normal output", "the answer is 42", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsFailedResult(tc.input)
			if got == tc.wantOK { // wantOK=true means NOT failed
				t.Errorf("IsFailedResult(%q) = %v, want %v (not-failed=%v)",
					tc.input, got, !tc.wantOK, tc.wantOK)
			}
		})
	}
}

// TestIsProcessFailed exercises the authoritative ExitCode path and the
// legacy text-heuristic fallback. The first case is the regression we are
// fixing: a code-review reviewer's success output containing "error" must
// no longer be flagged as a failed process.
func TestIsProcessFailed(t *testing.T) {
	cases := []struct {
		name        string
		exitCode    int
		exitCodeSet bool
		result      string
		want        bool
	}{
		// Authoritative path — ExitCode wins, result text is ignored.
		{"success with error keyword in output", 0, true, "found [HIGH] error in archive.go", false},
		{"success with fail keyword in output", 0, true, "this code will fail under load", false},
		{"success empty result", 0, true, "", false},
		{"non-zero exit code", 1, true, "ok", true},
		{"negative exit code (defensive)", -1, true, "ok", true},

		// Fallback path — ExitCodeSet=false defers to isFailedResult.
		{"legacy empty result is failure", 0, false, "", true},
		{"legacy result with error keyword", 0, false, "operation error", true},
		{"legacy success result", 0, false, "ok", false},
		{"legacy interrupted not failure", 0, false, "interrupted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsProcessFailed(tc.exitCode, tc.exitCodeSet, tc.result)
			if got != tc.want {
				t.Errorf("IsProcessFailed(%d,%v,%q) = %v, want %v",
					tc.exitCode, tc.exitCodeSet, tc.result, got, tc.want)
			}
		})
	}
}
