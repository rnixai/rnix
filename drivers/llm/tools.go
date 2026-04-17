package llm

import (
	"context"
)

// ToolDef describes a tool that an LLM can invoke.
type ToolDef struct {
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`        // JSON Schema
	MaxResultTokens   int            `json:"max_result_tokens,omitempty"` // 0 = unlimited
	IsReadOnly        bool           `json:"is_read_only,omitempty"`
	IsConcurrencySafe bool           `json:"is_concurrency_safe,omitempty"`
	IsDestructive     bool           `json:"is_destructive,omitempty"`
	ShouldDefer       bool           `json:"should_defer,omitempty"`
	SearchHint        string         `json:"search_hint,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Input      map[string]any `json:"input,omitempty"`
	ParseError string         `json:"parse_error,omitempty"` // set when arguments JSON cannot be parsed
}

// ToolResult represents the outcome of executing a tool call.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ToolCallingDriver extends LLMDriver with tool-use capabilities.
type ToolCallingDriver interface {
	LLMDriver
	CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error)
	StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error)
}

// SkillsBundleCapable marks drivers that consume req.Skills directly — typically
// by materializing a content-addressed skills directory and instructing the
// underlying CLI to discover it (e.g. Claude CLI's --add-dir). Drivers that do
// NOT implement this interface will have req.Skills' body auto-merged into
// req.SystemPrompt by the VFS layer before the driver is invoked.
type SkillsBundleCapable interface {
	LLMDriver
	UsesSkillsBundle() // marker method, no-op
}
