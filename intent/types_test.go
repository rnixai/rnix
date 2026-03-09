package intent

import (
	"testing"
	"time"
)

// --- Story 19.1 ATDD: IntentTree Data Model Tests (AC: #1, #4) ---
// These tests verify IntentTree helper methods: Progress, RunnableNodes,
// MarkCompleted, MarkFailed, IsTerminal.
// All tests are in RED phase: stubs return zero values.

func TestIntentTree_Progress(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.Progress not yet implemented")

	// Given: an IntentTree with 4 nodes — 2 completed, 1 executing, 1 pending
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design data model", State: IntentCompleted},
			"backend":  {ID: "backend", Intent: "implement backend", State: IntentCompleted},
			"frontend": {ID: "frontend", Intent: "implement frontend", State: IntentExecuting},
			"test":     {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: computing progress
	completed, total := tree.Progress()

	// Then: completed=2, total=4
	if total != 4 {
		t.Fatalf("expected total=4, got %d", total)
	}
	if completed != 2 {
		t.Fatalf("expected completed=2, got %d", completed)
	}
}

func TestIntentTree_RunnableNodes(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.RunnableNodes not yet implemented")

	// Given: an IntentTree where "design" is completed, "backend" and "frontend" depend on "design"
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design data model", State: IntentCompleted},
			"backend":  {ID: "backend", Intent: "implement backend", State: IntentPending, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "implement frontend", State: IntentPending, DependsOn: []string{"design"}},
			"test":     {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: getting runnable nodes
	runnable := tree.RunnableNodes()

	// Then: "backend" and "frontend" are runnable (their dependency "design" is completed)
	if len(runnable) != 2 {
		t.Fatalf("expected 2 runnable nodes, got %d", len(runnable))
	}
	ids := make(map[string]bool)
	for _, n := range runnable {
		ids[n.ID] = true
	}
	if !ids["backend"] || !ids["frontend"] {
		t.Fatalf("expected runnable nodes to be 'backend' and 'frontend', got %v", ids)
	}
}

func TestIntentTree_RunnableNodes_NoneReady(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.RunnableNodes not yet implemented")

	// Given: all nodes have unsatisfied dependencies
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"backend":  {ID: "backend", Intent: "implement backend", State: IntentPending, DependsOn: []string{"design"}},
			"design":   {ID: "design", Intent: "design data model", State: IntentExecuting},
			"frontend": {ID: "frontend", Intent: "implement frontend", State: IntentPending, DependsOn: []string{"design"}},
		},
		CreatedAt: time.Now(),
	}

	// When: getting runnable nodes
	runnable := tree.RunnableNodes()

	// Then: no nodes are runnable
	if len(runnable) != 0 {
		t.Fatalf("expected 0 runnable nodes, got %d", len(runnable))
	}
}

func TestIntentTree_MarkCompleted(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.MarkCompleted not yet implemented")

	// Given: an IntentTree with a pending node
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design data model", State: IntentExecuting},
			"backend": {ID: "backend", Intent: "implement backend", State: IntentPending, DependsOn: []string{"design"}},
		},
		CreatedAt: time.Now(),
	}

	// When: marking "design" as completed
	tree.MarkCompleted("design", "schema designed successfully")

	// Then: "design" state is completed with result
	node := tree.Nodes["design"]
	if node.State != IntentCompleted {
		t.Fatalf("expected design state=%q, got %q", IntentCompleted, node.State)
	}
	if node.Result != "schema designed successfully" {
		t.Fatalf("expected result=%q, got %q", "schema designed successfully", node.Result)
	}
}

func TestIntentTree_MarkFailed(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.MarkFailed not yet implemented")

	// Given: an IntentTree with a chain: design -> backend -> test
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design data model", State: IntentExecuting},
			"backend": {ID: "backend", Intent: "implement backend", State: IntentPending, DependsOn: []string{"design"}},
			"test":    {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: marking "design" as failed
	tree.MarkFailed("design", "LLM timeout")

	// Then: "design" is failed, and downstream "backend" and "test" are also failed (cascade)
	if tree.Nodes["design"].State != IntentFailed {
		t.Fatalf("expected design state=%q, got %q", IntentFailed, tree.Nodes["design"].State)
	}
	if tree.Nodes["design"].Error != "LLM timeout" {
		t.Fatalf("expected design error=%q, got %q", "LLM timeout", tree.Nodes["design"].Error)
	}
	if tree.Nodes["backend"].State != IntentFailed {
		t.Fatalf("expected backend state=%q (cascade), got %q", IntentFailed, tree.Nodes["backend"].State)
	}
	if tree.Nodes["test"].State != IntentFailed {
		t.Fatalf("expected test state=%q (cascade), got %q", IntentFailed, tree.Nodes["test"].State)
	}
}

func TestIntentTree_MarkFailed_IndependentBranchNotAffected(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.MarkFailed not yet implemented")

	// Given: diamond — design -> backend, design -> frontend; backend fails
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design", State: IntentCompleted},
			"backend":  {ID: "backend", Intent: "backend", State: IntentExecuting, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "frontend", State: IntentExecuting, DependsOn: []string{"design"}},
			"deploy":   {ID: "deploy", Intent: "deploy", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}

	// When: marking "backend" as failed
	tree.MarkFailed("backend", "compilation error")

	// Then: "frontend" is NOT affected (independent branch), "deploy" IS affected
	if tree.Nodes["frontend"].State != IntentExecuting {
		t.Fatalf("expected frontend still executing, got %q", tree.Nodes["frontend"].State)
	}
	if tree.Nodes["deploy"].State != IntentFailed {
		t.Fatalf("expected deploy failed (depends on failed backend), got %q", tree.Nodes["deploy"].State)
	}
}

func TestIntentTree_IsTerminal_AllCompleted(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")

	// Given: all nodes are completed
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "simple task",
		State:      IntentCompleted,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task a", State: IntentCompleted},
			"b": {ID: "b", Intent: "task b", State: IntentCompleted},
		},
		CreatedAt: time.Now(),
	}

	// When: checking if terminal
	terminal := tree.IsTerminal()

	// Then: true — all nodes are in terminal state
	if !terminal {
		t.Fatal("expected IsTerminal()=true when all nodes completed")
	}
}

func TestIntentTree_IsTerminal_MixedCompletedAndFailed(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")

	// Given: some nodes completed, some failed — all terminal
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "partial success",
		State:      IntentFailed,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task a", State: IntentCompleted},
			"b": {ID: "b", Intent: "task b", State: IntentFailed},
		},
		CreatedAt: time.Now(),
	}

	// When: checking if terminal
	terminal := tree.IsTerminal()

	// Then: true — both completed and failed are terminal
	if !terminal {
		t.Fatal("expected IsTerminal()=true when all nodes are completed or failed")
	}
}

func TestIntentTree_IsTerminal_StillExecuting(t *testing.T) {
	t.Skip("ATDD RED: IntentTree.IsTerminal not yet implemented")

	// Given: one node still executing
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "in progress",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task a", State: IntentCompleted},
			"b": {ID: "b", Intent: "task b", State: IntentExecuting},
		},
		CreatedAt: time.Now(),
	}

	// When: checking if terminal
	terminal := tree.IsTerminal()

	// Then: false — node b is still executing
	if terminal {
		t.Fatal("expected IsTerminal()=false when a node is still executing")
	}
}
