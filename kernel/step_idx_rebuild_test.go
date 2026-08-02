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

	RebuildIdx(jsonlPath)

	// Verify idx exists and is readable.
	entries, total, parseErrors, err := ReadStepsFromIdx(idxPath, jsonlPath, 0)
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
		RebuildIdx(jsonlPath)
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

func TestRebuildIdx_ConcurrentMax2(t *testing.T) {
	dir := t.TempDir()

	// Create 5 jsonl files.
	var paths []string
	for i := range 5 {
		p := filepath.Join(dir, fmt.Sprintf("steps-%d.jsonl", i))
		// Write enough lines to make rebuild take measurable time.
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
	var wg sync.WaitGroup

	// Wrap RebuildIdx to track concurrency via the semaphore.
	// We test the semaphore indirectly: launch 5 goroutines, each acquires
	// the semaphore manually to measure max concurrency.
	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			// Acquire semaphore the same way RebuildIdx does.
			select {
			case idxRebuildSem <- struct{}{}:
				n := running.Add(1)
				for {
					old := maxRunning.Load()
					if n <= old || maxRunning.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond) // simulate work
				running.Add(-1)
				<-idxRebuildSem
			default:
				// Semaphore full — skip (same as RebuildIdx).
			}
		}(p)
	}
	wg.Wait()

	if maxRunning.Load() > 2 {
		t.Errorf("max concurrent rebuilds = %d, want <= 2", maxRunning.Load())
	}
}
