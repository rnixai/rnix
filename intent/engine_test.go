package intent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// --- Story 19.1 ATDD: Intent Engine Tests (AC: #3, #6) ---
// Tests for the event-driven intent execution engine.
// Follows patterns from compose/engine_test.go with adaptations for intent domain.

// --- Mock KernelSpawner for Intent Engine ---

type mockIntentSpawnRecord struct {
	nodeID string
	intent string
}

type mockIntentSpawner struct {
	mu       sync.Mutex
	spawned  []mockIntentSpawnRecord
	pidAlloc uint64
	results  map[types.PID]mockIntentExecResult
	spawnErr error
}

type mockIntentExecResult struct {
	code   int
	reason string
	err    error
	delay  time.Duration
}

func newMockIntentSpawner() *mockIntentSpawner {
	return &mockIntentSpawner{
		results: make(map[types.PID]mockIntentExecResult),
	}
}

func (m *mockIntentSpawner) SpawnIntent(ctx context.Context, node *IntentNode) (types.PID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spawnErr != nil {
		return 0, m.spawnErr
	}
	m.pidAlloc++
	pid := types.PID(m.pidAlloc)
	m.spawned = append(m.spawned, mockIntentSpawnRecord{nodeID: node.ID, intent: node.Intent})
	return pid, nil
}

func (m *mockIntentSpawner) Wait(pid types.PID) (ExitStatus, error) {
	m.mu.Lock()
	result, ok := m.results[pid]
	m.mu.Unlock()

	if ok && result.delay > 0 {
		time.Sleep(result.delay)
	}
	if ok && result.err != nil {
		return ExitStatus{Code: result.code, Reason: result.reason}, result.err
	}
	if ok {
		return ExitStatus{Code: result.code, Reason: result.reason}, nil
	}
	return ExitStatus{Code: 0, Reason: "ok"}, nil
}

func (m *mockIntentSpawner) getSpawnOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	order := make([]string, len(m.spawned))
	for i, rec := range m.spawned {
		order[i] = rec.nodeID
	}
	return order
}

// noopCallbacks returns EngineCallbacks that do nothing (for tests that don't check callbacks).
func noopCallbacks() EngineCallbacks {
	return EngineCallbacks{}
}

func TestEngine_Execute_Sequential(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute not yet implemented")

	// Given: a linear intent chain design -> backend -> test
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "sequential pipeline",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentPending},
			"backend": {ID: "backend", Intent: "implement API", State: IntentPending, DependsOn: []string{"design"}},
			"test":    {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	execErr := engine.Execute(context.Background())

	// Then: all 3 nodes spawned in order
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}
	order := spawner.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d", len(order))
	}
	if order[0] != "design" {
		t.Fatalf("expected first spawn 'design', got %q", order[0])
	}
	if order[1] != "backend" {
		t.Fatalf("expected second spawn 'backend', got %q", order[1])
	}
	if order[2] != "test" {
		t.Fatalf("expected third spawn 'test', got %q", order[2])
	}
}

func TestEngine_Execute_Parallel(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute parallel scheduling not yet implemented")

	// Given: 3 independent nodes (no dependencies)
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "parallel tasks",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending},
			"c": {ID: "c", Intent: "task C", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	execErr := engine.Execute(context.Background())

	// Then: all 3 nodes are spawned
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}
	order := spawner.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d", len(order))
	}
}

func TestEngine_Execute_AllSuccess(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute success path not yet implemented")

	// Given: diamond — design -> backend, design -> frontend, both -> deploy
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "full success",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design", State: IntentPending},
			"backend":  {ID: "backend", Intent: "backend", State: IntentPending, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "frontend", State: IntentPending, DependsOn: []string{"design"}},
			"deploy":   {ID: "deploy", Intent: "deploy", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	execErr := engine.Execute(context.Background())

	// Then: all 4 nodes succeed, tree is terminal
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}
	order := spawner.getSpawnOrder()
	if len(order) != 4 {
		t.Fatalf("expected 4 spawns, got %d", len(order))
	}
	if !tree.IsTerminal() {
		t.Fatal("expected tree to be terminal after all nodes complete")
	}
}

func TestEngine_Execute_CascadeFailure(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute failure cascade not yet implemented")

	// Given: linear chain design -> backend -> test; design will fail
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "cascade failure",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "will fail", State: IntentPending},
			"backend": {ID: "backend", Intent: "depends on design", State: IntentPending, DependsOn: []string{"design"}},
			"test":    {ID: "test", Intent: "depends on backend", State: IntentPending, DependsOn: []string{"backend"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	// PID 1 (design) will fail
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "crashed", err: fmt.Errorf("process failed")}

	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	_ = engine.Execute(context.Background())

	// Then: only design was spawned, backend and test were NOT spawned (cascade failure)
	order := spawner.getSpawnOrder()
	if len(order) != 1 {
		t.Fatalf("expected 1 spawn (only design), got %d: %v", len(order), order)
	}
	if order[0] != "design" {
		t.Fatalf("expected only 'design' to be spawned, got %q", order[0])
	}

	// Verify cascade: backend and test should be in failed state
	if tree.Nodes["backend"].State != IntentFailed {
		t.Fatalf("expected backend state=failed (cascade), got %q", tree.Nodes["backend"].State)
	}
	if tree.Nodes["test"].State != IntentFailed {
		t.Fatalf("expected test state=failed (cascade), got %q", tree.Nodes["test"].State)
	}
}

func TestEngine_Execute_PartialFailure(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute partial failure not yet implemented")

	// Given: diamond where backend fails but frontend succeeds independently
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "partial failure",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"design":   {ID: "design", Intent: "design", State: IntentPending},
			"backend":  {ID: "backend", Intent: "will fail", State: IntentPending, DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "will succeed", State: IntentPending, DependsOn: []string{"design"}},
			"deploy":   {ID: "deploy", Intent: "needs both", State: IntentPending, DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	// PID 2 (backend, second spawn after design) will fail
	spawner.results[types.PID(2)] = mockIntentExecResult{code: 1, reason: "failed", err: fmt.Errorf("compilation error")}

	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	_ = engine.Execute(context.Background())

	// Then: design + backend + frontend spawned, but deploy NOT spawned
	order := spawner.getSpawnOrder()
	spawnedSet := make(map[string]bool)
	for _, id := range order {
		spawnedSet[id] = true
	}
	if !spawnedSet["design"] {
		t.Fatal("expected 'design' to be spawned")
	}
	if !spawnedSet["frontend"] {
		t.Fatal("expected 'frontend' to be spawned (independent branch)")
	}
	if spawnedSet["deploy"] {
		t.Fatal("'deploy' should NOT be spawned (upstream 'backend' failed)")
	}
}

func TestEngine_Execute_ContextCancel(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute context cancellation not yet implemented")

	// Given: nodes with delay
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "cancellable",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "slow task", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	spawner.results[types.PID(1)] = mockIntentExecResult{delay: 5 * time.Second}

	engine, err := NewEngine(tree, spawner, noopCallbacks())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing with a short-lived context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	execErr := engine.Execute(ctx)

	// Then: context cancellation error
	if execErr == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestEngine_Execute_Callbacks(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute callbacks not yet implemented")

	// Given: a simple two-node chain, with recording callbacks
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "callback test",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending, DependsOn: []string{"a"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()

	var mu sync.Mutex
	var started []string
	var completed []string

	callbacks := EngineCallbacks{
		OnNodeStart: func(nodeID string, pid types.PID) {
			mu.Lock()
			started = append(started, nodeID)
			mu.Unlock()
		},
		OnNodeComplete: func(nodeID string, result string) {
			mu.Lock()
			completed = append(completed, nodeID)
			mu.Unlock()
		},
	}

	engine, err := NewEngine(tree, spawner, callbacks)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	execErr := engine.Execute(context.Background())

	// Then: callbacks fired for both nodes
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("expected 2 OnNodeStart callbacks, got %d", len(started))
	}
	if len(completed) != 2 {
		t.Fatalf("expected 2 OnNodeComplete callbacks, got %d", len(completed))
	}
}

func TestEngine_Execute_ProgressCallback(t *testing.T) {
	t.Skip("ATDD RED: Engine.Execute progress callback not yet implemented")

	// Given: 3 independent nodes with progress tracking
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "progress test",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending},
			"c": {ID: "c", Intent: "task C", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()

	var mu sync.Mutex
	var progressCalls []struct{ completed, total int }

	callbacks := EngineCallbacks{
		OnProgress: func(completed, total int) {
			mu.Lock()
			progressCalls = append(progressCalls, struct{ completed, total int }{completed, total})
			mu.Unlock()
		},
	}

	engine, err := NewEngine(tree, spawner, callbacks)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	execErr := engine.Execute(context.Background())

	// Then: progress callbacks fired, final call shows 3/3
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progressCalls) == 0 {
		t.Fatal("expected at least one OnProgress callback")
	}
	last := progressCalls[len(progressCalls)-1]
	if last.completed != 3 || last.total != 3 {
		t.Fatalf("expected final progress 3/3, got %d/%d", last.completed, last.total)
	}
}
