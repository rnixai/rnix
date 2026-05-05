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

// ToolCall represents a tool invocation requested by the LLM.
// Fields and JSON tags are intentionally identical to llm.ToolCall for
// serialization compatibility across package boundaries.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

// ReasoningBlock carries a single thinking-mode content block across context
// persistence and round-trips. Type selects the provider shape:
//   - "thinking" / "redacted_thinking": Anthropic-style (Signature, Data)
//   - "thought": Gemini-style (ThoughtSignature must be echoed on subsequent
//     turns to preserve thought context for function-calling round-trips)
//
// Fields and JSON tags mirror llm.ReasoningBlock for wire compatibility.
type ReasoningBlock struct {
	Type             string `json:"type"`
	Thinking         string `json:"thinking,omitempty"`
	Signature        string `json:"signature,omitempty"`
	Data             string `json:"data,omitempty"`
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
}

// Message represents a single message in a conversation context.
type Message struct {
	Role            Role             `json:"role"`
	Content         string           `json:"content"`
	ToolCallID      string           `json:"tool_call_id,omitempty"`
	ToolCalls       []ToolCall       `json:"tool_calls,omitempty"`
	ReasoningBlocks []ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Reasoning       string           `json:"reasoning,omitempty"`
}

// Context represents an independent context space for accumulating conversation history.
type Context struct {
	ID           types.CtxID
	SystemPrompt string
	Sections     *SectionRegistry // When non-nil, Build() produces the system prompt
	Messages     []Message
	MaxSize      int // Max message count; MVP does not limit content byte size
	TokenLimit   int // Max token budget for this context; 0 = DefaultTokenLimit
	mu           sync.RWMutex
}

// contextSnapshot is the wire format for Context serialization.
type contextSnapshot struct {
	SystemPrompt string    `json:"system_prompt"`
	Messages     []Message `json:"messages"`
	MaxSize      int       `json:"max_size"`
}

// effectiveTokenLimit returns the context's token limit, defaulting to DefaultTokenLimit if unset.
func (c *Context) effectiveTokenLimit() int {
	if c.TokenLimit > 0 {
		return c.TokenLimit
	}
	return DefaultTokenLimit
}

// SetTokenLimit sets the token limit on a context.
// Must be called under the Manager's registry to locate the context.
func (m *Manager) SetTokenLimit(cid types.CtxID, limit int) error {
	ctx, err := m.getContext("SetTokenLimit", cid)
	if err != nil {
		return err
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.TokenLimit = limit
	return nil
}

// Serialize serializes the complete Context state (system prompt, messages, max size) to JSON.
// The caller must hold no lock on the Context; this method acquires a read lock internally.
func (c *Context) Serialize() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := make([]Message, len(c.Messages))
	copy(msgs, c.Messages)
	// Deep-copy ToolCalls and ReasoningBlocks slices to prevent aliasing
	for i, m := range msgs {
		if len(m.ToolCalls) > 0 {
			tcs := make([]ToolCall, len(m.ToolCalls))
			copy(tcs, m.ToolCalls)
			msgs[i].ToolCalls = tcs
		}
		if len(m.ReasoningBlocks) > 0 {
			rbs := make([]ReasoningBlock, len(m.ReasoningBlocks))
			copy(rbs, m.ReasoningBlocks)
			msgs[i].ReasoningBlocks = rbs
		}
	}

	snap := contextSnapshot{
		SystemPrompt: c.SystemPrompt,
		Messages:     msgs,
		MaxSize:      c.MaxSize,
	}
	return json.Marshal(snap)
}

// Deserialize restores a Context from JSON produced by Serialize.
// Overwrites all state fields (SystemPrompt, Messages, MaxSize).
// The caller must hold no lock on the Context; this method acquires a write lock internally.
func (c *Context) Deserialize(data []byte) error {
	var snap contextSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("context deserialize: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.SystemPrompt = snap.SystemPrompt
	if snap.Messages == nil {
		c.Messages = make([]Message, 0)
	} else {
		c.Messages = snap.Messages
	}
	c.MaxSize = snap.MaxSize
	return nil
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

// GetContext returns the Context for the given CtxID.
// Exported for use by checkpoint serialization in the kernel package.
func (m *Manager) GetContext(cid types.CtxID) (*Context, error) {
	return m.getContext("GetContext", cid)
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

// SetSections attaches a SectionRegistry to the context.
// When set, BuildPrompt uses Sections.Build() instead of the raw SystemPrompt string.
func (m *Manager) SetSections(cid types.CtxID, sections *SectionRegistry) error {
	ctx, err := m.getContext("SetSections", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.Sections = sections
	return nil
}

// InvalidateSections resets cached section values for the context.
// No-op if the context has no SectionRegistry.
func (m *Manager) InvalidateSections(cid types.CtxID) error {
	ctx, err := m.getContext("InvalidateSections", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.Sections != nil {
		ctx.Sections.Invalidate()
	}
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

// AppendAssistantWithToolCalls appends an assistant message that includes
// tool calls and (optionally) thinking-mode reasoning. Both reasoning forms
// must be persisted alongside the assistant turn so subsequent requests can
// echo them back to providers that require round-tripping:
//   - reasoningBlocks: Anthropic-style structured blocks with Signature
//     (claude-sonnet/opus/haiku and anthropic-compat endpoints).
//   - reasoning: OpenAI-compatible thinking text (DeepSeek's reasoning_content,
//     OpenRouter/GLM's reasoning). DeepSeek returns HTTP 400 if not echoed.
func (m *Manager) AppendAssistantWithToolCalls(cid types.CtxID, content string, reasoning string, reasoningBlocks []ReasoningBlock, toolCalls []ToolCall) error {
	ctx, err := m.getContext("AppendAssistantWithToolCalls", cid)
	if err != nil {
		return err
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if len(ctx.Messages) >= ctx.MaxSize {
		return &ContextError{
			Op:   "AppendAssistantWithToolCalls",
			CID:  cid,
			Err:  fmt.Errorf("context full"),
			Code: types.ErrInternal,
		}
	}

	ctx.Messages = append(ctx.Messages, Message{
		Role:            RoleAssistant,
		Content:         content,
		ToolCalls:       toolCalls,
		ReasoningBlocks: reasoningBlocks,
		Reasoning:       reasoning,
	})
	return nil
}

// BuildPrompt assembles the full LLM prompt from the context.
// Returns SystemPrompt separately and Messages in append order.
// When the context has a SectionRegistry, Build() produces the system prompt;
// otherwise the legacy SystemPrompt string is used directly.
func (m *Manager) BuildPrompt(cid types.CtxID) (*PromptResult, error) {
	ctx, err := m.getContext("BuildPrompt", cid)
	if err != nil {
		return nil, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	sysPrompt := ctx.SystemPrompt
	if ctx.Sections != nil {
		sysPrompt = ctx.Sections.Build()
	}

	msgs := make([]Message, len(ctx.Messages))
	copy(msgs, ctx.Messages)

	return &PromptResult{
		SystemPrompt: sysPrompt,
		Messages:     msgs,
	}, nil
}

// TokenUsage returns token usage statistics for the given context.
// Estimates tokens across the system prompt and all messages.
func (m *Manager) TokenUsage(cid types.CtxID) (TokenStats, error) {
	ctx, err := m.getContext("TokenUsage", cid)
	if err != nil {
		return TokenStats{}, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	sysPrompt := ctx.SystemPrompt
	if ctx.Sections != nil {
		sysPrompt = ctx.Sections.Build()
	}
	total := EstimateTokens(sysPrompt)
	for _, msg := range ctx.Messages {
		total += EstimateTokens(msg.Content)
	}

	limit := ctx.effectiveTokenLimit()
	pct := float64(total) / float64(limit) * 100
	return TokenStats{
		Used:       total,
		Limit:      limit,
		Percentage: pct,
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

// GetContextInfo returns structured context information for gdb inspect.
// Returns a map with system_prompt_chars, message counts by role,
// token estimates via EstimateTokens, and last message preview.
func (m *Manager) GetContextInfo(ctxID types.CtxID) (map[string]any, error) {
	ctx, err := m.getContext("GetContextInfo", ctxID)
	if err != nil {
		return nil, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	var systemCount, userCount, assistantCount, toolCount int
	var systemTokens, userTokens, assistantTokens, toolTokens int
	for _, msg := range ctx.Messages {
		tokens := EstimateTokens(msg.Content)
		switch msg.Role {
		case RoleSystem:
			systemCount++
			systemTokens += tokens
		case RoleUser:
			userCount++
			userTokens += tokens
		case RoleAssistant:
			assistantCount++
			assistantTokens += tokens
		case RoleTool:
			toolCount++
			toolTokens += tokens
		}
	}

	promptChars := len(ctx.SystemPrompt)
	promptTokens := EstimateTokens(ctx.SystemPrompt)
	totalTokens := promptTokens + systemTokens + userTokens + assistantTokens + toolTokens
	tokenUsagePct := float64(totalTokens) / float64(ctx.effectiveTokenLimit()) * 100

	info := map[string]any{
		"system_prompt_chars":  promptChars,
		"system_prompt_tokens": promptTokens,
		"total_messages":       len(ctx.Messages),
		"system_count":         systemCount,
		"user_count":           userCount,
		"assistant_count":      assistantCount,
		"tool_count":           toolCount,
		"system_tokens":        systemTokens,
		"user_tokens":          userTokens,
		"assistant_tokens":     assistantTokens,
		"tool_tokens":          toolTokens,
		"total_tokens":         totalTokens,
		"token_usage_percent":  tokenUsagePct,
	}

	if len(ctx.Messages) > 0 {
		last := ctx.Messages[len(ctx.Messages)-1]
		preview := last.Content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		info["last_message_role"] = string(last.Role)
		info["last_message_preview"] = preview
	}

	return info, nil
}
