package types

import (
	"fmt"
	"time"
)

// PID represents a process identifier.
type PID uint64

// FD represents a file descriptor.
type FD int

// CtxID represents a context identifier.
type CtxID uint64

// MsgSeq represents a globally unique message sequence number.
type MsgSeq uint64

// PGID represents a process group identifier.
type PGID uint64

// ErrCode represents a categorized error code.
type ErrCode string

const (
	ErrTimeout    ErrCode = "TIMEOUT"
	ErrNotFound   ErrCode = "NOT_FOUND"
	ErrPermission ErrCode = "PERMISSION"
	ErrInternal   ErrCode = "INTERNAL"
	ErrDriver     ErrCode = "DRIVER"
	ErrInvalid    ErrCode = "INVALID"
	ErrBrokenPipe ErrCode = "BROKEN_PIPE"
)

// Signal represents a process signal.
type Signal int

const (
	SIGTERM   Signal = iota + 1 // 1 — terminate (blockable, handleable)
	SIGKILL                     // 2 — force kill (not blockable, not handleable)
	SIGINT                      // 3 — interrupt (blockable, handleable)
	SIGPAUSE                    // 4 — pause reasoning loop (blockable, handleable)
	SIGRESUME                   // 5 — resume reasoning loop (blockable, handleable)
)

// Valid reports whether s is a recognized signal value.
func (s Signal) Valid() bool {
	return s >= SIGTERM && s <= SIGRESUME
}

// String returns the human-readable name for the Signal.
func (s Signal) String() string {
	switch s {
	case SIGTERM:
		return "SIGTERM"
	case SIGKILL:
		return "SIGKILL"
	case SIGINT:
		return "SIGINT"
	case SIGPAUSE:
		return "SIGPAUSE"
	case SIGRESUME:
		return "SIGRESUME"
	default:
		return fmt.Sprintf("Signal(%d)", s)
	}
}

// IsTermination reports whether the signal terminates the process by default.
func (s Signal) IsTermination() bool {
	return s == SIGTERM || s == SIGKILL || s == SIGINT
}

// Blockable reports whether the signal can be blocked. SIGKILL cannot be blocked.
func (s Signal) Blockable() bool {
	return s != SIGKILL
}

// ProcessState represents the lifecycle state of a process.
type ProcessState int

const (
	StateCreated ProcessState = iota
	StateRunning
	StateZombie
	StateDead
)

// String returns the human-readable name for the ProcessState.
func (s ProcessState) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateRunning:
		return "running"
	case StateZombie:
		return "zombie"
	case StateDead:
		return "dead"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// DriverError represents an error from a device driver with a categorized error code.
// Used by drivers instead of kernel.SyscallError to avoid drivers/ → kernel/ dependency.
type DriverError struct {
	Op     string
	Device string
	Err    error
	Code   ErrCode
}

// Error returns a formatted error string.
func (e *DriverError) Error() string {
	return fmt.Sprintf("[%s] %s: %s (%v)", e.Code, e.Op, e.Device, e.Err)
}

// Unwrap returns the underlying error for use with errors.Is and errors.As.
func (e *DriverError) Unwrap() error {
	return e.Err
}

// NewDriverError creates a new DriverError.
func NewDriverError(op, device string, err error, code ErrCode) *DriverError {
	return &DriverError{Op: op, Device: device, Err: err, Code: code}
}

// SyscallEvent records a single syscall invocation for tracing.
type SyscallEvent struct {
	Timestamp time.Duration
	PID       PID
	Syscall   string
	Args      map[string]any
	Result    any
	Err       error
	Duration  time.Duration
}
