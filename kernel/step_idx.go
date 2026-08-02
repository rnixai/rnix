package kernel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rnixai/rnix/internal/jsonl"
	"github.com/rnixai/rnix/internal/types"
)

// ErrIdxUnavailable is returned when the idx file is missing, corrupt, or
// inconsistent with the jsonl file. Callers should fall back to a full scan
// of steps.jsonl (ReadAllStepsWithErrors).
var ErrIdxUnavailable = errors.New("idx unavailable")

// StepIdxEntry holds one parsed line from steps.idx (Story 72.2 AC3).
// It carries the byte offset into steps.jsonl plus the scalar fields needed
// to build a StepSummaryWire without parsing the full StepRecord.
type StepIdxEntry struct {
	Offset      int64   // byte offset in steps.jsonl
	Step        int     // step number
	Action      string  // action type
	Summary     string  // summary (verbatim, not truncated)
	ToolPath    string  // tool path
	HasError    bool    // has tool error
	DurationMs  float64 // tool duration in ms
	TokenCount  int     // token count
	TimestampMs int64   // timestamp in ms from process start
}

// idxHeader is the first line of steps.idx.
type idxHeader struct {
	V         int   `json:"v"`
	JSONLSize int64 `json:"jsonl_size"`
	MtimeMs   int64 `json:"jsonl_mtime_ms"`
}

// ReadStepsFromIdx reads step summaries from the sidecar idx file.
// Returns deduped entries (last-write-wins per step, first-seen order),
// the total record count (including duplicates), and parseErrors.
//
// Consistency check (AC5):
//   - idx missing → ErrIdxUnavailable
//   - header unmarshal failure → ErrIdxUnavailable
//   - header jsonl_size > actual jsonl size → ErrIdxUnavailable (corrupt)
//   - header jsonl_size <= actual size → valid (lag is normal for live procs)
//
// The alive/dead distinction for "jsonl grew but process is dead" is handled
// by the caller (server-side cache logic), not here.
func ReadStepsFromIdx(idxPath, jsonlPath string, afterStep int) ([]StepIdxEntry, int, int, error) {
	// Stat jsonl for consistency check.
	jsonlFi, err := os.Stat(jsonlPath)
	if err != nil {
		return nil, 0, 0, ErrIdxUnavailable
	}
	jsonlSize := jsonlFi.Size()

	f, err := os.Open(idxPath)
	if err != nil {
		return nil, 0, 0, ErrIdxUnavailable
	}
	defer f.Close()

	// Read header (first line).
	var header idxHeader
	headerRead := false
	parseErrors := 0
	total := 0

	// Dedup state — mirrors ReadAllStepsWithErrors semantics (AC7).
	last := make(map[int]StepIdxEntry)
	var order []int

	scanErr := jsonl.Scan(f, idxPath, func(line []byte) error {
		if !headerRead {
			headerRead = true
			if err := json.Unmarshal(line, &header); err != nil {
				return ErrIdxUnavailable // header corrupt → whole idx invalid
			}
			if header.V != 1 {
				return ErrIdxUnavailable
			}
			// Consistency: header jsonl_size > actual → corrupt (jsonl truncated?).
			if header.JSONLSize > jsonlSize {
				return ErrIdxUnavailable
			}
			return nil
		}

		var raw idxEntry
		if err := json.Unmarshal(line, &raw); err != nil {
			parseErrors++
			return nil
		}
		total++
		entry := StepIdxEntry(raw)
		if _, seen := last[entry.Step]; !seen {
			order = append(order, entry.Step)
		}
		last[entry.Step] = entry
		return nil
	})
	if scanErr != nil {
		return nil, 0, 0, ErrIdxUnavailable
	}
	if !headerRead {
		return nil, 0, 0, ErrIdxUnavailable // empty idx file
	}

	// Build result with afterStep filter (same as ReadAllStepsWithErrors).
	var result []StepIdxEntry
	for _, stepNum := range order {
		if afterStep > 0 && stepNum <= afterStep {
			continue
		}
		result = append(result, last[stepNum])
	}
	return result, total, parseErrors, nil
}

// ReadStepOffsetFromIdx finds the jsonl byte offset of the LAST record for the
// given step number. Returns -1 if the step is not found in the idx.
//
// Story 72.2 F10: used by handleGetStepDetail to Seek directly to the record
// instead of sequential-scanning the full jsonl.
func ReadStepOffsetFromIdx(idxPath, jsonlPath string, step int) (int64, error) {
	// Stat jsonl for consistency check.
	jsonlFi, err := os.Stat(jsonlPath)
	if err != nil {
		return -1, ErrIdxUnavailable
	}
	jsonlSize := jsonlFi.Size()

	f, err := os.Open(idxPath)
	if err != nil {
		return -1, ErrIdxUnavailable
	}
	defer f.Close()

	var offset int64 = -1
	headerRead := false

	scanErr := jsonl.Scan(f, idxPath, func(line []byte) error {
		if !headerRead {
			headerRead = true
			var header idxHeader
			if err := json.Unmarshal(line, &header); err != nil {
				return ErrIdxUnavailable
			}
			if header.V != 1 || header.JSONLSize > jsonlSize {
				return ErrIdxUnavailable
			}
			return nil
		}
		var raw idxEntry
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil // skip malformed
		}
		if raw.Step == step {
			offset = raw.Offset // last-write-wins
		}
		return nil
	})
	if scanErr != nil {
		return -1, ErrIdxUnavailable
	}
	return offset, nil
}

// ParseIdxEntryFromJSONL parses a single steps.jsonl line into a StepIdxEntry
// without loading the full StepRecord (skips Messages, RawResponse, etc.).
// Story 72.2: used by server-side cache incremental reads and RebuildIdx.
func ParseIdxEntryFromJSONL(line []byte, offset int64) (StepIdxEntry, error) {
	var lite struct {
		Step         int    `json:"step"`
		Action       string `json:"action"`
		Summary      string `json:"summary"`
		ToolPath     string `json:"tool_path"`
		ToolError    string `json:"tool_error"`
		TokenCount   int    `json:"token_count"`
		Timestamp    int64  `json:"timestamp"`     // time.Duration marshals as ns
		ToolDuration int64  `json:"tool_duration"` // ns
		ToolCalls    []struct {
			Error string `json:"error"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(line, &lite); err != nil {
		return StepIdxEntry{}, err
	}
	hasErr := lite.ToolError != ""
	if !hasErr {
		for i := range lite.ToolCalls {
			if lite.ToolCalls[i].Error != "" {
				hasErr = true
				break
			}
		}
	}
	return StepIdxEntry{
		Offset:      offset,
		Step:        lite.Step,
		Action:      lite.Action,
		Summary:     lite.Summary,
		ToolPath:    lite.ToolPath,
		HasError:    hasErr,
		DurationMs:  float64(lite.ToolDuration) / 1e6, // ns → ms
		TokenCount:  lite.TokenCount,
		TimestampMs: lite.Timestamp / 1e6, // ns → ms
	}, nil
}

// ReadStepAtOffset reads a single StepRecord from jsonl at the given byte
// offset. Story 72.2 F10: used by handleGetStepDetail for O(line) reads via
// idx, eliminating the O(file) sequential scan of ReadStep.
func ReadStepAtOffset(jsonlPath string, offset int64) (*types.StepRecord, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	var rec types.StepRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// IdxPathForJSONL returns the idx path for a given steps.jsonl path.
func IdxPathForJSONL(jsonlPath string) string {
	return jsonlPath[:len(jsonlPath)-len(".jsonl")] + ".idx"
}

// RebuildIdx scans the full jsonl and builds the idx file atomically.
// Called in a background goroutine; guarded by idxRebuildSem (F7, cap=2).
// If the semaphore is full, returns immediately without blocking.
//
// Story 72.2 AC4: uses a temp file + os.Rename for atomic replacement,
// preventing half-built idx from being read.
func RebuildIdx(jsonlPath string) {
	// Non-blocking semaphore acquire (F7).
	select {
	case idxRebuildSem <- struct{}{}:
		defer func() { <-idxRebuildSem }()
	default:
		return // semaphore full, skip
	}

	idxPath := IdxPathForJSONL(jsonlPath)
	tmpPath := idxPath + ".tmp"

	f, err := os.Open(jsonlPath)
	if err != nil {
		return
	}
	defer f.Close()

	jsonlFi, err := f.Stat()
	if err != nil {
		return
	}

	tmp, err := os.Create(tmpPath)
	if err != nil {
		return
	}
	defer func() {
		tmp.Close()
		os.Remove(tmpPath) // clean up on any failure path
	}()

	// Write header.
	header := fmt.Sprintf(`{"v":1,"jsonl_size":%d,"jsonl_mtime_ms":%d}`,
		jsonlFi.Size(), jsonlFi.ModTime().UnixMilli())
	if _, err := tmp.WriteString(header + "\n"); err != nil {
		return
	}

	// Scan jsonl and write idx entries.
	// 🔴 jsonl.Scan returns lines INCLUDING the trailing '\n', so offset
	// advances by len(line) exactly — no +1.
	var offset int64
	scanErr := jsonl.Scan(f, jsonlPath, func(line []byte) error {
		entry, err := ParseIdxEntryFromJSONL(line, offset)
		if err != nil {
			// Skip malformed lines (same as ReadAllStepsWithErrors).
			offset += int64(len(line))
			return nil
		}
		data, err := json.Marshal(idxEntry(entry))
		if err != nil {
			offset += int64(len(line))
			return nil
		}
		if _, err := tmp.Write(append(data, '\n')); err != nil {
			return err
		}
		offset += int64(len(line))
		return nil
	})
	if scanErr != nil {
		return
	}

	if err := tmp.Sync(); err != nil {
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}

	// Atomic replace.
	if err := os.Rename(tmpPath, idxPath); err != nil {
		return
	}
}

// idxRebuildSem is the global concurrency gate for background idx rebuilds
// (Story 72.2 F7, cap=2).
var idxRebuildSem = make(chan struct{}, 2)
