package intent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- Story 19.1 ATDD: IntentManager Tests (AC: #1, #4, #6) ---
// Tests for intent lifecycle management: Apply, Confirm, Status, ListActive.

func TestManager_Apply(t *testing.T) {
	t.Skip("ATDD RED: Manager.Apply not yet implemented")

	// Given: a Manager with mock decomposer returning valid decomposition
	nodes := []struct {
		ID        string   `json:"id"`
		Intent    string   `json:"intent"`
		DependsOn []string `json:"depends_on"`
	}{
		{ID: "design", Intent: "design schema", DependsOn: []string{}},
		{ID: "backend", Intent: "implement API", DependsOn: []string{"design"}},
	}
	jsonBytes, _ := json.Marshal(nodes)
	caller := &mockLLMCaller{response: string(jsonBytes)}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	// When: applying an intent
	tree, err := mgr.Apply(context.Background(), ApplyRequest{
		Intent: "build a blog system",
		Model:  "claude-sonnet",
	})

	// Then: IntentTree is created with a unique ID and await_confirm state
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil IntentTree")
	}
	if tree.ID == "" {
		t.Fatal("expected non-empty IntentID")
	}
	if tree.State != IntentAwaitConfirm {
		t.Fatalf("expected state=%q, got %q", IntentAwaitConfirm, tree.State)
	}
	if len(tree.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(tree.Nodes))
	}
}

func TestManager_Apply_GeneratesUniqueIDs(t *testing.T) {
	t.Skip("ATDD RED: Manager.Apply ID generation not yet implemented")

	// Given: a Manager
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	// When: applying two intents
	tree1, err1 := mgr.Apply(context.Background(), ApplyRequest{Intent: "first"})
	tree2, err2 := mgr.Apply(context.Background(), ApplyRequest{Intent: "second"})

	// Then: both succeed with different IDs
	if err1 != nil || err2 != nil {
		t.Fatalf("Apply failed: err1=%v, err2=%v", err1, err2)
	}
	if tree1.ID == tree2.ID {
		t.Fatalf("expected unique IDs, both got %q", tree1.ID)
	}
}

func TestManager_Confirm(t *testing.T) {
	t.Skip("ATDD RED: Manager.Confirm not yet implemented")

	// Given: a Manager with an applied (await_confirm) intent
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	tree, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "test"})
	intentID := tree.ID

	// When: confirming the intent
	err := mgr.Confirm(intentID)

	// Then: no error, intent transitions out of await_confirm
	if err != nil {
		t.Fatalf("Confirm failed: %v", err)
	}
}

func TestManager_Confirm_NotFound(t *testing.T) {
	t.Skip("ATDD RED: Manager.Confirm error handling not yet implemented")

	// Given: a Manager with no intents
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	// When: confirming a non-existent intent
	err := mgr.Confirm("intent-999")

	// Then: error indicating intent not found
	if err == nil {
		t.Fatal("expected error for non-existent intent, got nil")
	}
}

func TestManager_Status(t *testing.T) {
	t.Skip("ATDD RED: Manager.Status not yet implemented")

	// Given: a Manager with an applied intent
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	tree, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "check status"})

	// When: querying status
	status, err := mgr.Status(tree.ID)

	// Then: returns the same tree
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.ID != tree.ID {
		t.Fatalf("expected ID=%q, got %q", tree.ID, status.ID)
	}
}

func TestManager_Status_NotFound(t *testing.T) {
	t.Skip("ATDD RED: Manager.Status error handling not yet implemented")

	// Given: an empty Manager
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	// When: querying a non-existent intent
	_, err := mgr.Status("intent-404")

	// Then: error
	if err == nil {
		t.Fatal("expected error for non-existent intent, got nil")
	}
}

func TestManager_ListActive(t *testing.T) {
	t.Skip("ATDD RED: Manager.ListActive not yet implemented")

	// Given: a Manager with 2 applied intents
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	mgr.Apply(context.Background(), ApplyRequest{Intent: "intent 1"})
	mgr.Apply(context.Background(), ApplyRequest{Intent: "intent 2"})

	// When: listing active intents
	active := mgr.ListActive()

	// Then: both intents are listed (both are non-terminal)
	if len(active) != 2 {
		t.Fatalf("expected 2 active intents, got %d", len(active))
	}
}

func TestManager_ListActive_ExcludesTerminal(t *testing.T) {
	t.Skip("ATDD RED: Manager.ListActive terminal exclusion not yet implemented")

	// Given: a Manager with one completed and one active intent
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner)

	tree1, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "will complete"})
	mgr.Apply(context.Background(), ApplyRequest{Intent: "still active"})

	// Manually mark tree1 as completed
	tree1.State = IntentCompleted
	now := time.Now()
	tree1.CompletedAt = &now
	for _, node := range tree1.Nodes {
		node.State = IntentCompleted
	}

	// When: listing active intents
	active := mgr.ListActive()

	// Then: only 1 active intent (the one not completed)
	if len(active) != 1 {
		t.Fatalf("expected 1 active intent, got %d", len(active))
	}
}
