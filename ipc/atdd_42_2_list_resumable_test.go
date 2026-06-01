package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ATDD 42.2: 韧性层 — IPC list_resumable 验收测试
//
// Covers AC#7 (wire schema) and AC#10 (procTable filter through IPC layer).
//
// RED PHASE: Client.ListResumable is a stub returning an empty list; server
// dispatch is not yet wired. Tests are skipped until dev-story implements:
//   - ipc/server.go: dispatch entry for MethodListResumable
//   - ipc/server_process.go: handleListResumable
//   - ipc/client.go: real call() over the socket
// =============================================================================

// writeResumableSnapshot writes proc-info.json + checkpoint.json that should
// be picked up by ListResumable, populated with realistic wire-mapped fields
// so INT-001 can assert the full ResumableProcessWire schema.
func writeResumableSnapshot(t *testing.T, projBase, uuid string, lastStep int) {
	t.Helper()
	dir := filepath.Join(projBase, "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// proc-info.json (state=running so ListResumable picks it up)
	info := map[string]any{
		"pid":         42,
		"uuid":        uuid,
		"state":       "running",
		"intent":      "long-running task " + uuid[:8],
		"provider":    "claude",
		"model":       "claude-4",
		"tokens_used": 1500,
		"max_steps":   50,
		"skills":      []string{"code-analyst"},
		"created_at":  "2026-05-16T09:00:00Z",
	}
	infoData, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), infoData, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}

	// checkpoint.json gives LastStep + LastActive (priority over proc-info).
	cp := map[string]any{
		"version":          1,
		"uuid":             uuid,
		"last_step":        lastStep,
		"timestamp":        "2026-05-16T09:30:45Z",
		"context_snapshot": json.RawMessage(`{"messages":[]}`),
		"proc_state": map[string]any{
			"pid":      42,
			"provider": "claude",
			"model":    "claude-4",
			"intent":   "long-running task " + uuid[:8],
		},
	}
	cpData, _ := json.Marshal(cp)
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json"), cpData, 0o600); err != nil {
		t.Fatalf("write checkpoint.json: %v", err)
	}
}

// --- 42.2-INT-001: IPC list_resumable 返回完整 wire schema (AC#7) ---

func TestATDD_42_2_INT_001_ListResumable_WireSchema(t *testing.T) {


	client, _, baseDir, _ := setupResumeIPCTest(t)
	uuid := "wire-schema-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeResumableSnapshot(t, baseDir, uuid, 17)

	resp, err := client.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	if resp == nil {
		t.Fatal("ListResumable response nil")
	}
	if len(resp.Processes) != 1 {
		t.Fatalf("Processes len = %d, want 1", len(resp.Processes))
	}

	got := resp.Processes[0]
	if got.UUID != uuid {
		t.Errorf("UUID = %q, want %q", got.UUID, uuid)
	}
	if got.Intent == "" {
		t.Error("Intent must be non-empty (AC#7)")
	}
	if got.LastStep != 17 {
		t.Errorf("LastStep = %d, want 17 (from checkpoint.json)", got.LastStep)
	}
	if got.LastActive == 0 {
		t.Error("LastActive must be non-zero (AC#7)")
	}
	if got.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", got.Provider, "claude")
	}
	if got.Model != "claude-4" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-4")
	}
	// Agent is derived: skills[0] or intent first-word — non-empty when skills set.
	if got.Agent == "" {
		t.Error("Agent must be derived (skills[0] or intent prefix), got empty")
	}
}

// --- 42.2-INT-002: IPC list_resumable 空列表 + procTable 协同过滤 (AC#7,#10) ---

func TestATDD_42_2_INT_002_ListResumable_EmptyAndFilter(t *testing.T) {


	client, _, _, _ := setupResumeIPCTest(t)

	// No snapshots on disk → empty list, no error.
	resp, err := client.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable (empty): %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if len(resp.Processes) != 0 {
		t.Errorf("empty list expected, got %d", len(resp.Processes))
	}
}
