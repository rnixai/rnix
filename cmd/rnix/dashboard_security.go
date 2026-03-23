package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Security Pane (Story 27-8)
// =============================================================================

func fetchImmuneStatusCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return immuneStatusMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ImmuneStatus()
		return immuneStatusMsg{status: resp, err: err}
	}
}

func sortAlertsByDeviation(alerts []ipc.AlertWire) []ipc.AlertWire {
	if len(alerts) == 0 {
		return nil
	}
	sorted := make([]ipc.AlertWire, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Deviation > sorted[j].Deviation
	})
	return sorted
}

func alertTypeColor(alertType string) lipgloss.Color {
	switch alertType {
	case "syscall_freq":
		return lipgloss.Color("220") // yellow
	case "token_rate":
		return lipgloss.Color("208") // orange
	case "device_access":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("240") // gray
	}
}

func securityStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42") // green
	case "warning":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("240") // gray
	}
}

func formatUptimeShort(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func (m dashboardModel) renderSecurityPane(width, height int) string {
	isActive := m.activePane == paneSecurity

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	var b strings.Builder
	b.WriteString(" Security \n")

	// nil guard: data not yet fetched
	if m.immuneStatus == nil {
		if m.immuneErr != nil {
			fmt.Fprintf(&b, " Error: %v\n", m.immuneErr)
		} else {
			b.WriteString(" Loading...\n")
		}
		return style.Render(b.String())
	}

	// AC-7: Immune Daemon not running
	if !m.immuneStatus.Running {
		b.WriteString(" Immune Daemon not running.\n")
		b.WriteString(" Security monitoring unavailable.\n")
		return style.Render(b.String())
	}

	// AC-5: Security status summary
	statusStr := m.immuneStatus.SecurityStatus
	statusStyle := lipgloss.NewStyle().Foreground(securityStatusColor(statusStr))

	if statusStr == "ok" {
		b.WriteString(statusStyle.Render(fmt.Sprintf(" Security: %s", strings.ToUpper(statusStr))))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", formatUptimeShort(m.immuneStatus.UptimeMs))
		fmt.Fprintf(&b, " Threats in memory: %d\n", m.immuneStatus.ThreatCount)
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(" All processes behaving normally"))
		b.WriteString("\n")
	} else {
		// warning status
		alertCount := len(m.securityAlerts)
		suspendedCount := len(m.immuneStatus.SuspendedPIDs)
		warnIcon := "!"
		ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
		if !ascii {
			warnIcon = "⚠"
		}
		summary := fmt.Sprintf(" %s Security: %d alerts, %d suspended", warnIcon, alertCount, suspendedCount)
		b.WriteString(statusStyle.Render(summary))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", formatUptimeShort(m.immuneStatus.UptimeMs))
		b.WriteString("\n")

		// AC-3: Alert list (with scroll offset)
		if alertCount > 0 {
			b.WriteString(" ALERTS\n")
			// Calculate visible range based on scroll offset
			visibleAlerts := max(innerH-6, 1) // reserve lines for header/summary/suspended
			startIdx := m.securityScrollOffset
			endIdx := min(startIdx+visibleAlerts, alertCount)
			if startIdx > 0 {
				fmt.Fprintf(&b, " ... %d more above\n", startIdx)
			}
			for i := startIdx; i < endIdx; i++ {
				alert := m.securityAlerts[i]
				cursor := "  "
				if i == m.securityCursor {
					cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Render("> ")
				}
				typeStyle := lipgloss.NewStyle().Foreground(alertTypeColor(alert.Type))
				ago := formatTimeAgo(alert.TimestampMs)
				line := fmt.Sprintf("%sPID:%-4d %-16s %s  (%.1fx)  %s",
					cursor,
					alert.PID,
					alert.AgentTemplate,
					typeStyle.Render(alert.Type),
					alert.Deviation,
					ago,
				)
				b.WriteString(line)
				b.WriteString("\n")
				// Detail line
				if alert.Detail != "" {
					fmt.Fprintf(&b, "    %s\n", alert.Detail)
				}
			}
			if endIdx < alertCount {
				fmt.Fprintf(&b, " ... %d more below\n", alertCount-endIdx)
			}
		}

		// AC-6: Suspended processes
		if suspendedCount > 0 {
			b.WriteString("\n SUSPENDED\n")
			for _, pid := range m.immuneStatus.SuspendedPIDs {
				fmt.Fprintf(&b, "   PID:%d → resume/kill\n", pid)
			}
		}
	}

	return style.Render(b.String())
}

func formatTimeAgo(timestampMs int64) string {
	if timestampMs <= 0 {
		return ""
	}
	elapsed := time.Since(time.UnixMilli(timestampMs))
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(elapsed.Seconds()), 0))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

// securityAdjustScroll ensures securityCursor is visible within the viewport.
func securityAdjustScroll(m *dashboardModel) {
	visibleLines := max(m.height/2-3, 1)
	if m.securityCursor < m.securityScrollOffset {
		m.securityScrollOffset = m.securityCursor
	}
	if m.securityCursor >= m.securityScrollOffset+visibleLines {
		m.securityScrollOffset = m.securityCursor - visibleLines + 1
	}
}
