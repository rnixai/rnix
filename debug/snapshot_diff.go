package debug

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SnapshotFinder searches for context_snapshot events in a recording.
type SnapshotFinder struct {
	events []RecordEvent
}

// NewSnapshotFinder creates a new SnapshotFinder from a list of events.
func NewSnapshotFinder(events []RecordEvent) *SnapshotFinder {
	return &SnapshotFinder{events: events}
}

// HasSnapshots returns true if any context_snapshot events exist in the event list.
func (f *SnapshotFinder) HasSnapshots() bool {
	for i := range f.events {
		if f.events[i].Type == RecordContextSnapshot {
			return true
		}
	}
	return false
}

// FindNearestBefore finds the nearest context_snapshot event at or before the given SeqNum.
// If seqNum itself is a context_snapshot, it is returned directly.
// Searches backward from seqNum to find the most recent snapshot.
func (f *SnapshotFinder) FindNearestBefore(seqNum uint64) (*RecordEvent, error) {
	if len(f.events) == 0 {
		return nil, fmt.Errorf("no context snapshot found before SeqNum %d", seqNum)
	}

	// Find the starting index: last event with SeqNum <= seqNum
	startIdx := -1
	for i := range f.events {
		if f.events[i].SeqNum <= seqNum {
			startIdx = i
		} else {
			break
		}
	}

	if startIdx < 0 {
		return nil, fmt.Errorf("no context snapshot found before SeqNum %d", seqNum)
	}

	// Search backward from startIdx for a context_snapshot
	for i := startIdx; i >= 0; i-- {
		if f.events[i].Type == RecordContextSnapshot {
			return &f.events[i], nil
		}
	}

	return nil, fmt.Errorf("no context snapshot found before SeqNum %d", seqNum)
}

// ContextDiff represents the difference between two context snapshots.
type ContextDiff struct {
	FromSeqNum    uint64        `json:"from_seq_num"`
	ToSeqNum      uint64        `json:"to_seq_num"`
	FromTimestamp time.Duration `json:"from_timestamp"`
	ToTimestamp   time.Duration `json:"to_timestamp"`
	SystemPrompt  PromptDiff    `json:"system_prompt"`
	Messages      MessagesDiff  `json:"messages"`
	TokenDelta    TokenDelta    `json:"token_delta"`
}

// PromptDiff represents a change in the system prompt.
type PromptDiff struct {
	Changed  bool   `json:"changed"`
	FromHash string `json:"from_hash"`
	ToHash   string `json:"to_hash"`
}

// MessagesDiff represents changes in the message list.
type MessagesDiff struct {
	Added     []string `json:"added"`
	Removed   []string `json:"removed"`
	FromCount int      `json:"from_count"`
	ToCount   int      `json:"to_count"`
}

// TokenDelta represents the change in token count.
type TokenDelta struct {
	FromTokens int `json:"from_tokens"`
	ToTokens   int `json:"to_tokens"`
	Delta      int `json:"delta"`
}

// ComputeContextDiff compares two ContextSnapshotData and returns the differences.
func ComputeContextDiff(from, to *ContextSnapshotData, fromEv, toEv *RecordEvent) *ContextDiff {
	diff := &ContextDiff{
		FromSeqNum:    fromEv.SeqNum,
		ToSeqNum:      toEv.SeqNum,
		FromTimestamp: fromEv.Timestamp,
		ToTimestamp:   toEv.Timestamp,
	}

	// System prompt comparison
	diff.SystemPrompt = PromptDiff{
		Changed:  from.SystemPromptHash != to.SystemPromptHash,
		FromHash: from.SystemPromptHash,
		ToHash:   to.SystemPromptHash,
	}

	// Messages comparison using common-prefix algorithm
	diff.Messages = computeMessagesDiff(from.Messages, to.Messages)
	diff.Messages.FromCount = from.MessageCount
	diff.Messages.ToCount = to.MessageCount

	// Token delta
	diff.TokenDelta = TokenDelta{
		FromTokens: from.TokenEstimate,
		ToTokens:   to.TokenEstimate,
		Delta:      to.TokenEstimate - from.TokenEstimate,
	}

	return diff
}

// computeMessagesDiff computes the added and removed messages between two message lists.
// Uses a common-prefix algorithm since messages typically grow by appending.
func computeMessagesDiff(from, to []string) MessagesDiff {
	// Find the common prefix length
	commonLen := 0
	maxCommon := len(from)
	if len(to) < maxCommon {
		maxCommon = len(to)
	}
	for i := 0; i < maxCommon; i++ {
		if from[i] == to[i] {
			commonLen++
		} else {
			break
		}
	}

	var added, removed []string

	// Messages after common prefix in 'from' are removed
	if commonLen < len(from) {
		removed = make([]string, len(from)-commonLen)
		copy(removed, from[commonLen:])
	}

	// Messages after common prefix in 'to' are added
	if commonLen < len(to) {
		added = make([]string, len(to)-commonLen)
		copy(added, to[commonLen:])
	}

	return MessagesDiff{
		Added:   added,
		Removed: removed,
	}
}

// FormatContextDiff formats a ContextDiff as a human-readable string.
func FormatContextDiff(diff *ContextDiff) string {
	fromTS := formatReplayTimestamp(diff.FromTimestamp)
	toTS := formatReplayTimestamp(diff.ToTimestamp)

	var b strings.Builder

	fmt.Fprintf(&b, "Context Diff: #%03d → #%03d (%s → %s)\n", diff.FromSeqNum, diff.ToSeqNum, fromTS, toTS)
	b.WriteString("─────────────────────────────────────\n")

	// Check if there are any changes at all
	noChanges := !diff.SystemPrompt.Changed &&
		len(diff.Messages.Added) == 0 &&
		len(diff.Messages.Removed) == 0 &&
		diff.TokenDelta.Delta == 0
	if noChanges {
		b.WriteString("No changes detected.\n")
		return b.String()
	}

	// System prompt
	if diff.SystemPrompt.Changed {
		fmt.Fprintf(&b, "System Prompt: changed (%s → %s)\n", diff.SystemPrompt.FromHash, diff.SystemPrompt.ToHash)
	} else {
		fmt.Fprintf(&b, "System Prompt: unchanged (hash=%s)\n", diff.SystemPrompt.FromHash)
	}

	// Messages
	msgDelta := diff.Messages.ToCount - diff.Messages.FromCount
	deltaStr := formatDelta(msgDelta)
	fmt.Fprintf(&b, "Messages: %d → %d (%s)\n", diff.Messages.FromCount, diff.Messages.ToCount, deltaStr)
	for _, msg := range diff.Messages.Added {
		fmt.Fprintf(&b, "  + %s\n", msg)
	}
	for _, msg := range diff.Messages.Removed {
		fmt.Fprintf(&b, "  - %s\n", msg)
	}

	// Tokens
	tokenDeltaStr := formatDelta(diff.TokenDelta.Delta)
	fmt.Fprintf(&b, "Tokens: %d → %d (%s)\n", diff.TokenDelta.FromTokens, diff.TokenDelta.ToTokens, tokenDeltaStr)

	return b.String()
}

// FormatContextDiffJSON returns the ContextDiff as JSON bytes.
func FormatContextDiffJSON(diff *ContextDiff) ([]byte, error) {
	return json.Marshal(diff)
}

// formatDelta formats an integer delta as "+N" or "-N" or "+0".
func formatDelta(delta int) string {
	if delta >= 0 {
		return fmt.Sprintf("+%d", delta)
	}
	return fmt.Sprintf("%d", delta)
}
