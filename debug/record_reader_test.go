package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// RecordReader Tests — Story 14-2 ATDD (RED PHASE)
// All tests reference RecordReader which does not yet exist.
// Compile errors are expected until implementation.
// =============================================================================

// --- AC1: Load valid recording (completed status) ---

// 14.2-RDR-001: NewRecordReader loads a completed recording successfully
func TestRecordReader_LoadCompleted(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	meta := reader.Metadata()
	if meta.Status != RecordStatusCompleted {
		t.Fatalf("expected status=%q, got %q", RecordStatusCompleted, meta.Status)
	}
	if meta.RecordID == "" {
		t.Fatal("expected non-empty RecordID")
	}
}

// 14.2-RDR-002: NewRecordReader loads a stopped recording successfully
func TestRecordReader_LoadStopped(t *testing.T) {
	dir := createTestRecording(t, RecordStatusStopped, 3)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	meta := reader.Metadata()
	if meta.Status != RecordStatusStopped {
		t.Fatalf("expected status=%q, got %q", RecordStatusStopped, meta.Status)
	}
}

// 14.2-RDR-003: NewRecordReader rejects recording with "recording" status
func TestRecordReader_RejectRecordingStatus(t *testing.T) {
	dir := createTestRecording(t, RecordStatusRecording, 2)

	_, err := NewRecordReader(dir)
	if err == nil {
		t.Fatal("expected error for recording status, got nil")
	}
}

// 14.2-RDR-004: NewRecordReader handles empty events.jsonl
func TestRecordReader_EmptyEvents(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 0)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	if reader.EventCount() != 0 {
		t.Fatalf("expected EventCount=0, got %d", reader.EventCount())
	}

	events := reader.Events()
	if len(events) != 0 {
		t.Fatalf("expected empty events slice, got %d events", len(events))
	}
}

// 14.2-RDR-005: NewRecordReader skips corrupted JSON lines
func TestRecordReader_CorruptedJSONLines(t *testing.T) {
	dir := t.TempDir()

	// Write metadata
	meta := RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "test",
		StartTime:  time.Now(),
		EventCount: 3,
		Status:     RecordStatusCompleted,
	}
	writeTestMetadata(t, dir, meta)

	// Write events with one corrupted line
	eventsPath := filepath.Join(dir, "events.jsonl")
	validEvent := RecordEvent{
		SeqNum:    1,
		Timestamp: 100 * time.Millisecond,
		PID:       42,
		Type:      RecordSyscall,
		Syscall:   &SyscallEventData{Syscall: "Open", Duration: time.Millisecond},
	}
	validJSON, _ := json.Marshal(validEvent)

	content := string(validJSON) + "\n" +
		"THIS IS INVALID JSON\n" +
		string(validJSON) + "\n"
	os.WriteFile(eventsPath, []byte(content), 0644)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	// Should have loaded 2 valid events (skipped the corrupted one)
	if reader.EventCount() != 2 {
		t.Fatalf("expected EventCount=2 (skipped corrupted), got %d", reader.EventCount())
	}
}

// 14.2-RDR-006: EventCount returns correct count
func TestRecordReader_EventCount(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	if reader.EventCount() != 10 {
		t.Fatalf("expected EventCount=10, got %d", reader.EventCount())
	}
}

// 14.2-RDR-007: Event(seqNum) finds a specific event by SeqNum
func TestRecordReader_EventBySeqNum(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	ev, err := reader.Event(3)
	if err != nil {
		t.Fatalf("Event(3) failed: %v", err)
	}
	if ev.SeqNum != 3 {
		t.Fatalf("expected SeqNum=3, got %d", ev.SeqNum)
	}
}

// 14.2-RDR-008: Event(seqNum) returns error for non-existent SeqNum
func TestRecordReader_EventBySeqNum_NotFound(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	_, err = reader.Event(999)
	if err == nil {
		t.Fatal("expected error for non-existent SeqNum, got nil")
	}
}

// 14.2-RDR-009: Events returns the complete event list
func TestRecordReader_Events(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 7)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	events := reader.Events()
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(events))
	}

	// Verify events are sorted by SeqNum
	for i := 1; i < len(events); i++ {
		if events[i].SeqNum <= events[i-1].SeqNum {
			t.Fatalf("events not sorted: SeqNum[%d]=%d <= SeqNum[%d]=%d",
				i, events[i].SeqNum, i-1, events[i-1].SeqNum)
		}
	}
}

// 14.2-RDR-010: EventsInRange returns events within SeqNum range
func TestRecordReader_EventsInRange(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 10)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	events := reader.EventsInRange(3, 7)
	if len(events) != 5 {
		t.Fatalf("expected 5 events in range [3,7], got %d", len(events))
	}

	for _, ev := range events {
		if ev.SeqNum < 3 || ev.SeqNum > 7 {
			t.Fatalf("event SeqNum %d outside range [3,7]", ev.SeqNum)
		}
	}
}

// 14.2-RDR-011: Metadata returns correct recording metadata
func TestRecordReader_Metadata(t *testing.T) {
	dir := createTestRecording(t, RecordStatusCompleted, 5)

	reader, err := NewRecordReader(dir)
	if err != nil {
		t.Fatalf("NewRecordReader failed: %v", err)
	}

	meta := reader.Metadata()
	if meta.PID != 42 {
		t.Fatalf("expected PID=42, got %d", meta.PID)
	}
	if meta.Intent != "test intent" {
		t.Fatalf("expected Intent='test intent', got %q", meta.Intent)
	}
}

// 14.2-RDR-012: NewRecordReader returns error for missing metadata.json
func TestRecordReader_MissingMetadata(t *testing.T) {
	dir := t.TempDir()
	// No metadata.json, just an empty directory

	_, err := NewRecordReader(dir)
	if err == nil {
		t.Fatal("expected error for missing metadata.json, got nil")
	}
}

// 14.2-RDR-013: NewRecordReader returns error for missing events.jsonl
func TestRecordReader_MissingEvents(t *testing.T) {
	dir := t.TempDir()
	meta := RecordMetadata{
		RecordID: "42-1709856000",
		PID:      42,
		Intent:   "test",
		Status:   RecordStatusCompleted,
	}
	writeTestMetadata(t, dir, meta)
	// No events.jsonl

	_, err := NewRecordReader(dir)
	if err == nil {
		t.Fatal("expected error for missing events.jsonl, got nil")
	}
}

// =============================================================================
// Test Helpers — create test recording fixtures
// =============================================================================

// createTestRecording creates a temporary recording directory with metadata and events.
func createTestRecording(t *testing.T, status RecordStatus, eventCount int) string {
	t.Helper()
	dir := t.TempDir()

	meta := RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        types.PID(42),
		Intent:     "test intent",
		StartTime:  time.Now().Add(-time.Minute),
		EndTime:    time.Now(),
		EventCount: uint64(eventCount),
		Status:     status,
	}
	writeTestMetadata(t, dir, meta)

	// Write events.jsonl
	eventsPath := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("create events.jsonl: %v", err)
	}
	defer f.Close()

	for i := 1; i <= eventCount; i++ {
		ev := RecordEvent{
			SeqNum:    uint64(i),
			Timestamp: time.Duration(i*100) * time.Millisecond,
			PID:       42,
			Type:      RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall:  "TestCall",
				Args:     map[string]any{"seq": i},
				Duration: time.Duration(i) * time.Millisecond,
			},
		}
		data, _ := json.Marshal(ev)
		f.Write(data)
		f.Write([]byte("\n"))
	}

	return dir
}

// writeTestMetadata writes a metadata.json file to the given directory.
func writeTestMetadata(t *testing.T, dir string, meta RecordMetadata) {
	t.Helper()
	metaPath := filepath.Join(dir, "metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
}
