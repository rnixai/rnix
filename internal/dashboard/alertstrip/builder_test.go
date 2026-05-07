// Package alertstrip — builder_test.go (Story 38-5 PR11 Step 4(a-2))
//
// 验证 BuildAlertEvents / SynthSecurityAlerts / BuildAlertEventsWith /
// AlertCountBadge 4 个 helper 的行为契约（与 cmd/rnix 端 1:1 等价）：
//
//   - BuildAlertEvents 过滤 ≥ SevWarn + TTL filter + IsSynthetic bypass
//   - SynthSecurityAlerts severity 映射 + Type=EventImmune + IsSynthetic=true
//   - BuildAlertEventsWith merge + 排序
//   - AlertCountBadge severity 分桶 + ASCII fallback + 空 alerts → ""
package alertstrip

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// --- BuildAlertEvents ---

func TestBuildAlertEvents_FiltersBelowWarn(t *testing.T) {
	now := time.Now()
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevInfo, Timestamp: now, Summary: "info-event"},
		{Type: event.EventStep, Severity: event.SevWarn, Timestamp: now, Summary: "warn-event"},
		{Type: event.EventStep, Severity: event.SevError, Timestamp: now, Summary: "error-event"},
		{Type: event.EventStep, Severity: event.SevCritical, Timestamp: now, Summary: "critical-event"},
	}
	got := BuildAlertEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 alerts (≥ SevWarn), got %d", len(got))
	}
	// Sorted by Severity descending: Critical → Error → Warn
	if got[0].Severity != event.SevCritical {
		t.Errorf("expected first alert SevCritical, got %d (%q)", got[0].Severity, got[0].Summary)
	}
	if got[1].Severity != event.SevError {
		t.Errorf("expected second alert SevError, got %d", got[1].Severity)
	}
	if got[2].Severity != event.SevWarn {
		t.Errorf("expected third alert SevWarn, got %d", got[2].Severity)
	}
}

func TestBuildAlertEvents_DropsExpiredByTTL(t *testing.T) {
	old := time.Now().Add(-1 * time.Hour) // far older than 30s TTL
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevError, Timestamp: old, Summary: "old-error"},
	}
	got := BuildAlertEvents(events)
	if len(got) != 0 {
		t.Errorf("expected 0 alerts (TTL expired), got %d", len(got))
	}
}

func TestBuildAlertEvents_KeepsZeroTimestamp(t *testing.T) {
	// Story 38.2 AC#4 IsZero guard: zero-time events are NEVER filtered
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevError, Summary: "no-timestamp"},
	}
	got := BuildAlertEvents(events)
	if len(got) != 1 {
		t.Errorf("expected 1 alert (zero timestamp kept), got %d", len(got))
	}
}

func TestBuildAlertEvents_SyntheticBypassesTTL(t *testing.T) {
	// Story 38-4 P0 patch: IsSynthetic=true bypasses TTL filter
	old := time.Now().Add(-1 * time.Hour)
	events := []event.UnifiedEvent{
		{Type: event.EventImmune, Severity: event.SevError, Timestamp: old,
			Summary: "old-synth", IsSynthetic: true},
	}
	got := BuildAlertEvents(events)
	if len(got) != 1 {
		t.Errorf("synthetic event should bypass TTL filter; got %d alerts", len(got))
	}
}

func TestBuildAlertEvents_StableSortByTimestamp(t *testing.T) {
	t1 := time.Now().Add(-5 * time.Second)
	t2 := time.Now().Add(-3 * time.Second)
	t3 := time.Now().Add(-1 * time.Second)
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevWarn, Timestamp: t1, Summary: "old"},
		{Type: event.EventStep, Severity: event.SevWarn, Timestamp: t3, Summary: "newest"},
		{Type: event.EventStep, Severity: event.SevWarn, Timestamp: t2, Summary: "middle"},
	}
	got := BuildAlertEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(got))
	}
	// Same severity → sorted by timestamp descending (newest first)
	if got[0].Summary != "newest" || got[1].Summary != "middle" || got[2].Summary != "old" {
		t.Errorf("expected newest→middle→old, got [%s, %s, %s]",
			got[0].Summary, got[1].Summary, got[2].Summary)
	}
}

func TestBuildAlertEvents_EmptyInput(t *testing.T) {
	got := BuildAlertEvents(nil)
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
	got = BuildAlertEvents([]event.UnifiedEvent{})
	if got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

// --- SynthSecurityAlerts ---

func TestSynthSecurityAlerts_EmptyInput(t *testing.T) {
	if got := SynthSecurityAlerts(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := SynthSecurityAlerts([]ipc.AlertWire{}); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestSynthSecurityAlerts_SeverityMapping(t *testing.T) {
	cases := []struct {
		name      string
		alertType string
		deviation float64
		want      int
	}{
		{"device_access", "device_access", 1.0, event.SevError},
		{"high_deviation", "syscall_freq", 5.0, event.SevError},
		{"medium_deviation", "syscall_freq", 2.0, event.SevWarn},
		{"low_deviation", "token_rate", 0.5, event.SevWarn},
	}
	for _, c := range cases {
		alerts := []ipc.AlertWire{
			{Type: c.alertType, Deviation: c.deviation, PID: 42, AgentTemplate: "test"},
		}
		got := SynthSecurityAlerts(alerts)
		if len(got) != 1 {
			t.Errorf("%s: expected 1 alert, got %d", c.name, len(got))
			continue
		}
		if got[0].Severity != c.want {
			t.Errorf("%s: expected Severity=%d, got %d", c.name, c.want, got[0].Severity)
		}
	}
}

func TestSynthSecurityAlerts_TypeAndFlags(t *testing.T) {
	alerts := []ipc.AlertWire{
		{Type: "device_access", PID: 42, AgentTemplate: "agent-x", Detail: "blocked /dev/foo"},
	}
	got := SynthSecurityAlerts(alerts)
	if len(got) != 1 {
		t.Fatalf("expected 1 alert")
	}
	if got[0].Type != event.EventImmune {
		t.Errorf("expected Type=EventImmune, got %q", got[0].Type)
	}
	if !got[0].IsSynthetic {
		t.Errorf("expected IsSynthetic=true")
	}
	if got[0].PID != types.PID(42) {
		t.Errorf("expected PID=42, got %d", got[0].PID)
	}
	if got[0].Detail != "blocked /dev/foo" {
		t.Errorf("expected Detail forwarded, got %q", got[0].Detail)
	}
}

func TestSynthSecurityAlerts_TimestampMs(t *testing.T) {
	tsMs := int64(1714780800000) // some recent ms
	alerts := []ipc.AlertWire{
		{Type: "device_access", TimestampMs: tsMs},
		{Type: "device_access", TimestampMs: 0}, // zero → time.Time{} zero value
	}
	got := SynthSecurityAlerts(alerts)
	if len(got) != 2 {
		t.Fatalf("expected 2 alerts")
	}
	if got[0].Timestamp.UnixMilli() != tsMs {
		t.Errorf("expected Timestamp from UnixMilli(%d), got %v", tsMs, got[0].Timestamp)
	}
	if !got[1].Timestamp.IsZero() {
		t.Errorf("expected zero Timestamp when TimestampMs==0, got %v", got[1].Timestamp)
	}
}

// --- BuildAlertEventsWith ---

func TestBuildAlertEventsWith_NoSecurityAlerts(t *testing.T) {
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevWarn, Summary: "warn"},
	}
	got := BuildAlertEventsWith(events, nil)
	want := BuildAlertEvents(events)
	if len(got) != len(want) {
		t.Errorf("with empty securityAlerts should equal BuildAlertEvents(events): got %d vs want %d",
			len(got), len(want))
	}
}

func TestBuildAlertEventsWith_MergesAndSorts(t *testing.T) {
	events := []event.UnifiedEvent{
		{Type: event.EventStep, Severity: event.SevWarn, Summary: "step-warn"},
	}
	secAlerts := []ipc.AlertWire{
		{Type: "device_access", PID: 1, AgentTemplate: "x"}, // SevError, IsSynthetic=true
	}
	got := BuildAlertEventsWith(events, secAlerts)
	if len(got) != 2 {
		t.Fatalf("expected 2 merged alerts, got %d", len(got))
	}
	// SevError (Immune) should sort before SevWarn (step)
	if got[0].Type != event.EventImmune {
		t.Errorf("expected first alert Type=EventImmune (sorted by severity desc), got %q", got[0].Type)
	}
}

// --- AlertCountBadge ---

func TestAlertCountBadge_EmptyAlerts(t *testing.T) {
	if got := AlertCountBadge(nil, false); got != "" {
		t.Errorf("expected empty badge for nil alerts, got %q", got)
	}
	if got := AlertCountBadge([]event.UnifiedEvent{}, false); got != "" {
		t.Errorf("expected empty badge for empty alerts, got %q", got)
	}
}

func TestAlertCountBadge_OnlyWarn(t *testing.T) {
	alerts := []event.UnifiedEvent{
		{Severity: event.SevWarn},
		{Severity: event.SevWarn},
	}
	got := AlertCountBadge(alerts, true) // ASCII mode
	if !strings.Contains(got, "!2") {
		t.Errorf("expected ASCII warn badge !2, got %q", got)
	}
	if strings.Contains(got, "X") {
		t.Errorf("expected no error icon when warnCount==0, got %q", got)
	}
}

func TestAlertCountBadge_OnlyError(t *testing.T) {
	alerts := []event.UnifiedEvent{
		{Severity: event.SevError},
		{Severity: event.SevCritical},
	}
	got := AlertCountBadge(alerts, true) // ASCII mode
	if !strings.Contains(got, "X2") {
		t.Errorf("expected ASCII error badge X2, got %q", got)
	}
}

func TestAlertCountBadge_BothErrAndWarn(t *testing.T) {
	alerts := []event.UnifiedEvent{
		{Severity: event.SevError},
		{Severity: event.SevWarn},
		{Severity: event.SevCritical},
	}
	got := AlertCountBadge(alerts, true) // ASCII mode
	if !strings.Contains(got, "X2") || !strings.Contains(got, "!1") {
		t.Errorf("expected both X2 (err) and !1 (warn), got %q", got)
	}
}

func TestAlertCountBadge_UnicodeMode(t *testing.T) {
	alerts := []event.UnifiedEvent{
		{Severity: event.SevError},
		{Severity: event.SevWarn},
	}
	got := AlertCountBadge(alerts, false) // Unicode mode
	if !strings.Contains(got, "✗") {
		t.Errorf("expected unicode ✗ icon, got %q", got)
	}
	if !strings.Contains(got, "⚠") {
		t.Errorf("expected unicode ⚠ icon, got %q", got)
	}
}

func TestAlertTTL_30Seconds(t *testing.T) {
	// Story 38.2 AC#4 contract: 30s TTL
	if AlertTTL != 30*time.Second {
		t.Errorf("expected AlertTTL=30s, got %v", AlertTTL)
	}
}
