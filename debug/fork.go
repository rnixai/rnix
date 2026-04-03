package debug

import (
	"fmt"
	"strings"

	"github.com/rnixai/rnix/internal/types"
)

// ForkMessage represents a single message in a fork context.
type ForkMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ForkContext holds the restored context data needed to fork a new process
// from a recorded execution point.
type ForkContext struct {
	OriginalPID  types.PID     `json:"original_pid"`
	SeqNum       uint64        `json:"seq_num"`
	Intent       string        `json:"intent"`
	SystemPrompt string        `json:"system_prompt"`
	Messages     []ForkMessage `json:"messages"`
}

// SetSystemPrompt replaces the system prompt in the fork context.
func (fc *ForkContext) SetSystemPrompt(prompt string) {
	fc.SystemPrompt = prompt
}

// AppendMessage adds a new message to the fork context.
func (fc *ForkContext) AppendMessage(role, content string) {
	fc.Messages = append(fc.Messages, ForkMessage{Role: role, Content: content})
}

// RemoveLastMessages removes the last n messages from the fork context.
// If n exceeds the number of messages, all messages are removed.
func (fc *ForkContext) RemoveLastMessages(n int) {
	if n >= len(fc.Messages) {
		fc.Messages = fc.Messages[:0]
		return
	}
	fc.Messages = fc.Messages[:len(fc.Messages)-n]
}

// ReplaceLastMessage replaces the content of the last message, preserving its role.
// No-op if there are no messages.
func (fc *ForkContext) ReplaceLastMessage(content string) {
	if len(fc.Messages) == 0 {
		return
	}
	fc.Messages[len(fc.Messages)-1].Content = content
}

// Summary returns a human-readable summary of the fork context.
func (fc *ForkContext) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Original PID: %d\n", fc.OriginalPID)
	fmt.Fprintf(&b, "SeqNum:       %d\n", fc.SeqNum)
	fmt.Fprintf(&b, "Intent:       %s\n", fc.Intent)
	fmt.Fprintf(&b, "Messages:     %d\n", len(fc.Messages))
	if fc.SystemPrompt != "" {
		fmt.Fprintf(&b, "System Prompt: len:%d\n", len(fc.SystemPrompt))
	}
	// Estimate tokens using EstimateTokens
	totalTokens := estimateTokens(fc.SystemPrompt)
	for _, msg := range fc.Messages {
		totalTokens += estimateTokens(msg.Content)
	}
	fmt.Fprintf(&b, "Tokens (est): ~%d\n", totalTokens)
	return b.String()
}

// SnapshotRestorer restores a ForkContext from recorded events by finding
// the nearest context snapshot and extracting metadata from syscall events.
type SnapshotRestorer struct {
	reader *RecordReader
}

// NewSnapshotRestorer creates a new SnapshotRestorer from a RecordReader.
func NewSnapshotRestorer(reader *RecordReader) *SnapshotRestorer {
	return &SnapshotRestorer{reader: reader}
}

// RestoreContext builds a ForkContext from the recording state at the given seqNum.
// It finds the nearest context_snapshot at or before seqNum, extracts the intent
// from the Spawn syscall event, and reconstructs messages from the snapshot.
func (r *SnapshotRestorer) RestoreContext(seqNum uint64) (*ForkContext, error) {
	events := r.reader.Events()
	finder := NewSnapshotFinder(events)

	// Find nearest context snapshot at or before seqNum
	snapEv, err := finder.FindNearestBefore(seqNum)
	if err != nil {
		return nil, fmt.Errorf("fork: %w", err)
	}
	if snapEv.Context == nil {
		return nil, fmt.Errorf("fork: context snapshot #%d has no context data", snapEv.SeqNum)
	}

	meta := r.reader.Metadata()

	// Extract intent from Spawn syscall event (scan events up to seqNum)
	intent := meta.Intent // fallback to metadata intent
	for _, ev := range events {
		if ev.SeqNum > seqNum {
			break
		}
		if ev.Type == RecordSyscall && ev.Syscall != nil && ev.Syscall.Syscall == "Spawn" {
			if i, ok := ev.Syscall.Args["intent"]; ok {
				if s, ok := i.(string); ok {
					intent = s
				}
			}
		}
	}

	// Extract messages from snapshot summaries like "[system] You are..."
	// The system message is used both as SystemPrompt and included in Messages
	// to preserve the original message count from the snapshot.
	systemPrompt := ""
	var messages []ForkMessage
	for _, msgSummary := range snapEv.Context.Messages {
		role, content := parseMessageSummary(msgSummary)
		if role == "system" && systemPrompt == "" {
			systemPrompt = content
		}
		messages = append(messages, ForkMessage{
			Role:    role,
			Content: content,
		})
	}

	return &ForkContext{
		OriginalPID:  meta.PID,
		SeqNum:       seqNum,
		Intent:       intent,
		SystemPrompt: systemPrompt,
		Messages:     messages,
	}, nil
}

// parseMessageSummary parses a snapshot message summary like "[system] You are..."
// into a role and content.
func parseMessageSummary(summary string) (role, content string) {
	// Format: "[role] content"
	if !strings.HasPrefix(summary, "[") {
		return "user", summary
	}
	idx := strings.Index(summary, "] ")
	if idx < 0 {
		return "user", summary
	}
	role = summary[1:idx]
	content = summary[idx+2:]
	return role, content
}
