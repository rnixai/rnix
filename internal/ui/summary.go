package ui

import (
	"fmt"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// RenderSummary outputs a summary footer line:
//
//	[kernel] PID {N} exited({code}) | provider/model (effort: {E}) | tokens: {N} | elapsed: {N}s
//
// The "(effort: …)" segment (Story 55.2) appears only when a reasoning effort
// is configured; it lives inside the provider segment, so the pipe-separator
// count is unchanged.
func RenderSummary(r *Renderer, pid types.PID, exitCode int, tokens int, elapsed time.Duration, provider, model, effort string) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}

	prefix := KernelStyle.Render("[kernel]")

	var exitPart string
	if exitCode == 0 {
		exitPart = MutedStyle.Render(fmt.Sprintf("PID %d exited(%d)", pid, exitCode))
	} else {
		exitPart = WarningStyle.Render(fmt.Sprintf("PID %d exited(%d)", pid, exitCode))
	}

	var providerPart string
	if provider != "" && model != "" {
		providerPart = BoldStyle.Render(fmt.Sprintf("%s/%s", provider, model))
	} else if provider != "" {
		providerPart = BoldStyle.Render(provider)
	}
	// Story 55.2: append reasoning effort inside the provider segment (no new
	// pipe separator) so it surfaces alongside provider/model when configured.
	if providerPart != "" && effort != "" {
		providerPart += " " + MutedStyle.Render(fmt.Sprintf("(effort: %s)", effort))
	}

	tokenPart := BoldStyle.Render(fmt.Sprintf("tokens: %d", tokens))
	elapsedPart := BoldStyle.Render(fmt.Sprintf("elapsed: %.1fs", elapsed.Seconds()))

	if providerPart != "" {
		fmt.Fprintf(r.Writer, "%s %s | %s | %s | %s\n", prefix, exitPart, providerPart, tokenPart, elapsedPart)
	} else {
		fmt.Fprintf(r.Writer, "%s %s | %s | %s\n", prefix, exitPart, tokenPart, elapsedPart)
	}
}
