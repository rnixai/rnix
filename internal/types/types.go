package types

import "time"

// PID represents a process identifier.
type PID uint64

// FD represents a file descriptor.
type FD int

// CtxID represents a context identifier.
type CtxID uint64

// ErrCode represents a categorized error code.
type ErrCode string

const (
	ErrTimeout    ErrCode = "TIMEOUT"
	ErrNotFound   ErrCode = "NOT_FOUND"
	ErrPermission ErrCode = "PERMISSION"
	ErrInternal   ErrCode = "INTERNAL"
	ErrDriver     ErrCode = "DRIVER"
)

// Signal represents a process signal.
type Signal int

const (
	SIGTERM Signal = iota + 1
	SIGKILL
)

// ProcessState represents the lifecycle state of a process.
type ProcessState int

const (
	StateCreated ProcessState = iota
	StateRunning
	StateZombie
	StateDead
)

// SyscallEvent records a single syscall invocation for tracing.
type SyscallEvent struct {
	Timestamp time.Time
	PID       PID
	Syscall   string
	Args      []any
	Result    any
	Err       error
	Duration  time.Duration
}
