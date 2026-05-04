// Package security — render.go (Story 38-5 PR11 Step 4(c) · 第 3 个 pane render
// 主体迁出 · 同 alertstrip / detail 模式)
//
// Render() 主体迁出自 cmd/rnix/dashboard_security.go::renderSecurityPane · 1:1
// 行为等价（Story 27-8 落地 Security Pane + 22-1 Immune Daemon + 22-3 Security
// Status 行为契约保留）。
//
// 接口签名变化（与 alertstrip / detail 同模式）：
//   - cmd/rnix renderSecurityPane(*dashboardModel, width, height) string
//   - security Render(state SecurityState, ctx RenderContext, innerH int) string
//
// 解耦理由：renderSecurityPane 原仅依赖 dashboardModel.{security, activePane} ·
// activePane 由 wrapper 注入 RenderContext.IsActive · security 字段已抽出至
// SecurityState（PR7 Step 1 落地）。
//
// helpers (sortAlertsByDeviation / alertTypeColor / securityStatusColor /
// formatUptimeShort / formatTimeAgo) 一并迁入本包（pure functions · 无 cmd/rnix
// 依赖）。
package security

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// RenderContext 注入 cmd/rnix 端运行时上下文（与 alertstrip/detail 同模式）。
type RenderContext struct {
	IsActive bool
	ASCII    bool // RNIX_ASCII=1 模式 · 影响 warn icon 选择（! 或 ⚠）
}

// SortAlertsByDeviation returns a copy of alerts sorted by Deviation descending.
// Public so cmd/rnix tests / external callers can verify the sort behavior.
func SortAlertsByDeviation(alerts []ipc.AlertWire) []ipc.AlertWire {
	if len(alerts) == 0 {
		return nil
	}
	sorted := make([]ipc.AlertWire, len(alerts))
	copy(sorted, alerts)
	sortStable(sorted)
	return sorted
}

// sortStable provides a stable insertion sort (Go's sort.Slice is unstable;
// for deterministic test output we use insertion sort here · O(N²) but N ≤ ~20
// in practice from the immune daemon ring).
func sortStable(alerts []ipc.AlertWire) {
	for i := 1; i < len(alerts); i++ {
		for j := i; j > 0 && alerts[j-1].Deviation < alerts[j].Deviation; j-- {
			alerts[j-1], alerts[j] = alerts[j], alerts[j-1]
		}
	}
}

// AlertTypeColor returns the lipgloss color for a security alert type.
//
// Story 22-3 mapping (preserved from cmd/rnix):
//   - syscall_freq → yellow (220)
//   - token_rate   → orange (208)
//   - device_access → red (196)
//   - default      → gray (240)
func AlertTypeColor(alertType string) lipgloss.Color {
	switch alertType {
	case "syscall_freq":
		return lipgloss.Color("220")
	case "token_rate":
		return lipgloss.Color("208")
	case "device_access":
		return lipgloss.Color("196")
	default:
		return lipgloss.Color("240")
	}
}

// SecurityStatusColor returns the lipgloss color for a security status value.
//
// Mapping (preserved from cmd/rnix):
//   - "ok"       → green (42)
//   - "warning"  → red (196)
//   - default    → gray (240)
func SecurityStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42")
	case "warning":
		return lipgloss.Color("196")
	default:
		return lipgloss.Color("240")
	}
}

// FormatUptimeShort formats a duration in milliseconds as a short string
// (e.g. "5s", "3m12s", "2h15m"). Used for Immune Daemon uptime display.
func FormatUptimeShort(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// FormatTimeAgo returns a human-friendly "Ns ago" / "Nm ago" / "Nh ago" /
// "Nd ago" for a Unix milliseconds timestamp. Returns "" when ts <= 0.
func FormatTimeAgo(timestampMs int64) string {
	if timestampMs <= 0 {
		return ""
	}
	elapsed := time.Since(time.UnixMilli(timestampMs))
	switch {
	case elapsed < time.Minute:
		s := max(int(elapsed.Seconds()), 0)
		return fmt.Sprintf("%ds ago", s)
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

// Render renders the Security pane inner content (excluding outer border).
//
// Behaviour contract (preserved from cmd/rnix.renderSecurityPane · zero behavior
// change · spec § AC5 + Story 22-3 + 27-8 + 38-4 Alert Immune routing context):
//   - state.ImmuneStatus == nil: Loading or Error state
//   - !ImmuneStatus.Running (AC-7): "Immune Daemon not running"
//   - SecurityStatus == "ok": green status summary
//   - SecurityStatus == "warning": red summary + Alert list (AC-3) +
//     Suspended processes list (AC-6) with cursor + scroll offset
//
// Performance: O(visibleAlerts) · 实际渲染 ≤ ~10 alerts (innerH-6 reserved
// lines).
func Render(state SecurityState, ctx RenderContext, innerH int) string {
	var b strings.Builder
	b.WriteString(" Security \n")

	// nil guard: data not yet fetched
	if state.ImmuneStatus == nil {
		if state.ImmuneErr != nil {
			fmt.Fprintf(&b, " Error: %v\n", state.ImmuneErr)
		} else {
			b.WriteString(" Loading...\n")
		}
		return b.String()
	}

	// AC-7: Immune Daemon not running
	if !state.ImmuneStatus.Running {
		b.WriteString(" Immune Daemon not running.\n")
		b.WriteString(" Security monitoring unavailable.\n")
		return b.String()
	}

	// AC-5: Security status summary
	statusStr := state.ImmuneStatus.SecurityStatus
	statusStyle := lipgloss.NewStyle().Foreground(SecurityStatusColor(statusStr))

	if statusStr == "ok" {
		b.WriteString(statusStyle.Render(fmt.Sprintf(" Security: %s", strings.ToUpper(statusStr))))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", FormatUptimeShort(state.ImmuneStatus.UptimeMs))
		fmt.Fprintf(&b, " Threats in memory: %d\n", state.ImmuneStatus.ThreatCount)
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(" All processes behaving normally"))
		b.WriteString("\n")
		return b.String()
	}

	// warning status
	alertCount := len(state.Alerts)
	suspendedCount := len(state.ImmuneStatus.SuspendedPIDs)
	warnIcon := "!"
	if !ctx.ASCII {
		warnIcon = "⚠"
	}
	summary := fmt.Sprintf(" %s Security: %d alerts, %d suspended", warnIcon, alertCount, suspendedCount)
	b.WriteString(statusStyle.Render(summary))
	b.WriteString("\n")
	fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", FormatUptimeShort(state.ImmuneStatus.UptimeMs))
	b.WriteString("\n")

	// AC-3: Alert list (with scroll offset)
	if alertCount > 0 {
		b.WriteString(" ALERTS\n")
		// Calculate visible range based on scroll offset
		visibleAlerts := max(innerH-6, 1)
		startIdx := state.ScrollOffset
		endIdx := min(startIdx+visibleAlerts, alertCount)
		if startIdx > 0 {
			fmt.Fprintf(&b, " ... %d more above\n", startIdx)
		}
		for i := startIdx; i < endIdx; i++ {
			alert := state.Alerts[i]
			cursor := "  "
			if i == state.Cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Render("> ")
			}
			typeStyle := lipgloss.NewStyle().Foreground(AlertTypeColor(alert.Type))
			ago := FormatTimeAgo(alert.TimestampMs)
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
		for _, pid := range state.ImmuneStatus.SuspendedPIDs {
			fmt.Fprintf(&b, "   PID:%d → resume/kill\n", pid)
		}
	}

	return b.String()
}
