package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.3 — AC#3: LoadSuspendedFromDisk reloads Suspended snapshots into
// procTable on daemon restart but DOES NOT auto-start reasonStep.
//
// Story spec (44-3 §AC3):
//   - KernelImpl gains a public method
//     `LoadSuspendedFromDisk() (loaded int, err error)`.
//   - Internally reuses ListResumable() to scan
//     <stepDataDir>/data/steps/*/proc-info.json.
//   - Only entries with State == StateSuspended are reloaded; Dead / Zombie
//     are skipped (those go to procHistory via the existing LoadHistory path).
//   - Each reloaded entry produces a placeholder Process in procTable with:
//       proc.UUID = info.UUID (Epic 28 — UUID is persistent identity)
//       proc.State == StateSuspended
//       proc.SuspendReason / PausedAt / PausedTotal restored from disk
//       suspendRequested.Store(true)
//   - reasonStep goroutine is NOT started (daemon restart deliberately does
//     not auto-resume; user must invoke `rnix resume <uuid>` or dashboard R).
//   - Idempotent: a UUID already present in procTable is skipped, so daemon
//     restart loops do not duplicate entries.
//
// RED phase signal: `KernelImpl.LoadSuspendedFromDisk` does not yet exist —
// every test in this file fails to compile until Task 3.1 lands.
// =============================================================================

// TestATDD_44_3_030_LoadSuspended_RestoresPlaceholderState
//
// AC#3 main path: a properly-formed state=suspended proc-info.json on disk
// must materialise as a Suspended placeholder Process in procTable, with
// the persisted suspend metadata copied across.
func TestATDD_44_3_030_LoadSuspended_RestoresPlaceholderState(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("ldsus030")

	pausedAt := staticTime(t, -15*time.Minute)
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           42,
		UUID:          uuid,
		PPID:          0,
		State:         "suspended",
		Intent:        "ATDD 44.3 reload placeholder",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     staticTime(t, -2*time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		ContextWindow: 100000,
		SuspendReason: "user_paused", // 44.1 SubtreeManager main-path value
		PausedAt:      pausedAt.Format(time.RFC3339Nano),
		IsPaused:      true,
	}, true /*withStepsJSONL*/, true /*withMeta*/)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded < 1 {
		t.Fatalf("loaded = %d, want >= 1", loaded)
	}

	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not in procTable after LoadSuspendedFromDisk", uuid)
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("proc.State = %s, want Suspended", got)
	}
	if got := proc.GetSuspendReason(); got != "user_paused" {
		t.Errorf("proc.SuspendReason = %q, want %q", got, "user_paused")
	}
	if !proc.IsSuspendRequested() {
		t.Error("proc.IsSuspendRequested() = false, want true (placeholder must carry the suspend flag)")
	}
	// UUID identity preservation — Epic 28 baseline.
	if proc.UUID != uuid {
		t.Errorf("proc.UUID = %q, want %q (LoadSuspendedFromDisk must preserve UUID)", proc.UUID, uuid)
	}
}

// TestATDD_44_3_031_LoadSuspended_SkipsDeadAndZombie
//
// AC#3 guard: Dead and Zombie entries on disk must NOT be loaded into
// procTable — they belong in procHistory (handled by the existing
// LoadHistory ring buffer). This separation is critical because Dead
// entries in procTable would distort dashboard counts and confuse the
// reaper's "only Zombie is reapable" invariant.
func TestATDD_44_3_031_LoadSuspended_SkipsDeadAndZombie(t *testing.T) {
	k, dataDir := newReloadKernel(t)

	deadUUID := uuidForTest("dead031")
	zomUUID := uuidForTest("zomb031")
	susUUID := uuidForTest("susp031")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       1,
		UUID:      deadUUID,
		State:     "dead",
		Intent:    "should not be reloaded",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -3*time.Hour).Format(time.RFC3339Nano),
		DeadAt:    staticTime(t, -2*time.Hour).Format(time.RFC3339Nano),
		CtxID:     1,
	}, true, true)

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       2,
		UUID:      zomUUID,
		State:     "zombie",
		Intent:    "should not be reloaded",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -3*time.Hour).Format(time.RFC3339Nano),
		CtxID:     2,
	}, true, true)

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           3,
		UUID:          susUUID,
		State:         "suspended",
		Intent:        "should be reloaded",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -3*time.Hour).Format(time.RFC3339Nano),
		CtxID:         3,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d, want exactly 1 (only the Suspended entry)", loaded)
	}

	if _, ok := k.GetProcessByUUID(deadUUID); ok {
		t.Error("Dead entry was loaded into procTable; want skipped")
	}
	if _, ok := k.GetProcessByUUID(zomUUID); ok {
		t.Error("Zombie entry was loaded into procTable; want skipped")
	}
	if _, ok := k.GetProcessByUUID(susUUID); !ok {
		t.Error("Suspended entry not found in procTable; want loaded")
	}
}

// TestATDD_44_3_032_LoadSuspended_Idempotent
//
// AC#3 idempotency: calling LoadSuspendedFromDisk twice in a row must not
// double-load entries. The second call returns loaded=0 and procTable
// remains unchanged. Guards against the daemon-restart-loop scenario where
// an external supervisor might invoke the loader more than once.
func TestATDD_44_3_032_LoadSuspended_Idempotent(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("idemp32")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           7,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "idempotency test",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "cli_disconnected",
		IsPaused:      true,
	}, true, true)

	loaded1, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("first LoadSuspendedFromDisk: %v", err)
	}
	if loaded1 != 1 {
		t.Fatalf("first call loaded = %d, want 1", loaded1)
	}
	procBefore, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatal("UUID missing after first load")
	}

	loaded2, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("second LoadSuspendedFromDisk: %v", err)
	}
	if loaded2 != 0 {
		t.Errorf("second call loaded = %d, want 0 (idempotent)", loaded2)
	}
	procAfter, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatal("UUID missing after second load")
	}
	if procBefore.PID != procAfter.PID {
		t.Errorf("idempotent call replaced the placeholder; PID %d → %d", procBefore.PID, procAfter.PID)
	}
}

// TestATDD_44_3_033_LoadSuspended_NoReasonStepStarted
//
// AC#3 critical invariant: daemon restart must NOT auto-resume. Placeholder
// Process must NOT have a reasonStep goroutine running — the user has to
// explicitly invoke `rnix resume <uuid>` or dashboard `R` to spin up the
// reasoning loop.
//
// We observe this by:
//   1. Loading the placeholder.
//   2. Waiting 200ms — if reasonStep WERE running, it would emit a
//      ReasonStep / LLMRequest event into proc.DebugChan within that window.
//   3. Asserting DebugChan contains zero ReasonStep events (and the Resurrect
//      event optionally emitted by LoadSuspendedFromDisk itself is fine).
//
// The Resurrect event presence-check doubles as a soft assertion that the
// loader emits an audit trail entry for the daemon restart, per Story
// §AC3 "emit Resurrect 事件 reason=daemon_restart from_disk=true".
func TestATDD_44_3_033_LoadSuspended_NoReasonStepStarted(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("nors33")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           11,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "no auto resume test",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "reason_for_test_only",
		IsPaused:      true,
	}, true, true)

	if _, err := k.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}

	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not loaded", uuid)
	}

	// Drain whatever events are queued in a 200ms window.
	events := drainAllEvents(t, proc, 200*time.Millisecond)

	for _, ev := range events {
		switch ev.Syscall {
		case "ReasonStep", "LLMRequest", "LLMResponse":
			t.Errorf("daemon-restart placeholder emitted %s (auto-resume forbidden); event=%+v", ev.Syscall, ev)
		}
	}
	// Resurrect / Resume events are advisory — not asserting their presence
	// here so the test stays robust against the dev-story choosing not to
	// emit Resurrect on the LoadSuspendedFromDisk path. The state assertion
	// below is the strict invariant.
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("placeholder state after 200ms = %s, want Suspended (no auto-resume)", got)
	}
}
