package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
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
func buildAlertEvents(events []UnifiedEvent) []UnifiedEvent {
	var alerts []UnifiedEvent
	now := time.Now()
	for _, ev := range events {
		if ev.Severity < SevWarn {
			continue
		}
		if !ev.Timestamp.IsZero() && now.Sub(ev.Timestamp) > alertTTL {
			continue // expired
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

// alertStripHeight returns the number of lines the alert strip should occupy.
func alertStripHeight(alertCount int, expanded bool) int {
	if alertCount == 0 {
		return 0
	}
	if expanded {
		return min(alertCount, 8)
	}
	return min(alertCount, 2)
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
// The icon characters are NOT taken from ui.AlertSeverityIcon — that helper
// renders 🔴/⚠ (full-width emoji) for the alert lines, which would inflate
// badge width. The badge spec uses the slimmer ✗/⚠ glyphs to keep the right
// edge stable across narrow terminals.
func alertCountBadge(alerts []UnifiedEvent, ascii bool) string {
	var errCount, warnCount int
	for _, a := range alerts {
		switch {
		case a.Severity >= SevError:
			errCount++
		case a.Severity == SevWarn:
			warnCount++
		}
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
	alerts := m.alertEvents
	if len(alerts) == 0 {
		return ""
	}

	ascii := ui.IsASCIIMode()
	visible := min(len(alerts), maxLines)
	hasOverflow := len(alerts) > maxLines

	// Story 38.2 AC#3: collapsed strip carries a right-aligned count badge.
	// Expanded mode keeps the cursor-highlight UX and skips the badge.
	var badge string
	if !m.alertExpanded {
		badge = alertCountBadge(alerts, ascii)
	}
	badgeWidth := lipgloss.Width(badge)

	var lines []string
	for i := range visible {
		if hasOverflow && i == visible-1 {
			remaining := len(alerts) - visible + 1
			overflow := fmt.Sprintf("+%d more", remaining)
			if ascii {
				lines = append(lines, overflow)
			} else {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorMuted)).
					Render(overflow))
			}
			break
		}

		alert := alerts[i]
		icon := ui.AlertSeverityIcon(alert.Severity)
		ts := ""
		if !alert.Timestamp.IsZero() {
			ts = ui.FormatWallClockShort(alert.Timestamp) + " "
		}
		line := fmt.Sprintf("%s %s%s", icon, ts, alert.Summary)

		// Story 38.2 AC#3: append badge to the FIRST line, right-aligned. We
		// truncate the summary first so the badge always survives in narrow
		// terminals; the leading "-1" is the existing right-margin reserve.
		if i == 0 && badgeWidth > 0 {
			summaryBudget := max(width-1-badgeWidth-1, 1)
			line = truncateAnsi(line, summaryBudget)
			pad := max(width-1-lipgloss.Width(line)-badgeWidth, 1)
			line = line + strings.Repeat(" ", pad) + badge
		} else {
			line = truncateAnsi(line, width-1)
		}

		// Highlight cursor line
		if m.alertExpanded && i == m.alertCursor {
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
