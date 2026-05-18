package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// ATDD 42.5: 治理层 — gc 核心扫描 + 清理 + daemon 后台 loop
//
// Acceptance criteria covered:
//   - AC#1   UNIT-010  retention_days time-window cleanup
//   - AC#2   UNIT-011  max_entries count-window cleanup
//   - AC#3   UNIT-012  Running / Suspended processes exempt
//   - AC#7   UNIT-013  StartGcDaemon ticks under enabled config
//   - AC#8   UNIT-014  StartGcDaemon noop / immediate return when disabled
//   - AC#1   UNIT-015  RunGc syncs procHistory in-memory via RemoveByUUID
//   - AC#4   UNIT-016  RunGc dry-run does not delete
//   - AC#1   UNIT-017  RunGc on empty .rnix/data/steps/ returns no error
//
// RED PHASE:
//   - kernel.RunGc returns errRunGcNotImplemented (zero-value GcResult)
//   - kernel.StartGcDaemon is a no-op
//   - Each behavior assertion sits behind a t.Skip("RED phase: ...") guard
//   - Stub-sanity tests (no Skip) confirm the sentinel error is in place so
//     dev-story can rely on the marker for "implementation pending" detection
// =============================================================================

// ---------------------------------------------------------------------------
// fixture helpers
// ---------------------------------------------------------------------------

// writeProcInfoWithDeadAt writes proc-info.json with both state and dead_at
// pre-filled. Useful for retention_days/max_entries tests where the existing
// writeProcInfoOnly helper (42.2) omits dead_at.
func writeProcInfoWithDeadAt(t *testing.T, baseDir, uuid, state, deadAt string) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	info := map[string]any{
		"pid":         99,
		"uuid":        uuid,
		"state":       state,
		"intent":      "gc test " + uuid[:8],
		"provider":    "claude",
		"model":       "claude-4",
		"tokens_used": 100,
		"created_at":  "2026-01-01T00:00:00Z",
	}
	if deadAt != "" {
		info["dead_at"] = deadAt
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UNIT-010 (AC#1): retention_days time-window cleanup
// ---------------------------------------------------------------------------

func TestATDD_42_5_010_RetentionDays_Cleanup(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 30, MaxEntries: 0, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	now := time.Now().UTC()
	staleDeadAt := now.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
	freshDeadAt := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339Nano)

	writeProcInfoWithDeadAt(t, baseDir, "stale-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000001", "dead", staleDeadAt)
	writeProcInfoWithDeadAt(t, baseDir, "fresh-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000002", "dead", freshDeadAt)

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Fatalf("RunGc: %v", err)
	}
	if result.RemovedCount != 1 {
		t.Fatalf("RemovedCount = %d, want 1 (only stale entry, AC#1)", result.RemovedCount)
	}

	// Stale directory should be gone.
	if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", "stale-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000001")); !os.IsNotExist(statErr) {
		t.Errorf("stale directory must be deleted; statErr=%v", statErr)
	}
	// Fresh directory should remain.
	if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", "fresh-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000002")); statErr != nil {
		t.Errorf("fresh directory must remain; statErr=%v", statErr)
	}
}

// ---------------------------------------------------------------------------
// UNIT-011 (AC#2): max_entries count-window cleanup
// ---------------------------------------------------------------------------

func TestATDD_42_5_011_MaxEntries_Cleanup(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 0, MaxEntries: 5, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	// Write 8 dead entries with linearly older dead_at — gc should reap the
	// oldest 3 (8 - max_entries).
	for i := range 8 {
		uuid := timestampedUUID(i)
		deadAt := time.Now().UTC().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339Nano)
		writeProcInfoWithDeadAt(t, baseDir, uuid, "dead", deadAt)
	}

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Fatalf("RunGc: %v", err)
	}
	if result.RemovedCount != 3 {
		t.Errorf("RemovedCount = %d, want 3 (8 entries − max_entries 5)", result.RemovedCount)
	}

	// Count remaining entries directly from disk.
	remaining, _ := os.ReadDir(filepath.Join(baseDir, "data", "steps"))
	if len(remaining) != 5 {
		t.Errorf("remaining entries on disk = %d, want 5", len(remaining))
	}
}

// ---------------------------------------------------------------------------
// UNIT-012 (AC#3): Running / Suspended processes are exempt
// ---------------------------------------------------------------------------

func TestATDD_42_5_012_RunningSuspended_Exempt(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	staleDeadAt := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339Nano)

	// Running snapshot — exempt regardless of dead_at being old.
	writeProcInfoWithDeadAt(t, baseDir, "running-aaaaaaaa-bbbb-cccc-dddd-000000000001", "running", staleDeadAt)
	// Suspended-like snapshot (state=running + checkpoint.json present implies suspended in
	// the 42-2 ListResumable parlance; gc must NOT touch it.)
	writeProcInfoWithDeadAt(t, baseDir, "suspended-aaaaaaaa-bbbb-cccc-dddd-000000000002", "running", staleDeadAt)
	// Dead — eligible.
	writeProcInfoWithDeadAt(t, baseDir, "dead-aaaaaaaa-bbbb-cccc-dddd-000000000003", "dead", staleDeadAt)

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Fatalf("RunGc: %v", err)
	}
	if result.RemovedCount != 1 {
		t.Fatalf("RemovedCount = %d, want 1 (only dead/zombie eligible, AC#3)", result.RemovedCount)
	}

	// Running entry must still exist.
	for _, uuid := range []string{
		"running-aaaaaaaa-bbbb-cccc-dddd-000000000001",
		"suspended-aaaaaaaa-bbbb-cccc-dddd-000000000002",
	} {
		if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", uuid)); statErr != nil {
			t.Errorf("AC#3 violation: gc deleted exempt process %s (%v)", uuid, statErr)
		}
	}
}

// ---------------------------------------------------------------------------
// UNIT-013 (AC#7): StartGcDaemon launches when policy enabled
// ---------------------------------------------------------------------------

func TestATDD_42_5_013_StartGcDaemon_RunsWhenEnabled(t *testing.T) {

	k := newThrottleTestKernel(t)
	// 1-second interval so the test does not block for long.
	k.SetGcConfig(GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 60})
	baseDir := k.GetStepDataDir()

	staleDeadAt := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339Nano)
	writeProcInfoWithDeadAt(t, baseDir, "stale-daemon-aaaaaaaa-bbbb-cccc-dddd-000000000001", "dead", staleDeadAt)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go k.StartGcDaemon(ctx)

	// Wait for the first immediate gc pass to delete the stale entry.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", "stale-daemon-aaaaaaaa-bbbb-cccc-dddd-000000000001")); os.IsNotExist(statErr) {
			return // pass
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("StartGcDaemon did not clean up stale entry within 2s (AC#7 first-immediate-run)")
}

// ---------------------------------------------------------------------------
// UNIT-014 (AC#8): StartGcDaemon noop / immediate return when disabled
// ---------------------------------------------------------------------------

func TestATDD_42_5_014_StartGcDaemon_NoopWhenDisabled(t *testing.T) {

	k := newThrottleTestKernel(t)
	// All-zero retention = disabled (AC#8).
	k.SetGcConfig(GcConfig{RetentionDays: 0, MaxEntries: 0, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	staleDeadAt := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339Nano)
	writeProcInfoWithDeadAt(t, baseDir, "stale-disabled-aaaaaaaa-bbbb-cccc-dddd-000000000001", "dead", staleDeadAt)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		k.StartGcDaemon(ctx)
		close(done)
	}()

	// Disabled config should let StartGcDaemon return immediately.
	select {
	case <-done:
		// pass
	case <-time.After(300 * time.Millisecond):
		t.Error("StartGcDaemon must return immediately when disabled (AC#8)")
	}

	// Stale entry must remain untouched (AC#8: 现有进程数据不被触碰).
	if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", "stale-disabled-aaaaaaaa-bbbb-cccc-dddd-000000000001")); statErr != nil {
		t.Errorf("disabled gc must NOT delete entries; statErr=%v", statErr)
	}
}

// ---------------------------------------------------------------------------
// UNIT-015 (AC#1 internal): RunGc syncs procHistory in-memory
// ---------------------------------------------------------------------------

func TestATDD_42_5_015_RunGc_SyncsProcHistory(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	uuid := "sync-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	staleDeadAt := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339Nano)
	writeProcInfoWithDeadAt(t, baseDir, uuid, "dead", staleDeadAt)

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if k.procHistory.FindByUUID(uuid) == nil {
		t.Fatal("procHistory must contain UUID after LoadHistory")
	}

	if _, err := k.RunGc(false, true); err != nil {
		t.Fatalf("RunGc: %v", err)
	}

	if k.procHistory.FindByUUID(uuid) != nil {
		t.Error("procHistory must drop UUID after gc (RunGc must call RemoveByUUID)")
	}
	if !k.procHistory.HasEverSeen(uuid) {
		t.Error("HasEverSeen must return true for gc'd UUID (AC#6 marker for garbage-collected vs never-spawned)")
	}
}

// ---------------------------------------------------------------------------
// UNIT-016 (AC#4): RunGc dry-run does not delete
// ---------------------------------------------------------------------------

func TestATDD_42_5_016_RunGc_DryRun_DoesNotDelete(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})
	baseDir := k.GetStepDataDir()

	uuid := "dryrun-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	staleDeadAt := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339Nano)
	writeProcInfoWithDeadAt(t, baseDir, uuid, "dead", staleDeadAt)

	result, err := k.RunGc(true, false) // dryRun=true
	if err != nil {
		t.Fatalf("RunGc dry-run: %v", err)
	}
	if result.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0 (dry-run must not delete)", result.RemovedCount)
	}
	if len(result.Candidates) != 1 {
		t.Errorf("len(Candidates) = %d, want 1", len(result.Candidates))
	}

	// Directory must still exist.
	if _, statErr := os.Stat(filepath.Join(baseDir, "data", "steps", uuid)); statErr != nil {
		t.Errorf("dry-run must NOT delete directory; statErr=%v", statErr)
	}
}

// ---------------------------------------------------------------------------
// UNIT-017 (AC#1 edge): RunGc on empty / missing steps dir returns no error
// ---------------------------------------------------------------------------

func TestATDD_42_5_017_RunGc_EmptyDataDir(t *testing.T) {

	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 30, MaxEntries: 500, IntervalSeconds: 3600})
	// Note: stepDataDir is t.TempDir() but no `data/steps/` subdir yet.

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Errorf("RunGc on empty data dir must not error; got %v", err)
	}
	if result.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0", result.RemovedCount)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates len = %d, want 0", len(result.Candidates))
	}
}

// ---------------------------------------------------------------------------
// Stub-sanity tests (post-GREEN: become natural no-ops)
// ---------------------------------------------------------------------------

// TestATDD_42_5_StubSanity_RunGc was the RED-phase sentinel-detector.
// Now that RunGc is implemented (GREEN phase), this test confirms that RunGc
// returns nil error on an empty kernel (no gc config set ⇒ disabled ⇒ no-op).
func TestATDD_42_5_StubSanity_RunGc(t *testing.T) {
	k := newThrottleTestKernel(t)
	_, err := k.RunGc(false, true)
	if err != nil {
		t.Errorf("RunGc on disabled kernel must succeed; got %v", err)
	}
}

// TestATDD_42_5_StubSanity_StartGcDaemon verifies that StartGcDaemon is a no-op
// in the RED phase (returns immediately for any config).
func TestATDD_42_5_StubSanity_StartGcDaemon(t *testing.T) {
	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 30, MaxEntries: 0, IntervalSeconds: 3600})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		k.StartGcDaemon(ctx)
		close(done)
	}()
	select {
	case <-done:
		// pass — stub returns immediately
	case <-time.After(500 * time.Millisecond):
		t.Error("RED phase StartGcDaemon must be a no-op returning immediately")
	}
}

// timestampedUUID generates a deterministic, sortable UUID-shaped string for
// fixtures. Uses index i so tests can predict ordering when they care.
func timestampedUUID(i int) string {
	return formatTestUUID(i)
}

// formatTestUUID encodes i into the 12-hex-char suffix of an otherwise-zero
// UUID. 8-4-4-4-12 shape; ordering by hex value matches numeric ordering.
func formatTestUUID(i int) string {
	const prefix = "00000000-0000-0000-0000-"
	suffix := []byte("000000000000")
	hex := []byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}
	for pos := len(suffix) - 1; i > 0 && pos >= 0; pos-- {
		suffix[pos] = hex[i&0xf]
		i >>= 4
	}
	return prefix + string(suffix)
}
