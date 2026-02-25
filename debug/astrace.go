package debug

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// ANSI escape codes for terminal coloring.
const (
	ansiRed   = "\033[91m" // bright red — error lines
	ansiGray  = "\033[90m" // dark gray  — slow operation annotation
	ansiReset = "\033[0m"
)

// Options configures Attach and FormatEvent behavior.
type Options struct {
	// ColorEnabled controls ANSI color output.
	// Use DefaultOptions() to auto-detect based on NO_COLOR env var.
	ColorEnabled bool

	// Verbose disables argument truncation (default: truncate at 50 chars).
	Verbose bool
}

// DefaultOptions returns Options with sensible defaults:
//   - ColorEnabled: true unless NO_COLOR env var is set
//   - Verbose: false
func DefaultOptions() Options {
	_, noColor := os.LookupEnv("NO_COLOR")
	return Options{
		ColorEnabled: !noColor,
		Verbose:      false,
	}
}

// Attach consumes SyscallEvents from ch and writes formatted trace lines to w.
// Returns ctx.Err() when context is cancelled, nil when ch is closed.
// Each event is written immediately (no batching) to satisfy NFR3 (≤500ms latency).
func Attach(ctx context.Context, ch <-chan types.SyscallEvent, w io.Writer, opts Options) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-ch:
			if !ok {
				return nil // process done, channel closed
			}
			line := FormatEvent(event, opts)
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
}

// FormatEvent formats a single SyscallEvent into a human-readable trace line.
// It is a pure function with no I/O or external state dependencies.
func FormatEvent(event types.SyscallEvent, opts Options) string {
	ts := formatTimestamp(event.Timestamp)
	args := formatArgs(event.Args, opts.Verbose)
	result := formatResult(event.Result, event.Err)
	dur := formatDuration(event.Duration)

	// Build base line: [timestamp] Syscall(args) → result    duration
	line := fmt.Sprintf("%s %s(%s) → %s    %s", ts, event.Syscall, args, result, dur)

	// Annotations (appended to line end)
	annotations := ""

	// Slow operation (duration > 1s)
	// Note: gray color is only applied when NOT an error line, because error lines
	// wrap the entire output in red — an intermediate ansiReset would break that.
	if event.Duration > time.Second {
		if opts.ColorEnabled && event.Err == nil {
			annotations += "  " + ansiGray + "← 慢操作" + ansiReset
		} else {
			annotations += "  ← 慢操作"
		}
	}

	// LLM syscall
	if isLLMSyscall(event.Args) {
		annotations += "  ← LLM 调用"
	}

	line += annotations

	// Error highlighting (wraps entire line including annotations)
	if event.Err != nil {
		if opts.ColorEnabled {
			return ansiRed + line + ansiReset
		}
		return "[ERR] " + line
	}

	return line
}

// formatTimestamp formats a duration as a fixed-width timestamp: [  0.012s]
func formatTimestamp(ts time.Duration) string {
	return fmt.Sprintf("[%7.3fs]", ts.Seconds())
}

// formatArgs formats syscall arguments as sorted key=value pairs with optional truncation.
func formatArgs(args map[string]any, verbose bool) string {
	if len(args) == 0 {
		return ""
	}
	// Sort keys for deterministic output.
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
		// String values get quoted.
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

// formatResult formats the syscall result: success as %v, error as err(%v).
func formatResult(result any, err error) string {
	if err != nil {
		return fmt.Sprintf("err(%v)", err)
	}
	return fmt.Sprintf("%v", result)
}

// formatDuration formats a duration with adaptive units (µs, ms, s).
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// isLLMSyscall checks if any path or tool argument contains /dev/llm/.
func isLLMSyscall(args map[string]any) bool {
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
