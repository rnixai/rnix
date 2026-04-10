package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// buildAlertEvents filters unified events with Severity >= SevWarn,
// sorts by Severity descending then Timestamp descending.
// Called after mergeUnifiedEvents to cache alerts for the strip.
func buildAlertEvents(events []UnifiedEvent) []UnifiedEvent {
	var alerts []UnifiedEvent
	for _, ev := range events {
		if ev.Severity >= SevWarn {
			alerts = append(alerts, ev)
		}
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
		line := fmt.Sprintf("%s %s", icon, alert.Summary)
		line = truncateAnsi(line, width-1)

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
