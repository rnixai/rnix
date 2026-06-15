package kernel

import (
	"bufio"
	"os"
	"sync"

	"github.com/rnixai/rnix/vfs"
)

// RawWriter appends vfs.RawCapture records as NDJSON to
// <baseDir>/steps/<uuid>/raw.jsonl (Story 56.1 AC#4). Mirrors EventWriter
// (kernel/event_writer.go) — bufio + mu + O_APPEND, one Flush per write so
// historical reads observe records immediately.
//
// 56.1 RED skeleton: struct + method shells exist so dev-story can fill the
// bodies without rewiring callers. All assertions in atdd_56_1_raw_writer_test.go
// are guarded by t.Skip("RED") until dev-story removes the skip.
//
//nolint:unused // field shells — dev-story (WriteRaw/Close/Flush) wires them
type RawWriter struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex
	path   string
}

// NewRawWriter creates a RawWriter pointing at <baseDir>/steps/<procUUID>/raw.jsonl.
// 56.1 RED skeleton: returns a zero-value RawWriter so the constructor compiles.
func NewRawWriter(baseDir string, procUUID string) (*RawWriter, error) {
	_ = baseDir
	_ = procUUID
	return &RawWriter{}, nil
}

// WriteRaw appends a single RawCapture as NDJSON.
// 56.1 RED skeleton: no-op.
func (rw *RawWriter) WriteRaw(rec vfs.RawCapture) error {
	_ = rec
	return nil
}

// Flush flushes buffered bytes to disk.
// 56.1 RED skeleton: no-op.
func (rw *RawWriter) Flush() error { return nil }

// Close flushes and closes the underlying file. Idempotent.
// 56.1 RED skeleton: no-op.
func (rw *RawWriter) Close() error { return nil }

// Path returns the on-disk file path for this writer.
// 56.1 RED skeleton: returns the captured path (empty in skeleton).
func (rw *RawWriter) Path() string { return rw.path }

// ReadAllRaw scans a raw.jsonl file and returns all records in order.
// Provided for 56.4 query path consumers.
//
// 56.1 RED skeleton: returns nil so dev-story can implement the scan.
func ReadAllRaw(path string) ([]vfs.RawCapture, error) {
	_ = path
	return nil, nil
}

// ReadRawForStep returns the RawCapture record whose Step matches the given
// step number, or nil if no such record exists.
//
// 56.1 RED skeleton: returns nil.
func ReadRawForStep(path string, step int) (*vfs.RawCapture, error) {
	_ = path
	_ = step
	return nil, nil
}
