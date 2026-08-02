package kernel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/jsonl"
	"github.com/rnixai/rnix/internal/types"
)

// SyscallEventDisk is the JSON-serializable representation of a SyscallEvent on disk.
type SyscallEventDisk struct {
	TimestampMs float64        `json:"ts_ms"`
	PID         uint64         `json:"pid"`
	Syscall     string         `json:"syscall"`
	Args        map[string]any `json:"args,omitempty"`
	Result      any            `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMs  float64        `json:"dur_ms"`
	TraceID     string         `json:"trace_id,omitempty"`
	SpanID      string         `json:"span_id,omitempty"`
}

func syscallEventToDisk(ev types.SyscallEvent) SyscallEventDisk {
	d := SyscallEventDisk{
		TimestampMs: float64(ev.Timestamp) / float64(time.Millisecond),
		PID:         uint64(ev.PID),
		Syscall:     ev.Syscall,
		Args:        ev.Args,
		Result:      ev.Result,
		DurationMs:  float64(ev.Duration) / float64(time.Millisecond),
		TraceID:     string(ev.TraceID),
		SpanID:      string(ev.SpanID),
	}
	if ev.Err != nil {
		d.Error = ev.Err.Error()
	}
	return d
}

// EventWriter writes SyscallEvent entries as NDJSON to disk.
type EventWriter struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewEventWriter creates an EventWriter that writes to .rnix/data/steps/<uuid>/events.jsonl.
func NewEventWriter(baseDir string, procUUID string) (*EventWriter, error) {
	dir := filepath.Join(baseDir, "steps", procUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &EventWriter{
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024),
	}, nil
}

// WriteEvent marshals and appends a SyscallEvent as a single NDJSON line.
func (ew *EventWriter) WriteEvent(ev types.SyscallEvent) error {
	ew.mu.Lock()
	defer ew.mu.Unlock()

	data, err := json.Marshal(syscallEventToDisk(ev))
	if err != nil {
		return err
	}
	if _, err := ew.writer.Write(data); err != nil {
		return err
	}
	if err := ew.writer.WriteByte('\n'); err != nil {
		return err
	}
	// Flush immediately so events are available on disk for historical reads
	// (e.g., debug panel reload on PID switch). The 64KB buffer provides no
	// benefit for NDJSON line-by-line writes at syscall frequency.
	return ew.writer.Flush()
}

// Flush flushes the buffered writer to disk.
func (ew *EventWriter) Flush() error {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	return ew.writer.Flush()
}

// Close flushes and closes the underlying file.
func (ew *EventWriter) Close() error {
	ew.mu.Lock()
	defer ew.mu.Unlock()

	if err := ew.writer.Flush(); err != nil {
		return err
	}
	return ew.file.Close()
}

// ReadAllEvents reads all syscall events from an events.jsonl file.
//
// Thin wrapper over ReadAllEventsWithErrors — existing callers keep the 2-value
// signature; the parse-error count is discarded here (Story 72.1 AC4).
func ReadAllEvents(path string) ([]SyscallEventDisk, error) {
	events, _, err := ReadAllEventsWithErrors(path)
	return events, err
}

// ReadAllEventsWithErrors is ReadAllEvents plus a count of lines that failed to
// unmarshal (Story 72.1 AC4). Blank lines are skipped without counting.
func ReadAllEventsWithErrors(path string) ([]SyscallEventDisk, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var events []SyscallEventDisk
	parseErrors := 0
	scanErr := jsonl.Scan(f, path, func(line []byte) error {
		var ev SyscallEventDisk
		if err := json.Unmarshal(line, &ev); err != nil {
			parseErrors++
			return nil
		}
		events = append(events, ev)
		return nil
	})
	if scanErr != nil {
		return nil, parseErrors, scanErr
	}
	return events, parseErrors, nil
}
