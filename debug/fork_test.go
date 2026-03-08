package debug

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// Story 14-4 ATDD RED PHASE: SnapshotRestorer, ForkContext, ForkMessage
//
// All tests reference types/functions in fork.go which does NOT exist yet.
// Compile errors are expected until implementation.
// =============================================================================

// --- Helper: build events with context snapshots and syscall details for fork ---

func buildForkTestEvents() []RecordEvent {
	return []RecordEvent{
		// Spawn event with intent in Args
		{SeqNum: 1, Timestamp: 100 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall:  "Spawn",
				Duration: 1 * time.Millisecond,
				Args:     map[string]any{"intent": "分析代码", "agent": "default"},
			}},
		// CtxAlloc
		{SeqNum: 2, Timestamp: 200 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall:  "CtxAlloc",
				Duration: 1 * time.Millisecond,
				Args:     map[string]any{"cid": float64(1)},
			}},
		// First context snapshot (5 messages)
		{SeqNum: 3, Timestamp: 300 * time.Millisecond, PID: 42, Type: RecordContextSnapshot,
			Context: &ContextSnapshotData{
				SystemPromptHash: "a1b2c3d4e5f6a7b8",
				MessageCount:     5,
				Messages:         []string{"[system] You are...", "[user] Hello", "[assistant] Hi", "[user] Analyze code", "[assistant] OK"},
				TokenEstimate:    2500,
			}},
		// Syscall between snapshots
		{SeqNum: 4, Timestamp: 400 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Open", Duration: 2 * time.Millisecond}},
		// LLM Response
		{SeqNum: 5, Timestamp: 500 * time.Millisecond, PID: 42, Type: RecordLLMResponse,
			LLM: &LLMResponseData{Model: "claude", RequestTokens: 2500, ResponseTokens: 300, ResponseSummary: "analysis result"}},
		// Second context snapshot (8 messages)
		{SeqNum: 6, Timestamp: 1800 * time.Millisecond, PID: 42, Type: RecordContextSnapshot,
			Context: &ContextSnapshotData{
				SystemPromptHash: "a1b2c3d4e5f6a7b8",
				MessageCount:     8,
				Messages:         []string{"[system] You are...", "[user] Hello", "[assistant] Hi", "[user] Analyze code", "[assistant] OK", "[user] Analyze perf", "[assistant] Analyzing...", "[user] Optimize findUser"},
				TokenEstimate:    4200,
			}},
		// More events after second snapshot
		{SeqNum: 7, Timestamp: 2000 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Write", Duration: 1 * time.Millisecond}},
		{SeqNum: 8, Timestamp: 2500 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Close", Duration: 1 * time.Millisecond}},
	}
}

// =============================================================================
// SnapshotRestorer Tests (AC #1: fork restores context from snapshot)
// =============================================================================

// 14.4-FORK-001: [P0] NewSnapshotRestorer creates restorer from RecordReader
func TestNewSnapshotRestorer(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	if restorer == nil {
		t.Fatal("NewSnapshotRestorer returned nil")
	}
}

// 14.4-FORK-002: [P0] RestoreContext extracts intent from Spawn event
func TestSnapshotRestorer_RestoreContext_ExtractsIntent(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	forkCtx, err := restorer.RestoreContext(6)
	if err != nil {
		t.Fatalf("RestoreContext(6) failed: %v", err)
	}
	if forkCtx.Intent != "分析代码" {
		t.Fatalf("expected Intent='分析代码', got %q", forkCtx.Intent)
	}
}

// 14.4-FORK-003: [P0] RestoreContext uses context snapshot Messages
func TestSnapshotRestorer_RestoreContext_UsesSnapshot(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	// SeqNum 6 is the second context_snapshot with 8 messages
	forkCtx, err := restorer.RestoreContext(6)
	if err != nil {
		t.Fatalf("RestoreContext(6) failed: %v", err)
	}
	if len(forkCtx.Messages) != 8 {
		t.Fatalf("expected 8 messages from snapshot, got %d", len(forkCtx.Messages))
	}
}

// 14.4-FORK-004: [P0] RestoreContext truncates at specified SeqNum
func TestSnapshotRestorer_RestoreContext_TruncatesAtSeqNum(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	// SeqNum 4 is between snapshots, nearest snapshot before is #3 with 5 messages
	forkCtx, err := restorer.RestoreContext(4)
	if err != nil {
		t.Fatalf("RestoreContext(4) failed: %v", err)
	}
	if len(forkCtx.Messages) != 5 {
		t.Fatalf("expected 5 messages (from snapshot #3), got %d", len(forkCtx.Messages))
	}
}

// 14.4-FORK-005: [P0] RestoreContext returns error when no snapshot before SeqNum
func TestSnapshotRestorer_RestoreContext_NoSnapshotBeforeSeqNum(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	// SeqNum 1 is before any context snapshot (first snapshot is #3)
	_, err = restorer.RestoreContext(1)
	if err == nil {
		t.Fatal("expected error for RestoreContext(1) where no snapshot before SeqNum 1, got nil")
	}
}

// 14.4-FORK-006: [P0] RestoreContext records OriginalPID from recording metadata
func TestSnapshotRestorer_RestoreContext_SetsOriginalPID(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	forkCtx, err := restorer.RestoreContext(6)
	if err != nil {
		t.Fatalf("RestoreContext(6) failed: %v", err)
	}
	if forkCtx.OriginalPID != types.PID(42) {
		t.Fatalf("expected OriginalPID=42, got %d", forkCtx.OriginalPID)
	}
}

// 14.4-FORK-007: [P0] RestoreContext records SeqNum
func TestSnapshotRestorer_RestoreContext_SetsSeqNum(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	forkCtx, err := restorer.RestoreContext(7)
	if err != nil {
		t.Fatalf("RestoreContext(7) failed: %v", err)
	}
	if forkCtx.SeqNum != 7 {
		t.Fatalf("expected SeqNum=7, got %d", forkCtx.SeqNum)
	}
}

// 14.4-FORK-008: [P1] RestoreContext extracts system prompt hash from snapshot
func TestSnapshotRestorer_RestoreContext_SystemPrompt(t *testing.T) {
	dir := t.TempDir()
	events := buildForkTestEvents()
	writeTestMetadata(t, dir, RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "分析代码",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(len(events)),
		Status:     RecordStatusCompleted,
	})
	writeTestEvents(t, dir, events)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	restorer := NewSnapshotRestorer(reader)
	forkCtx, err := restorer.RestoreContext(6)
	if err != nil {
		t.Fatalf("RestoreContext(6) failed: %v", err)
	}
	// SystemPrompt comes from the snapshot's first message [system] or is noted
	// The ForkContext should have some system prompt information
	if forkCtx.SystemPrompt == "" {
		t.Fatal("expected non-empty SystemPrompt, got empty string")
	}
}

// =============================================================================
// ForkContext Modification Tests (AC #2: modify context before continue)
// =============================================================================

// 14.4-FORK-009: [P0] ForkContext.SetSystemPrompt modifies system prompt
func TestForkContext_SetSystemPrompt(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "original prompt",
		Messages:     []ForkMessage{{Role: "user", Content: "hello"}},
	}

	fc.SetSystemPrompt("new prompt")
	if fc.SystemPrompt != "new prompt" {
		t.Fatalf("expected SystemPrompt='new prompt', got %q", fc.SystemPrompt)
	}
}

// 14.4-FORK-010: [P0] ForkContext.AppendMessage adds a message
func TestForkContext_AppendMessage(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "prompt",
		Messages:     []ForkMessage{{Role: "user", Content: "hello"}},
	}

	fc.AppendMessage("assistant", "world")
	if len(fc.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(fc.Messages))
	}
	last := fc.Messages[len(fc.Messages)-1]
	if last.Role != "assistant" || last.Content != "world" {
		t.Fatalf("expected last message {assistant, world}, got {%s, %s}", last.Role, last.Content)
	}
}

// 14.4-FORK-011: [P0] ForkContext.RemoveLastMessages removes messages from end
func TestForkContext_RemoveLastMessages(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "prompt",
		Messages: []ForkMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
			{Role: "user", Content: "third"},
		},
	}

	fc.RemoveLastMessages(2)
	if len(fc.Messages) != 1 {
		t.Fatalf("expected 1 message after removing 2, got %d", len(fc.Messages))
	}
	if fc.Messages[0].Content != "first" {
		t.Fatalf("expected remaining message 'first', got %q", fc.Messages[0].Content)
	}
}

// 14.4-FORK-012: [P0] ForkContext.RemoveLastMessages handles removing more than available
func TestForkContext_RemoveLastMessages_MoreThanAvailable(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "prompt",
		Messages: []ForkMessage{
			{Role: "user", Content: "only"},
		},
	}

	fc.RemoveLastMessages(5)
	if len(fc.Messages) != 0 {
		t.Fatalf("expected 0 messages after removing more than available, got %d", len(fc.Messages))
	}
}

// 14.4-FORK-013: [P0] ForkContext.ReplaceLastMessage replaces the last message
func TestForkContext_ReplaceLastMessage(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "prompt",
		Messages: []ForkMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "original reply"},
		},
	}

	fc.ReplaceLastMessage("replaced reply")
	if len(fc.Messages) != 2 {
		t.Fatalf("expected 2 messages (unchanged count), got %d", len(fc.Messages))
	}
	if fc.Messages[1].Content != "replaced reply" {
		t.Fatalf("expected last message content 'replaced reply', got %q", fc.Messages[1].Content)
	}
	// Role should be preserved
	if fc.Messages[1].Role != "assistant" {
		t.Fatalf("expected last message role preserved as 'assistant', got %q", fc.Messages[1].Role)
	}
}

// 14.4-FORK-014: [P1] ForkContext.ReplaceLastMessage on empty messages does not panic
func TestForkContext_ReplaceLastMessage_EmptyMessages(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "test",
		SystemPrompt: "prompt",
		Messages:     []ForkMessage{},
	}

	// Should not panic and should be a no-op or return error
	fc.ReplaceLastMessage("test")
	// Messages should still be empty (no-op)
	if len(fc.Messages) != 0 {
		t.Fatalf("expected 0 messages after ReplaceLastMessage on empty, got %d", len(fc.Messages))
	}
}

// 14.4-FORK-015: [P0] ForkContext.Summary returns readable summary
func TestForkContext_Summary(t *testing.T) {
	fc := &ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "分析代码",
		SystemPrompt: "You are a helpful assistant",
		Messages: []ForkMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "analyze this code"},
		},
	}

	summary := fc.Summary()
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	// Should contain message count
	if !strings.Contains(summary, "3") {
		t.Fatalf("expected summary to contain message count '3', got:\n%s", summary)
	}
	// Should contain PID
	if !strings.Contains(summary, "42") {
		t.Fatalf("expected summary to contain PID '42', got:\n%s", summary)
	}
}

// =============================================================================
// ForkMessage Struct Tests (AC #2)
// =============================================================================

// 14.4-FORK-016: [P0] ForkMessage struct has required fields
func TestForkMessage_Fields(t *testing.T) {
	msg := ForkMessage{
		Role:       "user",
		Content:    "test content",
		ToolCallID: "tool-123",
	}

	if msg.Role != "user" {
		t.Fatalf("expected Role='user', got %q", msg.Role)
	}
	if msg.Content != "test content" {
		t.Fatalf("expected Content='test content', got %q", msg.Content)
	}
	if msg.ToolCallID != "tool-123" {
		t.Fatalf("expected ToolCallID='tool-123', got %q", msg.ToolCallID)
	}
}

// 14.4-FORK-017: [P0] ForkMessage ToolCallID is optional
func TestForkMessage_ToolCallID_Optional(t *testing.T) {
	msg := ForkMessage{
		Role:    "assistant",
		Content: "response",
	}
	if msg.ToolCallID != "" {
		t.Fatalf("expected empty ToolCallID for non-tool message, got %q", msg.ToolCallID)
	}
}

// =============================================================================
// ForkContext JSON Serialization Tests (AC #2: IPC transmission)
// =============================================================================

// 14.4-FORK-018: [P0] ForkContext JSON round-trip preserves all fields
func TestForkContext_JSONRoundTrip(t *testing.T) {
	fc := ForkContext{
		OriginalPID:  42,
		SeqNum:       6,
		Intent:       "分析代码",
		SystemPrompt: "You are a helpful assistant",
		Messages: []ForkMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
			{Role: "tool", Content: "result", ToolCallID: "/dev/llm/claude"},
		},
	}

	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("json.Marshal ForkContext failed: %v", err)
	}

	var decoded ForkContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal ForkContext failed: %v", err)
	}

	if decoded.OriginalPID != 42 {
		t.Fatalf("expected OriginalPID=42, got %d", decoded.OriginalPID)
	}
	if decoded.SeqNum != 6 {
		t.Fatalf("expected SeqNum=6, got %d", decoded.SeqNum)
	}
	if decoded.Intent != "分析代码" {
		t.Fatalf("expected Intent='分析代码', got %q", decoded.Intent)
	}
	if len(decoded.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(decoded.Messages))
	}
	if decoded.Messages[2].ToolCallID != "/dev/llm/claude" {
		t.Fatalf("expected ToolCallID='/dev/llm/claude', got %q", decoded.Messages[2].ToolCallID)
	}
}
