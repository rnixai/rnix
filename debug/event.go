package debug

import (
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// EmitEvent sends a SyscallEvent to a DebugChan in a non-blocking manner.
// If ch is nil or the buffer is full, the call returns immediately without
// blocking or panicking.
func EmitEvent(ch chan<- types.SyscallEvent, event types.SyscallEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	default: // buffer full, drop event
	}
}

// NewEvent constructs a SyscallEvent populated with entry-side fields.
// Timestamp is computed as the elapsed time since the process was created.
// Result, Err, and Duration are left at their zero values for the caller
// to fill via CompleteEvent after the syscall returns.
func NewEvent(pid types.PID, createdAt time.Time, syscall string, args map[string]any) types.SyscallEvent {
	return types.SyscallEvent{
		Timestamp: time.Since(createdAt),
		PID:       pid,
		Syscall:   syscall,
		Args:      args,
	}
}

// CompleteEvent fills the exit-side fields of a SyscallEvent: Result, Err,
// and Duration. Call this after the syscall returns, passing the wall-clock
// duration measured by the caller (typically time.Since(start)).
func CompleteEvent(event *types.SyscallEvent, result any, err error, duration time.Duration) {
	event.Result = result
	event.Err = err
	event.Duration = duration
}
