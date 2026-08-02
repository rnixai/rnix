//go:build unix

package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Story 72.4 — AC7-4 (markReady timing) and AC7-7 (concurrency cap)
//
// Both must observe the batch build *while it is running*, which needs a
// blocking point inside the real scan path. The probe is a FIFO standing in for
// steps.jsonl: indexProcessDir's os.Open blocks until a writer attaches, so the
// test controls exactly when each scan goroutine may proceed — no timing
// guesswork and no test hook in production code.
//
// Probe discipline: the test opens the write end NON-BLOCKING (ENXIO while no
// scan goroutine is reading, so it never hangs) and then HOLDS the descriptor.
// A successful open is the "this scan reached the FIFO" signal; holding the fd
// keeps that scan parked in Read. Closing the fd is what releases it, so the
// probe must never open-and-close to peek — that would hand the reader an EOF
// and end the very scan the test is trying to observe.
//
// These drive BuildAllFromDisksAsync itself. The 72.2 review found a
// concurrency test that hand-rolled a semaphore and never called the function
// under test — raising the cap from 2 to 200 left it green.
// =============================================================================

// fifoStepsDir creates <base>/data/<rootName>/<uuid>/steps.jsonl as a FIFO and
// returns its path. A scan goroutine reaching this process blocks in os.Open
// until a writer attaches.
func fifoStepsDir(t *testing.T, base, rootName, uuid string) string {
	t.Helper()
	dir := filepath.Join(base, "data", rootName, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "steps.jsonl")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
	}
	return path
}

// tryAttachWriter attempts a non-blocking open of the FIFO's write end. It
// returns nil when no scan goroutine is reading yet (ENXIO). The caller keeps
// the returned file open to hold that scan parked.
func tryAttachWriter(path string) *os.File {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	return os.NewFile(uintptr(fd), path)
}

// releaseWriter feeds the parked scan one valid step record and closes the write
// end; the resulting EOF lets that scan finish.
func releaseWriter(t *testing.T, w *os.File, summary string) {
	t.Helper()
	if _, err := fmt.Fprintf(w, "{\"step\":1,\"action\":\"tool_call\",\"summary\":%q}\n", summary); err != nil {
		t.Fatalf("write to fifo: %v", err)
	}
	w.Close()
}

// 72.4-UNIT-007 (AC7-4 / F7): Ready() must stay false until the LAST directory
// finishes. Before this story every per-directory goroutine called markReady, so
// the first one to finish flipped Ready with 119/120 of the index still missing
// and /dev/memory/recall began answering searches from a 1/120-complete index.
//
// Dir A is ordinary and completes immediately; dir B holds a FIFO and stays
// parked. Old behavior → Ready() true as soon as A finishes. Required behavior →
// Ready() false until B is released.
func TestRecallIndex_724_BuildAllReadyOnlyAfterAllDirs(t *testing.T) {
	base := t.TempDir()
	createStepsDir(t, base, "uuid-fast", typicalProcessSteps("alpha fast dir", "tool_call"))
	rootA := filepath.Join(base, "data", "steps")

	fifoPath := fifoStepsDir(t, base, "steps-blocked", "uuid-blocked")
	rootB := filepath.Join(base, "data", "steps-blocked")

	ri := NewRecallIndex()
	ri.BuildAllFromDisksAsync([]string{rootA, rootB}, 2)

	// Park dir B and wait for dir A to finish. At this point the pre-72.4 code
	// would already have flipped Ready.
	deadline := time.Now().Add(20 * time.Second)
	var w *os.File
	for w == nil || ri.ProcessCount() < 1 {
		if w == nil {
			w = tryAttachWriter(fifoPath)
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup timed out (writer attached=%v, ProcessCount=%d)", w != nil, ri.ProcessCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Give any premature markReady time to land before sampling.
	time.Sleep(100 * time.Millisecond)

	if ri.Ready() {
		w.Close()
		t.Fatal("Ready() is true while a directory is still being scanned (F7 premature markReady)")
	}

	releaseWriter(t, w, "step 1: bravo blocked dir")
	if !ri.WaitReady(20 * time.Second) {
		t.Fatal("Ready never flipped after the last directory completed")
	}
	if len(ri.Search("bravo blocked", 20)) == 0 {
		t.Error("the blocked directory's content is missing from the index")
	}
}

// 72.4-UNIT-008 (AC7-7 / F8): peak in-flight scans must never exceed the gate.
// Each of the 5 directories parks on its own FIFO, so the number of attached
// writers IS the live scan concurrency. Unbounded fan-out would park all 5 at
// once; with cap=2 the count must reach 2 and stay there.
func TestRecallIndex_724_BuildAllConcurrencyCapped(t *testing.T) {
	base := t.TempDir()

	const dirs = 5
	const gate = 2

	fifos := make([]string, dirs)
	roots := make([]string, dirs)
	for i := range dirs {
		rootName := fmt.Sprintf("steps-%d", i)
		fifos[i] = fifoStepsDir(t, base, rootName, fmt.Sprintf("uuid-%d", i))
		roots[i] = filepath.Join(base, "data", rootName)
	}

	ri := NewRecallIndex()
	ri.BuildAllFromDisksAsync(roots, gate)

	writers := make([]*os.File, dirs)
	// attachAll returns how many scans are currently parked. Writers are kept
	// open, so the count only grows as the gate admits more scans.
	attachAll := func() int {
		n := 0
		for i, p := range fifos {
			if writers[i] == nil {
				writers[i] = tryAttachWriter(p)
			}
			if writers[i] != nil {
				n++
			}
		}
		return n
	}
	t.Cleanup(func() {
		for _, w := range writers {
			if w != nil {
				w.Close()
			}
		}
	})

	deadline := time.Now().Add(20 * time.Second)
	for attachAll() < gate {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d scans started — the gate never filled", attachAll(), gate)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Keep sampling: a broken cap shows up as the count climbing toward `dirs`.
	peak := 0
	for range 40 {
		if n := attachAll(); n > peak {
			peak = n
		}
		time.Sleep(5 * time.Millisecond)
	}

	if peak > gate {
		t.Errorf("peak concurrent scans = %d, exceeds gate cap %d", peak, gate)
	}
	if peak < gate {
		t.Errorf("peak concurrent scans = %d, want %d — the gate must not serialize the build", peak, gate)
	}

	// Drain: release each parked scan so the gate admits the next one, until all
	// five directories are done.
	released := make([]bool, dirs)
	for done := 0; done < dirs; {
		attachAll()
		progressed := false
		for i := range fifos {
			if writers[i] != nil && !released[i] {
				releaseWriter(t, writers[i], fmt.Sprintf("step 1: charlie dir %d", i))
				released[i] = true
				done++
				progressed = true
			}
		}
		if !progressed {
			if time.Now().After(deadline) {
				t.Fatalf("timed out draining FIFOs (%d/%d released)", done, dirs)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	if !ri.WaitReady(20 * time.Second) {
		t.Fatal("batch build never completed after draining")
	}
	if got := ri.ProcessCount(); got != dirs {
		t.Errorf("ProcessCount = %d, want %d (every gated directory must still be indexed)", got, dirs)
	}
}
