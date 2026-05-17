package kernel

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
)

// SyscallError represents an error that occurred during a syscall.
type SyscallError struct {
	Syscall string
	PID     types.PID
	Device  string
	Err     error
	Code    types.ErrCode
	// compact, when true, makes Error() return only Err.Error() verbatim. Used by
	// specs that pin user-visible error strings (e.g. Story 42.3 AC#2:
	// "ErrInvalid: from_step N exceeds total steps (actual: M)"). Code is still
	// preserved for errors.As / IPC mapping.
	compact bool
}

// Error returns a formatted error string: [Code] PID N Syscall: Device (Err).
// When compact==true, returns only the inner Err's text for spec-pinned messages.
func (e *SyscallError) Error() string {
	if e.compact && e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("[%s] PID %d %s: %s (%v)", e.Code, e.PID, e.Syscall, e.Device, e.Err)
}

// Unwrap returns the underlying error for use with errors.Is and errors.As.
func (e *SyscallError) Unwrap() error {
	return e.Err
}

// NewSyscallError creates a new SyscallError.
func NewSyscallError(syscall string, pid types.PID, device string, err error, code types.ErrCode) *SyscallError {
	return &SyscallError{
		Syscall: syscall,
		PID:     pid,
		Device:  device,
		Err:     err,
		Code:    code,
	}
}

// NewCompactSyscallError creates a SyscallError whose Error() returns only the
// inner err's text. Use for spec-pinned user-facing messages where the standard
// "[CODE] PID N Syscall: Device (Err)" decoration is undesirable.
func NewCompactSyscallError(syscall string, code types.ErrCode, err error) *SyscallError {
	return &SyscallError{
		Syscall: syscall,
		Err:     err,
		Code:    code,
		compact: true,
	}
}
