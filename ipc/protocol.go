package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// Method represents an IPC request method.
type Method string

const (
	MethodPing                   Method = "ping"
	MethodSpawn                  Method = "spawn"
	MethodListProcs              Method = "list_procs"
	MethodKill                   Method = "kill"
	MethodAttachDebug            Method = "attach_debug"
	MethodAttachLog              Method = "attach_log"
	MethodShutdown               Method = "shutdown"
	MethodSpawnPipeline          Method = "spawn_pipeline"
	MethodExecScript             Method = "exec_script"
	MethodAttachGdb              Method = "attach_gdb"
	MethodDetachGdb              Method = "detach_gdb"
	MethodGdbCommand             Method = "gdb_command"
	MethodRecordStart            Method = "record_start"
	MethodRecordStop             Method = "record_stop"
	MethodRecordList             Method = "record_list"
	MethodReplayLoad             Method = "replay_load"
	MethodForkContinue           Method = "fork_continue"
	MethodCtxProfile             Method = "ctx_profile"
	MethodCtxGrowth              Method = "ctx_growth"
	MethodApplyIntent            Method = "apply_intent"
	MethodIntentStatus           Method = "intent_status"
	MethodIntentConfirm          Method = "intent_confirm"
	MethodApplyIncrementalIntent Method = "apply_incremental_intent"
	MethodIntentList             Method = "intent_list"
	MethodLineage                Method = "lineage"
	MethodProviderStatus         Method = "provider_status"
	MethodBudgetStatus           Method = "budget_status"
	MethodSLAStatus              Method = "sla_status"
	MethodReputationStatus       Method = "reputation_status"
	MethodSynergyList            Method = "synergy_list"
	MethodImmuneStatus           Method = "immune_status"
	MethodImmuneResume           Method = "immune_resume"
	MethodSimilarityQuery        Method = "similarity_query"
	MethodTopologyQuery          Method = "topology_query"
	MethodGetStepDetail          Method = "get_step_detail"
	MethodListSteps              Method = "list_steps"
	MethodListEvents             Method = "list_events"
	MethodGetProcDetail          Method = "get_proc_detail"
	MethodTraceList              Method = "trace_list"
	MethodTraceTree              Method = "trace_tree"
	MethodListAllProcs           Method = "list_all_procs"
	MethodSuspend                Method = "suspend"
	MethodResume                 Method = "resume"
	MethodHeartbeatStatus        Method = "heartbeat_status"
	MethodCompact                Method = "compact"
	MethodAnswerUser             Method = "answer_user"
	MethodSignalTree             Method = "signal_tree"
	MethodListResumable          Method = "list_resumable"
	MethodGetResumeLineage       Method = "get_resume_lineage"

	// Story 44.4 — dedicated subtree pause/resume methods. dashboard p/r keys
	// route through these instead of emitting raw SIGPAUSE/SIGRESUME via
	// SignalTree, so the wire intent ("pause this subtree") is self-describing.
	MethodPauseSubtree  Method = "pause_subtree"
	MethodResumeSubtree Method = "resume_subtree"

	// Story 42.5 — disk gc (.rnix/data/steps/<uuid>/) governance.
	MethodGc       Method = "gc"
	MethodGcDryRun Method = "gc_dry_run"

	// Story 45.1 — daemon build provenance (commit + build_date + pid +
	// started_at). Distinct from MethodPing (which only returns version) so
	// downstream consumers can verify which rnix commit the running daemon
	// was built from without a Ping wire change.
	MethodDaemonStatus Method = "daemon_status"
)

// --- Trace Wire Types (Story 27.9) ---

type TraceSummaryWire struct {
	TraceID         string `json:"trace_id"`
	SpanCount       int    `json:"span_count"`
	StartTimeMs     int64  `json:"start_time_ms"`
	TotalDurationMs int64  `json:"total_duration_ms"`
	RootSpanName    string `json:"root_span_name"`
}

type TraceListResponse struct {
	Traces []TraceSummaryWire `json:"traces"`
}

type TraceTreeRequest struct {
	TraceID string `json:"trace_id"`
}

type TraceTreeResponse struct {
	Tree *SpanTreeWire `json:"tree"`
}

type SpanTreeWire struct {
	Root     *SpanNodeWire `json:"root"`
	TraceID  string        `json:"trace_id"`
	Metadata TraceMetaWire `json:"metadata"`
}

type SpanNodeWire struct {
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	PID          uint64         `json:"pid"`
	Name         string         `json:"name"`
	DurationMs   int64          `json:"duration_ms"`
	TokensUsed   int            `json:"tokens_used"`
	Status       string         `json:"status"`
	Children     []SpanNodeWire `json:"children,omitempty"`
}

type TraceMetaWire struct {
	TotalSpans      int   `json:"total_spans"`
	TotalTokens     int   `json:"total_tokens"`
	TotalDurationMs int64 `json:"total_duration_ms"`
	ErrorCount      int   `json:"error_count"`
}

// Request is the top-level IPC request envelope (NDJSON).
type Request struct {
	Method  Method          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is the top-level IPC response envelope (NDJSON).
type Response struct {
	OK      bool            `json:"ok"`                // Required. True if the request succeeded.
	Payload json.RawMessage `json:"payload,omitempty"` // Optional. Contains the method-specific response struct (JSON).
	Error   *ErrorPayload   `json:"error,omitempty"`   // Optional. Present only when OK is false.
}

// ErrorPayload carries structured error information across IPC.
type ErrorPayload struct {
	Code    string `json:"code"`    // Required. Machine-readable error code (e.g., "not_found", "already_recording").
	Message string `json:"message"` // Required. Human-readable error description.
}

// --- Spawn ---

// SpawnRequest is the payload for MethodSpawn.
type SpawnRequest struct {
	Intent           string `json:"intent"`
	Agent            string `json:"agent,omitempty"`
	Model            string `json:"model,omitempty"`
	Provider         string `json:"provider,omitempty"`
	FallbackModel    string `json:"fallback_model,omitempty"`
	FallbackProvider string `json:"fallback_provider,omitempty"`
	MaxSteps         int    `json:"max_steps,omitempty"`
	ContextBudget    int    `json:"context_budget,omitempty"`
	TimeoutMs        int64  `json:"timeout_ms,omitempty"`
	TraceID          string `json:"trace_id,omitempty"`
	ParentSpanID     string `json:"parent_span_id,omitempty"`
	ProjectDir       string   `json:"project_dir,omitempty"`
	RnixEnv          string   `json:"rnix_env,omitempty"`
	ComposeNode      string   `json:"compose_node,omitempty"`
	ComposeDeps      []string `json:"compose_deps,omitempty"`
	PipelineIndex    int      `json:"pipeline_index"`
	PipelineTotal    int      `json:"pipeline_total"`
}

// SpawnResponse is the initial (non-streaming) response to a Spawn.
type SpawnResponse struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// --- ListProcs ---

// ListProcsResponse is the payload for MethodListProcs.
type ListProcsResponse struct {
	Processes []ProcInfoWire `json:"processes"`
}

// ProcInfoWire is the wire-format representation of vfs.ProcInfo.
// Times are serialized as milliseconds for JSON portability.
type ProcInfoWire struct {
	PID             types.PID          `json:"pid"`
	UUID            string             `json:"uuid,omitempty"`
	OriginUUID      string             `json:"origin_uuid,omitempty"`
	ResumedFromStep int                `json:"resumed_from_step,omitempty"`
	ExitReason      string             `json:"exit_reason,omitempty"`
	CtxSize         int                `json:"ctx_size,omitempty"`
	PPID            types.PID          `json:"ppid"`
	ParentUUID      string             `json:"parent_uuid,omitempty"`
	State           types.ProcessState `json:"state"`
	Intent          string             `json:"intent"`
	Skills          []string           `json:"skills"`
	TokensUsed      int                `json:"tokens_used"`
	CreatedAt       int64              `json:"created_at_ms"`
	DeadAt          int64              `json:"dead_at_ms,omitempty"`
	CtxID           types.CtxID        `json:"ctx_id"`
	Result          string             `json:"result,omitempty"`
	ContextBudget   int                `json:"context_budget,omitempty"`
	MaxTokens       int64              `json:"max_tokens,omitempty"`
	MaxCost         float64            `json:"max_cost,omitempty"`
	UsedCost        float64            `json:"used_cost,omitempty"`
	MaxSteps        int                `json:"max_steps,omitempty"`
	Provider        string             `json:"provider,omitempty"`
	Model           string             `json:"model,omitempty"`
	LastHeartbeatMs int64              `json:"last_heartbeat_ms,omitempty"`
	StepTimeoutMs   int64              `json:"step_timeout_ms,omitempty"`
	SuspendReason   string             `json:"suspend_reason,omitempty"`
	IsPaused        bool               `json:"is_paused,omitempty"`
	PausedAtMs      int64              `json:"paused_at_ms,omitempty"`
	PausedTotalMs   int64              `json:"paused_total_ms,omitempty"`
	ComposeNode     string             `json:"compose_node,omitempty"`
	ComposeDeps     []string           `json:"compose_deps,omitempty"`
	PipelineIndex   int                `json:"pipeline_index"`
	PipelineTotal   int                `json:"pipeline_total"`

	// Authoritative exit signal mirrored from vfs.ProcInfo. Without these two
	// fields the dashboard falls back to a result-text heuristic that misfires
	// when a successful process's Result contains words like "Error" (e.g. a
	// code-review report listing "AbortError"). ExitCode is meaningful only
	// when ExitCodeSet=true; omitempty keeps the wire compact for legacy and
	// not-yet-exited processes.
	ExitCode    int  `json:"exit_code,omitempty"`
	ExitCodeSet bool `json:"exit_code_set,omitempty"`
}

// ProcInfoToWire converts a vfs.ProcInfo to wire format.
func ProcInfoToWire(p vfs.ProcInfo) ProcInfoWire {
	skills := p.Skills
	if skills == nil {
		skills = []string{}
	}
	w := ProcInfoWire{
		PID:             p.PID,
		UUID:            p.UUID,
		OriginUUID:      p.OriginUUID,
		ResumedFromStep: p.ResumedFromStep,
		ExitReason:      p.ExitReason,
		CtxSize:         p.CtxSize,
		PPID:            p.PPID,
		ParentUUID:    p.ParentUUID,
		State:         p.State,
		Intent:        p.Intent,
		Skills:        skills,
		TokensUsed:    p.TokensUsed,
		CreatedAt:     p.CreatedAt.UnixMilli(),
		CtxID:         p.CtxID,
		Result:        p.Result,
		ContextBudget: p.ContextBudget,
		MaxTokens:     p.MaxTokens,
		MaxCost:       p.MaxCost,
		UsedCost:      p.UsedCost,
		MaxSteps:      p.MaxSteps,
		Provider:      p.Provider,
		Model:         p.Model,
		StepTimeoutMs: p.StepTimeout.Milliseconds(),
		SuspendReason: p.SuspendReason,
		IsPaused:      p.IsPaused,
		ComposeNode:   p.ComposeNode,
		ComposeDeps:   append([]string(nil), p.ComposeDeps...),
		PipelineIndex: p.PipelineIndex,
		PipelineTotal: p.PipelineTotal,
		ExitCode:      p.ExitCode,
		ExitCodeSet:   p.ExitCodeSet,
	}
	if !p.DeadAt.IsZero() {
		w.DeadAt = p.DeadAt.UnixMilli()
	}
	if !p.LastHeartbeat.IsZero() {
		w.LastHeartbeatMs = p.LastHeartbeat.UnixMilli()
	}
	if !p.PausedAt.IsZero() {
		w.PausedAtMs = p.PausedAt.UnixMilli()
	}
	if p.PausedTotal > 0 {
		w.PausedTotalMs = p.PausedTotal.Milliseconds()
	}
	return w
}

// WireToProcInfo converts a ProcInfoWire back to vfs.ProcInfo.
func WireToProcInfo(w ProcInfoWire) vfs.ProcInfo {
	p := vfs.ProcInfo{
		PID:             w.PID,
		UUID:            w.UUID,
		OriginUUID:      w.OriginUUID,
		ResumedFromStep: w.ResumedFromStep,
		ExitReason:      w.ExitReason,
		CtxSize:         w.CtxSize,
		PPID:            w.PPID,
		ParentUUID:    w.ParentUUID,
		State:         w.State,
		Intent:        w.Intent,
		Skills:        w.Skills,
		TokensUsed:    w.TokensUsed,
		CreatedAt:     unixMilliToTime(w.CreatedAt),
		CtxID:         w.CtxID,
		Result:        w.Result,
		ContextBudget: w.ContextBudget,
		MaxTokens:     w.MaxTokens,
		MaxCost:       w.MaxCost,
		UsedCost:      w.UsedCost,
		MaxSteps:      w.MaxSteps,
		Provider:      w.Provider,
		Model:         w.Model,
		StepTimeout:   time.Duration(w.StepTimeoutMs) * time.Millisecond,
		SuspendReason: w.SuspendReason,
		IsPaused:      w.IsPaused,
		ComposeNode:   w.ComposeNode,
		ComposeDeps:   w.ComposeDeps,
		PipelineIndex: w.PipelineIndex,
		PipelineTotal: w.PipelineTotal,
		ExitCode:      w.ExitCode,
		ExitCodeSet:   w.ExitCodeSet,
	}
	if w.DeadAt != 0 {
		p.DeadAt = unixMilliToTime(w.DeadAt)
	}
	if w.LastHeartbeatMs != 0 {
		p.LastHeartbeat = unixMilliToTime(w.LastHeartbeatMs)
	}
	if w.PausedAtMs != 0 {
		p.PausedAt = unixMilliToTime(w.PausedAtMs)
	}
	if w.PausedTotalMs > 0 {
		p.PausedTotal = time.Duration(w.PausedTotalMs) * time.Millisecond
	}
	return p
}

// --- Kill ---

// KillRequest is the payload for MethodKill.
type KillRequest struct {
	PID    types.PID    `json:"pid"`
	UUID   string       `json:"uuid,omitempty"`
	Signal types.Signal `json:"signal"`
}

// SignalTreeRequest is the payload for MethodSignalTree.
type SignalTreeRequest struct {
	PID    types.PID    `json:"pid"`
	UUID   string       `json:"uuid,omitempty"`
	Signal types.Signal `json:"signal"`
}

// SignalTreeResponse is the response for MethodSignalTree.
type SignalTreeResponse struct {
	Affected int `json:"affected"`
}

// --- Pause / Resume Subtree (Story 44.4) ---

// PauseSubtreeRequest is the payload for MethodPauseSubtree.
type PauseSubtreeRequest struct {
	PID types.PID `json:"pid"`
}

// PauseSubtreeResponse mirrors kernel.SuspendSubtree's (affected int, error)
// return: Affected counts the Running nodes transitioned to Suspended.
type PauseSubtreeResponse struct {
	Affected int `json:"affected"`
}

// ResumeSubtreeRequest is the payload for MethodResumeSubtree.
type ResumeSubtreeRequest struct {
	PID types.PID `json:"pid"`
}

// ResumeSubtreeResponse mirrors kernel.ResumeSubtree's
// (affected, skipped int, error) return: Affected counts Suspended nodes
// resumed to Running; Skipped counts Dead/Failed/Zombie/already-Running nodes
// passed over.
type ResumeSubtreeResponse struct {
	Affected int `json:"affected"`
	Skipped  int `json:"skipped"`
}

// --- Suspend ---

// SuspendRequest is the payload for MethodSuspend.
type SuspendRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// SuspendResponse is the response for MethodSuspend.
type SuspendResponse struct {
	PID            types.PID `json:"pid"`
	UUID           string    `json:"uuid"`
	State          string    `json:"state"`
	CheckpointStep int       `json:"checkpoint_step"`
}

// --- Resume ---

// ResumeRequest is the payload for MethodResume.
type ResumeRequest struct {
	UUID string `json:"uuid"`
	Fork bool   `json:"fork,omitempty"`

	// FromStep — Story 42.3: truncate history replay at this step before
	// continuing reasoning. 0 = no truncation. Effective only on the history
	// path (rejected when checkpoint exists and FromStep<cpData.LastStep).
	FromStep int `json:"from_step,omitempty"`

	// ProjectDir — Epic 42 fix: project root for ProjectConfig (.env, providers.yaml,
	// LLMFileOpener). Mirrors SpawnRequest.ProjectDir; required for full-state resume
	// so the resumed process uses project-level API keys / agent dirs / skill dirs.
	// Empty = pure global mode (back-compat with older clients).
	ProjectDir string `json:"project_dir,omitempty"`
	RnixEnv    string `json:"rnix_env,omitempty"`
}

// ResumeResponse is the response for MethodResume.
type ResumeResponse struct {
	PID             types.PID `json:"pid"`
	UUID            string    `json:"uuid"`
	ResumedFromStep int       `json:"resumed_from_step"`
}

// --- ListResumable (Story 42.2) ---

// ResumableProcessWire describes a process snapshot recoverable after a daemon
// crash. UUID/Intent/Provider/Model come from proc-info.json; LastStep prefers
// checkpoint.json then falls back to steps.jsonl tail; LastActive is the most
// recent timestamp available (checkpoint > DeadAt > CreatedAt).
type ResumableProcessWire struct {
	UUID       string `json:"uuid"`
	Intent     string `json:"intent"`
	Agent      string `json:"agent,omitempty"`
	LastStep   int    `json:"last_step"`
	LastActive int64  `json:"last_active_ms"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

// ListResumableResponse is the response for MethodListResumable.
type ListResumableResponse struct {
	Processes []ResumableProcessWire `json:"processes"`
}

// --- Gc (Story 42.5) ---

// GcCandidateWire is the wire-shape of one candidate for deletion. See
// kernel.GcCandidate for the internal twin; both use snake_case JSON tags.
type GcCandidateWire struct {
	UUID      string `json:"uuid"`
	DeadAt    string `json:"dead_at"`
	SizeBytes int64  `json:"size_bytes"`
	Reason    string `json:"reason"`
}

// GcRequest is the payload for MethodGc / MethodGcDryRun.
//
// Force is honored only by MethodGc — daemon-side gc always uses force=true.
// MethodGcDryRun ignores Force entirely; the CLI guard lives in cmd/rnix/gc.go.
type GcRequest struct {
	Force bool `json:"force,omitempty"`
}

// GcResponse is the response for MethodGc.
type GcResponse struct {
	OK           bool     `json:"ok"`
	RemovedCount int      `json:"removed_count"`
	FreedBytes   int64    `json:"freed_bytes"`
	RemovedUUIDs []string `json:"removed_uuids"`
}

// GcDryRunResponse is the response for MethodGcDryRun.
type GcDryRunResponse struct {
	OK         bool              `json:"ok"`
	DryRun     bool              `json:"dry_run"`
	Candidates []GcCandidateWire `json:"candidates"`
}

// MarshalJSON ensures RemovedUUIDs is `[]` instead of `null` when nil — JSON
// consumers (CLI render path, scripts) expect a list shape always.
func (r GcResponse) MarshalJSON() ([]byte, error) {
	type Alias GcResponse
	a := Alias(r)
	if a.RemovedUUIDs == nil {
		a.RemovedUUIDs = []string{}
	}
	return json.Marshal(a)
}

// MarshalJSON ensures Candidates is `[]` instead of `null` when nil.
func (r GcDryRunResponse) MarshalJSON() ([]byte, error) {
	type Alias GcDryRunResponse
	a := Alias(r)
	if a.Candidates == nil {
		a.Candidates = []GcCandidateWire{}
	}
	return json.Marshal(a)
}

// --- AttachDebug ---

// AttachDebugRequest is the payload for MethodAttachDebug.
type AttachDebugRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// --- AttachLog ---

// AttachLogRequest is the payload for MethodAttachLog.
type AttachLogRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// --- AttachGdb ---

// AttachGdbRequest is the payload for MethodAttachGdb.
type AttachGdbRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
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
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// GdbCommandRequest sends a gdb command to a process via the daemon.
type GdbCommandRequest struct {
	PID     types.PID `json:"pid"`
	UUID    string    `json:"uuid,omitempty"`
	Command string    `json:"command"`
	Args    []string  `json:"args,omitempty"`
}

// GdbCommandResponse carries the result of a gdb command.
type GdbCommandResponse struct {
	OK      bool   `json:"ok"`                // Required. True if the gdb command succeeded.
	Message string `json:"message,omitempty"` // Optional. Human-readable result or error description.
	Data    any    `json:"data,omitempty"`    // Optional. Command-specific result data (varies by gdb command).
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

	// intent-specific stream event types
	StreamIntentDecomposed        StreamEventType = "intent_decomposed"
	StreamIntentConfirmReq        StreamEventType = "intent_confirm_required"
	StreamIntentNodeStart         StreamEventType = "intent_node_start"
	StreamIntentNodeDone          StreamEventType = "intent_node_done"
	StreamIntentNodeFailed        StreamEventType = "intent_node_failed"
	StreamIntentProgress          StreamEventType = "intent_progress"
	StreamIntentComplete          StreamEventType = "intent_complete"
	StreamIntentNodeRetry         StreamEventType = "intent_node_retry"
	StreamIntentNodeTimeout       StreamEventType = "intent_node_timeout"
	StreamIntentDriftDetected     StreamEventType = "intent_drift_detected"
	StreamIntentDriftResolved     StreamEventType = "intent_drift_resolved"
	StreamIntentIncrementalMerged StreamEventType = "intent_incremental_merged"

	// tty ask_user stream event type (Story 33-2)
	StreamAskUser StreamEventType = "ask_user"
)

// ProgressPayload maps kernel callback events to IPC wire format.
type ProgressPayload struct {
	Event string    `json:"event"` // "spawn", "step", "step_complete", "complete", "error"
	PID   types.PID `json:"pid"`

	// OnSpawn
	Intent   string `json:"intent,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	UUID     string `json:"uuid,omitempty"`

	// OnStep / OnStepComplete
	Step  int `json:"step,omitempty"`
	Total int `json:"total,omitempty"`

	// OnStepComplete
	Action     string  `json:"action,omitempty"`
	Summary    string  `json:"summary,omitempty"`
	HasError   bool    `json:"has_error,omitempty"`
	DurationMs float64 `json:"duration_ms,omitempty"`

	// OnComplete
	Result     string `json:"result,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	ExitReason string `json:"exit_reason,omitempty"`
	TokensUsed int    `json:"tokens_used,omitempty"`
	SpanID     string `json:"span_id,omitempty"` // Process SpanID for trace parent-child (Story 15.1)

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
	TraceID     string         `json:"trace_id,omitempty"`
	SpanID      string         `json:"span_id,omitempty"`
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
		TraceID:     string(e.TraceID),
		SpanID:      string(e.SpanID),
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

// --- Daemon Status (Story 45.1) ---

// DaemonStatusResponse is the payload for MethodDaemonStatus. Extends
// MethodPing by surfacing build provenance so downstream consumers (e.g.
// EchoMatrix) can verify which rnix commit the running daemon was built from
// without reading rnix's git log.
//
// Wire field naming follows project convention: JSON snake_case + Go PascalCase.
type DaemonStatusResponse struct {
	Version         string `json:"version"`                   // SemVer (e.g. "0.8.0")
	DaemonCommit    string `json:"daemon_commit"`             // short SHA (e.g. "abc1234" or "abc1234+dirty")
	DaemonBuildDate string `json:"daemon_build_date"`         // RFC3339 (e.g. "2026-05-23T10:00:00Z") or ""
	DaemonPID      int    `json:"daemon_pid,omitempty"`      // daemon's OS PID (informational)
	StartedAt      int64  `json:"started_at_ms,omitempty"`   // daemon start time (UnixMilli)
}

// --- Spawn Pipeline ---

// SpawnPipelineRequest is the payload for MethodSpawnPipeline.
type SpawnPipelineRequest struct {
	Commands   []SpawnPipelineCommand `json:"commands"`
	ProjectDir string                 `json:"project_dir,omitempty"`
	RnixEnv    string                 `json:"rnix_env,omitempty"`
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
	Script     string            `json:"script"`
	Env        map[string]string `json:"env,omitempty"`
	ScriptDir  string            `json:"script_dir,omitempty"`
	ProjectDir string            `json:"project_dir,omitempty"`
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
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// RecordStartResponse is the response for MethodRecordStart.
type RecordStartResponse struct {
	RecordID string `json:"record_id"` // Required. Unique recording identifier in "<pid>-<unix_timestamp>" format.
}

// RecordStopRequest is the payload for MethodRecordStop.
type RecordStopRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// RecordStopResponse is the response for MethodRecordStop.
type RecordStopResponse struct {
	EventCount uint64 `json:"event_count"` // Required. Total number of events captured during the recording.
}

// RecordListResponse is the response for MethodRecordList.
type RecordListResponse struct {
	Records []RecordMetadataWire `json:"records"` // Required. List of all recording sessions (may be empty).
}

// RecordMetadataWire is the wire-format representation of debug.RecordMetadata.
type RecordMetadataWire struct {
	RecordID   string    `json:"record_id"`             // Required. Unique recording identifier.
	PID        types.PID `json:"pid"`                   // Required. PID of the recorded process.
	Intent     string    `json:"intent"`                // Required. Original intent of the recorded process.
	StartTime  int64     `json:"start_time_ms"`         // Required. Recording start time as Unix milliseconds.
	EndTime    int64     `json:"end_time_ms,omitempty"` // Optional. Recording end time; zero if still recording.
	EventCount uint64    `json:"event_count"`           // Required. Number of events captured so far.
	Status     string    `json:"status"`                // Required. One of "recording", "completed", "stopped".
}

// --- Replay ---

// ReplayLoadRequest is the payload for MethodReplayLoad.
type ReplayLoadRequest struct {
	RecordID string `json:"record_id"`
}

// ReplayLoadResponse is the response for MethodReplayLoad.
type ReplayLoadResponse struct {
	RecordID    string    `json:"record_id"`     // Required. The loaded recording's identifier.
	PID         types.PID `json:"pid"`           // Required. PID of the recorded process.
	Intent      string    `json:"intent"`        // Required. Original intent of the recorded process.
	EventCount  int       `json:"event_count"`   // Required. Total number of events in the recording.
	StartTimeMs int64     `json:"start_time_ms"` // Required. Recording start time as Unix milliseconds.
	EndTimeMs   int64     `json:"end_time_ms"`   // Required. Recording end time as Unix milliseconds.
	Status      string    `json:"status"`        // Required. One of "completed", "stopped".
}

// --- Fork Continue ---

// ForkContinueMessage represents a single message in a fork-continue request.
type ForkContinueMessage struct {
	Role       string `json:"role"`                   // Required. One of "user", "assistant", "system", "tool".
	Content    string `json:"content"`                // Required. Message text content.
	ToolCallID string `json:"tool_call_id,omitempty"` // Optional. Tool call ID for role="tool" messages.
}

// ForkMessageWire is an alias for ForkContinueMessage for backward compatibility.
type ForkMessageWire = ForkContinueMessage

// ForkContinueRequest is the payload for MethodForkContinue.
type ForkContinueRequest struct {
	Intent       string                `json:"intent"`        // Required. Intent for the new forked process.
	SystemPrompt string                `json:"system_prompt"` // Required. System prompt to set on the new context.
	Messages     []ForkContinueMessage `json:"messages"`      // Required. Message history to replay into the new context.
	OriginalPID  uint64                `json:"original_pid"`  // Required. PID of the original recorded process (used as PPID).
}

// ForkContinueResponse is the response for MethodForkContinue.
type ForkContinueResponse struct {
	PID       types.PID `json:"pid"`        // Required. PID of the newly created forked process.
	PPID      types.PID `json:"ppid"`       // Required. Parent PID (OriginalPID if valid, 0 otherwise).
	PPIDValid bool      `json:"ppid_valid"` // Required. True if OriginalPID process still exists in the process table.
}

// --- Ctx Profile ---

// CtxProfileRequest is the payload for MethodCtxProfile.
type CtxProfileRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// --- Ctx Growth ---

// CtxGrowthRequest is the payload for MethodCtxGrowth.
type CtxGrowthRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// --- Socket Path ---

// --- Apply Intent ---

// ApplyIntentRequest is the payload for MethodApplyIntent.
type ApplyIntentRequest struct {
	Intent    string `json:"intent"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`
	AutoStart bool   `json:"auto_start,omitempty"`
}

// ApplyIntentResponse is the initial response for MethodApplyIntent.
type ApplyIntentResponse struct {
	IntentID string          `json:"intent_id"`
	Tree     *IntentTreeWire `json:"tree"`
}

// IntentConfirmRequest is sent by the client to confirm or reject an intent.
type IntentConfirmRequest struct {
	IntentID string `json:"intent_id"`
	Confirm  bool   `json:"confirm"`
}

// --- Intent Status ---

// IntentStatusRequest is the payload for MethodIntentStatus.
type IntentStatusRequest struct {
	IntentID string `json:"intent_id,omitempty"`
}

// IntentStatusResponse is the response for MethodIntentStatus.
type IntentStatusResponse struct {
	Intents []*IntentTreeWire `json:"intents"`
}

// --- Apply Incremental Intent ---

// ApplyIncrementalIntentRequest is the payload for MethodApplyIncrementalIntent.
type ApplyIncrementalIntentRequest struct {
	IntentID string `json:"intent_id"`
	Intent   string `json:"intent"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// ApplyIncrementalIntentResponse is the response for MethodApplyIncrementalIntent.
type ApplyIncrementalIntentResponse struct {
	IntentID      string          `json:"intent_id"`
	Tree          *IntentTreeWire `json:"tree"`
	AddedNodes    []string        `json:"added_nodes"`
	ModifiedNodes []string        `json:"modified_nodes"`
}

// --- Intent Wire Types ---

// IntentTreeWire is the wire-format representation of intent.IntentTree.
type IntentTreeWire struct {
	ID            string                     `json:"id"`
	RootIntent    string                     `json:"root_intent"`
	State         string                     `json:"state"`
	Nodes         map[string]*IntentNodeWire `json:"nodes"`
	Drifts        []DriftItemWire            `json:"drifts,omitempty"`
	CreatedAtMs   int64                      `json:"created_at_ms"`
	CompletedAtMs int64                      `json:"completed_at_ms,omitempty"`
}

// IntentNodeWire is the wire-format representation of intent.IntentNode.
type IntentNodeWire struct {
	ID         string   `json:"id"`
	Intent     string   `json:"intent"`
	Agent      string   `json:"agent,omitempty"`
	Model      string   `json:"model,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
	State      string   `json:"state"`
	PID        uint64   `json:"pid,omitempty"`
	Result     string   `json:"result,omitempty"`
	Error      string   `json:"error,omitempty"`
	RetryCount int      `json:"retry_count,omitempty"`
	MaxRetries int      `json:"max_retries,omitempty"`
	TimeoutMs  int64    `json:"timeout_ms,omitempty"`
}

// IntentNodeEventPayload carries data for intent node stream events.
type IntentNodeEventPayload struct {
	NodeID       string `json:"node_id"`
	Intent       string `json:"intent,omitempty"`
	PID          uint64 `json:"pid,omitempty"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	Completed    int    `json:"completed,omitempty"`
	Total        int    `json:"total,omitempty"`
	RetryAttempt int    `json:"retry_attempt,omitempty"`
	MaxRetries   int    `json:"max_retries,omitempty"`
	DriftType    string `json:"drift_type,omitempty"`
}

// DriftItemWire is the wire-format representation of intent.DriftItem.
type DriftItemWire struct {
	NodeID       string `json:"node_id"`
	Type         string `json:"type"`
	Message      string `json:"message"`
	DetectedAtMs int64  `json:"detected_at_ms"`
}

// --- Lineage ---

// LineageRequest is the payload for MethodLineage.
type LineageRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// LineageEvent is the wire-format representation of kernel.LineageEvent.
type LineageEvent struct {
	TimestampMs int64    `json:"timestamp_ms"`
	Phase       string   `json:"phase"`
	Skills      []string `json:"skills"`
	Trigger     string   `json:"trigger"`
	FromMemory  bool     `json:"from_memory"`
}

// LineageResponse is the response for MethodLineage.
type LineageResponse struct {
	PID    types.PID      `json:"pid"`
	Events []LineageEvent `json:"events"`
}

// --- Provider Status ---

// ProviderStatusResponse is the payload for MethodProviderStatus.
type ProviderStatusResponse struct {
	Providers []ProviderStatusWire `json:"providers"`
}

// ProviderStatusWire is the wire-format representation of a provider's status.
type ProviderStatusWire struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Health string `json:"health"`           // "healthy", "unhealthy", "unchecked"
	Source string `json:"source,omitempty"` // "global", "project"
}

// --- Budget Status (Story 21.1) ---

// BudgetStatusRequest is the payload for MethodBudgetStatus.
type BudgetStatusRequest struct {
	GroupID types.PGID `json:"group_id"`
}

// BudgetStatusResponse is the response for MethodBudgetStatus.
type BudgetStatusResponse struct {
	TotalBudget int              `json:"total_budget"`
	Allocated   int              `json:"allocated"`
	Consumed    int              `json:"consumed"`
	Remaining   int              `json:"remaining"`
	Quotas      []AgentQuotaWire `json:"quotas"`
}

// AgentQuotaWire is the wire-format representation of a single agent's quota.
type AgentQuotaWire struct {
	PID      types.PID `json:"pid"`
	Name     string    `json:"name"`
	Priority string    `json:"priority"`
	Quota    int       `json:"quota"`
	Consumed int       `json:"consumed"`
}

// --- SLA Status (Story 21.2) ---

// SLAStatusRequest is the payload for MethodSLAStatus.
type SLAStatusRequest struct {
	GroupID types.PGID `json:"group_id"`
}

// SLAStatusResponse is the response for MethodSLAStatus.
type SLAStatusResponse struct {
	Results []SLAResultWire `json:"results"`
}

// SLAResultWire is the wire-format representation of kernel.SLAResult.
type SLAResultWire struct {
	AgentName  string                  `json:"agent_name"`
	PID        types.PID               `json:"pid,omitempty"`
	Passed     bool                    `json:"passed"`
	Checks     []kernel.SLACheckResult `json:"checks"`
	TokensUsed int                     `json:"tokens_used"`
	DurationMs int64                   `json:"duration_ms"`
}

// --- Reputation Status (Story 21.3) ---

// ReputationStatusRequest is the payload for MethodReputationStatus.
type ReputationStatusRequest struct {
	AgentName string `json:"agent_name,omitempty"` // empty = query all
}

// ReputationStatusResponse is the response for MethodReputationStatus.
type ReputationStatusResponse struct {
	Summaries []kernel.ReputationSummary `json:"summaries"`
}

// --- Synergy List (Story 21.5) ---

// SynergyListRequest is the payload for MethodSynergyList.
type SynergyListRequest struct{}

// SynergyListResponse is the response for MethodSynergyList.
type SynergyListResponse struct {
	Combos []kernel.ComboSummary `json:"combos"`
}

// --- Immune Status (Story 22.1) ---

// ImmuneStatusRequest is the payload for MethodImmuneStatus.
type ImmuneStatusRequest struct{}

// ImmuneStatusResponse is the response for MethodImmuneStatus.
type ImmuneStatusResponse struct {
	Running        bool                             `json:"running"`
	UptimeMs       int64                            `json:"uptime_ms"`
	ProfileCount   int                              `json:"profile_count"`
	Profiles       map[string]*kernel.NormalProfile `json:"profiles"`
	ActivePIDs     []uint64                         `json:"active_pids"`
	SuspendedPIDs  []uint64                         `json:"suspended_pids"`
	Alerts         []AlertWire                      `json:"alerts"`
	ThreatCount    int                              `json:"threat_count"`
	SecurityStatus string                           `json:"security_status"`
}

// AlertWire is the IPC wire format for AnomalyAlert.
type AlertWire struct {
	PID           uint64  `json:"pid"`
	AgentTemplate string  `json:"agent_template"`
	Type          string  `json:"type"`
	Detail        string  `json:"detail"`
	Deviation     float64 `json:"deviation"`
	TimestampMs   int64   `json:"timestamp_ms"`
}

// --- Immune Resume (Story 22.2) ---

// ImmuneResumeRequest is the payload for MethodImmuneResume.
type ImmuneResumeRequest struct {
	PID  uint64 `json:"pid"`
	UUID string `json:"uuid,omitempty"`
}

// ImmuneResumeResponse is the response for MethodImmuneResume.
type ImmuneResumeResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// --- Similarity Query (Story 22.4) ---

// SimilarityQueryRequest is the payload for MethodSimilarityQuery.
type SimilarityQueryRequest struct {
	AgentName string  `json:"agent_name"`
	MinScore  float64 `json:"min_score,omitempty"` // default 0.0
}

// SimilarityQueryResponse is the response for MethodSimilarityQuery.
type SimilarityQueryResponse struct {
	Agent        string                        `json:"agent"`
	Similarities []kernel.CapabilitySimilarity `json:"similarities"`
}

// --- Topology Query (Story 22.5) ---

// TopologyQueryRequest is the payload for MethodTopologyQuery.
type TopologyQueryRequest struct{}

// TopologyQueryResponse is the response for MethodTopologyQuery.
type TopologyQueryResponse struct {
	Nodes           []kernel.TopologyNode    `json:"nodes"`
	Edges           []kernel.CooperationEdge `json:"edges"`
	ReinforcedPaths []kernel.CooperationEdge `json:"reinforced_paths"`
}

// --- GetStepDetail (Story 27.2) ---

// GetStepDetailRequest is the payload for MethodGetStepDetail.
type GetStepDetailRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
	Step int       `json:"step"`
}

// GetStepDetailResponse is the response for MethodGetStepDetail.
type GetStepDetailResponse struct {
	SystemPrompt      string                 `json:"system_prompt"`
	Tools             []ToolDefWire          `json:"tools"`
	Step              int                    `json:"step"`
	Messages          []MessageWire          `json:"messages"`
	MessageCount      int                    `json:"message_count"`
	TokenCount        int                    `json:"token_count"`
	RawResponse       string                 `json:"raw_response"`
	Action            string                 `json:"action"`
	Summary           string                 `json:"summary"`
	ToolPath          string                 `json:"tool_path,omitempty"`
	ToolInput         string                 `json:"tool_input,omitempty"`
	ToolResult        string                 `json:"tool_result,omitempty"`
	ToolError         string                 `json:"tool_error,omitempty"`
	ToolDurationMs    float64                `json:"tool_duration_ms,omitempty"`
	RequestTokens     int                    `json:"request_tokens"`
	ResponseTokens    int                    `json:"response_tokens"`
	InputTokens       int                    `json:"input_tokens,omitempty"`
	OutputTokens      int                    `json:"output_tokens,omitempty"`
	CachedInputTokens int                    `json:"cached_input_tokens,omitempty"`
	ToolCalls         []ToolCallDetailWire   `json:"tool_calls,omitempty"`
	// Story 41.2 AC#3: step-level provider/driver info,used to compute cache
	// hit rate with correct driver-specific formula (Anthropic vs OpenAI 语义)。
	// wire-backward compatible · 旧 daemon 不填这两字段时 dashboard 走 fallback。
	Provider      string `json:"provider,omitempty"`
	DriverType    string `json:"driver_type,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// ToolCallDetailWire 是单次工具调用的 wire 表示,用于 Step Inspector 渲染 N 个 parallel
// tool calls。与 ToolCallWire(只含 LLM 发起信息)不同,本结构含 result/error/duration。
type ToolCallDetailWire struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name"`
	Path       string  `json:"path,omitempty"`
	Input      string  `json:"input,omitempty"`
	Result     string  `json:"result,omitempty"`
	Error      string  `json:"error,omitempty"`
	ErrorCode  string  `json:"error_code,omitempty"`
	DurationMs float64 `json:"duration_ms,omitempty"`
}

// MessageWire is the wire-format representation of context.Message.
type MessageWire struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCallWire `json:"tool_calls,omitempty"`
}

// ToolCallWire is the wire-format representation of context.ToolCall.
type ToolCallWire struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

// ToolDefWire is the wire-format representation of vfs.ToolDef.
type ToolDefWire struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// --- ListSteps (Story 27.3) ---

// ListStepsRequest is the payload for MethodListSteps.
type ListStepsRequest struct {
	PID       types.PID `json:"pid"`
	UUID      string    `json:"uuid,omitempty"`
	AfterStep int       `json:"after_step,omitempty"`
}

// StepSummaryWire is the wire-format summary of a single step.
type StepSummaryWire struct {
	Step        int     `json:"step"`
	Action      string  `json:"action"`
	Summary     string  `json:"summary"`
	ToolPath    string  `json:"tool_path,omitempty"`
	HasError    bool    `json:"has_error"`
	DurationMs  float64 `json:"duration_ms"`
	TokenCount  int     `json:"token_count"`
	TimestampMs int64   `json:"timestamp_ms,omitempty"` // offset from process start in ms
}

// ListStepsResponse is the response for MethodListSteps.
type ListStepsResponse struct {
	Steps []StepSummaryWire `json:"steps"`
	Total int               `json:"total"`
}

// --- ListEvents ---

// ListEventsRequest is the payload for MethodListEvents.
type ListEventsRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// ListEventsResponse is the response for MethodListEvents.
type ListEventsResponse struct {
	Events []SyscallEventWire `json:"events"`
}

// --- GetProcDetail (Story 27.6) ---

// GetProcDetailRequest is the payload for MethodGetProcDetail.
type GetProcDetailRequest struct {
	PID  types.PID `json:"pid"`
	UUID string    `json:"uuid,omitempty"`
}

// GetProcDetailResponse is the response for MethodGetProcDetail.
type GetProcDetailResponse struct {
	PID            types.PID         `json:"pid"`
	UUID           string            `json:"uuid"`
	PPID           types.PID         `json:"ppid"`
	State          string            `json:"state"`
	Intent         string            `json:"intent"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	CreatedAtMs    int64             `json:"created_at_ms"`
	DeadAtMs       int64             `json:"dead_at_ms,omitempty"`
	PausedTotalMs  int64             `json:"paused_total_ms,omitempty"`
	Skills         []SkillInfoWire   `json:"skills"`
	AllowedDevices []string          `json:"allowed_devices"`
	EnvSnapshot    map[string]string `json:"env_snapshot"`
	FDTable        []FDEntryWire     `json:"fd_table"`
	ContextStats   ContextStatsWire  `json:"context_stats"`
	ComposeNode    string            `json:"compose_node,omitempty"`
	ComposeDeps    []string          `json:"compose_deps,omitempty"`
	PipelineIndex  int               `json:"pipeline_index"`
	PipelineTotal  int               `json:"pipeline_total"`

	// Story 42.3 — Resume lineage fields (mirrored from proc.OriginUUID /
	// proc.ResumedFromStep for active procs, procInfoDisk for history).
	// Omitempty so普通 spawn 进程序列化时不带噪声字段。
	OriginUUID      string `json:"origin_uuid,omitempty"`
	ResumedFromStep int    `json:"resumed_from_step,omitempty"`
}

// SkillInfoWire is the wire-format representation of a loaded skill with its allowed tools.
type SkillInfoWire struct {
	Name         string   `json:"name"`
	AllowedTools []string `json:"allowed_tools"`
}

// FDEntryWire is the wire-format representation of an open file descriptor.
type FDEntryWire struct {
	FD         types.FD `json:"fd"`
	DevicePath string   `json:"device_path"`
}

// ContextStatsWire is the wire-format representation of context usage statistics.
type ContextStatsWire struct {
	MessageCount   int     `json:"message_count"`
	TokensUsed     int     `json:"tokens_used"`
	ContextBudget  int     `json:"context_budget"`
	UsagePct       float64 `json:"usage_pct"`
	SlotUsed       int     `json:"slot_used"`
	SlotMax        int     `json:"slot_max"`
	SlotPercentage float64 `json:"slot_percentage"`
}

func unixMilliToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

// --- Heartbeat Status (Story 30.6) ---

// HeartbeatStatusRequest is the payload for MethodHeartbeatStatus (empty).
type HeartbeatStatusRequest struct{}

// HeartbeatStatusResponse is the response for MethodHeartbeatStatus.
type HeartbeatStatusResponse struct {
	Running              bool              `json:"running"`
	CheckIntervalMs      int64             `json:"check_interval_ms"`
	TotalStalledDetected int               `json:"total_stalled_detected"`
	CurrentStalled       []StalledProcWire `json:"current_stalled"`
}

// StalledProcWire is the wire-format representation of a stalled process.
type StalledProcWire struct {
	PID               types.PID `json:"pid"`
	UUID              string    `json:"uuid"`
	ConsecutiveStalls int       `json:"consecutive_stalls"`
	StalledDurationMs int64     `json:"stalled_duration_ms"`
	HeartbeatGapMs    int64     `json:"heartbeat_gap_ms"`
	LastAction        string    `json:"last_action"`
}

// SocketPathOverride allows tests to inject a custom socket path.
var SocketPathOverride string

// --- Compact (Story 31.2) ---

// CompactRequest is the payload for MethodCompact.
type CompactRequest struct {
	PID                types.PID `json:"pid"`
	CustomInstructions string    `json:"custom_instructions,omitempty"`
}

// CompactResponse is the response for MethodCompact.
type CompactResponse struct {
	PreTokens  int      `json:"pre_tokens"`
	PostTokens int      `json:"post_tokens"`
	Restored   []string `json:"restored"`
}

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

// --- /dev/tty AskUser Wire Types (Story 33-2) ---

// AskUserEvent is sent from daemon to CLI as a StreamAskUser event during spawn.
type AskUserEvent struct {
	PID       types.PID       `json:"pid"`
	RequestID string          `json:"request_id"`
	Questions json.RawMessage `json:"questions"` // raw JSON array of Question
}

// AnswerUserRequest is sent from CLI to daemon via MethodAnswerUser.
type AnswerUserRequest struct {
	RequestID string          `json:"request_id"`
	Answers   json.RawMessage `json:"answers"` // raw JSON array of Answer
}

// --- Resume Lineage Wire Types (Story 42.3) ---

// GetResumeLineageRequest is the payload for MethodGetResumeLineage.
type GetResumeLineageRequest struct {
	UUID string `json:"uuid"`
}

// ResumeLineageNode is a single node in the resume lineage graph (current node,
// ancestor, or descendant). Mirrors a subset of procInfoDisk + ProcInfoWire
// fields relevant to lineage display in the Dashboard.
type ResumeLineageNode struct {
	UUID            string `json:"uuid"`
	OriginUUID      string `json:"origin_uuid,omitempty"`
	ResumedFromStep int    `json:"resumed_from_step,omitempty"`
	State           string `json:"state,omitempty"`
	Intent          string `json:"intent,omitempty"`
	CreatedAtMs     int64  `json:"created_at_ms,omitempty"`
}

// GetResumeLineageResponse is the response for MethodGetResumeLineage.
//
// Spec AC#7 字面要求顶层平铺 OriginUUID / ResumedFromStep 便于 wire-format
// 消费者直接读取；Current 同时保留嵌套结构（含 State/Intent/CreatedAtMs 等
// 完整字段），既满足 spec 字面，也保留富信息节点视图。
//
// Truncated=true when:
//   - OriginUUID 链中检测到循环（A→B→A），visited 集合阻断；
//   - 链深度超过 kernel.ResumeLineageMaxDepth（默认 32）。
//
// Ancestors order: 直接父节点在 [0]，最远祖先在末尾。
type GetResumeLineageResponse struct {
	Current         ResumeLineageNode   `json:"current"`
	OriginUUID      string              `json:"origin_uuid,omitempty"`
	ResumedFromStep int                 `json:"resumed_from_step,omitempty"`
	Ancestors       []ResumeLineageNode `json:"ancestors"`
	Descendants     []ResumeLineageNode `json:"descendants"`
	Truncated       bool                `json:"truncated,omitempty"`
}
