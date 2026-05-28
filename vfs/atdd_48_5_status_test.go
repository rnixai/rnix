package vfs

import "testing"

// =============================================================================
// ATDD 48.5 AC5 — MCPStatus 5-state enum + MCPStatusString (Task 5.1-5.2)
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red`. Run:
//            go test -tags=atdd_48_5_red ./vfs/
//        Pre-impl: compile fails on undefined MCPStatusReconnecting /
//        MCPStatusBackoffExhausted — the red signal.
//
// Injection points (dev-story Task 5.1-5.2, Story §易错点 5):
//   MCPStatusReconnecting     (appended AFTER MCPStatusError — iota order kept)
//   MCPStatusBackoffExhausted (appended last)
//   MCPStatusString extended to return "reconnecting" / "backoff_exhausted"
// =============================================================================

// -----------------------------------------------------------------------------
// _040: all 5 states map to their canonical lowercase strings.
// -----------------------------------------------------------------------------
func TestATDD_48_5_040_MCPStatusString_FiveStates(t *testing.T) {
	cases := []struct {
		status MCPStatus
		want   string
	}{
		{MCPStatusConnected, "connected"},
		{MCPStatusDisconnected, "disconnected"},
		{MCPStatusError, "error"},
		{MCPStatusReconnecting, "reconnecting"},
		{MCPStatusBackoffExhausted, "backoff_exhausted"},
	}
	for _, c := range cases {
		if got := MCPStatusString(c.status); got != c.want {
			t.Errorf("MCPStatusString(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}

// -----------------------------------------------------------------------------
// _041: existing iota values MUST stay stable; new states are appended after
//        Error (Story §易错点 5 — wire/persistence reference the old values).
// -----------------------------------------------------------------------------
func TestATDD_48_5_041_MCPStatus_EnumValuesStable(t *testing.T) {
	if MCPStatusConnected != 0 {
		t.Errorf("MCPStatusConnected = %d, want 0 (must not shift)", MCPStatusConnected)
	}
	if MCPStatusDisconnected != 1 {
		t.Errorf("MCPStatusDisconnected = %d, want 1 (must not shift)", MCPStatusDisconnected)
	}
	if MCPStatusError != 2 {
		t.Errorf("MCPStatusError = %d, want 2 (must not shift)", MCPStatusError)
	}
	// New states are appended → strictly greater than Error.
	if MCPStatusReconnecting <= MCPStatusError {
		t.Errorf("MCPStatusReconnecting (%d) must be appended after MCPStatusError (%d)", MCPStatusReconnecting, MCPStatusError)
	}
	if MCPStatusBackoffExhausted <= MCPStatusReconnecting {
		t.Errorf("MCPStatusBackoffExhausted (%d) must be appended after MCPStatusReconnecting (%d)", MCPStatusBackoffExhausted, MCPStatusReconnecting)
	}
}

// -----------------------------------------------------------------------------
// _042: unknown / out-of-range status still falls back to "unknown" (default
//        branch preserved after the 5-state extension).
// -----------------------------------------------------------------------------
func TestATDD_48_5_042_MCPStatusString_UnknownFallback(t *testing.T) {
	if got := MCPStatusString(MCPStatus(99)); got != "unknown" {
		t.Errorf("MCPStatusString(99) = %q, want %q", got, "unknown")
	}
}
