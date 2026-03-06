package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// FormatTraceLine formats a SyscallEvent into a styled trace line using lipgloss.
// This is the UI-layer replacement for debug.FormatEvent's raw ANSI output.
func FormatTraceLine(r *Renderer, event types.SyscallEvent, verbose bool) string {
	ts := traceTimestamp(event.Timestamp)
	args := traceArgs(event.Args, verbose)
	result := traceResult(event.Result, event.Err)
	dur := traceDuration(event.Duration)

	isErr := event.Err != nil
	noColor := r.Profile.ColorLevel == 0

	// Error lines: use plain text components so ErrorStyle wraps the entire line
	// uniformly in red (AC #2). Per-component styles (MutedStyle, AgentBoldStyle)
	// produce ANSI resets that would break the outer ErrorStyle wrapper.
	if isErr {
		line := fmt.Sprintf("%s %s(%s) → %s    %s", ts, event.Syscall, args, result, dur)
		// LLM annotation (plain text — no MutedStyle to avoid ANSI nesting)
		if isLLMEvent(event.Args) {
			line += "  ← LLM 调用"
		}
		if noColor {
			return "[ERR] " + line
		}
		return ErrorStyle.Render(line)
	}

	// Non-error lines: style individual components
	var styledTS, styledName string
	if noColor {
		styledTS = ts
		styledName = event.Syscall
	} else {
		styledTS = MutedStyle.Render(ts)
		styledName = AgentBoldStyle.Render(event.Syscall)
	}

	line := fmt.Sprintf("%s %s(%s) → %s    %s", styledTS, styledName, args, result, dur)

	// Slow operation annotation (duration > 1s)
	if event.Duration > time.Second {
		if noColor {
			line += "  ← 慢操作"
		} else {
			line += "  " + MutedStyle.Render("← 慢操作")
		}
	}

	// LLM syscall annotation
	if isLLMEvent(event.Args) {
		if noColor {
			line += "  ← LLM 调用"
		} else {
			line += "  " + MutedStyle.Render("← LLM 调用")
		}
	}

	return line
}

// traceTimestamp formats a duration as a fixed-width timestamp: [  0.012s]
func traceTimestamp(ts time.Duration) string {
	return fmt.Sprintf("[%7.3fs]", ts.Seconds())
}

// traceArgs formats syscall arguments as sorted key=value pairs with optional truncation.
func traceArgs(args map[string]any, verbose bool) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := fmt.Sprintf("%v", args[k])
		if !verbose && len(v) > 50 {
			v = v[:47] + "..."
		}
		if s, ok := args[k].(string); ok {
			v = fmt.Sprintf("%q", s)
			if !verbose && len(v) > 52 { // 50 + 2 quotes
				v = v[:49] + `..."`
			}
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ", ")
}

// traceResult formats the syscall result: success as %v, error as err(%v).
func traceResult(result any, err error) string {
	if err != nil {
		return fmt.Sprintf("err(%v)", err)
	}
	return fmt.Sprintf("%v", result)
}

// traceDuration formats a duration with adaptive units (µs, ms, s).
func traceDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// isLLMEvent checks if any path or tool argument contains /dev/llm/.
func isLLMEvent(args map[string]any) bool {
	for _, key := range []string{"path", "tool"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				if strings.Contains(s, "/dev/llm/") {
					return true
				}
			}
		}
	}
	return false
}
