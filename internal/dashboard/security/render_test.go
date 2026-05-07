// Package security — render_test.go (Story 38-5 PR11 Step 4(c))
//
// 验证 Render() 行为契约 + 4 个 helpers（与 cmd/rnix.renderSecurityPane 1:1
// 等价 · Story 22-1/22-3/27-8 行为保留）。
package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/ipc"
)

// --- helpers ---

func TestSortAlertsByDeviation_Empty(t *testing.T) {
	if got := SortAlertsByDeviation(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := SortAlertsByDeviation([]ipc.AlertWire{}); got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

func TestSortAlertsByDeviation_Descending(t *testing.T) {
	alerts := []ipc.AlertWire{
		{Deviation: 1.5},
		{Deviation: 5.0},
		{Deviation: 3.2},
	}
	got := SortAlertsByDeviation(alerts)
	if len(got) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(got))
	}
	if got[0].Deviation != 5.0 || got[1].Deviation != 3.2 || got[2].Deviation != 1.5 {
		t.Errorf("expected descending order [5.0, 3.2, 1.5], got %v", got)
	}
	// Sort should not mutate the input
	if alerts[0].Deviation != 1.5 {
		t.Errorf("input should not be mutated, got %v", alerts)
	}
}

func TestAlertTypeColor(t *testing.T) {
	cases := map[string]lipgloss.Color{
		"syscall_freq":  lipgloss.Color("220"),
		"token_rate":    lipgloss.Color("208"),
		"device_access": lipgloss.Color("196"),
		"unknown":       lipgloss.Color("240"),
		"":              lipgloss.Color("240"),
	}
	for input, want := range cases {
		if got := AlertTypeColor(input); got != want {
			t.Errorf("AlertTypeColor(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSecurityStatusColor(t *testing.T) {
	cases := map[string]lipgloss.Color{
		"ok":      lipgloss.Color("42"),
		"warning": lipgloss.Color("196"),
		"":        lipgloss.Color("240"),
	}
	for input, want := range cases {
		if got := SecurityStatusColor(input); got != want {
			t.Errorf("SecurityStatusColor(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestFormatUptimeShort(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{1000, "1s"},
		{59000, "59s"},
		{60000, "1m0s"},
		{125000, "2m5s"},
		{3600000, "1h0m"},
		{7200000, "2h0m"},
	}
	for _, c := range cases {
		if got := FormatUptimeShort(c.ms); got != c.want {
			t.Errorf("FormatUptimeShort(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestFormatTimeAgo_ZeroOrNegative(t *testing.T) {
	if got := FormatTimeAgo(0); got != "" {
		t.Errorf("expected empty for ts=0, got %q", got)
	}
	if got := FormatTimeAgo(-1); got != "" {
		t.Errorf("expected empty for negative ts, got %q", got)
	}
}

func TestFormatTimeAgo_Buckets(t *testing.T) {
	now := time.Now()
	// secs ago
	if got := FormatTimeAgo(now.Add(-30 * time.Second).UnixMilli()); !strings.HasSuffix(got, "s ago") {
		t.Errorf("30s ago should produce '...s ago', got %q", got)
	}
	// mins ago
	if got := FormatTimeAgo(now.Add(-5 * time.Minute).UnixMilli()); !strings.HasSuffix(got, "m ago") {
		t.Errorf("5m ago should produce '...m ago', got %q", got)
	}
	// hours ago
	if got := FormatTimeAgo(now.Add(-2 * time.Hour).UnixMilli()); !strings.HasSuffix(got, "h ago") {
		t.Errorf("2h ago should produce '...h ago', got %q", got)
	}
	// days ago
	if got := FormatTimeAgo(now.Add(-50 * time.Hour).UnixMilli()); !strings.HasSuffix(got, "d ago") {
		t.Errorf("50h ago should produce '...d ago', got %q", got)
	}
}

// --- Render ---

func TestRender_NilImmuneStatusLoading(t *testing.T) {
	state := SecurityState{ImmuneStatus: nil, ImmuneErr: nil}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "Loading...") {
		t.Errorf("expected 'Loading...' for nil status, got %q", got)
	}
}

func TestRender_NilImmuneStatusError(t *testing.T) {
	state := SecurityState{ImmuneStatus: nil, ImmuneErr: errors.New("boom")}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "Error:") {
		t.Errorf("expected 'Error:' for ImmuneErr, got %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("expected error message in output, got %q", got)
	}
}

func TestRender_DaemonNotRunning(t *testing.T) {
	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{Running: false},
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "Immune Daemon not running") {
		t.Errorf("expected daemon-not-running message, got %q", got)
	}
}

func TestRender_StatusOK(t *testing.T) {
	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{
			Running:        true,
			SecurityStatus: "ok",
			UptimeMs:       60000,
			ThreatCount:    3,
		},
	}
	got := Render(state, RenderContext{}, 20)
	if !strings.Contains(got, "OK") {
		t.Errorf("expected 'OK' uppercase status, got %q", got)
	}
	if !strings.Contains(got, "1m0s") {
		t.Errorf("expected uptime '1m0s', got %q", got)
	}
	if !strings.Contains(got, "Threats in memory: 3") {
		t.Errorf("expected threat count, got %q", got)
	}
	if !strings.Contains(got, "All processes behaving normally") {
		t.Errorf("expected normal-behavior message, got %q", got)
	}
}

func TestRender_StatusWarningWithAlerts(t *testing.T) {
	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{
			Running:        true,
			SecurityStatus: "warning",
			UptimeMs:       60000,
			SuspendedPIDs:  []uint64{42, 43},
		},
		Alerts: []ipc.AlertWire{
			{PID: 42, AgentTemplate: "agent-x", Type: "syscall_freq", Deviation: 2.5,
				TimestampMs: time.Now().Add(-30 * time.Second).UnixMilli(),
				Detail:      "high syscall rate"},
		},
	}
	got := Render(state, RenderContext{ASCII: true}, 20)
	if !strings.Contains(got, "1 alerts, 2 suspended") {
		t.Errorf("expected counts in summary, got %q", got)
	}
	if !strings.Contains(got, "ALERTS") {
		t.Errorf("expected ALERTS section, got %q", got)
	}
	if !strings.Contains(got, "PID:42") {
		t.Errorf("expected PID:42 row, got %q", got)
	}
	if !strings.Contains(got, "high syscall rate") {
		t.Errorf("expected detail line, got %q", got)
	}
	if !strings.Contains(got, "SUSPENDED") {
		t.Errorf("expected SUSPENDED section, got %q", got)
	}
}

func TestRender_ASCIIWarnIcon(t *testing.T) {
	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{
			Running:        true,
			SecurityStatus: "warning",
		},
	}
	got := Render(state, RenderContext{ASCII: true}, 20)
	if !strings.Contains(got, "!") {
		t.Errorf("ASCII mode should use '!' warn icon, got %q", got)
	}
	gotUnicode := Render(state, RenderContext{ASCII: false}, 20)
	if !strings.Contains(gotUnicode, "⚠") {
		t.Errorf("Unicode mode should use ⚠ warn icon, got %q", gotUnicode)
	}
}

func TestRender_ScrollOffsetIndicators(t *testing.T) {
	alerts := make([]ipc.AlertWire, 20)
	for i := range alerts {
		alerts[i] = ipc.AlertWire{PID: uint64(i), Type: "syscall_freq", Deviation: 1.0,
			TimestampMs: time.Now().UnixMilli()}
	}
	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{
			Running:        true,
			SecurityStatus: "warning",
		},
		Alerts:       alerts,
		ScrollOffset: 5, // skip first 5
	}
	got := Render(state, RenderContext{}, 12) // visible alerts ~6
	if !strings.Contains(got, "more above") {
		t.Errorf("expected 'more above' indicator when ScrollOffset > 0, got %q", got)
	}
	if !strings.Contains(got, "more below") {
		t.Errorf("expected 'more below' indicator when alerts > visible, got %q", got)
	}
}

func TestRender_CursorHighlight(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("probe")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("lipgloss profile NoColor — skipping ANSI highlight assertion")
	}

	state := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{
			Running:        true,
			SecurityStatus: "warning",
		},
		Alerts: []ipc.AlertWire{
			{PID: 1, Type: "syscall_freq", TimestampMs: time.Now().UnixMilli()},
			{PID: 2, Type: "syscall_freq", TimestampMs: time.Now().UnixMilli()},
		},
		Cursor: 1, // second alert highlighted
	}
	got := Render(state, RenderContext{}, 20)
	// "> " prefix is ANSI-styled when colored; verify "> " marker is present
	if !strings.Contains(got, "> ") {
		t.Errorf("expected '> ' cursor marker, got %q", got)
	}
}
