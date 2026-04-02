package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rnixai/rnix/internal/ui"
)

// renderHelpOverlay renders a full-screen keyboard shortcut reference card.
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
	keyCol := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true).Width(8)
	descCol := lipgloss.NewStyle().Width(26)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	entry := func(key, desc string) string {
		return keyCol.Render(key) + descCol.Render(desc)
	}

	// Build two-column layout
	var left, right strings.Builder

	// Left column
	left.WriteString(groupTitle.Render("Navigation") + "\n")
	left.WriteString(entry("j/k", "Move up / down") + "\n")
	left.WriteString(entry("PgDn/Up", "Page down / up") + "\n")
	left.WriteString(entry("g/G", "Jump to top / bottom") + "\n")
	left.WriteString(entry("Tab", "Cycle panes") + "\n")
	left.WriteString(entry("Enter", "Expand / select") + "\n")
	left.WriteString("\n")
	left.WriteString(groupTitle.Render("Timeline") + "\n")
	left.WriteString(entry("f", "Filter mode") + "\n")
	left.WriteString(entry("v/Enter", "Expand step detail") + "\n")
	left.WriteString(entry("V", "Debug detail (L3)") + "\n")
	left.WriteString(entry("e", "Expand all visible") + "\n")
	left.WriteString(entry("E", "Collapse all visible") + "\n")
	left.WriteString(entry("n", "Next error") + "\n")
	left.WriteString(entry("N", "Previous error") + "\n")
	left.WriteString(entry("P", "Prompt viewer") + "\n")
	left.WriteString("\n")
	left.WriteString(groupTitle.Render("Filter Mode") + dimStyle.Render(" (press f)") + "\n")
	left.WriteString(entry("T", "Toggle tool") + "\n")
	left.WriteString(entry("P", "Toggle plan") + "\n")
	left.WriteString(entry("A", "Toggle text") + "\n")
	left.WriteString(entry("C", "Toggle done") + "\n")
	left.WriteString(entry("S", "Toggle spawn") + "\n")
	left.WriteString(entry("R", "Toggle replan") + "\n")
	left.WriteString(entry("Z", "Toggle specialize") + "\n")
	left.WriteString(entry("*", "Enable all") + "\n")
	left.WriteString(entry("Esc", "Exit filter") + "\n")

	// Right column
	right.WriteString(groupTitle.Render("View") + "\n")
	right.WriteString(entry("z", "Expand / restore pane") + "\n")
	right.WriteString(entry("1-8", "Jump to pane") + "\n")
	right.WriteString(entry("Esc", "Back / close") + "\n")
	right.WriteString("\n")
	right.WriteString(groupTitle.Render("Process") + "\n")
	right.WriteString(entry("K", "Kill process") + "\n")
	right.WriteString(entry("R", "Resume suspended") + "\n")
	right.WriteString(entry("a", "Attach GDB") + "\n")
	right.WriteString(entry("l", "View log") + "\n")
	right.WriteString(entry("r", "Toggle recording") + "\n")
	right.WriteString("\n")
	right.WriteString(groupTitle.Render("Global") + "\n")
	right.WriteString(entry("L", "LLM conversation") + "\n")
	right.WriteString(entry("H", "History view") + "\n")
	right.WriteString(entry("?", "This help") + "\n")
	right.WriteString(entry("q", "Quit") + "\n")

	leftBlock := lipgloss.NewStyle().Width(max(w/2-4, 20)).Render(left.String())
	rightBlock := lipgloss.NewStyle().Width(max(w/2-4, 20)).Render(right.String())
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftBlock, rightBlock)

	footer := dimStyle.Render("Press ? or Esc to close")
	footerCentered := lipgloss.NewStyle().Width(max(w-4, 20)).Align(lipgloss.Center).Render(footer)

	innerW := max(w-4, 20)
	innerH := max(h-4, 10)

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
