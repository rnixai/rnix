// Package main — dashboard_help.go (Story 38.1)
//
// 帮助叠加层：从 internal/ui/keylayer.go Dispatcher 自动派生。
// 重构前为 136 行手工硬编码列表；现在为 ~80 行渲染逻辑，键位文档由
// 各 KeyLayer 注册时通过 KeyDoc 元数据声明，避免文档与代码漂移。
package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// renderHelpOverlay renders a full-screen keyboard shortcut reference card,
// derived from the registered Dispatcher's KeyLayers.
//
// 布局策略（M3 修复后）：
//   - 单列阈值：终端宽度 < 90 cols（窄屏一列防溢出）
//   - 双列阈值：终端宽度 ≥ 90 cols（宽屏双列防垂直浪费）
//   - footer 前留空行分隔（恢复 Story 38.1 重构前布局）
func (m dashboardModel) renderHelpOverlay() string {
	w := m.width
	h := m.height
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 40
	}

	useTwoColumns := w >= 90
	colWidth := max(w/2-6, 30)
	if !useTwoColumns {
		colWidth = max(w-8, 30)
	}

	groupTitle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true)
	keyCol := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true).Width(10)
	descCol := lipgloss.NewStyle().Width(max(colWidth-10, 20))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	entry := func(key, desc, ctxNote string) string {
		line := keyCol.Render(key) + descCol.Render(desc)
		if ctxNote != "" {
			line += dimStyle.Render(" — " + ctxNote)
		}
		return line
	}

	var groups []ui.HelpGroup
	if m.dispatcher != nil {
		groups = m.dispatcher.HelpGroupedFor(ui.ViewID(m.viewMode), ui.PaneID(m.activePane))
	}

	// 把所有行拍平成 string slice，便于双列拆分
	var lines []string
	for i, g := range groups {
		header := g.Layer
		if g.Name != "" && g.Name != g.Layer {
			header = g.Layer + " — " + g.Name
		}
		lines = append(lines, groupTitle.Render(header))
		for _, doc := range g.Docs {
			lines = append(lines, entry(doc.Key, doc.Description, doc.ContextNote))
		}
		if i < len(groups)-1 {
			lines = append(lines, "")
		}
	}

	if len(lines) == 0 {
		lines = append(lines, dimStyle.Render("(no key bindings registered for this view/pane)"))
	}

	var body string
	if useTwoColumns && len(lines) > 12 {
		mid := (len(lines) + 1) / 2
		left := strings.Join(lines[:mid], "\n")
		right := strings.Join(lines[mid:], "\n")
		leftStyled := lipgloss.NewStyle().Width(colWidth).Render(left)
		rightStyled := lipgloss.NewStyle().Width(colWidth).Render(right)
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, "  ", rightStyled)
	} else {
		body = strings.Join(lines, "\n")
	}

	footer := dimStyle.Render("Press ? or Esc to close")
	footerCentered := lipgloss.NewStyle().Width(max(w-4, 20)).Align(lipgloss.Center).Render(footer)

	innerW := max(w-4, 20)
	innerH := max(h-4, 10)

	// M3: 在 body 与 footer 之间留空行，恢复重构前的呼吸感
	content := lipgloss.JoinVertical(lipgloss.Left, body, "", footerCentered)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorAgent)).
		Width(innerW).
		Height(innerH).
		Padding(1, 2)

	title := "  Keyboard Shortcuts"
	return lipgloss.JoinVertical(lipgloss.Left, title, box.Render(content))
}
