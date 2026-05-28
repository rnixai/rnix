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

// TID represents a thread identifier (process-local).
type TID uint64

// CoID represents a coroutine identifier (process-local).
type CoID uint64

// TraceID represents a distributed trace identifier.
type TraceID string

// SpanID represents a span identifier within a trace.
type SpanID string

// ErrCode represents a categorized error code.
type ErrCode string

const (
	ErrTimeout    ErrCode = "TIMEOUT"
	ErrNotFound   ErrCode = "NOT_FOUND"
	ErrPermission ErrCode = "PERMISSION"
	ErrInternal   ErrCode = "INTERNAL"
	ErrDriver     ErrCode = "DRIVER"
	ErrInvalid    ErrCode = "INVALID"
	ErrIsDirectory        ErrCode = "IS_DIRECTORY"
	ErrBrokenPipe         ErrCode = "BROKEN_PIPE"
	ErrServiceUnavailable ErrCode = "SERVICE_UNAVAILABLE"
	ErrAlreadyMounted     ErrCode = "ALREADY_MOUNTED"
	ErrResourceExhausted  ErrCode = "RESOURCE_EXHAUSTED"
	// ErrForceKilled is returned by drivers/mcp/transport.Close when the
	// graceful SIGTERM timeout expires and the transport had to escalate to
	// SIGKILL. Kernel finishProcess uses errors.As + DriverError.Code ==
	// ErrForceKilled to annotate the Unmount event (Story 48.2 AC4 + AC7).
	// Lives in internal/types — not drivers/mcp — to avoid a kernel →
	// drivers/mcp reverse dependency (Story 48.2 易错点 #8).
	ErrForceKilled ErrCode = "FORCE_KILLED"
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
	StateCreated   ProcessState = iota // 0
	StateRunning                       // 1
	StateZombie                        // 2
	StateDead                          // 3
	StateSuspended                     // 4
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
	case StateSuspended:
		return "suspended"
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

// NewErrNotFound returns a formatted not-found error for the given resource type and identifier.
func NewErrNotFound(resourceType, name string) error {
	return fmt.Errorf("%s %q not found", resourceType, name)
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
	TraceID   TraceID
	SpanID    SpanID
}

// LogCategory classifies reasoning log entries.
type LogCategory string

const (
	LogThink   LogCategory = "think"
	LogTool    LogCategory = "tool"
	LogOutput  LogCategory = "output"
	LogWarning LogCategory = "warning"
)

// TokenSnapshot records cumulative token usage at a given reasoning step.
type TokenSnapshot struct {
	Step    int   // reasoning step number
	Tokens  int   // cumulative tokens at this step
	DeltaMs int64 // milliseconds since process creation
}

// LogEntry records a single reasoning log event for high-level agent tracing.
type LogEntry struct {
	Timestamp time.Duration
	PID       PID
	Step      int
	Category  LogCategory
	Content   string
	ToolPath  string // only populated for LogTool entries
}

// LLMFileOpener opens an LLM device file for a given provider and flags.
// Returns any (actually vfs.VFSFile) to avoid types → vfs dependency;
// callers do type assertion. Used by ProjectConfig to inject project-level drivers.
type LLMFileOpener func(provider string, flags int) (any, error)

// AgentLoaderFunc loads an agent definition by name. Returns any (actually
// *agents.AgentInfo) so that internal/config can carry a project-level loader
// without importing agents — which would create a cycle (agents → internal/config).
// Callers in kernel/ipc do the type assertion. Used by ProjectConfig to inject
// project-aware agent lookup into sub-agent spawn flows.
type AgentLoaderFunc func(name string) (any, error)

// SkillLoaderFunc loads a skill (with body) by name. Returns any (actually
// *skills.SkillInfo) for the same circular-dependency reason as AgentLoaderFunc.
// Used by ProjectConfig to inject project-aware skill lookup into specialize flows.
type SkillLoaderFunc func(name string) (any, error)
