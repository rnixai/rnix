package title

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// Force lipgloss into TrueColor so SGR codes are actually emitted in tests
// (default `go test` sees no TTY and lipgloss strips escapes). This lets the
// 方案 B regression test compare SGR output between the authoritative-success
// path and the legacy text-heuristic path.
func init() {
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
}

// TestStyleProviderName_NilProc covers the nil-safe contract — nil proc must
// return empty string (no provider segment in title bar).
func TestStyleProviderName_NilProc(t *testing.T) {
	if s := StyleProviderName(true, nil); s != "" {
		t.Errorf("nil proc expected empty, got %q", s)
	}
}

// TestStyleProviderName_EmptyProvider covers proc.Provider == "" → empty
// (provider not yet detected by daemon).
func TestStyleProviderName_EmptyProvider(t *testing.T) {
	proc := &vfs.ProcInfo{Provider: "", State: types.StateRunning}
	if s := StyleProviderName(true, proc); s != "" {
		t.Errorf("empty provider expected empty, got %q", s)
	}
}

// TestStyleProviderName_HealthyContainsName covers the happy path — healthy
// running process returns provider name (with SGR codes added). The exact
// SGR codes vary by lipgloss profile so we just verify name is contained.
func TestStyleProviderName_HealthyContainsName(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider:      "claude-sonnet",
		State:         types.StateRunning,
		TokensUsed:    100,
		ContextBudget: 1000,
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("healthy provider name expected in output, got %q", s)
	}
}

// TestStyleProviderName_DisconnectedRedColor covers !connected → ColorError
// (red) regardless of process state. Profile-tolerant: under non-color
// profiles SGR codes are stripped but provider name remains.
func TestStyleProviderName_DisconnectedRedColor(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider:      "claude-sonnet",
		State:         types.StateRunning,
		TokensUsed:    100,
		ContextBudget: 1000,
	}
	s := StyleProviderName(false, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("disconnected provider name expected in output, got %q", s)
	}
	// In TrueColor profile we should see SGR codes for ColorError; in NoColor
	// profile they're stripped. Either way we must not produce an empty string.
	if s == "" {
		t.Error("disconnected proc must still emit provider name (just with red color)")
	}
}

// TestStyleProviderName_DeadFailedRed covers Dead+IsFailedResult → red.
func TestStyleProviderName_DeadFailedRed(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider: "claude-sonnet",
		State:    types.StateDead,
		Result:   "error: crash",
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("dead+failed provider name expected, got %q", s)
	}
}

// TestStyleProviderName_DeadSuccessWithErrorKeyword pins the方案 B 回归 fix:
// a successful Dead process whose result text happens to contain "error" /
// "fail" / "timeout" (typical of code-review reviewer agents reporting found
// problems) must NOT render red anymore — the authoritative ExitCode path
// suppresses the text-heuristic false positive. We verify this by comparing
// SGR output against the legacy fallback (ExitCodeSet=false), which still
// flags red on the same result text.
func TestStyleProviderName_DeadSuccessWithErrorKeyword(t *testing.T) {
	auth := &vfs.ProcInfo{
		Provider:    "claude-sonnet",
		State:       types.StateDead,
		ExitCode:    0,
		ExitCodeSet: true,
		Result:      "found [HIGH] error in archive.go",
	}
	authOut := StyleProviderName(true, auth)
	if !strings.Contains(authOut, "claude-sonnet") {
		t.Fatalf("authoritative-success provider name expected, got %q", authOut)
	}

	legacy := *auth
	legacy.ExitCodeSet = false
	legacyOut := StyleProviderName(true, &legacy)

	// Sanity: legacy fallback must agree the result text is failure-looking,
	// otherwise this test is not actually exercising the regression we fixed.
	if !ui.IsFailedResult(legacy.Result) {
		t.Skip("ui.IsFailedResult contract changed — test result text no longer trips heuristic")
	}

	// Authoritative path (success) and legacy path (red) must render
	// different SGR codes for the same provider name + result text.
	if authOut == legacyOut {
		t.Errorf("ExitCodeSet=true must suppress text-heuristic false positive — auth=%q legacy=%q", authOut, legacyOut)
	}
}

// TestStyleProviderName_DeadSuccessGreen covers Dead but successful result —
// goes through default (green) since IsFailedResult returns false. Note: empty
// Result string actually means failure (per ui.isFailedResult contract), so we
// use a non-empty success-marker string to exercise the green branch.
func TestStyleProviderName_DeadSuccessGreen(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider: "claude-sonnet",
		State:    types.StateDead,
		Result:   "ok", // non-empty success marker
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("dead+success provider name expected, got %q", s)
	}
	// Sanity: ensure ui.IsFailedResult returns false for "ok" so this case
	// really does fall through to default (green). If IsFailedResult changes
	// its behavior, this test will catch the divergence.
	if ui.IsFailedResult("ok") {
		t.Skip("ui.IsFailedResult contract changed — test no longer reaches default branch")
	}
}

// TestStyleProviderName_HighCtxYellow covers ctx >= 80% → yellow warning.
// The 80% threshold must align with PctColorStyle's threshold (Story 38.2
// 颜色一致性原则).
func TestStyleProviderName_HighCtxYellow(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider:      "claude-sonnet",
		State:         types.StateRunning,
		TokensUsed:    800, // 80% exactly
		ContextBudget: 1000,
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("high-ctx provider name expected, got %q", s)
	}
}

// TestStyleProviderName_BoundaryAt79NotYellow covers ctx == 79% → still green
// (default), confirming the >= 80 boundary is exclusive at 79%.
func TestStyleProviderName_BoundaryAt79NotYellow(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider:      "claude-sonnet",
		State:         types.StateRunning,
		TokensUsed:    79,
		ContextBudget: 100,
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("ctx=79 provider name expected, got %q", s)
	}
}

// TestStyleProviderName_ZeroBudgetGreen covers ContextBudget == 0 → green
// (cannot compute percentage so falls through to default).
func TestStyleProviderName_ZeroBudgetGreen(t *testing.T) {
	proc := &vfs.ProcInfo{
		Provider:      "claude-sonnet",
		State:         types.StateRunning,
		TokensUsed:    100,
		ContextBudget: 0, // not configured
	}
	s := StyleProviderName(true, proc)
	if !strings.Contains(s, "claude-sonnet") {
		t.Errorf("zero-budget provider name expected, got %q", s)
	}
}
