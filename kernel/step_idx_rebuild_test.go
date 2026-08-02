package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Story 72.2 AC10-5: RebuildIdx tests.

func TestRebuildIdx_Functional(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	idxPath := filepath.Join(dir, "steps.idx")

	// Write a jsonl with 5 records (3 unique steps).
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	steps := []types.StepRecord{
		{Step: 1, Action: "tool_call", Summary: "s1", TokenCount: 10},
		{Step: 2, Action: "plan", Summary: "s2", TokenCount: 20},
		{Step: 1, Action: "tool_call", Summary: "s1-rewrite", TokenCount: 30},
		{Step: 3, Action: "complete", Summary: "s3", TokenCount: 40},
		{Step: 2, Action: "plan", Summary: "s2-rewrite", TokenCount: 50},
	}
	for _, rec := range steps {
		data, _ := json.Marshal(rec)
		f.Write(append(data, '\n'))
	}
	f.Close()

	RebuildIdx(jsonlPath, false)

	// Verify idx exists and is readable.
	entries, total, parseErrors, err := ReadStepsFromIdx(idxPath, jsonlPath, 0, false)
	if err != nil {
		t.Fatalf("ReadStepsFromIdx after rebuild: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if parseErrors != 0 {
		t.Errorf("parseErrors = %d, want 0", parseErrors)
	}
	if len(entries) != 3 {
		t.Fatalf("deduped len = %d, want 3", len(entries))
	}
	// Last-write-wins: step 1 → "s1-rewrite", step 2 → "s2-rewrite".
	if entries[0].Summary != "s1-rewrite" {
		t.Errorf("step 1 summary = %q, want s1-rewrite", entries[0].Summary)
	}
	if entries[1].Summary != "s2-rewrite" {
		t.Errorf("step 2 summary = %q, want s2-rewrite", entries[1].Summary)
	}

	// Verify offsets point to correct jsonl lines.
	for _, e := range entries {
		jf, _ := os.Open(jsonlPath)
		jf.Seek(e.Offset, 0)
		scanner := bufio.NewScanner(jf)
		if scanner.Scan() {
			var rec types.StepRecord
			json.Unmarshal(scanner.Bytes(), &rec)
			if rec.Step != e.Step {
				t.Errorf("offset %d: jsonl step = %d, idx step = %d", e.Offset, rec.Step, e.Step)
			}
		}
		jf.Close()
	}

	// No .tmp file should remain.
	if _, err := os.Stat(idxPath + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful rebuild")
	}
}

func TestRebuildIdx_SemaphoreNonBlocking(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	os.WriteFile(jsonlPath, []byte(`{"step":1}`+"\n"), 0o644)

	// Fill the semaphore (cap=2).
	idxRebuildSem <- struct{}{}
	idxRebuildSem <- struct{}{}

	// RebuildIdx should return immediately without blocking.
	done := make(chan struct{})
	go func() {
		RebuildIdx(jsonlPath, false)
		close(done)
	}()

	select {
	case <-done:
		// Good — returned immediately.
	case <-time.After(2 * time.Second):
		t.Fatal("RebuildIdx blocked when semaphore was full")
	}

	// Drain semaphore.
	<-idxRebuildSem
	<-idxRebuildSem
}

// Story 72.2 P13: exercise the REAL RebuildIdx critical section (the old test
// re-implemented the semaphore logic inline and never called RebuildIdx, so it
// would stay green even if the production cap changed). The rebuildIdxStart /
// rebuildIdxDone hooks fire inside the semaphore-held region, so observing them
// measures genuine production concurrency.
func TestRebuildIdx_ConcurrentMax2(t *testing.T) {
	dir := t.TempDir()

	// Create 5 jsonl files so 5 rebuilds contend for the cap=2 semaphore.
	var paths []string
	for i := range 5 {
		p := filepath.Join(dir, fmt.Sprintf("steps-%d.jsonl", i))
		f, _ := os.Create(p)
		for j := range 100 {
			rec := types.StepRecord{Step: j, Action: "tool_call", Summary: "x"}
			data, _ := json.Marshal(rec)
			f.Write(append(data, '\n'))
		}
		f.Close()
		paths = append(paths, p)
	}

	var running atomic.Int32
	var maxRunning atomic.Int32

	// Install hooks that observe the real critical section. Each start blocks
	// briefly so overlapping rebuilds are detectable.
	rebuildIdxStart = func() {
		n := running.Add(1)
		for {
			old := maxRunning.Load()
			if n <= old || maxRunning.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond) // hold the slot so concurrency is observable
	}
	rebuildIdxDone = func() { running.Add(-1) }
	t.Cleanup(func() {
		rebuildIdxStart = nil
		rebuildIdxDone = nil
	})

	// Launch more rebuilds than the semaphore allows. RebuildIdx is
	// non-blocking (full semaphore → immediate return), so fire several rounds
	// to ensure at least some actually enter the critical section concurrently.
	var wg sync.WaitGroup
	for range 4 {
		for _, p := range paths {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				RebuildIdx(path, false) // real production call
			}(p)
		}
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()

	if got := maxRunning.Load(); got == 0 {
		t.Fatal("no rebuild ever entered the critical section — hooks not invoked")
	} else if got > 2 {
		t.Errorf("max concurrent rebuilds = %d, want <= 2 (semaphore cap)", got)
	}
}

// Story 72.2 P4: RebuildIdx must be a no-op for a live process — renaming the
// idx under a live StepWriter's O_APPEND fd would orphan its writes.
func TestRebuildIdx_SkipsLiveProcess(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "steps.jsonl")
	idxPath := filepath.Join(dir, "steps.idx")
	os.WriteFile(jsonlPath, []byte(`{"step":1}`+"\n"), 0o644)

	RebuildIdx(jsonlPath, true) // procAlive = true

	// No idx should have been created for a live process.
	if _, err := os.Stat(idxPath); !os.IsNotExist(err) {
		t.Errorf("idx was created for a live process; want it skipped (P4)")
	}
}
