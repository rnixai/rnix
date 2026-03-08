package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Recorder writes recording events to a JSONL file on disk.
type Recorder struct {
	dir      string
	file     *os.File
	writer   *bufio.Writer
	metadata RecordMetadata
	seqNum   atomic.Uint64
	mu       sync.Mutex
	closed   atomic.Bool
}

// NewRecorder creates a new Recorder that writes events to baseDir/<pid>-<timestamp>/.
func NewRecorder(baseDir string, pid types.PID, intent string) (*Recorder, error) {
	recordID := fmt.Sprintf("%d-%d", pid, time.Now().Unix())
	dir := filepath.Join(baseDir, recordID)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("record: create dir %s: %w", dir, err)
	}

	eventsPath := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("record: create events file: %w", err)
	}

	metadata := RecordMetadata{
		RecordID:  recordID,
		PID:       pid,
		Intent:    intent,
		StartTime: time.Now(),
		Status:    RecordStatusRecording,
	}

	r := &Recorder{
		dir:      dir,
		file:     f,
		writer:   bufio.NewWriterSize(f, 64*1024),
		metadata: metadata,
	}

	if err := r.writeMetadata(); err != nil {
		f.Close()
		return nil, fmt.Errorf("record: write metadata: %w", err)
	}

	return r, nil
}

// WriteEvent writes a single RecordEvent to the JSONL file.
func (r *Recorder) WriteEvent(event RecordEvent) error {
	if r.closed.Load() {
		return fmt.Errorf("record: recorder is closed")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Assign SeqNum under lock to guarantee JSONL order matches sequence order
	r.seqNum.Add(1)
	event.SeqNum = r.seqNum.Load()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("record: marshal event: %w", err)
	}

	if _, err := r.writer.Write(data); err != nil {
		return fmt.Errorf("record: write event: %w", err)
	}
	if err := r.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("record: write newline: %w", err)
	}

	r.metadata.EventCount++

	// Flush every 100 events
	if r.metadata.EventCount%100 == 0 {
		if err := r.writer.Flush(); err != nil {
			log.Printf("[record] flush error: %v", err)
		}
	}

	return nil
}

// Close flushes and closes the recorder with status "completed".
func (r *Recorder) Close() error {
	return r.finish(RecordStatusCompleted)
}

// Stop flushes and closes the recorder with status "stopped" (user-initiated stop).
func (r *Recorder) Stop() error {
	return r.finish(RecordStatusStopped)
}

// RecordID returns the recording session's unique ID.
func (r *Recorder) RecordID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metadata.RecordID
}

// EventCount returns the number of events written so far.
func (r *Recorder) EventCount() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metadata.EventCount
}

func (r *Recorder) finish(status RecordStatus) error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil // already closed
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.metadata.Status = status
	r.metadata.EndTime = time.Now()

	if err := r.writer.Flush(); err != nil {
		return fmt.Errorf("record: flush: %w", err)
	}

	if err := r.file.Close(); err != nil {
		return fmt.Errorf("record: close file: %w", err)
	}

	return r.writeMetadataLocked()
}

func (r *Recorder) writeMetadata() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeMetadataLocked()
}

func (r *Recorder) writeMetadataLocked() error {
	metaPath := filepath.Join(r.dir, "metadata.json")
	data, err := json.MarshalIndent(r.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("record: marshal metadata: %w", err)
	}
	return os.WriteFile(metaPath, data, 0644)
}
