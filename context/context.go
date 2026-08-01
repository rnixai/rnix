// Package context implements the context management layer for Rnix.
package context

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	// MaxSize caps the number of message slots. 0 = NO LIMIT, which is the
	// production default since Story 71.1; >0 keeps the admission checks alive as
	// an operator escape hatch (SpawnOpts.CtxSize / agent.yaml ctx_size).
	//
	// A slot count measures STRUCTURE (how many messages), while capacity is a
	// question of VOLUME (how many tokens). Measured over 822 samples the two have
	// no stable conversion rate — 205 slots spanned 36.7k…146.2k real tokens — so
	// a slot ceiling is a量纲 error rather than a mis-calibrated number. Capacity
	// management lives entirely on the token axis (TokenLimit, below).
	//
	// Negative values are equivalent to 0. "No limit" has no second meaning, so
	// unlike StepTimeout / loop_threshold there is no negative=disable convention
	// here.
	MaxSize    int
	TokenLimit int // Max token budget for this context; 0 = DefaultTokenLimit
	mu         sync.RWMutex
}

// unlimitedSlots is the AvailableSlots sentinel for a context with no ceiling.
//
// MaxInt32 rather than MaxInt on purpose: four call sites subtract from this
// value (kernel/compact.go's resumeFallbackHeadroom-avail and the two
// required-available forms). All four sit INSIDE an `avail < headroom` /
// `available < required` guard and are therefore unreachable at sentinel
// magnitude — verified call site by call site — but MaxInt32+MaxInt32 still fits
// in an int64, so the headroom is free insurance against a future consumer that
// forgets the guard. A new AvailableSlots consumer doing unguarded arithmetic
// must still check its own overflow.
const unlimitedSlots = math.MaxInt32

// contextSnapshot is the wire format for Context serialization.
type contextSnapshot struct {
	SystemPrompt string    `json:"system_prompt"`
	Messages     []Message `json:"messages"`
	// MaxSize is retained for backward/forward compatibility with snapshots
	// written before Story 71.1. It is WRITTEN from the live context but
	// deliberately IGNORED on read — see Deserialize.
	MaxSize int `json:"max_size"`
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
// Overwrites SystemPrompt and Messages.
//
// MaxSize is deliberately NOT restored — it is forced to 0 (no limit). Every
// snapshot written before Story 71.1 carries max_size: 256, which was the old
// default rather than an operator choice; honouring it would put the 256-slot
// ceiling straight back onto every resumed process, re-creating the very failure
// this story removes. The field stays in contextSnapshot so a NEW daemon can
// still read OLD snapshots (json.Unmarshal ignores it). The reverse direction
// is NOT compatible: an OLD daemon reading a new snapshot sees max_size: 0,
// which pre-71.1 CtxAlloc rejected — mixed-version daemon handoff requires all
// daemons on 71.1+.
//
// The caller must hold no lock on the Context; this method acquires a write lock
// internally.
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
	c.MaxSize = 0
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

// ErrContextFull signals the context buffer cannot accommodate a required
// message group atomically. AppendAssistantWithToolCalls returns this
// (wrapped in ContextError) when the remaining capacity is less than
// 1+len(toolCalls), i.e. there isn't room for the assistant message AND
// every required tool result. Callers MUST handle this error rather than
// ignore it; writing the assistant message without all subsequent tool
// messages produces a protocol-illegal state that downstream LLM
// providers reject (e.g. DeepSeek HTTP 400 "insufficient tool messages
// following tool_calls message"). Use errors.Is(err, ErrContextFull) to
// detect this condition.
var ErrContextFull = errors.New("context buffer full")

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

// CtxAlloc allocates a new context and returns its unique CtxID.
//
// size is the message-slot ceiling: `size <= 0` means NO LIMIT (the production
// default since Story 71.1) and is NOT an error. A positive size keeps the
// admission checks active as an operator escape hatch. 0 and negative values are
// synonymous — "no limit" has no second meaning, so there is no negative=disable
// convention here (contrast StepTimeout / loop_threshold).
func (m *Manager) CtxAlloc(size int) (types.CtxID, error) {
	if size < 0 {
		size = 0
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
		if ctx.MaxSize > 0 && len(ctx.Messages) >= ctx.MaxSize {
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

	if ctx.MaxSize > 0 && len(ctx.Messages) >= ctx.MaxSize {
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

	if ctx.MaxSize > 0 && len(ctx.Messages) >= ctx.MaxSize {
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

	required := 1 + len(toolCalls)
	// Story 71.1 AC5: the ATOMICITY guarantee below is untouched; only the
	// CAPACITY question moved off this function. With MaxSize == 0 (no ceiling)
	// the guarantee holds in its strongest form — there is always room — so the
	// check is skipped rather than weakened. With an explicit ceiling the logic is
	// byte-for-byte what it was, which keeps ErrContextFull →
	// selfSuspend("context_full") reachable and testable instead of degrading a
	// regression red line into an unverifiable comment.
	if ctx.MaxSize > 0 && len(ctx.Messages)+required > ctx.MaxSize {
		return &ContextError{
			Op:  "AppendAssistantWithToolCalls",
			CID: cid,
			Err: fmt.Errorf("%w: need %d slots (1 assistant + %d tool results), have %d/%d used",
				ErrContextFull, required, len(toolCalls), len(ctx.Messages), ctx.MaxSize),
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

	// Lock ordering (Story 69.1 AC4): snapshot under RLock, then release it
	// BEFORE calling Sections.Build(). Pre-71.1, the kernel's backpressure
	// ComputeFn called back into this Manager via SlotUsage(), which took a
	// second RLock on this same ctx.mu from this same goroutine — under
	// sync.RWMutex semantics a recursive RLock deadlocks as soon as any writer
	// is queued (Compact, IPC handleCompact, gdb AppendMessage, fork_continue
	// all take ctx.mu.Lock() from other goroutines). Since Story 71.1 the
	// backpressure ComputeFn reads kernel-side proc fields only and no longer
	// re-enters this Manager, but the snapshot-then-Build pattern is retained:
	// it keeps Build() outside any ctx lock, the safer invariant.
	// Deliberately no `defer RUnlock`.
	// Same de-coupling section.go's Build() already does for r.mu.
	ctx.mu.RLock()
	sysPrompt := ctx.SystemPrompt
	sections := ctx.Sections
	msgs := make([]Message, len(ctx.Messages))
	copy(msgs, ctx.Messages)
	ctx.mu.RUnlock()

	if sections != nil {
		sysPrompt = sections.Build()
	}

	return &PromptResult{
		SystemPrompt: sysPrompt,
		Messages:     msgs,
	}, nil
}

// AvailableSlots returns the number of remaining message slots in the context.
//
// With no ceiling (MaxSize == 0, the production default since Story 71.1) it
// returns the unlimitedSlots sentinel instead of the arithmetic result
// `0 - len(Messages)`, which would be NEGATIVE and would flip every
// "do I have room?" test into its opposite. The sentinel makes all seven call
// sites short-circuit into their "plenty of room" branch untouched, so the slot
// ceiling could be retired without editing unrelated call sites.
//
// Consequence to be honest about: the code below those call sites
// (preCompactForToolCalls' body, tool_exec.go's specialize rollback,
// compact.go's resume/fallback rechecks) is STRUCTURALLY UNREACHABLE while no
// ceiling is configured. Each of them carries a docstring saying so; do not read
// their existence as evidence that they still run.
func (m *Manager) AvailableSlots(cid types.CtxID) (int, error) {
	ctx, err := m.getContext("AvailableSlots", cid)
	if err != nil {
		return 0, err
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.MaxSize <= 0 {
		return unlimitedSlots, nil
	}
	return ctx.MaxSize - len(ctx.Messages), nil
}

// SlotUsage returns the number of used and maximum message slots for the given
// context. `used` is always the real history length (len(Messages)) and stays a
// meaningful observability figure; `max` is 0 when no ceiling is configured.
func (m *Manager) SlotUsage(cid types.CtxID) (used int, max int, err error) {
	ctx, err := m.getContext("SlotUsage", cid)
	if err != nil {
		return 0, 0, err
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return len(ctx.Messages), ctx.MaxSize, nil
}

// TokenUsage returns token usage statistics for the given context.
// Estimates tokens across the system prompt and all messages.
func (m *Manager) TokenUsage(cid types.CtxID) (TokenStats, error) {
	ctx, err := m.getContext("TokenUsage", cid)
	if err != nil {
		return TokenStats{}, err
	}

	// Lock ordering (Story 69.1 AC4): snapshot under RLock, Build() after release.
	// See BuildPrompt for the full rationale. Pre-71.1 the backpressure ComputeFn
	// re-entered this Manager via SlotUsage(); since 71.1 it reads kernel-side
	// proc fields only, but the snapshot-then-Build pattern is retained as the
	// safer invariant (Build() outside any ctx lock).
	ctx.mu.RLock()
	sysPrompt := ctx.SystemPrompt
	sections := ctx.Sections
	msgs := make([]Message, len(ctx.Messages))
	copy(msgs, ctx.Messages)
	limit := ctx.effectiveTokenLimit()
	slotUsed := len(ctx.Messages)
	slotMax := ctx.MaxSize
	ctx.mu.RUnlock()

	if sections != nil {
		sysPrompt = sections.Build()
	}
	total := EstimateTokens(sysPrompt)
	// Story 69.2 AC2/AC7: per-message accounting goes through the single
	// EstimateMessageTokens口径 so ToolCalls / ReasoningBlocks / Reasoning stop
	// being invisible to the token axis. It stays OUTSIDE the read lock and
	// reads only the snapshot copied above — moving this loop back under
	// ctx.mu.RLock would hold the lock across EstimateMessageTokens for large
	// histories, blocking concurrent writers. (Pre-71.1 this also avoided the
	// Story 69.1 recursive-RLock deadlock via the backpressure ComputeFn's
	// SlotUsage() callback; that re-entry path no longer exists since 71.1.)
	// The snapshot is shallow: ToolCalls / ReasoningBlocks share backing arrays
	// with ctx.Messages, so they may be read here but never mutated.
	for _, msg := range msgs {
		total += EstimateMessageTokens(msg)
	}

	pct := float64(total) / float64(limit) * 100

	var slotPct float64
	if slotMax > 0 {
		slotPct = float64(slotUsed) / float64(slotMax) * 100
	}

	return TokenStats{
		Used:           total,
		Limit:          limit,
		Percentage:     pct,
		SlotUsed:       slotUsed,
		SlotMax:        slotMax,
		SlotPercentage: slotPct,
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
// per-message token estimates via EstimateMessageTokens, and last message
// preview. The per-message accounting deliberately matches Manager.TokenUsage
// (Story 69.2 Review P1): both measure against effectiveTokenLimit(), so a
// Content-only numerator here would make gdb's token_usage_percent diverge
// from TokenUsage().Percentage by the same factor the full口径 closes.
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
		tokens := EstimateMessageTokens(msg)
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
