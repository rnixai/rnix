package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 22.4: 能力迁移与相似度矩阵
//
// IPC 协议层测试：SimilarityQueryRequest/Response 序列化、
// MethodSimilarityQuery 常量、向后兼容性。
//
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go 中新增类型和常量。
// ============================================================

// --- 22.4-IPC-001: [P0] MethodSimilarityQuery constant exists (AC6) ---

func TestMethodSimilarityQuery_Constant(t *testing.T) {
	// Given/When: the constant is defined
	// Then: it equals "similarity_query"
	if MethodSimilarityQuery != "similarity_query" {
		t.Errorf("MethodSimilarityQuery = %q, want %q", MethodSimilarityQuery, "similarity_query")
	}
}

// --- 22.4-IPC-002: [P0] SimilarityQueryRequest JSON serialization (AC6) ---

func TestSimilarityQueryRequest_Serialization(t *testing.T) {
	// Given: a SimilarityQueryRequest
	req := SimilarityQueryRequest{
		AgentName: "code-analyst",
		MinScore:  0.3,
	}

	// When: serializing to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: deserializing produces the same result
	var decoded SimilarityQueryRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.AgentName != "code-analyst" {
		t.Errorf("AgentName = %q, want %q", decoded.AgentName, "code-analyst")
	}
	if decoded.MinScore != 0.3 {
		t.Errorf("MinScore = %f, want 0.3", decoded.MinScore)
	}
}

// --- 22.4-IPC-003: [P0] SimilarityQueryResponse JSON serialization (AC6) ---

func TestSimilarityQueryResponse_Serialization(t *testing.T) {
	// Given: a SimilarityQueryResponse
	resp := SimilarityQueryResponse{
		Agent: "code-analyst",
		Similarities: []kernel.CapabilitySimilarity{
			{
				AgentA:     "code-analyst",
				AgentB:     "code-reviewer",
				SkillScore: 0.75,
				CoopScore:  0.5,
				Score:      0.675,
			},
		},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: deserializing produces the same result
	var decoded SimilarityQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.Agent != "code-analyst" {
		t.Errorf("Agent = %q, want %q", decoded.Agent, "code-analyst")
	}
	if len(decoded.Similarities) != 1 {
		t.Fatalf("expected 1 similarity, got %d", len(decoded.Similarities))
	}
	s := decoded.Similarities[0]
	if s.AgentB != "code-reviewer" {
		t.Errorf("Similarities[0].AgentB = %q, want %q", s.AgentB, "code-reviewer")
	}
	if s.SkillScore != 0.75 {
		t.Errorf("Similarities[0].SkillScore = %f, want 0.75", s.SkillScore)
	}
	if s.Score != 0.675 {
		t.Errorf("Similarities[0].Score = %f, want 0.675", s.Score)
	}
}

// --- 22.4-IPC-004: [P0] SimilarityQueryRequest JSON field names (AC6) ---

func TestSimilarityQueryRequest_JSONFieldNames(t *testing.T) {
	// Given: a SimilarityQueryRequest
	req := SimilarityQueryRequest{
		AgentName: "test-agent",
		MinScore:  0.5,
	}

	// When: serializing to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON uses snake_case field names
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, ok := raw["agent_name"]; !ok {
		t.Error("JSON should contain 'agent_name' field")
	}
	if _, ok := raw["min_score"]; !ok {
		t.Error("JSON should contain 'min_score' field")
	}
}

// --- 22.4-IPC-005: [P1] SimilarityQueryResponse empty similarities (AC6) ---

func TestSimilarityQueryResponse_EmptySimilarities(t *testing.T) {
	// Given: a response with no similarities
	resp := SimilarityQueryResponse{
		Agent:        "unknown-agent",
		Similarities: []kernel.CapabilitySimilarity{},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: similarities field is an empty array (not null)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	similarities, ok := raw["similarities"]
	if !ok {
		t.Fatal("JSON should contain 'similarities' field")
	}
	arr, ok := similarities.([]any)
	if !ok {
		t.Fatal("similarities should be an array")
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got length %d", len(arr))
	}
}

// --- 22.4-IPC-006: [P0] SimilarityQueryResponse backward compatible (AC6) ---

func TestSimilarityQueryResponse_BackwardCompatible(t *testing.T) {
	// Given: a JSON response from an older version (no similarities field)
	oldJSON := `{"agent":"test-agent"}`

	// When: deserializing
	var resp SimilarityQueryResponse
	if err := json.Unmarshal([]byte(oldJSON), &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: Similarities defaults to nil/zero value
	if resp.Agent != "test-agent" {
		t.Errorf("Agent = %q, want %q", resp.Agent, "test-agent")
	}
	if resp.Similarities != nil {
		t.Errorf("Similarities should be nil for backward compatible JSON, got %v", resp.Similarities)
	}
}
