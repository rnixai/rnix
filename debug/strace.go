package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
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

	// JSON enables JSON output mode (one JSON object per line).
	JSON bool

	// Formatter overrides FormatEvent with a custom formatting function.
	// When non-nil and JSON is false, Attach uses this instead of FormatEvent.
	Formatter func(types.SyscallEvent) string
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
			var line string
			if opts.JSON {
				line = FormatEventJSON(event)
			} else if opts.Formatter != nil {
				line = opts.Formatter(event)
			} else {
				line = FormatEvent(event, opts)
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
}

// FormatEvent formats a single SyscallEvent into a human-readable trace line.
// It is a pure function with no I/O or external state dependencies.
func FormatEvent(event types.SyscallEvent, opts Options) string {
	// ConfigResolve uses a specialized format
	if event.Syscall == "ConfigResolve" {
		return formatConfigResolve(event, opts)
	}
	// ReasonStep uses a specialized format to truncate large results
	if event.Syscall == "ReasonStep" {
		return formatReasonStep(event, opts)
	}
	// Driver events (DriverToolCall, DriverThinking, DriverInit, DriverUser, DriverEvent)
	if strings.HasPrefix(event.Syscall, "Driver") {
		return formatDriverEvent(event, opts)
	}

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
			annotations += "  " + ansiGray + "← slow op" + ansiReset
		} else {
			annotations += "  ← slow op"
		}
	}

	// LLM syscall
	if isLLMSyscall(event.Args) {
		annotations += "  ← LLM call"
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

// formatConfigResolve formats a ConfigResolve event with source annotations.
// Format: [N.NNNs] ConfigResolve(provider=X [source], model=Y [source])    duration
func formatConfigResolve(event types.SyscallEvent, opts Options) string {
	ts := formatTimestamp(event.Timestamp)
	dur := formatDuration(event.Duration)

	provider, _ := event.Args["provider"].(string)
	providerSource, _ := event.Args["provider_source"].(string)
	model, _ := event.Args["model"].(string)
	modelSource, _ := event.Args["model_source"].(string)
	projectDefault, hasProjectDefault := event.Args["project_default"].(string)

	// Build args with source annotations
	var parts []string
	if opts.ColorEnabled {
		parts = append(parts, fmt.Sprintf("provider=%s %s[%s]%s", provider, ansiGray, providerSource, ansiReset))
		if model != "" {
			parts = append(parts, fmt.Sprintf("model=%s %s[%s]%s", model, ansiGray, modelSource, ansiReset))
		} else {
			parts = append(parts, fmt.Sprintf("model= %s[%s]%s", ansiGray, modelSource, ansiReset))
		}
		if hasProjectDefault && projectDefault != "" {
			parts = append(parts, fmt.Sprintf("%sproject_default=%s%s", ansiGray, projectDefault, ansiReset))
		}
	} else {
		parts = append(parts, fmt.Sprintf("provider=%s [%s]", provider, providerSource))
		if model != "" {
			parts = append(parts, fmt.Sprintf("model=%s [%s]", model, modelSource))
		} else {
			parts = append(parts, fmt.Sprintf("model= [%s]", modelSource))
		}
		if hasProjectDefault && projectDefault != "" {
			parts = append(parts, fmt.Sprintf("project_default=%s", projectDefault))
		}
	}

	return fmt.Sprintf("%s ConfigResolve(%s)    %s", ts, strings.Join(parts, ", "), dur)
}

// formatReasonStep formats a ReasonStep event with truncated result.
// Large text/complete results are truncated to the first non-empty line (max 80 chars).
func formatReasonStep(event types.SyscallEvent, opts Options) string {
	ts := formatTimestamp(event.Timestamp)
	dur := formatDuration(event.Duration)

	action, _ := event.Args["action"].(string)
	stepNum, _ := event.Args["step"].(int)

	// Build args excluding step and action (shown separately)
	var extraParts []string
	for _, k := range sortedKeys(event.Args) {
		if k == "step" || k == "action" {
			continue
		}
		v := fmt.Sprintf("%v", event.Args[k])
		if !opts.Verbose && len(v) > 50 {
			v = v[:47] + "..."
		}
		extraParts = append(extraParts, k+"="+v)
	}

	argStr := fmt.Sprintf("action=%q, step=%d", action, stepNum)
	if len(extraParts) > 0 {
		argStr += ", " + strings.Join(extraParts, ", ")
	}

	// Truncate result for text-heavy actions
	resultStr := ""
	if event.Err != nil {
		resultStr = fmt.Sprintf("err(%v)", event.Err)
	} else if event.Result != nil {
		s := fmt.Sprintf("%v", event.Result)
		// Find first non-empty line, truncate to 80 chars
		brief := ""
		for line := range strings.SplitSeq(s, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				brief = line
				break
			}
		}
		if len([]rune(brief)) > 80 {
			brief = string([]rune(brief)[:80]) + "..."
		}
		if brief == "" {
			resultStr = "<nil>"
		} else {
			resultStr = brief
		}
	} else {
		resultStr = "<nil>"
	}

	line := fmt.Sprintf("%s ReasonStep(%s) → %s    %s", ts, argStr, resultStr, dur)

	// Annotations
	if event.Duration > time.Second {
		if opts.ColorEnabled && event.Err == nil {
			line += "  " + ansiGray + "← slow op" + ansiReset
		} else {
			line += "  ← slow op"
		}
	}

	if event.Err != nil {
		if opts.ColorEnabled {
			return ansiRed + line + ansiReset
		}
		return "[ERR] " + line
	}

	return line
}

// formatDriverEvent formats a driver event (DriverToolCall, DriverThinking, etc.).
// Format: [N.NNNs] DriverToolCall(tool=shell, started) "description"
func formatDriverEvent(event types.SyscallEvent, opts Options) string {
	ts := formatTimestamp(event.Timestamp)

	subtype, _ := event.Args["subtype"].(string)
	tool, _ := event.Args["tool"].(string)
	description, _ := event.Args["description"].(string)
	command, _ := event.Args["command"].(string)
	path, _ := event.Args["path"].(string)

	// Build args
	var parts []string
	if tool != "" {
		parts = append(parts, fmt.Sprintf("tool=%s", tool))
	}
	if path != "" {
		r := []rune(path)
		if len(r) > 50 {
			path = "..." + string(r[len(r)-47:])
		}
		parts = append(parts, fmt.Sprintf("path=%q", path))
	}
	if subtype != "" {
		parts = append(parts, subtype)
	}

	line := fmt.Sprintf("%s %s(%s)", ts, event.Syscall, strings.Join(parts, ", "))

	// Append description, command, or content as summary
	summary := description
	if summary == "" && command != "" {
		summary = command
	}
	// For DriverThinking, use content text as summary
	if summary == "" && event.Syscall == "DriverThinking" {
		if c, ok := event.Args["content"].(string); ok && c != "" && c != subtype {
			summary = c
		}
	}
	if summary != "" {
		r := []rune(summary)
		if len(r) > 60 {
			summary = string(r[:60]) + "..."
		}
		line += fmt.Sprintf(" %q", summary)
	}

	if opts.ColorEnabled {
		return ansiGray + line + ansiReset
	}
	return line
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// jsonEvent is the JSON representation of a SyscallEvent.
type jsonEvent struct {
	TimestampMs int64          `json:"timestamp_ms"`
	PID         types.PID      `json:"pid"`
	Syscall     string         `json:"syscall"`
	Args        map[string]any `json:"args"`
	Result      any            `json:"result"`
	Error       string         `json:"error"`
	DurationMs  int64          `json:"duration_ms"`
}

// FormatEventJSON formats a SyscallEvent as a single-line JSON string.
func FormatEventJSON(event types.SyscallEvent) string {
	je := jsonEvent{
		TimestampMs: event.Timestamp.Milliseconds(),
		PID:         event.PID,
		Syscall:     event.Syscall,
		Args:        event.Args,
		Result:      event.Result,
		Error:       "",
		DurationMs:  event.Duration.Milliseconds(),
	}
	if event.Err != nil {
		je.Error = event.Err.Error()
	}
	// Result needs special handling: error type cannot be JSON-serialized.
	if event.Result != nil {
		if _, ok := event.Result.(error); ok {
			je.Result = fmt.Sprintf("%v", event.Result)
		}
	}
	data, err := json.Marshal(je)
	if err != nil {
		// Fallback: return minimal valid JSON with error info.
		return fmt.Sprintf(`{"syscall":%q,"error":"json marshal failed: %s"}`, event.Syscall, err)
	}
	return string(data)
}
