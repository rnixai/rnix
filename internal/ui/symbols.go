package ui

import (
	"os"
	"strings"

	"github.com/rnixai/rnix/internal/types"
)

// StateSymbol returns a single-character status symbol for the given process
// state. For Dead processes, result is inspected to distinguish success (exit 0)
// from failure (exit != 0): an empty result or one containing "error", "fail",
// or "timeout" is treated as failure.
//
// When RNIX_ASCII=1, ASCII-safe characters are used instead of Unicode glyphs.
func StateSymbol(state types.ProcessState, result string) string {
	ascii := isASCIIMode()

	switch state {
	case types.StateRunning:
		if ascii {
			return "*"
		}
		return "●"
	case types.StateCreated:
		if ascii {
			return "o"
		}
		return "○"
	case types.StateDead:
		if isFailedResult(result) {
			if ascii {
				return "x"
			}
			return "✕"
		}
		if ascii {
			return "+"
		}
		return "✓"
	case types.StateZombie:
		if ascii {
			return "="
		}
		return "⏸"
	default:
		return "?"
	}
}

// isASCIIMode checks the RNIX_ASCII environment variable.
func isASCIIMode() bool {
	v := os.Getenv("RNIX_ASCII")
	return v == "1" || v == "true"
}

// isFailedResult returns true when the result string indicates failure:
// empty, or containing "error", "fail", or "timeout" (case-insensitive).
// Exception: "interrupted" / "cli_disconnected" mark CLI-disconnect suspends
// or user interruptions and must NOT render as failure — the script can be
// resumed via Dashboard, so the parent process is still in-flight.
func isFailedResult(result string) bool {
	if result == "" {
		return true
	}
	lower := strings.ToLower(result)
	if lower == "interrupted" || lower == "cli_disconnected" {
		return false
	}
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "timeout")
}

// StateBadge returns a coloured emoji or ASCII badge for a process state.
// For Dead processes, result distinguishes success (exit 0) from failure.
//
// Unicode: Running/Created → 🟢, Suspended → 🟡, Dead+fail → "" (row is styled red), Dead+success → "" (row is dimmed)
// ASCII:   [R], [S], [E], [D]
func StateBadge(state types.ProcessState, result string) string {
	ascii := isASCIIMode()

	switch state {
	case types.StateRunning, types.StateCreated:
		if ascii {
			return "[R]"
		}
		return "🟢"
	case types.StateSuspended:
		if ascii {
			return "[S]"
		}
		return "🟡"
	case types.StateDead, types.StateZombie:
		if isFailedResult(result) {
			if ascii {
				return "[E]"
			}
			return "" // dead+fail: no badge; caller styles the row red instead
		}
		if ascii {
			return "[D]"
		}
		return "" // dead+success: no badge; caller dims the row instead
	default:
		if ascii {
			return "[?]"
		}
		return "❓"
	}
}

// IsASCIIMode is the exported form of isASCIIMode.
// Returns true when RNIX_ASCII=1 or RNIX_ASCII=true.
func IsASCIIMode() bool {
	return isASCIIMode()
}

// IsFailedResult is the exported form of isFailedResult.
// Returns true when the result string indicates failure.
func IsFailedResult(result string) bool {
	return isFailedResult(result)
}

// IsProcessFailed returns true when the process is known to have failed.
//
// Authoritative path: when exitCodeSet=true, ExitCode != 0 marks failure.
// Fallback path: when exitCodeSet=false (legacy proc-info.json without these
// fields, or processes that haven't recorded an exit yet), falls back to the
// result-text heuristic. The fallback exists only for backward compatibility —
// new snapshots always set ExitCodeSet, eliminating false positives where a
// successful process's output happens to contain "error" / "fail" / "timeout"
// (e.g. code-review reports).
func IsProcessFailed(exitCode int, exitCodeSet bool, result string) bool {
	if exitCodeSet {
		return exitCode != 0
	}
	return isFailedResult(result)
}

// EventTypeIcon returns the display icon for a unified event type.
// In ASCII mode, emoji/Unicode symbols are replaced with terminal-safe equivalents.
func EventTypeIcon(eventType string) string {
	ascii := isASCIIMode()
	switch eventType {
	case "compact":
		if ascii {
			return "*"
		}
		return "★"
	case "budget":
		if ascii {
			return "(!)"
		}
		return "⚠"
	case "spawn":
		if ascii {
			return ">"
		}
		return "↳"
	case "exit":
		if ascii {
			return "<"
		}
		return "↲"
	case "stall":
		if ascii {
			return "(!)"
		}
		return "⚠"
	case "immune":
		if ascii {
			return "[I]"
		}
		return "🛡"
	default:
		return "·"
	}
}

// AlertSeverityIcon returns the icon for an alert severity level.
// In ASCII mode, error/critical use "[!]" and warn uses "(!)".
func AlertSeverityIcon(severity int) string {
	ascii := isASCIIMode()
	if ascii {
		if severity >= 2 {
			return "[!]"
		}
		return "(!)"
	}
	// SevError(2) / SevCritical(3) → 🔴, SevWarn(1) → ⚠
	if severity >= 2 {
		return "🔴"
	}
	return "⚠"
}
