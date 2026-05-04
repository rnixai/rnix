package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/alertstrip"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// alertTTL caps how long an alert stays in the strip after its event Timestamp.
//
// Rationale (Story 38.2 AC#4): the dashboard is a real-time monitoring view —
// 30s is long enough for a human to spot a flash but short enough to keep the
// strip from accumulating stale visual noise. The Story spec HTML mentions a
// 5-minute TTL but the binding Epic AC chooses 30s; the variable name keeps
// the threshold easy to revisit in retrospective.
const alertTTL = 30 * time.Second

// buildAlertEvents filters unified events with Severity >= SevWarn,
// drops alerts whose Timestamp is older than alertTTL,
// then sorts by Severity descending then Timestamp descending.
// Called after mergeUnifiedEvents to cache alerts for the strip.
//
// Zero-value Timestamps (time.Time{}) are NEVER filtered: tests and synthetic
// events frequently leave Timestamp unset, and silently dropping them would
// mask real bugs (Story 38.2 AC#4 IsZero guard).
//
// Code-review patch P0 (Story 38-4, 2026-05-03): rows with IsSynthetic == true
// (currently only synthSecurityAlerts) bypass the TTL filter entirely. Their
// lifecycle is driven by the upstream IPC list (m.securityAlerts) rather than
// wall-clock TTL — this fixes both (a) `TimestampMs == 0` synth rows
// previously latching forever via a `time.Now()` fallback and (b) real Immune
// alerts older than 30 s being silently stripped.
func buildAlertEvents(events []UnifiedEvent) []UnifiedEvent {
	var alerts []UnifiedEvent
	now := time.Now()
	for _, ev := range events {
		if ev.Severity < SevWarn {
			continue
		}
		if !ev.IsSynthetic && !ev.Timestamp.IsZero() && now.Sub(ev.Timestamp) > alertTTL {
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

// synthSecurityAlerts converts the IPC AlertWire list (from Immune daemon)
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
//   - IsSynthetic = true so buildAlertEvents bypasses the TTL filter
//     (code-review patch P0, 2026-05-03). Synth row lifecycle is driven
//     by m.securityAlerts presence rather than wall-clock TTL — this
//     simultaneously fixes "TimestampMs==0 latches forever via time.Now()
//     fallback" and "real Immune alerts older than 30s silently dropped".
//   - PID / Detail / Summary follow the spec template.
//
// Performance: O(N) over alerts; in practice N ≤ ~20 (immune daemon caps
// the alert ring).
//
// IMPORTANT (Dev Notes 2): the result is NEVER persisted into m.sysEvents.
// Callers should pass the slice straight to buildAlertEventsWith so that
// Timeline pane is not polluted with synthetic immune entries (existing
// EventImmune coverage already flows through debugStraceCh / sysEvents
// independently).
func synthSecurityAlerts(alerts []ipc.AlertWire) []UnifiedEvent {
	if len(alerts) == 0 {
		return nil
	}
	out := make([]UnifiedEvent, 0, len(alerts))
	for _, a := range alerts {
		sev := SevWarn
		if a.Type == "device_access" || a.Deviation >= 3.0 {
			sev = SevError
		}
		var ts time.Time
		if a.TimestampMs > 0 {
			ts = time.UnixMilli(a.TimestampMs)
		}
		out = append(out, UnifiedEvent{
			Type:        EventImmune,
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

// buildAlertEventsWith is the AC#4 extension entry point: merges a slice
// of UnifiedEvents with synthesised security alerts and runs them through
// the same TTL filter + severity sort as buildAlertEvents.
//
// The legacy buildAlertEvents signature is preserved unchanged; callers
// who don't care about security events can still use it directly. This
// keeps Story 38-2 tests free of the new dependency.
//
// When securityAlerts is empty, this is identical to buildAlertEvents
// over the same `events` input (regression-safe).
func buildAlertEventsWith(events []UnifiedEvent, securityAlerts []ipc.AlertWire) []UnifiedEvent {
	synth := synthSecurityAlerts(securityAlerts)
	if len(synth) == 0 {
		return buildAlertEvents(events)
	}
	combined := make([]UnifiedEvent, 0, len(events)+len(synth))
	combined = append(combined, events...)
	combined = append(combined, synth...)
	return buildAlertEvents(combined)
}

// alertStripHeight returns the number of lines the alert strip should occupy.
//
// Story 38-5 PR12 Step 2: thin wrapper delegating to
// internal/dashboard/alertstrip.AlertStripHeight (UnifiedEvent-independent
// helper migrated to the alertstrip package · 与 PR2/PR3 helper 公开化同模式).
// Existing callers (dashboard.go / dashboard_keylayers.go) call alertStripHeight
// by name unchanged; this wrapper preserves the contract while letting the
// alertstrip package be the source of truth.
func alertStripHeight(alertCount int, expanded bool) int {
	return alertstrip.AlertStripHeight(alertCount, expanded)
}

// alertCountBadge renders the right-aligned count badge "✗N ⚠M" (or
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
//   - Any other severity that survived buildAlertEvents (≥ SevWarn but
//     < SevError) → warnCount
//
// The icon characters are NOT taken from ui.AlertSeverityIcon — that helper
// renders 🔴/⚠ (full-width emoji) for the alert lines, which would inflate
// badge width. The badge spec uses the slimmer ✗/⚠ glyphs to keep the right
// edge stable across narrow terminals.
func alertCountBadge(alerts []UnifiedEvent, ascii bool) string {
	var errCount, warnCount int
	for _, a := range alerts {
		if a.Severity >= SevError {
			errCount++
			continue
		}
		// Any severity that passed buildAlertEvents (i.e. ≥ SevWarn) but is
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

// renderAlertStrip renders the bottom alert strip.
// Returns empty string when there are no alerts (0 height).
func renderAlertStrip(m *dashboardModel, width, maxLines int) string {
	alerts := m.alertStrip.Events
	if len(alerts) == 0 {
		return ""
	}

	ascii := ui.IsASCIIMode()
	visible := min(len(alerts), maxLines)
	hasOverflow := len(alerts) > maxLines

	// Story 38.2 AC#3: collapsed strip carries a right-aligned count badge.
	// Expanded mode keeps the cursor-highlight UX and skips the badge.
	var badge string
	if !m.alertStrip.Expanded {
		badge = alertCountBadge(alerts, ascii)
	}
	badgeWidth := lipgloss.Width(badge)
	// Code-review patch (P3, 2026-05-03): when there is not enough room for
	// `<icon><space><summary><space><badge>` (≈ icon-width + 1 + 1 summary +
	// 1 separator + badge) — concretely if width < badgeWidth+5 — drop the
	// badge entirely so the truncation arithmetic below stays sane and the
	// East Asian Width = 2 alert icon (🔴/⚠) is never amputated.
	if badgeWidth > 0 && width < badgeWidth+5 {
		badge = ""
		badgeWidth = 0
	}

	// appendBadge attaches the right-aligned count badge to a single line,
	// truncating the summary first so the badge always survives. Code-review
	// patch (P4, 2026-05-03): factored out so the overflow row (when
	// `maxLines == 1 && hasOverflow`) can also carry the badge.
	appendBadge := func(line string) string {
		if badgeWidth == 0 {
			return truncateAnsi(line, width-1)
		}
		summaryBudget := max(width-1-badgeWidth-1, 1)
		line = truncateAnsi(line, summaryBudget)
		pad := max(width-1-lipgloss.Width(line)-badgeWidth, 1)
		return line + strings.Repeat(" ", pad) + badge
	}

	var lines []string
	for i := range visible {
		if hasOverflow && i == visible-1 {
			remaining := len(alerts) - visible + 1
			overflow := fmt.Sprintf("+%d more", remaining)
			var ovLine string
			if ascii {
				ovLine = overflow
			} else {
				ovLine = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorMuted)).
					Render(overflow)
			}
			// P4: when maxLines==1 the overflow line IS the only visible line,
			// so attach the badge here too. When maxLines>1 the badge is
			// already on lines[0] and we leave overflow plain.
			if i == 0 && badgeWidth > 0 {
				ovLine = appendBadge(ovLine)
			}
			lines = append(lines, ovLine)
			break
		}

		alert := alerts[i]
		icon := ui.AlertSeverityIcon(alert.Severity)
		ts := ""
		if !alert.Timestamp.IsZero() {
			ts = ui.FormatWallClockShort(alert.Timestamp) + " "
		}
		line := fmt.Sprintf("%s %s%s", icon, ts, alert.Summary)

		// Story 38.2 AC#3: append badge to the FIRST line, right-aligned.
		// Code-review patch (P3/P4): use the appendBadge helper so the badge
		// arithmetic stays consistent with the overflow path above.
		if i == 0 && badgeWidth > 0 {
			line = appendBadge(line)
		} else {
			line = truncateAnsi(line, width-1)
		}

		// Highlight cursor line
		if m.alertStrip.Expanded && i == m.alertStrip.Cursor {
			if ascii {
				line = "> " + line
			} else {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#3D2F2F")).
					Render(line)
			}
		}

		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	if ascii {
		w := max(width, 1)
		lpad := min(w, 20)
		rpad := max(w-lpad-len(" Alerts "), 0)
		separator := strings.Repeat("-", lpad) + " Alerts " + strings.Repeat("-", rpad)
		return separator + "\n" + content
	}

	style := lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color("#2D1F1F")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ui.ColorError)).
		BorderTop(true).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false)

	return style.Render(content)
}
