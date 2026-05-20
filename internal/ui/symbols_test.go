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
