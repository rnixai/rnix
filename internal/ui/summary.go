package ui

import (
	"fmt"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// RenderSummary outputs a summary footer line:
//
//	[kernel] PID {N} exited({code}) | tokens: {N} | elapsed: {N}s
func RenderSummary(r *Renderer, pid types.PID, exitCode int, tokens int, elapsed time.Duration) {
	if r.OutputMode == ModeQuiet {
		return
	}

	prefix := KernelStyle.Render("[kernel]")

	var exitPart string
	if exitCode == 0 {
		exitPart = MutedStyle.Render(fmt.Sprintf("PID %d exited(%d)", pid, exitCode))
	} else {
		exitPart = WarningStyle.Render(fmt.Sprintf("PID %d exited(%d)", pid, exitCode))
	}

	tokenPart := BoldStyle.Render(fmt.Sprintf("tokens: %d", tokens))
	elapsedPart := BoldStyle.Render(fmt.Sprintf("elapsed: %.1fs", elapsed.Seconds()))

	fmt.Fprintf(r.Writer, "%s %s | %s | %s\n", prefix, exitPart, tokenPart, elapsedPart)
}
