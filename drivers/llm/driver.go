// Package llm implements the LLM driver layer for Crux.
package llm

import (
	"context"
)

// LLMRequest represents a request to an LLM driver.
type LLMRequest struct {
	Intent       string        `json:"intent"`
	SystemPrompt string        `json:"system_prompt,omitempty"`
	Model        string        `json:"model,omitempty"`
	MaxTurns     int           `json:"max_turns,omitempty"`
	TimeoutMs int64 `json:"timeout_ms,omitempty"`
}

// LLMResponse represents a response from an LLM driver.
type LLMResponse struct {
	Content    string `json:"content"`
	TokensUsed int    `json:"tokens_used"`
}

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type       string `json:"type"` // "content", "done", "error"
	Content    string `json:"content,omitempty"`
	TokensUsed int    `json:"tokens_used,omitempty"`
	Err        error  `json:"-"`
}

// DriverInfo holds metadata about an LLM driver.
type DriverInfo struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
}

// LLMDriver is the interface that all LLM drivers must implement.
type LLMDriver interface {
	Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
	Info() DriverInfo
}
