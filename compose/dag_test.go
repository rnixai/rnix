package compose

import (
	"strings"
	"testing"
)

// --- Story 7.1: DAG Construction and Topological Sort Tests ---
// These tests verify AC #1 (DAG construction), AC #2 (cycle detection), and AC #3 (topological sort).
// All tests are in RED phase: they reference BuildDAG/DetectCycle/TopologicalSort which do not exist yet.

func TestBuildDAG_NoDeps(t *testing.T) {
	// Given: a ComposeSpec where no agent has dependencies
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "parallel tasks",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "task A"},
			"b": {Intent: "task B"},
			"c": {Intent: "task C"},
		},
	}

	// When: building the DAG
	dag, err := BuildDAG(spec)

	// Then: DAG is built with 3 nodes and no edges
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}
	if len(dag.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(dag.Nodes))
	}
	for name, node := range dag.Nodes {
		if len(node.DependsOn) != 0 {
			t.Fatalf("node %q should have no dependencies, got %v", name, node.DependsOn)
		}
	}
}

func TestBuildDAG_LinearDeps(t *testing.T) {
	// Given: a linear dependency chain A -> B -> C
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "sequential pipeline",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1"},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
		},
	}

	// When: building the DAG
	dag, err := BuildDAG(spec)

	// Then: nodes have correct dependencies
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	nodeB := dag.Nodes["b"]
	if nodeB == nil {
		t.Fatal("expected node 'b'")
	}
	if len(nodeB.DependsOn) != 1 || nodeB.DependsOn[0] != "a" {
		t.Fatalf("node 'b' should depend on ['a'], got %v", nodeB.DependsOn)
	}

	nodeC := dag.Nodes["c"]
	if nodeC == nil {
		t.Fatal("expected node 'c'")
	}
	if len(nodeC.DependsOn) != 1 || nodeC.DependsOn[0] != "b" {
		t.Fatalf("node 'c' should depend on ['b'], got %v", nodeC.DependsOn)
	}

	// Verify DependedBy (reverse edges)
	nodeA := dag.Nodes["a"]
	if nodeA == nil {
		t.Fatal("expected node 'a'")
	}
	if len(nodeA.DependedBy) != 1 || nodeA.DependedBy[0] != "b" {
		t.Fatalf("node 'a' DependedBy should be ['b'], got %v", nodeA.DependedBy)
	}
}

func TestBuildDAG_DiamondDeps(t *testing.T) {
	// Given: a diamond dependency: A -> B, A -> C, B -> D, C -> D
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "diamond workflow",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "root"},
			"b": {Intent: "branch 1", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "branch 2", DependsOn: map[string]string{"a": "completed"}},
			"d": {Intent: "join", DependsOn: map[string]string{"b": "completed", "c": "completed"}},
		},
	}

	// When: building the DAG
	dag, err := BuildDAG(spec)

	// Then: node D depends on both B and C
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}
	if len(dag.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(dag.Nodes))
	}

	nodeD := dag.Nodes["d"]
	if nodeD == nil {
		t.Fatal("expected node 'd'")
	}
	if len(nodeD.DependsOn) != 2 {
		t.Fatalf("node 'd' should have 2 dependencies, got %d", len(nodeD.DependsOn))
	}
}

func TestDetectCycle_NoCycle(t *testing.T) {
	// Given: a valid DAG with no cycles (linear A -> B -> C)
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "no cycle",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1"},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
		},
	}

	// When: building the DAG (which calls DetectCycle internally)
	dag, err := BuildDAG(spec)

	// Then: no cycle detected
	if err != nil {
		t.Fatalf("BuildDAG should succeed for acyclic graph, got: %v", err)
	}

	// Verify DetectCycle returns no cycle
	cyclePath, cycleErr := dag.DetectCycle()
	if cycleErr != nil {
		t.Fatalf("DetectCycle should return nil for acyclic graph, got: %v", cycleErr)
	}
	if cyclePath != nil {
		t.Fatalf("cycle path should be nil, got: %v", cyclePath)
	}
}

func TestDetectCycle_SimpleCycle(t *testing.T) {
	// Given: a graph with cycle A -> B -> A
	// Note: We need to construct the DAG manually or use a lower-level builder
	// since BuildDAG should reject cycles.
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "cyclic",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1", DependsOn: map[string]string{"b": "completed"}},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
		},
	}

	// When: building the DAG
	_, err := BuildDAG(spec)

	// Then: cycle detected error
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}

	// Verify error message contains cycle information
	errMsg := err.Error()
	if !strings.Contains(errMsg, "cycle") {
		t.Fatalf("error should mention 'cycle', got: %q", errMsg)
	}
}

func TestDetectCycle_ComplexCycle(t *testing.T) {
	// Given: a graph with 3-node cycle A -> B -> C -> A
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "complex cycle",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1", DependsOn: map[string]string{"c": "completed"}},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
		},
	}

	// When: building the DAG
	_, err := BuildDAG(spec)

	// Then: cycle detected
	if err == nil {
		t.Fatal("expected cycle detection error for A->B->C->A, got nil")
	}
}

func TestDetectCycle_SelfCycle(t *testing.T) {
	// Given: an agent depends on itself
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "self cycle",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "recursive", DependsOn: map[string]string{"a": "completed"}},
		},
	}

	// When: building the DAG
	_, err := BuildDAG(spec)

	// Then: cycle detected
	if err == nil {
		t.Fatal("expected cycle detection error for self-dependency, got nil")
	}
}

func TestDetectCycle_PartialCycle(t *testing.T) {
	// Given: a graph where some nodes are acyclic but a subset has a cycle
	// D -> A (acyclic), A -> B -> C -> A (cycle)
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "partial cycle",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1", DependsOn: map[string]string{"c": "completed"}},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
			"d": {Intent: "step 4", DependsOn: map[string]string{"a": "completed"}},
		},
	}

	// When: building the DAG
	_, err := BuildDAG(spec)

	// Then: cycle detected
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestTopologicalSort_AllParallel(t *testing.T) {
	// Given: a DAG with no dependencies (all nodes in one layer)
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "all parallel",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "task A"},
			"b": {Intent: "task B"},
			"c": {Intent: "task C"},
		},
	}
	dag, err := BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: all nodes in a single layer
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 3 {
		t.Fatalf("expected 3 nodes in first layer, got %d", len(layers[0]))
	}
}

func TestTopologicalSort_Sequential(t *testing.T) {
	// Given: a linear chain A -> B -> C (each layer has 1 node)
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "sequential",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1"},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
		},
	}
	dag, err := BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: 3 layers, each with 1 node
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}
	for i, layer := range layers {
		if len(layer) != 1 {
			t.Fatalf("layer %d should have 1 node, got %d", i, len(layer))
		}
	}

	// Verify order: A first, B second, C third
	if layers[0][0] != "a" {
		t.Fatalf("expected layer 0 = ['a'], got %v", layers[0])
	}
	if layers[1][0] != "b" {
		t.Fatalf("expected layer 1 = ['b'], got %v", layers[1])
	}
	if layers[2][0] != "c" {
		t.Fatalf("expected layer 2 = ['c'], got %v", layers[2])
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	// Given: diamond A -> B, A -> C, B -> D, C -> D
	// Expected layers: [A], [B,C], [D]
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "diamond",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "root"},
			"b": {Intent: "left", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "right", DependsOn: map[string]string{"a": "completed"}},
			"d": {Intent: "join", DependsOn: map[string]string{"b": "completed", "c": "completed"}},
		},
	}
	dag, err := BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: 3 layers
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}

	// Layer 0: only A
	if len(layers[0]) != 1 || layers[0][0] != "a" {
		t.Fatalf("expected layer 0 = ['a'], got %v", layers[0])
	}

	// Layer 1: B and C (in any order)
	if len(layers[1]) != 2 {
		t.Fatalf("expected layer 1 with 2 nodes, got %d", len(layers[1]))
	}
	layer1Set := make(map[string]bool)
	for _, n := range layers[1] {
		layer1Set[n] = true
	}
	if !layer1Set["b"] || !layer1Set["c"] {
		t.Fatalf("expected layer 1 to contain 'b' and 'c', got %v", layers[1])
	}

	// Layer 2: only D
	if len(layers[2]) != 1 || layers[2][0] != "d" {
		t.Fatalf("expected layer 2 = ['d'], got %v", layers[2])
	}
}

func TestTopologicalSort_ComplexGraph(t *testing.T) {
	// Given: a more complex graph
	// A (no deps), B depends on A, C depends on A, D depends on B, E depends on C and D
	// Expected layers: [A], [B,C], [D], [E]
	// Or: [A], [B,C], [D], [E] — D must come before E because E depends on D
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "complex",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "root"},
			"b": {Intent: "b", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "c", DependsOn: map[string]string{"a": "completed"}},
			"d": {Intent: "d", DependsOn: map[string]string{"b": "completed"}},
			"e": {Intent: "e", DependsOn: map[string]string{"c": "completed", "d": "completed"}},
		},
	}
	dag, err := BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: layers respect all dependency constraints
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// Verify ordering constraints: for each node, all its dependencies appear in earlier layers
	nodeLayer := make(map[string]int)
	for i, layer := range layers {
		for _, name := range layer {
			nodeLayer[name] = i
		}
	}

	// A must be before B and C
	if nodeLayer["a"] >= nodeLayer["b"] {
		t.Fatal("a must be before b")
	}
	if nodeLayer["a"] >= nodeLayer["c"] {
		t.Fatal("a must be before c")
	}
	// B must be before D
	if nodeLayer["b"] >= nodeLayer["d"] {
		t.Fatal("b must be before d")
	}
	// C and D must be before E
	if nodeLayer["c"] >= nodeLayer["e"] {
		t.Fatal("c must be before e")
	}
	if nodeLayer["d"] >= nodeLayer["e"] {
		t.Fatal("d must be before e")
	}
}

func TestTopologicalSort_SingleNode(t *testing.T) {
	// Given: a DAG with a single node
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "solo",
		Agents: map[string]*AgentSpec{
			"solo": {Intent: "only one"},
		},
	}
	dag, err := BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: 1 layer with 1 node
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 1 || layers[0][0] != "solo" {
		t.Fatalf("expected layer 0 = ['solo'], got %v", layers[0])
	}
}
