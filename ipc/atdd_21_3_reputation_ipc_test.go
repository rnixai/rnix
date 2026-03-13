package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.3: 声誉系统与自动选择
//
// IPC 声誉查询测试。
// 测试引用 MethodReputationStatus, ReputationStatusRequest,
// ReputationStatusResponse 等类型和方法，
// 这些类型/方法尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go 添加类型，
//              在 ipc/server.go 注册 handler，
//              在 ipc/client.go 添加客户端方法，
//              测试编译并通过。
// ============================================================

// --- 21.3-IPC-001: [P0] ReputationStatusRequest/Response types exist (AC3) ---

func TestReputationStatus_TypesExist(t *testing.T) {
	// Given: ReputationStatusRequest and ReputationStatusResponse are defined
	req := ReputationStatusRequest{
		AgentName: "test-agent",
	}
	resp := ReputationStatusResponse{
		Summaries: []kernel.ReputationSummary{
			{
				AgentName:   "test-agent",
				Score:       0.85,
				SuccessRate: 0.9,
				AvgTokens:   5000,
				AvgDurationMs: 30000,
				TotalRecords: 10,
				RecentTrend: "stable",
			},
		},
	}

	// Then: types should serialize correctly
	reqData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal ReputationStatusRequest: %v", err)
	}
	if len(reqData) == 0 {
		t.Fatal("expected non-empty ReputationStatusRequest JSON")
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal ReputationStatusResponse: %v", err)
	}
	if len(respData) == 0 {
		t.Fatal("expected non-empty ReputationStatusResponse JSON")
	}
}

// --- 21.3-IPC-002: [P0] MethodReputationStatus constant exists (AC3) ---

func TestReputationStatus_MethodConstant(t *testing.T) {
	// Given: MethodReputationStatus is defined
	method := MethodReputationStatus

	// Then: it should be "reputation_status"
	if method != "reputation_status" {
		t.Fatalf("expected MethodReputationStatus 'reputation_status', got %q", method)
	}
}

// --- 21.3-IPC-003: [P0] ReputationStatusRequest empty AgentName means all agents (AC3) ---

func TestReputationStatus_EmptyAgentName(t *testing.T) {
	req := ReputationStatusRequest{
		AgentName: "",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ReputationStatusRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Empty AgentName should survive serialization (omitempty)
	if decoded.AgentName != "" {
		t.Fatalf("expected empty AgentName after round-trip, got %q", decoded.AgentName)
	}
}

// --- 21.3-IPC-004: [P1] ReputationStatusResponse with multiple summaries (AC3) ---

func TestReputationStatus_MultipleSummaries(t *testing.T) {
	resp := ReputationStatusResponse{
		Summaries: []kernel.ReputationSummary{
			{AgentName: "agent-a", Score: 0.9, SuccessRate: 0.95, AvgTokens: 3000, TotalRecords: 20, RecentTrend: "improving"},
			{AgentName: "agent-b", Score: 0.6, SuccessRate: 0.7, AvgTokens: 8000, TotalRecords: 15, RecentTrend: "stable"},
			{AgentName: "agent-c", Score: 0.3, SuccessRate: 0.4, AvgTokens: 15000, TotalRecords: 10, RecentTrend: "declining"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ReputationStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(decoded.Summaries) != 3 {
		t.Fatalf("expected 3 summaries after round-trip, got %d", len(decoded.Summaries))
	}

	if decoded.Summaries[0].AgentName != "agent-a" {
		t.Fatalf("expected first summary 'agent-a', got %q", decoded.Summaries[0].AgentName)
	}
	if decoded.Summaries[2].RecentTrend != "declining" {
		t.Fatalf("expected third summary trend 'declining', got %q", decoded.Summaries[2].RecentTrend)
	}
}
