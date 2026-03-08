package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RecordReader reads and provides access to a completed recording's data.
type RecordReader struct {
	dir      string
	metadata RecordMetadata
	events   []RecordEvent
}

// NewRecordReader loads a recording from the given directory.
// Returns an error if the recording is still in "recording" status.
func NewRecordReader(recordDir string) (*RecordReader, error) {
	// Read metadata.json
	metaPath := filepath.Join(recordDir, "metadata.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("record_reader: read metadata: %w", err)
	}

	var meta RecordMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("record_reader: parse metadata: %w", err)
	}

	if meta.Status == RecordStatusRecording {
		return nil, fmt.Errorf("record_reader: cannot load recording with status %q (still recording)", meta.Status)
	}

	// Read events.jsonl
	eventsPath := filepath.Join(recordDir, "events.jsonl")
	eventsFile, err := os.Open(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("record_reader: open events: %w", err)
	}
	defer eventsFile.Close()

	var events []RecordEvent
	scanner := bufio.NewScanner(eventsFile)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RecordEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Skip corrupted JSON lines
			continue
		}
		events = append(events, ev)
	}

	// Sort by SeqNum as a safety guarantee
	sort.Slice(events, func(i, j int) bool {
		return events[i].SeqNum < events[j].SeqNum
	})

	return &RecordReader{
		dir:      recordDir,
		metadata: meta,
		events:   events,
	}, nil
}

// Metadata returns the recording metadata.
func (r *RecordReader) Metadata() RecordMetadata {
	return r.metadata
}

// EventCount returns the number of events in the recording.
func (r *RecordReader) EventCount() int {
	return len(r.events)
}

// Event finds a single event by SeqNum.
func (r *RecordReader) Event(seqNum uint64) (*RecordEvent, error) {
	for i := range r.events {
		if r.events[i].SeqNum == seqNum {
			return &r.events[i], nil
		}
	}
	return nil, fmt.Errorf("record_reader: event with SeqNum %d not found", seqNum)
}

// Events returns the complete event list.
func (r *RecordReader) Events() []RecordEvent {
	return r.events
}

// EventsInRange returns events with SeqNum in [from, to] inclusive.
func (r *RecordReader) EventsInRange(from, to uint64) []RecordEvent {
	var result []RecordEvent
	for _, ev := range r.events {
		if ev.SeqNum >= from && ev.SeqNum <= to {
			result = append(result, ev)
		}
	}
	return result
}
