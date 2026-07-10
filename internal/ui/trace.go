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
	// ConfigResolve uses a specialized format with source annotations
	if event.Syscall == "ConfigResolve" {
		return formatConfigResolveTrace(r, event)
	}

	// Story 65.1 aggregate events use a specialized block-summary format:
	// the generic traceArgs 50-char truncation is too narrow for a whole
	// thinking block / tool call, and event.Duration is always 0 (the real
	// duration lives in args duration_ms).
	if isAggregateEvent(event) {
		return formatAggregateTrace(r, event, verbose)
	}

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
			line += "  ← LLM call"
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
			line += "  ← slow op"
		} else {
			line += "  " + MutedStyle.Render("← slow op")
		}
	}

	// LLM syscall annotation
	if isLLMEvent(event.Args) {
		if noColor {
			line += "  ← LLM call"
		} else {
			line += "  " + MutedStyle.Render("← LLM call")
		}
	}

	return line
}

// formatConfigResolveTrace formats a ConfigResolve event with styled source annotations.
// Source labels use MutedStyle; provider/model values use plain text.
func formatConfigResolveTrace(r *Renderer, event types.SyscallEvent) string {
	ts := traceTimestamp(event.Timestamp)
	dur := traceDuration(event.Duration)

	provider, _ := event.Args["provider"].(string)
	providerSource, _ := event.Args["provider_source"].(string)
	model, _ := event.Args["model"].(string)
	modelSource, _ := event.Args["model_source"].(string)
	projectDefault, hasProjectDefault := event.Args["project_default"].(string)

	noColor := r.Profile.ColorLevel == 0

	var parts []string
	if noColor {
		parts = append(parts, fmt.Sprintf("provider=%s [%s]", provider, providerSource))
		if model != "" {
			parts = append(parts, fmt.Sprintf("model=%s [%s]", model, modelSource))
		} else {
			parts = append(parts, fmt.Sprintf("model= [%s]", modelSource))
		}
		if hasProjectDefault && projectDefault != "" {
			parts = append(parts, fmt.Sprintf("project_default=%s", projectDefault))
		}
	} else {
		parts = append(parts, fmt.Sprintf("provider=%s %s", provider, MutedStyle.Render("["+providerSource+"]")))
		if model != "" {
			parts = append(parts, fmt.Sprintf("model=%s %s", model, MutedStyle.Render("["+modelSource+"]")))
		} else {
			parts = append(parts, fmt.Sprintf("model= %s", MutedStyle.Render("["+modelSource+"]")))
		}
		if hasProjectDefault && projectDefault != "" {
			parts = append(parts, MutedStyle.Render("project_default="+projectDefault))
		}
	}

	argsStr := strings.Join(parts, ", ")

	var styledTS, styledName string
	if noColor {
		styledTS = ts
		styledName = "ConfigResolve"
	} else {
		styledTS = MutedStyle.Render(ts)
		styledName = AgentBoldStyle.Render("ConfigResolve")
	}

	return fmt.Sprintf("%s %s(%s)    %s", styledTS, styledName, argsStr, dur)
}

// Aggregate summary rows are previews, not the fidelity layer (full text
// lives in events.jsonl / steps.jsonl): thinking content and tool input are
// capped at maxAggregatePreviewRunes, tool result at maxAggregateResultRunes.
const (
	maxAggregatePreviewRunes = 160
	maxAggregateResultRunes  = 80
)

// isAggregateEvent reports whether the event is a Story 65.1 block-level
// aggregate (DriverThinking/DriverToolCall with subtype="aggregate").
func isAggregateEvent(event types.SyscallEvent) bool {
	if event.Syscall != "DriverThinking" && event.Syscall != "DriverToolCall" {
		return false
	}
	subtype, _ := event.Args["subtype"].(string)
	return subtype == "aggregate"
}

// formatAggregateTrace renders a Story 65.1 aggregate event as a single
// block-summary line (mirrors the formatConfigResolveTrace NoColor split):
//
//	[  1.234s] DriverThinking(fragments=14) → "<content preview>"    3.20s
//	[  1.234s] DriverToolCall(tool=fs_read, path=/x, step=3) → input=<preview> result=<preview>    1.20s
//
// verbose=true disables preview truncation. Duration comes from args
// duration_ms (event.Duration is always 0 for aggregates); numeric args
// tolerate both float64 (post-IPC) and int (in-process/test) shapes.
func formatAggregateTrace(r *Renderer, event types.SyscallEvent, verbose bool) string {
	ts := traceTimestamp(event.Timestamp)
	dur := traceDuration(aggregateDurationMs(event.Args))

	var meta, body string
	if event.Syscall == "DriverThinking" {
		meta = "fragments=" + aggregateIntArg(event.Args, "fragments")
		content, _ := event.Args["content"].(string)
		body = fmt.Sprintf("%q", aggregatePreview(content, maxAggregatePreviewRunes, verbose))
	} else {
		tool, _ := event.Args["tool"].(string)
		parts := []string{"tool=" + tool}
		if path, _ := event.Args["path"].(string); path != "" {
			parts = append(parts, "path="+path)
		}
		if _, ok := event.Args["step"]; ok {
			parts = append(parts, "step="+aggregateIntArg(event.Args, "step"))
		}
		meta = strings.Join(parts, ", ")
		input, _ := event.Args["input"].(string)
		result, _ := event.Args["result"].(string)
		body = fmt.Sprintf("input=%s result=%s",
			aggregatePreview(input, maxAggregatePreviewRunes, verbose),
			aggregatePreview(result, maxAggregateResultRunes, verbose))
	}

	var styledTS, styledName string
	if r.Profile.ColorLevel == 0 {
		styledTS = ts
		styledName = event.Syscall
	} else {
		styledTS = MutedStyle.Render(ts)
		styledName = AgentBoldStyle.Render(event.Syscall)
	}

	return fmt.Sprintf("%s %s(%s) → %s    %s", styledTS, styledName, meta, body, dur)
}

// aggregatePreview flattens newlines to spaces, then truncates to max runes
// with a "..." marker (flatten before truncate so the marker never dangles
// after a line break). verbose disables truncation.
func aggregatePreview(s string, max int, verbose bool) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if verbose {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// aggregateIntArg renders a numeric arg as an integer string, tolerating the
// float64 shape that JSON IPC decoding produces alongside in-process ints.
func aggregateIntArg(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case float64:
		return fmt.Sprintf("%d", int64(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// aggregateDurationMs reads args duration_ms as a time.Duration.
func aggregateDurationMs(args map[string]any) time.Duration {
	switch v := args["duration_ms"].(type) {
	case float64:
		return time.Duration(v * float64(time.Millisecond))
	case int64:
		return time.Duration(v) * time.Millisecond
	case int:
		return time.Duration(v) * time.Millisecond
	default:
		return 0
	}
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
