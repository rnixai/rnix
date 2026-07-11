package wire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ProtocolVersion identifies the rnix.sock wire contract consumed by apex.
// Any incompatible field or envelope change must update the contract tests in
// both rnix and apex before release.
const ProtocolVersion = "rnix-ipc-wire/v1"

type Method string

const (
	MethodPing         Method = "ping"
	MethodSpawn        Method = "spawn"
	MethodDaemonStatus Method = "daemon_status"
	MethodAttachDebug  Method = "attach_debug"
	MethodListEvents   Method = "list_events"
	// MethodResume is the one-shot resume (response only, no event stream).
	MethodResume Method = "resume"
	// MethodResumeWatch resumes a process and keeps the connection open,
	// streaming the same StreamEvent shapes as MethodSpawn (initial Response
	// with ResumeResponse payload, then progress events until complete/error).
	// apex 10-11 B-route continuation consumes this for planning-session
	// continuations (resume --fork + NewInput).
	MethodResumeWatch Method = "resume_watch"
)

type Request struct {
	Method  Method          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorPayload   `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SpawnRequest struct {
	Intent           string   `json:"intent"`
	Agent            string   `json:"agent,omitempty"`
	Model            string   `json:"model,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	FallbackModel    string   `json:"fallback_model,omitempty"`
	FallbackProvider string   `json:"fallback_provider,omitempty"`
	ReasoningEffort  string   `json:"reasoning_effort,omitempty"`
	MaxSteps         int      `json:"max_steps,omitempty"`
	ContextBudget    int      `json:"context_budget,omitempty"`
	MaxTokens        int64    `json:"max_tokens,omitempty"`
	TimeoutMs        int64    `json:"timeout_ms,omitempty"`
	ParentPID        uint64   `json:"parent_pid,omitempty"`
	TraceID          string   `json:"trace_id,omitempty"`
	ParentSpanID     string   `json:"parent_span_id,omitempty"`
	ProjectDir       string   `json:"project_dir,omitempty"`
	RnixEnv          string   `json:"rnix_env,omitempty"`
	ComposeNode      string   `json:"compose_node,omitempty"`
	ComposeDeps      []string `json:"compose_deps,omitempty"`
	PipelineIndex    int      `json:"pipeline_index"`
	PipelineTotal    int      `json:"pipeline_total"`
}

type SpawnResponse struct {
	PID  uint64 `json:"pid"`
	UUID string `json:"uuid,omitempty"`
}

// ResumeRequest is the payload for MethodResume / MethodResumeWatch.
// Field set must stay in sync with ipc.ResumeRequest (wire_drift_test.go).
type ResumeRequest struct {
	UUID string `json:"uuid"`
	Fork bool   `json:"fork,omitempty"`

	// FromStep truncates history replay at this step before continuing.
	// 0 = no truncation. History path only.
	FromStep int `json:"from_step,omitempty"`

	// ProjectDir is the project root for ProjectConfig resolution
	// (.env / providers.yaml / agent dirs). Empty = global mode.
	ProjectDir string `json:"project_dir,omitempty"`
	RnixEnv    string `json:"rnix_env,omitempty"`

	// NewInput — apex 10-11 B-route continuation: appended as the next user
	// turn after the historical context is restored and before reasoning
	// continues (both checkpoint and history paths; the history path also
	// restores the previous round's final assistant output first, anti-replay).
	// Empty = pure resume semantics, zero behavior change.
	NewInput string `json:"new_input,omitempty"`
}

// ResumeResponse is the response payload for MethodResume / MethodResumeWatch.
type ResumeResponse struct {
	PID             uint64 `json:"pid"`
	UUID            string `json:"uuid"`
	ResumedFromStep int    `json:"resumed_from_step"`
}

type StreamEvent struct {
	Type    StreamEventType `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type StreamEventType string

const (
	StreamProgress     StreamEventType = "progress"
	StreamComplete     StreamEventType = "complete"
	StreamError        StreamEventType = "error"
	StreamSyscallEvent StreamEventType = "syscall_event"
	StreamEOF          StreamEventType = "eof"
)

type ProgressPayload struct {
	Event           string   `json:"event"`
	PID             uint64   `json:"pid"`
	Intent          string   `json:"intent,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Model           string   `json:"model,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	UUID            string   `json:"uuid,omitempty"`
	Warning         string   `json:"warning,omitempty"` // Story 66.4: spawn-time fallback resolve failure (additive)
	Step            int      `json:"step,omitempty"`
	Total           int      `json:"total,omitempty"`
	ThinkingText    string   `json:"thinking_text,omitempty"`
	Action          string   `json:"action,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	HasError        bool     `json:"has_error,omitempty"`
	DurationMs      float64  `json:"duration_ms,omitempty"`
	Result          string   `json:"result,omitempty"`
	ExitCode        int      `json:"exit_code,omitempty"`
	ExitReason      string   `json:"exit_reason,omitempty"`
	TokensUsed      int      `json:"tokens_used,omitempty"`
	SpanID          string   `json:"span_id,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
	StemMatches     []any    `json:"stem_matches,omitempty"`
	StemSelected    []string `json:"stem_selected,omitempty"`
	FromMemory      bool     `json:"from_memory,omitempty"`
}

type PingResponse struct {
	Version string `json:"version"`
}

type DaemonStatusResponse struct {
	Version         string          `json:"version"`
	DaemonCommit    string          `json:"daemon_commit"`
	DaemonBuildDate string          `json:"daemon_build_date"`
	DaemonPID       int             `json:"daemon_pid,omitempty"`
	StartedAt       int64           `json:"started_at_ms,omitempty"`
	FeatureProfile  string          `json:"feature_profile"`
	FeatureFlags    map[string]bool `json:"feature_flags,omitempty"`
}

type AttachDebugRequest struct {
	PID  uint64 `json:"pid"`
	UUID string `json:"uuid,omitempty"`
}

type ListEventsRequest struct {
	PID  uint64 `json:"pid"`
	UUID string `json:"uuid,omitempty"`
}

type ListEventsResponse struct {
	Events []SyscallEventWire `json:"events"`
}

type SyscallEventWire struct {
	TimestampMs int64          `json:"timestamp_ms"`
	PID         uint64         `json:"pid"`
	Syscall     string         `json:"syscall"`
	Args        map[string]any `json:"args,omitempty"`
	Result      any            `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMs  float64        `json:"duration_ms"`
	TraceID     string         `json:"trace_id,omitempty"`
	SpanID      string         `json:"span_id,omitempty"`
}

func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "rnix", "rnix.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("rnix-%d", os.Getuid()), "rnix.sock")
}
