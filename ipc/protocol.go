package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// Method represents an IPC request method.
type Method string

const (
	MethodPing        Method = "ping"
	MethodSpawn       Method = "spawn"
	MethodListProcs   Method = "list_procs"
	MethodKill        Method = "kill"
	MethodAttachDebug Method = "attach_debug"
	MethodShutdown    Method = "shutdown"
)

// Request is the top-level IPC request envelope (NDJSON).
type Request struct {
	Method  Method          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is the top-level IPC response envelope (NDJSON).
type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorPayload   `json:"error,omitempty"`
}

// ErrorPayload carries structured error information across IPC.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// --- Spawn ---

// SpawnRequest is the payload for MethodSpawn.
type SpawnRequest struct {
	Intent   string `json:"intent"`
	Agent    string `json:"agent,omitempty"`
	Model    string `json:"model,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
}

// SpawnResponse is the initial (non-streaming) response to a Spawn.
type SpawnResponse struct {
	PID types.PID `json:"pid"`
}

// --- ListProcs ---

// ListProcsResponse is the payload for MethodListProcs.
type ListProcsResponse struct {
	Processes []ProcInfoWire `json:"processes"`
}

// ProcInfoWire is the wire-format representation of vfs.ProcInfo.
// Times are serialized as milliseconds for JSON portability.
type ProcInfoWire struct {
	PID        types.PID          `json:"pid"`
	PPID       types.PID          `json:"ppid"`
	State      types.ProcessState `json:"state"`
	Intent     string             `json:"intent"`
	Skills     []string           `json:"skills"`
	TokensUsed int                `json:"tokens_used"`
	CreatedAt  int64              `json:"created_at_ms"`
	CtxID      types.CtxID        `json:"ctx_id"`
	Result     string             `json:"result,omitempty"`
}

// ProcInfoToWire converts a vfs.ProcInfo to wire format.
func ProcInfoToWire(p vfs.ProcInfo) ProcInfoWire {
	skills := p.Skills
	if skills == nil {
		skills = []string{}
	}
	return ProcInfoWire{
		PID:        p.PID,
		PPID:       p.PPID,
		State:      p.State,
		Intent:     p.Intent,
		Skills:     skills,
		TokensUsed: p.TokensUsed,
		CreatedAt:  p.CreatedAt.UnixMilli(),
		CtxID:      p.CtxID,
		Result:     p.Result,
	}
}

// WireToProcInfo converts a ProcInfoWire back to vfs.ProcInfo.
func WireToProcInfo(w ProcInfoWire) vfs.ProcInfo {
	return vfs.ProcInfo{
		PID:        w.PID,
		PPID:       w.PPID,
		State:      w.State,
		Intent:     w.Intent,
		Skills:     w.Skills,
		TokensUsed: w.TokensUsed,
		CreatedAt:  unixMilliToTime(w.CreatedAt),
		CtxID:      w.CtxID,
		Result:     w.Result,
	}
}

// --- Kill ---

// KillRequest is the payload for MethodKill.
type KillRequest struct {
	PID    types.PID    `json:"pid"`
	Signal types.Signal `json:"signal"`
}

// --- AttachDebug ---

// AttachDebugRequest is the payload for MethodAttachDebug.
type AttachDebugRequest struct {
	PID types.PID `json:"pid"`
}

// --- Streaming ---

// StreamEvent carries a single event on a streaming IPC connection.
type StreamEvent struct {
	Type    StreamEventType `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// StreamEventType enumerates streaming event types.
type StreamEventType string

const (
	StreamProgress     StreamEventType = "progress"
	StreamComplete     StreamEventType = "complete"
	StreamError        StreamEventType = "error"
	StreamSyscallEvent StreamEventType = "syscall_event"
	StreamEOF          StreamEventType = "eof"
)

// ProgressPayload maps kernel callback events to IPC wire format.
type ProgressPayload struct {
	Event string    `json:"event"` // "spawn", "step", "complete", "error"
	PID   types.PID `json:"pid"`

	// OnSpawn
	Intent string `json:"intent,omitempty"`

	// OnStep
	Step  int `json:"step,omitempty"`
	Total int `json:"total,omitempty"`

	// OnComplete
	Result     string `json:"result,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	ExitReason string `json:"exit_reason,omitempty"`
	TokensUsed int    `json:"tokens_used,omitempty"`

	// OnError
	ErrorMessage string `json:"error_message,omitempty"`
}

// SyscallEventWire is the wire representation of types.SyscallEvent.
type SyscallEventWire struct {
	TimestampMs int64          `json:"timestamp_ms"`
	PID         types.PID      `json:"pid"`
	Syscall     string         `json:"syscall"`
	Args        map[string]any `json:"args,omitempty"`
	Result      any            `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMs  float64        `json:"duration_ms"`
}

// SyscallEventToWire converts a types.SyscallEvent to wire format.
func SyscallEventToWire(e types.SyscallEvent) SyscallEventWire {
	w := SyscallEventWire{
		TimestampMs: e.Timestamp.Milliseconds(),
		PID:         e.PID,
		Syscall:     e.Syscall,
		Args:        e.Args,
		Result:      e.Result,
		DurationMs:  float64(e.Duration.Microseconds()) / 1000.0,
	}
	if e.Err != nil {
		w.Error = e.Err.Error()
	}
	return w
}

// --- Ping ---

// PingResponse is the payload for MethodPing.
type PingResponse struct {
	Version string `json:"version"`
}

// --- Socket Path ---

func unixMilliToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

// SocketPathOverride allows tests to inject a custom socket path.
var SocketPathOverride string

// SocketPath returns the platform-appropriate Unix socket path for the crux daemon.
// Prefers $XDG_RUNTIME_DIR/crux/crux.sock, falls back to /tmp/crux-$UID/crux.sock.
func SocketPath() string {
	if SocketPathOverride != "" {
		return SocketPathOverride
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "crux", "crux.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("crux-%d", os.Getuid()), "crux.sock")
}
