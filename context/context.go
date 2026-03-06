// Package context implements the context management layer for Rnix.
package context

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

// Role represents the role of a message participant in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in a conversation context.
type Message struct {
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Context represents an independent context space for accumulating conversation history.
type Context struct {
	ID           types.CtxID
	SystemPrompt string
	Messages     []Message
	MaxSize      int // Max message count; MVP does not limit content byte size
	mu           sync.RWMutex
}

// PromptResult is the return value of BuildPrompt, ready for LLM driver consumption.
type PromptResult struct {
	SystemPrompt string
	Messages     []Message
}

// ContextError represents an error from context operations.
type ContextError struct {
	Op   string
	CID  types.CtxID
	Err  error
	Code types.ErrCode
}

// Error returns a formatted error string.
func (e *ContextError) Error() string {
	return fmt.Sprintf("[%s] CtxID %d %s: %v", e.Code, e.CID, e.Op, e.Err)
}

// Unwrap returns the underlying error.
func (e *ContextError) Unwrap() error {
	return e.Err
}

// Manager manages context allocation and lifecycle.
type Manager struct {
	contexts *xsync.SyncMap[types.CtxID, *Context]
	nextID   atomic.Uint64
}

// NewManager creates a new context Manager.
func NewManager() *Manager {
	return &Manager{
		contexts: xsync.NewSyncMap[types.CtxID, *Context](),
	}
}

// CtxAlloc allocates a new context with the given size limit and returns its unique CtxID.
func (m *Manager) CtxAlloc(size int) (types.CtxID, error) {
	if size <= 0 {
		return 0, &ContextError{
			Op:   "CtxAlloc",
			CID:  0,
			Err:  fmt.Errorf("invalid size: %d", size),
			Code: types.ErrInternal,
		}
	}
	id := types.CtxID(m.nextID.Add(1))
	ctx := &Context{
		ID:       id,
		Messages: make([]Message, 0),
		MaxSize:  size,
	}
	m.contexts.Store(id, ctx)
	return id, nil
}

// CtxFree releases the context with the given CtxID.
func (m *Manager) CtxFree(cid types.CtxID) error {
	_, ok := m.contexts.LoadAndDelete(cid)
	if !ok {
		return &ContextError{
			Op:   "CtxFree",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
	}
	return nil
}

// getContext retrieves the context for the given CtxID.
// op is the caller's operation name, used in the error if the context is not found.
func (m *Manager) getContext(op string, cid types.CtxID) (*Context, error) {
	ctx, ok := m.contexts.Load(cid)
	if !ok {
		return nil, &ContextError{
			Op:   op,
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
	}
	return ctx, nil
}

// CtxWrite writes raw byte data to the context.
// offset=0 means append a new message (data is JSON-serialized Message).
// offset=1..N overwrites the message at that 1-based index (1 = first message).
// Note: CtxRead uses 0-based indexing for offset; CtxWrite reserves 0 for append.
func (m *Manager) CtxWrite(cid types.CtxID, offset int, data []byte) error {
	ctx, err := m.getContext("CtxWrite", cid)
	if err != nil {
		return err
	}

	var msg Message
	if jsonErr := json.Unmarshal(data, &msg); jsonErr != nil {
		return &ContextError{
			Op:   "CtxWrite",
			CID:  cid,
			Err:  fmt.Errorf("invalid message data: %w", jsonErr),
			Code: types.ErrInternal,
		}
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if offset == 0 {
		if len(ctx.Messages) >= ctx.MaxSize {
			return &ContextError{
				Op:   "CtxWrite",
				CID:  cid,
				Err:  fmt.Errorf("context full"),
				Code: types.ErrInternal,
			}
		}
		ctx.Messages = append(ctx.Messages, msg)
		return nil
	}

	if offset < 1 || offset > len(ctx.Messages) {
		return &ContextError{
			Op:   "CtxWrite",
			CID:  cid,
			Err:  fmt.Errorf("offset out of range: %d", offset),
			Code: types.ErrInternal,
		}
	}
	ctx.Messages[offset-1] = msg
	return nil
}

// CtxRead reads raw byte representation of the context content.
// offset and length operate on 0-based message indices.
// offset=0, length=0 reads all content (system prompt + all messages).
func (m *Manager) CtxRead(cid types.CtxID, offset int, length int) ([]byte, error) {
	ctx, err := m.getContext("CtxRead", cid)
	if err != nil {
		return nil, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	type contextData struct {
		SystemPrompt string    `json:"system_prompt"`
		Messages     []Message `json:"messages"`
	}

	msgs := ctx.Messages
	if offset > 0 || length > 0 {
		start := min(offset, len(msgs))
		end := len(msgs)
		if length > 0 && start+length < end {
			end = start + length
		}
		msgs = msgs[start:end]
	}

	result := contextData{
		SystemPrompt: ctx.SystemPrompt,
		Messages:     msgs,
	}

	bytes, jsonErr := json.Marshal(result)
	if jsonErr != nil {
		return nil, &ContextError{
			Op:   "CtxRead",
			CID:  cid,
			Err:  fmt.Errorf("failed to serialize context: %w", jsonErr),
			Code: types.ErrInternal,
		}
	}
	return bytes, nil
}

// SetSystemPrompt sets or updates the system prompt for the context.
func (m *Manager) SetSystemPrompt(cid types.CtxID, prompt string) error {
	ctx, err := m.getContext("SetSystemPrompt", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.SystemPrompt = prompt
	return nil
}

// AppendMessage appends a conversation message with the given role and content.
func (m *Manager) AppendMessage(cid types.CtxID, role Role, content string) error {
	ctx, err := m.getContext("AppendMessage", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if len(ctx.Messages) >= ctx.MaxSize {
		return &ContextError{
			Op:   "AppendMessage",
			CID:  cid,
			Err:  fmt.Errorf("context full"),
			Code: types.ErrInternal,
		}
	}

	ctx.Messages = append(ctx.Messages, Message{
		Role:    role,
		Content: content,
	})
	return nil
}

// AppendToolResult appends a tool execution result message.
func (m *Manager) AppendToolResult(cid types.CtxID, toolCallID string, content string) error {
	ctx, err := m.getContext("AppendToolResult", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if len(ctx.Messages) >= ctx.MaxSize {
		return &ContextError{
			Op:   "AppendToolResult",
			CID:  cid,
			Err:  fmt.Errorf("context full"),
			Code: types.ErrInternal,
		}
	}

	ctx.Messages = append(ctx.Messages, Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
	})
	return nil
}

// BuildPrompt assembles the full LLM prompt from the context.
// Returns SystemPrompt separately and Messages in append order.
func (m *Manager) BuildPrompt(cid types.CtxID) (*PromptResult, error) {
	ctx, err := m.getContext("BuildPrompt", cid)
	if err != nil {
		return nil, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	msgs := make([]Message, len(ctx.Messages))
	copy(msgs, ctx.Messages)

	return &PromptResult{
		SystemPrompt: ctx.SystemPrompt,
		Messages:     msgs,
	}, nil
}

// GetContextSummary returns a human-readable summary of the context.
// Satisfies vfs.ContextSummaryProvider interface via duck typing.
func (m *Manager) GetContextSummary(ctxID types.CtxID) (string, error) {
	ctx, err := m.getContext("GetContextSummary", ctxID)
	if err != nil {
		return "", err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	// Count messages by role
	var systemCount, userCount, assistantCount, toolCount int
	for _, msg := range ctx.Messages {
		switch msg.Role {
		case RoleSystem:
			systemCount++
		case RoleUser:
			userCount++
		case RoleAssistant:
			assistantCount++
		case RoleTool:
			toolCount++
		}
	}

	total := len(ctx.Messages)
	promptLen := len(ctx.SystemPrompt)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Messages: %d (system: %d, user: %d, assistant: %d", total, systemCount, userCount, assistantCount)
	if toolCount > 0 {
		fmt.Fprintf(&sb, ", tool: %d", toolCount)
	}
	sb.WriteString(")\n")
	fmt.Fprintf(&sb, "System Prompt: %d chars\n", promptLen)

	if total > 0 {
		last := ctx.Messages[total-1]
		preview := last.Content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		fmt.Fprintf(&sb, "Last Message: [%s] %s", last.Role, preview)
	}

	return sb.String(), nil
}
