// Package context implements the context management layer for Crux.
package context

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/xsync"
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
	MaxSize      int
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
func (m *Manager) getContext(cid types.CtxID) (*Context, error) {
	ctx, ok := m.contexts.Load(cid)
	if !ok {
		return nil, &ContextError{
			Op:   "getContext",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
	}
	return ctx, nil
}

// CtxWrite writes raw byte data to the context.
// offset=0 means append (data is JSON-serialized Message).
// Other offset values overwrite the message at that index.
func (m *Manager) CtxWrite(cid types.CtxID, offset int, data []byte) error {
	ctx, err := m.getContext(cid)
	if err != nil {
		return &ContextError{
			Op:   "CtxWrite",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
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
// offset and length operate on message indices (0-based).
// offset=0, length=0 reads all content.
func (m *Manager) CtxRead(cid types.CtxID, offset int, length int) ([]byte, error) {
	ctx, err := m.getContext(cid)
	if err != nil {
		return nil, &ContextError{
			Op:   "CtxRead",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
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
	ctx, err := m.getContext(cid)
	if err != nil {
		return &ContextError{
			Op:   "SetSystemPrompt",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.SystemPrompt = prompt
	return nil
}

// AppendMessage appends a conversation message with the given role and content.
func (m *Manager) AppendMessage(cid types.CtxID, role Role, content string) error {
	ctx, err := m.getContext(cid)
	if err != nil {
		return &ContextError{
			Op:   "AppendMessage",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
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
	ctx, err := m.getContext(cid)
	if err != nil {
		return &ContextError{
			Op:   "AppendToolResult",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
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
	ctx, err := m.getContext(cid)
	if err != nil {
		return nil, &ContextError{
			Op:   "BuildPrompt",
			CID:  cid,
			Err:  fmt.Errorf("context not found"),
			Code: types.ErrNotFound,
		}
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
