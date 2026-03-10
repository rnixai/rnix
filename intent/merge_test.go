package intent

import (
	"slices"
	"testing"
	"time"
)

// --- Story 19.3 AC#1, AC#3: Add new nodes correctly ---

func TestMergeIncremental_AddNewNodes(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentCompleted},
			"backend": {ID: "backend", Intent: "implement API", State: IntentExecuting, DependsOn: []string{"design"}},
		},
		DesiredNodes: map[string]IntentState{
			"design":  IntentCompleted,
			"backend": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
		{ID: "comment", Intent: "implement comment feature", DependsOn: []string{"design"}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil MergeResult")
	}
	if len(result.AddedNodes) != 1 {
		t.Fatalf("expected 1 added node, got %d: %v", len(result.AddedNodes), result.AddedNodes)
	}
	if result.AddedNodes[0] != "comment" {
		t.Fatalf("expected added node 'comment', got %q", result.AddedNodes[0])
	}

	commentNode, ok := existing.Nodes["comment"]
	if !ok {
		t.Fatal("expected 'comment' node to be added to existing tree")
	}
	if commentNode.State != IntentPending {
		t.Fatalf("expected new node state=pending, got %q", commentNode.State)
	}
}

// --- Story 19.3 AC#5: Modified existing node resets state ---

func TestMergeIncremental_ModifyExistingNode(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentCompleted, Result: "done"},
			"backend": {ID: "backend", Intent: "implement API", State: IntentPending, DependsOn: []string{"design"}},
		},
		DesiredNodes: map[string]IntentState{
			"design":  IntentCompleted,
			"backend": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema with comments", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}
	if len(result.ModifiedNodes) != 1 {
		t.Fatalf("expected 1 modified node, got %d: %v", len(result.ModifiedNodes), result.ModifiedNodes)
	}
	if result.ModifiedNodes[0] != "design" {
		t.Fatalf("expected modified node 'design', got %q", result.ModifiedNodes[0])
	}

	designNode := existing.Nodes["design"]
	if designNode.State != IntentPending {
		t.Fatalf("expected modified node state reset to pending, got %q", designNode.State)
	}
	if designNode.Result != "" {
		t.Fatalf("expected modified node Result cleared, got %q", designNode.Result)
	}
}

// --- Story 19.3 AC#3: Unchanged nodes keep state ---

func TestMergeIncremental_UnchangedNodes(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentCompleted, Result: "done"},
			"backend": {ID: "backend", Intent: "implement API", State: IntentExecuting, DependsOn: []string{"design"}, PID: 42},
		},
		DesiredNodes: map[string]IntentState{
			"design":  IntentCompleted,
			"backend": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}
	if len(result.UnchangedNodes) != 2 {
		t.Fatalf("expected 2 unchanged nodes, got %d: %v", len(result.UnchangedNodes), result.UnchangedNodes)
	}

	designNode := existing.Nodes["design"]
	if designNode.State != IntentCompleted {
		t.Fatalf("expected unchanged completed node to remain completed, got %q", designNode.State)
	}
	if designNode.Result != "done" {
		t.Fatalf("expected unchanged node result preserved, got %q", designNode.Result)
	}

	backendNode := existing.Nodes["backend"]
	if backendNode.State != IntentExecuting {
		t.Fatalf("expected unchanged executing node to remain executing, got %q", backendNode.State)
	}
	if backendNode.PID != 42 {
		t.Fatalf("expected unchanged node PID preserved, got %d", backendNode.PID)
	}
}

// --- Story 19.3 AC#1: Completed node with same intent stays completed ---

func TestMergeIncremental_CompletedNodePreserved(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema", State: IntentCompleted, Result: "schema v2"},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}

	found := slices.Contains(result.UnchangedNodes, "design")
	if !found {
		t.Fatal("expected 'design' to be in unchanged nodes (same intent, already completed)")
	}

	designNode := existing.Nodes["design"]
	if designNode.State != IntentCompleted {
		t.Fatalf("expected completed node with same intent to stay completed, got %q", designNode.State)
	}
}

// --- Story 19.3 AC#5: Completed node with different intent resets to pending ---

func TestMergeIncremental_ModifiedCompletedNode(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema v1", State: IntentCompleted, Result: "done v1"},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema v2 with comments", DependsOn: []string{}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}
	if len(result.ModifiedNodes) != 1 || result.ModifiedNodes[0] != "design" {
		t.Fatalf("expected 'design' in modified nodes, got %v", result.ModifiedNodes)
	}

	designNode := existing.Nodes["design"]
	if designNode.State != IntentPending {
		t.Fatalf("expected modified completed node to reset to pending, got %q", designNode.State)
	}
	if designNode.Result != "" {
		t.Fatalf("expected modified node Result cleared, got %q", designNode.Result)
	}
	if designNode.Error != "" {
		t.Fatalf("expected modified node Error cleared, got %q", designNode.Error)
	}
}

// --- Story 19.3 AC#7: Invalid dependency returns error and rolls back modified nodes ---

func TestMergeIncremental_InvalidDependency(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema", State: IntentCompleted},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"nonexistent"}},
	}

	result, err := MergeIncremental(existing, newNodes)

	if err == nil {
		t.Fatal("expected error for invalid dependency reference, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result on error, got %+v", result)
	}
}

// --- Story 19.3 AC#7: Modified node state is fully rolled back on validation failure ---

func TestMergeIncremental_RollbackModifiedNodes(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design v1", State: IntentCompleted, Result: "done", PID: 42, RetryCount: 1},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	// Modify "design" (different intent) AND add invalid dependency
	newNodes := []*IntentNode{
		{ID: "design", Intent: "design v2", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"nonexistent"}},
	}

	_, err := MergeIncremental(existing, newNodes)
	if err == nil {
		t.Fatal("expected error for invalid dependency")
	}

	// Verify modified node was fully rolled back
	design := existing.Nodes["design"]
	if design.Intent != "design v1" {
		t.Fatalf("expected Intent rolled back to 'design v1', got %q", design.Intent)
	}
	if design.State != IntentCompleted {
		t.Fatalf("expected State rolled back to completed, got %q", design.State)
	}
	if design.Result != "done" {
		t.Fatalf("expected Result rolled back to 'done', got %q", design.Result)
	}
	if design.PID != 42 {
		t.Fatalf("expected PID rolled back to 42, got %d", design.PID)
	}
	if design.RetryCount != 1 {
		t.Fatalf("expected RetryCount rolled back to 1, got %d", design.RetryCount)
	}

	// Verify added node was also rolled back
	if _, exists := existing.Nodes["backend"]; exists {
		t.Fatal("expected added node 'backend' to be removed on rollback")
	}
}

// --- Story 19.3: Cycle dependency returns error ---

func TestMergeIncremental_CycleDependency(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema", State: IntentCompleted},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{"backend"}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
	}

	_, err := MergeIncremental(existing, newNodes)

	if err == nil {
		t.Fatal("expected error for cyclic dependency, got nil")
	}
}

// --- Story 19.3 AC#3: DesiredNodes updated with new nodes ---

func TestMergeIncremental_DesiredNodesUpdated(t *testing.T) {
	existing := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema", State: IntentCompleted},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	newNodes := []*IntentNode{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
		{ID: "comment", Intent: "implement comments", DependsOn: []string{"design"}},
	}

	_, err := MergeIncremental(existing, newNodes)

	if err != nil {
		t.Fatalf("MergeIncremental failed: %v", err)
	}

	if len(existing.DesiredNodes) != 3 {
		t.Fatalf("expected 3 desired nodes after merge, got %d", len(existing.DesiredNodes))
	}
	for _, id := range []string{"design", "backend", "comment"} {
		desired, ok := existing.DesiredNodes[id]
		if !ok {
			t.Fatalf("expected DesiredNodes to contain %q", id)
		}
		if desired != IntentCompleted {
			t.Fatalf("expected desired state for %q = completed, got %q", id, desired)
		}
	}
}

// --- Story 19.3: ResetNode method ---

func TestIntentTree_ResetNode(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {
				ID:         "design",
				Intent:     "design schema",
				State:      IntentCompleted,
				Result:     "done",
				Error:      "",
				PID:        42,
				RetryCount: 2,
				MaxRetries: 3,
				Timeout:    5 * time.Minute,
			},
		},
		CreatedAt: time.Now(),
	}

	tree.ResetNode("design")

	node := tree.Nodes["design"]
	if node.State != IntentPending {
		t.Fatalf("expected state reset to pending, got %q", node.State)
	}
	if node.Result != "" {
		t.Fatalf("expected Result cleared, got %q", node.Result)
	}
	if node.Error != "" {
		t.Fatalf("expected Error cleared, got %q", node.Error)
	}
	if node.PID != 0 {
		t.Fatalf("expected PID cleared, got %d", node.PID)
	}
	if node.RetryCount != 0 {
		t.Fatalf("expected RetryCount cleared, got %d", node.RetryCount)
	}
	// MaxRetries and Timeout should be preserved
	if node.MaxRetries != 3 {
		t.Fatalf("expected MaxRetries preserved (3), got %d", node.MaxRetries)
	}
	if node.Timeout != 5*time.Minute {
		t.Fatalf("expected Timeout preserved (5m), got %v", node.Timeout)
	}
}
