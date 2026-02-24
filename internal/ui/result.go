package ui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// RenderResult outputs a result box with a double-line border.
// Border color is success green; width adapts to min(termWidth, 120).
func RenderResult(r *Renderer, title string, content string) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}

	maxWidth := max(min(r.Profile.Width, 120), 20)

	// Build top border: ══ {title} ══...══
	titlePart := fmt.Sprintf("══ %s ══", title)
	remaining := maxWidth - runeLen(titlePart)
	if remaining > 0 {
		titlePart += strings.Repeat("═", remaining)
	}

	// Build bottom border
	bottomBorder := strings.Repeat("═", maxWidth)

	// Indent content with 2 spaces
	lines := strings.Split(content, "\n")
	var indented strings.Builder
	for _, line := range lines {
		indented.WriteString("  ")
		indented.WriteString(line)
		indented.WriteString("\n")
	}

	if r.Profile.ColorLevel > 0 {
		fmt.Fprintf(r.Writer, "%s\n%s%s\n", SuccessStyle.Render(titlePart), indented.String(), SuccessStyle.Render(bottomBorder))
	} else {
		fmt.Fprintf(r.Writer, "%s\n%s%s\n", titlePart, indented.String(), bottomBorder)
	}
}

// runeLen returns the display width of a string, accounting for double-width CJK characters.
func runeLen(s string) int {
	return runewidth.StringWidth(s)
}
