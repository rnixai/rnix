package kernel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rnixai/rnix/internal/jsonl"
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
	dir := filepath.Join(baseDir, "steps", procUUID)
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
//
// Story 72.1 AC5: before marshaling, oversized string fields are truncated to
// defensive upper bounds. This is NOT what makes line length bounded — 98.6% of
// a line's bytes live in Messages (the resume data source), which is excluded
// here on purpose; true line bounding is Story 72.3. This AC only (a) caps the
// non-Messages fields defensively and (b) removes the existing asymmetry where
// the CLI-driver write path already truncated tool results to 64 KB but the API
// driver path did not.
func (sw *StepWriter) WriteStep(rec types.StepRecord) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	rec = truncateStepRecordForWrite(rec)

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

// truncateStepRecordForWrite returns a copy of rec with oversized string fields
// capped. Messages is deliberately left untouched (resume rebuilds context from
// it).
//
// 🔴 rec is received by value, but ToolCalls is a slice header sharing the
// caller's backing array — mutating rec.ToolCalls[i] would write through into
// the caller's data (the same never-mutated red line as context/context.go).
// Clone it first.
func truncateStepRecordForWrite(rec types.StepRecord) types.StepRecord {
	rec.ToolResult = truncateToBytes(rec.ToolResult, maxDriverToolResultBytes)
	rec.ToolInput = truncateToBytes(rec.ToolInput, maxDriverToolResultBytes)
	// RawResponse is raw LLM output shown verbatim in the dashboard inspector;
	// its observed max (96 KB) already exceeds 64 KB, so a 64 KB cap would
	// truncate live data. The 4 MB bound is purely defensive.
	rec.RawResponse = truncateToBytes(rec.RawResponse, int(defaultRawCaptureMaxOutputBytes))

	if len(rec.ToolCalls) > 0 {
		calls := slices.Clone(rec.ToolCalls)
		for i := range calls {
			calls[i].Result = truncateToBytes(calls[i].Result, maxDriverToolResultBytes)
			calls[i].Input = truncateToBytes(calls[i].Input, maxDriverToolResultBytes)
		}
		rec.ToolCalls = calls
	}
	return rec
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

// ErrStepNotFound is returned by ReadStep when the file was read successfully
// but contains no record for the requested step number.
//
// Story 72.1 AC3: callers (ipc.handleGetStepDetail) must distinguish "this step
// does not exist" from "reading the file failed". Without a sentinel, an I/O
// error was reported to the user as "step N not yet recorded" — a lie.
var ErrStepNotFound = errors.New("step not found")

// ReadStep reads a specific step from a steps.jsonl file by sequential scan.
// Returns the LAST record with the given step number, since each step may be
// written multiple times (once per tool call within the step), and the last
// write contains the most complete context.
//
// Returns an error wrapping ErrStepNotFound when the step is absent.
func ReadStep(path string, targetStep int) (*types.StepRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last *types.StepRecord
	scanErr := jsonl.Scan(f, path, func(line []byte) error {
		var rec types.StepRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil // best-effort: skip malformed lines
		}
		if rec.Step == targetStep {
			copy := rec
			last = &copy
		}
		return nil
	})
	if scanErr != nil {
		return nil, scanErr
	}
	if last == nil {
		return nil, fmt.Errorf("step %d: %w", targetStep, ErrStepNotFound)
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
//
// Thin wrapper over ReadAllStepsWithErrors — existing callers keep the 3-value
// signature; the parse-error count is discarded here (Story 72.1 AC4, mirroring
// ReadAllRaw/ReadAllRawWithErrors).
func ReadAllSteps(path string, afterStep int) ([]types.StepRecord, int, error) {
	records, total, _, err := ReadAllStepsWithErrors(path, afterStep)
	return records, total, err
}

// ReadAllStepsWithErrors is ReadAllSteps plus a count of lines that failed to
// unmarshal (Story 72.1 AC4). The malformed count makes "silently skipped"
// lines observable to IPC consumers instead of being swallowed.
//
// Blank lines are skipped without counting as parse errors — a trailing
// newline is a normal artifact of append-mode writing.
func ReadAllStepsWithErrors(path string, afterStep int) ([]types.StepRecord, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	// last maps step number → the most recent record seen for that step.
	// order preserves the first-seen order of each step number.
	last := make(map[int]types.StepRecord)
	var order []int
	total := 0
	parseErrors := 0
	scanErr := jsonl.Scan(f, path, func(line []byte) error {
		var rec types.StepRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			parseErrors++
			return nil
		}
		total++
		if _, seen := last[rec.Step]; !seen {
			order = append(order, rec.Step)
		}
		last[rec.Step] = rec
		return nil
	})
	if scanErr != nil {
		return nil, 0, parseErrors, scanErr
	}

	var all []types.StepRecord
	for _, stepNum := range order {
		if afterStep > 0 && stepNum <= afterStep {
			continue
		}
		all = append(all, last[stepNum])
	}
	return all, total, parseErrors, nil
}
