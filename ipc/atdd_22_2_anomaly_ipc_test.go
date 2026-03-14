package ipc

import (
	"encoding/json"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 22.2: 异常检测与威胁记忆 IPC Protocol Tests
//
// 测试 MethodImmuneResume 常量、ImmuneResumeRequest/Response 类型、
// AlertWire 类型以及 ImmuneStatusResponse 扩展字段。
//
// RED -> GREEN: 在 ipc/protocol.go, ipc/server.go, ipc/client.go 中实现。
// ============================================================

// --- 22.2-IPC-001: [P0] MethodImmuneResume constant value (AC3) ---

func TestMethodImmuneResume_Constant(t *testing.T) {
	// Then: MethodImmuneResume equals "immune_resume"
	if MethodImmuneResume != "immune_resume" {
		t.Errorf("MethodImmuneResume = %q, want %q", MethodImmuneResume, "immune_resume")
	}
}

// --- 22.2-IPC-002: [P0] ImmuneResumeRequest type and JSON fields (AC3) ---

func TestImmuneResumeRequest_JSON(t *testing.T) {
	// Given: an ImmuneResumeRequest
	req := ImmuneResumeRequest{PID: 42}

	// When: serializing to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains pid field
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m["pid"] != float64(42) {
		t.Errorf("pid = %v, want 42", m["pid"])
	}
}

// --- 22.2-IPC-003: [P0] ImmuneResumeResponse type and JSON fields (AC3) ---

func TestImmuneResumeResponse_JSON(t *testing.T) {
	// Given: an ImmuneResumeResponse
	resp := ImmuneResumeResponse{
		OK:      true,
		Message: "Process 42 resumed successfully.",
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
	if m["ok"] != true {
		t.Errorf("ok = %v, want true", m["ok"])
	}
	if m["message"] != "Process 42 resumed successfully." {
		t.Errorf("message = %v, want correct resume message", m["message"])
	}
}

// --- 22.2-IPC-004: [P0] AlertWire type and JSON fields (AC4) ---

func TestAlertWire_JSON(t *testing.T) {
	// Given: an AlertWire
	alert := AlertWire{
		PID:           42,
		AgentTemplate: "code-analyst",
		Type:          "syscall_freq",
		Detail:        "Open 频率 12.0 是基线 2.4 的 5.0 倍",
		Deviation:     5.0,
		TimestampMs:   1710403800000,
	}

	// When: serializing to JSON
	data, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains expected snake_case fields
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m["pid"] != float64(42) {
		t.Errorf("pid = %v, want 42", m["pid"])
	}
	if m["agent_template"] != "code-analyst" {
		t.Errorf("agent_template = %v, want code-analyst", m["agent_template"])
	}
	if m["type"] != "syscall_freq" {
		t.Errorf("type = %v, want syscall_freq", m["type"])
	}
	if m["deviation"] != float64(5.0) {
		t.Errorf("deviation = %v, want 5.0", m["deviation"])
	}
	if m["timestamp_ms"] != float64(1710403800000) {
		t.Errorf("timestamp_ms = %v, want 1710403800000", m["timestamp_ms"])
	}
}

// --- 22.2-IPC-005: [P0] ImmuneStatusResponse extended with Alerts and ThreatCount fields (AC4) ---

func TestImmuneStatusResponse_ExtendedFields(t *testing.T) {
	// Given: an ImmuneStatusResponse with new fields
	resp := ImmuneStatusResponse{
		Running:      true,
		ProfileCount: 3,
		ActivePIDs:   []uint64{1, 2},
		Alerts: []AlertWire{
			{
				PID:           42,
				AgentTemplate: "code-analyst",
				Type:          "syscall_freq",
				Detail:        "anomaly detected",
				Deviation:     5.0,
				TimestampMs:   1710403800000,
			},
		},
		ThreatCount: 5,
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains the new fields
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// And: alerts field exists and has correct length
	alerts, ok := m["alerts"]
	if !ok {
		t.Fatal("missing 'alerts' field in JSON")
	}
	alertArr, ok := alerts.([]any)
	if !ok {
		t.Fatalf("alerts is not an array, got %T", alerts)
	}
	if len(alertArr) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alertArr))
	}

	// And: threat_count field exists
	if m["threat_count"] != float64(5) {
		t.Errorf("threat_count = %v, want 5", m["threat_count"])
	}
}

// --- 22.2-IPC-006: [P1] ImmuneStatusResponse backward compatible with empty new fields (AC6) ---

func TestImmuneStatusResponse_BackwardCompatible(t *testing.T) {
	// Given: a JSON from old format (no alerts, no threat_count)
	oldJSON := `{"running":true,"profile_count":2,"profiles":null,"active_pids":[1]}`

	// When: deserializing into updated ImmuneStatusResponse
	var resp ImmuneStatusResponse
	if err := json.Unmarshal([]byte(oldJSON), &resp); err != nil {
		t.Fatalf("Unmarshal old format failed: %v", err)
	}

	// Then: new fields have zero values
	if len(resp.Alerts) != 0 {
		t.Errorf("Alerts should be nil or empty for old format, got %d", len(resp.Alerts))
	}
	if resp.ThreatCount != 0 {
		t.Errorf("ThreatCount should be 0 for old format, got %d", resp.ThreatCount)
	}
	// And: old fields are preserved
	if !resp.Running {
		t.Error("Running should be true")
	}
	if resp.ProfileCount != 2 {
		t.Errorf("ProfileCount = %d, want 2", resp.ProfileCount)
	}
}

// --- 22.2-IPC-007: [P1] Client.ImmuneResume method exists (AC3) ---

func TestClient_ImmuneResume_MethodExists(t *testing.T) {
	// Given: a Client with no connection (method existence check only)
	var c *Client
	// Type assertion to verify method signature
	type immuneResumeMethod interface {
		ImmuneResume(pid uint64) (*ImmuneResumeResponse, error)
	}
	var _ immuneResumeMethod = c
}
