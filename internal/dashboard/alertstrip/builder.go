// Package alertstrip — builder.go (Story 38-5 PR11 Step 4(a-2))
//
// Pure helper functions migrated from cmd/rnix/dashboard_alerts.go after the
// UnifiedEvent type cascade was unblocked by commit a08ae3d (PR11 Step 4(a)).
//
// 行为契约保留（关键 · 全部从 cmd/rnix 1:1 迁出 · 零行为变更）：
//   - alertTTL = 30s（Story 38.2 AC#4）
//   - IsSynthetic flag 跳过 TTL filter（Story 38-4 P0 patch）
//   - Severity descending → Timestamp descending 排序（buildAlertEvents）
//   - synthSecurityAlerts 严重度映射（device_access/deviation≥3.0 → SevError）
//   - alertCountBadge ✗N ⚠M 右对齐 + ASCII fallback（Story 38.2 AC#3）
//
// cmd/rnix 端通过 thin wrapper（dashboard_alerts.go）保留旧函数名给现有 caller，
// 与 PR2/PR3 helper 公开化同模式。
package alertstrip

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// AlertTTL caps how long an alert stays in the strip after its event Timestamp.
//
// Rationale (Story 38.2 AC#4): the dashboard is a real-time monitoring view —
// 30s is long enough for a human to spot a flash but short enough to keep the
// strip from accumulating stale visual noise. The Story spec HTML mentions a
// 5-minute TTL but the binding Epic AC chooses 30s.
const AlertTTL = 30 * time.Second

// BuildAlertEvents filters unified events with Severity >= SevWarn,
// drops alerts whose Timestamp is older than AlertTTL,
// then sorts by Severity descending then Timestamp descending.
//
// Zero-value Timestamps (time.Time{}) are NEVER filtered: tests and synthetic
// events frequently leave Timestamp unset, and silently dropping them would
// mask real bugs (Story 38.2 AC#4 IsZero guard).
//
// Code-review patch P0 (Story 38-4, 2026-05-03): rows with IsSynthetic == true
// (currently only synthSecurityAlerts) bypass the TTL filter entirely. Their
// lifecycle is driven by the upstream IPC list (m.security.Alerts) rather than
// wall-clock TTL — this fixes both (a) `TimestampMs == 0` synth rows
// previously latching forever via a `time.Now()` fallback and (b) real Immune
// alerts older than 30 s being silently stripped.
func BuildAlertEvents(events []event.UnifiedEvent) []event.UnifiedEvent {
	var alerts []event.UnifiedEvent
	now := time.Now()
	for _, ev := range events {
		if ev.Severity < event.SevWarn {
			continue
		}
		if !ev.IsSynthetic && !ev.Timestamp.IsZero() && now.Sub(ev.Timestamp) > AlertTTL {
			continue // expired (synthetic rows are immune to TTL — see godoc)
		}
		alerts = append(alerts, ev)
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Severity != alerts[j].Severity {
			return alerts[i].Severity > alerts[j].Severity
		}
		return alerts[i].Timestamp.After(alerts[j].Timestamp)
	})
	return alerts
}

// SynthSecurityAlerts converts the IPC AlertWire list (from Immune daemon)
// into transient UnifiedEvent rows for the alert strip (Story 38-4 AC#4).
//
// Mapping rules:
//   - Type   = EventImmune (existing constant; reused so downstream logic
//     such as the AC#2 alert-enter routing can branch on the same type).
//   - Severity = SevError when type == "device_access" or deviation ≥ 3.0;
//     otherwise SevWarn. Rationale: device_access is the tightest signal
//     in the immune daemon; high-deviation syscall_freq / token_rate pile
//     up to error severity per the spec.
//   - Timestamp = time.UnixMilli(TimestampMs); when TimestampMs == 0 the
//     UnifiedEvent.Timestamp is left as the zero value (time.Time{}).
//   - IsSynthetic = true so BuildAlertEvents bypasses the TTL filter
//     (code-review patch P0, 2026-05-03).
//   - PID / Detail / Summary follow the spec template.
//
// Performance: O(N) over alerts; in practice N ≤ ~20 (immune daemon caps
// the alert ring).
//
// IMPORTANT: the result is NEVER persisted into m.sysEvents. Callers should
// pass the slice straight to BuildAlertEventsWith so that Timeline pane is
// not polluted with synthetic immune entries.
func SynthSecurityAlerts(alerts []ipc.AlertWire) []event.UnifiedEvent {
	if len(alerts) == 0 {
		return nil
	}
	out := make([]event.UnifiedEvent, 0, len(alerts))
	for _, a := range alerts {
		sev := event.SevWarn
		if a.Type == "device_access" || a.Deviation >= 3.0 {
			sev = event.SevError
		}
		var ts time.Time
		if a.TimestampMs > 0 {
			ts = time.UnixMilli(a.TimestampMs)
		}
		out = append(out, event.UnifiedEvent{
			Type:        event.EventImmune,
			Severity:    sev,
			Timestamp:   ts,
			PID:         types.PID(a.PID),
			Summary:     fmt.Sprintf("[security] %s %s (%.1fx)", a.AgentTemplate, a.Type, a.Deviation),
			Detail:      a.Detail,
			IsSynthetic: true,
		})
	}
	return out
}

// BuildAlertEventsWith is the AC#4 extension entry point: merges a slice
// of UnifiedEvents with synthesised security alerts and runs them through
// the same TTL filter + severity sort as BuildAlertEvents.
//
// The legacy BuildAlertEvents signature is preserved unchanged; callers
// who don't care about security events can still use it directly. This
// keeps Story 38-2 tests free of the new dependency.
//
// When securityAlerts is empty, this is identical to BuildAlertEvents
// over the same `events` input (regression-safe).
func BuildAlertEventsWith(events []event.UnifiedEvent, securityAlerts []ipc.AlertWire) []event.UnifiedEvent {
	synth := SynthSecurityAlerts(securityAlerts)
	if len(synth) == 0 {
		return BuildAlertEvents(events)
	}
	combined := make([]event.UnifiedEvent, 0, len(events)+len(synth))
	combined = append(combined, events...)
	combined = append(combined, synth...)
	return BuildAlertEvents(combined)
}

// AlertCountBadge renders the right-aligned count badge "✗N ⚠M" (or
// "XN !M" in ASCII mode) for the collapsed alert strip (Story 38.2 AC#3).
//
// Behaviour:
//   - error count == 0 → drop the ✗ segment (only ⚠ shown)
//   - warn count == 0 → drop the ⚠ segment (only ✗ shown)
//   - both 0          → empty string (caller's len(alerts)==0 path already
//     skips the strip entirely; this is a safety net)
//
// Severity bucketing:
//   - Severity ≥ SevError (e.g. SevError, SevCritical) → errCount
//   - Any other severity that survived BuildAlertEvents (≥ SevWarn but
//     < SevError) → warnCount
//
// The icon characters are NOT taken from ui.AlertSeverityIcon — that helper
// renders 🔴/⚠ (full-width emoji) for the alert lines, which would inflate
// badge width. The badge spec uses the slimmer ✗/⚠ glyphs to keep the right
// edge stable across narrow terminals.
func AlertCountBadge(alerts []event.UnifiedEvent, ascii bool) string {
	var errCount, warnCount int
	for _, a := range alerts {
		if a.Severity >= event.SevError {
			errCount++
			continue
		}
		// Any severity that passed BuildAlertEvents (i.e. ≥ SevWarn) but is
		// not ≥ SevError lands in the warn bucket. Using `default` here
		// (rather than `case == SevWarn`) ensures future enum additions in
		// the SevWarn..SevError range do not silently disappear from the
		// badge total.
		warnCount++
	}
	if errCount == 0 && warnCount == 0 {
		return ""
	}

	errIcon := "✗"
	warnIcon := "⚠"
	if ascii {
		errIcon = "X"
		warnIcon = "!"
	}

	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	parts := make([]string, 0, 2)
	if errCount > 0 {
		parts = append(parts, errStyle.Render(fmt.Sprintf("%s%d", errIcon, errCount)))
	}
	if warnCount > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf("%s%d", warnIcon, warnCount)))
	}
	return strings.Join(parts, " ")
}
