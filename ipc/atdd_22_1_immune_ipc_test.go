package ipc

import (
	"encoding/json"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 22.1: Immune Daemon IPC Protocol Tests
//
// 测试 MethodImmuneStatus 常量和请求/响应类型。
// 测试引用的常量和类型尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go 中实现。
// ============================================================

// --- 22.1-IPC-001: [P0] MethodImmuneStatus constant value (AC3) ---

func TestMethodImmuneStatus_Constant(t *testing.T) {
	// Then: MethodImmuneStatus equals "immune_status"
	if MethodImmuneStatus != "immune_status" {
		t.Errorf("MethodImmuneStatus = %q, want %q", MethodImmuneStatus, "immune_status")
	}
}

// --- 22.1-IPC-002: [P0] ImmuneStatusRequest type exists (AC3) ---

func TestImmuneStatusRequest_TypeExists(t *testing.T) {
	// Given: an ImmuneStatusRequest
	req := ImmuneStatusRequest{}

	// Then: it can be serialized to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("serialized JSON should not be empty")
	}
}

// --- 22.1-IPC-003: [P0] ImmuneStatusResponse has correct fields (AC3) ---

func TestImmuneStatusResponse_Fields(t *testing.T) {
	// Given: an ImmuneStatusResponse with data
	resp := ImmuneStatusResponse{
		Running:      true,
		ProfileCount: 3,
		ActivePIDs:   []uint64{1, 2},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains expected fields
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m["running"] != true {
		t.Errorf("running = %v, want true", m["running"])
	}
	if m["profile_count"] != float64(3) {
		t.Errorf("profile_count = %v, want 3", m["profile_count"])
	}
}

// --- 22.1-IPC-004: [P0] ImmuneStatusResponse empty ActivePIDs serializes as [] (AC3) ---

func TestImmuneStatusResponse_EmptyActivePIDs(t *testing.T) {
	// Given: a response with empty active PIDs
	resp := ImmuneStatusResponse{
		Running:      false,
		ProfileCount: 0,
		ActivePIDs:   []uint64{},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: active_pids is [] not null
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	pids, ok := m["active_pids"]
	if !ok {
		t.Fatal("missing active_pids field")
	}
	arr, ok := pids.([]any)
	if !ok {
		t.Fatalf("active_pids is not an array, got %T", pids)
	}
	if len(arr) != 0 {
		t.Errorf("active_pids should be empty, got %d elements", len(arr))
	}
}

// --- 22.1-IPC-005: [P1] Client.ImmuneStatus method exists (AC3) ---

func TestClient_ImmuneStatus_MethodExists(t *testing.T) {
	// Given: a Client with no connection (method existence check only)
	// This test verifies the method signature compiles correctly.
	// It will fail at compile time if the method doesn't exist.
	var c *Client
	// Type assertion to verify method signature
	type immuneStatusMethod interface {
		ImmuneStatus() (*ImmuneStatusResponse, error)
	}
	var _ immuneStatusMethod = c
}
