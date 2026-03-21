package debug

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// RecordEventType classifies the type of a recorded event.
type RecordEventType string

const (
	RecordSyscall         RecordEventType = "syscall"
	RecordContextSnapshot RecordEventType = "context_snapshot"
	RecordLLMResponse     RecordEventType = "llm_response"
	RecordStateChange     RecordEventType = "state_change"
)

// RecordEvent represents a single recorded event in an execution recording.
type RecordEvent struct {
	SeqNum    uint64               `json:"seq_num"`
	Timestamp time.Duration        `json:"timestamp"`
	PID       types.PID            `json:"pid"`
	Type      RecordEventType      `json:"type"`
	Syscall   *SyscallEventData    `json:"syscall,omitempty"`
	Context   *ContextSnapshotData `json:"context,omitempty"`
	LLM       *LLMResponseData     `json:"llm,omitempty"`
	State     *StateChangeData     `json:"state,omitempty"`
}

// SyscallEventData holds syscall-specific data for a recorded event.
type SyscallEventData struct {
	Syscall  string         `json:"syscall"`
	Args     map[string]any `json:"args,omitempty"`
	Result   any            `json:"result,omitempty"`
	Err      string         `json:"err,omitempty"`
	Duration time.Duration  `json:"duration"`
}

// ContextSnapshotData holds a snapshot of the context state.
// Simplified in Story 27.1 AC-9: removed SystemPromptHash, MessageCount, TokenEstimate
// (StepRecord now captures complete data).
type ContextSnapshotData struct {
	Messages []string `json:"messages"`
}

// LLMResponseData holds LLM response metadata.
// Simplified in Story 27.1 AC-9: removed ResponseSummary
// (StepRecord now captures complete RawResponse).
type LLMResponseData struct {
	Model          string `json:"model"`
	RequestTokens  int    `json:"request_tokens"`
	ResponseTokens int    `json:"response_tokens"`
}

// StateChangeData records a process state transition.
type StateChangeData struct {
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Reason    string `json:"reason"`
}

// RecordStatus represents the status of a recording.
type RecordStatus string

const (
	RecordStatusRecording RecordStatus = "recording"
	RecordStatusCompleted RecordStatus = "completed"
	RecordStatusStopped   RecordStatus = "stopped"
)

// RecordMetadata holds metadata about a recording session.
type RecordMetadata struct {
	RecordID   string       `json:"record_id"`
	PID        types.PID    `json:"pid"`
	Intent     string       `json:"intent"`
	StartTime  time.Time    `json:"start_time"`
	EndTime    time.Time    `json:"end_time"`
	EventCount uint64       `json:"event_count"`
	Status     RecordStatus `json:"status"`
}

// RecordEventFromSyscall converts a types.SyscallEvent to a RecordEvent.
func RecordEventFromSyscall(ev types.SyscallEvent) RecordEvent {
	data := &SyscallEventData{
		Syscall:  ev.Syscall,
		Args:     ev.Args,
		Result:   ev.Result,
		Duration: ev.Duration,
	}
	if ev.Err != nil {
		data.Err = ev.Err.Error()
	}
	return RecordEvent{
		Timestamp: ev.Timestamp,
		PID:       ev.PID,
		Type:      RecordSyscall,
		Syscall:   data,
	}
}

// HashSystemPrompt returns a SHA-256 hash summary of a system prompt.
func HashSystemPrompt(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h[:8])
}

// TruncateString truncates a string to maxLen runes, appending "..." if truncated.
func TruncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
