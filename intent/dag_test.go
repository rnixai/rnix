package intent

import (
	"strings"
	"testing"
	"time"
)

// --- Story 19.1 ATDD: Intent DAG Tests (AC: #1, #3) ---
// Tests for BuildIntentDAG, DetectCycle, and TopologicalSort.
// Follows the same DAG testing patterns as compose/dag_test.go.

func TestBuildIntentDAG_NoDeps(t *testing.T) {
	t.Skip("ATDD RED: BuildIntentDAG not yet implemented")

	// Given: an IntentTree where no node has dependencies
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "parallel tasks",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending},
			"c": {ID: "c", Intent: "task C", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}

	// When: building the DAG
	dag, err := BuildIntentDAG(tree)

	// Then: DAG has 3 nodes, no edges
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG")
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

func TestBuildIntentDAG_LinearDeps(t *testing.T) {
	t.Skip("ATDD RED: BuildIntentDAG not yet implemented")

	// Given: a linear chain design -> backend -> test
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "sequential pipeline",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentPending},
			"backend": {ID: "backend", Intent: "implement API", State: IntentPending, DependsOn: []string{"design"}},
			"test":    {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: building the DAG
	dag, err := BuildIntentDAG(tree)

	// Then: nodes have correct dependency edges
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}
	nodeBackend := dag.Nodes["backend"]
	if nodeBackend == nil {
		t.Fatal("expected node 'backend'")
	}
	if len(nodeBackend.DependsOn) != 1 || nodeBackend.DependsOn[0] != "design" {
		t.Fatalf("node 'backend' should depend on ['design'], got %v", nodeBackend.DependsOn)
	}

	nodeDesign := dag.Nodes["design"]
	if len(nodeDesign.DependedBy) < 1 {
		t.Fatalf("node 'design' should have DependedBy including 'backend', got %v", nodeDesign.DependedBy)
	}
}

func TestBuildIntentDAG_DiamondDeps(t *testing.T) {
	t.Skip("ATDD RED: BuildIntentDAG not yet implemented")

	// Given: diamond — design -> backend, design -> frontend, both -> deploy
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "diamond workflow",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design", State: IntentPending},
			"backend":  {ID: "backend", Intent: "backend", State: IntentPending, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "frontend", State: IntentPending, DependsOn: []string{"design"}},
			"deploy":   {ID: "deploy", Intent: "deploy", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: building the DAG
	dag, err := BuildIntentDAG(tree)

	// Then: deploy depends on both backend and frontend
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}
	if len(dag.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(dag.Nodes))
	}
	nodeDeploy := dag.Nodes["deploy"]
	if len(nodeDeploy.DependsOn) != 2 {
		t.Fatalf("node 'deploy' should have 2 dependencies, got %d", len(nodeDeploy.DependsOn))
	}
}

func TestBuildIntentDAG_CycleDetection(t *testing.T) {
	t.Skip("ATDD RED: BuildIntentDAG cycle detection not yet implemented")

	// Given: a cyclic dependency A -> B -> A
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "cyclic",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending, DependsOn: []string{"b"}},
			"b": {ID: "b", Intent: "task B", State: IntentPending, DependsOn: []string{"a"}},
		},
		CreatedAt: time.Now(),
	}

	// When: building the DAG
	_, err := BuildIntentDAG(tree)

	// Then: error indicating cycle detected
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error should mention 'cycle', got: %q", err.Error())
	}
}

func TestBuildIntentDAG_SelfCycle(t *testing.T) {
	t.Skip("ATDD RED: BuildIntentDAG self-cycle detection not yet implemented")

	// Given: a node depends on itself
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "self cycle",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "recursive task", State: IntentPending, DependsOn: []string{"a"}},
		},
		CreatedAt: time.Now(),
	}

	// When: building the DAG
	_, err := BuildIntentDAG(tree)

	// Then: cycle detected
	if err == nil {
		t.Fatal("expected cycle detection error for self-dependency, got nil")
	}
}

func TestTopologicalSort_AllParallel(t *testing.T) {
	t.Skip("ATDD RED: TopologicalSort not yet implemented")

	// Given: a DAG with 3 independent nodes
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "parallel",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending},
			"c": {ID: "c", Intent: "task C", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: all nodes in a single layer (can execute in parallel)
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
	t.Skip("ATDD RED: TopologicalSort not yet implemented")

	// Given: a linear chain A -> B -> C
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "sequential",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "step 1", State: IntentPending},
			"b": {ID: "b", Intent: "step 2", State: IntentPending, DependsOn: []string{"a"}},
			"c": {ID: "c", Intent: "step 3", State: IntentPending, DependsOn: []string{"b"}},
		},
		CreatedAt: time.Now(),
	}
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: 3 layers, each with 1 node, in order A, B, C
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}
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
	t.Skip("ATDD RED: TopologicalSort not yet implemented")

	// Given: diamond — design -> backend, design -> frontend, both -> deploy
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "diamond",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design", State: IntentPending},
			"backend":  {ID: "backend", Intent: "backend", State: IntentPending, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "frontend", State: IntentPending, DependsOn: []string{"design"}},
			"deploy":   {ID: "deploy", Intent: "deploy", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: 3 layers — [design], [backend, frontend], [deploy]
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}
	if len(layers[0]) != 1 || layers[0][0] != "design" {
		t.Fatalf("expected layer 0 = ['design'], got %v", layers[0])
	}
	if len(layers[1]) != 2 {
		t.Fatalf("expected layer 1 with 2 nodes, got %d", len(layers[1]))
	}
	layer1Set := make(map[string]bool)
	for _, n := range layers[1] {
		layer1Set[n] = true
	}
	if !layer1Set["backend"] || !layer1Set["frontend"] {
		t.Fatalf("expected layer 1 = {backend, frontend}, got %v", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0] != "deploy" {
		t.Fatalf("expected layer 2 = ['deploy'], got %v", layers[2])
	}
}

func TestTopologicalSort_ComplexGraph(t *testing.T) {
	t.Skip("ATDD RED: TopologicalSort not yet implemented")

	// Given: A(root), B->A, C->A, D->B, E->{C,D}
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "complex",
		State:      IntentPending,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "root", State: IntentPending},
			"b": {ID: "b", Intent: "b", State: IntentPending, DependsOn: []string{"a"}},
			"c": {ID: "c", Intent: "c", State: IntentPending, DependsOn: []string{"a"}},
			"d": {ID: "d", Intent: "d", State: IntentPending, DependsOn: []string{"b"}},
			"e": {ID: "e", Intent: "e", State: IntentPending, DependsOn: []string{"c", "d"}},
		},
		CreatedAt: time.Now(),
	}
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		t.Fatalf("BuildIntentDAG failed: %v", err)
	}

	// When: computing topological sort
	layers, err := dag.TopologicalSort()

	// Then: ordering constraints are respected
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	nodeLayer := make(map[string]int)
	for i, layer := range layers {
		for _, name := range layer {
			nodeLayer[name] = i
		}
	}
	if nodeLayer["a"] >= nodeLayer["b"] {
		t.Fatal("a must be before b")
	}
	if nodeLayer["a"] >= nodeLayer["c"] {
		t.Fatal("a must be before c")
	}
	if nodeLayer["b"] >= nodeLayer["d"] {
		t.Fatal("b must be before d")
	}
	if nodeLayer["c"] >= nodeLayer["e"] {
		t.Fatal("c must be before e")
	}
	if nodeLayer["d"] >= nodeLayer["e"] {
		t.Fatal("d must be before e")
	}
}
