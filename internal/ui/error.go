package ui

import (
	"fmt"
)

// RenderError outputs a three-line error block:
//
//	✗ {device}: {reason}
//	  → {impact}
//	  → 建议: {suggestion}
func RenderError(r *Renderer, device string, reason string, impact string, suggestion string) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}

	var prefix string
	if r.Profile.ColorLevel > 0 {
		prefix = ErrorStyle.Render("✗")
	} else if r.Profile.IsUnicode {
		prefix = "✗"
	} else {
		prefix = "[ERR]"
	}

	arrow := "→"
	if !r.Profile.IsUnicode {
		arrow = "->"
	}

	fmt.Fprintf(r.Writer, "%s %s: %s\n", prefix, device, reason)
	fmt.Fprintf(r.Writer, "  %s %s\n", MutedStyle.Render(arrow), impact)
	fmt.Fprintf(r.Writer, "  %s suggestion: %s\n", MutedStyle.Render(arrow), suggestion)
}
