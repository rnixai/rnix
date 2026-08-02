package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// Story 72.4 — recall index startup cost: persistence + staleness invalidation
//
// Fixture helpers reuse createStepsDir / typicalProcessSteps from
// atdd_35_4_recall_test.go (same package).
// =============================================================================

// appendStep appends one raw step record to an existing steps.jsonl, simulating
// a process that keeps writing after the index snapshot was taken (resume /
// long-running process — the F3 scenario).
func appendStep(t *testing.T, stepsDir string, step int, action, summary string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(stepsDir, "steps.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rec, _ := json.Marshal(map[string]any{"step": step, "action": action, "summary": summary})
	if _, err := fmt.Fprintf(f, "%s\n", rec); err != nil {
		t.Fatal(err)
	}
}

// bumpMTime forces a distinct mtime on steps.jsonl. Appends within the same
// millisecond would otherwise leave mtime_ms unchanged; the fingerprint's size
// component already catches those, but tests that only touch mtime need this.
func bumpMTime(t *testing.T, stepsDir string, delta time.Duration) {
	t.Helper()
	path := filepath.Join(stepsDir, "steps.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := info.ModTime().Add(delta)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// postingCount returns how many postings the given token holds for uuid.
func postingCount(ri *RecallIndex, token, uuid string) int {
	ri.mu.RLock()
	defer ri.mu.RUnlock()
	n := 0
	for _, p := range ri.index[token] {
		if p.UUID == uuid {
			n++
		}
	}
	return n
}

// hasUUID reports whether uuid is present in the indexed set.
func hasUUID(ri *RecallIndex, uuid string) bool {
	ri.mu.RLock()
	defer ri.mu.RUnlock()
	return ri.indexed[uuid]
}

// =============================================================================
// T1 — fingerprint recording + purge primitive (AC3, F3/F4)
// =============================================================================

// 72.4-UNIT-001: indexProcessDir records a size+mtime fingerprint per UUID, and
// the fingerprint survives a SaveIndex/LoadIndex round-trip.
func TestRecallIndex_724_FingerprintRecordedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-fp", typicalProcessSteps("alpha content", "tool_call"))

	stepsRoot := filepath.Join(dir, "data", "steps")
	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(stepsDir, "steps.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	idx.mu.RLock()
	fp, ok := idx.fprints["uuid-fp"]
	idx.mu.RUnlock()
	if !ok {
		t.Fatal("expected fingerprint recorded for uuid-fp after BuildFromDisk")
	}
	if fp.Size != info.Size() {
		t.Errorf("fingerprint size = %d, want %d", fp.Size, info.Size())
	}
	if fp.MTimeMs != info.ModTime().UnixMilli() {
		t.Errorf("fingerprint mtime_ms = %d, want %d", fp.MTimeMs, info.ModTime().UnixMilli())
	}

	indexPath := filepath.Join(dir, "data", "recall", "index.json")
	if err := idx.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	idx2 := NewRecallIndex()
	if err := idx2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	idx2.mu.RLock()
	fp2, ok2 := idx2.fprints["uuid-fp"]
	idx2.mu.RUnlock()
	if !ok2 {
		t.Fatal("expected fingerprint restored by LoadIndex")
	}
	if fp2 != fp {
		t.Errorf("restored fingerprint = %+v, want %+v", fp2, fp)
	}
}

// 72.4-UNIT-002 (AC7-3): purge clears indexed + docs + ALL postings for the
// UUID, and a rescan afterwards yields exactly the cold-scan posting count
// (guards the `ri.index[tok] = append(...)` doubling trap in F3).
func TestRecallIndex_724_PurgeThenRescanDoesNotDoublePostings(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-A", typicalProcessSteps("alpha content", "tool_call", "complete"))
	createStepsDir(t, dir, "uuid-B", typicalProcessSteps("bravo content", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")

	// Cold-scan baseline for the expected posting count.
	cold := NewRecallIndex()
	if err := cold.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	want := postingCount(cold, "alpha", "uuid-A")
	if want == 0 {
		t.Fatal("fixture produced no postings for token alpha")
	}

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}

	idx.mu.Lock()
	idx.purgeUUIDLocked("uuid-A")
	idx.mu.Unlock()

	if hasUUID(idx, "uuid-A") {
		t.Error("uuid-A still marked indexed after purge")
	}
	if got := postingCount(idx, "alpha", "uuid-A"); got != 0 {
		t.Errorf("uuid-A still has %d postings for token alpha after purge, want 0", got)
	}
	idx.mu.RLock()
	_, docsLeft := idx.docs["uuid-A"]
	_, fpLeft := idx.fprints["uuid-A"]
	bravoIntact := len(idx.index["bravo"]) > 0
	idx.mu.RUnlock()
	if docsLeft {
		t.Error("docs entry for uuid-A survived purge")
	}
	if fpLeft {
		t.Error("fingerprint for uuid-A survived purge")
	}
	if !bravoIntact {
		t.Error("purging uuid-A destroyed unrelated uuid-B postings")
	}

	// Rescan: the purged UUID must be re-indexed with exactly one posting set.
	if err := idx.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if got := postingCount(idx, "alpha", "uuid-A"); got != want {
		t.Errorf("after purge+rescan uuid-A has %d postings for alpha, want %d (cold scan)", got, want)
	}
}

// =============================================================================
// T3 — batch entry + concurrency gate + single markReady (AC2/AC6, F7/F8)
// =============================================================================

// AC7-4 (markReady timing) and AC7-7 (concurrency cap) need a *blockable probe*
// inside the real scan path; they live in atdd_72_4_recall_concurrency_unix_test.go
// because the probe is a FIFO.

// 72.4-UNIT-009: the batch entry scans every directory it is given, and an
// unreadable directory does not abort the rest of the build.
func TestRecallIndex_724_BuildAllScansAllDirsAndToleratesBadDir(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-1", typicalProcessSteps("alpha one", "tool_call"))
	rootA := filepath.Join(dir, "data", "steps")

	dirB := t.TempDir()
	createStepsDir(t, dirB, "uuid-2", typicalProcessSteps("bravo two", "tool_call"))
	rootB := filepath.Join(dirB, "data", "steps")

	missing := filepath.Join(dir, "data", "does-not-exist")

	ri := NewRecallIndex()
	ri.BuildAllFromDisksAsync([]string{rootA, missing, rootB}, 2)
	if !ri.WaitReady(10 * time.Second) {
		t.Fatal("batch build never completed")
	}

	if got := ri.ProcessCount(); got != 2 {
		t.Errorf("ProcessCount = %d, want 2 (both readable dirs indexed despite the missing one)", got)
	}
	if len(ri.Search("alpha one", 20)) == 0 {
		t.Error("dir A not indexed")
	}
	if len(ri.Search("bravo two", 20)) == 0 {
		t.Error("dir B not indexed (a missing dir must not abort the batch)")
	}
}

// 72.4-UNIT-010: empty / nil stepsDirs still reaches Ready — the daemon must not
// hang on a fresh install with no project data.
func TestRecallIndex_724_BuildAllEmptyDirsStillReady(t *testing.T) {
	ri := NewRecallIndex()
	ri.BuildAllFromDisksAsync(nil, 0)
	if !ri.WaitReady(5 * time.Second) {
		t.Fatal("Ready never flipped for an empty stepsDirs list")
	}
}

// 72.4-UNIT-011 (AC6 red line): BuildFromDiskAsync keeps its original per-call
// markReady behavior — drivers/memory's recall device tests depend on it.
func TestRecallIndex_724_BuildFromDiskAsyncStillMarksReady(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-legacy-entry", typicalProcessSteps("alpha legacy entry", "tool_call"))

	ri := NewRecallIndex()
	ri.BuildFromDiskAsync(filepath.Join(dir, "data", "steps"))
	if !ri.WaitReady(5 * time.Second) {
		t.Fatal("BuildFromDiskAsync no longer marks the index ready")
	}
}

// =============================================================================
// T2 — load-time staleness invalidation (AC3, AC7-1/2/8)
// =============================================================================

// 72.4-UNIT-003 (AC7-1, 🔴 core red): a process that keeps appending steps after
// the index snapshot must be re-indexed on the next startup. Before this story
// `indexed[uuid]` made the skip permanent and "bravo" was unreachable forever.
func TestRecallIndex_724_StaleUUIDRescannedAfterAppend(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-A", typicalProcessSteps("alpha marker", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "data", "recall", "index.json")

	// gen1: build + save.
	gen1 := NewRecallIndex()
	if err := gen1.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if err := gen1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	// The process keeps running and appends a second step.
	appendStep(t, stepsDir, 2, "tool_call", "step 2: bravo marker appeared after snapshot")
	bumpMTime(t, stepsDir, time.Second)

	// gen2: load → invalidate stale → rescan (the production startup sequence).
	gen2 := NewRecallIndex()
	if err := gen2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	if purged := gen2.InvalidateStale([]string{stepsRoot}); purged != 1 {
		t.Errorf("InvalidateStale purged %d UUIDs, want 1 (appended steps.jsonl is stale)", purged)
	}
	if err := gen2.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}

	if got := len(gen2.Search("bravo marker", 20)); got == 0 {
		t.Error("appended step is unreachable after load+rescan (F3 stale-skip defect)")
	}
	if got := len(gen2.Search("alpha marker", 20)); got == 0 {
		t.Error("original step lost after rescan")
	}
}

// 72.4-UNIT-004 (AC7-2): a UUID whose steps dir was removed (gc / manual rm)
// must be purged from the persisted index instead of haunting Search results.
func TestRecallIndex_724_GhostUUIDPurgedAfterDirRemoved(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-gone", typicalProcessSteps("alpha ghost", "tool_call"))
	createStepsDir(t, dir, "uuid-live", typicalProcessSteps("charlie live", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "data", "recall", "index.json")

	gen1 := NewRecallIndex()
	if err := gen1.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if err := gen1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(stepsDir); err != nil {
		t.Fatal(err)
	}

	gen2 := NewRecallIndex()
	if err := gen2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	if purged := gen2.InvalidateStale([]string{stepsRoot}); purged != 1 {
		t.Errorf("InvalidateStale purged %d UUIDs, want 1 (ghost)", purged)
	}

	if got := len(gen2.Search("alpha ghost", 20)); got != 0 {
		t.Errorf("Search returned %d hits for a deleted process, want 0", got)
	}
	if hasUUID(gen2, "uuid-gone") {
		t.Error("deleted UUID still counted in ProcessCount / indexed set")
	}
	if !hasUUID(gen2, "uuid-live") {
		t.Error("ghost purge collaterally removed a live UUID")
	}
	if got := len(gen2.Search("charlie live", 20)); got == 0 {
		t.Error("live UUID no longer searchable after ghost purge")
	}
}

// 72.4-UNIT-005 (AC7-8): an index.json written before this story has no
// "fprints" field. Loading it must not panic; every UUID is treated as stale
// (conservative: rescan rather than serve possibly-truncated results).
func TestRecallIndex_724_LegacyIndexWithoutFingerprints(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-legacy", typicalProcessSteps("alpha legacy", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "index.json")

	// Hand-write the pre-72.4 schema: index/docs/indexed only.
	legacy := map[string]any{
		"index":   map[string][]map[string]any{"alpha": {{"UUID": "uuid-legacy", "Step": 1, "Time": time.Now()}}},
		"docs":    map[string][]map[string]any{"uuid-legacy": {{"step": 1, "time": time.Now(), "summary": "step 1: alpha legacy", "action": "tool_call"}}},
		"indexed": map[string]bool{"uuid-legacy": true},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewRecallIndex()
	if err := idx.LoadIndex(indexPath); err != nil {
		t.Fatalf("LoadIndex on legacy schema failed: %v", err)
	}
	if purged := idx.InvalidateStale([]string{stepsRoot}); purged != 1 {
		t.Errorf("InvalidateStale purged %d UUIDs, want 1 (missing fingerprint = stale)", purged)
	}

	// Rescan restores it from disk with a fresh fingerprint.
	if err := idx.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if !hasUUID(idx, "uuid-legacy") {
		t.Error("legacy UUID not re-indexed after invalidation")
	}
	idx.mu.RLock()
	_, ok := idx.fprints["uuid-legacy"]
	idx.mu.RUnlock()
	if !ok {
		t.Error("rescan did not record a fingerprint for the legacy UUID")
	}
	if got := len(idx.Search("alpha legacy", 20)); got == 0 {
		t.Error("legacy UUID not searchable after invalidate+rescan")
	}
	_ = stepsDir
}

// 72.4-UNIT-006: an unchanged UUID must be kept (the whole point of AC1 — the
// rescan then skips it in ~ms instead of re-parsing the file).
func TestRecallIndex_724_UnchangedUUIDKept(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-stable", typicalProcessSteps("alpha stable", "tool_call", "complete"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "index.json")

	gen1 := NewRecallIndex()
	if err := gen1.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if err := gen1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	gen2 := NewRecallIndex()
	if err := gen2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	if purged := gen2.InvalidateStale([]string{stepsRoot}); purged != 0 {
		t.Errorf("InvalidateStale purged %d UUIDs for an unchanged fixture, want 0", purged)
	}
	if !hasUUID(gen2, "uuid-stable") {
		t.Error("unchanged UUID was dropped")
	}
	if got := len(gen2.Search("alpha stable", 20)); got == 0 {
		t.Error("unchanged UUID not searchable after load")
	}
}

// =============================================================================
// T4 — end-to-end parity and degradation paths (AC1, AC7-5/6)
// =============================================================================

// 72.4-UNIT-012 (AC7-5): the restored index must be indistinguishable from a
// cold scan — same process count, same Search results, same order.
func TestRecallIndex_724_LoadedIndexMatchesColdScan(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-1", typicalProcessSteps("yaml config merge", "tool_call", "plan", "complete"))
	createStepsDir(t, dir, "uuid-2", typicalProcessSteps("yaml parser rewrite", "tool_call", "complete"))
	createStepsDir(t, dir, "uuid-3", typicalProcessSteps("unrelated topic", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "data", "recall", "index.json")

	cold := NewRecallIndex()
	if err := cold.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if err := cold.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	// The production startup sequence: load → invalidate → rescan.
	warm := NewRecallIndex()
	if err := warm.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	if purged := warm.InvalidateStale([]string{stepsRoot}); purged != 0 {
		t.Errorf("InvalidateStale purged %d UUIDs on an untouched corpus, want 0", purged)
	}
	if err := warm.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}

	if got, want := warm.ProcessCount(), cold.ProcessCount(); got != want {
		t.Errorf("ProcessCount after load = %d, want %d", got, want)
	}
	for _, q := range []string{"yaml", "config merge", "unrelated", "nonexistent term"} {
		coldRes := cold.Search(q, 20)
		warmRes := warm.Search(q, 20)
		if len(coldRes) != len(warmRes) {
			t.Errorf("Search(%q): loaded index returned %d results, cold scan %d", q, len(warmRes), len(coldRes))
			continue
		}
		for i := range coldRes {
			if coldRes[i].UUID != warmRes[i].UUID || coldRes[i].Summary != warmRes[i].Summary {
				t.Errorf("Search(%q) result %d: loaded=%+v cold=%+v", q, i, warmRes[i], coldRes[i])
			}
		}
	}
}

// 72.4-UNIT-013 (AC7-6): every load-failure mode degrades to a cold scan without
// panicking — missing file, corrupt JSON, and a path that is a directory.
func TestRecallIndex_724_LoadFailureDegradesToColdScan(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string) string // returns the index path
	}{
		{
			name: "missing file",
			setup: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "no-such-index.json")
			},
		},
		{
			name: "corrupt json",
			setup: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "index.json")
				if err := os.WriteFile(p, []byte("{invalid json}"), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
		},
		{
			name: "path is a directory",
			setup: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "index.json")
				if err := os.MkdirAll(p, 0o755); err != nil {
					t.Fatal(err)
				}
				return p
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			createStepsDir(t, dir, "uuid-fallback", typicalProcessSteps("alpha fallback", "tool_call"))
			stepsRoot := filepath.Join(dir, "data", "steps")
			indexPath := tc.setup(t, dir)

			idx := NewRecallIndex()
			if err := idx.LoadIndex(indexPath); err == nil {
				t.Fatalf("LoadIndex(%s) unexpectedly succeeded", tc.name)
			}
			// The startup path still calls InvalidateStale and the scan; neither
			// may panic on the empty index a failed load leaves behind.
			if purged := idx.InvalidateStale([]string{stepsRoot}); purged != 0 {
				t.Errorf("InvalidateStale purged %d on an empty index, want 0", purged)
			}
			if err := idx.BuildFromDisk(stepsRoot); err != nil {
				t.Fatal(err)
			}
			if len(idx.Search("alpha fallback", 20)) == 0 {
				t.Error("cold-scan fallback did not index the corpus")
			}
		})
	}
}

// 72.4-UNIT-014: a corpus that changed in every way at once — one process
// appended to, one removed, one added, one untouched. This is the realistic
// daemon-restart shape.
func TestRecallIndex_724_MixedCorpusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	appended := createStepsDir(t, dir, "uuid-appended", typicalProcessSteps("alpha appended", "tool_call"))
	removed := createStepsDir(t, dir, "uuid-removed", typicalProcessSteps("bravo removed", "tool_call"))
	createStepsDir(t, dir, "uuid-stable", typicalProcessSteps("charlie stable", "tool_call"))
	stepsRoot := filepath.Join(dir, "data", "steps")
	indexPath := filepath.Join(dir, "index.json")

	gen1 := NewRecallIndex()
	if err := gen1.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}
	if err := gen1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	appendStep(t, appended, 2, "complete", "step 2: delta appeared later")
	bumpMTime(t, appended, time.Second)
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}
	createStepsDir(t, dir, "uuid-new", typicalProcessSteps("echo brand new", "tool_call"))

	gen2 := NewRecallIndex()
	if err := gen2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	if purged := gen2.InvalidateStale([]string{stepsRoot}); purged != 2 {
		t.Errorf("InvalidateStale purged %d, want 2 (one appended, one removed)", purged)
	}
	if err := gen2.BuildFromDisk(stepsRoot); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		query string
		want  bool
		why   string
	}{
		{"delta appeared", true, "appended step must be reachable after rescan"},
		{"alpha appended", true, "original content of the appended process must survive"},
		{"bravo removed", false, "deleted process must not haunt the index"},
		{"charlie stable", true, "untouched process must be preserved across the restart"},
		{"echo brand", true, "newly created process must be picked up by the rescan"},
	}
	for _, c := range checks {
		got := len(gen2.Search(c.query, 20)) > 0
		if got != c.want {
			t.Errorf("Search(%q) hit=%v, want %v — %s", c.query, got, c.want, c.why)
		}
	}
	if got := gen2.ProcessCount(); got != 3 {
		t.Errorf("ProcessCount = %d, want 3 (appended + stable + new)", got)
	}
}
