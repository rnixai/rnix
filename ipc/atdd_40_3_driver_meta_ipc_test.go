package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 40.3: DriverMeta via IPC wire
//
// RED PHASE: These tests reference GetProcDetailResponse.DriverMeta (does not
// exist yet) and ProcInfo.DriverMeta / ProcInfoWire.DriverMeta. They will fail
// to compile until the feature is implemented.
//
// AC1: IPC get_proc_detail 返回 DriverMeta 字段（活跃 + 历史进程）
// ============================================================================

// ---------------------------------------------------------------------------
// AC #1: 活跃进程 — get_proc_detail 包含 DriverMeta
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_GetProcDetail_DriverMeta_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create a running process with DriverMeta simulating Claude CLI
	proc := kernel.NewProcess(0, "claude cli test", nil)
	_ = proc.Start()
	proc.Provider = "claude-cli"
	proc.Model = "claude-sonnet-4-6"
	proc.DriverMeta = map[string]string{
		"resolved_bin":         "/usr/local/bin/claude",
		"permission_mode":      "bypassPermissions",
		"cap_partial_messages": "true",
		"cap_add_dir":          "true",
		"cap_permission_mode":  "true",
	}

	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("get_proc_detail failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// AC1: DriverMeta field present and populated
	if detail.DriverMeta == nil {
		t.Fatal("AC1 FAIL: GetProcDetailResponse.DriverMeta is nil for Claude CLI process")
	}
	if detail.DriverMeta["resolved_bin"] != "/usr/local/bin/claude" {
		t.Errorf("AC1 FAIL: DriverMeta[resolved_bin] = %q, want %q",
			detail.DriverMeta["resolved_bin"], "/usr/local/bin/claude")
	}
	if detail.DriverMeta["permission_mode"] != "bypassPermissions" {
		t.Errorf("AC1 FAIL: DriverMeta[permission_mode] = %q, want %q",
			detail.DriverMeta["permission_mode"], "bypassPermissions")
	}
	if detail.DriverMeta["cap_partial_messages"] != "true" {
		t.Errorf("AC1 FAIL: DriverMeta[cap_partial_messages] = %q, want %q",
			detail.DriverMeta["cap_partial_messages"], "true")
	}
}

// ---------------------------------------------------------------------------
// AC #1: 历史进程 — get_proc_detail 从 proc-info.json 加载 DriverMeta
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_GetProcDetail_DriverMeta_ReapedProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	reapedUUID := "40300000-0000-7000-0000-deadbeef0001"
	pid := types.PID(40301)

	// Persist proc-info.json with DriverMeta
	procInfo := vfs.ProcInfo{
		PID:       pid,
		UUID:      reapedUUID,
		State:     types.StateDead,
		Intent:    "reaped claude cli process",
		Provider:  "claude-cli",
		Model:     "claude-sonnet-4-6",
		CreatedAt: time.Now().Add(-5 * time.Minute),
		DeadAt:    time.Now().Add(-1 * time.Minute),
		DriverMeta: map[string]string{
			"resolved_bin":         "/home/user/.local/bin/openclaude",
			"permission_mode":      "default",
			"cap_partial_messages": "false",
			"cap_add_dir":          "false",
			"cap_permission_mode":  "false",
		},
	}

	if err := kernel.SaveProcInfo(projBase, procInfo); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}

	// Also need process-meta.json for server_detail handler
	metaDir := filepath.Join(projBase, "steps", reapedUUID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	metaPath := filepath.Join(metaDir, "process-meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"system_prompt":"test","tool_defs":[]}`), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	if err := srv.kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{UUID: reapedUUID})
	if !resp.OK {
		t.Fatalf("get_proc_detail (reaped) failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// AC1: DriverMeta survives reap → proc-info.json → LoadHistory → get_proc_detail
	if detail.DriverMeta == nil {
		t.Fatal("AC1 FAIL: DriverMeta is nil for reaped Claude CLI process (should persist)")
	}
	if detail.DriverMeta["resolved_bin"] != "/home/user/.local/bin/openclaude" {
		t.Errorf("AC1 FAIL: reaped DriverMeta[resolved_bin] = %q, want %q",
			detail.DriverMeta["resolved_bin"], "/home/user/.local/bin/openclaude")
	}
}

// ---------------------------------------------------------------------------
// AC #1 (negative): 非 Claude CLI driver — DriverMeta 为空
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_GetProcDetail_DriverMeta_Empty_NonClaude(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create a process without DriverMeta (e.g. Anthropic API driver)
	proc := kernel.NewProcess(0, "anthropic api test", nil)
	_ = proc.Start()
	proc.Provider = "anthropic"
	proc.Model = "claude-sonnet-4-6"
	// DriverMeta NOT set — should remain nil/empty

	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("get_proc_detail failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// AC1: Non-Claude driver processes should NOT have DriverMeta
	if len(detail.DriverMeta) > 0 {
		t.Errorf("AC1 FAIL: non-Claude driver should have empty DriverMeta, got %v", detail.DriverMeta)
	}
}

// ---------------------------------------------------------------------------
// Wire 转换: ProcInfoToWire / WireToProcInfo 保留 DriverMeta
// ---------------------------------------------------------------------------

func TestATDD_40_3_ProcInfoWire_DriverMeta_RoundTrip(t *testing.T) {
	t.Parallel()

	// GIVEN: ProcInfo with DriverMeta
	// WHEN:  ProcInfoToWire → WireToProcInfo 往返
	// THEN:  DriverMeta 保持不变

	original := vfs.ProcInfo{
		PID:    types.PID(100),
		UUID:   "test-uuid-403",
		Intent: "round trip test",
		DriverMeta: map[string]string{
			"resolved_bin":         "/usr/bin/claude",
			"permission_mode":      "bypassPermissions",
			"cap_partial_messages": "true",
			"cap_add_dir":          "false",
			"cap_permission_mode":  "true",
		},
	}

	wire := ProcInfoToWire(original)

	// Verify wire has DriverMeta
	if wire.DriverMeta == nil {
		t.Fatal("ProcInfoToWire: DriverMeta is nil")
	}
	if wire.DriverMeta["resolved_bin"] != "/usr/bin/claude" {
		t.Errorf("ProcInfoToWire: DriverMeta[resolved_bin] = %q, want %q",
			wire.DriverMeta["resolved_bin"], "/usr/bin/claude")
	}

	// Round-trip back
	roundTripped := WireToProcInfo(wire)
	if roundTripped.DriverMeta == nil {
		t.Fatal("WireToProcInfo: DriverMeta is nil after round-trip")
	}
	for key, want := range original.DriverMeta {
		if got := roundTripped.DriverMeta[key]; got != want {
			t.Errorf("Round-trip: DriverMeta[%s] = %q, want %q", key, got, want)
		}
	}
}
