package debug

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Story 14-3 ATDD RED PHASE: SnapshotFinder, ComputeContextDiff, FormatContextDiff
//
// All tests reference types/functions in snapshot_diff.go which does NOT exist yet.
// Compile errors are expected until implementation.
// =============================================================================

// --- Helper: build events with context snapshots ---

func buildSnapshotTestEvents() []RecordEvent {
	return []RecordEvent{
		{SeqNum: 1, Timestamp: 100 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Spawn", Duration: 1 * time.Millisecond}},
		{SeqNum: 2, Timestamp: 200 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "CtxAlloc", Duration: 1 * time.Millisecond}},
		{SeqNum: 3, Timestamp: 300 * time.Millisecond, PID: 42, Type: RecordContextSnapshot,
			Context: &ContextSnapshotData{
				Messages: []string{"[system] You are...", "[user] Hello", "[assistant] Hi", "[user] Analyze code", "[assistant] OK"},
			}},
		{SeqNum: 4, Timestamp: 400 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Open", Duration: 2 * time.Millisecond}},
		{SeqNum: 5, Timestamp: 500 * time.Millisecond, PID: 42, Type: RecordLLMResponse,
			LLM: &LLMResponseData{Model: "claude", RequestTokens: 2500, ResponseTokens: 300}},
		{SeqNum: 6, Timestamp: 1800 * time.Millisecond, PID: 42, Type: RecordContextSnapshot,
			Context: &ContextSnapshotData{
				Messages: []string{"[system] You are...", "[user] Hello", "[assistant] Hi", "[user] Analyze code", "[assistant] OK", "[user] Analyze perf", "[assistant] Analyzing...", "[user] Optimize findUser"},
			}},
		{SeqNum: 7, Timestamp: 2000 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Write", Duration: 1 * time.Millisecond}},
		{SeqNum: 8, Timestamp: 2500 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Close", Duration: 1 * time.Millisecond}},
	}
}

func buildNoSnapshotEvents() []RecordEvent {
	return []RecordEvent{
		{SeqNum: 1, Timestamp: 100 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Spawn", Duration: 1 * time.Millisecond}},
		{SeqNum: 2, Timestamp: 200 * time.Millisecond, PID: 42, Type: RecordSyscall,
			Syscall: &SyscallEventData{Syscall: "Open", Duration: 1 * time.Millisecond}},
	}
}

// =============================================================================
// SnapshotFinder Tests (AC #3, #4)
// =============================================================================

// 14.3-SNAP-001: [P0] NewSnapshotFinder creates finder from events
func TestNewSnapshotFinder(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)
	if finder == nil {
		t.Fatal("NewSnapshotFinder returned nil")
	}
}

// 14.3-SNAP-002: [P0] HasSnapshots returns true when context_snapshot events exist
func TestSnapshotFinder_HasSnapshots_True(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)
	if !finder.HasSnapshots() {
		t.Fatal("HasSnapshots() returned false, expected true (events contain context_snapshot)")
	}
}

// 14.3-SNAP-003: [P0] HasSnapshots returns false when no context_snapshot events
func TestSnapshotFinder_HasSnapshots_False(t *testing.T) {
	events := buildNoSnapshotEvents()
	finder := NewSnapshotFinder(events)
	if finder.HasSnapshots() {
		t.Fatal("HasSnapshots() returned true, expected false (no context_snapshot events)")
	}
}

// 14.3-SNAP-004: [P0] FindNearestBefore finds exact match on context_snapshot SeqNum
func TestSnapshotFinder_FindNearestBefore_ExactMatch(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)

	// SeqNum 3 is a context_snapshot, should find itself
	ev, err := finder.FindNearestBefore(3)
	if err != nil {
		t.Fatalf("FindNearestBefore(3) failed: %v", err)
	}
	if ev.SeqNum != 3 {
		t.Fatalf("expected SeqNum=3 (exact match), got %d", ev.SeqNum)
	}
	if ev.Type != RecordContextSnapshot {
		t.Fatalf("expected type=context_snapshot, got %s", ev.Type)
	}
}

// 14.3-SNAP-005: [P0] FindNearestBefore finds previous snapshot when target is not snapshot
func TestSnapshotFinder_FindNearestBefore_SearchBackward(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)

	// SeqNum 5 is llm_response, nearest snapshot before it is #3
	ev, err := finder.FindNearestBefore(5)
	if err != nil {
		t.Fatalf("FindNearestBefore(5) failed: %v", err)
	}
	if ev.SeqNum != 3 {
		t.Fatalf("expected SeqNum=3 (nearest snapshot before 5), got %d", ev.SeqNum)
	}
}

// 14.3-SNAP-006: [P0] FindNearestBefore for SeqNum after second snapshot finds that snapshot
func TestSnapshotFinder_FindNearestBefore_SecondSnapshot(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)

	// SeqNum 7 is a syscall after snapshot #6, should find #6
	ev, err := finder.FindNearestBefore(7)
	if err != nil {
		t.Fatalf("FindNearestBefore(7) failed: %v", err)
	}
	if ev.SeqNum != 6 {
		t.Fatalf("expected SeqNum=6 (nearest snapshot before 7), got %d", ev.SeqNum)
	}
}

// 14.3-SNAP-007: [P0] FindNearestBefore returns error when no snapshot exists before target
func TestSnapshotFinder_FindNearestBefore_NoSnapshotBefore(t *testing.T) {
	events := buildSnapshotTestEvents()
	finder := NewSnapshotFinder(events)

	// SeqNum 1 is before any context_snapshot (first snapshot is #3)
	_, err := finder.FindNearestBefore(1)
	if err == nil {
		t.Fatal("expected error for FindNearestBefore(1), got nil (no snapshot before SeqNum 1)")
	}
}

// 14.3-SNAP-008: [P0] FindNearestBefore returns error when events have no snapshots at all
func TestSnapshotFinder_FindNearestBefore_NoSnapshots(t *testing.T) {
	events := buildNoSnapshotEvents()
	finder := NewSnapshotFinder(events)

	_, err := finder.FindNearestBefore(2)
	if err == nil {
		t.Fatal("expected error for FindNearestBefore when no snapshots exist, got nil")
	}
}

// 14.3-SNAP-009: [P1] FindNearestBefore handles empty event list
func TestSnapshotFinder_FindNearestBefore_EmptyEvents(t *testing.T) {
	finder := NewSnapshotFinder(nil)

	_, err := finder.FindNearestBefore(1)
	if err == nil {
		t.Fatal("expected error for FindNearestBefore on empty events, got nil")
	}
}

// =============================================================================
// ComputeContextDiff Tests (AC #1, #2)
// =============================================================================

// 14.3-DIFF-001: [P0] ComputeContextDiff detects message additions
func TestComputeContextDiff_MessageAdditions(t *testing.T) {
	events := buildSnapshotTestEvents()

	from := events[2].Context  // snapshot #3: 5 messages
	to := events[5].Context    // snapshot #6: 8 messages
	fromEv := &events[2]
	toEv := &events[5]

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	if diff == nil {
		t.Fatal("ComputeContextDiff returned nil")
	}
	if diff.FromSeqNum != 3 {
		t.Fatalf("expected FromSeqNum=3, got %d", diff.FromSeqNum)
	}
	if diff.ToSeqNum != 6 {
		t.Fatalf("expected ToSeqNum=6, got %d", diff.ToSeqNum)
	}
	if len(diff.Messages.Added) != 3 {
		t.Fatalf("expected 3 added messages, got %d", len(diff.Messages.Added))
	}
	if len(diff.Messages.Removed) != 0 {
		t.Fatalf("expected 0 removed messages, got %d", len(diff.Messages.Removed))
	}
	if diff.Messages.FromCount != 5 {
		t.Fatalf("expected FromCount=5, got %d", diff.Messages.FromCount)
	}
	if diff.Messages.ToCount != 8 {
		t.Fatalf("expected ToCount=8, got %d", diff.Messages.ToCount)
	}
}

// 14.3-DIFF-002: [P0] ComputeContextDiff system prompt change detection
// (Simplified in Story 27.1 AC-9: SystemPromptHash removed, Changed is always false)
func TestComputeContextDiff_SystemPromptChanged(t *testing.T) {
	from := &ContextSnapshotData{
		Messages: []string{"[system] v1", "[user] hi", "[assistant] hello"},
	}
	to := &ContextSnapshotData{
		Messages: []string{"[system] v2", "[user] hi", "[assistant] hello"},
	}
	fromEv := &RecordEvent{SeqNum: 1, Timestamp: 100 * time.Millisecond}
	toEv := &RecordEvent{SeqNum: 5, Timestamp: 500 * time.Millisecond}

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	// SystemPromptHash removed in Story 27.1, so Changed is always false
	if diff.SystemPrompt.Changed {
		t.Fatal("expected SystemPrompt.Changed=false (hash no longer tracked), got true")
	}
}

// 14.3-DIFF-003: [P0] ComputeContextDiff system prompt unchanged
func TestComputeContextDiff_SystemPromptUnchanged(t *testing.T) {
	events := buildSnapshotTestEvents()

	from := events[2].Context  // both have same hash
	to := events[5].Context
	fromEv := &events[2]
	toEv := &events[5]

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	if diff.SystemPrompt.Changed {
		t.Fatal("expected SystemPrompt.Changed=false, got true (same hash)")
	}
}

// 14.3-DIFF-004: [P0] ComputeContextDiff token delta
// (Simplified in Story 27.1 AC-9: TokenEstimate removed, delta always 0)
func TestComputeContextDiff_TokenDeltaPositive(t *testing.T) {
	events := buildSnapshotTestEvents()

	from := events[2].Context
	to := events[5].Context
	fromEv := &events[2]
	toEv := &events[5]

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	// TokenEstimate removed in Story 27.1 AC-9, so all token values are 0
	if diff.TokenDelta.FromTokens != 0 {
		t.Fatalf("expected FromTokens=0, got %d", diff.TokenDelta.FromTokens)
	}
	if diff.TokenDelta.ToTokens != 0 {
		t.Fatalf("expected ToTokens=0, got %d", diff.TokenDelta.ToTokens)
	}
	if diff.TokenDelta.Delta != 0 {
		t.Fatalf("expected Delta=0, got %d", diff.TokenDelta.Delta)
	}
}

// 14.3-DIFF-005: [P1] ComputeContextDiff calculates negative token delta
func TestComputeContextDiff_TokenDeltaNegative(t *testing.T) {
	from := &ContextSnapshotData{
		Messages: []string{"a", "b", "c", "d", "e"},
	}
	to := &ContextSnapshotData{
		Messages: []string{"a", "b", "c"},
	}
	fromEv := &RecordEvent{SeqNum: 10, Timestamp: time.Second}
	toEv := &RecordEvent{SeqNum: 20, Timestamp: 2 * time.Second}

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	// TokenEstimate removed in Story 27.1 AC-9, so Delta is always 0
	if diff.TokenDelta.Delta != 0 {
		t.Fatalf("expected Delta=0, got %d", diff.TokenDelta.Delta)
	}
}

// 14.3-DIFF-006: [P1] ComputeContextDiff detects zero changes (identical snapshots)
func TestComputeContextDiff_NoChanges(t *testing.T) {
	snap := &ContextSnapshotData{
		Messages: []string{"a", "b", "c"},
	}
	fromEv := &RecordEvent{SeqNum: 3, Timestamp: 300 * time.Millisecond}
	toEv := &RecordEvent{SeqNum: 6, Timestamp: 600 * time.Millisecond}

	diff := ComputeContextDiff(snap, snap, fromEv, toEv)

	if diff.SystemPrompt.Changed {
		t.Fatal("expected SystemPrompt.Changed=false for identical snapshots")
	}
	if len(diff.Messages.Added) != 0 {
		t.Fatalf("expected 0 added messages for identical snapshots, got %d", len(diff.Messages.Added))
	}
	if len(diff.Messages.Removed) != 0 {
		t.Fatalf("expected 0 removed messages for identical snapshots, got %d", len(diff.Messages.Removed))
	}
	if diff.TokenDelta.Delta != 0 {
		t.Fatalf("expected Delta=0 for identical snapshots, got %d", diff.TokenDelta.Delta)
	}
}

// 14.3-DIFF-007: [P1] ComputeContextDiff detects message removals
func TestComputeContextDiff_MessageRemovals(t *testing.T) {
	from := &ContextSnapshotData{
		Messages: []string{"a", "b", "c", "d", "e"},
	}
	to := &ContextSnapshotData{
		Messages: []string{"a", "b", "c"},
	}
	fromEv := &RecordEvent{SeqNum: 3, Timestamp: 300 * time.Millisecond}
	toEv := &RecordEvent{SeqNum: 6, Timestamp: 600 * time.Millisecond}

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	if len(diff.Messages.Removed) != 2 {
		t.Fatalf("expected 2 removed messages, got %d", len(diff.Messages.Removed))
	}
	if len(diff.Messages.Added) != 0 {
		t.Fatalf("expected 0 added messages, got %d", len(diff.Messages.Added))
	}
}

// 14.3-DIFF-008: [P1] ComputeContextDiff captures timestamps from events
func TestComputeContextDiff_Timestamps(t *testing.T) {
	from := &ContextSnapshotData{
		Messages: []string{"a"},
	}
	to := &ContextSnapshotData{
		Messages: []string{"a", "b"},
	}
	fromEv := &RecordEvent{SeqNum: 3, Timestamp: 300 * time.Millisecond}
	toEv := &RecordEvent{SeqNum: 6, Timestamp: 1800 * time.Millisecond}

	diff := ComputeContextDiff(from, to, fromEv, toEv)

	if diff.FromTimestamp != 300*time.Millisecond {
		t.Fatalf("expected FromTimestamp=300ms, got %v", diff.FromTimestamp)
	}
	if diff.ToTimestamp != 1800*time.Millisecond {
		t.Fatalf("expected ToTimestamp=1800ms, got %v", diff.ToTimestamp)
	}
}

// =============================================================================
// FormatContextDiff Tests (AC #1, #2)
// =============================================================================

// 14.3-FMT-001: [P0] FormatContextDiff formats diff with changes
func TestFormatContextDiff_WithChanges(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    3,
		ToSeqNum:      6,
		FromTimestamp:  300 * time.Millisecond,
		ToTimestamp:    1800 * time.Millisecond,
		SystemPrompt: PromptDiff{
			Changed:  false,
			FromHash: "a1b2c3d4",
			ToHash:   "a1b2c3d4",
		},
		Messages: MessagesDiff{
			Added:     []string{"[user] Analyze perf", "[assistant] Analyzing...", "[user] Optimize findUser"},
			Removed:   nil,
			FromCount: 5,
			ToCount:   8,
		},
		TokenDelta: TokenDelta{
			FromTokens: 2500,
			ToTokens:   4200,
			Delta:      1700,
		},
	}

	output := FormatContextDiff(diff)

	// Verify header with SeqNum range
	if !strings.Contains(output, "#003") || !strings.Contains(output, "#006") {
		t.Fatalf("expected output to contain '#003' and '#006', got:\n%s", output)
	}

	// Verify messages section shows count change
	if !strings.Contains(output, "5") || !strings.Contains(output, "8") {
		t.Fatalf("expected output to contain message count '5' and '8', got:\n%s", output)
	}

	// Verify added messages are shown with + prefix
	if !strings.Contains(output, "+") {
		t.Fatalf("expected output to contain '+' for added messages, got:\n%s", output)
	}

	// Verify token delta
	if !strings.Contains(output, "2500") || !strings.Contains(output, "4200") || !strings.Contains(output, "1700") {
		t.Fatalf("expected output to contain token values 2500, 4200, 1700, got:\n%s", output)
	}
}

// 14.3-FMT-002: [P0] FormatContextDiff formats diff with no changes
func TestFormatContextDiff_NoChanges(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    3,
		ToSeqNum:      6,
		FromTimestamp:  300 * time.Millisecond,
		ToTimestamp:    600 * time.Millisecond,
		SystemPrompt: PromptDiff{
			Changed:  false,
			FromHash: "aabbccdd",
			ToHash:   "aabbccdd",
		},
		Messages: MessagesDiff{
			Added:     nil,
			Removed:   nil,
			FromCount: 3,
			ToCount:   3,
		},
		TokenDelta: TokenDelta{
			FromTokens: 1500,
			ToTokens:   1500,
			Delta:      0,
		},
	}

	output := FormatContextDiff(diff)

	// Should indicate no changes
	if !strings.Contains(output, "No changes") && !strings.Contains(output, "no changes") && !strings.Contains(output, "unchanged") {
		t.Fatalf("expected output to indicate no changes, got:\n%s", output)
	}
}

// 14.3-FMT-003: [P0] FormatContextDiff shows system prompt change
func TestFormatContextDiff_SystemPromptChange(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    1,
		ToSeqNum:      10,
		FromTimestamp:  100 * time.Millisecond,
		ToTimestamp:    5000 * time.Millisecond,
		SystemPrompt: PromptDiff{
			Changed:  true,
			FromHash: "a1b2c3d4",
			ToHash:   "e5f6g7h8",
		},
		Messages: MessagesDiff{
			Added:     []string{"[user] msg"},
			FromCount: 3,
			ToCount:   4,
		},
		TokenDelta: TokenDelta{
			FromTokens: 1000,
			ToTokens:   1500,
			Delta:      500,
		},
	}

	output := FormatContextDiff(diff)

	// Should show "changed" for system prompt
	if !strings.Contains(output, "changed") && !strings.Contains(output, "Changed") {
		t.Fatalf("expected output to indicate system prompt changed, got:\n%s", output)
	}
	// Should show both hashes
	if !strings.Contains(output, "a1b2c3d4") || !strings.Contains(output, "e5f6g7h8") {
		t.Fatalf("expected output to contain both hashes, got:\n%s", output)
	}
}

// 14.3-FMT-004: [P0] FormatContextDiff shows removed messages with - prefix
func TestFormatContextDiff_RemovedMessages(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    1,
		ToSeqNum:      5,
		FromTimestamp:  100 * time.Millisecond,
		ToTimestamp:    500 * time.Millisecond,
		SystemPrompt: PromptDiff{Changed: false, FromHash: "aa", ToHash: "aa"},
		Messages: MessagesDiff{
			Added:     nil,
			Removed:   []string{"[user] old msg", "[assistant] old reply"},
			FromCount: 5,
			ToCount:   3,
		},
		TokenDelta: TokenDelta{FromTokens: 2000, ToTokens: 1200, Delta: -800},
	}

	output := FormatContextDiff(diff)

	// Should show removed messages with - prefix
	if !strings.Contains(output, "-") {
		t.Fatalf("expected output to contain '-' for removed messages, got:\n%s", output)
	}
}

// 14.3-FMT-005: [P1] FormatContextDiffJSON returns valid JSON
func TestFormatContextDiffJSON(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    3,
		ToSeqNum:      6,
		FromTimestamp:  300 * time.Millisecond,
		ToTimestamp:    1800 * time.Millisecond,
		SystemPrompt: PromptDiff{Changed: false, FromHash: "aa", ToHash: "aa"},
		Messages: MessagesDiff{
			Added:     []string{"[user] msg"},
			FromCount: 5,
			ToCount:   6,
		},
		TokenDelta: TokenDelta{FromTokens: 2500, ToTokens: 3000, Delta: 500},
	}

	data, err := FormatContextDiffJSON(diff)
	if err != nil {
		t.Fatalf("FormatContextDiffJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("FormatContextDiffJSON output is not valid JSON: %v\nOutput: %s", err, string(data))
	}

	// Verify required fields
	requiredFields := []string{"from_seq_num", "to_seq_num", "from_timestamp_ms", "to_timestamp_ms", "system_prompt", "messages", "token_delta"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("missing required field %q in JSON output: %s", field, string(data))
		}
	}

	// Verify timestamps are in milliseconds (not nanoseconds)
	fromTS, ok := parsed["from_timestamp_ms"].(float64)
	if !ok {
		t.Fatalf("from_timestamp_ms is not a number: %s", string(data))
	}
	if fromTS != 300 {
		t.Fatalf("from_timestamp_ms: got %v, want 300", fromTS)
	}
	toTS, ok := parsed["to_timestamp_ms"].(float64)
	if !ok {
		t.Fatalf("to_timestamp_ms is not a number: %s", string(data))
	}
	if toTS != 1800 {
		t.Fatalf("to_timestamp_ms: got %v, want 1800", toTS)
	}
}

// 14.3-FMT-006: [P1] FormatContextDiff shows negative token delta correctly
func TestFormatContextDiff_NegativeTokenDelta(t *testing.T) {
	diff := &ContextDiff{
		FromSeqNum:    1,
		ToSeqNum:      5,
		FromTimestamp:  100 * time.Millisecond,
		ToTimestamp:    500 * time.Millisecond,
		SystemPrompt: PromptDiff{Changed: false, FromHash: "aa", ToHash: "aa"},
		Messages: MessagesDiff{
			Removed:   []string{"[user] removed"},
			FromCount: 5,
			ToCount:   4,
		},
		TokenDelta: TokenDelta{FromTokens: 3000, ToTokens: 2500, Delta: -500},
	}

	output := FormatContextDiff(diff)

	// Should show negative delta
	if !strings.Contains(output, "-500") {
		t.Fatalf("expected output to contain '-500' for negative token delta, got:\n%s", output)
	}
}

// =============================================================================
// ContextDiff / PromptDiff / MessagesDiff / TokenDelta struct tests
// =============================================================================

// 14.3-TYPE-001: [P0] ContextDiff JSON serialization
func TestContextDiff_JSONSerialization(t *testing.T) {
	diff := ContextDiff{
		FromSeqNum:    3,
		ToSeqNum:      6,
		FromTimestamp:  300 * time.Millisecond,
		ToTimestamp:    1800 * time.Millisecond,
		SystemPrompt: PromptDiff{Changed: true, FromHash: "aabb", ToHash: "ccdd"},
		Messages: MessagesDiff{
			Added:     []string{"msg1", "msg2"},
			Removed:   []string{"old"},
			FromCount: 5,
			ToCount:   6,
		},
		TokenDelta: TokenDelta{FromTokens: 2500, ToTokens: 4200, Delta: 1700},
	}

	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("json.Marshal ContextDiff failed: %v", err)
	}

	var decoded ContextDiff
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal ContextDiff failed: %v", err)
	}

	if decoded.FromSeqNum != 3 {
		t.Fatalf("expected FromSeqNum=3, got %d", decoded.FromSeqNum)
	}
	if decoded.ToSeqNum != 6 {
		t.Fatalf("expected ToSeqNum=6, got %d", decoded.ToSeqNum)
	}
	if !decoded.SystemPrompt.Changed {
		t.Fatal("expected SystemPrompt.Changed=true")
	}
	if decoded.TokenDelta.Delta != 1700 {
		t.Fatalf("expected Delta=1700, got %d", decoded.TokenDelta.Delta)
	}
}
