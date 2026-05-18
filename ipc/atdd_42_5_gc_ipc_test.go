package ipc

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
//                     even under daemon error (RED-phase sentinel surface)
//
// RED PHASE: kernel.RunGc still returns errRunGcNotImplemented so the IPC
// handlers route through to ErrorPayload; INT-001 / INT-002 are skipped until
// dev-story lands the real kernel.RunGc. INT-003 stays live to verify the
// wire schema is intact today.
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
	t.Skip("RED phase: 42.5 kernel.RunGc returns errRunGcNotImplemented; daemon surfaces error not OK")

	client, _, baseDir := setupResumeIPCTest(t)
	uuid := "ipc-gc-dr-aaaaaaaa-bbbb-cccc-dddd-000000000001"
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
	t.Skip("RED phase: 42.5 kernel.RunGc not implemented")

	client, _, baseDir := setupResumeIPCTest(t)
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

// --- INT-003 (always-live): RED-phase surface — daemon returns error payload ---

// While kernel.RunGc is still a stub, the daemon surfaces the sentinel error
// through Response.Error.Code = "internal" + Message containing the sentinel.
// This test ensures the wire-level error path actually works end-to-end so
// dev-story has confidence the IPC contract is wired correctly.
func TestATDD_42_5_INT_003_REDPhase_DaemonSurfacesSentinel(t *testing.T) {
	client, _, _ := setupResumeIPCTest(t)

	_, err := client.Gc()
	if err == nil {
		t.Log("Gc has been implemented — sentinel no longer surfaced")
		return
	}
	// The client wraps server ErrorPayload into a Go error; the message must
	// at least mention "not implemented" for dev-story to detect "still RED".
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "not implemented") {
		t.Errorf("RED-phase Gc must surface sentinel; got %q", err.Error())
	}

	_, dryErr := client.GcDryRun()
	if dryErr == nil {
		t.Log("GcDryRun has been implemented — sentinel no longer surfaced")
		return
	}
	dryMsg := strings.ToLower(dryErr.Error())
	if !strings.Contains(dryMsg, "not implemented") {
		t.Errorf("RED-phase GcDryRun must surface sentinel; got %q", dryErr.Error())
	}

	// Sanity: errors are distinct values from generic transport errors.
	if errors.Is(err, errors.New("disconnected")) {
		t.Error("Gc error must be the server-side sentinel, not transport")
	}
}
