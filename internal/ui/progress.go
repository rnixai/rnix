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

// StemMessage outputs a stem-prefixed message: [stem] {message}
func (p *ProgressReporter) StemMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := StemStyle.Render("[stem]")
	fmt.Fprintf(p.renderer.Writer, "%s   %s\n", prefix, msg)
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

// AgentThinking renders a throttled, concise "thinking in progress" indicator
// for the LLM long-reasoning phase (Story 60.1 AC2/AC3). It MUST honor the
// output-mode contracts — no indicator under ModeQuiet, no unstructured thinking
// text under ModeJSON — and MUST throttle/aggregate high-frequency deltas rather
// than printing one line per delta.
//
// SKELETON (Story 60.1 ATDD red phase): intentionally a no-op until dev decides
// the throttle strategy (time window vs byte threshold) and the default
// presentation form (concise activity indicator vs limited scrolling text) —
// see the story Dev Notes design trade-offs #2/#3. The ModeQuiet/ModeJSON
// green-guards already pin the fixed contract; the visibility/throttle RED
// tests stay skipped until this method is implemented.
func (p *ProgressReporter) AgentThinking(pid types.PID, step int, text string) {
	// no-op — dev fills in throttle + render for Story 60.1
}
