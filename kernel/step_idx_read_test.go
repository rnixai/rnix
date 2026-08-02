package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Story 72.2 AC10-1: idx read tests.

// writeIdxFixture creates a steps.idx + steps.jsonl pair for testing.
// entries: idx entries to write; jsonlLines: corresponding jsonl lines.
func writeIdxFixture(t *testing.T, dir string, entries []idxEntry, jsonlSize int64) (idxPath, jsonlPath string) {
	t.Helper()
	idxPath = filepath.Join(dir, "steps.idx")
	jsonlPath = filepath.Join(dir, "steps.jsonl")

	// Write jsonl (content doesn't matter for read tests, just size).
	jf, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	if jsonlSize > 0 {
		jf.Write(make([]byte, jsonlSize))
	}
	jf.Close()

	// Write idx.
	f, err := os.Create(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := fmt.Sprintf(`{"v":1,"jsonl_size":%d,"jsonl_mtime_ms":0}`, jsonlSize)
	f.WriteString(header + "\n")
	for _, e := range entries {
		data, _ := json.Marshal(e)
		f.Write(append(data, '\n'))
	}
	return idxPath, jsonlPath
}

func TestReadStepsFromIdx_Dedup(t *testing.T) {
	dir := t.TempDir()
	// 10 records, 3 unique steps (1,2,3), step 1 appears 5x, step 2 3x, step 3 2x.
	var entries []idxEntry
	steps := []int{1, 1, 2, 1, 3, 2, 1, 3, 2, 1}
	for i, s := range steps {
		entries = append(entries, idxEntry{
			Offset: int64(i * 100),
			Step:   s,
			Action: fmt.Sprintf("action-%d-%d", s, i),
		})
	}
	idxPath, jsonlPath := writeIdxFixture(t, dir, entries, 1000)

	result, total, parseErrors, err := ReadStepsFromIdx(idxPath, jsonlPath, 0)
	if err != nil {
		t.Fatalf("ReadStepsFromIdx: %v", err)
	}
	if total != 10 {
		t.Errorf("total = %d, want 10", total)
	}
	if parseErrors != 0 {
		t.Errorf("parseErrors = %d, want 0", parseErrors)
	}
	if len(result) != 3 {
		t.Fatalf("deduped len = %d, want 3", len(result))
	}
	// Last-write-wins: step 1 → entry index 9, step 2 → index 8, step 3 → index 7.
	if result[0].Step != 1 || result[0].Action != "action-1-9" {
		t.Errorf("result[0] = step %d action %q, want step 1 action-1-9", result[0].Step, result[0].Action)
	}
	if result[1].Step != 2 || result[1].Action != "action-2-8" {
		t.Errorf("result[1] = step %d action %q, want step 2 action-2-8", result[1].Step, result[1].Action)
	}
	if result[2].Step != 3 || result[2].Action != "action-3-7" {
		t.Errorf("result[2] = step %d action %q, want step 3 action-3-7", result[2].Step, result[2].Action)
	}
}

func TestReadStepsFromIdx_Missing(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	os.WriteFile(jsonlPath, []byte("{}\n"), 0o644)

	_, _, _, err := ReadStepsFromIdx(filepath.Join(dir, "nonexistent.idx"), jsonlPath, 0)
	if err != ErrIdxUnavailable {
		t.Errorf("err = %v, want ErrIdxUnavailable", err)
	}
}

func TestReadStepsFromIdx_HeaderSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	// header says jsonl_size=9999 but actual is 10 → corrupt.
	entries := []idxEntry{{Offset: 0, Step: 1, Action: "x"}}
	idxPath, jsonlPath := writeIdxFixture(t, dir, entries, 10)

	// Overwrite idx with a header claiming larger size.
	f, _ := os.Create(idxPath)
	f.WriteString(`{"v":1,"jsonl_size":9999,"jsonl_mtime_ms":0}` + "\n")
	data, _ := json.Marshal(entries[0])
	f.Write(append(data, '\n'))
	f.Close()

	_, _, _, err := ReadStepsFromIdx(idxPath, jsonlPath, 0)
	if err != ErrIdxUnavailable {
		t.Errorf("err = %v, want ErrIdxUnavailable (header size > actual)", err)
	}
}

func TestReadStepsFromIdx_AfterStepFilter(t *testing.T) {
	dir := t.TempDir()
	entries := []idxEntry{
		{Offset: 0, Step: 1, Action: "a1"},
		{Offset: 100, Step: 2, Action: "a2"},
		{Offset: 200, Step: 3, Action: "a3"},
		{Offset: 300, Step: 4, Action: "a4"},
		{Offset: 400, Step: 5, Action: "a5"},
	}
	idxPath, jsonlPath := writeIdxFixture(t, dir, entries, 500)

	result, total, _, err := ReadStepsFromIdx(idxPath, jsonlPath, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5 (unaffected by afterStep)", total)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2 (steps 4,5)", len(result))
	}
	if result[0].Step != 4 || result[1].Step != 5 {
		t.Errorf("got steps %d,%d; want 4,5", result[0].Step, result[1].Step)
	}
}

func TestReadStepsFromIdx_ParseErrors(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	os.WriteFile(jsonlPath, make([]byte, 100), 0o644)
	idxPath := filepath.Join(dir, "steps.idx")

	f, _ := os.Create(idxPath)
	f.WriteString(`{"v":1,"jsonl_size":100,"jsonl_mtime_ms":0}` + "\n")
	f.WriteString(`{"o":0,"s":1,"a":"ok"}` + "\n")
	f.WriteString(`NOT-JSON` + "\n")
	f.WriteString(`{"o":50,"s":2,"a":"ok2"}` + "\n")
	f.Close()

	result, total, parseErrors, err := ReadStepsFromIdx(idxPath, jsonlPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if parseErrors != 1 {
		t.Errorf("parseErrors = %d, want 1", parseErrors)
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestReadStepOffsetFromIdx(t *testing.T) {
	dir := t.TempDir()
	entries := []idxEntry{
		{Offset: 0, Step: 1, Action: "a"},
		{Offset: 100, Step: 2, Action: "b"},
		{Offset: 200, Step: 1, Action: "c"}, // step 1 rewritten
		{Offset: 300, Step: 3, Action: "d"},
	}
	idxPath, jsonlPath := writeIdxFixture(t, dir, entries, 400)

	// Step 1 → last offset = 200.
	off, err := ReadStepOffsetFromIdx(idxPath, jsonlPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	if off != 200 {
		t.Errorf("offset for step 1 = %d, want 200 (last-write-wins)", off)
	}

	// Step 2 → offset = 100.
	off, err = ReadStepOffsetFromIdx(idxPath, jsonlPath, 2)
	if err != nil {
		t.Fatal(err)
	}
	if off != 100 {
		t.Errorf("offset for step 2 = %d, want 100", off)
	}

	// Step 99 → not found → -1.
	off, err = ReadStepOffsetFromIdx(idxPath, jsonlPath, 99)
	if err != nil {
		t.Fatal(err)
	}
	if off != -1 {
		t.Errorf("offset for step 99 = %d, want -1", off)
	}
}

func TestReadStepOffsetFromIdx_Missing(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	os.WriteFile(jsonlPath, []byte("{}\n"), 0o644)

	_, err := ReadStepOffsetFromIdx(filepath.Join(dir, "nope.idx"), jsonlPath, 1)
	if err != ErrIdxUnavailable {
		t.Errorf("err = %v, want ErrIdxUnavailable", err)
	}
}
