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
func isFailedResult(result string) bool {
	if result == "" {
		return true
	}
	lower := strings.ToLower(result)
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "timeout")
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
