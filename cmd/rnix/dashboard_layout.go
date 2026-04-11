package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderFixedPanel renders content inside a rigid bordered container.
// The panel never grows beyond the specified (width, height).
//
// Layout-First principle:
//  1. Each line is truncated to innerW (prevents wrapping → panel inflation)
//  2. Total lines are clipped to innerH (prevents overflow)
//  3. Height(innerH) is now an exact value (content ≤ innerH, no inflation)
//
// This replaces the previous pattern of style.Border().Width().Height().Render()
// where Height was only a minimum, causing panels to grow when content wrapped.
func renderFixedPanel(content string, width, height int, borderColor lipgloss.Color) string {
	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	// Step 1: truncate each line to innerW (prevent wrapping)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > innerW {
			lines[i] = lipgloss.NewStyle().MaxWidth(innerW).Render(line)
		}
	}

	// Step 2: clip total lines to innerH (prevent overflow)
	if len(lines) > innerH {
		lines = lines[:innerH]
	}

	// Step 3: render in bordered style — Height is now exact (content ≤ innerH)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	return style.Render(strings.Join(lines, "\n"))
}
