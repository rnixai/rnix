package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 42.5: 治理层 — IPC gc / gc_dry_run end-to-end (AC#13)
//
// Acceptance criteria covered:
//   - AC#13  INT-001  gc_dry_run returns GcDryRunResponse {ok, dry_run,
//                     candidates: [{uuid, dead_at, size_bytes, reason}]}
//   - AC#13  INT-002  gc returns GcResponse {ok, removed_count, freed_bytes,
//                     removed_uuids: [...]}
//   - AC#13  INT-003  Client.Gc / Client.GcDryRun marshal/unmarshal correctly
// =============================================================================

// writeProcInfoForGcTest writes a minimal proc-info.json fixture for gc tests.
// Kept local to this file so we don't depend on cross-test helpers that may
// move around. Mirrors the kernel-side writeProcInfoOnly + writeProcInfoWithDeadAt
// shape.
func writeProcInfoForGcTest(t *testing.T, baseDir, uuid, state, deadAt string) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	info := map[string]any{
		"pid":         99,
		"uuid":        uuid,
		"state":       state,
		"intent":      "ipc gc test " + uuid[:8],
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

// --- INT-001 (AC#13): gc_dry_run wire schema after dev-story activation ---

func TestATDD_42_5_INT_001_GcDryRun_Schema(t *testing.T) {
	client, kern, baseDir, _ := setupResumeIPCTest(t)
	// Enable gc with a 1-day retention so the stale fixture below is a candidate.
	kern.SetGcConfig(kernel.GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})

	uuid := "ipc-gc-dr-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	// dead_at = far past so retention_days=1 always hits it.
	writeProcInfoForGcTest(t, baseDir, uuid, "dead", "2026-01-01T00:00:00Z")

	resp, err := client.GcDryRun()
	if err != nil {
		t.Fatalf("GcDryRun: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if !resp.OK {
		t.Errorf("OK = false, want true")
	}
	if !resp.DryRun {
		t.Errorf("DryRun = false, want true")
	}
	if len(resp.Candidates) == 0 {
		t.Fatal("expected at least one candidate after writing a stale dead entry")
	}
	got := resp.Candidates[0]
	if got.UUID == "" {
		t.Error("Candidate.UUID empty (wire schema mandatory field)")
	}
	if got.Reason == "" {
		t.Error("Candidate.Reason empty (wire schema mandatory field)")
	}
	// size_bytes can be 0 if the dir only has a tiny proc-info.json on a
	// filesystem with no allocation overhead; do not enforce >= 1.
}

// --- INT-002 (AC#13): gc actually cleans and returns stats ---

func TestATDD_42_5_INT_002_Gc_End2End(t *testing.T) {
	client, kern, baseDir, _ := setupResumeIPCTest(t)
	kern.SetGcConfig(kernel.GcConfig{RetentionDays: 1, MaxEntries: 0, IntervalSeconds: 3600})

	uuid := "ipc-gc-e2e-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeProcInfoForGcTest(t, baseDir, uuid, "dead", "2026-01-01T00:00:00Z")

	resp, err := client.Gc()
	if err != nil {
		t.Fatalf("Gc: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if !resp.OK {
		t.Errorf("OK = false, want true")
	}
	if resp.RemovedCount < 1 {
		t.Errorf("RemovedCount = %d, want >= 1", resp.RemovedCount)
	}
	if resp.RemovedUUIDs == nil {
		t.Error("RemovedUUIDs nil — MarshalJSON contract says [] never null")
	}
}

// --- INT-003 (always-live): GREEN-phase smoke test — daemon returns OK even when gc disabled ---

// In RED phase this asserted the sentinel surface. After GREEN the daemon's
// kernel.RunGc returns nil with zero candidates when gc is disabled (the
// default for setupResumeIPCTest), so client.Gc()/.GcDryRun() must succeed.
func TestATDD_42_5_INT_003_GreenPhase_DisabledGcReturnsOK(t *testing.T) {
	client, _, _, _ := setupResumeIPCTest(t)

	gcResp, err := client.Gc()
	if err != nil {
		t.Fatalf("Gc on disabled kernel must succeed; got %v", err)
	}
	if !gcResp.OK {
		t.Errorf("Gc().OK = false")
	}
	if gcResp.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0 (disabled gc)", gcResp.RemovedCount)
	}

	dryResp, err := client.GcDryRun()
	if err != nil {
		t.Fatalf("GcDryRun on disabled kernel must succeed; got %v", err)
	}
	if !dryResp.OK {
		t.Errorf("GcDryRun().OK = false")
	}
	if len(dryResp.Candidates) != 0 {
		t.Errorf("Candidates len = %d, want 0 (disabled gc)", len(dryResp.Candidates))
	}

	// Sanity: no transport failure on a happy path — nothing to assert.
}
