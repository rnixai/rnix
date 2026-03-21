package types

import (
	"encoding/json"
	"time"
)

// StepRecord captures the complete data of a single reasonStep execution.
// STUB: Created for ATDD red phase — fields defined per AC-1, not yet populated by kernel.
//
// Messages is stored as json.RawMessage to avoid import cycles with the context package.
// The kernel layer marshals []context.Message into this field before writing.
type StepRecord struct {
	Step           int             `json:"step"`
	Timestamp      time.Duration   `json:"timestamp"`
	Messages       json.RawMessage `json:"messages"`
	MessageCount   int             `json:"message_count"`
	TokenCount     int             `json:"token_count"`
	RawResponse    string          `json:"raw_response"`
	Action         string          `json:"action"`
	Summary        string          `json:"summary"`
	ToolPath       string          `json:"tool_path,omitempty"`
	ToolInput      string          `json:"tool_input,omitempty"`
	ToolResult     string          `json:"tool_result,omitempty"`
	ToolError      string          `json:"tool_error,omitempty"`
	ToolDuration   time.Duration   `json:"tool_duration,omitempty"`
	RequestTokens  int             `json:"request_tokens"`
	ResponseTokens int             `json:"response_tokens"`
}
