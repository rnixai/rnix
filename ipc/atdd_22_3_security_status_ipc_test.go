package ipc

import (
	"encoding/json"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 22.3: 安全状态管理 IPC Protocol Tests
//
// 测试 ImmuneStatusResponse 扩展字段：UptimeMs、SuspendedPIDs、SecurityStatus。
// 测试引用尚不存在的新字段，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go, ipc/server.go 中实现。
// ============================================================

// --- 22.3-IPC-001: [P0] ImmuneStatusResponse contains UptimeMs field (AC1) ---

func TestImmuneStatusResponse_UptimeMs(t *testing.T) {
	// Given: an ImmuneStatusResponse with UptimeMs set
	resp := ImmuneStatusResponse{
		Running:      true,
		UptimeMs:     8100000, // 2h15m in milliseconds
		ProfileCount: 3,
		ActivePIDs:   []uint64{1, 2},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains uptime_ms field
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	uptimeMs, ok := m["uptime_ms"]
	if !ok {
		t.Fatal("missing 'uptime_ms' field in JSON")
	}
	if uptimeMs != float64(8100000) {
		t.Errorf("uptime_ms = %v, want 8100000", uptimeMs)
	}
}

// --- 22.3-IPC-002: [P0] ImmuneStatusResponse contains SuspendedPIDs field (AC3) ---

func TestImmuneStatusResponse_SuspendedPIDs(t *testing.T) {
	// Given: an ImmuneStatusResponse with SuspendedPIDs set
	resp := ImmuneStatusResponse{
		Running:       true,
		SuspendedPIDs: []uint64{42, 99},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains suspended_pids field
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	suspended, ok := m["suspended_pids"]
	if !ok {
		t.Fatal("missing 'suspended_pids' field in JSON")
	}
	arr, ok := suspended.([]any)
	if !ok {
		t.Fatalf("suspended_pids is not an array, got %T", suspended)
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 suspended PIDs, got %d", len(arr))
	}
}

// --- 22.3-IPC-003: [P0] ImmuneStatusResponse contains SecurityStatus field (AC5) ---

func TestImmuneStatusResponse_SecurityStatus(t *testing.T) {
	// Given: an ImmuneStatusResponse with SecurityStatus set
	resp := ImmuneStatusResponse{
		Running:        true,
		SecurityStatus: "ok",
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains security_status field
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	status, ok := m["security_status"]
	if !ok {
		t.Fatal("missing 'security_status' field in JSON")
	}
	if status != "ok" {
		t.Errorf("security_status = %v, want %q", status, "ok")
	}
}

// --- 22.3-IPC-004: [P0] ImmuneStatusResponse all new fields in JSON (AC6) ---

func TestImmuneStatusResponse_AllNewFields(t *testing.T) {
	// Given: a fully populated ImmuneStatusResponse with all 22.3 fields
	resp := ImmuneStatusResponse{
		Running:        true,
		UptimeMs:       3600000,
		ProfileCount:   5,
		ActivePIDs:     []uint64{1, 2, 3},
		SuspendedPIDs:  []uint64{42},
		Alerts:         []AlertWire{{PID: 42, Type: "syscall_freq", Detail: "anomaly", Deviation: 5.0, TimestampMs: 1710403800000}},
		ThreatCount:    10,
		SecurityStatus: "warning",
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains all fields
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	requiredFields := []string{
		"running", "uptime_ms", "profile_count", "active_pids",
		"suspended_pids", "alerts", "threat_count", "security_status",
	}
	for _, field := range requiredFields {
		if _, ok := m[field]; !ok {
			t.Errorf("missing required field %q in JSON", field)
		}
	}
}

// --- 22.3-IPC-005: [P1] ImmuneStatusResponse backward compatible with 22.2 format (AC6) ---

func TestImmuneStatusResponse_BackwardCompatible_22_3(t *testing.T) {
	// Given: a JSON from Story 22.2 format (no uptime_ms, suspended_pids, security_status)
	oldJSON := `{"running":true,"profile_count":2,"profiles":null,"active_pids":[1],"alerts":[],"threat_count":3}`

	// When: deserializing into updated ImmuneStatusResponse
	var resp ImmuneStatusResponse
	if err := json.Unmarshal([]byte(oldJSON), &resp); err != nil {
		t.Fatalf("Unmarshal old format failed: %v", err)
	}

	// Then: new fields have zero values
	if resp.UptimeMs != 0 {
		t.Errorf("UptimeMs should be 0 for old format, got %d", resp.UptimeMs)
	}
	if len(resp.SuspendedPIDs) != 0 {
		t.Errorf("SuspendedPIDs should be empty for old format, got %v", resp.SuspendedPIDs)
	}
	if resp.SecurityStatus != "" {
		t.Errorf("SecurityStatus should be empty for old format, got %q", resp.SecurityStatus)
	}

	// And: old fields are preserved
	if !resp.Running {
		t.Error("Running should be true")
	}
	if resp.ProfileCount != 2 {
		t.Errorf("ProfileCount = %d, want 2", resp.ProfileCount)
	}
	if resp.ThreatCount != 3 {
		t.Errorf("ThreatCount = %d, want 3", resp.ThreatCount)
	}
}
