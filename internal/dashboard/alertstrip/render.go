// Package alertstrip — render.go (Story 38-5 PR11 Step 4(a-2))
//
// Render() 主体迁出自 cmd/rnix/dashboard_alerts.go::renderAlertStrip · 1:1 行为
// 等价（Story 34.4 + 38.2 AC#3 + 38-4 P3/P4 patches 全部保留）。
//
// 接口签名变化：
//   - cmd/rnix renderAlertStrip(*dashboardModel, width, maxLines) string
//   - alertstrip Render(state AlertStripState, width, maxLines int) string
//
// 解耦理由：renderAlertStrip 原仅访问 m.alertStrip.{Events,Expanded,Cursor} ·
// 这 3 个字段都已在 PR11 Step 4(a) cascade fix 中迁出至 AlertStripState · 因此
// 函数签名可纯粹基于 AlertStripState + 几何参数（width / maxLines）· 完全脱耦
// dashboardModel 引用 · 包边界清晰可测。
package alertstrip

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// Render renders the bottom alert strip given the current AlertStripState +
// geometry. Returns empty string when there are no alerts (0 height).
//
// Behaviour contract (preserved from cmd/rnix.renderAlertStrip · zero behavior
// change):
//   - len(state.Events) == 0 → return ""（caller skips the strip entirely）
//   - state.Expanded == false → render up to maxLines (typically 2) + count badge
//   - state.Expanded == true  → render up to maxLines (typically 8) + cursor highlight
//   - Story 38.2 AC#3 (collapsed) badge "✗N ⚠M" right-aligned on first line
//   - Story 38-4 P3 patch: drop badge if width < badgeWidth+5（防 East Asian
//     Width=2 alert icon 被截断）
//   - Story 38-4 P4 patch: appendBadge helper 共享 badge 附加逻辑（first line
//     + overflow row 一致）
//   - ASCII mode: 用 "-" 分隔条 + ">" 光标前缀；TrueColor mode: lipgloss border
//     style + background highlight。
//
// Performance: O(maxLines) over state.Events; bounded by alertStripHeight()
// which clamps to 2 (collapsed) / 8 (expanded). 实际渲染 ≤ 8 行 · 每帧调用安全。
func Render(state AlertStripState, width, maxLines int) string {
	alerts := state.Events
	if len(alerts) == 0 {
		return ""
	}

	ascii := ui.IsASCIIMode()
	visible := minInt(len(alerts), maxLines)
	hasOverflow := len(alerts) > maxLines

	// Story 38.2 AC#3: collapsed strip carries a right-aligned count badge.
	// Expanded mode keeps the cursor-highlight UX and skips the badge.
	var badge string
	if !state.Expanded {
		badge = AlertCountBadge(alerts, ascii)
	}
	badgeWidth := lipgloss.Width(badge)
	// Code-review patch (P3, 2026-05-03): drop badge if width < badgeWidth+5
	// 防 East Asian Width=2 alert icon (🔴/⚠) 被截断。
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
		summaryBudget := maxInt(width-1-badgeWidth-1, 1)
		line = truncateAnsi(line, summaryBudget)
		pad := maxInt(width-1-lipgloss.Width(line)-badgeWidth, 1)
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
		// Code-review patch (P3/P4): use the appendBadge helper.
		if i == 0 && badgeWidth > 0 {
			line = appendBadge(line)
		} else {
			line = truncateAnsi(line, width-1)
		}

		// Highlight cursor line
		if state.Expanded && i == state.Cursor {
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
		w := maxInt(width, 1)
		lpad := minInt(w, 20)
		rpad := maxInt(w-lpad-len(" Alerts "), 0)
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

// maxInt is the package-private 2-arg int max helper (mirror of minInt in model.go).
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncateAnsi truncates an ANSI-styled string to fit within maxWidth display
// columns. Identical semantics to cmd/rnix's truncateAnsi helper · kept here
// to avoid alertstrip → cmd/rnix back-edge.
func truncateAnsi(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}
