package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 22.5: 协作拓扑与强化路径
//
// IPC 协议层测试：TopologyQueryRequest/Response 序列化、
// MethodTopologyQuery 常量、向后兼容性。
//
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 ipc/protocol.go 中新增类型和常量。
// ============================================================

// --- 22.5-IPC-001: [P0] MethodTopologyQuery constant exists (AC5) ---

func TestMethodTopologyQuery_Constant(t *testing.T) {
	// Given/When: the constant is defined
	// Then: it equals "topology_query"
	if MethodTopologyQuery != "topology_query" {
		t.Errorf("MethodTopologyQuery = %q, want %q", MethodTopologyQuery, "topology_query")
	}
}

// --- 22.5-IPC-002: [P0] TopologyQueryResponse JSON serialization (AC5) ---

func TestTopologyQueryResponse_Serialization(t *testing.T) {
	// Given: a TopologyQueryResponse
	resp := TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "code-analyst", ReputationScore: 0.85, Connections: 3},
			{Agent: "code-reviewer", ReputationScore: 0.72, Connections: 2},
		},
		Edges: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
		},
		ReinforcedPaths: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
		},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: deserializing produces the same result
	var decoded TopologyQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(decoded.Nodes))
	}
	if decoded.Nodes[0].Agent != "code-analyst" {
		t.Errorf("Nodes[0].Agent = %q, want %q", decoded.Nodes[0].Agent, "code-analyst")
	}
	if decoded.Nodes[0].ReputationScore != 0.85 {
		t.Errorf("Nodes[0].ReputationScore = %f, want 0.85", decoded.Nodes[0].ReputationScore)
	}

	if len(decoded.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(decoded.Edges))
	}
	edge := decoded.Edges[0]
	if edge.From != "code-analyst" {
		t.Errorf("Edges[0].From = %q, want %q", edge.From, "code-analyst")
	}
	if edge.SpawnCount != 8 {
		t.Errorf("Edges[0].SpawnCount = %d, want 8", edge.SpawnCount)
	}
	if edge.Total != 11 {
		t.Errorf("Edges[0].Total = %d, want 11", edge.Total)
	}
	if !edge.Reinforced {
		t.Error("Edges[0].Reinforced should be true")
	}

	if len(decoded.ReinforcedPaths) != 1 {
		t.Fatalf("expected 1 reinforced path, got %d", len(decoded.ReinforcedPaths))
	}
}

// --- 22.5-IPC-003: [P0] TopologyQueryResponse JSON field names use snake_case (AC5) ---

func TestTopologyQueryResponse_JSONFieldNames(t *testing.T) {
	// Given: a TopologyQueryResponse
	resp := TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "test-agent", ReputationScore: 0.5, Connections: 1},
		},
		Edges: []kernel.CooperationEdge{
			{From: "a", To: "b", SpawnCount: 1, MsgCount: 2, Total: 3, Reinforced: false},
		},
		ReinforcedPaths: []kernel.CooperationEdge{},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON uses snake_case field names
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// Check top-level fields
	if _, ok := raw["nodes"]; !ok {
		t.Error("JSON should contain 'nodes' field")
	}
	if _, ok := raw["edges"]; !ok {
		t.Error("JSON should contain 'edges' field")
	}
	if _, ok := raw["reinforced_paths"]; !ok {
		t.Error("JSON should contain 'reinforced_paths' field")
	}

	// Check edge field names
	edges, ok := raw["edges"].([]any)
	if !ok || len(edges) == 0 {
		t.Fatal("expected edges array with at least 1 element")
	}
	edgeMap, ok := edges[0].(map[string]any)
	if !ok {
		t.Fatal("expected edge to be a map")
	}
	for _, field := range []string{"from", "to", "spawn_count", "msg_count", "total", "reinforced"} {
		if _, ok := edgeMap[field]; !ok {
			t.Errorf("edge JSON should contain '%s' field", field)
		}
	}

	// Check node field names
	nodes, ok := raw["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		t.Fatal("expected nodes array with at least 1 element")
	}
	nodeMap, ok := nodes[0].(map[string]any)
	if !ok {
		t.Fatal("expected node to be a map")
	}
	for _, field := range []string{"agent", "reputation_score", "connections"} {
		if _, ok := nodeMap[field]; !ok {
			t.Errorf("node JSON should contain '%s' field", field)
		}
	}
}

// --- 22.5-IPC-004: [P1] TopologyQueryResponse empty topology (AC5) ---

func TestTopologyQueryResponse_EmptyTopology(t *testing.T) {
	// Given: a response with no topology data
	resp := TopologyQueryResponse{
		Nodes:           []kernel.TopologyNode{},
		Edges:           []kernel.CooperationEdge{},
		ReinforcedPaths: []kernel.CooperationEdge{},
	}

	// When: serializing to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: arrays are empty [] not null
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	for _, field := range []string{"nodes", "edges", "reinforced_paths"} {
		arr, ok := raw[field].([]any)
		if !ok {
			t.Fatalf("JSON field '%s' should be an array", field)
		}
		if len(arr) != 0 {
			t.Errorf("JSON field '%s' should be empty array, got length %d", field, len(arr))
		}
	}
}

// --- 22.5-IPC-005: [P0] TopologyQueryResponse backward compatible (AC5) ---

func TestTopologyQueryResponse_BackwardCompatible(t *testing.T) {
	// Given: a JSON response from an older version (missing reinforced_paths)
	oldJSON := `{"nodes":[],"edges":[]}`

	// When: deserializing
	var resp TopologyQueryResponse
	if err := json.Unmarshal([]byte(oldJSON), &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: ReinforcedPaths defaults to nil/zero value
	if resp.ReinforcedPaths != nil {
		t.Errorf("ReinforcedPaths should be nil for backward compatible JSON, got %v", resp.ReinforcedPaths)
	}
}

// --- 22.5-IPC-006: [P0] TopologyQueryRequest is empty struct (AC5) ---

func TestTopologyQueryRequest_Empty(t *testing.T) {
	// Given: a TopologyQueryRequest (empty struct per design)
	req := TopologyQueryRequest{}

	// When: serializing to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: produces valid empty JSON object
	var decoded TopologyQueryRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
}
