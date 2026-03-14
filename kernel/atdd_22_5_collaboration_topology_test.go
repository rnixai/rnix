package kernel

import (
	"sync"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 22.5: 协作拓扑与强化路径
//
// CooperationEdge, TopologyNode, CollaborationTopology 以及
// ImmuneDaemon 集成协作拓扑和强化路径识别的测试。
//
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 kernel/immune.go 中实现所有新增类型和方法。
// ============================================================

// --- 22.5-UNIT-001: [P0] DefaultReinforcementThreshold constant exists (AC2) ---

func TestDefaultReinforcementThreshold_Value(t *testing.T) {
	// Given/When: the constant is defined
	// Then: it equals 5
	if DefaultReinforcementThreshold != 5 {
		t.Errorf("DefaultReinforcementThreshold = %d, want 5", DefaultReinforcementThreshold)
	}
}

// --- 22.5-UNIT-002: [P0] CooperationEdge struct has required fields (AC1) ---

func TestCooperationEdge_Fields(t *testing.T) {
	// Given: a CooperationEdge instance
	edge := CooperationEdge{
		From:       "code-analyst",
		To:         "code-reviewer",
		SpawnCount: 8,
		MsgCount:   3,
		Total:      11,
		Reinforced: true,
	}

	// Then: all fields are accessible and correct
	if edge.From != "code-analyst" {
		t.Errorf("From = %q, want %q", edge.From, "code-analyst")
	}
	if edge.To != "code-reviewer" {
		t.Errorf("To = %q, want %q", edge.To, "code-reviewer")
	}
	if edge.SpawnCount != 8 {
		t.Errorf("SpawnCount = %d, want 8", edge.SpawnCount)
	}
	if edge.MsgCount != 3 {
		t.Errorf("MsgCount = %d, want 3", edge.MsgCount)
	}
	if edge.Total != 11 {
		t.Errorf("Total = %d, want 11", edge.Total)
	}
	if !edge.Reinforced {
		t.Error("Reinforced should be true")
	}
}

// --- 22.5-UNIT-003: [P0] TopologyNode struct has required fields (AC3) ---

func TestTopologyNode_Fields(t *testing.T) {
	// Given: a TopologyNode instance
	node := TopologyNode{
		Agent:           "code-analyst",
		ReputationScore: 0.85,
		Connections:     3,
	}

	// Then: all fields are accessible and correct
	if node.Agent != "code-analyst" {
		t.Errorf("Agent = %q, want %q", node.Agent, "code-analyst")
	}
	if node.ReputationScore != 0.85 {
		t.Errorf("ReputationScore = %f, want 0.85", node.ReputationScore)
	}
	if node.Connections != 3 {
		t.Errorf("Connections = %d, want 3", node.Connections)
	}
}

// --- 22.5-UNIT-004: [P0] CollaborationTopology struct has required fields (AC3) ---

func TestCollaborationTopology_Fields(t *testing.T) {
	// Given: a CollaborationTopology instance
	topo := CollaborationTopology{
		Nodes: []TopologyNode{
			{Agent: "agent-a", ReputationScore: 0.9, Connections: 2},
		},
		Edges: []CooperationEdge{
			{From: "agent-a", To: "agent-b", SpawnCount: 3, MsgCount: 2, Total: 5, Reinforced: true},
		},
		ReinforcedPaths: []CooperationEdge{
			{From: "agent-a", To: "agent-b", SpawnCount: 3, MsgCount: 2, Total: 5, Reinforced: true},
		},
	}

	// Then: all fields are accessible and correct
	if len(topo.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(topo.Nodes))
	}
	if topo.Nodes[0].Agent != "agent-a" {
		t.Errorf("Nodes[0].Agent = %q, want %q", topo.Nodes[0].Agent, "agent-a")
	}
	if len(topo.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(topo.Edges))
	}
	if topo.Edges[0].From != "agent-a" {
		t.Errorf("Edges[0].From = %q, want %q", topo.Edges[0].From, "agent-a")
	}
	if len(topo.ReinforcedPaths) != 1 {
		t.Fatalf("expected 1 reinforced path, got %d", len(topo.ReinforcedPaths))
	}
}

// --- 22.5-UNIT-005: [P0] RecordCooperationTyped records spawn type correctly (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_Spawn(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording typed cooperation events (spawn)
	daemon.RecordCooperationTyped("parent-agent", "child-agent", "spawn")
	daemon.RecordCooperationTyped("parent-agent", "child-agent", "spawn")
	daemon.RecordCooperationTyped("parent-agent", "child-agent", "spawn")

	// Then: topology should reflect spawn counts
	topo := daemon.GetTopology()
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	found := false
	for _, edge := range topo.Edges {
		if (edge.From == "parent-agent" && edge.To == "child-agent") ||
			(edge.From == "child-agent" && edge.To == "parent-agent") {
			found = true
			if edge.SpawnCount != 3 {
				t.Errorf("SpawnCount = %d, want 3", edge.SpawnCount)
			}
			if edge.MsgCount != 0 {
				t.Errorf("MsgCount = %d, want 0", edge.MsgCount)
			}
			if edge.Total != 3 {
				t.Errorf("Total = %d, want 3", edge.Total)
			}
			break
		}
	}
	if !found {
		t.Error("expected edge between parent-agent and child-agent")
	}
}

// --- 22.5-UNIT-006: [P0] RecordCooperationTyped records msg type correctly (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_Msg(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording typed cooperation events (msg)
	daemon.RecordCooperationTyped("sender", "receiver", "msg")
	daemon.RecordCooperationTyped("sender", "receiver", "msg")

	// Then: topology should reflect msg counts
	topo := daemon.GetTopology()
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	found := false
	for _, edge := range topo.Edges {
		if (edge.From == "sender" && edge.To == "receiver") ||
			(edge.From == "receiver" && edge.To == "sender") {
			found = true
			if edge.MsgCount != 2 {
				t.Errorf("MsgCount = %d, want 2", edge.MsgCount)
			}
			if edge.SpawnCount != 0 {
				t.Errorf("SpawnCount = %d, want 0", edge.SpawnCount)
			}
			if edge.Total != 2 {
				t.Errorf("Total = %d, want 2", edge.Total)
			}
			break
		}
	}
	if !found {
		t.Error("expected edge between sender and receiver")
	}
}

// --- 22.5-UNIT-007: [P0] RecordCooperationTyped mixed types (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_Mixed(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording both spawn and msg cooperation events
	daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	daemon.RecordCooperationTyped("agent-a", "agent-b", "msg")
	daemon.RecordCooperationTyped("agent-a", "agent-b", "msg")
	daemon.RecordCooperationTyped("agent-a", "agent-b", "msg")

	// Then: topology should reflect both types with correct counts
	topo := daemon.GetTopology()
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	found := false
	for _, edge := range topo.Edges {
		if (edge.From == "agent-a" && edge.To == "agent-b") ||
			(edge.From == "agent-b" && edge.To == "agent-a") {
			found = true
			if edge.SpawnCount != 2 {
				t.Errorf("SpawnCount = %d, want 2", edge.SpawnCount)
			}
			if edge.MsgCount != 3 {
				t.Errorf("MsgCount = %d, want 3", edge.MsgCount)
			}
			if edge.Total != 5 {
				t.Errorf("Total = %d, want 5 (2 spawn + 3 msg)", edge.Total)
			}
			break
		}
	}
	if !found {
		t.Error("expected edge between agent-a and agent-b")
	}
}

// --- 22.5-UNIT-008: [P0] RecordCooperationTyped also calls RecordCooperation for backward compat (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_BackwardCompat(t *testing.T) {
	// Given: a running ImmuneDaemon with similarity matrix
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording typed cooperation events
	daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	daemon.RecordCooperationTyped("agent-a", "agent-b", "msg")

	// And: updating similarity matrix (which uses coopHistory)
	agents := map[string][]string{
		"agent-a": {"skill-1"},
		"agent-b": {"skill-2"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Then: similarity should have coop score > 0 (backward compat with 22.4 coopHistory)
	sim := daemon.GetSimilarity("agent-a", "agent-b")
	if sim == nil {
		t.Fatal("expected non-nil similarity")
	}
	if sim.CoopScore <= 0.0 {
		t.Errorf("CoopScore = %f, want > 0 (RecordCooperationTyped should also update coopHistory)", sim.CoopScore)
	}
}

// --- 22.5-UNIT-009: [P0] RecordCooperationTyped nil daemon safe (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When/Then: no panic
	daemon.RecordCooperationTyped("a", "b", "spawn")
}

// --- 22.5-UNIT-010: [P0] GetTopology basic construction (AC3) ---

func TestImmuneDaemon_GetTopology_Basic(t *testing.T) {
	// Given: a running ImmuneDaemon with cooperation history
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Record cooperation events between 3 agents
	daemon.RecordCooperationTyped("analyst", "reviewer", "spawn")
	daemon.RecordCooperationTyped("analyst", "reviewer", "msg")
	daemon.RecordCooperationTyped("analyst", "debugger", "msg")

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: topology should have nodes and edges
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	// Check nodes
	if len(topo.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(topo.Nodes))
	}

	// Check edges
	if len(topo.Edges) < 2 {
		t.Errorf("expected at least 2 edges, got %d", len(topo.Edges))
	}

	// Verify node names
	nodeNames := make(map[string]bool)
	for _, n := range topo.Nodes {
		nodeNames[n.Agent] = true
	}
	for _, name := range []string{"analyst", "reviewer", "debugger"} {
		if !nodeNames[name] {
			t.Errorf("expected node %q in topology", name)
		}
	}
}

// --- 22.5-UNIT-011: [P0] GetTopology with reputation scores (AC3) ---

func TestImmuneDaemon_GetTopology_WithReputation(t *testing.T) {
	// Given: a running ImmuneDaemon with cooperation and reputation data
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Setup reputation store
	repDir := t.TempDir()
	repStore := NewReputationStore(repDir)
	for range 5 {
		_ = repStore.RecordResult("analyst", &SLAResult{AgentName: "analyst", Passed: true, DurationMs: 100, TokensUsed: 50})
	}
	daemon.SetReputationStore(repStore)

	// Record cooperation
	daemon.RecordCooperationTyped("analyst", "reviewer", "spawn")

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: node reputation scores should be populated
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	foundAnalyst := false
	for _, node := range topo.Nodes {
		if node.Agent == "analyst" {
			foundAnalyst = true
			if node.ReputationScore <= 0 {
				t.Errorf("analyst ReputationScore = %f, want > 0", node.ReputationScore)
			}
			break
		}
	}
	if !foundAnalyst {
		t.Error("expected analyst node in topology")
	}
}

// --- 22.5-UNIT-012: [P0] GetTopology reinforced paths marked (AC2) ---

func TestImmuneDaemon_GetTopology_ReinforcedPaths(t *testing.T) {
	// Given: a running ImmuneDaemon with high-frequency cooperation
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Record >= DefaultReinforcementThreshold cooperation events for one pair
	for range 6 { // 6 >= 5 (threshold)
		daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	}

	// Record < threshold for another pair
	for range 3 { // 3 < 5 (threshold)
		daemon.RecordCooperationTyped("agent-a", "agent-c", "msg")
	}

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: a-b edge should be reinforced, a-c should not
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	for _, edge := range topo.Edges {
		if (edge.From == "agent-a" && edge.To == "agent-b") ||
			(edge.From == "agent-b" && edge.To == "agent-a") {
			if !edge.Reinforced {
				t.Error("edge agent-a <-> agent-b should be reinforced (6 >= threshold 5)")
			}
			if edge.Total < 6 {
				t.Errorf("edge agent-a <-> agent-b Total = %d, want >= 6", edge.Total)
			}
		}
		if (edge.From == "agent-a" && edge.To == "agent-c") ||
			(edge.From == "agent-c" && edge.To == "agent-a") {
			if edge.Reinforced {
				t.Error("edge agent-a <-> agent-c should NOT be reinforced (3 < threshold 5)")
			}
		}
	}

	// ReinforcedPaths should only contain the a-b edge
	if len(topo.ReinforcedPaths) != 1 {
		t.Errorf("expected 1 reinforced path, got %d", len(topo.ReinforcedPaths))
	}
	if len(topo.ReinforcedPaths) > 0 {
		rp := topo.ReinforcedPaths[0]
		if (rp.From != "agent-a" || rp.To != "agent-b") && (rp.From != "agent-b" || rp.To != "agent-a") {
			t.Errorf("expected reinforced path between agent-a and agent-b, got %s -> %s", rp.From, rp.To)
		}
	}
}

// --- 22.5-UNIT-013: [P0] GetTopology nil daemon returns nil (AC3) ---

func TestImmuneDaemon_GetTopology_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: returns nil without panic
	if topo != nil {
		t.Errorf("expected nil from nil daemon, got %v", topo)
	}
}

// --- 22.5-UNIT-014: [P0] GetTopology empty history returns empty topology (AC3) ---

func TestImmuneDaemon_GetTopology_Empty(t *testing.T) {
	// Given: a running ImmuneDaemon with no cooperation history
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: returns non-nil but empty topology
	if topo == nil {
		t.Fatal("expected non-nil topology (empty, not nil)")
	}
	if len(topo.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(topo.Edges))
	}
	if len(topo.ReinforcedPaths) != 0 {
		t.Errorf("expected 0 reinforced paths, got %d", len(topo.ReinforcedPaths))
	}
}

// --- 22.5-UNIT-015: [P0] GetReinforcedPaths sorted by Total descending (AC4) ---

func TestImmuneDaemon_GetReinforcedPaths_Sorted(t *testing.T) {
	// Given: a running ImmuneDaemon with multiple reinforced paths of different frequencies
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Create reinforced paths with different totals
	for range 10 { // 10 >= 5
		daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	}
	for range 7 { // 7 >= 5
		daemon.RecordCooperationTyped("agent-c", "agent-d", "msg")
	}
	for range 5 { // 5 >= 5 (exactly at threshold)
		daemon.RecordCooperationTyped("agent-e", "agent-f", "spawn")
	}

	// When: getting reinforced paths
	paths := daemon.GetReinforcedPaths()

	// Then: paths should be sorted by Total descending
	if paths == nil {
		t.Fatal("expected non-nil reinforced paths")
	}
	if len(paths) != 3 {
		t.Fatalf("expected 3 reinforced paths, got %d", len(paths))
	}
	for i := 1; i < len(paths); i++ {
		if paths[i].Total > paths[i-1].Total {
			t.Errorf("reinforced paths not sorted descending: index %d (Total=%d) > index %d (Total=%d)",
				i, paths[i].Total, i-1, paths[i-1].Total)
		}
	}
}

// --- 22.5-UNIT-016: [P0] GetReinforcedPaths nil daemon returns nil (AC4) ---

func TestImmuneDaemon_GetReinforcedPaths_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: getting reinforced paths
	paths := daemon.GetReinforcedPaths()

	// Then: returns nil without panic
	if paths != nil {
		t.Errorf("expected nil from nil daemon, got %v", paths)
	}
}

// --- 22.5-UNIT-017: [P1] GetReinforcedPaths with no paths above threshold (AC4) ---

func TestImmuneDaemon_GetReinforcedPaths_NoneAboveThreshold(t *testing.T) {
	// Given: a running ImmuneDaemon with only low-frequency cooperation
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Record below threshold
	for range 3 { // 3 < 5
		daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	}

	// When: getting reinforced paths
	paths := daemon.GetReinforcedPaths()

	// Then: returns nil or empty
	if len(paths) > 0 {
		t.Errorf("expected no reinforced paths below threshold, got %d", len(paths))
	}
}

// --- 22.5-UNIT-018: [P0] GetTopology node connections count (AC3) ---

func TestImmuneDaemon_GetTopology_NodeConnections(t *testing.T) {
	// Given: a running ImmuneDaemon with cooperation forming a star topology
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Star: hub connects to spoke1, spoke2, spoke3
	daemon.RecordCooperationTyped("hub", "spoke1", "spawn")
	daemon.RecordCooperationTyped("hub", "spoke2", "msg")
	daemon.RecordCooperationTyped("hub", "spoke3", "spawn")

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: hub node should have 3 connections
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	for _, node := range topo.Nodes {
		if node.Agent == "hub" {
			if node.Connections != 3 {
				t.Errorf("hub Connections = %d, want 3", node.Connections)
			}
			return
		}
	}
	t.Error("expected hub node in topology")
}

// --- 22.5-UNIT-019: [P1] GetTopology reinforced path at exact threshold (AC2) ---

func TestImmuneDaemon_GetTopology_ReinforcedAtExactThreshold(t *testing.T) {
	// Given: a running ImmuneDaemon with cooperation exactly at threshold
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Record exactly DefaultReinforcementThreshold events
	for range DefaultReinforcementThreshold {
		daemon.RecordCooperationTyped("agent-x", "agent-y", "spawn")
	}

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: edge should be reinforced (>= threshold, not just >)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	found := false
	for _, edge := range topo.Edges {
		if (edge.From == "agent-x" && edge.To == "agent-y") ||
			(edge.From == "agent-y" && edge.To == "agent-x") {
			found = true
			if !edge.Reinforced {
				t.Error("edge should be reinforced at exact threshold boundary (>= threshold)")
			}
			break
		}
	}
	if !found {
		t.Error("expected edge between agent-x and agent-y")
	}
}

// --- 22.5-UNIT-020: [P1] RecordCooperationTyped concurrent access (AC1) ---

func TestImmuneDaemon_RecordCooperationTyped_ConcurrentAccess(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: multiple goroutines record typed cooperation concurrently
	const goroutines = 10
	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
			daemon.RecordCooperationTyped("agent-a", "agent-b", "msg")
		})
	}
	wg.Wait()

	// Then: no data race (test runs with -race detector)
	topo := daemon.GetTopology()
	if topo == nil {
		t.Fatal("expected non-nil topology after concurrent writes")
	}
}

// --- 22.5-UNIT-021: [P1] GetTopology reputation default 0.0 when store unavailable (AC3) ---

func TestImmuneDaemon_GetTopology_NoReputationStore(t *testing.T) {
	// Given: a running ImmuneDaemon without reputation store
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")

	// When: querying topology (no reputation store set)
	topo := daemon.GetTopology()

	// Then: node reputation scores default to 0.0
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	for _, node := range topo.Nodes {
		if node.ReputationScore != 0.0 {
			t.Errorf("node %s ReputationScore = %f, want 0.0 (no reputation store)", node.Agent, node.ReputationScore)
		}
	}
}

// --- 22.5-UNIT-022: [P1] GetTopology edges are directional from lower to higher (AC3) ---

func TestImmuneDaemon_GetTopology_EdgeDirectionConsistent(t *testing.T) {
	// Given: a running ImmuneDaemon with cooperation in one direction
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.RecordCooperationTyped("agent-a", "agent-b", "spawn")
	daemon.RecordCooperationTyped("agent-b", "agent-a", "msg")

	// When: querying topology
	topo := daemon.GetTopology()

	// Then: should have exactly one edge (aggregated, not duplicated)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	edgeCount := 0
	for _, edge := range topo.Edges {
		if (edge.From == "agent-a" && edge.To == "agent-b") ||
			(edge.From == "agent-b" && edge.To == "agent-a") {
			edgeCount++
		}
	}
	if edgeCount != 1 {
		t.Errorf("expected exactly 1 edge between agent-a and agent-b, got %d", edgeCount)
	}
}
