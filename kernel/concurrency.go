package kernel

import (
	"github.com/usecrux/crux/internal/types"
)

// CoroutineFunc is the function executed by a coroutine.
// The yield parameter is used to yield control and pass a value to the caller.
type CoroutineFunc func(yield func(any)) any

// ConcurrencyManager manages thread and coroutine concurrency primitives.
type ConcurrencyManager interface {
	SpawnThread(parentPID types.PID, intent string) (types.TID, error)
	JoinThread(parentPID types.PID, tid types.TID) error
	SpawnCoroutine(parentPID types.PID, fn CoroutineFunc) (types.CoID, error)
	Yield(parentPID types.PID, coID types.CoID, value any) error
	ResumeCoroutine(parentPID types.PID, coID types.CoID) (any, error)
}

// Compile-time interface compliance check.
var _ ConcurrencyManager = (*KernelImpl)(nil)
