package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// Story 43-3: Timeline Renderer for Script Trace Events.
//
// This file translates the 5 ScriptEventKind syscalls written by 43-2
// (ScriptStmtBegin / ScriptStmtEnd / ScriptSpawn / ScriptWhileIter /
// ScriptCondition) into UnifiedEvent rows shown in the main Timeline pane.
// The translation is invoked from the compactEventsMsg handler so the
// existing ListEvents IPC channel is reused — no new IPC method, no new
// polling goroutine.

// scriptEventSummaryMaxWidth caps the Summary column at 120 terminal columns
// (rune width, CJK-aware via runewidth.StringWidth) — Spec AC#3.
const scriptEventSummaryMaxWidth = 120

// scriptIntentMaxWidth caps the intent / agent label inside ScriptSpawn
// summaries before suffixes are appended (Spec AC#3 "截断到 60 列").
const scriptIntentMaxWidth = 60

// scriptEventFromSyscall translates a Script* syscall wire event into a
// UnifiedEvent. Returns (zero, false) when ev.Syscall is not one of the 5
// recognised Script kinds so callers can fall through to existing handlers.
//
// Field semantics (Spec AC#2):
//   - Type      = EventScript
//   - Severity  = SevError when args["error"] is a non-empty string OR
//     args["exit_code"] is numeric and != 0; otherwise SevInfo.
//   - Timestamp = time.UnixMilli(ev.TimestampMs)
//   - PID       = ev.PID; UUID is injected by the caller (compactEventsMsg
//     handler — see Dev Notes #5 for the PID-reuse rationale).
//   - Summary   = formatScriptEventSummary(syscall, args)
//   - Detail    = sorted-key "key=value\n..." render of args (matches the
//     pattern used by the strace branch — keeps the detail card simple)
//   - RawEvent  = pointer to a local copy of ev so callers don't share the
//     underlying map across goroutines.
func scriptEventFromSyscall(ev ipc.SyscallEventWire) (UnifiedEvent, bool) {
	if !isScriptSyscall(ev.Syscall) {
		return UnifiedEvent{}, false
	}
	evCopy := ev
	sev := SevInfo
	if scriptEventIsError(ev.Args) {
		sev = SevError
	}
	return UnifiedEvent{
		Type:      EventScript,
		Severity:  sev,
		Timestamp: time.UnixMilli(ev.TimestampMs),
		PID:       ev.PID,
		Summary:   formatScriptEventSummary(ev.Syscall, ev.Args),
		Detail:    formatScriptEventDetail(ev.Args),
		RawEvent:  &evCopy,
	}, true
}

// isScriptSyscall reports whether the syscall name matches one of the 5
// Script kinds emitted by ScriptExecutor (shell/script_events.go).
func isScriptSyscall(syscall string) bool {
	switch syscall {
	case "ScriptStmtBegin", "ScriptStmtEnd", "ScriptSpawn", "ScriptWhileIter", "ScriptCondition":
		return true
	}
	return false
}

// scriptEventIsError applies the AC#2 severity rule: args["error"] non-empty
// OR args["exit_code"] non-zero (int / int64 / uint64 / float64 / json.Number)
// → SevError. args["stopped"] (break/return) is NOT an error.
func scriptEventIsError(args map[string]any) bool {
	if errStr := getArgString(args, "error"); errStr != "" {
		return true
	}
	if v, ok := args["exit_code"]; ok {
		switch n := v.(type) {
		case int:
			return n != 0
		case int64:
			return n != 0
		case uint64:
			return n != 0
		case float64:
			return n != 0
		case json.Number:
			// json.Number arrives when the wire decoder uses
			// Decoder.UseNumber(); fall back to string compare against "0"
			// to avoid losing precision on huge integers.
			return string(n) != "0" && string(n) != ""
		}
	}
	return false
}

// formatScriptEventDetail renders args as sorted-key "key=value\n..." for the
// detail card (AC#7). args map ordering is non-deterministic; sorting keeps
// detail output stable across runs.
func formatScriptEventDetail(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%s=%v", k, args[k])
	}
	return sb.String()
}

// formatScriptEventSummary produces the one-line summary shown in the
// Timeline pane (Spec AC#3). Glyphs are downgraded to ASCII when
// RNIX_ASCII=1 via ui.IsASCIIMode().
//
// Templates (Unicode mode):
//   - ScriptStmtBegin : "L{line} ▸ {stmt_kind}"
//   - ScriptStmtEnd   : "L{line} ✓ {stmt_kind}"  (success)
//                       "L{line} ✗ {stmt_kind}: {error}"  (error)
//                       "L{line} ⊘ {stmt_kind} (break/return)"  (stopped)
//   - ScriptSpawn     : "L{line} ↳ spawn {intent or agent}[ → ${assign}][ [parallel]]"
//   - ScriptWhileIter : "L{line} ↻ while iter={iteration}[ ({condition})]"
//   - ScriptCondition : "L{line} ? {condition or left} → T|F"
//                       "L{line} ? {condition or left} → ERR: {error}"
//
// Output is single-line (newlines stripped) and truncated to
// scriptEventSummaryMaxWidth runes; the same rune-aware truncation
// (`runewidth.Truncate` with "…") used by timeline.ShortenArgs.
func formatScriptEventSummary(syscall string, args map[string]any) string {
	ascii := ui.IsASCIIMode()
	line := scriptLineLabel(args)

	var raw string
	switch syscall {
	case "ScriptStmtBegin":
		kind := scriptStmtKindLabel(args)
		raw = fmt.Sprintf("%s %s %s", line, scriptGlyph("begin", ascii), kind)
	case "ScriptStmtEnd":
		kind := scriptStmtKindLabel(args)
		switch {
		case scriptEventIsError(args):
			errMsg := getArgString(args, "error")
			if errMsg == "" {
				errMsg = fmt.Sprintf("exit_code=%v", args["exit_code"])
			}
			raw = fmt.Sprintf("%s %s %s: %s", line, scriptGlyph("err", ascii), kind, errMsg)
		case getArgBool(args, "stopped"):
			raw = fmt.Sprintf("%s %s %s (break/return)", line, scriptGlyph("stopped", ascii), kind)
		default:
			raw = fmt.Sprintf("%s %s %s", line, scriptGlyph("ok", ascii), kind)
		}
	case "ScriptSpawn":
		label := getArgString(args, "intent")
		if label == "" {
			label = getArgString(args, "agent")
		}
		if label == "" {
			label = "?"
		}
		if runewidth.StringWidth(label) > scriptIntentMaxWidth {
			label = runewidth.Truncate(label, scriptIntentMaxWidth-1, "…")
		}
		// Quote intent so multi-word strings read as a single token.
		quoted := fmt.Sprintf("%q", label)
		raw = fmt.Sprintf("%s %s spawn %s", line, scriptGlyph("spawn", ascii), quoted)
		if assign := getArgString(args, "assign"); assign != "" {
			raw += " " + scriptArrow(ascii) + " $" + assign
		}
		if getArgBool(args, "parallel") {
			raw += " [parallel]"
		}
	case "ScriptWhileIter":
		iter := getArgInt(args, "iteration")
		cond := getArgString(args, "condition")
		raw = fmt.Sprintf("%s %s while iter=%d", line, scriptGlyph("while", ascii), iter)
		if cond != "" {
			raw += " (" + cond + ")"
		}
	case "ScriptCondition":
		cond := getArgString(args, "condition")
		if cond == "" {
			cond = getArgString(args, "left")
		}
		if cond == "" {
			cond = "?"
		}
		arrow := scriptArrow(ascii)
		if errMsg := getArgString(args, "error"); errMsg != "" {
			raw = fmt.Sprintf("%s ? %s %s ERR: %s", line, cond, arrow, errMsg)
		} else {
			mark := "F"
			if getArgBool(args, "result") {
				mark = "T"
			}
			raw = fmt.Sprintf("%s ? %s %s %s", line, cond, arrow, mark)
		}
	default:
		// Unknown syscall — caller should have filtered via isScriptSyscall;
		// fall back to a clearly-tagged placeholder so a forgotten case is
		// visible rather than silent.
		raw = fmt.Sprintf("%s %s", line, syscall)
	}

	// Single-line + width clamp (Spec AC#3).
	raw = strings.ReplaceAll(raw, "\n", " ")
	raw = strings.ReplaceAll(raw, "\r", " ")
	if runewidth.StringWidth(raw) > scriptEventSummaryMaxWidth {
		raw = runewidth.Truncate(raw, scriptEventSummaryMaxWidth-1, "…")
	}
	return raw
}

// scriptArrow returns the "→" arrow used inside Summary strings (between
// intent/condition and the rendered value). Downgrades to ASCII "->" when
// ascii==true so RNIX_ASCII=1 terminals do not leak U+2192. Centralised so
// every formatScriptEventSummary caller stays consistent.
func scriptArrow(ascii bool) string {
	if ascii {
		return "->"
	}
	return "→"
}

// scriptGlyph returns the leading glyph for a Script summary, downgraded to
// ASCII when ascii==true. Mapping mirrors the spec AC#3 fallback table.
func scriptGlyph(kind string, ascii bool) string {
	if ascii {
		switch kind {
		case "begin":
			return ">"
		case "ok":
			return "."
		case "err":
			return "x"
		case "stopped":
			return "/"
		case "spawn":
			return ">"
		case "while":
			return "@"
		}
		return "?"
	}
	switch kind {
	case "begin":
		return "▸"
	case "ok":
		return "✓"
	case "err":
		return "✗"
	case "stopped":
		return "⊘"
	case "spawn":
		return "↳"
	case "while":
		return "↻"
	}
	return "?"
}

// scriptLineLabel returns "L<n>" if args carries an int line number, else
// "L?" — keeps the leading column predictable even when meta is missing.
func scriptLineLabel(args map[string]any) string {
	v, ok := args["line"]
	if !ok {
		return "L?"
	}
	switch n := v.(type) {
	case int:
		return fmt.Sprintf("L%d", n)
	case int64:
		return fmt.Sprintf("L%d", n)
	case float64:
		return fmt.Sprintf("L%d", int(n))
	}
	return "L?"
}

// scriptStmtKindLabel returns args["stmt_kind"] as string, or "?" if missing.
func scriptStmtKindLabel(args map[string]any) string {
	kind := getArgString(args, "stmt_kind")
	if kind == "" {
		return "?"
	}
	return kind
}

// getArgString fetches args[key] as a string. Non-string and missing values
// return "" so callers can use the empty check as a presence proxy.
func getArgString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// getArgBool fetches args[key] as a bool. Non-bool and missing values
// return false (the AC#3 default for stopped/parallel/result).
func getArgBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// mergeScriptEvent routes a single SyscallEventWire through the Script*
// translation pipeline (Story 43-3). Returns the updated dashboardModel so
// callers can use it under Bubble Tea's value-passing semantics:
//
//	m = m.mergeScriptEvent(ev, msg.uuid)
//
// Behaviour:
//   - non-Script syscalls are silently dropped (scriptEventFromSyscall
//     returns false);
//   - the model-level lastScriptEventMs watermark gates duplicates so
//     incremental ListEvents fetches do not re-emit prior events;
//   - the UUID is injected from msg.uuid to keep events tied to the
//     specific process instance even when PID is later reused (Dev Notes #5).
func (m dashboardModel) mergeScriptEvent(ev ipc.SyscallEventWire, uuid string) dashboardModel {
	// Watermark uses strict less-than so events sharing the same
	// TimestampMs (e.g. a tight ScriptStmtBegin/End pair) both pass; the
	// downstream sysEventDedup (key includes Summary) is the real dedup
	// gate. See Story 43-3 review patch P4.
	if ev.TimestampMs < m.lastScriptEventMs {
		return m
	}
	ue, ok := scriptEventFromSyscall(ev)
	if !ok {
		return m
	}
	ue.UUID = uuid
	deduped := sysEventDedup([]UnifiedEvent{ue}, m.sysEventSeen)
	m.sysEvents = append(m.sysEvents, deduped...)
	m.sysEvents = sysEventFIFO(m.sysEvents, m.sysEventSeen)
	if ev.TimestampMs > m.lastScriptEventMs {
		m.lastScriptEventMs = ev.TimestampMs
	}
	return m
}
