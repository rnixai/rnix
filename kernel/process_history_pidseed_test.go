package kernel

import (
	"strconv"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestSeedPIDCounterFromDisk_LiftsCounterToDiskMax is the regression test for
// the EchoMatrix 2026-05-26 dashboard symptom where a child appeared with a
// smaller PID than its parent after daemon restart. Root cause: pidCounter
// (a process-local atomic) reset to 0 on daemon start, then a reloaded
// Suspended placeholder kept its persisted PID (e.g. 2) while a fresh Spawn
// from the same daemon allocated PID=1 — visually "child PID=1 under parent
// PID=2". The fix seeds pidCounter from the max PID found on disk before
// any NewProcess invocation.
func TestSeedPIDCounterFromDisk_LiftsCounterToDiskMax(t *testing.T) {
	baseDir := t.TempDir()

	// Three persisted snapshots with PIDs 7, 3, 12 — max is 12.
	for _, pid := range []types.PID{7, 3, 12} {
		info := vfs.ProcInfo{
			PID:       pid,
			UUID:      uuidForTest("pid-" + strconv.FormatUint(uint64(pid), 10)),
			State:     types.StateZombie,
			Intent:    "test",
			CreatedAt: time.Now(),
		}
		if err := SaveProcInfo(baseDir, info); err != nil {
			t.Fatalf("SaveProcInfo pid=%d: %v", pid, err)
		}
	}

	// Reset counter to simulate fresh daemon startup.
	pidCounter.Store(0)

	if err := SeedPIDCounterFromDisk(baseDir); err != nil {
		t.Fatalf("SeedPIDCounterFromDisk: %v", err)
	}

	// Next nextPID() must return strictly greater than the max disk PID (12).
	next := nextPID()
	if next <= 12 {
		t.Errorf("nextPID() after seed = %d, want >12 (max disk PID); pidCounter would collide with persisted PIDs", next)
	}

	// Subsequent allocations remain monotonic.
	next2 := nextPID()
	if next2 != next+1 {
		t.Errorf("nextPID() second call = %d, want %d (monotonic)", next2, next+1)
	}
}

// TestSeedPIDCounterFromDisk_NoDirNoOp verifies that a missing steps dir is
// not an error and leaves the counter alone.
func TestSeedPIDCounterFromDisk_NoDirNoOp(t *testing.T) {
	baseDir := t.TempDir() // exists but empty — no data/steps subtree

	pidCounter.Store(42)
	if err := SeedPIDCounterFromDisk(baseDir); err != nil {
		t.Fatalf("SeedPIDCounterFromDisk on empty dir: %v", err)
	}
	if got := pidCounter.Load(); got != 42 {
		t.Errorf("counter mutated by empty-dir seed: got %d, want 42", got)
	}
}

// TestSeedPIDCounterFromDisk_NeverLowersCounter pins the invariant that
// seeding can only raise the counter — a smaller disk max must not roll the
// in-memory counter back, which would re-issue PIDs already in use by live
// (post-spawn) processes.
func TestSeedPIDCounterFromDisk_NeverLowersCounter(t *testing.T) {
	baseDir := t.TempDir()

	info := vfs.ProcInfo{
		PID:       3,
		UUID:      uuidForTest("pid-3-low"),
		State:     types.StateZombie,
		Intent:    "test",
		CreatedAt: time.Now(),
	}
	if err := SaveProcInfo(baseDir, info); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}

	pidCounter.Store(100)
	if err := SeedPIDCounterFromDisk(baseDir); err != nil {
		t.Fatalf("SeedPIDCounterFromDisk: %v", err)
	}
	if got := pidCounter.Load(); got != 100 {
		t.Errorf("counter lowered: got %d, want 100 (disk max=3 must not roll back live counter)", got)
	}
}
