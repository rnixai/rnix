package kernel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rnixai/rnix/internal/jsonl"
	"github.com/rnixai/rnix/internal/types"
)

// StepWriter writes StepRecord entries as NDJSON to disk.
// STUB: Created for ATDD red phase — implements structure per AC-2, not yet wired into kernel.
//
// Story 72.2 AC3/F8: also maintains a sidecar offset index (steps.idx) — an
// append-only NDJSON file with short field names that enables O(viewport) reads
// without parsing the full steps.jsonl.
type StepWriter struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex

	// idx fields (Story 72.2)
	idxFile     *os.File
	idxWriter   *bufio.Writer
	jsonlOffset int64 // byte offset of the next jsonl line to be written
}

// NewStepWriter creates a StepWriter that writes to .rnix/data/steps/<uuid>/steps.jsonl.
//
// Story 72.2 F8: also opens (or creates) steps.idx in the same directory and
// initializes jsonlOffset from the current jsonl file size (append mode
// continues from the end).
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

	// Initialize jsonlOffset from current file size (append continues from end).
	jsonlSize, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return nil, err
	}

	// Open idx file (append-only, same crash-consistency semantics as jsonl).
	idxPath := filepath.Join(dir, "steps.idx")
	idxFile, err := os.OpenFile(idxPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		f.Close()
		return nil, err
	}

	sw := &StepWriter{
		file:        f,
		writer:      bufio.NewWriterSize(f, 64*1024),
		idxFile:     idxFile,
		idxWriter:   bufio.NewWriterSize(idxFile, 64*1024),
		jsonlOffset: jsonlSize,
	}

	// Write header if idx is newly created (empty file).
	if idxFileSize(idxPath) == 0 {
		if err := sw.writeIdxHeader(jsonlSize); err != nil {
			idxFile.Close()
			f.Close()
			return nil, err
		}
	}

	return sw, nil
}

// idxFileSize returns the size of the idx file, or 0 on error.
func idxFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// writeIdxHeader writes the idx header line. Called once at idx creation.
func (sw *StepWriter) writeIdxHeader(jsonlSize int64) error {
	// Get jsonl mtime for the header.
	jsonlFi, err := sw.file.Stat()
	if err != nil {
		return err
	}
	header := fmt.Sprintf(`{"v":1,"jsonl_size":%d,"jsonl_mtime_ms":%d}`, jsonlSize, jsonlFi.ModTime().UnixMilli())
	if _, err := sw.idxWriter.WriteString(header + "\n"); err != nil {
		return err
	}
	return sw.idxWriter.Flush()
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
//
// Story 72.2 F8: after jsonl Flush succeeds, appends an idx entry with short
// field names. jsonl Flush failure → no idx write (idx must not run ahead of
// jsonl).
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
	if err := sw.writer.Flush(); err != nil {
		return err
	}

	// jsonl flushed successfully — now write the idx entry (F8).
	offset := sw.jsonlOffset
	sw.jsonlOffset += int64(len(data)) + 1 // +1 for '\n'

	if sw.idxWriter != nil {
		entry := idxEntryFromRecord(rec, offset)
		idxData, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := sw.idxWriter.Write(idxData); err != nil {
			return err
		}
		if err := sw.idxWriter.WriteByte('\n'); err != nil {
			return err
		}
		if err := sw.idxWriter.Flush(); err != nil {
			return err
		}
	}

	return nil
}

// idxEntry is the NDJSON record written to steps.idx (Story 72.2 F1).
// Short field names minimize idx size (~150 B/entry vs ~1.6 MB for a full
// StepRecord with Messages).
type idxEntry struct {
	Offset      int64   `json:"o"`           // byte offset in steps.jsonl
	Step        int     `json:"s"`           // step number
	Action      string  `json:"a"`           // action type
	Summary     string  `json:"m"`           // summary (stored verbatim, not truncated)
	ToolPath    string  `json:"t,omitempty"` // tool path
	HasError    bool    `json:"e"`           // has tool error
	DurationMs  float64 `json:"d"`           // tool duration in ms
	TokenCount  int     `json:"k"`           // token count
	TimestampMs int64   `json:"ts"`          // timestamp in ms from process start
}

// idxEntryFromRecord builds an idxEntry from a (already truncated) StepRecord.
// has_error logic is equivalent to ipc/server_observe.go hasToolCallError but
// computed inline in kernel (no cross-package call).
func idxEntryFromRecord(rec types.StepRecord, offset int64) idxEntry {
	hasErr := rec.ToolError != ""
	if !hasErr {
		for i := range rec.ToolCalls {
			if rec.ToolCalls[i].Error != "" {
				hasErr = true
				break
			}
		}
	}
	return idxEntry{
		Offset:      offset,
		Step:        rec.Step,
		Action:      rec.Action,
		Summary:     rec.Summary,
		ToolPath:    rec.ToolPath,
		HasError:    hasErr,
		DurationMs:  float64(rec.ToolDuration.Microseconds()) / 1000.0,
		TokenCount:  rec.TokenCount,
		TimestampMs: rec.Timestamp.Milliseconds(),
	}
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
	// 🔴 Input fields get a 4 MB bound, NOT the 64 KB used for results — two
	// independent reasons, both load-bearing (code-review 72.1, Decker 裁决
	// 2026-08-02):
	//
	//  1. Structural, not textual. ToolResult is free text: truncating it loses
	//     tail content but the prefix stays usable. ToolInput / ToolCalls[].Input
	//     hold the tool-argument JSON string (internal/types/step_record.go),
	//     and a truncated JSON document is not JSON — cmd/rnix/agtest_import.go
	//     decodeToolInput then fails and silently inlines the raw string, making
	//     the fixture unreplayable. The damage is unrecoverable at write time.
	//  2. steps.jsonl is the fidelity layer. kernel/observe.go bounds the
	//     events.jsonl aggregate event's input at 64 KB precisely because
	//     "steps.jsonl remains the fidelity layer for the full input" (Story
	//     65.1 裁决 1/2/4). Capping both sides at 64 KB would have deleted that
	//     last full copy — the two would truncate at the same offset and the
	//     comment's promise would fail exactly when it is needed.
	//
	// Same 4 MB quantum as RawResponse below (defaultRawCaptureMaxOutputBytes),
	// so the write-side bound never binds before raw.jsonl's own read ceiling.
	// Observed max ToolInput across 400 stored steps.jsonl files is 13,093 B, so
	// this bound — like RawResponse's — is purely defensive.
	rec.ToolInput = truncateToBytes(rec.ToolInput, int(defaultRawCaptureMaxOutputBytes))
	// RawResponse is raw LLM output shown verbatim in the dashboard inspector;
	// its observed max (96 KB) already exceeds 64 KB, so a 64 KB cap would
	// truncate live data. The 4 MB bound is purely defensive.
	rec.RawResponse = truncateToBytes(rec.RawResponse, int(defaultRawCaptureMaxOutputBytes))

	if len(rec.ToolCalls) > 0 {
		calls := slices.Clone(rec.ToolCalls)
		for i := range calls {
			calls[i].Result = truncateToBytes(calls[i].Result, maxDriverToolResultBytes)
			// Structured JSON — see the ToolInput rationale above.
			calls[i].Input = truncateToBytes(calls[i].Input, int(defaultRawCaptureMaxOutputBytes))
		}
		rec.ToolCalls = calls
	}
	return rec
}

// Close flushes and closes the underlying file.
//
// Story 72.2: also closes the idx writer/file.
func (sw *StepWriter) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if err := sw.writer.Flush(); err != nil {
		return err
	}
	if sw.idxWriter != nil {
		if err := sw.idxWriter.Flush(); err != nil {
			return err
		}
	}
	if sw.idxFile != nil {
		if err := sw.idxFile.Close(); err != nil {
			return err
		}
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
