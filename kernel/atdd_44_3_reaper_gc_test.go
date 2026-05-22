package kernel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.3 — AC#7: Reaper and gc must NOT remove daemon-restart Suspended
// placeholders (in-memory) or their on-disk .rnix/data/steps/<uuid>/
// directories.
//
// Story spec (44-3 §AC7):
//   - Reaper main path already filters to State==Zombie before reaping;
//     Suspended placeholders are skipped naturally. This AC pins that
//     invariant against a future refactor that might widen the filter.
//   - gc's scanGcCandidates already exempts non-{dead,zombie} state strings
//     (kernel/gc.go:228-235). Pin this against accidental regression.
//
// RED phase signal: tests depend on LoadSuspendedFromDisk (Task 3.1) so
// compile-fails today. Runtime behaviour should pass on the current code
// once compile barrier lifts — this AC is regression pinning, not
// behavioural-red.
// =============================================================================

// TestATDD_44_3_070_Reaper_DoesNotReapSuspendedPlaceholder
//
// AC#7-a: After LoadSuspendedFromDisk inserts a Suspended placeholder,
// invoking the public Reap(pid) helper must be a no-op (the placeholder
// is not Zombie, so Reap returns without touching procTable).
func TestATDD_44_3_070_Reaper_DoesNotReapSuspendedPlaceholder(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("rgcrep70")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           71,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "reaper non-interference",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -2*time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	if _, err := k.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatal("placeholder not loaded")
	}

	// Invoke the public Reap entry point — kernel/reap.go:294. The pre-44.3
	// implementation guards: `if proc.GetState() == types.StateZombie`. A
	// future refactor that drops this guard would reap the Suspended
	// placeholder; this test catches that.
	k.Reap(proc.PID)

	if got, present := k.GetProcess(proc.PID); !present || got != proc {
		t.Errorf("placeholder removed from procTable by Reap (expected no-op)")
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("placeholder state after Reap = %s, want Suspended", got)
	}
}

// TestATDD_44_3_071_Gc_DoesNotCleanSuspendedDirectory
//
// AC#7-b: gc.scanGcCandidates must NEVER include a state=suspended
// directory in its candidate list, even when DeadAt is far in the past.
// We invoke RunGc(force=true) on a config with retention_days=1 against a
// suspended directory whose DeadAt is set to "30 days ago" (worst-case
// adversarial fixture — even if the writer accidentally filled in DeadAt,
// the state filter must reject the entry).
func TestATDD_44_3_071_Gc_DoesNotCleanSuspendedDirectory(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})

	uuid := uuidForTest("rgcgc71")
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:   72,
		UUID:  uuid,
		State: "suspended",
		// Adversarial: dead_at filled in even though state is suspended.
		// gc must filter by state BEFORE age — a defensive design noted in
		// kernel/gc.go:228-235.
		DeadAt:        staticTime(t, -30*24*time.Hour).Format(time.RFC3339Nano),
		Intent:        "gc non-interference",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -31*24*time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Fatalf("RunGc: %v", err)
	}
	for _, c := range result.Candidates {
		if c.UUID == uuid {
			t.Errorf("gc picked Suspended directory as candidate: %+v", c)
		}
	}
	if result.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0 (no Suspended dir should be reaped)", result.RemovedCount)
	}
	// And the directory must still exist.
	dir := filepath.Join(dataDir, "data", "steps", uuid)
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("Suspended directory %s was removed by gc: %v", dir, statErr)
	}
}
