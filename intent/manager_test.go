package intent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestManager_Apply(t *testing.T) {
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
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	tree, err := mgr.Apply(context.Background(), ApplyRequest{
		Intent: "build a blog system",
		Model:  "claude-sonnet",
	})

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
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	tree1, err1 := mgr.Apply(context.Background(), ApplyRequest{Intent: "first"})
	tree2, err2 := mgr.Apply(context.Background(), ApplyRequest{Intent: "second"})

	if err1 != nil || err2 != nil {
		t.Fatalf("Apply failed: err1=%v, err2=%v", err1, err2)
	}
	if tree1.ID == tree2.ID {
		t.Fatalf("expected unique IDs, both got %q", tree1.ID)
	}
}

func TestManager_Confirm(t *testing.T) {
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	tree, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "test"})
	intentID := tree.ID

	err := mgr.Confirm(intentID)

	if err != nil {
		t.Fatalf("Confirm failed: %v", err)
	}
}

func TestManager_Confirm_NotFound(t *testing.T) {
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	err := mgr.Confirm("intent-999")

	if err == nil {
		t.Fatal("expected error for non-existent intent, got nil")
	}
}

func TestManager_Status(t *testing.T) {
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	tree, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "check status"})

	status, err := mgr.Status(tree.ID)

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
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	_, err := mgr.Status("intent-404")

	if err == nil {
		t.Fatal("expected error for non-existent intent, got nil")
	}
}

func TestManager_ListActive(t *testing.T) {
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	mgr.Apply(context.Background(), ApplyRequest{Intent: "intent 1"})
	mgr.Apply(context.Background(), ApplyRequest{Intent: "intent 2"})

	active := mgr.ListActive()

	if len(active) != 2 {
		t.Fatalf("expected 2 active intents, got %d", len(active))
	}
}

func TestManager_ListActive_ExcludesTerminal(t *testing.T) {
	caller := &mockLLMCaller{response: `[{"id":"a","intent":"task","depends_on":[]}]`}
	decomposer := NewDecomposer(caller)
	spawner := newMockIntentSpawner()
	mgr := NewManager(decomposer, spawner, DefaultReconcilerConfig())

	tree1, _ := mgr.Apply(context.Background(), ApplyRequest{Intent: "will complete"})
	mgr.Apply(context.Background(), ApplyRequest{Intent: "still active"})

	tree1.State = IntentCompleted
	now := time.Now()
	tree1.CompletedAt = &now
	for _, node := range tree1.Nodes {
		node.State = IntentCompleted
	}

	active := mgr.ListActive()

	if len(active) != 1 {
		t.Fatalf("expected 1 active intent, got %d", len(active))
	}
}
