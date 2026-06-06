package intent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// TestReconciler_InjectUpstreamContext verifies F4 (rnix-eval upstream finding):
// a node's completed dependency results are injected into node.Context so a
// strongly-coupled sequential sub-task (e.g. "write the result of the call")
// can see prior output, instead of being spawned with an isolated context that
// has no result to write.
func TestReconciler_InjectUpstreamContext(t *testing.T) {
	tree := &IntentTree{
		Nodes: map[string]*IntentNode{
			"call":  {ID: "call", Intent: "make tool call", State: IntentCompleted, Result: "tool returned 23 tools"},
			"write": {ID: "write", Intent: "write the call result", DependsOn: []string{"call"}, State: IntentPending},
		},
	}
	r := &Reconciler{tree: tree}

	r.injectUpstreamContext(tree.Nodes["write"])
	got := tree.Nodes["write"].Context
	if !strings.Contains(got, "tool returned 23 tools") {
		t.Errorf("write.Context = %q, want to contain upstream result", got)
	}
	if !strings.Contains(got, "call") {
		t.Errorf("write.Context = %q, want to reference upstream node id 'call'", got)
	}

	// A node with no dependencies gets no injected context.
	r.injectUpstreamContext(tree.Nodes["call"])
	if tree.Nodes["call"].Context != "" {
		t.Errorf("call.Context = %q, want empty (no dependencies)", tree.Nodes["call"].Context)
	}
}

// TestReconciler_InjectUpstreamContext_SkipsEmptyResult verifies that
// dependencies without a result contribute nothing (no dangling header).
func TestReconciler_InjectUpstreamContext_SkipsEmptyResult(t *testing.T) {
	tree := &IntentTree{
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", State: IntentCompleted, Result: ""},
			"b": {ID: "b", DependsOn: []string{"a"}, State: IntentPending},
		},
	}
	r := &Reconciler{tree: tree}
	r.injectUpstreamContext(tree.Nodes["b"])
	if tree.Nodes["b"].Context != "" {
		t.Errorf("b.Context = %q, want empty when upstream result is empty", tree.Nodes["b"].Context)
	}
}

// f4Spawner is a KernelSpawner whose Wait returns a per-node Result on the
// ExitStatus (simulating a child process's real output), and which records the
// node.Context captured at spawn time. It lets the test assert the FULL F4 data
// flow — child output → ExitStatus.Result → MarkCompleted → node.Result →
// injectUpstreamContext → dependent's spawn context — rather than hand-stuffing
// node.Result (the gap an auditor flagged in the unit test above).
type f4Spawner struct {
	mu          sync.Mutex
	nodeResults map[string]string         // node ID → output it produces
	pidToNode   map[types.PID]*IntentNode // pid → node (to map Wait back)
	spawnedCtx  map[string]string         // node ID → node.Context at spawn time
	nextPID     uint64
}

func (s *f4Spawner) SpawnIntent(_ context.Context, node *IntentNode) (types.PID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextPID++
	pid := types.PID(s.nextPID)
	s.pidToNode[pid] = node
	s.spawnedCtx[node.ID] = node.Context // capture the injected upstream context
	return pid, nil
}

func (s *f4Spawner) Wait(pid types.PID) (ExitStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.pidToNode[pid]
	return ExitStatus{Code: 0, Reason: "completed", Result: s.nodeResults[node.ID]}, nil
}

func (s *f4Spawner) Kill(_ types.PID) error { return nil }

// TestReconciler_InjectUpstreamContext_EndToEnd drives the real reconciler data
// flow: the "call" node completes with a real Result on its ExitStatus, and the
// dependent "write" node must be spawned with that result in its context.
func TestReconciler_InjectUpstreamContext_EndToEnd(t *testing.T) {
	tree := &IntentTree{
		ID:    "intent-e2e",
		State: IntentAwaitConfirm,
		Nodes: map[string]*IntentNode{
			"call":  {ID: "call", Intent: "call the tool", State: IntentPending},
			"write": {ID: "write", Intent: "write the call result", DependsOn: []string{"call"}, State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	sp := &f4Spawner{
		nodeResults: map[string]string{"call": "PLAYWRIGHT_23_TOOLS", "write": "done"},
		pidToNode:   map[types.PID]*IntentNode{},
		spawnedCtx:  map[string]string{},
	}
	rec, err := NewReconciler(tree, sp, DefaultReconcilerConfig(), noopReconcilerCallbacks())
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	if err := rec.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sp.mu.Lock()
	writeCtx := sp.spawnedCtx["write"]
	sp.mu.Unlock()
	if !strings.Contains(writeCtx, "PLAYWRIGHT_23_TOOLS") {
		t.Errorf("write spawn context = %q, want to contain upstream 'call' real result (full data flow)", writeCtx)
	}
}
