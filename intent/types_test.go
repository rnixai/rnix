package intent

import (
	"testing"
	"time"
)

func TestIntentTree_Progress(t *testing.T) {
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

	completed, total := tree.Progress()

	if total != 4 {
		t.Fatalf("expected total=4, got %d", total)
	}
	if completed != 2 {
		t.Fatalf("expected completed=2, got %d", completed)
	}
}

func TestIntentTree_RunnableNodes(t *testing.T) {
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

	runnable := tree.RunnableNodes()

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

	runnable := tree.RunnableNodes()

	if len(runnable) != 0 {
		t.Fatalf("expected 0 runnable nodes, got %d", len(runnable))
	}
}

func TestIntentTree_MarkCompleted(t *testing.T) {
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

	tree.MarkCompleted("design", "schema designed successfully")

	node := tree.Nodes["design"]
	if node.State != IntentCompleted {
		t.Fatalf("expected design state=%q, got %q", IntentCompleted, node.State)
	}
	if node.Result != "schema designed successfully" {
		t.Fatalf("expected result=%q, got %q", "schema designed successfully", node.Result)
	}
}

func TestIntentTree_MarkFailed(t *testing.T) {
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

	tree.MarkFailed("design", "LLM timeout")

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

	tree.MarkFailed("backend", "compilation error")

	if tree.Nodes["frontend"].State != IntentExecuting {
		t.Fatalf("expected frontend still executing, got %q", tree.Nodes["frontend"].State)
	}
	if tree.Nodes["deploy"].State != IntentFailed {
		t.Fatalf("expected deploy failed (depends on failed backend), got %q", tree.Nodes["deploy"].State)
	}
}

func TestIntentTree_IsTerminal_AllCompleted(t *testing.T) {
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

	terminal := tree.IsTerminal()

	if !terminal {
		t.Fatal("expected IsTerminal()=true when all nodes completed")
	}
}

func TestIntentTree_IsTerminal_MixedCompletedAndFailed(t *testing.T) {
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

	terminal := tree.IsTerminal()

	if !terminal {
		t.Fatal("expected IsTerminal()=true when all nodes are completed or failed")
	}
}

func TestIntentTree_IsTerminal_StillExecuting(t *testing.T) {
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

	terminal := tree.IsTerminal()

	if terminal {
		t.Fatal("expected IsTerminal()=false when a node is still executing")
	}
}
