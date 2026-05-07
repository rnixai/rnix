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

// NewStepWriter creates a StepWriter that writes to .rnix/data/steps/<uuid>/steps.jsonl.
func NewStepWriter(baseDir string, procUUID string) (*StepWriter, error) {
	dir := filepath.Join(baseDir, "data", "steps", procUUID)
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
// Returns the LAST record with the given step number, since each step may be
// written multiple times (once per tool call within the step), and the last
// write contains the most complete context.
func ReadStep(path string, targetStep int) (*types.StepRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last *types.StepRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		var rec types.StepRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Step == targetStep {
			copy := rec
			last = &copy
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if last == nil {
		return nil, fmt.Errorf("step %d not found", targetStep)
	}
	return last, nil
}

// ReadAllSteps reads all step records from a steps.jsonl file.
// If afterStep > 0, only records with Step > afterStep are returned.
// Returns the matching records and the total count of all records in the file.
//
// Each step number may appear multiple times (one write per tool call within a
// reasoning step). ReadAllSteps deduplicates by keeping the LAST record for
// each step number, which contains the most complete context. The returned
// slice preserves original step order.
func ReadAllSteps(path string, afterStep int) ([]types.StepRecord, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	// last maps step number → the most recent record seen for that step.
	// order preserves the first-seen order of each step number.
	last := make(map[int]types.StepRecord)
	var order []int
	total := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var rec types.StepRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		total++
		if _, seen := last[rec.Step]; !seen {
			order = append(order, rec.Step)
		}
		last[rec.Step] = rec
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	var all []types.StepRecord
	for _, stepNum := range order {
		if afterStep > 0 && stepNum <= afterStep {
			continue
		}
		all = append(all, last[stepNum])
	}
	return all, total, nil
}
