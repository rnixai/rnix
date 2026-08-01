package context

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ReadFileEntry records a file's content and when it was last read.
type ReadFileEntry struct {
	Content   string
	Timestamp time.Time
	Mtime     time.Time // file modification time at read
}

// SkillEntry represents a loaded skill's name and body content.
type SkillEntry struct {
	Name    string
	Content string
}

// CompactOpts configures a compact operation.
type CompactOpts struct {
	// LLMCall sends a compact request to the LLM. The system prompt is a simplified
	// summarization prompt; messages contain the conversation history plus the compact
	// prompt as the final user message. Returns the raw LLM response text.
	LLMCall func(systemPrompt string, messages []Message) (string, error)

	// CustomInstructions is optional additional instructions appended to the compact prompt.
	CustomInstructions string

	// Trigger records how this compact was initiated. Value域 (Story 71.1 AC3
	// retired the two slot-axis labels `slot_threshold` / `slot_watermark`, and
	// with them the `both` degeneracy):
	//   "token_threshold"      — autoCompactIfNeeded, token usage over threshold
	//   "precompact"           — preCompactForToolCalls, pre-write prevention
	//   "context_full_resume"  — resume-time reclamation after ErrContextFull
	//   "manual"               — IPC handleCompact
	// Proactive reclamation (kernel reclaimLeakedIfNeeded) emits CtxReclaim rather
	// than a compact, with its own "token_watermark" label.
	Trigger string

	// --- Post-compact restore data ---

	// ReadFileState maps file paths to their content and last-read timestamp.
	// Used to restore recently read files after compaction.
	ReadFileState map[string]ReadFileEntry

	// ActiveSkills lists currently loaded skills to restore after compaction.
	ActiveSkills []SkillEntry

	// ActivePlan is the current plan text to restore after compaction.
	// Empty string means no active plan.
	ActivePlan string
}

// CompactResult reports the outcome of a compact operation.
type CompactResult struct {
	PreTokens     int
	PostTokens    int
	ItemsRestored []string
	Duration      time.Duration
}

// compact token budget constants
const (
	maxRestoreFiles      = 5
	maxRestoreFileTokens = 50_000
	perFileTokenLimit    = 5_000
	maxSkillTokens       = 25_000
	perSkillTokenLimit   = 5_000
	maxPTLRetries        = 3
	minMessagesForCompact = 2
)

// compactSystemPrompt is the simplified system prompt used for the compact LLM call.
const compactSystemPrompt = "You are a helpful AI assistant tasked with summarizing conversations."

// EstimateRestoreTokens reports how many tokens the post-compact restore would
// put BACK into the context for these opts — the files, skills and plan that
// Compact re-appends after replacing the history.
//
// It runs the real restore builders rather than re-adding up the raw inputs, so
// the per-file / per-skill / total budget caps (and their truncation) are
// applied exactly once, here and in Compact. Re-deriving them would be a second
// place for the budget arithmetic to drift.
//
// Its consumer is the kernel's pre-flight gate (Story 71.2 AC3): a compaction
// whose output would be dominated by what it immediately restores cannot shrink
// the context, and the request is not worth a 30s timeout risk. Cheap — no
// locks, no LLM, just token estimation over data the caller already assembled.
func EstimateRestoreTokens(opts CompactOpts) int {
	total := 0
	for _, msg := range restoreFiles(opts.ReadFileState) {
		total += EstimateMessageTokens(msg)
	}
	for _, msg := range restoreSkills(opts.ActiveSkills) {
		total += EstimateMessageTokens(msg)
	}
	if msg := restorePlan(opts.ActivePlan); msg != nil {
		total += EstimateMessageTokens(*msg)
	}
	return total
}

// compactToolResultTokenLimit caps how much of a single tool result travels in a
// compaction request (Story 71.2 AC4). Pinned like the Story 69.4 gate
// constants: it is a behavioural threshold, so it gets a name, a rationale and a
// test rather than being a literal at the call site.
//
// 600 tokens ≈ 2100 ASCII bytes, the token-axis equivalent of opencode's
// toolOutputMaxChars: 2000 (packages/opencode/src/session/compaction.ts). rnix
// works in tokens throughout this subsystem, so the character budget is
// converted rather than importing a second量纲.
//
// It sits above LeakedThreshold (1000 bytes) on purpose: an entry small enough
// to escape the leaked classification is small enough to send whole, so the trim
// and the prune primitive cannot disagree about which payloads are "large".
const compactToolResultTokenLimit = 600

// shrinkToolResultsForCompact trims oversized tool outputs out of a compaction
// request. It mutates the caller's SNAPSHOT — never ctx.Messages — so the
// persisted context is untouched and only this one request body shrinks.
//
// 🔴 Content only. The snapshot is a shallow copy (Compact's copy() above): Role,
// Content and ToolCallID are value-semantics fields that are safe to overwrite
// on the copy, but ToolCalls and ReasoningBlocks are slice headers SHARING a
// backing array with ctx.Messages. Writing through either would silently corrupt
// the live context — after RUnlock, from another goroutine — which is the red
// line context.go:644 already draws for the identical shallow snapshot there.
// Trimming ToolCalls[].Input would first require deep-copying that slice.
//
// Entries already holding DefaultPrunePlaceholder are skipped. Story 69.4's
// in-place erasure is the PERSISTENT layer and this is the per-request one; the
// persistent layer wins, and re-truncating its 37-byte marker would only add a
// misleading "original N tokens" notice about content this layer never saw.
// opencode splits the same two layers the same way (message-v2.ts:888-891).
//
// Not implemented, deliberately: stripping media. cc-src's stripImagesFromMessages
// has no object to act on here — Message.Content is a flat string, the driver
// structs mirror that shape, and no image/inline-data block type exists anywhere
// in the tree. A strip function would be permanently no-op code, i.e. a second
// preCompactForToolCalls (Story 71.2 F3).
func shrinkToolResultsForCompact(messages []Message) {
	for i := range messages {
		if messages[i].Role != RoleTool {
			continue
		}
		if messages[i].Content == DefaultPrunePlaceholder {
			continue
		}
		originalTokens := EstimateTokens(messages[i].Content)
		trimmed, didTrim := TruncateResult(messages[i].Content, compactToolResultTokenLimit)
		if !didTrim {
			continue
		}
		// Carry the original size so an incident investigator reading a captured
		// compaction request can tell a trimmed payload from a genuinely short
		// one (same shape as kernel/observe.go truncateDriverToolResult).
		messages[i].Content = trimmed + FormatTruncationNotice(originalTokens, compactToolResultTokenLimit, "")
	}
}

// Compact compresses the conversation history for the given context.
// It sends the full message history plus a compact prompt to the LLM,
// replaces all messages with a boundary marker and the resulting summary,
// then restores key context (files, skills, plan).
func (m *Manager) Compact(cid types.CtxID, opts CompactOpts) (*CompactResult, error) {
	start := time.Now()

	if opts.LLMCall == nil {
		return nil, &ContextError{
			Op:   "Compact",
			CID:  cid,
			Err:  fmt.Errorf("LLMCall callback is required"),
			Code: types.ErrInternal,
		}
	}

	ctx, err := m.getContext("Compact", cid)
	if err != nil {
		return nil, err
	}

	// Read messages under lock, then release for the LLM call
	ctx.mu.RLock()
	if len(ctx.Messages) < minMessagesForCompact {
		ctx.mu.RUnlock()
		return nil, &ContextError{
			Op:   "Compact",
			CID:  cid,
			Err:  fmt.Errorf("not enough messages to compact (have %d, need %d)", len(ctx.Messages), minMessagesForCompact),
			Code: types.ErrInternal,
		}
	}
	messages := make([]Message, len(ctx.Messages))
	copy(messages, ctx.Messages)
	preTokens := m.estimateMessagesTokens(ctx)
	ctx.mu.RUnlock()

	// Story 71.2 AC4 — the summariser does not need full tool payloads, so the
	// REQUEST is trimmed before it goes out. Placed here so all five callers
	// (autoCompact / precompact / manual IPC / the two resume paths) are covered
	// by one edit; doing it in kernel's BuildCompactLLMCall would cover the
	// kernel side only, five times over.
	//
	// preTokens is read above, off ctx, so it keeps reporting the REAL
	// pre-compaction size — the trim must never be able to flatter the result.
	shrinkToolResultsForCompact(messages)

	trigger := opts.Trigger
	if trigger == "" {
		trigger = "manual"
	}

	// Build compact prompt and append as the last user message
	compactPrompt := getCompactPrompt(opts.CustomInstructions)
	messagesWithPrompt := append(messages, Message{
		Role:    RoleUser,
		Content: compactPrompt,
	})

	// Call LLM with PTL retry logic
	summary, err := m.compactWithPTLRetry(opts.LLMCall, messagesWithPrompt, opts.CustomInstructions)
	if err != nil {
		return nil, &ContextError{
			Op:   "Compact",
			CID:  cid,
			Err:  fmt.Errorf("compact LLM call failed: %w", err),
			Code: types.ErrInternal,
		}
	}

	// Strip <analysis> section, keep only <summary> content
	summary = stripAnalysis(summary)

	// Build replacement messages
	var newMessages []Message

	// [0] Boundary marker
	newMessages = append(newMessages, Message{
		Role:    RoleUser,
		Content: fmt.Sprintf("[compact_boundary: trigger=%s, pre_tokens=%d]", trigger, preTokens),
	})

	// [1] Summary
	newMessages = append(newMessages, Message{
		Role:    RoleUser,
		Content: summary,
	})

	// Post-compact restore
	var restored []string

	// Restore files
	if len(opts.ReadFileState) > 0 {
		fileRestores := restoreFiles(opts.ReadFileState)
		for _, fr := range fileRestores {
			newMessages = append(newMessages, fr)
			restored = append(restored, "file")
		}
	}

	// Restore skills
	if len(opts.ActiveSkills) > 0 {
		skillRestores := restoreSkills(opts.ActiveSkills)
		for _, sr := range skillRestores {
			newMessages = append(newMessages, sr)
			restored = append(restored, "skill")
		}
	}

	// Restore plan
	if msg := restorePlan(opts.ActivePlan); msg != nil {
		newMessages = append(newMessages, *msg)
		restored = append(restored, "plan")
	}

	// Replace messages under write lock
	ctx.mu.Lock()
	ctx.Messages = newMessages
	// Invalidate section caches so dynamic sections recompute after compact
	if ctx.Sections != nil {
		ctx.Sections.Invalidate()
	}
	// Story 69.2: postTokens used to be an inlined Content-only loop — a second
	// copy of the same accounting that had already drifted from
	// estimateMessagesTokens. Both now route through EstimateMessageTokens so
	// PreTokens and PostTokens stay comparable.
	postTokens := 0
	for _, msg := range ctx.Messages {
		postTokens += EstimateMessageTokens(msg)
	}
	ctx.mu.Unlock()

	return &CompactResult{
		PreTokens:     preTokens,
		PostTokens:    postTokens,
		ItemsRestored: restored,
		Duration:      time.Since(start),
	}, nil
}

// estimateMessagesTokens estimates total tokens across all messages (caller must hold at least RLock).
// Story 69.2: delegates per-message accounting to EstimateMessageTokens so this
// site cannot drift from TokenUsage / postTokens. The lock contract is unchanged
// — EstimateMessageTokens is a pure function and takes no locks.
func (m *Manager) estimateMessagesTokens(ctx *Context) int {
	total := 0
	for _, msg := range ctx.Messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// compactWithPTLRetry calls LLM and retries by dropping oldest API-rounds on "prompt too long" errors.
func (m *Manager) compactWithPTLRetry(llmCall func(string, []Message) (string, error), messages []Message, _ string) (string, error) {
	// Separate the compact prompt (last message) from conversation messages
	if len(messages) < 2 {
		return "", fmt.Errorf("not enough messages for compact")
	}
	compactPromptMsg := messages[len(messages)-1]
	// Clone to avoid aliasing the caller's backing array on append
	convMessages := make([]Message, len(messages)-1)
	copy(convMessages, messages[:len(messages)-1])

	for attempt := range maxPTLRetries {
		allMessages := append(convMessages, compactPromptMsg)
		result, err := llmCall(compactSystemPrompt, allMessages)
		if err == nil {
			return result, nil
		}

		// Check if this is a "prompt too long" error
		errStr := strings.ToLower(err.Error())
		if !strings.Contains(errStr, "prompt too long") &&
			!strings.Contains(errStr, "too many tokens") &&
			!strings.Contains(errStr, "context length exceeded") {
			return "", err // non-PTL error, don't retry
		}

		// Group by API-round and drop the oldest group
		groups := groupMessagesByAPIRound(convMessages)
		if len(groups) <= 1 {
			return "", fmt.Errorf("prompt too long: cannot reduce further (attempt %d/%d)", attempt+1, maxPTLRetries)
		}

		// Drop oldest group
		convMessages = flattenGroups(groups[1:])
	}

	return "", fmt.Errorf("prompt too long after %d retries", maxPTLRetries)
}

// groupMessagesByAPIRound splits messages into groups where each group represents
// one API round (assistant response + subsequent tool messages, preceded by user messages).
func groupMessagesByAPIRound(messages []Message) [][]Message {
	if len(messages) == 0 {
		return nil
	}

	var groups [][]Message
	var current []Message

	for _, msg := range messages {
		if msg.Role == RoleAssistant && len(current) > 0 {
			// Start a new group: flush current as the prefix of this round
			// But we want assistant + following tools to be in the same group
			// So we accumulate differently:
			// A "round" = any user messages leading up to an assistant message,
			// plus that assistant message and its tool results
			groups = append(groups, current)
			current = nil
		}
		current = append(current, msg)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

// flattenGroups concatenates message groups back into a single slice.
func flattenGroups(groups [][]Message) []Message {
	var result []Message
	for _, g := range groups {
		result = append(result, g...)
	}
	return result
}

// stripAnalysis extracts content between <summary> tags, discarding <analysis>.
// If no <summary> tag is found, returns the full text (best-effort).
func stripAnalysis(raw string) string {
	// Find <summary> and </summary>
	before, afterSummaryStart, hasSummary := strings.Cut(raw, "<summary>")
	if !hasSummary {
		// No <summary> tag — strip <analysis> block if present and return the rest
		beforeAnalysis, afterAnalysisStart, hasAnalysis := strings.Cut(raw, "<analysis>")
		if hasAnalysis {
			_, afterAnalysisEnd, hasEnd := strings.Cut(afterAnalysisStart, "</analysis>")
			if hasEnd {
				return strings.TrimSpace(beforeAnalysis + afterAnalysisEnd)
			}
		}
		_ = beforeAnalysis
		return strings.TrimSpace(raw)
	}

	_ = before
	summaryContent, _, hasEnd := strings.Cut(afterSummaryStart, "</summary>")
	if !hasEnd {
		// Opening <summary> but no closing tag — take everything after <summary>
		return strings.TrimSpace(afterSummaryStart)
	}

	return strings.TrimSpace(summaryContent)
}

// restoreFiles builds restore messages from ReadFileState, sorted by timestamp descending.
// Applies per-file (5K tokens) and total (50K tokens) budget limits.
// restorePlan builds the plan restore message, or nil for an empty plan. Shared
// by Compact and EstimateRestoreTokens so the "[Post-compact restore: plan]"
// prefix is counted exactly once, here — the same single-builder discipline the
// file and skill legs already follow. The plan is restored with NO truncation,
// the reason the post-compact token floor can be arbitrarily high.
func restorePlan(plan string) *Message {
	if plan == "" {
		return nil
	}
	return &Message{
		Role:    RoleUser,
		Content: fmt.Sprintf("[Post-compact restore: plan]\n%s", plan),
	}
}

func restoreFiles(state map[string]ReadFileEntry) []Message {
	// Sort by timestamp descending (most recent first)
	type fileItem struct {
		path  string
		entry ReadFileEntry
	}
	items := make([]fileItem, 0, len(state))
	for p, e := range state {
		items = append(items, fileItem{path: p, entry: e})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].entry.Timestamp.After(items[j].entry.Timestamp)
	})

	var messages []Message
	totalTokens := 0
	count := 0

	for _, item := range items {
		if count >= maxRestoreFiles {
			break
		}
		if totalTokens >= maxRestoreFileTokens {
			break
		}

		content := item.entry.Content
		tokens := EstimateTokens(content)
		if tokens > perFileTokenLimit {
			// Truncate to fit per-file limit
			content = truncateToTokens(content, perFileTokenLimit)
			tokens = perFileTokenLimit
		}
		if totalTokens+tokens > maxRestoreFileTokens {
			break
		}

		messages = append(messages, Message{
			Role:    RoleUser,
			Content: fmt.Sprintf("[Post-compact restore: file] %s\n%s", item.path, content),
		})
		totalTokens += tokens
		count++
	}

	return messages
}

// restoreSkills builds restore messages from ActiveSkills.
// Applies per-skill (5K tokens) and total (25K tokens) budget limits.
func restoreSkills(skills []SkillEntry) []Message {
	var messages []Message
	totalTokens := 0

	for _, skill := range skills {
		if totalTokens >= maxSkillTokens {
			break
		}

		content := skill.Content
		tokens := EstimateTokens(content)
		if tokens > perSkillTokenLimit {
			content = truncateToTokens(content, perSkillTokenLimit)
			content += "\n[... skill content truncated for compaction; use skill path if you need the full text]"
			tokens = perSkillTokenLimit
		}
		if totalTokens+tokens > maxSkillTokens {
			break
		}

		messages = append(messages, Message{
			Role:    RoleUser,
			Content: fmt.Sprintf("[Post-compact restore: skill] %s\n%s", skill.Name, content),
		})
		totalTokens += tokens
	}

	return messages
}

// truncateToTokens truncates text to approximately the given token budget.
// Uses EstimateTokens for accurate budget enforcement across ASCII and CJK content.
func truncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	// Binary search for the right rune cutoff
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if EstimateTokens(string(runes[:mid])) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return string(runes[:lo])
}
