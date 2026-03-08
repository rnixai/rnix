package debug

import (
	"fmt"
	"strings"
	"time"
)

// FormatReplayEvent formats a single RecordEvent for display.
// When verbose is true, additional detail is included.
func FormatReplayEvent(event *RecordEvent, verbose bool) string {
	ts := formatReplayTimestamp(event.Timestamp)
	seqStr := fmt.Sprintf("#%03d", event.SeqNum)

	switch event.Type {
	case RecordSyscall:
		return formatSyscallEvent(seqStr, ts, event, verbose)
	case RecordLLMResponse:
		return formatLLMEvent(seqStr, ts, event, verbose)
	case RecordContextSnapshot:
		return formatContextEvent(seqStr, ts, event, verbose)
	case RecordStateChange:
		return formatStateEvent(seqStr, ts, event, verbose)
	default:
		return fmt.Sprintf("[%s %s] %s: (unknown event type)", seqStr, ts, event.Type)
	}
}

func formatSyscallEvent(seq, ts string, event *RecordEvent, verbose bool) string {
	if event.Syscall == nil {
		return fmt.Sprintf("[%s %s] syscall: (no data)", seq, ts)
	}
	sc := event.Syscall
	base := fmt.Sprintf("[%s %s] syscall: %s", seq, ts, sc.Syscall)

	if verbose {
		var parts []string
		parts = append(parts, base)
		if len(sc.Args) > 0 {
			for k, v := range sc.Args {
				parts = append(parts, fmt.Sprintf("  %s=%v", k, v))
			}
		}
		if sc.Result != nil {
			parts = append(parts, fmt.Sprintf("  result=%v", sc.Result))
		}
		if sc.Err != "" {
			parts = append(parts, fmt.Sprintf("  err=%s", sc.Err))
		}
		parts = append(parts, fmt.Sprintf("  duration=%s", sc.Duration))
		return strings.Join(parts, "\n")
	}

	// Compact form
	argStr := ""
	if path, ok := sc.Args["path"]; ok {
		argStr = fmt.Sprintf("(%q)", path)
	} else if intent, ok := sc.Args["intent"]; ok {
		argStr = fmt.Sprintf("(intent=%q)", intent)
	}

	resultStr := ""
	if sc.Result != nil {
		resultStr = fmt.Sprintf(" → %v", sc.Result)
	}
	if sc.Err != "" {
		resultStr = fmt.Sprintf(" → err=%s", sc.Err)
	}

	durStr := ""
	if sc.Duration > 0 {
		durStr = fmt.Sprintf(" (%s)", sc.Duration)
	}

	return fmt.Sprintf("[%s %s] syscall: %s%s%s%s", seq, ts, sc.Syscall, argStr, resultStr, durStr)
}

func formatLLMEvent(seq, ts string, event *RecordEvent, verbose bool) string {
	if event.LLM == nil {
		return fmt.Sprintf("[%s %s] llm: (no data)", seq, ts)
	}
	l := event.LLM
	base := fmt.Sprintf("[%s %s] llm: model=%s req=%dtok resp=%dtok",
		seq, ts, l.Model, l.RequestTokens, l.ResponseTokens)

	if verbose && l.ResponseSummary != "" {
		return base + fmt.Sprintf(" %q", l.ResponseSummary)
	}
	return base
}

func formatContextEvent(seq, ts string, event *RecordEvent, verbose bool) string {
	if event.Context == nil {
		return fmt.Sprintf("[%s %s] context: (no data)", seq, ts)
	}
	c := event.Context
	base := fmt.Sprintf("[%s %s] context: msgs=%d tokens≈%d",
		seq, ts, c.MessageCount, c.TokenEstimate)

	if verbose && c.SystemPromptHash != "" {
		return base + fmt.Sprintf(" sysPromptHash=%s", c.SystemPromptHash)
	}
	return base
}

func formatStateEvent(seq, ts string, event *RecordEvent, verbose bool) string {
	if event.State == nil {
		return fmt.Sprintf("[%s %s] state: (no data)", seq, ts)
	}
	s := event.State
	base := fmt.Sprintf("[%s %s] state: %s → %s", seq, ts, s.FromState, s.ToState)

	if s.Reason != "" {
		base += fmt.Sprintf(" (%s)", s.Reason)
	}
	return base
}

func formatReplayTimestamp(d time.Duration) string {
	return fmt.Sprintf("+%.1fs", d.Seconds())
}

// FormatReplayList formats a list of ReplayListItem for display.
// The current position is marked with a "►" prefix.
func FormatReplayList(items []ReplayListItem) string {
	if len(items) == 0 {
		return ""
	}

	var lines []string
	for _, item := range items {
		marker := "  "
		if item.IsCursor {
			marker = "► "
		}

		ev := item.Event
		ts := formatReplayTimestamp(ev.Timestamp)
		seqStr := fmt.Sprintf("#%03d", ev.SeqNum)

		typeName := string(ev.Type)
		detail := formatListDetail(&ev)

		line := fmt.Sprintf("%s[%s] %s  %-8s  %s", marker, seqStr, ts, typeName, detail)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatListDetail(ev *RecordEvent) string {
	switch ev.Type {
	case RecordSyscall:
		if ev.Syscall != nil {
			return ev.Syscall.Syscall
		}
	case RecordLLMResponse:
		if ev.LLM != nil {
			return ev.LLM.Model
		}
	case RecordContextSnapshot:
		if ev.Context != nil {
			return fmt.Sprintf("msgs=%d", ev.Context.MessageCount)
		}
	case RecordStateChange:
		if ev.State != nil {
			return fmt.Sprintf("%s → %s", ev.State.FromState, ev.State.ToState)
		}
	}
	return ""
}

// FormatReplaySummary formats recording metadata as a summary string.
func FormatReplaySummary(meta RecordMetadata, eventCount int) string {
	duration := "N/A"
	if !meta.EndTime.IsZero() && !meta.StartTime.IsZero() {
		d := meta.EndTime.Sub(meta.StartTime)
		duration = fmt.Sprintf("%.0fs", d.Seconds())
	}

	return fmt.Sprintf("Record ID:  %s\nPID:        %d\nIntent:     %s\nEvents:     %d\nDuration:   %s\nStatus:     %s",
		meta.RecordID, meta.PID, meta.Intent, eventCount, duration, meta.Status)
}
