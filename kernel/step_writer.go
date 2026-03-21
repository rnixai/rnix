package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// StepWriter writes StepRecord entries as NDJSON to disk.
// STUB: Created for ATDD red phase — implements structure per AC-2, not yet wired into kernel.
type StepWriter struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewStepWriter creates a StepWriter that writes to .rnix/data/steps/<pid>/steps.jsonl.
func NewStepWriter(baseDir string, pid types.PID) (*StepWriter, error) {
	dir := filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "steps.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &StepWriter{
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024),
	}, nil
}

// WriteStep marshals and appends a StepRecord as a single NDJSON line.
func (sw *StepWriter) WriteStep(rec types.StepRecord) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := sw.writer.Write(data); err != nil {
		return err
	}
	if err := sw.writer.WriteByte('\n'); err != nil {
		return err
	}
	return sw.writer.Flush()
}

// Close flushes and closes the underlying file.
func (sw *StepWriter) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if err := sw.writer.Flush(); err != nil {
		return err
	}
	return sw.file.Close()
}

// ReadStep reads a specific step from a steps.jsonl file by sequential scan.
func ReadStep(path string, targetStep int) (*types.StepRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		var rec types.StepRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Step == targetStep {
			return &rec, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("step %d not found", targetStep)
}
