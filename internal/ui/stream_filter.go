package ui

import (
	"github.com/rnixai/rnix/internal/types"
)

// IsStreamFragment reports whether a Driver* syscall event is a streaming
// fragment that the kernel block aggregator (Story 65.1) absorbs into an
// aggregate event — human-readable default mode skips these rows.
//
// ATDD 65.2 skeleton: always false until dev-story implements 裁决 1
// (mirror kernel/observe.go absorption predicate).
func IsStreamFragment(event types.SyscallEvent) bool {
	_ = event
	return false
}
