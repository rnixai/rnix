// Package llm implements the LLM driver layer for Rnix.
package llm

import (
	"context"
)

// Message represents a single message in a conversation.
// JSON tags are compatible with context.Message for VFS bridge interop.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// LLMRequest represents a request to an LLM driver.
type LLMRequest struct {
	Intent       string        `json:"intent"`
	SystemPrompt string        `json:"system_prompt,omitempty"`
	Model        string        `json:"model,omitempty"`
	MaxTurns     int           `json:"max_turns,omitempty"`
	TimeoutMs    int64         `json:"timeout_ms,omitempty"`
	Messages     []Message     `json:"messages,omitempty"`
	Temperature  *float64      `json:"temperature,omitempty"`
	MaxTokens    int           `json:"max_tokens,omitempty"`
}

// LLMResponse represents a response from an LLM driver.
type LLMResponse struct {
	Content      string     `json:"content"`
	Reasoning    string     `json:"reasoning,omitempty"`
	TokensUsed   int        `json:"tokens_used"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
}

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type         string         `json:"type"` // "content", "done", "error", "tool_call"
	Content      string         `json:"content,omitempty"`
	TokensUsed   int            `json:"tokens_used,omitempty"`
	InputTokens  int            `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
	Data         map[string]any `json:"data,omitempty"` // extra metadata (e.g., tool_call details)
	Err          error          `json:"-"`
}

// DriverInfo holds metadata about an LLM driver.
type DriverInfo struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
	DriverType   string `json:"driver_type"`
}

// LLMDriver is the interface that all LLM drivers must implement.
type LLMDriver interface {
	Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
	Info() DriverInfo
}

// HealthChecker is an optional interface for drivers that support health checks.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}
