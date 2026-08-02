package kernel

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Story 72.2 AC10-2: idx write tests.

func TestStepWriter_WriteStep_IdxHeaderAndEntries(t *testing.T) {
	dir := t.TempDir()
	sw, err := NewStepWriter(dir, "test-uuid")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	t.Cleanup(func() { sw.Close() })

	// Write 3 steps.
	for i := 1; i <= 3; i++ {
		rec := types.StepRecord{
			Step:         i,
			Action:       "tool_call",
			Summary:      "test summary",
			TokenCount:   100 * i,
			Timestamp:    time.Duration(i) * time.Second,
			ToolDuration: time.Duration(i*10) * time.Millisecond,
		}
		if err := sw.WriteStep(rec); err != nil {
			t.Fatalf("WriteStep(%d): %v", i, err)
		}
	}

	// Read idx file.
	idxPath := filepath.Join(dir, "steps", "test-uuid", "steps.idx")
	f, err := os.Open(idxPath)
	if err != nil {
		t.Fatalf("open idx: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan idx: %v", err)
	}

	// header + 3 entries = 4 lines.
	if len(lines) != 4 {
		t.Fatalf("idx lines = %d, want 4 (header + 3 entries)", len(lines))
	}

	// Verify header.
	var header struct {
		V         int   `json:"v"`
		JSONLSize int64 `json:"jsonl_size"`
		MtimeMs   int64 `json:"jsonl_mtime_ms"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.V != 1 {
		t.Errorf("header.v = %d, want 1", header.V)
	}
	// New file → jsonl_size in header should be 0.
	if header.JSONLSize != 0 {
		t.Errorf("header.jsonl_size = %d, want 0 (new file)", header.JSONLSize)
	}

	// Verify entries.
	for i := range 3 {
		var entry idxEntry
		if err := json.Unmarshal([]byte(lines[i+1]), &entry); err != nil {
			t.Fatalf("unmarshal entry %d: %v", i, err)
		}
		if entry.Step != i+1 {
			t.Errorf("entry[%d].s = %d, want %d", i, entry.Step, i+1)
		}
		if entry.Action != "tool_call" {
			t.Errorf("entry[%d].a = %q, want tool_call", i, entry.Action)
		}
		if entry.TokenCount != 100*(i+1) {
			t.Errorf("entry[%d].k = %d, want %d", i, entry.TokenCount, 100*(i+1))
		}
	}
}

func TestStepWriter_WriteStep_IdxOffsetMatchesJSONL(t *testing.T) {
	dir := t.TempDir()
	sw, err := NewStepWriter(dir, "offset-uuid")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	t.Cleanup(func() { sw.Close() })

	// Write 3 steps with distinct content.
	for i := 1; i <= 3; i++ {
		rec := types.StepRecord{
			Step:    i,
			Action:  "tool_call",
			Summary: "offset test",
		}
		if err := sw.WriteStep(rec); err != nil {
			t.Fatalf("WriteStep(%d): %v", i, err)
		}
	}

	// Read idx entries to get offsets.
	idxPath := filepath.Join(dir, "steps", "offset-uuid", "steps.idx")
	idxF, err := os.Open(idxPath)
	if err != nil {
		t.Fatalf("open idx: %v", err)
	}
	defer idxF.Close()

	var entries []idxEntry
	scanner := bufio.NewScanner(idxF)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue // skip header
		}
		var e idxEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal idx entry: %v", err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan idx: %v", err)
	}

	// Open jsonl and verify each offset points to the correct line.
	jsonlPath := filepath.Join(dir, "steps", "offset-uuid", "steps.jsonl")
	jf, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer jf.Close()

	for i, e := range entries {
		// Seek to offset and read one line.
		if _, err := jf.Seek(e.Offset, 0); err != nil {
			t.Fatalf("seek to offset %d: %v", e.Offset, err)
		}
		lineScanner := bufio.NewScanner(jf)
		if !lineScanner.Scan() {
			t.Fatalf("no line at offset %d for entry %d", e.Offset, i)
		}
		var rec types.StepRecord
		if err := json.Unmarshal(lineScanner.Bytes(), &rec); err != nil {
			t.Fatalf("unmarshal jsonl line at offset %d: %v", e.Offset, err)
		}
		if rec.Step != e.Step {
			t.Errorf("entry[%d]: jsonl step = %d, idx step = %d", i, rec.Step, e.Step)
		}
	}
}

func TestStepWriter_WriteStep_HasError(t *testing.T) {
	dir := t.TempDir()
	sw, err := NewStepWriter(dir, "err-uuid")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	t.Cleanup(func() { sw.Close() })

	// Step with ToolError.
	if err := sw.WriteStep(types.StepRecord{Step: 1, ToolError: "boom"}); err != nil {
		t.Fatal(err)
	}
	// Step with ToolCalls[].Error.
	if err := sw.WriteStep(types.StepRecord{Step: 2, ToolCalls: []types.ToolCallRecord{{Name: "x", Error: "fail"}}}); err != nil {
		t.Fatal(err)
	}
	// Step with no error.
	if err := sw.WriteStep(types.StepRecord{Step: 3, Action: "complete"}); err != nil {
		t.Fatal(err)
	}

	// Read idx entries.
	idxPath := filepath.Join(dir, "steps", "err-uuid", "steps.idx")
	f, err := os.Open(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var entries []idxEntry
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		var e idxEntry
		json.Unmarshal(scanner.Bytes(), &e)
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan idx: %v", err)
	}

	if !entries[0].HasError {
		t.Error("entry[0] (ToolError) should have has_error=true")
	}
	if !entries[1].HasError {
		t.Error("entry[1] (ToolCalls[].Error) should have has_error=true")
	}
	if entries[2].HasError {
		t.Error("entry[2] (no error) should have has_error=false")
	}
}

// failWriter always fails Write. A bufio.Writer.Flush flushes its buffered
// bytes via the underlying Write, so once any data is buffered, Flush fails —
// simulating a jsonl that cannot be flushed to disk (ENOSPC / I/O error).
type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, errors.New("injected write failure") }

// Story 72.2 AC10-2 / F8 red light: when the jsonl Flush fails, NO idx entry may
// be written — the idx must never run ahead of the jsonl. Without the guard in
// WriteStep (jsonl Flush checked before any idx write), this test fails because
// the idx would gain an entry for a record that never reached disk.
func TestStepWriter_WriteStep_JSONLFlushFailure_NoIdxWrite(t *testing.T) {
	dir := t.TempDir()
	sw, err := NewStepWriter(dir, "flushfail-uuid")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	t.Cleanup(func() { sw.Close() })

	// One good write first, so the idx has header + 1 entry as a baseline.
	if err := sw.WriteStep(types.StepRecord{Step: 1, Action: "tool_call"}); err != nil {
		t.Fatalf("baseline WriteStep: %v", err)
	}

	// Sabotage the jsonl writer: buffered Writes succeed, but Flush fails
	// because the underlying Write rejects the buffered bytes.
	sw.mu.Lock()
	sw.writer = bufio.NewWriterSize(failWriter{}, 64*1024)
	sw.mu.Unlock()

	// This write must fail at the jsonl Flush and write no idx entry.
	if err := sw.WriteStep(types.StepRecord{Step: 2, Action: "tool_call"}); err == nil {
		t.Fatal("WriteStep with failing jsonl Flush returned nil, want error")
	}

	// The idx must still contain exactly header + 1 entry — the failed step 2
	// must NOT have produced an idx entry (F8: idx never runs ahead of jsonl).
	idxPath := filepath.Join(dir, "steps", "flushfail-uuid", "steps.idx")
	f, err := os.Open(idxPath)
	if err != nil {
		t.Fatalf("open idx: %v", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan idx: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("idx lines = %d, want 2 (header + 1 baseline entry); a failed jsonl Flush must not write idx", len(lines))
	}
}
