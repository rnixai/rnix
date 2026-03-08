package debug

import "fmt"

// ReplaySession provides navigation over a recorded execution.
type ReplaySession struct {
	reader *RecordReader
	cursor int // current position (0-based index into events), -1 means not started
}

// ReplayListItem represents a single item in a replay list display.
type ReplayListItem struct {
	Event    RecordEvent
	IsCursor bool
}

// NewReplaySession creates a new replay session from a RecordReader.
// The cursor starts at -1 (not yet started).
func NewReplaySession(reader *RecordReader) *ReplaySession {
	return &ReplaySession{
		reader: reader,
		cursor: -1,
	}
}

// Next advances the cursor forward by one and returns the event at the new position.
func (s *ReplaySession) Next() (*RecordEvent, error) {
	events := s.reader.Events()
	if s.cursor+1 >= len(events) {
		return nil, fmt.Errorf("replay: already at end of recording")
	}
	s.cursor++
	ev := events[s.cursor]
	return &ev, nil
}

// Prev moves the cursor backward by one and returns the event at the new position.
func (s *ReplaySession) Prev() (*RecordEvent, error) {
	if s.cursor <= 0 {
		return nil, fmt.Errorf("replay: already at beginning of recording")
	}
	s.cursor--
	events := s.reader.Events()
	ev := events[s.cursor]
	return &ev, nil
}

// Goto jumps the cursor to the event with the specified SeqNum.
func (s *ReplaySession) Goto(seqNum uint64) (*RecordEvent, error) {
	events := s.reader.Events()
	for i, ev := range events {
		if ev.SeqNum == seqNum {
			s.cursor = i
			return &events[i], nil
		}
	}
	return nil, fmt.Errorf("replay: event with SeqNum %d not found", seqNum)
}

// Current returns the event at the current cursor position.
func (s *ReplaySession) Current() (*RecordEvent, error) {
	if s.cursor < 0 {
		return nil, fmt.Errorf("replay: no current event (not started)")
	}
	events := s.reader.Events()
	if s.cursor >= len(events) {
		return nil, fmt.Errorf("replay: cursor past end of recording")
	}
	ev := events[s.cursor]
	return &ev, nil
}

// Position returns the current cursor position (0-based index) and total event count.
func (s *ReplaySession) Position() (current int, total int) {
	return s.cursor, s.reader.EventCount()
}

// Diff computes the context difference between two SeqNums.
// It finds the nearest context_snapshot at or before each SeqNum and computes the diff.
func (s *ReplaySession) Diff(seq1, seq2 uint64) (*ContextDiff, error) {
	finder := NewSnapshotFinder(s.reader.Events())
	if !finder.HasSnapshots() {
		return nil, fmt.Errorf("no context snapshots available in this recording")
	}

	fromEv, err := finder.FindNearestBefore(seq1)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	toEv, err := finder.FindNearestBefore(seq2)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}

	if fromEv.Context == nil {
		return nil, fmt.Errorf("diff: context snapshot #%d has no context data", fromEv.SeqNum)
	}
	if toEv.Context == nil {
		return nil, fmt.Errorf("diff: context snapshot #%d has no context data", toEv.SeqNum)
	}

	return ComputeContextDiff(fromEv.Context, toEv.Context, fromEv, toEv), nil
}

// DiffFromCursor computes the context difference between the cursor position and a target SeqNum.
// Uses the nearest context_snapshot before the cursor position and the nearest before the target.
func (s *ReplaySession) DiffFromCursor(seq uint64) (*ContextDiff, error) {
	if s.cursor < 0 {
		return nil, fmt.Errorf("replay: no current event (not started)")
	}

	events := s.reader.Events()
	cursorSeqNum := events[s.cursor].SeqNum

	return s.Diff(cursorSeqNum, seq)
}

// Fork creates a ForkContext from the current cursor position.
// Returns an error if the cursor has not been started (cursor == -1).
// Does not change the cursor position.
func (s *ReplaySession) Fork() (*ForkContext, error) {
	if s.cursor < 0 {
		return nil, fmt.Errorf("replay: no current event (not started)")
	}

	events := s.reader.Events()
	seqNum := events[s.cursor].SeqNum

	restorer := NewSnapshotRestorer(s.reader)
	return restorer.RestoreContext(seqNum)
}

// ForkAt creates a ForkContext at the specified SeqNum without changing the cursor.
// The cursor remains at its current position after the call.
func (s *ReplaySession) ForkAt(seqNum uint64) (*ForkContext, error) {
	// Verify the seqNum exists
	if _, err := s.reader.Event(seqNum); err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}

	restorer := NewSnapshotRestorer(s.reader)
	return restorer.RestoreContext(seqNum)
}

// List returns a window of events around the current cursor position.
// The context parameter controls how many events before and after the cursor to include.
func (s *ReplaySession) List(context int) []ReplayListItem {
	events := s.reader.Events()
	if len(events) == 0 || s.cursor < 0 {
		return nil
	}

	start := s.cursor - context
	if start < 0 {
		start = 0
	}
	end := s.cursor + context
	if end >= len(events) {
		end = len(events) - 1
	}

	items := make([]ReplayListItem, 0, end-start+1)
	for i := start; i <= end; i++ {
		items = append(items, ReplayListItem{
			Event:    events[i],
			IsCursor: i == s.cursor,
		})
	}
	return items
}
