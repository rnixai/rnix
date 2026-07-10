package main

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// shouldSkipStreamEvent reports whether a syscall event row is suppressed
// in human-readable default mode (fragments hidden unless --verbose).
// Shared by strace (main.go AttachDebug callback) and gdb (StreamGdbSyscall).
// --json output is never filtered: the json branch stays physically before
// this check in both callbacks (Story 65.2 裁决 4).
func shouldSkipStreamEvent(event types.SyscallEvent, jsonMode, verbose bool) bool {
	return !jsonMode && !verbose && ui.IsStreamFragment(event)
}
