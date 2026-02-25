package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gonewx/crux/internal/types"
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

	// Style components
	var styledTS, styledName string
	if noColor {
		styledTS = ts
		styledName = event.Syscall
	} else {
		styledTS = MutedStyle.Render(ts)
		styledName = AgentStyle.Bold(true).Render(event.Syscall)
	}

	// Build base line: [timestamp] Syscall(args) → result    duration
	line := fmt.Sprintf("%s %s(%s) → %s    %s", styledTS, styledName, args, result, dur)

	// Error return value styling (only the result part, when not wrapping entire line)
	if isErr && !noColor {
		styledResult := ErrorStyle.Render(result)
		line = fmt.Sprintf("%s %s(%s) → %s    %s", styledTS, styledName, args, styledResult, dur)
	}

	// Annotations
	annotations := ""

	// Slow operation (duration > 1s) — skip gray annotation on error lines
	if event.Duration > time.Second && !isErr {
		if noColor {
			annotations += "  ← 慢操作"
		} else {
			annotations += "  " + MutedStyle.Render("← 慢操作")
		}
	}

	// LLM syscall annotation
	if isLLMEvent(event.Args) {
		if noColor {
			annotations += "  ← LLM 调用"
		} else {
			annotations += "  " + MutedStyle.Render("← LLM 调用")
		}
	}

	line += annotations

	// Error highlighting — wraps entire line
	if isErr {
		if noColor {
			return "[ERR] " + line
		}
		return ErrorStyle.Render(line)
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
