package debug

import (
	"testing"
	"time"
)

// =============================================================================
// ReplaySession Tests — Story 14-2 ATDD (RED PHASE)
// All tests reference ReplaySession which does not yet exist.
// Compile errors are expected until implementation.
// =============================================================================

// --- AC2: Forward playback (next/n) ---

// 14.2-REPLAY-001: Next advances cursor and returns current event
func TestReplaySession_Next(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// First next should return event with SeqNum=1
	ev, err := session.Next()
	if err != nil {
		t.Fatalf("Next() failed: %v", err)
	}
	if ev.SeqNum != 1 {
		t.Fatalf("expected SeqNum=1, got %d", ev.SeqNum)
	}

	// Second next should return event with SeqNum=2
	ev, err = session.Next()
	if err != nil {
		t.Fatalf("Next() failed: %v", err)
	}
	if ev.SeqNum != 2 {
		t.Fatalf("expected SeqNum=2, got %d", ev.SeqNum)
	}
}

// 14.2-REPLAY-002: Next returns error at end of events
func TestReplaySession_Next_AtEnd(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 2)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Advance through all events
	session.Next()
	session.Next()

	// Next past the end should return error
	_, err = session.Next()
	if err == nil {
		t.Fatal("expected error at end of events, got nil")
	}
}

// 14.2-REPLAY-003: Next traverses all events in order
func TestReplaySession_Next_FullTraversal(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	for i := 1; i <= 10; i++ {
		ev, err := session.Next()
		if err != nil {
			t.Fatalf("Next() at step %d failed: %v", i, err)
		}
		if ev.SeqNum != uint64(i) {
			t.Fatalf("expected SeqNum=%d, got %d", i, ev.SeqNum)
		}
	}

	// One more should fail
	_, err = session.Next()
	if err == nil {
		t.Fatal("expected error past end, got nil")
	}
}

// --- AC3: Reverse step (prev/p) ---

// 14.2-REPLAY-004: Prev moves cursor backward
func TestReplaySession_Prev(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Advance to event 3
	session.Next() // 1
	session.Next() // 2
	session.Next() // 3

	// Go back to event 2
	ev, err := session.Prev()
	if err != nil {
		t.Fatalf("Prev() failed: %v", err)
	}
	if ev.SeqNum != 2 {
		t.Fatalf("expected SeqNum=2, got %d", ev.SeqNum)
	}
}

// 14.2-REPLAY-005: Prev returns error at beginning
func TestReplaySession_Prev_AtBeginning(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Advance to event 1
	session.Next()

	// Try to go before the beginning
	_, err = session.Prev()
	if err == nil {
		t.Fatal("expected error at beginning, got nil")
	}
}

// 14.2-REPLAY-006: Prev returns error when cursor not started
func TestReplaySession_Prev_NotStarted(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Prev without any Next should error
	_, err = session.Prev()
	if err == nil {
		t.Fatal("expected error when cursor not started, got nil")
	}
}

// --- AC4: Goto specific event (goto seq_num) ---

// 14.2-REPLAY-007: Goto jumps to valid SeqNum
func TestReplaySession_Goto(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	ev, err := session.Goto(5)
	if err != nil {
		t.Fatalf("Goto(5) failed: %v", err)
	}
	if ev.SeqNum != 5 {
		t.Fatalf("expected SeqNum=5, got %d", ev.SeqNum)
	}

	// Verify cursor is at position 5 (can do Next to get 6)
	ev, err = session.Next()
	if err != nil {
		t.Fatalf("Next after Goto failed: %v", err)
	}
	if ev.SeqNum != 6 {
		t.Fatalf("expected SeqNum=6 after Goto(5)+Next, got %d", ev.SeqNum)
	}
}

// 14.2-REPLAY-008: Goto returns error for invalid SeqNum
func TestReplaySession_Goto_InvalidSeqNum(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	_, err = session.Goto(999)
	if err == nil {
		t.Fatal("expected error for invalid SeqNum, got nil")
	}
}

// 14.2-REPLAY-009: Goto to first and last events
func TestReplaySession_Goto_Boundaries(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Jump to first
	ev, err := session.Goto(1)
	if err != nil {
		t.Fatalf("Goto(1) failed: %v", err)
	}
	if ev.SeqNum != 1 {
		t.Fatalf("expected SeqNum=1, got %d", ev.SeqNum)
	}

	// Jump to last
	ev, err = session.Goto(10)
	if err != nil {
		t.Fatalf("Goto(10) failed: %v", err)
	}
	if ev.SeqNum != 10 {
		t.Fatalf("expected SeqNum=10, got %d", ev.SeqNum)
	}
}

// --- AC2/3/4: Current event ---

// 14.2-REPLAY-010: Current returns the event at current cursor position
func TestReplaySession_Current(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Before any navigation, Current should error
	_, err = session.Current()
	if err == nil {
		t.Fatal("expected error for Current before navigation, got nil")
	}

	// After Next, Current should return same event
	ev1, _ := session.Next()
	ev2, err := session.Current()
	if err != nil {
		t.Fatalf("Current() failed: %v", err)
	}
	if ev1.SeqNum != ev2.SeqNum {
		t.Fatalf("Current() SeqNum mismatch: Next=%d, Current=%d", ev1.SeqNum, ev2.SeqNum)
	}
}

// --- AC5: Position tracking ---

// 14.2-REPLAY-011: Position returns correct current and total
func TestReplaySession_Position(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Initial position
	current, total := session.Position()
	if current != -1 {
		t.Fatalf("expected initial current=-1, got %d", current)
	}
	if total != 10 {
		t.Fatalf("expected total=10, got %d", total)
	}

	// After navigation
	session.Next()
	session.Next()
	session.Next()

	current, total = session.Position()
	if current != 2 { // 0-indexed
		t.Fatalf("expected current=2, got %d", current)
	}
	if total != 10 {
		t.Fatalf("expected total=10, got %d", total)
	}
}

// --- AC5: List context window ---

// 14.2-REPLAY-012: List returns events around current position
func TestReplaySession_List(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 20)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Navigate to middle (event 10)
	session.Goto(10)

	items := session.List(5)

	// Should have up to 11 items (5 before + current + 5 after)
	if len(items) == 0 {
		t.Fatal("expected non-empty list")
	}

	// Verify exactly one item is marked as cursor
	cursorCount := 0
	for _, item := range items {
		if item.IsCursor {
			cursorCount++
			if item.Event.SeqNum != 10 {
				t.Fatalf("cursor item SeqNum=%d, expected 10", item.Event.SeqNum)
			}
		}
	}
	if cursorCount != 1 {
		t.Fatalf("expected exactly 1 cursor item, got %d", cursorCount)
	}
}

// 14.2-REPLAY-013: List handles edge at beginning
func TestReplaySession_List_AtBeginning(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 20)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)
	session.Next() // position 0 (SeqNum=1)

	items := session.List(5)

	// At beginning, should not have items before current
	if len(items) == 0 {
		t.Fatal("expected non-empty list")
	}

	// First item should be the cursor (at beginning)
	if !items[0].IsCursor {
		t.Fatal("expected first item to be cursor at beginning")
	}
}

// 14.2-REPLAY-014: List handles edge at end
func TestReplaySession_List_AtEnd(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 20)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)
	session.Goto(20) // last event

	items := session.List(5)

	if len(items) == 0 {
		t.Fatal("expected non-empty list")
	}

	// Last item should be the cursor
	lastItem := items[len(items)-1]
	if !lastItem.IsCursor {
		t.Fatal("expected last item to be cursor at end")
	}
}

// --- Mixed navigation tests ---

// 14.2-REPLAY-015: Next then Prev returns to same position
func TestReplaySession_NextThenPrev(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	// Next to event 1
	ev1, _ := session.Next()
	// Next to event 2
	session.Next()
	// Prev back to event 1
	ev3, err := session.Prev()
	if err != nil {
		t.Fatalf("Prev() failed: %v", err)
	}
	if ev1.SeqNum != ev3.SeqNum {
		t.Fatalf("expected to return to SeqNum=%d, got %d", ev1.SeqNum, ev3.SeqNum)
	}
}

// 14.2-REPLAY-016: Goto then Next works correctly
func TestReplaySession_GotoThenNext(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	session.Goto(5)
	ev, err := session.Next()
	if err != nil {
		t.Fatalf("Next after Goto failed: %v", err)
	}
	if ev.SeqNum != 6 {
		t.Fatalf("expected SeqNum=6 after Goto(5)+Next, got %d", ev.SeqNum)
	}
}

// 14.2-REPLAY-017: Session with empty recording
func TestReplaySession_EmptyRecording(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 0)
	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	session := NewReplaySession(reader)

	_, err = session.Next()
	if err == nil {
		t.Fatal("expected error for Next on empty recording, got nil")
	}

	current, total := session.Position()
	if current != -1 {
		t.Fatalf("expected current=-1, got %d", current)
	}
	if total != 0 {
		t.Fatalf("expected total=0, got %d", total)
	}
}

// 14.2-REPLAY-018: Verify ReplayListItem struct fields
func TestReplayListItem_Fields(t *testing.T) {
	item := ReplayListItem{
		Event: RecordEvent{
			SeqNum:    1,
			Timestamp: 100 * time.Millisecond,
			PID:       42,
			Type:      RecordSyscall,
		},
		IsCursor: true,
	}

	if !item.IsCursor {
		t.Fatal("expected IsCursor=true")
	}
	if item.Event.SeqNum != 1 {
		t.Fatalf("expected SeqNum=1, got %d", item.Event.SeqNum)
	}
}
