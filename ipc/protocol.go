package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Method represents an IPC request method.
type Method string

const (
	MethodPing          Method = "ping"
	MethodSpawn         Method = "spawn"
	MethodListProcs     Method = "list_procs"
	MethodKill          Method = "kill"
	MethodAttachDebug   Method = "attach_debug"
	MethodAttachLog     Method = "attach_log"
	MethodShutdown      Method = "shutdown"
	MethodSpawnPipeline Method = "spawn_pipeline"
	MethodExecScript    Method = "exec_script"
	MethodAttachGdb     Method = "attach_gdb"
	MethodDetachGdb     Method = "detach_gdb"
	MethodGdbCommand    Method = "gdb_command"
	MethodRecordStart   Method = "record_start"
	MethodRecordStop    Method = "record_stop"
	MethodRecordList    Method = "record_list"
	MethodReplayLoad    Method = "replay_load"
	MethodForkContinue  Method = "fork_continue"
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
	Intent        string `json:"intent"`
	Agent         string `json:"agent,omitempty"`
	Model         string `json:"model,omitempty"`
	MaxSteps      int    `json:"max_steps,omitempty"`
	ContextBudget int    `json:"context_budget,omitempty"`
	TimeoutMs     int64  `json:"timeout_ms,omitempty"`
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
	PID           types.PID          `json:"pid"`
	PPID          types.PID          `json:"ppid"`
	State         types.ProcessState `json:"state"`
	Intent        string             `json:"intent"`
	Skills        []string           `json:"skills"`
	TokensUsed    int                `json:"tokens_used"`
	CreatedAt     int64              `json:"created_at_ms"`
	DeadAt        int64              `json:"dead_at_ms,omitempty"`
	CtxID         types.CtxID        `json:"ctx_id"`
	Result        string             `json:"result,omitempty"`
	ContextBudget int                `json:"context_budget,omitempty"`
}

// ProcInfoToWire converts a vfs.ProcInfo to wire format.
func ProcInfoToWire(p vfs.ProcInfo) ProcInfoWire {
	skills := p.Skills
	if skills == nil {
		skills = []string{}
	}
	w := ProcInfoWire{
		PID:           p.PID,
		PPID:          p.PPID,
		State:         p.State,
		Intent:        p.Intent,
		Skills:        skills,
		TokensUsed:    p.TokensUsed,
		CreatedAt:     p.CreatedAt.UnixMilli(),
		CtxID:         p.CtxID,
		Result:        p.Result,
		ContextBudget: p.ContextBudget,
	}
	if !p.DeadAt.IsZero() {
		w.DeadAt = p.DeadAt.UnixMilli()
	}
	return w
}

// WireToProcInfo converts a ProcInfoWire back to vfs.ProcInfo.
func WireToProcInfo(w ProcInfoWire) vfs.ProcInfo {
	p := vfs.ProcInfo{
		PID:           w.PID,
		PPID:          w.PPID,
		State:         w.State,
		Intent:        w.Intent,
		Skills:        w.Skills,
		TokensUsed:    w.TokensUsed,
		CreatedAt:     unixMilliToTime(w.CreatedAt),
		CtxID:         w.CtxID,
		Result:        w.Result,
		ContextBudget: w.ContextBudget,
	}
	if w.DeadAt != 0 {
		p.DeadAt = unixMilliToTime(w.DeadAt)
	}
	return p
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

// --- AttachLog ---

// AttachLogRequest is the payload for MethodAttachLog.
type AttachLogRequest struct {
	PID types.PID `json:"pid"`
}

// --- AttachGdb ---

// AttachGdbRequest is the payload for MethodAttachGdb.
type AttachGdbRequest struct {
	PID types.PID `json:"pid"`
}

// AttachGdbResponse is the initial response for MethodAttachGdb,
// containing a snapshot of process metadata at attach time.
type AttachGdbResponse struct {
	PID        types.PID          `json:"pid"`
	State      types.ProcessState `json:"state"`
	Intent     string             `json:"intent"`
	Skills     []string           `json:"skills"`
	TokensUsed int                `json:"tokens_used"`
}

// MarshalJSON ensures Skills is serialized as [] instead of null when nil.
func (r AttachGdbResponse) MarshalJSON() ([]byte, error) {
	type Alias AttachGdbResponse
	a := Alias(r)
	if a.Skills == nil {
		a.Skills = []string{}
	}
	return json.Marshal(a)
}

// DetachGdbRequest is sent by the client to explicitly detach from a gdb session.
type DetachGdbRequest struct {
	PID types.PID `json:"pid"`
}

// GdbCommandRequest sends a gdb command to a process via the daemon.
type GdbCommandRequest struct {
	PID     types.PID `json:"pid"`
	Command string    `json:"command"`
	Args    []string  `json:"args,omitempty"`
}

// GdbCommandResponse carries the result of a gdb command.
type GdbCommandResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// GdbEvent carries a single event on a gdb streaming connection.
type GdbEvent struct {
	Type    StreamEventType `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// LogEntryWire is the wire-format representation of types.LogEntry.
type LogEntryWire struct {
	TimestampMs int64     `json:"timestamp_ms"`
	PID         types.PID `json:"pid"`
	Step        int       `json:"step"`
	Category    string    `json:"category"`
	Content     string    `json:"content"`
	ToolPath    string    `json:"tool_path,omitempty"`
}

// LogEntryToWire converts a types.LogEntry to wire format.
func LogEntryToWire(e types.LogEntry) LogEntryWire {
	return LogEntryWire{
		TimestampMs: e.Timestamp.Milliseconds(),
		PID:         e.PID,
		Step:        e.Step,
		Category:    string(e.Category),
		Content:     e.Content,
		ToolPath:    e.ToolPath,
	}
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
	StreamLogEntry     StreamEventType = "log_entry"
	StreamEOF          StreamEventType = "eof"

	// gdb-specific stream event types
	StreamGdbSyscall     StreamEventType = "gdb_syscall"
	StreamGdbLog         StreamEventType = "gdb_log"
	StreamGdbStateChange StreamEventType = "gdb_state_change"
	StreamGdbPrompt      StreamEventType = "gdb_prompt"
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

// --- Spawn Pipeline ---

// SpawnPipelineRequest is the payload for MethodSpawnPipeline.
type SpawnPipelineRequest struct {
	Commands []SpawnPipelineCommand `json:"commands"`
}

// SpawnPipelineCommand describes one stage of a pipeline spawn.
type SpawnPipelineCommand struct {
	Intent string `json:"intent"`
	Agent  string `json:"agent,omitempty"`
	Model  string `json:"model,omitempty"`
}

// SpawnPipelineResponse is the final result of a pipeline spawn (sent as StreamComplete payload).
type SpawnPipelineResponse struct {
	Stages []PipelineStageWire `json:"stages"`
}

// PipelineStageWire is the wire-format result of a single pipeline stage.
type PipelineStageWire struct {
	PID        types.PID `json:"pid"`
	Intent     string    `json:"intent"`
	Result     string    `json:"result"`
	ExitCode   int       `json:"exit_code"`
	TokensUsed int       `json:"tokens_used"`
	ElapsedMs  int64     `json:"elapsed_ms"`
}

// --- Exec Script ---

// ExecScriptRequest is the payload for MethodExecScript.
type ExecScriptRequest struct {
	Script string            `json:"script"`
	Env    map[string]string `json:"env,omitempty"`
}

// ExecScriptResponse is the final result of a script execution (sent as StreamComplete payload).
type ExecScriptResponse struct {
	LastResult   string `json:"last_result"`
	LastExitCode int    `json:"last_exit_code"`
	TotalTokens  int    `json:"total_tokens"`
	ElapsedMs    int64  `json:"elapsed_ms"`
}

// --- Record ---

// RecordStartRequest is the payload for MethodRecordStart.
type RecordStartRequest struct {
	PID types.PID `json:"pid"`
}

// RecordStartResponse is the response for MethodRecordStart.
type RecordStartResponse struct {
	RecordID string `json:"record_id"`
}

// RecordStopRequest is the payload for MethodRecordStop.
type RecordStopRequest struct {
	PID types.PID `json:"pid"`
}

// RecordStopResponse is the response for MethodRecordStop.
type RecordStopResponse struct {
	EventCount uint64 `json:"event_count"`
}

// RecordListResponse is the response for MethodRecordList.
type RecordListResponse struct {
	Records []RecordMetadataWire `json:"records"`
}

// RecordMetadataWire is the wire-format representation of debug.RecordMetadata.
type RecordMetadataWire struct {
	RecordID   string    `json:"record_id"`
	PID        types.PID `json:"pid"`
	Intent     string    `json:"intent"`
	StartTime  int64     `json:"start_time_ms"`
	EndTime    int64     `json:"end_time_ms,omitempty"`
	EventCount uint64    `json:"event_count"`
	Status     string    `json:"status"`
}

// --- Replay ---

// ReplayLoadRequest is the payload for MethodReplayLoad.
type ReplayLoadRequest struct {
	RecordID string `json:"record_id"`
}

// ReplayLoadResponse is the response for MethodReplayLoad.
type ReplayLoadResponse struct {
	RecordID    string    `json:"record_id"`
	PID         types.PID `json:"pid"`
	Intent      string    `json:"intent"`
	EventCount  int       `json:"event_count"`
	StartTimeMs int64     `json:"start_time_ms"`
	EndTimeMs   int64     `json:"end_time_ms"`
	Status      string    `json:"status"`
}

// --- Fork Continue ---

// ForkContinueMessage represents a single message in a fork-continue request.
type ForkContinueMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ForkMessageWire is an alias for ForkContinueMessage for backward compatibility.
type ForkMessageWire = ForkContinueMessage

// ForkContinueRequest is the payload for MethodForkContinue.
type ForkContinueRequest struct {
	Intent       string                `json:"intent"`
	SystemPrompt string                `json:"system_prompt"`
	Messages     []ForkContinueMessage `json:"messages"`
	OriginalPID  uint64                `json:"original_pid"`
}

// ForkContinueResponse is the response for MethodForkContinue.
type ForkContinueResponse struct {
	PID      types.PID `json:"pid"`
	PPID     types.PID `json:"ppid"`
	PPIDValid bool     `json:"ppid_valid"`
}

// --- Socket Path ---

func unixMilliToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

// SocketPathOverride allows tests to inject a custom socket path.
var SocketPathOverride string

// SocketPath returns the platform-appropriate Unix socket path for the rnix daemon.
// Prefers $XDG_RUNTIME_DIR/rnix/rnix.sock, falls back to /tmp/rnix-$UID/rnix.sock.
func SocketPath() string {
	if SocketPathOverride != "" {
		return SocketPathOverride
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "rnix", "rnix.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("rnix-%d", os.Getuid()), "rnix.sock")
}
