package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.5: Skill 组合矩阵
//
// IPC 协议层测试：MethodSynergyList 常量、请求/响应类型。
// 测试引用尚不存在的 MethodSynergyList、SynergyListRequest、
// SynergyListResponse 类型，将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go 中添加常量和类型定义。
// ============================================================

// --- 21.5-IPC-001: [P0] MethodSynergyList constant value (AC2) ---

func TestMethodSynergyList_Constant(t *testing.T) {
	// Given: the MethodSynergyList constant
	// Then: it equals "synergy_list"
	if MethodSynergyList != "synergy_list" {
		t.Errorf("MethodSynergyList = %q, want %q", MethodSynergyList, "synergy_list")
	}
}

// --- 21.5-IPC-002: [P0] SynergyListRequest type exists (AC2) ---

func TestSynergyListRequest_TypeExists(t *testing.T) {
	// Given: a SynergyListRequest
	req := SynergyListRequest{}

	// When: marshaling to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal SynergyListRequest failed: %v", err)
	}

	// Then: valid JSON (empty object)
	if string(data) != "{}" {
		t.Errorf("expected empty JSON object, got %s", data)
	}
}

// --- 21.5-IPC-003: [P0] SynergyListResponse contains Combos field (AC2, AC5) ---

func TestSynergyListResponse_CombosField(t *testing.T) {
	// Given: a SynergyListResponse with combos
	resp := SynergyListResponse{
		Combos: []kernel.ComboSummary{
			{
				ComboKey:        kernel.NewComboKey([]string{"analysis", "review"}),
				Skills:          []string{"analysis", "review"},
				SuccessRate:     0.85,
				AvgTokens:       1200,
				TotalExecutions: 20,
				AvgSoloRate:     0.70,
				TokenImprovement: 12.3,
				Recommended:     true,
			},
		},
	}

	// When: marshaling to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal SynergyListResponse failed: %v", err)
	}

	// Then: JSON has "combos" field
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, ok := m["combos"]; !ok {
		t.Error("missing JSON field 'combos'")
	}

	// And: combos array has 1 element
	combos, ok := m["combos"].([]any)
	if !ok {
		t.Fatal("combos is not an array")
	}
	if len(combos) != 1 {
		t.Fatalf("expected 1 combo, got %d", len(combos))
	}
}

// --- 21.5-IPC-004: [P0] SynergyListResponse empty combos (AC6) ---

func TestSynergyListResponse_EmptyCombos(t *testing.T) {
	// Given: a SynergyListResponse with no combos
	resp := SynergyListResponse{
		Combos: []kernel.ComboSummary{},
	}

	// When: marshaling to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal SynergyListResponse failed: %v", err)
	}

	// Then: JSON has empty combos array (not null)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	combos, ok := m["combos"].([]any)
	if !ok {
		t.Fatal("combos should be an array, not null")
	}
	if len(combos) != 0 {
		t.Fatalf("expected 0 combos, got %d", len(combos))
	}
}

// --- 21.5-IPC-005: [P1] Client SynergyList method exists (AC2) ---

func TestClient_SynergyList_MethodExists(t *testing.T) {
	// This test verifies that the Client.SynergyList() method signature compiles.
	// Actual IPC call is not tested here (requires daemon).
	// The mere compilation of this test proves the method exists.
	var c *Client
	_ = c // Suppress unused variable. The method signature check happens at compile time.

	// Type assertion: SynergyList returns (*SynergyListResponse, error)
	fn := c.SynergyList
	_ = fn
}
