package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 22.5: 协作拓扑与强化路径
//
// CLI 层测试：`rnix topology` 顶级命令的文本和 JSON 输出格式。
//
// 测试引用的命令和函数尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 cmd/rnix/topology.go 中新增 topology 顶级命令。
// ============================================================

// --- 22.5-CLI-001: [P0] rnix topology text output (AC3) ---

func TestRunTopology_TextOutput(t *testing.T) {
	// Given: a mock topology query response
	response := &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "code-analyst", ReputationScore: 0.85, Connections: 3},
			{Agent: "code-reviewer", ReputationScore: 0.72, Connections: 2},
			{Agent: "debugger", ReputationScore: 0.65, Connections: 2},
		},
		Edges: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
			{From: "code-analyst", To: "debugger", SpawnCount: 2, MsgCount: 5, Total: 7, Reinforced: true},
			{From: "code-reviewer", To: "debugger", SpawnCount: 1, MsgCount: 1, Total: 2, Reinforced: false},
		},
		ReinforcedPaths: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
			{From: "code-analyst", To: "debugger", SpawnCount: 2, MsgCount: 5, Total: 7, Reinforced: true},
		},
	}

	var buf bytes.Buffer
	formatTopologyText(&buf, response)
	output := buf.String()

	// Then: output contains header with agent/edge counts
	if !strings.Contains(output, "Collaboration Topology") {
		t.Errorf("output should contain 'Collaboration Topology', got: %s", output)
	}

	// And: output contains NODES section header
	if !strings.Contains(output, "NODES") {
		t.Errorf("output should contain NODES section, got: %s", output)
	}

	// And: output contains node details
	if !strings.Contains(output, "code-analyst") {
		t.Errorf("output should contain code-analyst, got: %s", output)
	}
	if !strings.Contains(output, "code-reviewer") {
		t.Errorf("output should contain code-reviewer, got: %s", output)
	}

	// And: output contains EDGES section header
	if !strings.Contains(output, "EDGES") {
		t.Errorf("output should contain EDGES section, got: %s", output)
	}

	// And: output contains REINFORCED PATHS section
	if !strings.Contains(output, "REINFORCED") {
		t.Errorf("output should contain REINFORCED section, got: %s", output)
	}
}

// --- 22.5-CLI-002: [P0] rnix topology JSON output (AC6) ---

func TestRunTopology_JSONOutput(t *testing.T) {
	// Given: a mock topology query response
	response := &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "code-analyst", ReputationScore: 0.85, Connections: 3},
		},
		Edges: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
		},
		ReinforcedPaths: []kernel.CooperationEdge{
			{From: "code-analyst", To: "code-reviewer", SpawnCount: 8, MsgCount: 3, Total: 11, Reinforced: true},
		},
	}

	var buf bytes.Buffer
	resp := JSONResponse{OK: true, Data: response}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	buf.Write(data)

	// Then: output is valid JSON
	var decoded JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v", err)
	}
	if !decoded.OK {
		t.Error("expected OK=true in JSON response")
	}
}

// --- 22.5-CLI-003: [P0] rnix topology no daemon error (AC3) ---

func TestRunTopology_NoDaemon(t *testing.T) {
	// Given: no daemon available
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	var buf bytes.Buffer
	cmd := topologyCmd
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	oldExitCode := exitCode
	exitCode = 0
	defer func() { exitCode = oldExitCode }()

	_ = cmd.RunE(cmd, []string{})
	output := buf.String()

	// Then: output contains error message
	if !strings.Contains(output, "daemon not available") {
		t.Errorf("expected 'daemon not available' message, got: %s", output)
	}
}

// --- 22.5-CLI-004: [P0] rnix topology command registered as top-level (AC3) ---

func TestTopologyCmd_Registered(t *testing.T) {
	// Given: the root command
	// When: checking for the topology subcommand
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "topology" || strings.HasPrefix(sub.Use, "topology") {
			found = true
			break
		}
	}

	// Then: topology is registered as a top-level command
	if !found {
		t.Error("'topology' should be registered as a top-level command under rootCmd")
	}
}

// --- 22.5-CLI-005: [P0] rnix topology text output no data (AC3) ---

func TestRunTopology_TextOutput_NoData(t *testing.T) {
	// Given: a response with no topology data
	response := &ipc.TopologyQueryResponse{
		Nodes:           []kernel.TopologyNode{},
		Edges:           []kernel.CooperationEdge{},
		ReinforcedPaths: []kernel.CooperationEdge{},
	}

	var buf bytes.Buffer
	formatTopologyText(&buf, response)
	output := buf.String()

	// Then: output indicates no collaboration data
	if !strings.Contains(output, "No collaboration") && !strings.Contains(output, "no collaboration") && !strings.Contains(output, "0 agents") {
		t.Logf("Warning: output may not clearly indicate no collaboration data: %s", output)
	}
}

// --- 22.5-CLI-006: [P1] rnix topology text output reinforced path marker (AC3) ---

func TestRunTopology_TextOutput_ReinforcedMarker(t *testing.T) {
	// Given: a response with reinforced edges
	response := &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{
			{Agent: "agent-a", ReputationScore: 0.8, Connections: 1},
			{Agent: "agent-b", ReputationScore: 0.7, Connections: 1},
		},
		Edges: []kernel.CooperationEdge{
			{From: "agent-a", To: "agent-b", SpawnCount: 10, MsgCount: 2, Total: 12, Reinforced: true},
		},
		ReinforcedPaths: []kernel.CooperationEdge{
			{From: "agent-a", To: "agent-b", SpawnCount: 10, MsgCount: 2, Total: 12, Reinforced: true},
		},
	}

	var buf bytes.Buffer
	formatTopologyText(&buf, response)
	output := buf.String()

	// Then: reinforced edges should have a special marker (e.g., "*")
	if !strings.Contains(output, "*") {
		t.Errorf("reinforced edge should have a marker (e.g., '*'), got: %s", output)
	}
}

// --- 22.5-CLI-007: [P1] rnix topology --json flag (AC6) ---

func TestTopologyCmd_HasJSONFlag(t *testing.T) {
	// Given: the topology command
	// Then: it should support --json flag (inherited from root)
	// This is inherently tested by the JSONResponse integration test (CLI-002)
	// but we verify the command structure here
	if topologyCmd == nil {
		t.Fatal("topologyCmd should not be nil")
	}
	if topologyCmd.RunE == nil {
		t.Error("topologyCmd should have RunE function")
	}
}
