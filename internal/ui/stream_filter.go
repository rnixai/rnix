package ui

import (
	"github.com/rnixai/rnix/internal/types"
)

// IsStreamFragment reports whether a Driver* syscall event is a streaming
// fragment that the kernel block aggregator (Story 65.1) absorbs into an
// aggregate event — human-readable default mode skips these rows.
//
// The predicate mirrors the kernel absorption semantics
// (kernel/observe.go thinking accumulator + input_delta handling): any event
// absorbed there is guaranteed a compensating subtype="aggregate" row, so
// skipping it loses no information.
//
//   - DriverThinking: content != "" && content != subtype && subtype != "aggregate"
//     covers claude/qwen (subtype=delta), cursor (passthrough subtype),
//     API drivers and codex (no subtype key). "started" markers
//     (content == subtype) and aggregate rows pass through.
//   - DriverToolCall: content == "input_delta" (claude-family partial input;
//     the event carries no subtype key, so content is the discriminator).
func IsStreamFragment(event types.SyscallEvent) bool {
	content, _ := event.Args["content"].(string)
	switch event.Syscall {
	case "DriverThinking":
		subtype, _ := event.Args["subtype"].(string)
		return content != "" && content != subtype && subtype != "aggregate"
	case "DriverToolCall":
		return content == "input_delta"
	default:
		return false
	}
}
