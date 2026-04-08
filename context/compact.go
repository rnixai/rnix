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

	// Trigger records how this compact was initiated ("auto" or "manual").
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
	if opts.ActivePlan != "" {
		newMessages = append(newMessages, Message{
			Role:    RoleUser,
			Content: fmt.Sprintf("[Post-compact restore: plan]\n%s", opts.ActivePlan),
		})
		restored = append(restored, "plan")
	}

	// Replace messages under write lock
	ctx.mu.Lock()
	ctx.Messages = newMessages
	postTokens := 0
	for _, msg := range ctx.Messages {
		postTokens += EstimateTokens(msg.Content)
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
func (m *Manager) estimateMessagesTokens(ctx *Context) int {
	total := 0
	for _, msg := range ctx.Messages {
		total += EstimateTokens(msg.Content)
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
