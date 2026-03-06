package kernel

import (
	"fmt"

	"github.com/usecrux/crux/internal/types"
)

// SyscallError represents an error that occurred during a syscall.
type SyscallError struct {
	Syscall string
	PID     types.PID
	Device  string
	Err     error
	Code    types.ErrCode
}

// Error returns a formatted error string: [Code] PID N Syscall: Device (Err)
func (e *SyscallError) Error() string {
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
