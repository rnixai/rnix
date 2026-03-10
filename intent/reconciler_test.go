package intent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

func noopReconcilerCallbacks() ReconcilerCallbacks {
	return ReconcilerCallbacks{}
}

// --- AC#2, AC#3: Reconciler executes all nodes to completion ---

func TestReconciler_Execute_AllSuccess(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "all success reconciler",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentPending},
			"backend": {ID: "backend", Intent: "implement API", State: IntentPending, DependsOn: []string{"design"}},
			"test":    {ID: "test", Intent: "write tests", State: IntentPending, DependsOn: []string{"backend"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	execErr := reconciler.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	order := spawner.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d", len(order))
	}
	for _, node := range tree.Nodes {
		if node.State != IntentCompleted {
			t.Fatalf("expected node %q completed, got %q", node.ID, node.State)
		}
	}
	if !tree.IsTerminal() {
		t.Fatal("expected tree to be terminal after all nodes complete")
	}
}

// --- AC#2: Retry on failure ---

func TestReconciler_Execute_RetrySuccess(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "retry success",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "flaky task", State: IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	// First spawn (PID 1) fails, second spawn (PID 2) succeeds
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "transient error", err: fmt.Errorf("flaky failure")}

	var mu sync.Mutex
	var retryEvents []struct{ nodeID string; attempt int }
	callbacks := ReconcilerCallbacks{
		OnNodeRetry: func(nodeID string, attempt int, maxRetries int) {
			mu.Lock()
			retryEvents = append(retryEvents, struct{ nodeID string; attempt int }{nodeID, attempt})
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	execErr := reconciler.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	node := tree.Nodes["a"]
	if node.State != IntentCompleted {
		t.Fatalf("expected node completed after retry, got %q", node.State)
	}
	if node.RetryCount != 1 {
		t.Fatalf("expected RetryCount=1, got %d", node.RetryCount)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(retryEvents) != 1 {
		t.Fatalf("expected 1 OnNodeRetry callback, got %d", len(retryEvents))
	}
}

// --- AC#4: Retry exhausted → final failure + cascade ---

func TestReconciler_Execute_RetryExhausted(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "retry exhausted",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "always fails", State: IntentPending, MaxRetries: 2},
			"b": {ID: "b", Intent: "depends on a", State: IntentPending, DependsOn: []string{"a"}},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	// All spawns fail
	for pid := types.PID(1); pid <= 3; pid++ {
		spawner.results[pid] = mockIntentExecResult{code: 1, reason: "permanent error", err: fmt.Errorf("always fails")}
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	nodeA := tree.Nodes["a"]
	if nodeA.State != IntentFailed {
		t.Fatalf("expected node 'a' failed after retry exhaustion, got %q", nodeA.State)
	}
	if nodeA.RetryCount != 2 {
		t.Fatalf("expected RetryCount=2 (MaxRetries), got %d", nodeA.RetryCount)
	}

	nodeB := tree.Nodes["b"]
	if nodeB.State != IntentFailed {
		t.Fatalf("expected node 'b' cascade-failed, got %q", nodeB.State)
	}
}

// --- AC#5: Timeout triggers retry ---

func TestReconciler_Execute_Timeout(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "timeout retry",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"slow": {ID: "slow", Intent: "slow task", State: IntentPending, MaxRetries: 2, Timeout: 100 * time.Millisecond},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	// PID 1: times out (delay > node timeout); PID 2: succeeds immediately
	spawner.results[types.PID(1)] = mockIntentExecResult{delay: 5 * time.Second}

	var mu sync.Mutex
	var timeoutEvents []string
	callbacks := ReconcilerCallbacks{
		OnNodeTimeout: func(nodeID string) {
			mu.Lock()
			timeoutEvents = append(timeoutEvents, nodeID)
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	execErr := reconciler.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	node := tree.Nodes["slow"]
	if node.State != IntentCompleted {
		t.Fatalf("expected node completed after timeout+retry, got %q", node.State)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(timeoutEvents) != 1 {
		t.Fatalf("expected 1 OnNodeTimeout callback, got %d", len(timeoutEvents))
	}
}

// --- AC#5: Timeout exhausts retries ---

func TestReconciler_Execute_TimeoutExhausted(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "timeout exhausted",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"slow": {ID: "slow", Intent: "always slow", State: IntentPending, MaxRetries: 1, Timeout: 50 * time.Millisecond},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	// All spawns time out
	for pid := types.PID(1); pid <= 2; pid++ {
		spawner.results[pid] = mockIntentExecResult{delay: 5 * time.Second}
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	node := tree.Nodes["slow"]
	if node.State != IntentFailed {
		t.Fatalf("expected node failed after timeout exhaustion, got %q", node.State)
	}
}

// --- AC#4: Cascade failure after retry exhaustion ---

func TestReconciler_Execute_CascadeAfterExhausted(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "cascade after exhaust",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"root":    {ID: "root", Intent: "root task", State: IntentPending, MaxRetries: 1},
			"child1":  {ID: "child1", Intent: "child 1", State: IntentPending, DependsOn: []string{"root"}},
			"child2":  {ID: "child2", Intent: "child 2", State: IntentPending, DependsOn: []string{"root"}},
			"grandch": {ID: "grandch", Intent: "grandchild", State: IntentPending, DependsOn: []string{"child1"}},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	for pid := types.PID(1); pid <= 2; pid++ {
		spawner.results[pid] = mockIntentExecResult{code: 1, reason: "fail", err: fmt.Errorf("always fails")}
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	for _, id := range []string{"root", "child1", "child2", "grandch"} {
		if tree.Nodes[id].State != IntentFailed {
			t.Fatalf("expected node %q failed (cascade), got %q", id, tree.Nodes[id].State)
		}
	}
}

// --- AC#2: Parallel nodes with one retrying ---

func TestReconciler_Execute_ParallelWithRetry(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "parallel with retry",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"stable": {ID: "stable", Intent: "always succeeds", State: IntentPending},
			"flaky":  {ID: "flaky", Intent: "fails then succeeds", State: IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	// "flaky" gets PID 1 or 2 depending on spawn order; we set PID 2 to fail (assuming sorted order: flaky=1, stable=2)
	// Since spawn order is sorted by ID, flaky < stable alphabetically
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "transient", err: fmt.Errorf("first attempt")}
	// PID 2 (stable) succeeds (default)
	// PID 3 (flaky retry) succeeds (default)

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	execErr := reconciler.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	if tree.Nodes["stable"].State != IntentCompleted {
		t.Fatalf("expected 'stable' completed, got %q", tree.Nodes["stable"].State)
	}
	if tree.Nodes["flaky"].State != IntentCompleted {
		t.Fatalf("expected 'flaky' completed after retry, got %q", tree.Nodes["flaky"].State)
	}
}

// --- Context cancellation ---

func TestReconciler_Execute_ContextCancel(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "cancellable reconciler",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "slow task", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()
	spawner.results[types.PID(1)] = mockIntentExecResult{delay: 5 * time.Second}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	execErr := reconciler.Execute(ctx)
	if execErr == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

// --- AC#1, AC#2: Drift detection callback ---

func TestReconciler_Execute_DriftDetectedCallback(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "drift detection",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "will fail once", State: IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "error", err: fmt.Errorf("drift-causing failure")}

	var mu sync.Mutex
	var driftEvents []DriftItem
	callbacks := ReconcilerCallbacks{
		OnDriftDetected: func(drift DriftItem) {
			mu.Lock()
			driftEvents = append(driftEvents, drift)
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(driftEvents) == 0 {
		t.Fatal("expected at least one OnDriftDetected callback")
	}
	if driftEvents[0].NodeID != "a" {
		t.Fatalf("expected drift for node 'a', got %q", driftEvents[0].NodeID)
	}
	if driftEvents[0].Type != DriftNodeFailed {
		t.Fatalf("expected drift type %q, got %q", DriftNodeFailed, driftEvents[0].Type)
	}
}

// --- AC#2: Drift resolved after successful retry ---

func TestReconciler_Execute_DriftResolvedCallback(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "drift resolved",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "flaky", State: IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "error", err: fmt.Errorf("first fail")}

	var mu sync.Mutex
	var resolvedNodes []string
	callbacks := ReconcilerCallbacks{
		OnDriftResolved: func(nodeID string) {
			mu.Lock()
			resolvedNodes = append(resolvedNodes, nodeID)
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(resolvedNodes) == 0 {
		t.Fatal("expected OnDriftResolved callback after retry success")
	}
	if resolvedNodes[0] != "a" {
		t.Fatalf("expected drift resolved for node 'a', got %q", resolvedNodes[0])
	}
}

// --- NFR40: Drift-to-action latency ≤ 5s ---

func TestReconciler_Execute_NFR40_Latency(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "nfr40 latency",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "measure latency", State: IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	spawner := newMockIntentSpawner()
	spawner.results[types.PID(1)] = mockIntentExecResult{code: 1, reason: "fail", err: fmt.Errorf("trigger retry")}

	var mu sync.Mutex
	var failedAt, retriedAt time.Time
	callbacks := ReconcilerCallbacks{
		OnNodeFailed: func(nodeID string, errMsg string) {
			mu.Lock()
			if failedAt.IsZero() {
				failedAt = time.Now()
			}
			mu.Unlock()
		},
		OnNodeRetry: func(nodeID string, attempt int, maxRetries int) {
			mu.Lock()
			if retriedAt.IsZero() {
				retriedAt = time.Now()
			}
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	_ = reconciler.Execute(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if failedAt.IsZero() {
		t.Fatal("expected OnNodeFailed callback to record timestamp")
	}
	if retriedAt.IsZero() {
		t.Fatal("expected OnNodeRetry callback to record timestamp")
	}
	latency := retriedAt.Sub(failedAt)
	if latency > 5*time.Second {
		t.Fatalf("NFR40 violated: drift-to-action latency %v exceeds 5s", latency)
	}
}

// --- All callbacks fired ---

func TestReconciler_Callbacks(t *testing.T) {
	tree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "callbacks test",
		State:      IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task A", State: IntentPending},
			"b": {ID: "b", Intent: "task B", State: IntentPending, DependsOn: []string{"a"}},
		},
		CreatedAt: time.Now(),
	}
	spawner := newMockIntentSpawner()

	var mu sync.Mutex
	var startCalls, completeCalls []string
	var progressCalls []struct{ completed, total int }

	callbacks := ReconcilerCallbacks{
		OnNodeStart: func(nodeID string, pid types.PID) {
			mu.Lock()
			startCalls = append(startCalls, nodeID)
			mu.Unlock()
		},
		OnNodeComplete: func(nodeID string, result string) {
			mu.Lock()
			completeCalls = append(completeCalls, nodeID)
			mu.Unlock()
		},
		OnProgress: func(completed, total int) {
			mu.Lock()
			progressCalls = append(progressCalls, struct{ completed, total int }{completed, total})
			mu.Unlock()
		},
	}

	reconciler, err := NewReconciler(tree, spawner, DefaultReconcilerConfig(), callbacks)
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	execErr := reconciler.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute failed: %v", execErr)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(startCalls) != 2 {
		t.Fatalf("expected 2 OnNodeStart callbacks, got %d", len(startCalls))
	}
	if len(completeCalls) != 2 {
		t.Fatalf("expected 2 OnNodeComplete callbacks, got %d", len(completeCalls))
	}
	if len(progressCalls) == 0 {
		t.Fatal("expected at least one OnProgress callback")
	}
	last := progressCalls[len(progressCalls)-1]
	if last.completed != 2 || last.total != 2 {
		t.Fatalf("expected final progress 2/2, got %d/%d", last.completed, last.total)
	}
}
