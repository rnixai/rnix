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
func (m dashboardModel) renderHelpOverlay() string {
	w := m.width
	h := m.height
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 40
	}

	groupTitle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true)
	keyCol := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true).Width(10)
	descCol := lipgloss.NewStyle().Width(40)
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

	var b strings.Builder
	for i, g := range groups {
		header := g.Layer
		if g.Name != "" && g.Name != g.Layer {
			header = g.Layer + " — " + g.Name
		}
		b.WriteString(groupTitle.Render(header))
		b.WriteString("\n")
		for _, doc := range g.Docs {
			b.WriteString(entry(doc.Key, doc.Description, doc.ContextNote))
			b.WriteString("\n")
		}
		if i < len(groups)-1 {
			b.WriteString("\n")
		}
	}

	if len(groups) == 0 {
		b.WriteString(dimStyle.Render("(no key bindings registered for this view/pane)\n"))
	}

	footer := dimStyle.Render("Press ? or Esc to close")
	footerCentered := lipgloss.NewStyle().Width(max(w-4, 20)).Align(lipgloss.Center).Render(footer)

	innerW := max(w-4, 20)
	innerH := max(h-4, 10)

	content := lipgloss.JoinVertical(lipgloss.Left, b.String(), footerCentered)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorAgent)).
		Width(innerW).
		Height(innerH).
		Padding(1, 2)

	title := "  Keyboard Shortcuts"
	return lipgloss.JoinVertical(lipgloss.Left, title, box.Render(content))
}
