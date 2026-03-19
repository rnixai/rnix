package ui

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
)

// ProgressReporter outputs agent progress messages to the renderer.
type ProgressReporter struct {
	renderer *Renderer
}

// NewProgressReporter creates a ProgressReporter attached to the given renderer.
func NewProgressReporter(r *Renderer) *ProgressReporter {
	return &ProgressReporter{renderer: r}
}

// KernelMessage outputs a kernel-prefixed message: [kernel] {message}
func (p *ProgressReporter) KernelMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := KernelStyle.Render("[kernel]")
	fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, msg)
}

// AgentMessage outputs an agent-prefixed message: [agent/{pid}] {message}
func (p *ProgressReporter) AgentMessage(pid types.PID, format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, msg)
}

// AgentStep outputs a reasoning step progress line (verbose mode only after Story 3.6).
func (p *ProgressReporter) AgentStep(pid types.PID, step, total int) {
	if p.renderer.OutputMode != ModeVerbose {
		return
	}
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	fmt.Fprintf(p.renderer.Writer, "%s reasoning step %d...\n", prefix, step)
}

// AgentStepComplete outputs a step completion summary: [agent/{pid}] step {step}: {summary}
// When summary is non-empty, it is displayed directly (action type is omitted to avoid
// double-arrow with tool_call summaries that already contain "→").
// When summary is empty, only the action type is shown.
func (p *ProgressReporter) AgentStepComplete(pid types.PID, step int, action string, summary string) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	if summary != "" {
		fmt.Fprintf(p.renderer.Writer, "%s step %d: %s\n", prefix, step, summary)
	} else {
		fmt.Fprintf(p.renderer.Writer, "%s step %d: %s\n", prefix, step, action)
	}
}
