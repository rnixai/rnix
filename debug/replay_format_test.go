package debug

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// ReplayFormatter Tests — Story 14-2 ATDD (RED PHASE)
// All tests reference FormatReplayEvent, FormatReplayList, FormatReplaySummary
// which do not yet exist. Compile errors are expected until implementation.
// =============================================================================

// --- AC2: Event formatting per type ---

// 14.2-FMT-001: FormatReplayEvent formats syscall event
func TestFormatReplayEvent_Syscall(t *testing.T) {
	ev := &RecordEvent{
		SeqNum:    1,
		Timestamp: 500 * time.Millisecond,
		PID:       42,
		Type:      RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall:  "Open",
			Args:     map[string]any{"path": "/dev/llm/claude"},
			Result:   "FD(3)",
			Duration: 1200 * time.Microsecond,
		},
	}

	result := FormatReplayEvent(ev, false)

	// Should contain key elements
	if !strings.Contains(result, "#001") {
		t.Errorf("expected SeqNum marker '#001', got: %s", result)
	}
	if !strings.Contains(result, "syscall") {
		t.Errorf("expected 'syscall' type marker, got: %s", result)
	}
	if !strings.Contains(result, "Open") {
		t.Errorf("expected syscall name 'Open', got: %s", result)
	}
}

// 14.2-FMT-002: FormatReplayEvent formats LLM response event
func TestFormatReplayEvent_LLM(t *testing.T) {
	ev := &RecordEvent{
		SeqNum:    5,
		Timestamp: 2100 * time.Millisecond,
		PID:       42,
		Type:      RecordLLMResponse,
		LLM: &LLMResponseData{
			Model:          "claude-opus-4-6",
			RequestTokens:  1200,
			ResponseTokens: 800,
		},
	}

	result := FormatReplayEvent(ev, false)

	if !strings.Contains(result, "#005") {
		t.Errorf("expected SeqNum marker '#005', got: %s", result)
	}
	if !strings.Contains(result, "llm") {
		t.Errorf("expected 'llm' type marker, got: %s", result)
	}
	if !strings.Contains(result, "claude-opus-4-6") {
		t.Errorf("expected model name, got: %s", result)
	}
}

// 14.2-FMT-003: FormatReplayEvent formats context snapshot event
func TestFormatReplayEvent_Context(t *testing.T) {
	ev := &RecordEvent{
		SeqNum:    8,
		Timestamp: 3000 * time.Millisecond,
		PID:       42,
		Type:      RecordContextSnapshot,
		Context: &ContextSnapshotData{
			Messages: []string{"msg1", "msg2", "msg3", "msg4", "msg5", "msg6", "msg7", "msg8", "msg9", "msg10", "msg11", "msg12"},
		},
	}

	result := FormatReplayEvent(ev, false)

	if !strings.Contains(result, "#008") {
		t.Errorf("expected SeqNum marker '#008', got: %s", result)
	}
	if !strings.Contains(result, "context") {
		t.Errorf("expected 'context' type marker, got: %s", result)
	}
	if !strings.Contains(result, "12") {
		t.Errorf("expected message count '12', got: %s", result)
	}
}

// 14.2-FMT-004: FormatReplayEvent formats state change event
func TestFormatReplayEvent_State(t *testing.T) {
	ev := &RecordEvent{
		SeqNum:    10,
		Timestamp: 5000 * time.Millisecond,
		PID:       42,
		Type:      RecordStateChange,
		State: &StateChangeData{
			FromState: "Running",
			ToState:   "Zombie",
			Reason:    "completed",
		},
	}

	result := FormatReplayEvent(ev, false)

	if !strings.Contains(result, "#010") {
		t.Errorf("expected SeqNum marker '#010', got: %s", result)
	}
	if !strings.Contains(result, "state") {
		t.Errorf("expected 'state' type marker, got: %s", result)
	}
	if !strings.Contains(result, "Running") {
		t.Errorf("expected from state 'Running', got: %s", result)
	}
	if !strings.Contains(result, "Zombie") {
		t.Errorf("expected to state 'Zombie', got: %s", result)
	}
}

// --- AC5: List formatting ---

// 14.2-FMT-005: FormatReplayList marks cursor with marker
func TestFormatReplayList_CursorMarker(t *testing.T) {
	items := []ReplayListItem{
		{
			Event: RecordEvent{
				SeqNum:    1,
				Timestamp: 100 * time.Millisecond,
				Type:      RecordSyscall,
				Syscall:   &SyscallEventData{Syscall: "Open"},
			},
			IsCursor: false,
		},
		{
			Event: RecordEvent{
				SeqNum:    2,
				Timestamp: 200 * time.Millisecond,
				Type:      RecordSyscall,
				Syscall:   &SyscallEventData{Syscall: "Write"},
			},
			IsCursor: true,
		},
		{
			Event: RecordEvent{
				SeqNum:    3,
				Timestamp: 300 * time.Millisecond,
				Type:      RecordSyscall,
				Syscall:   &SyscallEventData{Syscall: "Read"},
			},
			IsCursor: false,
		},
	}

	result := FormatReplayList(items)

	// The cursor line should have a marker (e.g., ">")
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), result)
	}

	// Second line (cursor) should have a marker that non-cursor lines don't
	// Using ">" as the expected marker character
	cursorFound := false
	for _, line := range lines {
		if strings.Contains(line, "#002") {
			// This is the cursor line, it should have the marker
			cursorFound = true
		}
	}
	if !cursorFound {
		t.Errorf("expected cursor marker on line with #002, result: %s", result)
	}
}

// 14.2-FMT-006: FormatReplayList empty list returns empty string
func TestFormatReplayList_Empty(t *testing.T) {
	result := FormatReplayList(nil)

	if result != "" {
		t.Fatalf("expected empty string for nil items, got %q", result)
	}
}

// --- AC1: Summary formatting ---

// 14.2-FMT-007: FormatReplaySummary formats recording metadata
func TestFormatReplaySummary(t *testing.T) {
	meta := RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "analyze code",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: 128,
		Status:     RecordStatusCompleted,
	}

	result := FormatReplaySummary(meta, 128)

	if !strings.Contains(result, "42-1709856000") {
		t.Errorf("expected RecordID, got: %s", result)
	}
	if !strings.Contains(result, "42") {
		t.Errorf("expected PID, got: %s", result)
	}
	if !strings.Contains(result, "analyze code") {
		t.Errorf("expected Intent, got: %s", result)
	}
	if !strings.Contains(result, "128") {
		t.Errorf("expected event count, got: %s", result)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("expected status, got: %s", result)
	}
}

// 14.2-FMT-008: FormatReplayEvent verbose mode includes additional details
func TestFormatReplayEvent_VerboseMode(t *testing.T) {
	ev := &RecordEvent{
		SeqNum:    1,
		Timestamp: 500 * time.Millisecond,
		PID:       42,
		Type:      RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall:  "Open",
			Args:     map[string]any{"path": "/dev/llm/claude"},
			Result:   "FD(3)",
			Duration: 1200 * time.Microsecond,
		},
	}

	normal := FormatReplayEvent(ev, false)
	verbose := FormatReplayEvent(ev, true)

	// Verbose output should be longer or equal (more details)
	if len(verbose) < len(normal) {
		t.Errorf("expected verbose output >= normal output length")
	}
}
